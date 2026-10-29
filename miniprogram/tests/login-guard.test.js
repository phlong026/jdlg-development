const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
const root = path.resolve(__dirname, '..');
const complete = { id: 9, nickname: '林间访客', avatar: 'https://cdn.example/portrait.jpg' };
function load(extension, { token = '', loginOk = true, profile = complete, status = 200, defer = false, deferLogin = false, offline = false } = {}) {
  let definition;
  const storage = { token, userInfo: { id: 9 } }, toasts = [], events = [], requests = [], shows = [];
  const nicknameGuard = { data: { visible: false }, setData(v) { Object.assign(this.data, v); }, show(v) { shows.push(v); this.data.visible = true; } };
  const wx = {
    getStorageSync: key => storage[key], setStorageSync: (key, value) => { storage[key] = value; }, removeStorageSync: key => { delete storage[key]; },
    login: o => o.success({ code: 'CODE123' }),
    request(o) {
      requests.push(o);
      if (o.url.endsWith('/login') && deferLogin) return;
      if (o.url.endsWith('/login')) return o.success({ statusCode: 200, data: loginOk ? { code: 0, data: { token: 'NT', id: 9 } } : { code: 1, msg: 'bad code' } });
      if (defer) return;
      if (offline) return o.fail(new Error('offline'));
      o.success({ statusCode: status, data: { code: status === 200 ? 0 : status, data: profile } });
    },
    showToast: o => toasts.push(o.title), navigateTo() {},
  };
  const source = fs.readFileSync(path.join(root, `miniprogram/components/login-guard/login-guard.${extension}`), 'utf8');
  const code = extension === 'ts' ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText : source;
  vm.runInNewContext(code, { exports: {}, require: name => name.endsWith('profile-validation') ? require(path.join(root, 'miniprogram/components/nickname-guard/profile-validation.js')) : name.endsWith('deviceFingerprint')
    ? { generateDeviceFingerprint: async () => 'dev', getCachedDeviceFingerprint: () => 'dev', cacheDeviceFingerprint() {} }
    : { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) }, Component: d => { definition = d; }, wx, console: { error() {} } });
  const instance = { ...definition.methods, data: { ...definition.data }, setData(v) { Object.assign(this.data, v); }, selectComponent: () => nicknameGuard, triggerEvent(name, detail) { events.push({ name, detail }); } };
  return { instance, storage, toasts, events, requests, shows, nicknameGuard, detach: () => definition.lifetimes.detached.call(instance) };
}
for (const ext of ['ts', 'js']) {
  test(`${ext}: missing token remains blocked`, async () => {
    const f = load(ext); assert.equal(await f.instance.check(), false); assert.equal(f.instance.data.visible, true); assert.ok(f.events.some(e => e.name === 'gated')); assert.equal(f.requests.length, 0);
  });
  test(`${ext}: token alone does not grant access before authoritative profile read`, async () => {
    const f = load(ext, { token: 'tk', defer: true }); const pending = f.instance.check();
    assert.equal(f.instance.data.visible, true); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0); assert.equal(f.requests.length, 1);
    f.requests[0].success({ statusCode: 200, data: { code: 0, data: complete } });
    assert.equal(await pending, true); assert.equal(f.instance.data.visible, false);
    assert.equal(f.events.find(e => e.name === 'authenticated').detail.token, 'tk');
  });
  test(`${ext}: incomplete or placeholder avatar profile opens required editor with saved values`, async () => {
    for (const profile of [{ ...complete, nickname: '' }, { ...complete, avatar: '' }, { ...complete, avatar: '/assets/images/home.jpg' }, { ...complete, avatar: 'wxfile://temp' }]) {
      const f = load(ext, { token: 'tk', profile }); assert.equal(await f.instance.check(), false);
      assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0); assert.equal(f.shows.length, 1);
      assert.equal(f.shows[0].required, true); assert.equal(f.shows[0].nickname, profile.nickname); assert.equal(f.shows[0].userID, 9);
    }
  });
  test(`${ext}: network or business failure stays blocked and retry can verify`, async () => {
    for (const options of [{ offline: true }, { status: 500 }]) {
      const f = load(ext, { token: 'tk', ...options }); assert.equal(await f.instance.check(), false);
      assert.equal(f.instance.data.visible, true); assert.ok(f.instance.data.error); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0);
    }
  });
  test(`${ext}: retry verifies a fresh response and superseded same-token reads cannot override it`, async () => {
    const f = load(ext, { token: 'tk', defer: true });
    const failed = f.instance.check(); f.requests[0].fail(new Error('offline')); assert.equal(await failed, false);
    const stale = f.instance.check(); const fresh = f.instance.onRetry();
    f.requests[2].success({ statusCode: 200, data: { code: 0, data: complete } }); assert.equal(await fresh, true);
    f.requests[1].success({ statusCode: 500, data: { code: 500 } }); assert.equal(await stale, false);
    assert.equal(f.instance.data.visible, false); assert.equal(f.instance.data.error, '');
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 1);
  });
  test(`${ext}: expired token returns to login without granting`, async () => {
    const f = load(ext, { token: 'expired', status: 401 }); assert.equal(await f.instance.check(), false);
    assert.ok(!f.storage.token); assert.equal(f.instance.data.needsLogin, true); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0);
  });
  test(`${ext}: old response cannot grant, clear a new token or hide a newer gate`, async () => {
    for (const statusCode of [200, 401]) {
      const f = load(ext, { token: 'old', defer: true }); const pending = f.instance.check();
      f.storage.token = 'new'; f.storage.userInfo = { id: 10 };
      f.requests[0].success({ statusCode, data: { code: statusCode === 200 ? 0 : 401, data: complete } });
      assert.equal(await pending, false); assert.equal(f.storage.token, 'new'); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0);
    }
  });
  test(`${ext}: mismatched server account stays blocked`, async () => {
    const f = load(ext, { token: 'tk', profile: { ...complete, id: 10 } }); assert.equal(await f.instance.check(), false); assert.equal(f.storage.userInfo.id, 9);
  });
  test(`${ext}: wx login waits for complete server profile`, async () => {
    const f = load(ext, { profile: { ...complete, avatar: '' } }); await f.instance.check(); f.instance.onTogglePrivacy(); await f.instance.onWxLogin();
    assert.equal(f.storage.token, 'NT'); assert.equal(f.requests.length, 2); assert.equal(f.shows.length, 1); assert.equal(f.events.filter(e => e.name === 'authenticated' || e.name === 'loginsucceeded').length, 0);
  });
  test(`${ext}: profile completion event is reverified rather than trusted`, async () => {
    const f = load(ext, { token: 'tk', profile: { ...complete, avatar: '' } }); await f.instance.onProfileCompleted();
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0); assert.equal(f.requests.length, 1);
  });
  test(`${ext}: detached login dialog cannot install a late login result even if token is unchanged`, async () => {
    const f = load(ext, { deferLogin: true }); await f.instance.check(); f.instance.onTogglePrivacy();
    const pending = f.instance.onWxLogin(); await new Promise(setImmediate);
    assert.equal(f.requests.length, 1); f.detach();
    f.requests[0].success({ statusCode: 200, data: { code: 0, data: { token: 'late-token', id: 9 } } }); await pending;
    assert.equal(f.storage.token, ''); assert.equal(f.requests.length, 1);
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0);
  });
  test(`${ext}: privacy agreement remains necessary for wx login`, async () => {
    const f = load(ext); await f.instance.check(); await f.instance.onWxLogin(); assert.equal(f.requests.length, 0); assert.ok(f.toasts.some(t => /协议/.test(t)));
  });
  test(`${ext}: failed login cannot grant access or store token`, async () => {
    const f = load(ext, { loginOk: false }); await f.instance.check(); f.instance.onTogglePrivacy(); await f.instance.onWxLogin(); assert.ok(!f.storage.token); assert.equal(f.instance.data.visible, true);
  });
}

test('my 页：头像为官方 chooseAvatar 按钮且选完自动上传保存', () => {
  const wxml = fs.readFileSync(path.join(root, 'miniprogram/pages/my/my.wxml'), 'utf8');
  assert.match(wxml, /open-type="chooseAvatar"/);
  assert.match(wxml, /bind:chooseavatar="onChooseAvatar"/);
  for (const extension of ['ts', 'js']) {
    const source = fs.readFileSync(path.join(root, `miniprogram/pages/my/my.${extension}`), 'utf8');
    assert.match(source, /onChooseAvatar/, '头像选择处理存在');
    assert.match(source, /profile\/avatar/, '选中后保存到资料接口');
  }
});
