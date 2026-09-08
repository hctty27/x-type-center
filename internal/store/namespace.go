package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hctty27/x-type-center/internal/model"
)

func (s *MySQL) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at, updated_at
		FROM projects
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var result []model.Project
	for rows.Next() {
		var item model.Project
		if err := rows.Scan(&item.ID, &item.Name, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *MySQL) ListNamespaces(ctx context.Context) ([]model.Namespace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.code, n.display_name, n.description, n.aliases, n.next_value, n.min_value, n.max_value, n.status,
		       (SELECT MAX(e.value) FROM type_entries e WHERE e.namespace_id = n.id) AS current_max,
		       (SELECT COUNT(*) FROM type_entries e WHERE e.namespace_id = n.id AND e.status = 'ACTIVE') AS used_count
		FROM type_namespaces n
		ORDER BY n.code`)
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	defer rows.Close()

	var result []model.Namespace
	for rows.Next() {
		ns, err := scanNamespaceSummary(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, ns)
	}
	return result, rows.Err()
}

func (s *MySQL) GetNamespace(ctx context.Context, code string) (model.Namespace, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT n.id, n.code, n.display_name, n.description, n.aliases, n.next_value, n.min_value, n.max_value, n.status,
		       (SELECT MAX(e.value) FROM type_entries e WHERE e.namespace_id = n.id) AS current_max,
		       (SELECT COUNT(*) FROM type_entries e WHERE e.namespace_id = n.id AND e.status = 'ACTIVE') AS used_count
		FROM type_namespaces n
		WHERE n.code = ?`, code)
	ns, err := scanNamespaceSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Namespace{}, ErrNotFound
	}
	if err != nil {
		return model.Namespace{}, fmt.Errorf("get namespace: %w", err)
	}
	return ns, nil
}

