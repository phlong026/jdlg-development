'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const rules = require('../miniprogram/utils/ai-video-studio');
const { wrapProfileAccess } = require('../miniprogram/utils/profile-access');

function tick() { return new Promise(resolve => setImmediate(resolve)); }
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }

function loadPage(relativePath, api, wx) {
  const pagePath = require.resolve('../miniprogram/pages/aivideostudio/' + relativePath);
  const apiPath = require.resolve('../miniprogram/utils/ai-video-studio-api');
  const previousPage = global.Page;
  const previousWx = global.wx;
  const previousApi = require.cache[apiPath];
  let definition;
  require.cache[apiPath] = { id: apiPath, filename: apiPath, loaded: true, exports: api };
  delete require.cache[pagePath];
  global.Page = value => { definition = value; };
  global.wx = wx;
  require(pagePath);
  global.Page = previousPage;
  global.wx = previousWx;
  if (previousApi) require.cache[apiPath] = previousApi; else delete require.cache[apiPath];
  delete require.cache[pagePath];
  return definition;
}

function mount(definition, data, wx) {
  const page = Object.assign({}, definition);
  page.data = JSON.parse(JSON.stringify(Object.assign({}, definition.data, data || {})));
  page.setData = patch => Object.assign(page.data, patch);
  page._token = 'token';
  page._unloaded = false;
  page.current = page.current || (() => true);
  global.wx = wx;
  return page;
}

function audioContext() {
  const handlers = {};
  return {
    src: '', played: 0, paused: 0, destroyed: 0,
    play() { this.played += 1; }, pause() { this.paused += 1; }, destroy() { this.destroyed += 1; },
    onPlay(handler) { handlers.play = handler; }, onPause(handler) { handlers.pause = handler; },
    onEnded(handler) { handlers.ended = handler; }, onError(handler) { handlers.error = handler; },
    emit(name, value) { if (handlers[name]) handlers[name](value); },
  };
}

test.afterEach(() => { delete global.wx; });

test('reference input rejects an overlong purpose and the provider-visible merged prompt', () => {
  const base = {
    mode: 'reference', reference_prompt: '庭院镜头', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1,
    references: [{ media_id: 'image-1', media_type: 'image', duration: 0, purpose: '建筑材质', position: 1 }],
  };
  assert.equal(rules.validateCreation({ ...base, references: [{ ...base.references[0], purpose: '用'.repeat(121) }] }).code, 'reference_purpose_length');
  assert.equal(rules.validateCreation({ ...base, reference_prompt: '景'.repeat(6990) }).code, 'reference_prompt_total_length');
});

test('frame selection stays image-only at navigation and apply boundaries', () => {
  const navigations = [], toasts = [];
  const wx = { getStorageSync: () => 'token', navigateTo: options => navigations.push(options), showToast: value => toasts.push(value) };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, { first_frame_media_id: '', last_frame_media_id: '', firstFrame: {}, lastFrame: {} }, wx);
  page.chooseMaterial({ currentTarget: { dataset: { target: 'first_frame' } } });
  assert.match(navigations[0].url, /allowed_type=image/);
  page.applyMaterial('first_frame', { media_id: 'video-1', media_type: 'video', url: 'https://example.com/video.mp4' });
  assert.equal(page.data.first_frame_media_id, '');
  assert.match(toasts.at(-1).title, /图片/);
});

test('materials picker locks its filter and selection to the allowed type', async () => {
  const calls = [], toasts = [];
  const api = {
    ORIGIN: 'https://api.example.com', capabilities: async () => ({}),
    listMedia: async query => { calls.push(query); return { items: [{ media_id: 'video-1', media_type: 'video' }], total: 1 }; },
  };
  const wx = { getStorageSync: () => 'token', showToast: value => toasts.push(value), navigateBack() {} };
  const definition = loadPage('materials', api, wx);
  const page = mount(definition, {}, wx);
  page.onLoad({ select: '1', allowed_type: 'image' });
  await tick();
  assert.equal(page.data.mediaType, 'image');
  assert.deepEqual(page.data.types, [{ value: 'image', label: '图片' }]);
  assert.equal(calls.at(-1).media_type, 'image');
  page.data.items = [{ media_id: 'video-1', media_type: 'video' }];
  page.choose({ currentTarget: { dataset: { index: 0 } } });
  assert.match(toasts.at(-1).title, /图片/);
});

