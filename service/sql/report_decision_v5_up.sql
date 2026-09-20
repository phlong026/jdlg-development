CREATE TABLE IF NOT EXISTS report_decision_contexts (
  report_id VARCHAR(64) NOT NULL,
  confirmation_json JSON NULL,
  confirmation_hash CHAR(64) NULL,
  stage VARCHAR(64) NOT NULL DEFAULT 'preview_pending',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (report_id),
  CONSTRAINT fk_report_decision_context_report
    FOREIGN KEY (report_id) REFERENCES building_reports(report_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='决策型报告的确认快照与异步阶段';
