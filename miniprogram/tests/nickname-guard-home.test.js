const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');
const day = () => { const n = new Date(); return `${n.getFullYear()}-${n.getMonth() + 1}-${n.getDate()}`; };

function load(extension, { token = 'tk', profile = { code: 0, data: { nickname: '', avatar: 'a.png' } }, status = 200, fail = false } = {}) {
  let definition;
  const storage = { token, userInfo: { id: 101 } };
  const requests = [];
  const source = fs.readFileSync(path.join(root, `miniprogram/components/nickname-guard/nickname-guard.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('profile-validation') ? require(path.join(root, 'miniprogram/components/nickname-guard/profile-validation.js')) : name.endsWith('parameter') ? { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) }
      : name.endsWith('deviceFingerprint') ? { getCachedDeviceFingerprint: () => 'dev' }
      : { blockUnsafeText: () => '' },
    Component: value => { definition = value; },
    wx: {
      getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; },
      removeStorageSync: key => { delete storage[key]; },
      request: options => { requests.push(options.url); fail ? options.fail(new Error('offline')) : options.success({ statusCode: status, data: profile }); },
      showToast() {},
    },
    console: { error() {}, warn() {}, log() {} },
  });
  const instance = { data: { ...definition.data }, setData(v) { Object.assign(this.data, v); }, ...definition.methods, triggerEvent() {} };
  return { instance, storage, requests };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 昵称为空时提示，但展示不等于完成或推迟`, async () => {
    const { instance, storage, requests } = load(extension);
    assert.equal(await instance.checkAndShow(), true);
    assert.equal(instance.data.visible, true);
    assert.equal(instance.data.avatarUrl, 'a.png');
    assert.equal(storage['nickname_prompt_day:101'], undefined);
    assert.equal(requests.length, 1);
  });

  test(`${extension}: 明确稍后完善后按账号限制提醒，不影响其他账号`, async () => {
    const { instance, storage, requests } = load(extension);
    assert.equal(await instance.checkAndShow(), true);
    instance.onSkip();
    assert.equal(storage['nickname_prompt_day:101'], day());
    assert.equal(await instance.checkAndShow(), false);
    assert.equal(instance.data.visible, false);
    storage.userInfo = { id: 102 };
    assert.equal(await instance.checkAndShow(), true);
    assert.equal(requests.length, 3);
  });

  test(`${extension}: 已有昵称、未登录都不弹窗`, async () => {
    const named = load(extension, { profile: { code: 0, data: { nickname: '老王' } } });
    assert.equal(await named.instance.checkAndShow(), false);
    assert.equal(named.storage.nickname_prompt_day, undefined);
    const anonymous = load(extension, { token: '' });
    assert.equal(await anonymous.instance.checkAndShow(), false);
    assert.equal(anonymous.requests.length, 0);
  });

  test(`${extension}: 请求失败时放行，且不占用今天的提醒次数`, async () => {
    for (const options of [{ fail: true }, { status: 500 }, { profile: { code: 401, msg: 'x' } }]) {
      const { instance, storage } = load(extension, options);
      assert.equal(await instance.checkAndShow(), false);
      assert.equal(instance.data.visible, false);
      assert.equal(storage.nickname_prompt_day, undefined);
    }
  });
}

// 首页保留已有组件接口，但浏览生命周期不得自动要求资料登记。
for (const extension of ['ts', 'js']) {
  test(`${extension}: 首页 onShow 不自动触发昵称检查`, () => {
    const source = fs.readFileSync(path.join(root, `miniprogram/pages/index/index.${extension}`), 'utf8');
    const onShow = source.slice(source.indexOf('onShow()'), source.indexOf('onShareAppMessage()'));
    assert.doesNotMatch(onShow, /(?:void\s+)?this\.promptNicknameIfMissing\(\)/);
    const method = source.slice(source.indexOf('promptNicknameIfMissing()'));
    assert.match(method, /homeIntroPopupVisible/);
    assert.match(method, /selectComponent\('#nicknameGuard'\)/);
    assert.match(method, /checkAndShow/);
  });
}

test('首页声明并放置 nickname-guard 组件', () => {
  const json = JSON.parse(fs.readFileSync(path.join(root, 'miniprogram/pages/index/index.json'), 'utf8'));
  assert.equal(json.usingComponents['nickname-guard'], '/components/nickname-guard/nickname-guard');
  const wxml = fs.readFileSync(path.join(root, 'miniprogram/pages/index/index.wxml'), 'utf8');
  assert.match(wxml, /<nickname-guard id="nicknameGuard"/);
});
