package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hctty27/x-type-center/internal/model"
	"github.com/hctty27/x-type-center/internal/service"
	"github.com/hctty27/x-type-center/internal/store"
)

type API struct {
	registry         *service.Registry
	skillPackagePath string
	publicURL        string
	trustedProxies   []netip.Prefix
	logger           *slog.Logger
}

func New(registry *service.Registry, skillPackagePath, publicURL string, trustedProxies []netip.Prefix, logger *slog.Logger) *API {
	return &API{
		registry:         registry,
		skillPackagePath: strings.TrimSpace(skillPackagePath),
		publicURL:        strings.TrimRight(strings.TrimSpace(publicURL), "/"),
		trustedProxies:   trustedProxies,
		logger:           logger,
	}
}

func (a *API) Routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/v1/namespaces", a.listNamespaces)
	mux.HandleFunc("POST /api/v1/namespaces", a.createNamespace)
	mux.HandleFunc("GET /api/v1/projects", a.listProjects)
	mux.HandleFunc("GET /api/v1/namespaces/resolve", a.resolveNamespace)
	mux.HandleFunc("GET /api/v1/namespaces/{code}", a.getNamespace)
	mux.HandleFunc("PUT /api/v1/namespaces/{code}", a.updateNamespace)
	mux.HandleFunc("GET /api/v1/namespaces/{code}/aliases", a.listNamespaceAliases)
	mux.HandleFunc("POST /api/v1/namespaces/{code}/aliases", a.createNamespaceAlias)
	mux.HandleFunc("DELETE /api/v1/namespaces/{code}/aliases/{id}", a.deleteNamespaceAlias)
	mux.HandleFunc("GET /api/v1/types/search", a.searchTypes)
	mux.HandleFunc("GET /api/v1/skill-package", a.downloadSkillPackage)
	mux.HandleFunc("HEAD /api/v1/skill-package", a.downloadSkillPackage)
	mux.HandleFunc("POST /api/v1/types/allocate", a.allocateType)
	mux.HandleFunc("POST /api/v1/types/allocate-batch", a.allocateTypes)
	mux.HandleFunc("POST /api/v1/types/{id}/revoke", a.revokeType)
	mux.HandleFunc("POST /api/v1/allocations/{allocationId}/revoke", a.revokeAllocation)
	mux.HandleFunc("POST /api/v1/types/validate", a.validateType)
	mux.Handle("/", static)
	return a.withLogging(mux)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) listNamespaces(w http.ResponseWriter, r *http.Request) {
	items, err := a.registry.ListNamespaces(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createNamespace(w http.ResponseWriter, r *http.Request) {
	var req model.CreateNamespaceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ClientIP = a.clientIP(r)

	ns, err := a.registry.CreateNamespace(r.Context(), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ns)
}

func (a *API) updateNamespace(w http.ResponseWriter, r *http.Request) {
	var req model.UpdateNamespaceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ClientIP = a.clientIP(r)

	ns, err := a.registry.UpdateNamespace(r.Context(), r.PathValue("code"), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ns)
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := a.registry.ListProjects(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) resolveNamespace(w http.ResponseWriter, r *http.Request) {
	result, err := a.registry.ResolveNamespace(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) getNamespace(w http.ResponseWriter, r *http.Request) {
	ns, ranges, err := a.registry.GetNamespace(r.Context(), r.PathValue("code"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespace": ns, "reservedRanges": ranges})
}

func (a *API) listNamespaceAliases(w http.ResponseWriter, r *http.Request) {
	items, err := a.registry.ListNamespaceAliases(r.Context(), r.PathValue("code"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createNamespaceAlias(w http.ResponseWriter, r *http.Request) {
	var req model.CreateNamespaceAliasRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.registry.CreateNamespaceAlias(r.Context(), r.PathValue("code"), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) deleteNamespaceAlias(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid alias id")
		return
	}
	if err := a.registry.DeleteNamespaceAlias(r.Context(), r.PathValue("code"), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) downloadSkillPackage(w http.ResponseWriter, r *http.Request) {
	if a.skillPackagePath == "" {
		writeError(w, http.StatusNotFound, "skill package is not configured")
		return
	}

	baseURL, err := a.effectivePublicURL(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	packageData, modTime, err := buildSkillPackage(a.skillPackagePath, baseURL)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "skill package is not available")
			return
		}
		a.fail(w, err)
		return
	}

	filename := filepath.Base(a.skillPackagePath)
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename}); disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	w.Header().Set("Content-Type", "application/zip")
	http.ServeContent(w, r, filename, modTime, bytes.NewReader(packageData))
}

func (a *API) searchTypes(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize <= 0 {
		pageSize, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}

	result, err := a.registry.Search(r.Context(), model.SearchParams{
		Query:     r.URL.Query().Get("q"),
		Namespace: r.URL.Query().Get("namespace"),
		Project:   r.URL.Query().Get("project"),
		Limit:     pageSize,
		Offset:    (page - 1) * pageSize,
	})
	if err != nil {
		a.fail(w, err)
		return
	}

	totalPages := 0
	if result.Total > 0 {
		totalPages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      result.Items,
		"total":      result.Total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

func (a *API) allocateType(w http.ResponseWriter, r *http.Request) {
	var req model.AllocateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Requester == "" {
		req.Requester = strings.TrimSpace(r.Header.Get("X-Requester"))
	}
	req.ClientIP = a.clientIP(r)
	entry, err := a.registry.Allocate(r.Context(), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (a *API) allocateTypes(w http.ResponseWriter, r *http.Request) {
	var req model.AllocateBatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Requester == "" {
		req.Requester = strings.TrimSpace(r.Header.Get("X-Requester"))
	}
	req.ClientIP = a.clientIP(r)
	result, err := a.registry.AllocateBatch(r.Context(), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) revokeType(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid entry id")
		return
	}

	var req model.RevokeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Requester == "" {
		req.Requester = strings.TrimSpace(r.Header.Get("X-Requester"))
	}
	req.ClientIP = a.clientIP(r)

	entry, err := a.registry.RevokeEntry(r.Context(), id, req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) revokeAllocation(w http.ResponseWriter, r *http.Request) {
	var req model.RevokeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Requester == "" {
		req.Requester = strings.TrimSpace(r.Header.Get("X-Requester"))
	}
	req.ClientIP = a.clientIP(r)

	result, err := a.registry.RevokeAllocation(r.Context(), r.PathValue("allocationId"), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) validateType(w http.ResponseWriter, r *http.Request) {
	var req model.ValidateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.registry.Validate(r.Context(), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) fail(w http.ResponseWriter, err error) {
	a.logger.Error("api request failed", "error", err)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, store.ErrNamespaceExhausted):
		writeError(w, http.StatusConflict, "namespace has no available values")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		if isClientError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func isClientError(err error) bool {
	msg := err.Error()
	for _, marker := range []string{" is required", "must match", "invalid "} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("request body must contain exactly one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message, "status": status})
}

func (a *API) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "client_ip", a.clientIP(r))
		next.ServeHTTP(w, r)
	})
}
