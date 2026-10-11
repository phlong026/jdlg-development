// Exercise the real component handlers with controlled React hooks and deferred APIs.
// All network, confirmation and export calls are local fakes; no backend writes or tokens.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';

const flush = () => new Promise(resolve => setImmediate(resolve));
const clone = value => JSON.parse(JSON.stringify(value));
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const response = data => ({ ok: true, json: async () => ({ code: 0, data }) });
const file = id => ({ id, name: `file-${id}.png`, key: `assets/file-${id}.png`, size: 125, type: 'image', url: `https://fixture.invalid/${id}`, upload_time: '2026-10-11T00:00:00Z' });
const overview = { total_users: 12, total_revenue: 125, total_tasks: 13, total_templates: 14, designer_count: 15 };
function report(params, extra = {}) {
  return { report_type: params.report_type, period: params.period, start_date: params.start_date || '2026-10-01', end_date: params.end_date || '2026-10-11', columns: [{ key: 'new_users', label: '新增用户' }], rows: [{ new_users: 12 }], summary: { new_users: 12 }, ...extra };
}
function nodes(tree, predicate) {
  const result = [];
  const walk = node => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== 'object') return;
    if (predicate(node)) result.push(node);
    for (const key of ['children', 'controls', 'actions', 'summary']) walk(node.props?.[key]);
  };
  walk(tree); return result;
}
function textOf(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  if (typeof node !== 'object') return String(node);
  return ['children', 'controls', 'actions', 'summary'].map(key => textOf(node.props?.[key])).join('');
}
function one(runtime, predicate, label) { const found = nodes(runtime.tree, predicate); assert.equal(found.length, 1, label); return found[0]; }
function button(runtime, label) { return one(runtime, node => node.type === 'button' && textOf(node) === label, `button ${label}`); }
function input(runtime, label) { return one(runtime, node => node.props?.['aria-label'] === label, `input ${label}`); }
function click(runtime, label) { const node = button(runtime, label); assert.notEqual(node.props.disabled, true); const promise = node.props.onClick(); runtime.render(); return promise; }
function change(runtime, label, value) { input(runtime, label).props.onChange({ target: { value } }); runtime.render(); }
function toggle(runtime, id) { input(runtime, `选择文件 file-${id}.png（ID: ${id}）`).props.onChange(); runtime.render(); }
function all(runtime) { return input(runtime, '选择本页全部文件'); }
function selected(runtime) { return nodes(runtime.tree, node => node.type === 'input' && node.props.type === 'checkbox' && node.props.checked).map(node => node.props['aria-label']); }
async function settle(runtime) { await flush(); await flush(); runtime.render(); }
function harness(name) {
  const runtime = { slots: [], cursor: 0, pending: [], tree: null, updates: 0 };
  const requests = [], writes = [], errors = [], recovered = [], confirms = [], alerts = [], exports = [], opens = [];
  const settings = { confirm: () => true };
  const fakeReact = {
    ...React,
    useState(initial) {
      const index = runtime.cursor++;
      if (!(index in runtime.slots)) runtime.slots[index] = typeof initial === 'function' ? initial() : initial;
      return [runtime.slots[index], value => { runtime.updates++; runtime.slots[index] = typeof value === 'function' ? value(runtime.slots[index]) : value; }];
    },
    useRef(initial) { const index = runtime.cursor++; if (!(index in runtime.slots)) runtime.slots[index] = { current: initial }; return runtime.slots[index]; },
    useMemo(factory) { return factory(); },
    useEffect(effect, deps) {
      const index = runtime.cursor++, previous = runtime.slots[index];
      if (!previous || !deps || deps.some((value, i) => !Object.is(value, previous.deps[i]))) {
        runtime.slots[index] = { deps, cleanup: previous?.cleanup };
        runtime.pending.push(() => { runtime.slots[index].cleanup?.(); runtime.slots[index].cleanup = effect(); });
      }
    },
  };
  fakeReact.default = fakeReact; fakeReact.__esModule = true;
  const api = {
    getReportCenterOverview: async () => overview,
    getReportCenterReport: params => { const request = { ...deferred(), params: clone(params) }; requests.push(request); return request.promise; },
    getReportCenterExportUrl: params => { exports.push(clone(params)); return `https://fixture.invalid/export?${new URLSearchParams(params)}`; },
  };
  const module = { exports: {} };
  const context = vm.createContext({
    module, exports: module.exports, URLSearchParams, Error, console,
    FormData: class { constructor() { this.entries = []; } append(name, value) { this.entries.push([name, value]); } },
    alert: message => alerts.push(message),
    window: { location: { pathname: `/admin/${name}` }, confirm: message => { confirms.push(message); return settings.confirm(message); }, open: (...args) => opens.push(args) },
    fetch: (url, options) => {
      const request = { ...deferred(), url, options: clone(options) };
      (options?.method ? writes : requests).push(request);
      return request.promise;
    },
    require(id) {
      if (id === 'react') return fakeReact;
      if (id.endsWith('.scss')) return {};
      if (id === 'react-icons/fi') return new Proxy({}, { get: (_, key) => key });
      if (id === '../component/managementSearchPanel') return { __esModule: true, default: 'ManagementSearchPanel' };
      if (id === '../utils/pageLoadError') return { reportPageLoadError: (error, source) => errors.push({ error, source }), reportPageLoadRecovered: source => recovered.push(source) };
      if (id === '../api/reportCenter') return api;
      if (id === '../config/api') return { API_BASE_URL: 'https://fixture.invalid', API_ENDPOINTS: { OSS: { LIST: '/oss', DELETE: id => `/oss/${id}`, BATCH_DELETE: '/oss/batch', UPLOAD: '/oss/upload' } } };
      throw new Error(`Unexpected import ${id}`);
    },
  });
  const source = fs.readFileSync(new URL(`../src/pages/${name}.tsx`, import.meta.url), 'utf8');
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText, context);
  runtime.render = () => { runtime.cursor = 0; runtime.tree = module.exports.default({}); return runtime.tree; };
  runtime.effects = () => runtime.pending.splice(0).forEach(effect => effect());
  runtime.unmount = () => runtime.slots.forEach(slot => { if (slot && typeof slot.cleanup === 'function') slot.cleanup(); });
  runtime.render(); runtime.effects(); runtime.render();
  return { runtime, requests, writes, settings, errors, recovered, confirms, alerts, exports, opens, api };
}
async function resolveFiles(h, ids = [1, 2], index = h.requests.length - 1, total = 60) { h.requests[index].resolve(response({ list: ids.map(file), total })); await settle(h.runtime); }
async function resolveReport(h, index = h.requests.length - 1, extra = {}) { const request = h.requests[index]; request.resolve(report(request.params, extra)); await settle(h.runtime); }

