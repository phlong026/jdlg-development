import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const root = new URL('../', import.meta.url);
function load(file, overrides = {}) {
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(new URL(file, root), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, require: name => name in overrides ? overrides[name] : name === '../pages/shareTracking/access' ? { captureTrackingAccessBoundary: () => ({}), notifyTrackingAccessDenied() {} } : name === '../config/api' ? { API_BASE_URL: '' } : name === '../utils/session' ? { expireSession() {} } : require(name), Date, URL, Error, AbortController, AbortSignal, setTimeout, clearTimeout, window: { location: { origin: 'https://example.test', protocol: 'https:' } }, ...overrides.globals });
  return module.exports;
}
const model = load('src/pages/shareTracking/model.ts');
const flush = () => new Promise(r => setImmediate(r));
const counts = (over = {}) => ({ fetched: 3, created: 1, updated: 1, unchanged: 1, skipped: 0, conflicts: 0, failed: 0, images_imported: 0, image_failures: 0, ...over });
const enabledInfo = (over = {}) => ({ enabled: true, owner_id: 'owner-a', rights: 'own', max_articles: 30, import_images: false, interval_minutes: 0, publishes: false, ...over });

function harness(tracking) {
  const React = require('react'); let slots = [], index = 0, tree; const calls = []; const effects = [];
  const react = { ...React, useState(init) { const i = index++; if (!(i in slots)) slots[i] = typeof init === 'function' ? init() : init; return [slots[i], v => { slots[i] = typeof v === 'function' ? v(slots[i]) : v; }]; }, useRef(init) { const i = index++; if (!(i in slots)) slots[i] = { current: init }; return slots[i]; }, useEffect(effect, deps) { const i = index++; if (!slots[i] || deps.some((d, j) => d !== slots[i].deps[j])) { slots[i]?.cleanup?.(); slots[i] = { deps, cleanup: effect() }; effects.push(i); } } };
  const module = load('src/pages/shareTracking/mpSync.tsx', { react, '../../api/shareTracking': { tracking }, './model': model });
  // Child components without hooks are expanded, as React would render them.
  const expand = x => typeof x?.type === 'function' ? x.type(x.props) : x;
  const nodes = x => Array.isArray(x) ? x.flatMap(nodes) : !x || typeof x !== 'object' ? [] : (y => [y, ...nodes(y?.props?.children)])(expand(x));
  const text = x => Array.isArray(x) ? x.map(text).join('') : x == null || typeof x === 'boolean' ? '' : typeof x === 'object' ? (y => text(y?.props?.children))(expand(x)) : String(x);
  const render = () => { index = 0; tree = module.MPSyncPanel({ onBusyChange: v => calls.push(['busy', v]), changed: () => calls.push(['changed']) }); return tree; };
  render();
  return { module, calls, render, text: () => text(tree), button: label => nodes(tree).find(n => n.type === 'button' && text(n).includes(label)), unmount: () => effects.forEach(i => slots[i]?.cleanup?.()) };
}

test('the panel states that sync only creates drafts and never publishes', async () => {
  const h = harness({ mpSyncInfo: async () => enabledInfo(), mpSyncRun: async () => ({}) });
  await flush(); h.render();
  assert.match(h.text(), /草稿/); assert.match(h.text(), /不会自动发布/); assert.match(h.text(), /不会撤回已有素材/); assert.match(h.text(), /不会覆盖被人工修改过的草稿/);
  assert.match(h.text(), /归属 owner-a/); assert.match(h.text(), /仅手动同步/); assert.match(h.text(), /不导入配图/);
  assert.ok(h.button('立即同步'));
});

test('an incomplete configuration is explained and offers no way to run', async () => {
  const h = harness({ mpSyncInfo: async () => ({ enabled: false, problems: ['TRACKING_MP_OWNER 未配置：同步的草稿须有明确的跟进归属'] }), mpSyncRun: async () => assert.fail('must not run') });
  await flush(); h.render();
  assert.match(h.text(), /配置不完整，尚未启用/); assert.match(h.text(), /TRACKING_MP_OWNER/); assert.equal(h.button('立即同步'), undefined);
  const off = harness({ mpSyncInfo: async () => ({ enabled: false }), mpSyncRun: async () => assert.fail('must not run') });
  await flush(); off.render();
  assert.match(off.text(), /尚未启用/); assert.equal(off.button('立即同步'), undefined);
});

