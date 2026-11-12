'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');

const pageRoot = path.join(__dirname, '../miniprogram/pages/contentworkbench');
const tick = () => new Promise(resolve => setImmediate(resolve));

function deferred() {
  let resolve;
  const promise = new Promise(yes => { resolve = yes; });
  return { promise, resolve };
}

function loadPage({ token = 'account-a', shareContext } = {}) {
  let definition;
  let storedToken = token;
  const navigations = [];
  const warnings = [];
  const source = fs.readFileSync(path.join(pageRoot, 'contentworkbench.ts'), 'utf8');
  const javascript = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  const api = {
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['designer-7'] }),
    listContents: async () => ({ items: [], next_cursor: '' }),
    importCapabilities: async () => ({ enabled: false, platforms: [] }),
    mySharesContext: shareContext || (async () => ({ can_view: true, owners: ['designer-7'] })),
  };
  vm.runInNewContext(javascript, {
    exports: {},
    Page(value) { definition = value; },
    require(id) {
      if (id === '../../utils/phase3-api') return api;
      throw new Error(`unexpected require: ${id}`);
    },
    wx: {
      getStorageSync(key) { return key === 'token' ? storedToken : ''; },
      hideShareMenu() {},
      navigateTo(options) { navigations.push(options.url); },
      showToast() {},
    },
    console: { warn(...args) { warnings.push(args); }, log() {}, error() {} },
    Promise, Date, Object, String, Number, Boolean, Array, Set, Map, JSON, Error,
    setTimeout, clearTimeout,
  });
  const page = Object.assign({}, definition, {
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) {
      for (const [key, value] of Object.entries(patch)) {
        const parts = key.split('.');
        let target = this.data;
        for (const part of parts.slice(0, -1)) target = target[part];
        target[parts.at(-1)] = value;
      }
    },
  });
  return {
    page, navigations, warnings,
    setToken(value) { storedToken = value; },
  };
}

test('兼容分享入口保持独立鉴权且不阻塞内容列表，页面使用稳定导航', async () => {
  const request = deferred();
  const harness = loadPage({ shareContext: () => request.promise });

  harness.page.onShow();
  await tick();
  await tick();

  assert.equal(harness.page.data.loading, false);
  assert.equal(harness.page.data.hasShareVisitAccess, false);

  request.resolve({ can_view: true, owners: ['designer-7'] });
  await tick();
  assert.equal(harness.page.data.hasShareVisitAccess, true);

  const template = fs.readFileSync(path.join(pageRoot, 'contentworkbench.wxml'), 'utf8');
  assert.doesNotMatch(template, /bindtap="openShareVisits"/);
  assert.match(template, /bindtap="openCollections"/);
  assert.match(template, /bindtap="openExistingAssets"/);
  harness.page.openShareVisits();
  assert.equal(harness.navigations.at(-1), '/pages/sharevisits/sharevisits?tab=shares');
});

test('无权限、空归属和权限请求失败时始终隐藏入口', async () => {
  const cases = [
    { response: async () => ({ can_view: false, owners: ['designer-7'] }), warningCount: 0 },
    { response: async () => ({ can_view: true, owners: [] }), warningCount: 0 },
    { response: async () => { throw Object.assign(new Error('forbidden'), { statusCode: 403 }); }, warningCount: 1 },
    { response: async () => { throw Object.assign(new Error('expired'), { statusCode: 401 }); }, warningCount: 1 },
  ];
  for (const { response, warningCount } of cases) {
    const harness = loadPage({ shareContext: response });
    harness.page.onShow();
    await tick();
    await tick();
    assert.equal(harness.page.data.hasShareVisitAccess, false);
    assert.equal(harness.page.data.loginRequired, false);
    assert.equal(harness.navigations.length, 0);
    assert.equal(harness.warnings.length, warningCount);
  }
});

test('页面隐藏或账号切换后的迟到响应不能恢复入口', async () => {
  const hiddenRequest = deferred();
  const hidden = loadPage({ shareContext: () => hiddenRequest.promise });
  hidden.page.onShow();
  hidden.page.onHide();
  hiddenRequest.resolve({ can_view: true, owners: ['designer-7'] });
  await tick();
  assert.equal(hidden.page.data.hasShareVisitAccess, false);

  const firstRequest = deferred();
  let calls = 0;
  const switched = loadPage({
    shareContext() {
      calls += 1;
      if (calls === 1) return firstRequest.promise;
      throw Object.assign(new Error('forbidden'), { statusCode: 403 });
    },
  });
  switched.page.onShow();
  switched.setToken('account-b');
  switched.page.onShow();
  await tick();
  firstRequest.resolve({ can_view: true, owners: ['designer-7'] });
  await tick();
  assert.equal(switched.page.data.hasShareVisitAccess, false);
});

test('点击分享记录时再次核对当前会话和权限', () => {
  const harness = loadPage();
  harness.page._visible = true;
  harness.page._epoch = 3;
  harness.page._token = 'account-a';

  harness.page.setData({ hasShareVisitAccess: false });
  harness.page.openShareVisits();
  assert.deepEqual(harness.navigations, []);

  harness.page.setData({ hasShareVisitAccess: true });
  harness.setToken('account-b');
  harness.page.openShareVisits();
  assert.deepEqual(harness.navigations, []);

  harness.setToken('account-a');
  harness.page.openShareVisits();
  assert.deepEqual(harness.navigations, ['/pages/sharevisits/sharevisits?tab=shares']);
});

test('内容搜索不会丢弃同一会话正在返回的分享权限', async () => {
  const request = deferred();
  const harness = loadPage({ shareContext: () => request.promise });
  harness.page.onShow();
  await tick();
  harness.page.setData({ keyword: '庭院' });
  await harness.page.searchContents();
  request.resolve({ can_view: true, owners: ['designer-7'] });
  await tick();
  assert.equal(harness.page.data.hasShareVisitAccess, true);
  harness.page.openShareVisits();
  assert.deepEqual(harness.navigations, ['/pages/sharevisits/sharevisits?tab=shares']);
});
