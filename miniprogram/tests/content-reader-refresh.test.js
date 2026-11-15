'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');
const tick = () => new Promise(resolve => setImmediate(resolve));

const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
test('native reader keeps the article beside a transient error but hides it after access is lost', { skip: !fs.existsSync(compiler) }, () => {
  const source = require('node:child_process').execFileSync(compiler, ['./components/content-presentation/content-presentation.wxml', './pages/contentreader/contentreader.wxml'], { cwd: root, encoding: 'utf8', timeout: 10000 });
  const context = { window: {}, console };
  vm.createContext(context); vm.runInContext(source, context);
  const render = context.$gwx('./pages/contentreader/contentreader.wxml');
  const data = { __profileAccessMounted: true, loading: false, contentReady: true, error: '刷新失败，请重试', content: { title: '已验证正文', body: '保留的文章文字' }, presentationView: { kind: 'article', title: '已验证正文', sourceNote: '', publishedAt: '', blocks: [{ key: 'paragraph-0', kind: 'paragraph', text: '保留的文章文字', className: 'article-block' }] }, mediaErrors: {}, media: [], articleBlocks: [] };
  const retained = JSON.stringify(render(data));
  assert.match(retained, /保留的文章文字/);
  assert.match(retained, /刷新失败，请重试/);
  const blocked = JSON.stringify(render({ ...data, contentReady: false, content: null }));
  assert.doesNotMatch(blocked, /保留的文章文字|已验证正文/);
  assert.match(blocked, /刷新失败，请重试/);

  const videoView = { kind: 'video', videoLayout: 'channels', title: '视频', bodyText: '', poster: 'wxfile://cover.jpg', nativePoster: '', media: [{ domId: 'reader-media-0', playerKey: 'reader-player-0', media_id: 'video-one', errorKey: 'readerMedia0', url: 'wxfile://video.mp4', width: 300, height: 180 }] };
  const videoData = { ...data, error: '', content: { title: '视频' }, presentationView: videoView, videoStarted: {} };
  const covered = JSON.stringify(render(videoData));
  assert.match(covered, /wxfile:\/\/cover\.jpg/);
  assert.doesNotMatch(covered, /wxfile:\/\/video\.mp4/, '封面初态不得创建原生 video');
  const requested = JSON.stringify(render({ ...videoData, videoStarted: { 'reader-player-0': true } }));
  assert.doesNotMatch(requested, /wxfile:\/\/cover\.jpg/);
  assert.match(requested, /wxfile:\/\/video\.mp4/, '点击请求后才创建原生 video');
});

function harness(options = {}) {
  let definition;
  const pendingReads = [];
  let reads = 0;
  const trackingCalls = { resolve: 0, refresh: 0, preheat: 0 };
  const tracking = {
    captureEntryValidity: () => () => true,
    resolvePageOptions() { trackingCalls.resolve += 1; },
    refreshConfig: async () => { trackingCalls.refresh += 1; return { enabled: false }; },
    consented: () => false,
    preheatTimelineShare() { trackingCalls.preheat += 1; },
  };
  const api = {
    resolveReaderOptions: async options => options,
    mediaURL: value => value,
    request: async path => {
      assert.match(path, /^\/contents\//);
      reads += 1;
      if (reads === 1) return options.firstContent || { title: '已显示正文', body: '旧正文', media: [] };
      return new Promise((resolve, reject) => { pendingReads.push({ resolve, reject }); });
    },
  };
  let plays = 0;
  const wx = {
    hideShareMenu() {}, showShareMenu() {},
    getWindowInfo: () => ({ windowWidth: 400, windowHeight: 800, statusBarHeight: 20 }),
    createVideoContext: () => ({ pause() {}, play() { plays += 1; } }),
    getStorageSync: () => null,
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.js'), 'utf8'), {
    Page: value => { definition = value; }, wx, Promise, Object, Array, String, Number, Math, Error, encodeURIComponent,
    require: name => name.includes('tracking/index') ? tracking
      : name.includes('tracking/api') ? api
      : name.includes('protected-media') ? { resolveProtectedMediaURL: async value => { if (options.mediaError) throw options.mediaError; if (options.failMedia || options.failVideoMedia && /video/.test(value)) throw new Error('cdn unavailable'); return value; } }
      : name.includes('content-presentation') ? require(path.join(root, 'utils/content-presentation'))
      : name.includes('article-document') ? { renderArticleDocument: () => [] }
      : name.includes('phase3-api') ? { WORKBENCH: '/api/v1/miniprogram/workbench', request: async path => { assert.equal(path, '/api/v1/miniprogram/workbench/contents/content-0001/versions/1'); return options.managedContent; } }
      : { createVideoTracker: () => ({}) },
  });
  const page = Object.assign({}, definition, {
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values, callback) { Object.assign(this.data, values); if (callback) callback.call(this); },
    options: { content_id: 'content-0001', content_version: '1' },
    pausePlayers() {}, observePlayers() {},
  });
  return {
    page,
    finish: (value, index = 0) => pendingReads[index].resolve(value),
    fail: (error, index = 0) => pendingReads[index].reject(error),
    plays: () => plays,
    trackingCalls,
  };
}

