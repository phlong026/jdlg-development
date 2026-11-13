'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function runtime(name, api) {
  let definition;
  const storage = { token: 'account-one' }, navigation = [], toasts = [];
  const source = fs.readFileSync(path.join(__dirname, '../miniprogram/pages', name, name + '.ts'), 'utf8').replace(/\nexport \{\};?\s*$/, '');
  vm.runInNewContext(source, { Page: value => definition = value, require: () => api,
    wx: { getStorageSync: key => storage[key], hideShareMenu() {}, stopPullDownRefresh() {}, navigateTo: value => navigation.push(value.url), showToast: value => toasts.push(value.title), showModal() {} },
    Date, Promise, Number, String, Set, encodeURIComponent, setTimeout, clearTimeout, console });
  const page = { ...definition, data: structuredClone(definition.data), setData(value) { Object.assign(this.data, value); } };
  return { page, storage, navigation, toasts };
}

test('客户详情读取真实后端的顶层摘要，不把时长当匿名人数', async () => {
  const calls = [];
  const { page } = runtime('visitdetail', { listMyVisitorEntries: async (key, query) => {
    calls.push([key, query]);
    return { visitor_key: key, nickname: '陈先生', page_views: 4, content_count: 2, visible_ms: 200000,
      intent: { level: 'high' }, as_of: '2026-11-04T12:00:00Z',
      items: [{ view_id: 'v1', title: '庭院改造', visible_ms: 42000, video_watch_ms: 0, occurred_at: '2026-11-04T10:00:00Z' }] };
  } });
  page.onLoad({ visitor: 'u:77', as_of: '2026-11-04T12:00:00Z', start: '2026-11-01T00:00:00Z' });
  page._token = 'account-one'; page._epoch = 1;
  await page.loadCustomerEntries(true, 1);
  assert.equal(page.data.customerViews, 4);
  assert.equal(page.data.customerContents, 2);
  assert.equal(page.data.customerDuration, '3分20秒');
  assert.equal(page.data.visitor_name, '陈先生');
  assert.equal(calls[0][1].as_of, '2026-11-04T12:00:00Z');
  assert.equal(calls[0][1].start, '2026-11-01T00:00:00Z');
});

test('K04 素材钻取保持时间范围和统计快照，返回目标实际使用该范围', () => {
  const h = runtime('sharevisits', {});
  h.page.setData({ rangeIndex: 1, filterStart: '2026-11-10T00:00:00Z', filterEnd: '2026-11-13T00:00:00Z', materials_as_of: '2026-11-13T00:00:00Z', materials: [{ content_id: 'content-one', content_version: 2, title: '庭院' }] });
  h.page.openMaterial({ currentTarget: { dataset: { index: 0 } } });
  const options = Object.fromEntries(new URL('https://example.invalid' + h.navigation[0]).searchParams);
  const target = runtime('sharevisits', {}).page; target.onLoad(options);
  assert.equal(target.data.rangeIndex, 1);
  assert.equal(target.filterQuery().start, '2026-11-10T00:00:00Z');
  assert.equal(target.filterQuery().end, '2026-11-13T00:00:00Z');
  assert.equal(target._drillAsOf, '2026-11-13T00:00:00Z');
});

test('K05 设置保存期间返回，释放旧操作UI锁且不允许旧响应覆盖新状态', async () => {
  let release;
  const h = runtime('messagecenter', { request: async (url, method) => {
    if (url.endsWith('/preferences') && method === 'PUT') return new Promise(resolve => release = resolve);
    if (url.endsWith('/unread-count')) return { unread_count: 0 };
    return { items: [] };
  } });
  h.page._token = 'account-one'; h.page._epoch = 1;
  const pending = h.page.savePrefs(false, false);
  h.page.onShow(); release({}); await pending;
  assert.equal(h.page.data.prefsBusy, false);
  assert.equal(h.page.data.browseDelivery, true);
  h.page.data.bindingBusy = true; h.page.onShow();
  assert.equal(h.page.data.bindingBusy, false);
});

