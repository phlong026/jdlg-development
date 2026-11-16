'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { spawnSync } = require('node:child_process');

const root = path.join(__dirname, '../miniprogram/pages');
const wxmlCompiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';

function loadEditor(apiOverrides = {}) {
  let definition;
  const navigations = [];
  const navigationOptions = [];
  const toasts = [];
  const modals = [], backs = [];
  const actionSheets = [];
  const mediaChoosers = [];
  const clipboards = [], mediaPreviews = [], scrolls = [];
  let aiStreamOptions = null;
  let sessionToken = 'session-token';
  const api = {
    mutationKey: prefix => `${prefix}-key`,
    saveContent: async () => ({ content_id: 'content-one', revision: 2, version: 0, status: 'draft' }),
    previewContent: async () => ({ preview_token: 'preview-one', content: { title: '服务端预览', body_format: 'article_v2', body: '{"schema":1,"blocks":[{"kind":"paragraph","text":"正文"}]}' } }),
    downloadWorkbenchMedia: async () => '',
    ...apiOverrides,
  };
  const wx = {
    hideShareMenu() {},
    getStorageSync: key => key === 'token' ? sessionToken : '',
    navigateTo: options => { navigations.push(options.url); navigationOptions.push(options); },
    showToast: options => toasts.push(options),
    showModal: options => modals.push(options),
    navigateBack: options => backs.push(options),
    showActionSheet: options => actionSheets.push(options),
    chooseMedia: options => mediaChoosers.push(options),
    setClipboardData: options => { clipboards.push(options.data); options.success(); },
    previewMedia: options => mediaPreviews.push(options),
    getWindowInfo: () => ({ windowWidth: 375 }),
    createVideoContext: () => ({ pause() {} }),
    pageScrollTo: options => scrolls.push(options),
  };
  vm.runInNewContext(
    fs.readFileSync(path.join(root, 'contentedit/contentedit.js'), 'utf8'),
    {
      Page: page => { definition = page; }, wx, Promise, JSON, Set, Map,
      require: id => {
        if (id.includes('phase3-api')) return api;
        if (id.includes('ai-stream')) return { startAIStream: options => { aiStreamOptions = options; return { abort() {} }; } };
        if (id.includes('writing-policy')) return require(path.join(__dirname, '../miniprogram/utils/writing-policy.js'));
        return { preserveLayoutBody: (_layout, fallback) => fallback, renderArticleDocument: () => [] };
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
  return { page, navigations, navigationOptions, toasts, modals, backs, actionSheets, mediaChoosers, clipboards, mediaPreviews, scrolls, aiStream: () => aiStreamOptions, token(value) { sessionToken = value; } };
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

test('inline AI streaming cannot save preview or publish a partial introduction', async () => {
  let saves = 0, previews = 0, publishes = 0;
  const { page } = loadEditor({
    saveContent: async () => { saves += 1; return { content_id: 'content-one', revision: 2, version: 0, status: 'draft' }; },
    previewContent: async () => { previews += 1; return { preview_token: 'preview-one' }; },
    publishContent: async () => { publishes += 1; return {}; },
  });
  page.setData({ presentation: 'gallery', title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'image', media_id: 'image-one' }, { kind: 'paragraph', text: 'AI 半截内容' }], aiInlineStreaming: true });
  await page.saveDraft();
  await page.saveAndPreview();
  await page.publish();
  assert.equal(saves, 0);
  assert.equal(previews, 0);
  assert.equal(publishes, 0);
});

test('inline AI streaming disables and debounces every destructive management action', () => {
  const { page, modals } = loadEditor();
  page.setData({ id: 'content-one', revision: 2, version: 1, status: 'published', pinned: false, moreExpanded: false, aiInlineStreaming: true });
  page.toggleMore();
  page.togglePinned();
  page.revoke();
  page.setData({ version: 0, status: 'draft' });
  page.deleteDraft();
  assert.equal(page.data.moreExpanded, false);
  assert.equal(page.data.pinned, false);
  assert.equal(modals.length, 0);

  page.setData({ aiInlineStreaming: false, version: 1, status: 'published' });
  page.revoke();
  page.setData({ aiInlineStreaming: true });
  modals[0].success({ confirm: true, content: '测试撤回' });
  assert.equal(page.data.busy, false, '弹窗打开后开始生成也不得撤回');

  page.setData({ aiInlineStreaming: false, version: 0, status: 'draft' });
  page.deleteDraft();
  page.setData({ aiInlineStreaming: true });
  modals[1].success({ confirm: true });
  assert.equal(page.data.busy, false, '弹窗打开后开始生成也不得删除');

  const wxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /class="plain-action editor-more"[\s\S]*disabled="\{\{busy \|\| aiInlineStreaming \|\| aiInlineConflict\}\}"/);
  assert.match(wxml, /bindtap="revoke" disabled="\{\{busy \|\| aiInlineStreaming \|\| aiInlineConflict\}\}"/);
  assert.match(wxml, /bindtap="togglePinned" disabled="\{\{busy \|\| aiInlineStreaming \|\| aiInlineConflict\}\}"/);
  assert.match(wxml, /bindtap="deleteDraft" disabled="\{\{busy \|\| aiInlineStreaming \|\| aiInlineConflict\}\}"/);
});

test('inline AI conflict also blocks revoke and delete at markup and handler boundaries', async () => {
  let revokes = 0, deletes = 0;
  const { page, modals } = loadEditor({
    revokeContent: async () => { revokes += 1; return {}; },
    deleteContentDraft: async () => { deletes += 1; return {}; },
  });
  page.setData({ id: 'content-one', revision: 2, version: 1, status: 'published', aiInlineConflict: true });
  page.revoke();
  await page.revokeContent('原因');
  page.setData({ version: 0, status: 'draft' });
  page.deleteDraft();
  await page.deleteDraftConfirmed();
  assert.equal(modals.length, 0);
  assert.equal(revokes, 0);
  assert.equal(deletes, 0);
  const wxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /bindtap="revoke" disabled="\{\{busy \|\| aiInlineStreaming \|\| aiInlineConflict\}\}"/);
  assert.match(wxml, /bindtap="deleteDraft" disabled="\{\{busy \|\| aiInlineStreaming \|\| aiInlineConflict\}\}"/);
});

test('publish saves the latest draft, requests a current preview token, then publishes without opening preview', async () => {
  const calls = [];
  const { page, navigations } = loadEditor({
    saveContent: async draft => { calls.push(['save', draft.title]); return { content_id: 'content-one', revision: 2, version: 0, status: 'draft' }; },
    previewContent: async (id, revision) => { calls.push(['preview', id, revision]); return { preview_token: 'preview-current', content: { title: '庭院方案', body_format: 'article_v2', body: '{"schema":1,"blocks":[{"kind":"paragraph","text":"正文"}]}' } }; },
    publishContent: async (id, revision, token) => { calls.push(['publish', id, revision, token]); return { content: { content_id: id, revision: 3, version: 1, status: 'published' } }; },
  });
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });

  await page.publish();

  assert.deepEqual(calls, [
    ['save', '庭院方案'],
    ['preview', 'content-one', 2],
    ['publish', 'content-one', 2, 'preview-current'],
  ]);
  assert.equal(page.data.status, 'published');
  assert.equal(page.data.preview, null, '发布完不应强制渲染预览页');
  assert.equal(navigations.length, 0, '阅读页跳转由现有延迟逻辑触发');
});

