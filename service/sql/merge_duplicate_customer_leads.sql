-- 存量重复线索合并（PR #16 审计出的"同人重复"，一个人多条待处理线索）
-- 要求 MySQL 8.0+（使用窗口函数）。
--
-- 重复的定义：同一 user_id（>0）下，状态为 new / contacted 的线索多于 1 条。
--   · 已成交(converted)、无效(invalid) 的线索是历史记录，不参与合并
--   · 不同账号但手机号相同的线索不在本脚本范围，预览第 6 项仅列出供人工判断
--
-- 合并策略：软合并，不删除任何行
--   存活者  优先级：被分享归因引用 > 已联系 > 已分配跟进人 > 最早创建(id 最小)
--           选最早的是为了保住原始 created_at（漏斗时间窗）和已同步到 CRM 的 lead_id
--   并入者  状态改为 invalid，remark 追加"已合并至线索#X"；不删除，因为分享追踪的
--           business 记录（tracking_records，只增不改）按 lead:<id> 引用线索，
--           且校验线索状态；硬删或乱改会让历史归因失效
--   信息    存活者的空字段（姓名/手机/微信/企微/房屋土地信息/跟进人）由并入者补齐，
--           意向取最高，下次跟进取最早，状态有任一已联系则为已联系；
--           并入者的手机/微信/需求原文逐条写入存活者 remark，不丢信息
--   关联    customer_lead_followups、customer_service_events 的 lead_id 迁到存活者，
--           并给存活者补一条操作人为 dedup-merge-20261020 的跟进记录便于追溯
--   时间    保持并入者 updated_at 不变；存活者 updated_at 取组内最大值——
--           超时口径对"从未排期"的线索以 updated_at 起算 48h，不能被合并重置
--
-- 自动跳过、交人工的情况：
--   · 同一账号下有 ≥2 条线索被分享归因引用（无法保证全部归因仍然有效）
--   · @skip_user_ids 里指定的账号（如确认是两个不同房子的需求）
--
-- 用法（务必按顺序，且在同一个数据库会话里完成，候选集是临时表）：
--   第一步  执行「一、预览」，核对分组与角色，全程只读
--   第二步  执行「二、合并」：默认以 ROLLBACK 结尾（dry-run）
--   第三步  确认无误后，把末尾 ROLLBACK 改成 COMMIT 再执行一次
--   回滚    见「三、回滚」，依赖合并时写入的三张备份表
--
-- 重要：必须用 MySQL 客户端（mysql / Navicat 等）在同一会话里人工分步执行。
--       本文件会被 go:embed 打进服务二进制、出现在 `service migrate --list` 里，
--       但不要用 `service migrate` 执行：它按"整文件只跑一次"登记，且在一个事务里跑完全部语句，
--       与"先预览、再 dry-run、再 COMMIT"的两步流程冲突（dry-run 一跑就会被标记为已执行）。
--       它也不在启动自动迁移白名单(autoStartupMigrations)内，合并上线不会触发它。
--
-- 注意：
--   · 建议先执行 backfill_customer_leads_phone.sql，再执行本脚本——
--     这样合并时手机号更完整；反过来也可以，两者互不依赖
--   · 并入者若已同步到 CRM（预览第 5 项），CRM 侧会残留一条旧客户，需在客户管理里人工清理
--   · 存活者的手机号/微信/需求变化后，下一轮 CRM 同步会自动用新内容更新（幂等）
--   · 备份表是 DDL，不随事务回滚；dry-run 后留下的是空表，确认完成后可自行 DROP

SET SESSION group_concat_max_len = 1000000;

-- 不想合并的账号 ID，逗号分隔，例如 '12,34'；留空表示不排除
SET @skip_user_ids = '';

-- =====================================================================
-- 一、预览（只读）
-- =====================================================================

