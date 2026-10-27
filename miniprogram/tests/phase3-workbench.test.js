'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function loadAPI() {
  const calls = [];
  const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');
  const module = { exports: {} };
  const wx = {
    getStorageSync: key => key === 'token' ? 'session-token' : key === 'device_fingerprint' ? 'device-one' : '',
    request(options) { calls.push({ kind: 'request', options }); options.success({ statusCode: 200, data: { code: 0, data: { items: [], next_cursor: 'next-one' } } }); },
    downloadFile(options) { calls.push({ kind: 'download', options }); options.success({ statusCode: 200, tempFilePath: '/tmp/private-media' }); },
    uploadFile(options) { calls.push({ kind: 'upload', options }); options.success({ statusCode: 200, data: JSON.stringify({ code: 0, data: { media_id: 'asset-0001' } }) }); },
  };
  vm.runInNewContext(source, { module, exports: module.exports, wx, require(id) {
    if (id === './parameter') return {
      generateRequestParams(token, body, apiPath, device) { return { token, body, apiPath, device }; },
      paramsToHeaders(params) { return { signedPath: params.apiPath, token: params.token }; },
    };
    throw new Error(`unexpected require ${id}`);
  }, Promise, URLSearchParams, JSON, Error, Object, String, Number, encodeURIComponent });
  return { api: module.exports, calls };
}

function pageHarness(name, api, wxOverrides = {}) {
  let definition;
  const source = fs.readFileSync(path.join(root, `pages/${name}/${name}.js`), 'utf8');
  const wx = Object.assign({ getStorageSync: () => 'session-token', redirectTo() {}, navigateTo() {}, stopPullDownRefresh() {}, showToast() {}, showModal() {} }, wxOverrides);
  vm.runInNewContext(source, { Page(value) { definition = value; }, wx, require(id) { if (id === '../../utils/phase3-api') return api; throw new Error(`unexpected require ${id}`); }, console, Promise, JSON, Error, Object, String, Number, Date, Map, Set, encodeURIComponent });
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values) { Object.assign(this.data, values); } }, definition);
  return page;
}
const tick = () => new Promise(resolve => setImmediate(resolve));

