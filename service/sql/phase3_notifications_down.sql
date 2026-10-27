-- 仅用于空新表或经备份的离线结构清理。生产回退应关闭开关并保留投递/绑定证据。
-- 此脚本删除新表数据；真实投递发生后不得通过 down/up 自动重建并重放历史通知。
-- Rollback removes only Task 6 notification projections. Immutable tracking
-- notice_event rows remain; never automatically replay deliveries after down/up.
DROP TABLE IF EXISTS mini_notification_provider_receipts;
DROP TABLE IF EXISTS mini_notification_deliveries;
DROP TABLE IF EXISTS mini_notifications;
DROP TABLE IF EXISTS mini_notification_event_jobs;
