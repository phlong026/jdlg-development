'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function loadAPI(wxOverrides = {}) {
  const calls = [];
  const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');
  const module = { exports: {} };
  const wx = Object.assign({
    getStorageSync: key => key === 'token' ? 'session-token' : key === 'device_fingerprint' ? 'device-one' : '',
    request(options) { calls.push({ kind: 'request', options }); options.success({ statusCode: 200, data: { code: 0, data: {} } }); },
    downloadFile(options) { calls.push({ kind: 'download', options }); options.success({ statusCode: 200, tempFilePath: '/tmp/private-note-media' }); },
    uploadFile(options) { calls.push({ kind: 'upload', options }); options.success({ statusCode: 200, data: JSON.stringify({ code: 0, data: { media_id: 'private-media-1' } }) }); },
  }, wxOverrides);
  vm.runInNewContext(source, { module, exports: module.exports, wx, require(id) {
    if (id === './parameter') return {
      generateRequestParams(token, body, apiPath, device) { return { token, body, apiPath, device }; },
      paramsToHeaders(params) { return { signedPath: params.apiPath, token: params.token }; },
    };
    throw new Error(`unexpected require ${id}`);
  }, Promise, JSON, Error, Object, String, Number, Date, encodeURIComponent });
  return { api: module.exports, calls };
}

function pageHarness(name, api, wxOverrides = {}) {
  let definition;
  const source = fs.readFileSync(path.join(root, `pages/${name}/${name}.js`), 'utf8');
  const wx = Object.assign({
    getStorageSync: key => key === 'token' ? 'session-token' : key === 'userInfo' ? { id: 101 } : '',
    getWindowInfo: () => ({ windowWidth: 375 }),
    hideShareMenu() {}, showShareMenu() {}, navigateTo() {}, navigateBack() {}, showToast() {}, showModal() {}, setClipboardData() {},
    previewImage() {}, openDocument() {}, saveImageToPhotosAlbum() {}, createVideoContext: () => ({ pause() {} }),
  }, wxOverrides);
  const context = { Page(value) { definition = value; }, wx, require(id) {
    if (id === '../../utils/phase3-api' || id === '../../utils/project-member-api' || id === '../../utils/customer-link-api') return api;
    if (id === '../../utils/article-document') return require(path.join(root, 'utils/article-document.js'));
    if (id === '../../utils/content-presentation') return require(path.join(root, 'utils/content-presentation.js'));
    if (id === '../../utils/project-note-media') return require(path.join(root, 'utils/project-note-media.js'));
    if (id === '../../utils/ai-stream') return wxOverrides.aiStream || { startAIStream: () => ({ abort() {} }) };
    if (id === '../../utils/writing-policy') return require(path.join(root, 'utils/writing-policy.js'));
    if (id === './projectnotes-core') {
      const core = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes-core.js'), 'utf8');
      vm.runInNewContext(core, context);
      return {};
    }
    throw new Error(`unexpected require ${id}`);
  }, console, Promise, JSON, Error, Object, String, Number, Date, Map, Set, encodeURIComponent, decodeURIComponent, setTimeout, clearTimeout };
  vm.runInNewContext(source, context);
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values, done) { Object.assign(this.data, values); if (done) done(); } }, definition);
  return page;
}

test('draft preview reads unsaved text and project media without saving, freezing or sharing', async () => {
  const downloads = [];
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: async (project, id) => { downloads.push([project, id]); return '/tmp/' + id; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: '庭院方案', noteBody: '入口保留大树。\n\n客厅面向庭院。', noteMedia: [{ media_id: 'image-one', kind: 'image' }] });
  page._noteFormBaseline = page.noteFormSignature();
  await page.previewNote();
  assert.equal(page.data.editorMode, 'preview');
  assert.equal(page.data.previewBody, '入口保留大树。\n\n客厅面向庭院。');
  assert.equal(page.data.previewBodyFormat, 'plain');
  assert.equal(page.data.previewMedia[0].local_path, '/tmp/image-one');
  assert.deepEqual(downloads, [['project-one', 'image-one']]);
  assert.equal(page.data.note, null);
  assert.equal(page.hasUnsavedChanges(), false);
  page.editNote();
  assert.equal(page.data.editorMode, 'edit');
  assert.equal(page.data.noteBody, '入口保留大树。\n\n客厅面向庭院。');
  assert.equal(page.data.noteMedia[0].media_id, 'image-one');
});

test('shared project note preview rebuilds unknown image and video ratios once without mutating fixed media', async () => {
  for (const [kind, width, height] of [['image', 1920, 1080], ['video', 1080, 1920]]) {
    const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async () => `/tmp/${kind}` });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: `${kind}预览`, noteBody: '保持展示比例', noteMedia: [{ media_id: `${kind}-one`, kind }] });
    await page.previewNote();
    const immutablePreviewMedia = JSON.stringify(page.data.previewMedia);
    const fallback = page.data.previewPresentation.media[0];
    assert.notEqual(fallback.intrinsicWidth / fallback.intrinsicHeight, width / height);

    let writes = 0;
    const setData = page.setData.bind(page);
    page.setData = (values, done) => { writes += 1; setData(values, done); };
    const event = { currentTarget: { dataset: { mediaId: `${kind}-one` } }, detail: { width, height } };
    page.onContentVideoMetadata(event);
    assert.equal(page.data.previewPresentation.media[0].intrinsicWidth, width);
    assert.equal(page.data.previewPresentation.media[0].intrinsicHeight, height);
    assert.equal(JSON.stringify(page.data.previewMedia), immutablePreviewMedia, '固定版本展示媒体不应被元数据事件回写');
    assert.equal(writes, 1);
    page.onContentVideoMetadata(event);
    assert.equal(writes, 1, '相同尺寸不应再次setData');
  }
});

test('project note gallery swipe rebuilds the selected media height', async () => {
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async (_, id) => `/tmp/${id}.jpg` });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({
    pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: '混合图集', noteBody: '滑动后跟随当前图片',
    noteMedia: [{ media_id: 'wide', kind: 'image', width: 1920, height: 1080 }, { media_id: 'tall', kind: 'image', width: 900, height: 1600 }],
  });
  await page.previewNote();
  const firstHeight = page.data.previewPresentation.galleryHeight;
  page.onContentGalleryChange({ detail: { current: 1 } });
  assert.equal(page.data.previewPresentation.galleryIndex, 1);
  assert.equal(page.data.previewPresentation.galleryHeight, page.data.previewPresentation.media[1].height);
  assert.notEqual(page.data.previewPresentation.galleryHeight, firstHeight);
});

test('structured draft preview preserves original block order, headings and captions', async () => {
  const body = JSON.stringify({ schema: 1, blocks: [{ kind: 'heading', level: 2, text: '空间布局' }, { kind: 'image', media_id: 'image-one', caption: '一层平面' }, { kind: 'paragraph', text: '老人房靠近入口。' }] });
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async () => '/tmp/plan.png' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, note: { note_id: 'note-one', body, body_format: 'article_v2', media_ids: ['image-one'] }, noteTitle: '方案说明', noteBody: body, bodyReadOnly: true, noteMedia: [{ media_id: 'image-one', kind: 'image', width: 1000, height: 1500 }] });
  await page.previewNote();
  assert.equal(page.data.previewBody, body);
  assert.deepEqual(Array.from(page.data.previewBlocks, item => item.kind), ['heading', 'image', 'paragraph']);
  assert.equal(page.data.previewBlocks[1].caption, '一层平面');
  assert.equal(page.data.previewBlocks[1].local_path, '/tmp/plan.png');
});

test('preview download failure keeps the draft intact and permits a complete retry', async () => {
  let fail = true;
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async () => { if (fail) throw new Error('图片暂时无法读取'); return '/tmp/retry.png'; } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: '待保存方案', noteBody: '保留这段修改', noteMedia: [{ media_id: 'image-one', kind: 'image' }] });
  await page.previewNote();
  assert.equal(page.data.previewLoading, false);
  assert.match(page.data.previewError, /图片暂时无法读取/);
  assert.equal(page.data.previewMedia.length, 0);
  assert.equal(page.data.noteBody, '保留这段修改');
  fail = false;
  await page.previewNote();
  assert.equal(page.data.previewError, '');
  assert.equal(page.data.previewMedia[0].local_path, '/tmp/retry.png');
});

test('returning to edit ignores a late preview response and retains later input', async () => {
  let finish;
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: () => new Promise(resolve => { finish = resolve; }) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: '庭院', noteBody: '初稿', noteMedia: [{ media_id: 'image-one', kind: 'image' }] });
  const pending = page.previewNote();
  page.editNote();
  page.setNoteBody({ detail: { value: '继续修改后的正文' } });
  finish('/tmp/stale.png');
  await pending;
  assert.equal(page.data.editorMode, 'edit');
  assert.equal(page.data.previewMedia.length, 0);
  assert.equal(page.data.previewLoading, false);
  assert.equal(page.data.noteBody, '继续修改后的正文');
});

test('keyboard height and preview state reset when the project editor leaves the foreground', () => {
  const page = pageHarness('projectnotes', {});
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ noteTitle: '未保存', noteBody: '继续保留', editorMode: 'preview', previewMedia: [{ media_id: 'private-image', local_path: '/tmp/private.png' }] });
  page.onNoteKeyboardHeight({ detail: { height: 320 } });
  assert.equal(page.data.keyboardHeight, 320);
  page.onHide();
  assert.equal(page.data.keyboardHeight, 0);
  assert.equal(page.data.editorMode, 'edit');
  assert.equal(page.data.previewMedia.length, 0);
  assert.equal(page.data.noteBody, '继续保留');
});

test('preview UI and downloaded paths never enter the saved note payload or dirty state', async () => {
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async () => '/tmp/private-preview.png' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: '现场方案', noteBody: '未保存修改', noteMedia: [{ media_id: 'image-one', kind: 'image', local_path: '/tmp/picked.png' }] });
  page._noteFormBaseline = page.noteFormSignature();
  const before = JSON.stringify(page.noteDraft());
  await page.previewNote();
  assert.equal(JSON.stringify(page.noteDraft()), before);
  assert.equal(page.hasUnsavedChanges(), false);
  page.editNote();
  page.setNoteBody({ detail: { value: '真正的新修改' } });
  assert.equal(page.hasUnsavedChanges(), true);
});

test('preview response from a previous account cannot restore private media', async () => {
  let token = 'session-token', finish;
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: () => new Promise(resolve => { finish = resolve; }) }, { getStorageSync: key => key === 'token' ? token : '' });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'project-one' }, noteTitle: '私密方案', noteBody: '私密正文', noteMedia: [{ media_id: 'image-one', kind: 'image' }] });
  const pending = page.previewNote();
  token = 'new-account';
  page.clearProtected();
  finish('/tmp/old-account.png');
  await pending;
  assert.equal(page.data.previewMedia.length, 0);
  assert.equal(page.data.previewBody, '');
  assert.equal(page.data.noteBody, '');
});

test('gallery swipe keeps its own position and opening an uploaded image uses the original file', () => {
  let opened;
  const page = pageHarness('projectnotes', {}, { previewImage: options => { opened = options; } });
  page.setData({ pageView: 'editor', editorMode: 'edit', noteMedia: [{ media_id: 'image-one', kind: 'image', local_path: '/tmp/original-one.png' }, { media_id: 'image-two', kind: 'image', local_path: '/tmp/original-two.png' }] });
  page.onNoteGalleryChange({ currentTarget: { dataset: { key: 'note-gallery-0' } }, detail: { current: 1 } });
  assert.equal(page.data.galleryPositions['note-gallery-0'], 1);
  page.openImage({ currentTarget: { dataset: { id: 'image-two' } } });
  assert.equal(opened.current, '/tmp/original-two.png');
  assert.deepEqual(Array.from(opened.urls), ['/tmp/original-one.png', '/tmp/original-two.png']);
});

test('private project-note API helpers use signed member-checked endpoints and private media download', async () => {
  const { api, calls } = loadAPI();
  await api.listProjectNoteProjects();
  await api.createProjectNoteProject({ name: '张宅方案', owner_id: 'owner-a' });
  await api.getSharedProjectNote('opaque-private-token');
  await api.openSharedProjectNote('opaque-private-token', 'stable-open-event');
  await api.downloadSharedProjectNoteMedia('opaque-private-token', 'asset-0001');
  await api.uploadProjectNoteMedia('project-one', '/tmp/photo.jpg');
  assert.deepEqual(calls.map(call => call.options.header.signedPath), [
    '/api/v1/miniprogram/project-notes/projects',
    '/api/v1/miniprogram/project-notes/projects',
    '/api/v1/miniprogram/project-notes/shared/opaque-private-token',
    '/api/v1/miniprogram/project-notes/shared/opaque-private-token/open',
    '/api/v1/miniprogram/project-notes/shared/opaque-private-token/media/asset-0001',
    '/api/v1/miniprogram/project-notes/projects/project-one/media',
  ]);
  assert.equal(calls[3].options.data.event_id, 'stable-open-event');
  assert.equal(calls[4].kind, 'download');
  assert.equal(calls[5].kind, 'upload');
  assert.equal(calls[5].options.formData, undefined);
  assert.doesNotMatch(calls[4].options.url, /[?&](?:token|authorization)=/i);
});

test('shared video playback helper requests a signed streaming URL without downloading the file', async () => {
  const { api, calls } = loadAPI({ request(options) { calls.push({ kind: 'request', options }); options.success({ statusCode: 200, data: { code: 0, data: { url: '/api/v1/shared-video/ticket?sig=short', expires_at: '2026-11-11T09:00:00Z' } } }); } });
  const playback = await api.getSharedProjectNoteMediaPlayback('opaque-private-token', 'video-0001');
  assert.equal(calls.length, 1);
  assert.equal(calls[0].kind, 'request');
  assert.equal(calls[0].options.method, 'POST');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/project-notes/shared/opaque-private-token/media/video-0001/playback');
  assert.equal(playback.url, 'https://api.jiadilingguang.com/api/v1/shared-video/ticket?sig=short');
});

test('fixed-version and Word downloads sign their exact private routes without query credentials', async () => {
  const { api, calls } = loadAPI();
  await api.getProjectNoteVersion('project-one', 'version-one');
  await api.downloadProjectNoteMedia('project-one', 'asset-one');
  await api.downloadProjectNoteWord('project-one', 'version-one');
  await api.downloadSharedProjectNoteWord('private-share-one');
  assert.deepEqual(calls.map(call => call.options.header.signedPath), [
    '/api/v1/miniprogram/project-notes/projects/project-one/versions/version-one',
    '/api/v1/miniprogram/project-notes/projects/project-one/media/asset-one',
    '/api/v1/miniprogram/project-notes/projects/project-one/versions/version-one/export.docx',
    '/api/v1/miniprogram/project-notes/shared/private-share-one/export.docx',
  ]);
  for (const call of calls) assert.doesNotMatch(call.options.url, /[?&](?:token|authorization)=/i);
  assert.equal(calls[2].options.timeout, 30000);
  assert.equal(calls[3].options.timeout, 30000);
});

test('private pages are registered and explicitly excluded from public tracking', () => {
  const app = JSON.parse(fs.readFileSync(path.join(root, 'app.json'), 'utf8'));
  const routes = require(path.join(root, 'utils/tracking/routes.js'));
  for (const name of ['projectnotes', 'projectnoteview']) {
    assert.ok(app.subPackages.some(pack => pack.root === `pages/${name}` && pack.pages.includes(name)));
    assert.equal(routes.policy(`pages/${name}/${name}`).collect, false);
    assert.equal(routes.policy(`pages/${name}/${name}`).exclusionReason, 'private-project-notes');
  }
});

test('shared note clears protected content when account changes before response returns', async () => {
  let token = 'account-one';
  let resolveRead;
  const read = new Promise(resolve => { resolveRead = resolve; });
  const page = pageHarness('projectnoteview', { getSharedProjectNote: () => read }, { getStorageSync: key => key === 'token' ? token : key === 'userInfo' ? { id: 101 } : '' });
  page.onLoad({ pns: 'opaque-private-token' });
  const pending = page.loadShared();
  token = 'account-two';
  resolveRead({ project: { project_id: 'project-one', name: '张宅' }, share: { share_id: 'share-one' }, version: { version_id: 'version-one', title: '私密标题', body_format: 'article_v2', body: '{"schema":1,"blocks":[{"kind":"paragraph","text":"私密正文"}]}', media: [] } });
  await pending;
  assert.equal(page.data.view, null);
  assert.equal(page.data.articleBlocks.length, 0);
});

test('designer account guard clears every protected form and ownership field', () => {
  const page = pageHarness('projectnotes', {});
  Object.assign(page.data, { projects: [{ project_id: 'private-project' }], owners: ['owner-a'], ownerChoices: [{ owner_id: 'owner-a', display_name: '设计师' }], ownerID: 'owner-a', projectName: '张宅', crmID: 'crm-secret', noteTitle: '私密标题', noteBody: '私密草稿', joinRequests: [{ request_id: 'request-one' }], memberInvitations: [{ invitation_id: 'invitation-one' }], memberInvitation: { invitation_id: 'invitation-one' }, memberInvitationPath: '/private', confirmerAccount: '101', feedbackID: 'feedback-secret', feedbackReply: '已处理', noteMedia: [{ media_id: 'private-media' }] });
  page.clearProtected();
  assert.equal(page.data.projects.length, 0);
  assert.equal(page.data.owners.length, 0);
  assert.equal(page.data.ownerChoices.length, 0);
  assert.equal(page.data.ownerID, '');
  assert.equal(page.data.projectName, '');
  assert.equal(page.data.crmID, '');
  assert.equal(page.data.noteTitle, '');
  assert.equal(page.data.noteBody, '');
  assert.equal(page.data.joinRequests.length, 0);
  assert.equal(page.data.memberInvitations.length, 0);
  assert.equal(page.data.memberInvitation, null);
  assert.equal(page.data.memberInvitationPath, '');
  assert.equal(page.data.confirmerAccount, '');
  assert.equal(page.data.feedbackReply, '');
  assert.equal(page.data.feedbackID, '');
  assert.equal(page.data.noteMedia.length, 0);
});

test('member view reloads active invitations and can revoke one after page refresh', async () => {
  const revoked = [];
  const page = pageHarness('projectnotes', {
    listProjectJoinRequests: async () => ({ items: [] }),
    listProjectInvitations: async () => ({ items: [{ invitation_id: 'invitation-one', status: 'active', expires_at: '2026-11-01T08:00:00Z' }] }),
    revokeProjectInvitation: async (_project, invitation) => { revoked.push(invitation); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'project-one' }, pageView: 'members' });
  await page.loadJoinRequests();
  assert.equal(page.data.memberInvitations.length, 1);
  assert.match(page.data.memberInvitations[0].expiryLabel, /^\u6709\u6548\u81f3 /);
  assert.equal(page.data.memberInvitation, null);
  await page.revokeProjectInvitation({ currentTarget: { dataset: { id: 'invitation-one' } } });
  assert.deepEqual(revoked, ['invitation-one']);
  assert.equal(page.data.memberInvitations.length, 0);
});

