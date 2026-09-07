package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/hctty27/x-type-center/internal/model"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrConflict             = errors.New("conflict")
	ErrNamespaceExhausted   = errors.New("namespace exhausted")
	ErrReservedRangeOverlap = errors.New("reserved range overlaps existing range")
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type MySQL struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string, maxOpen, maxIdle int, maxLifetime time.Duration) (*MySQL, error) {
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.MultiStatements = true
	if cfg.Loc == nil {
		cfg.Loc = time.UTC
	}

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(maxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return &MySQL{db: db}, nil
}

func (s *MySQL) Close() error {
	return s.db.Close()
}

func (s *MySQL) Migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := s.db.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

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
		SELECT n.id, n.code, n.display_name, n.description, n.next_value, n.min_value, n.max_value, n.status,
		       MAX(e.value) AS current_max, COUNT(e.id) AS used_count
		FROM type_namespaces n
		LEFT JOIN type_entries e ON e.namespace_id = n.id AND e.status = 'ACTIVE'
		GROUP BY n.id, n.code, n.display_name, n.description, n.next_value, n.min_value, n.max_value, n.status
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close namespaces rows: %w", err)
	}
	if err := s.attachNamespaceAliases(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *MySQL) GetNamespace(ctx context.Context, code string) (model.Namespace, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT n.id, n.code, n.display_name, n.description, n.next_value, n.min_value, n.max_value, n.status,
		       MAX(e.value) AS current_max, COUNT(e.id) AS used_count
		FROM type_namespaces n
		LEFT JOIN type_entries e ON e.namespace_id = n.id AND e.status = 'ACTIVE'
		WHERE n.code = ?
		GROUP BY n.id, n.code, n.display_name, n.description, n.next_value, n.min_value, n.max_value, n.status`, code)
	ns, err := scanNamespaceSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Namespace{}, ErrNotFound
	}
	if err != nil {
		return model.Namespace{}, fmt.Errorf("get namespace: %w", err)
	}
	aliases, err := s.ListNamespaceAliases(ctx, ns.ID)
	if err != nil {
		return model.Namespace{}, err
	}
	for _, item := range aliases {
		ns.Aliases = append(ns.Aliases, item.Alias)
	}
	return ns, nil
}

func (s *MySQL) attachNamespaceAliases(ctx context.Context, namespaces []model.Namespace) error {
	if len(namespaces) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT namespace_id, alias
		FROM namespace_aliases
		ORDER BY alias`)
	if err != nil {
		return fmt.Errorf("list namespace aliases: %w", err)
	}
	defer rows.Close()

	byNamespace := make(map[int64][]string)
	for rows.Next() {
		var namespaceID int64
		var alias string
		if err := rows.Scan(&namespaceID, &alias); err != nil {
			return err
		}
		byNamespace[namespaceID] = append(byNamespace[namespaceID], alias)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range namespaces {
		namespaces[i].Aliases = byNamespace[namespaces[i].ID]
	}
	return nil
}

func (s *MySQL) ListNamespaceAliases(ctx context.Context, namespaceID int64) ([]model.NamespaceAlias, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, namespace_id, alias, created_at
		FROM namespace_aliases
		WHERE namespace_id = ?
		ORDER BY alias`, namespaceID)
	if err != nil {
		return nil, fmt.Errorf("list namespace aliases: %w", err)
	}
	defer rows.Close()

	var result []model.NamespaceAlias
	for rows.Next() {
		var item model.NamespaceAlias
		if err := rows.Scan(&item.ID, &item.NamespaceID, &item.Alias, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *MySQL) NamespaceLabelExists(ctx context.Context, label string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM type_namespaces
		WHERE code = ? OR display_name = ?`, label, label).Scan(&count); err != nil {
		return false, fmt.Errorf("check namespace label: %w", err)
	}
	return count > 0, nil
}