// M01: actual scope/selection/confirmation lifecycle, including calls before a rerender.
test('OSS current-page selection uses identities, partial state, and clears on same-sized replacement', async () => {
  const h = harness('oss'), page = h.runtime;
  await resolveFiles(h);
  assert.equal(all(page).props.checked, false);
  toggle(page, 1);
  assert.equal(all(page).props['aria-checked'], 'mixed');
  const dom = {}; all(page).ref(dom); assert.equal(dom.indeterminate, true);
  all(page).props.onChange(); page.render(); assert.equal(all(page).props.checked, true);
  const staleToggle = input(page, '选择文件 file-1.png（ID: 1）').props.onChange;
  click(page, '下一页'); page.effects(); page.render();
  assert.equal(nodes(page.tree, node => node.props?.type === 'checkbox').length, 0);
  await resolveFiles(h, [3, 4]);
  assert.equal(selected(page).length, 0); assert.equal(all(page).props.checked, false);
  staleToggle(); page.render(); assert.equal(selected(page).length, 0);
  all(page).props.onChange(); page.render(); assert.equal(all(page).props.checked, true);
  assert.match(textOf(page.tree), /本页已勾选\s*2\s*个/);
  all(page).props.onChange(); page.render(); assert.equal(selected(page).length, 0);
});

