-- 回填 customer_service_events.lead_id = 0 的历史事件到对应线索
-- 规则与 backfillLeadIDForEvents 一致：
--   1) 仅处理 user_id > 0、lead_id = 0 的事件
--   2) 优先按 (user_id, source_task_no) 匹配
--   3) 剩余按 (user_id, session_no) 匹配
--   4) 同一匹配键存在多条 lead 时取 id 最大的一条，对齐 findExistingLeadID 的 ORDER BY id DESC LIMIT 1

START TRANSACTION;

-- 执行前待回填的事件数，便于对比
SELECT COUNT(*) AS pending_events_total
FROM customer_service_events
WHERE lead_id = 0 AND user_id > 0;

-- 第一步：按 source_task_no 回填
UPDATE customer_service_events e
JOIN (
    SELECT l.user_id, l.source_task_no, MAX(l.id) AS lead_id
    FROM customer_leads l
    WHERE l.source_task_no <> ''
    GROUP BY l.user_id, l.source_task_no
) ll
  ON ll.user_id = e.user_id
 AND ll.source_task_no = e.source_task_no
SET e.lead_id = ll.lead_id
WHERE e.lead_id = 0
  AND e.user_id > 0
  AND e.source_task_no <> '';

SELECT ROW_COUNT() AS backfilled_by_source_task_no;

-- 第二步：按 session_no 回填剩余事件
UPDATE customer_service_events e
JOIN (
    SELECT l.user_id, l.session_no, MAX(l.id) AS lead_id
    FROM customer_leads l
    WHERE l.session_no <> ''
    GROUP BY l.user_id, l.session_no
) ll
  ON ll.user_id = e.user_id
 AND ll.session_no = e.session_no
SET e.lead_id = ll.lead_id
WHERE e.lead_id = 0
  AND e.user_id > 0
  AND e.session_no <> '';

SELECT ROW_COUNT() AS backfilled_by_session_no;

-- 执行后仍未匹配到的事件数（通常为从未走到过 createLead 的纯浏览会话）
SELECT COUNT(*) AS remaining_events_total
FROM customer_service_events
WHERE lead_id = 0 AND user_id > 0;

COMMIT;
