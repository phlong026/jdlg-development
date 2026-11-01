'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const pagesRoot = path.join(__dirname, '../miniprogram/pages');

function loadPage(name) {
  let definition;
  let token = 'session-one';
  const navigations = [];
  const toasts = [];
  const listeners = {};
  const emitted = [];
  const channel = {
    on(event, handler) { listeners[event] = handler; },
    emit(event, value) { emitted.push({ event, value }); },
  };
  const wx = {
    getStorageSync: key => key === 'token' ? token : '',
    hideShareMenu() {},
    navigateTo(options) { navigations.push(options); },
    navigateBack() {},
    showToast(options) { toasts.push(options); },
    getWindowInfo: () => ({ windowWidth: 375 }),
    createVideoContext: () => ({ pause() {} }),
  };
  vm.runInNewContext(fs.readFileSync(path.join(pagesRoot, name, `${name}.js`), 'utf8'), {
    Page(value) { definition = value; },
    wx,
    Promise,
    JSON,
    Set,
    Map,
    require(id) {
      if (id.includes('phase3-api')) return { writingPrompts: async () => { throw new Error('offline'); } };
      if (id.includes('article-document')) return { preserveLayoutBody: (_layout, fallback) => fallback, renderArticleDocument: () => [] };
      throw new Error(`unexpected require ${id}`);
    },
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values) { Object.assign(this.data, values); },
    getOpenerEventChannel: () => channel,
  };
  return { page, navigations, toasts, listeners, emitted, channel, token(value) { token = value; } };
}

test('a saved draft sends only existing text facts to AI and appends the return without losing media blocks', () => {
  const h = loadPage('contentedit');
  h.page.setData({
    id: 'content-42',
    revision: 7,
    title: '已保存的庭院方案',
    blocks: [
      { kind: 'paragraph', text: '入口朝南' },
      { kind: 'image', media_id: 'private-image', caption: '保留现有大树' },
      { kind: 'list', items: ['不改建围墙', '保留老井'] },
    ],
  });

  h.page.openAIWriting();
  assert.equal(h.navigations.length, 1, '已保存草稿也应能继续 AI 编辑');
  h.navigations[0].success({ eventChannel: h.channel });
  const context = h.emitted.find(item => item.event === 'writingContext').value;
  assert.equal(context.source, 'editor');
  assert.equal(context.content_id, 'content-42');
  assert.equal(context.revision, 7);
  assert.equal(context.title, '已保存的庭院方案');
  assert.match(context.facts, /入口朝南/);
  assert.match(context.facts, /保留现有大树/);
  assert.match(context.facts, /不改建围墙/);
  assert.doesNotMatch(context.facts, /private-image/);

  const returned = h.navigations[0].events.prefillContent;
  returned({ title: '不覆盖原标题', text: 'AI 补充的设计说明' });
  assert.equal(h.page.data.title, '已保存的庭院方案');
  assert.deepEqual(h.page.data.blocks[1], { kind: 'image', media_id: 'private-image', caption: '保留现有大树' });
  assert.equal(h.page.data.blocks.at(-1).text, 'AI 补充的设计说明');
  returned({ title: '不覆盖原标题', text: 'AI 补充的设计说明' });
  assert.equal(h.page.data.blocks.filter(block => block.text === 'AI 补充的设计说明').length, 1);
});

test('saved-draft AI return keeps account, revision, busy and original-layout guards', () => {
  const changed = loadPage('contentedit');
  changed.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  changed.page.openAIWriting();
  changed.page.setData({ revision: 8 });
  changed.navigations[0].events.prefillContent({ text: '不应回填' });
  assert.equal(changed.page.data.blocks.length, 1);
  assert.match(changed.page.data.notice, /变化|版本/);

  const busy = loadPage('contentedit');
  busy.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  busy.page.openAIWriting();
  busy.page.setData({ busy: true });
  busy.navigations[0].events.prefillContent({ text: '不应回填' });
  assert.equal(busy.page.data.blocks.length, 1);
  assert.match(busy.page.data.notice, /保存|处理/);

  const readOnly = loadPage('contentedit');
  readOnly.page.setData({ id: 'content-layout', layoutReadOnly: true });
  readOnly.page.openAIWriting();
  assert.equal(readOnly.navigations.length, 0);

  const switched = loadPage('contentedit');
  switched.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  switched.page.openAIWriting();
  switched.token('session-two');
  switched.navigations[0].events.prefillContent({ text: '不应回填' });
  assert.equal(switched.page.data.blocks.length, 1);
  assert.match(switched.page.data.notice, /账号/);
});

test('writing page receives editor context through EventChannel and returns to the same draft', () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'editor' });
  h.listeners.writingContext({ source: 'editor', content_id: 'content-42', revision: 7, title: '原标题', facts: '只有这些已知事实' });
  assert.equal(h.page.data.title, '原标题');
  assert.equal(h.page.data.facts, '只有这些已知事实');
  h.page.setData({ result: '用户核对后的文案' });
  h.page.saveToDraft();
  const returned = h.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(returned.text, '用户核对后的文案');
  assert.equal(returned.content_id, 'content-42');
  assert.equal(returned.revision, 7);
});

test('writing page returns explicit project-note identity through the same EventChannel', () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'project_note' });
  assert.equal(typeof h.listeners.writingContext, 'function');
  h.listeners.writingContext({
    source: 'project_note',
    project_id: 'project-8',
    note_id: 'note-12',
    note_revision: 4,
    title: '业主沟通纪要',
    facts: '业主明确要求保留东侧窗洞。',
  });
  assert.equal(h.page.data.title, '业主沟通纪要');
  assert.equal(h.page.data.facts, '业主明确要求保留东侧窗洞。');
  assert.match(h.page.data.returnLabel, /项目笔记/);

  h.page.editResult({ detail: { value: '经人工核对的沟通文案' } });
  assert.match(h.page.data.notice, /项目笔记/);
  h.page.saveToDraft();
  assert.equal(h.navigations.length, 0, '采纳项目笔记文案不应打开第二个内容编辑器');
  const returned = h.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(returned.source, 'project_note');
  assert.equal(returned.project_id, 'project-8');
  assert.equal(returned.note_id, 'note-12');
  assert.equal(returned.note_revision, 4);
  assert.equal(returned.title, '业主沟通纪要');
  assert.equal(returned.text, '经人工核对的沟通文案');
});

test('saved editable drafts expose AI writing while original-layout content stays read-only', () => {
  const wxml = fs.readFileSync(path.join(pagesRoot, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /class="text-action ai-action" bindtap="openAIWriting"/);
  assert.doesNotMatch(wxml, /wx:if="\{\{!id\}\}" class="text-action ai-action"/);
  assert.match(wxml, /<block wx:if="\{\{!layoutReadOnly\}\}">/);
});

test('AI writing navigation failure keeps the draft and reports a retryable error', () => {
  const h = loadPage('contentedit');
  h.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  h.page.openAIWriting();
  h.navigations[0].fail();
  assert.equal(h.page.data.blocks.length, 1);
  assert.equal(h.page.data.blocks[0].text, '原文');
  assert.match(h.page.data.notice, /无法打开|重试/);
});
