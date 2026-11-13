'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const studio = require('../miniprogram/utils/ai-video-studio');

for (const extension of ['ts', 'js']) test(`workbench ${extension} exposes the four fixed designer entries and keeps the legacy video separate`, () => {
  const source = fs.readFileSync(path.resolve(__dirname, `../miniprogram/pages/my/my.${extension}`), 'utf8');
  const functionStart = source.indexOf('function buildWorkbenchMenuItems');
  const menuStart = source.indexOf(extension === 'ts' ? 'const items: MyMenuItem[]' : 'const items =', functionStart);
  const menu = source.slice(menuStart, source.indexOf('return items;', menuStart));
  assert.ok(menu.indexOf("action: 'myProjects'") < menu.indexOf("action: 'contentWorkbench'"));
  assert.ok(menu.indexOf("action: 'contentWorkbench'") < menu.indexOf("action: 'aiVideoStudio'"));
  assert.ok(menu.indexOf("action: 'aiVideoStudio'") < menu.indexOf("action: 'customers'"));
  assert.doesNotMatch(menu, /designerHome/);
  assert.match(source, /pages\/aivideostudio\/index/);
  assert.doesNotMatch(source.match(/if \(action === 'aiVideoStudio'\)[\s\S]{0,300}/)[0], /pages\/aivideo\/aivideo/);
});

test('reference validation accepts exact limits and rejects overflow and invalid duration', () => {
  const limits = studio.DEFAULT_LIMITS;
  const exact = [
    ...Array.from({ length: 8 }, (_, index) => ({ id: `i${index}`, type: 'image' })),
    ...Array.from({ length: 2 }, (_, index) => ({ id: `v${index}`, type: 'video', duration: 7.5 })),
    ...Array.from({ length: 2 }, (_, index) => ({ id: `a${index}`, type: 'audio', duration: 7.5 })),
  ];
  assert.equal(studio.validateReferences(exact, limits).ok, true);
  assert.equal(studio.validateReferences([...exact, { id: 'overflow', type: 'image' }], limits).code, 'reference_total_limit');
  assert.equal(studio.validateReferences([{ id: 'short', type: 'video', duration: 1 }], limits).code, 'reference_duration');
  assert.equal(studio.validateReferences([{ id: 'dup', type: 'image' }, { id: 'dup', type: 'image' }], limits).code, 'reference_duplicate');
});

test('four creation modes enforce their required stable material ids', () => {
  const base = { prompt: '乡村住宅清晨航拍', parameters: { ratio: '16:9', duration: 5, resolution: '768P', count: 1 } };
  assert.equal(studio.validateCreation({ ...base, mode: 'text' }).ok, true);
  assert.equal(studio.validateCreation({ ...base, mode: 'first_frame' }).code, 'first_frame_required');
  assert.equal(studio.validateCreation({ ...base, mode: 'first_frame', first_frame_id: 'm1' }).ok, true);
  assert.equal(studio.validateCreation({ ...base, mode: 'first_last_frame', first_frame_id: 'm1' }).code, 'last_frame_required');
  assert.equal(studio.validateCreation({ ...base, mode: 'first_last_frame', first_frame_id: 'm1', last_frame_id: 'm2' }).ok, true);
  assert.equal(studio.validateCreation({ ...base, mode: 'reference', reference_prompt: '', references: [{ id: 'm3', type: 'image' }] }).code, 'reference_prompt_required');
  assert.equal(studio.validateCreation({ ...base, mode: 'reference', reference_prompt: '参考图1控制风格', references: [{ id: 'm3', type: 'image' }] }).ok, true);
});

test('frame generation follows source aspect ratio while text generation never sends adaptive', () => {
  const ratios = ['adaptive', '16:9', '9:16'];
  assert.deepEqual(studio.ratioOptionsForMode('first_frame', ratios), ['adaptive']);
  assert.deepEqual(studio.ratioOptionsForMode('first_last_frame', ratios), ['adaptive']);
  assert.deepEqual(studio.ratioOptionsForMode('text', ratios), ['16:9', '9:16']);
  assert.deepEqual(studio.ratioOptionsForMode('reference', ratios), ratios);
});

test('quote is invalidated whenever billable input changes', () => {
  const draft = { mode: 'text', prompt: '原描述', parameters: { ratio: '16:9', duration: 5, resolution: '768P', count: 1 } };
  const quote = { quote_id: 'q1', input_fingerprint: studio.billableFingerprint(draft), expires_at: '2999-01-01T00:00:00Z' };
  assert.equal(studio.isQuoteUsable(quote, draft, Date.parse('2026-11-13T00:00:00Z')), true);
  assert.equal(studio.isQuoteUsable(quote, { ...draft, prompt: '新描述' }, Date.parse('2026-11-13T00:00:00Z')), false);
  assert.equal(studio.isQuoteUsable({ ...quote, expires_at: '2020-01-01T00:00:00Z' }, draft, Date.parse('2026-11-13T00:00:00Z')), false);
});

test('paid submission requires a server quote and idempotency key', () => {
  assert.equal(studio.buildBatchRequest({ draft_id: 'd1', revision: 2 }, null).code, 'quote_required');
  const request = studio.buildBatchRequest({ draft_id: 'd1', revision: 2 }, { quote_id: 'q1', input_fingerprint: 'fp' }, () => 'idem-1');
  assert.equal(request.ok, true);
  assert.deepEqual(request.value, { draft_id: 'd1', expected_revision: 2, quote_id: 'q1', input_fingerprint: 'fp', idempotency_key: 'idem-1' });
});

