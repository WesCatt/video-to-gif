package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const openListTokenTTL = 6 * time.Hour

type StorageConfig struct {
	OpenListBaseURL   string
	OpenListUsername  string
	OpenListPassword  string
	OpenListVideoPath string
}

type gifManifest struct {
	JobID        string `json:"jobId"`
	DownloadName string `json:"downloadName"`
	RemotePath   string `json:"remotePath"`
	SizeBytes    int64  `json:"sizeBytes"`
	ExpiresAt    string `json:"expiresAt"`
}

type outputAsset struct {
	Name         string
	LocalPath    string
	ManifestPath string
	RemotePath   string
	SizeBytes    int64
	ExpiresAt    time.Time
	Remote       bool
}

type openListResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type openListLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type openListLoginData struct {
	Token string `json:"token"`
}

type openListMkdirRequest struct {
	Path string `json:"path"`
}

type openListGetRequest struct {
	Path     string `json:"path"`
	Password string `json:"password,omitempty"`
	Refresh  bool   `json:"refresh"`
}

type openListGetData struct {
	IsDir  bool   `json:"is_dir"`
	RawURL string `json:"raw_url"`
}

type openListRemoveRequest struct {
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
}

func (c StorageConfig) Enabled() bool {
	return strings.TrimSpace(c.OpenListBaseURL) != ""
}

func (c StorageConfig) RemoteVideoDir() string {
	target := strings.TrimSpace(c.OpenListVideoPath)
	if target == "" {
		target = "/video-to-gif"
	}

	return path.Clean("/" + strings.TrimPrefix(target, "/"))
}

func (c StorageConfig) APIRootURL() (*url.URL, error) {
	baseURL := strings.TrimSpace(c.OpenListBaseURL)
	if baseURL == "" {
		return nil, errors.New("OPENLIST_BASE_URL is empty")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/"
	parsed.RawPath = ""
	return parsed, nil
}

func manifestPath(outputDir, downloadName string) string {
	return filepath.Join(outputDir, downloadName+".json")
}

func writeManifest(path string, manifest gifManifest) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifest)
}

func readManifest(path string) (gifManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return gifManifest{}, err
	}
	defer file.Close()

	var manifest gifManifest
	if err := json.NewDecoder(file).Decode(&manifest); err != nil {
		return gifManifest{}, err
	}

	return manifest, nil
}

func deleteFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (s *Server) resolveOutputAsset(outputName string) (outputAsset, error) {
	localPath := filepath.Join(s.outputDir, outputName)
	if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
		return outputAsset{
			Name:      outputName,
			LocalPath: localPath,
			SizeBytes: info.Size(),
			ExpiresAt: info.ModTime().Add(jobRetention),
		}, nil
	}

	manifestFile := manifestPath(s.outputDir, outputName)
	manifest, err := readManifest(manifestFile)
	if err != nil {
		return outputAsset{}, os.ErrNotExist
	}

	expiresAt, err := time.Parse(time.RFC3339, manifest.ExpiresAt)
	if err != nil {
		return outputAsset{}, err
	}

	return outputAsset{
		Name:         outputName,
		ManifestPath: manifestFile,
		RemotePath:   manifest.RemotePath,
		SizeBytes:    manifest.SizeBytes,
		ExpiresAt:    expiresAt,
		Remote:       true,
	}, nil
}

func (s *Server) deleteOutputAsset(asset outputAsset) error {
	if asset.Remote {
		if asset.RemotePath != "" {
			if err := s.removeOpenListFile(asset.RemotePath); err != nil {
				return err
			}
		}

		return deleteFileIfExists(asset.ManifestPath)
	}

	if err := deleteFileIfExists(asset.LocalPath); err != nil {
		return err
	}

	manifestFile := manifestPath(s.outputDir, asset.Name)
	manifest, err := readManifest(manifestFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if manifest.RemotePath != "" {
		if err := s.removeOpenListFile(manifest.RemotePath); err != nil {
			return err
		}
	}

	return deleteFileIfExists(manifestFile)
}

func (s *Server) uploadToOpenList(localPath, downloadName string, sizeBytes int64) (string, error) {
	remotePath := path.Join(s.storage.RemoteVideoDir(), downloadName)
	if err := s.ensureOpenListDirectory(path.Dir(remotePath)); err != nil {
		return "", err
	}

	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	response, err := s.doOpenListRequest(http.MethodPut, []string{"api", "fs", "put"}, file, true, func(request *http.Request) {
		request.Header.Set("File-Path", url.PathEscape(remotePath))
		request.Header.Set("Content-Type", "image/gif")
		request.ContentLength = sizeBytes
	})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("openlist upload failed: %s", response.Status)
	}

	return remotePath, nil
}

