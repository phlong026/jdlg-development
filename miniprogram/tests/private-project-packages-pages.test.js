'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function pageHarness(name, api = {}, wxOverrides = {}) {
  let definition;
  const source = fs.readFileSync(path.join(root, `pages/${name}/${name}.ts`), 'utf8');
  let token = 'session-token';
  const wx = Object.assign({
    getStorageSync: key => key === 'token' ? token : '',
    getWindowInfo: () => ({ windowWidth: 375 }), hideShareMenu() {}, showShareMenu() {}, navigateTo() {}, navigateBack() {}, setClipboardData() {}, showToast() {}, previewImage() {}, saveImageToPhotosAlbum() {},
  }, wxOverrides);
  const context = { Page(value) { definition = value; }, wx, require(id) {
    if (id === '../../utils/project-packages-api') return api;
    if (id === '../../utils/article-document') return require(path.join(root, 'utils/article-document.js'));
    if (id === '../../utils/content-presentation') return require(path.join(root, 'utils/content-presentation.js'));
    if (id === '../../utils/project-note-media') return require(path.join(root, 'utils/project-note-media.js'));
    throw new Error(`unexpected require ${id}`);
  }, Promise, JSON, Error, Object, String, Number, Date, Map, Set, encodeURIComponent, decodeURIComponent, console };
  vm.runInNewContext(source, context);
  return Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values, done) { Object.assign(this.data, values); if (done) done(); }, setToken(value) { token = value; } }, definition);
}

test('package composer opens members for the current project without changing selections', () => {
  let target;
  const page = pageHarness('projectpackage', {}, { navigateTo(options) { target = options; } });
  page._projectID = 'project one';
  page.data.selectedNoteIDs = ['note-one'];
  page.openProjectMembers();
  assert.equal(target.url, '/pages/projectnotes/projectnotes?project_id=project%20one&view=members&return_to=package');
  assert.deepEqual(page.data.selectedNoteIDs, ['note-one']);
  target.fail();
  assert.equal(page.data.notice, '暂时无法打开项目成员，请重试。');
});

test('package composer preselects only the designated confirmer and never every member', () => {
  const page = pageHarness('projectpackage');
  page.applyProject({
    project: { project_id: 'project-one', name: '张宅', confirmer_id: 102 },
    members: [{ actor_id: 101 }, { actor_id: 102 }, { actor_id: 103 }],
    versions: [{ version_id: 'version-one', title: '确认版' }],
  });
  assert.equal(page.data.selectedRecipientIDs.join(','), '102');
  assert.equal(page.data.selectedVersionIDs.length, 0);
  assert.equal(page.data.projectName, '张宅');
});

test('package composer keeps frozen-version selection explicit and produces independent share path', async () => {
  let published;
  const page = pageHarness('projectpackage', {
    mutationKey: prefix => prefix + '-key',
    saveProjectPackageDraft: async (_project, body) => ({ package_id: 'package-one', revision: 1, ...body }),
    previewProjectPackage: async () => ({ package_id: 'package-one', revision: 1, preview_signature: 'preview-one', project: { name: '张宅' }, items: [{ version_id: 'version-one', title: '确认版' }], recipients: [{ actor_id: 102 }] }),
    publishProjectPackage: async (_id, body) => { published = body; return { delivery_id: 'delivery-one', package_token: 'package-token' }; },
  });
  page._visible = true; page._epoch = 1; page._accountToken = 'session-token'; page._projectID = 'project-one';
  page.applyProject({ project: { project_id: 'project-one', name: '张宅', confirmer_id: 102 }, members: [{ actor_id: 102 }], versions: [{ version_id: 'version-one', title: '确认版' }] });
  page.toggleVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  await page.previewPackage();
  assert.equal(page.data.step, 'preview');
  await page.publishPackage();
  assert.equal(page.data.step, 'shared');
  assert.equal(page.data.sharePath, '/pages/projectpackageview/projectpackageview?pps=package-token');
  assert.equal(page.data.historyDeliveries[0].delivery_id, 'delivery-one');
  assert.equal(published.preview_signature, 'preview-one');
});

