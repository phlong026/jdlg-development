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
  const source = require('node:child_process').execFileSync(compiler, ['./pages/contentreader/contentreader.wxml'], { cwd: root, encoding: 'utf8', timeout: 10000 });
  const context = { window: {}, console };
  vm.createContext(context); vm.runInContext(source, context);
  const render = context.$gwx('./pages/contentreader/contentreader.wxml');
  const data = { __profileAccessMounted: true, loading: false, contentReady: true, error: '刷新失败，请重试', content: { title: '已验证正文', body: '保留的文章文字' }, media: [], articleBlocks: [] };
  const retained = JSON.stringify(render(data));
  assert.match(retained, /保留的文章文字/);
  assert.match(retained, /刷新失败，请重试/);
  const blocked = JSON.stringify(render({ ...data, contentReady: false, content: null }));
  assert.doesNotMatch(blocked, /保留的文章文字|已验证正文/);
  assert.match(blocked, /刷新失败，请重试/);
});

function harness() {
  let definition;
  const pendingReads = [];
  let reads = 0;
  const tracking = {
    captureEntryValidity: () => () => true,
    resolvePageOptions() {},
    refreshConfig: async () => ({ enabled: false }),
    consented: () => false,
    preheatTimelineShare() {},
  };
  const api = {
    resolveReaderOptions: async options => options,
    mediaURL: value => value,
    request: async path => {
      assert.match(path, /^\/contents\//);
      reads += 1;
      if (reads === 1) return { title: '已显示正文', body: '旧正文', media: [] };
      return new Promise((resolve, reject) => { pendingReads.push({ resolve, reject }); });
    },
  };
  const wx = {
    hideShareMenu() {}, showShareMenu() {},
    getWindowInfo: () => ({ windowWidth: 400 }),
    createVideoContext: () => ({ pause() {} }),
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.js'), 'utf8'), {
    Page: value => { definition = value; }, wx, Promise, Object, Array, String, Number, Math, Error, encodeURIComponent,
    require: name => name.includes('tracking/index') ? tracking
      : name.includes('tracking/api') ? api
      : name.includes('protected-media') ? { resolveProtectedMediaURL: async value => value }
      : name.includes('article-document') ? { renderArticleDocument: () => [] }
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
  };
}

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
