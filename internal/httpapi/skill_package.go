package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const skillConfigPath = "type-registry/config.json"

func (a *API) effectivePublicURL(r *http.Request) (string, error) {
	if a.publicURL != "" {
		return a.publicURL, nil
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "", fmt.Errorf("public URL is not configured and request host is empty")
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	rawURL := scheme + "://" + host
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("cannot determine public URL from request")
	}
	return strings.TrimRight(rawURL, "/"), nil
}

func buildSkillPackage(packagePath, baseURL string) ([]byte, time.Time, error) {
	info, err := os.Stat(packagePath)
	if err != nil {
		return nil, time.Time{}, err
	}
	if info.IsDir() {
		return nil, time.Time{}, fmt.Errorf("configured skill package path is a directory")
	}

	reader, err := zip.OpenReader(packagePath)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open skill package zip: %w", err)
	}
	defer reader.Close()

	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if reader.Comment != "" {
		if err := writer.SetComment(reader.Comment); err != nil {
			return nil, time.Time{}, fmt.Errorf("copy skill package comment: %w", err)
		}
	}

	foundConfig := false
	for _, file := range reader.File {
		if file.Name != skillConfigPath {
			if err := writer.Copy(file); err != nil {
				return nil, time.Time{}, fmt.Errorf("copy skill package entry %s: %w", file.Name, err)
			}
			continue
		}

		foundConfig = true
		content, err := readZipFile(file)
		if err != nil {
			return nil, time.Time{}, err
		}

		var config map[string]any
		if err := json.Unmarshal(content, &config); err != nil {
			return nil, time.Time{}, fmt.Errorf("decode %s: %w", skillConfigPath, err)
		}
		config["baseUrl"] = strings.TrimRight(baseURL, "/")

		updated, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("encode %s: %w", skillConfigPath, err)
		}
		updated = append(updated, '\n')

		header := &zip.FileHeader{
			Name:     file.Name,
			Method:   zip.Deflate,
			Modified: file.Modified,
		}
		header.SetMode(file.Mode())

		target, err := writer.CreateHeader(header)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("create %s: %w", skillConfigPath, err)
		}
		if _, err := target.Write(updated); err != nil {
			return nil, time.Time{}, fmt.Errorf("write %s: %w", skillConfigPath, err)
		}
	}

	if !foundConfig {
		return nil, time.Time{}, fmt.Errorf("skill package does not contain %s", skillConfigPath)
	}
	if err := writer.Close(); err != nil {
		return nil, time.Time{}, fmt.Errorf("finalize skill package: %w", err)
	}

	return output.Bytes(), info.ModTime(), nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", file.Name, err)
	}
	defer reader.Close()

	const maxConfigSize = 1 << 20
	content, err := io.ReadAll(io.LimitReader(reader, maxConfigSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file.Name, err)
	}
	if len(content) > maxConfigSize {
		return nil, fmt.Errorf("%s exceeds 1 MiB", file.Name)
	}
	return content, nil
}
