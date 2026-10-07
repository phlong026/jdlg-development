-- 管理端操作审计表（权限与审计方案 阶段 A）。
-- 记录所有管理端的写操作（非 GET），回答"谁在什么时候对什么做了什么"。
-- 纯增量建表，幂等，可随服务启动自动执行。
CREATE TABLE IF NOT EXISTS management_operation_audit (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '操作人（management 账号 ID）',
  username VARCHAR(64) NOT NULL DEFAULT '' COMMENT '操作人用户名',
  method VARCHAR(8) NOT NULL DEFAULT '' COMMENT 'HTTP 方法（POST/PUT/DELETE/PATCH）',
  path VARCHAR(255) NOT NULL DEFAULT '' COMMENT '请求路径',
  target_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '资源类型（从路径提取，如 users / membership-plans）',
  target_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '资源 ID（路径中的数字段）',
  status INT NOT NULL DEFAULT 0 COMMENT '响应状态码',
  duration_ms INT NOT NULL DEFAULT 0 COMMENT '处理耗时（毫秒）',
  ip VARCHAR(45) NOT NULL DEFAULT '' COMMENT '来源 IP',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_audit_user (user_id, created_at),
  INDEX idx_audit_target (target_type, target_id),
  INDEX idx_audit_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
