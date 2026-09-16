SET SESSION group_concat_max_len = 1024000;

DROP TEMPORARY TABLE IF EXISTS tmp_identity_signal_raw;
CREATE TEMPORARY TABLE tmp_identity_signal_raw (
  signal_rule VARCHAR(64) NOT NULL,
  signal_key VARCHAR(255) NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL
);

INSERT INTO tmp_identity_signal_raw (signal_rule, signal_key, user_id)
SELECT 'same_unionid', CONCAT('unionid:', TRIM(unionid)), id
FROM users
WHERE user_type = 'miniprogram'
  AND unionid IS NOT NULL
  AND TRIM(unionid) <> '';

INSERT INTO tmp_identity_signal_raw (signal_rule, signal_key, user_id)
SELECT 'same_openid', CONCAT('openid:', TRIM(openid)), id
FROM users
WHERE user_type = 'miniprogram'
  AND openid IS NOT NULL
  AND TRIM(openid) <> '';

INSERT INTO tmp_identity_signal_raw (signal_rule, signal_key, user_id)
SELECT 'same_id_card', CONCAT('id_card:', REPLACE(REPLACE(UPPER(TRIM(id_card_no)), ' ', ''), '-', '')), user_id
FROM certification_applications
WHERE id_card_no IS NOT NULL
  AND TRIM(id_card_no) <> '';

INSERT INTO tmp_identity_signal_raw (signal_rule, signal_key, user_id)
SELECT 'same_credit_code', CONCAT('credit_code:', REPLACE(REPLACE(UPPER(TRIM(credit_code)), ' ', ''), '-', '')), user_id
FROM certification_applications
WHERE credit_code IS NOT NULL
  AND TRIM(credit_code) <> '';

INSERT INTO tmp_identity_signal_raw (signal_rule, signal_key, user_id)
SELECT 'same_device_id', CONCAT('device_id:', TRIM(device_id)), user_id
FROM user_profiles
WHERE device_id IS NOT NULL
  AND TRIM(device_id) <> '';

DROP TEMPORARY TABLE IF EXISTS tmp_identity_groups;
CREATE TEMPORARY TABLE tmp_identity_groups AS
SELECT
  signal_rule,
  signal_key,
  COUNT(DISTINCT user_id) AS user_count,
  GROUP_CONCAT(DISTINCT CAST(user_id AS CHAR) ORDER BY user_id SEPARATOR ',') AS user_ids
FROM tmp_identity_signal_raw
GROUP BY signal_rule, signal_key
HAVING COUNT(DISTINCT user_id) > 1;

DROP TEMPORARY TABLE IF EXISTS tmp_target_users;
CREATE TEMPORARY TABLE tmp_target_users AS
SELECT DISTINCT r.user_id
FROM tmp_identity_signal_raw r
INNER JOIN tmp_identity_groups g
  ON g.signal_rule = r.signal_rule
 AND g.signal_key = r.signal_key;

ALTER TABLE tmp_target_users ADD PRIMARY KEY (user_id);

DROP TEMPORARY TABLE IF EXISTS tmp_user_signal_summary;
CREATE TEMPORARY TABLE tmp_user_signal_summary AS
SELECT
  r.user_id,
  COUNT(DISTINCT CONCAT(r.signal_rule, '|', r.signal_key)) AS matched_signal_count,
  GROUP_CONCAT(DISTINCT CONCAT(r.signal_rule, ':', r.signal_key) ORDER BY r.signal_rule, r.signal_key SEPARATOR ' || ') AS matched_signals
FROM tmp_identity_signal_raw r
INNER JOIN tmp_identity_groups g
  ON g.signal_rule = r.signal_rule
 AND g.signal_key = r.signal_key
GROUP BY r.user_id;

SELECT
  signal_rule,
  COUNT(*) AS duplicate_group_count,
  SUM(user_count) AS involved_user_count
FROM tmp_identity_groups
GROUP BY signal_rule
ORDER BY FIELD(signal_rule, 'same_unionid', 'same_openid', 'same_id_card', 'same_credit_code', 'same_device_id'), duplicate_group_count DESC;

