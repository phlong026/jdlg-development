USE `jiadilinguang`;

SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci;
SET collation_connection = 'utf8mb4_unicode_ci';
SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO';

DROP PROCEDURE IF EXISTS `sp_merge_peihuorong_accounts`;
DELIMITER $$
CREATE PROCEDURE `sp_merge_peihuorong_accounts`()
BEGIN
  DECLARE v_batch_no VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'merge_peihuorong_40_from_22_42_20260401';
  DECLARE v_master_user_id BIGINT UNSIGNED DEFAULT 40;
  DECLARE v_exists INT DEFAULT 0;
  DECLARE v_rows BIGINT DEFAULT 0;
  DECLARE v_master_has_membership INT DEFAULT 0;
  DECLARE v_master_has_invite_code INT DEFAULT 0;
  DECLARE v_master_user_exists INT DEFAULT 0;
  DECLARE v_source_user_exists INT DEFAULT 0;
  DECLARE v_invalid_source_state INT DEFAULT 0;
  DECLARE v_invalid_master_state INT DEFAULT 0;
  DECLARE v_source_approved_cert_count INT DEFAULT 0;
  DECLARE v_source_profile_verified_count INT DEFAULT 0;

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
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'master user 40 not found or not miniprogram';
  END IF;

  SELECT COUNT(*) INTO v_source_user_exists
  FROM `users`
  WHERE `id` IN (22, 42)
    AND `user_type` = 'miniprogram';

  IF v_source_user_exists <> 2 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'source users 22/42 not found';
  END IF;

  SELECT COUNT(*) INTO v_invalid_master_state
  FROM `users`
  WHERE `id` = v_master_user_id
    AND (`merge_status` = 'merged_source' OR (`merged_to_user_id` IS NOT NULL AND `merged_to_user_id` <> v_master_user_id));

  IF v_invalid_master_state > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'master user 40 is in invalid merge state';
  END IF;

  SELECT COUNT(*) INTO v_invalid_source_state
  FROM `users`
  WHERE `id` IN (22, 42)
    AND ((`merged_to_user_id` IS NOT NULL AND `merged_to_user_id` <> v_master_user_id) OR `merge_status` = 'merged_target');

  IF v_invalid_source_state > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'source users 22/42 already merged elsewhere';
  END IF;

  SELECT COUNT(*) INTO v_source_approved_cert_count
  FROM `certification_applications`
  WHERE `user_id` = 42
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'approved';

  IF v_source_approved_cert_count < 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'approved certification on user 42 not found';
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
  VALUES (v_batch_no, 'PeiHuoRong', v_master_user_id, '22,42', 'running', 'master=40 source=22,42', NOW(), NULL);

  UPDATE `user_orders`
  SET `user_id` = CASE WHEN `user_id` IN (22, 42) THEN 40 ELSE `user_id` END,
      `designer_user_id` = CASE WHEN `designer_user_id` IN (22, 42) THEN 40 ELSE `designer_user_id` END
  WHERE `user_id` IN (22, 42) OR `designer_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_user_orders', v_rows, 'user_orders.user_id and designer_user_id to 40');

  UPDATE `stone_records`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_stone_records', v_rows, 'stone_records.user_id to 40');

  UPDATE `ai_tasks`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_ai_tasks', v_rows, 'ai_tasks.user_id to 40');

  UPDATE `ai_video_tasks`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_ai_video_tasks', v_rows, 'ai_video_tasks.user_id to 40');

  UPDATE `templates`
  SET `creator_user_id` = 40
  WHERE `creator_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_templates', v_rows, 'templates.creator_user_id to 40');

  UPDATE `template_comments`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_template_comments', v_rows, 'template_comments.user_id to 40');

  UPDATE `template_shares`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_template_shares', v_rows, 'template_shares.user_id to 40');

  INSERT IGNORE INTO `template_likes` (`user_id`, `template_id`, `created_at`)
  SELECT 40, `template_id`, MIN(`created_at`)
  FROM `template_likes`
  WHERE `user_id` IN (22, 42)
  GROUP BY `template_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_template_likes_to_master', v_rows, 'template_likes to 40');

  DELETE FROM `template_likes`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_template_likes_from_sources', v_rows, 'delete source template_likes');

  INSERT IGNORE INTO `template_unlocks` (`user_id`, `template_id`, `created_at`)
  SELECT 40, `template_id`, MIN(`created_at`)
  FROM `template_unlocks`
  WHERE `user_id` IN (22, 42)
  GROUP BY `template_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_template_unlocks_to_master', v_rows, 'template_unlocks to 40');

  DELETE FROM `template_unlocks`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_template_unlocks_from_sources', v_rows, 'delete source template_unlocks');

  INSERT INTO `tmp_designer_follow_merge` (`follower_user_id`, `designer_user_id`, `created_at`)
  SELECT `new_follower_user_id`, `new_designer_user_id`, `created_at`
  FROM (
    SELECT
      CASE WHEN `follower_user_id` IN (22, 42) THEN 40 ELSE `follower_user_id` END AS `new_follower_user_id`,
      CASE WHEN `designer_user_id` IN (22, 42) THEN 40 ELSE `designer_user_id` END AS `new_designer_user_id`,
      MIN(`created_at`) AS `created_at`
    FROM `designer_follows`
    WHERE `follower_user_id` IN (22, 42) OR `designer_user_id` IN (22, 42)
    GROUP BY
      CASE WHEN `follower_user_id` IN (22, 42) THEN 40 ELSE `follower_user_id` END,
      CASE WHEN `designer_user_id` IN (22, 42) THEN 40 ELSE `designer_user_id` END
  ) `follow_merge_rows`
  WHERE `new_follower_user_id` <> `new_designer_user_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'stage_designer_follows', v_rows, 'stage designer_follows merge rows');

  INSERT IGNORE INTO `designer_follows` (`follower_user_id`, `designer_user_id`, `created_at`)
  SELECT `follower_user_id`, `designer_user_id`, `created_at`
  FROM `tmp_designer_follow_merge`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_designer_follows_to_master', v_rows, 'designer_follows to 40');

  DELETE FROM `designer_follows`
  WHERE `follower_user_id` IN (22, 42) OR `designer_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_designer_follows_from_sources', v_rows, 'delete source designer_follows');

  UPDATE `designer_reviews`
  SET `designer_user_id` = CASE WHEN `designer_user_id` IN (22, 42) THEN 40 ELSE `designer_user_id` END,
      `reviewer_user_id` = CASE WHEN `reviewer_user_id` IN (22, 42) THEN 40 ELSE `reviewer_user_id` END
  WHERE `designer_user_id` IN (22, 42) OR `reviewer_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_designer_reviews', v_rows, 'designer_reviews to 40');

  INSERT INTO `tmp_invite_relation_merge` (`inviter_user_id`, `invitee_user_id`, `invited_at`, `first_recharge_done`)
  SELECT `new_inviter_user_id`, `new_invitee_user_id`, `invited_at`, `first_recharge_done`
  FROM (
    SELECT
      CASE WHEN `inviter_user_id` IN (22, 42) THEN 40 ELSE `inviter_user_id` END AS `new_inviter_user_id`,
      CASE WHEN `invitee_user_id` IN (22, 42) THEN 40 ELSE `invitee_user_id` END AS `new_invitee_user_id`,
      MIN(`invited_at`) AS `invited_at`,
      MAX(`first_recharge_done`) AS `first_recharge_done`
    FROM `invite_relations`
    WHERE `inviter_user_id` IN (22, 42) OR `invitee_user_id` IN (22, 42)
    GROUP BY
      CASE WHEN `inviter_user_id` IN (22, 42) THEN 40 ELSE `inviter_user_id` END,
      CASE WHEN `invitee_user_id` IN (22, 42) THEN 40 ELSE `invitee_user_id` END
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
  WHERE `inviter_user_id` IN (22, 42) OR `invitee_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_invite_relations_from_sources', v_rows, 'delete source invite_relations');

  SELECT COUNT(*) INTO v_master_has_membership
  FROM `user_memberships`
  WHERE `user_id` = 40;

  IF v_master_has_membership = 0 THEN
    UPDATE `user_memberships`
    SET `user_id` = 40
    WHERE `id` = (
      SELECT `picked_id`
      FROM (
        SELECT `id` AS `picked_id`
        FROM `user_memberships`
        WHERE `user_id` IN (22, 42)
        ORDER BY (`status` = 'active') DESC, `expired_at` DESC, `granted_at` DESC, `id` DESC
        LIMIT 1
      ) `picked_membership`
    );
    SET v_rows = ROW_COUNT();
    INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_best_membership_to_master', v_rows, 'move best membership to 40');
  END IF;

  DELETE FROM `user_memberships`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_memberships_from_sources', v_rows, 'delete source memberships');

  SELECT COUNT(*) INTO v_master_has_invite_code
  FROM `user_invite_codes`
  WHERE `user_id` = 40;

  IF v_master_has_invite_code = 0 THEN
    UPDATE `user_invite_codes`
    SET `user_id` = 40
    WHERE `id` = (
      SELECT `picked_id`
      FROM (
        SELECT `id` AS `picked_id`
        FROM `user_invite_codes`
        WHERE `user_id` IN (22, 42)
        ORDER BY `id` ASC
        LIMIT 1
      ) `picked_invite_code`
    );
    SET v_rows = ROW_COUNT();
    INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_invite_code_to_master', v_rows, 'move invite code to 40');
  END IF;

  DELETE FROM `user_invite_codes`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_invite_codes_from_sources', v_rows, 'delete source invite codes');

  SELECT COUNT(*) INTO v_source_profile_verified_count
  FROM `user_profiles`
  WHERE `user_id` IN (22, 42)
    AND `enterprise_wechat_verified` = 1;

  IF v_source_profile_verified_count > 0 THEN
    UPDATE `user_profiles`
    SET `enterprise_wechat_verified` = 1
    WHERE `user_id` = 40;
    SET v_rows = ROW_COUNT();
  ELSE
    SET v_rows = 0;
  END IF;

  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'strengthen_master_profile_flags', v_rows, 'strengthen enterprise_wechat_verified on 40');

  UPDATE `certification_applications`
  SET `user_id` = 40,
      `status` = 'rejected',
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN 'merged_close_source_22_duplicate_pending_payment'
        ELSE CONCAT(`admin_remark`, ';merged_close_source_22_duplicate_pending_payment')
      END,
      `reviewed_at` = COALESCE(`reviewed_at`, NOW()),
      `reviewed_by` = CASE WHEN `reviewed_by` IS NULL OR `reviewed_by` = 0 THEN 0 ELSE `reviewed_by` END
  WHERE `user_id` = 22
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'pending_payment';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'close_pending_cert_from_user_22', v_rows, 'close pending cert on 22');

  UPDATE `certification_applications`
  SET `user_id` = 40,
      `status` = 'rejected',
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN 'merged_close_master_40_duplicate_pending_payment'
        ELSE CONCAT(`admin_remark`, ';merged_close_master_40_duplicate_pending_payment')
      END,
      `reviewed_at` = COALESCE(`reviewed_at`, NOW()),
      `reviewed_by` = CASE WHEN `reviewed_by` IS NULL OR `reviewed_by` = 0 THEN 0 ELSE `reviewed_by` END
  WHERE `user_id` = 40
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'pending_payment';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'close_pending_cert_from_user_40', v_rows, 'close pending cert on 40');

  UPDATE `certification_applications`
  SET `user_id` = 40,
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN 'moved_approved_cert_from_42_to_40'
        ELSE CONCAT(`admin_remark`, ';moved_approved_cert_from_42_to_40')
      END
  WHERE `user_id` = 42
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'approved';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_approved_cert_from_user_42', v_rows, 'move approved cert from 42 to 40');

  INSERT INTO `certification_applications`
  (`user_id`, `type`, `real_name`, `id_card_no`, `company_name`, `credit_code`, `legal_person`, `aliyun_passed`, `aliyun_msg`, `extra_docs_remark`, `identity_type`, `status`, `admin_remark`, `reviewed_at`, `reviewed_by`, `created_at`, `updated_at`)
  SELECT
    40,
    `type`,
    `real_name`,
    `id_card_no`,
    `company_name`,
    `credit_code`,
    `legal_person`,
    `aliyun_passed`,
    `aliyun_msg`,
    `extra_docs_remark`,
    `identity_type`,
    'approved',
    CONCAT(COALESCE(NULLIF(`admin_remark`, ''), ''), CASE WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '' ELSE ';' END, 'created_master_approved_snapshot_after_merge'),
    `reviewed_at`,
    `reviewed_by`,
    NOW(),
    NOW()
  FROM `certification_applications`
  WHERE `user_id` = 40
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'approved'
  ORDER BY `id` DESC
  LIMIT 1;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'create_master_approved_cert_snapshot', v_rows, 'create latest approved snapshot on 40');

  UPDATE `users`
  SET `can_withdraw` = 1,
      `merge_status` = 'merged_target',
      `merged_to_user_id` = NULL
  WHERE `id` = 40;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'update_master_user_flags', v_rows, 'set 40 as merged_target');

  UPDATE `users`
  SET `can_withdraw` = 0,
      `merge_status` = 'merged_source',
      `merged_to_user_id` = 40
  WHERE `id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'freeze_source_users', v_rows, 'set 22 and 42 as merged_source');

  UPDATE `user_merge_batches`
  SET `status` = 'success',
      `finished_at` = NOW()
  WHERE `batch_no` COLLATE utf8mb4_unicode_ci = v_batch_no;

  DROP TEMPORARY TABLE IF EXISTS `tmp_designer_follow_merge`;
  DROP TEMPORARY TABLE IF EXISTS `tmp_invite_relation_merge`;

  COMMIT;
END$$
DELIMITER ;

CALL `sp_merge_peihuorong_accounts`();
DROP PROCEDURE IF EXISTS `sp_merge_peihuorong_accounts`;