test('project workspace uses in-page navigation and keeps native back for the project list', async () => {
  let nativeBacks = 0;
  const page = pageHarness('projectnotes', {}, { navigateBack: () => { nativeBacks++; } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one', name: '张宅' }, notes: [], versions: [], feedbacks: [] });
  assert.equal(page.data.pageView, 'detail');

  await page.startNewNote();
  assert.equal(page.data.pageView, 'editor');
  await page.handleBack();
  assert.equal(page.data.pageView, 'detail');
  await page.handleBack();
  assert.equal(page.data.pageView, 'list');
  assert.equal(page.data.selectedProject, null);
  await page.handleBack();
  assert.equal(nativeBacks, 1);
});

test('member view opened from a project package returns directly to that package', async () => {
  let nativeBacks = 0;
  const page = pageHarness('projectnotes', {}, { navigateBack: () => { nativeBacks += 1; } });
  page.onLoad({ project_id: 'project-one', view: 'members', return_to: 'package' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'members', selectedProject: { project_id: 'project-one' } });
  await page.handleBack();
  assert.equal(nativeBacks, 1);
  assert.equal(page.data.pageView, 'members');
});

test('project creation is a separate step and keeps readable owner labels separate from owner ids', async () => {
  let modal;
  const page = pageHarness('projectnotes', {}, { showModal: options => { modal = options; } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({
    loading: false, owners: ['designer-40', 'company-2'],
    ownerChoices: [
      { owner_id: 'designer-40', display_name: '裴设计师' },
      { owner_id: 'company-2', display_name: '华匠邦' },
    ],
    ownerID: 'designer-40',
  });

  page.openProjectCreation();
  assert.equal(page.data.pageView, 'create');
  assert.equal(page.data.navigationTitle, '新建项目');
  page.setOwnerID({ detail: { value: 1 } });
  assert.equal(page.data.ownerID, 'company-2');
  assert.equal(page.data.ownerIndex, 1);

  page.setProjectName({ detail: { value: '未保存项目' } });
  const pending = page.handleBack();
  assert.equal(page.data.pageView, 'create');
  modal.success({ confirm: true });
  await pending;
  assert.equal(page.data.pageView, 'list');
  assert.equal(page.data.projectName, '');
  assert.equal(page.data.ownerID, 'designer-40');
});

test('project list and detail keep one title and one empty-note action', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(template, /pageView === 'create'/);
  assert.match(template, /range="\{\{ownerChoices\}\}" range-key="display_name"/);
  assert.match(template, /selectedProject\.creator_display_name/);
  assert.doesNotMatch(template, /class="page-title">我的项目/);
  assert.doesNotMatch(template, />开始记录</);
  assert.doesNotMatch(template, /\{\{ownerID \|\| '选择项目归属'\}\}/);
  assert.doesNotMatch(template, /组合方案并指定接收人|版本、意见与确认/);
  assert.match(template, /class="menu-meta">\{\{activeMembers\.length\}\} 人/);
  assert.match(template, /\{\{activeMembers\.length\}\} 位成员/);
  assert.match(template, /wx:if="\{\{activeMembers\.length\}\}" class="card"><text class="section-title">方案确认人/);
  assert.match(template, /wx:if="\{\{projects\.length\}\}" class="primary list-create"/);
  const styles = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxss'), 'utf8');
  assert.match(styles, /\.page \.project-row[^}]*width:100%!important/);
  assert.match(styles, /\.page \.project-list\{overflow:hidden\}/);
  assert.match(styles, /\.page \.menu-card\{overflow:hidden\}/);
  assert.match(styles, /\.page \.menu-row\{min-height:96rpx/);
  assert.match(styles, /\.page \.empty-action\{width:240rpx!important\}/);
});

test('project deep link opens the exact version view and returns to project detail first', async () => {
  const detail = { project: { project_id: 'project-one', name: '张宅' }, notes: [], versions: [], feedbacks: [], shares: [] };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => detail,
    getProjectNoteVersion: async () => ({ project_id: 'project-one', version_id: 'version-one', note_id: 'note-one', revision: 2, title: '确认版', body: '固定正文', body_format: 'plain', media: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page._targetVersionID = 'version-one';
  await page.openProject('project-one', 1);
  assert.equal(page.data.pageView, 'version');
  assert.equal(page.data.versionView.version_id, 'version-one');
  await page.handleBack();
  assert.equal(page.data.pageView, 'detail');
  assert.equal(page.data.selectedProject.project_id, 'project-one');
});

test('project back cannot reopen protected detail after an account change during the discard prompt', async () => {
  let token = 'session-token', modal;
  const page = pageHarness('projectnotes', {}, { getStorageSync: () => token, showModal: options => { modal = options; } });
  page._visible = true; page._token = token; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one', name: '张宅' }, notes: [], versions: [] });
  await page.startNewNote();
  page.setNoteBody({ detail: { value: '修改中的内容' } });
  const pending = page.handleBack();
  token = 'different-account'; page.clearProtected();
  modal.success({ confirm: true });
  await pending;
  assert.equal(page.data.pageView, 'list');
  assert.equal(page.data.selectedProject, null);
});

test('project back waits for a pending save instead of leaving an active editor', async () => {
  const page = pageHarness('projectnotes', {});
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one', name: '张宅' }, notes: [], versions: [] });
  await page.startNewNote(); page.data.busy = true;
  await page.handleBack();
  assert.equal(page.data.pageView, 'editor');
});

test('fixed proposal shows only confirmations belonging to its exact version', () => {
  const page = pageHarness('projectnotes', {});
  page._selectedVersionID = 'version-one';
  page.applyProjectDetail({ project: { project_id: 'project-one', name: '张宅' }, confirmations: [
    { confirmation_id: 'confirm-one', version_id: 'version-one', confirmed_at: '2026-10-31T08:00:00Z' },
    { confirmation_id: 'confirm-two', version_id: 'version-two', confirmed_at: '2026-10-31T09:00:00Z' },
  ] }, '', { version_id: 'version-one', revision: 1 }, null, 'version');
  assert.equal(page.data.confirmations.length, 1);
  assert.equal(page.data.confirmations[0].confirmation_id, 'confirm-one');
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(template, /wx:for="\{\{confirmations\}\}"/);
});

test('project detail opens share materials and keeps profile labels bound to real actor ids', () => {
  let navigated = '';
  const page = pageHarness('projectnotes', {}, { navigateTo: options => { navigated = options.url; } });
  page.applyProjectDetail({
    project: { project_id: 'project-one', name: '张宅', confirmer_id: 101 },
    notes: [], versions: [], feedbacks: [],
    members: [{ member_id: 'member-one', actor_id: 101, active: true, display_name: '张女士', avatar: 'https://public.example/avatar.png' }],
  }, '', null, null, 'detail');
  assert.equal(page.data.activeMembers[0].actor_id, 101);
  assert.equal(page.data.confirmerChoices[1].actor_id, 101);
  assert.equal(page.data.confirmerChoices[1].label, '张女士');
  page.openProjectPackage();
  assert.equal(navigated, '/pages/projectpackage/projectpackage?project_id=project-one');
});

test('project member view renders presentation profile while retaining actor id only as behavior data', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(template, /bindtap="openProjectPackage"/);
  assert.match(template, /item\.display_name/);
  assert.match(template, /item\.avatar/);
  assert.match(template, /data-account="\{\{item\.actor_id\}\}"/);
  assert.doesNotMatch(template, /账号 #/);
});

test('project template separates core views and delegates custom navigation back to the page', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(template, /custom-back="\{\{true\}\}"/);
  assert.match(template, /bind:back="handleBack"/);
  for (const view of ['list', 'create', 'detail', 'editor', 'version', 'members', 'history']) {
    assert.match(template, new RegExp(`pageView === '${view}'`));
  }
  assert.doesNotMatch(template, /CRM 项目关联接入中|转发不会增加权限|完整项目记录/);
});

test('designer ignores account A project response after switching to account B', async () => {
  let token = 'account-a', resolveProjects;
  const projects = new Promise(resolve => { resolveProjects = resolve; });
  const page = pageHarness('projectnotes', { listProjectNoteProjects: () => projects, workbenchContext: async () => ({ owners: ['owner-a'] }) }, { getStorageSync: key => key === 'token' ? token : '' });
  page._visible = true; page._token = token; page._epoch = 1;
  const pending = page.loadProjects(1);
  token = 'account-b'; page._token = token; page._epoch += 1; page.clearProtected();
  resolveProjects({ items: [{ project_id: 'secret-a', name: 'A账号项目' }] });
  await pending;
  assert.equal(page.data.projects.length, 0);
  assert.equal(page.data.ownerID, '');
});

test('shared note sends a named receipt only on an explicit action and retries one stable event id', async () => {
  const openIDs = [];
  const api = {
    getSharedProjectNote: async () => ({
      project: { project_id: 'project-one', name: '张宅', confirmer_id: 101 },
      share: { share_id: 'share-one', share_token: 'opaque-private-token' },
      version: { version_id: 'version-one', title: '已冻结版本', body_format: 'article_v2', body: '{"schema":1,"blocks":[{"kind":"paragraph","text":"只读正文"}]}', media: [] }, can_confirm: false,
    }),
    openSharedProjectNote: async (_token, id) => { openIDs.push(id); if (openIDs.length === 1) throw new Error('temporary'); return {}; },
  };
  const page = pageHarness('projectnoteview', api);
  page.onLoad({ pns: 'opaque-private-token' });
  await page.loadShared();
  assert.equal(page.data.view.version.version_id, 'version-one');
  assert.equal(page.data.canConfirm, false);
  assert.equal(openIDs.length, 0);
  await page.acknowledgeRead();
  await page.reportOpen();
  assert.equal(openIDs.length, 2);
  assert.equal(openIDs[0], openIDs[1]);
});

test('account switch rotates open event and old pending completion cannot clear the new account attempt', async () => {
  let token = 'account-a', resolveA;
  const calls = [];
  const view = { project: { project_id: 'project-one', name: '张宅' }, share: { share_id: 'share-one' }, version: { version_id: 'version-one', title: '固定版本', body_format: 'plain', body: '正文', media: [] }, can_confirm: false };
  const page = pageHarness('projectnoteview', {
    getSharedProjectNote: async () => view,
    openSharedProjectNote: async (_share, eventID) => {
      calls.push({ token, eventID });
      if (token === 'account-a') return new Promise(resolve => { resolveA = resolve; });
      return {};
    },
  }, { getStorageSync: key => key === 'token' ? token : '' });
  page.onLoad({ pns: 'opaque-private-token' });
  page._accountToken = token; page._epoch = 1; page.setData({ view });
  page._feedbackAttempt = { signature: 'account-a-feedback', text: '私密反馈', key: 'old-key' };
  const pendingA = page.reportOpen();
  await Promise.resolve();
  token = 'account-b';
  page.onShow();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 1);
  await page.acknowledgeRead();
  assert.equal(calls.length, 2);
  assert.notEqual(calls[0].eventID, calls[1].eventID);
  assert.equal(calls[1].token, 'account-b');
  assert.equal(page._feedbackAttempt, null);
  resolveA({});
  await pendingA;
  assert.equal(page._openSent, true);
  assert.equal(page._openPending, null);
});

test('project metadata update sends the selected revision and preserves confirmed as a server-owned state', async () => {
  const calls = [];
  const page = pageHarness('projectnotes', { updateProjectNoteProject: async (id, patch) => { calls.push({ id, patch }); return { project_id: id, ...patch, revision: 8, status: 'confirmed' }; } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'project-one', revision: 7, status: 'confirmed' }, projectEdit: { name: '张宅', location: '苏州', building_area: '168.5', floors: '2', style: '现代' } });
  await page.saveProjectDetails();
  assert.equal(calls[0].id, 'project-one');
  assert.equal(calls[0].patch.expected_revision, 7);
  assert.equal(calls[0].patch.building_area, 168.5);
  assert.equal(calls[0].patch.floors, 2);
  assert.equal(calls[0].patch.status, undefined);
  assert.equal(page.data.selectedProject.revision, 8);
  assert.equal(page.data.selectedProject.status, 'confirmed');
});

test('selecting a draft picks its own versions and returning from a fixed version restores that draft', async () => {
  const detail = { project: { project_id: 'project-one' }, notes: [
    { note_id: 'note-b', title: 'B草稿', body: 'B正文', body_format: 'plain', revision: 2 },
    { note_id: 'note-a', title: 'A草稿', body: 'A正文', body_format: 'plain', revision: 3 },
  ], versions: [{ version_id: 'version-b', note_id: 'note-b', revision: 2 }, { version_id: 'version-a', note_id: 'note-a', revision: 1 }], feedbacks: [], shares: [] };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => detail,
    getProjectNoteVersion: async (_, id) => ({ project_id: 'project-one', version_id: id, note_id: 'note-a', revision: 1, title: 'A旧版', body: 'A旧正文', body_format: 'plain', media: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail(detail);
  await page.selectNote({ detail: { value: 1 } });
  assert.equal(page.data.note.note_id, 'note-a');
  assert.equal(page.data.frozenVersion.version_id, 'version-a');
  assert.equal(page.data.versionChoices.length, 1);
  await page.selectVersion({ currentTarget: { dataset: { id: 'version-a' } } });
  assert.equal(page.data.versionView.body, 'A旧正文');
  assert.equal(page.data.note, null);
  await page.returnToDraft();
  assert.equal(page.data.note.note_id, 'note-a');
  assert.equal(page.data.noteBody, 'A正文');
});

test('a new note form stays separate from existing drafts after a refresh', async () => {
  const detail = { project: { project_id: 'p' }, notes: [{ note_id: 'a', title: '已有', body: '正文', body_format: 'plain' }], versions: [], feedbacks: [] };
  const page = pageHarness('projectnotes', { getProjectNoteProject: async () => detail });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail(detail);
  await page.startNewNote();
  await page.refreshProject();
  assert.equal(page.data.note, null);
  assert.equal(page.data.noteTitle, '');
  assert.equal(page.data.frozenVersion, null);
});

test('cancelling a new note keeps the loaded version and share context on project detail', async () => {
  const detail = {
    project: { project_id: 'p', name: '张宅' },
    notes: [{ note_id: 'n', title: '方案', body: '正文', body_format: 'plain', revision: 1 }],
    versions: [{ version_id: 'v', note_id: 'n', title: '方案', revision: 1 }],
    shares: [{ share_id: 's', version_id: 'v', revoked: false }],
    feedbacks: [], confirmations: [],
  };
  const page = pageHarness('projectnotes', {});
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail(detail);
  const versionCount = page.data.versionChoices.length;
  const shareID = page.data.activeShare.share_id;
  await page.startNewNote();
  await page.handleBack();
  assert.equal(page.data.pageView, 'detail');
  assert.equal(page.data.versionChoices.length, versionCount);
  assert.equal(page.data.activeShare.share_id, shareID);
});

test('feedback can be selected directly and reply retries preserve a semantic idempotency key', async () => {
  const calls = []; let count = 0, keys = 0;
  const page = pageHarness('projectnotes', {
    mutationKey: () => 'key-' + ++keys,
    resolveProjectNoteFeedback: async (...args) => { calls.push(args); if (++count === 1) throw new Error('网络中断'); return {}; },
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, feedbacks: [], notes: [], versions: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, feedbacks: [{ feedback_id: 'f', text: '调整厨房', version_id: 'v' }] });
  page.selectFeedback({ currentTarget: { dataset: { id: 'f' } } });
  assert.equal(page.data.selectedFeedback.text, '调整厨房');
  page.setFeedbackReply({ detail: { value: '已调整厨房布局' } });
  await page.resolveProjectNoteFeedback(); await page.resolveProjectNoteFeedback();
  assert.equal(calls.length, 2);
  assert.equal(calls[0][0], 'f'); assert.equal(calls[0][2], calls[1][2]);
});

test('changing satisfaction creates a different feedback retry while preserving its exact version', async () => {
  const calls = []; let key = 0;
  const page = pageHarness('projectnoteview', { mutationKey: () => 'key-' + ++key, addSharedProjectNoteFeedback: async (...args) => { calls.push(args); throw new Error('网络中断'); } });
  page.onLoad({ pns: 'opaque-private-token' }); page._accountToken = 'session-token'; page._epoch = 1;
  page.setData({ view: { share: { share_id: 's' }, version: { version_id: 'v' } }, feedbackText: '请调整厨房' });
  page.setSatisfaction({ detail: { value: 'needs_adjustment' } });
  await page.submitFeedback(); await page.submitFeedback();
  assert.equal(calls[0][3], 'needs_adjustment'); assert.equal(calls[0][2], calls[1][2]);
  page.setSatisfaction({ detail: { value: 'unsatisfied' } });
  await page.submitFeedback();
  assert.equal(calls[2][3], 'unsatisfied'); assert.notEqual(calls[0][2], calls[2][2]);
});

test('project AI carries only known facts and inserts into the same note without losing media', async () => {
  const sent = [];
  const page = pageHarness('projectnotes', {}, {
    navigateTo: () => { throw new Error('AI 辅助不得跳页'); },
    aiStream: { startAIStream: options => { sent.push(options); return { abort() {} }; } },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅', location: '苏州', floors: 2, crm_id: 'private-crm', revision: 1 }, note: { note_id: 'n', revision: 3 }, noteTitle: '方案说明', noteBody: '已知事实', noteMedia: [{ media_id: 'private-media' }], feedbacks: [{ text: '私人反馈' }] });
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  assert.equal(page.data.aiPanelOpen, true);
  assert.equal(page.data.aiPanelStreaming, false);
  page.startNoteAI();
  assert.equal(sent.length, 1, '就地发起流式生成');
  const prompt = sent[0].messages[0].content;
  assert.match(prompt, /苏州/);
  assert.doesNotMatch(prompt, /private-|私人反馈|建筑面积/);
  sent[0].onChunk('新的');
  sent[0].onDone('新的方案说明');
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, '已知事实\n\n新的方案说明');
  page.onHide();
  await page.onShow();
  assert.equal(page.data.noteMedia[0].media_id, 'private-media');
  assert.equal(page.data.note.note_id, 'n');
});

test('project AI rejects generation and insertion after the identity changes', async () => {
  const calls = [];
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: options => { calls.push(options); return { abort() {} }; } } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page._token = 'session-two';
  page.startNoteAI();
  assert.equal(calls.length, 0, '身份变化后不得发起生成');
  page.setData({ aiPanelText: '不能采用' });
  await page.insertNoteAI();
  assert.doesNotMatch(page.data.noteBody, /不能采用/);
});

test('freezing requires the current form to be saved and never silently shares an older draft', async () => {
  let calls = 0;
  const page = pageHarness('projectnotes', { freezeProjectNoteVersion: async () => { calls++; return {}; } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 2, title: '原题', body: '已保存', body_format: 'plain', media_ids: [] }, noteTitle: '新标题', noteBody: '已保存' });
  await page.freezeProjectNoteVersion();
  assert.equal(calls, 0);
  assert.match(page.data.error, /先保存/);
});

test('project metadata PATCH and feedback satisfaction use the signed private contract', async () => {
  const { api, calls } = loadAPI();
  await api.updateProjectNoteProject('p', { expected_revision: 2, location: '苏州' });
  await api.addSharedProjectNoteFeedback('opaque-token', '调整厨房', 'stable-key', 'needs_adjustment');
  assert.equal(calls[0].options.method, 'PATCH');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/project-notes/projects/p');
  assert.equal(calls[0].options.data.expected_revision, 2);
  assert.equal(calls[1].options.data.satisfaction, 'needs_adjustment');
});

test('copy selects the new draft while delete clears only the selected draft after confirmation', async () => {
  const calls = [], project = { project_id: 'p' };
  let notes = [{ note_id: 'n', revision: 3, title: '原方案', body: '正文', body_format: 'plain' }];
  const page = pageHarness('projectnotes', {
    copyProjectNote: async (id, revision) => { calls.push(['copy', id, revision]); const copy = { ...notes[0], note_id: 'copy', revision: 1 }; notes.push(copy); return copy; },
    deleteProjectNote: async (id, revision) => { calls.push(['delete', id, revision]); notes = notes.filter(note => note.note_id !== id); return {}; },
    getProjectNoteProject: async () => ({ project, notes, versions: [], feedbacks: [], shares: [] }),
  }, { showModal: options => options.success({ confirm: true }) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project, notes, versions: [] });
  await page.copyProjectNote();
  assert.equal(page.data.note.note_id, 'copy');
  await page.deleteProjectNote();
  assert.deepEqual(calls, [['copy', 'n', 3], ['delete', 'copy', 1]]);
  assert.equal(page.data.note, null);
  assert.equal(page.data.notes.length, 1);
});

test('long-press deleting the selected note clears its editor baseline so returning to projects needs no discard confirmation', async () => {
  let modalCount = 0;
  const project = { project_id: 'p', name: '验收项目' };
  const note = { note_id: 'n', revision: 1, title: '验收笔记', body: '已保存正文', body_format: 'plain' };
  const page = pageHarness('projectnotes', {
    deleteProjectNote: async () => ({}),
  }, {
    showActionSheet: options => options.success({ tapIndex: 0 }),
    showModal: options => { modalCount++; options.success({ confirm: true }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project, notes: [note], versions: [] });

  await page.openNoteDeleteMenu({ currentTarget: { dataset: { id: 'n' } } });
  assert.equal(page.data.note, null);
  assert.equal(page.data.noteTitle, '');
  assert.equal(page.data.noteBody, '');
  assert.equal(page.data.noteMedia.length, 0);
  assert.equal(page._selectedNoteID, '');
  assert.equal(page.hasUnsavedChanges(), false);

  await page.handleBack();
  assert.equal(modalCount, 1);
  assert.equal(page.data.pageView, 'list');
});

test('long-press deleting another note does not mark the selected note edits as saved', async () => {
  let modalCount = 0;
  const project = { project_id: 'p', name: '验收项目' };
  const selected = { note_id: 'selected', revision: 2, title: '当前笔记', body: '已保存正文', body_format: 'plain' };
  const other = { note_id: 'other', revision: 1, title: '其它笔记', body: '其它正文', body_format: 'plain' };
  const page = pageHarness('projectnotes', {
    deleteProjectNote: async () => ({}),
  }, {
    showActionSheet: options => options.success({ tapIndex: 0 }),
    showModal: options => { modalCount++; options.success({ confirm: modalCount === 1 }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page._selectedNoteID = 'selected';
  page.applyProjectDetail({ project, notes: [selected, other], versions: [] });
  const baseline = page._noteFormBaseline;
  const loadedBody = page._loadedFormState.noteBody;
  page.setNoteBody({ detail: { value: '尚未保存的正文' } });

  await page.openNoteDeleteMenu({ currentTarget: { dataset: { id: 'other' } } });
  assert.equal(page.data.note.note_id, 'selected');
  assert.equal(page.data.noteBody, '尚未保存的正文');
  assert.equal(page._noteFormBaseline, baseline);
  assert.equal(page._loadedFormState.noteBody, loadedBody);
  assert.equal(page.hasUnsavedChanges(), true);

  await page.handleBack();
  assert.equal(modalCount, 2);
  assert.equal(page.data.pageView, 'detail');
});

test('designer history paginates with bounded replacement and can return to the previous page', async () => {
  const calls = [];
  const page = pageHarness('projectnotes', { getProjectNoteHistory: async (project, type, query) => {
    calls.push({ project, type, ...query });
    return query.cursor ? { items: [{ version_id: 'v-old', title: '旧方案', revision: 1, created_at: '2026-11-02T16:59:42.883Z' }], next_cursor: '' } : { items: [{ version_id: 'v-new', title: '新方案', revision: 2, created_at: '2026-11-03T08:09:10Z' }], next_cursor: 'next' };
  } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.setData({ selectedProject: { project_id: 'p' } });
  await page.loadHistory();
  await page.loadHistory({ currentTarget: { dataset: { direction: 'next' } } });
  assert.equal(page.data.historyItems.length, 1); assert.equal(page.data.historyItems[0].version_id, 'v-old');
  await page.loadHistory({ currentTarget: { dataset: { direction: 'previous' } } });
  assert.equal(page.data.historyItems[0].version_id, 'v-new');
  assert.deepEqual(calls.map(item => item.cursor), ['', 'next', '']);
  assert.ok(calls.every(item => item.project === 'p' && item.type === 'versions' && item.limit <= 50));
  assert.match(page.data.historyItems[0].displayTime, /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
});

test('project workspace scrolls new in-page views to top and keeps copy/delete under more', async () => {
  const scrolls = [];
  let sheet;
  const page = pageHarness('projectnotes', {}, {
    pageScrollTo: options => scrolls.push(options),
    showActionSheet: options => { sheet = options; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 1 }, pageView: 'detail' });
  await page.startNewNote();
  assert.equal(scrolls.at(-1).scrollTop, 0);
  assert.equal(scrolls.at(-1).duration, 0);
  page.setData({ note: { note_id: 'n', revision: 1 }, historyMode: false });
  page.openNoteMore();
  assert.equal(sheet.itemList.join(','), '复制笔记,删除笔记');
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(template, /bindtap="openNoteMore"/);
  assert.doesNotMatch(template, /class="inline-actions"[\s\S]*?bindtap="copyProjectNote"/);
});

test('old notification loads its exact feedback and old selected draft outside the initial window', async () => {
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [], versions: [], feedbacks: [] }),
    getProjectNoteVersion: async () => ({ project_id: 'p', version_id: 'old-v', note_id: 'old-n', title: '旧版', body: '旧文', body_format: 'plain' }),
    getProjectNoteFeedback: async () => ({ project_id: 'p', version_id: 'old-v', feedback_id: 'old-f', text: '旧意见', reply: '旧回复', resolved: true }),
    getProjectNote: async () => ({ project_id: 'p', note_id: 'old-n', title: '当前旧笔记草稿', body: '可编辑', body_format: 'plain', revision: 3 }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._targetVersionID = 'old-v'; page._targetFeedbackID = 'old-f';
  await page.openProject('p', 1);
  assert.equal(page.data.selectedFeedback.text, '旧意见');
  assert.equal(page.data.selectedFeedback.resolution_reply, '旧回复');
  await page.returnToDraft();
  assert.equal(page.data.note.note_id, 'old-n'); assert.equal(page.data.note.revision, 3);
});

test('shared history never appends old-account results to the current protected view', async () => {
  let token = 'a', finish;
  const page = pageHarness('projectnoteview', { getSharedProjectNoteHistory: () => new Promise(resolve => { finish = resolve; }) }, { getStorageSync: key => key === 'token' ? token : '' });
  page._visible = true; page._accountToken = token; page._epoch = 1; page._shareToken = 'opaque-token'; page.setData({ view: { version: { version_id: 'v' } } });
  const pending = page.loadActivityHistory();
  token = 'b'; page.clearProtected(); finish({ items: [{ text: '旧账号私密意见' }] }); await pending;
  assert.equal(page.data.activityItems.length, 0);
});

test('feedback lost-response retry reuses one semantic idempotency key and resets after success or text change', async () => {
  const keys = [];
  let sequence = 0, calls = 0;
  const api = {
    mutationKey: () => `feedback-key-${++sequence}`,
    addSharedProjectNoteFeedback: async (_token, _text, key) => { keys.push(key); calls += 1; if (calls === 1) throw new Error('响应丢失'); return { feedback_id: `feedback-${calls}`, text: _text }; },
  };
  const page = pageHarness('projectnoteview', api);
  page.onLoad({ pns: 'opaque-private-token' });
  page._accountToken = 'session-token'; page._epoch = 1;
  page.setData({ view: { share: { share_id: 'share-one' }, version: { version_id: 'version-one' }, feedbacks: [] }, feedbackText: '请调整厨房' });
  await page.submitFeedback();
  await page.submitFeedback();
  assert.deepEqual(keys, ['feedback-key-1', 'feedback-key-1']);
  assert.equal(page._feedbackAttempt, null);

  page.setData({ feedbackText: '第一条' });
  calls = 0;
  await page.submitFeedback();
  page.setFeedbackText({ detail: { value: '第二条' } });
  calls = 1;
  await page.submitFeedback();
  assert.equal(keys.at(-2), 'feedback-key-2');
  assert.equal(keys.at(-1), 'feedback-key-3');
});

test('designer preserves a freshly issued private token after returning from preview', async () => {
  const detail = {
    project: { project_id: 'project-one', name: '张宅' }, notes: [], versions: [{ version_id: 'version-one', note_id: 'note-one', revision: 1 }],
    shares: [{ share_id: 'share-one', version_id: 'version-one', revoked: false }], feedbacks: [], members: [], truncated: {},
  };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => detail,
    createProjectNoteShare: async () => ({ share_id: 'share-one', version_id: 'version-one', share_token: 'token-from-first-response' }),
    mutationKey: () => 'stable-share-key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: detail.project, frozenVersion: detail.versions[0] });
  await page.createProjectNoteShare();
  await page.openProject('project-one', 1);
  assert.equal(page.data.activeShare.share_token, 'token-from-first-response');
});

test('share retry keeps one idempotency key and expiry, and an empty replay token is not success', async () => {
  const attempts = [];
  let call = 0;
  const page = pageHarness('projectnotes', {
    createProjectNoteShare: async (_id, expires, key) => { attempts.push({ expires, key }); call += 1; if (call === 1) throw Object.assign(new Error('网络中断'), { networkError: true }); return { share_id: 'share-one', version_id: 'version-one' }; },
    mutationKey: () => 'stable-share-key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.setData({ frozenVersion: { version_id: 'version-one' } });
  await page.createProjectNoteShare();
  await page.createProjectNoteShare();
  assert.deepEqual(attempts[0], attempts[1]);
  assert.equal(page.data.activeShare.share_token, undefined);
  assert.match(page.data.error, /凭证|撤回/);
  assert.doesNotMatch(page.data.notice, /入口已生成/);
});

test('structured note title edit preserves complete body and private media ids', () => {
  const page = pageHarness('projectnotes', {});
  const body = JSON.stringify({ schema: 1, layout_html: '<h2>标题</h2><ul><li>条目</li></ul>', blocks: [{ kind: 'image', media_id: 'private-image' }, { kind: 'video', media_id: 'private-video' }] });
  page.setData({ note: { note_id: 'note-one', revision: 3, title: '旧标题', body, body_format: 'article_v2', cover_media_id: 'private-image', media_ids: ['private-image', 'private-video'] }, noteTitle: '新标题', noteBody: '不得覆盖', bodyReadOnly: true, noteMedia: [{ media_id: 'private-image' }, { media_id: 'private-video' }] });
  const draft = page.noteDraft();
  assert.equal(draft.body, body);
  assert.equal(draft.body_format, 'article_v2');
  assert.equal(draft.cover_media_id, 'private-image');
  assert.deepEqual(Array.from(draft.media_ids), ['private-image', 'private-video']);
});

test('ordinary editable note uses the backend-supported plain body format', () => {
  const page = pageHarness('projectnotes', {});
  page.setData({ noteTitle: '现场记录', noteBody: '保留现场尺寸', noteMedia: [{ media_id: 'private-image', kind: 'image' }] });
  const draft = page.noteDraft();
  assert.equal(draft.body_format, 'plain');
  assert.equal(draft.body, '保留现场尺寸');
  assert.equal(draft.cover_media_id, 'private-image');
});

test('private media upload unwraps the project-only asset response', async () => {
  const page = pageHarness('projectnotes', { uploadProjectNoteMedia: async () => ({ project_id: 'project-one', owner_id: 'owner-one', asset: { media_id: 'private-image', kind: 'image', title: '现场.jpg' } }) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.setData({ selectedProject: { project_id: 'project-one' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/photo.jpg', fileType: 'image' }]);
  assert.equal(page.data.noteMedia[0].media_id, 'private-image');
  assert.equal(page.data.noteMedia[0].title, '现场.jpg');
});

test('project list follows next_cursor and exact version outside the first fifty remains readable', async () => {
  const projects = Array.from({ length: 31 }, (_, index) => ({ project_id: `project-${index + 1}`, name: `项目${index + 1}` }));
  const listCalls = [];
  const page = pageHarness('projectnotes', {
    listProjectNoteProjects: async query => { listCalls.push(query || {}); return listCalls.length === 1 ? { items: projects.slice(0, 30), next_cursor: 'page-2' } : { items: projects.slice(30) }; },
    workbenchContext: async () => ({ owners: ['owner-one'] }),
    getProjectNoteProject: async () => ({ project: projects[30], notes: [{ note_id: 'note-first', title: '错误草稿', body: 'wrong', body_format: 'plain_text' }], versions: Array.from({ length: 50 }, (_, index) => ({ version_id: `old-${index}`, note_id: 'note-first' })), shares: [], feedbacks: [{ feedback_id: 'wrong', version_id: 'old-0' }], confirmations: [{ confirmation_id: 'wrong', version_id: 'old-0' }], truncated: { versions: true } }),
    getProjectNoteVersion: async (project, version) => { assert.equal(project, 'project-31'); assert.equal(version, 'wanted-51'); return { project_id: project, version_id: version, note_id: 'note-first', revision: 1, title: '真正历史版本', body_format: 'plain', body: '固定正文', media: [] }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._targetProjectID = 'project-31'; page._targetVersionID = 'wanted-51';
  await page.loadProjects(1);
  assert.equal(page.data.projects.length, 31);
  assert.equal(listCalls[1].cursor, 'page-2');
  assert.equal(page.data.frozenVersion.version_id, 'wanted-51');
  assert.equal(page.data.note, null);
  assert.equal(page.data.feedbacks.length, 0);
  assert.equal(page.data.historyMode, true);
  assert.equal(page.data.versionView.body, '固定正文');
});

test('a V1 notification never renders or edits the latest V2 draft', async () => {
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'project-one' }, notes: [{ note_id: 'note-one', revision: 2, title: '新标题', body: '这是V2正文', body_format: 'plain' }], versions: [{ version_id: 'version-one', note_id: 'note-one', revision: 1 }], shares: [], feedbacks: [] }),
    getProjectNoteVersion: async () => ({ project_id: 'project-one', version_id: 'version-one', note_id: 'note-one', revision: 1, title: '旧标题', body: '这是V1正文', body_format: 'plain', media: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._targetVersionID = 'version-one';
  await page.openProject('project-one', 1);
  assert.equal(page.data.versionView.body, '这是V1正文');
  assert.equal(page.data.note, null);
  assert.equal(page.data.noteBody, '');
  assert.equal(page.data.historyMode, true);
});

test('failed exact version does not replace the requested historical view with a latest draft', async () => {
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'project-one' }, notes: [{ note_id: 'note-one', body: 'V2' }], versions: [{ version_id: 'version-two', note_id: 'note-one' }] }),
    getProjectNoteVersion: async () => { throw new Error('版本不可访问'); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._targetVersionID = 'version-one';
  await page.openProject('project-one', 1);
  assert.equal(page.data.versionView, null); assert.equal(page.data.note, null);
  assert.match(page.data.error, /不可访问/);
});

test('late fixed-version result after account change cannot restore private content', async () => {
  let token = 'account-a', finish;
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'project-one' }, notes: [], versions: [] }),
    getProjectNoteVersion: () => new Promise(resolve => { finish = resolve; }),
  }, { getStorageSync: key => key === 'token' ? token : '' });
  page._visible = true; page._token = token; page._epoch = 1; page._targetVersionID = 'version-one';
  const pending = page.openProject('project-one', 1); await new Promise(resolve => setTimeout(resolve, 0));
  token = 'account-b'; page._token = token; page._epoch += 1; page.clearProtected();
  finish({ project_id: 'project-one', version_id: 'version-one', body_format: 'plain', body: 'A的正文', media: [] });
  await pending;
  assert.equal(page.data.versionView, null); assert.equal(page.data.frozenVersion, null);
});

test('a failed fixed-version media load never renders a partial snapshot or continues queued downloads', async () => {
  const downloads = []; let finishSecond;
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'project-one' }, notes: [], versions: [] }),
    getProjectNoteVersion: async () => ({ project_id: 'project-one', version_id: 'version-one', body_format: 'plain', body: '固定正文', media: [{ media_id: 'image-one', kind: 'image' }, { media_id: 'image-two', kind: 'image' }, { media_id: 'image-three', kind: 'image' }] }),
    downloadProjectNoteMedia: async (_project, id) => { downloads.push(id); if (id === 'image-one') throw new Error('素材缺失'); return new Promise(resolve => { finishSecond = resolve; }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._targetVersionID = 'version-one';
  await page.openProject('project-one', 1);
  finishSecond('/tmp/image-two.png'); await new Promise(resolve => setTimeout(resolve, 0));
  assert.deepEqual(downloads, ['image-one', 'image-two']);
  assert.equal(page.data.versionView, null); assert.equal(page.data.versionMedia.length, 0);
  assert.match(page.data.error, /不可访问/);
});

test('Word opens the selected fixed version and late download cannot open across accounts', async () => {
  let token = 'account-a', finish; const opened = [], requested = [];
  const page = pageHarness('projectnotes', {
    downloadProjectNoteWord: async (project, version) => { requested.push([project, version]); return new Promise(resolve => { finish = resolve; }); },
  }, { getStorageSync: key => key === 'token' ? token : '', openDocument: options => { opened.push(options); options.success(); } });
  page._visible = true; page._token = token; page._epoch = 1; page.setData({ selectedProject: { project_id: 'project-one' }, frozenVersion: { version_id: 'version-one' } });
  const pending = page.exportProjectNoteWord();
  token = 'account-b'; page._token = token; page._epoch += 1; page.clearProtected();
  finish('/tmp/v1.docx'); await pending;
  assert.deepEqual(requested, [['project-one', 'version-one']]); assert.equal(opened.length, 0);
});

test('shared Word download opens only after success and image saving reauthorizes original bytes', async () => {
  const opened = [], saved = [], downloads = [];
  const page = pageHarness('projectnoteview', {
    downloadSharedProjectNoteWord: async token => { downloads.push(['word', token]); return '/tmp/shared-v1.docx'; },
    downloadSharedProjectNoteMedia: async (token, id) => { downloads.push(['image', token, id]); return '/tmp/rechecked-original.png'; },
  }, { openDocument: options => { opened.push(options); options.success(); }, saveImageToPhotosAlbum: options => { saved.push(options.filePath); options.success(); } });
  page.onLoad({ pns: 'private-share-one' }); page._accountToken = 'session-token'; page._epoch = 1;
  page.setData({ view: { version: { version_id: 'version-one' } }, media: [{ media_id: 'image-one', kind: 'image', local_path: '/tmp/old-cache.png' }, { media_id: 'video-one', kind: 'video' }] });
  await page.exportSharedProjectNoteWord();
  assert.equal(opened[0].filePath, '/tmp/shared-v1.docx'); assert.equal(opened[0].fileType, 'docx'); assert.equal(opened[0].showMenu, true);
  await page.saveOriginalImage({ currentTarget: { dataset: { id: 'image-one' } } });
  await page.saveOriginalImage({ currentTarget: { dataset: { id: 'video-one' } } });
  assert.deepEqual(saved, ['/tmp/rechecked-original.png']);
  assert.deepEqual(downloads, [['word', 'private-share-one'], ['image', 'private-share-one', 'image-one']]);
});

test('failed shared export or revoked original image never opens or saves a cached file', async () => {
  const opened = [], saved = [];
  const page = pageHarness('projectnoteview', {
    downloadSharedProjectNoteWord: async () => { throw new Error('分享已撤回'); },
    downloadSharedProjectNoteMedia: async () => { throw new Error('分享已撤回'); },
  }, { openDocument: options => opened.push(options), saveImageToPhotosAlbum: options => saved.push(options) });
  page.onLoad({ pns: 'private-share-one' }); page._accountToken = 'session-token'; page._epoch = 1;
  page.setData({ view: { version: { version_id: 'version-one' } }, media: [{ media_id: 'image-one', kind: 'image', local_path: '/tmp/old-cache.png' }] });
  await page.exportSharedProjectNoteWord();
  await page.saveOriginalImage({ currentTarget: { dataset: { id: 'image-one' } } });
  assert.equal(opened.length, 0); assert.equal(saved.length, 0); assert.match(page.data.notice, /撤回/);
});

test('an in-flight Word download cannot open after switching the selected project version', async () => {
  let finish; const opened = [];
  const page = pageHarness('projectnotes', { downloadProjectNoteWord: () => new Promise(resolve => { finish = resolve; }) }, { openDocument: options => { opened.push(options); options.success(); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'project-one' }, frozenVersion: { version_id: 'version-one' } });
  const pending = page.exportProjectNoteWord();
  page.setData({ selectedProject: { project_id: 'project-two' }, frozenVersion: { version_id: 'version-two' }, busy: false });
  finish('/tmp/old-version.docx'); await pending;
  assert.equal(opened.length, 0);
});

test('approved join request refreshes active members and confirmer is selected from them', async () => {
  let reads = 0;
  const detail = () => ({ project: { project_id: 'project-one', name: '张宅', confirmer_id: reads > 1 ? 202 : 0 }, notes: [], versions: [], shares: [], feedbacks: [], confirmations: [], truncated: {}, members: [{ actor_id: 202, active: true }] });
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => { reads += 1; return detail(); }, approveProjectJoinRequest: async () => ({}), listProjectJoinRequests: async () => ({ items: [] }), setProjectNoteConfirmer: async () => ({}), mutationKey: () => 'approval-key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.setData({ selectedProject: { project_id: 'project-one' } });
  await page.approveProjectJoinRequest({ currentTarget: { dataset: { id: 'request-one' } } });
  assert.equal(page.data.activeMembers[0].actor_id, 202);
  page.setData({ confirmerIndex: 1, confirmerAccount: 202 });
  await page.setProjectNoteConfirmer();
  assert.equal(page.data.selectedProject.confirmer_id, 202);
});

test('private share card uses pns only and never enters public tracking or a fabricated H5 URL', () => {
  const source = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.ts'), 'utf8');
  assert.match(source, /pages\/projectnoteview\/projectnoteview\?pns=/);
  assert.doesNotMatch(source, /(?:\?|&)st=|utils\/tracking|https:\/\//);
  assert.match(source, /shareReady/);
});

test('private reader template uses native branches for view, rich text, truncation and completed confirmation', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.wxml'), 'utf8');
  const document = fs.readFileSync(path.join(root, 'components/project-note-document/note-document.wxml'), 'utf8');
  assert.doesNotMatch(template, /wx:else-if=/);
  assert.match(template, /<block\s+wx:elif="\{\{view\}\}">/);
  assert.match(template, /<template[^>]+is="note-document"/);
  assert.match(document, /format === 'rich_text'/);
  assert.match(document, /nodes="\{\{body\}\}"/);
  assert.match(document, />\{\{body\}\}<\/text>/);
  assert.match(template, /wx:if="\{\{view\.activity_truncated\}\}"[^>]*>[^<]*最近 50 条记录/);
  assert.match(template, /wx:if="\{\{!memberMode && \(canConfirm \|\| confirmed\)\}\}"\s+class="card confirm"/);
  assert.match(template, /disabled="\{\{busy \|\| confirmed\}\}"/);
});

test('private reader joins designer resolution replies onto feedback without rendering technical ids', async () => {
  const view = {
    project: { project_id: 'project-one', name: '张宅' }, share: { share_id: 'share-one' },
    version: { version_id: 'version-one', title: '固定版本', body_format: 'plain', body: '正文', media: [] },
    feedbacks: [{ feedback_id: 'feedback-one', text: '厨房动线需要调整', resolved: true }, { feedback_id: 'feedback-two', text: '卧室尺寸确认', resolved: false }],
    resolutions: [{ resolution_id: 'resolution-secret', feedback_id: 'feedback-one', reply: '已将冰箱移到入口侧，并扩大过道。' }],
    can_confirm: false,
  };
  const page = pageHarness('projectnoteview', { getSharedProjectNote: async () => view, openSharedProjectNote: async () => ({}) });
  page.onLoad({ pns: 'opaque-private-token' });
  await page.loadShared();
  assert.equal(page.data.feedbackRows[0].resolution_reply, '已将冰箱移到入口侧，并扩大过道。');
  assert.equal(page.data.feedbackRows[1].resolution_reply, '');

  const template = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.wxml'), 'utf8');
  assert.match(template, /wx:for="\{\{feedbackRows\}\}"/);
  assert.match(template, /设计师处理说明：\{\{item\.resolution_reply\}\}/);
  assert.doesNotMatch(template, /\{\{item\.(?:feedback_id|resolution_id)\}\}/);
});

test('designer management page exposes fixed-version workflow actions and workbench navigates to it', () => {
  const source = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes-core.js'), 'utf8');
  for (const action of ['createProjectNoteProject', 'createProjectInvitation', 'approveProjectJoinRequest', 'setProjectNoteConfirmer', 'createProjectNote', 'saveProjectNote', 'freezeProjectNoteVersion', 'createProjectNoteShare', 'revokeProjectNoteShare', 'resolveProjectNoteFeedback']) {
    assert.match(source, new RegExp(action));
  }
  const workbench = fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.ts'), 'utf8');
  assert.match(workbench, /openNotes\(\).*\/pages\/projectnotes\/projectnotes/);
});

test('an unversioned draft has no feedback confirmation or share and ignores a deleted latest note', () => {
  const page = pageHarness('projectnotes', {});
  page._newNote = true;
  page.applyProjectDetail({
    project: { project_id: 'project-one' },
    notes: [{ note_id: 'active-note', title: '活动草稿', body: '正文', body_format: 'plain' }],
    latest_version: { version_id: 'deleted-version', note_id: 'deleted-note', revision: 3 },
    versions: [{ version_id: 'deleted-version', note_id: 'deleted-note', revision: 3 }],
    active_share: { share_id: 'old-share', version_id: 'deleted-version' },
    shares: [{ share_id: 'old-share', version_id: 'deleted-version', revoked: false }],
    feedbacks: [{ feedback_id: 'old-feedback', version_id: 'deleted-version', text: '旧意见' }],
    confirmations: [{ confirmation_id: 'old-confirmation', version_id: 'deleted-version' }],
  });
  assert.equal(page.data.note, null);
  assert.equal(page.data.frozenVersion, null);
  assert.equal(page.data.activeShare, null);
  assert.deepEqual(Array.from(page.data.feedbacks), []);
  assert.deepEqual(Array.from(page.data.confirmations), []);

  page._newNote = false;
  page._selectedNoteID = '';
  page.applyProjectDetail({
    project: { project_id: 'project-one' },
    notes: [{ note_id: 'active-note', title: '活动草稿', body: '正文', body_format: 'plain' }],
    latest_version: { version_id: 'deleted-version', note_id: 'deleted-note', revision: 3 },
    versions: [{ version_id: 'deleted-version', note_id: 'deleted-note', revision: 3 }],
  });
  assert.equal(page.data.note.note_id, 'active-note');
  assert.equal(page.data.frozenVersion, null);
});

test('every draft-replacing action asks before discarding unsaved note input', async () => {
  const detail = {
    project: { project_id: 'project-one' },
    notes: [
      { note_id: 'note-one', revision: 2, title: '草稿一', body: '已保存', body_format: 'plain' },
      { note_id: 'note-two', revision: 1, title: '草稿二', body: '另一篇', body_format: 'plain' },
    ],
    versions: [{ version_id: 'version-one', note_id: 'note-one', revision: 1 }],
    shares: [], feedbacks: [], confirmations: [],
  };
  const actions = [
    page => page.selectProject({ currentTarget: { dataset: { id: 'project-two' } } }),
    page => page.selectNote({ detail: { value: 1 } }),
    page => page.selectVersion({ currentTarget: { dataset: { id: 'version-one' } } }),
    page => page.startNewNote(),
    page => { page.setData({ historyType: 'versions', historyItems: [{ version_id: 'version-one' }] }); return page.openHistoryItem({ currentTarget: { dataset: { index: 0 } } }); },
    page => page.returnToDraft(),
    page => page.copyProjectNote(),
  ];
  for (const action of actions) {
    let modalCount = 0, readCount = 0, copyCount = 0;
    const page = pageHarness('projectnotes', {
      getProjectNoteProject: async () => { readCount += 1; return detail; },
      copyProjectNote: async () => { copyCount += 1; return detail.notes[1]; },
    }, { showModal: options => { modalCount += 1; options.success({ confirm: false }); } });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.applyProjectDetail(detail);
    page.setData({ noteBody: '尚未保存的新内容' });
    await action(page);
    assert.equal(modalCount, 1);
    assert.equal(readCount, 0);
    assert.equal(copyCount, 0);
    assert.equal(page.data.note.note_id, 'note-one');
    assert.equal(page.data.noteBody, '尚未保存的新内容');
  }
});

test('same-account project resume revalidates access without replacing a dirty form', async () => {
  let mode = 'ok', reads = 0;
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => {
      reads += 1;
      if (mode === 'denied') throw Object.assign(new Error('已撤权'), { statusCode: 403 });
      if (mode === 'network') throw Object.assign(new Error('网络中断'), { networkError: true });
      return { project: { project_id: 'project-one' } };
    },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one' }, notes: [{ note_id: 'note-one', revision: 1, title: '标题', body: '已保存', body_format: 'plain' }], versions: [] });
  page.setData({ noteBody: '待保存输入' });
  await page.onShow();
  assert.equal(reads, 1);
  assert.equal(page.data.noteBody, '待保存输入');

  mode = 'network';
  await page.onShow();
  assert.equal(page.data.noteBody, '待保存输入');
  assert.equal(page.data.accessRecheckRequired, true);
  assert.match(page.data.notice, /重试/);

  mode = 'ok';
  await page.retryProjectAccess();
  assert.equal(page.data.accessRecheckRequired, false);
  assert.equal(page.data.noteBody, '待保存输入');

  mode = 'denied';
  await page.onShow();
  assert.equal(page.data.noteBody, '');
  assert.equal(page.data.selectedProject, null);
  assert.match(page.data.error, /权限|访问/);
});

test('same-account shared resume preserves feedback input and does not redownload media', async () => {
  let mode = 'ok', reads = 0, downloads = 0, submissions = 0;
  const view = { project: { project_id: 'project-one' }, share: { share_id: 'share-one' }, version: { version_id: 'version-one', title: '方案', body_format: 'plain', body: '正文', media: [{ media_id: 'image-one', kind: 'image' }] }, feedbacks: [], can_confirm: true };
  const page = pageHarness('projectnoteview', {
    getSharedProjectNote: async () => {
      reads += 1;
      if (mode === 'denied') throw Object.assign(new Error('已撤权'), { statusCode: 403 });
      if (mode === 'network') throw Object.assign(new Error('网络中断'), { networkError: true });
      return view;
    },
    downloadSharedProjectNoteMedia: async () => { downloads += 1; return '/tmp/image-one.png'; },
    addSharedProjectNoteFeedback: async () => { submissions += 1; return {}; },
    mutationKey: () => 'feedback-key',
  });
  page.onLoad({ pns: 'opaque-private-token' });
  page._accountToken = 'session-token'; page._epoch = 1; page._accessVerified = true;
  page.setData({ view, media: [{ media_id: 'image-one', kind: 'image', local_path: '/tmp/cached.png' }], feedbackText: '待提交意见', satisfaction: 'needs_adjustment', shareReady: true, canConfirm: true });
  await page.onShow();
  assert.equal(reads, 1);
  assert.equal(downloads, 0);
  assert.equal(page.data.feedbackText, '待提交意见');
  assert.equal(page.data.satisfaction, 'needs_adjustment');
  assert.equal(page.data.media[0].local_path, '/tmp/cached.png');

  mode = 'network';
  await page.onShow();
  assert.equal(page.data.feedbackText, '待提交意见');
  assert.equal(page.data.satisfaction, 'needs_adjustment');
  assert.match(page.data.error, /重试/);
  await page.submitFeedback();
  assert.equal(submissions, 0);

  mode = 'denied';
  await page.retry();
  assert.equal(page.data.view, null);
  assert.equal(page.data.feedbackText, '');
  assert.equal(page.data.satisfaction, '');
});

test('an absent form baseline is clean and never opens a discard dialog', async () => {
  let modalCount = 0;
  const page = pageHarness('projectnotes', {}, { showModal: () => { modalCount += 1; } });
  page.setData({ selectedProject: { project_id: 'project-one' }, note: { note_id: 'note-one', revision: 1 }, noteTitle: '临时标题', noteBody: '临时正文' });
  assert.equal(page.hasUnsavedChanges(), false);
  assert.equal(await page.confirmDiscardChanges(), true);
  assert.equal(modalCount, 0);
});

test('cancelled project creation and collaboration mutations keep unsaved draft input', async () => {
  const detail = {
    project: { project_id: 'project-one', revision: 2, status: 'initial' },
    notes: [{ note_id: 'note-one', revision: 2, title: '标题', body: '已保存', body_format: 'plain', media_ids: [] }],
    versions: [], shares: [], feedbacks: [{ feedback_id: 'feedback-one', text: '请调整' }], confirmations: [], members: [{ actor_id: 202, active: true }],
  };
  const actions = [
    page => { page.setData({ loading: false, projectName: '新项目', ownerID: 'owner-one' }); return page.createProjectNoteProject(); },
    page => page.revokeProjectNoteMember({ currentTarget: { dataset: { account: 202 } } }),
    page => { page.setData({ confirmerAccount: 202 }); return page.setProjectNoteConfirmer(); },
    page => { page.setData({ feedbackID: 'feedback-one', feedbackReply: '已处理' }); return page.resolveProjectNoteFeedback(); },
  ];
  for (const action of actions) {
    let mutationCount = 0, modalCount = 0;
    const mutation = async () => { mutationCount += 1; return {}; };
    const page = pageHarness('projectnotes', {
      createProjectNoteProject: mutation, revokeProjectNoteMember: mutation,
      setProjectNoteConfirmer: mutation, resolveProjectNoteFeedback: mutation, updateProjectNoteProject: mutation,
      freezeProjectNoteVersion: mutation, mutationKey: () => 'stable-key',
    }, { showModal: options => { modalCount += 1; options.success({ confirm: false }); } });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.applyProjectDetail(detail);
    page.setData({ noteBody: '尚未保存的新内容' });
    await action(page);
    assert.equal(modalCount, 1);
    assert.equal(mutationCount, 0);
    assert.equal(page.data.noteBody, '尚未保存的新内容');
  }
});

test('marking a project revising asks only when unsaved project fields would be replaced', async () => {
  let modalCount = 0, mutations = 0;
  const page = pageHarness('projectnotes', { updateProjectNoteProject: async () => { mutations += 1; return {}; } }, { showModal: options => { modalCount += 1; options.success({ confirm: false }); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one', revision: 2, name: '原项目', status: 'initial' }, notes: [{ note_id: 'note-one', revision: 1, title: '标题', body: '已保存', body_format: 'plain' }], versions: [] });
  page.setData({ noteBody: '未保存笔记' });
  await page.markProjectRevising();
  assert.equal(modalCount, 0);
  assert.equal(mutations, 1);
  page.applyProjectDetail({ project: { project_id: 'project-one', revision: 2, name: '原项目', status: 'initial' }, notes: [{ note_id: 'note-one', revision: 1, title: '标题', body: '已保存', body_format: 'plain' }], versions: [] });
  page.setData({ projectEdit: { ...page.data.projectEdit, name: '未保存项目名' } });
  await page.markProjectRevising();
  assert.equal(modalCount, 1);
  assert.equal(mutations, 1);
  assert.equal(page.data.projectEdit.name, '未保存项目名');
});

test('AI panel result is cleared with the draft after permission revocation', async () => {
  let options, checks = 0;
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => { checks += 1; throw Object.assign(new Error('已撤权'), { statusCode: 403 }); },
  }, { aiStream: { startAIStream: value => { options = value; return { abort() {} }; } } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one' }, notes: [{ note_id: 'note-one', revision: 1, title: '标题', body: '已保存', body_format: 'plain' }], versions: [] });
  page.openNoteAIWriting();
  page.startNoteAI();
  options.onDone('AI 补充');
  await page.insertNoteAI();
  assert.match(page.data.noteBody, /AI 补充/);
  page.onHide();
  await page.onShow();
  assert.equal(checks, 1);
  assert.equal(page.data.noteBody, '');
  assert.equal(page.data.selectedProject, null);
  assert.match(page.data.error, /权限|访问/);
});

for (const statusCode of [403, 500]) {
  test(`failed project load (${statusCode}) keeps creation closed and retries the projects API`, async () => {
    let failing = true, calls = 0, creates = 0;
    const page = pageHarness('projectnotes', {
      listProjectNoteProjects: async () => { calls++; if (failing) throw Object.assign(new Error('私人项目笔记暂未开通'), { statusCode }); return { items: [] }; },
      workbenchContext: async () => ({ owners: ['designer-40'] }),
      createProjectNoteProject: async () => { creates++; },
    });
    await page.onShow();
    assert.equal(page.data.projectLoadFailed, true);
    const error = page.data.error;
    page.openProjectCreation();
    assert.equal(page.data.pageView, 'list');
    assert.equal(page.data.error, error);
    await page.createProjectNoteProject();
    assert.equal(creates, 0);
    failing = false;
    await page.retryProjectAccess();
    assert.equal(calls, 2);
    assert.equal(page.data.projectLoadFailed, false);
    page.openProjectCreation();
    assert.equal(page.data.pageView, 'create');
    assert.equal(page.data.ownerID, 'designer-40');
  });
}

test('project load failure renders a retry state before project creation controls', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(template, /wx:elif="{{projectLoadFailed}}"[\s\S]*?bindtap="retryProjectAccess"/);
});

for (const unsavedNote of [false, true]) {
  test(`saved project settings keep their restored values and preserve unsaved note protection (${unsavedNote})`, async () => {
    let modalCount = 0;
    const original = { project_id: 'project-one', name: '原项目名称', revision: 1, status: 'initial' };
    const page = pageHarness('projectnotes', {
      updateProjectNoteProject: async (id, patch) => ({ ...original, ...patch, project_id: id, revision: 2 }),
    }, { showModal: options => { modalCount++; options.success({ confirm: false }); } });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.applyProjectDetail({ project: original, notes: [{ note_id: 'note-one', title: '已存笔记', body: '已存正文', body_format: 'plain', revision: 1 }], versions: [] });
    const noteBaseline = page._noteFormBaseline;
    const savedNoteSnapshot = page._loadedFormState.noteBody;
    if (unsavedNote) page.setNoteBody({ detail: { value: '尚未保存的正文' } });
    page.openProjectSettings();
    page.setProjectField({ currentTarget: { dataset: { field: 'name' } }, detail: { value: '新项目名称' } });
    await page.saveProjectDetails();
    assert.equal(page.data.selectedProject.name, '新项目名称');
    assert.equal(page._noteFormBaseline, noteBaseline);
    assert.equal(page._loadedFormState.noteBody, savedNoteSnapshot);
    await page.closeProjectSettings();
    assert.equal(page.data.projectSettingsOpen, false);
    assert.equal(page.data.projectEdit.name, '新项目名称');
    assert.equal(modalCount, 0);
    if (unsavedNote) assert.equal(page.data.noteBody, '尚未保存的正文');
    await page.startNewNote();
    assert.equal(modalCount, unsavedNote ? 1 : 0);
    if (unsavedNote) {
      assert.equal(page.data.note.note_id, 'note-one');
      assert.equal(page.data.noteBody, '尚未保存的正文');
    } else {
      assert.equal(page.data.pageView, 'editor');
      assert.equal(page.data.note, null);
    }
  });
}

for (const update of [false, true]) {
  test(`project note ${update ? 'update retains revision' : 'creation omits update-only revision'} for the strict request schema`, async () => {
    const allowed = ['title', 'body', 'body_format', 'cover_media_id', 'media_ids'];
    if (update) allowed.push('expected_revision');
    let accepted = null;
    const strictSave = async (id, input) => {
      if (Object.keys(input).some(key => !allowed.includes(key))) throw Object.assign(new Error('请求字段或体积无效'), { statusCode: 400 });
      accepted = { id, input };
      return { note_id: 'note-one', revision: update ? 8 : 1, ...input };
    };
    const page = pageHarness('projectnotes', { createProjectNote: strictSave, saveProjectNote: strictSave });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.refreshProject = async () => {};
    page.setData({ selectedProject: { project_id: 'project-one' }, note: update ? { note_id: 'note-one', revision: 7, cover_media_id: 'image-one' } : null, noteTitle: '方案说明', noteBody: '保留庭院入口', noteMedia: [{ media_id: 'image-one', kind: 'image' }] });
    await (update ? page.saveProjectNote() : page.createProjectNote());
    assert.equal(page.data.error, '');
    assert.ok(accepted);
    assert.deepEqual(Object.keys(accepted.input).sort(), allowed.sort());
    assert.equal(accepted.id, update ? 'note-one' : 'project-one');
    assert.equal(accepted.input.expected_revision, update ? 7 : undefined);
    assert.equal(accepted.input.cover_media_id, 'image-one');
    assert.deepEqual(Array.from(accepted.input.media_ids), ['image-one']);
    assert.equal(page.data.note.note_id, 'note-one');
  });
}

test('project long press requires menu and confirmation, suppresses opening, and keeps stale rows', async () => {
  const calls = [];
  let sheet, modal;
  const project = { project_id: 'project-one', revision: 3, name: '张宅' };
  const page = pageHarness('projectnotes', {
    deleteProjectNoteProject: async (id, revision) => { calls.push([id, revision]); throw Object.assign(new Error('项目资料已更新'), { statusCode: 409 }); },
  }, {
    showActionSheet: options => { sheet = options; },
    showModal: options => { modal = options; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ projects: [project], loading: false });

  const pending = page.openProjectDeleteMenu({ currentTarget: { dataset: { id: project.project_id } } });
  assert.deepEqual(Array.from(sheet.itemList), ['删除项目']);
  sheet.success({ tapIndex: 0 });
  modal.success({ confirm: true });
  await pending;
  assert.deepEqual(calls, [['project-one', 3]]);
  assert.equal(page.data.projects.length, 1);
  assert.match(page.data.error, /已更新/);

  let opened = 0;
  page.openProject = async () => { opened += 1; };
  await page.selectProject({ currentTarget: { dataset: { id: project.project_id } } });
  assert.equal(opened, 0);
});

test('project and note long-press confirmations reject account or revision changes', async () => {
  let token = 'session-token', projectDeletes = 0, noteDeletes = 0;
  let sheet, modal;
  const project = { project_id: 'project-one', revision: 3 };
  const note = { note_id: 'note-one', revision: 2, title: '草稿' };
  const page = pageHarness('projectnotes', {
    deleteProjectNoteProject: async () => { projectDeletes += 1; },
    deleteProjectNote: async () => { noteDeletes += 1; },
  }, {
    getStorageSync: key => key === 'token' ? token : '',
    showActionSheet: options => { sheet = options; },
    showModal: options => { modal = options; },
  });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ projects: [project], selectedProject: project, notes: [note] });

  let pending = page.openProjectDeleteMenu({ currentTarget: { dataset: { id: project.project_id } } });
  sheet.success({ tapIndex: 0 });
  token = 'other-account';
  modal.success({ confirm: true });
  await pending;
  assert.equal(projectDeletes, 0);

  token = 'session-token'; page._token = token;
  pending = page.openNoteDeleteMenu({ currentTarget: { dataset: { id: note.note_id } } });
  sheet.success({ tapIndex: 0 });
  page.setData({ notes: [{ ...note, revision: 3 }] });
  modal.success({ confirm: true });
  await pending;
  assert.equal(noteDeletes, 0);
});

test('cancelled long-press deletion makes no mutation', async () => {
  let mutations = 0, sheet, modal;
  const project = { project_id: 'project-one', revision: 1 };
  const page = pageHarness('projectnotes', { deleteProjectNoteProject: async () => { mutations += 1; } }, {
    showActionSheet: options => { sheet = options; }, showModal: options => { modal = options; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.setData({ projects: [project] });
  const pending = page.openProjectDeleteMenu({ currentTarget: { dataset: { id: project.project_id } } });
  sheet.success({ tapIndex: 0 });
  modal.success({ confirm: false });
  await pending;
  assert.equal(mutations, 0);
  assert.equal(page.data.projects.length, 1);
});

test('same-account resume releases an old pending project deletion lock without releasing a new lock', async () => {
  let finishDelete, sheet, modal;
  const project = { project_id: 'project-one', revision: 2, name: '张宅' };
  const page = pageHarness('projectnotes', {
    deleteProjectNoteProject: () => new Promise(resolve => { finishDelete = resolve; }),
    getProjectNoteProject: async () => ({ project, notes: [], versions: [] }),
  }, {
    showActionSheet: options => { sheet = options; }, showModal: options => { modal = options; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ projects: [project] });
  page.applyProjectDetail({ project, notes: [], versions: [] });

  const pending = page.openProjectDeleteMenu({ currentTarget: { dataset: { id: project.project_id } } });
  sheet.success({ tapIndex: 0 });
  modal.success({ confirm: true });
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(page.data.busy, true);

  page.onHide();
  await page.onShow();
  assert.equal(page.data.busy, false);

  page.setData({ busy: true });
  finishDelete({});
  await pending;
  assert.equal(page.data.busy, true);
  assert.equal(page.data.projects.length, 1);
});

// F1 / T1.1：项目笔记 AI 回填双向确认——成功只追加一次、忙时保留可重试、目标失效永久拒绝。
test('project note AI return acknowledges once, replays applied, and separates busy from an invalid target', () => {
  let token = 'session-token';
  let navigation = null;
  const sent = [];
  const page = pageHarness('projectnotes', {}, {
    getStorageSync: key => key === 'token' ? token : '',
    navigateTo: options => { navigation = options; options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); },
  });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 3 }, noteTitle: '标题', noteBody: '原文' });
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  const acks = () => sent.filter(args => args[0] === 'writingAck').map(args => args[1]);
  const payload = { source: 'project_note', project_id: 'p', note_id: 'n', note_revision: 3, title: '标题', text: '新文案' };

  page.setData({ busy: true });
  page.receiveNoteWriting(payload, page._writingLaunch);
  assert.equal(acks()[0].state, 'retryable', '忙时属于暂时拒绝');
  assert.equal(page.data.noteBody, '原文', '暂时拒绝不得改动正文');

  page.setData({ busy: false });
  page.receiveNoteWriting(payload, page._writingLaunch);
  assert.equal(acks()[1].state, 'applied');
  assert.equal(page.data.noteBody, '原文\n\n新文案');

  page.receiveNoteWriting(payload, page._writingLaunch);
  assert.equal(acks()[2].state, 'applied', '已应用后重试只重放成功确认');
  assert.equal(acks()[2].replayed, true);
  assert.equal(page.data.noteBody, '原文\n\n新文案', '不得重复追加');

  token = 'session-two';
  page._token = 'session-token';
  page.setData({ noteBody: '原文', busy: false });
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  page.receiveNoteWriting(payload, page._writingLaunch);
  const last = acks().length - 1;
  assert.equal(acks()[last].state, 'invalid', '换账号必须永久拒绝');
  assert.equal(page.data.noteBody, '原文', '目标失效不得写入正文');
  assert.match(page.data.notice, /账号/);
});

test('project note AI return rejects a changed revision even when the payload identity is copied', () => {
  let navigation = null;
  const sent = [];
  const page = pageHarness('projectnotes', {}, { navigateTo: options => { navigation = options; options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 3 }, noteTitle: '标题', noteBody: '原文' });
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  page.setData({ note: { note_id: 'n', revision: 4 } });
  page.receiveNoteWriting({ source: 'project_note', project_id: 'p', note_id: 'n', note_revision: 3, text: '不能采用' }, page._writingLaunch);
  const ack = sent.filter(args => args[0] === 'writingAck').map(args => args[1])[0];
  assert.equal(ack.state, 'invalid');
  assert.equal(page.data.noteBody, '原文');
  assert.match(page.data.notice, /未采用/);
});

// F1 / T1.1：已应用后的重放必须重新核对目标；载荷变化或目标失效时不得确认成功。
test('an applied project note AI return never replays success after the target or payload changes', () => {
  const build = () => {
    let token = 'session-token';
    let navigation = null;
    const sent = [];
    const page = pageHarness('projectnotes', {}, {
      getStorageSync: key => key === 'token' ? token : '',
      navigateTo: options => { navigation = options; options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); },
    });
    page._visible = true; page._token = token; page._epoch = 1;
    page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 3 }, noteTitle: '标题', noteBody: '原文' });
    page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
    const acks = () => sent.filter(args => args[0] === 'writingAck').map(args => args[1]);
    const payload = { source: 'project_note', project_id: 'p', note_id: 'n', note_revision: 3, title: '标题', text: '新文案', request: 7, operation: 'op-1' };
    return { page, navigation, acks, payload, setToken(value) { token = value; } };
  };

  const replay = build();
  replay.page.receiveNoteWriting(replay.payload, replay.page._writingLaunch);
  assert.equal(replay.acks()[0].state, 'applied');
  assert.equal(replay.acks()[0].request, 7, '回执必须回显请求 ID');
  assert.equal(replay.acks()[0].operation, 'op-1');
  replay.page.receiveNoteWriting(replay.payload, replay.page._writingLaunch);
  assert.equal(replay.acks()[1].state, 'applied', '同载荷重放只确认成功');
  assert.equal(replay.acks()[1].replayed, true);
  assert.equal(replay.page.data.noteBody, '原文\n\n新文案');

  const changedPayload = build();
  changedPayload.page.receiveNoteWriting(changedPayload.payload, changedPayload.page._writingLaunch);
  changedPayload.page.receiveNoteWriting(Object.assign({}, changedPayload.payload, { text: '改过的文案' }), changedPayload.page._writingLaunch);
  assert.equal(changedPayload.acks()[1].state, 'invalid', '载荷变化不得重放旧成功');
  assert.equal(changedPayload.page.data.noteBody, '原文\n\n新文案', '改过的文案不得写入');

  const switchedAccount = build();
  switchedAccount.page.receiveNoteWriting(switchedAccount.payload, switchedAccount.page._writingLaunch);
  switchedAccount.setToken('session-two');
  switchedAccount.page._token = 'session-token';
  switchedAccount.page.receiveNoteWriting(switchedAccount.payload, switchedAccount.page._writingLaunch);
  assert.equal(switchedAccount.acks()[1].state, 'invalid', '换账号后不得重放旧成功');
});

// F2 / T1.2：草稿实例 ID 在实例创建时生成、跨 AI 重进稳定，新建/切换/保存/丢弃/换账号时转换。
test('a draft instance id stays stable across AI re-entry and rotates on new, switch, save, discard and account change', async () => {
  const sent = [];
  const detail = {
    project: { project_id: 'p', name: '张宅' },
    notes: [
      { note_id: 'n1', revision: 2, title: '方案 A', body: '正文 A', body_format: 'plain' },
      { note_id: 'n2', revision: 1, title: '方案 B', body: '正文 B', body_format: 'plain' },
    ],
    versions: [], feedbacks: [], confirmations: [],
  };
  const page = pageHarness('projectnotes', {}, {
    navigateTo: options => { options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;

  page.applyProjectDetail(detail, '', null, null, 'detail');
  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  const firstInstance = page._draftInstance && page._draftInstance.id;
  assert.ok(firstInstance, '进入编辑器必须生成草稿实例 ID');
  assert.equal(page.draftInstanceAccount(), '101', '实例身份绑定当前账号');

  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  assert.equal(page._aiPanelLaunch.instanceID, firstInstance, 'AI 面板必须带草稿实例 ID');
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  assert.equal(page._draftInstance.id, firstInstance, '同一草稿多次发起 AI 不产生新实例 ID');

  page._selectedNoteID = 'n2';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  const secondInstance = page._draftInstance.id;
  assert.notEqual(secondInstance, firstInstance, '切换到另一篇草稿必须换实例');

  await page.startNewNote();
  const thirdInstance = page._draftInstance.id;
  assert.notEqual(thirdInstance, secondInstance, '新建草稿必须换实例');
  await page.startNewNote();
  const beforeSaveInstance = page._draftInstance.id;
  assert.notEqual(beforeSaveInstance, thirdInstance, '同项目连续新建两个空 ID 草稿实例不同');

  const saved = { project: { project_id: 'p' }, notes: [{ note_id: 'n3', revision: 1, title: '新方案', body: '新正文', body_format: 'plain' }], versions: [] };
  page._newNote = false; page._selectedNoteID = 'n3';
  page.applyProjectDetail(saved, '', null, null, 'editor');
  const savedInstance = page._draftInstance.id;
  assert.notEqual(savedInstance, beforeSaveInstance, '保存后身份从新草稿转为已保存笔记');

  const revised = { project: { project_id: 'p' }, notes: [{ note_id: 'n3', revision: 2, title: '新方案', body: '新正文', body_format: 'plain' }], versions: [] };
  page.applyProjectDetail(revised, '', null, null, 'editor');
  assert.notEqual(page._draftInstance.id, savedInstance, '修订变化必须换实例');

  await page.handleBack();
  assert.equal(page.data.pageView, 'detail');
  assert.equal(page._draftInstance, null, '丢弃/离开编辑器必须清空实例');
});

test('changing the account clears the draft instance so an old AI result cannot be adopted', async () => {
  let token = 'session-token';
  let navigation = null;
  const sent = [];
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [], versions: [] }),
    listProjectNoteProjects: async () => ({ items: [], next_cursor: '' }),
    workbenchContext: async () => ({ owners: [] }),
  }, {
    getStorageSync: key => key === 'token' ? token : '',
    navigateTo: options => { navigation = options; options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); },
  });
  page._visible = true; page._token = token; page._epoch = 1; page._loaded = true;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [], versions: [] }, '', null, null, 'editor');
  const instance = page._draftInstance.id;
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  const launch = page._writingLaunch;
  token = 'session-two';
  page._token = 'session-token';
  await page.onShow();
  assert.equal(page._draftInstance, null, '换账号必须清空草稿实例');
  page.receiveNoteWriting({ source: 'project_note', project_id: 'p', note_id: '', note_revision: 0, draft_instance_id: instance, text: '旧账号文案' }, launch);
  const ack = sent.filter(args => args[0] === 'writingAck').map(args => args[1]).pop();
  assert.equal(ack.state, 'invalid', '换账号后旧实例回填必须拒绝');
});

// F2 / T1.2：丢弃当前编辑实例（含历史版本往返）后，即使回到同一笔记也要换新实例。
test('discarding edits through the version view rotates the draft instance on return to the same note', async () => {
  const detail = { project: { project_id: 'p' }, notes: [{ note_id: 'n1', revision: 2, title: '方案', body: '正文', body_format: 'plain' }], versions: [{ version_id: 'v1', note_id: 'n1', revision: 2, label: 'V2' }] };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => detail,
    getProjectNoteVersion: async (projectID, versionID) => ({ project_id: projectID, version_id: versionID, note_id: 'n1', revision: 2, title: '方案', body: '正文', body_format: 'plain', media: [] }),
  }, {
    showModal: options => options.success({ confirm: true }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  const editing = page._draftInstance.id;

  page.setData({ busy: false });
  await page.selectVersion({ currentTarget: { dataset: { id: 'v1' } } });
  assert.equal(page._draftInstance, null, '进入历史版本即放弃当前编辑实例');

  page.setData({ busy: false });
  await page.returnToDraft();
  assert.ok(page._draftInstance && page._draftInstance.id, '返回草稿必须重新绑定实例');
  assert.notEqual(page._draftInstance.id, editing, '同一笔记同一修订也不得复用已丢弃实例');
});

// F1+F2 / T1.2：回执丢失后重进再采纳，只重放成功确认，不重复追加。
test('re-adopting the same AI result after a lost ack replays success instead of appending twice', () => {
  const sent = [];
  let navigation = null;
  const page = pageHarness('projectnotes', {}, { navigateTo: options => { navigation = options; options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 3 }, noteTitle: '标题', noteBody: '原文' });
  const acks = () => sent.filter(args => args[0] === 'writingAck').map(args => args[1]);
  const payload = { source: 'project_note', project_id: 'p', note_id: 'n', note_revision: 3, title: 'AI 改过的标题', text: 'AI 文案', request: 1, operation: 'ai-result-d1-r1' };
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  page.receiveNoteWriting(payload, page._writingLaunch);
  assert.equal(acks()[0].state, 'applied');
  assert.equal(page.data.noteBody, '原文\n\nAI 文案');
  // 回执丢失 → AI 页重进 → 原页发起新的 launch，同一 operation 再次采纳
  page.openNoteAIWriting();
  page._writingLaunch.channel = { emit: (...args) => sent.push(args) };
  page.receiveNoteWriting({ ...payload, request: 2 }, page._writingLaunch);
  assert.equal(acks()[1].state, 'applied');
  assert.equal(acks()[1].replayed, true, '跨 launch 只重放成功确认');
  assert.equal(page.data.noteBody, '原文\n\nAI 文案', '不得重复追加');
});

// F3 / T1.3：上传批次逐条保留、继续尝试剩余文件、失败项可重试且只重试失败项。
test('a failed media upload keeps earlier successes, continues the batch and only retries failures', async () => {
  const calls = [];
  let failuresLeft = 1;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async (projectID, path) => {
      calls.push(path);
      if (path === '/tmp/3.jpg' && failuresLeft > 0) { failuresLeft -= 1; throw Object.assign(new Error('文件超出大小限制'), { statusCode: 413 }); }
      return { asset: { media_id: 'm' + path.split('/').pop() } };
    },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const files = Array.from({ length: 9 }, (_, index) => ({ tempFilePath: `/tmp/${index + 1}.jpg`, fileType: 'image' }));
  await page.uploadNoteMedia(files);

  assert.equal(calls.length, 9, '第 3 张失败后仍继续尝试第 4~9 张');
  assert.equal(page.data.noteMedia.length, 8, '已成功的 8 张全部保留');
  assert.deepEqual(Array.from(page.data.noteMedia.slice(0, 2), item => item.media_id), ['m1.jpg', 'm2.jpg'], '前 2 张仍可见');
  assert.equal(page.noteDraft().media_ids.length, 8, '已成功的素材可保存');
  assert.equal(page.data.mediaUploadFailures.length, 1);
  assert.equal(page.data.mediaUploadFailures[0].name, '3.jpg');
  assert.equal(page.data.mediaUploadFailures[0].message, '文件超出大小限制');
  assert.match(page.data.notice, /已上传 8 个，1 个失败，可重试/);
  assert.match(page.data.mediaFailureSummary, /3\.jpg/);

  calls.length = 0;
  await page.retryNoteMediaFailures();
  assert.deepEqual(calls, ['/tmp/3.jpg'], '重试只上传失败项，不重复已成功项');
  assert.equal(page.data.noteMedia.length, 9);
  assert.equal(page.data.mediaUploadFailures.length, 0);
  assert.equal(page.data.mediaFailureSummary, '');
});

test('media upload errors keep the server message and flag network failures separately', async () => {
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async (projectID, path) => {
      if (path === '/tmp/a.jpg') throw Object.assign(new Error('单张图片不能超过 10 MiB'), { statusCode: 413 });
      throw new Error('私有素材上传失败');
    },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }, { tempFilePath: '/tmp/b.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures[0].message, '单张图片不能超过 10 MiB', 'HTTP 错误保留 data.msg');
  assert.equal(page.data.mediaUploadFailures[1].message, '网络异常，请检查网络后重试', '网络错误给出可区分提示');
  assert.equal(page.data.noteMedia.length, 0);
  assert.equal(page.data.error, '单张图片不能超过 10 MiB');
});

// F3 / T1.3：失败队列跨保存保留、跨草稿隔离，并有可见的失败原因。
test('unretried upload failures survive saving the same note and stay out of a new draft', async () => {
  const saved = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', media_ids: [] };
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { throw Object.assign(new Error('网络异常'), { statusCode: 502 }); },
    createProjectNote: async () => saved,
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [saved], versions: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [], versions: [] }, '', null, null, 'editor');
  const draftInstance = page._draftInstance.id;
  page.setData({ noteTitle: '标题', noteBody: '正文' });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1);
  assert.equal(page.data.mediaUploadFailures[0].name, 'a.jpg');

  // 保存当前（新）草稿：失败队列属于同一个笔记，必须保留
  await page.persistNote(false);
  const store = page._noteMediaFailures || {};
  const noteKey = Object.keys(store).find(key => key.endsWith('|note:n1'));
  assert.ok(noteKey, '保存后队列跟随新笔记身份');
  assert.equal(store[noteKey].length, 1);
  assert.equal(page.data.mediaUploadFailures.length, 1, '保存同一条草稿不得清空失败队列');

  // 新建草稿：不得继承上一条草稿的失败队列
  page._selectedNoteID = ''; page._newNote = false; page._draftInstance = null;
  await page.startNewNote();
  assert.notEqual(page._draftInstance.id, draftInstance);
  assert.equal(page.data.mediaUploadFailures.length, 0, '新草稿不得出现旧草稿的失败队列');
});

test('a failed batch is merged with new failures without duplicating entries', async () => {
  let fail = true;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { if (fail) throw new Error('私有素材上传失败'); return { asset: { media_id: 'm' + Math.random() } }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1);
  // 追加新批次：旧失败项保留、新的失败项合并，同一文件不重复
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }, { tempFilePath: '/tmp/b.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 2);
  assert.match(page.data.notice, /已上传 0 个，2 个失败，可重试/);
  fail = false;
  await page.retryNoteMediaFailures();
  assert.equal(page.data.mediaUploadFailures.length, 0, '重试成功后清空对应失败项');
});

test('the media retry surface exposes each failure reason, not only file names', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(wxml, /wx:for="\{\{mediaUploadFailures\}\}"/);
  assert.match(wxml, /\{\{item\.name\}\}：\{\{item\.message\}\}/, '失败项必须同时展示文件名与原因');
  assert.match(wxml, /bindtap="retryNoteMediaFailures"/);
  assert.match(wxml, /\{\{mediaRetryLabel\}\}/, '按钮文案必须与实际单轮处理数量一致');
});

test('an unparsable upload response keeps its http status code for error classification', () => {
  const { api } = loadAPI({
    uploadFile(options) { options.success({ statusCode: 502, data: '<html>bad gateway</html>' }); },
  });
  return api.uploadProjectNoteMedia('project-one', '/tmp/a.jpg').then(
    () => { throw new Error('应当失败'); },
    error => assert.equal(error.statusCode, 502),
  );
});

// F3 / T1.3：后台完成的上传不得被失效回调丢弃，返回后要接纳；队列按笔记隔离且可恢复。
test('uploads that finish while backgrounded are accepted on return instead of being lost', async () => {
  let resolveUpload;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [], versions: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._loaded = true;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [], versions: [] }, '', null, null, 'editor');
  const pendingUpload = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  page.onHide();
  resolveUpload({ asset: { media_id: 'm-a' } });
  await pendingUpload;
  assert.equal(page.data.noteMedia.length, 1, '同草稿后台完成的上传必须保留');
  assert.equal(page.data.noteMedia[0].media_id, 'm-a');
  assert.equal(page.data.busy, false, '完成后必须释放自己的锁');

  await page.onShow();
  assert.equal(page.data.noteMedia.length, 1, '返回后不得重复接纳');
  assert.equal(page.data.noteMedia[0].media_id, 'm-a');
});

