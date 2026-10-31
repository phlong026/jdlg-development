'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');

function loadAPI() {
  const calls = [], module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8'), {
    module, exports: module.exports, wx: {
      getStorageSync: key => key === 'token' ? 'test-session' : '',
      request(options) { calls.push(options); options.success({ statusCode: 200, data: { code: 0, data: {} } }); },
    }, require() { return { generateRequestParams: (_token, _data, route) => ({ route }), paramsToHeaders: value => ({ route: value.route }) }; },
    Promise, JSON, Error, Object, String, Number, Date, encodeURIComponent,
  });
  return { api: module.exports, calls };
}

test('native-reference helpers use signed source-version endpoints without accepting sharer metadata', async () => {
  const { api, calls } = loadAPI();
  await api.listNativeReferenceCandidates({ limit: 20 });
  await api.saveNativeReference('source-0001', 3, 'reference-attempt');
  await api.listNativeReferences();
  await api.getNativeReferenceTarget('reference-0001');
  await api.deleteNativeReference('reference-0001');
  assert.deepEqual(calls.map(item => item.header.route), [
    '/api/v1/miniprogram/workbench/native-reference-candidates?limit=20',
    '/api/v1/miniprogram/workbench/native-references',
    '/api/v1/miniprogram/workbench/native-references',
    '/api/v1/miniprogram/workbench/native-references/reference-0001/target',
    '/api/v1/miniprogram/workbench/native-references/reference-0001',
  ]);
  assert.deepEqual(JSON.parse(JSON.stringify(calls[1].data)), { content_id: 'source-0001', content_version: 3, idempotency_key: 'reference-attempt' });
  assert.equal(calls[4].method, 'DELETE');
});

function pageHarness(api, storage = { token: 'account-a' }) {
  let definition; const navigation = [];
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/nativereferences/nativereferences.js'), 'utf8'), {
    Page: value => { definition = value; }, require: () => api,
    wx: { getStorageSync: key => storage[key], hideShareMenu() {}, navigateTo: value => navigation.push(value.url) },
    Promise, Object, String, Number, Array, Set, Error, encodeURIComponent,
  });
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(value) { Object.assign(this.data, value); } }, definition);
  return { page, navigation, storage };
}
const tick = () => new Promise(resolve => setImmediate(resolve));
test('reference page preserves an idempotency attempt after a lost save response', async () => {
  const keys = []; let attempt = 0;
  const { page } = pageHarness({
    listNativeReferenceCandidates: async () => ({ items: [{ content_id: 'source-0001', content_version: 1 }] }),
    listNativeReferences: async () => ({ items: [] }), mutationKey: () => 'key-' + (++attempt),
    saveNativeReference: async (_id, _version, key) => { keys.push(key); if (keys.length === 1) throw new Error('响应丢失'); return { reference_id: 'ref-0001' }; },
  });
  page.onShow(); await tick();
  const event = { currentTarget: { dataset: { id: 'source-0001', version: 1 } } };
  await page.saveReference(event); await page.saveReference(event);
  assert.deepEqual(keys, ['key-1', 'key-1']);
});
test('reference page rejects late previous-account data and untrusted target paths', async () => {
  let finish; const api = {
    listNativeReferences: () => new Promise(resolve => { finish = resolve; }),
    listNativeReferenceCandidates: async () => ({ items: [] }),
    getNativeReferenceTarget: async () => ({ path: '/pages/projectnoteview/projectnoteview?pns=secret' }),
  };
  const { page, navigation, storage } = pageHarness(api);
  page.onShow(); await tick(); storage.token = ''; page.onShow();
  finish({ items: [{ reference_id: 'private-old' }] }); await tick();
  assert.equal(page.data.references.length, 0); assert.equal(page.data.loginRequired, true);
  storage.token = 'account-a'; page._token = storage.token; page._visible = true;
  await page.openReference({ currentTarget: { dataset: { id: 'ref-0001' } } });
  assert.equal(navigation.length, 0); assert.match(page.data.error, /目标/);
});
test('returning while a save is pending restores controls and keeps its retry key', async () => {
  let finish; const keys = [];
  const { page } = pageHarness({
    listNativeReferences: async () => ({ items: [] }),
    listNativeReferenceCandidates: async () => ({ items: [{ content_id: 'source-0001', content_version: 1 }] }),
    mutationKey: () => 'same-attempt',
    saveNativeReference: (_id, _version, key) => { keys.push(key); return keys.length === 1 ? new Promise(resolve => { finish = resolve; }) : Promise.resolve({ reference_id: 'same-ref' }); },
  });
  page.onShow(); await tick();
  const event = { currentTarget: { dataset: { id: 'source-0001', version: 1 } } };
  const old = page.saveReference(event); page.onHide(); page.onShow(); await tick();
  assert.equal(page.data.busy, false);
  await page.saveReference(event); finish({ reference_id: 'same-ref' }); await old;
  assert.deepEqual(keys, ['same-attempt', 'same-attempt']);
  assert.equal(page.data.references.length, 1);
});
test('bounded reference window can still reach sources after the first two hundred', async () => {
  const { page } = pageHarness({ listNativeReferenceCandidates: async input => { assert.equal(input.cursor, 'next-page'); return { items: [{ content_id: 'source-0201', content_version: 1 }], next_cursor: 'later-page' }; } });
  page._visible = true; page._epoch = 1; page._token = 'account-a';
  page.data.candidates = Array.from({ length: 200 }, (_, index) => ({ content_id: 'previous-' + index, content_version: 1 }));
  page.data.candidateCursor = 'next-page';
  await page.loadMore({ currentTarget: { dataset: { kind: 'candidates' } } });
  assert.equal(page.data.candidates.length, 1); assert.equal(page.data.candidates[0].content_id, 'source-0201'); assert.equal(page.data.candidateCursor, 'later-page');
});
