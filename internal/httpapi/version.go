package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	skillLatestVersion       = "1.1.0"
	skillMinSupportedVersion = "1.1.0"
	skillVersionHeader       = "X-Type-Registry-Skill-Version"
	skillManifestPath        = "type-registry/manifest.json"
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

type bufferedResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header)}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (a *API) versionInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"server": map[string]string{
			"version":   a.buildInfo.Version,
			"commit":    a.buildInfo.Commit,
			"buildTime": a.buildInfo.BuildTime,
		},
		"skill": a.currentSkillVersionInfo(),
	})
}

func (a *API) skillVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.currentSkillVersionInfo())
}

func (a *API) currentSkillVersionInfo() map[string]any {
	info := map[string]any{
		"latestVersion":       skillLatestVersion,
		"minSupportedVersion": skillMinSupportedVersion,
		"packageReady":        false,
		"downloadUrl":         "/api/v1/skill-package",
		"message":             "Skill 已支持版本检查和 update；只有 Registry allocate 返回的号码可以使用。",
	}
	if a.skillPackagePath == "" {
		return info
	}
	version, err := readSkillPackageVersion(a.skillPackagePath)
	if err != nil {
		return info
	}
	info["packageVersion"] = version
	info["packageReady"] = compareSemanticVersion(version, skillLatestVersion) == 0
	return info
}

func readSkillPackageVersion(packagePath string) (string, error) {
	reader, err := zip.OpenReader(packagePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != skillManifestPath {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, 64<<10))
		closeErr := rc.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		var manifest map[string]any
		if err := json.Unmarshal(content, &manifest); err != nil {
			return "", err
		}
		version := strings.TrimSpace(fmt.Sprint(manifest["version"]))
		if version == "" || version == "<nil>" {
			return "", errors.New("skill manifest version is empty")
		}
		return version, nil
	}
	return "", fmt.Errorf("%s not found", skillManifestPath)
}

func (a *API) ensureSkillPackageCurrent() error {
	if a.skillPackagePath == "" {
		return errors.New("skill package is not configured")
	}
	version, err := readSkillPackageVersion(a.skillPackagePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return fmt.Errorf("read configured skill package version: %w", err)
	}
	if compareSemanticVersion(version, skillLatestVersion) != 0 {
		return fmt.Errorf("configured skill package version %s does not match latest %s", version, skillLatestVersion)
	}
	return nil
}

func compareSemanticVersion(left, right string) int {
	l, lok := semanticVersionNumbers(left)
	r, rok := semanticVersionNumbers(right)
	if !lok || !rok {
		return strings.Compare(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}
	return 0
}

func semanticVersionNumbers(value string) ([3]int, bool) {
	var result [3]int
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return result, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return result, false
		}
		result[i] = number
	}
	return result, true
}

func (a *API) withSkillVersionNotice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentVersion, isSkill := requestSkillVersion(r)
		if !isSkill || strings.HasPrefix(r.URL.Path, "/api/v1/skill-") || r.URL.Path == "/api/v1/version" {
			next.ServeHTTP(w, r)
			return
		}

		info := a.currentSkillVersionInfo()
		latestVersion := fmt.Sprint(info["latestVersion"])
		minSupportedVersion := fmt.Sprint(info["minSupportedVersion"])
		required := currentVersion == "" || compareSemanticVersion(currentVersion, minSupportedVersion) < 0
		outdated := currentVersion == "" || compareSemanticVersion(currentVersion, latestVersion) < 0
		if !outdated {
			next.ServeHTTP(w, r)
			return
		}

		notice := map[string]any{
			"required":            required,
			"currentVersion":      currentVersion,
			"latestVersion":       latestVersion,
			"minSupportedVersion": minSupportedVersion,
			"packageReady":        info["packageReady"],
			"downloadUrl":         info["downloadUrl"],
		}
		if packageVersion, ok := info["packageVersion"]; ok {
			notice["packageVersion"] = packageVersion
		}
		if currentVersion == "" {
			notice["currentVersion"] = "legacy"
			notice["message"] = "检测到旧版 Type Registry Skill，请从 X系列类型中心重新下载技能包；新版后续可直接执行 update。"
		} else {
			notice["message"] = "Type Registry Skill 有新版本，请执行 type_registry.py update。"
		}

		if required && isSkillWriteRequest(r) {
			writeJSON(w, http.StatusUpgradeRequired, map[string]any{
				"error":       "当前 Type Registry Skill 版本过旧，写操作已阻止，请先升级。",
				"status":      http.StatusUpgradeRequired,
				"skillUpdate": notice,
			})
			return
		}

		recorder := newBufferedResponseWriter()
		next.ServeHTTP(recorder, r)
		body := recorder.body.Bytes()
		if recorder.status >= 200 && recorder.status < 300 &&
			strings.Contains(recorder.header.Get("Content-Type"), "application/json") {
			var payload map[string]any
			if json.Unmarshal(body, &payload) == nil {
				payload["skillUpdate"] = notice
				if encoded, err := json.Marshal(payload); err == nil {
					body = append(encoded, '\n')
					recorder.header.Del("Content-Length")
				}
			}
		}
		for key, values := range recorder.header {
			w.Header()[key] = append([]string(nil), values...)
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

func requestSkillVersion(r *http.Request) (string, bool) {
	version := strings.TrimSpace(r.Header.Get(skillVersionHeader))
	if version != "" {
		return version, true
	}
	userAgent := strings.TrimSpace(r.UserAgent())
	if strings.HasPrefix(userAgent, "Python-urllib/") {
		return "", true
	}
	if strings.HasPrefix(userAgent, "x-type-center-skill/") {
		return strings.TrimPrefix(userAgent, "x-type-center-skill/"), true
	}
	return "", false
}

func isSkillWriteRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/types/allocate", "/api/v1/types/allocate-batch":
		return true
	default:
		return strings.HasSuffix(r.URL.Path, "/revoke")
	}
}