test('studio API signs the exact path with query and rejects a response after account switch', async () => {
  const apiPath = require.resolve('../miniprogram/utils/ai-video-studio-api');
  const parameterPath = require.resolve('../miniprogram/utils/parameter');
  const originalParameter = require.cache[parameterPath];
  const signed = [];
  require.cache[parameterPath] = { id: parameterPath, filename: parameterPath, loaded: true, exports: {
    generateRequestParams(token, body, requestPath) { signed.push({ token, body, requestPath }); return { requestPath }; },
    paramsToHeaders(value) { return value; },
  } };
  delete require.cache[apiPath];
  let token = 'account-a', pending, uploadOptions, progressHandler, uploadAborted = false;
  const apiStorage = new Map();
  global.wx = {
    getStorageSync(key) { return key === 'token' ? token : key === 'userInfo' ? { id: 1001 } : apiStorage.get(key); },
    setStorageSync(key, value) { apiStorage.set(key, value); }, removeStorageSync(key) { apiStorage.delete(key); },
    request(options) { pending = options; },
    uploadFile(options) { uploadOptions = options; return { onProgressUpdate(handler) { progressHandler = handler; }, abort() { uploadAborted = true; options.fail({ errMsg: 'uploadFile:fail abort' }); } }; },
  };
  const api = require(apiPath);
  const first = api.listDrafts({ page: 2, page_size: 20 });
  assert.equal(signed[0].requestPath, '/api/v1/miniprogram/ai-video-studio/drafts?page=2&page_size=20');
  pending.success({ statusCode: 200, data: { code: 0, data: { items: [] } } });
  await first;
  const stale = api.capabilities(); token = 'account-b';
  pending.success({ statusCode: 200, data: { code: 0, data: { enabled: true } } });
  await assert.rejects(stale, error => error.staleAccount === true);
  token = 'account-b'; let progress = 0;
  const upload = api.uploadMedia('/tmp/reference.mp3', event => { progress = event.progress; });
  assert.equal(uploadOptions.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/ai-video-studio/media/uploads');
  progressHandler({ progress: 35 }); assert.equal(progress, 35);
  upload.cancel(); assert.equal(uploadAborted, true);
  await assert.rejects(upload.promise, error => error.cancelled === true);
  assert.deepEqual(api.normalizeBatch({ input_snapshot: { mode: 'text' }, items: [{ id: 'result-1', stable_media_id: 'media-1' }] }).results[0], {
    id: 'result-1', result_id: 'result-1', stable_media_id: 'media-1', retention_status: 'retained',
  });
  const adoption = api.adoptContent('media-1', { draft_instance_id: 'draft-123456', owner_id: 'designer-1', title: '成片草稿' });
  assert.equal(pending.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/ai-video-studio/media/media-1/adopt/content');
  pending.success({ statusCode: 200, data: { code: 0, data: { content_id: 'content-1' } } });
  assert.equal((await adoption).content_id, 'content-1');
  delete global.wx; delete require.cache[apiPath];
  if (originalParameter) require.cache[parameterPath] = originalParameter; else delete require.cache[parameterPath];
});

test('task detail aborts hidden downloads and treats retained media as complete', async () => {
  const source = fs.readFileSync(path.resolve(__dirname, '../miniprogram/pages/aivideostudio/detail.ts'), 'utf8');
  let definition;
  vm.runInNewContext(source, {
    exports: {},
    Page(value) { definition = value; },
    require() { return { retain: async () => ({ media_id: 'retained-media-1' }) }; },
    wx: { getStorageSync: key => key === 'token' ? 'account-a' : '' },
    String, Number, Object, Array, Promise,
  });
  let aborted = 0;
  const page = Object.assign({}, definition, {
    setData(patch) { Object.assign(this.data, patch); },
    _token: 'account-a',
    _downloadTask: { abort() { aborted += 1; } },
  });
  page.data = { ...definition.data, results: [{ result_id: 'result-1', retained: false, stable_media_id: '' }] };
  page.onHide();
  assert.equal(aborted, 1);
  assert.equal(page._downloadTask, null);
  await page.retain({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(page.data.results[0].retained, true);
  assert.equal(page.data.results[0].retention_status, 'retained');
  assert.equal(page.data.results[0].stable_media_id, 'retained-media-1');
});

test('studio list pages reject late same-account responses and keep primary actions visible while loading', () => {
  const root = path.resolve(__dirname, '../miniprogram/pages/aivideostudio');
  const index = fs.readFileSync(path.join(root, 'index.ts'), 'utf8');
  const markup = fs.readFileSync(path.join(root, 'index.wxml'), 'utf8');
  assert.match(index, /epoch===this\._epoch/);
  assert.ok(markup.indexOf('新建创作') < markup.indexOf('wx:if="{{loading}}"'));
  const materials = fs.readFileSync(path.join(root, 'materials.ts'), 'utf8');
  assert.match(materials, /epoch!==this\._listEpoch/);
  for (const name of ['prompts', 'records', 'generate', 'detail']) {
    const source = fs.readFileSync(path.join(root, name + '.ts'), 'utf8');
    assert.match(source, /epoch\s*===\s*this\._epoch|epoch\s*===\s*this\._loadEpoch/, name);
  }
});