test('one click runs one sync, reports the outcome and refreshes the list', async () => {
  let runs = 0, release;
  const h = harness({ mpSyncInfo: async () => enabledInfo({ interval_minutes: 60, import_images: true }), mpSyncRun: () => { runs++; return new Promise(r => { release = r; }); } });
  await flush(); h.render();
  assert.match(h.text(), /每 60 分钟自动同步/); assert.match(h.text(), /导入配图/);
  const button = h.button('立即同步'); button.props.onClick(); button.props.onClick(); h.render();
  assert.equal(runs, 1); assert.deepEqual(h.calls.filter(c => c[0] === 'busy'), [['busy', true]]); assert.match(h.text(), /正在同步/);
  release({ started_at: '2026-10-14T08:00:00Z', finished_at: '2026-10-14T08:00:05Z', counts: counts({ conflicts: 1, skipped: 1, images_imported: 2, image_failures: 1 }), problems: [{ ref: 'abc123def456', reason: 'edited_locally' }, { ref: '111111111111', reason: 'something_new' }] });
  await flush(); h.render();
  assert.deepEqual(h.calls.filter(c => c[0] === 'busy'), [['busy', true], ['busy', false]]); assert.ok(h.calls.some(c => c[0] === 'changed'));
  assert.match(h.text(), /新建草稿 1/); assert.match(h.text(), /需人工处理 1/); assert.match(h.text(), /配图导入 2，失败 1/);
  assert.match(h.text(), /文章 abc123def456：本地草稿已被人工修改/); assert.match(h.text(), /文章 111111111111：something_new/);
});

test('a failed run keeps the button and warns that drafts may exist', async () => {
  const h = harness({ mpSyncInfo: async () => enabledInfo(), mpSyncRun: async () => { throw new Error('公众号接口返回错误（错误码 48001）'); } });
  await flush(); h.render(); h.button('立即同步').props.onClick(); await flush(); h.render();
  assert.match(h.text(), /48001/); assert.match(h.text(), /草稿是否已生成请以素材列表为准/); assert.ok(h.button('立即同步')); assert.deepEqual(h.calls.filter(c => c[0] === 'busy'), [['busy', true], ['busy', false]]);
  assert.equal(h.calls.some(c => c[0] === 'changed'), false);
});

test('the last recorded run, including a failure, is shown when nothing was run now', async () => {
  const ok = harness({ mpSyncInfo: async () => enabledInfo({ last_run: { ok: true, started_at: 'a', finished_at: '2026-10-14T08:00:05Z', counts: counts({ failed: 2 }), problems: [{ ref: 'r1', reason: 'failed' }] } }), mpSyncRun: async () => ({}) });
  await flush(); ok.render();
  assert.match(ok.text(), /最近一次同步/); assert.match(ok.text(), /失败 2/); assert.match(ok.text(), /处理失败，下次同步会重试/);
  const bad = harness({ mpSyncInfo: async () => enabledInfo({ last_run: { ok: false, error: '公众号接口返回错误，请核对账号是否已认证并具备发布接口权限', wechat_errcode: 48001, started_at: 'a', finished_at: '2026-10-14T08:00:05Z', counts: counts(), problems: [] } }), mpSyncRun: async () => ({}) });
  await flush(); bad.render();
  assert.match(bad.text(), /具备发布接口权限/); assert.match(bad.text(), /微信错误码 48001/);
  const never = harness({ mpSyncInfo: async () => enabledInfo(), mpSyncRun: async () => ({}) });
  await flush(); never.render();
  assert.match(never.text(), /尚未同步过/);
});

test('every reason the server can report has a plain explanation', () => {
  const model = load('src/pages/shareTracking/mpSync.tsx', { react: require('react'), '../../api/shareTracking': { tracking: {} }, './model': load('src/pages/shareTracking/model.ts') });
  for (const reason of ['deleted_upstream', 'title_empty', 'empty_body', 'invalid', 'revoked', 'edited_locally', 'failed']) assert.ok(model.mpProblemText[reason], reason);
  assert.equal(model.mpCountsText(counts()), '读取 3 篇 · 新建草稿 1 · 更新草稿 1 · 无变化 1');
});

test('the API calls the sync endpoints, and a run may wait longer than other calls', async () => {
  const calls = [];
  const api = load('src/api/shareTracking.ts', { '../utils/request': { request: async (...args) => { calls.push(args); return { data: { enabled: true } }; } } });
  await api.tracking.mpSyncInfo(); await api.tracking.mpSyncRun();
  assert.deepEqual(calls.map(c => [c[0], c[1].method]), [['/api/v1/management/tracking/mp-sync', 'GET'], ['/api/v1/management/tracking/mp-sync', 'POST']]);
  assert.equal(calls[1][1].headers['X-Tracking-Request'], '1');
  // A run is not cut off after the 20 seconds other calls get.
  const delays = [];
  const timed = load('src/api/shareTracking.ts', { '../utils/request': { request: async () => ({ data: {} }) }, globals: { setTimeout: (fn, ms) => { delays.push(ms); return setTimeout(fn, 1e9).unref(); }, clearTimeout } });
  await timed.tracking.mpSyncInfo(); await timed.tracking.mpSyncRun();
  assert.deepEqual(delays, [20000, 330000]);
});
