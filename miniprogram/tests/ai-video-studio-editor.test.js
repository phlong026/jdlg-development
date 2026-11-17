'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const pagePath = require.resolve('../miniprogram/pages/aivideostudio/generate');
const apiPath = require.resolve('../miniprogram/utils/ai-video-studio-api');
const rulesPath = require.resolve('../miniprogram/utils/ai-video-studio');

function loadPage(apiOverrides = {}) {
  const originalApi = require.cache[apiPath];
  const originalRules = require.cache[rulesPath];
  const originalPage = global.Page;
  let definition;
  const api = Object.assign({
    capabilities: async () => ({}),
    getDraft: async () => ({}),
    getMedia: async mediaID => ({ media_id: mediaID }),
    createDraft: async input => Object.assign({ draft_id: 'draft-1', revision: 1 }, input),
    updateDraft: async (_id, input) => Object.assign({ draft_id: 'draft-1', revision: input.expected_revision + 1 }, input),
    quote: async () => ({ quote_id: 'quote-1', total_stones: 10, billable_units: 1, expires_at: '2999-01-01T00:00:00Z' }),
    createBatch: async () => ({ batch_id: 'batch-1' }),
    idempotencyKey: () => 'idem-1',
  }, apiOverrides);
  require.cache[apiPath] = { id: apiPath, filename: apiPath, loaded: true, exports: api };
  delete require.cache[pagePath];
  global.Page = value => { definition = value; };
  require(pagePath);
  global.Page = originalPage;
  if (originalApi) require.cache[apiPath] = originalApi; else delete require.cache[apiPath];
  if (originalRules) require.cache[rulesPath] = originalRules; else delete require.cache[rulesPath];
  delete require.cache[pagePath];
  return definition;
}

function instance(definition, patch = {}) {
  const page = Object.assign({}, definition, patch);
  page.data = JSON.parse(JSON.stringify(Object.assign({}, definition.data, patch.data || {})));
  page.setData = values => Object.assign(page.data, values);
  page.current = () => true;
  return page;
}

test('text and image editor derives mode from frame presence and never allows a last frame alone', () => {
  const page = instance(loadPage(), { data: { capabilities: { ratios: ['adaptive', '16:9'] }, resolution: '768P', firstFrame: {}, lastFrame: {} } });
  page.applyMaterial('first_frame', { media_id: 'first', media_type: 'image', name: '首帧', url: 'https://example.com/first.jpg' });
  assert.equal(page.data.mode, 'first_frame');
  page.applyMaterial('last_frame', { media_id: 'last', media_type: 'image', name: '尾帧', url: 'https://example.com/last.jpg' });
  assert.equal(page.data.mode, 'first_last_frame');
  page.removeFrame({ currentTarget: { dataset: { target: 'last_frame' } } });
  assert.equal(page.data.mode, 'first_frame');
  page.removeFrame({ currentTarget: { dataset: { target: 'first_frame' } } });
  assert.equal(page.data.mode, 'text');
  assert.equal(page.data.last_frame_media_id, '');
});

test('switching tabs preserves independent prompts and materials', () => {
  const page = instance(loadPage(), { data: { activeTab: 'creation', prompt: '文图描述', reference_prompt: '', references: [], firstFrame: { media_id: 'first' } } });
  page._tabStates = {};
  page.switchTab({ currentTarget: { dataset: { tab: 'reference' } } });
  page.inputField({ currentTarget: { dataset: { field: 'reference_prompt' } }, detail: { value: '参考描述' } });
  page.applyMaterial('reference', { media_id: 'ref-1', media_type: 'image', name: '参考图' });
  page.switchTab({ currentTarget: { dataset: { tab: 'creation' } } });
  assert.equal(page.data.prompt, '文图描述');
  assert.equal(page.data.firstFrame.media_id, 'first');
  page.switchTab({ currentTarget: { dataset: { tab: 'reference' } } });
  assert.equal(page.data.reference_prompt, '参考描述');
  assert.equal(page.data.references[0].media_id, 'ref-1');
});