test('OSS page, keyword, type, source, reset and refresh all invalidate selections before old handlers can submit', async () => {
  for (const action of ['page', 'keyword', 'type', 'source', 'reset', 'refresh']) {
    const h = harness('oss'), page = h.runtime;
    await resolveFiles(h); toggle(page, 1);
    const staleDelete = button(page, '批量删除 (1)').props.onClick;
    if (action === 'page') button(page, '下一页').props.onClick();
    if (action === 'keyword') { change(page, '文件名或路径', 'query'); button(page, '搜索文件').props.onClick(); }
    if (action === 'type') input(page, '文件类型').props.onChange({ target: { value: 'video' } });
    if (action === 'source') input(page, '文件来源').props.onChange({ target: { value: 'admin_upload' } });
    if (action === 'reset') button(page, '重置筛选').props.onClick();
    if (action === 'refresh') button(page, '刷新').props.onClick();
    staleDelete(); assert.equal(h.writes.length, 0, action); assert.equal(h.confirms.length, 0, action);
    page.render(); page.effects(); page.render(); await resolveFiles(h, [3, 4]);
    assert.equal(selected(page).length, 0, action); assert.equal(all(page).props.checked, false, action);
    h.runtime.unmount();
  }
});

test('OSS slow list successes and failures cannot replace newer scope or clear its loading state', async () => {
  const h = harness('oss'), page = h.runtime;
  change(page, '文件类型', 'video'); page.effects(); page.render();
  h.requests[0].resolve(response({ list: [file(1)], total: 1 })); await settle(page);
  assert.match(textOf(page.tree), /加载中/); assert.doesNotMatch(textOf(page.tree), /file-1/);
  await resolveFiles(h, [3, 4], 1);
  change(page, '文件来源', 'admin_upload'); page.effects(); page.render();
  change(page, '文件来源', 'user_ai'); page.effects(); page.render();
  await resolveFiles(h, [7], 3);
  h.requests[2].reject(new Error('late failure')); await settle(page);
  assert.match(textOf(page.tree), /file-7/); assert.equal(h.errors.length, 0);
  assert.equal(all(page).props.checked, false);
});

test('OSS failed list is unavailable, retry recovers its own error and only successful empty list says empty', async () => {
  const h = harness('oss'), page = h.runtime;
  h.requests[0].reject(new Error('offline')); await settle(page);
  assert.match(textOf(page.tree), /文件列表加载失败/); assert.doesNotMatch(textOf(page.tree), /暂无文件数据/);
  assert.equal(nodes(page.tree, node => node.type === 'input' && node.props.type === 'checkbox').length, 0);
  click(page, '刷新'); await resolveFiles(h, [], 1, 0);
  assert.match(textOf(page.tree), /暂无文件数据/); assert.equal(h.errors[0].source, h.recovered.at(-1));
});

test('OSS business errors and missing payloads are failures, while a successful nullable empty list remains compatible', async () => {
  const h = harness('oss'), page = h.runtime;
  h.requests[0].resolve({ ok: true, json: async () => ({ code: 1, msg: 'service failed' }) }); await settle(page);
  assert.match(textOf(page.tree), /文件列表加载失败/); assert.doesNotMatch(textOf(page.tree), /暂无文件数据/);
  click(page, '刷新'); h.requests[1].resolve(response(null)); await settle(page);
  assert.match(textOf(page.tree), /文件列表响应无效/); assert.doesNotMatch(textOf(page.tree), /暂无文件数据/);
  click(page, '刷新'); h.requests[2].resolve(response({ list: null, total: 0 })); await settle(page);
  assert.match(textOf(page.tree), /暂无文件数据/); assert.doesNotMatch(textOf(page.tree), /文件列表加载失败/);
});

