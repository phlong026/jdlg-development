'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function loadAPI(responseData = { items: [], next_cursor: 'next-one' }) {
  const calls = [];
  const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');
  const module = { exports: {} };
  const wx = {
    getStorageSync: key => key === 'token' ? 'session-token' : key === 'device_fingerprint' ? 'device-one' : '',
    request(options) { calls.push({ kind: 'request', options }); options.success({ statusCode: 200, data: { code: 0, data: responseData } }); },
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
  const wx = Object.assign({
    getStorageSync: () => 'session-token',
    getWindowInfo: () => ({ windowWidth: 375 }),
    createVideoContext: () => ({ pause() {} }),
    redirectTo() {}, navigateTo() {}, stopPullDownRefresh() {}, showToast() {}, showModal() {},
  }, wxOverrides);
  vm.runInNewContext(source, { Page(value) { definition = value; }, wx, require(id) {
    if (id === '../../utils/phase3-api') return api;
    if (id === '../../utils/article-document') return require(path.join(root, 'utils/article-document.js'));
    throw new Error(`unexpected require ${id}`);
  }, console, Promise, JSON, Error, Object, String, Number, Date, Map, Set, encodeURIComponent });
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)), setData(values) { Object.assign(this.data, values); } }, definition);
  return page;
}
const tick = () => new Promise(resolve => setImmediate(resolve));

test('workbench ownership labels follow authorized ID order without exposing internal identifiers', async () => {
  const original = { owners: ['designer-40', 'team-a'], owner_choices: [
    { owner_id: 'team-a', display_name: '设计团队' },
    { owner_id: 'outside', display_name: '无权限组织' },
    { owner_id: 'designer-40', display_name: '设计师昵称' },
  ] };
  const context = await loadAPI(original).api.workbenchContext();
  assert.deepEqual(JSON.parse(JSON.stringify(context.owner_choices)), [
    { owner_id: 'designer-40', display_name: '设计师昵称' },
    { owner_id: 'team-a', display_name: '设计团队' },
  ]);
  assert.deepEqual(context.owners, original.owners);
  const legacy = await loadAPI({ owners: ['designer-40'] }).api.workbenchContext();
  assert.equal(legacy.owner_choices[0].display_name, '我的内容');
  assert.equal(legacy.owner_choices[0].owner_id, 'designer-40');
  const missing = await loadAPI({ owners: ['owner-a', 'owner-b'], owner_choices: [{ owner_id: 'owner-a', display_name: '  ' }] }).api.workbenchContext();
  assert.deepEqual(Array.from(missing.owner_choices, item => item.display_name), ['内容归属 1', '内容归属 2']);
});

