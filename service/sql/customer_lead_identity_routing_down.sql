-- 回退分流 schema；用户资料中的 identity_type 仍保留。
SET @lead_identity_index_exists := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'customer_leads' AND INDEX_NAME = 'idx_identity_type'
);
SET @lead_identity_index_ddl := IF(@lead_identity_index_exists > 0, 'ALTER TABLE customer_leads DROP INDEX idx_identity_type', 'SELECT 1');
PREPARE lead_identity_index_stmt FROM @lead_identity_index_ddl;
EXECUTE lead_identity_index_stmt;
DEALLOCATE PREPARE lead_identity_index_stmt;

SET @lead_identity_column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'customer_leads' AND COLUMN_NAME = 'identity_type'
);
SET @lead_identity_column_ddl := IF(@lead_identity_column_exists > 0, 'ALTER TABLE customer_leads DROP COLUMN identity_type', 'SELECT 1');
PREPARE lead_identity_column_stmt FROM @lead_identity_column_ddl;
EXECUTE lead_identity_column_stmt;
DEALLOCATE PREPARE lead_identity_column_stmt;
