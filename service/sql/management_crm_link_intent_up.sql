-- Apply once to the existing Go platform database after customer_management_bridge_up.sql.
-- Authorization compensation (B1): the prepare transaction persists the pending replay intent and
-- the final transaction clears it only after remote provisioning is confirmed successful.
-- A null intent means no replay is pending; attempts counts automatic replays (capped at 5).
ALTER TABLE management_crm_link
 ADD COLUMN intent TEXT NULL COMMENT '待执行授权意图（JSON），终局成功后清空',
 ADD COLUMN attempts INT NOT NULL DEFAULT 0 COMMENT '自动重放已尝试次数';
