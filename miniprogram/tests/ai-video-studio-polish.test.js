'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const apiPath = require.resolve('../miniprogram/utils/ai-video-studio-api');
const generatePath = require.resolve('../miniprogram/pages/aivideostudio/generate.ts');
const polishPath = require.resolve('../miniprogram/pages/aivideostudio/polish.ts');

function loadPage(file, api) {
  const previous = require.cache[apiPath];
  const original = global.Page;
  let definition;
  require.cache[apiPath] = { id: apiPath, filename: apiPath, loaded: true, exports: api };
  delete require.cache[file];
  global.Page = value => { definition = value; };
  try { require(file); } finally {
    global.Page = original;
    if (previous) require.cache[apiPath] = previous; else delete require.cache[apiPath];
    delete require.cache[file];
  }
  return definition;
}

function mount(definition, data) {
  const page = Object.assign({}, definition);
  page.data = JSON.parse(JSON.stringify(Object.assign({}, definition.data, data || {})));
  page.setData = value => Object.assign(page.data, value);
  return page;
}

test.afterEach(() => { delete global.wx; });

function actualAPI(storage, send) {
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(require.resolve('../miniprogram/utils/ai-video-studio-api.ts'), 'utf8'), {
    module,
    require: () => ({ generateRequestParams: () => ({}), paramsToHeaders: value => value }),
    wx: { getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key), request: send },
  });
  return module.exports;
}

test('paid polish persists its original idempotency key and recovers through read-only lookup', async () => {
  const storage = new Map([['token', 'token-a'], ['userInfo', { id: 7 }]]);
  let requested;
  let api = actualAPI(storage, options => options.fail({ errMsg: 'timeout' }));
  await assert.rejects(api.createPolishTask({ quote_id: 'quote-1', idempotency_key: 'key-1' }), /timeout/);
  assert.equal(api.pendingPolish().idempotency_key, 'key-1');
  api = actualAPI(storage, options => { requested = options; options.success({ statusCode: 200, data: { code: 0, data: { task_id: 'task-1', status: 'submitting' } } }); });
  const task = await api.getPolishSubmission(api.pendingPolish());
  assert.equal(task.task_id, 'task-1');
  assert.match(requested.url, /prompt-polish\/submissions\/key-1$/);
  assert.equal(requested.method, 'GET');
  assert.equal(api.pendingPolish().task_id, 'task-1');
});

test('pending polish is account scoped and blocks replacement quotes without a second request', async () => {
  const storage = new Map([['token', 'token-a'], ['userInfo', { id: 7 }]]);
  let requests = 0;
  const api = actualAPI(storage, options => { requests += 1; options.fail({ errMsg: 'timeout' }); });
  await assert.rejects(api.createPolishTask({ quote_id: 'quote-1', idempotency_key: 'key-1' }));
  await assert.rejects(api.createPolishTask({ quote_id: 'quote-2', idempotency_key: 'key-2' }), /核对/);
  assert.equal(requests, 1);
  storage.set('token', 'token-b'); storage.set('userInfo', { id: 8 });
  assert.equal(api.pendingPolish(), null);
});

test('reading an old terminal task cannot clear a newer pending quote without a task id', async () => {
  const storage = new Map([['token', 'token-a'], ['userInfo', { id: 7 }], ['aivs:polish:7', { quote_id: 'quote-new', idempotency_key: 'key-new', task_id: '' }]]);
  const api = actualAPI(storage, options => options.success({ statusCode: 200, data: { code: 0, data: { task_id: 'task-old', quote_id: 'quote-old', status: 'completed' } } }));
  await api.getPolishTask('task-old');
  assert.equal(api.pendingPolish().quote_id, 'quote-new');
});