func (s *Server) promoteOutputToOpenList(localPath, downloadName, jobID string, sizeBytes int64) error {
	remotePath, err := s.uploadToOpenList(localPath, downloadName, sizeBytes)
	if err != nil {
		return err
	}

	if _, err := os.Stat(localPath); err != nil {
		_ = s.removeOpenListFile(remotePath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	expiresAt := time.Now().Add(jobRetention)
	manifestFile := manifestPath(s.outputDir, downloadName)
	if err := writeManifest(manifestFile, gifManifest{
		JobID:        jobID,
		DownloadName: downloadName,
		RemotePath:   remotePath,
		SizeBytes:    sizeBytes,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	}); err != nil {
		_ = s.removeOpenListFile(remotePath)
		return err
	}

	return deleteFileIfExists(localPath)
}

func (s *Server) proxyOpenListFile(writer http.ResponseWriter, request *http.Request, asset outputAsset, asAttachment bool) {
	rawURL, err := s.resolveOpenListRawURL(asset.RemotePath)
	if err != nil {
		http.Error(writer, "unable to resolve remote gif url", http.StatusBadGateway)
		return
	}

	writer.Header().Set("Cache-Control", "no-store")
	if asAttachment {
		http.Redirect(writer, request, rawURL, http.StatusFound)
		return
	}

	upstreamRequest, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		http.Error(writer, "unable to build preview request", http.StatusInternalServerError)
		return
	}

	response, err := http.DefaultClient.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, "unable to fetch remote gif", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(writer, "remote gif request failed", http.StatusBadGateway)
		return
	}

	writer.Header().Set("Content-Type", "image/gif")
	writer.Header().Set("Content-Disposition", "inline")
	if contentLength := response.Header.Get("Content-Length"); contentLength != "" {
		writer.Header().Set("Content-Length", contentLength)
	}
	writer.WriteHeader(http.StatusOK)

	if request.Method == http.MethodHead {
		return
	}

	_, _ = io.Copy(writer, response.Body)
}

func (s *Server) ensureOpenListDirectory(remoteDir string) error {
	remoteDir = path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(remoteDir), "/"))
	if remoteDir == "/" || remoteDir == "." {
		return nil
	}

	exists, err := s.openListPathExists(remoteDir)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	payload, err := json.Marshal(openListMkdirRequest{Path: remoteDir})
	if err != nil {
		return err
	}

	response, err := s.doOpenListRequest(http.MethodPost, []string{"api", "fs", "mkdir"}, bytes.NewReader(payload), true, func(request *http.Request) {
		request.Header.Set("Content-Type", "application/json")
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	var result openListResponse[struct{}]
	if err := decodeOpenListResponse(response, &result); err != nil {
		return err
	}

	if result.Code != 200 {
		return fmt.Errorf("openlist mkdir failed: %s", result.Message)
	}

	return nil
}

func (s *Server) openListPathExists(remotePath string) (bool, error) {
	payload, err := json.Marshal(openListGetRequest{
		Path:    remotePath,
		Refresh: false,
	})
	if err != nil {
		return false, err
	}

	response, err := s.doOpenListRequest(http.MethodPost, []string{"api", "fs", "get"}, bytes.NewReader(payload), true, func(request *http.Request) {
		request.Header.Set("Content-Type", "application/json")
	})
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}

	var result openListResponse[openListGetData]
	if err := decodeOpenListResponse(response, &result); err != nil {
		return false, err
	}

	return result.Code == 200, nil
}

