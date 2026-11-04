'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');
const tick = () => new Promise(resolve => setImmediate(resolve));

function harness(api, options = {}) {
  let definition;
  const storage = options.storage || { token: 'account-a' };
  const timers = [];
  const navigations = [];
  const wx = {
    getStorageSync: key => storage[key] || '',
    hideShareMenu() {}, stopPullDownRefresh() {}, showToast() {},
    navigateTo: value => navigations.push(value.url),
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.js'), 'utf8'), {
    Page: value => { definition = value; }, wx,
    require: id => id === '../../utils/phase3-api' ? api : (() => { throw new Error('unexpected require ' + id); })(),
    setTimeout(callback, delay) { const timer = { callback, delay, cleared: false }; timers.push(timer); return timer; },
    clearTimeout(timer) { if (timer) timer.cleared = true; },
    console, Promise, JSON, Error, Object, String, Number, Date, Map, Set, encodeURIComponent,
  });
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values) { Object.assign(this.data, values); } }, definition);
  return { page, storage, timers, navigations };
}

function baseAPI(overrides = {}) {
  let sequence = 0;
  return Object.assign({
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a', 'owner-b'] }),
    listContents: async () => ({ items: [] }),
    importCapabilities: async () => ({ enabled: true, platforms: [{ platform: 'wechat_article', available: true, reason: '图文可导入' }] }),
    listImports: async () => ({ items: [] }),
    mutationKey: prefix => prefix + '-' + (++sequence),
  }, overrides);
}

test('import API uses the authenticated workbench import routes', async () => {
  const calls = [];
  const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');
  const module = { exports: {} };
  const wx = {
    getStorageSync: key => key === 'token' ? 'session-token' : 'device-one',
    request(options) { calls.push(options); options.success({ statusCode: 200, data: { code: 0, data: { items: [] } } }); },
  };
  vm.runInNewContext(source, { module, exports: module.exports, wx, require: () => ({ generateRequestParams: (_t, _b, apiPath) => ({ apiPath }), paramsToHeaders: value => ({ signedPath: value.apiPath }) }), Promise, JSON, Error, Object, String, Number, encodeURIComponent });
  await module.exports.importCapabilities();
  await module.exports.listImports();
  await module.exports.submitImport({ source_url: 'https://mp.weixin.qq.com/s/example', owner_id: 'owner-a', rights: 'own', idempotency_key: 'import-1' });
  await module.exports.getImport('job one');
  await module.exports.retryImport('job one');
  assert.deepEqual(calls.map(call => call.method), ['GET', 'GET', 'POST', 'GET', 'POST']);
  assert.deepEqual(calls.map(call => call.header.signedPath), [
    '/api/v1/miniprogram/workbench/imports/capabilities', '/api/v1/miniprogram/workbench/imports',
    '/api/v1/miniprogram/workbench/imports', '/api/v1/miniprogram/workbench/imports/job%20one',
    '/api/v1/miniprogram/workbench/imports/job%20one/retry',
  ]);
});

test('submission keeps one idempotency key after a lost response and changes it with semantic input', async () => {
  const keys = [];
  let calls = 0;
  const { page } = harness(baseAPI({
    submitImport: async input => { keys.push(input.idempotency_key); if (++calls === 1) throw new Error('响应丢失'); return { import_id: 'job-one', status: 'queued', owner_id: input.owner_id }; },
  }));
  page.onShow(); await tick(); await tick();
  page.setImportURL({ detail: { value: 'https://mp.weixin.qq.com/s/example' } });
  page.setImportRights({ detail: { value: 'own' } });
  await page.submitImport();
  await page.submitImport();
  page.setImportURL({ detail: { value: 'https://v.douyin.com/new' } });
  await page.submitImport();
  assert.deepEqual(keys, ['content-import-1', 'content-import-1', 'content-import-2']);
  assert.equal(page.data.imports[0].status, 'queued');
});

test('pending tasks poll only while visible and discard a late response after hide or account switch', async () => {
  let resolvePoll;
  let listCalls = 0;
  const runtime = harness(baseAPI({
    listImports: async () => {
      listCalls += 1;
      if (listCalls === 1) return { items: [{ import_id: 'job-pending', status: 'queued' }] };
      return new Promise(resolve => { resolvePoll = resolve; });
    },
  }));
  runtime.page.onShow(); await tick(); await tick();
  const timer = runtime.timers.find(value => !value.cleared);
  assert.ok(timer);
  timer.callback(); await tick();
  runtime.page.onHide();
  resolvePoll({ items: [{ import_id: 'job-stale', status: 'ready', content_id: 'content-stale' }] });
  await tick();
  assert.equal(runtime.page.data.imports[0].import_id, 'job-pending');

  runtime.storage.token = 'account-b';
  runtime.page.onShow(); await tick(); await tick();
  assert.equal(runtime.page._token, 'account-b');
});