test('package preview presents recipient profile without replacing the real actor id', async () => {
  const page = pageHarness('projectpackage', {
    mutationKey: prefix => prefix + '-key',
    saveProjectPackageDraft: async (_project, body) => ({ package_id: 'package-one', revision: 1, ...body }),
    previewProjectPackage: async () => ({
      package_id: 'package-one', revision: 1, preview_signature: 'preview-one',
      project: { name: '张宅' }, items: [{ version_id: 'version-one', title: '确认版' }], recipients: [{ actor_id: 102 }],
    }),
  });
  page._visible = true; page._epoch = 1; page._accountToken = 'session-token'; page._projectID = 'project-one';
  page.applyProject({
    project: { project_id: 'project-one', name: '张宅', confirmer_id: 102 },
    members: [{ actor_id: 102, display_name: '张女士', avatar: 'https://public.example/avatar.png' }],
    versions: [{ version_id: 'version-one', title: '确认版' }],
  });
  page.toggleVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  await page.previewPackage();
  assert.equal(page.data.preview.recipients[0].actor_id, 102);
  assert.equal(page.data.preview.recipients[0].display_name, '张女士');
  assert.equal(page.data.preview.recipients[0].avatar, 'https://public.example/avatar.png');
});

test('package composer can preview saved notes without requiring a manual freeze first', async () => {
  let saved;
  const page = pageHarness('projectpackage', {
    mutationKey: prefix => prefix + '-key',
    saveProjectPackageDraft: async (_project, body) => { saved = body; return { package_id: 'package-note', revision: 1, ...body }; },
    previewProjectPackage: async () => ({ package_id: 'package-note', revision: 1, preview_signature: 'preview-note', project: { name: '张宅' }, items: [{ note_id: 'note-one', revision: 3, title: '平面方案', pending_freeze: true }], recipients: [{ actor_id: 102 }] }),
  });
  page._visible = true; page._epoch = 1; page._accountToken = 'session-token'; page._projectID = 'project-one';
  page.applyProject({ project: { project_id: 'project-one', name: '张宅', confirmer_id: 102 }, members: [{ actor_id: 102 }], notes: [{ note_id: 'note-one', revision: 3, title: '平面方案' }], versions: [] });
  page.toggleNote({ currentTarget: { dataset: { id: 'note-one' } } });
  await page.previewPackage();
  assert.equal(saved.note_selections.length, 1);
  assert.equal(saved.note_selections[0].note_id, 'note-one');
  assert.equal(saved.note_selections[0].revision, 3);
  assert.equal(page.data.preview.items[0].pending_freeze, true);
});

test('package composer keeps one mutation key and expiry across a retry', async () => {
  const drafts = [], publishes = [];
  const page = pageHarness('projectpackage', {
    mutationKey: prefix => prefix + '-stable',
    saveProjectPackageDraft: async (_project, body) => { drafts.push(body); if (drafts.length === 1) throw new Error('response lost'); return { package_id: 'package-one', revision: 1 }; },
    previewProjectPackage: async () => ({ package_id: 'package-one', revision: 1, preview_signature: 'preview-one', project: {}, items: [], recipients: [] }),
    publishProjectPackage: async (_id, body) => { publishes.push(body); if (publishes.length === 1) throw new Error('response lost'); return { delivery_id: 'delivery-one', package_token: 'package-token' }; },
  });
  page._visible = true; page._epoch = 1; page._accountToken = 'session-token'; page._projectID = 'project-one';
  page.applyProject({ project: { project_id: 'project-one', name: '张宅', confirmer_id: 101 }, members: [{ actor_id: 101 }], versions: [{ version_id: 'version-one' }] });
  page.toggleVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  await page.previewPackage();
  await page.previewPackage();
  assert.equal(drafts[0].idempotency_key, drafts[1].idempotency_key);
  await page.publishPackage();
  await page.publishPackage();
  assert.equal(publishes[0].idempotency_key, publishes[1].idempotency_key);
  assert.equal(publishes[0].expires_at, publishes[1].expires_at);
});