test('publish stops after save when the user changes the draft during the request', async () => {
  let finishSave;
  let previews = 0;
  const { page } = loadEditor({
    saveContent: () => new Promise(resolve => { finishSave = resolve; }),
    previewContent: async () => { previews += 1; return { preview_token: 'must-not-run' }; },
  });
  page.setData({ title: '原标题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });
  const pending = page.publish();
  page.setData({ title: '保存期间改了标题' });
  finishSave({ content_id: 'content-one', revision: 2, version: 0, status: 'draft' });
  await pending;
  assert.equal(previews, 0);
  assert.match(page.data.notice, /未保存修改/);
});

test('an uncertain publish retry reuses the saved revision and preview token instead of creating another draft revision', async () => {
  let saves = 0, previews = 0, publications = 0;
  const { page } = loadEditor({
    saveContent: async () => { saves += 1; return { content_id: 'content-one', revision: 2, version: 0, status: 'draft' }; },
    previewContent: async () => { previews += 1; return { preview_token: 'preview-current' }; },
    publishContent: async id => {
      publications += 1;
      if (publications === 1) throw new Error('网络超时，结果待确认');
      return { content: { content_id: id, revision: 3, version: 1, status: 'published' } };
    },
  });
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });

  await page.publish();
  assert.match(page.data.error, /结果待确认/);
  await page.publish();

  assert.equal(saves, 1);
  assert.equal(previews, 1);
  assert.equal(publications, 2);
  assert.equal(page.data.status, 'published');
});

test('a 409 publish conflict discards the checkpoint and asks the next retry to validate again', async () => {
  let saves = 0, previews = 0, publications = 0;
  const { page } = loadEditor({
    saveContent: async () => { saves += 1; return { content_id: 'content-one', revision: 2, version: 0, status: 'draft' }; },
    previewContent: async () => { previews += 1; return { preview_token: `preview-${previews}` }; },
    publishContent: async id => {
      publications += 1;
      if (publications === 1) { const error = new Error('预览已过期'); error.statusCode = 409; throw error; }
      return { content: { content_id: id, revision: 3, version: 1, status: 'published' } };
    },
  });
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });

  await page.publish();
  assert.match(page.data.error, /校验已过期/);
  await page.publish();

  assert.equal(saves, 2);
  assert.equal(previews, 2);
  assert.equal(publications, 2);
});

test('a 409 from an opened preview clears its stale token so retry saves and validates again', async () => {
  const calls = [];
  let publications = 0;
  const { page } = loadEditor({
    saveContent: async () => { calls.push('save'); return { content_id: 'content-one', revision: 3, version: 0, status: 'draft' }; },
    previewContent: async (id, revision) => { calls.push(['preview', id, revision]); return { preview_token: 'preview-fresh' }; },
    publishContent: async (id, revision, token) => {
      calls.push(['publish', id, revision, token]);
      publications += 1;
      if (publications === 1) { const error = new Error('预览已过期'); error.statusCode = 409; throw error; }
      return { content: { content_id: id, revision: 4, version: 1, status: 'published' } };
    },
  });
  page.setData({
    id: 'content-one', revision: 2, title: '庭院方案', owner_id: 'owner-a',
    blocks: [{ kind: 'paragraph', text: '正文' }],
    preview: { preview_token: 'preview-stale' }, previewBlocks: [{ kind: 'paragraph', text: '正文' }],
    previewCover: { media_id: 'cover-stale' }, previewLayoutBlocks: [{ kind: 'layout' }], previewLayoutReadOnly: true,
  });

  await page.publish();
  assert.equal(page.data.preview, null, '过期的可选预览不能继续被发布重试复用');
  assert.equal(page.data.previewBlocks.length, 0);
  assert.equal(page.data.previewCover, null);
  assert.equal(page.data.previewLayoutBlocks.length, 0);
  assert.equal(page.data.previewLayoutReadOnly, false);

  await page.publish();

  assert.deepEqual(calls, [
    ['publish', 'content-one', 2, 'preview-stale'],
    'save',
    ['preview', 'content-one', 3],
    ['publish', 'content-one', 3, 'preview-fresh'],
  ]);
  assert.equal(page.data.status, 'published');
});

test('edits made after the publish request stay unsaved and do not get mistaken for the published snapshot', async () => {
  let finishPublish;
  const { page, navigations, toasts } = loadEditor({
    saveContent: async () => ({ content_id: 'content-one', revision: 2, version: 0, status: 'draft' }),
    previewContent: async () => ({ preview_token: 'preview-current' }),
    publishContent: () => new Promise(resolve => { finishPublish = resolve; }),
  });
  page.setData({ title: '请求时标题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '请求时正文' }] });
  const pending = page.publish();
  await new Promise(resolve => setImmediate(resolve));
  page.setData({ title: '发布请求后新改的标题' });
  finishPublish({ content: { content_id: 'content-one', revision: 3, version: 1, status: 'published' } });
  await pending;

  assert.equal(page.data.status, 'published', '服务端真实成功必须反映');
  assert.equal(page.data.title, '发布请求后新改的标题', '新输入不得丢失');
  assert.match(page.data.notice, /已发布.*未保存修改/);
  assert.notEqual(page._savedSignature, page.draftSignature(), '当前新输入不得被标记为已发布');
  assert.equal(navigations.length, 0, '有新修改时不自动跳走');
  page.openPublished();
  assert.match(toasts.at(-1).title, /尚未发布/);
});

test('returning from preview keeps edits and forces direct publish to request a new token', async () => {
  let publications = 0;
  let previews = 0;
  const { page } = loadEditor({
    previewContent: async () => { previews += 1; return { preview_token: 'fresh-preview' }; },
    publishContent: async id => { publications += 1; return { content: { content_id: id, revision: 3, version: 1, status: 'published' } }; },
  });
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '保留正文' }], preview: { preview_token: 'old-preview' }, previewBlocks: [{ text: '旧预览' }] });
  page.returnToEdit();
  assert.equal(page.data.title, '庭院方案');
  assert.equal(page.data.blocks[0].text, '保留正文');
  assert.equal(page.data.preview, null);
  assert.equal(page.data.previewBlocks.length, 0);
  await page.publish();
  assert.equal(previews, 1);
  assert.equal(publications, 1);
});

