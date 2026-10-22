const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

function loadCustomerServicePage() {
  let definition;
  const navigations = [];
  const wx = {
    navigateTo(options) {
      navigations.push(options.url);
    },
  };
  const dependencies = {
    '../../utils/asset': { resolveAssetPath: value => value },
    '../../utils/parameter': {
      generateRequestParams: () => ({}),
      paramsToHeaders: () => ({}),
    },
    '../../utils/deviceFingerprint': { getCachedDeviceFingerprint: () => '' },
    '../../utils/promptReview': { blockUnsafeText: () => false },
    '../../utils/enterpriseWechat': {
      DEFAULT_CUSTOMER_SERVICE_CORP_ID: '',
      DEFAULT_CUSTOMER_SERVICE_URL: '',
      openEnterpriseCustomerServiceChat: async () => ({ opened: false }),
    },
    '../../utils/tracking/consultation': { optionalConsultationHeaders: (_path, headers) => headers },
  };

  vm.runInNewContext(read('miniprogram/pages/customerservice/customerservice.js'), {
    exports: {},
    require(request) {
      const dependency = dependencies[request];
      if (!dependency) throw new Error(`Unexpected dependency: ${request}`);
      return dependency;
    },
    Page(value) {
      definition = value;
    },
    wx,
    console: { log() {}, warn() {}, error() {} },
    Promise,
    Date,
    Math,
    setTimeout,
    clearTimeout,
  });

  return { definition, navigations };
}

function createPage(definition) {
  const handoffs = [];
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values) {
      Object.assign(this.data, values);
    },
    touchFlow() {},
    pushUserChoice() {},
    pushAIMessage() {},
    recordEvent() {},
    showDemandCollector() {},
    handoffHuman() {
      handoffs.push('human');
    },
  };
  return { page, handoffs };
}

const serviceWxml = read('miniprogram/pages/customerservice/customerservice.wxml');
const sendIcon = read('miniprogram/assets/icons/send-plane.svg');

test('需求摘要卡保留生成方案和设计师承接两个可执行 CTA', () => {
  const summaryCardMatch = serviceWxml.match(/<view wx:if="\{\{demandSummaryVisible\}\}" class="demand-summary-card">([\s\S]*?)<\/view>\s*<\/view>/);
  assert.ok(summaryCardMatch, 'customer service page should render a demand summary card');

  const summaryCard = summaryCardMatch[0];
  assert.match(summaryCard, /bindtap="goGenerate">一键生成专属方案<\/view>/);
  assert.match(summaryCard, /bindtap="handoffHuman">对接设计老师<\/view>/);
  assert.doesNotMatch(summaryCard, /了解造价|修改户型/);
});

test('需求采集后快捷操作能进入造价页并交给设计老师', () => {
  const { definition, navigations } = loadCustomerServicePage();
  const { page, handoffs } = createPage(definition);

  page.onQuickAction({ currentTarget: { dataset: { key: 'build' } } });
  assert.deepEqual(
    Array.from(page.data.quickActions, item => [item.key, item.label]),
    [['cost', '了解造价'], ['human', '找设计老师']],
  );

  page.onQuickAction({ currentTarget: { dataset: { key: 'cost' } } });
  page.onQuickAction({ currentTarget: { dataset: { key: 'human' } } });
  assert.deepEqual(navigations, ['/pages/aicost/aicost']);
  assert.equal(handoffs.length, 1);
});

test('底部快捷操作和输入区保持在同一操作面板', () => {
  assert.match(serviceWxml, /bottom-panel[\s\S]*quick-actions[\s\S]*input-panel/);
  assert.match(serviceWxml, /wx:for="\{\{quickActions\}\}"[\s\S]*bindtap="onQuickAction"[\s\S]*data-key="\{\{item\.key\}\}"/);
  assert.match(serviceWxml, /class="composer-shell"/);
  assert.match(serviceWxml, /class="composer-add" bindtap="onComposerAdd"/);
  assert.match(serviceWxml, /placeholder="\{\{aiStreaming \? 'AI 正在回复中\.\.\.' : '继续补充一句需求'\}\}"/);
  assert.match(serviceWxml, /<textarea[\s\S]*?\sauto-height\s/);
  assert.match(serviceWxml, /class="composer-action send \{\{inputText && !aiStreaming \? 'active' : 'disabled'\}\}" bindtap="onSend"/);
  assert.match(serviceWxml, /src="\{\{sendIcon\}\}"/);
  assert.doesNotMatch(serviceWxml, /onVoicePlaceholder|\)\)\)|class="composer-action voice"/);
  assert.match(sendIcon, /<svg[\s\S]*viewBox=/);
});
