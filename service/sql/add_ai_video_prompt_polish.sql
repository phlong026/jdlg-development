-- Additive schema only. Default polish remains off when this JSON is NULL.
-- Existing installations use the application initializer for repeat-safe setup.
ALTER TABLE ai_video_studio_config
  ADD COLUMN prompt_optimize_json JSON NULL AFTER prices_json;

CREATE TABLE IF NOT EXISTS ai_video_studio_prompt_tasks(
  id CHAR(36) NOT NULL PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  quote_id CHAR(36) NOT NULL,
  draft_id BIGINT UNSIGNED NOT NULL,
  draft_revision BIGINT UNSIGNED NOT NULL,
  input_hash CHAR(64) NOT NULL,
  input_snapshot JSON NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL,
  fee_status VARCHAR(32) NOT NULL,
  total_stones BIGINT UNSIGNED NOT NULL,
  optimized_prompt TEXT NOT NULL,
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  error_message VARCHAR(255) NOT NULL DEFAULT '',
  lease_owner VARCHAR(128) NOT NULL DEFAULT '',
  lease_until DATETIME NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_ai_video_studio_prompt_quote(quote_id),
  UNIQUE KEY uk_ai_video_studio_prompt_idempotency(user_id,idempotency_key),
  KEY idx_ai_video_studio_prompt_status(status,updated_at),
  KEY idx_ai_video_studio_prompt_user(user_id,updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ai_video_studio_prompt_fees(
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  task_id CHAR(36) NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  quote_id CHAR(36) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  amount BIGINT UNSIGNED NOT NULL,
  status VARCHAR(32) NOT NULL,
  reason VARCHAR(191) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_ai_video_studio_prompt_fee(task_id,kind),
  KEY idx_ai_video_studio_prompt_fee_user(user_id,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ai_video_studio_prompt_reconciliations(
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  task_id CHAR(36) NOT NULL,
  action VARCHAR(40) NOT NULL,
  decision_hash CHAR(64) NOT NULL,
  actor_id BIGINT NOT NULL,
  actor_name VARCHAR(191) NOT NULL,
  evidence TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_ai_video_studio_prompt_reconcile(task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
