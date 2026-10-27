-- The visitor_key backfill is valid denormalized share data and is deliberately
-- retained. Rollback removes only the additive query index.
SET @phase3_share_actor_index = (
 SELECT COUNT(*) FROM information_schema.statistics
 WHERE table_schema=DATABASE() AND table_name='tracking_records'
 AND index_name='tracking_share_actor_page'
);
SET @phase3_share_actor_ddl = IF(@phase3_share_actor_index>0,
 'ALTER TABLE tracking_records DROP INDEX tracking_share_actor_page',
 'SELECT 1');
PREPARE phase3_share_actor_stmt FROM @phase3_share_actor_ddl;
EXECUTE phase3_share_actor_stmt;
DEALLOCATE PREPARE phase3_share_actor_stmt;