-- 排名表：每个重复账号下的待处理线索各一行，rn=1 为存活者
DROP TEMPORARY TABLE IF EXISTS tmp_dup_rank;
CREATE TEMPORARY TABLE tmp_dup_rank AS
SELECT t.*,
       ROW_NUMBER() OVER (PARTITION BY t.user_id
                          ORDER BY t.tracking_ref DESC, t.is_contacted DESC, t.has_assignee DESC, t.id ASC) AS rn,
       SUM(t.tracking_ref) OVER (PARTITION BY t.user_id) AS group_tracking_refs,
       COUNT(*) OVER (PARTITION BY t.user_id)            AS group_size
FROM (
    SELECT l.id, l.user_id, l.status,
           (TRIM(l.assigned_to) <> '') AS has_assignee,
           (l.status = 'contacted')    AS is_contacted,
           EXISTS (SELECT 1 FROM tracking_records r
                   WHERE r.kind = 'business'
                     AND JSON_UNQUOTE(JSON_EXTRACT(r.payload, '$.business_record_id')) = CONCAT('lead:', l.id)
           ) AS tracking_ref
    FROM customer_leads l
    WHERE l.user_id > 0
      AND l.status IN ('new', 'contacted')
      AND FIND_IN_SET(l.user_id, @skip_user_ids) = 0
      AND l.user_id IN (SELECT user_id FROM customer_leads
                        WHERE user_id > 0 AND status IN ('new', 'contacted')
                        GROUP BY user_id HAVING COUNT(*) > 1)
) t;

-- 存活者 / 并入者（归因冲突的账号整组跳过）
DROP TEMPORARY TABLE IF EXISTS tmp_dup_survivor;
CREATE TEMPORARY TABLE tmp_dup_survivor AS
SELECT user_id, id AS survivor_id FROM tmp_dup_rank WHERE rn = 1 AND group_tracking_refs < 2;

DROP TEMPORARY TABLE IF EXISTS tmp_dup_loser;
CREATE TEMPORARY TABLE tmp_dup_loser AS
SELECT r.id AS loser_id, r.user_id, s.survivor_id
FROM tmp_dup_rank r
JOIN tmp_dup_survivor s ON s.user_id = r.user_id
WHERE r.rn > 1;

-- MySQL 临时表在同一条语句内只能引用一次；统计查询里需要两次引用并入者，故备一份副本
DROP TEMPORARY TABLE IF EXISTS tmp_dup_loser_b;
CREATE TEMPORARY TABLE tmp_dup_loser_b AS SELECT loser_id FROM tmp_dup_loser;

-- 每个存活者从并入者汇总出的内容（字段取并入者中 id 最大的非空值，即最新填写的）
DROP TEMPORARY TABLE IF EXISTS tmp_dup_agg;
CREATE TEMPORARY TABLE tmp_dup_agg AS
SELECT x.survivor_id,
       GROUP_CONCAT(l.id ORDER BY l.id SEPARATOR ',') AS loser_ids,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.name), '')                      ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS name,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.phone), '')                     ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS phone,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.wechat), '')                    ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS wechat,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.enterprise_wechat_contact), '') ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS enterprise_wechat_contact,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.house_floors), '')              ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS house_floors,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.house_style), '')               ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS house_style,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.land_width), '')                ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS land_width,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.land_depth), '')                ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS land_depth,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.room_requirement), '')          ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS room_requirement,
       SUBSTRING_INDEX(GROUP_CONCAT(NULLIF(TRIM(l.assigned_to), '')               ORDER BY l.id DESC SEPARATOR '<|>'), '<|>', 1) AS assigned_to,
       MAX(FIELD(l.intent_level, 'low', 'medium', 'high')) AS max_intent,
       MAX(l.status = 'contacted')                         AS any_contacted,
       MIN(l.next_followup_at)                             AS min_next,
       MAX(l.updated_at)                                   AS max_updated,
       GROUP_CONCAT(CONCAT('[合并自线索#', l.id, ' ', l.created_at, '] 手机:', IF(TRIM(l.phone) = '', '-', TRIM(l.phone)),
                           ' 微信:', IF(TRIM(l.wechat) = '', '-', TRIM(l.wechat)),
                           ' 需求:', LEFT(COALESCE(l.demand_summary, ''), 500))
                    ORDER BY l.id SEPARATOR '\n') AS merged_remark