test('reference media uses real duration fields and removal makes positions continuous with a review warning', () => {
  const page = instance(loadPage(), { data: { activeTab: 'reference', mode: 'reference', reference_prompt: '参考音频1和音频2', references: [], limits: { audio_max: 3, video_max: 3, image_max: 8, total_max: 12, media_duration_min: 2, media_duration_max: 15, video_total_duration_max: 15, audio_total_duration_max: 15 } } });
  page.applyMaterial('reference', { media_id: 'audio-1', media_type: 'audio', duration_seconds: 5 });
  page.applyMaterial('reference', { media_id: 'audio-2', media_type: 'audio', duration_seconds: 6 });
  assert.deepEqual(page.data.references.map(item => [item.media_id, item.duration, item.position]), [['audio-1', 5, 1], ['audio-2', 6, 2]]);
  page.removeReference({ currentTarget: { dataset: { id: 'audio-1' } } });
  assert.equal(page.data.references[0].position, 1);
  assert.match(page.data.notice, /重新核对/);
});

test('reference display indexes count each provider media type independently', () => {
  const page = instance(loadPage(), { data: { activeTab: 'reference', mode: 'reference', references: [], limits: { audio_max: 3, video_max: 3, image_max: 8, total_max: 12, media_duration_min: 2, media_duration_max: 15, video_total_duration_max: 15, audio_total_duration_max: 15 } } });
  page.applyMaterial('reference', { media_id: 'image-1', media_type: 'image' });
  page.applyMaterial('reference', { media_id: 'video-1', media_type: 'video', duration_seconds: 5, url: 'https://example.com/video.mp4' });
  page.applyMaterial('reference', { media_id: 'audio-1', media_type: 'audio', duration_seconds: 5 });
  assert.deepEqual(page.data.references.map(item => [item.media_type, item.position, item.display_index]), [
    ['image', 1, 1], ['video', 2, 1], ['audio', 3, 1],
  ]);
});

test('complete mixed reference input keeps durations for validation and automatic quote', async () => {
  let quoteCalls = 0;
  const page = instance(loadPage({ quote: async () => { quoteCalls += 1; return { quote_id: 'mixed', total_stones: 16, expires_at: '2999-01-01T00:00:00Z' }; } }), { data: {
    capabilities: { enabled: true, reference_limits: { image_max: 8, video_max: 3, audio_max: 3, total_max: 12, media_duration_min: 2, media_duration_max: 15, video_total_duration_max: 15, audio_total_duration_max: 15 } },
    selectedModeEnabled: true, activeTab: 'reference', mode: 'reference', reference_prompt: '图片1控制风格，视频1控制镜头，音频1保留环境声',
    ratio: '16:9', resolution: '768p', duration: 5, quantity: 1,
    references: [
      { media_id: 'image-1', media_type: 'image', position: 1, duration: 0 },
      { media_id: 'video-1', media_type: 'video', position: 2, duration: 5 },
      { media_id: 'audio-1', media_type: 'audio', position: 3, duration: 5 },
    ],
  } });
  assert.equal(page.generationReady(), true);
  await page.refreshQuote();
  assert.equal(quoteCalls, 1);
  assert.equal(page.data.quoteReady, true);
});

test('restoring an id-only frame draft fetches owned media metadata for its preview', async () => {
  const page = instance(loadPage({
    getDraft: async () => ({ draft_id: 'draft-frame', revision: 1, mode: 'first_frame', prompt: '首帧生成', first_frame_media_id: 'frame-1', last_frame_media_id: '', references: [], ratio: 'adaptive', resolution: '768p', duration: 5, quantity: 1 }),
    getMedia: async mediaID => ({ media_id: mediaID, media_type: 'image', url: 'https://example.com/frame.jpg', name: '真实首帧' }),
  }), { data: { capabilities: { ratios: ['adaptive'], resolutions: ['768p'] }, modeOptions: [{ code: 'first_frame', enabled: true }] }, scheduleAutoQuote() {} });
  page._editVersion = 0;
  page._tabStates = {};
  await page.restoreDraft('draft-frame', 1);
  assert.equal(page.data.first_frame_media_id, 'frame-1');
  assert.equal(page.data.firstFrame.preview_url, 'https://example.com/frame.jpg');
});

