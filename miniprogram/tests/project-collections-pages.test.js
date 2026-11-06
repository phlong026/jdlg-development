'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');
const tick = () => new Promise(resolve => setImmediate(resolve));

function pageRuntime(api, token = 'designer-session', resolveProtectedMediaURL = async url => 'wxfile://tmp/' + url.split('/').at(-1)) {
  let definition;
  const storage = { token, userInfo: { id: 9, nickname: '设计师', avatar: 'https://cdn.example/avatar.png', token } };
  const navigations = [];
  const modals = [], actionSheets = [];
  const wx = {
    getStorageSync: key => storage[key] || '',
    hideShareMenu() {},
    showShareMenu() {},
    navigateTo: options => { navigations.push(options.url); if(options.success) options.success(); },
    navigateBack() {},
    showToast() {},
    showActionSheet(options) { actionSheets.push(options); },
    showModal(options) { modals.push(options); options.success && options.success({ confirm: true }); },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/projectcollections/projectcollections.js'), 'utf8'), {
    Page: value => { definition = value; }, require: name => name.includes('protected-media') ? { resolveProtectedMediaURL } : api, wx,
    Promise, Date, Math, Object, Array, Set, String, Number, Error, encodeURIComponent,
  });
  const page = Object.assign({}, definition, {
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values, callback) {
      for (const [key, value] of Object.entries(values)) {
        const parts = key.split('.');
        let target = this.data;
        for (const part of parts.slice(0, -1)) target = target[part];
        target[parts.at(-1)] = value;
      }
      if (callback) callback();
    },
  });
  return { page, storage, navigations, modals, actionSheets };
}

test('collection access loss clears private editing state before a later retry', async () => {
  let revoked = false;
  const { page } = pageRuntime({
    workbenchContext: async () => ({ owners: ['owner-a'], owner_choices: [] }),
    listCollections: async () => {
      if (revoked) throw Object.assign(new Error('forbidden'), { statusCode: 403 });
      return { items: [] };
    },
  });
  page.onShow(); await tick(); await tick();
  page.setData({ editing: true, current: { collection_id: 'private' }, selectedItems: [{ content_id: 'private' }], candidates: [{ content_id: 'private' }], preview: { preview_token: 'private' }, title: 'private', owners: ['owner-a'] });
  revoked = true;
  await page.loadHome(page.epoch);
  assert.equal(page.data.denied, true);
  assert.equal(page.data.editing, false);
  assert.equal(page.data.current, null);
  assert.equal(page.data.preview, null);
  assert.equal(page.data.title, '');
  for (const key of ['selectedItems', 'candidates', 'owners', 'ownerChoices']) assert.equal(page.data[key].length, 0, key);
  revoked = false;
  await page.loadHome(page.epoch);
  assert.equal(page.data.denied, false);
  assert.equal(page.data.editing, false, '权限恢复后不能重新出现旧私有编辑内容');
});

test('collection editor multi-selects, reorders, saves, previews and publishes fixed items', async () => {
  const calls = [];
  const candidates = [
    { content_id: 'content-0001', content_version: 1, title: '案例甲' },
    { content_id: 'content-0002', content_version: 2, title: '案例乙' },
  ];
  const api = {
    mutationKey: prefix => prefix + '-key',
    workbenchContext: async () => ({ owners: ['owner-a'], owner_choices: [{ owner_id: 'owner-a', display_name: '设计师昵称' }], enabled: true }),
    listCollections: async () => ({ items: [] }),
    listCandidates: async () => ({ items: candidates }),
    saveCollection: async (input, id) => {
      calls.push({ type: 'save', input, id });
      return { collection_id: id || 'collection-0001', revision: id ? 2 : 1, version: 0, status: 'draft', title: input.title, owner_id: input.owner_id, items: input.items.map(ref => candidates.find(item => item.content_id === ref.content_id)) };
    },
    previewCollection: async () => ({ preview_token: 'preview-1', collection: { items: candidates } }),
    publishCollection: async (...args) => { calls.push({ type: 'publish', args }); return { collection_id: 'collection-0001', revision: 2, version: 1, status: 'published', items: candidates }; },
    mediaURL: () => '',
  };
  const { page, navigations } = pageRuntime(api);
  page.onShow(); await tick(); await tick();
  page.startCreate(); await tick();
  page.setTitle({ detail: { value: '精选项目' } });
  page.toggleCandidate({ currentTarget: { dataset: { id: 'content-0001', version: 1 } } });
  page.toggleCandidate({ currentTarget: { dataset: { id: 'content-0002', version: 2 } } });
  page.moveItem({ currentTarget: { dataset: { index: 1, direction: -1 } } });
  assert.deepEqual(Array.from(page.data.selectedItems, item => item.content_id), ['content-0002', 'content-0001']);
  await page.saveDraft();
  await page.previewCurrent();
  await page.publishCurrent();
  assert.deepEqual(Array.from(calls[0].input.items, item => item.content_id), ['content-0002', 'content-0001']);
  assert.equal(calls[1].args[2], 'preview-1');
  page.openReader({ currentTarget: { dataset: { id: 'collection-0001', version: 1 } } });
  assert.equal(navigations[0], '/pages/collectionreader/collectionreader?collection_id=collection-0001&collection_version=1');
});