FROM tmp_dup_loser x
JOIN customer_leads l ON l.id = x.loser_id
GROUP BY x.survivor_id;

-- 1. 总览
SELECT g.dup_accounts, g.leads_in_dup_groups,
       (SELECT COUNT(*) FROM tmp_dup_survivor) AS accounts_to_merge,
       (SELECT COUNT(*) FROM tmp_dup_loser)    AS leads_to_retire,
       g.accounts_skipped_manual
FROM (SELECT COUNT(DISTINCT user_id)                                  AS dup_accounts,
             COUNT(*)                                                 AS leads_in_dup_groups,
             COUNT(DISTINCT IF(group_tracking_refs >= 2, user_id, NULL)) AS accounts_skipped_manual
      FROM tmp_dup_rank) g;

-- 2. 逐条角色明细（并入者也会显示它并入到哪条）
SELECT r.user_id, r.id AS lead_id,
       CASE WHEN r.group_tracking_refs >= 2 THEN '跳过(归因冲突)'
            WHEN r.rn = 1 THEN '存活' ELSE '并入' END AS role,
       s.survivor_id AS merge_into,
       r.status, r.tracking_ref AS has_tracking_ref, r.has_assignee, l.created_at,
       CONCAT(LEFT(NULLIF(TRIM(l.phone), ''), 3), '****', RIGHT(NULLIF(TRIM(l.phone), ''), 4)) AS phone_masked,
       LEFT(COALESCE(l.demand_summary, ''), 40) AS demand_head
FROM tmp_dup_rank r
JOIN customer_leads l ON l.id = r.id
LEFT JOIN tmp_dup_survivor s ON s.user_id = r.user_id AND r.rn > 1
ORDER BY r.user_id, r.rn;

-- 3. 需人工复核：同账号内各线索手机号不一致（合并后仅保留存活者/最新非空，其余写入 remark）
SELECT r.user_id, COUNT(DISTINCT NULLIF(TRIM(l.phone), '')) AS distinct_phones,
       GROUP_CONCAT(l.id ORDER BY l.id) AS lead_ids,
       MIN(l.created_at) AS first_created, MAX(l.created_at) AS last_created
FROM tmp_dup_rank r
JOIN customer_leads l ON l.id = r.id
GROUP BY r.user_id
HAVING COUNT(DISTINCT NULLIF(TRIM(l.phone), '')) > 1
ORDER BY r.user_id;

-- 4. 将被迁移的关联记录数量
SELECT
    (SELECT COUNT(*) FROM customer_lead_followups f WHERE f.lead_id IN (SELECT loser_id FROM tmp_dup_loser)) AS followups_to_move,
    (SELECT COUNT(*) FROM customer_service_events e WHERE e.lead_id IN (SELECT loser_id FROM tmp_dup_loser_b)) AS events_to_move;

-- 5. CRM 侧残留：并入者已同步到客户管理，合并后需人工清理对应客户
SELECT x.loser_id, x.survivor_id, d.customer_id, d.synced_at
FROM tmp_dup_loser x
JOIN customer_lead_crm_delivery d ON d.lead_id = x.loser_id
WHERE d.customer_id <> ''
ORDER BY x.loser_id;

-- 6. 仅供参考（不合并）：不同账号/匿名但手机号相同的待处理线索
SELECT TRIM(phone) AS phone_value, COUNT(*) AS lead_count, COUNT(DISTINCT user_id) AS account_count,
       GROUP_CONCAT(id ORDER BY id) AS lead_ids
FROM customer_leads
WHERE status IN ('new', 'contacted') AND TRIM(phone) <> ''
GROUP BY TRIM(phone)
HAVING COUNT(DISTINCT user_id) > 1
ORDER BY lead_count DESC;

-- =====================================================================
-- 二、合并（默认 dry-run：末尾为 ROLLBACK）
-- =====================================================================

