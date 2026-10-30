-- Durable browse-notice inbox and delivery outbox. Applying this migration does
-- not enable notice generation or any external provider sender.
CREATE TABLE IF NOT EXISTS mini_notification_event_jobs (
  tenant_id VARCHAR(64) NOT NULL,
  event_id VARCHAR(128) NOT NULL,
  status VARCHAR(16) NOT NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  lease_token VARCHAR(64) NULL,
  lease_until DATETIME(6) NULL,
  last_error VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (tenant_id, event_id),
  INDEX mini_notice_event_claim (tenant_id, status, lease_until, attempt_count)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- One-time compatibility backfill for notice_event rows written before the
-- transactional enqueue hook was deployed. Runtime claims never repeat this scan.
INSERT IGNORE INTO mini_notification_event_jobs
  (tenant_id,event_id,status,attempt_count,created_at,updated_at)
SELECT tenant_id,record_id,'pending',0,occurred_at,occurred_at
FROM tracking_records WHERE kind='notice_event';

CREATE TABLE IF NOT EXISTS mini_notifications (
  tenant_id VARCHAR(64) NOT NULL,
  notification_id VARCHAR(128) NOT NULL,
  event_id VARCHAR(128) NOT NULL,
  recipient_id BIGINT NOT NULL,
  kind VARCHAR(32) NOT NULL,
  title VARCHAR(128) NOT NULL,
  body VARCHAR(512) NOT NULL,
  route_key VARCHAR(128) NOT NULL,
  content_id VARCHAR(128) NOT NULL DEFAULT '',
  share_id VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  read_at DATETIME(6) NULL,
  cancelled_at DATETIME(6) NULL,
  PRIMARY KEY (tenant_id, notification_id),
  UNIQUE KEY mini_notice_event_once (tenant_id, event_id),
  INDEX mini_notice_inbox (tenant_id, recipient_id, created_at, notification_id),
  INDEX mini_notice_content (tenant_id, content_id, cancelled_at),
  INDEX mini_notice_share (tenant_id, share_id, cancelled_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS mini_notification_deliveries (
  tenant_id VARCHAR(64) NOT NULL,
  delivery_id VARCHAR(191) NOT NULL,
  notification_id VARCHAR(128) NOT NULL,
  recipient_id BIGINT NOT NULL,
  channel VARCHAR(32) NOT NULL,
  status VARCHAR(16) NOT NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  binding_version BIGINT NOT NULL,
  lease_token VARCHAR(64) NULL,
  lease_until DATETIME(6) NULL,
  next_attempt_at DATETIME(6) NULL,
  provider_msg_id VARCHAR(128) NOT NULL DEFAULT '',
  provider_status VARCHAR(128) NOT NULL DEFAULT '',
  provider_received_at DATETIME(6) NULL,
  last_error VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (tenant_id, delivery_id),
  UNIQUE KEY mini_notice_delivery_once (tenant_id, notification_id, channel),
  INDEX mini_notice_delivery_claim (tenant_id, channel, status, next_attempt_at, attempt_count),
  INDEX mini_notice_delivery_receipt (tenant_id, provider_msg_id),
  CONSTRAINT mini_notice_delivery_notification FOREIGN KEY (tenant_id, notification_id)
    REFERENCES mini_notifications (tenant_id, notification_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS mini_notification_provider_receipts (
  tenant_id VARCHAR(64) NOT NULL,
  provider_msg_id VARCHAR(128) NOT NULL,
  provider_status VARCHAR(128) NOT NULL,
  received_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (tenant_id, provider_msg_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
