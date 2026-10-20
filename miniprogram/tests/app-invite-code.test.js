const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');

function loadApp(extension) {
  let definition;
  const storage = {};
  const source = fs.readFileSync(path.join(root, `miniprogram/app.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('promptReview')
      ? { blockUnsafeText: () => '', initPromptReview: () => Promise.resolve() }
      : { install() {} },
    App: value => { definition = value; },
    wx: {
      getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; },
      removeStorageSync: key => { delete storage[key]; },
      getImageInfo() {},
    },
    console,
  });
  return { definition, storage };
}

for (const extension of ['ts', 'js']) {
  // 微信启动场景值（1007 单聊卡片、1047 扫码…）是数字，绝不能当成邀请码。
  test(`${extension}: 启动场景值不会被存成邀请码`, () => {
    const { definition, storage } = loadApp(extension);
    definition.onLaunch({ scene: 1007, path: 'pages/index/index', query: {} });
    assert.equal(storage.pending_invite_code, undefined);
  });

  test(`${extension}: 分享卡片的 invite_code 冷启动被记录`, () => {
    const { definition, storage } = loadApp(extension);
    definition.onLaunch({ scene: 1007, path: 'pages/index/index', query: { invite_code: 'AB23CD' } });
    assert.equal(storage.pending_invite_code, 'AB23CD');
  });

  test(`${extension}: 热启动（onShow）同样记录 invite_code`, () => {
    const { definition, storage } = loadApp(extension);
    definition.onLaunch({ scene: 1001, path: 'pages/index/index', query: {} });
    definition.onShow({ scene: 1008, path: 'pages/index/index', query: { invite_code: 'XY45ZK' } });
    assert.equal(storage.pending_invite_code, 'XY45ZK');
  });

  test(`${extension}: 小程序码 scene 参数（URL 编码）作为邀请码`, () => {
    const { definition, storage } = loadApp(extension);
    definition.onLaunch({ scene: 1047, path: 'pages/index/index', query: { scene: 'AB23CD' } });
    assert.equal(storage.pending_invite_code, 'AB23CD');
  });

  test(`${extension}: 非 6 位邀请码格式的参数被忽略，已有的不被覆盖`, () => {
    const { definition, storage } = loadApp(extension);
    definition.onLaunch({ path: 'pages/index/index', query: { invite_code: 'AB23CD' } });
    definition.onShow({ path: 'pages/index/index', query: { invite_code: '1007' } });
    definition.onShow({ path: 'pages/index/index', query: {} });
    assert.equal(storage.pending_invite_code, 'AB23CD');
  });

  test(`${extension}: 阅读页不记录邀请码`, () => {
    const { definition, storage } = loadApp(extension);
    definition.onLaunch({ path: 'pages/contentreader/contentreader', query: { invite_code: 'AB23CD' } });
    assert.equal(storage.pending_invite_code, undefined);
  });
}