test('collection page discards late data after account switch', async () => {
  const finishes = [];
  const api = {
    workbenchContext: async () => ({ owners: ['owner-a'], enabled: true }),
    listCollections: () => new Promise(resolve => { finishes.push(resolve); }),
    mediaURL: () => '',
  };
  const runtime = pageRuntime(api);
  runtime.page.onShow(); await tick();
  runtime.storage.token = 'other-session';
  runtime.page.onShow();
  await tick();
  finishes[0]({ items: [{ collection_id: 'old-account-collection' }] });
  await tick();
  assert.equal(runtime.page.data.collections.length, 0);
});

test('collection page keeps the rendered list during same-account foreground refresh', async () => {
  let finishRefresh;
  let calls = 0;
  const api = {
    workbenchContext: async () => ({ owners: ['owner-a'], owner_choices: [] }),
    listCollections: () => {
      calls += 1;
      if (calls === 1) return Promise.resolve({ items: [{ collection_id: 'collection-0001', title: '已显示合集', items: [], version: 1, status: 'published' }] });
      return new Promise(resolve => { finishRefresh = resolve; });
    },
    mediaURL: () => '',
  };
  const { page } = pageRuntime(api);
  page.onShow(); await tick(); await tick();
  assert.equal(page.data.collections.length, 1);
  page.onHide(); page.onShow(); await tick();
  assert.equal(page.data.loading, false, '后台刷新不替换已显示主体');
  assert.equal(page.data.collections[0].title, '已显示合集');
  finishRefresh({ items: [{ collection_id: 'collection-0002', title: '刷新合集', items: [], version: 1, status: 'published' }] });
  await tick(); await tick();
  assert.equal(page.data.collections[0].title, '刷新合集');
});

test('collection workbench renders protected covers from authenticated temporary files', async () => {
  const api = {
    workbenchContext: async () => ({ owners: ['owner-a'], owner_choices: [] }),
    listCollections: async () => ({ items: [{ collection_id: 'collection-0001', version: 1, cover: { url: '/cover-workbench' } }] }),
    mediaURL: value => 'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/media' + value,
  };
  const { page } = pageRuntime(api);
  page.onShow();
  await tick(); await tick();
  assert.equal(page.data.collections[0].cover_url, 'wxfile://tmp/cover-workbench');
});

test('collection owner picker shows labels but saves the authorized owner ID', async () => {
  const saved = [];
  const api = {
    mutationKey: () => 'collection-key',
    workbenchContext: async () => ({ owners: ['designer-40', 'team-a'], owner_choices: [{ owner_id: 'team-a', display_name: '设计团队' }, { owner_id: 'designer-40', display_name: '设计师昵称' }] }),
    listCollections: async () => ({ items: [] }),
    listCandidates: async () => ({ items: [{ content_id: 'content-0001', content_version: 1, title: '作品' }] }),
    saveCollection: async input => { saved.push(input); return { collection_id: 'collection-1', revision: 1, title: input.title, owner_id: input.owner_id, items: input.items }; },
    mediaURL: () => '',
  };
  const { page } = pageRuntime(api);
  page.onShow(); await tick(); await tick();
  await page.startCreate();
  page.setOwner({ detail: { value: 0 } });
  page.setTitle({ detail: { value: '精选' } });
  page.toggleCandidate({ currentTarget: { dataset: { id: 'content-0001', version: 1 } } });
  await page.saveDraft();
  assert.equal(page.data.ownerChoices[0].display_name, '设计团队');
  assert.equal(saved[0].owner_id, 'team-a');
  const template = fs.readFileSync(path.join(root, 'pages/projectcollections/projectcollections.wxml'), 'utf8');
  assert.match(template, /range-key="display_name"/);
  assert.doesNotMatch(template, /{{owners\[ownerIndex\]}}/);
});

