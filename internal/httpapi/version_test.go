package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hctty27/x-type-center/internal/product"
)

func TestCompareSemanticVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left  string
		right string
		want  int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.2.0", "1.10.0", -1},
		{"v2.0.0", "1.9.9", 1},
	}
	for _, tt := range tests {
		got := compareSemanticVersion(tt.left, tt.right)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != tt.want {
			t.Fatalf("compareSemanticVersion(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func TestVersionInfoUsesUnifiedProductVersion(t *testing.T) {
	t.Parallel()

	api := &API{buildInfo: BuildInfo{
		Version:   "main-999-abcdef0",
		Commit:    "abcdef0123456789",
		BuildTime: "2026-09-08T00:00:00Z",
	}}
	request := httptest.NewRequest(http.MethodGet, "http://registry.local/api/v1/version", nil)
	response := httptest.NewRecorder()

	api.versionInfo(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["version"]; got != product.Version {
		t.Fatalf("version = %v, want %s", got, product.Version)
	}
	if got := payload["buildVersion"]; got != "main-999-abcdef0" {
		t.Fatalf("buildVersion = %v", got)
	}
}

func TestSkillManifestMatchesServerVersion(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "skills", "type-registry", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if got := manifest["version"]; got != product.Version {
		t.Fatalf("manifest version = %v, server latest = %s", got, product.Version)
	}
}

func TestLegacySkillGetsVisibleUpdateNotice(t *testing.T) {
	t.Parallel()

	api := &API{}
	handler := api.withSkillVersionNotice(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []string{"RankType"}})
	}))

	request := httptest.NewRequest(http.MethodGet, "http://registry.local/api/v1/namespaces", nil)
	request.Header.Set("User-Agent", "Python-urllib/3.12")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["skillUpdate"] == nil {
		t.Fatal("skillUpdate is missing")
	}
}

func TestLegacySkillWriteIsBlocked(t *testing.T) {
	t.Parallel()

	api := &API{}
	handler := api.withSkillVersionNotice(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"value": 100})
	}))

	request := httptest.NewRequest(http.MethodPost, "http://registry.local/api/v1/types/allocate-batch", nil)
	request.Header.Set("User-Agent", "Python-urllib/3.12")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUpgradeRequired)
	}
}

func TestCurrentSkillPassesWithoutNotice(t *testing.T) {
	t.Parallel()

	api := &API{}
	handler := api.withSkillVersionNotice(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	request := httptest.NewRequest(http.MethodGet, "http://registry.local/api/v1/namespaces", nil)
	request.Header.Set(skillVersionHeader, product.Version)
	request.Header.Set("User-Agent", "x-type-center-skill/"+product.Version)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, exists := payload["skillUpdate"]; exists {
		t.Fatal("current Skill should not receive skillUpdate")
	}
}