func (s *Server) removeOpenListFile(remotePath string) error {
	payload, err := json.Marshal(openListRemoveRequest{
		Dir:   path.Dir(remotePath),
		Names: []string{path.Base(remotePath)},
	})
	if err != nil {
		return err
	}

	response, err := s.doOpenListRequest(http.MethodPost, []string{"api", "fs", "remove"}, bytes.NewReader(payload), true, func(request *http.Request) {
		request.Header.Set("Content-Type", "application/json")
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	var result openListResponse[struct{}]
	if err := decodeOpenListResponse(response, &result); err != nil {
		return err
	}

	if result.Code != 200 {
		return fmt.Errorf("openlist remove failed: %s", result.Message)
	}

	return nil
}

func (s *Server) resolveOpenListRawURL(remotePath string) (string, error) {
	payload, err := json.Marshal(openListGetRequest{
		Path:    remotePath,
		Refresh: true,
	})
	if err != nil {
		return "", err
	}

	response, err := s.doOpenListRequest(http.MethodPost, []string{"api", "fs", "get"}, bytes.NewReader(payload), true, func(request *http.Request) {
		request.Header.Set("Content-Type", "application/json")
	})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	var result openListResponse[openListGetData]
	if err := decodeOpenListResponse(response, &result); err != nil {
		return "", err
	}

	if result.Code != 200 || strings.TrimSpace(result.Data.RawURL) == "" {
		return "", fmt.Errorf("openlist get failed: %s", result.Message)
	}

	return result.Data.RawURL, nil
}

func (s *Server) getOpenListToken(forceRefresh bool) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if !forceRefresh && s.token != "" && time.Since(s.tokenAt) < openListTokenTTL {
		return s.token, nil
	}

	payload, err := json.Marshal(openListLoginRequest{
		Username: s.storage.OpenListUsername,
		Password: s.storage.OpenListPassword,
	})
	if err != nil {
		return "", err
	}

	response, err := s.doOpenListRequest(http.MethodPost, []string{"api", "auth", "login"}, bytes.NewReader(payload), false, func(request *http.Request) {
		request.Header.Set("Content-Type", "application/json")
	})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	var result openListResponse[openListLoginData]
	if err := decodeOpenListResponse(response, &result); err != nil {
		return "", err
	}

	if result.Code != 200 || strings.TrimSpace(result.Data.Token) == "" {
		return "", fmt.Errorf("openlist login failed: %s", result.Message)
	}

	s.token = result.Data.Token
	s.tokenAt = time.Now()
	return s.token, nil
}

func (s *Server) doOpenListRequest(method string, segments []string, body io.Reader, withAuth bool, mutate func(*http.Request)) (*http.Response, error) {
	attempts := 1
	if withAuth {
		attempts = 2
	}

	for attempt := 0; attempt < attempts; attempt++ {
		var token string
		if withAuth {
			forceRefresh := attempt > 0
			nextToken, err := s.getOpenListToken(forceRefresh)
			if err != nil {
				return nil, err
			}
			token = nextToken
		}

		var requestBody io.Reader
		if seeker, ok := body.(io.ReadSeeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
			requestBody = seeker
		} else {
			requestBody = body
		}

		request, err := s.newOpenListRequest(method, segments, requestBody)
		if err != nil {
			return nil, err
		}
		if withAuth {
			request.Header.Set("Authorization", token)
		}
		if mutate != nil {
			mutate(request)
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil, err
		}

		if withAuth && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) && attempt == 0 {
			response.Body.Close()
			s.tokenMu.Lock()
			s.token = ""
			s.tokenAt = time.Time{}
			s.tokenMu.Unlock()
			continue
		}

		return response, nil
	}

	return nil, errors.New("openlist request failed after retry")
}

func (s *Server) newOpenListRequest(method string, segments []string, body io.Reader) (*http.Request, error) {
	rootURL, err := s.storage.APIRootURL()
	if err != nil {
		return nil, err
	}

	targetURL := rootURL.JoinPath(segments...)
	return http.NewRequest(method, targetURL.String(), body)
}

func decodeOpenListResponse[T any](response *http.Response, payload *openListResponse[T]) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("openlist request failed: %s %s", response.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(response.Body).Decode(payload); err != nil {
		return err
	}

	return nil
}