-- 备份与映射表（DDL，放在事务外；行的写入在事务内，dry-run 不会留下数据）
CREATE TABLE IF NOT EXISTS customer_leads_merge_bak LIKE customer_leads;
CREATE TABLE IF NOT EXISTS customer_leads_merge_map (
    loser_id    BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    survivor_id BIGINT UNSIGNED NOT NULL,
    user_id     BIGINT UNSIGNED NOT NULL,
    merged_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS customer_leads_merge_moves (
    kind      VARCHAR(16) NOT NULL,           -- followup / event
    row_id    BIGINT UNSIGNED NOT NULL,
    from_lead BIGINT UNSIGNED NOT NULL,
    to_lead   BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (kind, row_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

START TRANSACTION;

-- 1) 备份存活者和并入者合并前的完整行（INSERT IGNORE：重复执行保留第一次的原始值）
INSERT IGNORE INTO customer_leads_merge_bak
SELECT * FROM customer_leads
WHERE id IN (SELECT survivor_id FROM tmp_dup_survivor)
   OR id IN (SELECT loser_id FROM tmp_dup_loser);

INSERT IGNORE INTO customer_leads_merge_map (loser_id, survivor_id, user_id)
SELECT loser_id, survivor_id, user_id FROM tmp_dup_loser;

-- 2) 记录将迁移的跟进记录和客服事件，再迁移
INSERT IGNORE INTO customer_leads_merge_moves (kind, row_id, from_lead, to_lead)
SELECT 'followup', f.id, x.loser_id, x.survivor_id
FROM customer_lead_followups f JOIN tmp_dup_loser x ON f.lead_id = x.loser_id;

INSERT IGNORE INTO customer_leads_merge_moves (kind, row_id, from_lead, to_lead)
SELECT 'event', e.id, x.loser_id, x.survivor_id
FROM customer_service_events e JOIN tmp_dup_loser x ON e.lead_id = x.loser_id;

UPDATE customer_lead_followups f
JOIN tmp_dup_loser x ON f.lead_id = x.loser_id
SET f.lead_id = x.survivor_id;
SELECT ROW_COUNT() AS followups_moved;

UPDATE customer_service_events e
JOIN tmp_dup_loser x ON e.lead_id = x.loser_id
SET e.lead_id = x.survivor_id;
SELECT ROW_COUNT() AS events_moved;

-- 3) 存活者：补空、取最高意向/最早下次跟进、追加并入者原文
UPDATE customer_leads l
JOIN tmp_dup_agg a ON a.survivor_id = l.id
SET l.name                      = IF(TRIM(l.name) = '', COALESCE(a.name, l.name), l.name),
    l.phone                     = IF(TRIM(l.phone) = '', COALESCE(a.phone, l.phone), l.phone),
    l.wechat                    = IF(TRIM(l.wechat) = '', COALESCE(a.wechat, l.wechat), l.wechat),
    l.enterprise_wechat_contact = IF(TRIM(l.enterprise_wechat_contact) = '', COALESCE(a.enterprise_wechat_contact, l.enterprise_wechat_contact), l.enterprise_wechat_contact),
    l.house_floors              = IF(TRIM(l.house_floors) = '', COALESCE(a.house_floors, l.house_floors), l.house_floors),
    l.house_style               = IF(TRIM(l.house_style) = '', COALESCE(a.house_style, l.house_style), l.house_style),
    l.land_width                = IF(TRIM(l.land_width) = '', COALESCE(a.land_width, l.land_width), l.land_width),
    l.land_depth                = IF(TRIM(l.land_depth) = '', COALESCE(a.land_depth, l.land_depth), l.land_depth),
    l.room_requirement          = IF(TRIM(l.room_requirement) = '', COALESCE(a.room_requirement, l.room_requirement), l.room_requirement),
    l.assigned_to               = IF(TRIM(l.assigned_to) = '', COALESCE(a.assigned_to, l.assigned_to), l.assigned_to),
    l.intent_level              = ELT(GREATEST(COALESCE(FIELD(l.intent_level, 'low', 'medium', 'high'), 0), a.max_intent, 1), 'low', 'medium', 'high'),
    l.status                    = IF(l.status = 'new' AND a.any_contacted = 1, 'contacted', l.status),
    l.next_followup_at          = CASE WHEN l.next_followup_at IS NULL THEN a.min_next
                                       WHEN a.min_next IS NULL THEN l.next_followup_at
                                       ELSE LEAST(l.next_followup_at, a.min_next) END,
    l.remark                    = CONCAT_WS('\n', NULLIF(l.remark, ''), a.merged_remark),
    l.updated_at                = GREATEST(l.updated_at, a.max_updated);