function readerRuntime(api, token = '') {
  const events = [];
  const tracking = {splitPath:require('../miniprogram/utils/tracking/index').splitPath,resolvePageOptions(){}, collectionToken:()=>token, preheatTimelineShare(){},emit:(...args)=>events.push(args)};
  let definition;
  const navigations = [];
  const wx = {
    getStorageSync: key => key === 'token' ? token : key === 'userInfo' ? { id: 9, nickname: '设计师', avatar: 'https://cdn.example/avatar.png', token } : '',
    hideShareMenu() {}, showShareMenu() {},
    navigateTo: options => { navigations.push(options.url); if(options.success) options.success(); },
    navigateBack() {}, switchTab() {},
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/collectionreader/collectionreader.js'), 'utf8'), {
    Page: value => { definition = value; }, require: name => name.includes('tracking/index') ? tracking : name.includes('protected-media') ? { resolveProtectedMediaURL: async url => 'wxfile://tmp/' + url.split('/').at(-1) } : api, wx,
    Promise, Object, Array, String, Number, Error, encodeURIComponent,
  });
  const page = Object.assign({}, definition, { data: JSON.parse(JSON.stringify(definition.data)), setData(values, callback) { Object.assign(this.data, values); if (callback) callback(); } });
  return { page, navigations, events };
}

test('collection reader opens only the server-validated fixed child target and creates no child share', async () => {
  const calls = [];
  const api = {
    readPublishedCollection: async () => ({ collection_id: 'collection-0001', version: 3, title: '对外案例', cover: { url: '/cover-main' }, items: [{ title: '案例甲', available: true, cover: { url: '/cover-item' } }] }),
    openPublishedItem: async (...args) => { calls.push(args); return { route_key: 'pages/contentreader/contentreader', path: '/pages/contentreader/contentreader?collection_id=collection-0001&collection_version=3&content_id=content-0001&content_version=2&item_position=0' }; },
    mediaURL: value => 'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/media' + value,
  };
  const { page, navigations, events } = readerRuntime(api, 'verified-token-b');
  page.onLoad({ collection_id: 'collection-0001', collection_version: '3' });
  await tick();
  await page.openItem({ currentTarget: { dataset: { index: 0 } } });
  assert.deepEqual(calls[0], ['collection-0001', 3, 0]);
  assert.equal(navigations[0], '/pages/contentreader/contentreader?collection_id=collection-0001&collection_version=3&content_id=content-0001&content_version=2&item_position=0&st=verified-token-b');
  assert.equal(page.data.contentReady,true);
  assert.equal(page.data.collection.cover.url,'wxfile://tmp/cover-main');
  assert.equal(page.data.items[0].cover_url,'wxfile://tmp/cover-item');
  assert.equal(events[0][1],'content_click');
  assert.equal(page.onShareAppMessage().path, '/pages/collectionreader/collectionreader?collection_id=collection-0001&collection_version=3');
});

test('collection pages stay clean and use the existing navigation and brand palette', () => {
  for (const name of ['projectcollections', 'collectionreader']) {
    const config = JSON.parse(fs.readFileSync(path.join(root, 'pages', name, name + '.json'), 'utf8'));
    assert.equal(config.usingComponents['navigation-bar'], '/components/navigation-bar/navigation-bar');
    const template = fs.readFileSync(path.join(root, 'pages', name, name + '.wxml'), 'utf8');
    const style = fs.readFileSync(path.join(root, 'pages', name, name + '.wxss'), 'utf8');
    assert.match(template, /<navigation-bar/);
    assert.doesNotMatch(template, /后端|接口|固定快照|能力开关|实现说明/);
    assert.doesNotMatch(style, /font-family\s*:/);
  }
});