test('invited package opens exact version and returns to directory in-page', async () => {
  const calls = [];
  const page = pageHarness('projectpackageview', {
    getSharedProjectPackage: async token => { calls.push(['directory', token]); return { project: { name: '张宅' }, items: [{ version_id: 'version-one', title: '确认版' }] }; },
    getSharedProjectPackageVersion: async (token, version) => { calls.push(['version', token, version]); return { version: { version_id: version, body_format: 'plain', body: '固定正文', media: [] }, feedbacks: [], can_confirm: false }; },
  });
  page.onLoad({ pps: 'package-token' });
  await page.onShow();
  await page.openVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  assert.equal(page.data.pageView, 'version');
  assert.deepEqual(calls[1], ['version', 'package-token', 'version-one']);
  page.handleBack();
  assert.equal(page.data.pageView, 'directory');
});

test('new package templates keep the workflow compact and avoid fake file-package claims', () => {
  const composer = fs.readFileSync(path.join(root, 'pages/projectpackage/projectpackage.wxml'), 'utf8');
  const invited = fs.readFileSync(path.join(root, 'pages/projectpackageview/projectpackageview.wxml'), 'utf8');
  assert.match(composer, /分享内容/);
  assert.match(composer, /预览分享/);
  assert.match(invited, /pageView === 'directory'/);
  assert.match(invited, /pageView === 'version'/);
  assert.doesNotMatch(composer + invited, /一键生成所有文件|自动选择全部成员|Word资料包/);
});

test('invited package clears protected content on hide, preserves same-account input, and revalidates on foreground', async () => {
  let reads = 0;
  const page = pageHarness('projectpackageview', {
    getSharedProjectPackage: async () => { reads += 1; return { project: { name: '张宅' }, items: [{ version_id: 'version-one' }] }; },
  });
  page.onLoad({ pps: 'package-token' });
  await page.onShow();
  page.setData({ pageView: 'version', versionView: { version: { version_id: 'version-one' } }, feedbackText: '尚未提交的修改意见', satisfaction: 'needs_adjustment', busy: true });
  page.onHide();
  assert.equal(page.data.directory, null);
  assert.equal(page.data.versionView, null);
  assert.equal(page.data.feedbackText, '尚未提交的修改意见');
  assert.equal(page.data.busy, false);
  await page.onShow();
  assert.equal(reads, 2);
  assert.equal(page.data.feedbackText, '尚未提交的修改意见');
});

test('package feedback retries one key and does not erase text typed while saving', async () => {
  const keys = [];
  let finish;
  const pending = new Promise(resolve => { finish = resolve; });
  const page = pageHarness('projectpackageview', {
    mutationKey: prefix => prefix + '-' + (keys.length + 1),
    addSharedProjectPackageFeedback: async (_token, _version, text, _rating, key) => {
      keys.push(key);
      if (keys.length === 1) throw new Error('response lost');
      await pending;
      return { feedback_id: 'feedback-one', text };
    },
  });
  page.onLoad({ pps: 'package-token' });
  page._epoch = 1; page._accountToken = 'session-token';
  page.setData({ versionView: { version: { version_id: 'version-one' }, feedbacks: [] }, feedbackText: '原意见', satisfaction: 'needs_adjustment' });
  await page.submitFeedback();
  const saving = page.submitFeedback();
  page.setFeedbackText({ detail: { value: '请求期间新输入' } });
  finish();
  await saving;
  assert.equal(keys[0], keys[1]);
  assert.equal(page.data.feedbackText, '请求期间新输入');
});

test('package receipts are independent per version and confirmation retry keeps one key', async () => {
  const receipts = [], confirmationKeys = [];
  const page = pageHarness('projectpackageview', {
    mutationKey: prefix => prefix + '-' + (confirmationKeys.length + 1),
    getSharedProjectPackageVersion: async (_token, version) => ({ version: { version_id: version, body_format: 'plain', body: '', media: [] }, feedbacks: [], can_confirm: true }),
    acknowledgeSharedProjectPackage: async (_token, version, event) => { receipts.push([version, event]); },
    confirmSharedProjectPackageVersion: async (_token, _version, key) => { confirmationKeys.push(key); if (confirmationKeys.length === 1) throw new Error('response lost'); return { confirmation_id: 'confirmation-one' }; },
  });
  page.onLoad({ pps: 'package-token' });
  page._epoch = 1; page._accountToken = 'session-token';
  await page.openVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  await page.acknowledgeRead();
  await page.confirmVersion();
  await page.confirmVersion();
  await page.openVersion({ currentTarget: { dataset: { id: 'version-two' } } });
  await page.acknowledgeRead();
  assert.notEqual(receipts[0][1], receipts[1][1]);
  assert.equal(confirmationKeys[0], confirmationKeys[1]);
});

