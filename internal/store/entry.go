package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hctty27/x-type-center/internal/model"
)

func (s *MySQL) SearchEntries(ctx context.Context, params model.SearchParams) (model.SearchResult, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	var where []string
	var args []any
	where = append(where, "1=1")
	if params.Namespace != "" {
		where = append(where, "n.code = ?")
		args = append(args, params.Namespace)
	}
	if params.Project != "" {
		where = append(where, "e.project = ?")
		args = append(args, params.Project)
	}
	if params.Query != "" {
		like := "%" + params.Query + "%"
		where = append(where, `(n.code LIKE ? OR e.symbol LIKE ? OR e.description LIKE ? OR e.project LIKE ? OR e.requirement_ref LIKE ? OR CAST(e.value AS CHAR) = ?)`)
		args = append(args, like, like, like, like, like, params.Query)
	}

	whereSQL := strings.Join(where, " AND ")
	countQuery := `SELECT COUNT(*)
		FROM type_entries e JOIN type_namespaces n ON n.id = e.namespace_id
		WHERE ` + whereSQL

	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return model.SearchResult{}, fmt.Errorf("count search entries: %w", err)
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)

	query := `SELECT e.id, e.namespace_id, n.code, e.value, COALESCE(e.symbol, ''), e.project, e.description,
	                 e.requirement_ref, e.requester, e.request_ip, e.status,
	                 e.allocation_id, e.revoked_at, e.revoked_by, e.revoke_ip, e.revoke_reason,
	                 e.created_at
	          FROM type_entries e
	          JOIN type_namespaces n ON n.id = e.namespace_id
	          WHERE ` + whereSQL + `
	          ORDER BY COALESCE(e.revoked_at, e.created_at) DESC, e.id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return model.SearchResult{}, fmt.Errorf("search entries: %w", err)
	}
	defer rows.Close()

	result := make([]model.TypeEntry, 0, limit)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return model.SearchResult{}, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return model.SearchResult{}, err
	}
	return model.SearchResult{Items: result, Total: total}, nil
}

func (s *MySQL) GetEntry(ctx context.Context, namespace string, value int64) (model.TypeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.namespace_id, n.code, e.value, COALESCE(e.symbol, ''), e.project, e.description,
		       e.requirement_ref, e.requester, e.request_ip, e.status,
		       e.allocation_id, e.revoked_at, e.revoked_by, e.revoke_ip, e.revoke_reason,
		       e.created_at
		FROM type_entries e
		JOIN type_namespaces n ON n.id = e.namespace_id
		WHERE n.code = ? AND e.value = ? AND e.status <> 'REVOKED'`, namespace, value)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TypeEntry{}, ErrNotFound
	}
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("get entry: %w", err)
	}
	return entry, nil
}

