CREATE TABLE IF NOT EXISTS type_namespaces (
    id BIGINT NOT NULL AUTO_INCREMENT,
    code VARCHAR(128) NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    description VARCHAR(1000) NOT NULL DEFAULT '',
    aliases JSON NULL,
    reserved_ranges JSON NULL,
    next_value BIGINT NOT NULL DEFAULT 1,
    min_value BIGINT NULL,
    max_value BIGINT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_namespace_code (code),
    CONSTRAINT chk_namespace_bounds CHECK (min_value IS NULL OR max_value IS NULL OR min_value <= max_value)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS type_entries (
    id BIGINT NOT NULL AUTO_INCREMENT,
    namespace_id BIGINT NOT NULL,
    value BIGINT NOT NULL,
    symbol VARCHAR(191) NULL,
    project VARCHAR(128) NOT NULL DEFAULT '',
    description VARCHAR(1000) NOT NULL DEFAULT '',
    requirement_ref VARCHAR(255) NOT NULL DEFAULT '',
    requester VARCHAR(128) NOT NULL DEFAULT '',
    allocation_id VARCHAR(64) NOT NULL DEFAULT '',
    source VARCHAR(64) NOT NULL DEFAULT 'registry',
    source_ref VARCHAR(1000) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    revoked_by VARCHAR(128) NOT NULL DEFAULT '',
    revoke_reason VARCHAR(500) NOT NULL DEFAULT '',
    revoked_at TIMESTAMP(6) NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_namespace_value (namespace_id, value),
    UNIQUE KEY uk_namespace_symbol (namespace_id, symbol),
    KEY idx_entries_project (project),
    KEY idx_entries_description (description(191)),
    KEY idx_entries_allocation (allocation_id),
    KEY idx_entries_namespace_status (namespace_id, status),
    CONSTRAINT fk_entries_namespace FOREIGN KEY (namespace_id) REFERENCES type_namespaces(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS projects (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_project_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT NOT NULL AUTO_INCREMENT,
    action VARCHAR(64) NOT NULL,
    namespace_code VARCHAR(128) NOT NULL DEFAULT '',
    entry_value BIGINT NULL,
    actor VARCHAR(128) NOT NULL DEFAULT '',
    client_ip VARCHAR(45) NOT NULL DEFAULT '',
    detail JSON NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_audit_namespace_created (namespace_code, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO projects(name)
SELECT DISTINCT TRIM(project)
FROM type_entries
WHERE TRIM(project) <> '';

INSERT IGNORE INTO projects(name) VALUES
    ('XH2'),
    ('XH2-1'),
    ('7.3.2版本'),
    ('混沌灵域2'),
    ('XJ-1'),
    ('XJ-1 1.3'),
    ('7.4.1'),
    ('XJ-1 1.4'),
    ('7.4.2'),
    ('7.5.2'),
    ('XL1-1'),
    ('X-IAA'),
    ('XH-3-1'),
    ('XL2-1'),
    ('XJ-2'),
    ('XH2-3'),
    ('7.9.1');
