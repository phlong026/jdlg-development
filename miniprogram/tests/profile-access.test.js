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

test('every public route mounts the profile guard outside its protected content', () => {
  assert.equal(publicRoutes.length, 22);
  for (const route of publicRoutes) {
    const template = fs.readFileSync(path.join(root, route + '.wxml'), 'utf8');
    const config = JSON.parse(fs.readFileSync(path.join(root, route + '.json'), 'utf8'));
    assert.ok(config.usingComponents['login-guard'], route);
    assert.match(template, /^<block wx:if="\{\{__profileAccessMounted\}\}">/, route);
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

function harness(route = 'pages/index/index') {
  const calls = [], storage = { token: '' };
  let checks = 0;
  const module = { exports: {} };
  const wx = { getStorageSync: key => storage[key], hideShareMenu() {} };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/profile-access.js'), 'utf8'), {
    module, exports: module.exports, wx, console, Promise,
    require: name => require(path.join(root, 'utils', name)),
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
  return { page, calls, storage, checks: () => checks,
    mount(options = { st: 'original-share' }) { page.onLoad(options); page.onShow(); page.onReady(); },
    grant(token = 'verified-account') {
      storage.token = token;
      page.onProfileAccessGranted({ detail: { token, profile: { id: 9, nickname: '客户', avatar: 'https://example.com/avatar.png' } } });
    },
  };
}

for (const route of ['pages/index/index', 'pages/templatesquaredetails/templatesquaredetails', 'pages/aicost/aicost', 'pages/aicostshare/aicostshare']) {
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
test('cached token is not profile approval; stale approval cannot unlock another account', () => {
  const h = harness(); h.storage.token = 'cached'; h.mount();
  assert.deepEqual(h.calls, []);
  h.storage.token = 'different';
  h.page.onProfileAccessGranted({ detail: { token: 'cached' } });
  assert.deepEqual(h.calls, []);
});
test('every return verifies again, preserves business state and resumes once', () => {
  const h = harness(); h.mount(); h.grant(); h.page.data.value = 'retained';
  h.page.onHide(); h.page.onShow();
  assert.equal(h.page.data.__profileAccessReady, false);
  assert.equal(h.page.data.__profileAccessMounted, true, 'revalidation keeps mounted content and its observers');
  assert.equal(h.checks(), 2);
  h.grant(); h.grant();
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready', 'hide', 'show']);
  assert.equal(h.page.data.value, 'retained');
});
test('hidden/unloaded pages ignore late verification and never fetch content', () => {
  const h = harness(); h.mount(); h.page.onHide(); h.grant();
  assert.deepEqual(h.calls, []);
  h.page.onUnload(); h.page.onShow(); h.grant();
  assert.deepEqual(h.calls, []);
});
test('missing component fails closed instead of test-only bypass', () => {
  const h = harness(); h.page.selectComponent = () => null; h.mount();
  assert.deepEqual(h.calls, []); assert.equal(h.page.data.__profileAccessReady, false);
});
test('warm entry waits for profile and uses latest original options', () => {
  const h = harness('pages/templatesquaredetails/templatesquaredetails'); h.mount({ id: 'old', st: 'old-share' }); h.grant();
  h.page.onHide(); h.page.queueProfileEntry({ id: 'new', st: 'new-share' }); h.page.onShow();
  assert.equal(h.calls.filter(x => x[0] === 'load').length, 1);
  h.grant();
  assert.equal(h.calls.filter(x => x[0] === 'load').length, 2);
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
  const h = harness('pages/privacy/privacy'); h.mount();
  assert.deepEqual(h.calls.map(x => x[0]), ['load', 'show', 'ready']);
  assert.equal(h.checks(), 0);
});