test('phase3 API signs authenticated workbench and paginated self-visit requests', async () => {
  const { api, calls } = loadAPI();
  await api.workbenchContext();
  await api.listMyShares({ cursor: 'cursor one', as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' });
  assert.equal(calls[0].options.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/workbench/context');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/workbench/context');
  assert.match(calls[1].options.url, /cursor=cursor%20one/);
  assert.match(calls[1].options.url, /as_of=2026-10-27T00%3A00%3A00Z/);
  assert.equal(calls[1].options.header.signedPath, '/api/v1/miniprogram/tracking/my-shares');
});

test('private draft media is downloaded or uploaded with login signatures', async () => {
  const { api, calls } = loadAPI();
  const local = await api.downloadWorkbenchMedia('asset-0001');
  const uploaded = await api.uploadWorkbenchMedia('/tmp/source.jpg', 'image', 'owner-a', 'own');
  assert.equal(local, '/tmp/private-media');
  assert.equal(uploaded.media_id, 'asset-0001');
  assert.equal(calls[0].kind, 'download');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/workbench/media/asset-0001');
  assert.equal(calls[1].kind, 'upload');
  assert.equal(calls[1].options.formData.owner_id, 'owner-a');
  assert.ok(!calls[0].options.url.includes('tracking/media'));
});

test('private phase3 pages are registered, built, non-shareable, and expose real state branches', () => {
  const app = JSON.parse(fs.readFileSync(path.join(root, 'app.json'), 'utf8'));
  for (const page of ['contentworkbench', 'contentedit', 'sharevisits', 'visitdetail']) {
    assert.ok(app.subPackages.some(pack => pack.root === `pages/${page}` && pack.pages.includes(page)));
    const config = JSON.parse(fs.readFileSync(path.join(root, `pages/${page}/${page}.json`), 'utf8'));
    assert.notEqual(config.enableShareAppMessage, true);
    const source = fs.readFileSync(path.join(root, `pages/${page}/${page}.ts`), 'utf8');
    assert.doesNotMatch(source, /onShareAppMessage|onShareTimeline/);
    assert.match(source, /onShow\(\)/);
    assert.match(source, /onUnload\(\)/);
    assert.match(source, /hideShareMenu\(\)/);
    assert.match(source, /current\(epoch\)/);
    assert.match(source, /loading|error/);
  }
  const build = fs.readFileSync(path.join(__dirname, '../scripts/build-tracking.js'), 'utf8');
  for (const page of ['contentworkbench', 'contentedit', 'sharevisits', 'visitdetail']) assert.match(build, new RegExp(`pages/${page}/${page}\\.ts`));
  const trackingRoutes = require(path.join(root, 'utils/tracking/routes.js'));
  for (const page of ['contentworkbench', 'contentedit', 'sharevisits', 'visitdetail']) assert.equal(trackingRoutes.policy(`pages/${page}/${page}`).collect, false);
});

test('editor uses signed private previews and explicit save-preview-publish-revoke actions', () => {
  const source = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.ts'), 'utf8');
  assert.match(source, /downloadWorkbenchMedia/);
  assert.match(source, /saveContent/);
  assert.match(source, /previewContent/);
  assert.match(source, /publishContent/);
  assert.match(source, /revokeContent/);
  assert.match(source, /article_v2/);
  assert.doesNotMatch(source, /tracking\/media|share_token|url_link/);
});

test('self-share pages paginate only with server cursors and never invent traffic or links', () => {
  const list = fs.readFileSync(path.join(root, 'pages/sharevisits/sharevisits.ts'), 'utf8');
  const detail = fs.readFileSync(path.join(root, 'pages/visitdetail/visitdetail.ts'), 'utf8');
  assert.match(list, /next_cursor/);
  assert.match(detail, /next_cursor/);
  assert.match(list, /visitor_count/);
  assert.match(detail, /visitor/);
  assert.doesNotMatch(list + detail, /share_token|url_link|Math\.random/);
});

test('my page links to the two private phase3 entry pages behind login', () => {
  const source = fs.readFileSync(path.join(root, 'pages/my/my.ts'), 'utf8');
  assert.match(source, /\/pages\/contentworkbench\/contentworkbench/);
  assert.match(source, /\/pages\/sharevisits\/sharevisits/);
  assert.match(source, /isLoggedIn\(\)/);
});

test('workbench distinguishes feature-disabled from explicit manage denial', async () => {
  const disabled = pageHarness('contentworkbench', { workbenchContext: async () => ({ enabled: false, can_manage: true }) });
  await disabled.load(true);
  assert.equal(disabled.data.disabled, true);
  assert.equal(disabled.data.denied, false);
  const denied = pageHarness('contentworkbench', { workbenchContext: async () => ({ enabled: true, can_manage: false }) });
  await denied.load(true);
  assert.equal(denied.data.denied, true);
  assert.equal(denied.data.disabled, false);
});

test('self-share pagination appends only the server page and replays snapshot fields', async () => {
  const queries = [];
  const pages = [
    { items: [{ share_id: 'share-one', visitor_count: 2, page_views: 3 }], next_cursor: 'next-one', as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' },
    { items: [{ share_id: 'share-two', visitor_count: 1, page_views: 1 }], as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' },
  ];
  const page = pageHarness('sharevisits', { listMyShares: async query => { queries.push(query); return pages.shift(); } });
  await page.load(true);
  await page.load(false);
  assert.deepEqual(page.data.items.map(item => item.share_id), ['share-one', 'share-two']);
  assert.equal(queries[1].cursor, 'next-one');
  assert.equal(queries[1].as_of, '2026-10-27T00:00:00Z');
  assert.equal(queries[1].metric_version, 'visible-v1');
});

test('editor serializes paragraph-image-video order and only referenced media IDs', async () => {
  let savedDraft;
  const api = {
    saveContent: async draft => { savedDraft = draft; return { content_id: 'content-one', revision: 1, version: 0, status: 'draft' }; },
    previewContent: async () => ({ preview_token: 'preview-one', content: {} }),
  };
  const page = pageHarness('contentedit', api);
  page.setData({ title: '项目说明', owner_id: 'owner-a', blocks: [
    { kind: 'paragraph', text: '第一段' },
    { kind: 'image', media_id: 'asset-image' },
    { kind: 'video', media_id: 'asset-video' },
    { kind: 'image', media_id: 'asset-image' },
  ] });
  await page.saveAndPreview();
  const document = JSON.parse(savedDraft.body);
  assert.deepEqual(document.blocks.map(block => block.kind), ['paragraph', 'image', 'video', 'image']);
  assert.deepEqual(Array.from(savedDraft.media_ids), ['asset-image', 'asset-video']);
  assert.equal(savedDraft.body_format, 'article_v2');
});

test('editor eagerly downloads only referenced private media and leaves the library on demand', async () => {
  const downloads = [];
  const page = pageHarness('contentedit', {
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'] }),
    listWorkbenchMedia: async () => ({ items: [
      { media_id: 'asset-body', kind: 'image' },
      { media_id: 'asset-cover', kind: 'image' },
      { media_id: 'asset-unused', kind: 'video' },
    ] }),
    getContent: async () => ({ title: '已有文章', owner_id: 'owner-a', rights: 'own', revision: 1, version: 0, status: 'draft', body_format: 'article_v2', body: JSON.stringify({ schema: 1, blocks: [{ kind: 'image', media_id: 'asset-body' }] }), cover: { media_id: 'asset-cover' } }),
    downloadWorkbenchMedia: async id => { downloads.push(id); return '/tmp/' + id; },
  });
  page.setData({ id: 'content-one' });
  await page.load();
  assert.deepEqual(downloads.sort(), ['asset-body', 'asset-cover']);
  assert.equal(page.data.media.find(item => item.media_id === 'asset-unused').local_path, '');
});

test('legacy plain and rich text content stays read-only without implicit article conversion', async () => {
  let saves = 0;
  for (const [format, body] of [['plain', '原始纯文本'], ['rich_text', '<p><strong>原文</strong></p>']]) {
    const page = pageHarness('contentedit', {
      workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'] }),
      listWorkbenchMedia: async () => ({ items: [{ media_id: 'legacy-image', kind: 'image' }] }),
      getContent: async () => ({ title: '旧内容', owner_id: 'owner-a', rights: 'own', revision: 2, version: 1, status: 'published', body_format: format, body, media: [{ media_id: 'legacy-image', kind: 'image' }] }),
      downloadWorkbenchMedia: async id => '/tmp/' + id,
      saveContent: async () => { saves += 1; },
    });
    page.setData({ id: 'legacy-content' });
    await page.load();
    assert.equal(page.data.legacyReadOnly, true);
    assert.equal(page.data.body_format, format);
    assert.equal(page.data.legacyBody, body);
    assert.equal(page.data.media[0].local_path, '/tmp/legacy-image');
    await page.saveAndPreview();
    assert.match(page.data.error, /后台编辑/);
  }
  assert.equal(saves, 0);
  const wxml = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /legacyReadOnly/);
  assert.match(wxml, /rich-text/);
});

test('publish preview renders normalized server content and only its signed private media', async () => {
  const downloads = [];
  let imageAttempts = 0;
  const page = pageHarness('contentedit', {
    saveContent: async () => ({ content_id: 'content-one', revision: 3, version: 0, status: 'draft' }),
    previewContent: async () => ({
      preview_token: 'preview-one',
      content: {
        title: '服务端标题', body_format: 'article_v2', cover: { media_id: 'server-cover', kind: 'image' },
        body: JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '服务端归一化正文' }, { kind: 'list', ordered: true, items: ['第一项', '第二项'] }, { kind: 'image', media_id: 'server-image', caption: '服务端图说' }] }),
        media: [{ media_id: 'server-image', kind: 'image' }],
      },
    }),
    downloadWorkbenchMedia: async id => {
      downloads.push(id);
      if (id === 'server-image' && imageAttempts++ === 0) throw new Error('下载失败');
      return '/tmp/' + id;
    },
  });
  page._token = 'session-token'; page._epoch = 1;
  page.setData({ title: '本地标题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '本地未归一化正文' }], media: [{ media_id: 'local-extra', kind: 'image', local_path: '/tmp/local-extra' }] });
  await page.saveAndPreview();
  assert.equal(page.data.preview.content.title, '服务端标题');
  assert.equal(page.data.previewBlocks[0].text, '服务端归一化正文');
  assert.deepEqual(Array.from(page.data.previewBlocks[1].items), ['第一项', '第二项']);
  assert.equal(page.data.previewBlocks[2].caption, '服务端图说');
  assert.equal(page.data.previewBlocks[2].asset.local_path, '');
  assert.equal(page.data.previewBlocks[2].asset.preview_error, '预览暂不可用');
  assert.equal(page.data.previewCover.local_path, '/tmp/server-cover');
  await page.retryPreviewMedia({ currentTarget: { dataset: { mediaId: 'server-image' } } });
  assert.equal(page.data.previewBlocks[2].asset.local_path, '/tmp/server-image');
  assert.deepEqual(downloads.sort(), ['server-cover', 'server-image', 'server-image']);
  const wxml = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /item\.items/);
  assert.match(wxml, /item\.caption/);
  assert.match(wxml, /retryPreviewMedia/);
});