test('editor only creates a polish task after a visible matching price is confirmed twice', async () => {
  const calls = [];
  global.wx = { getStorageSync: key => key === 'token' ? 'account-a' : { id: 7, token: 'account-a' }, navigateTo: options => calls.push(['navigate', options]) };
  const api = {
    polishCapabilities: async () => ({ enabled: true, total_stones: 4, config_version: 2 }),
    createDraft: async input => Object.assign({ draft_id: 'draft-1', revision: 1 }, input),
    updateDraft: async (_id, input) => Object.assign({ draft_id: 'draft-1', revision: input.expected_revision + 1 }, input),
    polishQuote: async input => ({ quote_id: 'quote-1', draft_id: 'draft-1', draft_revision: input.expected_revision, input_hash: 'hash', total_stones: 5, quote_version: 3, expires_at: '2999-01-01T00:00:00Z' }),
    createPolishTask: async input => { calls.push(['create', input]); return { task_id: 'task-1' }; },
    idempotencyKey: () => 'key-1', pendingPolish: () => null,
  };
  const page = mount(loadPage(generatePath, api), { mode: 'text', prompt: '庭院向前推进', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, references: [], quote: { quote_id: 'old-video-quote' }, quoteReady: true, polishCapability: { enabled: true, total_stones: 4, config_version: 2 } });
  page._token = 'account-a'; page._sessionEpoch = 1; page._editVersion = 2;
  await page.startPolish();
  assert.equal(calls.some(item => item[0] === 'create'), false);
  assert.equal(page.data.quote, null);
  assert.equal(page.data.polishCapability.total_stones, 5);
  assert.match(page.data.notice, /价格已更新/);
  await page.startPolish();
  assert.equal(calls.filter(item => item[0] === 'create').length, 1);
  assert.match(calls.find(item => item[0] === 'navigate')[1].url, /task_id=task-1/);
});

test('pending polish is checked before any draft write or revision change', async () => {
  let writes = 0, checked = 0;
  global.wx = { getStorageSync: () => 'account-a' };
  const page = mount(loadPage(generatePath, { pendingPolish: () => ({ quote_id: 'pending', idempotency_key: 'old' }) }), { draft_id: 'draft-1', revision: 9, mode: 'text', prompt: '原文', references: [], polishPending: true, polishCapability: { enabled: true, total_stones: 4 } });
  page.persistDraft = async () => { writes += 1; };
  page.checkPolishPending = async () => { checked += 1; };
  await page.startPolish();
  assert.equal(checked, 1);
  assert.equal(writes, 0);
  assert.equal(page.data.revision, 9);
});

test('price refresh and definitive failure restore automatic video quote preparation', async () => {
  let scheduled = 0;
  global.wx = { getStorageSync: () => 'account-a' };
  const api = { pendingPolish: () => null, createDraft: async input => Object.assign({ draft_id: 'draft-1', revision: 1 }, input), updateDraft: async (_id, input) => Object.assign({ draft_id: 'draft-1', revision: input.expected_revision + 1 }, input), polishQuote: async input => ({ quote_id: 'q', total_stones: 5, quote_version: 2, draft_revision: input.expected_revision }) };
  const page = mount(loadPage(generatePath, api), { mode: 'text', prompt: '原文', references: [], ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, polishCapability: { enabled: true, total_stones: 4, config_version: 1 } });
  page._token = 'account-a'; page._sessionEpoch = 1; page._editVersion = 1; page.scheduleAutoQuote = () => { scheduled += 1; };
  await page.startPolish();
  assert.equal(scheduled, 1);
  assert.equal(page.data.quoteStatus, 'waiting');
  api.polishQuote = async () => { throw Object.assign(new Error('余额不足'), { statusCode: 402 }); };
  await page.startPolish();
  assert.equal(scheduled, 2);
  assert.match(page.data.error, /余额不足/);
});

test('quote network failure does not create or persist an unknown paid polish task', async () => {
  let creates = 0, scheduled = 0;
  global.wx = { getStorageSync: () => 'account-a' };
  const api = { pendingPolish: () => null, createDraft: async input => Object.assign({ draft_id: 'draft-1', revision: 1 }, input), polishQuote: async () => { throw Object.assign(new Error('quote timeout'), { networkError: true }); }, createPolishTask: async () => { creates += 1; } };
  const page = mount(loadPage(generatePath, api), { mode: 'text', prompt: '原文', references: [], ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, polishCapability: { enabled: true, total_stones: 4, config_version: 1 } });
  page._token = 'account-a'; page._sessionEpoch = 1; page._editVersion = 1; page.scheduleAutoQuote = () => { scheduled += 1; };
  await page.startPolish();
  assert.equal(creates, 0);
  assert.equal(page.data.polishPending, false);
  assert.equal(scheduled, 1);
  assert.match(page.data.error, /quote timeout/);
});

