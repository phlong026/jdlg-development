'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function loadAPI() {
  const calls = [];
  const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');
  const module = { exports: {} };
  const wx = {
    getStorageSync: key => key === 'token' ? 'session-token' : '',
    request(options) { calls.push(options); options.success({ statusCode: 200, data: { code: 0, data: {} } }); },
  };
  vm.runInNewContext(source, { module, exports: module.exports, wx, require(id) {
    if (id === './parameter') return { generateRequestParams: (_token, _body, apiPath) => ({ apiPath }), paramsToHeaders: params => ({ signedPath: params.apiPath }) };
    throw new Error(`unexpected require ${id}`);
  }, Promise, JSON, Error, Object, String, Number, Date, encodeURIComponent });
  return { api: module.exports, calls };
}

function invitePage(api, wxOverrides = {}) {
  let definition;
  const source = fs.readFileSync(path.join(root, 'pages/projectinvite/projectinvite.ts'), 'utf8');
  let token = 'account-a';
  const wx = Object.assign({ getStorageSync: key => key === 'token' ? token : '', hideShareMenu() {}, navigateTo() {}, navigateBack() {} }, wxOverrides);
  vm.runInNewContext(source, { Page(value) { definition = value; }, wx, require(id) {
    if (id === '../../utils/phase3-api') return api;
    throw new Error(`unexpected require ${id}`);
  }, Promise, JSON, Error, Object, String, Number, Date, encodeURIComponent, console, setTimeout, clearTimeout });
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values, done) { Object.assign(this.data, values); if (done) done(); }, setToken(value) { token = value; } }, definition);
  return page;
}

test('project invitation API separates designer review from applicant confirmation', async () => {
  const { api, calls } = loadAPI();
  await api.createProjectInvitation('project-one', '2026-11-01T08:00:00Z', 'create-key');
  await api.listProjectInvitations('project-one');
  await api.getProjectInvitation('invite-token');
  await api.applyProjectInvitation('invite-token', 'apply-key');
  await api.listProjectJoinRequests('project-one');
  await api.approveProjectJoinRequest('project-one', 'request-one', 'approve-key');
  await api.revokeProjectInvitation('project-one', 'invitation-one');
  assert.deepEqual(calls.map(call => call.header.signedPath), [
    '/api/v1/miniprogram/project-notes/projects/project-one/member-invitations',
    '/api/v1/miniprogram/project-notes/projects/project-one/member-invitations',
    '/api/v1/miniprogram/project-notes/member-invitations/invite-token',
    '/api/v1/miniprogram/project-notes/member-invitations/invite-token/apply',
    '/api/v1/miniprogram/project-notes/projects/project-one/member-requests',
    '/api/v1/miniprogram/project-notes/projects/project-one/member-requests/request-one/approve',
    '/api/v1/miniprogram/project-notes/projects/project-one/member-invitations/invitation-one',
  ]);
});

test('invitation confirmation survives login but drops an old-account response', async () => {
  let resolvePreview;
  const preview = new Promise(resolve => { resolvePreview = resolve; });
  let reads = 0;
  const page = invitePage({ getProjectInvitation: () => ++reads === 1 ? preview : new Promise(() => {}), mutationKey: prefix => prefix + '-key' });
  page.onLoad({ pni: 'invite-token' });
  const pending = page.onShow();
  page.setToken('account-b');
  page.onShow();
  resolvePreview({ project_name: '张宅方案', expires_at: '2026-11-01T08:00:00Z' });
  await pending;
  assert.equal(page.data.preview, null);
});

test('invitation preview is cleared on hide and revalidated without losing the apply retry key', async () => {
  let resolveStale;
  const stale = new Promise(resolve => { resolveStale = resolve; });
  let reads = 0;
  const page = invitePage({
    mutationKey: () => 'stable-apply-key',
    getProjectInvitation: async () => {
      reads += 1;
      if (reads === 1) return { project_name: '张宅方案' };
      if (reads === 2) return stale;
      throw Object.assign(new Error('已撤销'), { statusCode: 404 });
    },
  });
  page.onLoad({ pni: 'invite-token' });
  await page.onShow();
  page._applyAttempt = { key: 'stable-apply-key' };
  page.onHide();
  assert.equal(page.data.preview, null);
  assert.equal(page._applyAttempt.key, 'stable-apply-key');
  const oldLoad = page.onShow();
  page.onHide();
  await page.onShow();
  assert.equal(reads, 3);
  assert.match(page.data.error, /失效/);
  resolveStale({ project_name: '不应回填的项目' });
  await oldLoad;
  assert.equal(page.data.preview, null);
  assert.match(page.data.error, /失效/);
});