test('publish and revoke reuse the same idempotency key after a lost response', async () => {
  const publishKeys = [], revokeKeys = [];
  let publishCalls = 0, revokeCalls = 0, sequence = 0;
  const page = pageHarness('contentedit', {
    mutationKey: prefix => prefix + '-' + (++sequence),
    publishContent: async (_id, _revision, _preview, key) => {
      publishKeys.push(key); publishCalls += 1;
      if (publishCalls === 1) throw new Error('响应丢失');
      return { content: { revision: 4, version: 1, status: 'published' } };
    },
    revokeContent: async (_id, _revision, _reason, key) => {
      revokeKeys.push(key); revokeCalls += 1;
      if (revokeCalls === 1) throw new Error('响应丢失');
      return { revision: 5, status: 'revoked' };
    },
  });
  page._token = 'session-token'; page._epoch = 1;
  page.setData({ id: 'content-one', revision: 3, preview: { preview_token: 'preview-one' } });
  await page.publish(); await page.publish();
  assert.deepEqual(publishKeys, ['publish-1', 'publish-1']);
  page.setData({ revision: 4, version: 1, status: 'published' });
  await page.revokeContent('内容更新'); await page.revokeContent('内容更新');
  assert.deepEqual(revokeKeys, ['revoke-2', 'revoke-2']);
});

