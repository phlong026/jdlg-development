'use strict';
// AI 写作的统一防臆造规则：所有写作入口共用，避免各页面提示漂移。
const ANTI_FABRICATION_CLAUSE = '只根据以下已知资料写作；不得编造面积、层数、造价、工期、地名、项目状态、业主评价或其他未提供的事实。缺失的信息请省略。';
// 笔记编辑页的就地写作 Prompt：与 contentwriting 共用同一防臆造条款，保留标题主题。
function noteWritingPrompt(options) {
  const title = String(options && options.title || '').trim();
  const facts = String(options && options.facts || '').trim();
  const extra = String(options && options.extra || '').trim();
  return ['请撰写或补全这份设计说明笔记的正文，可直接插入编辑中的笔记。', ANTI_FABRICATION_CLAUSE,
    title ? '主题：' + title : '', '已知资料与要求：\n' + facts, extra ? '额外要求：' + extra : '输出可编辑的正文。'].filter(Boolean).join('\n\n');
}
function socialIntroPrompt(options) {
  const presentation = options && options.presentation === 'video' ? '视频笔记' : '图文笔记';
  const title = String(options && options.title || '').trim();
  const description = String(options && options.description || '').trim();
  const captions = String(options && options.captions || '').trim();
  const mediaSummary = String(options && options.media_summary || '').trim();
  return [`请为这篇${presentation}撰写一段简洁、自然、可直接编辑的介绍。`, ANTI_FABRICATION_CLAUSE,
    '你无法看到图片或视频画面；不得猜测或描述用户没有写明的视觉内容。', title ? '标题：' + title : '',
    description ? '已有介绍：\n' + description : '', captions ? '素材短注释：\n' + captions : '',
    mediaSummary ? '可确认的素材信息：' + mediaSummary : '', '只输出介绍正文，不要输出标题、字段名、Markdown 或解释。'].filter(Boolean).join('\n\n');
}
module.exports = { ANTI_FABRICATION_CLAUSE, noteWritingPrompt, socialIntroPrompt };
