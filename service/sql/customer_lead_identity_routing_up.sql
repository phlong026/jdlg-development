-- 线索身份快照与销售/合作分流。可重复执行，不改负责人、状态或 updated_at。
SET @lead_identity_column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'customer_leads' AND COLUMN_NAME = 'identity_type'
);
SET @lead_identity_column_ddl := IF(
  @lead_identity_column_exists = 0,
  "ALTER TABLE customer_leads ADD COLUMN identity_type VARCHAR(32) NOT NULL DEFAULT '' AFTER remark",
  'SELECT 1'
);
PREPARE lead_identity_column_stmt FROM @lead_identity_column_ddl;
EXECUTE lead_identity_column_stmt;
DEALLOCATE PREPARE lead_identity_column_stmt;

SET @lead_identity_index_exists := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'customer_leads' AND INDEX_NAME = 'idx_identity_type'
);
SET @lead_identity_index_ddl := IF(
  @lead_identity_index_exists = 0,
  'ALTER TABLE customer_leads ADD INDEX idx_identity_type (identity_type)',
  'SELECT 1'
);
PREPARE lead_identity_index_stmt FROM @lead_identity_index_ddl;
EXECUTE lead_identity_index_stmt;
DEALLOCATE PREPARE lead_identity_index_stmt;

UPDATE customer_leads l
JOIN user_profiles p ON p.user_id = l.user_id
SET l.identity_type = TRIM(p.identity_type), l.updated_at = l.updated_at
WHERE l.identity_type = '' AND l.user_id > 0
  AND TRIM(COALESCE(p.identity_type, '')) IN ('乡墅业主', '设计师', '施工队', '建材商', '机构用户');
