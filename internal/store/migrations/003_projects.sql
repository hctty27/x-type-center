CREATE TABLE IF NOT EXISTS projects (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_project_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO projects(name)
SELECT DISTINCT TRIM(project)
FROM type_entries
WHERE TRIM(project) <> '';

INSERT IGNORE INTO projects(name)
SELECT DISTINCT TRIM(project)
FROM reserved_ranges
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
