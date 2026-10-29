'use strict';
// 登录即完善昵称：所有昵称引导入口一律 required（不可跳过）；未登录访客依旧零弹窗。
// 组件默认不传参数仍为软模式，供既有测试与回退使用。
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');
const day = () => { const n = new Date(); return `${n.getFullYear()}-${n.getMonth() + 1}-${n.getDate()}`; };

function load(extension, { token = 'tk', profile = { code: 0, data: { nickname: '', id: 101 } }, presetDay = '' } = {}) {
  let definition;
  const storage = { token, userInfo: { id: 101 } };
  if (presetDay) storage[`nickname_prompt_day:101`] = presetDay;
  const toasts = [];
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
      request: options => { options.success({ statusCode: 200, data: profile }); },
      showToast(options) { toasts.push(options.title); },
    },
    console: { error() {}, warn() {}, log() {} },
  });
  const instance = { data: { ...definition.data }, setData(v) { Object.assign(this.data, v); }, ...definition.methods, triggerEvent() {} };
  return { instance, storage, toasts };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: required 弹窗进入强制态`, async () => {
    const { instance } = load(extension);
    assert.equal(await instance.checkAndShow({ required: true }), true);
    assert.equal(instance.data.visible, true);
    assert.equal(instance.data.required, true);
  });

  test(`${extension}: 当日已有跳过标记时 required 仍要弹`, async () => {
    const { instance } = load(extension, { presetDay: day() });
    assert.equal(await instance.checkAndShow({ required: true }), true);
    assert.equal(instance.data.visible, true);
  });

  test(`${extension}: required 下稍后完善与遮罩点击都关不掉`, () => {
    const { instance, toasts } = load(extension);
    instance.show({ required: true });
    instance.onSkip();
    assert.equal(instance.data.visible, true, 'skip must not close a required guard');
    instance.onMaskTap();
    assert.equal(instance.data.visible, true, 'mask tap must not close a required guard');
    assert.ok(toasts.length >= 1 && /昵称/.test(toasts[toasts.length - 1]), 'mask tap should instruct filling the nickname');
  });

  test(`${extension}: 默认调用仍是软模式可跳过`, async () => {
    const { instance } = load(extension);
    assert.equal(await instance.checkAndShow(), true);
    assert.equal(instance.data.required, false);
    instance.onSkip();
    assert.equal(instance.data.visible, false);
  });

  test(`${extension}: required 下未登录依旧不弹`, async () => {
    const anonymous = load(extension, { token: '' });
    assert.equal(await anonymous.instance.checkAndShow({ required: true }), false);
    assert.equal(anonymous.instance.data.visible, false);
  });
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: required also prompts when avatar is missing and preserves the nickname`, () => {
    const { instance } = load(extension);
    assert.equal(instance.showIfMissing({ id: 101, nickname: '已保存昵称', avatar: '' }, true), true);
    assert.equal(instance.data.nickname, '已保存昵称');
    assert.equal(instance.data.required, true);
  });
}

test('wxml：跳过按钮只在软模式渲染', () => {
  const wxml = fs.readFileSync(path.join(root, 'miniprogram/components/nickname-guard/nickname-guard.wxml'), 'utf8');
  assert.match(wxml, /<button[^>]*wx:if=("|')\{\{!required\}\}\1[^>]*ng-skip/);
});

// 五个入口全部以强制模式调用（ts/js 双产物一致）。
const callSites = [
  ['pages/index/index', /checkAndShow\(\{\s*required:\s*true\s*\}\)/, '首页'],
  ['pages/my/my', /showIfMissing\([^)]*,\s*true\s*\)/, '我的'],
  ['pages/login/login', /show\(\{\s*required:\s*true[,}]/, '登录'],
  ['pages/customerservice/customerservice', /checkAndShow\(\{\s*required:\s*true\s*\}\)/, '留资/转人工'],
  ['pages/aicostshare/aicostshare', /checkAndShow\(\{\s*required:\s*true\s*\}\)/, '公开报告'],
];
for (const [page, pattern, label] of callSites) {
  for (const extension of ['ts', 'js']) {
    test(`${extension}: ${label}入口以 required 调用`, () => {
      const source = fs.readFileSync(path.join(root, `miniprogram/${page}.${extension}`), 'utf8');
      assert.match(source, pattern);
    });
  }
}
