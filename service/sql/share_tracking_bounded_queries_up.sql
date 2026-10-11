-- Additive upgrade for an existing tracking_records table. Review index-build
-- locking/disk needs before running against a live database. This file is not
-- executed by the application. Re-running it is safe in the selected database.
SET @tracking_session_index = (
 SELECT COUNT(*) FROM information_schema.statistics
 WHERE table_schema=DATABASE() AND table_name='tracking_records'
 AND index_name='tracking_session_time'
);
SET @tracking_session_ddl = IF(@tracking_session_index=0,
 'ALTER TABLE tracking_records ADD INDEX tracking_session_time(tenant_id,kind,session_id,occurred_at,record_id)',
 'SELECT 1');
PREPARE tracking_session_stmt FROM @tracking_session_ddl;
EXECUTE tracking_session_stmt;
DEALLOCATE PREPARE tracking_session_stmt;

SET @tracking_content_index = (
 SELECT COUNT(*) FROM information_schema.statistics
 WHERE table_schema=DATABASE() AND table_name='tracking_records'
 AND index_name='tracking_scope_content_time'
);
SET @tracking_content_ddl = IF(@tracking_content_index=0,
 'ALTER TABLE tracking_records ADD INDEX tracking_scope_content_time(tenant_id,kind,owner_id,content_id,occurred_at)',
 'SELECT 1');
PREPARE tracking_content_stmt FROM @tracking_content_ddl;
EXECUTE tracking_content_stmt;
DEALLOCATE PREPARE tracking_content_stmt;
