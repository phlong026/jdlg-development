START TRANSACTION;

SELECT COUNT(*) AS pending_cleanup_count
FROM ai_video_tasks
WHERE status = 'in_progress';

UPDATE ai_video_tasks
SET status = 'processing',
    updated_at = updated_at
WHERE status = 'in_progress';

SELECT ROW_COUNT() AS cleaned_rows;

COMMIT;