func (s *MySQL) CreateNamespaceAlias(ctx context.Context, namespaceID int64, alias string) (model.NamespaceAlias, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO namespace_aliases(namespace_id, alias)
		VALUES (?, ?)`, namespaceID, alias)
	if isDuplicateKey(err) {
		return model.NamespaceAlias{}, fmt.Errorf("%w: alias already exists", ErrConflict)
	}
	if err != nil {
		return model.NamespaceAlias{}, fmt.Errorf("create namespace alias: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.NamespaceAlias{}, fmt.Errorf("read namespace alias id: %w", err)
	}
	return model.NamespaceAlias{
		ID:          id,
		NamespaceID: namespaceID,
		Alias:       alias,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *MySQL) DeleteNamespaceAlias(ctx context.Context, namespaceID, aliasID int64) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM namespace_aliases
		WHERE id = ? AND namespace_id = ?`, aliasID, namespaceID)
	if err != nil {
		return fmt.Errorf("delete namespace alias: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted alias count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQL) ResolveNamespace(ctx context.Context, query string) (model.Namespace, string, string, error) {
	ns, err := s.GetNamespace(ctx, query)
	if err == nil {
		return ns, "CODE", "", nil
	}
	if !errors.Is(err, ErrNotFound) {
		return model.Namespace{}, "", "", err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT code
		FROM type_namespaces
		WHERE display_name = ?
		ORDER BY id
		LIMIT 2`, query)
	if err != nil {
		return model.Namespace{}, "", "", fmt.Errorf("resolve namespace name: %w", err)
	}
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return model.Namespace{}, "", "", err
		}
		codes = append(codes, code)
	}
	if err := rows.Close(); err != nil {
		return model.Namespace{}, "", "", err
	}
	if len(codes) > 1 {
		return model.Namespace{}, "", "", fmt.Errorf("%w: namespace name %q is ambiguous", ErrConflict, query)
	}
	if len(codes) == 1 {
		ns, err := s.GetNamespace(ctx, codes[0])
		return ns, "DISPLAY_NAME", "", err
	}

	var code, matchedAlias string
	err = s.db.QueryRowContext(ctx, `
		SELECT n.code, a.alias
		FROM namespace_aliases a
		JOIN type_namespaces n ON n.id = a.namespace_id
		WHERE a.alias = ?
		LIMIT 1`, query).Scan(&code, &matchedAlias)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Namespace{}, "", "", ErrNotFound
	}
	if err != nil {
		return model.Namespace{}, "", "", fmt.Errorf("resolve namespace alias: %w", err)
	}
	ns, err = s.GetNamespace(ctx, code)
	return ns, "ALIAS", matchedAlias, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNamespaceSummary(row scanner) (model.Namespace, error) {
	var ns model.Namespace
	var minValue, maxValue, currentMax sql.NullInt64
	if err := row.Scan(&ns.ID, &ns.Code, &ns.DisplayName, &ns.Description, &ns.NextValue, &minValue, &maxValue, &ns.Status, &currentMax, &ns.UsedCount); err != nil {
		return model.Namespace{}, err
	}
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

func (s *MySQL) ListReservedRanges(ctx context.Context, namespaceID int64) ([]model.ReservedRange, error) {
	return listReservedRanges(ctx, s.db, namespaceID)
}

func listReservedRanges(ctx context.Context, q queryer, namespaceID int64) ([]model.ReservedRange, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, namespace_id, start_value, end_value, project, description, created_at
		FROM reserved_ranges WHERE namespace_id = ? ORDER BY start_value, end_value`, namespaceID)
	if err != nil {
		return nil, fmt.Errorf("list reserved ranges: %w", err)
	}
	defer rows.Close()
	var result []model.ReservedRange
	for rows.Next() {
		var item model.ReservedRange
		if err := rows.Scan(&item.ID, &item.NamespaceID, &item.StartValue, &item.EndValue, &item.Project, &item.Description, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

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
	                 e.requirement_ref, e.requester, e.source, e.source_ref, e.status, e.created_at, e.updated_at
	          FROM type_entries e JOIN type_namespaces n ON n.id = e.namespace_id
	          WHERE ` + whereSQL + `
	          ORDER BY e.updated_at DESC, e.id DESC LIMIT ? OFFSET ?`
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
		       e.requirement_ref, e.requester, e.source, e.source_ref, e.status, e.created_at, e.updated_at
		FROM type_entries e JOIN type_namespaces n ON n.id = e.namespace_id
		WHERE n.code = ? AND e.value = ?`, namespace, value)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TypeEntry{}, ErrNotFound
	}
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("get entry: %w", err)
	}
	return entry, nil
}