SELECT
  g.signal_rule,
  g.signal_key,
  g.user_count,
  g.user_ids,
  GROUP_CONCAT(DISTINCT CONCAT(CAST(u.id AS CHAR), ':', COALESCE(NULLIF(up.nickname, ''), u.username)) ORDER BY u.id SEPARATOR ' | ') AS user_display
FROM tmp_identity_groups g
INNER JOIN tmp_identity_signal_raw r
  ON r.signal_rule = g.signal_rule
 AND r.signal_key = g.signal_key
INNER JOIN users u
  ON u.id = r.user_id
LEFT JOIN user_profiles up
  ON up.user_id = u.id
GROUP BY g.signal_rule, g.signal_key, g.user_count, g.user_ids
ORDER BY FIELD(g.signal_rule, 'same_unionid', 'same_openid', 'same_id_card', 'same_credit_code', 'same_device_id'), g.user_count DESC, g.signal_key;

SELECT
  u.id AS user_id,
  u.username,
  u.openid,
  u.unionid,
  u.can_withdraw,
  u.created_at AS user_created_at,
  u.updated_at AS user_updated_at,
  COALESCE(up.nickname, '') AS nickname,
  COALESCE(up.avatar, '') AS avatar,
  COALESCE(up.device_id, '') AS device_id,
  up.device_bind_time,
  up.last_device_change_time,
  COALESCE(up.has_password, 0) AS has_password,
  COALESCE(up.designer_visible, 0) AS designer_visible,
  COALESCE(up.enterprise_wechat_verified, 0) AS enterprise_wechat_verified,
  COALESCE(up.enterprise_wechat_contact, '') AS enterprise_wechat_contact,
  COALESCE(sig.matched_signal_count, 0) AS matched_signal_count,
  COALESCE(sig.matched_signals, '') AS matched_signals,
  COALESCE(cert.cert_count, 0) AS cert_count,
  COALESCE(cert.cert_approved_count, 0) AS cert_approved_count,
  cert.cert_last_created_at,
  COALESCE(cert.cert_digest, '') AS cert_digest,
  COALESCE(ord.order_count, 0) AS order_count,
  COALESCE(ord.success_order_count, 0) AS success_order_count,
  COALESCE(ord.positive_amount_total, 0) AS positive_amount_total,
  COALESCE(ord.negative_amount_total, 0) AS negative_amount_total,
  ord.order_last_created_at,
  COALESCE(stone.stone_record_count, 0) AS stone_record_count,
  stone.stone_last_created_at,
  COALESCE(ai.ai_task_count, 0) AS ai_task_count,
  COALESCE(ai.ai_success_count, 0) AS ai_success_count,
  COALESCE(ai.ai_failed_count, 0) AS ai_failed_count,
  ai.ai_last_created_at,
  COALESCE(video.video_task_count, 0) AS video_task_count,
  COALESCE(video.video_success_count, 0) AS video_success_count,
  COALESCE(video.video_failed_count, 0) AS video_failed_count,
  video.video_last_created_at,
  COALESCE(tpl.template_count, 0) AS template_count,
  COALESCE(tpl.published_template_count, 0) AS published_template_count,
  tpl.template_last_created_at,
  COALESCE(likes.template_like_count, 0) AS template_like_count,
  COALESCE(unlocks.template_unlock_count, 0) AS template_unlock_count,
  COALESCE(comments.template_comment_count, 0) AS template_comment_count,
  COALESCE(shares.template_share_count, 0) AS template_share_count,
  COALESCE(following.following_count, 0) AS following_count,
  COALESCE(followers.follower_count, 0) AS follower_count,
  COALESCE(reviews.review_as_designer_count, 0) AS review_as_designer_count,
  COALESCE(reviews.review_as_reviewer_count, 0) AS review_as_reviewer_count,
  COALESCE(inviter.invite_as_inviter_count, 0) AS invite_as_inviter_count,
  COALESCE(invitee.invite_as_invitee_count, 0) AS invite_as_invitee_count,
  COALESCE(invitecode.has_invite_code, 0) AS has_invite_code,
  COALESCE(member.has_membership, 0) AS has_membership,
  COALESCE(member.membership_status, '') AS membership_status,
  member.membership_expired_at