test('package manager revalidates on foreground, preserves valid selection, drops old share, and exposes revoke history', async () => {
  let projectReads = 0, historyReads = 0, revoked = '';
  const detail = { project: { project_id: 'project-one', name: '张宅', confirmer_id: 102 }, members: [{ actor_id: 102 }], versions: [{ version_id: 'version-one' }] };
  const page = pageHarness('projectpackage', {
    getProjectPackageProject: async () => { projectReads += 1; return detail; },
    getProjectPackageHistory: async () => { historyReads += 1; return { deliveries: [{ delivery_id: 'delivery-one', revoked: false, recipient_ids: [102], items: [{ version_id: 'version-one' }] }], receipts: [] }; },
    revokeProjectPackageDelivery: async id => { revoked = id; },
  });
  page.onLoad({ project_id: 'project-one' });
  await page.onShow();
  page.toggleVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  page.setData({ step: 'shared', delivery: { delivery_id: 'old-delivery' }, sharePath: '/pages/projectpackageview/projectpackageview?pps=old-token' });
  page.onHide();
  assert.equal(page.data.sharePath, '');
  await page.onShow();
  assert.equal(projectReads, 2);
  assert.equal(historyReads, 2);
  assert.equal(page.data.selectedVersionIDs.join(','), 'version-one');
  assert.equal(page.data.step, 'select');
  await page.revokeDelivery({ currentTarget: { dataset: { id: 'delivery-one' } } });
  assert.equal(revoked, 'delivery-one');
  assert.equal(page.data.historyDeliveries[0].revoked, true);
  const template = fs.readFileSync(path.join(root, 'pages/projectpackage/projectpackage.wxml'), 'utf8');
  assert.match(template, /historyDeliveries/);
  assert.match(template, /revokeDelivery/);
});

test('foreground authorization failure removes cached invited and manager data', async () => {
  let invitedReads = 0;
  const invited = pageHarness('projectpackageview', {
    getSharedProjectPackage: async () => {
      invitedReads += 1;
      if (invitedReads > 1) throw Object.assign(new Error('access revoked'), { statusCode: 403 });
      return { project: { name: '张宅' }, items: [{ version_id: 'version-one' }] };
    },
  });
  invited.onLoad({ pps: 'package-token' });
  await invited.onShow();
  invited.onHide();
  await invited.onShow();
  assert.equal(invited.data.directory, null);
  assert.equal(invited.data.error, '该分享资料暂不可访问。');

  let managerReads = 0;
  const manager = pageHarness('projectpackage', {
    getProjectPackageProject: async () => {
      managerReads += 1;
      if (managerReads > 1) throw new Error('designer access revoked');
      return { project: { project_id: 'project-one', name: '张宅' }, members: [], versions: [{ version_id: 'version-one' }] };
    },
    getProjectPackageHistory: async () => ({ deliveries: [], receipts: [] }),
  });
  manager.onLoad({ project_id: 'project-one' });
  await manager.onShow();
  manager.toggleVersion({ currentTarget: { dataset: { id: 'version-one' } } });
  manager.onHide();
  await manager.onShow();
  assert.equal(manager.data.project, null);
  assert.equal(manager.data.selectedVersionIDs.length, 0);
  assert.equal(manager.data.error, 'designer access revoked');
});

test('history outage does not block an otherwise authorized package manager', async () => {
  const manager = pageHarness('projectpackage', {
    getProjectPackageProject: async () => ({ project: { project_id: 'project-one', name: '张宅' }, members: [], versions: [{ version_id: 'version-one' }] }),
    getProjectPackageHistory: async () => { throw new Error('history unavailable'); },
  });
  manager.onLoad({ project_id: 'project-one' });
  await manager.onShow();
  assert.equal(manager.data.project.project_id, 'project-one');
  assert.equal(manager.data.loading, false);
  assert.match(manager.data.notice, /分享记录/);
});