test('OSS confirmation names exactly the submitted frozen files; repeat batch and single handlers are mutually exclusive', async () => {
  const h = harness('oss'), page = h.runtime;
  await resolveFiles(h); toggle(page, 1); toggle(page, 2);
  const batch = button(page, '批量删除 (2)').props.onClick;
  const single = nodes(page.tree, node => node.type === 'button' && node.props.title === '删除')[0].props.onClick;
  const toggleFirst = input(page, '选择文件 file-1.png（ID: 1）').props.onChange;
  h.settings.confirm = () => { toggleFirst(); return true; };
  const first = batch(); batch(); single(); page.render();
  assert.equal(h.confirms.length, 1); assert.equal(h.writes.length, 1);
  assert.deepEqual(JSON.parse(h.writes[0].options.body), { ids: [1, 2] });
  for (const id of [1, 2]) { assert.match(h.confirms[0], new RegExp(`file-${id}\\.png（ID: ${id}）`)); assert.match(h.confirms[0], new RegExp(`路径：assets/file-${id}\\.png`)); }
  assert.equal(button(page, '删除中...').props.disabled, true); assert.equal(all(page).props.disabled, true);
  h.writes[0].resolve(response({})); await first; await settle(page);
  assert.equal(h.requests.length, 2); assert.equal(selected(page).length, 0);
});

test('OSS cancel, failed delete retry, single identity confirmation and changed-scope completion remain safe', async () => {
  const h = harness('oss'), page = h.runtime;
  await resolveFiles(h); toggle(page, 1);
  h.settings.confirm = () => false; await click(page, '批量删除 (1)'); assert.equal(h.writes.length, 0);
  h.settings.confirm = () => true;
  const single = nodes(page.tree, node => node.type === 'button' && node.props.title === '删除')[0].props.onClick;
  const first = single(); single(); assert.equal(h.writes.length, 1);
  assert.match(h.confirms.at(-1), /file-1\.png（ID: 1）/); assert.equal(h.writes[0].url, 'https://fixture.invalid/oss/1'); assert.equal(h.writes[0].options.method, 'DELETE');
  h.writes[0].reject(new Error('write failed')); await first; page.render();
  const retry = single(); assert.equal(h.writes.length, 2);
  change(page, '文件来源', 'user_ai'); page.effects(); page.render(); await resolveFiles(h, [8]);
  const readCount = h.requests.length;
  h.writes[1].resolve(response({})); await retry; await settle(page);
  assert.equal(h.requests.length, readCount); assert.match(textOf(page.tree), /file-8/); assert.doesNotMatch(textOf(page.tree), /file-1/);
});

test('OSS scope changes during confirmation cancel submission and unmounted list responses are ignored', async () => {
  const h = harness('oss'), page = h.runtime;
  await resolveFiles(h); toggle(page, 1);
  h.settings.confirm = () => { input(page, '文件类型').props.onChange({ target: { value: 'video' } }); return true; };
  await button(page, '批量删除 (1)').props.onClick(); assert.equal(h.writes.length, 0);
  page.render(); page.effects(); page.render(); page.unmount(); const updates = page.updates;
  h.requests.at(-1).resolve(response({ list: [file(5)], total: 1 })); await flush();
  assert.equal(page.updates, updates);
});

// R01: render through the component rather than testing a copied formatter.
test('report rate fields format 0/1/0.5 explicitly; null/string and non-rate values keep their original meaning', async () => {
  const h = harness('reportCenter'), page = h.runtime;
  const keys = ['success_rate', 'pay_conversion_rate', 'template_conversion_rate', 'success_amount', 'new_users'];
  await resolveReport(h, 0, {
    columns: keys.map(key => ({ key, label: key })),
    rows: [0, 1, 0.5, null, '0.5', 'offline'].map(value => Object.fromEntries(keys.map(key => [key, value]))),
    summary: { success_rate: 0, pay_conversion_rate: 1, template_conversion_rate: 0.5, success_amount: 100, new_users: 0.5, active_designers: 7 },
  });
  const body = one(page, node => node.type === 'tbody', 'report tbody');
  const rows = nodes(body, node => node.type === 'tr').map(row => nodes(row, node => node.type === 'td').map(textOf));
  assert.deepEqual(rows, [
    ['0.0%', '0.0%', '0.0%', '0', '0'], ['100.0%', '100.0%', '100.0%', '1', '1'],
    ['50.0%', '50.0%', '50.0%', '0.5', '0.5'], ['-', '-', '-', '-', '-'],
    ['0.5', '0.5', '0.5', '0.5', '0.5'], ['offline', 'offline', 'offline', 'offline', 'offline'],
  ]);
  const chips = nodes(page.tree, node => node.props?.className === 'summary-chip').map(textOf);
  assert.deepEqual(chips, ['成功率0.0%', '支付转化率100.0%', '模板转化率50.0%', '成交金额100', '新增用户0.5', '活跃设计师数7']);
});

