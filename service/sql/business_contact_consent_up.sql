-- 业务联系授权与客户线索手机来源。
-- 本脚本可在任何顺序重复执行：线上可先执行它，应用启动后 InitTable
-- 再检查一次也不会因重复列失败。旧行标记为 legacy_unknown，不冒充用户授权。

SET @contact_phone_source_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'customer_leads'
    AND COLUMN_NAME = 'phone_source'
);
SET @contact_phone_source_ddl := IF(
  @contact_phone_source_exists = 0,
  "ALTER TABLE customer_leads ADD COLUMN phone_source VARCHAR(32) NOT NULL DEFAULT 'legacy_unknown' AFTER phone",
  'SELECT 1'
);
PREPARE contact_phone_source_stmt FROM @contact_phone_source_ddl;
EXECUTE contact_phone_source_stmt;
DEALLOCATE PREPARE contact_phone_source_stmt;

SET @contact_phone_revision_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'customer_leads'
    AND COLUMN_NAME = 'phone_consent_revision'
);
SET @contact_phone_revision_ddl := IF(
  @contact_phone_revision_exists = 0,
  'ALTER TABLE customer_leads ADD COLUMN phone_consent_revision BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER phone_source',
  'SELECT 1'
);
PREPARE contact_phone_revision_stmt FROM @contact_phone_revision_ddl;
EXECUTE contact_phone_revision_stmt;
DEALLOCATE PREPARE contact_phone_revision_stmt;

CREATE TABLE IF NOT EXISTS business_contact_consents (
  user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  accepted TINYINT(1) NOT NULL DEFAULT 0,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
  purpose VARCHAR(64) NOT NULL DEFAULT 'use_bound_phone_for_business_contact',
  consent_version VARCHAR(32) NOT NULL DEFAULT 'phone_contact_v1',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
