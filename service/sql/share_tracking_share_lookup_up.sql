-- Additive upgrade: index and backfill for share lookups. Review index-build
-- locking and disk needs before running against a live database. This file is
-- not executed by the application. Re-running it is safe in the selected database.
--
-- Share and share_request rows are found by share ID when an operator shares
-- content (every share_intent event) and when a visitor is deleted. New rows store
-- the ID in share_id. Rows written without it keep the ID only in the payload and
-- are still found, but only by examining rows whose share_id is blank.

-- 1. Index the kind so a lookup touches share rows, not every row of the tenant.
SET @tracking_kind_share_index = (
 SELECT COUNT(*) FROM information_schema.statistics
 WHERE table_schema=DATABASE() AND table_name='tracking_records'
 AND index_name='tracking_kind_share'
);
SET @tracking_kind_share_ddl = IF(@tracking_kind_share_index=0,
 'ALTER TABLE tracking_records ADD INDEX tracking_kind_share(tenant_id,kind,share_id)',
 'SELECT 1');
PREPARE tracking_kind_share_stmt FROM @tracking_kind_share_ddl;
EXECUTE tracking_kind_share_stmt;
DEALLOCATE PREPARE tracking_kind_share_stmt;

-- 2. Backfill the column from the payload for rows that predate it. The
-- predicate leaves rows that already have a value, or that have no share ID in
-- the payload, untouched. Run it in a maintenance window on a large table.
UPDATE tracking_records
SET share_id = JSON_UNQUOTE(JSON_EXTRACT(payload,'$.share_id'))
WHERE kind IN ('share','share_request')
  AND share_id = ''
  AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.share_id')) IS NOT NULL
  AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.share_id')) <> '';

-- Verify: the count should be 0 once the backfill is complete.
-- SELECT COUNT(*) FROM tracking_records WHERE kind IN ('share','share_request') AND share_id='';
