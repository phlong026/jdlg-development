'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.js');
const tick = () => new Promise(resolve => setImmediate(resolve));

test('content pages explicitly register their custom navigation component', () => {
  for (const name of ['contentworkbench', 'contentedit', 'contentwriting', 'contentreader', 'nativereferences', 'sharevisits', 'visitdetail']) {
    const config = JSON.parse(fs.readFileSync(path.join(__dirname, '../miniprogram/pages', name, name + '.json'), 'utf8'));
    assert.equal(config.usingComponents && config.usingComponents['navigation-bar'], '/components/navigation-bar/navigation-bar');
  }
});

function runtime(overrides = {}) {
  let definition;
  const queries = [], navigations = [];
  let actionSheet;
  const api = Object.assign({
    mySharesContext: async () => ({ can_view: true, owners: ['owner-a'] }),
    workbenchContext: async () => ({ enabled: true, can_manage: true, owners: ['owner-a'] }),
    listContents: async query => { queries.push(query); return { items: [{ content_id: 'article-a', title: '庭院图文', type: 'article', status: 'draft', media: [] }, { content_id: 'video-a', title: '庭院视频', type: 'article', status: 'published', media: [{ kind: 'video', media_id: 'video-asset' }] }, { content_id: 'document-a', title: '庭院方案.pdf', type: 'document', status: 'draft', media: [{ kind: 'document', media_id: 'document-asset' }] }], next_cursor: 'page-2', as_of: '2026-10-27T00:00:00Z', metric_version: 'v1' }; },
    importCapabilities: async () => ({ enabled: true, platforms: [{ platform: 'wechat_article', available: true }] }),
    listImports: async () => ({ items: [] }),
  }, overrides);
  vm.runInNewContext(fs.readFileSync(source, 'utf8'), {
    Page: value => { definition = value; }, require: () => api,
    wx: { getStorageSync: () => 'account-a', hideShareMenu() {}, stopPullDownRefresh() {}, navigateTo: value => navigations.push(value.url), showToast() {}, showActionSheet(options) { actionSheet = options; }, getClipboardData: options => options.success({ data: 'https://mp.weixin.qq.com/s/original' }) },
    setTimeout, clearTimeout, console,
  });
  const page = Object.assign({}, definition, { data: JSON.parse(JSON.stringify(definition.data)), setData(values, callback) { for (const [key, value] of Object.entries(values)) { const parts = key.replace(/\[(\d+)\]/g, '.$1').split('.'); let object = this.data; for (const part of parts.slice(0, -1)) object = object[part]; object[parts.at(-1)] = value; } if (callback) callback(); } });
  return { page, queries, navigations, actionSheet: () => actionSheet };
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

test('confirmed workbench access loss clears all private state and invalidates pending requests', async () => {
  let revoked = false;
  const { page } = runtime({ workbenchContext: async () => {
    if (revoked) throw Object.assign(new Error('forbidden'), { statusCode: 403 });
    return { enabled: true, can_manage: true, owners: ['owner-a'] };
  } });
  page.onShow(); await tick(); await tick();
  page.setData({ imports: [{ import_id: 'private-import' }], importURL: 'private-link' });
  revoked = true;
  page.onHide(); page.onShow();
  const epoch = page._epoch;
  await tick(); await tick();
  assert.equal(page.data.denied, true);
  assert.equal(page._loaded, false);
  assert.equal(page.data.context, null);
  for (const key of ['items', 'visibleItems', 'imports']) assert.equal(page.data[key].length, 0, key);
  assert.equal(page.data.importURL, '');
  assert.equal(page.data.hasShareVisitAccess, false);
  assert.ok(page._epoch > epoch, '失权后在途回调立即作废');
});

test('category changes query the full catalog and reset pagination while keeping search and owner', async () => {
  const calls = [];
  const { page } = runtime({ listContents: async query => {
    calls.push({ ...query });
    return { items: query.type ? [{ content_id: query.type + '-remote', type: query.type, media: [] }] : [], next_cursor: 'more', as_of: '2026-11-04T00:00:00Z', metric_version: 'v1' };
  }});
  page.onShow(); await tick(); await tick();
  page.setData({ ownerFilter: 'owner-a', searchTerm: '庭院' });
  await page.selectCategory({ currentTarget: { dataset: { key: 'video' } } });
  assert.equal(calls.at(-1).type, 'video');
  assert.equal(calls.at(-1).owner, 'owner-a');
  assert.equal(calls.at(-1).content_q, '庭院');
  assert.equal(calls.at(-1).cursor, undefined);
  assert.equal(calls.at(-1).as_of, undefined);
  assert.equal(page.data.visibleItems[0].content_id, 'video-remote');
  await page.load(false, page._epoch);
  assert.equal(calls.at(-1).type, 'video');
  assert.equal(calls.at(-1).cursor, 'more');
  assert.equal(calls.at(-1).as_of, '2026-11-04T00:00:00Z');
  await page.selectCategory({ currentTarget: { dataset: { key: 'all' } } });
  assert.equal(calls.at(-1).type, undefined);
  assert.equal(calls.at(-1).cursor, undefined);
});

test('a late category response cannot replace the newly selected catalog', async () => {
  const pending = [];
  const { page } = runtime({ listContents: query => query.type ? new Promise(resolve => pending.push({ type: query.type, resolve })) : Promise.resolve({ items: [] }) });
  page.onShow(); await tick(); await tick();
  const video = page.selectCategory({ currentTarget: { dataset: { key: 'video' } } });
  await tick();
  const note = page.selectCategory({ currentTarget: { dataset: { key: 'note' } } });
  await tick();
  assert.equal(pending.length, 2);
  pending[1].resolve({ items: [{ content_id: 'note-current', type: 'note' }] }); await note;
  pending[0].resolve({ items: [{ content_id: 'video-old', type: 'video' }] }); await video;
  assert.equal(page.data.visibleItems[0].content_id, 'note-current');
});

test('content studio never fetches unrelated project tasks', async () => {
  let projectCalls = 0;
  const { page } = runtime({ listProjectNoteProjects: async () => { projectCalls += 1; return { items: [] }; }});
  page.onShow(); await tick(); await tick();
  page.onHide(); page.onShow(); await tick(); await tick();
  assert.equal(projectCalls, 0);
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

test('import view keeps a neutral loading state until capability is known and skips the content catalog', async () => {
  let finishCapability;
  let listCalls = 0;
  const { page } = runtime({
    listContents: async () => { listCalls += 1; return { items: [] }; },
    importCapabilities: () => new Promise(resolve => { finishCapability = resolve; }),
  });
  page.onLoad({ view: 'import' });
  page.onShow();
  await tick(); await tick();
  assert.equal(listCalls, 0, '导入页不应为不可见的内容列表等待');
  assert.equal(page.data.loading, false, '工作台权限确认后应展示稳定的导入页壳');
  assert.equal(page.data.importCapabilityLoading, true);
  const template = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.match(template, /wx:if="\{\{importCapabilityLoading\}\}"[^>]*class="import-gate/);
  finishCapability({ enabled: true, platforms: [{ platform: 'wechat_article', available: true }] });
  await tick(); await tick();
  assert.equal(page.data.importCapabilityLoading, false);
  assert.equal(page.data.importEnabled, true);
});

test('quiet refresh preserves matching cover and share entry while fresh checks run', async () => {
  let contentCalls = 0, coverCalls = 0;
  const { page } = runtime({
    listContents: async () => {
      contentCalls += 1;
      return { items: [{ content_id: 'article-a', version: contentCalls < 3 ? 1 : 2, type: 'article', status: 'draft', media: [], cover: { kind: 'image', media_id: 'cover-a' } }] };
    },
    downloadWorkbenchMedia: async id => { coverCalls += 1; return '/tmp/' + id; },
  });
  page.onShow(); await tick(); await tick(); await tick();
  assert.equal(page.data.items[0].cover_path, '/tmp/cover-a');
  assert.equal(page.data.hasShareVisitAccess, true);
  page.onHide();
  assert.equal(page.data.hasShareVisitAccess, true, '隐藏页面不等于失去权限');
  page.onShow(); await tick(); await tick();
  assert.equal(page.data.items[0].cover_path, '/tmp/cover-a', '相同媒体刷新时保留已解码封面');
  assert.equal(coverCalls, 1, '相同媒体不重复下载');
  page.onHide(); page.onShow(); await tick(); await tick(); await tick();
  assert.equal(coverCalls, 2, '内容版本变化后重新解析封面');
});

for (const statusCode of [undefined, 503, 401, 403]) {
  test(`quiet refresh ${statusCode || 'network'} preserves transient UI but clears real lost access`, async () => {
    let failed = false;
    const failure = () => Object.assign(new Error('refresh failed'), { statusCode });
    const { page } = runtime({
      mySharesContext: async () => {
        if (failed) throw failure();
        return { can_view: true, owners: ['owner-a'] };
      },
      listProjectNoteProjects: async () => {
        if (failed) throw failure();
        return { items: [{ project_id: 'project-a', name: '已显示的项目' }] };
      },
    });
    page.onShow(); await tick(); await tick();
    assert.equal(page.data.hasShareVisitAccess, true);
    failed = true;
    page.onHide(); page.onShow(); await tick(); await tick();
    const revoked = statusCode === 401 || statusCode === 403;
    assert.equal(page.data.hasShareVisitAccess, !revoked);
  });
}

test('studio offers real destinations for existing assets and visits', async () => {
  const { page, navigations } = runtime();
  page.onShow(); await tick();
  page.openExistingAssets(); page.openShareVisits();
  assert.deepEqual(navigations, ['/pages/nativereferences/nativereferences', '/pages/sharevisits/sharevisits?tab=shares']);
});

test('studio puts image and video first, keeps import secondary, and moves article and file into more', () => {
  const harness = runtime();
  harness.page.openCreateMenu();
  assert.equal(harness.page.data.createMenuOpen, true);
  assert.equal(harness.page.data.createMoreOpen, false);
  harness.page.chooseCreate({ currentTarget: { dataset: { route: '/pages/contentedit/contentedit?kind=gallery' } } });
  harness.page.openCreateMenu(); harness.page.chooseCreate({ currentTarget: { dataset: { route: '/pages/contentedit/contentedit?kind=video' } } });
  harness.page.openCreateMenu(); harness.page.chooseCreate({ currentTarget: { dataset: { route: '/pages/contentworkbench/contentworkbench?view=import' } } });
  harness.page.openCreateMenu(); harness.page.openMoreCreateMenu();
  assert.equal(harness.page.data.createMoreOpen, true);
  harness.page.chooseCreate({ currentTarget: { dataset: { route: '/pages/contentedit/contentedit?kind=article' } } });
  harness.page.openCreateMenu(); harness.page.openMoreCreateMenu(); harness.page.chooseCreate({ currentTarget: { dataset: { route: '/pages/contentedit/contentedit?action=document' } } });
  assert.deepEqual(harness.navigations, [
    '/pages/contentedit/contentedit?kind=gallery',
    '/pages/contentedit/contentedit?kind=video',
    '/pages/contentworkbench/contentworkbench?view=import',
    '/pages/contentedit/contentedit?kind=article',
    '/pages/contentedit/contentedit?action=document',
  ]);
  const template = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  const style = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxss'), 'utf8');
  assert.match(template, /class="create-primary"[\s\S]*图文笔记[\s\S]*视频笔记/);
  assert.match(template, /class="create-row"[^>]*view=import/);
  assert.match(template, /class="create-handle"/);
  assert.match(template, /mini-icon name="image"/);
  assert.match(template, /mini-icon name="video"/);
  assert.match(template, /mini-icon name="link"[\s\S]*导入链接/);
  assert.match(template, /更多类型[\s\S]*文章、文件/);
  assert.match(style, /\.create-primary\s*>\s*button\.create-choice\s*\{[^}]*width:\s*100%[^}]*min-width:\s*0[^}]*box-sizing:\s*border-box/);
});

test('studio fidelity keeps four primary filters and moves article and project notes to the secondary filter', async () => {
  const harness = runtime();
  const template = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  const style = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxss'), 'utf8');
  assert.match(template, /variant="workbench"/);
  assert.match(template, />我的内容</);
  assert.match(template, /class="category-tabs primary-categories"[\s\S]*data-key="all"[\s\S]*data-key="gallery"[\s\S]*data-key="video"[\s\S]*data-key="document"/);
  assert.match(template, /wx:if="\{\{moreFiltersOpen\}\}" class="secondary-categories"[\s\S]*data-key="article"[\s\S]*data-key="note"/);
  assert.doesNotMatch(template, /class="content-labels"|class="content-metrics"/);
  assert.match(style, /\.content-cover\s*\{[^}]*width:\s*220rpx[^}]*height:\s*148rpx/);
  harness.page.toggleMoreFilters();
  assert.equal(harness.page.data.moreFiltersOpen, true);
  await harness.page.selectCategory({ currentTarget: { dataset: { key: 'note' } } });
  assert.equal(harness.page.data.moreFiltersOpen, false);
});

test('studio trusts presentation and never turns a mixed article into a video row', async () => {
  const seen = [];
  const { page } = runtime({
    listContents: async query => { seen.push(query); return { items: [
      { content_id: 'mixed-article', title: '图文混排', type: 'article', presentation: 'article', media: [{ kind: 'video' }] },
      { content_id: 'gallery-note', title: '图集', type: 'article', presentation: 'gallery', status: 'published', version: 1, media: [] },
    ] }; },
  });
  page.onShow(); await tick(); await tick();
  assert.deepEqual(page.data.items.map(item => item.kind), ['article', 'gallery']);
  assert.equal(page.data.items[1].action_label, '分享');
  assert.deepEqual(page.data.categories.map(item => item.key), ['all', 'gallery', 'video', 'article', 'note', 'document']);
  await page.selectCategory({ currentTarget: { dataset: { key: 'gallery' } } });
  assert.equal(seen.at(-1).type, 'gallery');
});

test('failed imports can go straight to a truthful manual editor path', () => {
  const harness = runtime();
  harness.page._visible = true; harness.page._token = 'account-a'; harness.page._epoch = 1;
  harness.page.setData({ imports: [{ import_id: 'failed-xhs', platform: 'xiaohongshu', status: 'failed' }] });
  harness.page.manualImport({ currentTarget: { dataset: { id: 'failed-xhs' } } });
  assert.deepEqual(harness.navigations, ['/pages/contentedit/contentedit?kind=gallery']);
});

test('published rows open their immutable reader while drafts continue in the editor', () => {
  const harness = runtime();
  harness.page.openContent({ currentTarget: { dataset: { id: 'video-a', status: 'published', version: 3 } } });
  harness.page.openContent({ currentTarget: { dataset: { id: 'article-a', status: 'draft', version: 0 } } });
  assert.deepEqual(harness.navigations, [
    '/pages/contentreader/contentreader?content_id=video-a&content_version=3',
    '/pages/contentedit/contentedit?id=article-a',
  ]);
});

test('published content keeps editing in its more menu while the row remains the share entry', () => {
  const harness = runtime();
  harness.page._visible = true; harness.page._token = 'account-a'; harness.page._epoch = 1;
  harness.page.setData({ items: [{ content_id: 'published-a', status: 'published', revision: 4 }] });
  harness.page.openContentMore({ currentTarget: { dataset: { id: 'published-a' } } });
  assert.deepEqual(Array.from(harness.actionSheet().itemList), ['编辑内容', '删除内容']);
  harness.actionSheet().success({ tapIndex: 0 });
  assert.deepEqual(harness.navigations, ['/pages/contentedit/contentedit?id=published-a']);
  const template = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.match(template, /catchtap="openContentMore"/);
  assert.doesNotMatch(template, />编辑<\/button>/);
});

test('legacy snapshots keep their existing read-only editor instead of calling native published-content APIs', () => {
  const harness = runtime();
  harness.page.openContent({ currentTarget: { dataset: { id: 'legacy-a', status: 'published', version: 3, readOnly: true, route: 'pages/work/work' } } });
  assert.deepEqual(harness.navigations, ['/pages/contentedit/contentedit?id=legacy-a']);
});

test('studio template removes duplicate create and unavailable note controls', () => {
  const template = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  assert.equal((template.match(/bindtap="openCreateMenu"/g) || []).length, 1);
  assert.doesNotMatch(template, /上传资料|项目笔记协作尚未开通|已有生成记录、模板与作品可直接选用/);
  assert.match(template, /bindtap="openExistingAssets"/);
  assert.doesNotMatch(template, /class="active-projects"|仅筛选已加载内容/);
});

test('content pages use one clear title and hide unavailable writing actions', () => {
  const writing = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentwriting/contentwriting.wxml'), 'utf8');
  const writingStyle = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentwriting/contentwriting.wxss'), 'utf8');
  const references = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/nativereferences/nativereferences.wxml'), 'utf8');
  const referenceSource = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/nativereferences/nativereferences.js'), 'utf8');
  const workbench = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.wxml'), 'utf8');
  const editor = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentedit/contentedit.wxml'), 'utf8');
  const workbenchConfig = JSON.parse(fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentworkbench/contentworkbench.json'), 'utf8'));
  assert.match(writing, /<navigation-bar title="AI辅助"/);
  assert.doesNotMatch(writing, />[^<]*(?:文案辅助|AI文案)[^<]*</);
  assert.doesNotMatch(writing, /AI内容策划|title="甲第灵光"/);
  assert.match(writing, /wx:if="{{result}}" class="writing-panel refine-panel"/);
  assert.match(writing, /<block wx:elif="{{result}}">[\s\S]*?<button class="primary" bindtap="saveToDraft"/);
  assert.match(writing, /bindtap="quickGenerate"/);
  assert.match(writing, /bindtap="generateWriting"[^>]*/); // 深聊入口保留
  assert.match(writingStyle, /\.writing-footer\s*\{[^}]*position:\s*sticky/s);
  assert.match(references, /<navigation-bar title="{{navigationTitle}}"/);
  assert.match(references, /bindtap="selectLibraryTab"/);
  assert.match(references, /class="media-thumb/);
  assert.match(references, /{{item\.meta_label}}/);
  assert.match(referenceSource, /downloadWorkbenchMedia/);
  assert.doesNotMatch(references, /保存已发布的固定版本|仅展示当前授权范围/);
  assert.match(workbench, /wx:if="{{context\.owner_choices\.length > 1}}"/);
  assert.match(workbench, /class="workspace-tabs"/);
  assert.match(workbench, /bindtap="openCollections"/);
  assert.match(workbench, /bindtap="openExistingAssets"/);
  assert.match(workbench, /bindtap="openCreateMenu"/);
  assert.match(workbench, /range-key="display_name"/);
  assert.doesNotMatch(workbench, />{{importOwner \|\| '请选择'}}/);
  assert.match(editor, /range="{{context\.owner_choices}}" range-key="display_name"/);
  assert.doesNotMatch(editor, /{{owner_id \|\| '请选择'}}/);
  assert.equal(workbenchConfig.navigationBarTitleText, '内容创作');
});

test('switching owner reloads the selected catalog from its first page', async () => {
  const { page, queries } = runtime({ workbenchContext: async () => ({ enabled: true, can_manage: true, owner_choices: [{ owner_id: 'owner-a' }, { owner_id: 'owner-b' }] }) });
  page.onShow(); await tick(); await tick();
  await page.selectOwnerTab({ currentTarget: { dataset: { owner: 'owner-b' } } });
  await tick();
  assert.equal(queries.at(-1).owner, 'owner-b');
  assert.equal(queries.at(-1).cursor, undefined);
  assert.equal(page.data.items.length, 3);
});

test('an imported cover preserves the complete native list and searchable rows', async () => {
  const { page } = runtime({ downloadWorkbenchMedia: async () => '/tmp/imported-cover' });
  page._token = 'account-a';
  page._visible = true;
  page._epoch = 1;
  const rows = [
    { content_id: 'plain', title: '普通内容', version: 0 },
    { content_id: 'imported', title: '导入视频', version: 0, cover: { kind: 'image', media_id: 'cover' } },
  ];
  page.setData({ items: rows, visibleItems: rows, category: 'all', keyword: '' });
  const setData = page.setData.bind(page);
  page.setData = (patch, callback) => {
    // Native wx.setData reserves brackets for array indices. The old fixture
    // accepted dotted numbers, masking the real simulator's array replacement.
    for (const key of Object.keys(patch)) {
      if (/^(items|visibleItems)\.\d+\./.test(key)) throw Error('unsupported native array data path');
    }
    return setData(patch, callback);
  };
  await page.loadCovers(1, rows);
  assert.equal(Array.isArray(page.data.items), true);
  assert.equal(page.data.items.length, 2);
  assert.equal(page.data.visibleItems.length, 2);
  assert.equal(page.data.items[1].cover_path, '/tmp/imported-cover');
  assert.equal(page.data.items[0].content_id, 'plain');
});