test('an access recheck keeps the in-flight upload lock so no save starts underneath it', async () => {
  let resolveUpload, saves = 0;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
    createProjectNote: async () => { saves += 1; return { note_id: 'n1', revision: 1 }; },
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [], versions: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._loaded = true;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [], versions: [] }, '', null, null, 'editor');
  page.setData({ noteTitle: '标题', noteBody: '正文' });
  const pendingUpload = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  page.onHide();
  await page.onShow();
  assert.equal(page.data.busy, true, '同账号重验不得释放在途上传的锁');
  await page.createProjectNote();
  assert.equal(saves, 0, '上传未结束不得开始保存');

  resolveUpload({ asset: { media_id: 'm-a' } });
  await pendingUpload;
  assert.equal(page.data.busy, false);
});

test('failure queues are kept per note so switching away and back restores them', async () => {
  const detail = {
    project: { project_id: 'p' },
    notes: [
      { note_id: 'n1', revision: 1, title: '方案一', body: '正文一', body_format: 'plain' },
      { note_id: 'n2', revision: 1, title: '方案二', body: '正文二', body_format: 'plain' },
    ],
    versions: [],
  };
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { throw new Error('私有素材上传失败'); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1, 'n1 的失败队列可见');

  page._selectedNoteID = 'n2';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  assert.equal(page.data.mediaUploadFailures.length, 0, 'n2 不得继承 n1 的失败队列');
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/b.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1);
  assert.equal(page.data.mediaUploadFailures[0].name, 'b.jpg');

  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  assert.equal(page.data.mediaUploadFailures.length, 1, '切回 n1 恢复自己的失败队列');
  assert.equal(page.data.mediaUploadFailures[0].name, 'a.jpg');
});