func (s *MySQL) GetEntryByID(ctx context.Context, id int64) (model.TypeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.namespace_id, n.code, e.value, COALESCE(e.symbol, ''), e.project, e.description,
		       e.requirement_ref, e.requester, e.request_ip, e.status,
		       e.allocation_id, e.revoked_at, e.revoked_by, e.revoke_ip, e.revoke_reason,
		       e.created_at
		FROM type_entries e
		JOIN type_namespaces n ON n.id = e.namespace_id
		WHERE e.id = ?`, id)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TypeEntry{}, ErrNotFound
	}
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("get entry by id: %w", err)
	}
	return entry, nil
}

func scanEntry(row scanner) (model.TypeEntry, error) {
	var e model.TypeEntry
	var revokedAt sql.NullTime
	err := row.Scan(
		&e.ID, &e.NamespaceID, &e.Namespace, &e.Value, &e.Symbol, &e.Project, &e.Description,
		&e.Requirement, &e.Requester, &e.RequestIP, &e.Status, &e.AllocationID,
		&revokedAt, &e.RevokedBy, &e.RevokeIP, &e.RevokeReason, &e.CreatedAt,
	)
	if err != nil {
		return model.TypeEntry{}, err
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		e.RevokedAt = &value
	}
	return e, nil
}

func (s *MySQL) Allocate(ctx context.Context, req model.AllocateRequest) (model.TypeEntry, error) {
	_, items, err := s.AllocateBatch(ctx, req, 1)
	if err != nil {
		return model.TypeEntry{}, err
	}
	return items[0], nil
}

func (s *MySQL) AllocateBatch(ctx context.Context, req model.AllocateRequest, count int) (string, []model.TypeEntry, error) {
	if count <= 0 {
		return "", nil, fmt.Errorf("allocation count must be positive")
	}

	allocationID, err := newAllocationID()
	if err != nil {
		return "", nil, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", nil, fmt.Errorf("begin allocation tx: %w", err)
	}
	defer tx.Rollback()

	ns, err := lockNamespace(ctx, tx, req.Namespace)
	if err != nil {
		return "", nil, err
	}

	if req.Project != "" {
		project, err := ensureProject(ctx, tx, req.Project)
		if err != nil {
			return "", nil, err
		}
		req.Project = project
	}

	items := make([]model.TypeEntry, 0, count)
	for i := 0; i < count; i++ {
		candidate, fromReserved, err := chooseCandidate(ctx, tx, ns, req.Project)
		if err != nil {
			return "", nil, err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO type_entries(namespace_id, value, symbol, project, description, requirement_ref, requester, request_ip, allocation_id, status)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, 'ACTIVE')`,
			ns.ID, candidate, req.Symbol, req.Project, req.Description, req.Requirement, req.Requester, req.ClientIP, allocationID)
		if err != nil {
			if isDuplicateKey(err) {
				return "", nil, fmt.Errorf("%w: value or symbol already registered", ErrConflict)
			}
			return "", nil, fmt.Errorf("insert allocated type: %w", err)
		}
		entryID, err := result.LastInsertId()
		if err != nil {
			return "", nil, fmt.Errorf("read allocated id: %w", err)
		}

		if !fromReserved || candidate >= ns.NextValue {
			next, err := nextGlobalCandidate(ctx, tx, ns, candidate+1)
			if err != nil && !errors.Is(err, ErrNamespaceExhausted) {
				return "", nil, err
			}
			if errors.Is(err, ErrNamespaceExhausted) {
				next = candidate + 1
			}
			if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET next_value = ? WHERE id = ?`, next, ns.ID); err != nil {
				return "", nil, fmt.Errorf("update namespace cursor: %w", err)
			}
			ns.NextValue = next
		}


		now := time.Now().UTC()
		items = append(items, model.TypeEntry{
			ID:           entryID,
			NamespaceID:  ns.ID,
			Namespace:    ns.Code,
			Value:        candidate,
			Symbol:       req.Symbol,
			Project:      req.Project,
			Description:  req.Description,
			Requirement:  req.Requirement,
			Requester:    req.Requester,
			RequestIP:    req.ClientIP,
			Status:       model.StatusActive,
			AllocationID: allocationID,
			CreatedAt:    now,
		})
	}

	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("commit allocation: %w", err)
	}
	return allocationID, items, nil
}

func newAllocationID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate allocation id: %w", err)
	}
	return "alloc_" + hex.EncodeToString(data[:]), nil
}

func (s *MySQL) RevokeEntry(ctx context.Context, id int64, requester, reason, clientIP string) (model.TypeEntry, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("begin revoke tx: %w", err)
	}
	defer tx.Rollback()

	var namespaceID int64
	if err := tx.QueryRowContext(ctx, `SELECT namespace_id FROM type_entries WHERE id = ?`, id).Scan(&namespaceID); errors.Is(err, sql.ErrNoRows) {
		return model.TypeEntry{}, ErrNotFound
	} else if err != nil {
		return model.TypeEntry{}, fmt.Errorf("find entry namespace for revoke: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT id FROM type_namespaces WHERE id = ? FOR UPDATE`, namespaceID); err != nil {
		return model.TypeEntry{}, fmt.Errorf("lock namespace for revoke: %w", err)
	}

	var status string
	var value int64
	err = tx.QueryRowContext(ctx, `
		SELECT status, value
		FROM type_entries
		WHERE id = ?
		FOR UPDATE`, id).Scan(&status, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TypeEntry{}, ErrNotFound
	}
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("lock entry for revoke: %w", err)
	}

	if status == model.StatusRevoked {
		if err := tx.Commit(); err != nil {
			return model.TypeEntry{}, fmt.Errorf("commit revoke lookup: %w", err)
		}
		return s.GetEntryByID(ctx, id)
	}
	if status != model.StatusActive {
		return model.TypeEntry{}, fmt.Errorf("%w: only ACTIVE entries can be revoked", ErrConflict)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE type_entries
		SET status = 'REVOKED', revoked_by = ?, revoke_ip = ?, revoke_reason = ?, revoked_at = CURRENT_TIMESTAMP(6)
		WHERE id = ?`, requester, clientIP, reason, id); err != nil {
		return model.TypeEntry{}, fmt.Errorf("revoke entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE type_namespaces
		SET next_value = LEAST(next_value, ?)
		WHERE id = ?`, value, namespaceID); err != nil {
		return model.TypeEntry{}, fmt.Errorf("release revoked value: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.TypeEntry{}, fmt.Errorf("commit revoke: %w", err)
	}
	return s.GetEntryByID(ctx, id)
}