test('collection editing and reading keep each work format visible', async () => {
  const api = {
    mutationKey: () => 'collection-format-key',
    workbenchContext: async () => ({ owners: ['owner-a'], owner_choices: [] }),
    listCollections: async () => ({ items: [] }),
    listCandidates: async () => ({ items: [
      { content_id: 'gallery-0001', content_version: 1, title: '图集', presentation: 'gallery' },
      { content_id: 'video-0001', content_version: 1, title: '视频', presentation: 'video' },
    ] }),
    mediaURL: () => '',
  };
  const { page } = pageRuntime(api);
  page.onShow(); await tick(); await page.startCreate();
  assert.deepEqual(Array.from(page.data.candidates, item => item.format_label), ['图集笔记', '视频笔记']);
  const editor = fs.readFileSync(path.join(root, 'pages/projectcollections/projectcollections.wxml'), 'utf8');
  const reader = fs.readFileSync(path.join(root, 'pages/collectionreader/collectionreader.wxml'), 'utf8');
  assert.match(editor, /{{item\.format_label}}/);
  assert.match(reader, /{{item\.format_label}}/);
});

test('collection editor keeps a single hierarchy and disables preview without works', () => {
  const template = fs.readFileSync(path.join(root, 'pages/projectcollections/projectcollections.wxml'), 'utf8');
  const style = fs.readFileSync(path.join(root, 'pages/projectcollections/projectcollections.wxss'), 'utf8');
  assert.doesNotMatch(template, /page-title\">我的合集/);
  assert.match(template, /{{collections\.length}}个合集/);
  assert.match(template, /wx:if="{{selectedItems\.length}}" class="section selected-section"/);
  assert.match(template, /暂无已发布作品/);
  assert.doesNotMatch(template, /class="secondary" bindtap="closeEditor"/);
  assert.match(template, /bindtap="previewCurrent" disabled="{{busy \|\| !selectedItems\.length}}"/);
  assert.match(style, /\.form-card input\{[^}]*height:88rpx;[^}]*line-height:88rpx;[^}]*padding:0 24rpx/);
});

test('collection save cannot overwrite edits made while its request is in flight',async()=>{
 let finish;
 const api={mutationKey:()=> 'key',workbenchContext:async()=>({owners:['owner-a'],owner_choices:[{owner_id:'owner-a',display_name:'设计师昵称'}]}),listCollections:async()=>({items:[]}),listCandidates:async()=>({items:[]}),saveCollection:()=>new Promise(r=>finish=r),mediaURL:()=>''};
 const {page}=pageRuntime(api);page.onShow();await tick();
 page.setData({editing:true,title:'原稿',owners:['owner-a'],ownerChoices:[{owner_id:'owner-a',display_name:'设计师昵称'}],selectedItems:[{content_id:'content-0001',content_version:1}]});
 const saving=page.saveDraft();page.setTitle({detail:{value:'继续编辑'}});
 finish({collection_id:'collection-0001',revision:1,title:'原稿',items:[{content_id:'content-0001',content_version:1}]});await saving;
 assert.equal(page.data.title,'继续编辑');assert.equal(page.data.dirty,true);assert.equal(page.data.current.revision,1);
});

test('collection cover download cannot overwrite edits made after save returns',async()=>{
 let finishMedia;
 const api={mutationKey:()=> 'key',workbenchContext:async()=>({owners:['owner-a'],owner_choices:[{owner_id:'owner-a',display_name:'设计师昵称'}]}),listCollections:async()=>({items:[]}),listCandidates:async()=>({items:[]}),saveCollection:async input=>({collection_id:'collection-0001',revision:1,title:input.title,owner_id:'owner-a',items:[{content_id:'content-0001',content_version:1,cover:{url:'/cover'}}]}),mediaURL:value=>'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/media'+value};
 const {page}=pageRuntime(api,'designer-session',()=>new Promise(resolve=>{finishMedia=resolve;}));page.onShow();await tick();
 page.setData({editing:true,title:'原稿',owners:['owner-a'],ownerChoices:[{owner_id:'owner-a',display_name:'设计师昵称'}],selectedItems:[{content_id:'content-0001',content_version:1}]});
 const saving=page.saveDraft();await tick();page.setTitle({detail:{value:'继续编辑'}});finishMedia('wxfile://tmp/cover');await saving;
 assert.equal(page.data.title,'继续编辑');assert.equal(page.data.dirty,true);
});