// F3 / T1.3：批次身份校验——换账号立即停批、后台素材进保存载荷、接纳素材保持未保存、队列不被淘汰。
test('an account change stops the rest of an in-flight upload batch', async () => {
  let token = 'session-token';
  let resolveFirst;
  const calls = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: (projectID, path) => { calls.push(path); if (path === '/tmp/1.jpg') return new Promise(resolve => { resolveFirst = resolve; }); return Promise.resolve({ asset: { media_id: 'm2' } }); },
    listProjectNoteProjects: async () => ({ items: [], next_cursor: '' }),
    workbenchContext: async () => ({ owners: [] }),
  }, { getStorageSync: key => key === 'token' ? token : (key === 'userInfo' ? { id: 101 } : '') });
  page._visible = true; page._token = token; page._epoch = 1; page._loaded = true;
  page.setData({ selectedProject: { project_id: 'p' } });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/1.jpg' }, { tempFilePath: '/tmp/2.jpg' }, { tempFilePath: '/tmp/3.jpg' }]);
  token = 'session-two';
  await page.onShow();
  resolveFirst({ asset: { media_id: 'm1' } });
  await pending;
  assert.deepEqual(calls, ['/tmp/1.jpg'], '换账号后不得再用新账号凭证发旧账号的文件');
  assert.equal(page.data.noteMedia.length, 0, '旧账号上传不得写入');
  assert.equal(page.data.busy, false);
  assert.equal(page._noteMediaFailures, null, '旧回调不得重建已清空的存储');
});

