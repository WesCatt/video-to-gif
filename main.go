package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"video-to-gif/internal/app"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("resolve working directory: %v", err)
	}

	storageConfig, err := resolveStorageConfig()
	if err != nil {
		log.Fatalf("resolve storage config: %v", err)
	}

	server, err := app.NewServer(
		filepath.Join(workingDir, "dist"),
		filepath.Join(workingDir, "outputs"),
		filepath.Join(workingDir, "temp"),
		storageConfig,
	)
	if err != nil {
		log.Fatalf("bootstrap server: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	address := ":" + port
	log.Printf("video-to-gif server listening on %s", address)
	if storageConfig.Enabled() {
		log.Printf("gif storage mode: openlist")
		log.Printf("openlist base url: %s", storageConfig.OpenListBaseURL)
		log.Printf("openlist remote path: %s", storageConfig.RemoteVideoDir())
	} else {
		log.Printf("gif storage mode: local")
		log.Printf("gif output directory: %s", filepath.Join(workingDir, "outputs"))
	}

	if err := http.ListenAndServe(address, server); err != nil {
		log.Fatalf("serve http: %v", err)
	}
}

func resolveStorageConfig() (app.StorageConfig, error) {
	config := app.StorageConfig{
		OpenListBaseURL:   strings.TrimSpace(os.Getenv("OPENLIST_BASE_URL")),
		OpenListUsername:  strings.TrimSpace(os.Getenv("OPENLIST_USERNAME")),
		OpenListPassword:  os.Getenv("OPENLIST_PASSWORD"),
		OpenListVideoPath: strings.TrimSpace(os.Getenv("OPENLIST_VIDEO_PATH")),
	}

	if !config.Enabled() {
		return config, nil
	}

	if config.OpenListUsername == "" {
		return app.StorageConfig{}, fmt.Errorf("OPENLIST_USERNAME is required when OPENLIST_BASE_URL is set")
	}

	if config.OpenListPassword == "" {
		return app.StorageConfig{}, fmt.Errorf("OPENLIST_PASSWORD is required when OPENLIST_BASE_URL is set")
	}

	return config, nil
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("line %d: expected KEY=VALUE", lineNumber)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("line %d: empty key", lineNumber)
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)

		if _, alreadySet := os.LookupEnv(key); !alreadySet {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("line %d: set %s: %w", lineNumber, key, err)
			}
		}
	}

	return scanner.Err()
}
