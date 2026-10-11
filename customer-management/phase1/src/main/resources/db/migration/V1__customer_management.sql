CREATE TABLE iyque_customer_info (
 eId VARCHAR(255) PRIMARY KEY, customerName VARCHAR(255), avatar VARCHAR(255), type INTEGER,
 externalUserid VARCHAR(255), addWay VARCHAR(255), userId VARCHAR(255), tagIds VARCHAR(255),
 state VARCHAR(255), addTime DATETIME(6), status INTEGER
);
CREATE INDEX idx_relation_customer ON iyque_customer_info(externalUserid);
CREATE INDEX idx_relation_employee ON iyque_customer_info(userId);
CREATE TABLE crm_customer (
 id VARCHAR(128) PRIMARY KEY, name VARCHAR(200), avatar VARCHAR(1024), ownerId VARCHAR(128),
 stage VARCHAR(24), category VARCHAR(24), intent VARCHAR(12), phone VARCHAR(80), demand VARCHAR(2000),
 createdAt DATETIME(6), updatedAt DATETIME(6), lastFollowupAt DATETIME(6), nextFollowupAt DATETIME(6), version BIGINT
);
CREATE INDEX idx_customer_owner_due ON crm_customer(ownerId,nextFollowupAt);
CREATE TABLE crm_followup (
 id BIGINT AUTO_INCREMENT PRIMARY KEY, customerId VARCHAR(128) NOT NULL, operatorId VARCHAR(64) NOT NULL,
 content VARCHAR(2000) NOT NULL, stage VARCHAR(24), kind VARCHAR(24), nextFollowupAt DATETIME(6), createdAt DATETIME(6),
 FOREIGN KEY(customerId) REFERENCES crm_customer(id)
);
CREATE INDEX idx_followup_customer ON crm_followup(customerId,createdAt);
CREATE TABLE crm_employee (id VARCHAR(128) PRIMARY KEY, name VARCHAR(200), active BOOLEAN NOT NULL);
CREATE TABLE crm_account (username VARCHAR(64) PRIMARY KEY, employeeId VARCHAR(128), displayName VARCHAR(100),
 passwordHash VARCHAR(100), role VARCHAR(12), enabled BOOLEAN NOT NULL);
CREATE TABLE crm_session (id VARCHAR(64) PRIMARY KEY, username VARCHAR(64), expiresAt DATETIME(6));
CREATE TABLE crm_event (id VARCHAR(64) PRIMARY KEY, customerId VARCHAR(128), employeeId VARCHAR(128),
 changeType VARCHAR(64), eventTime BIGINT NOT NULL, sourceState VARCHAR(255), status VARCHAR(16), attempts INTEGER NOT NULL,
 error VARCHAR(200), nextAttemptAt DATETIME(6), createdAt DATETIME(6));
CREATE INDEX idx_event_pending ON crm_event(status,nextAttemptAt);
CREATE TABLE crm_setting (id VARCHAR(64) PRIMARY KEY, settingValue VARCHAR(255));
CREATE TABLE crm_sync_run (id BIGINT AUTO_INCREMENT PRIMARY KEY, status VARCHAR(16), processed INTEGER NOT NULL,
 error VARCHAR(200), startedAt DATETIME(6), finishedAt DATETIME(6));

