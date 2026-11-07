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
  const modals = [], backs = [];
  const actionSheets = [];
  const mediaChoosers = [];
  const clipboards = [], mediaPreviews = [], scrolls = [];
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
  return { page, navigations, navigationOptions, toasts, modals, backs, actionSheets, mediaChoosers, clipboards, mediaPreviews, scrolls, token(value) { sessionToken = value; } };
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

test('returning from preview keeps edits and invalidates the old publish token', async () => {
  let publications = 0;
  const { page } = loadEditor({ publishContent: async () => { publications += 1; } });
  page.setData({ title: '庭院方案', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '保留正文' }], preview: { preview_token: 'old-preview' }, previewBlocks: [{ text: '旧预览' }] });
  page.returnToEdit();
  assert.equal(page.data.title, '庭院方案');
  assert.equal(page.data.blocks[0].text, '保留正文');
  assert.equal(page.data.preview, null);
  assert.equal(page.data.previewBlocks.length, 0);
  await page.publish();
  assert.equal(publications, 0);
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

test('AI writing return explains changed-draft and account-change rejection without overwriting', () => {
  const saved = loadEditor();
  saved.page._loaded = true;
  saved.page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a' });
  saved.page.openAIWriting();
  saved.page.setData({ id: 'saved-content', title: '已保存标题', blocks: [{ kind: 'paragraph', text: '已保存正文' }] });
  saved.navigationOptions[0].events.prefillContent({ title: 'AI 标题', text: '不应覆盖保存内容' });
  assert.equal(saved.page.data.blocks[0].text, '已保存正文');
  assert.match(saved.page.data.notice, /草稿版本已变化/);

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

test('video owner stays visible at the top and validation scrolls directly to actionable fields', () => {
  const wxml = fs.readFileSync(path.join(root, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /video-workspace-head[^>]*>\s*<picker wx:if="\{\{context\.owner_choices\.length > 1\}\}"[^>]*bindchange="setOwner"/);
  assert.match(wxml, /wx:if="\{\{fieldErrors\.owner_id\}\}" class="field-error"/);
  assert.match(wxml, /wx:if="\{\{presentation !== 'video' && context\.owner_choices\.length > 1\}\}"/);

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

test('U6 form polish: sticky error banner, rights wording and rejection toast carry the reason', () => {
  const { page, navigationOptions, toasts } = loadEditor();
  page._loaded = true;
  page.setData({ context: { owners: ['owner-a'] }, owner_id: 'owner-a', title: '标题', blocks: [{ kind: 'paragraph', text: '正文' }] });
  page.openAIWriting();
  page.setData({ title: '期间已修改' });
  navigationOptions[0].events.prefillContent({ title: 'AI', text: '不应采用' });
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

  assert.match(readerWxml, /class="reader-brand"/);
  assert.match(readerWxml, /class="reader-actions[^"]*"/);
  assert.match(readerWxml, /open-type="share"/);
  assert.match(readerWxml, /bindtap="copyWebLink"/);
  assert.match(readerStyle, /#a87943/i);
  assert.match(readerStyle, /\.reader-actions\s*\{[^}]*position:\s*fixed/);
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
