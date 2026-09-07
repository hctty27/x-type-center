package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/hctty27/x-type-center/internal/model"
	"github.com/hctty27/x-type-center/internal/service"
	"github.com/hctty27/x-type-center/internal/store"
)

type API struct {
	registry *service.Registry
	apiToken string
	logger   *slog.Logger
}

func New(registry *service.Registry, apiToken string, logger *slog.Logger) *API {
	return &API{registry: registry, apiToken: apiToken, logger: logger}
}

func (a *API) Routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/v1/namespaces", a.listNamespaces)
	mux.HandleFunc("GET /api/v1/namespaces/{code}", a.getNamespace)
	mux.HandleFunc("GET /api/v1/types/search", a.searchTypes)
	mux.Handle("POST /api/v1/types/allocate", a.requireWriteAuth(http.HandlerFunc(a.allocateType)))
	mux.Handle("POST /api/v1/types/validate", a.requireWriteAuth(http.HandlerFunc(a.validateType)))
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

func (a *API) getNamespace(w http.ResponseWriter, r *http.Request) {
	ns, ranges, err := a.registry.GetNamespace(r.Context(), r.PathValue("code"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespace": ns, "reservedRanges": ranges})
}

func (a *API) searchTypes(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := a.registry.Search(r.Context(), model.SearchParams{
		Query:     r.URL.Query().Get("q"),
		Namespace: r.URL.Query().Get("namespace"),
		Project:   r.URL.Query().Get("project"),
		Limit:     limit,
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	entry, err := a.registry.Allocate(r.Context(), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
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

func (a *API) requireWriteAuth(next http.Handler) http.Handler {
	if a.apiToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if subtle.ConstantTimeCompare([]byte(provided), []byte(a.apiToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid API token")
			return
		}
		next.ServeHTTP(w, r)
	})
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
		a.logger.Info("http request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
