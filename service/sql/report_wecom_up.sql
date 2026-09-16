-- Run explicitly before REPORT_WECOM_ENABLED. No credentials or user data are seeded.
CREATE TABLE IF NOT EXISTS report_wecom_tickets (
 report_id VARCHAR(64) NOT NULL,
 user_id BIGINT NOT NULL,
 state VARCHAR(30) NOT NULL,
 unionid VARCHAR(128) NOT NULL,
 contact_way_id VARCHAR(128) NOT NULL DEFAULT '',
 qr_url VARCHAR(1024) NOT NULL DEFAULT '',
 external_user_id VARCHAR(128) NOT NULL DEFAULT '',
 welcome_state VARCHAR(32) NOT NULL DEFAULT 'pending',
 expires_at DATETIME(6) NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (report_id,user_id),
 UNIQUE KEY uq_report_wecom_state(state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
