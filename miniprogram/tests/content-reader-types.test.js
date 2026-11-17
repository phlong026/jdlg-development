'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function reader(getPerformance) {
  let definition;
  const events = [];
  const wx = {
    ...(getPerformance ? { getPerformance } : {}),
    createVideoContext: () => ({ pause() {} }),
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.js'), 'utf8'), {
    Page: value => { definition = value; },
    wx, Promise, Object, Array, String, Number, Math, Error, Set, Date: { now: () => { throw new Error('wall clock must not measure viewing'); } }, encodeURIComponent,
    require: name => name.includes('tracking/index') ? { emit: (page, type, event) => events.push({ type, ...event }) }
      : name.includes('tracking/video') ? require(path.join(root, 'utils/tracking/video'))
      : name.includes('note-reader-actions') || name.includes('note-interactions') ? { data: {}, methods: {} }
      : name.includes('protected-media') ? { resolveProtectedMediaURL: async value => value }
      : name.includes('content-presentation') ? { buildContentPresentation: () => ({}) }
      : {},
  });
  const media = { domId: 'video-one', errorKey: 'videoOne', kind: 'video', media_id: 'video-one', playerKey: 'player-one', url: 'https://example.test/video.mp4' };
  const page = Object.assign({}, definition, {
    data: { ...definition.data, media: [media], presentationView: { kind: 'video', media: [media] }, sourceOpen: false },
    observers: [],
    pageVisible: true,
    setData(values) { Object.assign(this.data, values); },
  });
  return { events, page };
}

test('reader video progress stays usable when monotonic performance time is unavailable', () => {
  for (const getPerformance of [undefined, () => ({})]) {
    const { events, page } = reader(getPerformance);
    assert.doesNotThrow(() => {
      page.observePlayers();
      page.videoState({ type: 'play', currentTarget: { dataset: { playerKey: 'player-one' } }, detail: {} });
      page.videoProgress({ type: 'timeupdate', currentTarget: { dataset: { playerKey: 'player-one' } }, detail: { currentTime: 1, duration: 10 } });
      page.videoProgress({ type: 'timeupdate', currentTarget: { dataset: { playerKey: 'player-one' } }, detail: { currentTime: 2, duration: 10 } });
    });
    assert.deepEqual(events, [], 'missing monotonic time must not fabricate analytics events');
  }
});

function rejectedActionReader(rejection) {
  let definition;
  const toasts = [];
  const wx = {
    getStorageSync: () => '',
    showToast: value => { toasts.push(value); },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.js'), 'utf8'), {
    Page: value => { definition = value; },
    wx, Promise, Object, Array, String, Number, Math, Error, Set, Date, encodeURIComponent,
    require: name => name.includes('tracking/index') ? { consented: () => false, consentChoice: () => ({ resolved: true, accepted: false }) }
      : name.includes('tracking/video') ? { createVideoTracker: () => ({}) }
      : name.includes('note-reader-actions') || name.includes('note-interactions') ? { data: {}, methods: {} }
      : name.includes('protected-media') ? { resolveProtectedMediaURL: async value => value }
      : name.includes('content-presentation') ? { buildContentPresentation: () => ({}) }
      : name.includes('phase3-api') ? { request: async () => { throw rejection; } }
      : {},
  });
  const page = Object.assign({}, definition, {
    data: { ...definition.data, contentReady: true, content: { owner_id: 'owner-one', title: '内容' }, posterAvailable: true },
    options: { content_id: 'content-one', content_version: '1', untracked_share: '1' },
    posterCapabilitySequence: 1,
    posterCapability: { token: '', contentID: 'content-one', version: '1', owner: 'owner-one', sequence: 1 },
    setData(values) { Object.assign(this.data, values); },
  });
  return { page, toasts };
}

for (const [label, rejection, expected] of [
  ['null', null, '分享链接暂不可用'],
  ['string', 'offline', '分享链接暂不可用'],
  ['plain object', {}, '分享链接暂不可用'],
  ['Error', new Error('网络中断'), '网络中断'],
]) test('reader safely reports rejected share and poster actions: ' + label, async () => {
  const share = rejectedActionReader(rejection);
  await assert.doesNotReject(() => share.page.copyWebLink());
  assert.equal(share.page.data.webLinkBusy, false);
  assert.equal(share.toasts[0].title, expected);

  const poster = rejectedActionReader(rejection);
  await assert.doesNotReject(() => poster.page.generatePoster());
  assert.equal(poster.page.data.posterBusy, false);
  assert.equal(poster.toasts[0].title, label === 'Error' ? '网络中断' : '海报生成失败');
});

function rejectedLoadReader(options) {
  let definition;
  const wx = {
    getStorageSync: () => '',
    getWindowInfo: () => ({ windowHeight: 800, statusBarHeight: 20 }),
    hideShareMenu() {},
  };
  const tracking = {
    captureEntryValidity: () => () => true,
    consentChoice: () => ({ resolved: true, accepted: false }),
    refreshConfig: async () => ({ enabled: false }),
    resolvePageOptions() {},
  };
  const trackingApi = {
    mediaURL: value => value,
    request: options.request,
    resolveReaderOptions: async value => value,
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.js'), 'utf8'), {
    Page: value => { definition = value; },
    wx, Promise, Object, Array, String, Number, Math, Error, Set, Date, encodeURIComponent,
    require: name => name.includes('tracking/index') ? tracking
      : name.includes('tracking/api') ? trackingApi
      : name.includes('tracking/video') ? { createVideoTracker: () => ({}) }
      : name.includes('note-reader-actions') || name.includes('note-interactions') ? { data: {}, methods: {} }
      : name.includes('protected-media') ? { resolveProtectedMediaURL: options.resolveMedia || (async value => value) }
      : name.includes('content-presentation') ? { buildContentPresentation: () => ({ kind: 'article', blocks: [{}], media: [] }) }
      : {},
  });
  return Object.assign({}, definition, {
    data: { ...definition.data, content: { title: '旧内容' }, contentReady: true },
    options: { content_id: 'content-one', content_version: '1' },
    observers: [],
    pausePlayers() {},
    setData(values) { Object.assign(this.data, values); },
  });
}

test('reader still clears verified content after 401 and media identity loss', async () => {
  const unauthorized = rejectedLoadReader({ request: async () => { throw { statusCode: 401 }; } });
  await unauthorized.loadContent();
  assert.equal(unauthorized.data.contentReady, false);
  assert.equal(unauthorized.data.content, null);

  const identityChanged = rejectedLoadReader({
    request: async () => ({ title: '跨账户内容', cover: { url: 'https://example.test/cover.jpg' }, media: [] }),
    resolveMedia: async () => { throw { code: 'MEDIA_IDENTITY_CHANGED' }; },
  });
  await identityChanged.loadContent();
  assert.equal(identityChanged.data.contentReady, false);
  assert.equal(identityChanged.data.content, null);
});