test('invitation page distinguishes pending review from approved membership', async () => {
  const pending = invitePage({ getProjectInvitation: async () => ({ project_name: '张宅方案', request_status: 'pending' }) });
  pending.onLoad({ pni: 'invite-token' });
  await pending.onShow();
  assert.equal(pending.data.requestStatus, 'pending');

  const approved = invitePage({ getProjectInvitation: async () => ({ project_name: '张宅方案', request_status: 'approved' }) });
  approved.onLoad({ pni: 'invite-token' });
  await approved.onShow();
  assert.equal(approved.data.requestStatus, 'approved');

  const template = fs.readFileSync(path.join(root, 'pages/projectinvite/projectinvite.wxml'), 'utf8');
  assert.match(template, /申请待审核/);
  assert.match(template, /已加入项目/);
});

test('invitation application retries one stable key and never claims direct access', async () => {
  const keys = [];
  const page = invitePage({
    mutationKey: prefix => prefix + '-stable',
    getProjectInvitation: async () => ({ project_name: '张宅方案' }),
    applyProjectInvitation: async (_token, key) => { keys.push(key); if (keys.length === 1) throw new Error('response lost'); return { status: 'pending' }; },
  });
  page.onLoad({ pni: 'invite-token' });
  await page.onShow();
  await page.applyInvitation();
  await page.applyInvitation();
  assert.equal(keys[0], keys[1]);
  assert.equal(page.data.applied, true);
  const template = fs.readFileSync(path.join(root, 'pages/projectinvite/projectinvite.wxml'), 'utf8');
  assert.match(template, /申请待审核/);
  assert.doesNotMatch(template, /已获得项目权限|查看方案/);
});

test('project member UI uses invite requests and does not expose manual account ids', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  const core = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes-core.js'), 'utf8');
  const packageTemplate = fs.readFileSync(path.join(root, 'pages/projectpackage/projectpackage.wxml'), 'utf8');
  assert.match(template, /createProjectInvitation/);
  assert.match(template, /approveProjectJoinRequest/);
  assert.match(template, /joinRequests/);
  assert.match(template, /memberInvitations/);
  assert.match(core, /listProjectInvitations/);
  assert.doesNotMatch(template, /memberAccount|输入对方甲第账号|account ID|账号 #/);
  assert.doesNotMatch(core, /inviteProjectNoteMember|memberMutation\(false/);
  assert.doesNotMatch(packageTemplate, /账号 #|#\{\{item\.actor_id\}\}/);
});

test('project invitation page is registered as private and excluded from tracking', () => {
  const app = JSON.parse(fs.readFileSync(path.join(root, 'app.json'), 'utf8'));
  const routes = require(path.join(root, 'utils/tracking/routes.js'));
  assert.ok(app.subPackages.some(pack => pack.root === 'pages/projectinvite' && pack.pages.includes('projectinvite')));
  const policy = routes.policy('pages/projectinvite/projectinvite');
  assert.equal(policy.collect, false);
  assert.equal(policy.exclusionReason, 'private-project-notes');
});


test('invitation enter opens the member reader and discards a late apply from another account', async () => {
  let resolveApply; const navigation = [];
  const page = invitePage({ mutationKey: () => 'stable-key', getProjectInvitation: async () => ({ project_name: '方案', request_status: 'approved', project_id: 'project-b' }), applyProjectInvitation: () => new Promise(resolve => { resolveApply = resolve; }) }, { navigateTo: value => navigation.push(value.url) });
  page.onLoad({ pni: 'invite-token' }); await page.onShow();
  page.enterProject();
  assert.equal(navigation[0], '/pages/projectnoteview/projectnoteview?project_id=project-b');
  const pending = page.applyInvitation(); page.onHide(); page.setToken('account-b'); await page.onShow();
  resolveApply({ status: 'approved', project_id: 'project-a' }); await pending;
  assert.equal(page.data.projectID, 'project-b');
  assert.equal(navigation.length, 1);
});
