'use strict';
// T7.9：分享前成员提示 + 一键邀请（不自动建成员）
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');

function pageHarness(api) {
  let definition;
  const source = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.js'), 'utf8');
  const wx = {
    getStorageSync: key => key === 'token' ? 'session-token' : key === 'userInfo' ? { id: 101 } : '',
    getWindowInfo: () => ({ windowWidth: 375 }), hideShareMenu() {}, showShareMenu() {}, navigateTo() {}, showToast() {}, showModal() {}, setClipboardData() {}, previewImage() {},
  };
  const context = { Page(value) { definition = value; }, wx, require(id) {
    if (id === '../../utils/phase3-api') return api;
    if (id === '../../utils/article-document') return require(path.join(root, 'utils/article-document.js'));
    if (id === '../../utils/project-note-media') return require(path.join(root, 'utils/project-note-media.js'));
    if (id === '../../utils/ai-stream') return { startAIStream: () => ({ abort() {} }) };
    if (id === '../../utils/writing-policy') return require(path.join(root, 'utils/writing-policy.js'));
    if (id === './projectnotes-core') { vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes-core.js'), 'utf8'), context); return {}; }
    throw new Error('unexpected require ' + id);
  }, console, Promise, JSON, Error, Object, String, Number, Date, Map, Set, encodeURIComponent, decodeURIComponent, setTimeout, clearTimeout };
  vm.runInNewContext(source, context);
  return Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values, done) { Object.assign(this.data, values); if (done) done(); } }, definition);
}

const project = { project_id: 'p1', name: '张宅', revision: 1 };
const version = { version_id: 'v1', note_id: 'n1', revision: 1, title: '方案', body: '正文', body_format: 'plain', media_ids: [] };

test('the share panel warns when nobody is a member and offers a one-tap invite', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/projectnotes/projectnotes.wxml'), 'utf8');
  assert.match(wxml, /只有项目成员才能打开此链接，请先邀请对方成为项目成员/);
  assert.match(wxml, /一键邀请成员/);
  assert.match(wxml, /activeMembers\.length/);
  assert.match(wxml, /joinRequests\.length/);
  assert.match(wxml, /open-type="share" data-kind="project-invitation"/);
});

test('entering a fixed version loads pending join requests for the hint', async () => {
  let requested = 0;
  const page = pageHarness({
    listProjectJoinRequests: async () => { requested += 1; return { items: [{ request_id: 'r1', status: 'pending' }, { request_id: 'r2', status: 'approved' }] }; },
    listProjectInvitations: async () => ({ items: [] }),
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.applyProjectDetail({ project, notes: [], versions: [version] }, '', version, null, 'version');
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(requested, 1, '版本页应加载加入申请');
  assert.equal(page.data.joinRequests.length, 1, '只显示待审批申请');
});

test('creating an invitation yields a shareable path without creating members', async () => {
  const invitations = [];
  const page = pageHarness({
    mutationKey: () => 'key-1',
    createProjectInvitation: async () => { invitations.push('created'); return { invitation_id: 'i1', invitation_token: 'tok-1', expires_at: '2026-11-11T00:00:00Z' }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: project, activeMembers: [], members: [], memberInvitations: [] });
  await page.createProjectInvitation();
  assert.equal(invitations.length, 1);
  assert.match(page.data.memberInvitationPath, /^\/pages\/projectinvite\/projectinvite\?pni=/);
  assert.equal(page.data.activeMembers.length, 0, '不得自动创建成员');
});

test('creating a note share never silently creates members or invitations', async () => {
  const shareCalls = [], memberCalls = [];
  const page = pageHarness({
    mutationKey: () => 'key-1',
    createProjectNoteShare: async () => { shareCalls.push('share'); return { share_id: 's1', share_token: 'share-token-1' }; },
    createProjectInvitation: async () => { memberCalls.push('invite'); return { invitation_id: 'i1', invitation_token: 't' }; },
  });
  page._visible = true; page._token = 'session-token'; page._epoch = 1;
  page.setData({ selectedProject: project, frozenVersion: version, activeMembers: [], members: [] });
  await page.createProjectNoteShare();
  assert.equal(shareCalls.length, 1);
  assert.equal(memberCalls.length, 0, '分享不得暗中创建邀请');
  assert.equal(page.data.activeMembers.length, 0, '分享不得自动建成员');
});
