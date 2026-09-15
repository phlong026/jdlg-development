-- Apply once to the existing Go platform database after customer_management_bridge_up.sql.
-- Identity is separate from CRM business ownership. Do not backfill by display name.
CREATE TABLE management_wecom_identity (
 user_id BIGINT PRIMARY KEY,
 corp_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 employee_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 employee_name VARCHAR(200) NOT NULL DEFAULT '',
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 version BIGINT NOT NULL DEFAULT 1,
 updated_by BIGINT NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY uq_management_wecom_member(corp_id,employee_id)
);
CREATE TABLE management_wecom_oauth_state (
 state_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 browser_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 corp_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 agent_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 INDEX idx_management_wecom_state_expiry(expires_at)
);
