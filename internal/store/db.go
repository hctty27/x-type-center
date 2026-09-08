package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
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

//go:embed migrations/001_init.sql
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
	content, err := migrationFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		return fmt.Errorf("read migration 001_init.sql: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("apply migration 001_init.sql: %w", err)
	}
	if err := s.upgradeLegacySchema(ctx); err != nil {
		return fmt.Errorf("upgrade legacy schema: %w", err)
	}
	return nil
}

func (s *MySQL) upgradeLegacySchema(ctx context.Context) error {
	for _, column := range []struct {
		table string
		name  string
		ddl   string
	}{
		{"type_namespaces", "aliases", "ALTER TABLE type_namespaces ADD COLUMN aliases JSON NULL AFTER description"},
		{"type_namespaces", "reserved_ranges", "ALTER TABLE type_namespaces ADD COLUMN reserved_ranges JSON NULL AFTER aliases"},
		{"type_entries", "allocation_id", "ALTER TABLE type_entries ADD COLUMN allocation_id VARCHAR(64) NOT NULL DEFAULT '' AFTER requester"},
		{"type_entries", "revoked_by", "ALTER TABLE type_entries ADD COLUMN revoked_by VARCHAR(128) NOT NULL DEFAULT '' AFTER status"},
		{"type_entries", "revoke_reason", "ALTER TABLE type_entries ADD COLUMN revoke_reason VARCHAR(500) NOT NULL DEFAULT '' AFTER revoked_by"},
		{"type_entries", "revoked_at", "ALTER TABLE type_entries ADD COLUMN revoked_at TIMESTAMP(6) NULL AFTER revoke_reason"},
		{"type_entries", "request_ip", "ALTER TABLE type_entries ADD COLUMN request_ip VARCHAR(45) NOT NULL DEFAULT '' AFTER requester"},
		{"type_entries", "revoke_ip", "ALTER TABLE type_entries ADD COLUMN revoke_ip VARCHAR(45) NOT NULL DEFAULT '' AFTER revoked_by"},
	} {
		if err := s.ensureColumn(ctx, column.table, column.name, column.ddl); err != nil {
			return err
		}
	}

	auditExists, err := s.tableExists(ctx, "audit_logs")
	if err != nil {
		return err
	}
	if auditExists {
		if err := s.ensureColumn(ctx, "audit_logs", "client_ip", "ALTER TABLE audit_logs ADD COLUMN client_ip VARCHAR(45) NOT NULL DEFAULT '' AFTER actor"); err != nil {
			return err
		}
	}

	if err := s.ensureIndex(ctx, "type_entries", "idx_entries_allocation", "CREATE INDEX idx_entries_allocation ON type_entries(allocation_id)"); err != nil {
		return err
	}
	if err := s.ensureIndex(ctx, "type_entries", "idx_entries_namespace_status", "CREATE INDEX idx_entries_namespace_status ON type_entries(namespace_id, status)"); err != nil {
		return err
	}

	if err := s.migrateLegacyAliases(ctx); err != nil {
		return err
	}
	if err := s.migrateLegacyReservedRanges(ctx); err != nil {
		return err
	}
	if err := s.migrateLegacyAllocations(ctx); err != nil {
		return err
	}
	if err := s.migrateLegacyRevocations(ctx); err != nil {
		return err
	}
	if err := s.backfillProjects(ctx); err != nil {
		return err
	}
	if err := s.validateLegacyMigration(ctx); err != nil {
		return err
	}
	if err := s.normalizeEntryLifecycle(ctx); err != nil {
		return err
	}
	if err := s.migrateAuditLogIPs(ctx); err != nil {
		return err
	}
	if err := s.recalculateAllNamespaceCursors(ctx); err != nil {
		return err
	}
	if err := s.finalizeEntrySchema(ctx); err != nil {
		return err
	}

	for _, table := range []string{
		"type_allocation_entries",
		"type_entry_revocations",
		"type_allocations",
		"namespace_aliases",
		"reserved_ranges",
		"audit_logs",
	} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			return fmt.Errorf("drop legacy table %s: %w", table, err)
		}
	}
	return nil
}

