const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '../miniprogram');

for (const extension of ['ts', 'js']) {
  test(`${extension}: 设备指纹使用离屏 Canvas，不依赖页面节点`, async () => {
    let canvasCount = 0;
    const source = fs.readFileSync(path.join(root, `utils/deviceFingerprint.${extension}`), 'utf8');
    const code = extension === 'ts'
      ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
      : source;
    const exports = {};
    const context = {
      fillRect() {}, fillText() {}, beginPath() {}, arc() {}, fill() {},
      measureText: () => ({ width: 80 }),
    };
    vm.runInNewContext(code, {
      exports,
      require: () => require(path.join(root, 'utils/crypto-js/index.js')),
      setTimeout: () => 1,
      clearTimeout() {},
      wx: {
        getSystemInfoSync: () => ({ brand: 'fixture', model: 'test-phone', SDKVersion: '2.32.3' }),
        createSelectorQuery() { throw new Error('不应访问页面 Canvas'); },
        createOffscreenCanvas(options) {
          canvasCount += 1;
          assert.equal(options.type, '2d');
          return {
            getContext: () => context,
            toDataURL: options => options.success({ data: 'data:image/png;base64,dGVzdA==' }),
          };
        },
      },
    });
    const fingerprint = await exports.generateDeviceFingerprint();
    assert.match(fingerprint, /^[a-f0-9]{32}$/);
    assert.equal(canvasCount, 1);
    assert.equal(await exports.generateDeviceFingerprint(), fingerprint);
  });
}

test('我的页不创建触发旧基础库渲染错误的闲置指纹 Canvas', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/my/my.wxml'), 'utf8');
  assert.ok(!wxml.includes('device-fingerprint-canvas'));
});
