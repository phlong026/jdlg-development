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
    data: { value: 'initial', view: null, media: [] },
    onLoad(options) { calls.push(['load', options]); },
    onShow() { calls.push(['show']); if (!storage.token) this.setData({ view: null, media: [] }); },
    onReady() { calls.push(['ready']); },
    onHide() { calls.push(['hide']); },
    onUnload() { calls.push(['unload']); },
    onShareAppMessage() { return { path: '/original' }; },
    onPullDownRefresh() { calls.push(['refresh']); },
  });
  const guard = { visible: false, options: null, setData(value) { Object.assign(this, value); }, check: async options => { checks++; guard.options = options || null; return false; } };
  const page = { ...definition, route, data: { ...definition.data }, _pendingCallbacks: [], deferCallbacks: false,
    setData(value, done) {
      Object.assign(this.data, value);
      if (!done) return;
      if (this.deferCallbacks) this._pendingCallbacks.push(done.bind(this)); else done.call(this);
    },
    selectComponent: () => guard,
  };
  return { page, calls, storage, navigations, requests, toasts, guard, checks: () => checks,
    flush() { while (page._pendingCallbacks.length) page._pendingCallbacks.shift()(); },
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

// T7.6 匿名说明态：未登录打开分享链接不锁页、不跳登录、不弹浮层；登录后回到正常门禁。
test('the project-note share page mounts an anonymous explanation state instead of locking the page', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  assert.equal(h.page.data.__profileAccessMounted, true, '未登录只挂壳');
  assert.equal(h.checks(), 0, '匿名说明态不触发资料核验');
  assert.equal(h.navigations.length, 0, '不跳登录页');
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show'], '页面生命周期仍执行以显示说明态');
  assert.equal(h.calls[0][1].pns, 'opaque-share-token', '入口参数已初始化');
});

test('logging in from the anonymous share state re-runs the normal gate and reveals content', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'session-account';
  h.page.onShow();
  assert.equal(h.checks(), 1, '登录后回到正常门禁核验');
  assert.equal(h.page.data.__profileAccessReady, false, '核验前不放行受保护内容');
  h.grant('session-account');
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'show', 'ready']);
  assert.equal(h.page.data.__profileAccessReady, true);
});

test('cancelling login returns to the explanation state without a redirect loop', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'session-account';
  h.page.onShow();
  delete h.storage.token;
  h.page.onShow();
  assert.equal(h.navigations.length, 0, '取消登录后不得跳登录页');
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'show'], '仍可再次进入说明态');
  assert.equal(h.page.data.__profileAccessMounted, true);
});

test('a new share entry keeps the anonymous explanation state', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'first-token' });
  h.page.queueProfileEntry({ pns: 'second-token' });
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'load'], '新入口重新初始化但不锁页');
  assert.equal(h.calls[2][1].pns, 'second-token');
  assert.equal(h.page.data.__profileAccessMounted, true);
  assert.equal(h.checks(), 0, '新入口仍不触发核验');
});

test('the anonymous share page is not locked when the app goes to background', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.page.onProfileAccessAppHide();
  assert.equal(h.page.data.__profileAccessMounted, true, '后台返回不出现锁页闪烁');
});

test('the anonymous explanation copy and login entry are present on the share page', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.wxml'), 'utf8');
  assert.match(wxml, /设计师分享给你的私密方案/);
  assert.match(wxml, /登录后即可查看/);
  assert.match(wxml, /bindtap="goLogin"/);
});

// T7.6：token 失效后必须真正回到匿名态（关浮层、不跳登录、不锁页）。
test('an expired token on the share page falls back to the anonymous state with the overlay hidden', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  assert.equal(h.page.data.view, null, '未登录不得渲染受保护内容');
  assert.equal(h.page.data.media.length, 0);

  h.storage.token = 'expired-session';
  h.page.onShow();
  assert.equal(h.page.data.__profileAccessMounted, false, '已登录重验期间按原门禁上锁');
  delete h.storage.token;
  h.page.onProfileAccessBlocked();
  assert.equal(h.page.data.__profileAccessMounted, true, 'token 失效后回到说明态');
  assert.equal(h.page.data.__profileAccessReady, false, '受保护内容仍为空');
  assert.equal(h.guard.visible, false, '旧登录浮层必须关闭');
  assert.equal(h.navigations.length, 0, '不得跳登录页');

  h.page.onShow();
  assert.equal(h.navigations.length, 0, '后续 onShow 仍停留在说明态');
  assert.equal(h.guard.visible, false);
});