test('pending recovery adopts the task snapshot after revision-only autoquote but rejects different draft content', async () => {
  const navigations = [];
  global.wx = { getStorageSync: () => 'account-a', navigateTo: value => navigations.push(value) };
  const snapshot = { mode: 'text', prompt: '同一内容', reference_prompt: '', first_frame_media_id: '', last_frame_media_id: '', references: [], ratio: '16:9', resolution: '768p', duration: 5, quantity: 1 };
  const task = { task_id: 'task-1', draft_id: 'draft-1', draft_revision: 2, input_hash: 'hash-1', input_snapshot: snapshot, status: 'completed' };
  const api = { pendingPolish: () => ({ quote_id: 'q', idempotency_key: 'k', task_id: 'task-1' }), getPolishTask: async () => task };
  const page = mount(loadPage(generatePath, api), Object.assign({ draft_id: 'draft-1', revision: 4, polishPending: true }, snapshot));
  page._token = 'account-a'; page._sessionEpoch = 1; page._editVersion = 3;
  await page.checkPolishPending();
  navigations[0].events.adopted({ prompt: '同一内容的结果', draft_id: 'draft-1', draft_revision: 2, input_fingerprint: 'hash-1' });
  assert.equal(page.data.prompt, '同一内容的结果');

  const badNavigations = [];
  global.wx.navigateTo = value => badNavigations.push(value);
  const bad = mount(loadPage(generatePath, api), Object.assign({ draft_id: 'draft-1', revision: 4, polishPending: true }, snapshot, { prompt: '已经改过' }));
  bad._token = 'account-a'; bad._sessionEpoch = 1; bad._editVersion = 3;
  await bad.checkPolishPending();
  badNavigations[0].events.adopted({ prompt: '不应采用', draft_id: 'draft-1', draft_revision: 2, input_fingerprint: 'hash-1' });
  assert.equal(bad.data.prompt, '已经改过');
});

test('editor serializes polishing with save, quote and video submission and pauses hidden writes', async () => {
  global.wx = { getStorageSync: () => 'account-a' };
  const page = mount(loadPage(generatePath, { pendingPolish: () => null }), { polishing: true, quoting: false, saving: false, submitting: false, mode: 'text', prompt: 'x', references: [] });
  let writes = 0; page.persistDraft = async () => { writes += 1; };
  assert.equal(await page.saveDraft(), null);
  assert.equal(await page.refreshQuote(), null);
  page.onHide();
  page.data.polishing = false;
  page.scheduleAutoQuote();
  assert.equal(page._quoteTimer, null);
  assert.equal(writes, 0);
});

test('adoption callback changes only the matching prompt and invalidates the video quote', () => {
  const navigation = [];
  global.wx = { getStorageSync: () => 'account-a', navigateTo: options => navigation.push(options) };
  const page = mount(loadPage(generatePath, { pendingPolish: () => null }), { draft_id: 'draft-1', revision: 3, mode: 'reference', reference_prompt: '原文', references: [{ media_id: 'r1', media_type: 'image', purpose: '风格', position: 1 }], ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, quote: { quote_id: 'video' }, quoteReady: true });
  page._token = 'account-a'; page._sessionEpoch = 1; page._editVersion = 4;
  page.openPolishTask({ task_id: 'task-1', input_hash: 'hash' }, { editVersion: 4, draftID: 'draft-1', revision: 3, fingerprint: page.polishFingerprint() });
  navigation[0].events.adopted({ prompt: '润色结果', draft_id: 'draft-1', draft_revision: 3, input_fingerprint: 'hash' });
  assert.equal(page.data.reference_prompt, '润色结果');
  assert.equal(page.data.quote, null);
  assert.equal(page.data.prompt, '');
});

test('stale adoption is rejected after local edit, material change or account switch', () => {
  const navigation = [];
  let token = 'account-a';
  global.wx = { getStorageSync: () => token, navigateTo: options => navigation.push(options) };
  const page = mount(loadPage(generatePath, { pendingPolish: () => null }), { draft_id: 'draft-1', revision: 2, mode: 'text', prompt: '原文', references: [], ratio: '16:9', resolution: '768p', duration: 5, quantity: 1 });
  page._token = token; page._sessionEpoch = 1; page._editVersion = 1;
  const frozen = { editVersion: 1, draftID: 'draft-1', revision: 2, fingerprint: page.polishFingerprint() };
  page.openPolishTask({ task_id: 'task-1' }, frozen);
  page._editVersion = 2;
  navigation[0].events.adopted({ prompt: '迟到结果', draft_id: 'draft-1', draft_revision: 2, input_fingerprint: frozen.fingerprint });
  assert.equal(page.data.prompt, '原文');
  token = 'account-b';
  navigation[0].events.adopted({ prompt: '跨账号结果', draft_id: 'draft-1', draft_revision: 2, input_fingerprint: frozen.fingerprint });
  assert.equal(page.data.prompt, '原文');
});

