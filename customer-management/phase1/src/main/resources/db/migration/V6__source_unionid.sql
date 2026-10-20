-- 微信开放平台 unionid：小程序用户与企微外部联系人同属一个开放平台主体时才有值，仅用于提示主管核实，不用于自动合并。
ALTER TABLE crm_source ADD COLUMN unionid VARCHAR(64);
CREATE INDEX idx_source_unionid ON crm_source(unionid);