test('report export freezes actual successful dates, rejects unapplied dates and an old handler before render', async () => {
  const h = harness('reportCenter'), page = h.runtime;
  assert.equal(button(page, '导出当前报表').props.disabled, true); await resolveReport(h);
  click(page, '导出当前报表'); assert.deepEqual(h.exports[0], { report_type: 'user_growth', period: 'daily', start_date: '2026-10-01', end_date: '2026-10-11' });
  const oldExport = button(page, '导出当前报表').props.onClick;
  input(page, '开始日期').props.onChange({ target: { value: '2026-10-03' } }); oldExport(); assert.equal(h.exports.length, 1);
  page.render(); assert.match(textOf(page.tree), /条件已更改/); assert.match(textOf(page.tree), /2026-10-01 ~ 2026-10-11/);
  assert.equal(button(page, '导出当前报表').props.disabled, true); assert.equal(h.requests.length, 1);
  change(page, '结束日期', '2026-10-05'); click(page, '应用筛选'); oldExport(); assert.equal(h.exports.length, 1);
  assert.equal(button(page, '导出当前报表').props.disabled, true);
  await resolveReport(h, 1, { start_date: '2026-10-02', end_date: '2026-10-06' });
  click(page, '导出当前报表'); assert.deepEqual(h.exports[1], { report_type: 'user_growth', period: 'daily', start_date: '2026-10-02', end_date: '2026-10-06' });
  assert.match(textOf(page.tree), /已预览：.*2026-10-02 ~ 2026-10-06/);
});

test('report reversed dates show inline validation and cannot fetch or export even by direct handlers', async () => {
  const h = harness('reportCenter'), page = h.runtime; await resolveReport(h);
  const staleApply = button(page, '应用筛选').props.onClick, staleExport = button(page, '导出当前报表').props.onClick;
  change(page, '开始日期', '2026-10-20'); change(page, '结束日期', '2026-10-01');
  assert.match(textOf(page.tree), /开始日期不能晚于结束日期/); assert.equal(input(page, '开始日期').props['aria-invalid'], true);
  for (const label of ['应用筛选', '刷新', '导出当前报表']) assert.equal(button(page, label).props.disabled, true);
  staleApply(); staleExport(); assert.equal(h.requests.length, 1); assert.equal(h.exports.length, 0);
  change(page, '结束日期', '2026-10-21'); click(page, '应用筛选'); assert.equal(h.requests.length, 2);
});

test('report loading and failure never expose old export, zeros or false empty data; retry recovers', async () => {
  const h = harness('reportCenter'), page = h.runtime; await resolveReport(h);
  const oldExport = button(page, '导出当前报表').props.onClick;
  click(page, '刷新'); oldExport(); assert.equal(h.exports.length, 0); assert.match(textOf(page.tree), /正在加载预览/);
  h.requests[1].reject(new Error('offline')); await settle(page);
  assert.equal(button(page, '导出当前报表').props.disabled, true); oldExport(); assert.equal(h.exports.length, 0);
  assert.match(textOf(page.tree), /报表加载失败/); assert.doesNotMatch(textOf(page.tree), /暂无报表数据|当前没有摘要数据/);
  assert.deepEqual(nodes(page.tree, node => node.props?.className === 'stat-value').map(textOf), ['—', '—', '—', '—', '—']);
  click(page, '刷新'); await resolveReport(h, 2, { rows: [], summary: {} });
  assert.match(textOf(page.tree), /暂无报表数据/); assert.equal(h.errors[0].source, h.recovered.at(-1));
  assert.equal(button(page, '导出当前报表').props.disabled, false);
});

