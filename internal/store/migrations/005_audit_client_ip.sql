SET @client_ip_column_exists = (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'audit_logs'
      AND COLUMN_NAME = 'client_ip'
);

SET @client_ip_ddl = IF(
    @client_ip_column_exists = 0,
    'ALTER TABLE audit_logs ADD COLUMN client_ip VARCHAR(45) NOT NULL DEFAULT '''' AFTER actor',
    'SELECT 1'
);

PREPARE client_ip_stmt FROM @client_ip_ddl;
EXECUTE client_ip_stmt;
DEALLOCATE PREPARE client_ip_stmt;
