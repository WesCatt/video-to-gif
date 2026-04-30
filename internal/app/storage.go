package app

import (
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

func (c StorageConfig) WebDAVRootURL() (*url.URL, error) {
	baseURL := strings.TrimSpace(c.OpenListBaseURL)
	if baseURL == "" {
		return nil, errors.New("OPENLIST_BASE_URL is empty")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	cleanPath := strings.TrimSuffix(parsed.Path, "/")
	if cleanPath == "" {
		cleanPath = "/dav"
	} else if !strings.HasSuffix(cleanPath, "/dav") {
		cleanPath += "/dav"
	}

	parsed.Path = cleanPath + "/"
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
			if err := s.deleteFromOpenList(asset.RemotePath); err != nil {
				return err
			}
		}

		return deleteFileIfExists(asset.ManifestPath)
	}

	return deleteFileIfExists(asset.LocalPath)
}

func (s *Server) uploadToOpenList(localPath, downloadName, jobID string, sizeBytes int64) (outputAsset, error) {
	remotePath := path.Join(s.storage.RemoteVideoDir(), downloadName)
	if err := s.ensureOpenListDirectory(path.Dir(remotePath)); err != nil {
		return outputAsset{}, err
	}

	file, err := os.Open(localPath)
	if err != nil {
		return outputAsset{}, err
	}
	defer file.Close()

	request, err := s.newOpenListRequest(http.MethodPut, remotePath, file)
	if err != nil {
		return outputAsset{}, err
	}
	request.ContentLength = sizeBytes
	request.Header.Set("Content-Type", "image/gif")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return outputAsset{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return outputAsset{}, fmt.Errorf("openlist upload failed: %s", response.Status)
	}

	expiresAt := time.Now().Add(jobRetention)
	asset := outputAsset{
		Name:         downloadName,
		ManifestPath: manifestPath(s.outputDir, downloadName),
		RemotePath:   remotePath,
		SizeBytes:    sizeBytes,
		ExpiresAt:    expiresAt,
		Remote:       true,
	}

	if err := writeManifest(asset.ManifestPath, gifManifest{
		JobID:        jobID,
		DownloadName: downloadName,
		RemotePath:   remotePath,
		SizeBytes:    sizeBytes,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	}); err != nil {
		_ = s.deleteFromOpenList(remotePath)
		return outputAsset{}, err
	}

	return asset, nil
}

func (s *Server) deleteFromOpenList(remotePath string) error {
	request, err := s.newOpenListRequest(http.MethodDelete, remotePath, nil)
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("openlist delete failed: %s", response.Status)
	}
}

func (s *Server) proxyOpenListFile(writer http.ResponseWriter, request *http.Request, asset outputAsset, asAttachment bool) {
	upstreamRequest, err := s.newOpenListRequest(request.Method, asset.RemotePath, nil)
	if err != nil {
		http.Error(writer, "unable to build openlist request", http.StatusInternalServerError)
		return
	}

	response, err := http.DefaultClient.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, "unable to fetch remote gif", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		_ = deleteFileIfExists(asset.ManifestPath)
		http.NotFound(writer, request)
		return
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(writer, "remote gif request failed", http.StatusBadGateway)
		return
	}

	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Expires", asset.ExpiresAt.UTC().Format(http.TimeFormat))
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		writer.Header().Set("Content-Type", contentType)
	}
	if contentLength := response.Header.Get("Content-Length"); contentLength != "" {
		writer.Header().Set("Content-Length", contentLength)
	}
	if asAttachment {
		writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", asset.Name))
	}

	writer.WriteHeader(response.StatusCode)
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

	current := ""
	for _, part := range strings.Split(strings.Trim(remoteDir, "/"), "/") {
		current += "/" + part

		request, err := s.newOpenListRequest("MKCOL", current, nil)
		if err != nil {
			return err
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return err
		}
		response.Body.Close()

		switch response.StatusCode {
		case http.StatusCreated, http.StatusOK, http.StatusNoContent, http.StatusMethodNotAllowed:
			continue
		default:
			return fmt.Errorf("openlist mkdir failed for %s: %s", current, response.Status)
		}
	}

	return nil
}

func (s *Server) newOpenListRequest(method, remotePath string, body io.Reader) (*http.Request, error) {
	rootURL, err := s.storage.WebDAVRootURL()
	if err != nil {
		return nil, err
	}

	cleanPath := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(remotePath), "/"))
	var targetURL *url.URL
	if cleanPath == "/" || cleanPath == "." {
		targetURL = rootURL
	} else {
		segments := strings.Split(strings.Trim(cleanPath, "/"), "/")
		targetURL = rootURL.JoinPath(segments...)
	}

	request, err := http.NewRequest(method, targetURL.String(), body)
	if err != nil {
		return nil, err
	}

	request.SetBasicAuth(s.storage.OpenListUsername, s.storage.OpenListPassword)
	return request, nil
}
