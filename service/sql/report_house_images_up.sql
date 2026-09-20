-- Apply after report_module_up.sql and report_decision_v5_up.sql.
-- Each image is reserved once before contacting a paid provider. Never reset
-- submitting automatically: it requires provider reconciliation.
CREATE TABLE IF NOT EXISTS report_house_image_storage (
  singleton TINYINT UNSIGNED NOT NULL,
  storage_id CHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (singleton)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='整屋报告私有图片共享存储唯一身份';

CREATE TABLE IF NOT EXISTS report_house_images (
  report_id VARCHAR(64) NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  image_id VARCHAR(80) NOT NULL,
  status VARCHAR(16) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  record_json JSON NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (report_id, image_id),
  KEY idx_house_image_owner (user_id, report_id),
  KEY idx_house_image_state (status, updated_at),
  CONSTRAINT fk_house_image_report FOREIGN KEY (report_id) REFERENCES building_reports(report_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='整屋报告图片提交收据与私有资产索引';
