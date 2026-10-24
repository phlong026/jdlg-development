'use strict';
// 留资/转人工（customerservice.createLead）与公开报告页（aicostshare 内容就绪）
// 都要引导缺昵称的已登录用户补全资料；未登录与提交失败不触发。
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');
const settle = () => new Promise(resolve => setImmediate(resolve));

function transpile(source) {
  return ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
}

function loadSource(relative, extension) {
  const source = fs.readFileSync(path.join(root, relative + '.' + extension), 'utf8');
  return extension === 'ts' ? transpile(source) : source;
}

function createGuard(calls) {
  return { checkAndShow: () => { calls.push('guard'); return Promise.resolve(true); } };
}

function loadCustomerService(extension, { token = 'tk', leadOk = true } = {}) {
  let definition;
  const calls = [];
  const wx = {
    getStorageSync: key => (key === 'token' ? token : undefined),
    setStorageSync() {}, removeStorageSync() {},
    request(options) { options.success({ statusCode: 200, data: leadOk ? { code: 0, data: { id: 9 } } : { code: 1, msg: 'failed' } }); },
    showToast() {}, navigateTo() {},
  };
  const dependencies = {
    '../../utils/asset': { resolveAssetPath: value => value },
    '../../utils/parameter': { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) },
    '../../utils/deviceFingerprint': { getCachedDeviceFingerprint: () => '' },
    '../../utils/promptReview': { blockUnsafeText: () => false },
    '../../utils/enterpriseWechat': {
      DEFAULT_CUSTOMER_SERVICE_CORP_ID: '', DEFAULT_CUSTOMER_SERVICE_URL: '',
      openEnterpriseCustomerServiceChat: async () => ({ opened: false }),
    },
    '../../utils/tracking/consultation': { optionalConsultationHeaders: (_path, headers) => headers },
  };
  vm.runInNewContext(loadSource('miniprogram/pages/customerservice/customerservice', extension), {
    exports: {},
    require(request) {
      const dependency = dependencies[request];
      if (!dependency) throw new Error(`Unexpected dependency: ${request}`);
      return dependency;
    },
    Page: value => { definition = value; },
    wx, console: { log() {}, warn() {}, error() {} }, Promise, Date, Math, setTimeout, clearTimeout,
  });
  function makePage() {
    return {
      ...definition,
      data: JSON.parse(JSON.stringify(definition.data)),
      setData(values) { Object.assign(this.data, values); },
      selectComponent: id => (id === '#nicknameGuard' ? createGuard(calls) : null),
      touchFlow() {}, pushUserChoice() {}, pushAIMessage() {}, recordEvent() {}, scrollToBottom() {},
      buildCompleteDemandText() { return ''; },
    };
  }
  return { makePage, calls };
}

function loadCostShare(extension, { token = 'tk' } = {}) {
  let definition;
  const calls = [];
  const snapshot = { result: {
    basicInfo: [{ label: '所在地区', value: '福建 / 泉州' }, { label: '总建筑面积', value: '320㎡' }],
    totalLabel: '总造价', estimatedTotalCost: '100万',
  } };
  const dependencies = {
    '../../utils/costEstimateShare': { getCostEstimateShare: async () => snapshot },
    '../../utils/costEstimateShareImage': { prepareCostEstimateShareImage: async () => 'chat', prepareCostEstimateTimelineImage: async () => 'timeline' },
  };
  const wx = {
    getStorageSync: key => (key === 'token' ? token : undefined),
    setStorageSync() {}, removeStorageSync() {}, navigateTo() {},
  };
  vm.runInNewContext(loadSource('miniprogram/pages/aicostshare/aicostshare', extension), {
    exports: {},
    require(request) {
      const dependency = dependencies[request];
      if (!dependency) throw new Error(`Unexpected dependency: ${request}`);
      return dependency;
    },
    Page: value => { definition = value; },
    wx, console: { log() {}, warn() {}, error() {} }, Promise, Date, Math, setTimeout, clearTimeout,
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values, done) { Object.assign(this.data, values); if (done) done.call(this); },
    selectComponent: id => (id === '#nicknameGuard' ? createGuard(calls) : null),
  };
  return { page, calls };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: customerservice 留资/转人工线索提交成功后引导补昵称`, async () => {
    const { makePage, calls } = loadCustomerService(extension);
    const page = makePage();
    await page.createLead('structured_requirement', { silent: true });
    assert.equal(page.data.submitting, false);
    assert.equal(calls.length, 1, 'guard.checkAndShow runs once after a saved lead');
  });

  test(`${extension}: customerservice 未登录或提交失败不触发昵称引导`, async () => {
    const anonymous = loadCustomerService(extension, { token: '' });
    await anonymous.makePage().createLead('structured_requirement', { silent: true });
    assert.equal(anonymous.calls.length, 0, 'anonymous visitors are never prompted');

    const failed = loadCustomerService(extension, { leadOk: false });
    await failed.makePage().createLead('structured_requirement', { silent: true }).catch(() => {});
    assert.equal(failed.calls.length, 0, 'a failed lead save must not prompt');
  });

  test(`${extension}: aicostshare 公开报告就绪后引导已登录访客补昵称`, async () => {
    const { page, calls } = loadCostShare(extension);
    page.onLoad({ report_id: 'x'.repeat(32) });
    await settle(); await settle();
    assert.equal(page.data.contentReady, true);
    assert.equal(calls.length, 1, 'content readiness prompts once');
  });

  test(`${extension}: aicostshare 快照加载失败不触发昵称引导`, async () => {
    const failed = loadCostShareFailed(extension);
    failed.page.onLoad({ report_id: 'x'.repeat(32) });
    await settle(); await settle();
    assert.equal(failed.page.data.contentReady, false);
    assert.equal(failed.calls.length, 0);
  });
}

function loadCostShareFailed(extension) {
  let definition;
  const calls = [];
  const dependencies = {
    '../../utils/costEstimateShare': { getCostEstimateShare: async () => { throw new Error('revoked'); } },
    '../../utils/costEstimateShareImage': { prepareCostEstimateShareImage: async () => 'chat', prepareCostEstimateTimelineImage: async () => 'timeline' },
  };
  const wx = {
    getStorageSync: key => (key === 'token' ? 'tk' : undefined),
    setStorageSync() {}, removeStorageSync() {}, navigateTo() {},
  };
  vm.runInNewContext(loadSource('miniprogram/pages/aicostshare/aicostshare', extension), {
    exports: {},
    require(request) {
      const dependency = dependencies[request];
      if (!dependency) throw new Error(`Unexpected dependency: ${request}`);
      return dependency;
    },
    Page: value => { definition = value; },
    wx, console: { log() {}, warn() {}, error() {} }, Promise, Date, Math, setTimeout, clearTimeout,
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values, done) { Object.assign(this.data, values); if (done) done.call(this); },
    selectComponent: id => (id === '#nicknameGuard' ? createGuard(calls) : null),
  };
  return { page, calls };
}
