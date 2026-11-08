'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { execFileSync } = require('node:child_process');
const root = path.resolve(__dirname, '../miniprogram');
const publicRoutes = Object.entries(require('../miniprogram/utils/tracking/routes').ROUTES)
  .filter(([, policy]) => policy.collect).map(([route]) => route);
const app = require('../miniprogram/app.json');
const openRoutes = new Set(['pages/login/login', 'pages/privacy/privacy', 'pages/passwordrecovery/passwordrecovery']);
const businessRoutes = [
  ...(app.pages || []),
  ...(app.subPackages || []).flatMap(group => group.pages.map(page => `${group.root}/${page}`)),
].filter(route => !openRoutes.has(route));

test('every business route hides its content behind the shared profile guard', () => {
  assert.equal(businessRoutes.length, 69);
  for (const route of businessRoutes) {
    const template = fs.readFileSync(path.join(root, route + '.wxml'), 'utf8');
    const config = JSON.parse(fs.readFileSync(path.join(root, route + '.json'), 'utf8'));
    assert.equal(config.usingComponents && config.usingComponents['login-guard'], '/components/login-guard/login-guard', route);
    assert.match(template, /^(?:<import src="[^"\n]+"\s*\/>\s*)*<block wx:if="\{\{__profileAccessMounted\}\}">/, route);
    assert.match(template, /<login-guard id="profileAccessGuard" bind:authenticated="onProfileAccessGranted" bind:gated="onProfileAccessBlocked"\s*\/>\s*$/, route);
  }
});

test('every public route mounts the profile guard outside its protected content', () => {
  assert.equal(publicRoutes.length, 23);
  for (const route of publicRoutes) {
    const template = fs.readFileSync(path.join(root, route + '.wxml'), 'utf8');
    const config = JSON.parse(fs.readFileSync(path.join(root, route + '.json'), 'utf8'));
    assert.ok(config.usingComponents['login-guard'], route);
    assert.match(template, /^(?:<import src="[^"\n]+"\s*\/>\s*)*<block wx:if="\{\{__profileAccessMounted\}\}">/, route);
    assert.match(template, /<\/block>\s*<login-guard id="profileAccessGuard" bind:authenticated="onProfileAccessGranted" bind:gated="onProfileAccessBlocked"\s*\/>\s*$/, route);
  }
});

const compiler = process.env.WXML_COMPILER;
test('native WXML compiler renders only the login gate before profile approval on all public pages', {
  skip: !compiler || !fs.existsSync(compiler) ? 'Set WXML_COMPILER to the installed WeChat compiler' : false,
}, () => {
  const files = publicRoutes.map(route => 'miniprogram/' + route + '.wxml');
  const source = execFileSync(compiler, files, { cwd: path.dirname(root), encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
  const context = { window: {}, console };
  vm.createContext(context); vm.runInContext(source, context);
  for (const file of files) {
    const tree = context.$gwx(file)({ __profileAccessMounted: false });
    assert.equal(tree.children.length, 1, file);
    assert.equal(tree.children[0].attr.id, 'profileAccessGuard', file);
  }
});

// 默认用身份功能页测门禁行为；公开浏览页的乐观放行在下方独立用例覆盖。
function harness(route = 'pages/aitoolworkbench/aitoolworkbench') {
  const calls = [], storage = { token: '' }, navigations = [], requests = [], toasts = [];
  let checks = 0;
  const module = { exports: {} };
  const wx = {
    getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
    hideShareMenu() {},
    navigateTo: options => navigations.push(options),
    showToast: options => toasts.push(options.title),
    request: options => requests.push(options),
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/profile-access.js'), 'utf8'), {
    module, exports: module.exports, wx, console, Promise,
    require: name => name === './parameter'
      ? { generateRequestParams: token => ({ token }), paramsToHeaders: value => value }
      : require(path.join(root, 'utils', name)),
  });
  const definition = module.exports.wrapProfileAccess({
    data: { value: 'initial' },
    onLoad(options) { calls.push(['load', options]); },
    onShow() { calls.push(['show']); },
    onReady() { calls.push(['ready']); },
    onHide() { calls.push(['hide']); },
    onUnload() { calls.push(['unload']); },
    onShareAppMessage() { return { path: '/original' }; },
    onPullDownRefresh() { calls.push(['refresh']); },
  });
  const guard = { check: async () => { checks++; return false; } };
  const page = { ...definition, route, data: { ...definition.data },
    setData(value, done) { Object.assign(this.data, value); if (done) done.call(this); },
    selectComponent: () => guard,
  };
  return { page, calls, storage, navigations, requests, toasts, checks: () => checks,
    mount(options = { st: 'original-share' }) { page.onLoad(options); page.onShow(); page.onReady(); },
    grant(token = 'verified-account') {
      storage.token = token;
      page.onProfileAccessGranted({ detail: { token, profile: { id: 9, nickname: '客户', avatar: 'https://example.com/avatar.png' } } });
    },
  };
}

// 公开浏览页（微信审核整改）：匿名/资料不全用户先浏览，不锁页、不弹核验、
// 不跳登录；登录与补全由身份功能页触发。匿名浏览由 tracking 匿名池记录。
for (const route of ['pages/index/index', 'pages/templatesquaredetails/templatesquaredetails', 'pages/aicostshare/aicostshare']) {
  test(`${route}: anonymous users browse immediately without login gate (审核整改)`, () => {
    const h = harness(route); h.mount({ id: '17', report_id: 'report', st: 'shared' });
    assert.equal(h.checks(), 0, '公开页不触发资料核验');
    assert.equal(h.navigations.length, 0, '公开页不跳登录页');
    assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready'], '生命周期立即执行');
    assert.equal(h.calls[0][1].st, 'shared');
    assert.equal(h.page.data.__profileAccessReady, true);
    assert.equal(h.page.data.__profileAccessMounted, true);
    assert.equal(h.page.onShareAppMessage().path, '/original', '分享能力保留');
    h.page.onPullDownRefresh();
    assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready', 'refresh']);
  });
  test(`${route}: already-logged-in users with incomplete profiles also browse first`, () => {
    const h = harness(route); h.storage.token = 'logged-in-incomplete'; h.mount();
    assert.equal(h.checks(), 0, '已登录资料不全也不强制补全');
    assert.equal(h.navigations.length, 0);
    assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready']);
  });
}
// 身份功能页门禁行为保持不变（用户主动进入功能时才要求登录+资料完整）。
for (const route of ['pages/aicost/aicost']) {
  test(`${route}: no content/business/visit lifecycle before server profile approval`, () => {
    const h = harness(route); h.mount({ id: '17', report_id: 'report', st: 'shared' });
    assert.equal(h.checks(), 1);
    assert.deepEqual(h.calls, []);
    assert.equal(h.page.data.__profileAccessReady, false);
    assert.equal(Object.keys(h.page.onShareAppMessage()).length, 0);
    h.page.onPullDownRefresh(); assert.deepEqual(h.calls, []);
    h.grant();
    assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready']);
    assert.equal(h.calls[0][1].st, 'shared');
    assert.equal(h.page.data.__profileAccessReady, true);
    assert.equal(h.page.onShareAppMessage().path, '/original');
  });
}
test('ordinary business pages are protected even when analytics collection is disabled', () => {
  const h = harness('pages/wallet/wallet'); h.mount({ from: 'tab' });
  assert.equal(h.checks(), 1);
  assert.deepEqual(h.calls, []);
  h.grant();
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready']);
});
test('cached token is not profile approval; stale approval cannot unlock another account', () => {
  const h = harness(); h.storage.token = 'cached'; h.mount();
  assert.deepEqual(h.calls, []);
  h.storage.token = 'different';
  h.page.onProfileAccessGranted({ detail: { token: 'cached' } });
  assert.deepEqual(h.calls, []);
});
test('returning to an approved page keeps content mounted and verifies silently in the background', () => {
  const h = harness(); h.mount(); h.grant(); h.page.data.value = 'retained';
  h.page.onHide(); h.page.onShow();
  assert.equal(h.page.data.__profileAccessMounted, true, 'same-account return keeps previously approved content mounted');
  assert.equal(h.checks(), 2, 'a silent background verification still runs');
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready', 'hide'], 'lifecycle stays suspended until the background verify resolves');
  h.grant();
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready', 'hide', 'show']);
  assert.equal(h.page.data.value, 'retained');
});
test('a different account on return goes through the full gate again', () => {
  const h = harness(); h.mount(); h.grant('account-a');
  h.page.onHide();
  h.storage.token = 'account-b';
  h.page.onShow();
  assert.equal(h.page.data.__profileAccessMounted, false, 'account change relocks immediately');
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready', 'hide']);
  h.grant('account-b');
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready', 'hide', 'show']);
});
test('hidden/unloaded pages ignore late verification and never fetch content', () => {
  const h = harness(); h.mount(); h.page.onHide(); h.grant();
  assert.deepEqual(h.calls, []);
  h.page.onUnload(); h.page.onShow(); h.grant();
  assert.deepEqual(h.calls, []);
});
test('missing component without a login redirects to the reusable login flow', () => {
  const h = harness(); h.page.selectComponent = () => null; h.mount();
  assert.deepEqual(h.calls, []); assert.equal(h.page.data.__profileAccessReady, false);
  assert.equal(h.navigations.length, 1);
  assert.equal(h.navigations[0].url, '/pages/login/login?from=profile-gate');
});
test('missing component verifies a complete profile before running ordinary page code', () => {
  const h = harness('pages/wallet/wallet'); h.page.selectComponent = () => null;
  h.storage.token = 'verified'; h.storage.userInfo = { id: 99 };
  h.mount({ from: 'tab' });
  assert.deepEqual(h.calls, []); assert.equal(h.requests.length, 1);
  h.requests[0].success({ statusCode: 200, data: { code: 0, data: { id: 9, nickname: '客户', avatar: 'https://cdn.example/avatar.png' } } });
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready']);
  assert.equal(h.page.data.__profileAccessReady, true);
  assert.equal(h.storage.userInfo.id, 9, 'authority replaces a stale cache id for the current token');
});
test('missing component clears an unverifiable current account and returns to login', () => {
  const h = harness('pages/wallet/wallet'); h.page.selectComponent = () => null;
  h.storage.token = 'unverifiable'; h.storage.userInfo = { id: 99 }; h.mount();
  h.requests[0].success({ statusCode: 200, data: { code: 0, data: { id: 0, nickname: '客户', avatar: 'https://cdn.example/avatar.png' } } });
  assert.equal(h.storage.token, undefined); assert.equal(h.storage.userInfo, undefined);
  assert.equal(h.navigations.length, 1); assert.deepEqual(h.calls, []);
});
test('missing component stores an authoritative incomplete profile before opening completion', () => {
  const h = harness('pages/wallet/wallet'); h.page.selectComponent = () => null;
  h.storage.token = 'verified'; h.storage.userInfo = { id: 99, staleBusinessField: 'old-account' }; h.mount();
  h.requests[0].success({ statusCode: 200, data: { code: 0, data: { id: 9, nickname: '客户', avatar: '' } } });
  assert.equal(h.storage.userInfo.id, 9); assert.equal(h.storage.userInfo.nickname, '客户');
  assert.equal(h.storage.userInfo.staleBusinessField, undefined);
  assert.equal(h.navigations.length, 1); assert.deepEqual(h.calls, []);
});
test('warm entry waits for profile and uses latest original options', () => {
  const h = harness(); h.mount({ id: 'old', st: 'old-share' }); h.grant();
  h.page.onHide(); h.page.queueProfileEntry({ id: 'new', st: 'new-share' }); h.page.onShow();
  assert.equal(h.calls.filter(x => x[0] === 'load').length, 1);
  h.grant();
  assert.equal(h.calls.filter(x => x[0] === 'load').length, 2);
  assert.equal(h.calls.filter(x => x[0] === 'load')[1][1].st, 'new-share');
  assert.equal(h.calls.filter(x => x[0] === 'load')[1][1].id, 'new');
});
test('public browse pages reload a new share entry immediately without waiting for login (审核整改)', () => {
  const h = harness('pages/templatesquaredetails/templatesquaredetails'); h.mount({ id: 'old', st: 'old-share' });
  h.page.onHide(); h.page.queueProfileEntry({ id: 'new', st: 'new-share' }); h.page.onShow();
  assert.equal(h.navigations.length, 0, '不跳登录');
  assert.equal(h.calls.filter(x => x[0] === 'load').length, 2, '新入口立即乐观重载');
  assert.equal(h.calls.filter(x => x[0] === 'load')[1][1].st, 'new-share');
  assert.equal(h.calls.filter(x => x[0] === 'load')[1][1].id, 'new');
});
test('repeated foreground query never replays the same share as a fresh entry', () => {
  const h = harness(); h.mount({ st: 'same-share', id: '17' }); h.grant();
  h.page.onHide(); h.page.queueProfileEntry({ id: '17', st: 'same-share' }); h.page.onShow(); h.grant();
  assert.equal(h.calls.filter(x => x[0] === 'load').length, 1);
  assert.equal(h.calls.filter(x => x[0] === 'unload').length, 0);
});
test('app background suspends approval even before native page hide arrives', () => {
  const h = harness(); h.mount(); h.page.onProfileAccessAppHide(); h.grant();
  assert.deepEqual(h.calls, []);
  assert.equal(h.page.data.__profileAccessReady, false);
});
test('late render callbacks cannot replay lifecycle for a replaced warm entry', () => {
  const h = harness(); h.mount();
  const callbacks = [];
  h.page.setData = function(value, done) { Object.assign(this.data, value); if (done) callbacks.push(done); };
  h.grant(); h.page.queueProfileEntry({ st: 'new' }); h.grant();
  callbacks[0]();
  assert.equal(h.calls.filter(x => x[0] === 'show').length, 0);
  callbacks[1]();
  assert.equal(h.calls.filter(x => x[0] === 'show').length, 1);
});
test('token change during rendering relocks content and starts server revalidation', () => {
  const h = harness(); h.mount(); let done;
  h.page.setData = function(value, callback) { Object.assign(this.data, value); if (callback) done = callback; };
  h.grant(); h.storage.token = 'another-account'; done();
  assert.equal(h.page.data.__profileAccessReady, false);
  assert.equal(h.page.__accessActive, false);
  assert.equal(h.checks(), 2);
  assert.equal(h.calls.filter(x => x[0] === 'show').length, 0);
});
test('privacy/login pages remain accessible without recursively requiring login', () => {
  for (const route of ['pages/privacy/privacy', 'pages/login/login', 'pages/passwordrecovery/passwordrecovery']) {
    const h = harness(route); h.mount();
    assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready']);
    assert.equal(h.checks(), 0);
  }
});