test('media uploaded while backgrounded is included in the next save payload', async () => {
  let resolveUpload, savedPayload = null;
  const saved = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', media_ids: ['m-a'] };
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
    createProjectNote: async (projectID, draft) => { savedPayload = draft; return saved; },
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [saved], versions: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._loaded = true;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [], versions: [] }, '', null, null, 'editor');
  page.setData({ noteTitle: '标题', noteBody: '正文' });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  page.onHide();
  resolveUpload({ asset: { media_id: 'm-a' } });
  await pending;
  await page.onShow();
  assert.equal(page.data.noteMedia.length, 1);
  await page.persistNote(false);
  assert.deepEqual(Array.from(savedPayload.media_ids), ['m-a'], '后台完成的素材必须进入保存载荷');
});

test('media accepted after a refresh stays unsaved so leaving asks before dropping it', () => {
  const note = { note_id: 'n1', revision: 1, title: '方案', body: '正文', body_format: 'plain', media_ids: [] };
  const detail = { project: { project_id: 'p' }, notes: [note], versions: [] };
  const page = pageHarness('projectnotes', {});
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  assert.equal(page.hasUnsavedChanges(), false);
  page.stashPendingMedia(page.mediaFailureOwnerKey(), [{ file: { tempFilePath: '/tmp/a.jpg' }, asset: { media_id: 'm-a' } }]);
  page.applyProjectDetail(detail, '', null, null, 'editor');
  assert.equal(page.data.noteMedia.length, 1);
  assert.equal(page.data.noteMedia[0].media_id, 'm-a');
  assert.equal(page.hasUnsavedChanges(), true, '接纳的素材必须保持未保存状态');
});

test('the current note failure queue is never evicted by many other owners', async () => {
  const notes = Array.from({ length: 12 }, (_, index) => ({ note_id: 'n' + (index + 1), revision: 1, title: '方案' + index, body: '正文', body_format: 'plain' }));
  const detail = { project: { project_id: 'p' }, notes, versions: [] };
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { throw new Error('私有素材上传失败'); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  const store = {};
  for (let index = 1; index <= 11; index += 1) store['101|p|note:n' + index] = [{ tempFilePath: '/tmp/old' + index + '.jpg', name: 'old' + index + '.jpg', message: '网络异常' }];
  page._noteMediaFailures = store;
  page._selectedNoteID = 'n12';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1, '当前笔记的失败项不得被淘汰');
  assert.equal(page.data.mediaUploadFailures[0].name, 'a.jpg');
  assert.ok(Object.keys(page._noteMediaFailures).length <= 10, '归属数量保持有界');
});

test('a new batch reports its own counts while older failures stay pending separately', async () => {
  let fail = true;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { if (fail) throw new Error('私有素材上传失败'); return { asset: { media_id: 'm' + Math.random() } }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const batch = Array.from({ length: 9 }, (_, index) => ({ tempFilePath: `/tmp/${index + 1}.jpg`, fileType: 'image' }));
  await page.uploadNoteMedia(batch);
  assert.match(page.data.notice, /已上传 0 个，9 个失败，可重试/);
  fail = false;
  await page.uploadNoteMedia(Array.from({ length: 9 }, (_, index) => ({ tempFilePath: `/tmp/ok${index + 1}.jpg`, fileType: 'image' })));
  assert.equal(page.data.mediaUploadFailures.length, 9, '历史失败仍在队列可重试');
  assert.match(page.data.notice, /已上传 9 个，0 个失败/, '全成功也要显示本批 N/M 数量');
  assert.doesNotMatch(page.data.notice, /9 个失败，可重试/);
  assert.match(page.data.notice, /另有 9 个历史失败待重试/);
});

// F3 / T1.3：卸载/撤权后旧上传不得重建已清空的状态；回访旧 owner 不得丢失当前失败队列。
test('an upload finishing after unload never rebuilds the cleared state', async () => {
  let resolveUpload;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  page.onUnload();
  assert.equal(page._noteMediaFailures, null);
  assert.equal(page._noteMediaPending, null);
  resolveUpload({ asset: { media_id: 'm-a' } });
  await pending;
  assert.equal(page._noteMediaFailures, null, '卸载后旧回调不得重建失败队列');
  assert.equal(page._noteMediaPending, null, '卸载后旧回调不得写挂起素材');
  assert.equal(page.data.noteMedia.length, 0, '卸载后不得再写页面');
});

test('a revoked access invalidates an in-flight upload without rebuilding state', async () => {
  let resolveUpload;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
    getProjectNoteProject: async () => { throw Object.assign(new Error('无权访问'), { statusCode: 403 }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._loaded = true;
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'old' }] });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  await page.revalidateProjectAccess();
  assert.equal(page.data.noteMedia.length, 0, '撤权清空受保护内容');
  assert.equal(page._noteMediaFailures, null);
  resolveUpload({ asset: { media_id: 'm-a' } });
  await pending;
  assert.equal(page._noteMediaFailures, null, '撤权后旧回调不得重建失败队列');
  assert.equal(page.data.noteMedia.length, 0);
});

test('returning to the oldest failure owner keeps its queue visible and bounded', async () => {
  const notes = Array.from({ length: 12 }, (_, index) => ({ note_id: 'n' + (index + 1), revision: 1, title: '方案' + index, body: '正文', body_format: 'plain' }));
  const detail = { project: { project_id: 'p' }, notes, versions: [] };
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { throw new Error('私有素材上传失败'); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  const store = {};
  for (let index = 1; index <= 11; index += 1) store['101|p|note:n' + index] = [{ tempFilePath: '/tmp/old' + index + '.jpg', name: 'old' + index + '.jpg', message: '网络异常' }];
  page._noteMediaFailures = store;
  for (let index = 0; index < 12; index += 1) {
    page._selectedNoteID = 'n' + (index + 1);
    page.applyProjectDetail(detail, '', null, null, 'editor');
  }
  assert.equal(Object.keys(page._noteMediaFailures).length, 11, '浏览笔记不得凭空创建空归属');

  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/new.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 2, '回访最老 owner 时其失败队列必须可见');
  const keys = Object.keys(page._noteMediaFailures);
  assert.ok(keys.length <= 10, '归属数量保持有界');
  assert.ok(keys.some(key => key.endsWith('|note:n1')), '当前 owner 不得被自己的写入淘汰');
});

// F3 / T1.3：成功批次不占用失败队列容量；历史失败超过 9 项时分轮重试。
test('successful batches never store empty owners or evict a real failure queue', async () => {
  const notes = Array.from({ length: 11 }, (_, index) => ({ note_id: 'n' + (index + 1), revision: 1, title: '方案' + index, body: '正文', body_format: 'plain' }));
  const detail = { project: { project_id: 'p' }, notes, versions: [] };
  let fail = true;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { if (fail) throw new Error('私有素材上传失败'); return { asset: { media_id: 'm' + Math.random() } }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1);

  fail = false;
  for (let index = 2; index <= 11; index += 1) {
    page._selectedNoteID = 'n' + index;
    page.applyProjectDetail(detail, '', null, null, 'editor');
    await page.uploadNoteMedia([{ tempFilePath: '/tmp/ok' + index + '.jpg', fileType: 'image' }]);
  }
  const store = page._noteMediaFailures || {};
  assert.equal(Object.keys(store).length, 1, '成功的批次不得留下空 owner');
  assert.ok(Object.keys(store).some(key => key.endsWith('|note:n1')), '真实失败队列不得被挤掉');

  page._selectedNoteID = 'n1';
  page.applyProjectDetail(detail, '', null, null, 'editor');
  assert.equal(page.data.mediaUploadFailures.length, 1, '切回后失败项仍可重试');
});