test('new content type opens its chooser once after authorization without triggering for saved content', () => {
  const { page } = loadEditor();
  let selections = 0;
  page.chooseMedia = () => { selections += 1; };
  page.onLoad({ action: 'media' });
  page.performCreateAction();
  assert.equal(selections, 0);
  page._loaded = true;
  page.setData({ context: { enabled: true, can_manage: true, media_upload_configured: true } });
  page.performCreateAction();
  page.performCreateAction();
  assert.equal(selections, 1);
  page.onLoad({ id: 'saved-content', action: 'media' });
  page.performCreateAction();
  assert.equal(selections, 1);
});

test('editor back preserves a preview before considering page exit', () => {
  const { page } = loadEditor();
  page.setData({ preview: { preview_token: 'preview' }, title: '未退出' });
  page.handleBack();
  assert.equal(page.data.preview, null);
  assert.equal(page.data.title, '未退出');
});

test('back warns about edits made while a new draft was being saved', async () => {
  let finish;
  const saved = new Promise(resolve => { finish = resolve; });
  const { page, modals, backs } = loadEditor({ saveContent: () => saved });
  page.setData({ title: '提交时标题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '提交正文' }] });
  const saving = page.saveDraft();
  page.setField({ currentTarget: { dataset: { field: 'title' } }, detail: { value: '保存期间继续输入' } });
  finish({ content_id: 'new-content', revision: 1, version: 0, status: 'draft' });
  await saving;
  page.handleBack();
  assert.equal(backs.length, 0);
  assert.equal(modals.length, 1);
  modals[0].success({ confirm: false });
  assert.equal(backs.length, 0);
  assert.equal(page.data.title, '保存期间继续输入');
});

test('back exits a saved draft without a redundant confirmation', async () => {
  const { page, modals, backs } = loadEditor();
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });
  await page.saveDraft();
  page.handleBack();
  assert.equal(modals.length, 0);
  assert.equal(backs.length, 1);
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

test('new article editor generates AI text in the same draft without opening another page', () => {
  const editor = loadEditor();
  const { page, navigationOptions } = editor;
  page._loaded = true;
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', presentation: 'article', title: '返回原编辑器' });

  page.openAIWriting();
  assert.equal(navigationOptions.length, 0);
  assert.ok(editor.aiStream());
  editor.aiStream().onDone('同一草稿中的正文');
  assert.equal(page.data.title, '返回原编辑器');
  assert.equal(page.data.blocks[0].text, '同一草稿中的正文');
});

test('inline article AI appends to a full draft and restores the exact original blocks', () => {
  const editor = loadEditor();
  const { page, navigationOptions } = editor;
  page._loaded = true;
  const original = [{ kind: 'heading', level: 2, text: '原有小标题' }, { kind: 'paragraph', text: '原有正文' }];
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', presentation: 'article', title: '原有标题', blocks: original });

  page.openAIWriting();
  editor.aiStream().onDone('采用的 AI 正文');
  assert.equal(page.data.title, '原有标题');
  assert.deepEqual(page.data.blocks.map(block => block.text), ['原有小标题', '原有正文', '采用的 AI 正文']);
  page.restoreInlineAIOriginal();
  assert.equal(JSON.stringify(page.data.blocks), JSON.stringify(original));
  assert.equal(navigationOptions.length, 0);
});

test('inline article AI rejects late revision and account results without overwriting', () => {
  const saved = loadEditor();
  saved.page._loaded = true;
  saved.page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', presentation: 'article', title: '标题', revision: 1, blocks: [{ kind: 'paragraph', text: '原正文' }] });
  saved.page.openAIWriting();
  saved.page.setData({ revision: 2 });
  saved.aiStream().onDone('不应覆盖保存内容');
  assert.equal(saved.page.data.blocks[0].text, '原正文');

  const switched = loadEditor();
  switched.page._loaded = true;
  switched.page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', presentation: 'article', title: '当前标题', blocks: [{ kind: 'paragraph', text: '当前正文' }] });
  switched.page.openAIWriting();
  switched.token('session-two');
  switched.aiStream().onDone('上一账号正文');
  assert.equal(switched.page.data.blocks.length, 1);
  assert.equal(switched.page.data.blocks[0].text, '当前正文');
});

test('block insert menu offers heading, quote and list blocks that serialize correctly', () => {
  const { page, actionSheets } = loadEditor();
  page.showInsertMenu();
  assert.equal(actionSheets.length, 1, '插入块应通过动作面板提供全部文本块类型');
  actionSheets[0].success({ tapIndex: 1 });
  assert.equal(page.data.blocks[page.data.blocks.length - 1].kind, 'heading');

  page.insertBlock('quote');
  page.insertBlock('list-ordered');
  const blocks = page.data.blocks;
  assert.equal(blocks[blocks.length - 2].kind, 'quote');
  assert.equal(blocks[blocks.length - 1].kind, 'list');
  assert.equal(blocks[blocks.length - 1].ordered, true);

  page.setData({ title: '块类型方案', owner_id: 'owner-a', blocks: [
    { kind: 'heading', level: 2, text: '户型布局' },
    { kind: 'quote', text: '业主希望保留老宅门头' },
    { kind: 'list', ordered: true, items: ['一层客厅', '二层主卧'] },
    { kind: 'paragraph', text: '正文' },
  ] });
  const input = page.draftInput();
  assert.ok(input, '六种块的草稿应可提交');
  const doc = JSON.parse(input.draft.body);
  assert.equal(doc.blocks[0].kind, 'heading');
  assert.equal(doc.blocks[0].level, 2);
  assert.equal(doc.blocks[1].kind, 'quote');
  assert.equal(doc.blocks[2].kind, 'list');
  assert.equal(doc.blocks[2].ordered, true);
  assert.deepEqual(doc.blocks[2].items, ['一层客厅', '二层主卧']);
});

test('heading blocks switch between H2 and H3 and text blocks convert without losing text', () => {
  const { page } = loadEditor();
  page.setData({ blocks: [{ kind: 'paragraph', text: '南立面设计说明' }] });
  page.convertBlock({ currentTarget: { dataset: { index: 0, to: 'heading' } } });
  assert.equal(page.data.blocks[0].kind, 'heading');
  assert.equal(page.data.blocks[0].level, 2);
  assert.equal(page.data.blocks[0].text, '南立面设计说明');

  page.setHeadingLevel({ currentTarget: { dataset: { index: 0, level: '3' } } });
  assert.equal(page.data.blocks[0].level, 3);

  page.convertBlock({ currentTarget: { dataset: { index: 0, to: 'quote' } } });
  assert.equal(page.data.blocks[0].kind, 'quote');
  assert.equal(page.data.blocks[0].text, '南立面设计说明');

  page.convertBlock({ currentTarget: { dataset: { index: 0, to: 'paragraph' } } });
  assert.equal(page.data.blocks[0].kind, 'paragraph');
  assert.equal(page.data.blocks[0].text, '南立面设计说明');
});

test('list blocks support item editing, add, remove and ordered toggle', () => {
  const { page, toasts } = loadEditor();
  page.setData({ blocks: [{ kind: 'list', ordered: false, items: [''] }] });

  page.editListItem({ currentTarget: { dataset: { index: 0, itemIndex: 0 } }, detail: { value: '一层客厅' } });
  page.addListItem({ currentTarget: { dataset: { index: 0 } } });
  page.editListItem({ currentTarget: { dataset: { index: 0, itemIndex: 1 } }, detail: { value: '二层主卧' } });
  assert.deepEqual([...page.data.blocks[0].items], ['一层客厅', '二层主卧']);

  page.removeListItem({ currentTarget: { dataset: { index: 0, itemIndex: 0 } } });
  assert.deepEqual([...page.data.blocks[0].items], ['二层主卧']);

  page.removeListItem({ currentTarget: { dataset: { index: 0, itemIndex: 0 } } });
  assert.deepEqual([...page.data.blocks[0].items], [''], '最后一项删除后保留空位而不是空列表');

  page.toggleListOrdered({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(page.data.blocks[0].ordered, true);

  page.setData({ blocks: [{ kind: 'list', ordered: false, items: Array.from({ length: 100 }, () => '项') }] });
  page.addListItem({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(page.data.blocks[0].items.length, 100, '列表项不得超过服务端上限 100');
  assert.ok(toasts.some(toast => /100/.test(toast.title)));
});

test('image and video blocks expose caption editing that survives serialization', () => {
  const { page } = loadEditor();
  page.setData({ title: '图注方案', owner_id: 'owner-a', blocks: [{ kind: 'image', media_id: 'media-1', caption: '' }] });
  page.editCaption({ currentTarget: { dataset: { index: 0 } }, detail: { value: '南立面效果图' } });
  assert.equal(page.data.blocks[0].caption, '南立面效果图');

  const input = page.draftInput();
  const doc = JSON.parse(input.draft.body);
  assert.equal(doc.blocks[0].caption, '南立面效果图');
  assert.equal(doc.blocks[0].media_id, 'media-1');
});

test('multi-select media uploads serially and batches state updates with failure tally', async () => {
  const uploaded = [];
  let calls = 0;
  const { page, mediaChoosers } = loadEditor({
    uploadWorkbenchMedia: async (filePath, kind) => {
      calls += 1;
      if (calls === 2) throw new Error('第二个文件上传失败');
      uploaded.push({ filePath, kind });
      return { media_id: `media-${calls}`, kind };
    },
  });
  page.setData({ context: { enabled: true, can_manage: true, media_upload_configured: true }, owner_id: 'owner-a', rights: 'own', blocks: [] });

  page.chooseMedia();
  assert.equal(mediaChoosers.length, 1, 'chooseMedia 应仍由按钮触发');
  assert.equal(mediaChoosers[0].count, 9, '相册应放开多选（对齐黑谷 9 张）');
  mediaChoosers[0].success({ type: 'image', tempFiles: [
    { tempFilePath: 'tmp://one.jpg', fileType: 'image' },
    { tempFilePath: 'tmp://two.jpg', fileType: 'image' },
    { tempFilePath: 'tmp://three.jpg', fileType: 'image' },
  ] });
  await new Promise(resolve => setImmediate(resolve));

  assert.equal(calls, 3, '三个文件都应逐个尝试上传');
  assert.equal(uploaded.length, 2);
  assert.equal(page.data.media.length, 2, '失败的文件不进入素材列表');
  assert.deepEqual(page.data.blocks.map(block => block.kind), ['image', 'image']);
  assert.equal(page.data.busy, false, '批量结束后释放 busy');
  assert.match(page.data.notice, /2.*1/);
  assert.ok(page.data.notice.indexOf('失败') >= 0);
});

test('video chooser consumes the top-level media type and reports cancel or service upgrade clearly', async () => {
  const { page, mediaChoosers } = loadEditor();
  page._token = 'session-token';
  page._epoch = 3;
  page.setData({ presentation: 'video', context: { media_upload_configured: true, video_resumable_upload: true }, owner_id: 'owner-a', rights: 'own' });
  let upload;
  page.uploadMediaBatch = (files, type) => { upload = { files, type }; };
  page.chooseMedia();
  mediaChoosers[0].success({ type: 'video', tempFiles: [{ tempFilePath: 'tmp://video.mp4', size: 99 }] });
  assert.equal(upload.type, 'video');
  assert.equal(upload.files[0].fileType, undefined, '不得依赖 MediaFile 上不存在的 fileType');

  page.chooseMedia();
  mediaChoosers[1].fail({ errMsg: 'chooseMedia:fail cancel' });
  assert.match(page.data.notice, /取消.*原内容/);

  page.setData({ context: { media_upload_configured: true, video_resumable_upload: false } });
  page.chooseMedia();
  assert.equal(page.data.error, '当前版本暂不支持本地上传，请从素材库选择视频');
  assert.equal(mediaChoosers.length, 2, '服务未升级时不应打开本地视频选择器');
});

test('native media chooser ignores success after account or content ownership changes', () => {
  const { page, mediaChoosers, token } = loadEditor();
  let uploads = 0;
  page.uploadMediaBatch = () => { uploads += 1; };
  page.setData({ id: 'draft-one', presentation: 'video', context: { media_upload_configured: true, video_resumable_upload: true }, owner_id: 'owner-a', rights: 'own' });

  page.chooseMedia();
  token('account-two');
  mediaChoosers[0].success({ type: 'video', tempFiles: [{ tempFilePath: 'tmp://old-account.mp4' }] });
  assert.equal(uploads, 0);

  token('session-token');
  page.chooseMedia();
  page.setData({ owner_id: 'owner-b' });
  mediaChoosers[1].success({ type: 'video', tempFiles: [{ tempFilePath: 'tmp://old-owner.mp4' }] });
  assert.equal(uploads, 0);

  page.setData({ owner_id: 'owner-a' });
  page.chooseMedia();
  page._epoch += 1;
  mediaChoosers[2].success({ type: 'video', tempFiles: [{ tempFilePath: 'tmp://same-account.mp4' }] });
  assert.equal(uploads, 1, '同账号返回时应使用回调当下 epoch，不被 onShow 误丢弃');
});

test('video owner stays available in content settings and validation opens the actionable fields', () => {
  const wxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.doesNotMatch(wxml, /video-workspace-head/);
  assert.match(wxml, /wx:if="\{\{fieldErrors\.owner_id\}\}" class="field-error"/);
  assert.match(wxml, /wx:if="\{\{context\.owner_choices\.length > 1\}\}"[^>]*class="field"[\s\S]*bindchange="setOwner"/);

  const { page, scrolls } = loadEditor();
  page.setData({ presentation: 'video', context: { media_upload_configured: true, video_resumable_upload: true, owner_choices: [{ owner_id: 'one' }, { owner_id: 'two' }] }, owner_id: '' });
  page.chooseMedia();
  assert.equal(page.data.settingsExpanded, true);
  assert.equal(page.data['fieldErrors.owner_id'], '请选择内容归属');
  assert.equal(scrolls.at(-1).scrollTop, 0);

  page.setData({ owner_id: 'one', title: '', blocks: [], context: { presentation_formats: ['video'], video_editor_fields: true } });
  assert.equal(page.draftInput(), null);
  assert.equal(page.data.fieldErrors.video, '请添加主视频');
  assert.equal(scrolls.at(-1).scrollTop, 0);
});

test('empty gallery exposes AI assistance and both social editors use one compact copy group', () => {
  const wxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const style = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  assert.match(wxml, /presentation === 'gallery'[\s\S]*class="editor-copy-group gallery-copy-group"[\s\S]*bindtap="openAIWriting"[\s\S]*AI辅助/);
  assert.match(wxml, /class="editor-copy-group video-copy-group"/);
  assert.match(wxml, /class="compact-intro"/);
  assert.match(style, /\.compact-intro\s*\{[^}]*min-height:\s*72rpx[^}]*height:\s*72rpx/);
  assert.doesNotMatch(wxml, /class="panel video-title-card"/);
  assert.doesNotMatch(wxml, /class="copy-card video-copy-card"[^>]*>\s*<text class="field-label">视频介绍/);
});

test('video copy actions copy only the requested generated field and explain empty copy', () => {
  const { page, clipboards, toasts } = loadEditor();
  page.setData({ shareCopy: '转发这条视频', momentsCopy: '' });
  page.copyVideoField({ currentTarget: { dataset: { field: 'shareCopy' } } });
  assert.deepEqual(clipboards, ['转发这条视频']);
  page.copyVideoField({ currentTarget: { dataset: { field: 'momentsCopy' } } });
  assert.match(toasts.at(-1).title, /还没有可复制/);
});

test('batch upload aborts without state changes when the page token changes mid-flight', async () => {
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const { page, token } = loadEditor({
    uploadWorkbenchMedia: async () => { await gate; return { media_id: 'media-x', kind: 'image' }; },
  });
  page.setData({ context: { enabled: true, can_manage: true, media_upload_configured: true }, owner_id: 'owner-a', rights: 'own', blocks: [] });
  const pending = page.uploadMediaBatch([{ tempFilePath: 'tmp://one.jpg', fileType: 'image' }]);
  token('another-session');
  release();
  await pending;
  assert.equal(page.data.media.length, 0, '账号切换后不得再写入旧会话的上传结果');
  assert.equal(page.data.busy, false);
});

test('editor wxml wires the new block editing affordances', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(editorWxml, /bindtap="showInsertMenu"/);
  assert.match(editorWxml, /bindinput="editListItem"/);
  assert.match(editorWxml, /bindinput="editCaption"/);
  assert.match(editorWxml, /bindtap="toggleListOrdered"/);
  assert.match(editorWxml, /bindtap="setHeadingLevel"/);
  assert.match(editorWxml, /bindtap="convertBlock"/);
  assert.match(editorWxml, /bindtap="addListItem"/);
  const editorStyle = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  assert.match(editorStyle, /\.list-editor|\.list-item-row/);
  assert.match(editorStyle, /\.caption-input/);
});

test('gallery uses compact thumbnails with one selected-image toolbar and video options stay folded', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const editorStyle = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  assert.match(editorWxml, /class="gallery-thumbs"/);
  assert.match(editorWxml, /class="gallery-selected-tools"/);
  assert.doesNotMatch(editorWxml, /class="editor-gallery"/, '有图后不再占用一屏显示大图编辑区');
  assert.doesNotMatch(editorWxml, /wx:for="\{\{imageBlocks\}\}"[^>]*class="gallery-item"/);
  assert.match(editorStyle, /\.gallery-thumbs/);
  assert.match(editorWxml, />分享文案</);
  assert.match(editorWxml, />显示设置</);
  assert.doesNotMatch(editorWxml, /class="video-layout-card"/);
});

test('gallery keeps one compact action row and moves optional captions into more settings', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const toolbarStart = editorWxml.indexOf('class="gallery-selected-tools"');
  const toolbarEnd = editorWxml.indexOf('</view>', editorWxml.indexOf('</view>', toolbarStart) + 1) + 7;
  const toolbar = editorWxml.slice(toolbarStart, toolbarEnd);
  assert.match(toolbar, /bindtap="setBlockCover"/);
  assert.match(toolbar, /bindtap="sortSelectedGalleryImage"[^>]*>排序</);
  assert.match(toolbar, /bindtap="removeBlock"[^>]*>删除</);
  assert.doesNotMatch(toolbar, /editCaption|前移|后移/);
  assert.match(editorWxml, /settingsExpanded[\s\S]*presentation === 'gallery' && selectedImageBlock[\s\S]*bindinput="editCaption"/);
});

test('gallery sorting asks for a direction then reuses the existing block move behavior', () => {
  const { page, actionSheets } = loadEditor();
  const blocks = [
    { kind: 'image', media_id: 'image-a', caption: '' },
    { kind: 'image', media_id: 'image-b', caption: '' },
  ];
  page.setData({
    presentation: 'gallery', blocks, media: [],
    imageBlocks: blocks.map((block, blockIndex) => ({ ...block, blockIndex })),
    selectedImageBlock: { ...blocks[1], blockIndex: 1 },
  });

  page.sortSelectedGalleryImage();
  assert.deepEqual(Array.from(actionSheets[0].itemList), ['前移', '后移']);
  actionSheets[0].success({ tapIndex: 0 });
  assert.equal(page.data.blocks[0].media_id, 'image-b');
  assert.equal(page.data.selectedImageBlock.media_id, 'image-b');
});

test('a late gallery sort cannot reorder another account, draft or unloaded page', () => {
  for (const invalidate of [
    runtime => runtime.token('account-two'),
    runtime => runtime.page.setData({ title: '已切换到另一份草稿' }),
    runtime => runtime.page.onUnload(),
  ]) {
    const runtime = loadEditor();
    const { page, actionSheets } = runtime;
    const blocks = [
      { kind: 'image', media_id: 'image-a', caption: '' },
      { kind: 'image', media_id: 'image-b', caption: '' },
    ];
    page.setData({
      id: 'draft-one', title: '原草稿', owner_id: 'owner-a', presentation: 'gallery', blocks, media: [],
      imageBlocks: blocks.map((block, blockIndex) => ({ ...block, blockIndex })),
      selectedImageBlock: { ...blocks[1], blockIndex: 1 },
    });

    page.sortSelectedGalleryImage();
    invalidate(runtime);
    actionSheets[0].success({ tapIndex: 0 });

    assert.deepEqual(page.data.blocks.map(block => block.media_id), ['image-a', 'image-b']);
  }
});

test('empty video uses one guarded chooser for phone or media library', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(editorWxml, /class="video-compact-empty"[^>]*bindtap="chooseVideoSource"/);
  assert.doesNotMatch(editorWxml, /class="video-empty-actions"/);

  const { page, actionSheets } = loadEditor();
  let phoneSelections = 0;
  let librarySelections = 0;
  page.chooseMedia = () => { phoneSelections += 1; };
  page.openMediaPicker = () => { librarySelections += 1; };
  page.setData({ presentation: 'video', videoBlocks: [] });
  page.chooseVideoSource();
  assert.deepEqual(Array.from(actionSheets[0].itemList), ['从手机选择', '从素材库选择']);
  actionSheets[0].success({ tapIndex: 0 });
  page.chooseVideoSource();
  actionSheets[1].success({ tapIndex: 1 });
  assert.equal(phoneSelections, 1);
  assert.equal(librarySelections, 1);

  page.setData({ busy: true });
  page.chooseVideoSource();
  assert.equal(actionSheets.length, 2, '忙碌时不应重复打开来源选择');
});

test('a late video source choice cannot start work for another account, draft or unloaded page', () => {
  for (const invalidate of [
    runtime => runtime.token('account-two'),
    runtime => runtime.page.setData({ title: '新草稿' }),
    runtime => runtime.page.onUnload(),
  ]) {
    const runtime = loadEditor();
    const { page, actionSheets } = runtime;
    let phoneSelections = 0;
    let librarySelections = 0;
    page.chooseMedia = () => { phoneSelections += 1; };
    page.openMediaPicker = () => { librarySelections += 1; };
    page.setData({ id: 'draft-one', title: '原草稿', owner_id: 'owner-a', presentation: 'video', videoBlocks: [] });

    page.chooseVideoSource();
    invalidate(runtime);
    actionSheets[0].success({ tapIndex: 0 });
    actionSheets[0].success({ tapIndex: 1 });

    assert.equal(phoneSelections, 0);
    assert.equal(librarySelections, 0);
  }
});

test('native article drafts expose a secondary preview without changing the save and publish footer', { skip: !fs.existsSync(wxmlCompiler) }, () => {
  const file = './pages/contentedit/contentedit.wxml';
  const compiled = spawnSync(wxmlCompiler, [file, './components/content-presentation/content-presentation.wxml'], {
    cwd: path.dirname(root), encoding: 'utf8', timeout: 10000, maxBuffer: 4 * 1024 * 1024,
  });
  assert.equal(compiled.status, 0, compiled.stderr || compiled.stdout);
  const context = { window: {}, console };
  vm.createContext(context);
  vm.runInContext(compiled.stdout, context);
  const { page } = loadEditor();
  const initial = { ...page.data, __profileAccessMounted: true, loading: false, presentation: 'article', status: 'draft' };
  const render = overrides => context.$gwx(file)({ ...initial, ...overrides });
  const allNodes = node => node && typeof node === 'object'
    ? [node, ...(node.children || []).flatMap(allNodes)] : [];
  const hasClass = (node, name) => String(node.attr && node.attr.class || '').split(/\s+/).includes(name);
  const previewButtons = node => allNodes(node).filter(item => item.tag === 'wx-button' && item.attr.bindtap === 'saveAndPreview');

  for (const id of ['', 'article-one']) {
    const tree = render({ id });
    const hero = allNodes(tree).find(node => hasClass(node, 'editor-hero'));
    const buttons = previewButtons(hero);
    assert.equal(buttons.length, 1, `${id || 'new article'} needs an actual rendered preview action`);
    assert.equal(hasClass(buttons[0], 'primary'), false, '预览保持次要操作');
    const busyHero = allNodes(render({ id, busy: true })).find(node => hasClass(node, 'editor-hero'));
    assert.equal(previewButtons(busyHero)[0].attr.disabled, true);
  }
  for (const presentation of ['article', 'gallery', 'video']) {
    for (const id of ['', 'saved-content']) {
      const footer = allNodes(render({ presentation, id })).find(node => hasClass(node, 'editor-actions'));
      assert.deepEqual(Array.from(allNodes(footer).filter(node => node.tag === 'wx-button'), node => node.attr.bindtap), ['saveDraft', 'publish']);
    }
  }
  for (const blocked of [
    { deleted: true }, { documentReadOnly: true }, { legacyReadOnly: true },
    { __profileAccessMounted: false }, { loading: true }, { loginRequired: true },
    { denied: true }, { disabled: true }, { preview: { preview_token: 'current-preview' } },
  ]) {
    for (const id of ['', 'article-one']) {
      assert.equal(previewButtons(render({ id, ...blocked })).length, 0, `blocked article exposed preview: ${JSON.stringify(blocked)}`);
    }
  }
});

test('gallery and video editors follow the approved compact note layout', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const editorStyle = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  const editorJson = fs.readFileSync(path.join(root, 'contentedit/contentedit.json'), 'utf8');

  assert.match(editorWxml, /variant="workbench"/);
  assert.match(editorWxml, /'新建图文笔记'/);
  assert.match(editorWxml, /'新建视频笔记'/);
  assert.match(editorJson, /"mini-icon"/);
  assert.match(editorWxml, /class="social-editor-toolbar"[\s\S]*bindtap="saveAndPreview"[\s\S]*name="more"/);
  assert.match(editorWxml, /class="gallery-add-tile"/);
  assert.match(editorWxml, /name="edit"[\s\S]*AI辅助/);
  assert.match(editorWxml, /class="video-options-mask"/);
  assert.match(editorWxml, /class="video-options-sheet"/);
  assert.doesNotMatch(editorWxml, /class="preview-link"/);
  assert.match(editorStyle, /\.gallery-thumb\s*\{[^}]*width:\s*168rpx[^}]*height:\s*168rpx/);
  assert.match(editorStyle, /\.gallery-stage-empty\s*\{[^}]*width:\s*220rpx[^}]*height:\s*220rpx/);
  assert.match(editorStyle, /\.gallery-stage-empty\s*\{[^}]*margin-left:\s*0\s*!important[^}]*margin-right:\s*0\s*!important/);
  assert.match(editorStyle, /\.gallery-copy-group\s+\.intro-field,\s*\.video-copy-group\s+\.intro-field\s*\{[^}]*border-top:\s*0/);
  assert.match(editorStyle, /\.editor-copy-group\s*>\s*\.title-input\s*\{[^}]*font-size:\s*40rpx[^}]*font-weight:\s*600/);
  assert.match(editorStyle, /\.video-compact-empty\s*\{[^}]*border:\s*1rpx dashed/);
  assert.match(editorStyle, /\.editor-copy-group\s*\{[^}]*border:\s*0/);
  assert.match(editorStyle, /\.video-media-card\s*\{[^}]*border:\s*0/);
});