test('authorized management preview reads exact version without initializing tracking or share preheat', async () => {
  const { page, trackingCalls } = harness({ managedContent: { title: '历史固定版', body: '不可变正文', media: [] } });
  page.managementPreview = true;
  page.options = { management_preview: '1', content_id: 'content-0001', content_version: '1' };
  await page.loadContent();
  assert.equal(page.data.content.title, '历史固定版');
  assert.equal(page.data.contentReady, true);
  assert.deepEqual(trackingCalls, { resolve: 0, refresh: 0, preheat: 0 });
  assert.equal(Object.keys(page.onShareAppMessage()).length, 0);
});

test('reader keeps note copy and an inline retry when protected media is temporarily unavailable', async () => {
  const { page } = harness({
    failMedia: true,
    firstContent: { title: '乡墅日记', body_format: 'article_v2', body: JSON.stringify({ schema: 1, presentation: 'gallery', blocks: [{ kind: 'paragraph', text: '保留整条文案' }, { kind: 'image', media_id: 'image-one' }] }), media: [{ media_id: 'image-one', kind: 'image', url: 'https://example.test/image.jpg' }] },
  });
  await page.loadContent();
  assert.equal(page.data.contentReady, true);
  assert.equal(page.data.presentationView.bodyText, '保留整条文案');
  assert.equal(page.data.presentationView.media.length, 1);
  assert.match(page.data.mediaErrors[page.data.presentationView.media[0].errorKey], /加载失败/);
});

for (const invalidContent of [{}, { title: '空内容', body_format: 'unsupported', body: '不支持' }]) {
  test('reader rejects unusable content without rendering source or consultation shell: ' + JSON.stringify(invalidContent), async () => {
    const { page } = harness({ firstContent: invalidContent });
    await page.loadContent();
    assert.equal(page.data.contentReady, false);
    assert.equal(page.data.content, null);
    assert.match(page.data.error, /内容.*完整|内容.*支持/);
  });
}

test('reader keeps video copy when authenticated media fails and uses the authenticated cover as poster', async () => {
  const { page } = harness({
    failVideoMedia: true,
    firstContent: {
      title: '新中式别墅',
      body_format: 'article_v2',
      body: JSON.stringify({ schema: 1, presentation: 'video', video_layout: 'channels', share_copy: '分享文案', moments_copy: '朋友圈文案', blocks: [{ kind: 'paragraph', text: '视频介绍' }, { kind: 'video', media_id: 'video-one' }] }),
      cover: { media_id: 'cover-one', kind: 'image', url: 'https://example.test/cover.jpg' },
      media: [{ media_id: 'video-one', kind: 'video', url: 'https://example.test/video.mp4' }],
    },
  });
  await page.loadContent();
  assert.equal(page.data.contentReady, true);
  assert.equal(page.data.presentationView.bodyText, '视频介绍');
  assert.equal(page.data.presentationView.poster, 'https://example.test/cover.jpg');
  assert.equal(page.data.presentationView.nativePoster, 'https://example.test/cover.jpg');
  assert.equal(page.data.presentationView.shareCopy, '分享文案');
  assert.match(page.data.mediaErrors[page.data.presentationView.media[0].errorKey], /加载失败/);
});