test('late frame metadata cannot overwrite an edited draft', async () => {
  let resolveMedia;
  const page = instance(loadPage({
    getDraft: async () => ({ draft_id: 'draft-late', revision: 1, mode: 'first_frame', prompt: '旧内容', first_frame_media_id: 'old-frame', references: [], ratio: 'adaptive', resolution: '768p', duration: 5, quantity: 1 }),
    getMedia: () => new Promise(resolve => { resolveMedia = resolve; }),
  }), { data: { capabilities: { ratios: ['adaptive'], resolutions: ['768p'] }, modeOptions: [{ code: 'first_frame', enabled: true }] }, scheduleAutoQuote() {} });
  page._editVersion = 0;
  page._tabStates = {};
  const restoring = page.restoreDraft('draft-late', 1);
  await Promise.resolve();
  await Promise.resolve();
  page._editVersion = 1;
  resolveMedia({ media_id: 'old-frame', media_type: 'image', url: 'https://example.com/late.jpg' });
  await restoring;
  assert.notEqual(page.data.firstFrame.preview_url, 'https://example.com/late.jpg');
});

test('draft payload only contains the active branch and keeps server parameter values unchanged', () => {
  const page = instance(loadPage(), { data: {
    activeTab: 'reference', mode: 'reference', prompt: '不应提交', reference_prompt: '参考描述',
    first_frame_media_id: 'old-frame', last_frame_media_id: 'old-tail',
    references: [{ media_id: 'ref-1', media_type: 'image', purpose: '风格', position: 1 }],
    ratio: '16:9', resolution: '2k', duration: 8, quantity: 1,
  } });
  const reference = page.draftInput();
  assert.equal(reference.prompt, '');
  assert.equal(reference.first_frame_media_id, '');
  assert.equal(reference.last_frame_media_id, '');
  assert.equal(reference.resolution, '2k');
  page.data.mode = 'text';
  page.data.prompt = '文生描述';
  assert.deepEqual(page.draftInput().references, []);
  assert.equal(page.draftInput().reference_prompt, '');
});

test('saving a draft does not require generation-ready inputs', async () => {
  let savedInput;
  const page = instance(loadPage({ createDraft: async input => { savedInput = input; return { draft_id: 'draft-empty', revision: 1 }; } }), {
    data: { mode: 'text', prompt: '', name: '未完成草稿', ratio: '16:9', resolution: '768P', duration: 5, quantity: 1, references: [] },
  });
  await page.saveDraft();
  assert.equal(savedInput.prompt, '');
  assert.equal(page.data.draft_id, 'draft-empty');
});

test('manual save is serialized behind an automatic quote write', async () => {
  let creates = 0;
  const page = instance(loadPage({ createDraft: async () => { creates += 1; return { draft_id: 'unexpected', revision: 1 }; } }), {
    data: { quoting: true, mode: 'text', prompt: '', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, references: [] },
  });
  assert.equal(await page.saveDraft(), null);
  assert.equal(creates, 0);
});

test('an automatic quote response is discarded when the editor changes while it is in flight', async () => {
  let resolveQuote;
  const quotePromise = new Promise(resolve => { resolveQuote = resolve; });
  const page = instance(loadPage({ quote: () => quotePromise }), {
    data: { capabilities: { enabled: true, modes: [{ code: 'text', enabled: true }] }, selectedModeEnabled: true, mode: 'text', prompt: '原描述', ratio: '16:9', resolution: '768P', duration: 5, quantity: 1, references: [] },
  });
  page._editVersion = 1;
  const pending = page.refreshQuote();
  await Promise.resolve();
  page.inputField({ currentTarget: { dataset: { field: 'prompt' } }, detail: { value: '新描述' } });
  resolveQuote({ quote_id: 'late', expires_at: '2999-01-01T00:00:00Z' });
  await pending;
  assert.equal(page.data.quote, null);
  assert.equal(page.data.quoting, false);
});