func (s *MySQL) ensureColumn(ctx context.Context, table, column, ddl string) error {
	exists, err := s.columnExists(ctx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *MySQL) ensureIndex(ctx context.Context, table, index, ddl string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`, table, index).Scan(&count); err != nil {
		return fmt.Errorf("check index %s.%s: %w", table, index, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create index %s.%s: %w", table, index, err)
	}
	return nil
}

func (s *MySQL) indexExists(ctx context.Context, table, index string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`, table, index).Scan(&count); err != nil {
		return false, fmt.Errorf("check index %s.%s: %w", table, index, err)
	}
	return count > 0, nil
}

func (s *MySQL) dropIndexIfExists(ctx context.Context, table, index string) error {
	exists, err := s.indexExists(ctx, table, index)
	if err != nil || !exists {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "DROP INDEX "+index+" ON "+table); err != nil {
		return fmt.Errorf("drop index %s.%s: %w", table, index, err)
	}
	return nil
}

func (s *MySQL) dropColumnIfExists(ctx context.Context, table, column string) error {
	exists, err := s.columnExists(ctx, table, column)
	if err != nil || !exists {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN "+column); err != nil {
		return fmt.Errorf("drop column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *MySQL) normalizeEntryLifecycle(ctx context.Context) error {
	statusExists, err := s.columnExists(ctx, "type_entries", "status")
	if err != nil || !statusExists {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE type_entries
		SET status = 'REVOKED'
		WHERE status = 'ACTIVE' AND revoked_at IS NOT NULL`); err != nil {
		return fmt.Errorf("normalize migrated revoked entries: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE type_entries
		SET revoked_at = created_at
		WHERE status = 'REVOKED' AND revoked_at IS NULL`); err != nil {
		return fmt.Errorf("backfill revoked timestamps: %w", err)
	}
	return nil
}

func (s *MySQL) migrateAuditLogIPs(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "audit_logs")
	if err != nil || !exists {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE type_entries e
		JOIN type_namespaces n ON n.id = e.namespace_id
		JOIN (
			SELECT a.namespace_code, a.entry_value, a.client_ip
			FROM audit_logs a
			JOIN (
				SELECT namespace_code, entry_value, MIN(id) AS id
				FROM audit_logs
				WHERE action = 'ALLOCATE' AND entry_value IS NOT NULL AND client_ip <> ''
				GROUP BY namespace_code, entry_value
			) picked ON picked.id = a.id
		) audit ON audit.namespace_code = n.code AND audit.entry_value = e.value
		SET e.request_ip = audit.client_ip
		WHERE e.request_ip = ''`); err != nil {
		return fmt.Errorf("migrate allocation client ip: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE type_entries e
		JOIN type_namespaces n ON n.id = e.namespace_id
		JOIN (
			SELECT a.namespace_code, a.entry_value, a.client_ip
			FROM audit_logs a
			JOIN (
				SELECT namespace_code, entry_value, MAX(id) AS id
				FROM audit_logs
				WHERE action = 'REVOKE' AND entry_value IS NOT NULL AND client_ip <> ''
				GROUP BY namespace_code, entry_value
			) picked ON picked.id = a.id
		) audit ON audit.namespace_code = n.code AND audit.entry_value = e.value
		SET e.revoke_ip = audit.client_ip
		WHERE e.revoke_ip = '' AND e.status = 'REVOKED'`); err != nil {
		return fmt.Errorf("migrate revoke client ip: %w", err)
	}
	return nil
}

func (s *MySQL) finalizeEntrySchema(ctx context.Context) error {
	if err := s.ensureColumn(ctx, "type_entries", "active_value", `
		ALTER TABLE type_entries
		ADD COLUMN active_value BIGINT
		GENERATED ALWAYS AS (CASE WHEN status = 'REVOKED' THEN NULL ELSE value END) STORED`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "type_entries", "active_symbol", `
		ALTER TABLE type_entries
		ADD COLUMN active_symbol VARCHAR(191)
		GENERATED ALWAYS AS (CASE WHEN status = 'REVOKED' THEN NULL ELSE symbol END) STORED`); err != nil {
		return err
	}
	if err := s.ensureIndex(ctx, "type_entries", "uk_namespace_active_value", "CREATE UNIQUE INDEX uk_namespace_active_value ON type_entries(namespace_id, active_value)"); err != nil {
		return err
	}
	if err := s.ensureIndex(ctx, "type_entries", "uk_namespace_active_symbol", "CREATE UNIQUE INDEX uk_namespace_active_symbol ON type_entries(namespace_id, active_symbol)"); err != nil {
		return err
	}
	for _, index := range []string{"uk_namespace_value", "uk_namespace_symbol"} {
		if err := s.dropIndexIfExists(ctx, "type_entries", index); err != nil {
			return err
		}
	}
	for _, column := range []string{"source", "source_ref", "updated_at"} {
		if err := s.dropColumnIfExists(ctx, "type_entries", column); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQL) recalculateAllNamespaceCursors(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM type_namespaces ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list namespaces for cursor recalculation: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.RecalculateNamespaceCursor(ctx, id); err != nil {
			return fmt.Errorf("recalculate namespace %d cursor: %w", id, err)
		}
	}
	return nil
}

func (s *MySQL) tableExists(ctx context.Context, table string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND TABLE_TYPE = 'BASE TABLE'`, table).Scan(&count); err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return count > 0, nil
}

func (s *MySQL) columnExists(ctx context.Context, table, column string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, table, column).Scan(&count); err != nil {
		return false, fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	return count > 0, nil
}

func (s *MySQL) migrateLegacyAliases(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "namespace_aliases")
	if err != nil || !exists {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, namespace_id, alias, created_at
		FROM namespace_aliases
		ORDER BY namespace_id, id`)
	if err != nil {
		return fmt.Errorf("read legacy namespace aliases: %w", err)
	}
	defer rows.Close()

	byNamespace := make(map[int64][]model.NamespaceAlias)
	for rows.Next() {
		var item model.NamespaceAlias
		if err := rows.Scan(&item.ID, &item.NamespaceID, &item.Alias, &item.CreatedAt); err != nil {
			return err
		}
		byNamespace[item.NamespaceID] = append(byNamespace[item.NamespaceID], item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for namespaceID, aliases := range byNamespace {
		payload, err := json.Marshal(aliases)
		if err != nil {
			return fmt.Errorf("marshal legacy aliases for namespace %d: %w", namespaceID, err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE type_namespaces SET aliases = ? WHERE id = ?`, payload, namespaceID); err != nil {
			return fmt.Errorf("write migrated aliases for namespace %d: %w", namespaceID, err)
		}
	}
	return nil
}

func (s *MySQL) migrateLegacyReservedRanges(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "reserved_ranges")
	if err != nil || !exists {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, namespace_id, start_value, end_value, project, description, created_at
		FROM reserved_ranges
		ORDER BY namespace_id, start_value, end_value, id`)
	if err != nil {
		return fmt.Errorf("read legacy reserved ranges: %w", err)
	}
	defer rows.Close()

	byNamespace := make(map[int64][]model.ReservedRange)
	for rows.Next() {
		var item model.ReservedRange
		if err := rows.Scan(&item.ID, &item.NamespaceID, &item.StartValue, &item.EndValue, &item.Project, &item.Description, &item.CreatedAt); err != nil {
			return err
		}
		byNamespace[item.NamespaceID] = append(byNamespace[item.NamespaceID], item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for namespaceID, ranges := range byNamespace {
		payload, err := json.Marshal(ranges)
		if err != nil {
			return fmt.Errorf("marshal legacy reserved ranges for namespace %d: %w", namespaceID, err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE type_namespaces SET reserved_ranges = ? WHERE id = ?`, payload, namespaceID); err != nil {
			return fmt.Errorf("write migrated reserved ranges for namespace %d: %w", namespaceID, err)
		}
	}
	return nil
}

func (s *MySQL) migrateLegacyAllocations(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "type_allocation_entries")
	if err != nil || !exists {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE type_entries e
		JOIN type_allocation_entries ae ON ae.entry_id = e.id
		SET e.allocation_id = ae.allocation_id`); err != nil {
		return fmt.Errorf("migrate allocation ids: %w", err)
	}
	return nil
}

func (s *MySQL) migrateLegacyRevocations(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "type_entry_revocations")
	if err != nil || !exists {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE type_entries e
		JOIN type_entry_revocations r ON r.entry_id = e.id
		SET e.revoked_by = r.revoked_by,
		    e.revoke_reason = r.reason,
		    e.revoked_at = r.revoked_at`); err != nil {
		return fmt.Errorf("migrate revocations: %w", err)
	}
	return nil
}

func (s *MySQL) backfillProjects(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO projects(name)
		SELECT DISTINCT TRIM(project)
		FROM type_entries
		WHERE TRIM(project) <> ''`); err != nil {
		return fmt.Errorf("backfill projects from entries: %w", err)
	}
	exists, err := s.tableExists(ctx, "reserved_ranges")
	if err != nil {
		return err
	}
	if exists {
		if _, err := s.db.ExecContext(ctx, `
			INSERT IGNORE INTO projects(name)
			SELECT DISTINCT TRIM(project)
			FROM reserved_ranges
			WHERE TRIM(project) <> ''`); err != nil {
			return fmt.Errorf("backfill projects from legacy reserved ranges: %w", err)
		}
	}
	return nil
}

func (s *MySQL) validateLegacyMigration(ctx context.Context) error {
	checks := []struct {
		table string
		newQ  string
		label string
	}{
		{"namespace_aliases", `SELECT COALESCE(SUM(JSON_LENGTH(aliases)), 0) FROM type_namespaces`, "namespace aliases"},
		{"reserved_ranges", `SELECT COALESCE(SUM(JSON_LENGTH(reserved_ranges)), 0) FROM type_namespaces`, "reserved ranges"},
		{"type_allocation_entries", `SELECT COUNT(*) FROM type_entries WHERE allocation_id <> ''`, "allocation entries"},
		{"type_entry_revocations", `SELECT COUNT(*) FROM type_entries WHERE revoked_at IS NOT NULL`, "entry revocations"},
	}
	for _, check := range checks {
		exists, err := s.tableExists(ctx, check.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		var oldCount, newCount int64
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+check.table).Scan(&oldCount); err != nil {
			return fmt.Errorf("count legacy %s: %w", check.label, err)
		}
		if err := s.db.QueryRowContext(ctx, check.newQ).Scan(&newCount); err != nil {
			return fmt.Errorf("count migrated %s: %w", check.label, err)
		}
		if oldCount != newCount {
			return fmt.Errorf("%s migration mismatch: old=%d new=%d", check.label, oldCount, newCount)
		}
	}

	exists, err := s.tableExists(ctx, "type_allocations")
	if err != nil {
		return err
	}
	if exists {
		linksExist, err := s.tableExists(ctx, "type_allocation_entries")
		if err != nil {
			return err
		}
		var orphaned int64
		if linksExist {
			if err := s.db.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM type_allocations a
				LEFT JOIN type_allocation_entries ae ON ae.allocation_id = a.allocation_id
				WHERE ae.allocation_id IS NULL`).Scan(&orphaned); err != nil {
				return fmt.Errorf("check orphaned legacy allocations: %w", err)
			}
		} else if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM type_allocations`).Scan(&orphaned); err != nil {
			return fmt.Errorf("count legacy allocations: %w", err)
		}
		if orphaned > 0 {
			return fmt.Errorf("refusing to drop type_allocations: %d orphaned allocation rows exist", orphaned)
		}
	}
	return nil
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