func (s *MySQL) RevokeAllocation(ctx context.Context, allocationID, requester, reason, clientIP string) ([]model.TypeEntry, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin allocation revoke tx: %w", err)
	}
	defer tx.Rollback()

	var namespaceID int64
	err = tx.QueryRowContext(ctx, `
		SELECT namespace_id
		FROM type_entries
		WHERE allocation_id = ?
		LIMIT 1`, allocationID).Scan(&namespaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find allocation namespace for revoke: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT id FROM type_namespaces WHERE id = ? FOR UPDATE`, namespaceID); err != nil {
		return nil, fmt.Errorf("lock namespace for allocation revoke: %w", err)
	}

	type revokeItem struct {
		id     int64
		status string
		value  int64
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, status, value
		FROM type_entries
		WHERE allocation_id = ?
		ORDER BY id
		FOR UPDATE`, allocationID)
	if err != nil {
		return nil, fmt.Errorf("list allocation entries for revoke: %w", err)
	}

	var targets []revokeItem
	for rows.Next() {
		var item revokeItem
		if err := rows.Scan(&item.id, &item.status, &item.value); err != nil {
			rows.Close()
			return nil, err
		}
		if item.status != model.StatusActive && item.status != model.StatusRevoked {
			rows.Close()
			return nil, fmt.Errorf("%w: allocation contains a non-revocable entry", ErrConflict)
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close allocation revoke rows: %w", err)
	}
	if len(targets) == 0 {
		return nil, ErrNotFound
	}

	var minReleased *int64
	for _, item := range targets {
		if item.status == model.StatusRevoked {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE type_entries
			SET status = 'REVOKED', revoked_by = ?, revoke_ip = ?, revoke_reason = ?, revoked_at = CURRENT_TIMESTAMP(6)
			WHERE id = ?`, requester, clientIP, reason, item.id); err != nil {
			return nil, fmt.Errorf("revoke allocation entry: %w", err)
		}
		if minReleased == nil || item.value < *minReleased {
			value := item.value
			minReleased = &value
		}
	}
	if minReleased != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE type_namespaces
			SET next_value = LEAST(next_value, ?)
			WHERE id = ?`, *minReleased, namespaceID); err != nil {
			return nil, fmt.Errorf("release allocation values: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit allocation revoke: %w", err)
	}
	return s.ListAllocationEntries(ctx, allocationID)
}

func (s *MySQL) ListAllocationEntries(ctx context.Context, allocationID string) ([]model.TypeEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.namespace_id, n.code, e.value, COALESCE(e.symbol, ''), e.project, e.description,
		       e.requirement_ref, e.requester, e.request_ip, e.status,
		       e.allocation_id, e.revoked_at, e.revoked_by, e.revoke_ip, e.revoke_reason,
		       e.created_at
		FROM type_entries e
		JOIN type_namespaces n ON n.id = e.namespace_id
		WHERE e.allocation_id = ?
		ORDER BY e.id`, allocationID)
	if err != nil {
		return nil, fmt.Errorf("list allocation entries: %w", err)
	}
	defer rows.Close()

	var items []model.TypeEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func chooseCandidate(ctx context.Context, tx *sql.Tx, ns model.Namespace, project string) (int64, bool, error) {
	if project != "" {
		for _, r := range ns.ReservedRanges {
			if !strings.EqualFold(strings.TrimSpace(r.Project), strings.TrimSpace(project)) {
				continue
			}
			candidate, ok, err := firstFreeInRange(ctx, tx, ns.ID, r.StartValue, r.EndValue)
			if err != nil {
				return 0, false, err
			}
			if ok {
				return candidate, true, nil
			}
		}
	}
	candidate, err := nextGlobalCandidate(ctx, tx, ns, ns.NextValue)
	return candidate, false, err
}

func firstFreeInRange(ctx context.Context, tx *sql.Tx, namespaceID, start, end int64) (int64, bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT value FROM type_entries
		WHERE namespace_id = ? AND value BETWEEN ? AND ? AND status <> 'REVOKED'
		ORDER BY value`, namespaceID, start, end)
	if err != nil {
		return 0, false, fmt.Errorf("query reserved range usage: %w", err)
	}
	defer rows.Close()

	candidate := start
	for rows.Next() {
		var used int64
		if err := rows.Scan(&used); err != nil {
			return 0, false, err
		}
		if used < candidate {
			continue
		}
		if used == candidate {
			candidate++
			continue
		}
		break
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if candidate > end {
		return 0, false, nil
	}
	return candidate, true, nil
}

func nextGlobalCandidate(ctx context.Context, tx *sql.Tx, ns model.Namespace, start int64) (int64, error) {
	candidate := start
	if ns.MinValue != nil && candidate < *ns.MinValue {
		candidate = *ns.MinValue
	}

	for {
		if ns.MaxValue != nil && candidate > *ns.MaxValue {
			return 0, ErrNamespaceExhausted
		}
		skipped := false
		for _, r := range ns.ReservedRanges {
			if candidate >= r.StartValue && candidate <= r.EndValue {
				candidate = r.EndValue + 1
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}

		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM type_entries WHERE namespace_id = ? AND value = ? AND status <> 'REVOKED' LIMIT 1`, ns.ID, candidate).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return 0, fmt.Errorf("check allocated value: %w", err)
		}
		candidate++
	}
}

func (s *MySQL) RecalculateNamespaceCursor(ctx context.Context, namespaceID int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT id, code, display_name, description, next_value, min_value, max_value, status, reserved_ranges
		FROM type_namespaces WHERE id = ? FOR UPDATE`, namespaceID)
	var ns model.Namespace
	var minValue, maxValue sql.NullInt64
	var rawRanges []byte
	if err := row.Scan(&ns.ID, &ns.Code, &ns.DisplayName, &ns.Description, &ns.NextValue, &minValue, &maxValue, &ns.Status, &rawRanges); err != nil {
		return err
	}
	if minValue.Valid {
		ns.MinValue = &minValue.Int64
	}
	if maxValue.Valid {
		ns.MaxValue = &maxValue.Int64
	}
	ns.ReservedRanges, err = decodeReservedRanges(rawRanges, ns.ID)
	if err != nil {
		return err
	}

	start := int64(1)
	if ns.MinValue != nil {
		start = *ns.MinValue
	}
	candidate, err := nextGlobalCandidate(ctx, tx, ns, start)
	if err != nil && !errors.Is(err, ErrNamespaceExhausted) {
		return err
	}
	if errors.Is(err, ErrNamespaceExhausted) {
		candidate = start
		if ns.MaxValue != nil {
			candidate = *ns.MaxValue + 1
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET next_value = ? WHERE id = ?`, candidate, namespaceID); err != nil {
		return err
	}
	return tx.Commit()
}