func (s *MySQL) ListNamespaceAliases(ctx context.Context, namespaceID int64) ([]model.NamespaceAlias, error) {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT aliases FROM type_namespaces WHERE id = ?`, namespaceID).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("list namespace aliases: %w", err)
	}
	aliases, err := decodeAliases(raw, namespaceID)
	if err != nil {
		return nil, err
	}
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
	return aliases, nil
}

func (s *MySQL) NamespaceLabelExists(ctx context.Context, label string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, code, display_name, aliases FROM type_namespaces`)
	if err != nil {
		return false, fmt.Errorf("check namespace label: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var code, displayName string
		var raw []byte
		if err := rows.Scan(&id, &code, &displayName, &raw); err != nil {
			return false, err
		}
		if strings.EqualFold(label, code) || strings.EqualFold(label, displayName) {
			return true, nil
		}
		aliases, err := decodeAliases(raw, id)
		if err != nil {
			return false, err
		}
		for _, item := range aliases {
			if strings.EqualFold(label, item.Alias) {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

func (s *MySQL) CreateNamespaceAlias(ctx context.Context, namespaceID int64, alias string) (model.NamespaceAlias, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return model.NamespaceAlias{}, fmt.Errorf("begin alias tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, code, display_name, aliases
		FROM type_namespaces
		ORDER BY id
		FOR UPDATE`)
	if err != nil {
		return model.NamespaceAlias{}, fmt.Errorf("lock namespaces for alias: %w", err)
	}

	var target []model.NamespaceAlias
	found := false
	for rows.Next() {
		var id int64
		var code, displayName string
		var raw []byte
		if err := rows.Scan(&id, &code, &displayName, &raw); err != nil {
			rows.Close()
			return model.NamespaceAlias{}, err
		}
		aliases, err := decodeAliases(raw, id)
		if err != nil {
			rows.Close()
			return model.NamespaceAlias{}, err
		}
		if strings.EqualFold(alias, code) || strings.EqualFold(alias, displayName) {
			rows.Close()
			return model.NamespaceAlias{}, fmt.Errorf("%w: alias conflicts with a namespace code or display name", ErrConflict)
		}
		for _, item := range aliases {
			if strings.EqualFold(alias, item.Alias) {
				rows.Close()
				return model.NamespaceAlias{}, fmt.Errorf("%w: alias already exists", ErrConflict)
			}
		}
		if id == namespaceID {
			found = true
			target = aliases
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return model.NamespaceAlias{}, err
	}
	if err := rows.Close(); err != nil {
		return model.NamespaceAlias{}, err
	}
	if !found {
		return model.NamespaceAlias{}, ErrNotFound
	}

	var nextID int64 = 1
	for _, item := range target {
		if item.ID >= nextID {
			nextID = item.ID + 1
		}
	}
	item := model.NamespaceAlias{
		ID:          nextID,
		NamespaceID: namespaceID,
		Alias:       alias,
		CreatedAt:   time.Now().UTC(),
	}
	target = append(target, item)
	payload, err := json.Marshal(target)
	if err != nil {
		return model.NamespaceAlias{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET aliases = ? WHERE id = ?`, payload, namespaceID); err != nil {
		return model.NamespaceAlias{}, fmt.Errorf("create namespace alias: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.NamespaceAlias{}, fmt.Errorf("commit alias: %w", err)
	}
	return item, nil
}

func (s *MySQL) DeleteNamespaceAlias(ctx context.Context, namespaceID, aliasID int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin delete alias tx: %w", err)
	}
	defer tx.Rollback()

	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT aliases FROM type_namespaces WHERE id = ? FOR UPDATE`, namespaceID).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock namespace aliases: %w", err)
	}
	aliases, err := decodeAliases(raw, namespaceID)
	if err != nil {
		return err
	}
	index := -1
	for i, item := range aliases {
		if item.ID == aliasID {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNotFound
	}
	aliases = append(aliases[:index], aliases[index+1:]...)
	payload, err := json.Marshal(aliases)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET aliases = ? WHERE id = ?`, payload, namespaceID); err != nil {
		return fmt.Errorf("delete namespace alias: %w", err)
	}
	return tx.Commit()
}

func (s *MySQL) ResolveNamespace(ctx context.Context, query string) (model.Namespace, string, string, error) {
	ns, err := s.GetNamespace(ctx, query)
	if err == nil {
		return ns, "CODE", "", nil
	}
	if !errors.Is(err, ErrNotFound) {
		return model.Namespace{}, "", "", err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, code, display_name, aliases FROM type_namespaces ORDER BY id`)
	if err != nil {
		return model.Namespace{}, "", "", fmt.Errorf("resolve namespace: %w", err)
	}
	defer rows.Close()

	var displayCodes []string
	var aliasCode, matchedAlias string
	for rows.Next() {
		var id int64
		var code, displayName string
		var raw []byte
		if err := rows.Scan(&id, &code, &displayName, &raw); err != nil {
			return model.Namespace{}, "", "", err
		}
		if strings.EqualFold(query, displayName) {
			displayCodes = append(displayCodes, code)
		}
		aliases, err := decodeAliases(raw, id)
		if err != nil {
			return model.Namespace{}, "", "", err
		}
		for _, item := range aliases {
			if strings.EqualFold(query, item.Alias) {
				aliasCode = code
				matchedAlias = item.Alias
			}
		}
	}
	if err := rows.Err(); err != nil {
		return model.Namespace{}, "", "", err
	}
	if len(displayCodes) > 1 {
		return model.Namespace{}, "", "", fmt.Errorf("%w: namespace name %q is ambiguous", ErrConflict, query)
	}
	if len(displayCodes) == 1 {
		ns, err := s.GetNamespace(ctx, displayCodes[0])
		return ns, "DISPLAY_NAME", "", err
	}
	if aliasCode != "" {
		ns, err := s.GetNamespace(ctx, aliasCode)
		return ns, "ALIAS", matchedAlias, err
	}
	return model.Namespace{}, "", "", ErrNotFound
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNamespaceSummary(row scanner) (model.Namespace, error) {
	var ns model.Namespace
	var rawAliases []byte
	var minValue, maxValue, currentMax sql.NullInt64
	if err := row.Scan(&ns.ID, &ns.Code, &ns.DisplayName, &ns.Description, &rawAliases, &ns.NextValue, &minValue, &maxValue, &ns.Status, &currentMax, &ns.UsedCount); err != nil {
		return model.Namespace{}, err
	}
	aliases, err := decodeAliases(rawAliases, ns.ID)
	if err != nil {
		return model.Namespace{}, err
	}
	for _, item := range aliases {
		ns.Aliases = append(ns.Aliases, item.Alias)
	}
	sort.Strings(ns.Aliases)
	if minValue.Valid {
		ns.MinValue = &minValue.Int64
	}
	if maxValue.Valid {
		ns.MaxValue = &maxValue.Int64
	}
	if currentMax.Valid {
		ns.CurrentMax = &currentMax.Int64
	}
	return ns, nil
}

func decodeAliases(raw []byte, namespaceID int64) ([]model.NamespaceAlias, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var aliases []model.NamespaceAlias
	if err := json.Unmarshal(raw, &aliases); err != nil {
		return nil, fmt.Errorf("decode namespace aliases: %w", err)
	}
	for i := range aliases {
		aliases[i].NamespaceID = namespaceID
	}
	return aliases, nil
}

func (s *MySQL) ListReservedRanges(ctx context.Context, namespaceID int64) ([]model.ReservedRange, error) {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT reserved_ranges FROM type_namespaces WHERE id = ?`, namespaceID).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("list reserved ranges: %w", err)
	}
	return decodeReservedRanges(raw, namespaceID)
}

func decodeReservedRanges(raw []byte, namespaceID int64) ([]model.ReservedRange, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var ranges []model.ReservedRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, fmt.Errorf("decode reserved ranges: %w", err)
	}
	for i := range ranges {
		ranges[i].NamespaceID = namespaceID
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].StartValue == ranges[j].StartValue {
			return ranges[i].EndValue < ranges[j].EndValue
		}
		return ranges[i].StartValue < ranges[j].StartValue
	})
	return ranges, nil
}

