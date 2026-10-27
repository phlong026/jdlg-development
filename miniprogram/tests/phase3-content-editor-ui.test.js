'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram/pages');

function loadEditor(apiOverrides = {}) {
  let definition;
  const navigations = [];
  const navigationOptions = [];
  const toasts = [];
  let sessionToken = 'session-token';
  const api = {
    mutationKey: prefix => `${prefix}-key`,
    saveContent: async () => ({ content_id: 'content-one', revision: 2, version: 0, status: 'draft' }),
    previewContent: async () => ({ preview_token: 'preview-one', content: { title: '服务端预览', body_format: 'article_v2', body: '{"schema":1,"blocks":[{"kind":"paragraph","text":"正文"}]}' } }),
    downloadWorkbenchMedia: async () => '',
    ...apiOverrides,
  };
  const wx = {
    getStorageSync: key => key === 'token' ? sessionToken : '',
    navigateTo: options => { navigations.push(options.url); navigationOptions.push(options); },
    showToast: options => toasts.push(options),
    getWindowInfo: () => ({ windowWidth: 375 }),
    createVideoContext: () => ({ pause() {} }),
  };
  vm.runInNewContext(
    fs.readFileSync(path.join(root, 'contentedit/contentedit.js'), 'utf8'),
    {
      Page: page => { definition = page; }, wx, Promise, JSON, Set, Map,
      require: id => id.includes('phase3-api') ? api : {
        preserveLayoutBody: (_layout, fallback) => fallback,
        renderArticleDocument: () => [],
      },
    },
  );
  const page = {
    ...definition,
    data: { ...definition.data },
    _token: 'session-token',
    _epoch: 1,
    _visible: true,
    setData(values) { Object.assign(this.data, values); },
  };
  return { page, navigations, navigationOptions, toasts, token(value) { sessionToken = value; } };
}

test('editor exposes separate draft, preview, publish and published-reader actions', async () => {
  let saves = 0;
  let previews = 0;
  const { page, navigations } = loadEditor({
    saveContent: async () => { saves += 1; return { content_id: 'content-one', revision: 2, version: 0, status: 'draft' }; },
    previewContent: async () => { previews += 1; return { preview_token: 'preview-one', content: { title: '预览', body_format: 'article_v2', body: '{"schema":1,"blocks":[{"kind":"paragraph","text":"正文"}]}' } }; },
  });
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });

  await page.saveDraft();
  assert.equal(saves, 1);
  assert.equal(previews, 0, '保存草稿不应强制进入预览');

  page.setData({ status: 'published', version: 3, id: 'content-one' });
  page.openPublished();
  assert.equal(navigations[0], '/pages/contentreader/contentreader?content_id=content-one&content_version=3');
});

test('AI writing prefill stays account-scoped and never overwrites a saved draft', () => {
  const { page } = loadEditor();
  page.receivePrefill({ title: 'AI 标题', text: 'AI 正文' });
  assert.equal(page.data.title, '', '服务端上下文加载前不能提前写入草稿');

  page._loaded = true;
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a' });
  page.applyPendingPrefill();
  assert.equal(page.data.title, 'AI 标题');
  assert.equal(page.data.blocks[0].text, 'AI 正文');

  page.setData({ title: '', blocks: [{ kind: 'paragraph', text: '' }] });
  page.receivePrefill({ title: '其他账号标题', text: '其他账号正文' }, 'other-session');
  assert.equal(page.data.title, '');

  page.receivePrefill({ title: 'AI 标题', text: 'AI 正文' });
  assert.equal(page.data.title, 'AI 标题');
  assert.equal(page.data.blocks[0].text, 'AI 正文');

  page.setData({ id: 'saved-content', title: '已保存标题', blocks: [{ kind: 'paragraph', text: '已保存正文' }] });
  page.receivePrefill({ title: '迟到标题', text: '迟到正文' });
  assert.equal(page.data.title, '已保存标题');
  assert.equal(page.data.blocks[0].text, '已保存正文');
});

