ALTER TABLE crm_account ADD COLUMN authMode VARCHAR(16) NOT NULL DEFAULT 'standalone';
ALTER TABLE crm_account ADD COLUMN managementUserId BIGINT;
ALTER TABLE crm_account ADD COLUMN managedRevision BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX idx_account_management_user ON crm_account(managementUserId);
ALTER TABLE crm_customer ADD COLUMN addedTimeSource VARCHAR(16) NOT NULL DEFAULT 'unknown';
CREATE INDEX idx_customer_added ON crm_customer(addedTimeSource,createdAt);
CREATE TABLE crm_bridge_nonce (id VARCHAR(64) PRIMARY KEY, expiresAt DATETIME(6) NOT NULL);
CREATE INDEX idx_bridge_expiry ON crm_bridge_nonce(expiresAt);
CREATE TABLE crm_operation_audit (
 id BIGINT AUTO_INCREMENT PRIMARY KEY, managementUserId BIGINT NOT NULL, crmUsername VARCHAR(64) NOT NULL,
 action VARCHAR(12) NOT NULL, resource VARCHAR(512) NOT NULL, requestId VARCHAR(128) NOT NULL,
 result INTEGER NOT NULL, createdAt DATETIME(6) NOT NULL
);
