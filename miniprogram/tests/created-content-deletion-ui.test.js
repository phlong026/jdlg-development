'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');
const tick = () => new Promise(resolve => setImmediate(resolve));

function contentHarness(deleteCreatedContent) {
  let definition, actionSheet, modal;
  const storage = { token: 'account-a' }, navigations = [], toasts = [];
  const api = { deleteCreatedContent, workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'] }), listContents: async () => ({ items: [] }), mySharesContext: async () => ({ can_view: false }), importCapabilities: async () => ({ enabled: false }) };
  const wx = {
    getStorageSync: key => storage[key] || '', hideShareMenu() {}, navigateTo: value => navigations.push(value.url),
    showToast: value => toasts.push(value), showActionSheet: value => { actionSheet = value; }, showModal: value => { modal = value; },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.js'), 'utf8'), { Page: value => { definition = value; }, require: () => api, wx, Promise, Object, String, Number, Array, Map, Set, Date, Math, Error, encodeURIComponent, setTimeout, clearTimeout, console });
  const page = Object.assign({}, definition, { data: JSON.parse(JSON.stringify(definition.data)), setData(value) { Object.assign(this.data, value); } });
  page._visible = true; page._epoch = 1; page._token = storage.token;
  return { page, storage, navigations, toasts, actionSheet: () => actionSheet, modal: () => modal };
}

test('content long press confirms current revision, suppresses open, and removes only after success', async () => {
  const calls = [];
  const h = contentHarness(async (...args) => calls.push(args));
  h.page.setData({ items: [{ content_id: 'content-a', revision: 7, status: 'published', version: 2 }], visibleItems: [{ content_id: 'content-a', revision: 7 }] });
  const event = { currentTarget: { dataset: { id: 'content-a', status: 'published', version: 2 } } };
  h.page.deleteContentMenu(event);
  h.page.openContent(event);
  assert.equal(h.navigations.length, 0, '长按不应误打开详情');
  h.actionSheet().success({ tapIndex: 0 });
  h.modal().success({ confirm: false });
  assert.equal(calls.length, 0, '取消不发送删除');
  h.page.deleteContentMenu(event); h.actionSheet().success({ tapIndex: 0 }); h.modal().success({ confirm: true }); await tick();
  assert.deepEqual(calls[0], ['content-a', 7]);
  assert.equal(h.page.data.items.length, 0);
});

test('content deletion ignores old account callbacks and reloads on revision conflict', async () => {
  let finish;
  const h = contentHarness(() => new Promise((resolve, reject) => { finish = { resolve, reject }; }));
  h.page.setData({ items: [{ content_id: 'content-a', revision: 3 }], visibleItems: [{ content_id: 'content-a', revision: 3 }] });
  const pending = h.page.performContentDelete('content-a', 3, 1);
  h.storage.token = 'account-b'; finish.resolve({}); await pending;
  assert.equal(h.page.data.items.length, 1, '旧账号回调不得改写新账号页面');

  let reloads = 0;
  const conflict = contentHarness(async () => { throw Object.assign(new Error('冲突'), { statusCode: 409 }); });
  conflict.page.load = () => { reloads += 1; };
  conflict.page.setData({ items: [{ content_id: 'content-b', revision: 4 }], visibleItems: [{ content_id: 'content-b', revision: 4 }] });
  await conflict.page.performContentDelete('content-b', 4, 1);
  assert.equal(reloads, 1);
  assert.equal(conflict.page.data.items.length, 1);
});

test('all owned deletion surfaces expose a long-press entry', () => {
  const files = [
    ['pages/contentworkbench/contentworkbench.wxml', 'bindlongpress="deleteContentMenu"'],
    ['pages/projectcollections/projectcollections.wxml', 'bindlongpress="deleteCollectionMenu"'],
    ['pages/nativereferences/nativereferences.wxml', 'bindlongpress="referenceDeleteMenu"'],
    ['pages/release/release.wxml', 'bindlongpress="onWorkLongPress"'],
    ['pages/generatehistory/generatehistory.wxml', 'bindlongpress="onLongDelete"'],
  ];
  for (const [file, marker] of files) assert.match(fs.readFileSync(path.join(root, file), 'utf8'), new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
});

test('release and generation long-press handlers are registered on the page', () => {
  for (const [file, method] of [['pages/release/release.js', 'onWorkLongPress'], ['pages/generatehistory/generatehistory.js', 'onLongDelete']]) {
    let definition;
    vm.runInNewContext(fs.readFileSync(path.join(root, file), 'utf8'), {
      Page: value => { definition = value; }, wx: {}, console, Promise, Object, String, Number, Array, Date, Math, Error, Set, Map, encodeURIComponent, decodeURIComponent, setTimeout, clearTimeout,
      exports: {}, module: { exports: {} }, require: () => ({
        generateRequestParams: () => ({}), paramsToHeaders: value => value,
        getCachedDeviceFingerprint: () => '', generateDeviceFingerprint: async () => '', cacheDeviceFingerprint() {},
        resolveAssetPath: value => value,
        buildSelectedMap: () => ({}), getCurrentSelectableIds: () => [], isEveryCurrentSelected: () => false, toggleCurrentSelection: () => [],
      }),
    });
    assert.equal(typeof definition[method], 'function', `${file} missing ${method}`);
  }
});
