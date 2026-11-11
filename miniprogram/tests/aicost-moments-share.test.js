const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
const costRegions = require('../miniprogram/utils/costRegions');

const formPath = path.join(__dirname, '../miniprogram/pages/aicost/aicost.ts');
const resultPath = path.join(__dirname, '../miniprogram/pages/aicostresult/aicostresult.ts');

const currentResult = {
  version: 4,
  estimatedTotalCost: '58.36万元',
  estimatedUnitCost: '1823.75元/㎡',
  totalLabel: '参考总造价',
  unitLabel: '参考单价',
  basicInfo: [
    { label: '所在地区', value: '福建 / 泉州', fullWidth: true },
    { label: '建筑占地面积', value: '120㎡' },
    { label: '总建筑面积', value: '320㎡' },
    { label: '楼层数', value: '3层' },
    { label: '首层高度', value: '3.6m' },
    { label: '其他楼层高度', value: '3.3m' },
    { label: '地下室形式', value: '无地下室' },
    { label: '建筑风格', value: '新闽派' },
    { label: '结构形式', value: '框架结构' },
    { label: '屋顶形式', value: '坡屋顶' },
    { label: '外立面样式', value: '真石漆' },
    { label: '建筑基础', value: '独立基础' },
    { label: '装修标准', value: '标准型' },
    { label: '门窗标准', value: '标准型' },
  ],
  costBreakdown: [
    { name: '地基工程', amount: 78000, amountText: '7.80万元', percent: 13, note: '土方、垫层', estimable: true },
    { name: '主体工程', amount: 235200, amountText: '23.52万元', percent: 40, note: '梁板柱、砌体', estimable: true },
    { name: '屋顶工程', amount: 59200, amountText: '5.92万元', percent: 10, note: '防水、保温', estimable: true },
    { name: '门窗工程', amount: 60800, amountText: '6.08万元', percent: 10, note: '断桥铝合金窗', estimable: true },
    { name: '外墙装饰', amount: 137600, amountText: '13.76万元', percent: 24, note: '粉刷、真石漆', estimable: true },
    { name: '水电及其它费用', amount: 12800, amountText: '1.28万元', percent: 2, note: '水电管线预埋', estimable: true },
  ],
  dynamicNotes: ['不同地基因方案不同，价格不同。'],
};

function transpile(file) {
  return ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
}

test('budget form prepares a local square cover and exposes the same Moments landing page', async () => {
  let definition;
  vm.runInNewContext(transpile(formPath), {
    exports: {},
    Page(value) { definition = value; },
    require(request) {
      if (request === '../../utils/costRegions') return costRegions;
      if (request === '../../utils/asset') return { resolveAssetPath: value => `https://cdn.test${value}` };
      if (request === '../../utils/cloudImage') return {
        prepareCloudImage: async (url, fileName) => {
          assert.equal(url, 'https://cdn.test/assets/share/budget-timeline-v2.jpg');
          assert.equal(fileName, 'budget-timeline-v2.jpg');
          return 'wxfile://user-data/cloud-images/budget-timeline-v2.jpg';
        },
      };
      return { prepareCostEstimateShareImage: async () => 'wxfile://chat.jpg' };
    },
    console: { warn() {} },
    wx: {
    },
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) { Object.assign(this.data, patch); },
  };
  page.onLoad({});
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(page.data.contentReady, true);
  const timeline = page.onShareTimeline();
  assert.equal(timeline.title, '10秒估算建房报价\n建别墅从这里开始');
  assert.equal(timeline.query, 'from=share');
  assert.equal(timeline.imageUrl, 'wxfile://user-data/cloud-images/budget-timeline-v2.jpg');
});