test('generation page previews frames and references and owns one disposable audio player', () => {
  const previews = [], mediaPreviews = [], player = audioContext();
  const wx = {
    getStorageSync: () => 'token',
    previewImage: options => previews.push(options),
    previewMedia: options => mediaPreviews.push(options),
    createInnerAudioContext: () => player,
  };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    firstFrame: { media_id: 'image-1', preview_url: 'https://example.com/first.jpg' },
    lastFrame: {}, playingAudioID: '', audioPreviewError: '',
    references: [
      { media_id: 'image-2', media_type: 'image', preview_url: 'https://example.com/reference.jpg' },
      { media_id: 'video-1', media_type: 'video', media_url: 'https://example.com/reference.mp4' },
      { media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' },
    ],
  }, wx);
  page.previewFrame({ currentTarget: { dataset: { target: 'first_frame' } } });
  page.previewReference({ currentTarget: { dataset: { id: 'image-2' } } });
  page.previewReference({ currentTarget: { dataset: { id: 'video-1' } } });
  assert.equal(previews.length, 2);
  assert.equal(mediaPreviews[0].sources[0].type, 'video');
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  assert.equal(player.src, 'https://example.com/reference.mp3');
  assert.equal(player.played, 1);
  assert.equal(page.data.loadingAudioID, 'audio-1');
  assert.equal(page.data.playingAudioID, '');
  player.emit('play');
  assert.equal(page.data.playingAudioID, 'audio-1');
  assert.equal(page.data.loadingAudioID, '');
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  assert.equal(player.paused, 1);
  page.onHide();
  assert.equal(player.destroyed, 1);
  assert.equal(page.data.playingAudioID, '');
});

test('audio preview is destroyed when the active account changes', () => {
  let token = 'token';
  const player = audioContext();
  const wx = { getStorageSync: () => token, createInnerAudioContext: () => player };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    playingAudioID: '', audioPreviewError: '',
    references: [{ media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' }],
  }, wx);
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  token = 'different-account';
  page.onShow();
  assert.equal(player.destroyed, 1);
  player.emit('play');
  assert.equal(page.data.playingAudioID, '');
});

test('audio preview failure is visible and releases the native player', () => {
  const player = audioContext();
  const wx = { getStorageSync: () => 'token', createInnerAudioContext: () => player };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    playingAudioID: '', audioPreviewError: '',
    references: [{ media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' }],
  }, wx);
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  player.emit('error', { errMsg: 'decode failed' });
  assert.match(page.data.audioPreviewError, /失败/);
  assert.equal(player.destroyed, 1);
  assert.equal(page.data.playingAudioID, '');
});

test('audio loading timeout never leaves a false playing state', () => {
  const player = audioContext();
  const wx = { getStorageSync: () => 'token', createInnerAudioContext: () => player };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    playingAudioID: '', loadingAudioID: '', audioPreviewError: '',
    references: [{ media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' }],
  }, wx);
  page._sessionEpoch = 1;
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  const timer = page._audioLoadTimer;
  assert.equal(page.data.playingAudioID, '');
  assert.equal(page.data.loadingAudioID, 'audio-1');
  timer._onTimeout();
  assert.equal(page.data.loadingAudioID, '');
  assert.match(page.data.audioPreviewError, /超时/);
  assert.equal(player.destroyed, 1);
});

test('rapid audio taps cancel the loading player without starting a second session', () => {
  const players = [];
  const wx = { getStorageSync: () => 'token', createInnerAudioContext: () => { const player = audioContext(); players.push(player); return player; } };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    playingAudioID: '', audioPreviewError: '',
    references: [{ media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' }],
  }, wx);
  const event = { currentTarget: { dataset: { id: 'audio-1' } } };
  page.toggleReferenceAudio(event);
  page.toggleReferenceAudio(event);
  assert.equal(players.length, 1);
  assert.equal(players[0].destroyed, 1);
  players[0].emit('play');
  assert.equal(page.data.playingAudioID, '');
});