test('reader creates a covered video after one tap, preserves it through pause or reflow, and restores the cover on error', () => {
  const { page, plays } = harness();
  page.setData({
    videoStarted: {},
    content: { title: '视频', body_format: 'article_v2', body: JSON.stringify({ schema: 1, presentation: 'video', blocks: [{ kind: 'video', media_id: 'video-one' }] }) },
    media: [{ media_id: 'video-one', kind: 'video', url: 'wxfile://video.mp4', width: 1920, height: 1080 }],
    presentationView: { kind: 'video', galleryIndex: 0, descriptionExpanded: false },
  });
  const target = { dataset: { domId: 'reader-media-0', playerKey: 'reader-player-0', tracking: false } };
  page.playContentVideo({ currentTarget: target });
  assert.equal(plays(), 1);
  assert.equal(page.data.videoStarted['reader-player-0'], true, '点击封面后先创建原生播放器');
  page.videoState({ type: 'play', currentTarget: target });
  assert.equal(page.data.videoStarted['reader-player-0'], true);
  page.videoState({ type: 'pause', currentTarget: target });
  page.rebuildPresentation(0);
  assert.equal(page.data.videoStarted['reader-player-0'], true, 'metadata/layout rebuild must preserve playback state');
  page.videoState({ type: 'error', currentTarget: { dataset: { ...target.dataset, errorKey: 'readerMedia0' } } });
  assert.equal(page.data.videoStarted['reader-player-0'], undefined, '播放失败后应退回可重试封面');
});

test('reader keeps verified content visible while foreground refresh is pending', async () => {
  const { page, finish } = harness();
  page.pageVisible = true;
  await page.loadContent();
  assert.equal(page.data.content.title, '已显示正文');
  assert.equal(page.data.contentReady, true);
  const pending = page.loadContent();
  await tick();
  assert.equal(page.data.loading, false, '刷新期间不切回整页加载态');
  assert.equal(page.data.content.title, '已显示正文');
  finish({ title: '新正文', body: '更新内容', media: [] });
  await pending;
  assert.equal(page.data.content.title, '新正文');
});

for (const response of [{ statusCode: 403 }, { statusCode: 200, code: 403 }]) test('reader removes verified inaccessible content: ' + JSON.stringify(response), async () => {
  const { page, fail } = harness();
  page.pageVisible = true;
  await page.loadContent();
  page.setData({ media: [{ media_id: 'old-media' }], articleBlocks: [{ key: 'old-block' }] });
  const pending = page.loadContent();
  await tick();
  const forbidden = Object.assign(new Error('forbidden'), response);
  fail(forbidden);
  await pending;
  assert.equal(page.data.content, null);
  assert.deepEqual(Array.from(page.data.media), []);
  assert.deepEqual(Array.from(page.data.articleBlocks), []);
  assert.equal(page.data.contentReady, false);
  assert.match(page.data.error, /不可访问/);
});

for (const [label, refreshError, message] of [
  ['network failure', Object.assign(new Error('offline'), { networkError: true }), /网络/],
  ['server failure', Object.assign(new Error('server unavailable'), { statusCode: 503 }), /稍后/],
  ['media download failure', Object.assign(new Error('media unavailable'), { code: 'MEDIA_DOWNLOAD_FAILED', networkError: true }), /素材/],
]) {
  test(`reader keeps verified content after a quiet refresh ${label}`, async () => {
    const { page, fail } = harness();
    page.pageVisible = true;
    await page.loadContent();
    page.setData({ media: [{ media_id: 'old-media' }], articleBlocks: [{ key: 'old-block' }] });
    const pending = page.loadContent();
    await tick();
    fail(refreshError);
    await pending;
    assert.equal(page.data.content.title, '已显示正文');
    assert.equal(page.data.contentReady, true);
    assert.equal(page.data.media[0].media_id, 'old-media');
    assert.equal(page.data.articleBlocks[0].key, 'old-block');
    assert.match(page.data.error, message);
  });
}

test('reader ignores an older refresh that completes after a newer successful refresh', async () => {
  const { page, finish } = harness();
  page.pageVisible = true;
  await page.loadContent();
  const older = page.loadContent();
  await tick();
  const newer = page.loadContent();
  await tick();
  finish({ title: '最新正文', body: 'new', media: [] }, 1);
  await newer;
  finish({ title: '迟到旧正文', body: 'old', media: [] }, 0);
  await older;
  assert.equal(page.data.content.title, '最新正文');
});

test('media account boundary failure clears content instead of becoming a retry placeholder', async () => {
 const {page}=harness({mediaError:Object.assign(new Error('account changed'),{code:'MEDIA_IDENTITY_CHANGED'}),firstContent:{title:'旧账户内容',body:'正文',media:[{media_id:'image-one',kind:'image',url:'https://example.test/image.jpg'}]}});
 await page.loadContent();
 assert.equal(page.data.contentReady,false);assert.equal(page.data.content,null);
});