test('video option rows open one bottom sheet at a time and can be closed', () => {
  const { page } = loadEditor();
  page.toggleVideoShare();
  assert.equal(page.data.videoShareExpanded, true);
  assert.equal(page.data.videoDisplayExpanded, false);
  page.toggleVideoDisplay();
  assert.equal(page.data.videoShareExpanded, false);
  assert.equal(page.data.videoDisplayExpanded, true);
  page.closeVideoOptions();
  assert.equal(page.data.videoShareExpanded, false);
  assert.equal(page.data.videoDisplayExpanded, false);
});

test('both video setting rows open the same complete share and display sheet', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const start = editorWxml.indexOf('class="video-options-sheet"');
  const end = editorWxml.indexOf('</view></block>', start);
  const sheet = editorWxml.slice(start, end);
  assert.match(sheet, />分享与显示</);
  assert.match(sheet, /data-field="shareCopy"/);
  assert.match(sheet, /data-field="momentsCopy"/);
  assert.match(sheet, />阅读样式</);
  assert.match(sheet, />标准视频</);
  assert.match(sheet, />沉浸视频</);
  assert.match(sheet, /仅影响小程序内的阅读效果/);
  assert.match(sheet, /bindtap="openVideoSettings"[^>]*>更多设置</);
  assert.doesNotMatch(sheet, /wx:if="\{\{videoShareExpanded\}\}"/);
  assert.doesNotMatch(sheet, /视频短注释/);
  assert.match(editorWxml, /settingsExpanded[\s\S]*presentation === 'video' && videoBlocks\.length[\s\S]*视频短注释/);
  const editorStyle = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  assert.match(editorStyle, /\.video-sheet-head\s*\{[^}]*justify-content:\s*center[^}]*position:\s*relative/);
  assert.match(editorStyle, /\.video-options-sheet \.video-copy-card textarea\s*\{[^}]*min-height:\s*72rpx/);
  assert.match(editorStyle, /\.video-layout-options button\.selected\s*\{[^}]*background:\s*#a87943[^}]*color:\s*#fff/);
});

