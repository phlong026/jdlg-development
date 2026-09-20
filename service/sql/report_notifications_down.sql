-- 仅在明确回滚本通知模块并备份通知记录后执行；不改变报告或支付状态。
DROP TABLE IF EXISTS report_notifications;
