-- Reverse of phase3_writing_prompts_up.sql. Dropping the table only removes
-- operable overrides; the API keeps serving code defaults afterwards.
DROP TABLE IF EXISTS content_writing_prompts;