test('a logged-in share page rejects optimistic grants from outside the anonymous state', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'session-account';
  const before = h.calls.length;
  h.page.onProfileAccessGranted({ detail: { token: 'session-account', profile: { id: 9 }, optimistic: true } });
  assert.equal(h.page.data.__profileAccessReady, false, '已登录页面不得被乐观放行');
  assert.equal(h.calls.length, before, '未核验不放行页面生命周期');
});

// T7.6：组件 gated 前先写浮层状态，匿名降级必须赢过这次写入；渲染回调丢 token 不白屏。
test('the anonymous downgrade wins over the overlay write that precedes gated', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'expired-session';
  h.page.onShow();
  h.guard.setData({ visible: true, checking: false, needsLogin: true });
  delete h.storage.token;
  h.page.onProfileAccessBlocked();
  assert.equal(h.page.data.__profileAccessMounted, true);
  assert.equal(h.guard.visible, false, '匿名降级后浮层必须保持关闭');
});

test('the login guard writes its overlay state before emitting gated', () => {
  for (const file of ['login-guard.js', 'login-guard.ts']) {
    const code = fs.readFileSync(path.join(root, 'components/login-guard', file), 'utf8');
    for (const marker of ['登录已过期，请重新登录', '账号资料无法确认，请重新登录']) {
      const visibleAt = code.indexOf("visible: true, checking: false, needsLogin: true, error: '" + marker + "'");
      assert.ok(visibleAt > 0, file + ' 缺少浮层写入: ' + marker);
      const gatedAt = code.indexOf("triggerEvent('gated')", visibleAt);
      assert.ok(gatedAt > visibleAt, file + ' 必须先写浮层状态再抛 gated: ' + marker);
    }
  }
});

test('losing the token during the granted render falls back to the anonymous state instead of a white screen', () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'session-account';
  h.page.onShow();
  h.page.deferCallbacks = true;
  h.page.onProfileAccessGranted({ detail: { token: 'session-account', profile: { id: 9 } } });
  delete h.storage.token;
  h.flush();
  assert.equal(h.page.data.__profileAccessMounted, true, '渲染竞态下必须挂壳而不是白屏');
  assert.equal(h.page.data.__profileAccessReady, false, '受保护内容不得放行');
  assert.equal(h.guard.visible, false);
});

// T7.6：真实 login-guard 组件的失效分支联动（401、无 token、无效账号）。
function realGuard(h, response) {
  let definition;
  const storage = h.storage;
  const wxStub = {
    getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
    request: options => { queueMicrotask(() => options.success(response)); },
    showToast() {},
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'components/login-guard/login-guard.js'), 'utf8'), {
    Component: value => { definition = value; }, wx: wxStub, console, Promise, setTimeout, clearTimeout,
    require: name => name.includes('deviceFingerprint') ? { getCachedDeviceFingerprint: () => '' }
      : name.includes('parameter') ? { generateRequestParams: () => ({}), paramsToHeaders: value => value }
        : require(path.join(root, 'components/nickname-guard/profile-validation')),
    exports: {}, module: { exports: {} },
  });
  const guard = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)) }, definition.methods);
  guard.setData = function (value, done) { Object.assign(this.data, value); if (done) done.call(this); };
  guard.triggerEvent = name => { if (name === 'gated') h.page.onProfileAccessBlocked(); };
  guard.selectComponent = () => null;
  return guard;
}
const flushAsync = () => new Promise(resolve => setTimeout(resolve, 0));

test('the real login guard 401 branch degrades the share page to the anonymous state with the overlay hidden', async () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'expired-session';
  const guard = realGuard(h, { statusCode: 401, data: { code: 401 } });
  h.page.selectComponent = () => guard;
  h.page.onShow();
  await flushAsync();
  assert.equal(h.page.data.__profileAccessMounted, true);
  assert.equal(h.page.data.__profileAccessReady, false);
  assert.equal(guard.data.visible, false, '401 后匿名降级必须关掉浮层');
  assert.equal(h.navigations.length, 0, '不跳登录页');
});

