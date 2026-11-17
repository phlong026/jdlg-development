const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');
const pricing = require('../miniprogram/utils/renovation-pricing');
const utils = path.join(__dirname, '../miniprogram/utils');
const plain = value => JSON.parse(JSON.stringify(value));

function load() {
  const client = { exports: {}, require: () => ({}) };
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(utils, 'costEstimateShare.ts'), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText, client);
  const storage = new Map([['token', 'owner-token'], ['userInfo', { id: 7, token: 'owner-token' }]]);
  const calls = [];
  const context = { module: { exports: {} }, require(name) {
    if (name === './costEstimateShare') return client.exports;
    throw new Error(name);
  }, wx: {
    getStorageSync: key => storage.get(key),
    setStorageSync: (key, value) => storage.set(key, plain(value)),
    showModal: options => calls.push(options),
    navigateTo: options => calls.push(options),
  } };
  vm.runInNewContext(fs.readFileSync(path.join(utils, 'renovation-report.js'), 'utf8'), context);
  return { api: context.module.exports, storage, calls, wx: context.wx, validator: client.exports.isCostEstimateShareResult };
}

test('real pricing output produces the strict v5 snapshot and preserves amounts and quantities', () => {
  const h = load();
  for (const standard of ['economy', 'standard', 'quality']) {
    const estimate = pricing.estimateRenovation({ ...pricing.createDefaultRenovationInput(), standard, width: null });
    const report = h.api.estimateToSnapshot(estimate);
    assert.equal(h.validator(report), true);
    assert.equal(report.renovation.totalAmount, estimate.totalAmount);
    assert.deepEqual(plain(report.renovation.spaces), estimate.spaces);
    assert.equal(report.costBreakdown.reduce((sum, item) => sum + item.amount, 0), estimate.totalAmount);
    assert.equal(report.basicInfo.find(item => item.label === '面宽').value, '未填写');
    estimate.spaces[0].groups[0].items[0].quantity = 999;
    assert.notEqual(report.renovation.spaces[0].groups[0].items[0].quantity, 999);
  }
});

test('zero-count room keeps the complete group snapshot and passes strict item formulas', () => {
  const h = load();
  const input = pricing.createDefaultRenovationInput();
  input.rooms.bedroom = 0;
  const estimate = pricing.estimateRenovation(input);
  const report = h.api.estimateToSnapshot(estimate);
  assert.equal(h.validator(report), true);
  const bedroom = report.renovation.spaces.find(space => space.id === 'bedroom');
  assert.equal(bedroom.groups.length, 4);
  assert.ok(bedroom.groups.flatMap(group => group.items).every(item => item.included === false && item.amount === 0));
});

test('private cache is isolated by the current account and never writes the building cache', () => {
  const h = load(); const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput());
  const selections = { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } };
  const record = h.api.writeRenovationReport(estimate, selections);
  assert.equal(record.userID, 7);
  assert.equal(h.api.readRenovationReport().report.renovation.totalAmount, 97814);
  assert.equal(h.storage.has('aicostEstimateResult:7'), false);
  selections.excludedItems.push('later-edit');
  assert.deepEqual(plain(h.api.readRenovationReport().selections.excludedItems), []);
  h.storage.set('userInfo', { id: 8, token: 'owner-token' });
  assert.throws(() => h.api.readRenovationReport(), /重新测算/);
  h.storage.set('renovationBudgetResult:8', record);
  assert.throws(() => h.api.readRenovationReport(), /重新测算/);
  h.storage.set('userInfo', { id: 7, token: 'stale-token' });
  assert.equal(h.api.getBudgetAccountId(), 0);
  assert.throws(() => h.api.writeRenovationReport(estimate, selections), /登录/);
});

test('storage failures surface before opening a result and corrupt snapshots are rejected', () => {
  const h = load(); const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput());
  h.wx.setStorageSync = () => { throw new Error('quota'); };
  assert.throws(() => h.api.writeRenovationReport(estimate, {}), /保存/);
  h.storage.set('renovationBudgetResult:7', { version: 1, userID: 7, input: estimate.input, selections: {}, report: { version: 5 } });
  assert.throws(() => h.api.readRenovationReport(), /重新测算/);
});

test('building prefill is read only and login prompt follows the existing flow', () => {
  const h = load();
  const labels = ['所在地区', '建筑占地面积', '总建筑面积', '楼层数', '首层高度', '其他楼层高度', '地下室形式', '建筑风格', '结构形式', '屋顶形式', '外立面样式', '建筑基础', '装修标准', '门窗标准'];
  const building = { version: 4, estimatedTotalCost: '53.68万元', estimatedUnitCost: '1677.38元/㎡', totalLabel: '参考总造价', unitLabel: '参考单价',
    basicInfo: labels.map((label, index) => ({ label, value: ['福建 / 泉州', '120㎡', '320㎡', '3层', '3.6m'][index] || '原值' })),
    costBreakdown: Array.from({ length: 6 }, (_, i) => ({ name: String(i), amount: 0, amountText: '0元', percent: 0, note: '原值', estimable: true })), dynamicNotes: [] };
  h.storage.set('aicostEstimateResult:7', building);
  const before = JSON.stringify(building);
  assert.deepEqual(plain(h.api.readBuildingPrefill()), { province: '福建', city: '泉州', area: 320, floorCount: 3, height: 3.6 });
  assert.equal(JSON.stringify(h.storage.get('aicostEstimateResult:7')), before);
  h.api.requestBudgetLogin();
  assert.equal(h.calls[0].content, '登录后可保存本次测算结果');
  h.calls[0].success({ confirm: true });
  assert.equal(h.calls[1].url, '/pages/login/login?from=ai-action');
});
