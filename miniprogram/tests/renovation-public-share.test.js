'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const pageBase = path.join(__dirname, '../miniprogram/pages/aicostshare/aicostshare');
const reportId = 'RenoEFghIJklMNopQRstUVwxYZ012345';
const renovation = {
  version: 5, quoteType: 'renovation', estimatedTotalCost: '伪造总价', estimatedUnitCost: '1004.44元/㎡', totalLabel: '装修参考总价', unitLabel: '参考单价',
  basicInfo: [{ label: '所在地区', value: '福建 / 泉州', fullWidth: true }, { label: '装修面积', value: '90㎡' }],
  costBreakdown: [
    { name: '主材费', amount: 42200, amountText: '伪造主材' }, { name: '辅材费', amount: 10900, amountText: '伪造辅材' }, { name: '人工费', amount: 26300, amountText: '伪造人工' },
    { name: '管理费', amount: 7900, amountText: '0.79万元' }, { name: '设计费', amount: 3200, amountText: '0.32万元' }, { name: '税费', amount: 0, amountText: '0元' },
  ], dynamicNotes: [], renovation: { totalAmount: 90400, spaces: [] },
};
const building = {
  version: 4, estimatedTotalCost: '58.36万元', estimatedUnitCost: '1823.75元/㎡', totalLabel: '参考总造价', unitLabel: '参考单价',
  basicInfo: [{ label: '所在地区', value: '福建 / 泉州' }, { label: '总建筑面积', value: '320㎡' }, { label: '结构形式', value: '框架结构' }, { label: '门窗标准', value: '标准型' }],
  costBreakdown: [], dynamicNotes: [],
};
const settle = async () => { for (let i = 0; i < 5; i += 1) await new Promise(resolve => setImmediate(resolve)); };

function load(extension, result) {
  let definition;
  const calls = { building: [], renovation: [], navigation: [] };
  let source = fs.readFileSync(`${pageBase}.${extension}`, 'utf8');
  if (extension === 'ts') source = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
  const dependencies = {
    '../../utils/costEstimateShare': { getCostEstimateShare: async () => ({ report_id: reportId, expires_at: '2026-12-01T00:00:00Z', result }) },
    '../../utils/costEstimateShareImage': {
      prepareCostEstimateShareImage: async card => { calls.building.push(card); return 'wxfile://building-chat.jpg'; },
      prepareCostEstimateTimelineImage: async () => 'wxfile://building-timeline.jpg',
    },
    '../../utils/renovationCostShareImage': {
      prepareRenovationShareImage: async card => { calls.renovation.push(card); return 'wxfile://renovation-chat.jpg'; },
      prepareRenovationTimelineImage: async () => 'wxfile://renovation-timeline.jpg',
    },
  };
  vm.runInNewContext(source, { exports: {}, Page: value => { definition = value; }, require: request => dependencies[request],
    wx: { navigateTo: ({ url }) => calls.navigation.push(url) }, console, Promise, Math, Date, setTimeout, clearTimeout });
  const page = { ...definition, data: JSON.parse(JSON.stringify(definition.data)), setData(value) { Object.assign(this.data, value); } };
  return { page, calls };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: v5 public snapshot renders as readonly renovation report and shares renovation facts`, async () => {
    const h = load(extension, renovation);
    h.page.onLoad({ report_id: reportId });
    await settle();
    h.page.onReady();
    await settle();
    assert.equal(h.page.data.isRenovation, true);
    assert.equal(h.page.data.pageTitle, '公开装修报价');
    assert.equal(h.page.data.startLabel, '测算我的装修报价');
    assert.equal(h.page.data.shareTitle, '泉州90㎡装修参考总价9.04万元，查看装修报价明细');
    assert.equal(h.calls.building.length, 0, 'renovation must not use the villa cover');
    assert.deepEqual(JSON.parse(JSON.stringify(h.calls.renovation[0])), {
      city: '泉州', area: '90㎡', totalCost: '9.04万元', materialCost: '42,200元', auxiliaryCost: '10,900元', laborCost: '26,300元',
    });
    assert.equal(h.page.onShareAppMessage().imageUrl, 'wxfile://renovation-chat.jpg');
    assert.equal(h.page.onShareTimeline().imageUrl, 'wxfile://renovation-timeline.jpg');
    h.page.startOwnEstimate();
    assert.equal(h.calls.navigation[0], '/pages/renovationbudget/index');
  });

  test(`${extension}: v4 public snapshot retains its existing title, cover and destination`, async () => {
    const h = load(extension, building);
    h.page.onLoad({ report_id: reportId });
    await settle();
    h.page.onReady();
    await settle();
    assert.equal(h.page.data.isRenovation, false);
    assert.equal(h.page.data.pageTitle, '公开造价报告');
    assert.equal(h.page.data.shareTitle, '泉州320㎡参考总造价58.36万元，查看公开测算报告');
    assert.equal(h.calls.building.length, 1);
    assert.equal(h.calls.renovation.length, 0);
    h.page.startOwnEstimate();
    assert.equal(h.calls.navigation[0], '/pages/aicost/aicost');
  });
}

test('public page binds v5 to the readonly renovation component while retaining the v4 markup branch', () => {
  const wxml = fs.readFileSync(`${pageBase}.wxml`, 'utf8');
  const config = JSON.parse(fs.readFileSync(`${pageBase}.json`, 'utf8'));
  assert.match(wxml, /<renovation-report\s+report="\{\{result\}\}"\s+readonly="\{\{true\}\}"\s*\/>/);
  assert.match(wxml, /wx:if="\{\{isRenovation\}\}"/);
  assert.match(wxml, /wx:else/);
  assert.equal(config.usingComponents['renovation-report'], '/components/renovation-report/index');
});
