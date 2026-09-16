USE `jiadilinguang`;

SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci;
SET collation_connection = 'utf8mb4_unicode_ci';

SET @master_user_id = 13;
SET @source_user_id = 25;
SET @merge_real_name = CAST('甘玲玲' AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_unicode_ci;
SET @merge_id_card_no = CAST('350123198310115142' AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_unicode_ci;

SELECT
  @master_user_id AS `recommended_master_user_id`,
  @source_user_id AS `recommended_source_user_id`,
  @merge_real_name AS `merge_real_name`,
  @merge_id_card_no AS `merge_id_card_no`;

SELECT
  `id`,
  `username`,
  `user_type`,
  `can_withdraw`,
  `merge_status`,
  `merged_to_user_id`,
  CASE WHEN COALESCE(NULLIF(`password`, ''), '') = '' THEN 0 ELSE 1 END AS `has_password_hash`,
  `created_at`,
  `updated_at`
FROM `users`
WHERE `id` IN (@master_user_id, @source_user_id)
ORDER BY `id`;

SELECT
  `user_id`,
  `nickname`,
  `device_id`,
  `has_password`,
  `enterprise_wechat_verified`,
  `designer_visible`,
  `device_bind_time`,
  `last_device_change_time`,
  `updated_at`
FROM `user_profiles`
WHERE `user_id` IN (@master_user_id, @source_user_id)
ORDER BY `user_id`;

SELECT
  `id`,
  `user_id`,
  `type`,
  `identity_type`,
  `status`,
  `real_name`,
  `id_card_no`,
  `company_name`,
  `credit_code`,
  `legal_person`,
  `admin_remark`,
  `reviewed_at`,
  `reviewed_by`,
  `created_at`,
  `updated_at`
FROM `certification_applications`
WHERE `user_id` IN (@master_user_id, @source_user_id)
   OR (`real_name` COLLATE utf8mb4_unicode_ci = @merge_real_name AND `id_card_no` COLLATE utf8mb4_unicode_ci = @merge_id_card_no)
ORDER BY `user_id`, `created_at`, `id`;

SELECT
  `user_id`,
  `type`,
  `identity_type`,
  `status`,
  COUNT(*) AS `row_count`,
  MAX(`created_at`) AS `latest_created_at`,
  GROUP_CONCAT(CAST(`id` AS CHAR) ORDER BY `id` SEPARATOR ',') AS `cert_ids`
FROM `certification_applications`
WHERE `user_id` IN (@master_user_id, @source_user_id)
   OR (`real_name` COLLATE utf8mb4_unicode_ci = @merge_real_name AND `id_card_no` COLLATE utf8mb4_unicode_ci = @merge_id_card_no)
GROUP BY `user_id`, `type`, `identity_type`, `status`
ORDER BY `user_id`, `type`, `identity_type`, `status`;

SELECT
  `type`,
  `identity_type`,
  COUNT(DISTINCT `user_id`) AS `approved_user_count`,
  GROUP_CONCAT(DISTINCT CAST(`user_id` AS CHAR) ORDER BY `user_id` SEPARATOR ',') AS `approved_user_ids`,
  COUNT(*) AS `approved_row_count`,
  GROUP_CONCAT(CAST(`id` AS CHAR) ORDER BY `user_id`, `id` SEPARATOR ',') AS `approved_cert_ids`
FROM `certification_applications`
WHERE `user_id` IN (@master_user_id, @source_user_id)
  AND `status` = 'approved'
GROUP BY `type`, `identity_type`
ORDER BY `type`, `identity_type`;

SELECT
  `id`,
  `user_id`,
  `plan_code`,
  `plan_title`,
  `status`,
  `granted_at`,
  `expired_at`,
  `created_at`,
  `updated_at`
FROM `user_memberships`
WHERE `user_id` IN (@master_user_id, @source_user_id)
ORDER BY `user_id`, `id`;

SELECT
  `id`,
  `user_id`,
  `invite_code`,
  `created_at`,
  `updated_at`
FROM `user_invite_codes`
WHERE `user_id` IN (@master_user_id, @source_user_id)
ORDER BY `user_id`, `id`;

SELECT 'user_orders' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `user_orders`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'stone_records' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `stone_records`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'ai_tasks' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `ai_tasks`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'ai_video_tasks' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `ai_video_tasks`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'templates' AS `table_name`, `creator_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `templates`
WHERE `creator_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `creator_user_id`
UNION ALL
SELECT 'template_comments' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_comments`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'template_shares' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_shares`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'template_likes' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_likes`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'template_unlocks' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_unlocks`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'designer_follows_as_follower' AS `table_name`, `follower_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_follows`
WHERE `follower_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `follower_user_id`
UNION ALL
SELECT 'designer_follows_as_designer' AS `table_name`, `designer_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_follows`
WHERE `designer_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `designer_user_id`
UNION ALL
SELECT 'designer_reviews_as_reviewer' AS `table_name`, `reviewer_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_reviews`
WHERE `reviewer_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `reviewer_user_id`
UNION ALL
SELECT 'designer_reviews_as_designer' AS `table_name`, `designer_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_reviews`
WHERE `designer_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `designer_user_id`
UNION ALL
SELECT 'invite_relations_as_inviter' AS `table_name`, `inviter_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `invite_relations`
WHERE `inviter_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `inviter_user_id`
UNION ALL
SELECT 'invite_relations_as_invitee' AS `table_name`, `invitee_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `invite_relations`
WHERE `invitee_user_id` IN (@master_user_id, @source_user_id)
GROUP BY `invitee_user_id`
UNION ALL
SELECT 'user_memberships' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `user_memberships`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
UNION ALL
SELECT 'user_invite_codes' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `user_invite_codes`
WHERE `user_id` IN (@master_user_id, @source_user_id)
GROUP BY `user_id`
ORDER BY `table_name`, `user_id`;

SELECT
  (SELECT COUNT(*) FROM `users` WHERE `id` = @master_user_id AND `user_type` = 'miniprogram') AS `master_exists`,
  (SELECT COUNT(*) FROM `users` WHERE `id` = @source_user_id AND `user_type` = 'miniprogram') AS `source_exists`,
  (SELECT `merge_status` FROM `users` WHERE `id` = @master_user_id LIMIT 1) AS `master_merge_status`,
  (SELECT `merge_status` FROM `users` WHERE `id` = @source_user_id LIMIT 1) AS `source_merge_status`,
  (SELECT COUNT(*) FROM `certification_applications` WHERE `user_id` = @master_user_id AND `status` = 'approved') AS `master_approved_cert_count`,
  (SELECT COUNT(*) FROM `certification_applications` WHERE `user_id` = @source_user_id AND `status` = 'approved') AS `source_approved_cert_count`,
  (SELECT COUNT(*) FROM `certification_applications` WHERE `user_id` IN (@master_user_id, @source_user_id) AND `status` = 'approved' GROUP BY `type`, `identity_type` HAVING COUNT(DISTINCT `user_id`) > 1 LIMIT 1) AS `has_cross_user_same_type_approved_conflict`,
  (SELECT COUNT(*) FROM `user_memberships` WHERE `user_id` = @master_user_id AND `status` = 'active') AS `master_active_membership_count`,
  (SELECT COUNT(*) FROM `user_memberships` WHERE `user_id` = @source_user_id AND `status` = 'active') AS `source_active_membership_count`,
  (SELECT COALESCE(MAX(`has_password`), 0) FROM `user_profiles` WHERE `user_id` = @source_user_id) AS `source_has_password_login`,
  (SELECT COALESCE(MAX(`has_password`), 0) FROM `user_profiles` WHERE `user_id` = @master_user_id) AS `master_has_password_login`;