func scanEntry(row scanner) (model.TypeEntry, error) {
	var e model.TypeEntry
	err := row.Scan(&e.ID, &e.NamespaceID, &e.Namespace, &e.Value, &e.Symbol, &e.Project, &e.Description,
		&e.Requirement, &e.Requester, &e.Source, &e.SourceRef, &e.Status, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func (s *MySQL) Allocate(ctx context.Context, req model.AllocateRequest) (model.TypeEntry, error) {
	items, err := s.AllocateBatch(ctx, req, 1)
	if err != nil {
		return model.TypeEntry{}, err
	}
	return items[0], nil
}

func (s *MySQL) AllocateBatch(ctx context.Context, req model.AllocateRequest, count int) ([]model.TypeEntry, error) {
	if count <= 0 {
		return nil, fmt.Errorf("allocation count must be positive")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin allocation tx: %w", err)
	}
	defer tx.Rollback()

	ns, err := lockNamespace(ctx, tx, req.Namespace)
	if err != nil {
		return nil, err
	}

	if req.Project != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO projects(name)
			VALUES (?)`, req.Project); err != nil {
			return nil, fmt.Errorf("ensure project: %w", err)
		}
	}

	items := make([]model.TypeEntry, 0, count)
	for i := 0; i < count; i++ {
		candidate, fromReserved, err := chooseCandidate(ctx, tx, ns, req.Project)
		if err != nil {
			return nil, err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO type_entries(namespace_id, value, symbol, project, description, requirement_ref, requester, source, status)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, 'registry', 'ACTIVE')`,
			ns.ID, candidate, req.Symbol, req.Project, req.Description, req.Requirement, req.Requester)
		if err != nil {
			if isDuplicateKey(err) {
				return nil, fmt.Errorf("%w: value or symbol already registered", ErrConflict)
			}
			return nil, fmt.Errorf("insert allocated type: %w", err)
		}
		entryID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("read allocated id: %w", err)
		}

		if !fromReserved || candidate >= ns.NextValue {
			next, err := nextGlobalCandidate(ctx, tx, ns, candidate+1)
			if err != nil && !errors.Is(err, ErrNamespaceExhausted) {
				return nil, err
			}
			if errors.Is(err, ErrNamespaceExhausted) {
				next = candidate + 1
			}
			if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET next_value = ? WHERE id = ?`, next, ns.ID); err != nil {
				return nil, fmt.Errorf("update namespace cursor: %w", err)
			}
			ns.NextValue = next
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_logs(action, namespace_code, entry_value, actor, detail)
			VALUES ('ALLOCATE', ?, ?, ?, JSON_OBJECT('project', ?, 'symbol', ?, 'description', ?))`,
			req.Namespace, candidate, req.Requester, req.Project, req.Symbol, req.Description); err != nil {
			return nil, fmt.Errorf("write audit log: %w", err)
		}

		now := time.Now().UTC()
		items = append(items, model.TypeEntry{
			ID:          entryID,
			NamespaceID: ns.ID,
			Namespace:   ns.Code,
			Value:       candidate,
			Symbol:      req.Symbol,
			Project:     req.Project,
			Description: req.Description,
			Requirement: req.Requirement,
			Requester:   req.Requester,
			Source:      "registry",
			Status:      model.StatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit allocation: %w", err)
	}
	return items, nil
}

func lockNamespace(ctx context.Context, tx *sql.Tx, code string) (model.Namespace, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, code, display_name, description, next_value, min_value, max_value, status
		FROM type_namespaces WHERE code = ? FOR UPDATE`, code)
	var ns model.Namespace
	var minValue, maxValue sql.NullInt64
	if err := row.Scan(&ns.ID, &ns.Code, &ns.DisplayName, &ns.Description, &ns.NextValue, &minValue, &maxValue, &ns.Status); err != nil {
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
	return ns, nil
}

func chooseCandidate(ctx context.Context, tx *sql.Tx, ns model.Namespace, project string) (int64, bool, error) {
	if project != "" {
		ranges, err := listReservedRanges(ctx, tx, ns.ID)
		if err != nil {
			return 0, false, err
		}
		for _, r := range ranges {
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
		WHERE namespace_id = ? AND value BETWEEN ? AND ? AND status = 'ACTIVE'
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

	ranges, err := listReservedRanges(ctx, tx, ns.ID)
	if err != nil {
		return 0, err
	}
	for {
		if ns.MaxValue != nil && candidate > *ns.MaxValue {
			return 0, ErrNamespaceExhausted
		}
		skipped := false
		for _, r := range ranges {
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
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM type_entries WHERE namespace_id = ? AND value = ? LIMIT 1`, ns.ID, candidate).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return 0, fmt.Errorf("check allocated value: %w", err)
		}
		candidate++
	}
}

