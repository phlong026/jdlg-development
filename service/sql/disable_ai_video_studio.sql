-- 回滚新模块时只关闭新提交，不删除草稿、任务、结果或费用事实。
UPDATE ai_video_studio_config
SET enabled = FALSE,
    modes_json = JSON_OBJECT(
      'text', FALSE,
      'first_frame', FALSE,
      'first_last_frame', FALSE,
      'reference', FALSE
    ),
    version = version + 1
WHERE id = 1;
