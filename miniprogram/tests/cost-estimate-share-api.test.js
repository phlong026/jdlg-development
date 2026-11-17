const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const helperPath = path.join(__dirname, '../miniprogram/utils/costEstimateShare.ts');

function loadClient(responses) {
  const requests = [];
  const code = ts.transpileModule(fs.readFileSync(helperPath, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  const context = { exports: {}, console: { error() {} } };
  context.require = request => {
    if (request === './parameter') return {
      generateRequestParams: (token, body, apiPath, deviceId) => ({ token, body, apiPath, deviceId }),
      paramsToHeaders: params => ({ token: params.token, signedPath: params.apiPath }),
    };
    if (request === './deviceFingerprint') return { getCachedDeviceFingerprint: () => 'device-1' };
    throw new Error(`unexpected require ${request}`);
  };
  context.wx = {
    getStorageSync(key) { return key === 'token' ? 'owner-token' : ''; },
    request(options) {
      requests.push(options);
      const response = responses.shift();
      if (response instanceof Error) options.fail(response);
      else options.success(response);
    },
  };
  vm.runInNewContext(code, context);
  return { api: context.exports, requests };
}

const report = {
  version: 4, estimatedTotalCost: '58.36万元', estimatedUnitCost: '1823.75元/㎡', totalLabel: '参考总造价', unitLabel: '参考单价',
  basicInfo: Array.from({ length: 14 }, (_, index) => ({ label: `字段${index}`, value: `值${index}`, ...(index === 0 ? { fullWidth: true } : {}) })),
  costBreakdown: Array.from({ length: 6 }, (_, index) => ({ name: `分项${index}`, amount: index * 100, amountText: `${index}元`, percent: index, note: `说明${index}`, estimable: true, color: '#fff' })),
  dynamicNotes: ['说明'],
};

const renovationDefinitions = [
  ['bedroom', '卧室', [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['door', '室内门']]],
  ['living', '客餐厅', [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['threshold', '门槛石']]],
  ['kitchen', '厨房', [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['kitchenware', '厨具'], ['door', '室内门'], ['lighting', '灯具']]],
  ['bathroom', '卫生间', [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['sanitary', '卫浴用具'], ['door', '室内门']]],
  ['balcony', '阳台', [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面']]],
  ['common', '全屋公共', [['utilities', '水电改造'], ['handling', '搬运清洁'], ['demolition', '拆改与保护']]],
];

function renovationSpaces() {
  return renovationDefinitions.map(([id, name, groups], spaceIndex) => ({
    id, name, count: spaceIndex === 0 ? 1 : 0, amount: spaceIndex === 0 ? 2000 : 0,
    groups: groups.map(([groupId, groupName], groupIndex) => ({
      id: groupId, name: groupName, amount: spaceIndex === 0 && groupIndex === 0 ? 2000 : 0,
      items: spaceIndex === 0 && groupIndex === 0
        ? [{ id: 'bedroom.ceiling.fixture', name: '顶面工程', unit: '项', quantity: 1, materialUnitPrice: 1000, laborAuxUnitPrice: 1000, amount: 2000, included: true, private: 'drop-me' }]
        : [],
    })),
  }));
}

const renovationReport = {
  version: 5, quoteType: 'renovation', estimatedTotalCost: '0.23万元', estimatedUnitCost: '23.48元/㎡', totalLabel: '装修参考总价', unitLabel: '参考单价',
  basicInfo: ['所在地区', '装修面积', '房屋状态', '楼层数', '层高', '面宽', '进深', '户型', '室内风格', '装修标准']
    .map((label, index) => ({ label, value: index === 1 ? '100㎡' : `值${index}`, ...(index === 0 ? { fullWidth: true } : {}) })),
  costBreakdown: ['主材费', '辅材费', '人工费', '管理费', '设计费', '税费'].map((name, index) => {
    const amounts = [1000, 294, 706, 200, 80, 68];
    return { name, amount: amounts[index], amountText: `${amounts[index]}元`, percent: amounts[index] / 2348 * 100, note: '参考测算', estimable: true, color: '#fff' };
  }),
  dynamicNotes: [],
  renovation: {
    pricingVersion: 'renovation-reference-v1', totalAmount: 2348,
    spaces: renovationSpaces(),
  },
};

test('create signs the exact POST path and removes presentation-only colors from the DTO', async () => {
  const h = loadClient([{ statusCode: 200, data: { code: 0, data: { report_id: 'AbcdEFghIJklMNopQRstUVwxYZ012345', expires_at: '2026-11-20T00:00:00Z' } } }]);
  const result = await h.api.createCostEstimateShare(report);
  assert.equal(result.report_id, 'AbcdEFghIJklMNopQRstUVwxYZ012345');
  assert.equal(h.requests[0].method, 'POST');
  assert.equal(h.requests[0].header.token, 'owner-token');
  assert.equal(h.requests[0].header.signedPath, '/api/v1/miniprogram/ai/cost/shares');
  assert.equal('color' in h.requests[0].data.costBreakdown[0], false);
  assert.equal('quoteType' in h.requests[0].data, false);
  assert.equal('renovation' in h.requests[0].data, false);
  assert.equal(h.requests[0].timeout, 8000);
});

test('public report fetch validates the id and returns the version 4 result', async () => {
  const h = loadClient([{ statusCode: 200, data: { code: 0, data: { report_id: 'abcDEF0123456789abcDEF0123456789', expires_at: '2026-11-20T00:00:00Z', result: report } } }]);
  const response = await h.api.getCostEstimateShare('abcDEF0123456789abcDEF0123456789');
  assert.equal(response.result.version, 4);
  assert.equal(h.requests[0].method, 'GET');
  assert.equal(h.requests[0].url.endsWith('/api/v1/miniprogram/ai/cost/shares/abcDEF0123456789abcDEF0123456789'), true);
  await assert.rejects(() => h.api.getCostEstimateShare('../bad'), /无效/);
  assert.equal(h.requests.length, 1);
});

test('renovation create signs the independent v5 path and strips non-contract nested fields', async () => {
  const h = loadClient([{ statusCode: 200, data: { code: 0, data: { report_id: 'RenoEFghIJklMNopQRstUVwxYZ012345', expires_at: '2026-11-20T00:00:00Z' } } }]);
  await h.api.createCostEstimateShare(renovationReport);
  assert.equal(h.requests[0].header.signedPath, '/api/v1/miniprogram/ai/renovation-cost/shares');
  assert.equal(h.requests[0].url.endsWith('/api/v1/miniprogram/ai/renovation-cost/shares'), true);
  assert.equal('color' in h.requests[0].data.costBreakdown[0], false);
  assert.equal('private' in h.requests[0].data.renovation.spaces[0].groups[0].items[0], false);
});

test('renovation rejects a tree-balanced item amount that does not equal rounded quantity times unit prices', () => {
  const h = loadClient([]);
  const forged = JSON.parse(JSON.stringify(renovationReport));
  const item = forged.renovation.spaces[0].groups[0].items[0];
  item.materialUnitPrice = 1;
  item.laborAuxUnitPrice = 1;
  assert.equal(item.amount, 2000, 'parent totals intentionally remain balanced');
  assert.equal(h.api.isCostEstimateShareResult(forged), false);
  assert.throws(() => h.api.sanitizeCostEstimateShareResult(forged), /数据无效/);
});

test('renovation rejects a snapshot that omits a required space group', () => {
  const h = loadClient([]);
  const incomplete = JSON.parse(JSON.stringify(renovationReport));
  incomplete.renovation.spaces[0].groups.pop();
  assert.equal(h.api.isCostEstimateShareResult(incomplete), false);
});

test('renovation rejects a tree-balanced but forged material, auxiliary and labor distribution', () => {
  const h = loadClient([]);
  const forged = JSON.parse(JSON.stringify(renovationReport));
  forged.costBreakdown[0].amount += 1;
  forged.costBreakdown[1].amount -= 1;
  assert.equal(forged.costBreakdown.slice(0, 3).reduce((sum, item) => sum + item.amount, 0), 2000);
  assert.equal(h.api.isCostEstimateShareResult(forged), false);
});

test('public report fetch accepts a valid immutable version 5 snapshot on the shared GET path', async () => {
  const id = 'renoEF0123456789abcDEF0123456789';
  const h = loadClient([{ statusCode: 200, data: { code: 0, data: { report_id: id, expires_at: '2026-11-20T00:00:00Z', result: renovationReport } } }]);
  const response = await h.api.getCostEstimateShare(id);
  assert.equal(response.result.version, 5);
  assert.equal(response.result.quoteType, 'renovation');
  assert.equal(h.requests[0].url.endsWith(`/api/v1/miniprogram/ai/cost/shares/${id}`), true);
});

test('API and network failures become visible user-safe errors', async () => {
  const apiFailure = loadClient([{ statusCode: 410, data: { code: 1, msg: '分享报告已过期' } }]);
  await assert.rejects(() => apiFailure.api.getCostEstimateShare('abcDEF0123456789abcDEF0123456789'), /分享报告已过期/);
  const networkFailure = loadClient([new Error('socket closed')]);
  await assert.rejects(() => networkFailure.api.createCostEstimateShare(report), /网络连接失败/);
});