FROM tmp_target_users tu
INNER JOIN users u
  ON u.id = tu.user_id
LEFT JOIN user_profiles up
  ON up.user_id = u.id
LEFT JOIN tmp_user_signal_summary sig
  ON sig.user_id = u.id
LEFT JOIN (
  SELECT
    user_id,
    COUNT(*) AS cert_count,
    SUM(CASE WHEN status = 'approved' THEN 1 ELSE 0 END) AS cert_approved_count,
    MAX(created_at) AS cert_last_created_at,
    GROUP_CONCAT(
      CONCAT(
        COALESCE(type, ''), '/',
        COALESCE(identity_type, ''), '/',
        COALESCE(status, ''), '/',
        COALESCE(real_name, ''), '/',
        COALESCE(id_card_no, ''), '/',
        DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s')
      )
      ORDER BY created_at DESC SEPARATOR ' || '
    ) AS cert_digest
  FROM certification_applications
  GROUP BY user_id
) cert
  ON cert.user_id = u.id
LEFT JOIN (
  SELECT
    user_id,
    COUNT(*) AS order_count,
    SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success_order_count,
    SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END) AS positive_amount_total,
    SUM(CASE WHEN amount < 0 THEN amount ELSE 0 END) AS negative_amount_total,
    MAX(created_at) AS order_last_created_at
  FROM user_orders
  GROUP BY user_id
) ord
  ON ord.user_id = u.id
LEFT JOIN (
  SELECT
    user_id,
    COUNT(*) AS stone_record_count,
    MAX(created_at) AS stone_last_created_at
  FROM stone_records
  GROUP BY user_id
) stone
  ON stone.user_id = u.id
LEFT JOIN (
  SELECT
    user_id,
    COUNT(*) AS ai_task_count,
    SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS ai_success_count,
    SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS ai_failed_count,
    MAX(created_at) AS ai_last_created_at
  FROM ai_tasks
  GROUP BY user_id
) ai
  ON ai.user_id = u.id
LEFT JOIN (
  SELECT
    user_id,
    COUNT(*) AS video_task_count,
    SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS video_success_count,
    SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS video_failed_count,
    MAX(created_at) AS video_last_created_at
  FROM ai_video_tasks
  GROUP BY user_id
) video
  ON video.user_id = u.id
LEFT JOIN (
  SELECT
    creator_user_id AS user_id,
    COUNT(*) AS template_count,
    SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END) AS published_template_count,
    MAX(created_at) AS template_last_created_at
  FROM templates
  WHERE creator_user_id IS NOT NULL
    AND creator_user_id > 0
  GROUP BY creator_user_id
) tpl
  ON tpl.user_id = u.id
LEFT JOIN (
  SELECT user_id, COUNT(*) AS template_like_count
  FROM template_likes
  GROUP BY user_id
) likes
  ON likes.user_id = u.id
LEFT JOIN (
  SELECT user_id, COUNT(*) AS template_unlock_count
  FROM template_unlocks
  GROUP BY user_id
) unlocks
  ON unlocks.user_id = u.id
LEFT JOIN (
  SELECT user_id, COUNT(*) AS template_comment_count
  FROM template_comments
  GROUP BY user_id
) comments
  ON comments.user_id = u.id
LEFT JOIN (
  SELECT user_id, COUNT(*) AS template_share_count
  FROM template_shares
  WHERE user_id > 0
  GROUP BY user_id
) shares
  ON shares.user_id = u.id
LEFT JOIN (
  SELECT follower_user_id AS user_id, COUNT(*) AS following_count
  FROM designer_follows
  GROUP BY follower_user_id
) following
  ON following.user_id = u.id
LEFT JOIN (
  SELECT designer_user_id AS user_id, COUNT(*) AS follower_count
  FROM designer_follows
  GROUP BY designer_user_id
) followers
  ON followers.user_id = u.id
LEFT JOIN (
  SELECT
    designer_user_id AS user_id,
    COUNT(*) AS review_as_designer_count,
    0 AS review_as_reviewer_count
  FROM designer_reviews
  GROUP BY designer_user_id
) review_designer
  ON review_designer.user_id = u.id
