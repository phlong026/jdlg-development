'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const miniprogramRoot = path.join(__dirname, '../miniprogram');

function loadTracking(wx) {
  global.wx = wx;
  const modulePath = require.resolve('../miniprogram/utils/tracking/index');
  delete require.cache[modulePath];
  return require(modulePath);
}

function loadPage(name, wx = {}) {
  let definition;
  const dependency = new Proxy({}, {
    get: (_, key) => {
      if (key === 'resolveAssetPath' || key === 'normalizeCosUrl') return value => value;
      if (key === 'prepareShareCardImage') return async value => value;
      if (key === 'prefetchShareCardImage') return () => {};
      return () => {};
    },
  });
  const sandbox = {
    exports: {},
    require: () => dependency,
    Page: value => { definition = value; },
    wx,
    console: { log() {}, warn() {}, error() {} },
    setTimeout,
    clearTimeout,
    Promise,
    Date,
    Math,
  };
  const source = fs.readFileSync(path.join(miniprogramRoot, `pages/${name}/${name}.js`), 'utf8');
  vm.runInNewContext(source, sandbox);
  return definition;
}

test.afterEach(() => {
  delete global.wx;
});

test('public pages and pages with an explicit timeline callback enable both native share menu entries', () => {
  const shown = [];
  const hidden = [];
  const { wrapPage } = loadTracking({
    showShareMenu: options => shown.push(options),
    hideShareMenu: options => hidden.push(options),
    getLaunchOptionsSync: () => ({ scene: 1001 }),
  });
  const publicPage = wrapPage({ onShow() { return 'shown'; } });
  const explicitPage = wrapPage({ onShow() { return 'shown'; }, onShareTimeline() { return { title: '我的' }; } });

  assert.equal(publicPage.onShow.call({ route: 'pages/index/index', data: {} }), 'shown');
  assert.equal(explicitPage.onShow.call({ route: 'pages/my/my', data: {} }), 'shown');

  assert.equal(shown.length, 2);
  assert.equal(hidden.length, 0);
  for (const menu of shown) assert.deepEqual(Array.from(menu.menus), ['shareAppMessage', 'shareTimeline']);
});

test('share menu stays closed for unread content and in Moments single-page mode', () => {
  const menus = [];
  let scene = 1001;
  const { wrapPage } = loadTracking({
    showShareMenu: options => menus.push(options),
    getLaunchOptionsSync: () => ({ scene }),
  });
  const wrapped = wrapPage({ onShow() {} });

  wrapped.onShow.call({ route: 'pages/contentreader/contentreader', data: { contentReady: false } });
  scene = 1154;
  wrapped.onShow.call({ route: 'pages/inspirationdetail/inspirationdetail', data: {} });

  assert.equal(menus.length, 0);
});

test('content readiness refreshes the menu only on the visible page, independently of tracking support', () => {
  const shown = [], hidden = [];
  const { wrapPage } = loadTracking({
    showShareMenu: options => shown.push(options),
    hideShareMenu: options => hidden.push(options),
  });
  const wrapped = wrapPage({ onShareTimeline() { return { title: '首页' }; } });
  const page = {
    route: 'pages/index/index', data: { contentReady: false },
    setData(value, callback) { Object.assign(this.data, value); if (callback) callback(); },
  };
  wrapped.onLoad.call(page, {});
  wrapped.onShow.call(page);
  assert.equal(shown.length, 0);
  assert.equal(hidden.length, 1);
  page.setData({ contentReady: true });
  assert.equal(shown.length, 1);
  wrapped.onHide.call(page);
  page.setData({ contentReady: false });
  page.setData({ contentReady: true });
  assert.equal(shown.length, 1, '离开首页后异步完成不能改变其他页面菜单');
});

test('share menu errors are logged and do not interrupt the page lifecycle', () => {
  const warnings = [];
  const originalWarn = console.warn;
  console.warn = (...args) => warnings.push(args);
  try {
    const { wrapPage } = loadTracking({
      showShareMenu(options) { options.fail({ errMsg: 'showShareMenu:fail' }); },
    });
    const wrapped = wrapPage({ onShow() { return 'shown'; } });
    assert.equal(wrapped.onShow.call({ route: 'pages/index/index', data: {} }), 'shown');
    assert.equal(warnings.length, 1);
    assert.equal(warnings[0][0], '分享菜单开启失败');
    assert.equal(warnings[0][1], 'pages/index/index');
  } finally {
    console.warn = originalWarn;
  }
});

test('private pages without a business share callback hide Moments and share friends to home', () => {
  const shown = [];
  const hidden = [];
  const { wrapPage } = loadTracking({
    showShareMenu: options => shown.push(options),
    hideShareMenu: options => hidden.push(options),
    getLaunchOptionsSync: () => ({ scene: 1001 }),
  });
  const privateRoutes = [
    'pages/ordermanagement/ordermanagement',
    'pages/wallet/wallet',
    'pages/real/real',
    'pages/report/questionnaire',
    'pages/report/preview',
    'pages/report/delivery',
    'pages/unknown/unknown',
  ];

  for (const route of privateRoutes) {
    const wrapped = wrapPage({ onLoad() {}, onShow() {} });
    const page = { route, data: {} };
    wrapped.onLoad.call(page, { id: 'private-id', order_no: 'private-order' });
    wrapped.onShow.call(page);
    assert.deepEqual(wrapped.onShareTimeline.call(page), {});
    assert.equal(wrapped.onShareAppMessage.call(page).path, '/pages/index/index');
  }

  assert.equal(shown.length, 0);
  assert.equal(hidden.length, privateRoutes.length);
  for (const menu of hidden) assert.deepEqual(Array.from(menu.menus), ['shareTimeline']);
});

