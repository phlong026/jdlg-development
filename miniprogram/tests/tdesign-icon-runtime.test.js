'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '../miniprogram/components/tdesign-miniprogram');

// Resolve only files shipped with the component, as the mini-program runtime
// cannot fall back to the developer's node_modules for bare npm imports.
function loadIcon() {
  const registrations = [];
  const cache = new Map();
  const context = vm.createContext({
    console, setTimeout, clearTimeout,
    getApp: () => ({}),
    Component: definition => registrations.push(definition),
    Behavior: definition => definition,
    wx: {
      getSystemInfoSync: () => ({ SDKVersion: '3.16.3', windowWidth: 390, platform: 'devtools' }),
    },
  });
  function load(file) {
    if (cache.has(file)) return cache.get(file).exports;
    const module = { exports: {} };
    cache.set(file, module);
    const source = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
      fileName: file,
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
    }).outputText;
    const run = vm.runInContext('(function(require, module, exports) {' + source + '\n})', context, { filename: file });
    run(id => {
      assert.ok(id.startsWith('.'), '小程序包不能加载未打包的 npm 模块: ' + id);
      let dependency = path.resolve(path.dirname(file), id);
      if (!path.extname(dependency)) dependency += '.js';
      assert.ok(dependency.startsWith(root + path.sep), '组件依赖应包含在本地目录中');
      return load(dependency);
    }, module, module.exports);
    return module.exports;
  }
  const exported = load(path.join(root, 'icon/icon.js'));
  assert.equal(registrations.length, 1, '图标组件必须完成原生注册');
  const definition = registrations[0];
  const data = Object.fromEntries(Object.entries(definition.properties).map(([key, prop]) => [key, prop.value]));
  const events = [];
  const instance = {
    ...definition.methods,
    data: { ...data, ...definition.data },
    setData(patch, callback) { Object.assign(this.data, patch); if (callback) callback(); },
    triggerEvent(...args) { events.push(args); },
  };
  return { exported, definition, instance, events };
}

const settle = () => new Promise(resolve => setImmediate(resolve));

test('vendored TDesign icon registers without npm modules and forwards click events', () => {
  const { exported, definition, instance, events } = loadIcon();
  assert.equal(typeof exported.default, 'function');
  assert.equal(definition.options.virtualHost, true);
  const detail = { x: 10, y: 20 };
  instance.onTap({ detail });
  assert.deepEqual(events, [['click', detail]]);
});

test('named icons preserve color, numeric size and observer updates', async () => {
  const { definition, instance } = loadIcon();
  Object.assign(instance.data, { name: 'search', color: '#123456', size: 24 });
  definition.observers['name, color, size, style'].call(instance);
  await settle();
  assert.equal(instance.data.isImage, false);
  assert.equal(instance.data.iconStyle, 'color: #123456; font-size: 24px');
  Object.assign(instance.data, { color: '', size: '' });
  instance.setIconStyle();
  await settle();
  assert.equal(instance.data.iconStyle, '');
});

test('image icons with explicit units keep width and height without measuring', async () => {
  const { instance } = loadIcon();
  Object.assign(instance.data, { name: '/assets/icon.png', size: '40rpx' });
  instance.setIconStyle();
  await settle();
  assert.equal(instance.data.isImage, true);
  assert.equal(instance.data.iconStyle, 'font-size: 40rpx; width: 40rpx; height: 40rpx');
});

test('image icons without size await the measured height', async () => {
  const { instance } = loadIcon();
  Object.assign(instance.data, { name: 'https://example.test/icon.png' });
  const query = {
    select(selector) { assert.equal(selector, '.t-icon'); return this; },
    boundingClientRect(callback) { this.callback = callback; return this; },
    exec() { this.callback({ height: 28 }); },
  };
  instance.createSelectorQuery = () => query;
  instance.setIconStyle();
  await settle();
  assert.equal(instance.data.isImage, true);
  assert.equal(instance.data.iconStyle, 'width: 28px; height: 28px');
});