test('phase3 API signs authenticated workbench and paginated self-visit requests', async () => {
  const { api, calls } = loadAPI();
  await api.workbenchContext();
  await api.mySharesContext();
  await api.listMyShares({ cursor: 'cursor one', as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' });
  assert.equal(calls[0].options.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/workbench/context');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/workbench/context');
  assert.equal(calls[1].options.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/my-shares/context');
  assert.equal(calls[1].options.header.signedPath, '/api/v1/miniprogram/tracking/my-shares/context');
  assert.match(calls[2].options.url, /cursor=cursor%20one/);
  assert.match(calls[2].options.url, /as_of=2026-10-27T00%3A00%3A00Z/);
  assert.equal(calls[2].options.header.signedPath, '/api/v1/miniprogram/tracking/my-shares?cursor=cursor%20one&as_of=2026-10-27T00%3A00%3A00Z&metric_version=visible-v1');
});

test('content editor receives existing media from the picker without uploading or replacing current text', async () => {
  let picker;
  const page = pageHarness('contentedit', { downloadWorkbenchMedia: async () => '/tmp/existing-image' }, {
    navigateTo(options) { picker = options; },
  });
  page._token = 'session-token'; page._epoch = 2;
  page.data.blocks = [{ kind: 'paragraph', text: '保留正文' }]; page.data.media = [];
  page.openMediaPicker();
  assert.match(picker.url, /picker=media/);
  await picker.events.selectMaterial({ kind: 'media', asset: { media_id: 'existing-image', kind: 'image', title: '已有图' } });
  assert.equal(page.data.blocks[0].text, '保留正文');
  assert.equal(page.data.blocks.at(-1).media_id, 'existing-image');
  assert.equal(page.data.media[0].local_path, '/tmp/existing-image');
});

test('phase3 API production cryptography signs the exact encoded URL path and preserves session inputs', async () => {
  const calls = [];
  const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');
  const module = { exports: {} };
  const ts = require('./report-runtime')('typescript');
  const parameter = {};
  const parameterSource = fs.readFileSync(path.join(root, 'utils/parameter.ts'), 'utf8');
  const parameterJS = ts.transpileModule(parameterSource, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(parameterJS, {
    exports: parameter,
    require(id) { return require(path.join(root, 'utils', id)); },
    Date, JSON, Error, Object, String, Number,
  });
  const token = 'session-token';
  const device = 'cached-device';
  const query = { content_q: '中 文&方案', limit: 20, cursor: '页 一&二', as_of: '2026-10-31T00:00:00Z' };
  const encoded = Object.entries(query).map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`).join('&');
  const fullPath = `/api/v1/miniprogram/workbench/contents?${encoded}`;
  const originalNow = Date.now;
  Date.now = () => 1793376000000;
  try {
    const wx = {
      getStorageSync: key => key === 'token' ? token : key === 'device_fingerprint' ? device : '',
      request(options) { calls.push(options); options.success({ statusCode: 200, data: { code: 0, data: {} } }); },
    };
    vm.runInNewContext(source, {
      module, exports: module.exports, wx,
      require(id) { assert.equal(id, './parameter'); return parameter; },
      Promise, JSON, Error, Object, String, Number, encodeURIComponent,
    });
    await module.exports.listContents(query);
    const expected = parameter.paramsToHeaders(parameter.generateRequestParams(token, {}, fullPath, device));
    assert.equal(calls[0].url, `https://api.jiadilingguang.com${fullPath}`);
    assert.equal(calls[0].header.token, token);
    assert.equal(calls[0].header['md5-signature'], expected['md5-signature']);
    assert.equal(calls[0].header.pass, expected.pass);
    assert.equal(calls[0].header.tm, expected.tm);
  } finally {
    Date.now = originalNow;
  }
});

test('private draft media is downloaded or uploaded with login signatures', async () => {
  const { api, calls } = loadAPI();
  const local = await api.downloadWorkbenchMedia('asset-0001');
  const uploaded = await api.uploadWorkbenchMedia('/tmp/source.jpg', 'image', 'owner-a', 'own');
  await api.uploadWorkbenchMedia('/tmp/wxfile', 'document', 'owner-a', 'own', '方案.pdf');
  assert.equal(local, '/tmp/private-media');
  assert.equal(uploaded.media_id, 'asset-0001');
  assert.equal(calls[0].kind, 'download');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/workbench/media/asset-0001');
  assert.equal(calls[0].options.url, `https://api.jiadilingguang.com${calls[0].options.header.signedPath}`);
  assert.equal(calls[1].kind, 'upload');
  assert.equal(calls[1].options.url, `https://api.jiadilingguang.com${calls[1].options.header.signedPath}`);
  assert.equal(calls[1].options.formData.owner_id, 'owner-a');
  assert.equal(calls[2].options.formData.original_name, '方案.pdf');
  assert.ok(!calls[0].options.url.includes('tracking/media'));
});

test('draft deletion uses the signed workbench endpoint and expected revision', async () => {
  const { api, calls } = loadAPI();
  await api.deleteContentDraft('draft-content-001', 3);
  assert.equal(calls[0].options.method, 'DELETE');
  assert.equal(calls[0].options.header.signedPath, '/api/v1/miniprogram/workbench/contents/draft-content-001');
  assert.equal(calls[0].options.data.expected_revision, 3);
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
  await page.loadShares(true);
  await page.loadShares(false);
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

test('editor uploads a PDF or DOCX as a private document draft and reopens the original file on demand', async () => {
  const calls = [];
  const api = {
    uploadWorkbenchMedia: async (path, kind, owner, rights, originalName) => {
      calls.push({ action: 'upload', path, kind, owner, rights, originalName });
      return { media_id: 'document-asset-0001', kind: 'document', mime: 'application/pdf', title: '庭院方案.pdf' };
    },
    saveContent: async draft => {
      calls.push({ action: 'save', draft });
      return { content_id: 'document-content-0001', revision: 1, version: 0, status: 'draft' };
    },
    downloadWorkbenchMedia: async id => { calls.push({ action: 'download', id }); return '/tmp/private-document.pdf'; },
  };
  let opened = '';
  const page = pageHarness('contentedit', api, {
    chooseMessageFile(options) { options.success({ tempFiles: [{ path: '/tmp/source.pdf', name: '庭院方案.pdf', size: 1024 }] }); },
    openDocument(options) { opened = options.filePath; options.success(); },
  });
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
  page.setData({ context: { media_upload_configured: true }, owner_id: 'owner-a', rights: 'own', title: '' });
  page.chooseDocument();
  await tick(); await tick();
  assert.deepEqual(calls[0], { action: 'upload', path: '/tmp/source.pdf', kind: 'document', owner: 'owner-a', rights: 'own', originalName: '庭院方案.pdf' });
  assert.equal(calls[1].action, 'save');
  assert.equal(calls[1].draft.body_format, 'document_v1');
  assert.deepEqual(Array.from(calls[1].draft.media_ids), ['document-asset-0001']);
  assert.equal(page.data.id, 'document-content-0001');
  assert.equal(page.data.documentReadOnly, true);
  await page.openDocument();
  assert.equal(calls[2].id, 'document-asset-0001');
  assert.equal(opened, '/tmp/private-document.pdf');

  const wxml = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /bindtap="chooseDocument"/);
  assert.match(wxml, /bindtap="openDocument"/);
});

test('archiving a document preserves the existing or unsaved article and its next save target', async () => {
  for (const id of ['existing-article', '']) {
    const calls = [];
    const page = pageHarness('contentedit', {
      saveContent: async (draft, contentID) => {
        calls.push({ draft, contentID });
        return { content_id: contentID || (draft.body_format === 'document_v1' ? 'new-document' : 'new-article'), revision: 1, version: 0, status: 'draft' };
      },
    });
    page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
    page.setData({ id, title: '尚未保存的图文', revision: id ? 4 : 0, owner_id: 'owner-a', blocks: [
      { kind: 'paragraph', text: '尚未保存的正文' }, { kind: 'image', media_id: 'article-image' },
    ], cover_media_id: 'article-image' });
    const original = page.draftSignature();
    await page.archiveDocumentAsset({ media_id: 'document-asset', kind: 'document', title: '建筑图纸.pdf', mime: 'application/pdf' }, 1);
    assert.equal(page.draftSignature(), original);
    assert.equal(page.data.body_format, 'article_v2');
    assert.equal(page.data.documentReadOnly, false);
    assert.equal(page.data.busy, false);
    assert.equal(calls[0].draft.title, '建筑图纸');
    assert.equal(calls[0].contentID, undefined);
    assert.match(page.data.notice, /文档资料已单独保存/);
    await page.saveDraft();
    assert.equal(calls[1].contentID, id);
    assert.equal(calls[1].draft.body_format, 'article_v2');
    assert.equal(JSON.parse(calls[1].draft.body).blocks[0].text, '尚未保存的正文');
  }
});

test('typing while a document is being archived keeps the new article editable', async () => {
  let finishArchive;
  const page = pageHarness('contentedit', { saveContent: () => new Promise(resolve => { finishArchive = resolve; }) });
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
  page.setData({ owner_id: 'owner-a' });
  const pending = page.archiveDocumentAsset({ media_id: 'document-asset', title: '方案.pdf' }, 1);
  page.editBlock({ currentTarget: { dataset: { index: 0 } }, detail: { value: '上传时继续输入的正文' } });
  finishArchive({ content_id: 'new-document', revision: 1, version: 0, status: 'draft' });
  await pending;
  assert.equal(page.data.id, '');
  assert.equal(page.data.documentReadOnly, false);
  assert.equal(page.data.blocks[0].text, '上传时继续输入的正文');
});

test('document chooser reports real failures but ignores user cancellation', () => {
  const failures = ['chooseMessageFile:fail denied', 'chooseMessageFile:fail cancel'];
  const page = pageHarness('contentedit', {}, {
    chooseMessageFile(options) { options.fail({ errMsg: failures.shift() }); },
  });
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
  page.setData({ context: { media_upload_configured: true }, owner_id: 'owner-a', rights: 'own' });
  page.chooseDocument();
  assert.equal(page.data.error, '文档选择失败，请重试');
  page.setData({ error: '' });
  page.chooseDocument();
  assert.equal(page.data.error, '');
});

test('reopened editor archives an uploaded document without uploading it again', async () => {
  const calls = [];
  const page = pageHarness('contentedit', {
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'], media_upload_configured: true }),
    listWorkbenchMedia: async () => ({ items: [{ media_id: 'pending-document-01', kind: 'document', mime: 'application/pdf', title: '待归档方案.pdf' }] }),
    saveContent: async draft => {
      calls.push({ action: 'save', draft });
      return { content_id: 'document-content-01', revision: 1, version: 0, status: 'draft' };
    },
    uploadWorkbenchMedia: async () => { calls.push({ action: 'upload' }); throw new Error('不应重复上传'); },
  });
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
  await page.load(1);
  assert.equal(page.data.documentCandidates.length, 1);
  await page.archiveDocument({ currentTarget: { dataset: { index: 0 } } });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].action, 'save');
  assert.deepEqual(Array.from(calls[0].draft.media_ids), ['pending-document-01']);
  assert.equal(page.data.id, 'document-content-01');
  assert.equal(page.data.documentReadOnly, true);
});

test('editor explicitly soft-deletes only a never-published draft without revoking content', async () => {
  const calls = [];
  const page = pageHarness('contentedit', {
    deleteContentDraft: async (id, revision) => {
      calls.push({ action: 'delete', id, revision });
      return { content_id: id, revision: revision + 1, status: 'deleted', deleted_at: '2026-10-30T00:00:00Z' };
    },
    revokeContent: async () => { calls.push({ action: 'revoke' }); },
  }, {
    showModal(options) { options.success({ confirm: true }); },
    redirectTo(options) { calls.push({ action: 'redirect', url: options.url }); },
  });
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
  page.setData({ id: 'draft-content-001', revision: 3, version: 0, status: 'draft' });
  page.deleteDraft();
  await tick(); await tick();
  assert.deepEqual(calls, [
    { action: 'delete', id: 'draft-content-001', revision: 3 },
    { action: 'redirect', url: '/pages/contentworkbench/contentworkbench' },
  ]);
  assert.equal(page.data.deleted, true);
  const wxml = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /bindtap="deleteDraft"/);
  assert.match(wxml, /删除草稿/);
});

test('reopened document metadata stays read-only and downloads only after explicit open', async () => {
  const downloads = [];
  let opened = '';
  const page = pageHarness('contentedit', {
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'], media_upload_configured: true }),
    listWorkbenchMedia: async () => ({ items: [{ media_id: 'document-asset-0001', kind: 'document', mime: 'application/pdf', title: '庭院方案.pdf' }] }),
    getContent: async () => ({ content_id: 'document-content-0001', type: 'document', title: '庭院方案', owner_id: 'owner-a', rights: 'own', revision: 1, version: 0, status: 'draft', body_format: 'document_v1', body: '{"schema":1,"media_id":"document-asset-0001"}', media: [{ media_id: 'document-asset-0001', kind: 'document', mime: 'application/pdf', title: '庭院方案.pdf' }] }),
    downloadWorkbenchMedia: async id => { downloads.push(id); return '/tmp/reopened.pdf'; },
  }, { openDocument(options) { opened = options.filePath; options.success(); } });
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;; page.setData({ id: 'document-content-0001' });
  await page.load(1);
  assert.equal(page.data.documentReadOnly, true);
  assert.equal(page.data.documentAsset.media_id, 'document-asset-0001');
  assert.equal(page.data.media.length, 0, '文档不应混入图片/视频正文素材库');
  assert.equal(downloads.length, 0, '重开只加载元数据，不应自动下载原文件');
  await page.openDocument();
  assert.deepEqual(downloads, ['document-asset-0001']);
  assert.equal(opened, '/tmp/reopened.pdf');
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

test('saving keeps edits made during the request and the next save uses the returned revision', async () => {
  for (const action of ['saveDraft', 'saveAndPreview']) {
    const calls = [], responses = [];
    let previewCalls = 0;
    const page = pageHarness('contentedit', {
      saveContent: (draft, id) => { calls.push({ draft, id }); return new Promise(resolve => responses.push(resolve)); },
      previewContent: async () => { previewCalls++; return { preview_token: 'outdated-preview', content: {} }; },
    });
    page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
    page.setData({ id: 'content-one', revision: 4, title: '提交时的标题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '提交时的正文' }] });
    const pending = page[action]();
    page.editBlock({ currentTarget: { dataset: { index: 0 } }, detail: { value: '保存期间继续输入的正文' } });
    page.setField({ currentTarget: { dataset: { field: 'title' } }, detail: { value: '保存期间的新标题' } });
    await page[action]();
    assert.equal(calls.length, 1, '重复点击不能创建并发保存');
    responses[0]({ content_id: 'content-one', revision: 5, version: 0, status: 'draft' });
    await pending;
    assert.equal(page.data.title, '保存期间的新标题');
    assert.equal(page.data.blocks[0].text, '保存期间继续输入的正文');
    assert.equal(page.data.revision, 5);
    assert.equal(page.data.busy, false);
    assert.equal(page.data.preview, null);
    assert.match(page.data.notice, /未保存/);
    assert.equal(previewCalls, 0, '有新修改时不能继续生成旧内容的发布预览');
    const nextSave = page.saveDraft();
    assert.equal(calls[1].draft.expected_revision, 5);
    assert.equal(JSON.parse(calls[1].draft.body).blocks[0].text, '保存期间继续输入的正文');
    responses[1]({ content_id: 'content-one', revision: 6, version: 0, status: 'draft' });
    await nextSave;
    assert.match(page.data.notice, /草稿已保存/);
  }
});

test('edits during preview or preview-media requests prevent an outdated publish preview', async () => {
  for (const stage of ['preview', 'media']) {
    let finish;
    const preview = { preview_token: 'outdated-preview', content: { title: '旧预览', body_format: 'article_v2', body: JSON.stringify({ schema: 1, blocks: [{ kind: 'image', media_id: 'preview-image' }] }), media: [{ media_id: 'preview-image', kind: 'image' }] } };
    const page = pageHarness('contentedit', {
      saveContent: async () => ({ content_id: 'content-one', revision: 2, version: 0, status: 'draft' }),
      previewContent: () => stage === 'preview' ? new Promise(resolve => { finish = () => resolve(preview); }) : Promise.resolve(preview),
      downloadWorkbenchMedia: () => stage === 'media' ? new Promise(resolve => { finish = () => resolve('/tmp/preview-image'); }) : Promise.resolve('/tmp/preview-image'),
    });
    page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
    page.setData({ id: 'content-one', revision: 1, title: '图文', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '预览请求前的正文' }] });
    const pending = page.saveAndPreview();
    await tick();
    assert.equal(typeof finish, 'function');
    page.editBlock({ currentTarget: { dataset: { index: 0 } }, detail: { value: '等待预览期间继续编辑的正文' } });
    finish();
    await pending;
    assert.equal(page.data.blocks[0].text, '等待预览期间继续编辑的正文');
    assert.equal(page.data.preview, null);
    assert.equal(page.data.busy, false);
    assert.match(page.data.notice, /未保存/);
  }
});

test('a stale save response cannot overwrite or unlock a newer editor request', async () => {
  for (const action of ['saveDraft', 'saveAndPreview']) {
    const responses = [];
    const page = pageHarness('contentedit', {
      saveContent: () => new Promise(resolve => responses.push(resolve)),
      previewContent: async () => ({ preview_token: 'current-preview', content: {} }),
    });
    page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
    page.setData({ title: '旧页面标题', owner_id: 'owner-a', blocks: [{ kind: 'paragraph', text: '旧页面正文' }] });
    const oldRequest = page[action]();
    page.onUnload();
    page.setData({ id: 'current-content', revision: 7, busy: false, title: '新页面标题', blocks: [{ kind: 'paragraph', text: '新页面正文' }] });
    const currentRequest = page[action]();
    responses[0]({ content_id: 'stale-content', revision: 1, version: 0, status: 'draft' });
    await oldRequest;
    assert.equal(page.data.id, 'current-content');
    assert.equal(page.data.revision, 7);
    assert.equal(page.data.busy, true);
    assert.equal(page.data.blocks[0].text, '新页面正文');
    responses[1]({ content_id: 'current-content', revision: 8, version: 0, status: 'draft' });
    await currentRequest;
    assert.equal(page.data.id, 'current-content');
    assert.equal(page.data.revision, 8);
    assert.equal(page.data.busy, false);
    assert.equal(page.data.preview && page.data.preview.preview_token, action === 'saveAndPreview' ? 'current-preview' : null);
  }
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
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
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

test('editor renders and previews preserved layout with native video without flattening fallback blocks', async () => {
  const body = JSON.stringify({
    schema: 1,
    layout_html: '<section style="padding:12px"><blockquote style="border-left:2px solid #ddd">前<jd-video data-media-id="layout-video-0001" style="margin:8px"></jd-video>后</blockquote></section>',
    blocks: [{ kind: 'paragraph', text: '不能代替原排版的兼容正文' }],
  });
  const content = {
    title: '公众号原排版', owner_id: 'owner-a', rights: 'own', revision: 2, version: 0, status: 'draft', body_format: 'article_v2', body,
    media: [{ media_id: 'layout-video-0001', kind: 'video', width: 1280, height: 720 }],
  };
  let savedDraft;
  const paused = [];
  const page = pageHarness('contentedit', {
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'] }),
    listWorkbenchMedia: async () => ({ items: [] }),
    getContent: async () => content,
    downloadWorkbenchMedia: async id => 'wxfile://' + id,
    saveContent: async draft => { savedDraft = draft; return { content_id: 'content-layout', revision: 3, version: 0, status: 'draft' }; },
    previewContent: async () => ({ preview_token: 'preview-layout', content: { ...content, revision: 3 } }),
  }, {
    createVideoContext: id => ({ pause() { paused.push(id); } }),
  });
  page.setData({ id: 'content-layout' });
  await page.load();

  assert.equal(page.data.layoutReadOnly, true);
  assert.deepEqual(Array.from(page.data.layoutBlocks, block => block.kind), ['layout', 'video', 'layout']);
  assert.match(page.data.layoutBlocks[0].html, /前<\/blockquote><\/section>$/);
  assert.match(page.data.layoutBlocks[2].html, /^<section[^>]*><blockquote[^>]*>后/);
  assert.equal(page.data.layoutBlocks[1].url, 'wxfile://layout-video-0001');
  assert.match(page.data.layoutBlocks[1].style, /padding:12px/);
  assert.match(page.data.layoutBlocks[1].style, /border-left:2px solid #ddd/);
  assert.match(page.data.layoutBlocks[1].domId, /^contentedit-layout-media-/);

  await page.saveAndPreview();
  assert.equal(savedDraft.body, body);
  assert.deepEqual(Array.from(savedDraft.media_ids), ['layout-video-0001']);
  assert.equal(page.data.previewLayoutReadOnly, true);
  assert.deepEqual(Array.from(page.data.previewLayoutBlocks, block => block.kind), ['layout', 'video', 'layout']);
  assert.match(page.data.previewLayoutBlocks[1].domId, /^contentedit-preview-media-/);
  assert.equal(page.data.previewBlocks[0].text, '不能代替原排版的兼容正文');

  page.onHide();
  assert.deepEqual(paused.sort(), [page.data.layoutBlocks[1].domId, page.data.previewLayoutBlocks[1].domId].sort());

  const wxml = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /layoutBlocks/);
  assert.match(wxml, /previewLayoutBlocks/);
  assert.match(wxml, /<video[^>]+controls/);
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
  page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
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
    listMyVisitors: async () => ({ items: [] }),
    listMyContentStats: async () => ({ items: [] }),
  }, { getStorageSync: key => storage[key] || '', hideShareMenu() {} });
  page.onShow(); await tick();
  // 新交互：分享在切 Tab 时加载
  page.switchTab({ currentTarget: { dataset: { tab: 'shares' } } }); await tick();
  storage.token = 'account-b'; page.onShow(); await tick();
  page.switchTab({ currentTarget: { dataset: { tab: 'shares' } } }); await tick();
  resolveOld({ items: [{ share_id: 'share-account-a' }], as_of: '2026-10-27T00:00:00Z', metric_version: 'visible-v1' }); await tick();
  console.log('DBG2', JSON.stringify(page.data.items.map(i=>i.share_id)), 'tab=', page.data.activeTab, '_loadedTabs=', JSON.stringify(page._loadedTabs), 'cursor=', page.data.next_cursor)
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
  page.switchTab({ currentTarget: { dataset: { tab: 'shares' } } }); await tick();
  page.loadMore(); page.loadMore(); await tick();
  assert.equal(calls, 2);
  const refresh = page.loadShares(true); await tick();
  resolveMore({ items: [{ share_id: 'stale-more' }] }); await refresh; await tick();
  assert.deepEqual(page.data.items.map(item => item.share_id), ['refreshed']);
});

for (const outcome of ['return', 'account-change', 'draft-change', 'unload']) {
  test(`media selection survives return and rejects stale ${outcome}`, async () => {
    let picker, finish, token = 'session-token';
    const page = pageHarness('contentedit', { downloadWorkbenchMedia: () => new Promise(resolve => { finish = resolve; }) }, {
      getStorageSync: () => token, hideShareMenu() {}, navigateTo(options) { picker = options; },
    });
    page._token = token; page._epoch = 2; page._loaded = true;
    page.data.blocks = [{ kind: 'paragraph', text: '保留正文' }]; page.data.media = [];
    page.openMediaPicker(); page.onHide();
    const pending = picker.events.selectMaterial({ kind: 'media', asset: { media_id: 'picked', kind: 'image' } });
    page.onShow();
    if (outcome === 'account-change') token = 'other-account';
    if (outcome === 'draft-change') page.data.title = '另一份修改';
    if (outcome === 'unload') page.onUnload();
    finish('/tmp/picked'); await pending;
    assert.equal(page.data.blocks.some(block => block.media_id === 'picked'), outcome === 'return');
  });
}

for (const field of ['tagsText', 'pinned']) {
  test(`editor warns for unsaved ${field} and clears warning after saving`, async () => {
    let warned = 0, left = 0, finish;
    const page = pageHarness('contentedit', { saveContent: () => new Promise(resolve => { finish = resolve; }) }, {
      showModal() { warned++; }, navigateBack() { left++; },
    });
    page._token = 'session-token'; page._epochShares = 1; page._epochCustomers = 1; page._epochMaterials = 1;;
    Object.assign(page.data, { id: 'content-1', title: '作品', owner_id: 'designer-1', blocks: [{ kind: 'paragraph', text: '正文' }] });
    page._savedSignature = page.draftSignature();
    page.setData({ [field]: field === 'pinned' ? true : '庭院' });
    page.handleBack(); assert.equal(warned, 1); assert.equal(left, 0);
    const save = page.saveDraft();
    finish({ content_id: 'content-1', revision: 2, version: 0, status: 'draft' }); await save;
    assert.equal(page._savedSignature, page.draftSignature());
    page.handleBack(); assert.equal(left, 1);
    page.setData({ [field]: field === 'pinned' ? false : '别墅' });
    const secondSave = page.saveDraft();
    page.setData({ [field]: field === 'pinned' ? true : '花园' });
    finish({ content_id: 'content-1', revision: 3, version: 0, status: 'draft' }); await secondSave;
    assert.notEqual(page._savedSignature, page.draftSignature());
    page.handleBack(); assert.equal(warned, 2);
  });
}
