const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');

function harness() {
  let page;
  const requests = [];
  const toasts = [];
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/distribution/distribution.js'), 'utf8'), {
    exports: {}, require: name => require(path.resolve(root, 'pages/distribution', name)), Page: value => { page = value; }, console: { error() {}, warn() {} },
    wx: { getSystemInfoSync: () => ({}), request: request => requests.push(request), showToast: value => toasts.push(value.title) },
  });
  page.data = JSON.parse(JSON.stringify(page.data));
  page.setData = value => Object.assign(page.data, value);
  const respond = (index, data) => requests[index].success({ statusCode: 200, data: { code: 0, data } });
  return { page, requests, respond, toasts };
}
const previews = { popular: [{ id: 1, likes: 5 }, { id: 2, likes: 3 }], latest: [{ id: 2 }, { id: 1 }] };
const designer = { designer_user_id: 7, designer_name: '设计师', work_previews: previews };

test('擅长筛选由服务端在分页前执行，保留真实匹配，不按卡片推测标签二次删掉', async () => {
  const h = harness();
  h.page.data.selectedTrait = '新中式';
  const pending = h.page.loadDesigners(true);
  assert.equal(h.requests[0].data.specialty, '新中式');
  h.respond(0, { list: [designer], total: 23, filters_applied: true, filter_options: { specialties: ['新中式', '庭院设计'] } });
  await pending;
  assert.equal(h.page.data.designerCards.length, 1);
  assert.equal(h.page.data.designerCountText, '23');
  assert.equal(h.page.data.hasMore, true);
  assert.deepEqual(Array.from(h.page.data.traitFilters), ['全部', '新中式', '庭院设计']);
});

test('综合排序可切换，地区筛选与排序参数共同生效', async () => {
  const h = harness();
  let pending = h.page.onDesignerSortChange({ detail: { value: '1' } });
  assert.equal(h.requests[0].data.sort, 'popular');
  h.respond(0, { list: [designer], total: 1, filters_applied: true });
  await pending;
  pending = h.page.onRegionChange({ detail: { value: ['福建省', '厦门市', '不限'] } });
  assert.equal(h.requests[1].data.region, '福建省 厦门市');
  assert.equal(h.requests[1].data.sort, 'popular');
  h.respond(1, { list: [{ ...designer, cert_status: 'approved' }], total: 1, filters_applied: true });
  await pending;
  assert.equal(h.page.data.selectedRegionText, '福建省 厦门市');
});

test('旧服务不支持筛选时明确提示且恢复选项，网络失败保留当前页码与结果', async () => {
  const h = harness();
  Object.assign(h.page.data, { designerCards: [{ designerUserId: 9 }], page: 4, hasMore: false });
  const pending = h.page.onDesignerSortChange({ detail: { value: '1' } });
  h.respond(0, { list: [designer], total: 1 });
  await pending;
  assert.equal(h.page.data.designerSortIndex, 0);
  assert.equal(h.page.data.designerCards[0].designerUserId, 9);
  assert.equal(h.page.data.page, 4);
  assert.equal(h.page.data.hasMore, false);
  assert.ok(h.toasts.some(value => value.includes('暂时无法筛选')));
});

test('旧列表只有代表作品时，从同一设计师公开主页补齐两件真实作品并按赞数/时间排序', async () => {
  const h = harness();
  const pending = h.page.loadDesigners(true);
  h.respond(0, { list: [{ designer_user_id: 7, representative_work: { id: 1, image: '/1.jpg' } }], total: 1 });
  await new Promise(resolve => setImmediate(resolve));
  assert.ok(h.requests[1].url.endsWith('/designers/7/homepage'));
  h.respond(1, { works: [
    { id: 1, likes: 1, created_at: '2026-11-01', image: '/1.jpg' },
    { id: 2, likes: 9, created_at: '2026-11-02', image: '/2.jpg' },
    { id: 3, likes: 2, created_at: '2026-11-03', image: '/3.jpg' },
  ] });
  await pending;
  assert.deepEqual(Array.from(h.page.data.designerCards[0].works, work => work.id), [2, 3]);
  h.page.onWorkSortTap({ currentTarget: { dataset: { userId: 7, sort: 'latest' } } });
  assert.deepEqual(Array.from(h.page.data.designerCards[0].works, work => work.id), [3, 2]);
});

test('主页补图失败保留代表作品，单作品不重复伪造第二张', async () => {
  const h = harness();
  const pending = h.page.loadDesigners(true);
  h.respond(0, { list: [{ designer_user_id: 7, representative_work: { id: 1 } }], total: 1 });
  await new Promise(resolve => setImmediate(resolve));
  h.requests[1].fail(new Error('offline'));
  await pending;
  assert.equal(h.page.data.designerCards[0].works.length, 1);
  assert.equal(h.page.data.designerCards[0].works[0].id, 1);
});

