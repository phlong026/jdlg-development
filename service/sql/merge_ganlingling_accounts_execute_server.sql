USE `jiadilinguang`;

SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci;
SET collation_connection = 'utf8mb4_unicode_ci';
SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO';

DROP PROCEDURE IF EXISTS `sp_merge_ganlingling_accounts`;
DELIMITER $$
CREATE PROCEDURE `sp_merge_ganlingling_accounts`()
BEGIN
  DECLARE v_batch_no VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'merge_ganlingling_13_from_25_20260401';
  DECLARE v_master_user_id BIGINT UNSIGNED DEFAULT 13;
  DECLARE v_source_user_id BIGINT UNSIGNED DEFAULT 25;
  DECLARE v_exists INT DEFAULT 0;
  DECLARE v_rows BIGINT DEFAULT 0;
  DECLARE v_master_has_membership INT DEFAULT 0;
  DECLARE v_master_has_invite_code INT DEFAULT 0;
  DECLARE v_master_user_exists INT DEFAULT 0;
  DECLARE v_source_user_exists INT DEFAULT 0;
  DECLARE v_invalid_source_state INT DEFAULT 0;
  DECLARE v_invalid_master_state INT DEFAULT 0;
  DECLARE v_master_designer_approved_count INT DEFAULT 0;
  DECLARE v_source_designer_approved_count INT DEFAULT 0;
  DECLARE v_source_enterprise_approved_count INT DEFAULT 0;
  DECLARE v_master_designer_cert_id BIGINT UNSIGNED DEFAULT 15;
  DECLARE v_source_designer_cert_id_1 BIGINT UNSIGNED DEFAULT 1;
  DECLARE v_source_designer_cert_id_2 BIGINT UNSIGNED DEFAULT 13;
  DECLARE v_source_enterprise_cert_id BIGINT UNSIGNED DEFAULT 6;
  DECLARE v_source_profile_verified_count INT DEFAULT 0;
  DECLARE v_source_has_password_count INT DEFAULT 0;

  DECLARE EXIT HANDLER FOR SQLEXCEPTION
  BEGIN
    ROLLBACK;
    RESIGNAL;
  END;

  SELECT COUNT(*) INTO v_exists
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'users'
    AND COLUMN_NAME = 'merge_status';

  IF v_exists = 0 THEN
    SET @sql_add_merge_status = 'ALTER TABLE users ADD COLUMN merge_status VARCHAR(32) NOT NULL DEFAULT ''normal''';
    PREPARE stmt_add_merge_status FROM @sql_add_merge_status;
    EXECUTE stmt_add_merge_status;
    DEALLOCATE PREPARE stmt_add_merge_status;
  END IF;

  SELECT COUNT(*) INTO v_exists
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'users'
    AND COLUMN_NAME = 'merged_to_user_id';

  IF v_exists = 0 THEN
    SET @sql_add_merged_to = 'ALTER TABLE users ADD COLUMN merged_to_user_id BIGINT UNSIGNED NULL DEFAULT NULL';
    PREPARE stmt_add_merged_to FROM @sql_add_merged_to;
    EXECUTE stmt_add_merged_to;
    DEALLOCATE PREPARE stmt_add_merged_to;
  END IF;

  CREATE TABLE IF NOT EXISTS `user_merge_batches` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `batch_no` VARCHAR(64) NOT NULL,
    `subject_name` VARCHAR(64) NOT NULL DEFAULT '',
    `master_user_id` BIGINT UNSIGNED NOT NULL,
    `source_user_ids` VARCHAR(255) NOT NULL DEFAULT '',
    `status` VARCHAR(32) NOT NULL DEFAULT 'pending',
    `notes` TEXT NULL,
    `started_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `finished_at` DATETIME NULL DEFAULT NULL,
    UNIQUE KEY `uk_batch_no` (`batch_no`)
  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

  CREATE TABLE IF NOT EXISTS `user_merge_items` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `batch_no` VARCHAR(64) NOT NULL,
    `step_name` VARCHAR(128) NOT NULL,
    `affected_rows` BIGINT NOT NULL DEFAULT 0,
    `detail` TEXT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY `idx_batch_no` (`batch_no`)
  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

  SELECT COUNT(*) INTO v_master_user_exists
  FROM `users`
  WHERE `id` = v_master_user_id
    AND `user_type` = 'miniprogram';

  IF v_master_user_exists <> 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'master user 13 not found or not miniprogram';
  END IF;

  SELECT COUNT(*) INTO v_source_user_exists
  FROM `users`
  WHERE `id` = v_source_user_id
    AND `user_type` = 'miniprogram';

  IF v_source_user_exists <> 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'source user 25 not found or not miniprogram';
  END IF;

  SELECT COUNT(*) INTO v_invalid_master_state
  FROM `users`
  WHERE `id` = v_master_user_id
    AND (`merge_status` = 'merged_source' OR (`merged_to_user_id` IS NOT NULL AND `merged_to_user_id` <> v_master_user_id));

  IF v_invalid_master_state > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'master user 13 is in invalid merge state';
  END IF;

  SELECT COUNT(*) INTO v_invalid_source_state
  FROM `users`
  WHERE `id` = v_source_user_id
    AND ((`merged_to_user_id` IS NOT NULL AND `merged_to_user_id` <> v_master_user_id) OR `merge_status` = 'merged_target');

  IF v_invalid_source_state > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'source user 25 already merged elsewhere';
  END IF;

  SELECT COUNT(*) INTO v_master_designer_approved_count
  FROM `certification_applications`
  WHERE `id` = v_master_designer_cert_id
    AND `user_id` = v_master_user_id
    AND `type` = 'designer'
    AND `status` = 'approved';

  IF v_master_designer_approved_count < 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'approved designer certification id 15 on user 13 not found';
  END IF;

  SELECT COUNT(*) INTO v_source_designer_approved_count
  FROM `certification_applications`
  WHERE `id` IN (v_source_designer_cert_id_1, v_source_designer_cert_id_2)
    AND `user_id` = v_source_user_id
    AND `type` = 'designer'
    AND `status` = 'approved';

  IF v_source_designer_approved_count < 2 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'approved designer certification ids 1,13 on user 25 not found';
  END IF;

  SELECT COUNT(*) INTO v_source_enterprise_approved_count
  FROM `certification_applications`
  WHERE `id` = v_source_enterprise_cert_id
    AND `user_id` = v_source_user_id
    AND `type` = 'enterprise'
    AND `status` = 'approved';

  IF v_source_enterprise_approved_count < 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'enterprise approved certification id 6 on user 25 not found';
  END IF;

  START TRANSACTION;

  DROP TEMPORARY TABLE IF EXISTS `tmp_designer_follow_merge`;
  DROP TEMPORARY TABLE IF EXISTS `tmp_invite_relation_merge`;

  CREATE TEMPORARY TABLE `tmp_designer_follow_merge` (
    `follower_user_id` BIGINT UNSIGNED NOT NULL,
    `designer_user_id` BIGINT UNSIGNED NOT NULL,
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`follower_user_id`, `designer_user_id`)
  ) ENGINE=InnoDB;

  CREATE TEMPORARY TABLE `tmp_invite_relation_merge` (
    `inviter_user_id` BIGINT UNSIGNED NOT NULL,
    `invitee_user_id` BIGINT UNSIGNED NOT NULL,
    `invited_at` DATETIME NOT NULL,
    `first_recharge_done` TINYINT(1) NOT NULL DEFAULT 0,
    PRIMARY KEY (`invitee_user_id`)
  ) ENGINE=InnoDB;

  DELETE FROM `user_merge_batches`
  WHERE `batch_no` COLLATE utf8mb4_unicode_ci = v_batch_no;

  INSERT INTO `user_merge_batches` (`batch_no`, `subject_name`, `master_user_id`, `source_user_ids`, `status`, `notes`, `started_at`, `finished_at`)
  VALUES (v_batch_no, 'GanLingLing', v_master_user_id, '25', 'running', 'master=13 source=25; keep 13 designer approved; close source designer approved duplicates; close source enterprise approved for GuoCuiYi; move source business data to 13.', NOW(), NULL);

  UPDATE `user_orders`
  SET `user_id` = CASE WHEN `user_id` = v_source_user_id THEN v_master_user_id ELSE `user_id` END,
      `designer_user_id` = CASE WHEN `designer_user_id` = v_source_user_id THEN v_master_user_id ELSE `designer_user_id` END
  WHERE `user_id` = v_source_user_id OR `designer_user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_user_orders', v_rows, 'user_orders.user_id and designer_user_id to 13');

  UPDATE `stone_records`
  SET `user_id` = v_master_user_id
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_stone_records', v_rows, 'stone_records.user_id to 13');

  UPDATE `ai_tasks`
  SET `user_id` = v_master_user_id
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_ai_tasks', v_rows, 'ai_tasks.user_id to 13');

  UPDATE `ai_video_tasks`
  SET `user_id` = v_master_user_id
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_ai_video_tasks', v_rows, 'ai_video_tasks.user_id to 13');

  UPDATE `templates`
  SET `creator_user_id` = v_master_user_id
  WHERE `creator_user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_templates', v_rows, 'templates.creator_user_id to 13');

  UPDATE `template_comments`
  SET `user_id` = v_master_user_id
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_template_comments', v_rows, 'template_comments.user_id to 13');

  UPDATE `template_shares`
  SET `user_id` = v_master_user_id
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_template_shares', v_rows, 'template_shares.user_id to 13');

  INSERT IGNORE INTO `template_likes` (`user_id`, `template_id`, `created_at`)
  SELECT v_master_user_id, `template_id`, MIN(`created_at`)
  FROM `template_likes`
  WHERE `user_id` = v_source_user_id
  GROUP BY `template_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_template_likes_to_master', v_rows, 'template_likes to 13');

  DELETE FROM `template_likes`
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_template_likes_from_source', v_rows, 'delete source template_likes');

  INSERT IGNORE INTO `template_unlocks` (`user_id`, `template_id`, `created_at`)
  SELECT v_master_user_id, `template_id`, MIN(`created_at`)
  FROM `template_unlocks`
  WHERE `user_id` = v_source_user_id
  GROUP BY `template_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_template_unlocks_to_master', v_rows, 'template_unlocks to 13');

  DELETE FROM `template_unlocks`
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_template_unlocks_from_source', v_rows, 'delete source template_unlocks');

  INSERT INTO `tmp_designer_follow_merge` (`follower_user_id`, `designer_user_id`, `created_at`)
  SELECT `new_follower_user_id`, `new_designer_user_id`, `created_at`
  FROM (
    SELECT
      CASE WHEN `follower_user_id` = v_source_user_id THEN v_master_user_id ELSE `follower_user_id` END AS `new_follower_user_id`,
      CASE WHEN `designer_user_id` = v_source_user_id THEN v_master_user_id ELSE `designer_user_id` END AS `new_designer_user_id`,
      MIN(`created_at`) AS `created_at`
    FROM `designer_follows`
    WHERE `follower_user_id` = v_source_user_id OR `designer_user_id` = v_source_user_id
    GROUP BY
      CASE WHEN `follower_user_id` = v_source_user_id THEN v_master_user_id ELSE `follower_user_id` END,
      CASE WHEN `designer_user_id` = v_source_user_id THEN v_master_user_id ELSE `designer_user_id` END
  ) `follow_merge_rows`
  WHERE `new_follower_user_id` <> `new_designer_user_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'stage_designer_follows', v_rows, 'stage designer_follows merge rows');

  INSERT IGNORE INTO `designer_follows` (`follower_user_id`, `designer_user_id`, `created_at`)
  SELECT `follower_user_id`, `designer_user_id`, `created_at`
  FROM `tmp_designer_follow_merge`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_designer_follows_to_master', v_rows, 'designer_follows to 13');

  DELETE FROM `designer_follows`
  WHERE `follower_user_id` = v_source_user_id OR `designer_user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_designer_follows_from_source', v_rows, 'delete source designer_follows');

  UPDATE `designer_reviews`
  SET `designer_user_id` = CASE WHEN `designer_user_id` = v_source_user_id THEN v_master_user_id ELSE `designer_user_id` END,
      `reviewer_user_id` = CASE WHEN `reviewer_user_id` = v_source_user_id THEN v_master_user_id ELSE `reviewer_user_id` END
  WHERE `designer_user_id` = v_source_user_id OR `reviewer_user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_designer_reviews', v_rows, 'designer_reviews to 13');

  INSERT INTO `tmp_invite_relation_merge` (`inviter_user_id`, `invitee_user_id`, `invited_at`, `first_recharge_done`)
  SELECT `new_inviter_user_id`, `new_invitee_user_id`, `invited_at`, `first_recharge_done`
  FROM (
    SELECT
      CASE WHEN `inviter_user_id` = v_source_user_id THEN v_master_user_id ELSE `inviter_user_id` END AS `new_inviter_user_id`,
      CASE WHEN `invitee_user_id` = v_source_user_id THEN v_master_user_id ELSE `invitee_user_id` END AS `new_invitee_user_id`,
      MIN(`invited_at`) AS `invited_at`,
      MAX(`first_recharge_done`) AS `first_recharge_done`
    FROM `invite_relations`
    WHERE `inviter_user_id` = v_source_user_id OR `invitee_user_id` = v_source_user_id
    GROUP BY
      CASE WHEN `inviter_user_id` = v_source_user_id THEN v_master_user_id ELSE `inviter_user_id` END,
      CASE WHEN `invitee_user_id` = v_source_user_id THEN v_master_user_id ELSE `invitee_user_id` END
  ) `invite_merge_rows`
  WHERE `new_inviter_user_id` <> `new_invitee_user_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'stage_invite_relations', v_rows, 'stage invite_relations merge rows');

  UPDATE `invite_relations` `dst`
  INNER JOIN `tmp_invite_relation_merge` `src`
    ON `dst`.`invitee_user_id` = `src`.`invitee_user_id`
  SET `dst`.`invited_at` = LEAST(`dst`.`invited_at`, `src`.`invited_at`),
      `dst`.`first_recharge_done` = GREATEST(`dst`.`first_recharge_done`, `src`.`first_recharge_done`);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'update_existing_invite_relations', v_rows, 'update existing invite_relations by invitee');

  INSERT INTO `invite_relations` (`inviter_user_id`, `invitee_user_id`, `invited_at`, `first_recharge_done`)
  SELECT `src`.`inviter_user_id`, `src`.`invitee_user_id`, `src`.`invited_at`, `src`.`first_recharge_done`
  FROM `tmp_invite_relation_merge` `src`
  LEFT JOIN `invite_relations` `dst`
    ON `dst`.`invitee_user_id` = `src`.`invitee_user_id`
  WHERE `dst`.`id` IS NULL;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_invite_relations_to_master', v_rows, 'insert new invite_relations rows');

  DELETE FROM `invite_relations`
  WHERE `inviter_user_id` = v_source_user_id OR `invitee_user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_invite_relations_from_source', v_rows, 'delete source invite_relations');

  SELECT COUNT(*) INTO v_master_has_membership
  FROM `user_memberships`
  WHERE `user_id` = v_master_user_id;

  IF v_master_has_membership = 0 THEN
    UPDATE `user_memberships`
    SET `user_id` = v_master_user_id
    WHERE `id` = (
      SELECT `picked_id`
      FROM (
        SELECT `id` AS `picked_id`
        FROM `user_memberships`
        WHERE `user_id` = v_source_user_id
        ORDER BY (`status` = 'active') DESC, `expired_at` DESC, `granted_at` DESC, `id` DESC
        LIMIT 1
      ) `picked_membership`
    );
    SET v_rows = ROW_COUNT();
  ELSE
    SET v_rows = 0;
  END IF;
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_best_membership_to_master_if_needed', v_rows, 'keep master membership first, only move source membership if master has none');

  DELETE FROM `user_memberships`
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_memberships_from_source', v_rows, 'delete source memberships');

  SELECT COUNT(*) INTO v_master_has_invite_code
  FROM `user_invite_codes`
  WHERE `user_id` = v_master_user_id;

  IF v_master_has_invite_code = 0 THEN
    UPDATE `user_invite_codes`
    SET `user_id` = v_master_user_id
    WHERE `id` = (
      SELECT `picked_id`
      FROM (
        SELECT `id` AS `picked_id`
        FROM `user_invite_codes`
        WHERE `user_id` = v_source_user_id
        ORDER BY `id` ASC
        LIMIT 1
      ) `picked_invite_code`
    );
    SET v_rows = ROW_COUNT();
  ELSE
    SET v_rows = 0;
  END IF;
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_invite_code_to_master_if_needed', v_rows, 'move source invite code only if master has none');

  DELETE FROM `user_invite_codes`
  WHERE `user_id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_invite_codes_from_source', v_rows, 'delete source invite codes');

  SELECT COUNT(*) INTO v_source_profile_verified_count
  FROM `user_profiles`
  WHERE `user_id` = v_source_user_id
    AND `enterprise_wechat_verified` = 1;

  IF v_source_profile_verified_count > 0 THEN
    UPDATE `user_profiles`
    SET `enterprise_wechat_verified` = 1
    WHERE `user_id` = v_master_user_id;
    SET v_rows = ROW_COUNT();
  ELSE
    SET v_rows = 0;
  END IF;
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'strengthen_master_profile_flags', v_rows, 'strengthen enterprise_wechat_verified on 13');

  SELECT COUNT(*) INTO v_source_has_password_count
  FROM `user_profiles`
  WHERE `user_id` = v_source_user_id
    AND `has_password` = 1;

  IF v_source_has_password_count > 0 THEN
    UPDATE `users`
    SET `password` = CASE
      WHEN COALESCE(NULLIF(`password`, ''), '') = '' THEN (
        SELECT `picked_password`
        FROM (
          SELECT `password` AS `picked_password`
          FROM `users`
          WHERE `id` = v_source_user_id
          LIMIT 1
        ) `picked_password_row`
      )
      ELSE `password`
    END,
        `updated_at` = NOW()
    WHERE `id` = v_master_user_id;
    SET v_rows = ROW_COUNT();

    UPDATE `user_profiles`
    SET `has_password` = 1,
        `updated_at` = NOW()
    WHERE `user_id` = v_master_user_id;
  ELSE
    SET v_rows = 0;
  END IF;
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'inherit_password_capability_to_master', v_rows, 'if source has password, copy password hash to master when master is empty and mark master profile has_password=1');

  UPDATE `certification_applications`
  SET `user_id` = v_master_user_id,
      `status` = 'rejected',
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '账号合并关闭：原账号25并入主账号13，关闭重复设计师已通过认证，保留主账号13当前已通过记录为最终有效认证。'
        ELSE CONCAT(`admin_remark`, '；账号合并关闭：原账号25并入主账号13，关闭重复设计师已通过认证，保留主账号13当前已通过记录为最终有效认证。')
      END,
      `reviewed_at` = COALESCE(`reviewed_at`, NOW()),
      `reviewed_by` = CASE WHEN `reviewed_by` IS NULL OR `reviewed_by` = 0 THEN 0 ELSE `reviewed_by` END,
      `updated_at` = NOW()
  WHERE `id` IN (v_source_designer_cert_id_1, v_source_designer_cert_id_2)
    AND `user_id` = v_source_user_id
    AND `type` = 'designer'
    AND `status` = 'approved';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'close_source_designer_approved_duplicates', v_rows, 'move source designer approved cert ids 1,13 to master and close as rejected');

  UPDATE `certification_applications`
  SET `status` = 'rejected',
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '账号合并关闭：原账号25并入主账号13，关闭与主账号实名不一致的企业认证，该认证不随本次合并迁移。'
        ELSE CONCAT(`admin_remark`, '；账号合并关闭：原账号25并入主账号13，关闭与主账号实名不一致的企业认证，该认证不随本次合并迁移。')
      END,
      `reviewed_at` = COALESCE(`reviewed_at`, NOW()),
      `reviewed_by` = CASE WHEN `reviewed_by` IS NULL OR `reviewed_by` = 0 THEN 0 ELSE `reviewed_by` END,
      `updated_at` = NOW()
  WHERE `id` = v_source_enterprise_cert_id
    AND `user_id` = v_source_user_id
    AND `type` = 'enterprise'
    AND `status` = 'approved';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'close_source_enterprise_approved', v_rows, 'close source enterprise approved certification id 6 that belongs to another identity');

  UPDATE `users`
  SET `can_withdraw` = 1,
      `merge_status` = 'merged_target',
      `merged_to_user_id` = NULL
  WHERE `id` = v_master_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'update_master_user_flags', v_rows, 'set 13 as merged_target');

  UPDATE `users`
  SET `can_withdraw` = 0,
      `merge_status` = 'merged_source',
      `merged_to_user_id` = v_master_user_id
  WHERE `id` = v_source_user_id;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'freeze_source_user', v_rows, 'set 25 as merged_source to 13');

  UPDATE `user_merge_batches`
  SET `status` = 'success',
      `finished_at` = NOW()
  WHERE `batch_no` COLLATE utf8mb4_unicode_ci = v_batch_no;

  DROP TEMPORARY TABLE IF EXISTS `tmp_designer_follow_merge`;
  DROP TEMPORARY TABLE IF EXISTS `tmp_invite_relation_merge`;

  COMMIT;
END$$
DELIMITER ;

CALL `sp_merge_ganlingling_accounts`();
DROP PROCEDURE IF EXISTS `sp_merge_ganlingling_accounts`;
SET SQL_MODE=@OLD_SQL_MODE;
