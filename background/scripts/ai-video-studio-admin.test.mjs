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

function loadApi() {
  const file = path.join(root, 'src/api/aiVideoStudio.ts');
  const source = fs.readFileSync(file, 'utf8');
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  const calls = [];
  const request = (method) => async (endpoint, payload) => {
    calls.push([method, endpoint, payload]);
    return { code: 0, data: method === 'get' ? { items: [] } : payload };
  };
  const module = { exports: {} };
  vm.runInNewContext(output, {
    module,
    exports: module.exports,
    require(name) {
      if (name === '../utils/request') return { get: request('get'), put: request('put') };
      if (name === '../config/api') return {
        API_ENDPOINTS: { AI_VIDEO_STUDIO: {
          CONFIG: '/api/v1/management/ai-video-studio/config',
          TASKS: '/api/v1/management/ai-video-studio/tasks',
          TASK_DETAIL: id => `/api/v1/management/ai-video-studio/tasks/${encodeURIComponent(id)}`,
          LEDGER: '/api/v1/management/ai-video-studio/ledger',
        } },
      };
      throw new Error(`unexpected import ${name}`);
    },
  });
  return { api: module.exports, calls };
}

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
  assert.match(source, /未配置[^<]*不能开放|未配置/);
  assert.match(source, /unit_stones/);
  assert.doesNotMatch(source, /api_key|secret|access_token|endpoint/);
  assert.match(source, /待确认|费用待核对/);
});

test('configuration stays fail-closed until provider, mode and positive price are ready', () => {
  const { validateAIVideoStudioConfig } = loadModel();
  const config = {
    enabled: true,
    provider_code: 'metaso_h3',
    provider_configured: false,
    modes: { text: false, first_frame: false, first_last_frame: false, reference: false },
    prices: [{ code: 'ai_video_studio_768p', billing_unit: 'per_second', enabled: true, unit_stones: 0 }],
    limits: { max_duration_2k: 16, max_reference_total: 13 },
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
