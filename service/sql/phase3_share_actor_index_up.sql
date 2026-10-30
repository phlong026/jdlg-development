-- Additive migration for authenticated sharer keyset queries. Review online
-- DDL, lock time and disk headroom before production execution.

-- Older share rows kept sharer_id only in JSON. New reads retain a bounded
-- blank-scalar fallback during rollout, but this backfill restores the indexed
-- path for historical shares without guessing or identity-link expansion.
UPDATE tracking_records
SET visitor_key = JSON_UNQUOTE(JSON_EXTRACT(payload,'$.sharer_id'))
WHERE kind = 'share'
  AND visitor_key = ''
  AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.sharer_id')) IS NOT NULL
  AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.sharer_id')) <> '';

SET @phase3_share_actor_index = (
 SELECT COUNT(*) FROM information_schema.statistics
 WHERE table_schema=DATABASE() AND table_name='tracking_records'
 AND index_name='tracking_share_actor_page'
);
SET @phase3_share_actor_ddl = IF(@phase3_share_actor_index=0,
 'ALTER TABLE tracking_records ADD INDEX tracking_share_actor_page(tenant_id,kind,visitor_key,occurred_at,record_id)',
 'SELECT 1');
PREPARE phase3_share_actor_stmt FROM @phase3_share_actor_ddl;
EXECUTE phase3_share_actor_stmt;
DEALLOCATE PREPARE phase3_share_actor_stmt;

-- Verify after rollout:
-- SELECT COUNT(*) FROM tracking_records WHERE kind='share' AND visitor_key='';
