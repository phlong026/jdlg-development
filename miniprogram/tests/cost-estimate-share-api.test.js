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

test('create signs the exact POST path and removes presentation-only colors from the DTO', async () => {
  const h = loadClient([{ statusCode: 200, data: { code: 0, data: { report_id: 'AbcdEFghIJklMNopQRstUVwxYZ012345', expires_at: '2026-11-20T00:00:00Z' } } }]);
  const result = await h.api.createCostEstimateShare(report);
  assert.equal(result.report_id, 'AbcdEFghIJklMNopQRstUVwxYZ012345');
  assert.equal(h.requests[0].method, 'POST');
  assert.equal(h.requests[0].header.token, 'owner-token');
  assert.equal(h.requests[0].header.signedPath, '/api/v1/miniprogram/ai/cost/shares');
  assert.equal('color' in h.requests[0].data.costBreakdown[0], false);
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

test('API and network failures become visible user-safe errors', async () => {
  const apiFailure = loadClient([{ statusCode: 410, data: { code: 1, msg: '分享报告已过期' } }]);
  await assert.rejects(() => apiFailure.api.getCostEstimateShare('abcDEF0123456789abcDEF0123456789'), /分享报告已过期/);
  const networkFailure = loadClient([new Error('socket closed')]);
  await assert.rejects(() => networkFailure.api.createCostEstimateShare(report), /网络连接失败/);
});
