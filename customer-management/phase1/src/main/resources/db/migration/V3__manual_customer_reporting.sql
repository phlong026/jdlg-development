-- Existing WeCom records retain their original assignment policy.
ALTER TABLE crm_customer ADD COLUMN customerNumber VARCHAR(40);
ALTER TABLE crm_customer ADD COLUMN ownerUsername VARCHAR(64);
ALTER TABLE crm_customer ADD COLUMN wechat VARCHAR(80) DEFAULT '';
ALTER TABLE crm_customer ADD COLUMN address VARCHAR(500) DEFAULT '';
ALTER TABLE crm_customer ADD COLUMN buildingArea DECIMAL(19,2);
ALTER TABLE crm_customer ADD COLUMN buildingType VARCHAR(12) DEFAULT '';
ALTER TABLE crm_customer ADD COLUMN expectedBuildTime VARCHAR(100) DEFAULT '';
ALTER TABLE crm_customer ADD COLUMN remark VARCHAR(2000) DEFAULT '';
ALTER TABLE crm_customer ADD COLUMN phoneKey VARCHAR(32);
ALTER TABLE crm_customer ADD COLUMN protectionExpiresAt DATETIME(6);
ALTER TABLE crm_customer ADD COLUMN poolAt DATETIME(6);
CREATE UNIQUE INDEX uk_customer_number ON crm_customer(customerNumber);
CREATE UNIQUE INDEX uk_customer_phone_key ON crm_customer(phoneKey);
CREATE INDEX idx_customer_protection ON crm_customer(protectionExpiresAt);
CREATE INDEX idx_customer_reporting_owner ON crm_customer(ownerUsername);
INSERT INTO crm_setting(id,settingValue) VALUES ('report-lock','1');