test('video hides the third settings row by default and the sheet can reveal the complete settings', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(editorWxml, /wx:if="\{\{presentation !== 'video' \|\| settingsExpanded\}\}" class="settings-card"/);
  assert.match(editorWxml, /settingsExpanded[\s\S]*版权来源[\s\S]*原文链接/);

  const { page } = loadEditor();
  page.setData({ presentation: 'video', videoShareExpanded: true, videoDisplayExpanded: false, settingsExpanded: false });
  page.openVideoSettings();
  assert.equal(page.data.videoShareExpanded, false);
  assert.equal(page.data.videoDisplayExpanded, false);
  assert.equal(page.data.settingsExpanded, true);
});

test('social toolbar never labels a dirty draft as saved', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.doesNotMatch(editorWxml, /busy \? '保存中' : '已保存'/);
  assert.match(editorWxml, /notice === '有未保存修改' \? '未保存'/);

  const { page } = loadEditor();
  page._savedSignature = page.draftSignature();
  page.setField({ currentTarget: { dataset: { field: 'title' } }, detail: { value: '修改后的标题' } });
  assert.notEqual(page._savedSignature, page.draftSignature());
  assert.equal(page.data.notice, '有未保存修改');
});

test('the selected gallery image stays selected while its caption and order change', () => {
  const { page } = loadEditor();
  const blocks = [
    { kind: 'image', media_id: 'image-a', caption: '' },
    { kind: 'image', media_id: 'image-b', caption: '' },
  ];
  const media = [
    { kind: 'image', media_id: 'image-a', local_path: '/tmp/a.jpg' },
    { kind: 'image', media_id: 'image-b', local_path: '/tmp/b.jpg' },
  ];
  page.setData({ presentation: 'gallery', blocks, media, imageBlocks: blocks.map((block, blockIndex) => ({ ...block, blockIndex })) });
  page.selectGalleryImage({ currentTarget: { dataset: { index: 1 } } });
  page.editCaption({ currentTarget: { dataset: { index: 1 } }, detail: { value: '南立面' } });
  assert.equal(page.data.selectedImageBlock.media_id, 'image-b');
  assert.equal(page.data.selectedImageBlock.caption, '南立面');

  page.moveBlock({ currentTarget: { dataset: { index: 1, step: -1 } } });
  assert.equal(page.data.blocks[0].media_id, 'image-b');
  assert.equal(page.data.selectedImageBlock.media_id, 'image-b');
  assert.equal(page.data.selectedImageBlock.blockIndex, 0);
});

