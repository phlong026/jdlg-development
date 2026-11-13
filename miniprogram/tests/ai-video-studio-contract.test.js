'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const rules = require('../miniprogram/utils/ai-video-studio');

test('reference limits consume server duration_seconds and reject unknown or non-finite media durations', () => {
  const input = [{ media_id: 'audio-1', media_type: 'audio', duration_seconds: 5 }];
  assert.equal(rules.validateReferences(input).ok, true);
  for (const duration of [NaN, Infinity, -Infinity]) {
    assert.equal(rules.validateReferences([{ media_id: 'audio-1', media_type: 'audio', duration_seconds: duration }]).code, 'reference_duration');
  }
});

test('saved material metadata is loaded from its own authenticated studio endpoint', async () => {
  const module = { exports: {} };
  let requested;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/utils/ai-video-studio-api.js'), 'utf8'), {
    module,
    require: () => ({ generateRequestParams: (_token, _body, requestPath) => ({ requestPath }), paramsToHeaders: value => value }),
    wx: {
      getStorageSync: () => 'test-account',
      request(options) { requested = options; options.success({ statusCode: 200, data: { code: 0, data: { media_id: 'owned-media', url: 'https://media.example.com/signed-image' } } }); },
    },
  });
  const item = await module.exports.getMedia('owned-media');
  assert.equal(item.media_id, 'owned-media');
  assert.equal(requested.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/ai-video-studio/media/owned-media');
  assert.equal(requested.header.requestPath, '/api/v1/miniprogram/ai-video-studio/media/owned-media');
});

test('creation cannot quote non-finite counts and durations', () => {
  const input = { mode: 'text', prompt: '庭院镜头', ratio: '16:9', resolution: '768p', duration: 8, quantity: 1 };
  assert.equal(rules.validateCreation(input).ok, true);
  for (const field of ['duration', 'quantity']) {
    for (const value of [NaN, Infinity, 0, -1, 1.5]) {
      assert.equal(rules.validateCreation({ ...input, [field]: value }).code, 'parameters', `${field}=${value}`);
    }
  }
});

function submissionAPI(storage, send) {
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/utils/ai-video-studio-api.js'), 'utf8'), {
    module,
    require: () => ({ generateRequestParams: () => ({}), paramsToHeaders: value => value }),
    wx: {
      getStorageSync: key => storage.get(key),
      setStorageSync: (key, value) => storage.set(key, value),
      removeStorageSync: key => storage.delete(key),
      request: send,
    },
  });
  return module.exports;
}

test('lost paid submission survives a page restart and replays the original key', async () => {
  const storage = new Map([['token', 'account-1-token'], ['userInfo', { id: 1001 }]]);
  const requests = [];
  let api = submissionAPI(storage, options => { requests.push(options); options.fail({ errMsg: 'timeout' }); });
  await assert.rejects(api.createBatch({ quote_id: 'quote-1', idempotency_key: 'first-key' }), /timeout/);
  assert.equal(api.pendingSubmission().idempotency_key, 'first-key');
  assert.equal(JSON.stringify(api.pendingSubmission()).includes('token'), false);
  api = submissionAPI(storage, options => {
    requests.push(options);
    options.success({ statusCode: 200, data: { code: 0, data: { batch_id: 'durable-batch' } } });
  });
  const batch = await api.createBatch({ quote_id: 'quote-1', idempotency_key: 'must-not-replace' });
  assert.equal(batch.batch_id, 'durable-batch');
  assert.equal(requests[1].data.idempotency_key, 'first-key');
  assert.equal(api.pendingSubmission(), null);
});

test('pending payment blocks a new quote and remains scoped to its account', async () => {
  const storage = new Map([['token', 'account-1-token'], ['userInfo', { id: 1001 }]]);
  let calls = 0;
  const api = submissionAPI(storage, options => { calls += 1; options.fail({ errMsg: 'timeout' }); });
  await assert.rejects(api.createBatch({ quote_id: 'quote-1', idempotency_key: 'original' }));
  await assert.rejects(api.createBatch({ quote_id: 'quote-2', idempotency_key: 'new-key' }), /核对/);
  assert.equal(calls, 1);
  storage.set('token', 'account-2-token'); storage.set('userInfo', { id: 1002 });
  assert.equal(api.pendingSubmission(), null);
  storage.set('token', 'account-1-token'); storage.set('userInfo', { id: 1001 });
  assert.equal(api.pendingSubmission().quote_id, 'quote-1');
});

