-- Apply before CRM_BRIDGE_URL is enabled. Keep existing platform access explicit.
CREATE TABLE management_access (
 user_id BIGINT PRIMARY KEY, scope VARCHAR(24) NOT NULL DEFAULT 'crm_only',
 enabled BOOLEAN NOT NULL DEFAULT FALSE, version BIGINT NOT NULL DEFAULT 0,
 updated_by BIGINT NOT NULL, updated_at DATETIME(6) NOT NULL
);
CREATE TABLE management_crm_link (
 user_id BIGINT PRIMARY KEY, crm_username VARCHAR(64) NOT NULL UNIQUE,
 state VARCHAR(16) NOT NULL DEFAULT 'pending', enabled BOOLEAN NOT NULL DEFAULT FALSE,
 version BIGINT NOT NULL DEFAULT 0, updated_by BIGINT NOT NULL, updated_at DATETIME(6) NOT NULL
);
CREATE TABLE management_access_audit (
 id BIGINT AUTO_INCREMENT PRIMARY KEY, operator_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
 action VARCHAR(32) NOT NULL, before_value TEXT, after_value TEXT,
 result VARCHAR(32) NOT NULL, request_id VARCHAR(64) NOT NULL, created_at DATETIME(6) NOT NULL
);
-- After reviewing actual management user IDs, explicitly grant initial platform access:
-- INSERT INTO management_access(user_id,scope,enabled,version,updated_by,updated_at)
-- SELECT id,'platform_admin',TRUE,0,id,NOW(6) FROM users WHERE user_type='management' AND id IN (<reviewed IDs>);
-- Never infer platform administrators by display name. Unlisted users are denied.