LEFT JOIN (
  SELECT
    reviewer_user_id AS user_id,
    0 AS review_as_designer_count,
    COUNT(*) AS review_as_reviewer_count
  FROM designer_reviews
  GROUP BY reviewer_user_id
) review_reviewer
  ON review_reviewer.user_id = u.id
LEFT JOIN (
  SELECT
    base.user_id,
    SUM(base.review_as_designer_count) AS review_as_designer_count,
    SUM(base.review_as_reviewer_count) AS review_as_reviewer_count
  FROM (
    SELECT designer_user_id AS user_id, COUNT(*) AS review_as_designer_count, 0 AS review_as_reviewer_count
    FROM designer_reviews
    GROUP BY designer_user_id
    UNION ALL
    SELECT reviewer_user_id AS user_id, 0 AS review_as_designer_count, COUNT(*) AS review_as_reviewer_count
    FROM designer_reviews
    GROUP BY reviewer_user_id
  ) base
  GROUP BY base.user_id
) reviews
  ON reviews.user_id = u.id
LEFT JOIN (
  SELECT inviter_user_id AS user_id, COUNT(*) AS invite_as_inviter_count
  FROM invite_relations
  GROUP BY inviter_user_id
) inviter
  ON inviter.user_id = u.id
LEFT JOIN (
  SELECT invitee_user_id AS user_id, COUNT(*) AS invite_as_invitee_count
  FROM invite_relations
  GROUP BY invitee_user_id
) invitee
  ON invitee.user_id = u.id
LEFT JOIN (
  SELECT user_id, 1 AS has_invite_code
  FROM user_invite_codes
) invitecode
  ON invitecode.user_id = u.id
LEFT JOIN (
  SELECT user_id, 1 AS has_membership, status AS membership_status, expired_at AS membership_expired_at
  FROM user_memberships
) member
  ON member.user_id = u.id
ORDER BY matched_signal_count DESC, u.id DESC;

SELECT
  c.real_name,
  c.id_card_no,
  COUNT(DISTINCT c.user_id) AS user_count,
  GROUP_CONCAT(DISTINCT CAST(c.user_id AS CHAR) ORDER BY c.user_id SEPARATOR ',') AS user_ids,
  GROUP_CONCAT(DISTINCT CONCAT(CAST(c.user_id AS CHAR), ':', COALESCE(u.username, '')) ORDER BY c.user_id SEPARATOR ' | ') AS usernames,
  GROUP_CONCAT(CONCAT(COALESCE(c.type, ''), '/', COALESCE(c.identity_type, ''), '/', COALESCE(c.status, ''), '/', DATE_FORMAT(c.created_at, '%Y-%m-%d %H:%i:%s')) ORDER BY c.created_at DESC SEPARATOR ' || ') AS cert_records
FROM certification_applications c
LEFT JOIN users u
  ON u.id = c.user_id
GROUP BY c.real_name, c.id_card_no
HAVING COUNT(DISTINCT c.user_id) > 1
ORDER BY user_count DESC, MAX(c.created_at) DESC;

SELECT
  up.device_id,
  COUNT(DISTINCT up.user_id) AS user_count,
  GROUP_CONCAT(DISTINCT CAST(up.user_id AS CHAR) ORDER BY up.user_id SEPARATOR ',') AS user_ids,
  GROUP_CONCAT(DISTINCT CONCAT(CAST(up.user_id AS CHAR), ':', COALESCE(NULLIF(up.nickname, ''), u.username)) ORDER BY up.user_id SEPARATOR ' | ') AS user_display,
  MIN(up.device_bind_time) AS first_bind_time,
  MAX(COALESCE(up.last_device_change_time, up.updated_at)) AS last_active_time
FROM user_profiles up
LEFT JOIN users u
  ON u.id = up.user_id
WHERE up.device_id IS NOT NULL
  AND TRIM(up.device_id) <> ''
GROUP BY up.device_id
HAVING COUNT(DISTINCT up.user_id) > 1
ORDER BY user_count DESC, last_active_time DESC;
