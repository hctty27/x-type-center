CREATE TABLE IF NOT EXISTS namespace_aliases (
    id BIGINT NOT NULL AUTO_INCREMENT,
    namespace_id BIGINT NOT NULL,
    alias VARCHAR(255) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_namespace_alias (alias),
    KEY idx_namespace_alias_namespace (namespace_id),
    CONSTRAINT fk_namespace_alias_namespace FOREIGN KEY (namespace_id) REFERENCES type_namespaces(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