test('report type and period late responses cannot mismatch the table, summary, label and export', async () => {
  const h = harness('reportCenter'), page = h.runtime;
  change(page, '报表类型', 'ai_success_rate'); page.effects(); page.render();
  change(page, '统计周期', 'monthly'); page.effects(); page.render();
  await resolveReport(h, 2, { columns: [{ key: 'success_rate', label: '成功率' }], rows: [{ success_rate: 1 }], summary: { success_rate: 1 } });
  h.requests[0].resolve(report(h.requests[0].params, { rows: [{ new_users: 9999 }] }));
  h.requests[1].reject(new Error('late failed daily report')); await settle(page);
  assert.match(textOf(page.tree), /AI任务成功率 · 月报/); assert.match(textOf(page.tree), /100.0%/); assert.doesNotMatch(textOf(page.tree), /9999|加载失败/);
  click(page, '导出当前报表'); assert.equal(h.exports[0].report_type, 'ai_success_rate'); assert.equal(h.exports[0].period, 'monthly'); assert.equal(h.errors.length, 0);
});

test('report draft edits during loading invalidate late success; same-scope repeated requests only apply newest', async () => {
  const h = harness('reportCenter'), page = h.runtime;
  change(page, '开始日期', '2026-10-03');
  await resolveReport(h, 0); assert.equal(button(page, '导出当前报表').props.disabled, true); assert.match(textOf(page.tree), /尚未成功预览/);
  const apply = button(page, '应用筛选').props.onClick;
  apply(); apply(); page.render(); assert.equal(h.requests.length, 3);
  await resolveReport(h, 2, { rows: [{ new_users: 22 }] });
  await resolveReport(h, 1, { rows: [{ new_users: 11 }] });
  const cells = nodes(one(page, node => node.type === 'tbody', 'tbody'), node => node.type === 'td').map(textOf);
  assert.deepEqual(cells, ['22']); click(page, '导出当前报表'); assert.equal(h.exports[0].start_date, '2026-10-03');
});

test('report mismatched response or missing actual dates cannot be exported; unmount drops pending responses', async () => {
  for (const extra of [{ report_type: 'wrong_type' }, { period: 'monthly' }, { start_date: '' }, { end_date: '2026-09-01' }]) {
    const h = harness('reportCenter'); await resolveReport(h, 0, extra);
    assert.match(textOf(h.runtime.tree), /报表加载失败/); button(h.runtime, '导出当前报表').props.onClick(); assert.equal(h.exports.length, 0);
  }
  const h = harness('reportCenter'); h.runtime.unmount(); const updates = h.runtime.updates;
  h.requests[0].resolve(report(h.requests[0].params)); await flush(); await flush();
  assert.equal(h.runtime.updates, updates); assert.equal(h.errors.length, 0); assert.equal(h.recovered.length, 0);
});

test('OSS deleting the sole second-page file corrects 21→20 to page one with cleared selection', async () => {
  const h = harness('oss'), page = h.runtime;
  await resolveFiles(h, [1], 0, 21);
  click(page, '下一页'); page.effects(); page.render(); await resolveFiles(h, [21], 1, 21);
  toggle(page, 21); const remove = click(page, '批量删除 (1)');
  assert.deepEqual(JSON.parse(h.writes[0].options.body), { ids: [21] });
  h.writes[0].resolve(response({})); await remove; await settle(page);
  assert.equal(new URL(h.requests[2].url).searchParams.get('page'), '2');
  await resolveFiles(h, [], 2, 20); page.effects(); page.render();
  assert.equal(new URL(h.requests[3].url).searchParams.get('page'), '1');
  await resolveFiles(h, [1, 2], 3, 20);
  assert.match(textOf(page.tree), /file-1/); assert.equal(selected(page).length, 0);
  assert.doesNotMatch(textOf(page.tree), /第 2 页|暂无文件数据/);
});

