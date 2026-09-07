package httpapi

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSkillPackageInjectsBaseURL(t *testing.T) {
	t.Parallel()

	path := createSkillPackage(t, "http://127.0.0.1:8080")
	data, _, err := buildSkillPackage(path, "https://x-type-center.internal/")
	if err != nil {
		t.Fatalf("build skill package: %v", err)
	}

	reader, err := zip.NewReader(bytesReaderAt(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open generated zip: %v", err)
	}

	var configFile *zip.File
	for _, file := range reader.File {
		if file.Name == skillConfigPath {
			configFile = file
			break
		}
	}
	if configFile == nil {
		t.Fatalf("%s not found", skillConfigPath)
	}

	rc, err := configFile.Open()
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if got := config["baseUrl"]; got != "https://x-type-center.internal" {
		t.Fatalf("baseUrl = %v", got)
	}
	if got := config["timeoutSeconds"]; got != float64(10) {
		t.Fatalf("timeoutSeconds = %v", got)
	}
}

func TestEffectivePublicURL(t *testing.T) {
	t.Parallel()

	t.Run("configured", func(t *testing.T) {
		api := &API{publicURL: "https://registry.internal"}
		request := httptest.NewRequest("GET", "http://ignored.local/api/v1/skill-package", nil)

		got, err := api.effectivePublicURL(request)
		if err != nil {
			t.Fatalf("effective public URL: %v", err)
		}
		if got != "https://registry.internal" {
			t.Fatalf("public URL = %q", got)
		}
	})

	t.Run("request fallback", func(t *testing.T) {
		api := &API{}
		request := httptest.NewRequest("GET", "http://registry.local:8080/api/v1/skill-package", nil)

		got, err := api.effectivePublicURL(request)
		if err != nil {
			t.Fatalf("effective public URL: %v", err)
		}
		if got != "http://registry.local:8080" {
			t.Fatalf("public URL = %q", got)
		}
	})
}

func createSkillPackage(t *testing.T, baseURL string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "type-registry-skill.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}

	writer := zip.NewWriter(file)
	configWriter, err := writer.Create(skillConfigPath)
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := io.WriteString(configWriter, `{"baseUrl":"`+baseURL+`","timeoutSeconds":10}`); err != nil {
		t.Fatalf("write config: %v", err)
	}
	skillWriter, err := writer.Create("type-registry/SKILL.md")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := io.WriteString(skillWriter, "# Type Registry\n"); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return path
}

type byteReaderAt []byte

func (b byteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func bytesReaderAt(data []byte) byteReaderAt {
	return byteReaderAt(data)
}