func ensureProject(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO projects(name)
		VALUES (?)`, name); err != nil {
		return "", fmt.Errorf("ensure project: %w", err)
	}

	var canonicalName string
	if err := tx.QueryRowContext(ctx, `
		SELECT name
		FROM projects
		WHERE name = ?
		LIMIT 1`, name).Scan(&canonicalName); err != nil {
		return "", fmt.Errorf("read project: %w", err)
	}
	return canonicalName, nil
}

func lockNamespace(ctx context.Context, tx *sql.Tx, code string) (model.Namespace, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, code, display_name, description, next_value, min_value, max_value, status, reserved_ranges
		FROM type_namespaces WHERE code = ? FOR UPDATE`, code)
	var ns model.Namespace
	var minValue, maxValue sql.NullInt64
	var rawRanges []byte
	if err := row.Scan(&ns.ID, &ns.Code, &ns.DisplayName, &ns.Description, &ns.NextValue, &minValue, &maxValue, &ns.Status, &rawRanges); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Namespace{}, ErrNotFound
		}
		return model.Namespace{}, fmt.Errorf("lock namespace: %w", err)
	}
	if ns.Status != model.StatusActive {
		return model.Namespace{}, fmt.Errorf("namespace %s is not active", code)
	}
	if minValue.Valid {
		ns.MinValue = &minValue.Int64
	}
	if maxValue.Valid {
		ns.MaxValue = &maxValue.Int64
	}
	ranges, err := decodeReservedRanges(rawRanges, ns.ID)
	if err != nil {
		return model.Namespace{}, err
	}
	ns.ReservedRanges = ranges
	return ns, nil
}

func (s *MySQL) EnsureNamespace(ctx context.Context, code, displayName string) (model.Namespace, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return model.Namespace{}, fmt.Errorf("namespace code is required")
	}
	if displayName == "" {
		displayName = code
	}
	if existing, err := s.GetNamespace(ctx, code); err == nil {
		if existing.DisplayName == "" && displayName != "" {
			if _, err := s.db.ExecContext(ctx, `UPDATE type_namespaces SET display_name = ? WHERE id = ?`, displayName, existing.ID); err != nil {
				return model.Namespace{}, fmt.Errorf("update namespace display name: %w", err)
			}
			return s.GetNamespace(ctx, code)
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return model.Namespace{}, err
	}

	for _, label := range []string{code, displayName} {
		conflict, err := s.namespaceAliasExists(ctx, label)
		if err != nil {
			return model.Namespace{}, err
		}
		if conflict {
			return model.Namespace{}, fmt.Errorf("%w: namespace code or display name conflicts with an existing alias", ErrConflict)
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO type_namespaces(code, display_name, next_value, status)
		VALUES (?, ?, 1, 'ACTIVE')`, code, displayName)
	if isDuplicateKey(err) {
		return s.GetNamespace(ctx, code)
	}
	if err != nil {
		return model.Namespace{}, fmt.Errorf("ensure namespace: %w", err)
	}
	return s.GetNamespace(ctx, code)
}

func (s *MySQL) namespaceAliasExists(ctx context.Context, label string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, aliases FROM type_namespaces`)
	if err != nil {
		return false, fmt.Errorf("check namespace alias conflict: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return false, err
		}
		aliases, err := decodeAliases(raw, id)
		if err != nil {
			return false, err
		}
		for _, item := range aliases {
			if strings.EqualFold(label, item.Alias) {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

func (s *MySQL) EnsureReservedRange(ctx context.Context, namespaceID, start, end int64, project, description string) error {
	if start > end {
		return fmt.Errorf("invalid reserved range %d-%d", start, end)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin reserved range tx: %w", err)
	}
	defer tx.Rollback()

	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT reserved_ranges FROM type_namespaces WHERE id = ? FOR UPDATE`, namespaceID).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock namespace reserved ranges: %w", err)
	}
	ranges, err := decodeReservedRanges(raw, namespaceID)
	if err != nil {
		return err
	}
	for _, item := range ranges {
		if item.StartValue == start && item.EndValue == end && item.Project == project {
			return tx.Commit()
		}
		if !(item.EndValue < start || item.StartValue > end) {
			return ErrReservedRangeOverlap
		}
	}
	if project != "" {
		canonical, err := ensureProject(ctx, tx, project)
		if err != nil {
			return err
		}
		project = canonical
	}
	var nextID int64 = 1
	for _, item := range ranges {
		if item.ID >= nextID {
			nextID = item.ID + 1
		}
	}
	ranges = append(ranges, model.ReservedRange{
		ID:          nextID,
		NamespaceID: namespaceID,
		StartValue:  start,
		EndValue:    end,
		Project:     project,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	})
	payload, err := json.Marshal(ranges)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET reserved_ranges = ? WHERE id = ?`, payload, namespaceID); err != nil {
		return fmt.Errorf("ensure reserved range: %w", err)
	}
	return tx.Commit()
}