test('OSS shrinking an out-of-range list to zero recovers page one and ignores obsolete page corrections', async () => {
  const h = harness('oss'), page = h.runtime;
  await resolveFiles(h, [1], 0, 21); click(page, '下一页'); page.effects(); page.render();
  change(page, '文件来源', 'user_ai'); page.effects(); page.render();
  await resolveFiles(h, [9], 2, 1); const readCount = h.requests.length;
  await resolveFiles(h, [], 1, 0); page.effects(); page.render();
  assert.equal(h.requests.length, readCount); assert.match(textOf(page.tree), /file-9/);
  click(page, '刷新'); await resolveFiles(h, [], 3, 0);
  assert.match(textOf(page.tree), /暂无文件数据/); assert.equal(new URL(h.requests[3].url).searchParams.get('page'), '1');
});

test('OSS native picker opens without posting; upload lock blocks duplicate change and picker activation until retry', async () => {
  const h = harness('oss'), page = h.runtime; await resolveFiles(h); click(page, '上传文件');
  let picks = 0; const fileInput = input(page, '上传 OSS 文件'); fileInput.ref.current = { click() { picks++; } };
  const picker = button(page, '选择文件'); assert.equal(picker.props.type, 'button'); assert.equal(picker.props.disabled, false);
  picker.props.onClick(); picker.props.onClick(); assert.equal(picks, 2); assert.equal(h.writes.length, 0);
  await fileInput.props.onChange({ target: { files: [], value: '' } }); assert.equal(h.writes.length, 0);
  const event = { target: { files: [{ name: 'fixture.png' }], value: 'fixture.png' } };
  const pending = fileInput.props.onChange(event); picker.props.onClick(); await fileInput.props.onChange(event);
  assert.equal(picks, 2); assert.equal(h.writes.length, 1); assert.equal(h.writes[0].url, 'https://fixture.invalid/oss/upload');
  page.render(); assert.equal(button(page, '上传中...').props.disabled, true);
  h.writes[0].reject(new Error('fixture retry')); await pending; page.render(); assert.equal(event.target.value, '');
  click(page, '选择文件'); assert.equal(picks, 3); assert.equal(h.writes.length, 1);
  page.unmount(); button(page, '选择文件').props.onClick(); assert.equal(picks, 3);
});

test('OSS canceled/reopened picker sessions reject old picker, change and close handlers before and after render', async () => {
  const h = harness('oss'), page = h.runtime; await resolveFiles(h); click(page, '上传文件');
  let picks = 0; const oldInput = input(page, '上传 OSS 文件'); oldInput.ref.current = { click() { picks++; } };
  const oldPicker = button(page, '选择文件').props.onClick;
  const oldClose = input(page, '关闭上传文件').props.onClick;
  const open = button(page, '上传文件').props.onClick;
  oldClose(); oldPicker(); await oldInput.props.onChange({ target: { files: [{ name: 'canceled.png' }], value: 'canceled.png' } });
  assert.equal(picks, 0); assert.equal(h.writes.length, 0);
  open(); page.render(); oldClose(); oldPicker(); await oldInput.props.onChange({ target: { files: [{ name: 'old.png' }], value: 'old.png' } });
  page.render(); assert.equal(nodes(page.tree, node => node.props?.className === 'modal-overlay').length, 1);
  assert.equal(picks, 0); assert.equal(h.writes.length, 0);
  // Repeated open does not replace an already active session.
  const currentPicker = button(page, '选择文件').props.onClick; open(); currentPicker(); assert.equal(picks, 1);
  const currentInput = input(page, '上传 OSS 文件'); await currentInput.props.onChange({ target: { files: [], value: '' } });
  assert.equal(h.writes.length, 0); assert.equal(h.alerts.length, 0);
});

