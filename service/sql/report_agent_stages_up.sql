CREATE TABLE IF NOT EXISTS report_agent_stages (
  report_id VARCHAR(64) NOT NULL,
  run_key VARCHAR(128) NOT NULL,
  stage VARCHAR(64) NOT NULL,
  payload_json JSON NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (report_id, run_key, stage),
  CONSTRAINT fk_report_agent_stages_report
    FOREIGN KEY (report_id) REFERENCES building_reports (report_id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报告多Agent阶段不可变检查点';
