-- Per-recipient delivery category preferences (三期增补 B16b).
-- Closing a category stops external (official-account) delivery only; the
-- in-app inbox keeps every notice. Missing table or missing row means "all
-- categories enabled", so applying this migration changes nothing by itself.
CREATE TABLE IF NOT EXISTS notification_preferences (
  user_id BIGINT NOT NULL PRIMARY KEY,
  browse_delivery TINYINT NOT NULL DEFAULT 1,
  project_delivery TINYINT NOT NULL DEFAULT 1,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
