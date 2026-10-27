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
    if (id === '../../utils/phase3-api') return api;
    if (id === '../../utils/article-document') return require(path.join(root, 'utils/article-document.js'));
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
  Object.assign(page.data, { projects: [{ project_id: 'private-project' }], owners: ['owner-a'], ownerID: 'owner-a', projectName: '张宅', crmID: 'crm-secret', noteTitle: '私密标题', noteBody: '私密草稿', memberAccount: '101', confirmerAccount: '101', feedbackID: 'feedback-secret', feedbackReply: '已处理', noteMedia: [{ media_id: 'private-media' }] });
  page.clearProtected();
  assert.equal(page.data.projects.length, 0);
  assert.equal(page.data.owners.length, 0);
  assert.equal(page.data.ownerID, '');
  assert.equal(page.data.projectName, '');
  assert.equal(page.data.crmID, '');
  assert.equal(page.data.noteTitle, '');
  assert.equal(page.data.noteBody, '');
  assert.equal(page.data.memberAccount, '');
  assert.equal(page.data.confirmerAccount, '');
  assert.equal(page.data.feedbackReply, '');
  assert.equal(page.data.feedbackID, '');
  assert.equal(page.data.noteMedia.length, 0);
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

test('shared note opens only after protected content is rendered and retries one stable event id', async () => {
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
  assert.equal(calls.length, 2);
  assert.notEqual(calls[0].eventID, calls[1].eventID);
  assert.equal(calls[1].token, 'account-b');
  assert.equal(page._feedbackAttempt, null);
  resolveA({});
  await pendingA;
  assert.equal(page._openSent, true);
  assert.equal(page._openPending, null);
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

test('project list follows next_cursor and exact missing version never falls back to first fifty', async () => {
  const projects = Array.from({ length: 31 }, (_, index) => ({ project_id: `project-${index + 1}`, name: `项目${index + 1}` }));
  const listCalls = [];
  const page = pageHarness('projectnotes', {
    listProjectNoteProjects: async query => { listCalls.push(query || {}); return listCalls.length === 1 ? { items: projects.slice(0, 30), next_cursor: 'page-2' } : { items: projects.slice(30) }; },
    workbenchContext: async () => ({ owners: ['owner-one'] }),
    getProjectNoteProject: async () => ({ project: projects[30], notes: [{ note_id: 'note-first', title: '错误草稿', body: 'wrong', body_format: 'plain_text' }], versions: Array.from({ length: 50 }, (_, index) => ({ version_id: `old-${index}`, note_id: 'note-first' })), shares: [], feedbacks: [{ feedback_id: 'wrong', version_id: 'old-0' }], confirmations: [{ confirmation_id: 'wrong', version_id: 'old-0' }], truncated: { versions: true } }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page._targetProjectID = 'project-31'; page._targetVersionID = 'wanted-51';
  await page.loadProjects(1);
  assert.equal(page.data.projects.length, 31);
  assert.equal(listCalls[1].cursor, 'page-2');
  assert.equal(page.data.frozenVersion, null);
  assert.equal(page.data.note, null);
  assert.equal(page.data.feedbacks.length, 0);
  assert.match(page.data.historyNotice, /历史|未加载/);
});

test('member mutations refresh active members and confirmer is selected from them', async () => {
  let reads = 0;
  const detail = () => ({ project: { project_id: 'project-one', name: '张宅', confirmer_id: reads > 1 ? 202 : 0 }, notes: [], versions: [], shares: [], feedbacks: [], confirmations: [], truncated: {}, members: [{ actor_id: 202, active: true }] });
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => { reads += 1; return detail(); }, inviteProjectNoteMember: async () => ({}), setProjectNoteConfirmer: async () => ({}),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1; page.setData({ selectedProject: { project_id: 'project-one' }, memberAccount: '202' });
  await page.inviteProjectNoteMember();
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
  assert.doesNotMatch(template, /wx:else-if=/);
  assert.match(template, /<block\s+wx:elif="\{\{view\}\}">/);
  assert.match(template, /<rich-text\s+wx:elif="\{\{view\.version\.body_format === 'rich_text'\}\}"[^>]*nodes="\{\{view\.version\.body\}\}"/);
  assert.match(template, /<text\s+wx:else\s+class="plain"[^>]*>\{\{view\.version\.body\}\}<\/text>/);
  assert.match(template, /wx:if="\{\{view\.activity_truncated\}\}"[^>]*>[^<]*最近50条反馈与处理记录/);
  assert.match(template, /wx:if="\{\{canConfirm \|\| confirmed\}\}"\s+class="card confirm"/);
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
  for (const action of ['createProjectNoteProject', 'inviteProjectNoteMember', 'setProjectNoteConfirmer', 'createProjectNote', 'saveProjectNote', 'freezeProjectNoteVersion', 'createProjectNoteShare', 'revokeProjectNoteShare', 'resolveProjectNoteFeedback']) {
    assert.match(source, new RegExp(action));
  }
  const workbench = fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.ts'), 'utf8');
  assert.match(workbench, /openNotes\(\).*\/pages\/projectnotes\/projectnotes/);
});
