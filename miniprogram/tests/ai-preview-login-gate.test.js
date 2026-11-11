'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '../miniprogram');

function loadCostPage() {
  let definition;
  const storage = {};
  const modals = [];
  const navigations = [];
  const source = fs.readFileSync(path.join(root, 'pages/aicost/aicost.ts'), 'utf8');
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(code, {
    exports: {},
    Page(value) { definition = value; },
    require(name) {
      if (name.endsWith('costRegions')) return require(path.join(root, 'utils/costRegions.js'));
      if (name.endsWith('asset')) return { resolveAssetPath: value => value };
      if (name.endsWith('cloudImage')) return { prepareCloudImage: async () => '' };
      return { prepareCostEstimateShareImage: async () => '' };
    },
    wx: {
      getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; },
      showToast() {},
      showModal(options) { modals.push(options); options.success?.({ confirm: true }); },
      navigateTo(options) { navigations.push(options.url); },
    },
    console,
  });
  const page = {
    ...definition,
    data: { ...definition.data, footprintArea: '120', buildingArea: '360' },
    setData(value) { Object.assign(this.data, value); },
  };
  return { page, storage, modals, navigations };
}

function loadGeneratePage() {
  let definition;
  const storage = {};
  const requests = [];
  const modals = [];
  const navigations = [];
  const source = fs.readFileSync(path.join(root, 'pages/aigenerate/aigenerate.ts'), 'utf8');
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(code, {
    exports: {},
    Page(value) { definition = value; },
    require(name) {
      if (name.endsWith('parameter')) return { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) };
      if (name.endsWith('deviceFingerprint')) return { getCachedDeviceFingerprint: () => '' };
      if (name.endsWith('asset')) return { resolveAssetPath: value => value };
      if (name.endsWith('aiError')) return { sanitizeAIGenerationErrorMessage: value => value };
      if (name.endsWith('promptReview')) return { blockUnsafeText: () => false };
      throw new Error(`unexpected require ${name}`);
    },
    wx: {
      getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; },
      removeStorageSync: key => { delete storage[key]; },
      getSystemInfoSync: () => ({ statusBarHeight: 20, windowWidth: 375 }),
      getMenuButtonBoundingClientRect: () => null,
      request(options) {
        requests.push(options);
        if (options.url.includes('/ai/pricing')) {
          options.success?.({ statusCode: 200, data: { code: 0, data: { prices: {}, configs: {} } } });
        }
      },
      showModal(options) { modals.push(options); options.success?.({ confirm: true }); },
      navigateTo(options) { navigations.push(options.url); },
      showToast() {},
    },
    console,
    decodeURIComponent,
    Date,
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(value, done) { Object.assign(this.data, value); done?.call(this); },
    getOpenerEventChannel: () => ({ on() {} }),
  };
  return { page, storage, requests, modals, navigations };
}

test('anonymous cost form keeps entered values and asks for account only when estimating', () => {
  const fixture = loadCostPage();
  fixture.page.onEstimateCost();

  assert.equal(fixture.page.data.footprintArea, '120');
  assert.equal(fixture.page.data.buildingArea, '360');
  assert.equal(fixture.storage.aicostEstimateResult, undefined);
  assert.equal(fixture.modals.length, 1);
  assert.deepEqual(fixture.navigations, ['/pages/login/login?from=ai-action']);
});

test('anonymous AI deep link never reads private task/template/draft state and keeps the public form usable', async () => {
  for (const privateOptions of [{ taskId: '8' }, { templateId: '12' }, { draft: '1' }]) {
    const fixture = loadGeneratePage();
    await fixture.page.onLoad({ ...privateOptions, prompt: encodeURIComponent('保留的匿名输入') });
    assert.equal(fixture.page.data.promptText, '保留的匿名输入');
    assert.equal(fixture.modals.length, 1);
    assert.deepEqual(fixture.navigations, ['/pages/login/login?from=ai-action']);
    assert.equal(fixture.requests.filter(request => /original-task|\/ai\/task\//.test(request.url)).length, 0);
  }

  const ordinary = loadGeneratePage();
  await ordinary.page.onLoad({ source: 'index' });
  assert.equal(ordinary.modals.length, 0, 'normal AI form entry must never force login');
});

test('public AI style metadata uses reference mode for pricing without exposing a model image URL', () => {
  let definition;
  const source = fs.readFileSync(path.join(root, 'pages/aitoolworkbench/aitoolworkbench.ts'), 'utf8');
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(code, {
    exports: {},
    Page(value) { definition = value; },
    require(name) {
      if (name.endsWith('parameter')) return { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) };
      if (name.endsWith('deviceFingerprint')) return { getCachedDeviceFingerprint: () => '' };
      if (name.endsWith('aiError')) return { sanitizeAIGenerationErrorMessage: value => value };
      if (name.endsWith('promptReview')) return { blockUnsafeText: () => false };
      if (name.endsWith('aiTools')) return { getCategoryLabel: () => '建筑' };
      if (name.endsWith('aiToolApi')) return { fetchAIToolDetail: async () => ({}) };
      if (name.endsWith('aiToolPresentation')) return { shouldUseMinimalAIToolPresentation: () => false };
      throw new Error(`unexpected require ${name}`);
    },
    wx: { getStorageSync: () => '', hideLoading() {} },
    console,
  });
  const page = {
    ...definition,
    data: {
      ...definition.data,
      selectedStyleOptionKey: 'style:8',
      visualStyleOptions: [{
        key: 'style:8', sourceType: 'style', sourceId: '8', name: '新中式', imageUrl: 'https://cdn.example/preview.jpg',
        modelReferenceImageUrl: '', referenceMode: 'preview_and_model', modeLabel: '图像参考', promptSuffix: '',
      }],
    },
  };
  assert.equal(page.getSelectedVisualStyleImageUrl(), '');
  assert.equal(page.getCurrentScene(), 'ai_draw_multi');
});
