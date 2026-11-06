const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');
const title = '甲第灵光 · AI建房助手\n建别墅从这里开始';

function loadPage(extension, { resumeReportAfterLogin = () => false, discardReportLoginReturn = () => {}, prepareCloudImage } = {}) {
  let definition;
  const menus = [];
  const warnings = [];
  const source = fs.readFileSync(path.join(root, `miniprogram/pages/index/index.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('reportNavigation')
      ? { resumeReportAfterLogin, discardReportLoginReturn }
      : name.endsWith('costRegions')
      ? require('../miniprogram/utils/costRegions.js')
      : name.endsWith('cloudImage')
      ? { prepareCloudImage: prepareCloudImage || (async (_url, fileName) => `wxfile://usr/cloud-images/${fileName}`) }
      : { resolveAssetPath: value => `https://cdn.test${value}` },
    Page: value => { definition = value; },
    wx: {
      showShareMenu: options => menus.push(options),
      env: { USER_DATA_PATH: 'wxfile://usr' },
    },
    console: { warn: (...args) => warnings.push(args) },
  });
  return { definition, menus, warnings };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 首页分享使用建房助手标题、专用封面和首页路径`, () => {
    const { definition } = loadPage(extension);
    const share = definition.onShareAppMessage();
    assert.equal(share.title, title);
    assert.equal(share.path, '/pages/index/index?from=share');
    assert.equal(share.imageUrl, undefined, '未准备好时不返回已知会破图的包内路径');
  });

  test(`${extension}: 朋友圈使用方形封面并留在当前首页`, () => {
    const { definition } = loadPage(extension);
    const share = definition.onShareTimeline.call({ data: { ...definition.data, contentReady: true, homeTimelineCover: 'wxfile://usr/home-timeline-v2.jpg' } });
    assert.equal(share.title, title);
    assert.equal(share.imageUrl, 'wxfile://usr/home-timeline-v2.jpg');
    assert.equal(share.query, 'from=share');
    assert.equal(share.path, undefined);
  });

  test(`${extension}: 云端分享封面下载为微信本地文件，保留各自原图`, async () => {
    const downloaded = [];
    const { definition } = loadPage(extension, { prepareCloudImage: async (url, fileName) => {
      downloaded.push({ url, fileName });
      return `wxfile://usr/cloud-images/${fileName}`;
    } });
    const page = { data: { ...definition.data }, setData(value) { Object.assign(this.data, value); } };
    await definition.prepareHomeShareCovers.call(page);
    assert.deepEqual(downloaded, [
      { url: 'https://cdn.test/assets/share/home-chat.jpg', fileName: 'home-chat.jpg' },
      { url: 'https://cdn.test/assets/share/home-timeline-v2.jpg', fileName: 'home-timeline-v2.jpg' },
    ]);
    assert.equal(definition.onShareAppMessage.call(page).imageUrl, 'wxfile://usr/cloud-images/home-chat.jpg');
    assert.equal(definition.onShareTimeline.call(page).imageUrl, 'wxfile://usr/cloud-images/home-timeline-v2.jpg');
  });

  test(`${extension}: 封面准备失败关闭朋友圈并记录原因`, async () => {
    const { definition, warnings } = loadPage(extension, { prepareCloudImage: async () => '' });
    const page = { data: { ...definition.data }, setData(value) { Object.assign(this.data, value); } };
    await definition.prepareHomeShareCovers.call(page);
    assert.equal(page.data.contentReady, false);
    assert.equal(definition.onShareTimeline.call(page).imageUrl, undefined);
    assert.equal(warnings.length, 0, '下载工具统一记录失败，页面只负责关闭分享');
  });

  test(`${extension}: 首次立即分享会等待封面且不会返回错误封面`, async () => {
    const pending = [];
    const { definition } = loadPage(extension, { prepareCloudImage: (url, fileName) => new Promise(resolve => pending.push({ url, fileName, resolve })) });
    const page = { data: { ...definition.data }, setData(value) { Object.assign(this.data, value); } };
    page.shareCoversPromise = definition.prepareHomeShareCovers.call(page);
    const share = definition.onShareAppMessage.call(page);
    assert.equal(share.imageUrl, undefined);
    assert.equal(typeof share.promise?.then, 'function');
    assert.equal(Object.keys(definition.onShareTimeline.call(page)).length, 0);
    pending.forEach(item => item.resolve(`wxfile://usr/cloud-images/${item.fileName}`));
    const readyShare = await share.promise;
    assert.equal(readyShare.title, title);
    assert.equal(readyShare.path, '/pages/index/index?from=share');
    assert.equal(readyShare.imageUrl, 'wxfile://usr/cloud-images/home-chat.jpg');
    assert.equal(page.data.contentReady, true);
  });

  test(`${extension}: 首页加载保留初始化，分享菜单由统一生命周期管理`, () => {
    const { definition, menus } = loadPage(extension);
    let introCalls = 0;
    definition.onLoad.call({
      prepareHomeShareCovers() {},
      showHomeIntroPopupOnce() { introCalls += 1; },
      initializeUnreadBannerLayout() {},
    });
    assert.equal(introCalls, 1);
    assert.equal(menus.length, 0, '单页模式禁止分享菜单，由全局 onShow 判断后再开启');
  });

  test(`${extension}: 首页分享清理旧报告记录，后来产生的新报告仍可恢复`, () => {
      let pendingReport = true;
      let resumeCalls = 0;
      const { definition } = loadPage(extension, {
        resumeReportAfterLogin: () => { resumeCalls++; return pendingReport; },
        discardReportLoginReturn: () => { pendingReport = false; },
      });
      let contentLoads = 0;
      const page = {
        prepareHomeShareCovers() {},
        showHomeIntroPopupOnce() {},
        initializeUnreadBannerLayout() {},
        syncTabBar() {},
        loadFeaturedGroups() { contentLoads++; },
        promptNicknameIfMissing() {},
      };
      definition.onLoad.call(page, { from: 'share' });
      definition.onShow.call(page);
      assert.equal(resumeCalls, 1);
      assert.equal(pendingReport, false);
      assert.equal(contentLoads, 1);
      pendingReport = true;
      definition.onShow.call(page);
      assert.equal(resumeCalls, 2, '同一首页实例仍可恢复新报告');
      assert.equal(contentLoads, 1);
  });

  test(`${extension}: 普通登录返回首页仍可恢复报告`, () => {
    let resumeCalls = 0;
    const { definition } = loadPage(extension, { resumeReportAfterLogin: () => { resumeCalls++; return true; } });
    const page = { prepareHomeShareCovers() {}, showHomeIntroPopupOnce() {}, initializeUnreadBannerLayout() {}, syncTabBar() { assert.fail('应先恢复报告'); } };
    definition.onLoad.call(page, {});
    definition.onShow.call(page);
    assert.equal(resumeCalls, 1);
  });
}

test('两张分享封面由云端加载，不再强制打进代码包', () => {
  const config = JSON.parse(fs.readFileSync(path.join(root, 'project.config.json'), 'utf8'));
  assert.ok(!config.packOptions.include?.some(item => item.type === 'folder' && item.value === 'assets/share'));
  for (const name of ['home-chat.jpg', 'home-timeline-v2.jpg']) {
    const bytes = fs.readFileSync(path.join(root, 'miniprogram/assets/share', name));
    assert.equal(bytes.readUInt16BE(0), 0xffd8);
    assert.ok(bytes.length < 300 * 1024, '封面应控制体积，避免占用过多代码包空间');
  }
});
