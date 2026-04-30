package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxUploadSize = 500 << 20
	jobRetention  = 24 * time.Hour
	cleanupPeriod = 30 * time.Minute
)

var (
	filenameCleaner = regexp.MustCompile(`[^a-zA-Z0-9-_]+`)
	hexColorPattern = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)
)

type Server struct {
	mux       *http.ServeMux
	distDir   string
	outputDir string
	tempDir   string
	storage   StorageConfig
	tokenMu   sync.Mutex
	token     string
	tokenAt   time.Time
}

type conversionRequest struct {
	FPS              float64
	Width            int
	Height           int
	FitMode          string
	Background       string
	Start            float64
	Duration         float64
	Speed            float64
	Loop             int
	MaxColors        int
	Dither           string
	PaletteStatsMode string
	DiffMode         string
	ScaleAlgorithm   string
	Reverse          bool
	OutputName       string
}

type conversionResponse struct {
	JobID        string   `json:"jobId"`
	OutputURL    string   `json:"outputUrl"`
	DownloadURL  string   `json:"downloadUrl"`
	DownloadName string   `json:"downloadName"`
	SizeBytes    int64    `json:"sizeBytes"`
	ExpiresAt    string   `json:"expiresAt"`
	Commands     []string `json:"commands"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type healthResponse struct {
	Available bool   `json:"available"`
	FFmpeg    string `json:"ffmpeg"`
}

func NewServer(distDir, outputDir, tempDir string, storage StorageConfig) (*Server, error) {
	for _, directory := range []string{outputDir, tempDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", directory, err)
		}
	}

	server := &Server{
		mux:       http.NewServeMux(),
		distDir:   distDir,
		outputDir: outputDir,
		tempDir:   tempDir,
		storage:   storage,
	}

	server.routes()
	go server.cleanupLoop()

	return server, nil
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mux.ServeHTTP(writer, request)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/convert", s.handleConvert)
	s.mux.HandleFunc("/api/download/", s.handleDownload)
	s.mux.HandleFunc("/api/gifs/", s.handleDeleteGIF)
	s.mux.HandleFunc("/outputs/", s.handleOutput)
	s.mux.HandleFunc("/", s.handleSPA)
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		writeJSON(writer, http.StatusOK, healthResponse{
			Available: false,
			FFmpeg:    "",
		})
		return
	}

	writeJSON(writer, http.StatusOK, healthResponse{
		Available: true,
		FFmpeg:    ffmpegPath,
	})
}

func (s *Server) handleConvert(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{
			Error: "ffmpeg is not installed or not available in PATH",
		})
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxUploadSize)
	requestStartedAt := time.Now()

	jobID, err := newJobID()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "unable to create job id"})
		return
	}

	workspace := filepath.Join(s.tempDir, jobID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "unable to create temp workspace"})
		return
	}
	defer os.RemoveAll(workspace)

	params, inputPath, sourceName, err := streamUploadToWorkspace(request, workspace)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	uploadFinishedAt := time.Now()

	baseName := params.OutputName
	if baseName == "" {
		baseName = sanitizeName(strings.TrimSuffix(sourceName, filepath.Ext(sourceName)))
	}
	if baseName == "" {
		baseName = "clip"
	}

	outputFilename := fmt.Sprintf("%s-%s.gif", baseName, jobID[:8])
	outputPath := filepath.Join(s.outputDir, outputFilename)

	renderCommands, renderErr := renderGIF(ffmpegPath, workspace, inputPath, outputPath, params)
	if renderErr != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{
			Error: renderErr.Error(),
		})
		return
	}
	renderFinishedAt := time.Now()

	info, err := os.Stat(outputPath)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "render completed but output file is missing"})
		return
	}

	expiresAt := info.ModTime().Add(jobRetention)
	localOutputPath := outputPath
	outputSize := info.Size()

	log.Printf(
		"job %s ready in %s (upload=%s render=%s storage=%s)",
		jobID,
		time.Since(requestStartedAt).Round(time.Millisecond),
		uploadFinishedAt.Sub(requestStartedAt).Round(time.Millisecond),
		renderFinishedAt.Sub(uploadFinishedAt).Round(time.Millisecond),
		storageModeLabel(s.storage.Enabled()),
	)

	writeJSON(writer, http.StatusOK, conversionResponse{
		JobID:        jobID,
		OutputURL:    "/outputs/" + outputFilename,
		DownloadURL:  "/api/download/" + outputFilename,
		DownloadName: outputFilename,
		SizeBytes:    info.Size(),
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		Commands:     renderCommands,
	})

	if s.storage.Enabled() {
		go s.uploadOutputToOpenListAsync(localOutputPath, outputFilename, jobID, outputSize)
	}
}

func (s *Server) handleOutput(writer http.ResponseWriter, request *http.Request) {
	s.serveOutput(writer, request, false)
}

func (s *Server) handleDownload(writer http.ResponseWriter, request *http.Request) {
	s.serveOutput(writer, request, true)
}

func (s *Server) handleDeleteGIF(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodDelete {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	outputName, err := s.parseOutputName(request.URL.Path, "/api/gifs/")
	if err != nil {
		http.NotFound(writer, request)
		return
	}

	asset, err := s.resolveOutputAsset(outputName)
	if err != nil {
		http.NotFound(writer, request)
		return
	}

	if removeErr := s.deleteOutputAsset(asset); removeErr != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "unable to delete gif"})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]string{
		"deleted": outputName,
	})
}

func (s *Server) serveOutput(writer http.ResponseWriter, request *http.Request, asAttachment bool) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.NotFound(writer, request)
		return
	}

	prefix := "/outputs/"
	if asAttachment {
		prefix = "/api/download/"
	}

	outputName, err := s.parseOutputName(request.URL.Path, prefix)
	if err != nil {
		http.NotFound(writer, request)
		return
	}

	asset, err := s.resolveOutputAsset(outputName)
	if err != nil {
		http.NotFound(writer, request)
		return
	}

	if asset.ExpiresAt.Before(time.Now()) {
		_ = s.deleteOutputAsset(asset)
		http.NotFound(writer, request)
		return
	}

	if asset.Remote {
		s.proxyOpenListFile(writer, request, asset, asAttachment)
		return
	}

	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Expires", asset.ExpiresAt.UTC().Format(http.TimeFormat))
	if asAttachment {
		writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", outputName))
	}

	http.ServeFile(writer, request, asset.LocalPath)
}

func (s *Server) parseOutputName(requestPath, prefix string) (string, error) {
	outputName := strings.TrimPrefix(path.Clean(requestPath), prefix)
	outputName = strings.TrimPrefix(outputName, "/")
	if outputName == "" || outputName == "." || outputName != path.Base(outputName) {
		return "", os.ErrNotExist
	}

	return outputName, nil
}

func (s *Server) handleSPA(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.NotFound(writer, request)
		return
	}

	if strings.HasPrefix(request.URL.Path, "/api/") {
		http.NotFound(writer, request)
		return
	}

	cleanPath := path.Clean(request.URL.Path)
	if cleanPath == "." {
		cleanPath = "/"
	}

	if cleanPath != "/" && filepath.Ext(cleanPath) != "" {
		assetPath := filepath.Join(s.distDir, strings.TrimPrefix(cleanPath, "/"))
		if fileInfo, err := os.Stat(assetPath); err == nil && !fileInfo.IsDir() {
			http.ServeFile(writer, request, assetPath)
			return
		}
		http.NotFound(writer, request)
		return
	}

	indexPath := filepath.Join(s.distDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		renderMissingFrontend(writer)
		return
	}

	http.ServeFile(writer, request, indexPath)
}

func parseConversionRequest(values url.Values) (conversionRequest, error) {
	fitMode, err := parseChoice(values.Get("fitMode"), "contain", []string{"contain", "cover", "stretch", "original"})
	if err != nil {
		return conversionRequest{}, err
	}

	dither, err := parseChoice(values.Get("dither"), "sierra2_4a", []string{
		"none",
		"bayer",
		"heckbert",
		"floyd_steinberg",
		"sierra2",
		"sierra2_4a",
	})
	if err != nil {
		return conversionRequest{}, err
	}

	paletteStatsMode, err := parseChoice(values.Get("paletteStatsMode"), "full", []string{"full", "diff", "single"})
	if err != nil {
		return conversionRequest{}, err
	}

	diffMode, err := parseChoice(values.Get("diffMode"), "rectangle", []string{"rectangle", "none"})
	if err != nil {
		return conversionRequest{}, err
	}

	scaleAlgorithm, err := parseChoice(values.Get("scaleAlgorithm"), "lanczos", []string{
		"lanczos",
		"bicubic",
		"bilinear",
		"neighbor",
		"spline",
	})
	if err != nil {
		return conversionRequest{}, err
	}

	background, err := normalizeHexColor(values.Get("background"), "#0f172a")
	if err != nil {
		return conversionRequest{}, err
	}

	return conversionRequest{
		FPS:              parseFloat(values.Get("fps"), 12, 1, 60),
		Width:            parseInt(values.Get("width"), 0, 0, 4096),
		Height:           parseInt(values.Get("height"), 0, 0, 4096),
		FitMode:          fitMode,
		Background:       background,
		Start:            parseFloat(values.Get("start"), 0, 0, 86400),
		Duration:         parseFloat(values.Get("duration"), 0, 0, 86400),
		Speed:            parseFloat(values.Get("speed"), 1, 0.1, 8),
		Loop:             parseInt(values.Get("loop"), 0, 0, 1000),
		MaxColors:        parseInt(values.Get("maxColors"), 128, 2, 256),
		Dither:           dither,
		PaletteStatsMode: paletteStatsMode,
		DiffMode:         diffMode,
		ScaleAlgorithm:   scaleAlgorithm,
		Reverse:          parseBool(values.Get("reverse")),
		OutputName:       sanitizeName(values.Get("outputName")),
	}, nil
}

func buildFFmpegCommand(inputPath, outputPath string, params conversionRequest) []string {
	timeArgs := make([]string, 0, 4)
	if params.Start > 0 {
		timeArgs = append(timeArgs, "-ss", formatSeconds(params.Start))
	}
	if params.Duration > 0 {
		timeArgs = append(timeArgs, "-t", formatSeconds(params.Duration))
	}

	baseFilter := strings.Join(buildVideoFilters(params), ",")
	filterGraph := fmt.Sprintf(
		"[0:v]%s,split=2[palette_src][gif_src];[palette_src]palettegen=max_colors=%d:stats_mode=%s[palette];[gif_src][palette]paletteuse=dither=%s:diff_mode=%s[gif]",
		baseFilter,
		params.MaxColors,
		params.PaletteStatsMode,
		params.Dither,
		params.DiffMode,
	)

	commandArgs := append([]string{"-y"}, timeArgs...)
	commandArgs = append(commandArgs,
		"-i", inputPath,
		"-filter_complex", filterGraph,
		"-map", "[gif]",
		"-an",
		"-loop", strconv.Itoa(params.Loop),
		outputPath,
	)

	return commandArgs
}

func buildFFmpegFallbackCommands(inputPath, palettePath, outputPath string, params conversionRequest) ([]string, []string) {
	timeArgs := make([]string, 0, 4)
	if params.Start > 0 {
		timeArgs = append(timeArgs, "-ss", formatSeconds(params.Start))
	}
	if params.Duration > 0 {
		timeArgs = append(timeArgs, "-t", formatSeconds(params.Duration))
	}

	baseFilter := strings.Join(buildVideoFilters(params), ",")
	paletteFilter := fmt.Sprintf("%s,palettegen=max_colors=%d:stats_mode=%s", baseFilter, params.MaxColors, params.PaletteStatsMode)
	renderFilter := fmt.Sprintf("%s[stream];[stream][1:v]paletteuse=dither=%s:diff_mode=%s", baseFilter, params.Dither, params.DiffMode)

	paletteArgs := append([]string{"-y"}, timeArgs...)
	paletteArgs = append(paletteArgs,
		"-i", inputPath,
		"-vf", paletteFilter,
		palettePath,
	)

	renderArgs := append([]string{"-y"}, timeArgs...)
	renderArgs = append(renderArgs,
		"-i", inputPath,
		"-i", palettePath,
		"-lavfi", renderFilter,
		"-an",
		"-loop", strconv.Itoa(params.Loop),
		outputPath,
	)

	return paletteArgs, renderArgs
}

func buildVideoFilters(params conversionRequest) []string {
	filters := make([]string, 0, 8)

	if params.Speed != 1 {
		filters = append(filters, fmt.Sprintf("setpts=PTS/%.4f", params.Speed))
	}

	if params.Reverse {
		filters = append(filters, "reverse")
	}

	if scaleFilter := buildScaleFilter(params); scaleFilter != "" {
		filters = append(filters, scaleFilter)
	}

	filters = append(filters, fmt.Sprintf("fps=%.2f", params.FPS))
	return filters
}

func buildScaleFilter(params conversionRequest) string {
	if params.Width == 0 && params.Height == 0 {
		return ""
	}

	widthValue := strconv.Itoa(params.Width)
	heightValue := strconv.Itoa(params.Height)
	if params.Width == 0 {
		widthValue = "-1"
	}
	if params.Height == 0 {
		heightValue = "-1"
	}

	if params.Width == 0 || params.Height == 0 {
		return fmt.Sprintf("scale=w=%s:h=%s:flags=%s", widthValue, heightValue, params.ScaleAlgorithm)
	}

	switch params.FitMode {
	case "cover":
		return fmt.Sprintf(
			"scale=w=%d:h=%d:force_original_aspect_ratio=increase:flags=%s,crop=%d:%d",
			params.Width,
			params.Height,
			params.ScaleAlgorithm,
			params.Width,
			params.Height,
		)
	case "stretch":
		return fmt.Sprintf("scale=w=%d:h=%d:flags=%s", params.Width, params.Height, params.ScaleAlgorithm)
	case "original":
		return fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease:flags=%s", params.Width, params.Height, params.ScaleAlgorithm)
	default:
		background := strings.Replace(params.Background, "#", "0x", 1)
		return fmt.Sprintf(
			"scale=w=%d:h=%d:force_original_aspect_ratio=decrease:flags=%s,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s",
			params.Width,
			params.Height,
			params.ScaleAlgorithm,
			params.Width,
			params.Height,
			background,
		)
	}
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(cleanupPeriod)
	defer ticker.Stop()

	s.cleanupExpiredFiles()
	for range ticker.C {
		s.cleanupExpiredFiles()
	}
}

func (s *Server) cleanupExpiredFiles() {
	cutoff := time.Now().Add(-jobRetention)
	entries, err := os.ReadDir(s.outputDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			fullPath := filepath.Join(s.outputDir, entry.Name())
			if strings.HasSuffix(entry.Name(), ".json") {
				manifest, manifestErr := readManifest(fullPath)
				if manifestErr != nil {
					continue
				}

				expiresAt, parseErr := time.Parse(time.RFC3339, manifest.ExpiresAt)
				if parseErr != nil || expiresAt.Before(time.Now()) {
					_ = s.deleteOutputAsset(outputAsset{
						Name:         manifest.DownloadName,
						ManifestPath: fullPath,
						RemotePath:   manifest.RemotePath,
						Remote:       true,
					})
				}
				continue
			}

			info, statErr := entry.Info()
			if statErr != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(fullPath)
			}
		}
	}

	tempEntries, err := os.ReadDir(s.tempDir)
	if err != nil {
		return
	}
	for _, entry := range tempEntries {
		fullPath := filepath.Join(s.tempDir, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(fullPath)
		}
	}
}

func copyUploadToDisk(source io.Reader, targetPath string) error {
	target, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer target.Close()

	_, err = io.Copy(target, source)
	return err
}

func streamUploadToWorkspace(request *http.Request, workspace string) (conversionRequest, string, string, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return conversionRequest{}, "", "", errors.New("request must use multipart/form-data")
	}

	values := url.Values{}
	var inputPath string
	var sourceName string

	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return conversionRequest{}, "", "", errors.New("unable to read upload stream")
		}

		partName := part.FormName()
		fileName := strings.TrimSpace(part.FileName())

		switch {
		case fileName != "":
			if partName != "video" {
				_, _ = io.Copy(io.Discard, part)
				_ = part.Close()
				continue
			}
			if inputPath != "" {
				_ = part.Close()
				return conversionRequest{}, "", "", errors.New("only one video file is supported")
			}

			sourceName = filepath.Base(fileName)
			inputExtension := strings.ToLower(filepath.Ext(sourceName))
			if inputExtension == "" {
				inputExtension = guessExtension(part.Header.Get("Content-Type"))
			}
			if inputExtension == "" {
				inputExtension = ".mp4"
			}

			inputPath = filepath.Join(workspace, "source"+inputExtension)
			copyErr := copyUploadToDisk(part, inputPath)
			_ = part.Close()
			if copyErr != nil {
				return conversionRequest{}, "", "", errors.New("unable to persist uploaded file")
			}
		case partName != "":
			payload, readErr := io.ReadAll(io.LimitReader(part, 64<<10))
			_ = part.Close()
			if readErr != nil {
				return conversionRequest{}, "", "", errors.New("unable to read form fields")
			}
			values.Set(partName, strings.TrimSpace(string(payload)))
		default:
			_ = part.Close()
		}
	}

	if inputPath == "" {
		return conversionRequest{}, "", "", errors.New("video file is required")
	}

	params, err := parseConversionRequest(values)
	if err != nil {
		return conversionRequest{}, "", "", err
	}

	return params, inputPath, sourceName, nil
}

func runFFmpeg(ffmpegPath string, args []string) (string, error) {
	command := exec.Command(ffmpegPath, args...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func renderGIF(ffmpegPath, workspace, inputPath, outputPath string, params conversionRequest) ([]string, error) {
	singlePassArgs := buildFFmpegCommand(inputPath, outputPath, params)
	commands := []string{ffmpegPath + " " + strings.Join(singlePassArgs, " ")}

	output, err := runFFmpeg(ffmpegPath, singlePassArgs)
	if err == nil {
		return commands, nil
	}

	if !shouldFallbackToTwoPass(output) {
		return commands, errors.New(buildFFmpegError("gif rendering failed", output, err))
	}

	_ = deleteFileIfExists(outputPath)
	palettePath := filepath.Join(workspace, "palette.png")
	paletteArgs, renderArgs := buildFFmpegFallbackCommands(inputPath, palettePath, outputPath, params)
	commands = append(commands,
		ffmpegPath+" "+strings.Join(paletteArgs, " "),
		ffmpegPath+" "+strings.Join(renderArgs, " "),
	)

	log.Printf("single-pass gif render produced zero frames, retrying with two-pass palette fallback")

	if paletteOutput, paletteErr := runFFmpeg(ffmpegPath, paletteArgs); paletteErr != nil {
		return commands, errors.New(buildFFmpegError("palette generation failed during fallback", paletteOutput, paletteErr))
	}

	if renderOutput, renderErr := runFFmpeg(ffmpegPath, renderArgs); renderErr != nil {
		return commands, errors.New(buildFFmpegError("gif rendering failed during fallback", renderOutput, renderErr))
	}

	return commands, nil
}

func shouldFallbackToTwoPass(output string) bool {
	return strings.Contains(output, "No filtered frames for output stream") ||
		strings.Contains(output, "Output file is empty, nothing was encoded")
}

func (s *Server) uploadOutputToOpenListAsync(localPath, downloadName, jobID string, sizeBytes int64) {
	startedAt := time.Now()
	if err := s.promoteOutputToOpenList(localPath, downloadName, jobID, sizeBytes); err != nil {
		log.Printf("job %s async openlist upload failed for %s: %v", jobID, downloadName, err)
		return
	}

	log.Printf(
		"job %s async openlist upload completed in %s",
		jobID,
		time.Since(startedAt).Round(time.Millisecond),
	)
}

func buildFFmpegError(prefix, output string, commandError error) string {
	snippet := strings.TrimSpace(output)
	if len(snippet) > 2500 {
		snippet = snippet[len(snippet)-2500:]
	}
	if snippet == "" {
		return fmt.Sprintf("%s: %v", prefix, commandError)
	}
	return fmt.Sprintf("%s: %v\n%s", prefix, commandError, snippet)
}

func parseChoice(value, fallback string, allowed []string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	for _, option := range allowed {
		if value == option {
			return value, nil
		}
	}
	return "", fmt.Errorf("invalid value %q", value)
}

func parseFloat(value string, fallback, minValue, maxValue float64) float64 {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func parseInt(value string, fallback, minValue, maxValue int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeHexColor(value, fallback string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	if !hexColorPattern.MatchString(value) {
		return "", errors.New("background must be a 6 digit hex color")
	}
	if strings.HasPrefix(value, "#") {
		return strings.ToLower(value), nil
	}
	return "#" + strings.ToLower(value), nil
}

func sanitizeName(value string) string {
	cleaned := filenameCleaner.ReplaceAllString(strings.TrimSpace(strings.ToLower(value)), "-")
	cleaned = strings.Trim(cleaned, "-")
	if len(cleaned) > 48 {
		cleaned = cleaned[:48]
	}
	return cleaned
}

func formatSeconds(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func guessExtension(contentType string) string {
	extensions, err := mime.ExtensionsByType(contentType)
	if err != nil || len(extensions) == 0 {
		return ""
	}
	return extensions[0]
}

func newJobID() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

func storageModeLabel(remote bool) string {
	if remote {
		return "openlist-async"
	}
	return "local"
}

func renderMissingFrontend(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Build frontend first</title>
    <style>
      body { font-family: ui-sans-serif, sans-serif; background: #020617; color: #e2e8f0; display: grid; place-items: center; min-height: 100vh; margin: 0; }
      main { max-width: 720px; padding: 32px; border: 1px solid #1e293b; background: rgba(15, 23, 42, 0.9); }
      code { color: #bfdbfe; }
    </style>
  </head>
  <body>
    <main>
      <h1>Frontend bundle not found</h1>
      <p>Run <code>npm install</code> and <code>npm run build</code>, then refresh this page.</p>
      <p>The Go backend is running, but the React build output is missing from <code>dist/</code>.</p>
    </main>
  </body>
</html>`))
}