test('definite insufficient balance clears pending payment while retaining caller input', async () => {
  const storage = new Map([['token', 'account-token'], ['userInfo', { id: 1001 }]]);
  const api = submissionAPI(storage, options => options.success({ statusCode: 402, data: { code: 402, msg: '灵石余额不足' } }));
  const input = { quote_id: 'quote-1', idempotency_key: 'key-1' };
  await assert.rejects(api.createBatch(input), /余额不足/);
  assert.equal(input.quote_id, 'quote-1');
  assert.equal(api.pendingSubmission(), null);
});

for (const lateStatus of [200, 402]) test(`late ${lateStatus} reply for A cannot remove the pending key for B`, async () => {
  const storage = new Map([['token', 'account-token'], ['userInfo', { id: 1001 }]]);
  const requests = [];
  const api = submissionAPI(storage, options => requests.push(options));
  const first = api.createBatch({ quote_id: 'quote-A', idempotency_key: 'key-A' });
  const replay = api.createBatch({ quote_id: 'quote-A', idempotency_key: 'key-A' });
  requests[1].success({ statusCode: 200, data: { code: 0, data: { batch_id: 'batch-A' } } });
  await replay;
  const second = api.createBatch({ quote_id: 'quote-B', idempotency_key: 'key-B' });
  requests[0].success(lateStatus === 200 ? { statusCode: 200, data: { code: 0, data: { batch_id: 'batch-A' } } } : { statusCode: 402, data: { code: 402, msg: '余额不足' } });
  if (lateStatus === 200) await first; else await assert.rejects(first);
  assert.equal(api.pendingSubmission().quote_id, 'quote-B');
  requests[2].fail({ errMsg: 'timeout' });
  await assert.rejects(second);
  assert.equal(api.pendingSubmission().idempotency_key, 'key-B');
});

test('automatic request recovery reads an existing batch without submitting paid work', async () => {
  const storage = new Map([['token', 'account-token'], ['userInfo', { id: 1001 }]]);
  const requests = [];
  let api = submissionAPI(storage, options => options.fail({ errMsg: 'timeout' }));
  await assert.rejects(api.createBatch({ quote_id: 'quote-A', idempotency_key: 'key-A' }));
  api = submissionAPI(storage, options => {
    requests.push(options);
    options.success({ statusCode: 200, data: { code: 0, data: { batch_id: 'batch-A' } } });
  });
  assert.equal((await api.getSubmission(api.pendingSubmission())).batch_id, 'batch-A');
  assert.equal(requests[0].method, 'GET');
  assert.match(requests[0].url, /\/submissions\/key-A$/);
  assert.equal(api.pendingSubmission(), null);
});

function detailPage(api, timers) {
  let page;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aivideostudio/detail.js'), 'utf8'), {
    Page: value => { page = value; },
    require: id => id.includes('ai-video-studio-api') ? api : {},
    wx: { getStorageSync: () => 'owner-token' },
    setTimeout: callback => { timers.push(callback); return timers.length; },
    clearTimeout: id => { timers[id - 1] = null; },
  });
  page.data = JSON.parse(JSON.stringify(page.data));
  page.setData = patch => Object.assign(page.data, patch);
  page.id = 'batch-1'; page._token = 'owner-token'; page._visible = true;
  return page;
}

test('detail polls durable server work, retains playback across refreshes and stops when hidden', async () => {
  const timers = [];
  let calls = 0;
  const api = { getBatch: async () => {
    calls += 1;
    return { batch_id: 'batch-1', status: 'running', results: [{ result_id: 'result-1', status: 'completed' }] };
  } };
  const page = detailPage(api, timers);
  await page.load();
  page.data.results[0].playback_url = 'https://media.example.com/signed-video';
  assert.equal(typeof timers[0], 'function');
  await timers[0]();
  assert.equal(calls, 2);
  assert.equal(page.data.results[0].playback_url, 'https://media.example.com/signed-video');
  page.onHide();
  assert.equal(timers.filter(Boolean).length, 1); // The first callback already ran; the pending timer was cleared.
  const late = await page.load(true);
  assert.equal(late, undefined);
  assert.equal(calls, 2);
});

test('completed detail stops polling and exposes the actual fee totals', async () => {
  const timers = [];
  const page = detailPage({ getBatch: async () => ({ batch_id: 'batch-1', status: 'partial_success', charge: { quoted_stones: 20, settled_stones: 10, released_stones: 10 }, results: [] }) }, timers);
  await page.load();
  assert.equal(timers.length, 0);
  assert.equal(page.data.batch.charge.settled_stones, 10);
  assert.equal(page.data.statusLabel, '部分完成');
});