function createResultHarness({ cached = currentResult, options = {}, token = 'token', userID = 17, publishError = null, redirectError = false } = {}) {
  let definition, currentToken = token;
  const calls = { create: [], navigation: [], redirects: [], toasts: [], profileChecks: [] };
  const api = {
    createCostEstimateShare: async (result) => {
      calls.create.push(result);
      if (publishError) throw publishError;
      return { report_id: 'AbcdEFghIJklMNopQRstUVwxYZ012345', expires_at: '2026-11-20T00:00:00Z' };
    },
    isCostEstimateShareResult: value => Boolean(value && value.version === 4 && value.basicInfo?.length === 14 && value.costBreakdown?.length >= 6),
    sanitizeCostEstimateShareResult: value => JSON.parse(JSON.stringify(value, (key, item) => key === 'color' ? undefined : item)),
  };
  vm.runInNewContext(transpile(resultPath), {
    exports: {}, Page(value) { definition = value; },
    require(request) {
      if (request === '../../utils/enterpriseWechat') return {
        DEFAULT_CUSTOMER_SERVICE_CORP_ID: 'corp', DEFAULT_CUSTOMER_SERVICE_URL: 'service',
        resolveEnterpriseWechatServiceConfig: value => value, tryOpenCustomerServiceDirect: async () => true,
      };
      if (request === '../../utils/costEstimateShare') return api;
      throw new Error(`unexpected require ${request}`);
    },
    console: { error() {}, warn() {} }, setTimeout(callback) { callback(); },
    wx: {
      getStorageSync(key) {
        if (key === `aicostEstimateResult:${userID}`) return cached;
        if (key === 'token') return currentToken;
        if (key === 'userInfo') return userID ? { id: userID } : {};
        return '';
      },
      showToast(value) { calls.toasts.push(value.title); }, navigateBack() {},
      navigateTo(value) { calls.navigation.push(value.url); },
      redirectTo(value) { calls.redirects.push(value.url); if (redirectError) value.fail(); },
    },
  });
  const page = { ...definition, data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) { Object.assign(this.data, patch); }, loadEnterpriseWechatServiceConfig() {},
    selectComponent() { return { check: options => { calls.profileChecks.push(options); return Promise.resolve(false); } }; } };
  page.onLoad(options);
  return { page, calls, setToken(value) { currentToken = value; } };
}

test('private result lifecycle never publishes automatically, including login return', async () => {
  const h = createResultHarness({ token: '' });
  h.page.onReady(); h.page.onShow(); h.setToken('signed-in'); h.page.onShow();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(h.calls.create.length, 0);
  assert.equal(Object.keys(h.page.onShareAppMessage()).length, 0);
  assert.equal(Object.keys(h.page.onShareTimeline()).length, 0);
});

test('explicit publishing creates one immutable snapshot and opens its public landing', async () => {
  const h = createResultHarness();
  await h.page.onPrepareShareReport();
  assert.equal(h.calls.create.length, 1);
  assert.equal(h.page.data.contentReady, false, 'private source must remain excluded');
  assert.equal(h.calls.navigation[0], '/pages/aicostshare/aicostshare?report_id=AbcdEFghIJklMNopQRstUVwxYZ012345');
  await h.page.onPrepareShareReport();
  assert.equal(h.calls.create.length, 1, 'reopening reuses the published snapshot');
  assert.equal(h.calls.navigation.length, 2);
  assert.equal(Object.keys(h.page.onShareTimeline()).length, 0);
});

test('anonymous users cannot read or publish a private local result', async () => {
  const h = createResultHarness({ token: '' });
  h.page.onReady(); h.page.onShow();
  assert.deepEqual(h.calls.toasts, ['测算数据已更新，请重新测算']);
  await h.page.onPrepareShareReport();
  assert.equal(h.calls.create.length, 0);
  assert.equal(h.page.reportSnapshot, null);
});

test('publish failure remains private and is retryable', async () => {
  const h = createResultHarness({ publishError: new Error('网络连接失败') });
  await h.page.onPrepareShareReport();
  assert.equal(h.page.data.sharePreparing, false);
  assert.equal(h.page.data.shareReportId, '');
  assert.equal(h.calls.navigation.length, 0);
  assert.deepEqual(h.calls.toasts, ['网络连接失败']);
});

test('profile-required publish opens a soft full-profile editor without hiding the personal result', async () => {
  const error = Object.assign(new Error('请先补充头像和昵称'), { code: 428, statusCode: 428 });
  const h = createResultHarness({ publishError: error });
  const snapshot = h.page.data.estimatedTotalCost;
  await h.page.onPrepareShareReport();
  assert.equal(h.calls.profileChecks.length, 1);
  assert.equal(h.calls.profileChecks[0].soft, true);
  assert.equal(h.page.data.estimatedTotalCost, snapshot);
  assert.equal(h.page.data.sharePreparing, false);
  assert.equal(h.calls.navigation.length, 0);
});

test('old public report links redirect without reading private cache and preserve st', () => {
  const reportId = 'abcDEF0123456789abcDEF0123456789';
  const h = createResultHarness({ cached: { corrupted: true }, token: '', options: { report_id: reportId, st: 'upstream-token' } });
  assert.equal(h.calls.redirects[0], `/pages/aicostshare/aicostshare?report_id=${reportId}&st=upstream-token`);
  assert.equal(h.page.data.estimatedTotalCost, '');
  assert.equal(h.calls.create.length, 0);
});

test('broken legacy public links cannot fall back to a visitors local estimate', () => {
  const h = createResultHarness({ options: { report_id: '' }, redirectError: true });
  assert.equal(h.calls.redirects[0], '/pages/aicostshare/aicostshare?report_id=');
  assert.equal(h.page.data.estimatedTotalCost, '');
  assert.match(h.page.data.reportError, /公开报告页面暂时无法打开/);
  h.page.onRetrySharedReport(); assert.equal(h.calls.redirects.length, 2);
});
