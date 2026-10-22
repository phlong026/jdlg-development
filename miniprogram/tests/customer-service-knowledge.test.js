const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');

const root = path.resolve(__dirname, '..');

function loadCustomerServicePage() {
  let definition;
  let chunkReceived;
  let requestOptions;
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
  const wx = {
    getStorageSync(key) {
      return key === 'token' ? 'test-token' : '';
    },
    request(options) {
      requestOptions = options;
      return {
        onChunkReceived(callback) {
          chunkReceived = callback;
        },
      };
    },
  };

  vm.runInNewContext(
    fs.readFileSync(path.join(root, 'miniprogram/pages/customerservice/customerservice.js'), 'utf8'),
    {
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
      Uint8Array,
      setTimeout,
      clearTimeout,
    },
  );

  return {
    definition,
    getRequestOptions: () => requestOptions,
    emitChunk(text) {
      assert.ok(chunkReceived, 'stream request should register onChunkReceived');
      const bytes = Buffer.from(text, 'utf8');
      chunkReceived({ data: bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) });
    },
  };
}

function createPage(definition) {
  const collectorSteps = [];
  const recordedEvents = [];
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values) {
      Object.assign(this.data, values);
    },
    scrollToBottom() {},
    recordEvent(name, payload) {
      recordedEvents.push({ name, payload });
    },
    showDemandCollector(step) {
      collectorSteps.push(step);
    },
  };
  return { page, collectorSteps, recordedEvents };
}

test('小程序消费线上知识库 SSE 回复并执行引导动作', async () => {
  const harness = loadCustomerServicePage();
  const { page, collectorSteps, recordedEvents } = createPage(harness.definition);

  await page.sendIntentStream('几个房间');

  const request = harness.getRequestOptions();
  assert.equal(request.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/ai/intent/stream');
  assert.equal(request.method, 'POST');
  assert.equal(request.enableChunked, true);
  assert.equal(request.data.text, '几个房间');

  harness.emitChunk([
    'data: {"type":"intent","intent_type":"knowledge_hit","intent_level":"medium","guide_action":"collect"}',
    'data: {"type":"content","content":"一般三层可以规划 5-8 个房间。"}',
    'data: {"type":"done"}',
    '',
  ].join('\n'));

  assert.equal(page.data.messages.at(-1).content, '一般三层可以规划 5-8 个房间。');
  assert.equal(page.data.aiStreaming, false);
  assert.equal(page.data.waitingFirstChunk, false);
  assert.deepEqual(collectorSteps, [1]);
  assert.equal(recordedEvents.length, 1);
  assert.equal(recordedEvents[0].name, 'intent_classified');
  assert.equal(recordedEvents[0].payload.intent_type, 'knowledge_hit');
  assert.equal(recordedEvents[0].payload.guide_action, 'collect');
});
