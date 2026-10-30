-- 仅用于空新表或经备份的离线结构清理。生产回退应关闭开关并保留投递/绑定证据。
-- 此脚本删除新表数据；真实投递发生后不得通过 down/up 自动重建并重放历史通知。
DROP TABLE IF EXISTS official_account_callback_events;
DROP TABLE IF EXISTS official_account_bindings;
DROP TABLE IF EXISTS official_account_binding_intents;
