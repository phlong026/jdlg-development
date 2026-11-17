import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ts = require(path.join(root, 'node_modules/typescript'));

function loadApi(getData = { items: [] }) {
  const file = path.join(root, 'src/api/aiVideoStudio.ts');
  const source = fs.readFileSync(file, 'utf8');
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  const calls = [];
  const request = (method) => async (endpoint, payload) => {
    calls.push([method, endpoint, payload]);
    return { code: 0, data: method === 'get' ? getData : payload };
  };
  const module = { exports: {} };
  vm.runInNewContext(output, {
    module,
    exports: module.exports,
    require(name) {
      if (name === '../utils/request') return { get: request('get'), put: request('put'), post: request('post') };
      if (name === '../config/api') return {
        API_ENDPOINTS: { AI_VIDEO_STUDIO: {
          CONFIG: '/api/v1/management/ai-video-studio/config',
          TASKS: '/api/v1/management/ai-video-studio/tasks',
          TASK_DETAIL: id => `/api/v1/management/ai-video-studio/tasks/${encodeURIComponent(id)}`,
          RECONCILE: id => `/api/v1/management/ai-video-studio/results/${encodeURIComponent(id)}/reconcile`,
          LEDGER: '/api/v1/management/ai-video-studio/ledger',
        } },
      };
      throw new Error(`unexpected import ${name}`);
    },
  });
  return { api: module.exports, calls };
}

test('configuration defaults global concurrency to three without replacing an explicit value', () => {
  const fallback = loadApi({ version: 1, enabled: false, limits: {}, modes: {}, prices: [] }).api.normalizeAIVideoStudioConfig;
  assert.equal(fallback({ version: 1, enabled: false, limits: {}, modes: {}, prices: [] }).limits.max_concurrency, 3);
  assert.equal(fallback({ version: 1, enabled: false, limits: { max_concurrency: 7 }, modes: {}, prices: [] }).limits.max_concurrency, 7);
});

test('saving configuration never echoes runtime readiness or credential state', async () => {
  const { api, calls } = loadApi();
  await api.saveAIVideoStudioConfig({
    version: 4,
    enabled: true,
    provider_code: 'metaso_h3',
    provider_configured: true,
    execution_ready: true,
    accounting_ready: true,
    billing_ready: true,
    media_storage_ready: true,
    media_signing_ready: true,
    ffprobe_ready: true,
    new_tasks_ready: true,
    disabled_reason: '',
    mode_readiness: { text: { enabled: true, disabled_reason: '' } },
    modes: { text: true },
    limits: { max_concurrency: 3 },
    prices: [],
  });
  assert.deepEqual(JSON.parse(JSON.stringify(calls[0][2])), {
    enabled: true,
    provider_code: 'metaso_h3',
    modes: { text: true },
    limits: { max_concurrency: 3 },
    prices: [],
    expected_version: 4,
  });
});

test('management readiness is fail-closed and reports every mode reason', () => {
  const { summarizeAIVideoStudioReadiness } = loadModel();
  const config = {
    enabled: true,
    new_tasks_ready: false,
    disabled_reason: '视频生成价格尚未配置',
    mode_readiness: {
      text: { enabled: false, disabled_reason: '视频生成价格尚未配置' },
      first_frame: { enabled: false, disabled_reason: '素材存储或安全签名尚未配置' },
      first_last_frame: { enabled: false, disabled_reason: '素材存储或安全签名尚未配置' },
      reference: { enabled: false, disabled_reason: '音视频时长探测尚未配置' },
    },
  };
  const summary = summarizeAIVideoStudioReadiness(config);
  assert.equal(summary.ready, false);
  assert.equal(summary.status, '等待服务就绪');
  assert.equal(summary.reason, '视频生成价格尚未配置');
  assert.deepEqual(Array.from(summary.modes, (item) => [item.code, item.enabled, item.reason]), [
    ['text', false, '视频生成价格尚未配置'],
    ['first_frame', false, '素材存储或安全签名尚未配置'],
    ['first_last_frame', false, '素材存储或安全签名尚未配置'],
    ['reference', false, '音视频时长探测尚未配置'],
  ]);
  const missing = summarizeAIVideoStudioReadiness({ enabled: true });
  assert.equal(missing.ready, false);
  assert.match(missing.reason, /就绪状态/);
});

function loadModel() {
  const file = path.join(root, 'src/pages/aiVideoStudioModel.ts');
  const output = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(output, { module, exports: module.exports, require });
  return module.exports;
}

test('admin API uses the isolated management namespace and optimistic version', async () => {
  const { api, calls } = loadApi();
  await api.getAIVideoStudioConfig();
  await api.saveAIVideoStudioConfig({ enabled: true, version: 4 });
  await api.getAIVideoStudioTasks({ status: 'failed', page: 2 });
  await api.getAIVideoStudioTask('task/7');
  await api.getAIVideoStudioLedger({ batch_id: 'batch-1' });
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    ['get', '/api/v1/management/ai-video-studio/config', null],
    ['put', '/api/v1/management/ai-video-studio/config', { enabled: true, expected_version: 4 }],
    ['get', '/api/v1/management/ai-video-studio/tasks', { status: 'failed', page: 2 }],
    ['get', '/api/v1/management/ai-video-studio/tasks/task%2F7', null],
    ['get', '/api/v1/management/ai-video-studio/ledger', { batch_id: 'batch-1' }],
  ]);
});

