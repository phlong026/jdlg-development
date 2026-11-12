-- Read-only JSONL export; mysql --default-character-set=utf8mb4 --batch --raw --skip-column-names.
-- Use an existing secure defaults file/login path. Do not pass passwords on the command line.
SET NAMES utf8mb4;
SELECT JSON_OBJECT(
  'id', id, 'prompt_key', prompt_key, 'label', label, 'heading', heading,
  'example', example, 'instruction', instruction,
  'updated_at', DATE_FORMAT(updated_at, '%Y-%m-%d %H:%i:%s.%f'),
  'updated_by', updated_by, 'sort_order', sort_order, 'enabled', enabled
)
FROM content_writing_prompts ORDER BY id;