test('new editor opens AI writing with an event return path into the same empty draft', () => {
  const { page, navigationOptions } = loadEditor();
  page._loaded = true;
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a' });

  page.openAIWriting();
  assert.equal(navigationOptions.length, 1);
  assert.equal(navigationOptions[0].url, '/pages/contentwriting/contentwriting?source=editor');
  assert.equal(typeof navigationOptions[0].events.prefillContent, 'function');

  navigationOptions[0].events.prefillContent({ title: '返回原编辑器', text: '同一草稿中的正文' });
  assert.equal(page.data.title, '返回原编辑器');
  assert.equal(page.data.blocks[0].text, '同一草稿中的正文');
  assert.equal(navigationOptions.length, 1, '回填不得再打开第二个编辑器');

  page.setData({ id: 'saved-content', title: '已有标题', blocks: [{ kind: 'paragraph', text: '已有正文' }] });
  navigationOptions[0].events.prefillContent({ title: '迟到标题', text: '迟到正文' });
  assert.equal(page.data.title, '已有标题');
  assert.equal(page.data.blocks[0].text, '已有正文');
});

test('AI writing return appends once without replacing an unchanged unsaved draft', () => {
  const { page, navigationOptions, toasts } = loadEditor();
  page._loaded = true;
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', title: '原有标题', blocks: [{ kind: 'paragraph', text: '原有正文' }] });

  page.openAIWriting();
  const returned = navigationOptions[0].events.prefillContent;
  returned({ title: 'AI 标题', text: '采用的 AI 正文' });
  assert.equal(page.data.title, '原有标题');
  assert.deepEqual(page.data.blocks.map(block => block.text), ['原有正文', '采用的 AI 正文']);

  returned({ title: 'AI 标题', text: '采用的 AI 正文' });
  assert.deepEqual(page.data.blocks.map(block => block.text), ['原有正文', '采用的 AI 正文'], '同一次返回事件不得重复追加');
  assert.match(page.data.notice, /已采用|已处理/);

  page.openAIWriting();
  navigationOptions[1].events.prefillContent({ title: 'AI 标题', text: '采用的 AI 正文' });
  assert.deepEqual(page.data.blocks.map(block => block.text), ['原有正文', '采用的 AI 正文'], '正文中已有同一文案时不得重复追加');

  page.openAIWriting();
  page.setData({ title: '期间已修改标题' });
  navigationOptions[2].events.prefillContent({ title: '新 AI 标题', text: '不应采用' });
  assert.equal(page.data.blocks.some(block => block.text === '不应采用'), false);
  assert.match(page.data.notice, /未采用/);
  assert.ok(toasts.some(toast => /未采用/.test(toast.title)));
});

test('AI writing return explains saved-draft and account-change rejection without overwriting', () => {
  const saved = loadEditor();
  saved.page._loaded = true;
  saved.page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a' });
  saved.page.openAIWriting();
  saved.page.setData({ id: 'saved-content', title: '已保存标题', blocks: [{ kind: 'paragraph', text: '已保存正文' }] });
  saved.navigationOptions[0].events.prefillContent({ title: 'AI 标题', text: '不应覆盖保存内容' });
  assert.equal(saved.page.data.blocks[0].text, '已保存正文');
  assert.match(saved.page.data.notice, /草稿已保存|正在保存/);

  const switched = loadEditor();
  switched.page._loaded = true;
  switched.page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', title: '当前标题', blocks: [{ kind: 'paragraph', text: '当前正文' }] });
  switched.page.openAIWriting();
  switched.token('session-two');
  switched.navigationOptions[0].events.prefillContent({ title: '上一账号标题', text: '上一账号正文' });
  assert.equal(switched.page.data.blocks.length, 1);
  assert.equal(switched.page.data.blocks[0].text, '当前正文');
  assert.match(switched.page.data.notice, /账号已变化/);
});

test('editor and reader implement the selected warm-gold content studio hierarchy', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const editorStyle = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  const readerWxml = fs.readFileSync(path.join(root, 'contentreader/contentreader.wxml'), 'utf8');
  const readerStyle = fs.readFileSync(path.join(root, 'contentreader/contentreader.wxss'), 'utf8');

  assert.match(editorWxml, /class="editor-hero"/);
  assert.match(editorWxml, /bindtap="saveDraft"/);
  assert.match(editorWxml, /bindtap="saveAndPreview"/);
  assert.match(editorWxml, /bindtap="openPublished"/);
  assert.match(editorWxml, /bindtap="openAIWriting"/);
  assert.match(editorStyle, /#a87943/i);
  assert.match(editorStyle, /position:\s*sticky/);

  assert.match(readerWxml, /class="reader-brand"/);
  assert.match(readerWxml, /class="reader-actions"/);
  assert.match(readerWxml, /open-type="share"/);
  assert.match(readerWxml, /bindtap="copyWebLink"/);
  assert.match(readerStyle, /#a87943/i);
  assert.match(readerStyle, /\.reader-actions\s*\{[^}]*position:\s*fixed/);
});
