-- Logical rollback: disable new work, retain tasks, quotes and financial audit.
-- Run this first. Resolve every returned reservation before reverting the app.
-- Additive tables/column are compatible with the previous app version and must
-- not be dropped: completed results and wallet history remain audit evidence.
UPDATE ai_video_studio_config
SET prompt_optimize_json=JSON_SET(COALESCE(prompt_optimize_json,JSON_OBJECT()),'$.enabled',JSON_EXTRACT('false','$')),
    version=version+1
WHERE id=1;

SELECT id,user_id,status,fee_status,total_stones
FROM ai_video_studio_prompt_tasks
WHERE fee_status='reserved'
ORDER BY created_at;
-- The result above must be empty before the application code is rolled back.
-- queued tasks can be cancelled; submitting/uncertain tasks require their real
-- supplier outcome. Never release an unknown request merely to clear this list.
