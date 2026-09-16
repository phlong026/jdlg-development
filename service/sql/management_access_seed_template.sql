-- 管理员访问授权迁移辅助脚本（评审报告 P0-3，发布必做）
--
-- 背景：management_access 表一旦存在，所有未登记的管理账号一律被拒绝，
-- 且关闭 CRM_BRIDGE_URL 也不会恢复旧行为。发布前必须把需要保留后台
-- 访问的账号逐个登记为 platform_admin，并用每个账号实际登录验证。
--
-- 使用方法（在 Go 业务库执行，宝塔可用数据库工具）：
--   第 1 步：运行下面的查询，导出现有管理后台账号清单；
--   第 2 步：与业务负责人逐个核对，确认每个账号是否保留后台访问；
--   第 3 步：把第 2 步 INSERT 中的占位 ID 替换为核对后的真实 ID 后执行；
--   第 4 步：用清单中每个账号实际登录后台并打开总控台/订单/用户页面验证。
--
-- ⚠️ 禁止按用户名或姓名猜测账号。宁可在维护窗口逐个核对，也不要
--    漏迁导致同事无法登录，或误授导致越权。

-- 第 1 步：导出现有管理账号（供核对，不修改数据）
SELECT u.id, u.username, u.created_at,
       a.scope, a.enabled
FROM users u
LEFT JOIN management_access a ON a.user_id = u.id
WHERE u.user_type = 'management'
ORDER BY u.id;

-- 第 3 步：登记首批平台管理员（把 <ADMIN_USER_ID> 替换为核对后的真实 ID，
-- 有多个账号就写多行 VALUES）。
-- enabled=TRUE 且 version=0 与 Go 端授权语义一致（版本从 0 起，保存后递增）。
-- INSERT INTO management_access(user_id,scope,enabled,version,updated_by,updated_at)
-- VALUES (<ADMIN_USER_ID>,'platform_admin',TRUE,0,<OPERATOR_USER_ID>,NOW(6))
-- ON DUPLICATE KEY UPDATE scope='platform_admin',enabled=TRUE,updated_by=<OPERATOR_USER_ID>,updated_at=NOW(6);

-- 迁移完成后写入审计（operator_id 使用执行人账号 ID）：
-- INSERT INTO management_access_audit(operator_id,user_id,action,after_value,result,request_id,created_at)
-- VALUES (<OPERATOR_USER_ID>,<ADMIN_USER_ID>,'seed_platform_admin','{"scope":"platform_admin","enabled":true}','success','migration-seed',NOW(6));

-- 回退提示：不要用删除 management_access 行的方式回退授权问题；
-- 个别账号授权错误时，按《统一后台部署与运维》第 5 节走页面内
-- 授权修改（带版本校验与审计），或停用该账号。
