'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.js');
const tick = () => new Promise(resolve => setImmediate(resolve));

test('content pages explicitly register their custom navigation component', () => {
  for (const name of ['contentworkbench', 'contentedit', 'contentwriting', 'contentreader', 'sharevisits', 'visitdetail']) {
    const config = JSON.parse(fs.readFileSync(path.join(__dirname, '../miniprogram/pages', name, name + '.json'), 'utf8'));
    assert.equal(config.usingComponents && config.usingComponents['navigation-bar'], '/components/navigation-bar/navigation-bar');
  }
});

function runtime(overrides = {}) {
  let definition;
  const queries = [], navigations = [];
  const api = Object.assign({
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'] }),
    listContents: async query => { queries.push(query); return { items: [{ content_id: 'article-a', title: '庭院图文', type: 'article', status: 'draft', media: [] }, { content_id: 'video-a', title: '庭院视频', type: 'article', status: 'published', media: [{ kind: 'video', media_id: 'video-asset' }] }, { content_id: 'document-a', title: '庭院方案.pdf', type: 'document', status: 'draft', media: [{ kind: 'document', media_id: 'document-asset' }] }], next_cursor: 'page-2', as_of: '2026-10-27T00:00:00Z', metric_version: 'v1' }; },
    importCapabilities: async () => ({ enabled: true, platforms: [{ platform: 'wechat_article', available: true }] }),
    listImports: async () => ({ items: [] }),
  }, overrides);
  vm.runInNewContext(fs.readFileSync(source, 'utf8'), {
    Page: value => { definition = value; }, require: () => api,
    wx: { getStorageSync: () => 'account-a', hideShareMenu() {}, stopPullDownRefresh() {}, navigateTo: value => navigations.push(value.url), showToast() {}, getClipboardData: options => options.success({ data: 'https://mp.weixin.qq.com/s/original' }) },
    setTimeout, clearTimeout, console,
  });
  const page = Object.assign({}, definition, { data: JSON.parse(JSON.stringify(definition.data)), setData(values, callback) { for (const [key, value] of Object.entries(values)) { const parts = key.split('.'); let object = this.data; for (const part of parts.slice(0, -1)) object = object[part]; object[parts.at(-1)] = value; } if (callback) callback(); } });
  return { page, queries, navigations };
}

test('content studio searches the authorized catalog and resets cursor snapshots', async () => {
  const { page, queries } = runtime();
  page.onShow(); await tick(); await tick();
  page.setKeyword({ detail: { value: '庭院' } });
  await page.searchContents();
  const query = queries.at(-1);
  assert.equal(query.content_q, '庭院');
  assert.equal(query.cursor, undefined);
  assert.equal(query.as_of, undefined);
  assert.equal(page.data.items.length, 3);
  assert.equal(page.data.searchTerm, '庭院');
});

test('content categories filter loaded records without manufacturing assets or totals', async () => {
  const { page } = runtime();
  page.onShow(); await tick(); await tick();
  page.selectCategory({ currentTarget: { dataset: { key: 'video' } } });
  assert.equal(page.data.visibleItems.length, 1);
  assert.equal(page.data.visibleItems[0].content_id, 'video-a');
  page.selectCategory({ currentTarget: { dataset: { key: 'note' } } });
  assert.equal(page.data.category, 'video');
  page.selectCategory({ currentTarget: { dataset: { key: 'document' } } });
  assert.equal(page.data.category, 'document');
  assert.equal(page.data.visibleItems.length, 1);
  assert.equal(page.data.visibleItems[0].content_id, 'document-a');
  assert.equal(page.data.next_cursor, 'page-2');
});

test('disabling the import service keeps content usable without requesting import tasks', async () => {
  let importListCalls = 0;
  const { page } = runtime({
    importCapabilities: async () => ({ enabled: false, reason: '链接导入暂未开通', platforms: [] }),
    listImports: async () => { importListCalls += 1; throw Object.assign(new Error('内容导入服务暂不可用'), { statusCode: 503 }); },
  });
  page.onShow(); await tick(); await tick();
  assert.equal(page.data.importEnabled, false);
  assert.equal(page.data.items[0].content_id, 'article-a');
  assert.equal(page.data.imports.length, 0);
  assert.equal(importListCalls, 0);
});

test('pagination reuses a pending cover download and late covers cannot cross accounts', async () => {
  const calls = [];
  let finishCover;
  const { page } = runtime({
    listContents: async query => ({ items: [{ content_id: query.cursor ? 'article-b' : 'article-a', type: 'article', media: [], cover: { kind: 'image', media_id: query.cursor ? 'cover-b' : 'cover-a' } }], next_cursor: query.cursor ? '' : 'page-2' }),
    downloadWorkbenchMedia: id => { calls.push(id); if (id === 'cover-a') return new Promise(resolve => { finishCover = resolve; }); return Promise.resolve('/tmp/b'); },
  });
  page.onShow(); await tick(); await tick();
  await page.load(false, page._epoch); await tick();
  assert.equal(calls.filter(id => id === 'cover-a').length, 1);
  page.onHide();
  finishCover('/tmp/old-account-cover'); await tick();
  assert.equal(page.data.items[0].cover_path, '');
});

test('link import is an explicit view and paste reuses the existing submission form', () => {
  const { page, navigations } = runtime();
  page.onLoad({ view: 'import' });
  assert.equal(page.data.view, 'import');
  page.pasteImportLink();
  assert.equal(page.data.importURL, 'https://mp.weixin.qq.com/s/original');
  page.openImportView();
  assert.equal(navigations[0], '/pages/contentworkbench/contentworkbench?view=import');
});

test('studio offers real destinations for existing assets and visits', () => {
  const { page, navigations } = runtime();
  page.openExistingAssets(); page.openShareVisits();
  assert.deepEqual(navigations, ['/pages/nativereferences/nativereferences', '/pages/sharevisits/sharevisits']);
});
