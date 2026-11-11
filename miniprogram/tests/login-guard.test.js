const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
const root = path.resolve(__dirname, '..');
const complete = { id: 9, nickname: '林间访客', avatar: 'https://cdn.example/portrait.jpg' };

test('真实登录与资料补全仍保留明确的拒绝或返回入口', () => {
  const wxml = fs.readFileSync(path.join(root, 'miniprogram/components/login-guard/login-guard.wxml'), 'utf8');
  // 审核整改：登录环节必须有显著的取消/拒绝按钮
  assert.match(wxml, /lg-close" catchtap="onDismiss"/, '右上角关闭按钮');
  assert.match(wxml, /lg-skip" catchtap="onDismiss"/, '暂不登录按钮');
  assert.match(wxml, /暂不登录，先逛逛/);
  assert.match(wxml, /稍后补充，继续浏览/);
});
function load(extension, { token = '', loginOk = true, profile = complete, status = 200, defer = false, deferLogin = false, offline = false, cachedProfile = null } = {}) {
  let definition;
  const storage = { token, userInfo: cachedProfile ? { id: 9, ...cachedProfile } : { id: 9 } }, toasts = [], events = [], requests = [], shows = [], navigations = [];
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
    getCurrentPages: () => [{ route: 'pages/index/index' }, { route: 'pages/aigenerate/aigenerate' }],
    navigateBack: o => navigations.push({ kind: 'back', delta: o && o.delta }),
    switchTab: o => navigations.push({ kind: 'switchTab', url: o && o.url }),
  };
  const source = fs.readFileSync(path.join(root, `miniprogram/components/login-guard/login-guard.${extension}`), 'utf8');
  const code = extension === 'ts' ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText : source;
  vm.runInNewContext(code, { exports: {}, require: name => name.endsWith('profile-validation') ? require(path.join(root, 'miniprogram/components/nickname-guard/profile-validation.js')) : name.endsWith('profile-access') ? { isPublicBrowseRoute: route => String(route || '').startsWith('pages/index/') } : name.endsWith('deviceFingerprint')
    ? { generateDeviceFingerprint: async () => 'dev', getCachedDeviceFingerprint: () => 'dev', cacheDeviceFingerprint() {} }
    : { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) }, Component: d => { definition = d; }, wx, getCurrentPages: wx.getCurrentPages, console: { error() {} } });
  const instance = { ...definition.methods, data: { ...definition.data }, setData(v) { Object.assign(this.data, v); }, selectComponent: () => nicknameGuard, triggerEvent(name, detail) { events.push({ name, detail }); } };
  return { instance, storage, toasts, events, requests, shows, navigations, nicknameGuard, detach: () => definition.lifetimes.detached.call(instance) };
}
for (const ext of ['ts', 'js']) {
  test(`${ext}: 拒绝登录关闭浮层并返回上一页（审核整改）`, async () => {
    const f = load(ext);
    await f.instance.check();
    assert.equal(f.instance.data.visible, true, '未登录时浮层展示');
    f.instance.onDismiss();
    assert.equal(f.instance.data.visible, false, '拒绝后浮层关闭');
    assert.ok(f.events.some(e => e.name === 'dismissed'));
    assert.equal(f.navigations.length, 1, '身份页拒绝登录返回上一页');
    assert.equal(f.navigations[0].kind, 'back');
  });
  test(`${ext}: 拒绝后切后台核验失败不再重弹（审核整改）`, async () => {
    const f = load(ext, { token: 'tk', cachedProfile: complete, offline: true });
    f.instance.onDismiss();
    f.navigations.splice(0);
    assert.equal(await f.instance.check({ silent: true }), false);
    assert.equal(f.instance.data.visible, false, '静默失败不重弹浮层');
    assert.equal(f.navigations.length, 1, '自动按拒绝处理返回');
  });
  test(`${ext}: missing token remains blocked`, async () => {
    const f = load(ext); assert.equal(await f.instance.check(), false); assert.equal(f.instance.data.visible, true); assert.ok(f.events.some(e => e.name === 'gated')); assert.equal(f.requests.length, 0);
  });
  test(`${ext}: silent background verify keeps the panel hidden, never gates early and still grants`, async () => {
    const f = load(ext, { token: 'tk', cachedProfile: complete, defer: true });
    const pending = f.instance.check({ silent: true });
    assert.equal(f.instance.data.visible, false);
    assert.equal(f.instance.data.checking, false, '后台重验不得闪现核验面板');
    assert.equal(f.events.filter(e => e.name === 'gated').length, 0, '后台重验开始时不得提前上锁');
    assert.equal(f.requests.length, 1);
    f.requests[0].success({ statusCode: 200, data: { code: 0, data: complete } });
    assert.equal(await pending, true);
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 1);
  });
  test(`${ext}: silent verify still gates on real failures`, async () => {
    const f = load(ext, { token: 'tk', cachedProfile: complete, offline: true });
    assert.equal(await f.instance.check({ silent: true }), false);
    assert.ok(f.events.some(e => e.name === 'gated'), '核验失败仍必须上锁');
    assert.equal(f.instance.data.visible, true);
  });
  test(`${ext}: token without cached profile verifies silently before granting access`, async () => {
    const f = load(ext, { token: 'tk', defer: true }); const pending = f.instance.check();
    assert.equal(f.instance.data.visible, false); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0); assert.equal(f.requests.length, 1);
    f.requests[0].success({ statusCode: 200, data: { code: 0, data: complete } });
    assert.equal(await pending, true); assert.equal(f.instance.data.visible, false);
    assert.equal(f.events.find(e => e.name === 'authenticated').detail.token, 'tk');
  });
  test(`${ext}: complete cached profile still waits for authoritative confirmation`, async () => {
    const f = load(ext, { token: 'tk', cachedProfile: complete, defer: true }); const pending = f.instance.check();
    assert.equal(f.instance.data.visible, false);
    const granted = f.events.filter(e => e.name === 'authenticated');
    assert.equal(granted.length, 0); assert.equal(f.requests.length, 1);
    f.requests[0].success({ statusCode: 200, data: { code: 0, data: complete } });
    assert.equal(await pending, true);
    assert.equal(f.instance.data.visible, false); assert.equal(f.storage.userInfo.nickname, complete.nickname);
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 1);
  });
  test(`${ext}: cached profile stays blocked when authoritative read finds gaps`, async () => {
    const f = load(ext, { token: 'tk', cachedProfile: complete, profile: { ...complete, avatar: '' } });
    assert.equal(await f.instance.check(), false);
    assert.ok(f.events.some(e => e.name === 'gated'));
    assert.equal(f.shows.length, 1); assert.equal(f.shows[0].required, true); assert.equal(f.shows[0].userID, 9);
    assert.equal(f.instance.data.visible, false);
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0);
  });
  test(`${ext}: cached profile stays blocked on network failure`, async () => {
    const f = load(ext, { token: 'tk', cachedProfile: complete, offline: true });
    assert.equal(await f.instance.check(), false);
    assert.ok(f.events.some(e => e.name === 'gated'));
    assert.equal(f.instance.data.visible, true); assert.ok(f.instance.data.error);
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
  test(`${ext}: authoritative profile replaces a stale cached account id for the same token`, async () => {
    const f = load(ext, { token: 'tk', profile: { ...complete, id: 10 } }); assert.equal(await f.instance.check(), true);
    assert.equal(f.storage.userInfo.id, 10); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 1);
  });
  test(`${ext}: profile without an authoritative account id clears only the current session`, async () => {
    const f = load(ext, { token: 'tk', profile: { ...complete, id: 0 } }); assert.equal(await f.instance.check(), false);
    assert.equal(f.storage.token, undefined); assert.equal(f.storage.userInfo, undefined);
    assert.equal(f.instance.data.needsLogin, true); assert.equal(f.events.filter(e => e.name === 'authenticated').length, 0);
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

// 单篇项目笔记分享与其他受保护页统一：登录后必须有已保存的昵称和头像。
for (const ext of ['ts', 'js']) {
  test(`${ext}: 分享查看场景缺头像仍拦截并打开完整资料表单`, async () => {
    const f = load(ext, { token: 'tk', profile: { id: 9, nickname: '林间访客', avatar: '' } });
    const ok = await f.instance.check({ scope: 'share' });
    assert.equal(ok, false, '缺头像不得放行');
    assert.ok(f.events.some(e => e.name === 'gated'));
    assert.equal(f.shows.length, 1);
    assert.equal(Object.hasOwn(f.shows[0], 'nicknameOnly'), false, '不得保留仅昵称的入参');
  });
  test(`${ext}: 分享查看场景缺昵称同样使用完整资料口径`, async () => {
    const f = load(ext, { token: 'tk', profile: { id: 9, nickname: '', avatar: '' } });
    const ok = await f.instance.check({ scope: 'share' });
    assert.equal(ok, false, '缺昵称必须补');
    assert.ok(f.shows.length >= 1);
    assert.equal(Object.hasOwn(f.shows[f.shows.length - 1], 'nicknameOnly'), false);
  });
  test(`${ext}: 分享查看只要求登录昵称和头像，不要求手机号`, async () => {
    const f = load(ext, { token: 'tk', profile: { id: 9, nickname: '林间访客', avatar: 'https://cdn.example/avatar.jpg' } });
    assert.equal(await f.instance.check({ scope: 'share' }), true);
    assert.equal(f.shows.length, 0);
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 1);
  });
  test(`${ext}: 身份功能页仍要求完整资料`, async () => {
    const f = load(ext, { token: 'tk', profile: { id: 9, nickname: '林间访客', avatar: '' } });
    const ok = await f.instance.check();
    assert.equal(ok, false, '身份页缺头像不放行');
    assert.ok(f.shows.length >= 1);
    assert.equal(Object.hasOwn(f.shows[f.shows.length - 1], 'nicknameOnly'), false);
  });
}

// 分享页仍保留 share-view 返回语义，但登录页不得再降级为仅昵称。
test('the share page keeps its return scene without a nickname-only login exception', () => {
  for (const file of ['projectnoteview.js', 'projectnoteview.ts']) {
    const code = fs.readFileSync(path.join(root, 'miniprogram/pages/projectnoteview', file), 'utf8');
    assert.match(code, /\/pages\/login\/login\?from=share-view/, file);
  }
  for (const file of ['login.js', 'login.ts']) {
    const code = fs.readFileSync(path.join(root, 'miniprogram/pages/login', file), 'utf8');
    assert.doesNotMatch(code, /hasProfileNickname|_loginScene/, file + ' 分享登录不得再放宽资料要求');
    assert.match(code, /hasCompleteProfile/, file + ' 必须统一校验完整资料');
  }
});

test('the removed P8 nickname-only mode cannot be re-enabled by any profile gate caller', () => {
  for (const relativePath of [
    'miniprogram/components/login-guard/login-guard.ts',
    'miniprogram/components/login-guard/login-guard.js',
    'miniprogram/components/login-guard/login-guard.wxml',
    'miniprogram/components/nickname-guard/nickname-guard.ts',
    'miniprogram/components/nickname-guard/nickname-guard.js',
    'miniprogram/components/nickname-guard/nickname-guard.wxml',
    'miniprogram/pages/login/login.ts',
    'miniprogram/pages/login/login.js',
  ]) {
    const source = fs.readFileSync(path.join(root, relativePath), 'utf8');
    assert.doesNotMatch(source, /nicknameOnly/, relativePath);
  }
});

// 补齐头像后的内部重检仍复用分享场景，但只有完整资料才放行。
for (const ext of ['ts', 'js']) {
  test(`${ext}: 分享场景补齐头像后重检才放行`, async () => {
    const profile = { id: 9, nickname: '林间访客', avatar: '' };
    const f = load(ext, { token: 'tk', profile });
    assert.equal(await f.instance.check({ scope: 'share' }), false);
    profile.avatar = 'https://cdn.example/confirmed-avatar.jpg';
    assert.equal(await f.instance.onProfileCompleted(), true, '头像和昵称完整后放行');
    assert.equal(await f.instance.onRetry(), true, '重试仍校验完整资料');
    assert.equal(f.events.filter(e => e.name === 'authenticated').length, 2);
  });
}
