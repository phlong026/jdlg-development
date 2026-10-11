-- 客户管理来源同步检查点表（一次性执行；对应实现 route/crm_sources.go）
-- 新库直接执行即可；若曾用 2026-09-15 之前版本自动建过此表，
-- 需补执行最后一行 ALTER（列不存在时才执行）。
CREATE TABLE IF NOT EXISTS customer_lead_crm_delivery (
 lead_id BIGINT UNSIGNED PRIMARY KEY,
 customer_id VARCHAR(128) NOT NULL DEFAULT '',
 source_hash VARCHAR(64) NOT NULL DEFAULT '',
 attempts INT NOT NULL DEFAULT 0,
 last_error VARCHAR(255) NOT NULL DEFAULT '',
 next_attempt_at DATETIME NULL,
 synced_at DATETIME NULL,
 history_cursor BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 仅升级旧表时需要（新表已包含该列，重复执行会报 Duplicate column）：
-- ALTER TABLE customer_lead_crm_delivery ADD COLUMN history_cursor BIGINT NOT NULL DEFAULT 0;
