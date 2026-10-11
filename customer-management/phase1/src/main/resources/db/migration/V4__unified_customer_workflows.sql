ALTER TABLE crm_customer ADD COLUMN lifecycle VARCHAR(24) DEFAULT 'legacy';
ALTER TABLE crm_customer ADD COLUMN mergedInto VARCHAR(128);
ALTER TABLE crm_customer ADD COLUMN protectionStartedAt DATETIME(6);
ALTER TABLE crm_customer ADD COLUMN historicalReviewed BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE crm_customer SET lifecycle=CASE WHEN poolAt IS NOT NULL THEN 'pool' ELSE 'protected' END,protectionStartedAt=COALESCE(lastFollowupAt,createdAt),historicalReviewed=TRUE WHERE customerNumber IS NOT NULL;
ALTER TABLE crm_followup ADD COLUMN outcome VARCHAR(24) DEFAULT 'contacted';
ALTER TABLE crm_followup ADD COLUMN effective BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE crm_followup ADD COLUMN reviewReason VARCHAR(2000) DEFAULT '';
ALTER TABLE crm_followup ADD COLUMN attachmentIds VARCHAR(2000) DEFAULT '';
UPDATE crm_followup SET effective=FALSE WHERE kind NOT IN ('followup','communication','drawing','quotation');
CREATE TABLE crm_source(id VARCHAR(160) PRIMARY KEY,customerId VARCHAR(128) NOT NULL,originCustomerId VARCHAR(128) NOT NULL,kind VARCHAR(24),externalId VARCHAR(128),name VARCHAR(200),phone VARCHAR(80),wechat VARCHAR(80),summary VARCHAR(2000),createdAt DATETIME(6));
CREATE INDEX idx_source_customer ON crm_source(customerId);
CREATE TABLE crm_attachment(id VARCHAR(36) PRIMARY KEY,customerId VARCHAR(128),name VARCHAR(200),mime VARCHAR(80),operatorId VARCHAR(64),size BIGINT NOT NULL,data LONGBLOB,createdAt DATETIME(6));
CREATE INDEX idx_attachment_customer ON crm_attachment(customerId);
CREATE TABLE crm_case(id BIGINT AUTO_INCREMENT PRIMARY KEY,customerId VARCHAR(128),kind VARCHAR(24),status VARCHAR(24),applicant VARCHAR(64),content VARCHAR(2000),attachmentIds VARCHAR(2000),reviewer VARCHAR(64),decision VARCHAR(2000),createdAt DATETIME(6),resolvedAt DATETIME(6));
CREATE INDEX idx_case_pending ON crm_case(status,createdAt);
CREATE TABLE crm_notice(id VARCHAR(64) PRIMARY KEY,username VARCHAR(64),customerId VARCHAR(128),content VARCHAR(500),seen BOOLEAN NOT NULL DEFAULT FALSE,createdAt DATETIME(6));
CREATE INDEX idx_notice_account ON crm_notice(username,createdAt);
CREATE INDEX idx_customer_lifecycle ON crm_customer(lifecycle);

ALTER TABLE crm_case ADD COLUMN requestData VARCHAR(8000);

ALTER TABLE crm_notice ADD COLUMN pushState VARCHAR(16) DEFAULT 'pending';
ALTER TABLE crm_notice ADD COLUMN pushError VARCHAR(255) DEFAULT '';
CREATE INDEX idx_notice_push ON crm_notice(pushState,createdAt);

ALTER TABLE crm_source ADD COLUMN submittedBy VARCHAR(64) DEFAULT '';
ALTER TABLE crm_source ADD COLUMN channel VARCHAR(64) DEFAULT '';