test('K06 初次明细保留列表快照，显式刷新才请求新快照', async () => {
  const calls = [];
  const h = runtime('visitdetail', { listMyVisitorEntries: async (_, query) => {
    calls.push({ ...query }); return { items: [], as_of: '2026-11-13T00:00:00Z' };
  } });
  h.page.onLoad({ visitor: 'u:77', as_of: '2026-11-12T00:00:00Z', start: '2026-11-10T00:00:00Z' });
  h.page._token = 'account-one'; h.page._epoch = 1;
  await h.page.loadCustomerEntries(true, 1);
  await h.page.onPullDownRefresh();
  assert.equal(calls[0].as_of, '2026-11-12T00:00:00Z');
  assert.equal(calls[1].as_of, undefined);
  assert.equal(calls[1].start, '2026-11-10T00:00:00Z');
});

test('客户详情在账号切换后拒绝旧响应', async () => {
  let resolve;
  const { page, storage } = runtime('visitdetail', { listMyVisitorEntries: () => new Promise(r => resolve = r) });
  page.onLoad({ visitor: 'u:77' }); page._token = 'account-one'; page._epoch = 1;
  const pending = page.loadCustomerEntries(true, 1); storage.token = 'account-two';
  resolve({ nickname: '旧账号客户', page_views: 3, items: [] }); await pending;
  assert.notEqual(page.data.visitor_name, '旧账号客户');
});

test('分享旧入口和素材筛选能直达分享Tab，不重置成客户', () => {
  const { page } = runtime('sharevisits', {});
  page.onLoad({ tab: 'shares', content_id: 'content-one', content_version: '2' });
  assert.equal(page.data.activeTab, 'shares');
  assert.equal(page.data.contentFilter, 'content-one');
  assert.equal(page.data.contentVersion, 2);
});

test('客户列表进入明细携带同一快照与筛选窗口', () => {
  const { page, navigation } = runtime('sharevisits', {});
  page.setData({ customers: [{ visitor_key: 'u:77', nickname: '陈先生', intent_level: 'high' }],
    customers_as_of: '2026-11-04T12:00:00Z', filterStart: '2026-11-01T00:00:00Z', filterEnd: '2026-11-04T12:00:00Z' });
  page.openCustomer({ currentTarget: { dataset: { index: 0 } } });
  const query = new URL('https://example.invalid' + navigation[0]).searchParams;
  assert.equal(query.get('as_of'), '2026-11-04T12:00:00Z');
  assert.equal(query.get('start'), '2026-11-01T00:00:00Z');
});

test('消息中心首屏不要求关联或加载外发设置，显式展开后才读取', async () => {
  const calls = [];
  const { page } = runtime('messagecenter', { request: async url => {
    calls.push(url);
    if (url.endsWith('/unread-count')) return { unread_count: 0 };
    if (url.endsWith('/preferences')) return { browse_delivery: true, project_delivery: true };
    return { items: [] };
  } });
  page.onShow(); await new Promise(resolve => setImmediate(resolve));
  assert.equal(calls.some(url => url.includes('official-account') || url.endsWith('/preferences')), false);
  await page.toggleSettings();
  assert.equal(calls.some(url => url.endsWith('/preferences')), true);
});

test('素材封面按受保护媒体契约读取，Asset对象不会直接作为图片地址', async () => {
  let downloads = 0;
  const { page } = runtime('sharevisits', {
    listMyContentStats: async () => ({ items: [{ content_id: 'c1', content_version: 2, title: '庭院', cover: {kind:'image',media_id:'m1',url:''} }] }),
    downloadWorkbenchMedia: async id => { downloads++; assert.equal(id,'m1'); return 'wxfile://cover'; },
  });
  page._token='account-one'; await page.loadMaterials(true);
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(page.data.materials[0].cover, 'wxfile://cover');
  assert.equal(downloads,1);
});

test('原生跳转传入编码参数时只解码一次，访客和快照不会再次转义', async () => {
  let captured;
  const {page}=runtime('visitdetail',{listMyVisitorEntries:async(visitor,query)=>{captured={visitor,query};return {items:[],page_views:1}}});
  page.onLoad({visitor:'u%3A4',name:'%E9%99%88%E5%85%88%E7%94%9F',as_of:'2026-11-04T20%3A26%3A41.717493839Z'});
  page._token='account-one';page._epoch=1;await page.loadCustomerEntries(true,1);
  assert.equal(captured.visitor,'u:4');assert.equal(captured.query.as_of,'2026-11-04T20:26:41.717493839Z');
  assert.equal(page.data.visitor_name,'陈先生');
  const {page:shares}=runtime('sharevisits',{});shares.onLoad({tab:'shares',title:'%E5%BA%AD%E9%99%A2',content_id:'c1'});assert.equal(shares.data.contentTitle,'庭院');
});
