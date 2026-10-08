const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicost/aicost.js'), 'utf8');
const costRegions = require('../miniprogram/utils/costRegions');
function form(overrides = {}, storageFails = false) {
  let config, saved, toast, route;
  vm.runInNewContext(source, { exports: {}, require: request => request === '../../utils/costRegions'
    ? costRegions
    : { prepareCostEstimateShareImage: async () => '' }, Page: p => { config = p; }, wx: {
    showToast: p => { toast = p.title; },
    setStorageSync: (key, value) => { if (storageFails) throw new Error('storage full'); saved = value; },
    navigateTo: p => { route = p.url; },
  }, console: { error() {} } });
  const page = { ...config, data: { ...config.data, footprintArea: '120', buildingArea: '320', ...overrides },
    setData(values) { Object.assign(this.data, values); } };
  page.onEstimateCost();
  return { page, saved, toast, route };
}
const normal = form();
assert.equal(normal.saved.version, 4);
assert.equal(normal.saved.estimatedTotalCost, '53.68万元');
assert.equal(normal.saved.estimatedUnitCost, '1677.38元/㎡');
assert.deepEqual(Array.from(normal.saved.costBreakdown, i => i.amount), [70200, 211680, 53280, 48960, 123840, 28800]);
assert.equal(normal.saved.basicInfo.length, 14);
assert.equal(normal.route, '/pages/aicostresult/aicostresult');
normal.page.data.buildingArea = '999';
assert.equal(normal.saved.basicInfo.find(i => i.label === '总建筑面积').value, '320㎡');
assert.equal(form({ foundationType: '条形基础' }).saved.costBreakdown[0].amount, 97200);
assert.equal(form({ foundationType: '筏板基础' }).saved.costBreakdown[0].amount, 129600);
assert.equal(form({ roofType: '燕尾脊' }).saved.costBreakdown[2].amount, 58354.2);
for (const [structureType, price] of [['砖混结构',685], ['框架结构',735], ['钢结构',830]]) {
  assert.equal(form({ structureType }).saved.costBreakdown[1].amount, 320 * price * 0.9);
}
for (const [roofType, price] of [['平屋顶',105], ['组合屋顶',145], ['坡屋顶',185]]) {
  assert.equal(form({ roofType }).saved.costBreakdown[2].amount, 320 * price * 0.9);
}
for (const [facadeStyle, price] of [['涂料外墙',430], ['真石漆',430], ['陶瓷砖外墙',450], ['石材外墙',750], ['铝板/金属外立面',750]]) {
  assert.equal(form({ facadeStyle }).saved.costBreakdown[4].amount, 320 * price * 0.9);
}
for (const [windowStandard, price] of [['经济型', 530], ['标准型', 680], ['品质型', 1000]]) {
  const report = form({ windowStandard, buildingArea: '440' }).saved;
  assert.equal(report.costBreakdown[3].amount, 440 * 0.25 * price * 0.9);
  assert.equal(report.basicInfo.find(item => item.label === '门窗标准').value, windowStandard);
  assert.equal(report.costBreakdown[5].name, '水电及其它费用');
  assert.equal(report.costBreakdown[5].amount, 440 * 100 * 0.9);
}
assert.equal(form({ windowStandard: '常规' }).saved, undefined);
assert.equal(form({ windowStandard: '系统窗高档' }).saved, undefined);
assert.equal(form({ city: '上海', buildingStyle: '欧式', decorationStyle: '品质型' }).saved.estimatedTotalCost, normal.saved.estimatedTotalCost);
const pile = form({ foundationType: '桩基础' }).saved;
assert.equal(pile.totalLabel, '已测算部分合计');
assert.equal(pile.unitLabel, '已测算部分单价');
assert.equal(pile.costBreakdown[0].amountText, '按图纸核算');
assert.equal(pile.costBreakdown[0].estimable, false);
assert.equal(pile.estimatedTotalCost, '46.66万元');
assert.equal(form({ footprintArea: '' }).saved, undefined);
assert.equal(form({ buildingArea: '-1' }).saved, undefined);
assert.equal(form({ footprintArea: '321' }).saved, undefined);
assert.equal(form({ floorCount: '1.5' }).saved, undefined);
assert.equal(form({ firstFloorHeight: '' }).saved, undefined);
assert.equal(form({ floorCount: '1', upperFloorHeight: '', hasUpperFloorHeight: false }).saved.basicInfo.find(i => i.label === '其他楼层高度').value, '不适用');
assert.equal(form({}, true).route, undefined);
console.log('Six-part pricing, all tiers, derived rates, snapshots, partial estimates and validation passed');

for (const [basementType, cost, total] of [['半地下室', 91800, '62.86万元'], ['全地下室', 183600, '72.04万元']]) {
  const report = form({ basementType }).saved;
  assert.equal(report.costBreakdown.length, 7);
  assert.equal(report.costBreakdown[6].name, '地下室工程');
  assert.equal(report.costBreakdown[6].amount, cost);
  assert.equal(report.estimatedTotalCost, total);
  const sum = report.costBreakdown.reduce((acc, item) => acc + item.amount, 0);
  assert.equal(report.estimatedUnitCost, `${(sum / 320).toFixed(2)}元/㎡`);
}
assert.equal(form({ basementType: '未知' }).saved, undefined);

const layout = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicost/aicost.scss'), 'utf8');
assert.doesNotMatch(layout, /188rpx/);
assert.match(layout, /flex-direction:\s*column/);
