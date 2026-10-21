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
      return { prepareCostEstimateShareImage: async () => 'wxfile://chat.jpg' };
    },
    console: { warn() {} },
    wx: {
      env: { USER_DATA_PATH: 'wxfile://user-data' },
      getFileSystemManager() {
        return { copyFile({ destPath, success }) { assert.equal(destPath, 'wxfile://user-data/budget-timeline-v2.jpg'); success(); } };
      },
    },
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) { Object.assign(this.data, patch); },
  };
  page.onLoad({});
  assert.equal(page.data.contentReady, true);
  const timeline = page.onShareTimeline();
  assert.equal(timeline.title, '10秒估算建房报价，建别墅从这里开始');
  assert.equal(timeline.query, 'from=share');
  assert.equal(timeline.imageUrl, 'wxfile://user-data/budget-timeline-v2.jpg');
});

function createResultHarness({ cached = currentResult, reportId, sharedResult = currentResult, sharedError = null, token = 'token', chatCover = 'wxfile://chat.jpg', timelineCover = 'wxfile://timeline.jpg' } = {}) {
  let definition;
  const calls = { create: [], get: [], login: [] };
  const api = {
    createCostEstimateShare: async (result) => {
      calls.create.push(result);
      return { report_id: 'AbcdEFghIJklMNopQRstUVwxYZ012345', expires_at: '2026-11-20T00:00:00Z' };
    },
    getCostEstimateShare: async (id) => {
      calls.get.push(id);
      if (sharedError) throw sharedError;
      return { report_id: id, expires_at: '2026-11-20T00:00:00Z', result: sharedResult };
    },
  };
  vm.runInNewContext(transpile(resultPath), {
    exports: {},
    Page(value) { definition = value; },
    require(request) {
      if (request === '../../utils/enterpriseWechat') return {
        DEFAULT_CUSTOMER_SERVICE_CORP_ID: 'corp', DEFAULT_CUSTOMER_SERVICE_URL: 'service',
        resolveEnterpriseWechatServiceConfig: value => value, tryOpenCustomerServiceDirect: async () => true,
      };
      if (request === '../../utils/costEstimateShareImage') return {
        prepareCostEstimateShareImage: async () => chatCover,
        prepareCostEstimateTimelineImage: async () => timelineCover,
      };
      if (request === '../../utils/costEstimateShare') return api;
      if (request === '../../utils/reportNavigation') return {
        requireReportLogin(pathname) { calls.login.push(pathname); return Boolean(token); },
      };
      throw new Error(`unexpected require ${request}`);
    },
    console: { ...console, error() {}, warn() {} },
    setTimeout(callback) { callback(); },
    wx: {
      getStorageSync(key) { return key === 'aicostEstimateResult' ? cached : key === 'token' ? token : ''; },
      showToast() {}, navigateBack() {}, navigateTo() {},
    },
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) { Object.assign(this.data, patch); },
    loadEnterpriseWechatServiceConfig() {},
  };
  api.isCostEstimateShareResult = value => Boolean(value && value.version === 4 && value.basicInfo?.length === 14 && value.costBreakdown?.length >= 6);
  api.sanitizeCostEstimateShareResult = value => JSON.parse(JSON.stringify(value, (key, item) => key === 'color' ? undefined : item));
  page.onLoad(reportId === undefined ? {} : { report_id: reportId });
  return { page, calls };
}

test('preparing a local report publishes once, then both share channels point to that exact report', async () => {
  const h = createResultHarness();
  await h.page.onPrepareShareReport();
  assert.equal(h.calls.create.length, 1);
  assert.equal(h.page.data.shareReady, true);
  assert.equal(h.page.data.contentReady, true);
  const expectedTitle = '泉州320㎡建房参考造价58.36万，查看测算报告';
  const timeline = h.page.onShareTimeline();
  assert.equal(timeline.title, expectedTitle);
  assert.equal(timeline.query, 'report_id=AbcdEFghIJklMNopQRstUVwxYZ012345');
  assert.equal(timeline.imageUrl, 'wxfile://timeline.jpg');
  const friend = h.page.onShareAppMessage();
  assert.equal(friend.title, expectedTitle);
  assert.equal(friend.path, '/pages/aicostresult/aicostresult?report_id=AbcdEFghIJklMNopQRstUVwxYZ012345');
  assert.equal(friend.imageUrl, 'wxfile://chat.jpg');
});

test('opening a shared report fetches only that immutable snapshot and reshares the same id', async () => {
  const h = createResultHarness({ cached: { corrupted: true }, reportId: 'abcDEF0123456789abcDEF0123456789' });
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(h.calls.get, ['abcDEF0123456789abcDEF0123456789']);
  assert.equal(h.page.data.estimatedTotalCost, '58.36万元');
  await h.page.onPrepareShareReport();
  assert.equal(h.calls.create.length, 0, 'resharing must not create a different report');
  assert.equal(h.page.onShareTimeline().query, 'report_id=abcDEF0123456789abcDEF0123456789');
});

test('share preparation sends an unauthenticated owner through the existing login return flow', async () => {
  const h = createResultHarness({ token: '' });
  await h.page.onPrepareShareReport();
  assert.deepEqual(h.calls.login, ['/pages/aicostresult/aicostresult']);
  assert.equal(h.calls.create.length, 0);
  assert.equal(h.page.data.shareReady, false);
});

test('a failed shared-report fetch stays in an error state and never falls back to local cache', async () => {
  const h = createResultHarness({ reportId: 'badBAD0123456789badBAD0123456789', cached: currentResult, sharedError: new Error('分享报告已失效或不存在') });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(h.page.data.reportError, '分享报告已失效或不存在');
  assert.equal(h.page.data.estimatedTotalCost, '', 'visitor cache must not replace the requested report');
});

test('a blank report id is treated as a broken shared link instead of opening local data', async () => {
  const h = createResultHarness({ reportId: '', cached: currentResult, sharedError: new Error('无效的分享报告链接') });
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(h.calls.get, ['']);
  assert.equal(h.page.data.reportError, '无效的分享报告链接');
  assert.equal(h.page.data.estimatedTotalCost, '');
  h.page.onRetrySharedReport();
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(h.calls.get, ['', '']);
});

test('report sharing stays closed if either prepared cover is unavailable', async () => {
  const h = createResultHarness({ chatCover: '' });
  await h.page.onPrepareShareReport();
  assert.equal(h.page.data.shareReady, false);
  assert.equal(h.page.data.contentReady, false);
  assert.equal(h.page.data.shareReportId, 'AbcdEFghIJklMNopQRstUVwxYZ012345', 'retry keeps the already-published immutable report');
});
