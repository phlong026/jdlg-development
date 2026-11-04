const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { spawnSync } = require('node:child_process');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');
const read = relative => fs.readFileSync(path.join(root, relative), 'utf8');
const { HOME_ASSETS } = require('../miniprogram/utils/homeAssets.js');

function transpile(relative, extension) {
  const source = read(`miniprogram/${relative}.${extension}`);
  return extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 启动时预热实际使用的远程底栏图片`, () => {
    let definition;
    const imageSources = [];
    vm.runInNewContext(transpile('app', extension), {
      exports: {},
      require: name => name.endsWith('promptReview')
        ? { blockUnsafeText: () => '', initPromptReview: () => Promise.resolve() }
        : name.endsWith('homeAssets') ? { HOME_ASSETS } : { install() {} },
      App: value => { definition = value; },
      wx: {
        getStorageSync: () => [],
        setStorageSync() {},
        getImageInfo: options => imageSources.push(options.src),
      },
      console,
    });

    definition.onLaunch({ query: {} });
    assert.deepEqual(Array.from(imageSources), [
      HOME_ASSETS.defaultTabbar,
      HOME_ASSETS.homeTabbar,
      HOME_ASSETS.templateTabbar,
      HOME_ASSETS.designerTabbar,
      HOME_ASSETS.myTabbar,
    ]);
  });

  test(`${extension}: 点击底栏时保留当前图片，目标页面显示后再按路由同步`, () => {
    let definition;
    let route = 'pages/index/index';
    const navigations = [];
    vm.runInNewContext(transpile('custom-tab-bar/index', extension), {
      exports: {},
      require: name => name.endsWith('homeAssets') ? { HOME_ASSETS } : {},
      Component: value => { definition = value; },
      getApp: () => ({ globalData: {} }),
      getCurrentPages: () => [{ route }],
      wx: { switchTab: options => navigations.push(options), showToast() {} },
    });
    const instance = {
      ...definition.methods,
      data: { ...definition.data },
      setData(values) { Object.assign(this.data, values); },
    };

    instance.updateCurrentByRoute();
    instance.switchTab({ currentTarget: { dataset: { index: 1 } } });
    instance.switchTab({ currentTarget: { dataset: { index: 1 } } });
    assert.equal(instance.data.current, 0, '离开旧页面前不换整张导航图片');
    assert.equal(navigations.length, 1, '同一跳转未结束时忽略重复点击');

    route = 'pages/template/template';
    navigations[0].success();
    instance.updateCurrentByRoute();
    assert.equal(instance.data.current, 1, '路由成为目标页后再同步选中态');
  });
}

function loadDistribution() {
  let definition;
  const requests = [];
  const toasts = [];
  vm.runInNewContext(read('miniprogram/pages/distribution/distribution.js'), {
    exports: {},
    Page: value => { definition = value; },
    wx: {
      getSystemInfoSync: () => ({ statusBarHeight: 20, windowWidth: 375 }),
      getMenuButtonBoundingClientRect: () => ({ top: 24, bottom: 56, height: 32, left: 280 }),
      request: options => requests.push(options),
      showToast: options => toasts.push(options.title),
      navigateTo() {},
    },
    console,
  });
  const instance = {
    ...definition,
    data: { ...definition.data },
    setData(values) { Object.assign(this.data, values); },
  };
  return { instance, requests, toasts };
}

test('设计师分类切换保留旧列表，失败时恢复原分类', async () => {
  const { instance, requests, toasts } = loadDistribution();
  const oldCards = [{ designerUserId: 7, designerName: '原设计师' }];
  instance.data.designerCards = oldCards;
  instance.data.contentReady = true;
  const pending = instance.onMainTabTap({ currentTarget: { dataset: { index: 1 } } });

  assert.equal(instance.data.designerCards, oldCards, '请求期间保留已经渲染的卡片');
  assert.equal(instance.data.loading, true);
  requests[0].fail(new Error('offline'));
  await pending;

  assert.equal(instance.data.currentTabIndex, 0, '失败后标签与旧列表保持一致');
  assert.equal(instance.data.designerCards, oldCards);
  assert.deepEqual(toasts, ['设计师列表加载失败，请稍后重试']);
});

test('设计师分类切换保留旧结果时明确显示刷新状态', () => {
  const markup = read('miniprogram/pages/distribution/distribution.wxml');
  assert.match(markup, /wx:if="\{\{loading && designerCards\.length > 0\}\}"[^>]*>正在切换，暂时保留当前结果…<\/view>/);
});

test('设计师搜索和特征筛选失败时保留列表并恢复筛选语义', async () => {
  const { instance, requests } = loadDistribution();
  const oldCards = [{ designerUserId: 7, designerName: '原设计师' }];
  instance.data.designerCards = oldCards;
  instance.data.contentReady = true;
  instance.data.searchInputValue = '庭院';
  const searchPending = instance.onSearchConfirm();
  assert.equal(instance.data.designerCards, oldCards);
  requests[0].fail(new Error('offline'));
  await searchPending;
  assert.equal(instance.data.searchKeyword, '');
  assert.equal(instance.data.designerCards, oldCards);

  const traitPending = instance.onTraitFilterTap({ currentTarget: { dataset: { index: 1 } } });
  assert.equal(instance.data.designerCards, oldCards);
  requests[1].fail(new Error('offline'));
  await traitPending;
  assert.equal(instance.data.selectedTrait, '全部');
});

test('设计师请求未结束时忽略新的分类点击', async () => {
  const { instance, requests } = loadDistribution();
  instance.data.designerCards = [{ designerUserId: 7 }];
  instance.data.contentReady = true;
  const first = instance.onMainTabTap({ currentTarget: { dataset: { index: 1 } } });
  await instance.onMainTabTap({ currentTarget: { dataset: { index: 2 } } });
  assert.equal(requests.length, 1);
  assert.equal(instance.data.currentTabIndex, 1);
  requests[0].success({ statusCode: 200, data: { code: 0, data: { list: [], total: 0 } } });
  await first;
});

const wxmlCompiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
test('原生WXML渲染始终保留四个品牌图片图层且仅激活当前图', { skip: !fs.existsSync(wxmlCompiler) }, () => {
  const result = spawnSync(wxmlCompiler, ['./custom-tab-bar/index.wxml'], {
    cwd: path.join(root, 'miniprogram'),
    encoding: 'utf8',
    timeout: 10000,
    maxBuffer: 4 * 1024 * 1024,
  });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const context = { window: {}, console };
  vm.createContext(context);
  vm.runInContext(result.stdout, context);
  const render = context.$gwx('./custom-tab-bar/index.wxml');
  const sources = ['HOME', 'TEMPLATE', 'DESIGNER', 'MY'];
  for (let current = 0; current < 4; current++) {
    const tree = render({
      current,
      homeTabbarImage: sources[0],
      templateTabbarImage: sources[1],
      designerTabbarImage: sources[2],
      myTabbarImage: sources[3],
      tabs: [],
    });
    const images = tree.children[0].children[0].children.filter(node => node.tag === 'wx-image');
    assert.deepEqual(Array.from(images, node => node.attr.src), sources, '切换时图片节点及地址保持不变');
    const activeImages = images.filter(node => String(node.attr.class).includes('tabbar-background--active'));
    assert.equal(activeImages.length, 1);
    assert.equal(activeImages[0].attr.src, sources[current]);
  }
  for (const file of ['index.scss', 'index.wxss']) {
    const styles = read(`miniprogram/custom-tab-bar/${file}`);
    assert.match(styles, /\.tabbar-background\s*\{[^}]*position:\s*absolute;[^}]*opacity:\s*0;/s);
    assert.match(styles, /\.tabbar-background--active\s*\{[^}]*opacity:\s*1;/s);
  }
});

function loadTemplate() {
  let definition;
  const imageRequests = [];
  vm.runInNewContext(read('miniprogram/pages/template/template.js'), {
    exports: {},
    require: name => name.endsWith('/asset')
      ? { normalizeCosUrl: value => value, resolveAssetPath: value => value }
      : { getPageCache: () => null, prefetchImages: async () => {}, setPageCache() {} },
    Page: value => { definition = value; },
    wx: {
      getSystemInfoSync: () => ({ statusBarHeight: 20, windowWidth: 375 }),
      getMenuButtonBoundingClientRect: () => ({ top: 24, bottom: 56, height: 32, left: 280 }),
      getImageInfo: options => imageRequests.push(options),
      request() {},
      navigateTo() {},
      showToast() {},
    },
    console,
  });
  const instance = {
    ...definition,
    data: { ...definition.data },
    templateFirstPageCache: {},
    requestSerial: 0,
    imageSizeCache: {},
    imageSizeLoadingKeys: new Set(),
    setData(values) { Object.assign(this.data, values); },
  };
  return { instance, imageRequests };
}

test('模板首屏从加载态开始，不短暂显示空结果', () => {
  const { instance } = loadTemplate();
  assert.equal(instance.data.loading, true);
});

test('模板请求期间忽略分类和搜索提交，旧响应不会覆盖未请求标签', async () => {
  const { instance } = loadTemplate();
  instance.data.loading = true;
  instance.data.initialLoadPending = false;
  instance.data.mainTabs = [{ label: '场景', value: 'scene' }, { label: '风格', value: 'style' }];
  instance.data.allSubTabs = [
    { label: '乡墅', value: 'villa', parent: 'scene' },
    { label: '现代', value: 'modern', parent: 'style' },
  ];
  instance.data.subTabs = [instance.data.allSubTabs[0]];
  instance.data.currentMainTabIndex = 0;
  instance.data.currentSubTabIndex = 0;
  instance.data.searchInputValue = '庭院';

  await instance.onMainTabTap({ currentTarget: { dataset: { index: 1 } } });
  await instance.onSubTabTap({ currentTarget: { dataset: { index: 0 } } });
  instance.onSearchConfirm();

  assert.equal(instance.data.currentMainTabIndex, 0);
  assert.equal(instance.data.currentSubTabIndex, 0);
  assert.equal(instance.data.searchKeyword, '');
});

test('模板图片尺寸陆续返回时不跨列移动卡片', () => {
  const { instance, imageRequests } = loadTemplate();
  const cards = [1, 2, 3, 4].map(id => ({
    id,
    cardKey: `template-${id}`,
    image: `https://cdn.example/${id}.jpg`,
    displayTitle: `模板${id}`,
    tags: [],
  }));
  instance.syncWaterfallCards(cards);
  const beforeLeft = instance.data.leftColumnCards.map(item => item.cardKey);
  const beforeRight = instance.data.rightColumnCards.map(item => item.cardKey);

  const dimensions = [[100, 500], [500, 100], [100, 500], [500, 100]];
  imageRequests.forEach((request, index) => {
    request.success({ width: dimensions[index][0], height: dimensions[index][1] });
    request.complete();
  });

  assert.deepEqual(instance.data.leftColumnCards.map(item => item.cardKey), beforeLeft);
  assert.deepEqual(instance.data.rightColumnCards.map(item => item.cardKey), beforeRight);
});
