CREATE TABLE IF NOT EXISTS type_namespaces (
    id BIGINT NOT NULL AUTO_INCREMENT,
    code VARCHAR(128) NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    description VARCHAR(1000) NOT NULL DEFAULT '',
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
    source VARCHAR(64) NOT NULL DEFAULT 'registry',
    source_ref VARCHAR(1000) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_namespace_value (namespace_id, value),
    UNIQUE KEY uk_namespace_symbol (namespace_id, symbol),
    KEY idx_entries_project (project),
    KEY idx_entries_description (description(191)),
    CONSTRAINT fk_entries_namespace FOREIGN KEY (namespace_id) REFERENCES type_namespaces(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS reserved_ranges (
    id BIGINT NOT NULL AUTO_INCREMENT,
    namespace_id BIGINT NOT NULL,
    start_value BIGINT NOT NULL,
    end_value BIGINT NOT NULL,
    project VARCHAR(128) NOT NULL DEFAULT '',
    description VARCHAR(1000) NOT NULL DEFAULT '',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_reserved_exact (namespace_id, start_value, end_value, project),
    KEY idx_reserved_lookup (namespace_id, start_value, end_value),
    CONSTRAINT chk_reserved_bounds CHECK (start_value <= end_value),
    CONSTRAINT fk_reserved_namespace FOREIGN KEY (namespace_id) REFERENCES type_namespaces(id)
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