SELECT ROW_COUNT() AS survivors_updated;

-- 4) 并入者：置为无效并标注去向（不删除，保持 updated_at）
UPDATE customer_leads l
JOIN tmp_dup_loser x ON x.loser_id = l.id
SET l.status     = 'invalid',
    l.remark     = CONCAT_WS('\n', NULLIF(l.remark, ''), CONCAT('[重复合并] 已合并至线索#', x.survivor_id)),
    l.updated_at = l.updated_at;
SELECT ROW_COUNT() AS losers_retired;

-- 5) 给存活者留一条追溯用的跟进记录（回滚时按 operator_name 删除）
INSERT INTO customer_lead_followups (lead_id, status, remark, assigned_to, next_followup_at, operator_id, operator_name, created_at)
SELECT l.id, l.status, CONCAT('重复线索合并：线索#', a.loser_ids, ' 已并入本线索'),
       l.assigned_to, l.next_followup_at, 0, 'dedup-merge-20261020', NOW()
FROM customer_leads l
JOIN tmp_dup_agg a ON a.survivor_id = l.id;
SELECT ROW_COUNT() AS audit_followups_added;

-- 校验：这些账号下仍有多条待处理线索的应只剩"跳过/排除"的账号
SELECT user_id, COUNT(*) AS open_leads
FROM customer_leads
WHERE user_id > 0 AND status IN ('new', 'contacted')
  AND user_id IN (SELECT user_id FROM tmp_dup_survivor)
GROUP BY user_id
HAVING COUNT(*) > 1;

-- 校验：并入者名下不应再有跟进记录/事件
SELECT
    (SELECT COUNT(*) FROM customer_lead_followups f WHERE f.lead_id IN (SELECT loser_id FROM tmp_dup_loser)) AS loser_followups_left,
    (SELECT COUNT(*) FROM customer_service_events e WHERE e.lead_id IN (SELECT loser_id FROM tmp_dup_loser_b)) AS loser_events_left;

-- dry-run：撤销本事务。确认无误后，把下一行改成 COMMIT; 再执行第二部分
ROLLBACK;

-- =====================================================================
-- 三、回滚（仅在已 COMMIT 且需要撤销时执行；合并后不要再对这些线索做手工编辑，
--         否则回滚会用合并前的备份覆盖掉这些编辑）
-- =====================================================================
-- START TRANSACTION;
-- UPDATE customer_lead_followups f
-- JOIN customer_leads_merge_moves m ON m.kind = 'followup' AND m.row_id = f.id
-- SET f.lead_id = m.from_lead
-- WHERE f.lead_id = m.to_lead;
-- UPDATE customer_service_events e
-- JOIN customer_leads_merge_moves m ON m.kind = 'event' AND m.row_id = e.id
-- SET e.lead_id = m.from_lead
-- WHERE e.lead_id = m.to_lead;
-- DELETE FROM customer_lead_followups WHERE operator_name = 'dedup-merge-20261020';
-- REPLACE INTO customer_leads SELECT * FROM customer_leads_merge_bak;
-- COMMIT;
-- 回滚后如需重新合并：先 DROP 三张备份表(customer_leads_merge_bak/_map/_moves)，
-- 否则 INSERT IGNORE 会保留旧备份，掩盖回滚之后的新数据。
