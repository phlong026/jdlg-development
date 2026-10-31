'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');
const tick = () => new Promise(resolve => setImmediate(resolve));

function pageRuntime(api, token = 'designer-session') {
  let definition;
  const storage = { token };
  const navigations = [];
  const modals = [];
  const wx = {
    getStorageSync: key => storage[key] || '',
    hideShareMenu() {},
    showShareMenu() {},
    navigateTo: options => { navigations.push(options.url); if(options.success) options.success(); },
    navigateBack() {},
    showToast() {},
    showModal(options) { modals.push(options); options.success && options.success({ confirm: true }); },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/projectcollections/projectcollections.js'), 'utf8'), {
    Page: value => { definition = value; }, require: () => api, wx,
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
  return { page, storage, navigations, modals };
}

test('collection editor multi-selects, reorders, saves, previews and publishes fixed items', async () => {
  const calls = [];
  const candidates = [
    { content_id: 'content-0001', content_version: 1, title: '案例甲' },
    { content_id: 'content-0002', content_version: 2, title: '案例乙' },
  ];
  const api = {
    mutationKey: prefix => prefix + '-key',
    workbenchContext: async () => ({ owners: ['owner-a'], enabled: true }),
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

function readerRuntime(api, token = '') {
  const events = [];
  const tracking = {splitPath:require('../miniprogram/utils/tracking/index').splitPath,resolvePageOptions(){}, collectionToken:()=>token, preheatTimelineShare(){},emit:(...args)=>events.push(args)};
  let definition;
  const navigations = [];
  const wx = {
    hideShareMenu() {}, showShareMenu() {},
    navigateTo: options => { navigations.push(options.url); if(options.success) options.success(); },
    navigateBack() {}, switchTab() {},
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/collectionreader/collectionreader.js'), 'utf8'), {
    Page: value => { definition = value; }, require: name => name.includes('tracking/index') ? tracking : api, wx,
    Promise, Object, Array, String, Number, Error, encodeURIComponent,
  });
  const page = Object.assign({}, definition, { data: JSON.parse(JSON.stringify(definition.data)), setData(values, callback) { Object.assign(this.data, values); if (callback) callback(); } });
  return { page, navigations, events };
}

test('collection reader opens only the server-validated fixed child target and creates no child share', async () => {
  const calls = [];
  const api = {
    readPublishedCollection: async () => ({ collection_id: 'collection-0001', version: 3, title: '对外案例', items: [{ title: '案例甲', available: true }] }),
    openPublishedItem: async (...args) => { calls.push(args); return { route_key: 'pages/contentreader/contentreader', path: '/pages/contentreader/contentreader?collection_id=collection-0001&collection_version=3&content_id=content-0001&content_version=2&item_position=0' }; },
    mediaURL: () => '',
  };
  const { page, navigations, events } = readerRuntime(api, 'verified-token-b');
  page.onLoad({ collection_id: 'collection-0001', collection_version: '3' });
  await tick();
  await page.openItem({ currentTarget: { dataset: { index: 0 } } });
  assert.deepEqual(calls[0], ['collection-0001', 3, 0]);
  assert.equal(navigations[0], '/pages/contentreader/contentreader?collection_id=collection-0001&collection_version=3&content_id=content-0001&content_version=2&item_position=0&st=verified-token-b');
  assert.equal(page.data.contentReady,true);
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

test('collection save cannot overwrite edits made while its request is in flight',async()=>{
 let finish;
 const api={mutationKey:()=> 'key',workbenchContext:async()=>({owners:['owner-a']}),listCollections:async()=>({items:[]}),listCandidates:async()=>({items:[]}),saveCollection:()=>new Promise(r=>finish=r),mediaURL:()=>''};
 const {page}=pageRuntime(api);page.onShow();await tick();
 page.setData({editing:true,title:'原稿',owners:['owner-a'],selectedItems:[{content_id:'content-0001',content_version:1}]});
 const saving=page.saveDraft();page.setTitle({detail:{value:'继续编辑'}});
 finish({collection_id:'collection-0001',revision:1,title:'原稿',items:[{content_id:'content-0001',content_version:1}]});await saving;
 assert.equal(page.data.title,'继续编辑');assert.equal(page.data.dirty,true);assert.equal(page.data.current.revision,1);
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
