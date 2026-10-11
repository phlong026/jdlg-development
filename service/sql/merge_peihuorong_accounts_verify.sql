USE `jiadilinguang`;

SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci;
SET collation_connection = 'utf8mb4_unicode_ci';
SET @merge_batch_no = CAST('merge_peihuorong_40_from_22_42_20260401' AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_unicode_ci;
SET @merge_real_name = CAST('裴火荣' AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_unicode_ci;
SET @merge_id_card_no = CAST('362202198504264450' AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_unicode_ci;

SELECT
  `id`,
  `username`,
  `openid`,
  `unionid`,
  `can_withdraw`,
  `merge_status`,
  `merged_to_user_id`,
  `created_at`,
  `updated_at`
FROM `users`
WHERE `id` IN (22, 40, 42)
ORDER BY `id`;

SELECT
  `user_id`,
  `nickname`,
  `device_id`,
  `has_password`,
  `enterprise_wechat_verified`,
  `designer_visible`,
  `updated_at`
FROM `user_profiles`
WHERE `user_id` IN (22, 40, 42)
ORDER BY `user_id`;

SELECT
  `id`,
  `user_id`,
  `type`,
  `identity_type`,
  `status`,
  `admin_remark`,
  `reviewed_at`,
  `reviewed_by`,
  `created_at`
FROM `certification_applications`
WHERE `real_name` COLLATE utf8mb4_unicode_ci = @merge_real_name
  AND `id_card_no` COLLATE utf8mb4_unicode_ci = @merge_id_card_no
ORDER BY `id`;

SELECT 'user_orders' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `user_orders`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'stone_records' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `stone_records`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'ai_tasks' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `ai_tasks`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'ai_video_tasks' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `ai_video_tasks`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'templates' AS `table_name`, `creator_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `templates`
WHERE `creator_user_id` IN (22, 40, 42)
GROUP BY `creator_user_id`
UNION ALL
SELECT 'template_comments' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_comments`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'template_shares' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_shares`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'template_likes' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_likes`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'template_unlocks' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `template_unlocks`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'designer_follows_as_follower' AS `table_name`, `follower_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_follows`
WHERE `follower_user_id` IN (22, 40, 42)
GROUP BY `follower_user_id`
UNION ALL
SELECT 'designer_follows_as_designer' AS `table_name`, `designer_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_follows`
WHERE `designer_user_id` IN (22, 40, 42)
GROUP BY `designer_user_id`
UNION ALL
SELECT 'designer_reviews_as_reviewer' AS `table_name`, `reviewer_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_reviews`
WHERE `reviewer_user_id` IN (22, 40, 42)
GROUP BY `reviewer_user_id`
UNION ALL
SELECT 'designer_reviews_as_designer' AS `table_name`, `designer_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `designer_reviews`
WHERE `designer_user_id` IN (22, 40, 42)
GROUP BY `designer_user_id`
UNION ALL
SELECT 'invite_relations_as_inviter' AS `table_name`, `inviter_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `invite_relations`
WHERE `inviter_user_id` IN (22, 40, 42)
GROUP BY `inviter_user_id`
UNION ALL
SELECT 'invite_relations_as_invitee' AS `table_name`, `invitee_user_id` AS `user_id`, COUNT(*) AS `row_count`
FROM `invite_relations`
WHERE `invitee_user_id` IN (22, 40, 42)
GROUP BY `invitee_user_id`
UNION ALL
SELECT 'user_memberships' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `user_memberships`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
UNION ALL
SELECT 'user_invite_codes' AS `table_name`, `user_id`, COUNT(*) AS `row_count`
FROM `user_invite_codes`
WHERE `user_id` IN (22, 40, 42)
GROUP BY `user_id`
ORDER BY `table_name`, `user_id`;

SELECT
  `batch_no`,
  `subject_name`,
  `master_user_id`,
  `source_user_ids`,
  `status`,
  `notes`,
  `started_at`,
  `finished_at`
FROM `user_merge_batches`
WHERE `batch_no` COLLATE utf8mb4_unicode_ci = @merge_batch_no;

SELECT
  `batch_no`,
  `step_name`,
  `affected_rows`,
  `detail`,
  `created_at`
FROM `user_merge_items`
WHERE `batch_no` COLLATE utf8mb4_unicode_ci = @merge_batch_no
ORDER BY `id`;

SELECT
  (SELECT COUNT(*) FROM `user_orders` WHERE `user_id` = 40) AS `current_user_orders_on_40`,
  (SELECT COUNT(*) FROM `stone_records` WHERE `user_id` = 40) AS `current_stone_records_on_40`,
  (SELECT COUNT(*) FROM `ai_tasks` WHERE `user_id` = 40) AS `current_ai_tasks_on_40`,
  (SELECT COUNT(*) FROM `ai_video_tasks` WHERE `user_id` = 40) AS `current_ai_video_tasks_on_40`,
  (SELECT COUNT(*) FROM `templates` WHERE `creator_user_id` = 40) AS `current_templates_on_40`,
  (SELECT COUNT(*) FROM `certification_applications` WHERE `user_id` = 40 AND `real_name` COLLATE utf8mb4_unicode_ci = @merge_real_name AND `id_card_no` COLLATE utf8mb4_unicode_ci = @merge_id_card_no AND `status` = 'approved') AS `approved_cert_count_on_40`,
  (SELECT COUNT(*) FROM `certification_applications` WHERE `user_id` = 40 AND `real_name` COLLATE utf8mb4_unicode_ci = @merge_real_name AND `id_card_no` COLLATE utf8mb4_unicode_ci = @merge_id_card_no) AS `all_cert_rows_on_40`,
  (SELECT `status` FROM `certification_applications` WHERE `user_id` = 40 AND `real_name` COLLATE utf8mb4_unicode_ci = @merge_real_name AND `id_card_no` COLLATE utf8mb4_unicode_ci = @merge_id_card_no ORDER BY `id` DESC LIMIT 1) AS `latest_cert_status_on_40`,
  (SELECT COUNT(*) FROM `users` WHERE `id` IN (22, 42) AND `merge_status` = 'merged_source' AND `merged_to_user_id` = 40) AS `merged_source_user_count`,
  (SELECT COUNT(*) FROM `user_orders` WHERE `user_id` IN (22, 42)) AS `remaining_user_orders_on_22_42`,
  (SELECT COUNT(*) FROM `stone_records` WHERE `user_id` IN (22, 42)) AS `remaining_stone_records_on_22_42`,
  (SELECT COUNT(*) FROM `ai_tasks` WHERE `user_id` IN (22, 42)) AS `remaining_ai_tasks_on_22_42`,
  (SELECT COUNT(*) FROM `ai_video_tasks` WHERE `user_id` IN (22, 42)) AS `remaining_ai_video_tasks_on_22_42`,
  (SELECT COUNT(*) FROM `templates` WHERE `creator_user_id` IN (22, 42)) AS `remaining_templates_on_22_42`;