test('generation-ready edits debounce automatic server quotes', async () => {
  let quoteCalls = 0;
  const page = instance(loadPage({ quote: async () => { quoteCalls += 1; return { quote_id: 'auto', total_stones: 12, expires_at: '2999-01-01T00:00:00Z' }; } }), {
    data: { capabilities: { enabled: true }, modeOptions: [{ code: 'text', enabled: true }], selectedModeEnabled: true, mode: 'text', prompt: '可生成的画面', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, references: [] },
  });
  const originalSetTimeout = global.setTimeout;
  const originalClearTimeout = global.clearTimeout;
  let scheduled;
  global.setTimeout = callback => { scheduled = callback; return 1; };
  global.clearTimeout = () => {};
  try {
    page.inputField({ currentTarget: { dataset: { field: 'prompt' } }, detail: { value: '修改后的画面' } });
    assert.equal(quoteCalls, 0);
    await scheduled();
    assert.equal(quoteCalls, 1);
    assert.equal(page.data.quote.total_stones, 12);
    assert.equal(page.data.quoteReady, true);
  } finally {
    global.setTimeout = originalSetTimeout;
    global.clearTimeout = originalClearTimeout;
  }
});

test('quote failure retains the saved draft identity so retry updates instead of creating again', async () => {
  let creates = 0;
  let updates = 0;
  let quotes = 0;
  const page = instance(loadPage({
    createDraft: async () => { creates += 1; return { draft_id: 'draft-retained', revision: 1 }; },
    updateDraft: async (_id, input) => { updates += 1; return { draft_id: 'draft-retained', revision: input.expected_revision + 1 }; },
    quote: async () => { quotes += 1; if (quotes === 1) throw new Error('临时失败'); return { quote_id: 'retry-ok', total_stones: 8, expires_at: '2999-01-01T00:00:00Z' }; },
  }), { data: { capabilities: { enabled: true }, selectedModeEnabled: true, mode: 'text', prompt: '庭院镜头', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, references: [] } });
  await page.refreshQuote();
  assert.equal(page.data.draft_id, 'draft-retained');
  assert.equal(page.data.revision, 1);
  await page.refreshQuote();
  assert.equal(creates, 1);
  assert.equal(updates, 1);
  assert.equal(page.data.quoteReady, true);
});

test('one generate click submits the current unexpired quote without another quote request', async () => {
  let quoteCalls = 0;
  let batchCalls = 0;
  let redirected = '';
  const page = instance(loadPage({
    quote: async () => { quoteCalls += 1; },
    createBatch: async () => { batchCalls += 1; return { batch_id: 'batch-direct' }; },
  }), { data: { capabilities: { enabled: true }, selectedModeEnabled: true, draft_id: 'draft-1', revision: 2, mode: 'text', prompt: '庭院慢推镜头', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, references: [] } });
  const rules = require(rulesPath);
  page.data.quote = { quote_id: 'quote-current', client_fingerprint: rules.billableFingerprint(page.draftInput()), expires_at: '2999-01-01T00:00:00Z' };
  page.data.quoteReady = true;
  global.wx = { redirectTo({ url }) { redirected = url; } };
  try { await page.generateVideo(); } finally { delete global.wx; }
  assert.equal(quoteCalls, 0);
  assert.equal(batchCalls, 1);
  assert.match(redirected, /batch-direct/);
});

test('unknown submission state survives edits and tab switches and blocks another paid batch', async () => {
  let batchCalls = 0;
  const page = instance(loadPage({ createBatch: async () => { batchCalls += 1; return { id: 'duplicate' }; } }), { data: {
    capabilities: { enabled: true }, selectedModeEnabled: true, submissionUncertain: true,
    activeTab: 'creation', mode: 'text', prompt: '原描述', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1, references: [],
  } });
  page._tabStates = {};
  page.inputField({ currentTarget: { dataset: { field: 'prompt' } }, detail: { value: '新描述' } });
  assert.equal(page.data.submissionUncertain, true);
  page.switchTab({ currentTarget: { dataset: { tab: 'reference' } } });
  assert.equal(page.data.submissionUncertain, true);
  await page.generateVideo();
  assert.equal(batchCalls, 0);
  assert.match(page.data.error, /创作记录/);
});