test('retrying more than nine failures splits into batches of nine', async () => {
  const calls = [];
  let fail = true;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async (projectID, path) => { calls.push(path); if (fail) throw new Error('私有素材上传失败'); return { asset: { media_id: 'm' + path.split('/').pop() } }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const firstBatch = Array.from({ length: 9 }, (_, index) => ({ tempFilePath: `/tmp/a${index + 1}.jpg`, fileType: 'image' }));
  const secondBatch = Array.from({ length: 9 }, (_, index) => ({ tempFilePath: `/tmp/b${index + 1}.jpg`, fileType: 'image' }));
  await page.uploadNoteMedia(firstBatch);
  await page.uploadNoteMedia(secondBatch);
  assert.equal(page.data.mediaUploadFailures.length, 18);

  calls.length = 0;
  await page.retryNoteMediaFailures();
  assert.equal(calls.length, 9, '单轮重试不得超过 9 项');
  assert.match(page.data.notice, /已上传 0 个，9 个失败，可重试/);
  assert.equal(page.data.mediaUploadFailures.length, 18, '未重试的 9 项继续保留');

  assert.match(page.data.mediaRetryLabel, /重试下一批 9 个（共 18 个待处理）/, '按钮文案与实际处理数量一致');
  calls.length = 0;
  fail = false;
  await page.retryNoteMediaFailures();
  assert.equal(calls.length, 9, '第二轮仍只重试 9 项');
  assert.ok(calls.every(path => path.startsWith('/tmp/b')), '队首持续失败项必须让位给未尝试项');
  assert.equal(page.data.mediaUploadFailures.length, 9, '成功后只剩持续失败的第一批 9 项');
  assert.ok(page.data.mediaUploadFailures.every(item => item.tempFilePath.startsWith('/tmp/a')));
  assert.match(page.data.mediaRetryLabel, /重试失败的 9 个素材/);
});

// F3 / T1.3：旧重试收尾不得影响新重试的轮转集合（集合绑定单次上传操作）。
test('an old retry cannot disturb the rotation of a newer retry', async () => {
  const pending = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise((resolve, reject) => pending.push({ resolve, reject })),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  // 先建立旧失败队列，并必须通过 retryNoteMediaFailures() 入口启动旧重试（旧缺陷就在它的 finally）。
  const oldItems = Array.from({ length: 9 }, (_, index) => ({ tempFilePath: `/tmp/o${index + 1}.jpg`, name: `o${index + 1}.jpg`, message: '网络异常' }));
  page.setMediaFailures(page.mediaFailureOwnerKey(), oldItems);
  page.publishMediaFailures();
  const oldRetry = page.retryNoteMediaFailures();
  assert.equal(pending.length, 1, '旧重试发起首个请求');

  // 旧重试期间撤权/清空，再重建队列并启动新重试
  page.clearProtected();
  page.setData({ selectedProject: { project_id: 'p' } });
  const seeded = [];
  for (let index = 1; index <= 9; index += 1) seeded.push({ tempFilePath: `/tmp/b${index}.jpg`, name: `b${index}.jpg`, message: '网络异常' });
  for (let index = 1; index <= 9; index += 1) seeded.push({ tempFilePath: `/tmp/a${index}.jpg`, name: `a${index}.jpg`, message: '网络异常' });
  page.setMediaFailures(page.mediaFailureOwnerKey(), seeded);
  page.publishMediaFailures();
  const newRetry = page.retryNoteMediaFailures();
  assert.equal(pending.length, 2, '新重试串行发起首个请求');

  pending[0].reject(new Error('旧批次已失效'));
  await oldRetry;
  for (let index = 1; index <= 9; index += 1) {
    pending[index].reject(new Error('仍然失败'));
    await new Promise(resolve => setTimeout(resolve, 0));
  }
  await newRetry;

  const order = Array.from(page.data.mediaUploadFailures, item => item.tempFilePath);
  assert.equal(order.length, 18, '失败项一个都不丢');
  assert.ok(order.slice(0, 9).every(path => path.startsWith('/tmp/a')), '未尝试项排在本轮失败项之前（轮转生效）');
  assert.ok(order.slice(9).every(path => path.startsWith('/tmp/b')));
});

// F3 / T1.4：上传操作身份——进入历史/只读状态后，后续请求立即停止且不写回。
test('an in-flight upload stops before the next request when the draft becomes read-only', async () => {
  let resolveFirst;
  const calls = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: (projectID, path) => {
      calls.push(path);
      if (path === '/tmp/1.jpg') return new Promise(resolve => { resolveFirst = resolve; });
      return Promise.resolve({ asset: { media_id: 'm2' } });
    },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/1.jpg' }, { tempFilePath: '/tmp/2.jpg' }, { tempFilePath: '/tmp/3.jpg' }]);
  page.setData({ historyMode: true });
  resolveFirst({ asset: { media_id: 'm1' } });
  await pending;
  assert.deepEqual(calls, ['/tmp/1.jpg'], '身份失效后不得再发后续请求');
  assert.equal(page.data.noteMedia.length, 0, '失效批次不得写回页面');
});

test('the notes page marks itself unloaded so late uploads never write back', async () => {
  let resolveUpload;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image' }]);
  page.onUnload();
  assert.equal(page._unloaded, true, 'onUnload 必须设置卸载标记');
  resolveUpload({ asset: { media_id: 'm-a' } });
  await pending;
  assert.equal(page.data.noteMedia.length, 0);
});

// F3 / 批次 1：上传接口权限终态、并发重入、token 变化未 onShow 的锁释放。
test('an upload permission failure clears protected content and never enters the retry queue', async () => {
  const calls = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async (projectID, path) => { calls.push(path); throw Object.assign(new Error('无权访问'), { statusCode: 403 }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'old' }], noteBody: '私密正文' });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/1.jpg' }, { tempFilePath: '/tmp/2.jpg' }, { tempFilePath: '/tmp/3.jpg' }]);
  assert.deepEqual(calls, ['/tmp/1.jpg'], '权限终态后立即停止余下请求');
  assert.equal(page.data.noteMedia.length, 0, '清空受保护素材');
  assert.equal(page.data.noteBody, '', '清空受保护正文');
  assert.equal(page.data.mediaUploadFailures.length, 0, '权限错误不得进入重试队列');
  assert.equal(page.data.projectLoadFailed, true);
  assert.equal(page.data.busy, false);
});

test('concurrent upload batches are rejected and cannot unlock each other', async () => {
  const pending = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => pending.push(resolve)),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const first = page.uploadNoteMedia([{ tempFilePath: '/tmp/1.jpg' }]);
  assert.equal(pending.length, 1);
  const second = page.uploadNoteMedia([{ tempFilePath: '/tmp/2.jpg' }]);
  assert.equal(pending.length, 1, '并发批次必须被拒绝');
  await second;
  pending[0]({ asset: { media_id: 'm1' } });
  await first;
  assert.equal(page.data.busy, false);
  assert.equal(page.data.noteMedia.length, 1);
  assert.equal(page.data.noteMedia[0].media_id, 'm1');
});

test('a token change without onShow still releases the upload lock without writing data', async () => {
  let resolveUpload;
  let token = 'session-token';
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise(resolve => { resolveUpload = resolve; }),
  }, { getStorageSync: key => key === 'token' ? token : '' });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  const pending = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg' }]);
  token = 'session-two';
  resolveUpload({ asset: { media_id: 'm-a' } });
  await pending;
  assert.equal(page.data.busy, false, 'token 变化后必须释放自己的锁');
  assert.equal(page.data.noteMedia.length, 0, '不得写入旧账号素材');
});

// F3 / 批次 1：迟到权限错误不得清理新账号状态；选择器迟到回调不得跨账号上传。
test('a late permission error from an old upload cannot clear the new account state', async () => {
  const pending = [];
  let token = 'session-token';
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: () => new Promise((resolve, reject) => pending.push({ resolve, reject })),
  }, { getStorageSync: key => key === 'token' ? token : (key === 'userInfo' ? { id: 101 } : '') });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteBody: 'A 正文' });
  const oldUpload = page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg' }]);
  assert.equal(pending.length, 1);

  // 换账号并载入 B 的状态
  token = 'session-two'; page._token = 'session-two';
  page.clearProtected();
  page._loaded = true;
  page.setData({ selectedProject: { project_id: 'p' }, noteBody: 'B 正文', noteMedia: [{ media_id: 'b1' }] });
  const newUpload = page.uploadNoteMedia([{ tempFilePath: '/tmp/b.jpg' }]);
  assert.equal(pending.length, 2);

  pending[0].reject(Object.assign(new Error('无权访问'), { statusCode: 403 }));
  await oldUpload;
  assert.equal(page.data.noteBody, 'B 正文', '旧账号迟到 403 不得清空新账号正文');
  assert.equal(page.data.selectedProject && page.data.selectedProject.project_id, 'p');
  assert.equal(page.data.noteMedia.length, 1, '既有素材保留');
  assert.ok(page._noteMediaUpload, '新上传的锁不得被旧响应解除');

  pending[1].resolve({ asset: { media_id: 'b2' } });
  await newUpload;
  assert.equal(page.data.noteMedia.length, 2);
  assert.equal(page.data.busy, false);
});

test('a late media picker success cannot upload into another account', async () => {
  const uploads = [];
  const pickers = [];
  let token = 'session-token';
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async (projectID, path) => { uploads.push([token, projectID, path]); return { asset: { media_id: 'm1' } }; },
  }, {
    getStorageSync: key => key === 'token' ? token : (key === 'userInfo' ? { id: 101 } : ''),
    chooseMedia: options => pickers.push(options),
  });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  page.chooseNoteMedia();
  page.chooseNoteMedia();
  assert.equal(pickers.length, 1, '连续点击不得打开两个选择器');

  token = 'session-two'; page._token = 'session-two';
  page.clearProtected();
  page.setData({ selectedProject: { project_id: 'p2' } });
  pickers[0].success({ tempFiles: [{ tempFilePath: '/tmp/a.jpg' }] });
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(uploads.length, 0, '旧账号的迟到选择结果不得上传');
});

// 批次 2 / T2.4：客户端预检先于请求（20 MiB / 500 MiB / 50 项）。
test('client precheck rejects oversized or over-count media before any request', async () => {
  const calls = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { calls.push('upload'); return { asset: { media_id: 'm' } }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/big.jpg', fileType: 'image', size: 21 * 1024 * 1024 }]);
  assert.equal(calls.length, 0, '超限不得发起上传');
  assert.match(page.data.error, /图片不能超过 20\.0 MB/);

  page.setData({ noteMedia: Array.from({ length: 50 }, (_, index) => ({ media_id: 'm' + index })), error: '' });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image', size: 1024 }]);
  assert.equal(calls.length, 0, '累计超 50 项不得上传');
  assert.match(page.data.error, /最多 50 项/);

  page.setData({ noteMedia: [], error: '' });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/v.mp4', fileType: 'video', size: 501 * 1024 * 1024 }]);
  assert.equal(calls.length, 0, '超大视频不得上传');
  assert.match(page.data.error, /视频不能超过 500\.0 MB/);

  page.setData({ error: '' });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/ok.jpg', fileType: 'image', size: 19 * 1024 * 1024 }]);
  assert.equal(calls.length, 1, '合法大图照常上传');
});

// 批次 2 / T2.1：选择器取消与权限拒绝给出可行动提示且不改变内容。
test('cancelled or permission-denied pickers report actionable messages', () => {
  const pickers = [];
  const page = pageHarness('projectnotes', {}, { chooseMedia: options => pickers.push(options) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteBody: '保留内容' });
  page.chooseNoteMedia();
  pickers[0].fail({ errMsg: 'chooseMedia:fail cancel' });
  assert.match(page.data.notice, /已取消/);
  assert.equal(page.data.noteBody, '保留内容');

  page.chooseNoteMedia();
  pickers[1].fail({ errMsg: 'chooseMedia:fail auth deny' });
  assert.match(page.data.error, /权限/);
  assert.equal(page.data.noteBody, '保留内容');
});

// 批次 2 / T2.2：重开草稿补缩略图、失败单项重试、补下载路径不进保存载荷。
test('reopened drafts hydrate thumbnails without dirtying the form', async () => {
  let fail = true;
  const downloads = [];
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', media: [{ media_id: 'img1', kind: 'image' }, { media_id: 'vid1', kind: 'video' }] };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [note], versions: [] }),
    downloadProjectNoteMedia: async (projectID, id) => { downloads.push(id); if (fail) throw new Error('下载失败'); return '/tmp/' + id; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  const tick = () => new Promise(resolve => setTimeout(resolve, 10));
  await tick();
  assert.equal(page.data.noteMedia.length, 2);
  assert.match(page.data.noteMedia[0].preview_error, /素材暂时无法显示/);
  assert.equal(page.hasUnsavedChanges(), false, '补缩略图不得弄脏表单');

  fail = false;
  page.retryNoteMediaPreview({ currentTarget: { dataset: { id: 'img1' } } });
  await tick();
  assert.equal(page.data.noteMedia[0].local_path, '/tmp/img1');
  assert.equal(page.data.noteMedia[0].preview_error || '', '');
  assert.equal(page.hasUnsavedChanges(), false, '补下载路径不进 dirty state');
  assert.deepEqual(Array.from(page.noteDraft().media_ids), ['img1', 'vid1'], '保存载荷仍是 media_id');
});

// 批次 2 / T2.3：视频保留选择器缩略图并可进入原生播放。
test('videos keep their picker thumbnail and can be previewed in the editor', async () => {
  const previews = [];
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => ({ asset: { media_id: 'v1', kind: 'video' } }),
  }, { previewMedia: options => previews.push(options) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/v.mp4', fileType: 'video', size: 1024, thumbTempFilePath: '/tmp/v.jpg' }]);
  assert.equal(page.data.noteMedia[0].thumb_path, '/tmp/v.jpg', '视频缩略图作为封面');
  page.openNoteVideo({ currentTarget: { dataset: { id: 'v1' } } });
  assert.equal(previews.length, 1);
  assert.equal(previews[0].sources[0].type, 'video');
  assert.equal(previews[0].sources[0].url, '/tmp/v.mp4');
});

test('the media list exposes fallbacks, retries and video playback wiring', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(wxml, /binderror="noteMediaLoadError"/);
  assert.match(wxml, /'retryNoteMediaPreview'/);
  assert.match(wxml, /bindtap="openNoteVideo"/);
  assert.match(wxml, /poster="\{\{item\.thumb_path \|\| ''\}\}"/);
  assert.match(wxml, /item\.kind === 'private' \? '素材'/);
});

// 批次 2 复评回归：跨后台恢复、提示保留、身份守卫、单项重试、结构化草稿、视频闭环。
test('thumbnail hydration interrupted by background resumes on return', async () => {
  const downloads = [];
  let resolveDownload;
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', media: [{ media_id: 'img1', kind: 'image' }] };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [note], versions: [] }),
    downloadProjectNoteMedia: (projectID, id) => { downloads.push(id); if (downloads.length === 1) return new Promise(resolve => { resolveDownload = resolve; }); return Promise.resolve('/tmp/' + id); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1'; page._loaded = true;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  page.onHide();
  resolveDownload('/tmp/img1');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(page.data.noteMedia[0].preview_loading, false, '中断后不得永久加载中');
  await page.onShow();
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.deepEqual(downloads, ['img1', 'img1'], '回前台后重新补下载');
  assert.equal(page.data.noteMedia[0].local_path, '/tmp/img1');
});

test('picker cancellation feedback survives the access recheck', async () => {
  const pickers = [];
  const page = pageHarness('projectnotes', { getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [], versions: [] }) }, { chooseMedia: options => pickers.push(options) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._loaded = true;
  page.setData({ selectedProject: { project_id: 'p' } });
  page.chooseNoteMedia();
  pickers[0].fail({ errMsg: 'chooseMedia:fail cancel' });
  assert.match(page.data.notice, /已取消/);
  await page.onShow();
  assert.match(page.data.notice, /已取消/, '重验不得清掉取消提示');
});

test('a late picker failure from an old account cannot rewrite the current message', () => {
  const pickers = [];
  let token = 'session-token';
  const page = pageHarness('projectnotes', {}, {
    getStorageSync: key => key === 'token' ? token : '',
    chooseMedia: options => pickers.push(options),
  });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, error: '新账号错误', notice: '' });
  page.chooseNoteMedia();
  token = 'session-two';
  pickers[0].fail({ errMsg: 'chooseMedia:fail cancel' });
  assert.equal(page.data.error, '新账号错误', '旧回调不得改写新账号错误');
  assert.doesNotMatch(page.data.notice || '', /已取消/);
});

test('single-item retry only downloads the requested media', async () => {
  const downloads = [];
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async (projectID, id) => { downloads.push(id); return '/tmp/' + id; } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'a', kind: 'image' }, { media_id: 'b', kind: 'image' }] });
  page.retryNoteMediaPreview({ currentTarget: { dataset: { id: 'a' } } });
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.deepEqual(downloads, ['a'], '只重试目标素材');
});

test('structured drafts hydrate their media thumbnails too', async () => {
  const downloads = [];
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'article_v2', media: [{ media_id: 'img1', kind: 'image' }] };
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: async (projectID, id) => { downloads.push(id); return '/tmp/' + id; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.deepEqual(downloads, ['img1'], '结构化草稿也要补缩略图');
  assert.equal(page.data.bodyReadOnly, true);
});

test('a failed video preview falls back to a retryable placeholder', () => {
  const page = pageHarness('projectnotes', {}, { previewMedia: options => options.fail() });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'v1', kind: 'video', local_path: '/tmp/v.mp4' }] });
  page.openNoteVideo({ currentTarget: { dataset: { id: 'v1' } } });
  assert.equal(page.data.noteMedia[0].local_path, '', '失败后回到占位');
  assert.match(page.data.noteMedia[0].preview_error, /视频加载失败/);
});

test('a retried video upload keeps its picker thumbnail', async () => {
  let attempt = 0;
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => { attempt += 1; if (attempt === 1) throw new Error('私有素材上传失败'); return { asset: { media_id: 'v1', kind: 'video' } }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/v.mp4', fileType: 'video', size: 1024, thumbTempFilePath: '/tmp/v.jpg' }]);
  assert.equal(page.data.mediaUploadFailures.length, 1);
  assert.equal(page.data.mediaUploadFailures[0].thumbTempFilePath, '/tmp/v.jpg', '失败队列保留封面元数据');
  await page.retryNoteMediaFailures();
  assert.equal(page.data.noteMedia.length, 1);
  assert.equal(page.data.noteMedia[0].thumb_path, '/tmp/v.jpg', '重试成功仍带封面');
});

// 批次 2 复评回归：ID-only 类型恢复、反馈身份、恢复调度、批量/单项交叠。
test('id-only draft media infer their kind from the downloaded file', async () => {
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async (projectID, id) => id === 'img2' ? '/tmp/img2.jpg' : '/tmp/vid1.mp4' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'img2', kind: 'private' }, { media_id: 'vid1', kind: 'private' }] });
  await page.hydrateNoteMedia(1, 'p');
  assert.equal(page.data.noteMedia[0].kind, 'image');
  assert.equal(page.data.noteMedia[0].local_path, '/tmp/img2.jpg');
  assert.equal(page.data.noteMedia[1].kind, 'video');
  assert.equal(page.data.noteMedia[1].local_path, '/tmp/vid1.mp4');
});

test('deferred picker feedback is discarded once the account changes', () => {
  const pickers = [];
  let token = 'session-token';
  const page = pageHarness('projectnotes', {}, { getStorageSync: key => key === 'token' ? token : '', chooseMedia: options => pickers.push(options) });
  page._visible = true; page._token = token; page._epoch = 1; page._loaded = true;
  page.setData({ selectedProject: { project_id: 'p' } });
  page.chooseNoteMedia();
  pickers[0].fail({ errMsg: 'chooseMedia:fail cancel' });
  assert.match(page.data.notice, /已取消/);
  token = 'session-two'; page._token = 'session-two';
  page.setData({ notice: '', error: '新账号错误' });
  page.flushPickerFeedback();
  assert.equal(page.data.notice, '', '旧账号反馈不得回填');
  assert.equal(page.data.error, '新账号错误');
});

test('a recheck while a download is still in flight schedules a later resume', async () => {
  const downloads = [];
  const resolvers = [];
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', media: [{ media_id: 'img1', kind: 'image' }] };
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [note], versions: [] }),
    downloadProjectNoteMedia: (projectID, id) => { downloads.push(id); return new Promise(resolve => resolvers.push(resolve)); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1'; page._loaded = true;
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  page.onHide();
  await page.onShow();
  assert.equal(downloads.length, 1, '旧下载在途时不得并发发起新下载');
  resolvers[0]('/tmp/img1');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(downloads.length, 2, '旧下载收尾后自动恢复一次');
  resolvers[1]('/tmp/img1');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(page.data.noteMedia[0].local_path, '/tmp/img1');
});

test('a retry during a batch hydration never downloads the same item twice', async () => {
  const downloads = [];
  const resolvers = [];
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: (projectID, id) => { downloads.push(id); return new Promise(resolve => resolvers.push({ id, resolve })); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'a', kind: 'image' }, { media_id: 'b', kind: 'image' }] });
  const batch = page.hydrateNoteMedia(1, 'p');
  assert.deepEqual(downloads, ['a'], '批量串行，先下载第一项');
  page.retryNoteMediaPreview({ currentTarget: { dataset: { id: 'b' } } });
  assert.deepEqual(downloads, ['a', 'b'], '等待期间单项重试可先下载第二项');
  resolvers[0].resolve('/tmp/a');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.deepEqual(downloads, ['a', 'b'], '批量循环不得重复下载已占用的项');
  resolvers[1].resolve('/tmp/b');
  await batch;
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.deepEqual(downloads, ['a', 'b']);
  assert.equal(page.data.noteMedia[0].local_path, '/tmp/a');
  assert.equal(page.data.noteMedia[1].local_path, '/tmp/b');
});

// 批次 2 三评回归：封面保持、媒体鉴权终态、草稿实例绑定。
test('hydration never rewrites the loaded cover or dirties the form', async () => {
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: 'cover', media_ids: ['other', 'cover'] };
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async (projectID, id) => '/tmp/' + id + '.jpg' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(page.data.noteMedia[0].kind, 'image', 'private 项恢复为图片');
  assert.equal(page.noteDraft().cover_media_id, 'cover', '原封面不得被第一张图片替换');
  assert.equal(page.hasUnsavedChanges(), false, '补图不得污染 dirty state');
});

test('a media auth failure during hydration clears protected content and stops', async () => {
  const calls = [];
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '私密正文', body_format: 'plain', cover_media_id: 'cover', media_ids: ['cover', 'other'] };
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: async (projectID, id) => { calls.push(id); throw Object.assign(new Error('仅受邀账号可访问'), { statusCode: 403 }); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.deepEqual(calls, ['cover'], '鉴权终态后停止后续下载');
  assert.equal(page.data.noteBody, '', '清空受保护正文');
  assert.equal(page.data.projectLoadFailed, true);
});