test('removing an audio reference while it is loading destroys the pending player', () => {
  const player = audioContext();
  const wx = { getStorageSync: () => 'token', createInnerAudioContext: () => player };
  const page = mount(loadPage('generate', {}, wx), {
    playingAudioID: '', loadingAudioID: '', audioPreviewError: '', reference_prompt: '',
    references: [{ media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' }],
  }, wx);
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  assert.equal(page.data.loadingAudioID, 'audio-1');
  page.removeReference({ currentTarget: { dataset: { id: 'audio-1' } } });
  assert.equal(player.destroyed, 1);
  assert.equal(page.data.loadingAudioID, '');
  player.emit('play');
  assert.equal(page.data.playingAudioID, '');
});

test('reference audio and inline video are mutually exclusive', () => {
  const player = audioContext(), pausedVideos = [];
  const wx = {
    getStorageSync: () => 'token', createInnerAudioContext: () => player,
    createVideoContext: id => ({ pause: () => pausedVideos.push(id) }),
  };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    playingAudioID: '', audioPreviewError: '',
    references: [
      { media_id: 'video-1', media_type: 'video', media_url: 'https://example.com/reference.mp4', video_dom_id: 'ai-reference-video-video-1' },
      { media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/reference.mp3' },
    ],
  }, wx);
  page.toggleReferenceAudio({ currentTarget: { dataset: { id: 'audio-1' } } });
  assert.deepEqual(pausedVideos, ['ai-reference-video-video-1']);
  page.onReferenceVideoPlay();
  assert.equal(player.destroyed, 1);
});

test('late preview errors cannot update an unloaded or switched-account page', () => {
  let token = 'token', imageOptions, mediaOptions;
  const wx = {
    getStorageSync: () => token,
    previewImage: options => { imageOptions = options; },
    previewMedia: options => { mediaOptions = options; },
  };
  const definition = loadPage('generate', {}, wx);
  const page = mount(definition, {
    firstFrame: { media_id: 'image-1', preview_url: 'https://example.com/first.jpg' },
    references: [{ media_id: 'video-1', media_type: 'video', media_url: 'https://example.com/reference.mp4' }],
    error: '',
  }, wx);
  page.previewFrame({ currentTarget: { dataset: { target: 'first_frame' } } });
  page.previewReference({ currentTarget: { dataset: { id: 'video-1' } } });
  token = 'different-account';
  imageOptions.fail();
  mediaOptions.fail();
  assert.equal(page.data.error, '');
});

test('materials page previews image video and audio without selecting them', () => {
  const images = [], videos = [], player = audioContext();
  const wx = {
    getStorageSync: () => 'token', previewImage: value => images.push(value), previewMedia: value => videos.push(value),
    createInnerAudioContext: () => player,
  };
  const definition = loadPage('materials', {}, wx);
  const page = mount(definition, { items: [
    { media_id: 'image-1', media_type: 'image', preview_url: 'https://example.com/a.jpg' },
    { media_id: 'video-1', media_type: 'video', media_url: 'https://example.com/a.mp4' },
    { media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/a.mp3' },
  ] }, wx);
  page.preview({ currentTarget: { dataset: { index: 0 } } });
  page.preview({ currentTarget: { dataset: { index: 1 } } });
  page.preview({ currentTarget: { dataset: { index: 2 } } });
  assert.equal(images.length, 1);
  assert.equal(videos.length, 1);
  assert.equal(player.played, 1);
  page.onUnload();
  assert.equal(player.destroyed, 1);
});

test('materials preview rejects late account errors and rapid audio replay', () => {
  let token = 'token', imageOptions;
  const players = [];
  const wx = {
    getStorageSync: () => token,
    previewImage: options => { imageOptions = options; },
    createInnerAudioContext: () => { const player = audioContext(); players.push(player); return player; },
  };
  const definition = loadPage('materials', {}, wx);
  const page = mount(definition, { previewError: '', items: [
    { media_id: 'image-1', media_type: 'image', preview_url: 'https://example.com/a.jpg' },
    { media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/a.mp3' },
  ] }, wx);
  page.preview({ currentTarget: { dataset: { index: 0 } } });
  token = 'different-account';
  imageOptions.fail();
  assert.equal(page.data.previewError, '');
  token = 'token';
  const audioEvent = { currentTarget: { dataset: { index: 1 } } };
  page.preview(audioEvent);
  page.preview(audioEvent);
  assert.equal(players.length, 1);
  assert.equal(players[0].destroyed, 1);
});

test('materials replacing its list stops orphan audio without touching the next player', async () => {
  const players = [];
  const api = { listMedia: async () => ({ items: [{ media_id: 'audio-1', media_type: 'audio', url: 'https://example.com/a.mp3' }], total: 1 }) };
  const wx = { getStorageSync: () => 'token', createInnerAudioContext: () => { const player = audioContext(); players.push(player); return player; } };
  const page = mount(loadPage('materials', api, wx), { items: [{ media_id: 'audio-1', media_type: 'audio', media_url: 'https://example.com/a.mp3' }] }, wx);
  page.preview({ currentTarget: { dataset: { index: 0 } } });
  await page.load();
  assert.equal(players[0].destroyed, 1);
  page.preview({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(players.length, 2);
  players[0].emit('ended');
  assert.equal(players[1].destroyed, 0);
  assert.equal(page._audioPlayer, players[1]);
});

test('studio home loads once for initial show and refreshes after returning', async () => {
  let loads = 0;
  const api = {
    capabilities: async () => { loads += 1; return { enabled: true }; },
    listDrafts: async () => ({ items: [] }), listBatches: async () => ({ items: [] }),
  };
  const wx = { getStorageSync: () => 'token' };
  const page = mount(loadPage('index', api, wx), {}, wx);
  page.onLoad();
  page.onShow();
  await tick();
  assert.equal(loads, 1);
  page.onShow();
  await tick();
  assert.equal(loads, 2);
});

test('prompt library paginates and accepts the editor 7000 character limit', async () => {
  const calls = [];
  const api = { listPrompts: async query => { calls.push(query); return { items: [{ prompt_id: `p${query.page}` }], total: 51 }; } };
  const wx = { getStorageSync: () => 'token' };
  const page = mount(loadPage('prompts', api, wx), {}, wx);
  page.onLoad({});
  await tick();
  assert.equal(page.data.hasMore, true);
  await page.loadMore();
  assert.deepEqual(calls.map(call => call.page), [1, 2]);
  assert.equal(page.data.items.length, 2);
  page.inputPrompt({ detail: { value: '字'.repeat(7000) } });
  assert.equal(page.data.prompt.length, 7000);
});

test('list retries never treat tap events as append and append pages deduplicate stable ids', async () => {
  const wx = { getStorageSync: () => 'token', previewImage() {} };
  const materialCalls = [];
  const materials = mount(loadPage('materials', {
    ORIGIN: '', listMedia: async query => { materialCalls.push(query); return { items: [{ media_id: 'm1', media_type: 'image', url: '/m1.jpg' }, { media_id: 'm2', media_type: 'image', url: '/m2.jpg' }], total: 2 }; },
  }, wx), { page: 1, items: [{ media_id: 'm1', media_type: 'image' }], previewError: '旧错误' }, wx);
  await materials.load({ currentTarget: {} });
  assert.equal(materialCalls.at(-1).page, 1);
  assert.equal(materials.data.items.length, 2);
  materials.data.items = [{ media_id: 'm1', media_type: 'image' }];
  await materials.load(true);
  assert.deepEqual(materials.data.items.map(item => item.media_id), ['m1', 'm2']);
  materials.preview({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(materials.data.previewError, '');

  const promptCalls = [];
  const prompts = mount(loadPage('prompts', {
    listPrompts: async query => { promptCalls.push(query); return { items: [{ prompt_id: 'p1' }, { prompt_id: 'p2' }], total: 2 }; },
  }, wx), { page: 1, items: [{ prompt_id: 'p1' }] }, wx);
  await prompts.load({ currentTarget: {} });
  assert.equal(promptCalls.at(-1).page, 1);
  prompts.data.items = [{ prompt_id: 'p1' }];
  await prompts.load(true);
  assert.deepEqual(prompts.data.items.map(item => item.prompt_id), ['p1', 'p2']);

  const recordCalls = [];
  const records = mount(loadPage('records', {
    listBatches: async query => { recordCalls.push(query); return { items: [{ batch_id: 'b1' }, { batch_id: 'b2' }], total: 2 }; },
  }, wx), { tab: 'batches', page: 1, items: [{ id: 'b1' }] }, wx);
  await records.load({ currentTarget: {} });
  assert.equal(recordCalls.at(-1).page, 1);
  records.data.items = [{ id: 'b1' }];
  await records.load(true);
  assert.deepEqual(records.data.items.map(item => item.id), ['b1', 'b2']);
});

test('profile-access warm account reload revives the page and rejects the old account response', async () => {
  let token = 'account-a';
  const pending = [];
  const wx = {
    getStorageSync: key => key === 'token' ? token : '', hideShareMenu() {}, navigateTo() {}, showToast() {},
  };
  const api = {
    ORIGIN: '', capabilities: async () => ({}),
    listMedia: () => new Promise(resolve => pending.push({ token, resolve })),
  };
  const wrapped = wrapProfileAccess(loadPage('materials', api, wx));
  const page = mount(wrapped, {}, wx);
  page.route = 'pages/aivideostudio/materials';
  page.setData = (patch, done) => { Object.assign(page.data, patch); if (done) done.call(page); };
  page.selectComponent = () => ({ check: async () => false });
  page.onLoad({}); page.onShow(); page.onReady();
  page.onProfileAccessGranted({ detail: { token, profile: { id: 1, nickname: 'A', avatar: 'a' } } });
  assert.equal(page._unloaded, false);
  page.onProfileAccessBlocked({ detail: {} });
  token = 'account-b';
  page.onProfileAccessGranted({ detail: { token, profile: { id: 2, nickname: 'B', avatar: 'b' } } });
  assert.equal(page._unloaded, false);
  assert.equal(pending.length, 2);
  pending[1].resolve({ items: [{ media_id: 'b-media', media_type: 'image', url: '/b.jpg' }], total: 1 });
  await tick();
  pending[0].resolve({ items: [{ media_id: 'a-media', media_type: 'image', url: '/a.jpg' }], total: 1 });
  await tick();
  assert.deepEqual(page.data.items.map(item => item.media_id), ['b-media']);
});

test('all six studio pages reset their unloaded lifecycle on a warm onLoad', () => {
  const never = () => new Promise(() => {});
  const api = {
    pendingSubmission: () => null, capabilities: never, listMedia: never, listPrompts: never,
    listDrafts: never, listBatches: never, getBatch: never,
  };
  const wx = { getStorageSync: () => 'token' };
  const cases = [
    ['index', {}], ['generate', {}], ['materials', {}], ['prompts', {}],
    ['records', {}], ['detail', { batch_id: 'batch-1' }],
  ];
  for (const [name, options] of cases) {
    const page = mount(loadPage(name, api, wx), {}, wx);
    page._unloaded = true;
    page.onLoad(options);
    assert.equal(page._unloaded, false, name);
  }
});

test('studio home keeps successful sections and exposes independent retry errors', async () => {
  const api = {
    capabilities: async () => ({ enabled: true }),
    listDrafts: async () => { throw new Error('草稿暂不可用'); },
    listBatches: async () => ({ items: [{ batch_id: 'batch-1', mode: 'text' }] }),
  };
  const wx = { getStorageSync: () => 'token' };
  const page = mount(loadPage('index', api, wx), {}, wx);
  page.onLoad();
  await tick();
  assert.equal(page.data.batches.length, 1);
  assert.match(page.data.draftsError, /草稿/);
  assert.equal(page.data.batchesError, '');
});

test('materials capability failure is visible without hiding loaded media', async () => {
  const api = {
    ORIGIN: '', capabilities: async () => { throw new Error('配置读取失败'); },
    listMedia: async () => ({ items: [{ media_id: 'm1', media_type: 'image', url: '/m1.jpg' }], total: 1 }),
  };
  const wx = { getStorageSync: () => 'token' };
  const page = mount(loadPage('materials', api, wx), {}, wx);
  page.onLoad({});
  await tick();
  assert.equal(page.data.items.length, 1);
  assert.match(page.data.capabilitiesError, /配置读取失败/);
  assert.equal(page.data.error, '');
});

test('old prompt save cannot clear the new account saving state', async () => {
  let token = 'account-a';
  const creates = [];
  const api = { listPrompts: async () => ({ items: [], total: 0 }), createPrompt: () => { const call = deferred(); creates.push(call); return call.promise; } };
  const wx = { getStorageSync: () => token };
  const page = mount(loadPage('prompts', api, wx), {}, wx);
  page.onLoad({});
  page.setData({ name: 'A', prompt: 'A提示词' });
  const saveA = page.save();
  page.onUnload(); token = 'account-b'; page.onLoad({});
  page.setData({ name: 'B', prompt: 'B提示词' });
  const saveB = page.save();
  creates[0].reject(new Error('A失败'));
  await saveA;
  assert.equal(page.data.saving, true);
  creates[1].resolve({ prompt_id: 'p-b' });
  await saveB;
  assert.equal(page.data.saving, false);
});

test('old upload progress and failure cannot release the new account upload', async () => {
  let token = 'account-a';
  const uploads = [];
  const api = {
    ORIGIN: '', capabilities: async () => ({}), listMedia: async () => ({ items: [], total: 0 }),
    uploadMedia: (_path, progress) => { const call = deferred(); const operation = { promise: call.promise, cancel() {}, progress, call }; uploads.push(operation); return operation; },
  };
  const wx = { getStorageSync: () => token, showToast() {} };
  const page = mount(loadPage('materials', api, wx), {}, wx);
  page.onLoad({});
  const uploadA = page.upload({ tempFilePath: '/a.jpg' }, 'image');
  page.onUnload(); token = 'account-b'; page.onLoad({});
  const uploadB = page.upload({ tempFilePath: '/b.jpg' }, 'image');
  uploads[0].progress({ progress: 88 });
  uploads[0].call.reject(new Error('A失败'));
  await uploadA;
  assert.equal(page.data.uploading, true);
  assert.equal(page._uploadOperation, uploads[1]);
  uploads[1].call.resolve({ media_id: 'm-b' });
  await uploadB;
  assert.equal(page.data.uploading, false);
});

test('old draft save cannot clear the new account save lock', async () => {
  let token = 'account-a';
  const saves = [];
  const api = { pendingSubmission: () => null, capabilities: () => new Promise(() => {}) };
  const wx = { getStorageSync: () => token };
  const page = mount(loadPage('generate', api, wx), { capabilities: { enabled: false } }, wx);
  page.persistDraft = () => { const call = deferred(); saves.push(call); return call.promise; };
  page.onLoad({});
  const saveA = page.saveDraft();
  page.onUnload(); token = 'account-b'; page.onLoad({});
  const saveB = page.saveDraft();
  saves[0].reject(new Error('A失败'));
  await saveA;
  assert.equal(page.data.saving, true);
  saves[1].resolve({ draft_id: 'draft-b', revision: 1 });
  await saveB;
  assert.equal(page.data.saving, false);
});

test('old detail cancellation cannot clear the new account cancellation lock', async () => {
  let token = 'account-a';
  const cancels = [];
  const api = {
    getBatch: () => new Promise(() => {}),
    cancelBatch: () => { const call = deferred(); cancels.push(call); return call.promise; },
  };
  const wx = { getStorageSync: () => token };
  const page = mount(loadPage('detail', api, wx), { batch: { can_cancel: true } }, wx);
  page.onLoad({ batch_id: 'batch-a' }); page.data.batch = { can_cancel: true };
  const cancelA = page.cancelQueued();
  page.onUnload(); token = 'account-b'; page.onLoad({ batch_id: 'batch-b' }); page.data.batch = { can_cancel: true };
  const cancelB = page.cancelQueued();
  cancels[0].reject(new Error('A失败'));
  await cancelA;
  assert.equal(page.data.cancelling, true);
  cancels[1].reject(new Error('B失败'));
  await cancelB;
  assert.equal(page.data.cancelling, false);
});

test('old prompt modal cannot choose into a reloaded account', () => {
  let token = 'account-a', modal, emits = 0, backs = 0;
  const api = { listPrompts: async () => ({ items: [], total: 0 }) };
  const wx = { getStorageSync: () => token, showModal: options => { modal = options; }, navigateBack: () => { backs += 1; } };
  const page = mount(loadPage('prompts', api, wx), { items: [{ prompt_id: 'p-a', prompt: 'A提示词' }] }, wx);
  page.getOpenerEventChannel = () => ({ emit: () => { emits += 1; } });
  page.onLoad({}); page.data.items = [{ prompt_id: 'p-a', prompt: 'A提示词' }];
  page.choose({ currentTarget: { dataset: { index: 0 } } });
  page.onUnload(); token = 'account-b'; page.onLoad({});
  modal.success({ confirm: true });
  assert.equal(emits, 0);
  assert.equal(backs, 0);
});

test('old quote failure cannot clear the new account quote lock', async () => {
  let token = 'account-a';
  const saves = [], quotes = [];
  const api = {
    pendingSubmission: () => null, capabilities: () => new Promise(() => {}),
    quote: () => { const call = deferred(); quotes.push(call); return call.promise; },
  };
  const wx = { getStorageSync: () => token };
  const page = mount(loadPage('generate', api, wx), { capabilities: { enabled: true } }, wx);
  page.generationReady = () => true;
  page.draftInput = () => ({ mode: 'text', prompt: '镜头', ratio: '16:9', resolution: '768p', duration: 5, quantity: 1 });
  page.persistDraft = () => { const call = deferred(); saves.push(call); return call.promise; };
  page.onLoad({});
  const quoteA = page.refreshQuote();
  page.onUnload(); token = 'account-b'; page.onLoad({});
  const quoteB = page.refreshQuote();
  saves[0].reject(new Error('A失败'));
  await quoteA;
  assert.equal(page.data.quoting, true);
  saves[1].resolve({ draft_id: 'draft-b', revision: 1 });
  await tick();
  quotes[0].resolve({ quote_id: 'quote-b', total_stones: 3, expires_at: '2099-01-01T00:00:00Z' });
  await quoteB;
  assert.equal(page.data.quoting, false);
  assert.equal(page.data.quote.quote_id, 'quote-b');
});

test('old playback response cannot replace the new account result', async () => {
  let token = 'account-a';
  const plays = [];
  const api = {
    ORIGIN: 'https://api.example.com', getBatch: () => new Promise(() => {}),
    playback: () => { const call = deferred(); plays.push(call); return call.promise; },
  };
  const wx = { getStorageSync: () => token };
  const page = mount(loadPage('detail', api, wx), { results: [] }, wx);
  page.onLoad({ batch_id: 'batch-a' }); page.data.results = [{ result_id: 'result-a', playback_url: '' }];
  const playA = page.play({ currentTarget: { dataset: { index: 0 } } });
  page.onUnload(); token = 'account-b'; page.onLoad({ batch_id: 'batch-b' }); page.data.results = [{ result_id: 'result-b', playback_url: '' }];
  const playB = page.play({ currentTarget: { dataset: { index: 0 } } });
  plays[0].resolve({ url: '/a.mp4' });
  await playA;
  assert.equal(page.data.results[0].playback_url, '');
  plays[1].resolve({ url: '/b.mp4' });
  await playB;
  assert.equal(page.data.results[0].playback_url, 'https://api.example.com/b.mp4');
});
