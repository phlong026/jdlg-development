-- 存量线索回填手机号（配合 PR #16 的"新线索自动补号"，治理上线前遗留的幽灵线索）
-- 规则与 route/customer_service.go 的 createLead 自动补号一致：
--   1) 仅处理 user_id > 0 且 phone 为空（含纯空白）的线索，绝不覆盖已有号码
--   2) 号码来源 user_identities(identity_type='phone')，与 GetUserPhones 同源
--   3) 账号绑定了多个号码时无法判断该用哪个，跳过并在预览中列出，交人工处理
--   4) 号码不是 11 位大陆手机号格式的跳过，避免把脏数据写进销售的拨号入口
--   5) 保持 updated_at 不变：超时口径对"从未排期"的线索以 updated_at 起算 48h，
--      回填若刷新 updated_at 会让这批线索的超时计时被重置
--
-- 用法（务必按顺序）：
--   第一步  只执行「一、预览」，核对数量与样本，全程只读
--   第二步  执行「二、回填」：默认以 ROLLBACK 结尾（dry-run），只看 ROW_COUNT 与结果
--   第三步  确认无误后，把末尾 ROLLBACK 改成 COMMIT 再执行一次
--   回滚    见「三、回滚」，依赖回填时写入的备份表
--
-- 注意：本脚本会创建一张备份表 customer_leads_phone_backfill_bak（DDL，不随事务回滚），
--       留空表无副作用；确认回填无误后可自行 DROP。

-- =====================================================================
-- 一、预览（只读）
-- =====================================================================

-- 候选集：与回填使用同一份定义，保证"预览什么就回填什么"
DROP TEMPORARY TABLE IF EXISTS tmp_lead_phone_candidates;
CREATE TEMPORARY TABLE tmp_lead_phone_candidates AS
SELECT l.id AS lead_id, l.user_id, p.phone AS new_phone
FROM customer_leads l
JOIN (
    SELECT user_id, MIN(identity_key) AS phone
    FROM user_identities
    WHERE identity_type = 'phone'
    GROUP BY user_id
    HAVING COUNT(*) = 1
) p ON p.user_id = l.user_id
WHERE l.user_id > 0
  AND TRIM(l.phone) = ''
  AND p.phone REGEXP '^1[3-9][0-9]{9}$';

-- 1. 总览：无号码线索共多少，其中能补多少、补不了的原因分布
SELECT
    COUNT(*)                                                        AS empty_phone_leads,
    SUM(c.lead_id IS NOT NULL)                                      AS will_backfill,
    SUM(l.user_id = 0)                                              AS skip_anonymous_user,
    SUM(l.user_id > 0 AND c.lead_id IS NULL
        AND NOT EXISTS (SELECT 1 FROM user_identities i
                        WHERE i.user_id = l.user_id AND i.identity_type = 'phone'))
                                                                    AS skip_no_bound_phone,
    SUM(l.user_id > 0 AND c.lead_id IS NULL
        AND (SELECT COUNT(*) FROM user_identities i
             WHERE i.user_id = l.user_id AND i.identity_type = 'phone') > 1)
                                                                    AS skip_multi_phone,
    SUM(l.user_id > 0 AND c.lead_id IS NULL
        AND (SELECT COUNT(*) FROM user_identities i
             WHERE i.user_id = l.user_id AND i.identity_type = 'phone') = 1)
                                                                    AS skip_bad_format
FROM customer_leads l
LEFT JOIN tmp_lead_phone_candidates c ON c.lead_id = l.id
WHERE TRIM(l.phone) = '';

-- 2. 将被回填的线索明细（号码脱敏展示：前 3 位 + **** + 后 4 位）
SELECT l.id AS lead_id, l.user_id, l.status, l.created_at,
       CONCAT(LEFT(c.new_phone, 3), '****', RIGHT(c.new_phone, 4)) AS new_phone_masked
FROM tmp_lead_phone_candidates c
JOIN customer_leads l ON l.id = c.lead_id
ORDER BY l.id;

-- 3. 需人工处理：账号绑定了多个号码，脚本不替人选
SELECT l.id AS lead_id, l.user_id, l.status, COUNT(i.id) AS bound_phone_count
FROM customer_leads l
JOIN user_identities i ON i.user_id = l.user_id AND i.identity_type = 'phone'
WHERE TRIM(l.phone) = '' AND l.user_id > 0
GROUP BY l.id, l.user_id, l.status
HAVING COUNT(i.id) > 1
ORDER BY l.id;

-- 4. 补不了的存量：账号没绑过手机号，也没有微信/企微联系方式（真正的"幽灵线索"）
SELECT l.id AS lead_id, l.user_id, l.status, l.created_at
FROM customer_leads l
WHERE TRIM(l.phone) = ''
  AND TRIM(l.wechat) = ''
  AND TRIM(l.enterprise_wechat_contact) = ''
  AND l.id NOT IN (SELECT lead_id FROM tmp_lead_phone_candidates)
ORDER BY l.id;

-- =====================================================================
-- 二、回填（默认 dry-run：末尾为 ROLLBACK）
-- =====================================================================

CREATE TABLE IF NOT EXISTS customer_leads_phone_backfill_bak (
    lead_id       BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    old_phone     VARCHAR(32) NOT NULL,
    new_phone     VARCHAR(32) NOT NULL,
    backfilled_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

START TRANSACTION;

-- 先备份旧值（INSERT IGNORE：重复执行不会覆盖第一次留下的原始值）
INSERT IGNORE INTO customer_leads_phone_backfill_bak (lead_id, old_phone, new_phone)
SELECT l.id, l.phone, c.new_phone
FROM tmp_lead_phone_candidates c
JOIN customer_leads l ON l.id = c.lead_id;

-- 回填；再次校验 phone 为空，防止预览与执行之间有线索被客户补填
UPDATE customer_leads l
JOIN tmp_lead_phone_candidates c ON c.lead_id = l.id
SET l.phone = c.new_phone,
    l.updated_at = l.updated_at
WHERE TRIM(l.phone) = '';

SELECT ROW_COUNT() AS backfilled_rows;

-- 校验：候选集中仍为空的应为 0
SELECT COUNT(*) AS candidates_still_empty
FROM customer_leads l
JOIN tmp_lead_phone_candidates c ON c.lead_id = l.id
WHERE TRIM(l.phone) = '';

-- 校验：回填后剩余无号码线索数（对比预览第 1 项）
SELECT COUNT(*) AS remaining_empty_phone_leads
FROM customer_leads
WHERE TRIM(phone) = '';

-- dry-run：撤销本事务。确认无误后，把下一行改成 COMMIT; 再执行第二部分
ROLLBACK;

-- =====================================================================
-- 三、回滚（仅在已 COMMIT 且需要撤销时执行）
-- 只还原"现在仍等于回填值"的行，避免覆盖回填后客户或销售的手工修改
-- =====================================================================
-- START TRANSACTION;
-- UPDATE customer_leads l
-- JOIN customer_leads_phone_backfill_bak b ON b.lead_id = l.id
-- SET l.phone = b.old_phone,
--     l.updated_at = l.updated_at
-- WHERE l.phone = b.new_phone;
-- SELECT ROW_COUNT() AS reverted_rows;
-- COMMIT;
