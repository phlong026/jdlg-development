'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');
const completeProfile = { nickname: '已登记用户', avatar: 'https://static.jiadilingguang.com/avatar.jpg' };

function loadPage(relativePath, { token = '', userInfo } = {}) {
  let page;
  const requests = [];
  const prefetchedImages = [];
  const storage = { token, userInfo };
  const sourcePath = path.join(root, relativePath);
  const sourceDir = path.dirname(sourcePath);
  const requireStub = name => {
    if (name.includes('profile-validation')) return require(path.join(root, 'components/nickname-guard/profile-validation.js'));
    if (name.endsWith('/asset')) return { resolveAssetPath: value => value, normalizeCosUrl: value => value };
    if (name.endsWith('/cloudImage')) return { prepareCloudImage: value => Promise.resolve(value) };
    if (name.endsWith('/homeAssets')) return { HOME_ASSETS: {} };
    if (name.endsWith('/costRegions')) return { cityOptionsMap: { '福建': ['泉州'] } };
    if (name.endsWith('/reportNavigation')) return { discardReportLoginReturn() {}, resumeReportAfterLogin() {} };
    if (name.endsWith('/perf')) return {
      getPageCache: () => undefined,
      setPageCache() {},
      prefetchImages: values => { prefetchedImages.push(...values); return Promise.resolve(); },
    };
    if (name.endsWith('/enterpriseWechat')) return {
      DEFAULT_ENTERPRISE_SERVICE_PHONE: '', DEFAULT_ENTERPRISE_WECHAT_QRCODE: '',
      DEFAULT_CUSTOMER_SERVICE_CORP_ID: '', DEFAULT_CUSTOMER_SERVICE_URL: '',
      openEnterpriseCustomerServiceChat: () => Promise.resolve({ opened: false }),
      resolveEnterpriseWechatServiceConfig: () => ({}), tryOpenCustomerServiceDirect: () => Promise.resolve(false),
    };
    if (name.endsWith('/phase3-api')) return { request: () => Promise.resolve({}) };
    return require(path.resolve(sourceDir, name));
  };
  vm.runInNewContext(fs.readFileSync(sourcePath, 'utf8'), {
    exports: {}, require: requireStub, Page: value => { page = value; }, console,
    setTimeout, clearTimeout,
    wx: {
      getStorageSync: key => storage[key],
      request: options => { requests.push(options); return { abort() {} }; },
      getSystemInfoSync: () => ({}),
      getWindowInfo: () => ({}), getDeviceInfo: () => ({}), getAppBaseInfo: () => ({}),
      getMenuButtonBoundingClientRect: () => ({ bottom: 0 }),
      showToast() {}, navigateTo() {}, switchTab() {},
    },
  });
  page.data = JSON.parse(JSON.stringify(page.data));
  page.setData = values => Object.assign(page.data, values);
  return { page, requests, prefetchedImages, storage };
}

test('anonymous home and template square prefetch list images without protected detail requests', () => {
  const home = loadPage('pages/index/index.js');
  home.page.prefetchFeaturedGroups([{ case1: { id: 11, thumbnail: 'https://img/11.jpg' } }]);
  assert.deepEqual(home.requests, []);
  assert.deepEqual(home.prefetchedImages, ['https://img/11.jpg']);

  const square = loadPage('pages/template/template.js');
  square.page.warmLeadingCards([{ id: 12, image: 'https://img/12.jpg', targetType: 'template', cardKey: 'template:12' }]);
  assert.deepEqual(square.requests, []);
  assert.deepEqual(square.prefetchedImages, ['https://img/12.jpg']);
});

test('registered users retain home and template protected-detail warming', () => {
  const session = { token: 'valid-account', userInfo: completeProfile };
  const home = loadPage('pages/index/index.js', session);
  home.page.prefetchFeaturedGroups([{ case1: { id: 21, thumbnail: 'https://img/21.jpg' } }]);
  assert.equal(home.requests.length, 1);
  assert.match(home.requests[0].url, /\/templates\/21$/);

  const square = loadPage('pages/template/template.js', session);
  square.page.warmLeadingCards([{ id: 22, image: 'https://img/22.jpg', targetType: 'template', cardKey: 'template:22' }]);
  assert.equal(square.requests.length, 1);
  assert.match(square.requests[0].url, /\/templates\/22$/);
});
