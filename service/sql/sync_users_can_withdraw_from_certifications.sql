USE `jiadilinguang`;

SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci;
SET collation_connection = 'utf8mb4_unicode_ci';
SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO';

DROP PROCEDURE IF EXISTS `sp_sync_users_can_withdraw_from_certifications`;
DELIMITER $$
CREATE PROCEDURE `sp_sync_users_can_withdraw_from_certifications`()
BEGIN
  DECLARE v_batch_no VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
  DECLARE v_total_users BIGINT DEFAULT 0;
  DECLARE v_changed_users BIGINT DEFAULT 0;
  DECLARE v_approved_users BIGINT DEFAULT 0;

  DECLARE EXIT HANDLER FOR SQLEXCEPTION
  BEGIN
    ROLLBACK;
    RESIGNAL;
  END;

  SET v_batch_no = DATE_FORMAT(NOW(6), 'sync_can_withdraw_%Y%m%d_%H%i%s_%f');

  CREATE TABLE IF NOT EXISTS `user_can_withdraw_sync_backup` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `batch_no` VARCHAR(64) NOT NULL,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `old_can_withdraw` TINYINT(1) NOT NULL DEFAULT 0,
    `new_can_withdraw` TINYINT(1) NOT NULL DEFAULT 0,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY `uk_batch_user` (`batch_no`, `user_id`),
    KEY `idx_user_id` (`user_id`)
  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

  START TRANSACTION;

  DROP TEMPORARY TABLE IF EXISTS `tmp_user_can_withdraw_expected`;
  CREATE TEMPORARY TABLE `tmp_user_can_withdraw_expected` (
    `user_id` BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    `old_can_withdraw` TINYINT(1) NOT NULL DEFAULT 0,
    `new_can_withdraw` TINYINT(1) NOT NULL DEFAULT 0
  ) ENGINE=InnoDB;

  INSERT INTO `tmp_user_can_withdraw_expected` (`user_id`, `old_can_withdraw`, `new_can_withdraw`)
  SELECT
    u.`id` AS `user_id`,
    COALESCE(u.`can_withdraw`, 0) AS `old_can_withdraw`,
    CASE WHEN approved_users.`user_id` IS NULL THEN 0 ELSE 1 END AS `new_can_withdraw`
  FROM `users` u
  LEFT JOIN (
    SELECT DISTINCT `user_id`
    FROM `certification_applications`
    WHERE `status` = 'approved'
  ) approved_users ON approved_users.`user_id` = u.`id`;

  SELECT COUNT(*) INTO v_total_users
  FROM `tmp_user_can_withdraw_expected`;

  SELECT COUNT(*) INTO v_changed_users
  FROM `tmp_user_can_withdraw_expected`
  WHERE `old_can_withdraw` <> `new_can_withdraw`;

  SELECT COUNT(*) INTO v_approved_users
  FROM `tmp_user_can_withdraw_expected`
  WHERE `new_can_withdraw` = 1;

  DELETE FROM `user_can_withdraw_sync_backup`
  WHERE `batch_no` = v_batch_no;

  INSERT INTO `user_can_withdraw_sync_backup` (`batch_no`, `user_id`, `old_can_withdraw`, `new_can_withdraw`)
  SELECT
    v_batch_no,
    `user_id`,
    `old_can_withdraw`,
    `new_can_withdraw`
  FROM `tmp_user_can_withdraw_expected`
  WHERE `old_can_withdraw` <> `new_can_withdraw`;

  UPDATE `users` u
  INNER JOIN `tmp_user_can_withdraw_expected` t
    ON t.`user_id` = u.`id`
  SET
    u.`can_withdraw` = t.`new_can_withdraw`,
    u.`updated_at` = NOW()
  WHERE COALESCE(u.`can_withdraw`, 0) <> t.`new_can_withdraw`;

  COMMIT;

  SELECT
    v_batch_no AS `batch_no`,
    v_total_users AS `total_users`,
    v_changed_users AS `changed_users`,
    v_approved_users AS `approved_users`;

  SELECT
    `user_id`,
    `old_can_withdraw`,
    `new_can_withdraw`,
    `created_at`
  FROM `user_can_withdraw_sync_backup`
  WHERE `batch_no` = v_batch_no
  ORDER BY `user_id`;

  SELECT CONCAT(
    'UPDATE users u INNER JOIN user_can_withdraw_sync_backup b ON b.user_id = u.id ',
    'SET u.can_withdraw = b.old_can_withdraw, u.updated_at = NOW() ',
    'WHERE b.batch_no = ''', v_batch_no, ''';'
  ) AS `rollback_sql`;
END$$
DELIMITER ;

CALL `sp_sync_users_can_withdraw_from_certifications`();

SELECT COUNT(*) AS `remaining_mismatched_users`
FROM `users` u
LEFT JOIN (
  SELECT DISTINCT `user_id`
  FROM `certification_applications`
  WHERE `status` = 'approved'
) approved_users ON approved_users.`user_id` = u.`id`
WHERE COALESCE(u.`can_withdraw`, 0) <> CASE WHEN approved_users.`user_id` IS NULL THEN 0 ELSE 1 END;

SET SQL_MODE=@OLD_SQL_MODE;