func (s *MySQL) EnsureNamespace(ctx context.Context, code, displayName string) (model.Namespace, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return model.Namespace{}, fmt.Errorf("namespace code is required")
	}
	if displayName == "" {
		displayName = code
	}
	var aliasCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM namespace_aliases
		WHERE alias = ? OR alias = ?`, code, displayName).Scan(&aliasCount); err != nil {
		return model.Namespace{}, fmt.Errorf("check namespace alias conflict: %w", err)
	}
	if aliasCount > 0 {
		return model.Namespace{}, fmt.Errorf("%w: namespace code or display name conflicts with an existing alias", ErrConflict)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO type_namespaces(code, display_name, next_value, status)
		VALUES (?, ?, 1, 'ACTIVE')
		ON DUPLICATE KEY UPDATE display_name = IF(display_name = '', ?, display_name)`, code, displayName, displayName)
	if err != nil {
		return model.Namespace{}, fmt.Errorf("ensure namespace: %w", err)
	}
	return s.GetNamespace(ctx, code)
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func (s *MySQL) EnsureReservedRange(ctx context.Context, namespaceID, start, end int64, project, description string) error {
	if start > end {
		return fmt.Errorf("invalid reserved range %d-%d", start, end)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reserved_ranges
		WHERE namespace_id = ? AND NOT (end_value < ? OR start_value > ?)
		  AND NOT (start_value = ? AND end_value = ? AND project = ?)`,
		namespaceID, start, end, start, end, project).Scan(&count); err != nil {
		return fmt.Errorf("check reserved overlap: %w", err)
	}
	if count > 0 {
		return ErrReservedRangeOverlap
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reserved_ranges(namespace_id, start_value, end_value, project, description)
		VALUES (?, ?, ?, ?, ?)`, namespaceID, start, end, project, description)
	if isDuplicateKey(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ensure reserved range: %w", err)
	}
	return nil
}

func (s *MySQL) ImportHistoricalEntry(ctx context.Context, namespaceID, value int64, project, description, sourceRef string) (bool, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO type_entries(namespace_id, value, symbol, project, description, source, source_ref, status)
		VALUES (?, ?, NULL, ?, ?, 'xlsx', ?, 'ACTIVE')`, namespaceID, value, project, description, sourceRef)
	if isDuplicateKey(err) {
		_, _ = s.db.ExecContext(ctx, `
			UPDATE type_entries SET
				project = IF(project = '' AND ? <> '', ?, project),
				description = IF(description = '' AND ? <> '', ?, description),
				source_ref = IF(source_ref = '', ?, source_ref)
			WHERE namespace_id = ? AND value = ?`,
			project, project, description, description, sourceRef, namespaceID, value)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert historical entry: %w", err)
	}
	return true, nil
}

func (s *MySQL) RecalculateNamespaceCursor(ctx context.Context, namespaceID int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT id, code, display_name, description, next_value, min_value, max_value, status
		FROM type_namespaces WHERE id = ? FOR UPDATE`, namespaceID)
	var ns model.Namespace
	var minValue, maxValue sql.NullInt64
	if err := row.Scan(&ns.ID, &ns.Code, &ns.DisplayName, &ns.Description, &ns.NextValue, &minValue, &maxValue, &ns.Status); err != nil {
		return err
	}
	if minValue.Valid {
		ns.MinValue = &minValue.Int64
	}
	if maxValue.Valid {
		ns.MaxValue = &maxValue.Int64
	}

	var maxUsed sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(value) FROM type_entries WHERE namespace_id = ?`, namespaceID).Scan(&maxUsed); err != nil {
		return err
	}
	start := int64(1)
	if ns.MinValue != nil {
		start = *ns.MinValue
	}
	if maxUsed.Valid && maxUsed.Int64 >= start {
		start = maxUsed.Int64 + 1
	}
	candidate, err := nextGlobalCandidate(ctx, tx, ns, start)
	if err != nil && !errors.Is(err, ErrNamespaceExhausted) {
		return err
	}
	if errors.Is(err, ErrNamespaceExhausted) {
		candidate = start
	}
	if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET next_value = ? WHERE id = ?`, candidate, namespaceID); err != nil {
		return err
	}
	return tx.Commit()
}
