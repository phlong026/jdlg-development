'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');

function load(extension, options = {}) {
  let definition;
  const requests = [];
  const navigations = [];
  const toasts = [];
  const warnings = [];
  const storage = { token: options.token === undefined ? 'account-a' : options.token };
  const pagePath = path.join(root, `miniprogram/pages/index/index.${extension}`);
  const source = fs.readFileSync(pagePath, 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  const asset = value => value;
  const dependencies = {
    '../../utils/asset': { resolveAssetPath: asset, normalizeCosUrl: asset },
    '../../utils/cloudImage': { prepareCloudImage: async value => value },
    '../../utils/homeAssets': { HOME_ASSETS: {} },
    '../../utils/costRegions': { cityOptionsMap: { '福建': [] } },
    '../../utils/reportNavigation': { discardReportLoginReturn() {}, resumeReportAfterLogin: () => false },
    '../../utils/perf': { getPageCache() {}, prefetchImages() {}, setPageCache() {} },
    '../../utils/enterpriseWechat': {
      DEFAULT_ENTERPRISE_SERVICE_PHONE: '', DEFAULT_ENTERPRISE_WECHAT_QRCODE: '',
      DEFAULT_CUSTOMER_SERVICE_CORP_ID: '', DEFAULT_CUSTOMER_SERVICE_URL: '',
      openEnterpriseCustomerServiceChat() {}, resolveEnterpriseWechatServiceConfig() {}, tryOpenCustomerServiceDirect() {},
    },
    '../../utils/phase3-api': {
      request: (...args) => new Promise((resolve, reject) => requests.push({ args, resolve, reject })),
    },
  };
  const wx = {
    getStorageSync: key => storage[key],
    getMenuButtonBoundingClientRect: options.noMenuRect ? undefined : () => ({ bottom: 92 }),
    getSystemInfoSync: () => ({ statusBarHeight: 24 }),
    navigateTo: request => navigations.push(request),
    request: request => requests.push({
      args: [request.url, request.method],
      resolve: data => request.success({ statusCode: 200, data: { code: 0, data } }),
      reject: error => request.fail(error),
    }),
    showToast: value => toasts.push(value),
  };
  vm.runInNewContext(code, {
    exports: {}, module: { exports: {} }, require: name => dependencies[name] || {}, Page: value => { definition = value; },
    wx, console: { warn: (...args) => warnings.push(args), error() {}, log() {} }, Promise, Set, Date, Math, Object,
  });
  const createPage = () => {
    const page = { ...definition, data: structuredClone(definition.data) };
    page.setData = values => Object.assign(page.data, values);
    return page;
  };
  return { createPage, requests, navigations, storage, toasts, warnings };
}

async function settle() {
  await new Promise(resolve => setImmediate(resolve));
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: dismiss survives home page recreation for the same runtime account`, async () => {
    const runtime = load(extension);
    const first = runtime.createPage();
    const initial = first.checkUnreadNotifications();
    runtime.requests[0].resolve({ unread_count: 3 });
    await initial;
    assert.equal(first.data.unreadBannerVisible, true);
    first.dismissUnreadBanner();
    assert.equal(first.data.unreadBannerVisible, false);

    const recreated = runtime.createPage();
    const second = recreated.checkUnreadNotifications();
    runtime.requests[1].resolve({ unread_count: 3 });
    await second;
    assert.equal(recreated.data.unreadBannerVisible, false);
    assert.equal(recreated.data.unreadCount, 3, '关闭横幅不能清空未读语义');

    runtime.storage.token = 'account-b';
    const otherAccount = runtime.createPage();
    const third = otherAccount.checkUnreadNotifications();
    runtime.requests[2].resolve({ unread_count: 1 });
    await third;
    assert.equal(otherAccount.data.unreadBannerVisible, true, '抑制状态必须按账号隔离');
  });

  test(`${extension}: stale, hidden, dismissed and signed-out responses cannot reopen the banner`, async () => {
    const runtime = load(extension);
    const page = runtime.createPage();
    page._unreadBannerPageVisible = true;
    page.checkUnreadNotifications();
    page.checkUnreadNotifications();
    assert.equal(runtime.requests.length, 2);
    runtime.requests[1].resolve({ unread_count: 2 });
    await settle();
    runtime.requests[0].resolve({ unread_count: 9 });
    await settle();
    assert.equal(page.data.unreadCount, 2, '迟到的旧请求不能覆盖新请求');

    page.checkUnreadNotifications();
    page.dismissUnreadBanner();
    runtime.requests[2].resolve({ unread_count: 4 });
    await settle();
    assert.equal(page.data.unreadBannerVisible, false);

    runtime.storage.token = '';
    await page.checkUnreadNotifications();
    assert.equal(page.data.unreadCount, 0);
    assert.equal(page.data.unreadBannerVisible, false);

    runtime.storage.token = 'account-a';
    page.checkUnreadNotifications();
    page.onHide();
    runtime.requests[3].resolve({ unread_count: 5 });
    await settle();
    assert.equal(page.data.unreadBannerVisible, false);
    page.onUnload();
  });

  test(`${extension}: hidden, unloaded and switched accounts reject pending responses before any dismissal`, async () => {
    for (const lifecycle of ['onHide', 'onUnload']) {
      const runtime = load(extension);
      const page = runtime.createPage();
      page._unreadBannerPageVisible = true;
      const pending = page.checkUnreadNotifications();
      page[lifecycle]();
      runtime.requests[0].resolve({ unread_count: 7 });
      await pending;
      assert.equal(page.data.unreadBannerVisible, false, lifecycle);
      assert.equal(page.data.unreadCount, 0, lifecycle);
    }

    const runtime = load(extension);
    const page = runtime.createPage();
    const oldAccount = page.checkUnreadNotifications();
    runtime.storage.token = 'account-b';
    const currentAccount = page.checkUnreadNotifications();
    runtime.requests[1].resolve({ unread_count: 1 });
    await currentAccount;
    runtime.requests[0].resolve({ unread_count: 8 });
    await oldAccount;
    assert.equal(page.data.unreadCount, 1);
    assert.equal(page.data.unreadBannerVisible, true);
  });

  test(`${extension}: message-center navigation suppresses the banner without marking messages read and recovers on failure`, async () => {
    const runtime = load(extension);
    const page = runtime.createPage();
    page.setData({ unreadCount: 2, unreadBannerVisible: true });
    page.goToMessageCenter();
    assert.equal(page.data.unreadBannerVisible, false);
    assert.equal(runtime.navigations.length, 1);
    assert.equal(runtime.requests.some(item => String(item.args[0]).includes('read-all')), false);
    runtime.navigations[0].fail(new Error('blocked'));
    assert.equal(page.data.unreadBannerVisible, true);
    assert.match(runtime.toasts[0].title, /打开失败|重试/);

    const dismissedDuringNavigation = runtime.createPage();
    dismissedDuringNavigation._unreadBannerPageVisible = true;
    dismissedDuringNavigation.setData({ unreadCount: 2, unreadBannerVisible: true });
    dismissedDuringNavigation.goToMessageCenter();
    dismissedDuringNavigation.dismissUnreadBanner();
    runtime.navigations[1].fail(new Error('late failure'));
    assert.equal(dismissedDuringNavigation.data.unreadBannerVisible, false, '迟到的跳转失败不能覆盖用户再次关闭');

    const hiddenDuringNavigation = runtime.createPage();
    hiddenDuringNavigation._unreadBannerPageVisible = true;
    hiddenDuringNavigation.setData({ unreadCount: 2, unreadBannerVisible: true });
    hiddenDuringNavigation.goToMessageCenter();
    hiddenDuringNavigation.onHide();
    runtime.navigations[2].fail(new Error('late failure'));
    assert.equal(hiddenDuringNavigation.data.unreadBannerVisible, false, '页面离开后不能被跳转回调重新打开横幅');
  });

  test(`${extension}: account switches clear previous unread state before the new request settles`, async () => {
    const runtime = load(extension);
    const page = runtime.createPage();
    const first = page.checkUnreadNotifications();
    runtime.requests[0].resolve({ unread_count: 6 });
    await first;
    assert.equal(page.data.unreadBannerVisible, true);

    runtime.storage.token = 'account-b';
    const second = page.checkUnreadNotifications();
    assert.equal(page.data.unreadCount, 0);
    assert.equal(page.data.unreadBannerVisible, false);
    runtime.requests[1].reject(new Error('offline'));
    await second;
    assert.equal(page.data.unreadCount, 0, '新账号请求失败时不能残留旧账号数据');
    assert.equal(page.data.unreadBannerVisible, false);
  });

  test(`${extension}: unread failures stay quiet and safe-top layout uses menu and fallback metrics`, async () => {
    const runtime = load(extension);
    const page = runtime.createPage();
    page.initializeUnreadBannerLayout();
    assert.equal(page.data.unreadBannerTop, 100);
    const pending = page.checkUnreadNotifications();
    runtime.requests[0].reject(new Error('offline'));
    await pending;
    assert.equal(page.data.unreadBannerVisible, false);
    assert.equal(runtime.warnings.length, 1);

    const fallback = load(extension, { noMenuRect: true });
    const fallbackPage = fallback.createPage();
    fallbackPage.initializeUnreadBannerLayout();
    assert.equal(fallbackPage.data.unreadBannerTop, 76);
  });
}

test('banner template and styles preserve close semantics and a reachable fixed hit target', () => {
  const wxml = fs.readFileSync(path.join(root, 'miniprogram/pages/index/index.wxml'), 'utf8');
  const scss = fs.readFileSync(path.join(root, 'miniprogram/pages/index/index.scss'), 'utf8');
  const wxss = fs.readFileSync(path.join(root, 'miniprogram/pages/index/index.wxss'), 'utf8');
  assert.match(wxml, /catchtap="dismissUnreadBanner"/);
  assert.match(wxml, /aria-label="关闭消息提醒"/);
  assert.match(wxml, /查看访问提醒与项目动态/);
  assert.match(wxml, /style="top: \{\{unreadBannerTop\}\}px"/);
  for (const style of [scss, wxss]) {
    assert.match(style, /\.unread-banner\s*\{[^}]*position:\s*fixed/s);
    assert.match(style, /\.unread-banner-close\s*\{[^}]*min-width:\s*44px/s);
    assert.match(style, /\.unread-banner-close\s*\{[^}]*min-height:\s*44px/s);
  }
});