test('account change clears private share rows and ignores the previous account response', async () => {
  let resolveOld, calls = 0;
  const storage = { token: 'account-a' };
  const page = pageHarness('sharevisits', {
    listMyShares: async () => {
      calls += 1;
      if (calls === 1) return new Promise(resolve => { resolveOld = resolve; });
      return { items: [{ share_id: 'share-account-b' }], as_of: '2026-10-27T01:00:00Z', metric_version: 'visible-v1' };
    },
  }, { getStorageSync: key => storage[key] || '', hideShareMenu() {} });
  page.onShow(); await tick();
  storage.token = 'account-b'; page.onShow(); await tick();
  resolveOld({ items: [{ share_id: 'share-account-a' }], as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' }); await tick();
  assert.deepEqual(page.data.items.map(item => item.share_id), ['share-account-b']);
});

test('account change clears editor data and ignores the previous account load', async () => {
  let resolveOldContext, contextCalls = 0;
  const storage = { token: 'account-a' };
  const page = pageHarness('contentedit', {
    workbenchContext: async () => {
      contextCalls += 1;
      if (contextCalls === 1) return new Promise(resolve => { resolveOldContext = resolve; });
      return { enabled: true, can_manage: true, owners: ['owner-b'] };
    },
    listWorkbenchMedia: async () => ({ items: [] }),
    getContent: async () => ({ title: 'B 账号草稿', owner_id: 'owner-b', rights: 'own', body_format: 'plain', body: '新内容' }),
  }, { getStorageSync: key => storage[key] || '', hideShareMenu() {} });
  page.setData({ id: 'content-one', title: 'A 账号旧数据' });
  page.onShow(); await tick();
  storage.token = 'account-b'; page.onShow(); await tick(); await tick();
  resolveOldContext({ enabled: true, can_manage: true, owners: ['owner-a'] }); await tick();
  assert.equal(page.data.title, 'B 账号草稿');
  assert.equal(page.data.owner_id, 'owner-b');
});

test('refresh supersedes delayed load-more and duplicate load-more stays single-flight', async () => {
  let resolveMore, calls = 0;
  const page = pageHarness('sharevisits', {
    listMyShares: async query => {
      calls += 1;
      if (calls === 1) return { items: [{ share_id: 'first' }], next_cursor: 'next-one', as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' };
      if (query.cursor) return new Promise(resolve => { resolveMore = resolve; });
      return { items: [{ share_id: 'refreshed' }], as_of: '2026-10-27T01:00:00Z', metric_version: 'visible-v1' };
    },
  }, { hideShareMenu() {}, stopPullDownRefresh() {} });
  page.onShow(); await tick();
  page.loadMore(); page.loadMore(); await tick();
  assert.equal(calls, 2);
  const refresh = page.load(true); await tick();
  resolveMore({ items: [{ share_id: 'stale-more' }] }); await refresh; await tick();
  assert.deepEqual(page.data.items.map(item => item.share_id), ['refreshed']);
});
