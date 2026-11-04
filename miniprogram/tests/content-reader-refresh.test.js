'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');
const tick = () => new Promise(resolve => setImmediate(resolve));

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

test('reader removes previously displayed content after a verified inaccessible response', async () => {
  const { page, fail } = harness();
  page.pageVisible = true;
  await page.loadContent();
  page.setData({ media: [{ media_id: 'old-media' }], articleBlocks: [{ key: 'old-block' }] });
  const pending = page.loadContent();
  await tick();
  const forbidden = Object.assign(new Error('forbidden'), { statusCode: 403 });
  fail(forbidden);
  await pending;
  assert.equal(page.data.content, null);
  assert.deepEqual(Array.from(page.data.media), []);
  assert.deepEqual(Array.from(page.data.articleBlocks), []);
  assert.equal(page.data.contentReady, false);
  assert.match(page.data.error, /不可访问/);
});

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
