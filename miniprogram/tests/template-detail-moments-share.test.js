const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const root = path.resolve(__dirname, '../miniprogram');

function loadPage({ detail, prepareShareCardImage, prepareTimelineShareImage, requestError } = {}) {
  let definition;
  const toasts = [];
  const wx = {
    getStorageSync: () => '',
    setBackgroundColor() {},
    getMenuButtonBoundingClientRect: () => ({ top: 24, height: 32 }),
    getSystemInfoSync: () => ({ statusBarHeight: 20, windowWidth: 375 }),
    request(options) {
      if (requestError) {
        options.fail(requestError);
        return;
      }
      options.success({ statusCode: 200, data: { code: 0, data: detail } });
    },
    showToast(options) { toasts.push(options); },
  };
  const dependency = new Proxy({}, {
    get: (_, key) => {
      if (key === 'resolveAssetPath' || key === 'normalizeCosUrl') return value => value;
      if (key === 'prepareShareCardImage') return prepareShareCardImage || (async value => `local:${value}`);
      if (key === 'prepareTimelineShareImage') return prepareTimelineShareImage || (async value => `timeline:${value}`);
      if (key === 'getPageCache') return () => detail || null;
      if (key === 'setPageCache' || key === 'prefetchImages') return () => {};
      if (key === 'getCachedDeviceFingerprint') return () => '';
      return () => ({});
    },
  });
  const source = fs.readFileSync(path.join(root, 'pages/templatesquaredetails/templatesquaredetails.js'), 'utf8');
  vm.runInNewContext(source, {
    exports: {},
    require: () => dependency,
    Page(value) { definition = value; },
    wx,
    console: { log() {}, warn() {}, error() {} },
    Promise,
    Date,
    Math,
    setTimeout,
    clearTimeout,
  });
  return { definition, toasts };
}

function createPage(definition, data = {}) {
  const page = {
    ...definition,
    data: { ...definition.data, ...data },
    setData(values, callback) {
      Object.assign(this.data, values);
      if (callback) callback();
    },
  };
  return page;
}

const detail = {
  id: 21,
  name: '新闽派三层别墅',
  description: '模板描述',
  preview_url: 'https://static.example.com/first.jpg',
  images: [
    'https://static.example.com/first.jpg',
    'https://static.example.com/second.jpg',
  ],
  is_free: true,
};

test('朋友圈分享把当前第二张图的短标识与封面绑定到同一模板', async () => {
  const { definition } = loadPage();
  const page = createPage(definition, {
    templateId: 21,
    template: detail,
    templateName: detail.name,
    imageList: detail.images,
    currentImageIndex: 1,
    contentReady: true,
  });

  await page.prepareCurrentShareImages(detail.images[1]);
  const share = page.onShareTimeline();

  assert.equal(share.title, `${detail.name}，建别墅从这里开始`);
  assert.equal(share.imageUrl, `timeline:${detail.images[1]}`);
  assert.match(share.query, /^id=21&image_ref=[a-z0-9]+$/);
  assert.doesNotMatch(share.query, /https|static\.example/);
  const friendShare = page.onShareAppMessage();
  assert.equal(friendShare.imageUrl, `local:${detail.images[1]}`);
  assert.match(friendShare.path, /^\/pages\/templatesquaredetails\/templatesquaredetails\?id=21&image_ref=[a-z0-9]+$/);
});

test('从朋友圈打开时按图片短标识恢复同一模板的同一张图', async () => {
  const { definition } = loadPage({ detail });
  const sourcePage = createPage(definition, {
    templateId: 21,
    imageList: detail.images,
    currentImageIndex: 1,
  });
  const imageRef = sourcePage.buildShareImageRef(detail.images[1]);
  const page = createPage(definition);

  page.onLoad({ id: '21', image_ref: imageRef });
  await new Promise(resolve => setImmediate(resolve));
  await new Promise(resolve => setImmediate(resolve));

  assert.equal(page.data.currentImageIndex, 1);
  assert.equal(page.data.contentReady, true);
  assert.equal(page.getCurrentShareSourceUrl(), detail.images[1]);
});