test('OSS pending upload synchronously blocks close, overlay dismissal, reopen, duplicate change and picker; failure allows retry', async () => {
  const h = harness('oss'), page = h.runtime; await resolveFiles(h); click(page, '上传文件');
  let picks = 0; const pickerInput = input(page, '上传 OSS 文件'); pickerInput.ref.current = { click() { picks++; } };
  const close = input(page, '关闭上传文件').props.onClick, open = button(page, '上传文件').props.onClick, picker = button(page, '选择文件').props.onClick;
  const overlay = one(page, node => node.props?.className === 'modal-overlay', 'upload overlay').props.onClick;
  const event = { target: { files: [{ name: 'one.png' }], value: 'one.png' } };
  const pending = pickerInput.props.onChange(event); close(); overlay(); open(); picker(); await pickerInput.props.onChange(event);
  assert.equal(h.writes.length, 1); assert.equal(picks, 0); page.render();
  assert.equal(input(page, '关闭上传文件').props.disabled, true); assert.equal(button(page, '上传文件').props.disabled, true);
  h.writes[0].resolve({ ok: true, json: async () => ({ code: 1, msg: 'upload retry' }) }); await pending; page.render();
  assert.equal(h.alerts.at(-1), 'upload retry'); assert.equal(event.target.value, ''); assert.equal(input(page, '关闭上传文件').props.disabled, false);
  button(page, '选择文件').props.onClick(); assert.equal(picks, 1);
  const retry = input(page, '上传 OSS 文件').props.onChange(event); assert.equal(h.writes.length, 2);
  h.writes[1].resolve(response({ url: 'https://fixture.invalid/uploaded' })); await retry; page.render();
  assert.equal(nodes(page.tree, node => node.props?.className === 'modal-overlay').length, 0);
  assert.equal(h.alerts.filter(text => text === '上传成功').length, 1);
});

test('OSS upload completion refreshes current filters rather than its original list closure', async () => {
  const h = harness('oss'), page = h.runtime; await resolveFiles(h); click(page, '上传文件');
  const pending = input(page, '上传 OSS 文件').props.onChange({ target: { files: [{ name: 'one.png' }], value: 'one.png' } });
  change(page, '文件来源', 'user_ai'); page.effects(); page.render();
  change(page, '文件名或路径', 'new-scope'); click(page, '搜索文件'); page.effects(); page.render();
  const beforeRefresh = h.requests.length;
  h.writes[0].resolve(response({ url: 'https://fixture.invalid/uploaded' })); await pending; page.render();
  assert.equal(h.requests.length, beforeRefresh + 1);
  const params = new URL(h.requests.at(-1).url).searchParams;
  assert.equal(params.get('source_type'), 'user_ai'); assert.equal(params.get('keyword'), 'new-scope'); assert.equal(params.get('page'), '1');
  await resolveFiles(h, [9], h.requests.length - 1, 1);
  await resolveFiles(h, [8], beforeRefresh - 1, 1);
  assert.match(textOf(page.tree), /file-9/); assert.doesNotMatch(textOf(page.tree), /file-8/);
  assert.equal(h.writes[0].options.credentials, 'include'); assert.equal(h.writes[0].options.body.entries[0][0], 'file');
});

test('OSS unmount silences late upload success, business failure and network rejection without closing, updating or refreshing', async () => {
  for (const outcome of ['success', 'business', 'network']) {
    const h = harness('oss'), page = h.runtime; await resolveFiles(h); click(page, '上传文件');
    const pending = input(page, '上传 OSS 文件').props.onChange({ target: { files: [{ name: 'one.png' }], value: 'one.png' } });
    page.unmount(); const updates = page.updates, reads = h.requests.length;
    if (outcome === 'network') h.writes[0].reject(new Error('late upload'));
    else h.writes[0].resolve({ ok: true, json: async () => ({ code: outcome === 'success' ? 0 : 1, msg: 'late business', data: {} }) });
    await pending; await flush(); assert.equal(page.updates, updates, outcome); assert.equal(h.requests.length, reads, outcome); assert.equal(h.alerts.length, 0, outcome);
  }
});
