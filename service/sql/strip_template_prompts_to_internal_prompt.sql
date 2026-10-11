UPDATE templates
SET
  internal_prompt = CASE
    WHEN COALESCE(TRIM(internal_prompt), '') = ''
      THEN TRIM(SUBSTRING(description, LOCATE('\n\n提示词: ', description) + CHAR_LENGTH('\n\n提示词: ')))
    ELSE internal_prompt
  END,
  description = TRIM(LEFT(description, LOCATE('\n\n提示词: ', description) - 1)),
  updated_at = NOW()
WHERE LOCATE('\n\n提示词: ', description) > 0;