test('recovery uses the durable submission without another quote or a new key', async () => {
  const requests = [];
  let redirected = '';
  const page = instance(loadPage({
    pendingSubmission: () => ({ quote_id: 'original-quote', idempotency_key: 'original-key' }),
    createBatch: async input => { requests.push(input); return { batch_id: 'recovered-batch' }; },
  }), { data: { submissionUncertain: true } });
  global.wx = { redirectTo({ url }) { redirected = url; } };
  try { await page.recoverSubmission(); } finally { delete global.wx; }
  assert.deepEqual(requests, [{ quote_id: 'original-quote', idempotency_key: 'original-key' }]);
  assert.match(redirected, /recovered-batch/);
});

test('definite recovery rejection unlocks editing without discarding the prompt', async () => {
  let pending = { quote_id: 'expired', idempotency_key: 'original-key' };
  const page = instance(loadPage({
    pendingSubmission: () => pending,
    createBatch: async () => { pending = null; throw Object.assign(new Error('报价已过期'), { statusCode: 410 }); },
  }), { data: { submissionUncertain: true, prompt: '需要保留的画面描述' }, scheduleAutoQuote() {} });
  await page.recoverSubmission();
  assert.equal(page.data.submissionUncertain, false);
  assert.equal(page.data.prompt, '需要保留的画面描述');
  assert.match(page.data.error, /过期/);
});

test('opening an unresolved editor only queries and never creates a paid batch automatically', async () => {
  let submitted = 0;
  let queried = 0;
  const page = instance(loadPage({
    pendingSubmission: () => ({ quote_id: 'quote-A', idempotency_key: 'key-A' }),
    getSubmission: async () => { queried += 1; throw Object.assign(new Error('未找到任务'), { statusCode: 404 }); },
    createBatch: async () => { submitted += 1; },
  }), { data: { submissionUncertain: true } });
  await page.checkSubmission();
  assert.equal(queried, 1);
  assert.equal(submitted, 0);
  assert.equal(page.data.submissionUncertain, true);
  assert.match(page.data.notice, /原请求/);
});

test('editor markup follows the confirmed two-tab workspace with the connected AI polish entry', () => {
  const root = path.resolve(__dirname, '../miniprogram/pages/aivideostudio');
  const markup = fs.readFileSync(path.join(root, 'generate.wxml'), 'utf8');
  const style = fs.readFileSync(path.join(root, 'generate.wxss'), 'utf8');
  const config = JSON.parse(fs.readFileSync(path.join(root, 'generate.json'), 'utf8'));
  assert.equal(config.navigationBarTitleText, 'AI 视频');
  assert.match(markup, />文 \/ 图生视频</);
  assert.match(markup, />参考生视频</);
  assert.match(markup, /我的素材/);
  assert.match(markup, /我的提示词/);
  assert.match(markup, /创作记录/);
  assert.match(markup, /AI 润色/);
  assert.match(markup, /bindtap="startPolish"/);
  assert.doesNotMatch(markup, /bindtap="polishUnavailable"/);
  assert.match(markup, />生成视频</);
  assert.doesNotMatch(markup, /查看报价|确认并生成/);
  assert.match(markup, /<video/);
  assert.match(markup, /display_index/);
  assert.match(markup, /mini-icon[^>]+chevron-left/);
  assert.match(markup, /<picker[\s\S]*<picker[\s\S]*<picker[\s\S]*<picker/);
  assert.doesNotMatch(style, /linear-gradient|radial-gradient/);
  assert.match(style, /#fbfaf7/i);
  assert.match(style, /#b98245/i);
  assert.match(style, /\.frame-slot::before\s*\{[^}]*padding-top:177\.7778%/);
  assert.match(style, /\.frame-image,\.frame-empty\s*\{[^}]*position:absolute[^}]*inset:0/);
});