test('补图按当前分类匹配，补图并发最多三位设计师，新接口无需额外主页请求', async () => {
  const h = harness();
  h.page.data.currentTabIndex = 1;
  const pending = h.page.loadDesigners(true);
  h.respond(0, { list: Array.from({ length: 5 }, (_, i) => ({ designer_user_id: i + 1 })), total: 5 });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(h.requests.length, 4);
  for (let i = 1; i <= 5; i++) {
    h.respond(i, { works: [{ id: 1, main_tab: 'material' }, { id: 2, main_tab: 'template' }, { id: 3, sub_tab: 'interior_space' }] });
    await new Promise(resolve => setImmediate(resolve));
  }
  await pending;
  assert.deepEqual(Array.from(h.page.data.designerCards[0].works, work => work.id), [3, 2]);
  const h2 = harness();
  const p2 = h2.page.loadDesigners(true);
  h2.respond(0, { list: [designer], total: 1 });
  await p2;
  assert.equal(h2.requests.length, 1);
});

test('四个入口保留原图形并内嵌到样式，打包排除assets/designer不再影响图标', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/distribution/distribution.wxml'), 'utf8');
  const scss = fs.readFileSync(path.join(root, 'pages/distribution/distribution.scss'), 'utf8');
  assert.ok(!wxml.includes('/assets/designer/'));
  for (const name of ['cases', 'villa', 'courtyard', 'collections']) {
    assert.ok(wxml.includes(`feature-icon-${name}`));
    assert.ok(scss.includes(`.feature-icon-${name}`));
  }
  assert.equal((scss.match(/data:image\/svg\+xml/g) || []).length, 4);
});


test('列表头像沿用合法CDN，图片失败只回退对应设计师', async () => {
  const h = harness();
  const pending = h.page.loadDesigners(true);
  h.respond(0, { list: [
    { ...designer, designer_avatar: 'https://jiadilingguangcos-1393500756.cos.ap-chongqing.myqcloud.com/avatars/7.jpg' },
    { ...designer, designer_user_id: 8, designer_avatar: 'https://static.jiadilingguang.com/avatars/8.jpg' },
  ], total: 2 });
  await pending;
  assert.equal(h.page.data.designerCards[0].designerAvatar, 'https://static.jiadilingguang.com/avatars/7.jpg');
  h.page.onDesignerAvatarError({ currentTarget: { dataset: { userId: 7 } } });
  assert.equal(h.page.data.designerCards[0].designerAvatar, '');
  assert.equal(h.page.data.designerCards[1].designerAvatar, 'https://static.jiadilingguang.com/avatars/8.jpg');
});

test('地区可以按省、市筛选并还原全国，失败保留上次所在地', async () => {
  const h = harness();
  let pending = h.page.onRegionChange({ detail: { value: ['福建省', '不限', '不限'] } });
  assert.equal(h.requests[0].data.region, '福建省');
  h.respond(0, { list: [designer], total: 1, filters_applied: true });
  await pending;
  pending = h.page.onRegionChange({ detail: { value: ['广东省', '深圳市', '南山区'] } });
  h.requests[1].fail(new Error('offline'));
  await pending;
  assert.equal(h.page.data.selectedRegionText, '福建省');
  pending = h.page.onRegionChange({ detail: { value: ['不限', '不限', '不限'] } });
  assert.equal(h.requests[2].data.region, '');
  h.respond(2, { list: [designer], total: 1, filters_applied: true });
  await pending;
  assert.equal(h.page.data.selectedRegionText, '');
});


test('显示接口总人数，支持主动加载下一页且不把第一页当成全部', async () => {
  const h = harness();
  let pending = h.page.loadDesigners(true);
  h.respond(0, { list: [designer], total: 25, filters_applied: true });
  await pending;
  assert.equal(h.page.data.designerCountText, '25');
  assert.equal(h.page.data.hasMore, true);
  pending = h.page.onLoadMoreDesigners();
  assert.equal(h.requests[1].data.page, 2);
  h.respond(1, { list: [{ ...designer, designer_user_id: 8 }], total: 25, filters_applied: true });
  await pending;
  assert.equal(h.page.data.designerCards.length, 2);
  assert.equal(h.page.data.designerCountText, '25');
  assert.equal(h.page.data.hasMore, true);
});

test('筛选箭头为固定大小三角形，长文字单独省略不挤压箭头', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/distribution/distribution.wxml'), 'utf8');
  const scss = fs.readFileSync(path.join(root, 'pages/distribution/distribution.scss'), 'utf8');
  assert.equal((wxml.match(/class="filter-arrow"/g) || []).length, 3);
  assert.ok(!wxml.includes('⌄'));
  assert.equal((wxml.match(/class="filter-label"/g) || []).length, 3);
  assert.ok(scss.includes('border-top: 8rpx solid'));
  assert.ok(wxml.includes('共 {{designerCountText}} 位设计师'));
  assert.ok(wxml.includes('bindtap="onLoadMoreDesigners"'));
});