test('a stale media auth failure cannot clear the new identity state', async () => {
  let rejectDownload;
  const note = { note_id: 'n1', revision: 1, title: '标题', body: 'A 正文', body_format: 'plain', cover_media_id: 'cover', media_ids: ['cover'] };
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: () => new Promise((resolve, reject) => { rejectDownload = reject; }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  page.onHide();
  page._token = 'session-two';
  page.setData({ noteBody: 'B 正文', projectLoadFailed: false });
  rejectDownload(Object.assign(new Error('仅受邀账号可访问'), { statusCode: 403 }));
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(page.data.noteBody, 'B 正文', '旧身份迟到 403 不得清空新状态');
  assert.equal(page.data.projectLoadFailed, false);
});

test('hydration write-back is bound to the draft instance that started it', async () => {
  const resolvers = [];
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: 'cover', media_ids: ['cover'] };
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: () => new Promise(resolve => resolvers.push(resolve)) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  const oldInstance = page.currentDraftInstanceID();
  page._newNote = true;
  page.ensureDraftInstance(true);
  assert.notEqual(page.currentDraftInstanceID(), oldInstance);
  resolvers[0]('/tmp/cover.jpg');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(page.data.noteMedia[0].local_path || '', '', '旧草稿实例的下载不得写入新草稿');
});

// 批次 2 四评：已保存但无封面的草稿，补缩略图不得推导出封面或弄脏表单。
test('a saved draft without a cover keeps the empty cover after hydration', async () => {
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: '', media_ids: ['img20001'] };
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async (projectID, id) => '/tmp/' + id + '.jpg' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(page.data.noteMedia[0].kind, 'image', '类型恢复仍生效');
  assert.equal(page.noteDraft().cover_media_id, '', '无封面草稿不得因补图产生封面');
  assert.equal(page.hasUnsavedChanges(), false, '补图不得弄脏表单');
});

// 已保存无封面草稿：用户新增图片后才默认取封面。
test('adding a new image to a coverless draft sets a default cover', async () => {
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: '', media_ids: [] };
  const page = pageHarness('projectnotes', { uploadProjectNoteMedia: async () => ({ asset: { media_id: 'newimg01', kind: 'image' } }) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ note, selectedProject: { project_id: 'p' }, noteMedia: [] });
  page.rememberLoadedForms();
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image', size: 1024 }]);
  assert.equal(page.noteDraft().cover_media_id, 'newimg01', '用户新增图片成为默认封面');
  assert.equal(page.hasUnsavedChanges(), true);
  page.removeNoteMedia({ currentTarget: { dataset: { id: 'newimg01' } } });
  assert.equal(page.noteDraft().cover_media_id, '', '删除新增图片后封面回到空');
});

// 批次 2 五评：删除非封面素材不得改变封面意图。
test('removing a non-cover item keeps the coverless draft coverless', async () => {
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: '', media_ids: ['oldimg01', 'vid20001'] };
  const page = pageHarness('projectnotes', { downloadProjectNoteMedia: async (projectID, id) => '/tmp/' + id + '.jpg' });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.applyProjectDetail({ project: { project_id: 'p' }, notes: [note], versions: [] }, '', null, null, 'editor');
  await new Promise(resolve => setTimeout(resolve, 10));
  page.removeNoteMedia({ currentTarget: { dataset: { id: 'vid20001' } } });
  assert.equal(page.noteDraft().cover_media_id, '', '删除视频不得让旧图片变成封面');
});

test('removing a non-cover item keeps the user chosen default cover', async () => {
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: '', media_ids: ['oldimg01', 'vid20001'] };
  const page = pageHarness('projectnotes', {
    uploadProjectNoteMedia: async () => ({ asset: { media_id: 'newimg01', kind: 'image' } }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ note, selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'oldimg01', kind: 'image' }, { media_id: 'vid20001', kind: 'video' }] });
  page.rememberLoadedForms();
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/new.jpg', fileType: 'image', size: 1024 }]);
  assert.equal(page.noteDraft().cover_media_id, 'newimg01');
  page.removeNoteMedia({ currentTarget: { dataset: { id: 'vid20001' } } });
  assert.equal(page.noteDraft().cover_media_id, 'newimg01', '删除无关视频不得覆盖新增封面');
});

// 批次 4 / T4.1+T4.2：就地面板、光标插入、8000 字截断、busy 排队、身份隔离。
test('the AI panel inserts at the tracked cursor without navigating', async () => {
  const page = pageHarness('projectnotes', {}, {
    navigateTo: () => { throw new Error('不得跳页'); },
    aiStream: { startAIStream: () => ({ abort() {} }) },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: 'abcdef' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: 'XY' });
  page._noteCursor = 3;
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, 'abcXYdef', '按光标插入而不是只追加');
});

test('AI insertion truncates at 8000 characters and keeps the original text', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  const existing = 'a'.repeat(7990);
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: existing });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: 'b'.repeat(50) });
  await page.insertNoteAI();
  assert.equal(page.data.noteBody.length, 8000);
  assert.ok(page.data.noteBody.startsWith(existing), '原文不得被破坏');
  assert.match(page.data.aiPanelNotice, /剩余 42 字/, '剩余内容可复制');
});

test('AI insertion waits for the page to become idle instead of dropping the result', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '排队结果', busy: true });
  const pending = page.insertNoteAI();
  page.setData({ busy: false });
  await pending;
  assert.equal(page.data.noteBody, '原文\n\n排队结果', '保存结束后必须插入，不得丢弃');
});

test('AI insertion after an identity change is refused', async () => {
  let token = 'session-token';
  const page = pageHarness('projectnotes', {}, {
    getStorageSync: key => key === 'token' ? token : '',
    aiStream: { startAIStream: () => ({ abort() {} }) },
  });
  page._visible = true; page._token = token; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '旧账号结果' });
  token = 'session-two'; page._token = 'session-two';
  await page.insertNoteAI();
  assert.doesNotMatch(page.data.noteBody, /旧账号结果/);
});

// Phase B P2 回归：去设置动作、private 单项重试、失败诊断字段、提示优先级。
test('a permission denial offers the settings action and clears it after success', async () => {
  const pickers = [];
  const page = pageHarness('projectnotes', {}, { chooseMedia: options => pickers.push(options), openSetting: () => {} });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  page.chooseNoteMedia();
  pickers[0].fail({ errMsg: 'chooseMedia:fail auth deny' });
  assert.equal(page.data.mediaPermissionDenied, true);
  page.chooseNoteMedia();
  pickers[1].success({ tempFiles: [] });
  assert.equal(page.data.mediaPermissionDenied, false);
});

test('failed uploads keep structured diagnostics for troubleshooting', async () => {
  const page = pageHarness('projectnotes', { uploadProjectNoteMedia: async () => { throw Object.assign(new Error('网络异常：timeout'), { networkError: true, errMsg: 'uploadFile:fail timeout' }); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' } });
  await page.uploadNoteMedia([{ tempFilePath: '/tmp/a.jpg', fileType: 'image', size: 1024 }]);
  const failure = page.data.mediaUploadFailures[0];
  assert.equal(failure.networkError, true);
  assert.equal(failure.errMsg, 'uploadFile:fail timeout');
  assert.match(failure.message, /网络异常/);
});

test('a recheck network error outranks a deferred cancel notice', async () => {
  const pickers = [];
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => { throw new Error('网络异常'); },
  }, { chooseMedia: options => pickers.push(options) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._loaded = true;
  page.setData({ selectedProject: { project_id: 'p' } });
  page.chooseNoteMedia();
  pickers[0].fail({ errMsg: 'chooseMedia:fail cancel' });
  await page.onShow();
  assert.match(page.data.notice, /重验失败/, '重验失败的提示优先，不被取消提示覆盖');
  assert.doesNotMatch(page.data.notice, /已取消/);
});

// 批次 3-4 复评 P1：面板隐私、旧流回调、可编辑结果、结构签名。
test('clearing protected state closes the AI panel and its result', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '私密结果' });
  page.clearProtected();
  assert.equal(page.data.aiPanelOpen, false);
  assert.equal(page.data.aiPanelText, '');
});

test('a stream from a closed panel cannot write into a new draft', async () => {
  let options;
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: value => { options = value; return { abort() {} }; } } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.startNoteAI();
  page.closeNoteAI();
  page.setData({ noteBody: '新草稿正文' });
  options.onDone('旧账号结果');
  assert.doesNotMatch(page.data.noteBody, /旧账号结果/, '关闭面板后旧回调必须失效');
});

test('the AI result is editable before insertion', async () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(wxml, /value="\{\{aiPanelText\}\}" bindinput="setNoteAIText"/, '结果区必须可编辑');
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '生成内容' });
  page.setNoteAIText({ detail: { value: '手工改写后的内容' } });
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, '原文\n\n手工改写后的内容');
});

test('revision or media changes invalidate the AI panel adoption', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '结果' });
  page.setData({ note: { note_id: 'n', revision: 4 } });
  await page.insertNoteAI();
  assert.doesNotMatch(page.data.noteBody, /结果/);
});

test('inserting the same AI result twice does not duplicate text', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '结果' });
  await page.insertNoteAI();
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, '原文\n\n结果');
});

// 批次 3-4 复评 P2：光标离焦位置、申请加载归属释放。
test('blur captures the cursor position used by the next insertion', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: 'abcdef' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: 'XY' });
  page.onNoteBodyBlur({ detail: { cursor: 3 } });
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, 'abcXYdef', '离焦光标位置必须生效');
});

test('a background join-request load releases its loading lock', async () => {
  let resolveList;
  const page = pageHarness('projectnotes', {
    listProjectJoinRequests: () => new Promise(resolve => { resolveList = resolve; }),
    listProjectInvitations: async () => ({ items: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, requestsLoading: false });
  const pending = page.loadJoinRequests();
  page.onHide();
  resolveList({ items: [] });
  await pending;
  assert.equal(page.data.requestsLoading, false, '后台返回后必须释放 requestsLoading');
});

// T3.1：本地文件可读时复用，失效时单项重下。
test('a still-readable local file is reused while a stale one is re-downloaded', async () => {
  const downloads = [];
  const note = { note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain', cover_media_id: '', media_ids: ['okfile01', 'stale001'] };
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: async (projectID, id) => { downloads.push(id); return '/tmp/' + id; },
  }, {
    getFileSystemManager: () => ({ accessSync(path) { if (String(path).includes('stale')) throw new Error('not found'); } }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._selectedNoteID = 'n1';
  page.setData({ selectedProject: { project_id: 'p' }, noteMedia: [{ media_id: 'okfile01', kind: 'image', local_path: '/tmp/okfile01.jpg' }, { media_id: 'stale001', kind: 'image', local_path: '/tmp/stale001.jpg' }] });
  await page.hydrateNoteMedia(1, 'p');
  assert.deepEqual(downloads, ['stale001'], '仅重下失效文件');
  assert.equal(page.data.noteMedia[1].local_path, '/tmp/stale001');
});

// 批次 3-4 复评：守卫锁定清面板、排队去重、连续插入光标推进。
test('the profile guard lock clears the AI panel and blocks copying', () => {
  const clipboard = [];
  const page = pageHarness('projectnotes', {}, { setClipboardData: options => clipboard.push(options.data), aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '私密结果' });
  page.onHide();
  assert.equal(page.data.aiPanelOpen, false, '守卫锁定/后台返回必须关闭面板');
  assert.equal(page.data.aiPanelText, '', '私密结果必须清空');
  page.copyNoteAI();
  assert.equal(clipboard.length, 0, '失效后不得复制旧结果');
});

test('queued insertion dedups and uses the edited result', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '旧编辑', busy: true });
  const first = page.insertNoteAI();
  const second = page.insertNoteAI();
  page.setData({ aiPanelText: '新编辑', busy: false });
  await Promise.all([first, second]);
  assert.equal(page.data.noteBody, '原文\n\n新编辑', '队列去重且采用等待后的最新结果');
});

test('sequential appends account for the separator when advancing the cursor', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: 'AB' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '1234' });
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, 'AB\n\n1234');
  page.setData({ aiPanelText: 'XY' });
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, 'AB\n\n1234\n\nXY', '第二次追加必须接在末尾而不是插进上次结果');
});

// 批次 3-4 三评：流式期间禁插、关闭后守卫锁定清空内存结果。
test('a streaming result cannot be inserted as a partial', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '半截', aiPanelStreaming: true });
  await page.insertNoteAI();
  assert.equal(page.data.noteBody, '原文', '流式中的半截结果不得插入');
});

test('a manually closed panel is still wiped when the guard locks later', () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '私密结果' });
  page.closeNoteAI();
  page.onHide();
  assert.equal(page.data.aiPanelText, '', '守卫锁定时必须清空内存中的私密结果');
});

// D21/D22：同 media_id 跨草稿实例不互相阻塞；迟到鉴权错误收尾加载态。
test('the same media id downloads independently for a new draft instance', async () => {
  const downloads = [];
  const resolvers = [];
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: (projectID, id) => { downloads.push(id); return new Promise(resolve => resolvers.push(resolve)); },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 1 }, noteMedia: [{ media_id: 'shared01', kind: 'image' }] });
  page.ensureDraftInstance();
  const first = page.hydrateNoteMedia(1, 'p');
  assert.equal(downloads.length, 1);
  page.ensureDraftInstance(true);
  const second = page.hydrateNoteMedia(1, 'p');
  assert.equal(downloads.length, 2, '新草稿实例必须发起自己的下载');
  resolvers[0]('/tmp/shared01');
  resolvers[1]('/tmp/shared01');
  await Promise.all([first, second]);
});

test('a late media auth error still clears the loading state', async () => {
  let rejectDownload;
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: () => new Promise((resolve, reject) => { rejectDownload = reject; }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 1 }, noteMedia: [{ media_id: 'img40001', kind: 'image' }] });
  page.ensureDraftInstance();
  const pending = page.hydrateNoteMedia(1, 'p');
  page.onHide();
  rejectDownload(Object.assign(new Error('仅受邀账号可访问'), { statusCode: 403 }));
  await pending;
  assert.equal(page.data.noteMedia[0].preview_loading, false, '迟到鉴权错误也必须收尾加载态');
});

// D49/D53：面板 Prompt 复用统一构建函数；结构字段变化全部拒绝；后台旧请求失败不写错误。
test('the AI panel prompt reuses the shared builder with the note title', async () => {
  const { ANTI_FABRICATION_CLAUSE } = require(path.join(root, 'utils/writing-policy.js'));
  let captured;
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: options => { captured = options; return { abort() {} }; } } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteTitle: '主题A', noteBody: '已知事实' });
  page.openNoteAIWriting();
  page.startNoteAI();
  const prompt = captured.messages[0].content;
  assert.match(prompt, /主题：主题A/, '必须保留标题主题');
  assert.ok(prompt.includes(ANTI_FABRICATION_CLAUSE), '必须复用共享防臆造条款');
});

test('every forbidden structure change blocks AI adoption', async () => {
  for (const change of ['note_id', 'revision', 'format', 'cover', 'media']) {
    const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3, body_format: 'plain', cover_media_id: 'c1' }, noteBody: '原文', noteMedia: [{ media_id: 'c1', kind: 'image' }, { media_id: 'c2', kind: 'image' }] });
    page.openNoteAIWriting();
    page.setData({ aiPanelText: 'AI 结果' });
    if (change === 'note_id') page.setData({ note: { note_id: 'n2', revision: 3, body_format: 'plain', cover_media_id: 'c1' } });
    if (change === 'revision') page.setData({ note: { note_id: 'n', revision: 4, body_format: 'plain', cover_media_id: 'c1' } });
    if (change === 'format') page.setData({ note: { note_id: 'n', revision: 3, body_format: 'article_v2', cover_media_id: 'c1' } });
    if (change === 'cover') page.setData({ note: { note_id: 'n', revision: 3, body_format: 'plain', cover_media_id: 'c2' } });
    if (change === 'media') page.setData({ noteMedia: [{ media_id: 'c1', kind: 'image' }, { media_id: 'm2', kind: 'image' }] });
    await page.insertNoteAI();
    assert.doesNotMatch(page.data.noteBody, /AI 结果/, `${change} 变化必须拒绝采纳`);
  }
});

