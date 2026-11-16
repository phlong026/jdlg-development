'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram/pages');
function runtime(name, api = {}, channel) {
  let definition;
  let token = 'account-one';
  const navigations = [];
  vm.runInNewContext(fs.readFileSync(path.join(root, name, name + '.js'), 'utf8'), {
    Page: value => { definition = value; }, require: () => api,
    wx: { getStorageSync: () => token, hideShareMenu() {}, navigateTo: value => navigations.push(value) },
    console, setTimeout, clearTimeout, Date, Promise, Number, String, encodeURIComponent,
  });
  const page = { ...definition, data: structuredClone(definition.data), setData(value) { Object.assign(this.data, value); }, getOpenerEventChannel() { return channel; } };
  return { page, navigations, setToken(value) { token = value; } };
}
test('material drilldown passes its actual snapshot summary without changing statistics filters', () => {
  const parent = runtime('sharevisits');
  const row = { content_id: 'content-one', content_version: 2, route_key: 'pages/contentreader/contentreader', title: '庭院', visitor_count: 12, views: 28, cover: 'wxfile://cover' };
  parent.page.setData({ materials: [row], materials_as_of: '2026-11-15T00:00:00Z', filterStart: '2026-11-01T00:00:00Z', filterEnd: '2026-11-15T00:00:00Z', shareWindowCursor: 'window-a' });
  parent.page.openMaterial({ currentTarget: { dataset: { index: 0 } } });
  const navigation = parent.navigations[0];
  const options = Object.fromEntries(new URL('https://example.invalid' + navigation.url).searchParams);
  const listeners = {};
  const child = runtime('sharevisits', {}, { on(name, callback) { listeners[name] = callback; } });
  child.page.onLoad(options);
  navigation.success({ eventChannel: { emit(name, data) { listeners[name](data); } } });
  assert.equal(child.page.data.materialSummary.visitor_count, 12);
  assert.equal(child.page.data.materialSummary.views, 28);
  assert.equal(child.page.data.materialSummary.cover, 'wxfile://cover');
  assert.equal(child.page.filterQuery().content_id, 'content-one');
  assert.equal(child.page.filterQuery().content_version, 2);
  assert.equal(child.page.filterQuery().share_cursor, 'window-a');
  assert.equal(child.page._windowAsOf, '2026-11-15T00:00:00Z');
});
test('material drilldown normalizes legacy route and version before matching the child snapshot', () => {
  const parent = runtime('sharevisits');
  parent.page.setData({ materials: [{ content_id: 'legacy-content', title: '旧素材', visitor_count: 3, views: 7 }], materials_as_of: '2026-11-15T00:00:00Z', filterStart: '2026-11-01T00:00:00Z', filterEnd: '2026-11-15T00:00:00Z' });
  parent.page.openMaterial({ currentTarget: { dataset: { index: 0 } } });
  const navigation = parent.navigations[0];
  const options = Object.fromEntries(new URL('https://example.invalid' + navigation.url).searchParams);
  const listeners = {};
  const child = runtime('sharevisits', {}, { on(name, callback) { listeners[name] = callback; } });
  child.page.onLoad(options);
  navigation.success({ eventChannel: { emit(name, data) { listeners[name](data); } } });
  assert.equal(child.page.data.contentVersion, 1);
  assert.equal(child.page.data.contentRoute, '');
  assert.equal(child.page.data.materialSummary.content_version, 1);
  assert.equal(child.page.data.materialSummary.route_key, '');
  assert.equal(child.page.data.materialSummary.visitor_count, 3);
});
test('late material context is rejected after account or snapshot window changes', () => {
  const parent = runtime('sharevisits');
  parent.page.setData({ materials: [{ content_id: 'content-one', content_version: 2, route_key: '', title: '素材' }], materials_as_of: '2026-11-15T00:00:00Z', filterStart: '2026-11-01T00:00:00Z', filterEnd: '2026-11-15T00:00:00Z' });
  parent.page.openMaterial({ currentTarget: { dataset: { index: 0 } } });
  const navigation = parent.navigations[0];
  let emitted = 0;
  parent.setToken('account-two');
  navigation.success({ eventChannel: { emit() { emitted += 1; } } });
  assert.equal(emitted, 0);

  const options = Object.fromEntries(new URL('https://example.invalid' + navigation.url).searchParams);
  const listeners = {};
  const child = runtime('sharevisits', {}, { on(name, callback) { listeners[name] = callback; } });
  child.page.onLoad(options);
  child.page.selectRange({ currentTarget: { dataset: { index: 2 } } });
  listeners.materialContext({ content_id: 'content-one', content_version: 2, route_key: '', summary_as_of: '2026-11-15T00:00:00Z', summary_start: '2026-11-01T00:00:00Z', summary_end: '2026-11-15T00:00:00Z' });
  assert.equal(child.page.data.materialSummary, null);
});
test('customer detail uses returned identity and keeps recent entries separate from full history', async () => {
  const entries = Array.from({ length: 4 }, (_, i) => ({ view_id: 'v' + i, content_id: 'content-one', content_version: 2, route_key: 'pages/contentreader/contentreader', title: '庭院', visible_ms: 12000, occurred_at: '2026-11-15T00:00:00Z' }));
  const h = runtime('visitdetail', { listMyVisitorEntries: async () => ({ nickname: '林女士', avatar_url: 'https://avatar.example/77', identity_state: 'identified', page_views: 4, content_count: 1, visible_ms: 48000, items: entries, project_activities: [] }) });
  h.page.onLoad({ visitor: 'u:77' }); h.page._token = 'account-one'; h.page._epoch = 1;
  await h.page.loadCustomerEntries(true, 1);
  assert.equal(h.page.data.customerAvatar, 'https://avatar.example/77');
  assert.equal(h.page.data.customerIdentityLabel, '已识别');
  assert.equal(h.page.data.visibleEntries.length, 2);
  assert.equal(h.page.data.entries.length, 4);
  h.page.toggleEntries();
  assert.equal(h.page.data.visibleEntries.length, 4);
  assert.equal(h.page.data.customerViews, 4);
  assert.equal(h.page.data.customerDuration, '48秒');
});
test('reference page structures keep a dedicated material visitor header and inline browsing rows', () => {
  const shares = fs.readFileSync(path.join(root, 'sharevisits/sharevisits.wxml'), 'utf8');
  const detail = fs.readFileSync(path.join(root, 'visitdetail/visitdetail.wxml'), 'utf8');
  assert.match(shares, /material-visitor-summary/);
  assert.match(shares, /material-visitor-toolbar/);
  assert.match(shares, /material-viewer-row/);
  assert.match(detail, /recent-browsing/);
  assert.match(detail, /关联项目/);
  assert.doesNotMatch(detail, /客户访问概览/);
});
test('changing a material date range invalidates the old summary instead of relabelling its counts', () => {
  const h = runtime('sharevisits');
  h.page.setData({ contentFilter: 'content-one', materialSummary: { visitor_count: 12, views: 28, countsCurrent: true } });
  h.page.selectRange({ currentTarget: { dataset: { index: 2 } } });
  assert.equal(h.page.data.materialSummary.countsCurrent, false);
  assert.equal(h.page.data.materialSummary.visitor_count, 12);
  assert.equal(h.page.data.rangeIndex, 2);
});
test('a cover handed from a visit card applies only to the same content version and route', async () => {
  const listeners = {};
  const h = runtime('visitdetail', { listMyVisitorEntries: async () => ({ nickname: '林女士', identity_state: 'identified', items: [
    { view_id: 'one', content_id: 'content-one', content_version: 2, route_key: 'pages/contentreader/contentreader' },
    { view_id: 'older', content_id: 'content-one', content_version: 1, route_key: 'pages/contentreader/contentreader' },
    { view_id: 'other', content_id: 'content-two', content_version: 2, route_key: 'pages/contentreader/contentreader' },
  ] }) }, { on(name, callback) { listeners[name] = callback; } });
  h.page.onLoad({ visitor: 'u:77' }); h.page._token = 'account-one'; h.page._epoch = 1;
  listeners.customerContext({ visitor_key: 'u:77', content_id: 'content-one', content_version: 2, route_key: 'pages/contentreader/contentreader', cover: 'wxfile://existing-cover' });
  await h.page.loadCustomerEntries(true, 1);
  assert.equal(h.page.data.entries[0].cover, 'wxfile://existing-cover');
  assert.equal(h.page.data.entries[1].cover, '');
  assert.equal(h.page.data.entries[2].cover, '');
  assert.equal(h.page.data.entries.length, 3);
});
test('failed avatar URLs fall back locally without clearing a newer avatar or customer metrics', async () => {
  const feed = runtime('sharevisits'); feed.page._token = 'account-one';
  feed.page.setData({ customerVisits: [{ visitor_key: 'u:77', avatar: 'https://avatar.example/old', views: 4 }], customers: [{ visitor_key: 'u:77', avatar: 'https://avatar.example/new', views: 4 }] });
  feed.page.avatarLoadError({ currentTarget: { dataset: { visitor: 'u:77', avatar: 'https://avatar.example/old' } } });
  assert.equal(feed.page.data.customerVisits[0].avatar, '');
  assert.equal(feed.page.data.customers[0].avatar, 'https://avatar.example/new');
  const detail = runtime('visitdetail'); detail.page.onLoad({ visitor: 'u:77' }); detail.page._token = 'account-one';
  detail.page.setData({ customerAvatar: 'https://avatar.example/new', customerViews: 4 });
  detail.page.customerAvatarLoadError({ currentTarget: { dataset: { avatar: 'https://avatar.example/old' } } });
  assert.equal(detail.page.data.customerAvatar, 'https://avatar.example/new');
  detail.page.customerAvatarLoadError({ currentTarget: { dataset: { avatar: 'https://avatar.example/new' } } });
  assert.equal(detail.page.data.customerAvatar, '');
  assert.equal(detail.page.data.customerViews, 4);
});
test('failed covers only clear the same actor and original URL in the current account', () => {
  const feed = runtime('sharevisits'); feed.page._token = 'account-one';
  feed.page.setData({ contentFilter: 'content-one', contentVersion: 1, contentRoute: '', materialSummary: { content_id: 'content-one', content_version: 1, route_key: '', cover: 'wxfile://old' }, materials: [{ content_key: 'one', cover: 'wxfile://old' }, { content_key: 'two', cover: 'wxfile://new' }] });
  feed.page.materialSummaryCoverError({ currentTarget: { dataset: { content: 'content-one', version: 1, route: '', cover: 'wxfile://stale' } } });
  assert.equal(feed.page.data.materialSummary.cover, 'wxfile://old');
  feed.page.materialSummaryCoverError({ currentTarget: { dataset: { content: 'content-one', version: 1, route: '', cover: 'wxfile://old' } } });
  assert.equal(feed.page.data.materialSummary.cover, '');
  feed.page.materialCoverLoadError({ currentTarget: { dataset: { key: 'one', cover: 'wxfile://old' } } });
  assert.equal(feed.page.data.materials[0].cover, '');
  assert.equal(feed.page.data.materials[1].cover, 'wxfile://new');

  const detail = runtime('visitdetail'); detail.page.onLoad({ visitor: 'u:77' }); detail.page._token = 'account-one';
  detail.page._entryCoverContext = { visitor_key: 'u:77', content_id: 'content-one', content_version: 1, route_key: '', cover: 'wxfile://old' };
  detail.page.setData({ entries: [{ view_id: 'entry-one', content_id: 'content-one', content_version: 1, route_key: '', cover: 'wxfile://old' }, { view_id: 'entry-two', cover: 'wxfile://new' }], visibleEntries: [{ view_id: 'entry-one', content_id: 'content-one', content_version: 1, route_key: '', cover: 'wxfile://old' }, { view_id: 'entry-two', cover: 'wxfile://new' }] });
  detail.page.entryCoverLoadError({ currentTarget: { dataset: { id: 'entry-one', cover: 'wxfile://old' } } });
  assert.equal(detail.page.data.entries[0].cover, '');
  assert.equal(detail.page.data.entries[1].cover, 'wxfile://new');
  assert.equal(detail.page._entryCoverContext, null);
  detail.setToken('account-two');
  detail.page.entryCoverLoadError({ currentTarget: { dataset: { id: 'entry-two', cover: 'wxfile://new' } } });
  assert.equal(detail.page.data.entries[1].cover, 'wxfile://new');
});
test('cover bindings carry actor and original URL guards and video progress stays inline', () => {
  const shares = fs.readFileSync(path.join(root, 'sharevisits/sharevisits.wxml'), 'utf8');
  const detail = fs.readFileSync(path.join(root, 'visitdetail/visitdetail.wxml'), 'utf8');
  assert.match(shares, /binderror="materialSummaryCoverError"/);
  assert.match(shares, /binderror="materialCoverLoadError"/);
  assert.match(detail, /binderror="entryCoverLoadError"/);
  assert.match(detail, /item\.is_video && item\.progress/);
});
test('compact customer dates are presentation fields and retain the original visit timestamp', async () => {
  const now = new Date(); now.setHours(12, 34, 0, 0);
  const occurred = now.toISOString();
  const h = runtime('visitdetail', { listMyVisitorEntries: async () => ({ nickname: '林女士', identity_state: 'identified', items: [{ view_id: 'one', type: 'video', progress_pct: 55, occurred_at: occurred, visible_ms: 12000 }] }) });
  h.page.onLoad({ visitor: 'u:77' }); h.page._token = 'account-one'; h.page._epoch = 1;
  await h.page.loadCustomerEntries(true, 1);
  assert.equal(h.page.data.entries[0].occurred_at, occurred);
  assert.equal(h.page.data.entries[0].occurred_short_text, '今天 12:34');
  assert.match(h.page.data.entries[0].occurred_text, /^\d{4}-/);
  assert.equal(h.page.data.entries[0].is_video, true);
  assert.equal(h.page.data.entries[0].progress, 55);
});
