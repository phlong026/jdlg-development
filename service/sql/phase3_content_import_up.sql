-- Phase 3 content import job state. Applying this migration only creates
-- durable storage; it does not enable import routes, workers, or providers.
CREATE TABLE IF NOT EXISTS content_import_jobs (
  tenant_id VARCHAR(64) NOT NULL,
  import_id VARCHAR(64) NOT NULL,
  actor_id BIGINT NOT NULL,
  owner_id VARCHAR(128) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  semantics_hash CHAR(64) NOT NULL,
  source_url VARCHAR(2048) NOT NULL,
  platform VARCHAR(32) NOT NULL,
  rights VARCHAR(16) NOT NULL,
  target_mp_appid VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL,
  content_id VARCHAR(128) NOT NULL DEFAULT '',
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  request_sent BOOLEAN NOT NULL DEFAULT FALSE,
  resolution_json MEDIUMBLOB NULL,
  lease_token VARCHAR(64) NULL,
  lease_until DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (tenant_id, import_id),
  UNIQUE KEY content_import_idempotency (tenant_id, actor_id, idempotency_key),
  INDEX content_import_actor_list (tenant_id, actor_id, owner_id, created_at, import_id),
  INDEX content_import_actor_pending (tenant_id, actor_id, status),
  INDEX content_import_claim (tenant_id, status, lease_until, created_at, import_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
