-- 应用版本回退时无需回退此结构：旧版代码会忽略额外列和表。
-- 故意保留授权审计记录和手机来源，避免 schema rollback 导致不可恢复的数据丢失。
-- 如需物理删除，必须先单独导出 business_contact_consents 与两个来源列。
SELECT 'business contact consent schema retained for audit-safe rollback' AS migration_note;