test('collection editor asks before dropping changes and modal callbacks cannot clear another account',async()=>{
 const {page,modals,storage}=pageRuntime({});
 page.visible=true;page.account='designer-session';page.epoch=1;page.setData({editing:true,dirty:true,title:'未保存'});
 page.closeEditor();assert.equal(modals.length,1);
 page.setData({editing:true,title:'另一个账号'});storage.token='other';modals[0].success({confirm:true});
 assert.equal(page.data.editing,true);assert.equal(page.data.title,'另一个账号');
});

test('late collection responses cannot replace a newly opened collection',async()=>{
 const pending=[];const {page}=readerRuntime({readPublishedCollection:()=>new Promise(r=>pending.push(r)),mediaURL:()=>''});
 page.onLoad({collection_id:'collection-old',collection_version:'1'});
 page.onUnload();page.onLoad({collection_id:'collection-new',collection_version:'2'});
 pending[1]({collection_id:'collection-new',version:2,items:[]});await tick();
 pending[0]({collection_id:'collection-old',version:1,items:[]});await tick();
 assert.equal(page.data.collection.collection_id,'collection-new');
});

for (const method of ['removeItem', 'toggleCandidate']) {
  test(`collection ${method} keeps the selected cover attached to its source`, () => {
    const { page } = pageRuntime({});
    const items = ['a', 'b', 'c'].map(content_id => ({ content_id, content_version: 1 }));
    page.setData({ selectedItems: items, candidates: items, coverItemIndex: 1 });
    const remove = index => page[method]({ currentTarget: { dataset: { index, id: page.data.selectedItems[index].content_id, version: 1 } } });
    remove(0);
    assert.equal(page.data.coverItemIndex, 0);
    assert.equal(page.data.selectedItems[page.data.coverItemIndex].content_id, 'b');
    assert.equal(page.draftInput().cover_item_index, 0);
    remove(0);
    assert.equal(page.data.coverItemIndex, null);
    assert.equal(page.draftInput().cover_item_index, undefined);
  });
}

for (const method of ['moveItem', 'topItem']) {
  test(`collection ${method} sends the updated cover index to the rendered view`, () => {
    const { page } = pageRuntime({});
    const items = ['a', 'b', 'c'].map(content_id => ({ content_id, content_version: 1 }));
    page.setData({ selectedItems: items, candidates: items, coverItemIndex: 1 });
    const updates = [];
    const setData = page.setData.bind(page);
    page.setData = values => { updates.push(values); setData(values); };
    page[method]({ currentTarget: { dataset: { index: 1, direction: -1 } } });
    assert.equal(page.data.selectedItems[0].content_id, 'b');
    assert.equal(page.data.coverItemIndex, 0);
    assert.equal(updates.at(-1).coverItemIndex, 0);
  });
}

test('reopening a collection preserves its saved cover when editing its title', async () => {
  const items = ['a', 'b'].map(content_id => ({ content_id, content_version: 1, cover: { media_id: 'cover-' + content_id, url: '/cover-' + content_id } }));
  const savedInputs = [];
  const collection = { collection_id: 'collection-0001', owner_id: 'owner-a', title: '原名称', revision: 1, items, cover: items[1].cover };
  const { page } = pageRuntime({
    getCollection: async () => collection,
    listCandidates: async () => ({ items }),
    mediaURL: value => value,
    saveCollection: async input => { savedInputs.push(input); return { ...collection, title: input.title, revision: 2 }; },
  });
  page.visible = true;
  page.account = 'designer-session';
  page.epoch = 1;
  await page.openEditorByID(collection.collection_id, 1);
  assert.equal(page.data.coverItemIndex, 1);
  page.setTitle({ detail: { value: '新名称' } });
  await page.saveDraft();
  assert.equal(savedInputs[0].cover_item_index, 1);
});