test('图片短标识过期时明确提示并回退当前模板第一张图', async () => {
  const { definition, toasts } = loadPage({ detail });
  const page = createPage(definition);

  page.onLoad({ id: '21', image_ref: 'expired' });
  await new Promise(resolve => setImmediate(resolve));
  await new Promise(resolve => setImmediate(resolve));

  assert.equal(page.data.currentImageIndex, 0);
  assert.equal(page.data.contentReady, true);
  assert.equal(toasts.some(item => item.title === '分享图片已更新，已展示该模板最新图片'), true);
});

test('模板加载失败时不开放朋友圈，也不泄漏旧模板内容', async () => {
  const { definition } = loadPage({ requestError: new Error('offline') });
  const page = createPage(definition, {
    template: { id: 99, name: '旧模板' },
    templateName: '旧模板',
    imageList: ['old.jpg'],
    contentReady: true,
  });

  page.onLoad({ id: '21' });
  await new Promise(resolve => setImmediate(resolve));

  assert.equal(page.data.contentReady, false);
  assert.equal(page.data.template, null);
  assert.equal(Object.keys(page.onShareTimeline()).length, 0);
});

test('快速切换图片时较慢的旧封面结果不能覆盖新图', async () => {
  const pendingFriend = new Map();
  const pendingTimeline = new Map();
  const { definition } = loadPage({
    prepareShareCardImage(url) {
      return new Promise(resolve => pendingFriend.set(url, resolve));
    },
    prepareTimelineShareImage(url) {
      return new Promise(resolve => pendingTimeline.set(url, resolve));
    },
  });
  const page = createPage(definition, {
    templateId: 21,
    template: detail,
    templateName: detail.name,
    imageList: detail.images,
    currentImageIndex: 0,
    contentReady: true,
  });

  const first = page.prepareCurrentShareImages(detail.images[0]);
  page.data.currentImageIndex = 1;
  const second = page.prepareCurrentShareImages(detail.images[1]);
  pendingFriend.get(detail.images[1])('local:second');
  pendingTimeline.get(detail.images[1])('timeline:second');
  await second;
  pendingFriend.get(detail.images[0])('local:first');
  pendingTimeline.get(detail.images[0])('timeline:first');
  await first;

  assert.equal(page.data.shareImageUrl, 'local:second');
  assert.equal(page.data.timelineShareImageUrl, 'timeline:second');
  assert.equal(page.onShareTimeline().imageUrl, 'timeline:second');
  assert.equal(page.onShareAppMessage().imageUrl, 'local:second');
  assert.equal(page.onShareTimeline().query, `id=21&image_ref=${page.buildShareImageRef(detail.images[1])}`);
});

test('朋友圈方图准备失败时保持关闭，重试成功后才开放且不回退远程原图', async () => {
  let timelineAttempts = 0;
  const { definition } = loadPage({
    prepareShareCardImage: async () => 'local:friend',
    prepareTimelineShareImage: async () => {
      timelineAttempts += 1;
      return timelineAttempts === 1 ? '' : 'local:timeline-square';
    },
  });
  const page = createPage(definition, {
    templateId: 21,
    template: detail,
    templateName: detail.name,
    imageList: detail.images,
    currentImageIndex: 0,
  });

  await page.prepareCurrentShareImages(detail.images[0]);
  assert.equal(page.data.contentReady, false);
  assert.equal(page.getPreparedTimelineShareImageUrl(detail.images[0]), '');
  assert.equal(Object.keys(page.onShareTimeline()).length, 0);

  await page.prepareCurrentShareImages(detail.images[0]);
  assert.equal(page.data.contentReady, true);
  assert.equal(page.onShareTimeline().imageUrl, 'local:timeline-square');
});

test('TypeScript 与 JavaScript 都保留模板分享图片标识和内容就绪保护', () => {
  for (const extension of ['ts', 'js']) {
    const source = fs.readFileSync(path.join(root, `pages/templatesquaredetails/templatesquaredetails.${extension}`), 'utf8');
    assert.match(source, /buildShareImageRef/);
    assert.match(source, /requestedShareImageRef/);
    assert.match(source, /contentReady/);
    assert.match(source, /image_ref/);
    assert.match(source, /prepareTimelineShareImage/);
    assert.match(source, /timelineShareImageUrl/);
  }
});
