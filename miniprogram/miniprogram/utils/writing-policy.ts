// @ts-nocheck
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
module.exports = { ANTI_FABRICATION_CLAUSE, noteWritingPrompt };
