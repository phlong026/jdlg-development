-- Intro presets (B8): personal business-card or tenant brand intros that get
-- injected into AI writing prompts as restricted context. Personal presets are
-- per-user; tenant presets serve all users of the same tenant.
CREATE TABLE IF NOT EXISTS content_intro_presets (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  owner_kind VARCHAR(16) NOT NULL DEFAULT 'personal',
  owner_id VARCHAR(64) NOT NULL,
  name VARCHAR(64) NOT NULL,
  content TEXT NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  enabled TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  INDEX idx_intro_presets_owner (owner_kind, owner_id, enabled, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
