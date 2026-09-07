CREATE TABLE IF NOT EXISTS type_allocations (
    allocation_id VARCHAR(64) NOT NULL,
    namespace_id BIGINT NOT NULL,
    project VARCHAR(128) NOT NULL DEFAULT '',
    requester VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (allocation_id),
    KEY idx_allocations_namespace_created (namespace_id, created_at),
    CONSTRAINT fk_allocations_namespace FOREIGN KEY (namespace_id) REFERENCES type_namespaces(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS type_allocation_entries (
    allocation_id VARCHAR(64) NOT NULL,
    entry_id BIGINT NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (allocation_id, entry_id),
    UNIQUE KEY uk_allocation_entry (entry_id),
    CONSTRAINT fk_allocation_entries_allocation FOREIGN KEY (allocation_id) REFERENCES type_allocations(allocation_id),
    CONSTRAINT fk_allocation_entries_entry FOREIGN KEY (entry_id) REFERENCES type_entries(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS type_entry_revocations (
    entry_id BIGINT NOT NULL,
    revoked_by VARCHAR(128) NOT NULL DEFAULT '',
    reason VARCHAR(500) NOT NULL,
    revoked_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (entry_id),
    CONSTRAINT fk_entry_revocations_entry FOREIGN KEY (entry_id) REFERENCES type_entries(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