test('a backgrounded join-request failure never writes a stale error', async () => {
  let rejectList;
  const page = pageHarness('projectnotes', {
    listProjectJoinRequests: () => new Promise((resolve, reject) => { rejectList = reject; }),
    listProjectInvitations: async () => ({ items: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p' }, requestsLoading: false, error: '' });
  const pending = page.loadJoinRequests();
  page.onHide();
  rejectList(new Error('后台失败'));
  await pending;
  assert.equal(page.data.error, '', '后台旧请求失败不得写入错误');
  assert.equal(page.data.requestsLoading, false);
});

// D1：后台完成的上传素材持久化，重启（新页面实例）后仍能补回。
test('pending media survives a restart for the same owner', async () => {
  const storage = { token: 'session-token' };
  const wxStubs = {
    getStorageSync: key => key === 'token' ? 'session-token' : storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
  };
  const first = pageHarness('projectnotes', {}, wxStubs);
  first._visible = true; first._token = 'session-token'; first._epoch = 1;
  first.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n1', revision: 1 } });
  const owner = first.mediaFailureOwnerKey();
  first.stashPendingMedia(owner, [{ file: { tempFilePath: '/tmp/a.jpg' }, asset: { media_id: 'm9001', kind: 'image' } }]);
  assert.ok(storage.project_note_pending_media_v1, '挂起素材必须持久化');

  const second = pageHarness('projectnotes', {}, wxStubs);
  second._visible = true; second._token = 'session-token'; second._epoch = 1;
  second._selectedNoteID = 'n1';
  second.applyProjectDetail({ project: { project_id: 'p' }, notes: [{ note_id: 'n1', revision: 1, title: '标题', body: '正文', body_format: 'plain' }], versions: [] }, '', null, null, 'editor');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.ok(second.data.noteMedia.some(item => item.media_id === 'm9001'), '重启后补回挂起素材');
});

// D44/D53：真实守卫包装器锁定清面板；排队等待有超时。
test('the real profile-access wrapper lock clears the AI panel', () => {
  global.wx = { getStorageSync: () => '', hideShareMenu() {} };
  const wrapper = require(path.join(root, 'utils/profile-access.js'));
  const base = {
    route: 'pages/projectnotes/projectnotes',
    data: { aiPanelOpen: true, aiPanelText: '私密结果', aiPanelPrompt: 'p', aiPanelStreaming: false, aiPanelError: '', aiPanelNotice: '' },
    setData(values, done) { Object.assign(this.data, values); if (done) done.call(this); },
    onHide() { this.data.aiPanelOpen = false; this.data.aiPanelText = ''; },
    onShow() {},
    onLoad() {},
    onReady() {},
  };
  base.__accessActive = true;
  const wrapped = wrapper.wrapProfileAccess(base);
  wrapped.onProfileAccessBlocked();
  assert.equal(wrapped.data.aiPanelOpen, false, '真实守卫锁定必须关闭面板');
  assert.equal(wrapped.data.aiPanelText, '', '真实守卫锁定必须清空私密结果');
  delete global.wx;
});

test('the AI insertion queue times out instead of waiting forever', async () => {
  const page = pageHarness('projectnotes', {}, { aiStream: { startAIStream: () => ({ abort() {} }) } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅' }, note: { note_id: 'n', revision: 3 }, noteBody: '原文' });
  page.openNoteAIWriting();
  page.setData({ aiPanelText: '排队结果', busy: true });
  page._aiIdleTimeoutMs = 200;
  const started = Date.now();
  await page.insertNoteAI();
  assert.ok(Date.now() - started >= 180, '必须等待到超时');
  assert.equal(page.data.noteBody, '原文', '超时后不得插入');
});

test('pending media survives normal unload and a fresh onLoad onShow for the same account', async () => {
  const storage = { token: 'session-token', userInfo: { id: 101 } };
  const wxStubs = { getStorageSync: key => storage[key], setStorageSync: (key, value) => { storage[key] = value; }, removeStorageSync: key => { delete storage[key]; } };
  const first = pageHarness('projectnotes', {}, wxStubs);
  first._token = 'session-token'; first._visible = true; first._epoch = 1;
  first.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n1', revision: 1 } });
  first.stashPendingMedia(first.mediaFailureOwnerKey(), [{ file: { tempFilePath: '/tmp/p.jpg' }, asset: { media_id: 'pending-image', kind: 'image' } }]);
  first.onUnload();
  assert.ok(storage.project_note_pending_media_v1, 'unload must retain account-scoped recovery records');
  const second = pageHarness('projectnotes', { listProjectNoteProjects: async () => ({ items: [] }), workbenchContext: async () => ({ owners: ['designer-101'] }) }, wxStubs);
  second.onLoad({}); await second.onShow();
  second._selectedNoteID = 'n1';
  second.applyProjectDetail({ project: { project_id: 'p' }, notes: [{ note_id: 'n1', revision: 1, title: '方案', body: '保留正文', body_format: 'plain' }], versions: [] }, '', null, null, 'editor');
  assert.ok(second.data.noteMedia.some(item => item.media_id === 'pending-image'));
  storage.userInfo = { id: 102 };
  const third = pageHarness('projectnotes', {}, wxStubs);
  third._selectedNoteID = 'n1';
  third.applyProjectDetail({ project: { project_id: 'p' }, notes: [{ note_id: 'n1', revision: 1, title: '方案', body: '正文', body_format: 'plain' }], versions: [] }, '', null, null, 'editor');
  assert.equal(third.data.noteMedia.some(item => item.media_id === 'pending-image'), false);
});


test('invited member opens a frozen directory without designer management and cannot share it', async () => {
  let managementCalls = 0;
  const page = pageHarness('projectnoteview', {
    directory: async () => ({ project: { project_id: 'p', name: '成员项目' }, versions: [{ version_id: 'v', title: '固定方案' }] }),
    version: async () => ({ project: { project_id: 'p' }, version: { version_id: 'v', body: '固定正文', body_format: 'plain', media: [] } }),
    getProjectNoteProject: async () => { managementCalls++; throw new Error('must not request management'); },
  });
  page.onLoad({ project_id: 'p' }); await page.onShow();
  assert.equal(page.data.memberDirectory.versions[0].version_id, 'v');
  await page.openMemberVersion({ currentTarget: { dataset: { id: 'v' } } });
  assert.equal(page.data.view.version.body, '固定正文');
  assert.equal(page.data.shareReady, false); assert.equal(managementCalls, 0);
  await page.backToMemberDirectory(); assert.equal(page.data.view, null);
  assert.equal(page.data.memberDirectory.project.name, '成员项目');
});

test('project creation selects only a returned customer and retains paging and older-share scope', async () => {
  const queries = [], created = [];
  const page = pageHarness('projectnotes', {
    projectCustomers: async query => { queries.push(query); return { items: [{ customer_ref: 'u:202', nickname: '张女士' }], next_cursor: 'customer-next', as_of: '2026-11-13T12:00:00Z', share_window: { truncated: true, next_cursor: 'older-shares' } }; },
    createProjectNoteProject: async input => { created.push(input); return { project_id: 'p1', ...input }; },
  });
  page._token = 'session-token'; page._visible = true; page._epoch = 1;
  page.setData({ pageView: 'create', projectName: '张宅', ownerID: 'designer-101', loading: false });
  await page.openProjectCustomers(); await page.loadMoreProjectCustomers();
  assert.equal(queries[1].cursor, 'customer-next'); assert.equal(queries[1].as_of, '2026-11-13T12:00:00Z');
  await page.loadOlderProjectCustomers(); assert.equal(queries[2].share_cursor, 'older-shares'); assert.equal(queries[2].cursor, '');
  page.selectProjectCustomer({ currentTarget: { dataset: { ref: 'u:999' } } }); assert.equal(page.data.customerRef, '');
  page.selectProjectCustomer({ currentTarget: { dataset: { ref: 'u:202' } } });
  await page.createProjectNoteProject(); page.onUnload();
  assert.equal(created[0].customer_ref, 'u:202'); assert.equal(created[0].owner_id, 'designer-101');
});

test('project detail can bind, replace and unbind an authorized customer after creation', async () => {
  const patches = [];
  const page = pageHarness('projectnotes', {
    updateProjectNoteProject: async (_id, patch) => { patches.push(patch); return { project_id: 'p1', name: '张宅', revision: patch.expected_revision + 1, customer_ref: patch.customer_ref }; },
  });
  page._token = 'session-token'; page._visible = true; page._epoch = 1;
  page.setData({ pageView: 'detail', selectedProject: { project_id: 'p1', name: '张宅', revision: 3, customer_ref: '' }, projectCustomers: [{ customer_ref: 'u:202', nickname: '张女士' }], customerPickerOpen: true });
  await page.selectProjectCustomer({ currentTarget: { dataset: { ref: 'u:202' } } });
  assert.equal(JSON.stringify(patches[0]), JSON.stringify({ customer_ref: 'u:202', expected_revision: 3 }));
  assert.equal(page.data.selectedProject.customer_ref, 'u:202');
  assert.equal(page.data.customerName, '张女士');
  await page.clearProjectCustomer();
  assert.equal(JSON.stringify(patches[1]), JSON.stringify({ customer_ref: '', expected_revision: 4 }));
  assert.equal(page.data.selectedProject.customer_ref, '');
});

test('project note template keeps project creation minimal and editor actions to save or send', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  const createView = template.slice(template.indexOf("<block wx:elif=\"{{pageView === 'create'}}\">"), template.indexOf("<block wx:elif=\"{{pageView === 'detail'"));
  const editorView = template.slice(template.indexOf("<block wx:elif=\"{{pageView === 'editor'"), template.indexOf("<block wx:elif=\"{{pageView === 'version'"));
  assert.match(createView, /项目名称/);
  assert.doesNotMatch(createView, /选择本人客户|搜索客户昵称|项目地点|建筑面积/);
  assert.equal((editorView.match(/>保存笔记</g) || []).length, 2, '新建和已保存状态各保留一个保存动作');
  assert.equal((editorView.match(/>发给客户</g) || []).length, 1);
  assert.doesNotMatch(editorView, /生成方案版本|历史版本|下一步/);
});

test('saving a project note completes the internal task and preserves edits typed during the request', async () => {
  let resolveSave;
  const page = pageHarness('projectnotes', {
    saveProjectNote: async (_id, draft) => new Promise(resolve => { resolveSave = () => resolve({ project_id: 'p', note_id: 'n', revision: 3, ...draft }); }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note: { project_id: 'p', note_id: 'n', revision: 2, title: '原题', body: '原文', body_format: 'plain', media_ids: [] }, noteTitle: '已修改', noteBody: '保存这一版', noteMedia: [] });
  const saving = page.saveProjectNote();
  page.setData({ noteBody: '请求中继续输入' });
  resolveSave();
  await saving;
  assert.equal(page.data.noteBody, '请求中继续输入');
  assert.equal(page.data.note.revision, 3, '已成功保存的修订号可继续用于下一次保存');
  assert.match(page.data.notice, /已保存/);
  assert.doesNotMatch(page.data.notice, /下一步|生成方案版本|创建分享/);
});

test('send stops when media changes while its prerequisite save is in flight', async () => {
  let finishSave, freezes = 0;
  const page = pageHarness('projectnotes', {
    saveProjectNote: async (_id, draft) => new Promise(resolve => { finishSave = () => resolve({ project_id: 'p', note_id: 'n', revision: 3, ...draft }); }),
    freezeProjectNoteVersion: async () => { freezes++; return {}; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note: { project_id: 'p', note_id: 'n', revision: 2, title: '方案', body: '正文', body_format: 'plain', media_ids: [] }, noteTitle: '方案', noteBody: '修改正文', noteMedia: [] });
  const sending = page.prepareProjectNoteSend();
  page.setData({ noteMedia: [{ media_id: 'new-image', kind: 'image' }] });
  finishSave(); await sending;
  assert.equal(freezes, 0);
  assert.equal(page.data.noteMedia[0].media_id, 'new-image');
});

test('send to customer saves, freezes and creates one private preview without claiming delivery', async () => {
  const calls = [], navigations = [];
  const page = pageHarness('projectnotes', {
    createProjectNote: async (_projectID, draft) => { calls.push('save'); return { project_id: 'p', note_id: 'n', revision: 1, ...draft }; },
    freezeProjectNoteVersion: async (_noteID, revision) => { calls.push('freeze:' + revision); return { project_id: 'p', note_id: 'n', version_id: 'v1', revision: 1, title: '首版', body: '方案正文', body_format: 'plain', media: [], project_snapshot: { name: '', location: '', building_area: 0, floors: 0, style: '' } }; },
    createProjectNoteShare: async (_versionID, _expires, key) => { calls.push('share:' + key); return { share_id: 's1', version_id: 'v1', share_token: 'private-token' }; },
    mutationKey: () => 'send-key',
  }, { navigateTo: options => navigations.push(options.url) });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._newNote = true; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, noteTitle: '首版', noteBody: '方案正文', noteMedia: [] });
  await page.prepareProjectNoteSend();
  assert.deepEqual(calls, ['save', 'freeze:1', 'share:send-key']);
  assert.equal(navigations[0], '/pages/projectnoteview/projectnoteview?pns=private-token');
  assert.doesNotMatch(page.data.notice, /已收到|已送达/);
});

test('send recovery reads back an exact frozen snapshot after a lost response without freezing twice', async () => {
  let freezeCalls = 0;
  const fixed = { project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', cover: null, media: [], project_snapshot: { name: '', location: '', building_area: 0, floors: 0, style: '' } };
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => { freezeCalls++; throw Object.assign(new Error('网络中断'), { networkError: true }); },
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, versions: [{ version_id: 'v1', note_id: 'n', revision: 2 }] }),
    getProjectNoteVersion: async () => fixed,
    createProjectNoteShare: async () => ({ share_id: 's1', version_id: 'v1', share_token: 'private-token' }),
    mutationKey: () => 'recovery-key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [] });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend();
  assert.equal(freezeCalls, 1);
  assert.equal(page.data.frozenVersion.version_id, 'v1');
  assert.equal(page.data.activeShare.share_token, 'private-token');
});

test('an unresolved freeze checkpoint is read back again and never blindly frozen twice', async () => {
  let freezes = 0, reads = 0;
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => { freezes++; throw Object.assign(new Error('网络中断'), { networkError: true }); },
    getProjectNoteProject: async () => { reads++; return { project: { project_id: 'p' }, versions: [] }; },
    mutationKey: () => 'uncertain-key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [] });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend(); await page.prepareProjectNoteSend();
  assert.equal(freezes, 1);
  assert.equal(reads, 2);
  assert.match(page.data.error, /结果暂不明确/);
});

test('send rejects a mismatched freeze response before creating a share', async () => {
  let shares = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => ({ project_id: 'p', note_id: 'n', version_id: 'wrong', revision: 2, title: '已保存', body: '另一份正文', body_format: 'plain', media: [] }),
    createProjectNoteShare: async () => { shares++; return { share_id: 'new', version_id: 'v1', share_token: 'new-token' }; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [], activeShare: { share_id: 'old', version_id: 'wrong', share_token: 'old-token', expires_at: '2020-01-01T00:00:00Z' } });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend();
  assert.equal(shares, 0);
  assert.match(page.data.error, /固定版本与当前笔记不一致/);
});

test('send ignores an expired cached share for the exact version', async () => {
  let shares = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const fixed = { project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2, title: note.title, body: note.body, body_format: 'plain', cover: null, media: [], project_snapshot: { name: '', location: '', building_area: 0, floors: 0, style: '' } };
  const page = pageHarness('projectnotes', {
    createProjectNoteShare: async () => { shares++; return { share_id: 'new', version_id: 'v1', share_token: 'new-token' }; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [], frozenVersion: fixed, activeShare: { share_id: 'old', version_id: 'v1', share_token: 'old-token', expires_at: '2020-01-01T00:00:00Z' } });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend();
  assert.equal(shares, 1);
  assert.equal(page.data.activeShare.share_token, 'new-token');
});

test('send never reuses a fixed version whose project snapshot is stale', async () => {
  let freezes = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const oldVersion = { project_id: 'p', note_id: 'n', version_id: 'old', revision: 2, title: note.title, body: note.body, body_format: 'plain', cover: null, media: [], project_snapshot: { name: '旧项目名', location: '旧址', building_area: 100, floors: 2, style: '旧风格', revision: 3 } };
  const newVersion = { ...oldVersion, version_id: 'new', project_snapshot: { name: '新项目名', location: '新址', building_area: 128, floors: 3, style: '新风格', revision: 8 } };
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => { freezes++; return newVersion; },
    createProjectNoteShare: async () => ({ share_id: 's', version_id: 'new', share_token: 'token' }), mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p', name: '新项目名', location: '新址', building_area: 128, floors: 3, style: '新风格', revision: 9 }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [], frozenVersion: oldVersion });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend();
  assert.equal(freezes, 1);
  assert.equal(page.data.frozenVersion.version_id, 'new');
});

test('send stops when an active exact-version share lost its one-time token', async () => {
  let shares = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const fixed = { project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2, title: note.title, body: note.body, body_format: 'plain', cover: null, media: [], project_snapshot: { name: '张宅', location: '', building_area: 0, floors: 0, style: '' } };
  const page = pageHarness('projectnotes', {
    createProjectNoteShare: async () => { shares++; return { share_id: 'new', version_id: 'v1', share_token: 'new-token' }; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p', name: '张宅' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [], frozenVersion: fixed, activeShare: { share_id: 'existing', version_id: 'v1', revoked: false, expires_at: '2099-01-01T00:00:00Z' } });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend();
  assert.equal(shares, 0);
  assert.match(page.data.error, /凭证|恢复/);
});

test('send retry does not create another share after a tokenless replay', async () => {
  let shares = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const fixed = { project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2, title: note.title, body: note.body, body_format: 'plain', cover: null, media: [], project_snapshot: { name: '张宅', location: '', building_area: 0, floors: 0, style: '' } };
  const page = pageHarness('projectnotes', {
    createProjectNoteShare: async () => { shares++; return { share_id: 'existing', version_id: 'v1', expires_at: '2099-01-01T00:00:00Z' }; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p', name: '张宅' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [], frozenVersion: fixed });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend(); await page.prepareProjectNoteSend();
  assert.equal(shares, 1);
  assert.match(page.data.error, /凭证|恢复/);
});

test('changing project metadata after share failure freezes and shares a new snapshot', async () => {
  const frozen = [], shareVersions = [], keys = [];
  let shareCall = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => {
      const project = page.data.selectedProject;
      const version = { project_id: 'p', note_id: 'n', version_id: 'v' + (frozen.length + 1), revision: 2, title: note.title, body: note.body, body_format: 'plain', cover: null, media: [], project_snapshot: { name: project.name, location: project.location || '', building_area: project.building_area || 0, floors: project.floors || 0, style: project.style || '' } };
      frozen.push(version); return version;
    },
    createProjectNoteShare: async (versionID, _expires, key) => { shareVersions.push(versionID); keys.push(key); shareCall++; if (shareCall === 1) throw Object.assign(new Error('网络中断'), { networkError: true }); return { share_id: 's2', version_id: versionID, share_token: 'token-2' }; },
    mutationKey: prefix => prefix + '-' + (keys.length + 1),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p', name: '旧项目名', location: '旧址', revision: 3 }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [] });
  page._noteFormBaseline = page.noteFormSignature();
  await page.prepareProjectNoteSend();
  page.setData({ selectedProject: { ...page.data.selectedProject, name: '新项目名', location: '新址', revision: 5 } });
  await page.prepareProjectNoteSend();
  assert.deepEqual(frozen.map(item => item.version_id), ['v1', 'v2']);
  assert.deepEqual(shareVersions, ['v1', 'v2']);
  assert.notEqual(keys[0], keys[1]);
  assert.equal(page.data.activeShare.version_id, 'v2');
});

test('changing project metadata while freeze is pending stops the old send', async () => {
  let finishFreeze, shares = 0;
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => new Promise(resolve => { finishFreeze = () => resolve({ project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2, title: note.title, body: note.body, body_format: 'plain', cover: null, media: [], project_snapshot: { name: '旧项目名', location: '', building_area: 0, floors: 0, style: '' } }); }),
    createProjectNoteShare: async () => { shares++; return {}; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p', name: '旧项目名', revision: 3 }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [] });
  page._noteFormBaseline = page.noteFormSignature();
  const sending = page.prepareProjectNoteSend(); await Promise.resolve();
  page.setData({ selectedProject: { ...page.data.selectedProject, name: '新项目名', revision: 4 } });
  finishFreeze(); await sending;
  assert.equal(shares, 0);
  assert.equal(page.data.busy, false);
  assert.match(page.data.notice, /停止|尚未发送/);
});

test('first send continues after save even when the setData render callback is asynchronous', async () => {
  let freezes = 0, shares = 0;
  const callbacks = [];
  const page = pageHarness('projectnotes', {
    createProjectNote: async (_project, draft) => ({ project_id: 'p', note_id: 'n', revision: 1, ...draft }),
    freezeProjectNoteVersion: async (_note, revision) => { freezes++; return { project_id: 'p', note_id: 'n', version_id: 'v1', revision, title: '首版', body: '正文', body_format: 'plain', cover: null, media: [], project_snapshot: { name: '张宅', location: '', building_area: 0, floors: 0, style: '' } }; },
    createProjectNoteShare: async () => { shares++; return { share_id: 's1', version_id: 'v1', share_token: 'token' }; }, mutationKey: () => 'key',
  });
  page.setData = function(values, done) { Object.assign(this.data, values); if (done) callbacks.push(done); };
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._newNote = true; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p', name: '张宅' }, noteTitle: '首版', noteBody: '正文', noteMedia: [] });
  await page.prepareProjectNoteSend();
  assert.equal(freezes, 1);
  assert.equal(shares, 1);
  callbacks.forEach(callback => callback());
});

test('send stops when the same note revision changes while freeze is pending', async () => {
  let finishFreeze, shares = 0;
  const fixed = { project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media: [] };
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => new Promise(resolve => { finishFreeze = () => resolve(fixed); }),
    createProjectNoteShare: async () => { shares++; return {}; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [] });
  page._noteFormBaseline = page.noteFormSignature();
  const sending = page.prepareProjectNoteSend(); await Promise.resolve();
  page.setData({ note: { ...note, revision: 3 } });
  finishFreeze(); await sending;
  assert.equal(shares, 0);
});

test('send preparation stops after an account change and never freezes the old account draft', async () => {
  let finishSave, freezes = 0;
  const storage = { token: 'account-a', userInfo: { id: 101 } };
  const page = pageHarness('projectnotes', {
    createProjectNote: async (_project, draft) => new Promise(resolve => { finishSave = () => resolve({ project_id: 'p', note_id: 'n', revision: 1, ...draft }); }),
    freezeProjectNoteVersion: async () => { freezes++; return {}; }, mutationKey: () => 'key',
  }, { getStorageSync: key => storage[key] || '' });
  page._visible = true; page._token = 'account-a'; page._epoch = 1; page._newNote = true; page.ensureDraftInstance(true);
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, noteTitle: '首版', noteBody: '正文', noteMedia: [] });
  const sending = page.prepareProjectNoteSend();
  storage.token = 'account-b'; storage.userInfo = { id: 202 }; page._token = 'account-b';
  finishSave(); await sending;
  assert.equal(freezes, 0);
});

test('editing during version freeze stops send and keeps the newer text editable', async () => {
  let finishFreeze, shares = 0;
  const page = pageHarness('projectnotes', {
    freezeProjectNoteVersion: async () => new Promise(resolve => { finishFreeze = () => resolve({ project_id: 'p', note_id: 'n', version_id: 'v1', revision: 2 }); }),
    createProjectNoteShare: async () => { shares++; return {}; }, mutationKey: () => 'key',
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.ensureDraftInstance(true);
  const note = { project_id: 'p', note_id: 'n', revision: 2, title: '已保存', body: '固定正文', body_format: 'plain', media_ids: [] };
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: note.body, noteMedia: [] });
  page._noteFormBaseline = page.noteFormSignature();
  const sending = page.prepareProjectNoteSend();
  await Promise.resolve();
  page.setData({ noteBody: '冻结期间继续修改' });
  finishFreeze(); await sending;
  assert.equal(shares, 0);
  assert.equal(page.data.noteBody, '冻结期间继续修改');
  assert.equal(page.data.busy, false);
  assert.match(page.data.notice, /停止|尚未发送/);
});

test('returning to an adopted article note preserves structured body media and cover when saving its title', async () => {
  const body = JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '原正文' }, { kind: 'video', media_id: 'adopted-video' }] });
  let note = { project_id: 'p', note_id: 'n', revision: 2, title: '原题', body, body_format: 'article_v2', media_ids: ['adopted-video'], cover_media_id: 'original-cover' }, saved;
  const page = pageHarness('projectnotes', {
    listProjectNoteProjects: async () => ({ items: [{ project_id: 'p' }] }), workbenchContext: async () => ({ owners: ['designer-101'] }),
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [note], versions: [] }),
    saveProjectNote: async (_id, input) => { saved = input; note = { ...note, ...input, revision: 3 }; return note; },
    downloadProjectNoteMedia: async () => '/tmp/adopted.mp4',
  });
  page.onLoad({ project_id: 'p', note_id: 'n', view: 'editor' }); await page.onShow();
  assert.equal(page.data.pageView, 'editor'); assert.equal(page.data.bodyReadOnly, true);
  page.setNoteTitle({ detail: { value: '修改标题' } }); await page.persistNote(true);
  assert.equal(saved.body, body); assert.equal(saved.body_format, 'article_v2'); assert.equal(saved.cover_media_id, 'original-cover');
  assert.deepEqual(Array.from(saved.media_ids), ['adopted-video']); page.onUnload();
});

test('customer deep link preselects only a freshly authorized exact customer', async () => {
  for (const allowed of [true, false]) {
    let query;
    const page = pageHarness('projectnotes', {
      listProjectNoteProjects: async () => ({ items: [] }), workbenchContext: async () => ({ owners: ['designer-101'] }),
      projectCustomers: async value => { query = value; return { items: allowed ? [{ customer_ref: 'u:202', nickname: '张女士' }] : [] }; },
    });
    page.onLoad({ customer_ref: 'u:202' }); await page.onShow();
    assert.equal(query.visitor, 'u:202'); assert.equal(page.data.pageView, 'create');
    assert.equal(page.data.customerRef, allowed ? 'u:202' : '');
    if (!allowed) assert.match(page.data.error, /无法关联/);
    page.onUnload();
  }
});

test('member media 404 revalidates fixed project and version while true revocation clears the view', async () => {
  let denied = false, reads = 0;
  const page = pageHarness('projectnoteview', {
    version: async () => { reads++; if (denied) throw Object.assign(new Error('成员已撤销'), { statusCode: 403 }); return { project: { project_id: 'p' }, version: { version_id: 'v', body_format: 'plain', body: '合法成员正文', media: [{ media_id: 'missing-image', kind: 'image' }] } }; },
    download: async () => { throw Object.assign(new Error('图片暂不可用'), { statusCode: 404 }); },
  });
  page.onLoad({ project_id: 'p' }); page._memberVersionID = 'v'; await page.onShow();
  assert.equal(reads, 2); assert.equal(page.data.view.version.body, '合法成员正文'); assert.equal(page.data.mediaFailuresCount, 1);
  denied = true; await page.revalidateShared(); assert.equal(page.data.view, null); assert.match(page.data.error, /访问权限/);
});

test('adopted structured note previews original image and both videos in document order without changing saved draft', async () => {
  const body = JSON.stringify({ schema: 1, blocks: [
    { kind: 'paragraph', text: '保留原正文' }, { kind: 'image', media_id: 'original-image' },
    { kind: 'video', media_id: 'original-video' }, { kind: 'video', media_id: 'adopted-video' },
  ] });
  let savedPayload;
  let note = { project_id: 'p', note_id: 'n', revision: 2, title: '采用后方案', body, body_format: 'article_v2', cover_media_id: 'original-image', media_ids: ['original-image', 'original-video', 'adopted-video'] };
  const page = pageHarness('projectnotes', {
    downloadProjectNoteMedia: async (_project, id) => '/tmp/' + id,
    saveProjectNote: async (_id, input) => { savedPayload = input; note = { ...note, ...input, revision: 3 }; return note; },
    getProjectNoteProject: async () => ({ project: { project_id: 'p' }, notes: [note], versions: [] }),
  });
  page._token = 'session-token'; page._epoch = 1; page._visible = true;
  page.setData({ pageView: 'editor', selectedProject: { project_id: 'p' }, note, noteTitle: note.title, noteBody: body, bodyReadOnly: true,
    noteMedia: [
      { media_id: 'original-image', kind: 'image', local_path: '/tmp/image.jpg', width: 800, height: 600 },
      { media_id: 'original-video', kind: 'video', local_path: '/tmp/original.mp4', width: 1280, height: 720 },
      { media_id: 'adopted-video', kind: 'video', local_path: '/tmp/adopted.mp4', width: 1280, height: 720 },
    ],
  });
  const draftBefore = JSON.stringify(page.noteDraft());
  await page.previewNote();
  assert.equal(page.data.previewPresentation.kind, 'article');
  assert.deepEqual(Array.from(page.data.previewPresentation.blocks, item => [item.kind, item.media_id || item.text]), [
    ['paragraph', '保留原正文'], ['image', 'original-image'], ['video', 'original-video'], ['video', 'adopted-video'],
  ]);
  assert.deepEqual(Array.from(page.data.previewPresentation.media, item => item.media_id), ['original-image', 'original-video', 'adopted-video']);
  assert.equal(JSON.stringify(page.noteDraft()), draftBefore);
  await page.persistNote(true);
  assert.equal(savedPayload.body, body); assert.equal(savedPayload.body_format, 'article_v2'); assert.equal(savedPayload.cover_media_id, 'original-image');
  assert.deepEqual(Array.from(savedPayload.media_ids), ['original-image', 'original-video', 'adopted-video']);
  page.onUnload();
});
