package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

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

func (r *Registry) ListProjects(ctx context.Context) ([]model.Project, error) {
	return r.store.ListProjects(ctx)
}

func (r *Registry) GetNamespace(ctx context.Context, code string) (model.Namespace, []model.ReservedRange, error) {
	ns, err := r.store.GetNamespace(ctx, strings.TrimSpace(code))
	if err != nil {
		return model.Namespace{}, nil, err
	}
	ranges, err := r.store.ListReservedRanges(ctx, ns.ID)
	return ns, ranges, err
}

func (r *Registry) ResolveNamespace(ctx context.Context, query string) (model.NamespaceResolveResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return model.NamespaceResolveResult{}, fmt.Errorf("query is required")
	}
	ns, matchType, matchedAlias, err := r.store.ResolveNamespace(ctx, query)
	if errors.Is(err, store.ErrNotFound) {
		return model.NamespaceResolveResult{Matched: false}, nil
	}
	if err != nil {
		return model.NamespaceResolveResult{}, err
	}
	return model.NamespaceResolveResult{
		Matched:      true,
		MatchType:    matchType,
		Namespace:    &ns,
		MatchedAlias: matchedAlias,
	}, nil
}

func (r *Registry) ListNamespaceAliases(ctx context.Context, code string) ([]model.NamespaceAlias, error) {
	ns, err := r.store.GetNamespace(ctx, strings.TrimSpace(code))
	if err != nil {
		return nil, err
	}
	return r.store.ListNamespaceAliases(ctx, ns.ID)
}

func (r *Registry) CreateNamespaceAlias(ctx context.Context, code string, req model.CreateNamespaceAliasRequest) (model.NamespaceAlias, error) {
	ns, err := r.store.GetNamespace(ctx, strings.TrimSpace(code))
	if err != nil {
		return model.NamespaceAlias{}, err
	}

	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		return model.NamespaceAlias{}, fmt.Errorf("alias is required")
	}
	if utf8.RuneCountInString(alias) > 255 {
		return model.NamespaceAlias{}, fmt.Errorf("invalid alias: must be at most 255 characters")
	}
	if strings.EqualFold(alias, ns.Code) || strings.EqualFold(alias, ns.DisplayName) {
		return model.NamespaceAlias{}, fmt.Errorf("invalid alias: duplicates namespace code or display name")
	}
	conflict, err := r.store.NamespaceLabelExists(ctx, alias)
	if err != nil {
		return model.NamespaceAlias{}, err
	}
	if conflict {
		return model.NamespaceAlias{}, fmt.Errorf("%w: alias conflicts with a namespace code or display name", store.ErrConflict)
	}
	return r.store.CreateNamespaceAlias(ctx, ns.ID, alias)
}

func (r *Registry) DeleteNamespaceAlias(ctx context.Context, code string, aliasID int64) error {
	if aliasID <= 0 {
		return fmt.Errorf("invalid alias id")
	}
	ns, err := r.store.GetNamespace(ctx, strings.TrimSpace(code))
	if err != nil {
		return err
	}
	return r.store.DeleteNamespaceAlias(ctx, ns.ID, aliasID)
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
		ClientIP:    req.ClientIP,
	})
	if err != nil {
		return model.AllocateBatchResult{}, err
	}
	if count > 1 && normalized.Symbol != "" {
		return model.AllocateBatchResult{}, fmt.Errorf("invalid symbol: batch allocation requires an empty symbol")
	}

	allocationID, items, err := r.store.AllocateBatch(ctx, normalized, count)
	if err != nil {
		return model.AllocateBatchResult{}, err
	}
	values := make([]int64, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
	}
	return model.AllocateBatchResult{
		AllocationID: allocationID,
		Namespace:    normalized.Namespace,
		Count:        len(items),
		Values:       values,
		Items:        items,
	}, nil
}

func (r *Registry) RevokeEntry(ctx context.Context, id int64, req model.RevokeRequest) (model.TypeEntry, error) {
	if id <= 0 {
		return model.TypeEntry{}, fmt.Errorf("invalid entry id")
	}
	normalized, err := normalizeRevokeRequest(req)
	if err != nil {
		return model.TypeEntry{}, err
	}
	return r.store.RevokeEntry(ctx, id, normalized.Requester, normalized.Reason, normalized.ClientIP)
}

func (r *Registry) RevokeAllocation(ctx context.Context, allocationID string, req model.RevokeRequest) (model.RevokeAllocationResult, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return model.RevokeAllocationResult{}, fmt.Errorf("allocation id is required")
	}
	normalized, err := normalizeRevokeRequest(req)
	if err != nil {
		return model.RevokeAllocationResult{}, err
	}
	items, err := r.store.RevokeAllocation(ctx, allocationID, normalized.Requester, normalized.Reason, normalized.ClientIP)
	if err != nil {
		return model.RevokeAllocationResult{}, err
	}
	return model.RevokeAllocationResult{
		AllocationID: allocationID,
		Count:        len(items),
		Items:        items,
	}, nil
}

func normalizeRevokeRequest(req model.RevokeRequest) (model.RevokeRequest, error) {
	req.Requester = strings.TrimSpace(req.Requester)
	req.Reason = strings.TrimSpace(req.Reason)

	if utf8.RuneCountInString(req.Requester) > 128 {
		return model.RevokeRequest{}, fmt.Errorf("invalid requester: must be at most 128 characters")
	}
	if utf8.RuneCountInString(req.Reason) > 500 {
		return model.RevokeRequest{}, fmt.Errorf("invalid reason: must be at most 500 characters")
	}
	return req, nil
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
	if utf8.RuneCountInString(req.Project) > 128 {
		return model.AllocateRequest{}, fmt.Errorf("invalid project: must be at most 128 characters")
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
