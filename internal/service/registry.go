package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hctty27/x-type-center/internal/model"
	"github.com/hctty27/x-type-center/internal/store"
)

var symbolPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

type Registry struct {
	store *store.MySQL
}

func NewRegistry(s *store.MySQL) *Registry {
	return &Registry{store: s}
}

func (r *Registry) ListNamespaces(ctx context.Context) ([]model.Namespace, error) {
	return r.store.ListNamespaces(ctx)
}

func (r *Registry) GetNamespace(ctx context.Context, code string) (model.Namespace, []model.ReservedRange, error) {
	ns, err := r.store.GetNamespace(ctx, strings.TrimSpace(code))
	if err != nil {
		return model.Namespace{}, nil, err
	}
	ranges, err := r.store.ListReservedRanges(ctx, ns.ID)
	return ns, ranges, err
}

func (r *Registry) Search(ctx context.Context, params model.SearchParams) (model.SearchResult, error) {
	params.Query = strings.TrimSpace(params.Query)
	params.Namespace = strings.TrimSpace(params.Namespace)
	params.Project = strings.TrimSpace(params.Project)
	return r.store.SearchEntries(ctx, params)
}

func (r *Registry) Allocate(ctx context.Context, req model.AllocateRequest) (model.TypeEntry, error) {
	normalized, err := normalizeAllocateRequest(req)
	if err != nil {
		return model.TypeEntry{}, err
	}
	return r.store.Allocate(ctx, normalized)
}

func (r *Registry) AllocateBatch(ctx context.Context, req model.AllocateBatchRequest) (model.AllocateBatchResult, error) {
	count := req.Count
	if count == 0 {
		count = 1
	}
	if count < 1 || count > 100 {
		return model.AllocateBatchResult{}, fmt.Errorf("invalid count: must be between 1 and 100")
	}

	normalized, err := normalizeAllocateRequest(model.AllocateRequest{
		Namespace:   req.Namespace,
		Project:     req.Project,
		Symbol:      req.Symbol,
		Description: req.Description,
		Requirement: req.Requirement,
		Requester:   req.Requester,
	})
	if err != nil {
		return model.AllocateBatchResult{}, err
	}
	if count > 1 && normalized.Symbol != "" {
		return model.AllocateBatchResult{}, fmt.Errorf("invalid symbol: batch allocation requires an empty symbol")
	}

	items, err := r.store.AllocateBatch(ctx, normalized, count)
	if err != nil {
		return model.AllocateBatchResult{}, err
	}
	values := make([]int64, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
	}
	return model.AllocateBatchResult{
		Namespace: normalized.Namespace,
		Count:     len(items),
		Values:    values,
		Items:     items,
	}, nil
}

func normalizeAllocateRequest(req model.AllocateRequest) (model.AllocateRequest, error) {
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Project = strings.TrimSpace(req.Project)
	req.Symbol = strings.TrimSpace(req.Symbol)
	req.Description = strings.TrimSpace(req.Description)
	req.Requirement = strings.TrimSpace(req.Requirement)
	req.Requester = strings.TrimSpace(req.Requester)

	if req.Namespace == "" {
		return model.AllocateRequest{}, fmt.Errorf("namespace is required")
	}
	if req.Symbol != "" && !symbolPattern.MatchString(req.Symbol) {
		return model.AllocateRequest{}, fmt.Errorf("symbol must match %s", symbolPattern.String())
	}
	return req, nil
}

func (r *Registry) Validate(ctx context.Context, req model.ValidateRequest) (model.ValidationResult, error) {
	if strings.TrimSpace(req.Namespace) == "" {
		return model.ValidationResult{}, fmt.Errorf("namespace is required")
	}
	entry, err := r.store.GetEntry(ctx, strings.TrimSpace(req.Namespace), req.Value)
	if errors.Is(err, store.ErrNotFound) {
		return model.ValidationResult{Valid: false, Exists: false, Message: "type is not registered"}, nil
	}
	if err != nil {
		return model.ValidationResult{}, err
	}

	symbolMatch := req.Symbol == "" || entry.Symbol == req.Symbol
	projectMatch := req.Project == "" || strings.EqualFold(entry.Project, req.Project)
	valid := symbolMatch && projectMatch && entry.Status == model.StatusActive
	message := "ok"
	if !symbolMatch {
		message = "symbol does not match registry"
	} else if !projectMatch {
		message = "project does not match registry"
	} else if entry.Status != model.StatusActive {
		message = "type is not active"
	}
	return model.ValidationResult{
		Valid:        valid,
		Exists:       true,
		SymbolMatch:  symbolMatch,
		ProjectMatch: projectMatch,
		Entry:        &entry,
		Message:      message,
	}, nil
}
