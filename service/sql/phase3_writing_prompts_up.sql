-- Server-side registry for AI writing prompt templates (三期增补 B5).
-- Business wording (heading/instruction/example) becomes operable without an
-- app release; the anti-fabrication clause deliberately stays client-side so
-- configuration can never disable it. Applying this migration does not change
-- any behavior until rows are enabled; the API falls back to code defaults
-- while the table is empty or missing.
CREATE TABLE IF NOT EXISTS content_writing_prompts (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  prompt_key VARCHAR(32) NOT NULL,
  label VARCHAR(64) NOT NULL,
  heading VARCHAR(80) NOT NULL,
  example TEXT NOT NULL,
  instruction TEXT NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  enabled TINYINT NOT NULL DEFAULT 1,
  updated_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  UNIQUE KEY uk_content_writing_prompt_key (prompt_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Seed the three shipped types so operators only edit, never author from zero.
INSERT INTO content_writing_prompts (prompt_key, label, heading, example, instruction, sort_order, enabled, updated_by)
VALUES
  ('design', '设计说明', '方案设计说明',
   '本方案为三层框架结构自建房，占地约120平方米。一层布置客厅、餐厨与老人房，二层为主卧套房与书房。立面采用新闽派风格，坡屋顶配合浅色真石漆，整体与村落肌理协调。',
   '请撰写方案设计说明。面向专业读者，结构清晰，先总体后分项。', 1, 1, 'seed'),
  ('project', '项目介绍', '项目内容介绍',
   '这是位于镇郊的一栋自建房项目，前后院采光充足。设计保留了场地原有大树，入户朝南，室内动线围绕家庭聚餐与老人起居展开，屋顶露台可远眺后山。',
   '请撰写项目内容介绍。面向业主，语言平实易读，讲清项目亮点与生活场景。', 2, 1, 'seed'),
  ('promotion', '推广文案', '作品推广文案',
   '老宅翻新，不拆承重、不动感情。这次改造保留了三十年前的门头石雕，把新生活装进旧骨架。回乡建房的朋友，欢迎来聊聊你家的老宅故事。',
   '请撰写作品推广文案。适合朋友圈与社群转发，短句为主，结尾给出自然的行动引导。', 3, 1, 'seed')
ON DUPLICATE KEY UPDATE prompt_key = prompt_key;