test('the real login guard invalid-account branch also degrades to the anonymous state', async () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'odd-account';
  const guard = realGuard(h, { statusCode: 200, data: { code: 0, data: {} } });
  h.page.selectComponent = () => guard;
  h.page.onShow();
  await flushAsync();
  assert.equal(h.page.data.__profileAccessMounted, true);
  assert.equal(guard.data.visible, false, '无效账号后匿名降级必须关掉浮层');
});

test('the real login guard hides its overlay only when the anonymous option is passed without a token', async () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  const guard = realGuard(h, { statusCode: 200, data: { code: 0, data: {} } });
  await guard.check({ anonymous: true });
  assert.equal(guard.data.visible, false, '匿名态不得弹登录浮层');
  // 非分享页无 token 时仍弹浮层（分享页的 gated 一律匿名降级）
  const business = harness('pages/aicost/aicost'); business.mount();
  const businessGuard = realGuard(business, { statusCode: 200, data: { code: 0, data: {} } });
  await businessGuard.check({});
  assert.equal(businessGuard.data.visible, true, '普通无 token 页面仍弹浮层');
});

test('a late 401 while backgrounded keeps the page hidden and only clears content on the next onShow', async () => {
  const route = 'pages/projectnoteview/projectnoteview';
  const h = harness(route); h.mount({ pns: 'opaque-share-token' });
  h.storage.token = 'session-account';
  h.page.onShow();
  h.grant('session-account');
  h.page.data.view = { project: { name: '私密方案' } };
  h.page.data.media = [{ media_id: 'm-1' }];
  h.page.onHide();
  const shows = h.calls.filter(call => call[0] === 'show').length;
  delete h.storage.token;
  h.page.onProfileAccessBlocked();
  assert.equal(h.calls.filter(call => call[0] === 'show').length, shows, '后台迟到 401 不得补跑 onShow');
  assert.equal(h.page.data.__profileAccessReady, false);
  h.page.onShow();
  assert.equal(h.page.data.__profileAccessMounted, true, '回到前台后恢复说明页');
  assert.equal(h.page.data.view, null, '受保护内容必须清空');
  assert.equal(h.page.data.media.length, 0);
});

// T7.7：分享查看页核验时带 scope=share，身份功能页保持原口径。
test('the share page asks the guard to verify with the share scope', () => {
  const share = harness('pages/projectnoteview/projectnoteview'); share.mount({ pns: 'opaque-share-token' });
  share.storage.token = 'session-account';
  share.page.onShow();
  assert.equal(share.guard.options && share.guard.options.scope, 'share');
  const business = harness('pages/aicost/aicost'); business.mount({});
  business.storage.token = 'session-account';
  business.page.onShow();
  assert.notEqual(business.guard.options && business.guard.options.scope, 'share', '身份页不得使用分享场景');
});

// T7.7：无组件兜底跳登录时，分享页带 share-view 场景，其它页面保持 profile-gate。
test('the fallback login redirect keeps the share-view scene for the share page only', () => {
  const share = harness('pages/projectnoteview/projectnoteview');
  share.page.selectComponent = () => null;
  share.storage.token = 'session-account';
  share.page.onShow(); share.page.onReady();
  assert.equal(share.requests.length, 1, '无组件路径应发起资料核验请求');
  share.requests[0].success({ statusCode: 200, data: { code: 0, data: { id: 9, nickname: '', avatar: '' } } });
  assert.match(share.navigations[0].url, /from=share-view/);

  const business = harness('pages/aicost/aicost');
  business.page.selectComponent = () => null;
  business.storage.token = 'session-account';
  business.page.onShow(); business.page.onReady();
  assert.equal(business.requests.length, 1);
  business.requests[0].success({ statusCode: 200, data: { code: 0, data: { id: 9, nickname: '', avatar: '' } } });
  assert.match(business.navigations[0].url, /from=profile-gate/);
});