test('a delayed previous-account list cannot block or clear the current-account refresh', async () => {
  let resolveA, resolveB;
  let calls = 0;
  const runtime = harness(baseAPI({
    listImports: async () => {
      calls += 1;
      return new Promise(resolve => { if (calls === 1) resolveA = resolve; else resolveB = resolve; });
    },
  }));
  runtime.page.onShow(); await tick(); await tick();
  assert.equal(calls, 1);
  runtime.page.onHide(); runtime.storage.token = 'account-b'; runtime.page.onShow(); await tick(); await tick();
  assert.equal(calls, 2);
  const currentEpoch = runtime.page._epoch;
  resolveA({ items: [{ import_id: 'job-a', status: 'ready' }] }); await tick();
  assert.equal(runtime.page._importPollEpoch, currentEpoch);
  assert.equal(runtime.page.data.importPolling, true);
  resolveB({ items: [{ import_id: 'job-b', status: 'ready' }] }); await tick();
  assert.equal(runtime.page._importPollEpoch, null);
  assert.equal(runtime.page.data.importPolling, false);
  assert.equal(runtime.page.data.imports[0].import_id, 'job-b');
});

test('account change while visible blocks submit, retry, and draft navigation before side effects', async () => {
  let submits = 0, retries = 0;
  const runtime = harness(baseAPI({
    submitImport: async () => { submits += 1; },
    retryImport: async () => { retries += 1; },
  }));
  runtime.page.onShow(); await tick(); await tick();
  runtime.page.setData({ importEnabled: true, importURL: 'https://mp.weixin.qq.com/s/example', importOwner: 'owner-a', importRights: 'own', imports: [
    { import_id: 'job-safe', status: 'failed', can_retry: true },
    { import_id: 'job-ready', status: 'ready', content_id: 'content-one' },
  ] });
  runtime.storage.token = 'account-b';
  await runtime.page.submitImport();
  await runtime.page.retryImport({ currentTarget: { dataset: { id: 'job-safe' } } });
  runtime.page.openImport({ currentTarget: { dataset: { id: 'job-ready' } } });
  assert.equal(submits, 0);
  assert.equal(retries, 0);
  assert.deepEqual(runtime.navigations, []);
});

test('uncertain tasks never retry while safe retry and ready draft actions remain explicit', async () => {
  const retried = [];
  const runtime = harness(baseAPI({ retryImport: async id => { retried.push(id); return { import_id: id, status: 'queued' }; } }));
  runtime.page._visible = true; runtime.page._token = 'account-a'; runtime.page._epoch = 1;
  runtime.page.setData({ imports: [
    { import_id: 'job-uncertain', status: 'uncertain', can_retry: true },
    { import_id: 'job-safe', status: 'failed', can_retry: true },
    { import_id: 'job-ready', status: 'ready', content_id: 'content-one' },
  ] });
  await runtime.page.retryImport({ currentTarget: { dataset: { id: 'job-uncertain' } } });
  runtime.page.openImport({ currentTarget: { dataset: { id: 'job-ready' } } });
  await runtime.page.retryImport({ currentTarget: { dataset: { id: 'job-safe' } } });
  assert.deepEqual(retried, ['job-safe']);
  assert.deepEqual(runtime.navigations, ['/pages/contentedit/contentedit?id=content-one']);
});

test('disabled import capability leaves normal content workbench usable and explains the gate', async () => {
  let listCalls = 0;
  const runtime = harness(baseAPI({
    importCapabilities: async () => ({ enabled: false, reason: '链接导入暂未开通', platforms: [] }),
    listImports: async () => { listCalls += 1; throw Object.assign(new Error('内容导入服务暂不可用'), { statusCode: 503 }); },
  }));
  const { page } = runtime;
  page.onShow(); await tick(); await tick();
  assert.equal(page.data.disabled, false);
  assert.equal(page.data.importEnabled, false);
  assert.equal(page.data.importServiceEnabled, false);
  assert.equal(page.data.importCapabilityReason, '链接导入暂未开通');
  assert.equal(listCalls, 0);
  assert.equal(runtime.timers.some(timer => !timer.cleared), false);
  await page.refreshImportList();
  assert.equal(listCalls, 0);
  const template = fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.match(template, /wx:if="\{\{importServiceEnabled\}\}"[^>]*class="recent-imports"/);
  assert.match(template, /wx:elif="\{\{!importEnabled\}\}"[^>]*class="import-gate"/);
  assert.match(template, /wx:else[^>]*class="import-editor"/);
});

test('repeated platform reasons collapse into one short import gate', async () => {
  const runtime = harness(baseAPI({
    importCapabilities: async () => ({
      enabled: true,
      platforms: ['wechat_article', 'xiaohongshu', 'channels', 'douyin'].map(platform => ({ platform, available: false, reason: '该平台导入服务尚未开通' })),
    }),
  }));
  runtime.page.onShow(); await tick(); await tick();
  assert.equal(runtime.page.data.importCapabilityReason, '该平台导入服务尚未开通');
});

