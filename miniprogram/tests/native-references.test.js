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

test('workbench media helper signs paginated server-side filters', async () => {
  const { api, calls } = loadAPI();
  await api.listWorkbenchMedia({ cursor: 'next page', limit: 20, q: '庭院', kind: 'image,video' });
  assert.equal(calls[0].header.route, '/api/v1/miniprogram/workbench/media?cursor=next%20page&limit=20&q=%E5%BA%AD%E9%99%A2&kind=image%2Cvideo');
});

function pageHarness(api, storage = { token: 'account-a' }) {
  let definition; const navigation = [], emitted = [], modals = [], actionSheets = [];
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/nativereferences/nativereferences.js'), 'utf8'), {
    Page: value => { definition = value; }, require: () => api,
    wx: { getStorageSync: key => storage[key], hideShareMenu() {}, navigateTo: value => navigation.push(value.url), navigateBack() {}, showModal: value => modals.push(value), showActionSheet: value => actionSheets.push(value) },
    Promise, Object, String, Number, Array, Set, Error, encodeURIComponent,
  });
  const page = Object.assign({ getOpenerEventChannel: () => ({ emit: (name, value) => emitted.push({ name, value }) }), data: JSON.parse(JSON.stringify(definition.data)), setData(value) { Object.assign(this.data, value); } }, definition);
  return { page, navigation, emitted, storage, modals, actionSheets };
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
test('reference removal requires confirmation and preserves the source wording', async () => {
  const removed = [];
  const { page, modals, actionSheets } = pageHarness({ deleteNativeReference: async id => removed.push(id) });
  page._visible = true; page._epoch = 1; page._token = 'account-a';
  page.setData({ references: [{ reference_id: 'ref-0001' }] });
  const event = { currentTarget: { dataset: { id: 'ref-0001' } } };
  page.referenceDeleteMenu(event); actionSheets[0].success({ tapIndex: 0 });
  assert.match(modals[0].content, /不会删除原作品/);
  modals[0].success({ confirm: false }); assert.equal(removed.length, 0);
  page.removeReference(event); modals[1].success({ confirm: true }); await tick();
  assert.deepEqual(removed, ['ref-0001']);
  assert.equal(page.data.references.length, 0);
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
test('same-account return keeps the reference library visible during refresh', async () => {
  let finishReferences;
  let referenceCalls = 0;
  const { page } = pageHarness({
    listNativeReferences: () => {
      referenceCalls += 1;
      if (referenceCalls === 1) return Promise.resolve({ items: [{ reference_id: 'ref-old' }] });
      return new Promise(resolve => { finishReferences = resolve; });
    },
    listNativeReferenceCandidates: async () => ({ items: [] }),
  });
  page.onShow(); await tick();
  assert.equal(page.data.references.length, 1);
  page.onHide(); page.onShow(); await tick();
  assert.equal(page.data.loading, false, '后台刷新不替换已显示主体');
  assert.equal(page.data.references[0].reference_id, 'ref-old');
  finishReferences({ items: [{ reference_id: 'ref-new' }] });
  await tick();
  assert.equal(page.data.references[0].reference_id, 'ref-new');
});
test('bounded reference window can still reach sources after the first two hundred', async () => {
  const { page } = pageHarness({ listNativeReferenceCandidates: async input => { assert.equal(input.cursor, 'next-page'); return { items: [{ content_id: 'source-0201', content_version: 1 }], next_cursor: 'later-page' }; } });
  page._visible = true; page._epoch = 1; page._token = 'account-a';
  page.data.candidates = Array.from({ length: 200 }, (_, index) => ({ content_id: 'previous-' + index, content_version: 1 }));
  page.data.candidateCursor = 'next-page';
  await page.loadMore({ currentTarget: { dataset: { kind: 'candidates' } } });
  assert.equal(page.data.candidates.length, 1); assert.equal(page.data.candidates[0].content_id, 'source-0201'); assert.equal(page.data.candidateCursor, 'later-page');
});

test('media picker uses server paging and returns the selected asset without creating a favorite', async () => {
  const calls = [];
  const asset = { media_id: 'media-0001', kind: 'image', title: '庭院图' };
  const { page, emitted } = pageHarness({
    listWorkbenchMedia: async query => { calls.push(query); return { items: [asset], next_cursor: 'media-next' }; },
  });
  page.onLoad({ picker: 'media', kind: 'image,video' });
  page.onShow(); await tick();
  assert.deepEqual(JSON.parse(JSON.stringify(calls[0])), { limit: 20, kind: 'image,video' });
  page.selectMedia({ currentTarget: { dataset: { id: 'media-0001' } } });
  assert.deepEqual(JSON.parse(JSON.stringify(emitted)), [{ name: 'selectMaterial', value: { kind: 'media', asset } }]);
});

test('content picker searches on the server and returns a fixed version without saving a reference', async () => {
  const calls = [], candidate = { content_id: 'content-0001', content_version: 3, title: '庭院内容' };
  const { page, emitted } = pageHarness({
    listNativeReferenceCandidates: async query => { calls.push(query); return { items: [candidate] }; },
    listNativeReferences: async () => ({ items: [] }),
  });
  page.onLoad({ picker: 'content' }); page.onShow(); await tick();
  page.onSearchInput({ detail: { value: '庭院' } }); await page.submitSearch();
  assert.equal(calls.at(-1).q, '庭院');
  page.selectContent({ currentTarget: { dataset: { id: 'content-0001', version: 3 } } });
  assert.deepEqual(JSON.parse(JSON.stringify(emitted)), [{ name: 'selectMaterial', value: { kind: 'content', target: candidate } }]);
});

test('repeated identical search coalesces in-flight reads while a new query may supersede them', async () => {
  let reads=0;const pending=[];
  const {page}=pageHarness({listNativeReferences:async()=>({items:[]}),listNativeReferenceCandidates:()=>{reads++;return new Promise(resolve=>pending.push(resolve));}});
  page.onShow();page.submitSearch();page.submitSearch();assert.equal(reads,1);
  page.setData({search:'庭院'});page.submitSearch();assert.equal(reads,2);
  pending.forEach(resolve=>resolve({items:[]}));await tick();assert.equal(page.data.searching,false);
});

test('returning to the library restarts a read invalidated on hide', async () => {
  let reads=0;const pending=[];const {page}=pageHarness({listNativeReferences:async()=>({items:[]}),listNativeReferenceCandidates:()=>{reads++;return new Promise(resolve=>pending.push(resolve));}});
  page.onShow();page.onHide();page.onShow();assert.equal(reads,2);
  pending[1]({items:[{content_id:'new'}]});await tick();pending[0]({items:[{content_id:'old'}]});await tick();assert.equal(page.data.candidates[0].content_id,'new');
});
