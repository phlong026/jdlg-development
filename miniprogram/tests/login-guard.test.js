'use strict';
// 分享内容登录门禁：模板广场与公开测算报告未登录时挡住内容，
// 微信一键登录（勾选协议）成功后才由页面加载内容。
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');

function load(extension, { token = '', loginOk = true } = {}) {
  let definition;
  const storage = { token };
  const toasts = [];
  const events = [];
  const requests = [];
  const wx = {
    getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
    login: options => { options.success({ code: 'CODE123' }); },
    request(options) {
      requests.push(options.url);
      const ok = loginOk && options.data?.code === 'CODE123';
      options.success({ statusCode: 200, data: ok ? { code: 0, data: { token: 'NT', id: 9, username: 'wx_abc' } } : { code: 1, msg: 'bad code' } });
    },
    showToast(options) { toasts.push(options.title); },
    navigateTo() {},
  };
  const source = fs.readFileSync(path.join(root, `miniprogram/components/login-guard/login-guard.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('deviceFingerprint')
      ? { generateDeviceFingerprint: async () => 'dev', getCachedDeviceFingerprint: () => 'dev-cached', cacheDeviceFingerprint: () => {} }
      : (name.endsWith('parameter') ? { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) } : {}),
    Component: value => { definition = value; },
    wx, console: { log() {}, warn() {}, error() {} },
  });
  const instance = { data: { ...definition.data }, setData(v) { Object.assign(this.data, v); }, ...definition.methods, triggerEvent(name) { events.push(name); } };
  return { instance, storage, toasts, events, requests };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 未登录时 check 挡住内容并触发 gated`, () => {
    const { instance, events } = load(extension);
    assert.equal(instance.check(), false);
    assert.equal(instance.data.visible, true);
    assert.ok(events.includes('gated'));
  });

  test(`${extension}: 已登录时 check 放行且不显示门禁`, () => {
    const { instance, events } = load(extension, { token: 'tk' });
    assert.equal(instance.check(), true);
    assert.equal(instance.data.visible, false);
    assert.ok(events.includes('authenticated'));
  });

  test(`${extension}: 未勾协议直接点登录只提示不发请求`, async () => {
    const { instance, requests, toasts } = load(extension);
    instance.check();
    await instance.onWxLogin();
    assert.equal(requests.length, 0);
    assert.ok(toasts.some(t => /协议/.test(t)));
    assert.equal(instance.data.visible, true);
  });

  test(`${extension}: 勾选协议后登录成功存 token 并放行`, async () => {
    const { instance, storage, events, requests } = load(extension);
    instance.check();
    instance.onTogglePrivacy();
    await instance.onWxLogin();
    assert.equal(requests.length, 1);
    assert.equal(storage.token, 'NT');
    assert.equal(storage.userInfo.id, 9);
    assert.equal(instance.data.visible, false);
    assert.ok(events.includes('loginsucceeded'));
  });

  test(`${extension}: 登录接口失败保持门禁不存 token`, async () => {
    const { instance, storage, toasts } = load(extension, { loginOk: false });
    instance.check();
    instance.onTogglePrivacy();
    await instance.onWxLogin();
    assert.equal(instance.data.visible, true);
    assert.ok(!storage.token, '失败时不得写入 token');
    assert.ok(toasts.length >= 1);
  });
}

// 页面契约：两个分享内容页挂 login-guard 且登录成功后重新加载内容；my 页头像用官方 chooseAvatar。
const pageContracts = [
  ['pages/aicostshare/aicostshare', 'loadPublicSnapshot', '公开测算报告'],
  ['pages/template/template', 'initializePage', '模板广场'],
];
for (const [page, reloadMethod, label] of pageContracts) {
  test(`${label}：声明并放置 login-guard 组件`, () => {
    const json = JSON.parse(fs.readFileSync(path.join(root, `miniprogram/${page}.json`), 'utf8'));
    assert.ok(json.usingComponents['login-guard'], 'json 注册 login-guard');
    const wxml = fs.readFileSync(path.join(root, `miniprogram/${page}.wxml`), 'utf8');
    assert.match(wxml, /<login-guard id="loginGuard"/);
  });

  for (const extension of ['ts', 'js']) {
    test(`${extension}：${label}未登录不加载内容，登录成功后重新加载`, () => {
      const source = fs.readFileSync(path.join(root, `miniprogram/${page}.${extension}`), 'utf8');
      assert.match(source, /#loginGuard/, '取到 login-guard 组件');
      assert.match(source, /loginsucceeded|onLoginSucceeded/, '登录成功事件处理');
      assert.match(source, new RegExp(`${reloadMethod}\\(`), '登录后重新加载内容');
    });
  }
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
