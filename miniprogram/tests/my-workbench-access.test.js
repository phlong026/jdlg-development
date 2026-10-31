'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const pageRoot = path.join(__dirname, '../miniprogram/pages/my');

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function loadPage({
  token = 'account-a',
  context = async () => ({ can_manage: true, owners: ['designer-7'] }),
  shareContext = async () => { throw Object.assign(new Error('forbidden'), { statusCode: 403 }); },
} = {}) {
  let definition;
  let storedToken = token;
  let storedUserInfo = { username: '测试用户' };
  const navigations = [];
  const toasts = [];
  let actionSheet;
  const source = fs.readFileSync(path.join(pageRoot, 'my.js'), 'utf8');
  vm.runInNewContext(source, {
    exports: {},
    Page(value) { definition = value; },
    require(id) {
      if (id.endsWith('/phase3-api')) return { workbenchContext: context, mySharesContext: shareContext };
      if (id.endsWith('/asset')) return { resolveAssetPath: value => value };
      if (id.endsWith('/parameter')) return { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) };
      if (id.endsWith('/deviceFingerprint')) return { generateDeviceFingerprint: async () => '', getCachedDeviceFingerprint: () => '', cacheDeviceFingerprint() {} };
      if (id.endsWith('/tracking/materials')) return { data: {}, methods: { refreshShareMaterials() {}, resetShareMaterials() {} } };
      throw new Error(`unexpected require: ${id}`);
    },
    wx: {
      getStorageSync(key) { return key === 'token' ? storedToken : key === 'userInfo' ? storedUserInfo : ''; },
      navigateTo(options) { navigations.push(options.url); },
      showToast(options) { toasts.push(options.title); },
      showActionSheet(options) { actionSheet = options; },
    },
    console, Promise, Date, Object, String, Number, Boolean, Array, setTimeout,
  });
  const page = Object.assign({
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) { Object.assign(this.data, patch); },
  }, definition);
  page.data.token = token;
  page.data.hasLoginToken = !!token;
  return {
    page, navigations, toasts, actionSheet: () => actionSheet,
    setToken(value) { storedToken = value; page.data.token = value; },
    switchStoredAccount(value, userInfo = { username: '新账号' }) { storedToken = value; storedUserInfo = userInfo; },
  };
}

test('只有后端工作台能力允许时才显示并打开内容创作入口', async () => {
  const allowed = loadPage();
  await allowed.page.loadWorkbenchAccess();
  assert.equal(allowed.page.data.hasContentWorkbenchAccess, true);
  assert.equal(allowed.page.data.showWorkbenchSection, true);
  allowed.page.onWorkbenchMenuTap({ currentTarget: { dataset: { action: 'contentWorkbench' } } });
  assert.deepEqual(allowed.navigations, ['/pages/contentworkbench/contentworkbench']);

  const owner = loadPage({ context: async () => { throw Object.assign(new Error('forbidden'), { statusCode: 403 }); } });
  await owner.page.loadWorkbenchAccess();
  assert.equal(owner.page.data.showWorkbenchSection, false);
  owner.page.onWorkbenchMenuTap({ currentTarget: { dataset: { action: 'contentWorkbench' } } });
  assert.deepEqual(owner.navigations, []);
  assert.equal(owner.toasts.at(-1), '当前账号无内容创作权限');

  const disabled = loadPage({ context: async () => ({ enabled: false, can_manage: true, owners: ['designer-7'] }) });
  await disabled.page.loadWorkbenchAccess();
  assert.equal(disabled.page.data.showWorkbenchSection, false);
});

test('收起的旧作品发布入口仍可从更多打开并复核权限', async () => {
  const harness = loadPage();
  harness.page.setData({ certStatus: 'approved', certType: 'designer', certIdentityType: '设计师' });
  await harness.page.loadWorkbenchAccess();
  harness.page.onWorkbenchMore();
  assert.deepEqual(Array.from(harness.actionSheet().itemList), ['作品发布', '可分享素材']);
  harness.actionSheet().success({ tapIndex: 0 });
  assert.deepEqual(harness.navigations, ['/pages/release/release']);
  harness.switchStoredAccount('account-b');
  harness.actionSheet().success({ tapIndex: 0 });
  assert.equal(harness.navigations.length, 1);
});

test('认证资料只决定设计师标题和主页，不替代后端创作授权', async () => {
  const user = loadPage({ context: async () => ({ can_manage: false, owners: [] }) });
  user.page.setData({ certStatus: 'approved', certType: 'designer', certIdentityType: '设计师' });
  user.page.refreshWorkbenchMenuItems();
  assert.equal(user.page.data.isApprovedDesigner, true);
  assert.equal(user.page.data.showWorkbenchSection, false);

  const explicitlyGranted = loadPage();
  explicitlyGranted.page.setData({ certStatus: 'approved', certType: 'enterprise', certIdentityType: '企业主' });
  await explicitlyGranted.page.loadWorkbenchAccess();
  assert.equal(explicitlyGranted.page.data.workbenchSectionTitle, '内容创作工作台');
  assert.equal(explicitlyGranted.page.data.workbenchMenuItems.some(item => item.action === 'designerHome'), false);
});

