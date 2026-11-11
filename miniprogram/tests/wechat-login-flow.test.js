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
  profileAvatar = 'https://cdn.example/saved-avatar.png',
  pageStack = [{ route: 'pages/login/login' }],
  loginError = '',
  deferLogin = false,
  deferProfile = false,
  deferIdentity = false,
} = {}) {
  let definition;
  const storage = {};
  const requests = [];
  const toasts = [];
  const navigation = [];
  const guardOptions = [];
  const serverProfile = { id: loginResult.id, nickname: profileNickname, avatar: profileAvatar };

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
      if (['/login', '/login/phone', '/login/password'].some(suffix => options.url.endsWith(suffix))) {
        if (deferLogin) return;
        if (loginError) {
          options.success({ statusCode: 500, data: { code: 500, msg: loginError } });
        } else {
          options.success({ statusCode: 200, data: { code: 0, data: loginResult } });
        }
        return;
      }
      if (options.url.endsWith('/api/v1/miniprogram/profile/identity')) {
        if (!deferIdentity) options.success({ statusCode: 200, data: { code: 0 } });
        return;
      }
      if (options.url.endsWith('/api/v1/miniprogram/profile')) {
        if (deferProfile) return;
        options.success({ statusCode: 200, data: { code: 0, data: { ...serverProfile } } });
        return;
      }
      throw new Error(`unexpected request: ${options.url}`);
    },
    showToast: options => toasts.push(options.title),
    switchTab: options => navigation.push({ method: 'switchTab', url: options.url }),
    reLaunch: options => navigation.push({ method: 'reLaunch', url: options.url }),
    navigateBack: options => navigation.push({ method: 'navigateBack', delta: options.delta }),
  };

  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('profile-validation')
      ? require(path.join(root, 'miniprogram/components/nickname-guard/profile-validation.js'))
      : name.endsWith('deviceFingerprint')
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
    getCurrentPages: () => pageStack,
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
      return { show(options) { guardOptions.push(options); } };
    },
  };

  return {
    page,
    storage,
    requests,
    toasts,
    navigation,
    guardShows: () => guardOptions.length,
    guardOptions,
    setServerProfile: profile => Object.assign(serverProfile, profile),
  };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 微信 code 登录缓存账号，服务器昵称头像完整后进入首页`, async () => {
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
    assert.equal(fixture.storage.userInfo.nickname, '山居用户');
    assert.equal(fixture.storage.userInfo.avatar, 'https://cdn.example/saved-avatar.png');
    assert.equal(fixture.requests[1].method, 'GET');
    assert.ok(fixture.requests[1].url.endsWith('/api/v1/miniprogram/profile'));
    assert.equal(fixture.guardShows(), 0);
    assert.deepEqual(fixture.navigation, [{ method: 'switchTab', url: '/pages/index/index' }]);
  });

  test(`${extension}: 老用户缺昵称时在原登录卡内确认资料并停止跳转`, async () => {
    const fixture = loadLoginPage(extension, { profileNickname: '   ' });
    await fixture.page.handleWechatLogin();

    assert.equal(fixture.storage.token, 'login-token');
    assert.equal(fixture.guardShows(), 1);
    assert.equal(fixture.navigation.length, 0);
    assert.equal(fixture.page.data.identityPopupVisible, false);
    assert.equal(fixture.page.data.completingProfile, true);
    assert.equal(fixture.page.data.loading, false);
  });

  test(`${extension}: 老用户已有昵称但缺头像时仍须完善资料，不能进入分享内容`, async () => {
    const fixture = loadLoginPage(extension, { profileAvatar: '' });
    await fixture.page.handleWechatLogin();
    assert.equal(fixture.guardShows(), 1);
    assert.equal(fixture.guardOptions[0].required, true);
    assert.equal(fixture.guardOptions[0].userID, 101);
    assert.equal(fixture.guardOptions[0].nickname, '山居用户');
    assert.equal(fixture.guardOptions[0].avatar, '');
    assert.equal(fixture.navigation.length, 0);
    // 组件完成通知本身不是服务器资料已保存的证据。
    await fixture.page.onNicknameCompleted();
    assert.equal(fixture.navigation.length, 0);
    assert.equal(fixture.requests.filter(r => r.method === 'GET').length, 2);
  });

  test(`${extension}: share-view 登录返回同样要求头像和昵称`, async () => {
    const fixture = loadLoginPage(extension, { profileAvatar: '' });
    fixture.page.onLoad({ from: 'share-view' });
    fixture.page.setData({ agreedPrivacy: true });
    await fixture.page.handleWechatLogin();
    assert.equal(fixture.guardShows(), 1);
    assert.equal(fixture.guardOptions[0].required, true);
    assert.equal(Object.hasOwn(fixture.guardOptions[0], 'nicknameOnly'), false);
    assert.equal(fixture.navigation.length, 0);
  });

  test(`${extension}: 从分享内容登录后，资料回读完整才返回原页面`, async () => {
    const fixture = loadLoginPage(extension, {
      profileAvatar: '',
      pageStack: [{ route: 'pages/aicostshare/aicostshare', options: { snapshot: 'saved-report' } }, { route: 'pages/login/login' }],
    });
    await fixture.page.handleWechatLogin();
    assert.equal(fixture.navigation.length, 0);
    fixture.setServerProfile({ avatar: 'https://cdn.example/confirmed-avatar.png' });
    await fixture.page.onNicknameCompleted();
    assert.deepEqual(fixture.navigation, [{ method: 'navigateBack', delta: 1 }]);
    assert.equal(fixture.storage.userInfo.nickname, '山居用户');
    assert.equal(fixture.storage.userInfo.avatar, 'https://cdn.example/confirmed-avatar.png');
    assert.equal(fixture.page.data.completingProfile, false);
    assert.equal(fixture.requests.filter(r => r.method === 'GET').length, 2);
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

const flush = () => new Promise(setImmediate);
for (const extension of ['ts', 'js']) {
  for (const method of ['handleWechatLogin', 'handlePhoneLogin', 'handlePasswordLogin']) {
    test(`${extension}: ${method} 的旧响应在退出或换账号后不能覆盖会话`, async () => {
      for (const mode of ['unload', 'switch', 'unload-and-switch']) {
        const f = loadLoginPage(extension, { deferLogin: true });
        Object.assign(f.page.data, { phone: '13800000000', smsCode: 'fixture-code', username: 'fixture-user', password: 'fixture-password' });
        const pending = f.page[method](); await flush();
        assert.equal(f.requests.length, 1);
        if (mode.includes('unload')) f.page.onUnload();
        if (mode.includes('switch')) { f.storage.token = 'B-current'; f.storage.userInfo = { id: 202, nickname: 'B' }; }
        const before = JSON.stringify(f.storage);
        f.requests[0].success({ statusCode: 200, data: { code: 0, data: { token: 'A-stale', id: 101 } } });
        await pending;
        assert.equal(JSON.stringify(f.storage), before);
        assert.equal(f.requests.length, 1, '失效登录不能继续读取账号资料');
        assert.equal(f.navigation.length, 0); assert.equal(f.guardShows(), 0);
      }
    });
  }
  test(`${extension}: profile 回包在退出后不得缓存资料、清会话或跳转`, async () => {
    for (const statusCode of [200, 401]) {
      const f = loadLoginPage(extension, { deferProfile: true });
      f.storage.token = 'A-current'; f.storage.userInfo = { id: 101, nickname: 'before' };
      const pending = f.page.enterAppAfterLogin(); await flush();
      assert.equal(f.requests.length, 1); f.page.onUnload();
      const before = JSON.stringify(f.storage);
      f.requests[0].success({ statusCode, data: { code: statusCode === 200 ? 0 : 401, data: { id: 101, nickname: 'after', avatar: 'https://cdn.example/A.jpg' } } });
      await pending;
      assert.equal(JSON.stringify(f.storage), before); assert.equal(f.navigation.length, 0); assert.equal(f.guardShows(), 0);
    }
  });
  test(`${extension}: 当前页面收到401后回到可重新登录状态，不保留加载遮罩`, async () => {
    const f = loadLoginPage(extension, { deferProfile: true });
    f.storage.token = 'expired'; f.storage.userInfo = { id: 101 };
    const pending = f.page.enterAppAfterLogin(); await flush();
    f.requests[0].success({ statusCode: 401, data: { code: 401 } }); await pending;
    assert.equal(f.storage.token, undefined); assert.equal(f.page.data.loading, false);
    assert.match(f.page.data.profileError, /重新登录/); assert.equal(f.navigation.length, 0);
  });
  test(`${extension}: 身份选择旧响应不得把旧登录资料合并到新账号`, async () => {
    for (const mode of ['unload', 'switch']) {
      const f = loadLoginPage(extension, { deferIdentity: true });
      f.storage.token = 'A-current'; f.storage.userInfo = { id: 101 };
      f.page.data.pendingLoginResult = { token: 'A-current', id: 101 };
      const pending = f.page.onIdentitySelect({ currentTarget: { dataset: { identity: '乡墅业主' } } }); await flush();
      assert.equal(f.requests.length, 1);
      if (mode === 'unload') f.page.onUnload();
      else { f.storage.token = 'B-current'; f.storage.userInfo = { id: 202 }; }
      const before = JSON.stringify(f.storage);
      f.requests[0].success({ statusCode: 200, data: { code: 0 } }); await pending;
      assert.equal(JSON.stringify(f.storage), before); assert.equal(f.requests.length, 1); assert.equal(f.navigation.length, 0);
    }
  });
}