test('capability failure uses one short gate instead of a second inline error', async () => {
  const runtime = harness(baseAPI({
    importCapabilities: async () => { throw new Error('导入能力加载失败'); },
  }));
  runtime.page.onShow(); await tick(); await tick();
  assert.equal(runtime.page.data.importCapabilityReason, '链接导入能力暂不可用');
  assert.equal(runtime.page.data.importError, '');
});

test('temporary import list failure keeps the existing retry polling behavior', async () => {
  let listCalls = 0;
  const runtime = harness(baseAPI({
    listImports: async () => { listCalls += 1; throw Object.assign(new Error('导入服务暂时繁忙'), { statusCode: 503 }); },
  }));
  runtime.page.onShow(); await tick(); await tick();
  assert.equal(runtime.page.data.importServiceEnabled, true);
  assert.equal(listCalls, 1);
  assert.equal(runtime.page.data.importError, '导入服务暂时繁忙');
  assert.equal(runtime.timers.some(timer => !timer.cleared), true);
});

test('import form names the tracking owner as content ownership rather than a business project', () => {
  const template = fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.match(template, /内容归属/);
  assert.doesNotMatch(template, /归属项目/);
});

test('expired media can refill the original source but requires rights and a new idempotency key', async () => {
  const keys = [];
  const sourceURL = 'https://mp.weixin.qq.com/s/expired-example';
  const runtime = harness(baseAPI({
    submitImport: async input => { keys.push(input.idempotency_key); return { import_id: 'job-new', owner_id: input.owner_id, source_url: input.source_url, status: 'queued' }; },
  }));
  runtime.page.onShow(); await tick(); await tick();
  runtime.page._importSignature = [sourceURL, 'owner-a', 'own'].join('\n');
  runtime.page._importKey = 'old-terminal-key';
  runtime.page.setData({ imports: [{ import_id: 'job-expired', owner_id: 'owner-a', source_url: sourceURL, status: 'blocked', error_code: 'media_source_expired' }] });
  runtime.page.prepareReimport({ currentTarget: { dataset: { id: 'job-expired' } } });
  assert.equal(runtime.page.data.importURL, sourceURL);
  assert.equal(runtime.page.data.importOwner, 'owner-a');
  assert.equal(runtime.page.data.importRights, '');
  assert.match(runtime.page.data.importError, /确认素材权属/);
  assert.equal(runtime.page._importKey, '');
  await runtime.page.submitImport();
  assert.equal(keys.length, 0, 'rights must be explicitly confirmed again');
  runtime.page.setImportRights({ detail: { value: 'own' } });
  await runtime.page.submitImport();
  assert.deepEqual(keys, ['content-import-1']);
  assert.notEqual(keys[0], 'old-terminal-key');
});

test('uncertain or unrelated failures cannot use the expired-media restart action', async () => {
  const runtime = harness(baseAPI());
  runtime.page.onShow(); await tick(); await tick();
  runtime.page.setData({ importURL: 'keep-me', importOwner: 'owner-b', importRights: 'authorized', imports: [
    { import_id: 'job-unknown', owner_id: 'owner-a', source_url: 'https://v.douyin.com/unknown', status: 'uncertain', error_code: 'media_source_expired' },
    { import_id: 'job-invalid', owner_id: 'owner-a', source_url: 'https://v.douyin.com/invalid', status: 'failed', error_code: 'invalid_media' },
  ] });
  runtime.page.prepareReimport({ currentTarget: { dataset: { id: 'job-unknown' } } });
  runtime.page.prepareReimport({ currentTarget: { dataset: { id: 'job-invalid' } } });
  assert.equal(runtime.page.data.importURL, 'keep-me');
  assert.equal(runtime.page.data.importOwner, 'owner-b');
  assert.equal(runtime.page.data.importRights, 'authorized');
  const template = fs.readFileSync(path.join(root, 'pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.match(template, /item\.error_code === 'media_source_expired'/);
});

test('retry patches returned queued task and keeps polling during an older list request', async () => {
 let finishList;
 const runtime=harness(baseAPI({listImports:()=>new Promise(resolve=>{finishList=resolve;}),retryImport:async()=>({import_id:'job-retry',owner_id:'owner-a',status:'queued',can_retry:false})}));
 const page=runtime.page;page._visible=true;page._token='account-a';page._epoch=1;
 page.setData({importServiceEnabled:true,imports:[{import_id:'job-retry',status:'failed',can_retry:true}]});
 const inflight=page.refreshImports(1);await tick();
 await page.retryImport({currentTarget:{dataset:{id:'job-retry'}}});
 assert.equal(page.data.imports[0].status,'queued');
 assert.ok(runtime.timers.some(timer=>!timer.cleared),'requeued task has no scheduled refresh');
 finishList({items:[{import_id:'job-retry',status:'failed',can_retry:true}]});await inflight;
 assert.equal(page.data.imports[0].status,'queued','late list overwrote the requeued task');
 assert.ok(runtime.timers.some(timer=>!timer.cleared),'late response stopped polling');
});
