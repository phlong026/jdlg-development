-- 查询 user_profiles 中同一手机号关联了多个用户的记录
-- 用于排查上线手机号登录前，是否存在手机号重复绑定的情况

SELECT
    p.phone,
    COUNT(DISTINCT p.user_id) AS user_count,
    GROUP_CONCAT(p.user_id ORDER BY p.user_id) AS user_ids,
    GROUP_CONCAT(
        COALESCE(NULLIF(u.username, ''), CONCAT('uid:', u.id))
        ORDER BY p.user_id
    ) AS usernames
FROM user_profiles p
INNER JOIN users u ON u.id = p.user_id
WHERE p.phone IS NOT NULL
  AND p.phone != ''
  AND u.user_type = 'miniprogram'
  AND u.merge_status = 'normal'
GROUP BY p.phone
HAVING COUNT(DISTINCT p.user_id) > 1
ORDER BY user_count DESC;
