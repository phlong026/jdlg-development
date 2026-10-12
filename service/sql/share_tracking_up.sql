-- Additive only. No implicit user grants and no production collection enablement.
CREATE TABLE IF NOT EXISTS tracking_tenant_lock (
 tenant_id VARCHAR(64) NOT NULL PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS tracking_access (
 user_id BIGINT NOT NULL,
 tenant_id VARCHAR(64) NOT NULL,
 owner_id VARCHAR(64) NOT NULL,
 can_view BOOLEAN NOT NULL DEFAULT FALSE,
 can_manage BOOLEAN NOT NULL DEFAULT FALSE,
 can_crm BOOLEAN NOT NULL DEFAULT FALSE,
 can_delete BOOLEAN NOT NULL DEFAULT FALSE,
 can_rules BOOLEAN NOT NULL DEFAULT FALSE,
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 updated_by BIGINT NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY(user_id,tenant_id,owner_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS tracking_records (
 tenant_id VARCHAR(64) NOT NULL,
 kind VARCHAR(24) NOT NULL,
 record_id VARCHAR(128) NOT NULL,
 owner_id VARCHAR(64) NOT NULL DEFAULT '',
 visitor_key VARCHAR(128) NOT NULL DEFAULT '',
 session_id VARCHAR(128) NOT NULL DEFAULT '',
 content_id VARCHAR(128) NOT NULL DEFAULT '',
 share_id VARCHAR(128) NOT NULL DEFAULT '',
 occurred_at DATETIME(6) NOT NULL,
 payload JSON NOT NULL,
 PRIMARY KEY(tenant_id,kind,record_id),
 INDEX tracking_scope_time(tenant_id,kind,owner_id,occurred_at),
 INDEX tracking_visitor_time(tenant_id,visitor_key,occurred_at),
 INDEX tracking_content_time(tenant_id,content_id,occurred_at),
 INDEX tracking_share_time(tenant_id,share_id,occurred_at),
 INDEX tracking_session(tenant_id,session_id,kind),
 INDEX tracking_session_time(tenant_id,kind,session_id,occurred_at,record_id),
 INDEX tracking_scope_content_time(tenant_id,kind,owner_id,content_id,occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- Immutable kind=event rows use INSERT only. kind=sequence contains interval fingerprints.
-- kind=deletion is retained independently to reject replay after personal data deletion.