test('page exposes fail-closed provider and price states without credential fields', () => {
  const source = fs.readFileSync(path.join(root, 'src/pages/aiVideoStudio.tsx'), 'utf8');
  assert.match(source, /provider_configured/);
  assert.match(source, /summarizeAIVideoStudioReadiness/);
  assert.match(source, /media_storage_ready/);
  assert.match(source, /media_signing_ready/);
  assert.match(source, /ffprobe_ready/);
  assert.match(source, /aivs-readiness/);
  assert.match(source, /未配置[^<]*不能开放|未配置/);
  assert.match(source, /unit_stones/);
  assert.doesNotMatch(source, /api_key|secret|access_token|endpoint/);
  assert.match(source, /待确认|费用待核对/);
});

test('reconciliation requires a specific task and operator evidence without a fabricated result', async () => {
  const model = loadModel();
  assert.throws(() => model.buildStudioReconciliation('attach_external_id', '', 'a valid explanation'), /标识/);
  assert.throws(() => model.buildStudioReconciliation('refund_confirmed_rejected', '', ''), /依据/);
  const payload = model.buildStudioReconciliation('attach_external_id', 'external-123', '已与供应商核对该任务属于本次提交');
  assert.equal(payload.action, 'attach_external_id');
  assert.equal(payload.external_id, 'external-123');
  assert.equal('result_url' in payload, false);
  const { api, calls } = loadApi();
  await api.reconcileAIVideoStudioResult('result/1', payload);
  assert.equal(calls[0][0], 'post');
  assert.equal(calls[0][1], '/api/v1/management/ai-video-studio/results/result%2F1/reconcile');
});

test('configuration stays fail-closed until provider, mode and positive price are ready', () => {
  const { validateAIVideoStudioConfig } = loadModel();
  const config = {
    enabled: true,
    provider_code: 'metaso_h3',
    provider_configured: false,
    modes: { text: false, first_frame: false, first_last_frame: false, reference: false },
    prices: [{ code: 'ai_video_studio_768p', billing_unit: 'per_second', enabled: true, unit_stones: 0 }],
    limits: {
      max_concurrency: 3, timeout_seconds: 900, max_quantity: 1,
      max_duration_768p: 15, max_duration_2k: 16,
      max_reference_images: 8, max_reference_videos: 3,
      max_reference_audios: 3, max_reference_total: 13,
    },
  };
  assert.deepEqual(Array.from(validateAIVideoStudioConfig(config)), [
    '供应商未配置，主开关不能开放。',
    '至少开放一种生成模式。',
    '已启用价格项必须配置大于 0 的整数灵石价格。',
    '2K 时长上限不能超过 15 秒。',
    '混合参考素材总数不能超过 12。',
  ]);
  config.provider_configured = true;
  config.modes.text = true;
  config.prices[0].unit_stones = 12;
  config.limits.max_duration_2k = 15;
  config.limits.max_reference_total = 12;
  assert.deepEqual(Array.from(validateAIVideoStudioConfig(config)), []);
});

test('configuration validates the same numeric bounds as the service', () => {
  const { validateAIVideoStudioConfig } = loadModel();
  const base = {
    enabled: false,
    provider_code: 'metaso_h3',
    provider_configured: false,
    modes: { text: false, first_frame: false, first_last_frame: false, reference: false },
    prices: [
      { code: 'ai_video_studio_prompt_optimize', billing_unit: 'per_request', enabled: false, unit_stones: null },
      { code: 'ai_video_studio_768p', billing_unit: 'per_second', enabled: false, unit_stones: null },
      { code: 'ai_video_studio_2k', billing_unit: 'per_second', enabled: false, unit_stones: null },
      { code: 'ai_video_studio_768p_long', billing_unit: 'per_second', enabled: false, unit_stones: null },
    ],
    limits: {
      max_concurrency: 3, timeout_seconds: 900, max_quantity: 1,
      max_duration_768p: 15, max_duration_2k: 15,
      max_reference_images: 8, max_reference_videos: 3,
      max_reference_audios: 3, max_reference_total: 12,
    },
  };
  assert.deepEqual(Array.from(validateAIVideoStudioConfig(base)), []);
  const invalid = structuredClone(base);
  invalid.limits.max_concurrency = 21;
  invalid.limits.timeout_seconds = 3601;
  invalid.limits.max_quantity = 11;
  invalid.limits.max_reference_images = -1;
  invalid.limits.max_reference_total = 2;
  assert.deepEqual(Array.from(validateAIVideoStudioConfig(invalid)), [
    '并发任务上限必须为 1–20。',
    '单任务超时必须为 1–3600 秒。',
    '单批数量上限必须为 1–10。',
    '参考图片、视频、音频和总数上限不能小于 0。',
    '参考素材总上限不能小于任一单类上限。',
  ]);
  const invalidPrices = structuredClone(base);
  invalidPrices.prices[0].unit_stones = -1;
  invalidPrices.prices[1].unit_stones = 1_000_000_001;
  assert.deepEqual(Array.from(validateAIVideoStudioConfig(invalidPrices)), [
    '灵石单价必须是 0–1000000000 的整数；未配置请留空。',
  ]);
});