test('editor footer keeps preview optional and exposes save and direct publish together', () => {
  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(editorWxml, /class="social-editor-toolbar"[\s\S]*bindtap="saveAndPreview"/);
  assert.doesNotMatch(editorWxml, /class="preview-link"/);
  assert.match(editorWxml, /bindtap="saveDraft"[^>]*>保存草稿</);
  assert.match(editorWxml, /bindtap="publish"[^>]*>发布</);
  assert.doesNotMatch(editorWxml, />预览发布</);
});

test('U6 form polish: sticky error banner, rights wording and rejection toast carry the reason', () => {
  const { page, toasts } = loadEditor();
  page._loaded = true;
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', title: '标题', blocks: [{ kind: 'paragraph', text: '正文' }] });
  const launch = { token: 'session-token', contentID: '', revision: 0, signature: page.draftSignature(), state: '', message: '', channel: null };
  page.setData({ title: '期间已修改' });
  page.receiveWritingReturn({ title: 'AI', text: '不应采用' }, launch);
  // 拒收 toast 必须带原因，只写"未采用"用户不知道下一步
  assert.ok(toasts.some(toast => /已发生变化/.test(toast.title)));

  const editorWxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(editorWxml, /版权来源/);
  assert.ok(editorWxml.indexOf('素材权属') < 0, '旧措辞不得残留');
  const workbenchWxml = fs.readFileSync(path.join(root, 'contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.match(workbenchWxml, /版权来源/);
  const editorStyle = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxss'), 'utf8');
  assert.match(editorStyle, /\.error,\s*\.notice\s*\{[^}]*position:\s*sticky/);
});

test('U6 button baseline lands on content pages that lacked it', () => {
  for (const pageName of ['contentwriting', 'nativereferences', 'projectnotes']) {
    const style = fs.readFileSync(path.join(root, `${pageName}/${pageName}.wxss`), 'utf8');
    assert.match(style, /button\s*\{[^}]*display:\s*flex[^}]*align-items:\s*center/, `${pageName} 应有按钮垂直居中基线`);
  }
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

  assert.doesNotMatch(readerWxml, /class="reader-brand"/, '阅读页不再展示品牌头');
  assert.match(readerWxml, /class="reader-actions[^"]*"/);
  assert.match(readerWxml, /open-type="share"/);
  assert.match(readerWxml, /bindtap="openNoteMore"/);
  assert.match(readerWxml, /class="note-interaction-dock"/);
  assert.match(readerStyle, /#a87943/i);
  assert.match(readerStyle, /\.note-interaction-dock\s*\{[^}]*position:\s*fixed/);
});

test('cover picker returns an image without changing body blocks and rejects a stale return', async () => {
  const harness = loadEditor();
  const { page, navigationOptions } = harness;
  page.setData({ blocks: [{ kind: 'paragraph', text: '原正文' }], preview: { preview_token: 'old' } });
  page.openCoverPicker();
  assert.equal(navigationOptions[0].url, '/pages/nativereferences/nativereferences?picker=media&kind=image');
  await navigationOptions[0].events.selectMaterial({ kind: 'media', asset: { media_id: 'image-a', kind: 'image' } });
  assert.equal(page.data.cover_media_id, 'image-a');
  assert.equal(page.data.blocks.length, 1);
  assert.equal(page.data.blocks[0].text, '原正文');
  assert.equal(page.data.preview, null);
  page.openCoverPicker();
  harness.token('other-account');
  await navigationOptions[1].events.selectMaterial({ kind: 'media', asset: { media_id: 'image-other', kind: 'image' } });
  assert.equal(page.data.cover_media_id, 'image-a');
});

test('adding an uploaded asset to the body remains independent of the cover picker', () => {
  const { page } = loadEditor();
  page.setData({ media: [{ media_id: 'image-a', kind: 'image' }], blocks: [{ kind: 'paragraph', text: '原正文' }], cover_media_id: '' });
  page.addMediaBlock({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(page.data.blocks.length, 2);
  assert.equal(page.data.blocks[1].media_id, 'image-a');
  assert.equal(page.data.cover_media_id, '');
});

test('published reader action does not imply unsaved edits have been published', () => {
  const {page,navigations,toasts}=loadEditor();
  page.setData({id:'content-one',status:'published',version:2,title:'原稿'});
  page._savedSignature=page.draftSignature();page.setData({title:'未保存修改'});page.openPublished();
  assert.equal(navigations.length,0);assert.match(toasts[0].title,/修改.*未发布/);
});


test('compact video opens full-screen playback with its original video and poster', () => {
  const { page, mediaPreviews, toasts } = loadEditor();
  page.setData({ media: [{ media_id: 'video-one', kind: 'video', local_path: '/private/video.mp4', poster_path: '/private/poster.jpg' }] });
  page.openEditorVideo({ currentTarget: { dataset: { mediaId: 'video-one' } } });
  assert.equal(mediaPreviews.length, 1);
  assert.equal(mediaPreviews[0].sources[0].url, '/private/video.mp4');
  assert.equal(mediaPreviews[0].sources[0].type, 'video');
  assert.equal(mediaPreviews[0].sources[0].poster, '/private/poster.jpg');
  mediaPreviews[0].fail({ errMsg: 'previewMedia:fail' });
  assert.match(toasts[0].title, /播放失败/);
  page.openEditorVideo({ currentTarget: { dataset: { mediaId: 'missing' } } });
  assert.equal(mediaPreviews.length, 1);
});


test('library videos without a thumbnail use a touchable preview card without an inline native video layer', () => {
  const { page, mediaPreviews } = loadEditor();
  page.setData({ media: [{ media_id: 'library-video', kind: 'video', local_path: '/private/library.mp4' }] });
  page.openEditorVideo({ currentTarget: { dataset: { mediaId: 'library-video' } } });
  assert.equal(mediaPreviews[0].sources[0].url, '/private/library.mp4');
  const template = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  const thumbnail = template.slice(template.indexOf('class="video-thumbnail"'), template.indexOf('class="video-media-details"'));
  assert.ok(thumbnail.includes('bindtap="openEditorVideo"'));
  assert.doesNotMatch(thumbnail, /<video\b/, 'a controls=false native video would intercept the full-screen preview tap');
});


test('image upload survives the picker returning after its success callback', async () => {
  let finish;
  const { page } = loadEditor({ uploadWorkbenchMedia: () => new Promise(resolve => { finish = resolve; }) });
  page.setData({ presentation: 'gallery', owner_id: 'owner-a', rights: 'own', blocks: [] });
  const pending = page.uploadMediaBatch([{ tempFilePath: 'tmp://one.jpg' }], 'image');
  page._epoch += 1;
  finish({ media_id: 'image-one', kind: 'image' });
  await pending;
  assert.equal(page.data.imageBlocks.length, 1);
  assert.equal(page.data.imageBlocks[0].asset.local_path, 'tmp://one.jpg');
  assert.equal(page.data.busy, false);
});

test('image upload ignores results after changing owner or unloading', async () => {
  for (const change of [page => page.setData({ owner_id: 'owner-b' }), page => page.onUnload()]) {
    let finish;
    const { page } = loadEditor({ uploadWorkbenchMedia: () => new Promise(resolve => { finish = resolve; }) });
    page.setData({ presentation: 'gallery', owner_id: 'owner-a', rights: 'own', blocks: [] });
    const pending = page.uploadMediaBatch([{ tempFilePath: 'tmp://one.jpg' }], 'image');
    change(page);
    finish({ media_id: 'image-one', kind: 'image' });
    await pending;
    assert.equal(page.data.imageBlocks.length, 0);
  }
});

test('typing gallery copy during upload is preserved when images finish', async () => {
  let finish;
  const { page } = loadEditor({ uploadWorkbenchMedia: () => new Promise(resolve => { finish = resolve; }) });
  page.setData({ presentation: 'gallery', owner_id: 'owner-a', rights: 'own', blocks: [] });
  const pending = page.uploadMediaBatch([{ tempFilePath: 'tmp://one.jpg' }], 'image');
  page.editPostText({ detail: { value: '上传时继续填写的正文' } });
  finish({ media_id: 'image-one', kind: 'image' });
  await pending;
  assert.equal(page.data.postText, '上传时继续填写的正文');
  assert.equal(page.data.imageBlocks.length, 1);
});

test('content save survives same-account background return without losing the saved identity', async () => {
  let finish;
  const { page } = loadEditor({ saveContent: () => new Promise(resolve => { finish = resolve; }) });
  page._loaded = true;
  page.setData({ title: '原题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });
  const saving = page.saveDraft();
  page.onHide(); page.onShow();
  assert.equal(page.data.busy, true, '同一保存仍在途时不能解锁再次创建');
  page.setField({ currentTarget: { dataset: { field: 'title' } }, detail: { value: '请求中的新输入' } });
  finish({ content_id: 'content-saved', revision: 1, status: 'draft', version: 0 });
  await saving;
  assert.equal(page.data.id, 'content-saved');
  assert.equal(page.data.revision, 1);
  assert.equal(page.data.title, '请求中的新输入');
  assert.equal(page.data.busy, false);
  assert.match(page.data.notice, /未保存/);
});

test('late content save cannot apply another account or an unloaded page', async () => {
  for (const invalidation of ['account', 'unload']) {
    let finish;
    const runtime = loadEditor({ saveContent: () => new Promise(resolve => { finish = resolve; }) });
    const { page } = runtime;
    page._loaded = true;
    page.setData({ title: '原题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '正文' }] });
    const saving = page.saveDraft();
    if (invalidation === 'account') runtime.token('another-account'); else page.onUnload();
    finish({ content_id: 'foreign-save', revision: 2, version: 0, status: 'draft' });
    await saving;
    assert.equal(page.data.id, '');
  }
});

test('media and cover pickers carry the target content owner', () => {
  const { page, navigations } = loadEditor();
  page.setData({ owner_id: 'owner-company' });
  page.openMediaPicker(); page.openCoverPicker();
  assert.ok(navigations.every(url => url.includes('owner=owner-company')));
});
