-- ============================================================
-- 手机号去重：将同一手机号仅绑定到指定主账号，其余账号解绑手机号
-- ============================================================
-- 手机号 15959089910 → 仅绑定到用户 4，解绑 13/17/91
-- 手机号 18721724142 → 仅绑定到用户 15，解绑 65
-- ============================================================

USE `jiadilinguang`;

-- ============================================================
-- 第一部分：手机号 15959089910 → 用户 4
-- ============================================================

-- 1.1 确认用户 4 的 profile 有该手机号（如果没有则补上）
UPDATE `user_profiles`
SET `phone` = '15959089910'
WHERE `user_id` = 4
  AND COALESCE(`phone`, '') != '15959089910';

-- 1.2 将该手机号写入 user_identities（如果不存在）
INSERT IGNORE INTO `user_identities` (`user_id`, `identity_type`, `identity_key`, `verified_at`)
VALUES (4, 'phone', '15959089910', NOW());

-- 1.3 清除其他用户(13/17/91) profile 中的该手机号
UPDATE `user_profiles`
SET `phone` = ''
WHERE `user_id` IN (13, 17, 91)
  AND `phone` = '15959089910';

-- 1.4 删除其他用户(13/17/91)在 user_identities 中的该手机号绑定
DELETE FROM `user_identities`
WHERE `identity_type` = 'phone'
  AND `identity_key` = '15959089910'
  AND `user_id` IN (13, 17, 91);


-- ============================================================
-- 第二部分：手机号 18721724142 → 用户 15
-- ============================================================

-- 2.1 确认用户 15 的 profile 有该手机号
UPDATE `user_profiles`
SET `phone` = '18721724142'
WHERE `user_id` = 15
  AND COALESCE(`phone`, '') != '18721724142';

-- 2.2 将该手机号写入 user_identities（如果不存在）
INSERT IGNORE INTO `user_identities` (`user_id`, `identity_type`, `identity_key`, `verified_at`)
VALUES (15, 'phone', '18721724142', NOW());

-- 2.3 清除其他用户(65) profile 中的该手机号
UPDATE `user_profiles`
SET `phone` = ''
WHERE `user_id` = 65
  AND `phone` = '18721724142';

-- 2.4 删除其他用户(65)在 user_identities 中的该手机号绑定
DELETE FROM `user_identities`
WHERE `identity_type` = 'phone'
  AND `identity_key` = '18721724142'
  AND `user_id` = 65;


-- ============================================================
-- 验证结果
-- ============================================================

-- 验证1：user_identities 中每个手机号应该只有1条记录
SELECT
    ui.identity_key AS phone,
    COUNT(*) AS bind_count,
    GROUP_CONCAT(ui.user_id ORDER BY ui.user_id) AS bound_user_ids
FROM user_identities ui
WHERE ui.identity_type = 'phone'
  AND ui.identity_key IN ('15959089910', '18721724142')
GROUP BY ui.identity_key;
-- 期望结果：每个手机号 bind_count = 1

-- 验证2：user_profiles 中这些手机号应该只出现在指定用户上
SELECT
    u.id AS user_id,
    u.username,
    p.phone,
    CASE
        WHEN p.phone = '15959089910' AND u.id = 4 THEN 'OK'
        WHEN p.phone = '18721724142' AND u.id = 15 THEN 'OK'
        WHEN p.phone IN ('15959089910', '18721724142') THEN 'PROBLEM: should be cleared'
        ELSE 'OK'
    END AS status
FROM users u
INNER JOIN user_profiles p ON p.user_id = u.id
WHERE p.phone IN ('15959089910', '18721724142')
  AND u.merge_status IN ('normal', 'merged_target');
-- 期望结果：只有用户4和用户15两条，status都是OK