test('账号切换后旧请求不能把前一个账号的工作台重新显示', async () => {
  const first = deferred();
  let callCount = 0;
  const harness = loadPage({
    context: () => {
      callCount += 1;
      if (callCount === 1) return first.promise;
      throw Object.assign(new Error('forbidden'), { statusCode: 403 });
    },
  });
  const pending = harness.page.loadWorkbenchAccess();
  harness.setToken('account-b');
  const second = harness.page.loadWorkbenchAccess();
  first.resolve({ can_manage: true, owners: ['designer-old'] });
  await pending;
  assert.equal(harness.page.data.showWorkbenchSection, false);
  await second;
  assert.equal(harness.page.data.showWorkbenchSection, false);
});

test('分享访问使用独立 can_view，不能从内容管理权限推断', async () => {
  const creator = loadPage();
  await Promise.all([creator.page.loadWorkbenchAccess(), creator.page.loadShareVisitAccess()]);
  assert.equal(creator.page.data.showWorkbenchSection, true);
  assert.equal(creator.page.data.hasShareVisitAccess, false);
  creator.page.onWorkbenchMenuTap({ currentTarget: { dataset: { action: 'shareVisits' } } });
  assert.deepEqual(creator.navigations, []);
  assert.equal(creator.toasts.at(-1), '当前账号无分享访问权限');

  const viewer = loadPage({
    context: async () => { throw Object.assign(new Error('forbidden'), { statusCode: 403 }); },
    shareContext: async () => ({ can_view: true, owners: ['designer-8'] }),
  });
  await Promise.all([viewer.page.loadWorkbenchAccess(), viewer.page.loadShareVisitAccess()]);
  assert.equal(viewer.page.data.showWorkbenchSection, false);
  assert.equal(viewer.page.data.commonMenuItems.some(item => item.action === 'shareVisits'), true);
  viewer.page.onProjectMenuTap({ currentTarget: { dataset: { action: 'shareVisits' } } });
  assert.deepEqual(viewer.navigations, ['/pages/sharevisits/sharevisits']);
});

test('分享权限旧响应不能跨账号回显', async () => {
  const first = deferred();
  let callCount = 0;
  const harness = loadPage({
    shareContext: () => {
      callCount += 1;
      if (callCount === 1) return first.promise;
      throw Object.assign(new Error('forbidden'), { statusCode: 403 });
    },
  });
  const pending = harness.page.loadShareVisitAccess();
  harness.setToken('account-b');
  const second = harness.page.loadShareVisitAccess();
  first.resolve({ can_view: true, owners: ['designer-old'] });
  await Promise.all([pending, second]);
  assert.equal(harness.page.data.hasShareVisitAccess, false);
  assert.equal(harness.page.data.commonMenuItems.some(item => item.action === 'shareVisits'), false);
});

test('切换账号同步清空旧设计师认证，等待新账号认证结果', () => {
  const harness = loadPage();
  harness.page.setData({
    certStatus: 'approved', certType: 'designer', certIdentityType: '设计师',
    userProfile: { nickname: '旧账号', avatar: '', identityType: '设计师' },
    lastProfileRefreshToken: 'account-b', lastProfileRefreshTime: Date.now(),
  });
  harness.switchStoredAccount('account-b');
  harness.page.loadUserData();
  assert.equal(harness.page.data.certStatus, '');
  assert.equal(harness.page.data.certType, '');
  assert.equal(harness.page.data.certIdentityType, '');
  assert.equal(harness.page.data.userProfile.identityType, '');
  assert.equal(harness.page.data.isApprovedDesigner, false);
});

test('工作台区块按权限隐藏，普通生成记录与独立客户管理保留在其他分区', () => {
  const wxml = fs.readFileSync(path.join(pageRoot, 'my.wxml'), 'utf8');
  const source = fs.readFileSync(path.join(pageRoot, 'my.ts'), 'utf8');
  assert.match(wxml, /wx:if="\{\{showWorkbenchSection\}\}"/);
  assert.match(wxml, /\{\{workbenchSectionTitle\}\}/);
  assert.match(wxml, /wx:if="\{\{hasShareVisitAccess\}\}"/);
  assert.match(source, /buildCommonMenuItems[\s\S]+action: 'project'/);
  assert.match(source, /buildServiceMenuItems[\s\S]+action: 'customerManagement'/);
  assert.doesNotMatch(source, /action: 'contentWorkbench'[^\n]+available: hasLoginToken/);
});


test('认证设计师工作台只保留任务入口，收起能力仍从内容页继续可达', async () => {
  const designer = loadPage();
  designer.page.setData({ certStatus: 'approved', certType: 'designer', certIdentityType: '设计师' });
  await designer.page.loadWorkbenchAccess();
  assert.deepEqual(Array.from(designer.page.data.workbenchMenuItems, item => item.action), [
    'myProjects', 'contentWorkbench', 'designerHome',
  ]);
  designer.page.onWorkbenchMenuTap({ currentTarget: { dataset: { action: 'contentWorkbench' } } });
  assert.deepEqual(designer.navigations, ['/pages/contentworkbench/contentworkbench']);
  const owner = loadPage({ context: async () => ({ can_manage: false, owners: [] }) });
  await owner.page.loadWorkbenchAccess();
  owner.page.onWorkbenchMenuTap({ currentTarget: { dataset: { action: 'contentWorkbench' } } });
  assert.deepEqual(owner.navigations, []);
  assert.equal(owner.toasts.length, 1);
});
