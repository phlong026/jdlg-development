ALTER TABLE crm_followup ADD COLUMN externalKey VARCHAR(160);
CREATE UNIQUE INDEX idx_followup_external_key ON crm_followup(externalKey);
