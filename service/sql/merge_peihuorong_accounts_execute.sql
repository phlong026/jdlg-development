USE `jiadilinguang`;

DROP PROCEDURE IF EXISTS `sp_merge_peihuorong_accounts`;
DELIMITER $$
CREATE PROCEDURE `sp_merge_peihuorong_accounts`()
BEGIN
  DECLARE v_batch_no VARCHAR(64) DEFAULT 'merge_peihuorong_40_from_22_42_20260401';
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
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '主账号40不存在，或不是小程序用户';
  END IF;

  SELECT COUNT(*) INTO v_source_user_exists
  FROM `users`
  WHERE `id` IN (22, 42)
    AND `user_type` = 'miniprogram';

  IF v_source_user_exists <> 2 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '源账号22/42不存在，或不是完整的小程序账号组';
  END IF;

  SELECT COUNT(*) INTO v_invalid_master_state
  FROM `users`
  WHERE `id` = v_master_user_id
    AND (`merge_status` = 'merged_source' OR (`merged_to_user_id` IS NOT NULL AND `merged_to_user_id` <> v_master_user_id));

  IF v_invalid_master_state > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '主账号40当前处于异常合并状态，不能执行本次归并';
  END IF;

  SELECT COUNT(*) INTO v_invalid_source_state
  FROM `users`
  WHERE `id` IN (22, 42)
    AND ((`merged_to_user_id` IS NOT NULL AND `merged_to_user_id` <> v_master_user_id) OR `merge_status` = 'merged_target');

  IF v_invalid_source_state > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '源账号22/42已并入其他主账号，或自身已被标记为主账号，不能重复归并';
  END IF;

  SELECT COUNT(*) INTO v_source_approved_cert_count
  FROM `certification_applications`
  WHERE `user_id` = 42
    AND `real_name` = '裴火荣'
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'approved';

  IF v_source_approved_cert_count < 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '未找到账号42上可归并的 approved 设计师认证，已停止执行';
  END IF;

  START TRANSACTION;

  INSERT INTO `user_merge_batches` (`batch_no`, `subject_name`, `master_user_id`, `source_user_ids`, `status`, `notes`, `started_at`, `finished_at`)
  VALUES (v_batch_no, '裴火荣', v_master_user_id, '22,42', 'running', '主账号40，归并22与42；保留42的approved认证，将22与40的pending_payment关闭为rejected并写合并备注。', NOW(), NULL)
  ON DUPLICATE KEY UPDATE
    `subject_name` = VALUES(`subject_name`),
    `master_user_id` = VALUES(`master_user_id`),
    `source_user_ids` = VALUES(`source_user_ids`),
    `status` = 'running',
    `notes` = VALUES(`notes`),
    `started_at` = NOW(),
    `finished_at` = NULL;

  UPDATE `user_orders`
  SET `user_id` = CASE WHEN `user_id` IN (22, 42) THEN 40 ELSE `user_id` END,
      `designer_user_id` = CASE WHEN `designer_user_id` IN (22, 42) THEN 40 ELSE `designer_user_id` END
  WHERE `user_id` IN (22, 42) OR `designer_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_user_orders', v_rows, '更新 user_orders.user_id / designer_user_id 到 40');

  UPDATE `stone_records`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_stone_records', v_rows, '更新 stone_records.user_id 到 40');

  UPDATE `ai_tasks`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_ai_tasks', v_rows, '更新 ai_tasks.user_id 到 40');

  UPDATE `ai_video_tasks`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_ai_video_tasks', v_rows, '更新 ai_video_tasks.user_id 到 40');

  UPDATE `templates`
  SET `creator_user_id` = 40
  WHERE `creator_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_templates', v_rows, '更新 templates.creator_user_id 到 40');

  UPDATE `template_comments`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_template_comments', v_rows, '更新 template_comments.user_id 到 40');

  UPDATE `template_shares`
  SET `user_id` = 40
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_template_shares', v_rows, '更新 template_shares.user_id 到 40');

  INSERT IGNORE INTO `template_likes` (`user_id`, `template_id`, `created_at`)
  SELECT 40, `template_id`, MIN(`created_at`)
  FROM `template_likes`
  WHERE `user_id` IN (22, 42)
  GROUP BY `template_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_template_likes_to_master', v_rows, '把旧账号点赞补到 40，重复点赞自动忽略');

  DELETE FROM `template_likes`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_template_likes_from_sources', v_rows, '删除 22/42 的点赞记录');

  INSERT IGNORE INTO `template_unlocks` (`user_id`, `template_id`, `created_at`)
  SELECT 40, `template_id`, MIN(`created_at`)
  FROM `template_unlocks`
  WHERE `user_id` IN (22, 42)
  GROUP BY `template_id`;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_template_unlocks_to_master', v_rows, '把旧账号模板解锁补到 40，重复解锁自动忽略');

  DELETE FROM `template_unlocks`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_template_unlocks_from_sources', v_rows, '删除 22/42 的模板解锁记录');

  INSERT IGNORE INTO `designer_follows` (`follower_user_id`, `designer_user_id`, `created_at`)
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
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_designer_follows_to_master', v_rows, '把旧账号关注关系归并到 40，重复关系自动忽略');

  DELETE FROM `designer_follows`
  WHERE `follower_user_id` IN (22, 42) OR `designer_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_designer_follows_from_sources', v_rows, '删除 22/42 的原关注关系');

  UPDATE `designer_reviews`
  SET `designer_user_id` = CASE WHEN `designer_user_id` IN (22, 42) THEN 40 ELSE `designer_user_id` END,
      `reviewer_user_id` = CASE WHEN `reviewer_user_id` IN (22, 42) THEN 40 ELSE `reviewer_user_id` END
  WHERE `designer_user_id` IN (22, 42) OR `reviewer_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'merge_designer_reviews', v_rows, '更新 designer_reviews 里 22/42 的设计师/评价人归属到 40');

  INSERT INTO `invite_relations` (`inviter_user_id`, `invitee_user_id`, `invited_at`, `first_recharge_done`)
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
  WHERE `new_inviter_user_id` <> `new_invitee_user_id`
  SELECT
    CASE WHEN `inviter_user_id` IN (22, 42) THEN 40 ELSE `inviter_user_id` END AS new_inviter_user_id,
    CASE WHEN `invitee_user_id` IN (22, 42) THEN 40 ELSE `invitee_user_id` END AS new_invitee_user_id,
    MIN(`invited_at`) AS invited_at,
    MAX(`first_recharge_done`) AS first_recharge_done
  FROM `invite_relations`
  WHERE `inviter_user_id` IN (22, 42) OR `invitee_user_id` IN (22, 42)
  GROUP BY new_inviter_user_id, new_invitee_user_id
  HAVING new_inviter_user_id <> new_invitee_user_id
  ON DUPLICATE KEY UPDATE
    `invited_at` = LEAST(`invite_relations`.`invited_at`, VALUES(`invited_at`)),
    `first_recharge_done` = GREATEST(`invite_relations`.`first_recharge_done`, VALUES(`first_recharge_done`));
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'insert_invite_relations_to_master', v_rows, '归并 invite_relations，已有 invitee 关系则保留主账号关系并同步首充标记');

  DELETE FROM `invite_relations`
  WHERE `inviter_user_id` IN (22, 42) OR `invitee_user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_invite_relations_from_sources', v_rows, '删除 22/42 的原邀请关系');

  SELECT COUNT(*) INTO v_master_has_membership
  FROM `user_memberships`
  WHERE `user_id` = 40;

  IF v_master_has_membership = 0 THEN
    UPDATE `user_memberships`
    SET `user_id` = 40
    WHERE `id` = (
      SELECT `picked_id`
      FROM (
        SELECT `id` AS picked_id
        FROM `user_memberships`
        WHERE `user_id` IN (22, 42)
        ORDER BY (`status` = 'active') DESC, `expired_at` DESC, `granted_at` DESC, `id` DESC
        LIMIT 1
      ) AS picked_membership
    );
    SET v_rows = ROW_COUNT();
    INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_best_membership_to_master', v_rows, '主账号无会员时，把旧账号中最优会员迁到 40');
  END IF;

  DELETE FROM `user_memberships`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_memberships_from_sources', v_rows, '删除 22/42 的剩余会员记录');

  SELECT COUNT(*) INTO v_master_has_invite_code
  FROM `user_invite_codes`
  WHERE `user_id` = 40;

  IF v_master_has_invite_code = 0 THEN
    UPDATE `user_invite_codes`
    SET `user_id` = 40
    WHERE `id` = (
      SELECT `picked_id`
      FROM (
        SELECT `id` AS picked_id
        FROM `user_invite_codes`
        WHERE `user_id` IN (22, 42)
        ORDER BY `id` ASC
        LIMIT 1
      ) AS picked_invite_code
    );
    SET v_rows = ROW_COUNT();
    INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_invite_code_to_master', v_rows, '主账号无邀请码时，迁移一个旧账号邀请码到 40');
  END IF;

  DELETE FROM `user_invite_codes`
  WHERE `user_id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'delete_invite_codes_from_sources', v_rows, '删除 22/42 的剩余邀请码记录');

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

  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'strengthen_master_profile_flags', v_rows, '若旧账号企业微信验证更强，则补强主账号 user_profiles 标记');

  UPDATE `certification_applications`
  SET `user_id` = 40,
      `status` = 'rejected',
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '账号合并关闭：原账号22并入主账号40，关闭重复待支付认证。'
        ELSE CONCAT(`admin_remark`, '；账号合并关闭：原账号22并入主账号40，关闭重复待支付认证。')
      END,
      `reviewed_at` = COALESCE(`reviewed_at`, NOW()),
      `reviewed_by` = CASE WHEN `reviewed_by` IS NULL OR `reviewed_by` = 0 THEN 0 ELSE `reviewed_by` END
  WHERE `user_id` = 22
    AND `real_name` = '裴火荣'
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'pending_payment';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'close_pending_cert_from_user_22', v_rows, '把 22 的 pending_payment 认证关闭为 rejected，并写入合并备注');

  UPDATE `certification_applications`
  SET `user_id` = 40,
      `status` = 'rejected',
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '账号合并关闭：主账号40已继承原账号42的已通过认证，关闭当前重复待支付认证。'
        ELSE CONCAT(`admin_remark`, '；账号合并关闭：主账号40已继承原账号42的已通过认证，关闭当前重复待支付认证。')
      END,
      `reviewed_at` = COALESCE(`reviewed_at`, NOW()),
      `reviewed_by` = CASE WHEN `reviewed_by` IS NULL OR `reviewed_by` = 0 THEN 0 ELSE `reviewed_by` END
  WHERE `user_id` = 40
    AND `real_name` = '裴火荣'
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'pending_payment';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'close_pending_cert_from_user_40', v_rows, '把 40 的 pending_payment 认证关闭为 rejected，并写入合并备注');

  UPDATE `certification_applications`
  SET `user_id` = 40,
      `admin_remark` = CASE
        WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '账号合并归并：原账号42并入主账号40，保留该条 approved 认证作为主账号最终有效认证。'
        ELSE CONCAT(`admin_remark`, '；账号合并归并：原账号42并入主账号40，保留该条 approved 认证作为主账号最终有效认证。')
      END
  WHERE `user_id` = 42
    AND `real_name` = '裴火荣'
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'approved';
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'move_approved_cert_from_user_42', v_rows, '把 42 的 approved 认证归并到主账号 40');

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
    CONCAT(COALESCE(NULLIF(`admin_remark`, ''), ''), CASE WHEN COALESCE(NULLIF(`admin_remark`, ''), '') = '' THEN '' ELSE '；' END, '账号合并快照：为确保主账号40在当前系统口径下按最新认证记录显示为已认证，复制一条 approved 认证快照。'),
    `reviewed_at`,
    `reviewed_by`,
    NOW(),
    NOW()
  FROM `certification_applications`
  WHERE `user_id` = 40
    AND `real_name` = '裴火荣'
    AND `id_card_no` = '362202198504264450'
    AND `type` = 'designer'
    AND `status` = 'approved'
  ORDER BY `id` DESC
  LIMIT 1;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'create_master_approved_cert_snapshot', v_rows, '为主账号 40 生成最新 approved 认证快照，确保按最新id读取时仍展示已认证');

  UPDATE `users`
  SET `can_withdraw` = 1,
      `merge_status` = 'merged_target',
      `merged_to_user_id` = NULL
  WHERE `id` = 40;
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'update_master_user_flags', v_rows, '设置主账号 40 为 merged_target，并打开 can_withdraw');

  UPDATE `users`
  SET `can_withdraw` = 0,
      `merge_status` = 'merged_source',
      `merged_to_user_id` = 40
  WHERE `id` IN (22, 42);
  SET v_rows = ROW_COUNT();
  INSERT INTO `user_merge_items` (`batch_no`, `step_name`, `affected_rows`, `detail`) VALUES (v_batch_no, 'freeze_source_users', v_rows, '把 22/42 标记为 merged_source，并指向主账号 40');

  UPDATE `user_merge_batches`
  SET `status` = 'success',
      `finished_at` = NOW()
  WHERE `batch_no` = v_batch_no;

  COMMIT;
END$$
DELIMITER ;

CALL `sp_merge_peihuorong_accounts`();
DROP PROCEDURE IF EXISTS `sp_merge_peihuorong_accounts`;
