-- Scheduled publish reminders (B15). The server checks for due reminders and
-- sends a template message via the official account when available. Without
-- the official account configured, reminders sit in the table (no-op).
CREATE TABLE IF NOT EXISTS publish_reminders (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL,
  actor_id BIGINT NOT NULL,
  content_id VARCHAR(64) NOT NULL,
  content_title VARCHAR(200) NOT NULL DEFAULT '',
  remind_at DATETIME(6) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  sent_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  INDEX idx_publish_reminders_due (tenant_id, status, remind_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
