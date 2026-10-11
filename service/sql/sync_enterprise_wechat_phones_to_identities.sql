-- 迁移：将企业微信授权绑定的手机号同步到 user_identities 表
-- 背景：之前用户通过企业微信授权绑定的手机号只存在 user_profiles.enterprise_wechat_contact 中，
--       没有写入 user_identities 表。而新的手机号登录功能只查 user_identities 表，
--       导致老用户用企微授权过的手机号去"手机登录"时会被当成新用户，产生重复账号。
-- 本脚本把符合条件的企微手机号同步到 user_identities，确保老用户可以用手机号正常登录。
-- 去重规则：同一手机号若有多个用户通过企微授权，只绑定最早注册的那个有效用户（user_id 最小的）。

-- 第一步：将企微授权的手机号写入 user_identities 表（每个手机号只取 user_id 最小的有效用户）
INSERT IGNORE INTO user_identities (user_id, identity_type, identity_key, verified_at)
SELECT winner.user_id, 'phone', winner.phone, winner.verified_at
FROM (
    SELECT p.enterprise_wechat_contact AS phone,
           MIN(p.user_id) AS user_id,
           MIN(p.enterprise_wechat_verified_at) AS verified_at
    FROM user_profiles p
    INNER JOIN users u ON u.id = p.user_id AND u.user_type = 'miniprogram'
    WHERE p.enterprise_wechat_verified = 1
      AND p.enterprise_wechat_contact REGEXP '^1[0-9]{10}$'
      AND COALESCE(u.merge_status, 'normal') != 'merged_source'
    GROUP BY p.enterprise_wechat_contact
) AS winner
WHERE NOT EXISTS (
    SELECT 1 FROM user_identities ui
    WHERE ui.identity_type = 'phone'
      AND ui.identity_key = winner.phone
);

-- 第二步：同步补充 user_profiles.phone 字段
-- 只给 user_identities 中已绑定的那个用户补 phone，其他同手机号用户不补（避免混淆）
UPDATE user_profiles p
INNER JOIN user_identities ui
    ON ui.user_id = p.user_id
   AND ui.identity_type = 'phone'
   AND ui.identity_key = p.enterprise_wechat_contact
SET p.phone = p.enterprise_wechat_contact,
    p.updated_at = NOW()
WHERE p.enterprise_wechat_verified = 1
  AND p.enterprise_wechat_contact REGEXP '^1[0-9]{10}$'
  AND (p.phone IS NULL OR p.phone = '');