test('default public share excludes form state and custom Moments callbacks get an explicit empty query', () => {
  const { wrapPage } = loadTracking({ showShareMenu() {}, getLaunchOptionsSync: () => ({ scene: 1001 }) });
  const defaultShare = wrapPage({ onLoad() {} });
  const page = { route: 'pages/aicost/aicost', data: {} };
  defaultShare.onLoad.call(page, { province: '浙江', city: '杭州', buildingArea: '320' });

  const timeline = defaultShare.onShareTimeline.call(page);
  assert.equal(timeline.title, '甲第灵光 · AI建房助手');
  assert.equal(timeline.query, '', '必须显式清空，否则微信会沿用省市和建筑面积');
  assert.equal(defaultShare.onShareAppMessage.call(page).path, '/pages/aicost/aicost');

  const customShare = wrapPage({ onShareTimeline() { return { title: '生成结果' }; } });
  assert.equal(customShare.onShareTimeline.call({ route: 'pages/generatedetails/generatedetails', data: {} }).query, '');
});

test('public detail shares retain only the route inventory content identifier', () => {
  const { wrapPage } = loadTracking({});
  for (const [route, options, query] of [
    ['pages/aitooldetail/aitooldetail', { id: 'tool 1', order_no: 'private' }, 'id=tool%201'],
    ['pages/aitoolworkbench/aitoolworkbench', { id: '2', prompt: 'private' }, 'id=2'],
    ['pages/utilitytooldetail/utilitytooldetail', { id: '3', phone: 'private' }, 'id=3'],
    ['pages/designerhome/designerhome', { id: '4', phone: 'private' }, 'userId=4'],
  ]) {
    const wrapped = wrapPage({ onLoad() {} });
    const page = { route, data: {} };
    wrapped.onLoad.call(page, options);
    assert.equal(wrapped.onShareTimeline.call(page).query, query);
    assert.equal(wrapped.onShareAppMessage.call(page).path, '/' + route + '?' + query);
  }
});

test('generated result Moments share carries only its task number', () => {
  const definition = loadPage('generatedetails');
  const share = definition.onShareTimeline.call({
    data: { taskData: { task_no: 'task 1', user_prompt: '一栋现代别墅' } },
    buildShareTitle: definition.buildShareTitle,
    getCurrentShareImageUrl: () => '/tmp/result.jpg',
  });

  assert.equal(share.query, 'task_no=task%201');
  assert.equal(share.imageUrl, '/tmp/result.jpg');
  assert.equal(Object.keys(share).includes('path'), false);
});

test('invitation Moments share keeps the invite code', () => {
  const definition = loadPage('Invitefriends');
  const share = definition.onShareTimeline.call({
    data: {
      inviteCode: 'ABC123',
      userInfo: { nickname: '小甲' },
      shareImageUrl: '/tmp/invite.jpg',
      posterImageUrl: '',
    },
  });

  assert.equal(share.query, 'invite_code=ABC123');
  assert.equal(share.imageUrl, '/tmp/invite.jpg');
});

test('invitation poster gives an actionable Moments choice', () => {
  const calls = [];
  const definition = loadPage('Invitefriends', {
    showShareImageMenu: options => calls.push(options),
  });
  const page = {
    data: { posterImageUrl: '/tmp/invite.jpg', inviteCode: 'ABC123' },
    setData(values) { Object.assign(this.data, values); },
  };

  definition.onSharePoster.call(page);

  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, '/tmp/invite.jpg');
  assert.equal(calls[0].entrancePath, '/pages/index/index?invite_code=ABC123');
  const template = fs.readFileSync(path.join(miniprogramRoot, 'pages/Invitefriends/Invitefriends.wxml'), 'utf8');
  assert.match(template, /发送给好友或分享到朋友圈/);
  assert.match(template, /选择好友或朋友圈/);
});

test('My Plans fallback Moments title is readable Chinese', () => {
  const definition = loadPage('myplan');
  const share = definition.onShareTimeline.call({
    data: { pendingSharePlanId: 0, groupedPlans: [] },
    findPlanById: definition.findPlanById,
  });

  assert.equal(share.title, '我的方案');
});


test('私人预算仅在自定义公开快照准备完后开放分享，仍保持不采集埋点', () => {
  const shown = [], hidden = [];
  const { wrapPage } = loadTracking({
    showShareMenu: value => shown.push(value), hideShareMenu: value => hidden.push(value),
  });
  const wrapped = wrapPage({ onShareTimeline() { return { query: 'report_id=public-snapshot' }; } });
  const page = { route: 'pages/aicostresult/aicostresult', data: { contentReady: false },
    setData(value, callback) { Object.assign(this.data, value); if (callback) callback(); },
  };
  wrapped.onLoad.call(page, {});
  wrapped.onShow.call(page);
  assert.equal(shown.length, 0);
  assert.deepEqual(Array.from(hidden[0].menus), ['shareAppMessage', 'shareTimeline']);
  page.setData({ contentReady: true });
  assert.equal(shown.length, 1);
  wrapped.onHide.call(page);
  page.setData({ contentReady: false });
  page.setData({ contentReady: true });
  assert.equal(shown.length, 1);
});
