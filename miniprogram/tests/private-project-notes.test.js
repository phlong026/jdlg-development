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

test('project AI carries only known facts and returns to the same note across hide/show without losing media', async () => {
  let navigation; const sent = [];
  const page = pageHarness('projectnotes', { getProjectNoteProject: async () => ({ project: { project_id: 'p' } }) }, { navigateTo: options => { navigation = options; options.success({ eventChannel: { emit: (...args) => sent.push(args) } }); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: { project_id: 'p', name: '张宅', location: '苏州', floors: 2, crm_id: 'private-crm', revision: 1 }, note: { note_id: 'n', revision: 3 }, noteTitle: '方案说明', noteBody: '已知事实', noteMedia: [{ media_id: 'private-media' }], feedbacks: [{ text: '私人反馈' }] });
  page.openNoteAIWriting();
  assert.match(navigation.url, /source=project_note/);
  assert.equal(sent[0][0], 'writingContext');
  assert.match(sent[0][1].facts, /苏州/);
  assert.doesNotMatch(sent[0][1].facts, /private-|私人反馈|建筑面积/);
  page.onHide();
  const result = { source: 'project_note', project_id: 'p', note_id: 'n', note_revision: 3, title: 'AI标题', text: '新的方案说明' };
  navigation.events.prefillContent(result); navigation.events.prefillContent(result);
  await page.onShow();
  assert.equal(page.data.noteTitle, '方案说明');
  assert.equal(page.data.noteBody, '已知事实\n\n新的方案说明');
  assert.equal(page.data.noteMedia[0].media_id, 'private-media');
  assert.equal(page.data.note.note_id, 'n');
});

test('project AI rejects changed drafts, revision mismatches and wrong target identities', () => {
  for (const change of ['text', 'revision', 'identity']) {
    let navigation;
    const page = pageHarness('projectnotes', {}, { navigateTo: options => { navigation = options; } });
    page._visible = true; page._token = 'session-token'; page._epoch = 1;
    page.setData({ selectedProject: { project_id: 'p' }, note: { note_id: 'n', revision: 3 }, noteTitle: '标题', noteBody: '原文' });
    page.openNoteAIWriting();
    if (change === 'text') page.setData({ noteBody: '用户刚修改' });
    if (change === 'revision') page.setData({ note: { note_id: 'n', revision: 4 } });
    navigation.events.prefillContent({ source: 'project_note', project_id: change === 'identity' ? 'other' : 'p', note_id: 'n', note_revision: 3, text: '不能采用' });
    assert.doesNotMatch(page.data.noteBody, /不能采用/);
    assert.match(page.data.notice, /未采用/);
  }
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

test('AI return preserves its fill but still clears private content after permission revocation', async () => {
  let navigation, checks = 0;
  const page = pageHarness('projectnotes', {
    getProjectNoteProject: async () => { checks += 1; throw Object.assign(new Error('已撤权'), { statusCode: 403 }); },
  }, { navigateTo: options => { navigation = options; options.success({ eventChannel: { emit() {} } }); } });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project: { project_id: 'project-one' }, notes: [{ note_id: 'note-one', revision: 1, title: '标题', body: '已保存', body_format: 'plain' }], versions: [] });
  page.openNoteAIWriting();
  page.onHide();
  navigation.events.prefillContent({ source: 'project_note', project_id: 'project-one', note_id: 'note-one', note_revision: 1, text: 'AI 补充' });
  assert.match(page.data.noteBody, /AI 补充/);
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
    page.setData({ selectedProject: { project_id: 'project-one' }, note: update ? { note_id: 'note-one', revision: 7 } : null, noteTitle: '方案说明', noteBody: '保留庭院入口', noteMedia: [{ media_id: 'image-one', kind: 'image' }] });
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
