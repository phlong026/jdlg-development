const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const pagePath = path.join(__dirname, '../miniprogram/pages/aicostresult/aicostresult.ts');
const shareCardPath = path.join(__dirname, '../miniprogram/utils/costEstimateShareImage.ts');

function createHarness(cachedResult) {
  let definition;
  const toasts = [];
  let navigatedBack = 0;
  const code = ts.transpileModule(fs.readFileSync(pagePath, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;

  vm.runInNewContext(code, {
    exports: {},
    Page(value) { definition = value; },
    require() {
      return {
        DEFAULT_CUSTOMER_SERVICE_CORP_ID: 'corp',
        DEFAULT_CUSTOMER_SERVICE_URL: 'service-url',
        resolveEnterpriseWechatServiceConfig: value => value,
        tryOpenCustomerServiceDirect: async () => true,
        prepareCostEstimateShareImage: async () => 'wxfile://cost-share.jpg',
      };
    },
    setTimeout(callback) { callback(); },
    console: { ...console, error() {} },
    wx: {
      getStorageSync(key) {
        if (key === 'aicostEstimateResult') {
          if (cachedResult instanceof Error) throw cachedResult;
          return cachedResult;
        }
        return '';
      },
      showToast(options) { toasts.push(options.title); },
      navigateBack() { navigatedBack += 1; },
      navigateTo() {},
    },
  });

  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(patch) { Object.assign(this.data, patch); },
    loadEnterpriseWechatServiceConfig() {},
  };
  return { page, toasts, get navigatedBack() { return navigatedBack; } };
}

const currentResult = {
  version: 4,
  estimatedTotalCost: '58.36万元',
  estimatedUnitCost: '1823.75元/㎡',
  totalLabel: '参考总造价',
  unitLabel: '参考单价',
  basicInfo: [
    { label: '所在地区', value: '福建 / 泉州', fullWidth: true },
    { label: '建筑占地面积', value: '120 ㎡' },
    { label: '总建筑面积', value: '320 ㎡' },
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
    { label: '门窗标准', value: '常规' },
  ],
  costBreakdown: [
    { name: '地基工程', amount: 78000, amountText: '7.80万元', percent: 13, note: '土方、垫层', estimable: true },
    { name: '主体工程', amount: 235200, amountText: '23.52万元', percent: 40, note: '梁板柱、砌体', estimable: true },
    { name: '屋顶工程', amount: 59200, amountText: '5.92万元', percent: 10, note: '防水、保温', estimable: true },
    { name: '门窗工程', amount: 60800, amountText: '6.08万元', percent: 10, note: '断桥铝合金窗', estimable: true },
    { name: '外墙装饰', amount: 137600, amountText: '13.76万元', percent: 24, note: '粉刷、真石漆', estimable: true },
    { name: '水电暖费用', amount: 12800, amountText: '1.28万元', percent: 2, note: '水电管线预埋', estimable: true },
  ],
  dynamicNotes: ['不同地基因方案不同，价格不同'],
};

test('report loads the complete version 2 estimate snapshot', () => {
  const h = createHarness(currentResult);
  h.page.onLoad();
  assert.equal(h.page.data.estimatedTotalCost, '58.36万元');
  assert.equal(h.page.data.totalLabel, '参考总造价');
  assert.equal(h.page.data.unitLabel, '参考单价');
  assert.equal(h.page.data.basicInfo.length, 14);
  assert.equal(h.page.data.costBreakdown.length, 6);
  assert.equal(h.toasts.length, 0);
});

test('report share keeps the dynamic title and path when its cover finishes asynchronously', async () => {
  const h = createHarness(currentResult);
  h.page.onLoad();
  const share = h.page.onShareAppMessage();
  assert.equal(share.title, '泉州320 ㎡建房参考造价58.36万，查看测算报告');
  assert.equal(share.path, '/pages/aicost/aicost?source=cost-share');
  const resolvedShare = await share.promise;
  assert.equal(resolvedShare.title, '泉州320 ㎡建房参考造价58.36万，查看测算报告');
  assert.equal(resolvedShare.path, '/pages/aicost/aicost?source=cost-share');
  assert.equal(resolvedShare.imageUrl, 'wxfile://cost-share.jpg');
});

test('share cover uses the supplied villa visual without a line-art fallback', () => {
  const source = fs.readFileSync(shareCardPath, 'utf8');
  assert.match(source, /cost-estimate-share-house\.jpg/);
  assert.match(source, /drawCardHouseImage/);
  assert.doesNotMatch(source, /drawBuildingOutline/);
  assert.match(source, /建房造价测算/);
  assert.match(source, /点击查看测算报告/);
  assert.doesNotMatch(source, /点击查看完整测算报告/);
});

test('a partially estimated report labels both total and unit price clearly', () => {
  const partialResult = {
    ...currentResult,
    totalLabel: '已测算部分合计',
    unitLabel: '已测算部分单价',
    costBreakdown: currentResult.costBreakdown.map((item, index) => index === 0
      ? { ...item, amount: 0, amountText: '按图纸核算', percent: 0, estimable: false }
      : item),
  };
  const h = createHarness(partialResult);
  h.page.onLoad();
  assert.equal(h.page.data.totalLabel, '已测算部分合计');
  assert.equal(h.page.data.unitLabel, '已测算部分单价');
  assert.equal(h.page.data.costBreakdown[0].amountText, '按图纸核算');
});

test('report rejects a version 2 cache with missing rows or malformed items', () => {
  const damagedCaches = [
    { ...currentResult, basicInfo: currentResult.basicInfo.slice(0, 13) },
    { ...currentResult, costBreakdown: currentResult.costBreakdown.map((item, index) => index === 0 ? { ...item, estimable: 'yes' } : item) },
  ];
  for (const cached of damagedCaches) {
    const h = createHarness(cached);
    h.page.onLoad();
    assert.equal(h.toasts[0], '测算数据已更新，请重新测算');
    assert.equal(h.navigatedBack, 1);
  }
});

test('report rejects old or unreadable cache and asks for a fresh estimate', () => {
  for (const cached of [{ estimatedTotalCost: '88.5万元' }, new Error('storage unavailable')]) {
    const h = createHarness(cached);
    h.page.onLoad();
    assert.equal(h.toasts[0], '测算数据已更新，请重新测算');
    assert.equal(h.navigatedBack, 1);
  }
});

test('cost details toggle without changing the estimate snapshot', () => {
  const h = createHarness(currentResult);
  h.page.onLoad();
  h.page.onViewCostDetails();
  assert.equal(h.page.data.showCostDetails, true);
  assert.equal(h.page.data.costBreakdown[0].name, '地基工程');
  h.page.onViewCostDetails();
  assert.equal(h.page.data.showCostDetails, false);
});

test('report template keeps the warm two-card layout and designer conversion entry', () => {
  const wxml = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicostresult/aicostresult.wxml'), 'utf8');
  const scss = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicostresult/aicostresult.scss'), 'utf8');
  const js = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicostresult/aicostresult.js'), 'utf8');
  assert.match(wxml, /navigation-bar title="造价测算报告"/);
  assert.doesNotMatch(wxml, /class="report-title"/);
  assert.match(wxml, /basicInfo/);
  assert.match(wxml, /联系设计师优化/);
  assert.match(wxml, /costBreakdown/);
  assert.doesNotMatch(wxml, /cost-donut|donut-legend/);
  assert.match(scss, /#e6daca/);
  assert.match(js, /RESULT_SCHEMA_VERSION\s*=\s*4/);
  assert.match(js, /timeout:\s*8000/);
});

test('report accepts basement as a seventh cost item and rejects previous pricing', () => {
  const h = createHarness({ ...currentResult, costBreakdown: [...currentResult.costBreakdown, { name: '地下室工程', amount: 183600, amountText: '18.36万元', percent: 26, note: '不同地下室因方案不同，价格不同。', estimable: true }] });
  h.page.onLoad();
  assert.equal(h.page.data.costBreakdown.length, 7);
  const old = createHarness({ ...currentResult, version: 2 });
  old.page.onLoad();
  assert.equal(old.navigatedBack, 1);
});

test('report rejects the previous window price version', () => {
  const h = createHarness({ ...currentResult, version: 3 });
  h.page.onLoad();
  assert.equal(h.navigatedBack, 1);
});