test('polish page polls read-only, edits the result and emits adoption without generating video', async () => {
  let taskCalls = 0, generated = 0, emitted;
  global.wx = { getStorageSync: () => 'account-a', navigateBack: () => {} };
  const api = { getPolishTask: async () => { taskCalls += 1; return { task_id: 'task-1', draft_id: 'draft-1', draft_revision: 2, input_hash: 'hash', status: 'completed', original_prompt: '原文', optimized_prompt: '润色后', input_snapshot: { mode: 'text', references: [] } }; }, createBatch: () => { generated += 1; } };
  const page = mount(loadPage(polishPath, api));
  page.getOpenerEventChannel = () => ({ emit: (_name, value) => { emitted = value; } });
  page.onLoad({ task_id: 'task-1' });
  await Promise.resolve(); await Promise.resolve();
  assert.equal(page.data.result, '润色后');
  page.inputResult({ detail: { value: '用户编辑后的结果' } });
  page.adopt();
  assert.equal(emitted.prompt, '用户编辑后的结果');
  assert.equal(generated, 0);
  assert.equal(taskCalls, 1);
});

test('same polish page object rejects account A callbacks after loading account B', async () => {
  let token = 'account-a', resolveA, rejectA;
  const calls = [];
  global.wx = { getStorageSync: () => token };
  const api = { getPolishTask: id => id === 'task-a' ? new Promise((resolve, reject) => { resolveA = resolve; rejectA = reject; }) : Promise.resolve({ task_id: 'task-b', status: 'completed', optimized_prompt: 'B结果' }) };
  const page = mount(loadPage(polishPath, api));
  const originalSetData = page.setData; page.setData = value => { calls.push(value); originalSetData(value); };
  page.onLoad({ task_id: 'task-a' });
  token = 'account-b'; page.onLoad({ task_id: 'task-b' });
  await Promise.resolve(); await Promise.resolve();
  resolveA({ task_id: 'task-a', status: 'completed', optimized_prompt: 'A迟到结果' });
  await Promise.resolve(); await Promise.resolve();
  assert.equal(page.data.result, 'B结果');
  assert.equal(calls.some(value => value.result === 'A迟到结果'), false);
  rejectA(new Error('A迟到失败'));
});

test('cancel hidden callback cannot leave the next visible session permanently cancelling', async () => {
  let resolveCancel;
  global.wx = { getStorageSync: () => 'account-a' };
  const task = { task_id: 'task-1', status: 'queued' };
  const page = mount(loadPage(polishPath, { getPolishTask: async () => task, cancelPolishTask: () => new Promise(resolve => { resolveCancel = resolve; }) }), { task });
  page.onLoad({ task_id: 'task-1' }); await Promise.resolve(); await Promise.resolve();
  const cancelling = page.cancel(); await Promise.resolve();
  page.onHide(); page.onShow();
  assert.equal(page.data.cancelling, false);
  resolveCancel({ cancelled: true }); await cancelling;
  assert.equal(page.data.cancelling, false);
});

test('successful cancel clears busy before a failing reload', async () => {
  let reads = 0;
  global.wx = { getStorageSync: () => 'account-a' };
  const page = mount(loadPage(polishPath, { getPolishTask: async () => { reads += 1; if (reads > 1) throw new Error('reload failed'); return { task_id: 'task-1', status: 'queued' }; }, cancelPolishTask: async () => ({ cancelled: true }) }));
  page.onLoad({ task_id: 'task-1' }); await Promise.resolve(); await Promise.resolve();
  await page.cancel();
  assert.equal(page.data.cancelling, false);
  assert.match(page.data.error, /reload failed/);
});

test('result length counts Unicode code points without splitting emoji or silently truncating', () => {
  global.wx = { getStorageSync: () => 'account-a' };
  const page = mount(loadPage(polishPath, {}), { task: { status: 'completed', input_snapshot: { mode: 'text' } } });
  const exact = '😀'.repeat(7000);
  page.inputResult({ detail: { value: exact } });
  assert.equal(page.data.result, exact);
  assert.equal(page.data.resultLength, 7000);
  assert.equal(page.data.canAdopt, true);
  page.inputResult({ detail: { value: exact + '😀' } });
  assert.equal(page.data.resultLength, 7001);
  assert.equal(page.data.canAdopt, false);
  assert.match(page.data.error, /7000/);
});

test('uncertain polish task remains query-only and cannot be adopted', async () => {
  global.wx = { getStorageSync: () => 'account-a' };
  const page = mount(loadPage(polishPath, { getPolishTask: async () => ({ task_id: 'task-u', status: 'uncertain', original_prompt: '原文', optimized_prompt: '' }) }));
  page.onLoad({ task_id: 'task-u' });
  await Promise.resolve(); await Promise.resolve();
  page.stopPolling();
  assert.equal(page.data.canAdopt, false);
  assert.match(page.data.statusLabel, /核对/);
});
