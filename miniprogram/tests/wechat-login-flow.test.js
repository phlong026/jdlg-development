const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');

function loadLoginPage(extension, {
  loginResult = { id: 101, username: 'wx_login_user', token: 'login-token' },
  profileNickname = '山居用户',
  loginError = '',
} = {}) {
  let definition;
  const storage = {};
  const requests = [];
  const toasts = [];
  const navigation = [];
  let guardShows = 0;

  const source = fs.readFileSync(path.join(root, `miniprogram/pages/login/login.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;

  const wx = {
    getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
    login: options => options.success({ code: 'wx-login-code' }),
    request(options) {
      requests.push(options);
      if (options.url.endsWith('/api/v1/miniprogram/login')) {
        if (loginError) {
          options.success({ statusCode: 500, data: { code: 500, msg: loginError } });
        } else {
          options.success({ statusCode: 200, data: { code: 0, data: loginResult } });
        }
        return;
      }
      if (options.url.endsWith('/api/v1/miniprogram/profile')) {
        options.success({ statusCode: 200, data: { code: 0, data: { nickname: profileNickname } } });
        return;
      }
      throw new Error(`unexpected request: ${options.url}`);
    },
    showToast: options => toasts.push(options.title),
    switchTab: options => navigation.push({ method: 'switchTab', url: options.url }),
    reLaunch: options => navigation.push({ method: 'reLaunch', url: options.url }),
  };

  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('deviceFingerprint')
      ? {
          getCachedDeviceFingerprint: () => 'device-fixture',
          generateDeviceFingerprint: async () => 'generated-device',
          cacheDeviceFingerprint() {},
        }
      : name.endsWith('parameter')
        ? { generateRequestParams: () => ({}), paramsToHeaders: () => ({ token: 'login-token' }) }
        : { resolveAssetPath: value => value },
    Page: value => { definition = value; },
    wx,
    getApp: () => ({}),
    setTimeout: callback => { callback(); return 1; },
    clearTimeout() {},
    setInterval: callback => { callback(); return 1; },
    clearInterval() {},
    console: { error() {}, warn() {}, log() {} },
    Date,
    Promise,
  });

  const page = {
    ...definition,
    data: { ...definition.data, agreedPrivacy: true },
    setData(value) { Object.assign(this.data, value); },
    selectComponent(selector) {
      assert.equal(selector, '#nicknameGuard');
      return { show() { guardShows += 1; } };
    },
  };

  return {
    page,
    storage,
    requests,
    toasts,
    navigation,
    guardShows: () => guardShows,
  };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 微信 code 登录缓存账号，已有昵称后进入首页`, async () => {
    const fixture = loadLoginPage(extension);
    assert.equal(fixture.page.data.username, '');
    assert.equal(fixture.page.data.password, '');

    await fixture.page.handleWechatLogin();

    const loginRequest = fixture.requests[0];
    assert.equal(loginRequest.method, 'POST');
    assert.ok(loginRequest.url.endsWith('/api/v1/miniprogram/login'));
    assert.equal(loginRequest.data.code, 'wx-login-code');
    assert.equal(loginRequest.data.device_id, 'device-fixture');
    assert.equal(Object.hasOwn(loginRequest.data, 'username'), false);
    assert.equal(Object.hasOwn(loginRequest.data, 'password'), false);
    assert.equal(fixture.storage.token, 'login-token');
    assert.equal(fixture.storage.userInfo.id, 101);
    assert.equal(fixture.storage.userInfo.username, 'wx_login_user');
    assert.equal(fixture.storage.userInfo.token, 'login-token');
    assert.equal(fixture.requests[1].method, 'GET');
    assert.ok(fixture.requests[1].url.endsWith('/api/v1/miniprogram/profile'));
    assert.equal(fixture.guardShows(), 0);
    assert.deepEqual(fixture.navigation, [{ method: 'switchTab', url: '/pages/index/index' }]);
  });

  test(`${extension}: 老用户缺昵称时显示昵称弹窗并停止跳转`, async () => {
    const fixture = loadLoginPage(extension, { profileNickname: '   ' });
    await fixture.page.handleWechatLogin();

    assert.equal(fixture.storage.token, 'login-token');
    assert.equal(fixture.guardShows(), 1);
    assert.equal(fixture.navigation.length, 0);
    assert.equal(fixture.page.data.identityPopupVisible, false);
    assert.equal(fixture.page.data.loading, false);
  });

  test(`${extension}: 新用户登录后停在现有身份选择弹窗`, async () => {
    const loginResult = { id: 202, username: 'wx_new_user', token: 'new-token', is_new_user: true };
    const fixture = loadLoginPage(extension, { loginResult, profileNickname: '' });
    await fixture.page.handleWechatLogin();

    assert.equal(fixture.storage.token, 'new-token');
    assert.equal(fixture.storage.userInfo.id, 202);
    assert.equal(fixture.page.data.identityPopupVisible, true);
    assert.equal(fixture.page.data.pendingLoginResult.id, 202);
    assert.equal(fixture.requests.length, 1, '身份选择前不应提前查询昵称');
    assert.equal(fixture.guardShows(), 0);
    assert.equal(fixture.navigation.length, 0);
  });

  test(`${extension}: 后端拒绝微信登录时不缓存也不放行`, async () => {
    const fixture = loadLoginPage(extension, { loginError: '微信登录失败' });
    await fixture.page.handleWechatLogin();

    assert.equal(fixture.storage.token, undefined);
    assert.equal(fixture.storage.userInfo, undefined);
    assert.equal(fixture.requests.length, 1);
    assert.equal(fixture.guardShows(), 0);
    assert.equal(fixture.navigation.length, 0);
    assert.equal(fixture.page.data.identityPopupVisible, false);
    assert.equal(fixture.page.data.loading, false);
    assert.ok(fixture.toasts.includes('微信登录失败'));
  });
}
