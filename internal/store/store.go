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
	return result, rows.Err()
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
	return ns, nil
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

func (s *MySQL) SearchEntries(ctx context.Context, params model.SearchParams) ([]model.TypeEntry, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
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
		where = append(where, `(e.symbol LIKE ? OR e.description LIKE ? OR e.project LIKE ? OR e.requirement_ref LIKE ? OR CAST(e.value AS CHAR) = ?)`)
		args = append(args, like, like, like, like, params.Query)
	}
	args = append(args, limit)

	query := `SELECT e.id, e.namespace_id, n.code, e.value, COALESCE(e.symbol, ''), e.project, e.description,
	                 e.requirement_ref, e.requester, e.source, e.source_ref, e.status, e.created_at, e.updated_at
	          FROM type_entries e JOIN type_namespaces n ON n.id = e.namespace_id
	          WHERE ` + strings.Join(where, " AND ") + `
	          ORDER BY e.updated_at DESC, e.id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer rows.Close()

	var result []model.TypeEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
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
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("begin allocation tx: %w", err)
	}
	defer tx.Rollback()

	ns, err := lockNamespace(ctx, tx, req.Namespace)
	if err != nil {
		return model.TypeEntry{}, err
	}

	candidate, fromReserved, err := chooseCandidate(ctx, tx, ns, req.Project)
	if err != nil {
		return model.TypeEntry{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO type_entries(namespace_id, value, symbol, project, description, requirement_ref, requester, source, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'registry', 'ACTIVE')`,
		ns.ID, candidate, req.Symbol, req.Project, req.Description, req.Requirement, req.Requester)
	if err != nil {
		if isDuplicateKey(err) {
			return model.TypeEntry{}, fmt.Errorf("%w: value or symbol already registered", ErrConflict)
		}
		return model.TypeEntry{}, fmt.Errorf("insert allocated type: %w", err)
	}
	entryID, err := result.LastInsertId()
	if err != nil {
		return model.TypeEntry{}, fmt.Errorf("read allocated id: %w", err)
	}

	if !fromReserved || candidate >= ns.NextValue {
		next, err := nextGlobalCandidate(ctx, tx, ns, candidate+1)
		if err != nil && !errors.Is(err, ErrNamespaceExhausted) {
			return model.TypeEntry{}, err
		}
		if errors.Is(err, ErrNamespaceExhausted) {
			next = candidate + 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE type_namespaces SET next_value = ? WHERE id = ?`, next, ns.ID); err != nil {
			return model.TypeEntry{}, fmt.Errorf("update namespace cursor: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs(action, namespace_code, entry_value, actor, detail)
		VALUES ('ALLOCATE', ?, ?, ?, JSON_OBJECT('project', ?, 'symbol', ?, 'description', ?))`,
		req.Namespace, candidate, req.Requester, req.Project, req.Symbol, req.Description); err != nil {
		return model.TypeEntry{}, fmt.Errorf("write audit log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.TypeEntry{}, fmt.Errorf("commit allocation: %w", err)
	}
	return model.TypeEntry{
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
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}, nil
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