test('collection preview resolves its hero and item covers while preserving the publish token', async () => {
  const downloads = [], published = [];
  const { page } = pageRuntime({
    previewCollection: async () => ({ preview_token: 'verified-token', collection: { title: '庭院合集', description: '设计案例', cover: { url: '/hero' }, items: [{ content_id: 'work-a', content_version: 2, title: '庭院', cover: { url: '/work' } }, { content_id: 'work-b', content_version: 1, title: '无图案例' }] } }),
    mediaURL: value => 'https://api.example' + value,
    mutationKey: () => 'publish-key',
    publishCollection: async (...args) => { published.push(args); return { version: 1 }; },
    listCollections: async () => ({ items: [] }),
  }, 'designer-session', async url => { downloads.push(url); return 'wxfile://' + url.split('/').at(-1); });
  page.visible = true; page.account = 'designer-session'; page.epoch = 1;
  page.setData({ current: { collection_id: 'collection-a', revision: 3 }, dirty: false });
  await page.previewCurrent();
  assert.equal(page.data.preview.preview_token, 'verified-token');
  assert.equal(page.data.preview.collection.cover_url, 'wxfile://hero');
  assert.equal(page.data.preview.collection.items[0].cover_url, 'wxfile://work');
  assert.equal(page.data.preview.collection.items[1].cover_url, '');
  assert.deepEqual(downloads.sort(), ['https://api.example/hero', 'https://api.example/work']);
  await page.publishCurrent();
  assert.equal(published[0][2], 'verified-token');
});

test('collection preview cover completion cannot restore a previous account preview', async () => {
  let finish;
  const { page, storage } = pageRuntime({
    previewCollection: async () => ({ preview_token: 'old-token', collection: { cover: { url: '/hero' }, items: [] } }),
    mediaURL: value => 'https://api.example' + value,
    workbenchContext: async () => ({ owners: [], owner_choices: [] }),
    listCollections: async () => ({ items: [] }),
  }, 'designer-session', () => new Promise(resolve => { finish = resolve; }));
  page.visible = true; page.account = 'designer-session'; page.epoch = 1;
  page.setData({ current: { collection_id: 'collection-a', revision: 3 }, dirty: false });
  const pending = page.previewCurrent();
  await tick();
  storage.token = 'other-account';
  page.onShow();
  if (finish) finish('wxfile://old-cover');
  await pending;
  await tick();
  assert.equal(page.data.preview, null);
});

test('published collection opens its fixed reader only after the draft has been saved', () => {
  const { page, navigations } = pageRuntime({});
  page.setData({ current: { collection_id: 'collection-0001', version: 2, status: 'published' }, dirty: false });
  page.openPublished();
  assert.equal(navigations[0], '/pages/collectionreader/collectionreader?collection_id=collection-0001&collection_version=2');
  page.setData({ dirty: true });
  page.openPublished();
  assert.equal(navigations.length, 1, '新修改不能被误当成已发布版本');
  page.setData({ dirty: false, current: { collection_id: 'collection-0001', version: 0, status: 'draft' } });
  page.openPublished();
  assert.equal(navigations.length, 1);
});


test('collection more actions preserve confirmation and ignore a previous-account choice', () => {
  const { page, storage, modals, actionSheets } = pageRuntime({});
  page.visible = true; page.epoch = 1; page.account = storage.token;
  page.setData({ collections: [{ collection_id: 'collection-a', version: 0, revision: 1, status: 'draft' }] });
  const event = { currentTarget: { dataset: { id: 'collection-a' } } };
  page.openCollectionMore(event);
  assert.deepEqual(Array.from(actionSheets[0].itemList), ['删除草稿']);
  assert.equal(modals.length, 0);
  storage.token = 'other-account';
  actionSheets[0].success({ tapIndex: 0 });
  assert.equal(modals.length, 0, '旧账号选择不弹出新账号的删除确认');
});

test('collection long press deletes the current revision through the general endpoint', async () => {
  const calls = [];
  const { page, actionSheets } = pageRuntime({ deleteCollection: async (...args) => calls.push(args) });
  page.visible = true; page.epoch = 1; page.account = 'designer-session';
  page.setData({ collections: [{ collection_id: 'collection-a', version: 2, revision: 5, status: 'revoked' }] });
  page.deleteCollectionMenu({ currentTarget: { dataset: { id: 'collection-a' } } });
  actionSheets[0].success({ tapIndex: 0 });
  await tick();
  assert.deepEqual(calls[0], ['collection-a', 5]);
  assert.equal(page.data.collections.length, 0);
});
