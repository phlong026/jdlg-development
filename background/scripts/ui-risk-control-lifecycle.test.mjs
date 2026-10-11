// Execute real TSX handlers with deterministic hooks and delayed read-only APIs.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';
import * as sass from 'sass';

const flush = () => new Promise(resolve => setImmediate(resolve));
const labels = { groups: '同设备账号', changes: '设备换绑', alerts: '风险告警', users: '风险用户' };
const keys = Object.keys(labels);
const overview = { shared_devices: 3, device_risk_users: 42, recent_device_changes: 5, abnormal_payments: 6, failed_tasks: 7 };
const user = id => ({ user_id: id, username: `user-${id}`, nickname: `昵称${id}` });
const item = (kind, id) => ({
  groups: { device_id: `device-${id}`, user_count: 2, users: [user(String(id)), user(String(id + 100))], latest_activity_at: '' },
  changes: { ...user(id), device_id: `device-${id}`, last_device_change_time: '2020-01-01T00:00:00Z' },
  alerts: { ...user(id), alert_type: 'payment', alert_count: 2, latest_time: '', detail: `alert-${id}` },
  users: { ...user(id), device_id: `device-${id}`, shared_device_count: 2, failed_task_count: 3, abnormal_payment_count: 2, last_device_change_time: '', risk_tags: ['近期换绑设备'] },
})[kind];
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function nodes(tree, predicate) {
  const found = [];
  function walk(node) {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== 'object') return;
    if (predicate(node)) found.push(node);
    walk(node.props?.children);
  }
  walk(tree); return found;
}
function textOf(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  if (typeof node !== 'object') return String(node);
  return textOf(node.props?.children);
}
function one(tree, predicate, label) { const result = nodes(tree, predicate); assert.equal(result.length, 1, label); return result[0]; }
const named = (page, label) => one(page.tree, node => node.props?.['aria-label'] === label, label);
const button = (page, label) => one(page.tree, node => node.type === 'button' && textOf(node) === label, label);
const input = page => one(page.tree, node => node.type === 'input', 'search field');
const table = (h, kind) => named(h.page, labels[kind]);
const total = (h, kind) => textOf(named(h.page, `${labels[kind]}分页`));
const metrics = h => nodes(h.page.tree, node => node.props?.className === 'stat-value').map(textOf);
function click(h, label, accessible = false) { const node = accessible ? named(h.page, label) : button(h.page, label); assert.notEqual(node.props.disabled, true, `${label} enabled`); node.props.onClick(); h.page.render(); }
function fill(h, value) { input(h.page).props.onChange({ target: { value } }); h.page.render(); }
function search(h, value, enter = false) { fill(h, value); if (enter) { input(h.page).props.onKeyDown({ key: 'Enter' }); h.page.render(); } else click(h, '搜索风险'); }
async function settle(h) { await flush(); await flush(); h.page.render(); }
function envelope(request, rows, total = rows.length) { return { list: rows, total, page: request.params.page, page_size: 10 }; }
async function resolveList(h, kind, ids = [1], total = ids.length, index = h.requests[kind].length - 1) {
  const request = h.requests[kind][index]; request.resolve(envelope(request, ids.map(id => item(kind, id)), total)); await settle(h);
}
async function loaded(h, total = 23) {
  h.requests.overview.at(-1).resolve(overview);
  for (const key of keys) { const request = h.requests[key].at(-1); request.resolve(envelope(request, Array.from({ length: Math.min(10, total) }, (_, i) => item(key, i + 1)), total)); }
  await settle(h);
}
function harness() {
  const page = { slots: [], cursor: 0, pending: [], tree: null, updates: 0 };
  const requests = Object.fromEntries(['overview', ...keys].map(key => [key, []]));
  const failures = [], recoveries = [], navigation = [], forbidden = [];
  const fakeReact = {
    ...React,
    useState(initial) { const i = page.cursor++; if (!(i in page.slots)) page.slots[i] = typeof initial === 'function' ? initial() : initial; return [page.slots[i], value => { page.updates++; page.slots[i] = typeof value === 'function' ? value(page.slots[i]) : value; }]; },
    useRef(initial) { const i = page.cursor++; if (!(i in page.slots)) page.slots[i] = { current: initial }; return page.slots[i]; },
    useEffect(effect, deps) { const i = page.cursor++, previous = page.slots[i]; if (!previous || deps.some((value, n) => !Object.is(value, previous.deps[n]))) { page.slots[i] = { deps, cleanup: previous?.cleanup, effect }; page.pending.push(() => { page.slots[i].cleanup?.(); page.slots[i].cleanup = effect(); }); } },
  };
  fakeReact.default = fakeReact; fakeReact.__esModule = true;
  const api = {};
  for (const [key, method] of Object.entries({ overview: 'getRiskControlOverview', groups: 'getRiskControlDeviceGroups', changes: 'getRiskControlDeviceChanges', alerts: 'getRiskControlAlerts', users: 'getRiskControlUsers' })) {
    api[method] = params => { const request = { ...deferred(), params: params && JSON.parse(JSON.stringify(params)) }; requests[key].push(request); return request.promise; };
  }
  function compile(file) {
    const module = { exports: {} };
    const context = vm.createContext({
      module, exports: module.exports, console, Error,
      fetch: (...args) => { forbidden.push(args); throw new Error('Unexpected direct network request'); },
      require(name) {
        if (name === 'react') return fakeReact;
        if (name.endsWith('.scss')) return {};
        if (name === 'react-icons/fi') return new Proxy({}, { get: (_, name) => name });
        if (name === 'react-router-dom') return { useNavigate: () => path => navigation.push(path) };
        if (name === '../utils/pageLoadError') return { reportPageLoadError: (error, source) => failures.push({ error, source }), reportPageLoadRecovered: source => recoveries.push(source) };
        if (name === '../component/managementSearchPanel') return { __esModule: true, default: compile('../src/component/managementSearchPanel.tsx').default };
        if (name === '../api/riskControl') return new Proxy(api, { get(target, key) { if (!(key in target)) { forbidden.push(key); throw new Error(`Unexpected API ${String(key)}`); } return target[key]; } });
        throw new Error(`Unexpected import ${name}`);
      },
    });
    vm.runInContext(ts.transpileModule(fs.readFileSync(new URL(file, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText, context);
    return module.exports;
  }
  const Component = compile('../src/pages/riskControl.tsx').default;
  function expand(node) {
    if (Array.isArray(node)) return node.map(expand);
    if (!node || typeof node !== 'object') return node;
    if (typeof node.type === 'function') return expand(node.type(node.props));
    return { ...node, props: { ...node.props, children: expand(node.props?.children) } };
  }
  page.render = () => { page.cursor = 0; page.tree = expand(Component({})); };
  page.effects = () => page.pending.splice(0).forEach(effect => effect());
  page.unmount = () => page.slots.forEach(slot => { if (slot && typeof slot.cleanup === 'function') slot.cleanup(); });
  page.strictReplay = () => { page.unmount(); page.slots.forEach(slot => { if (slot?.effect) slot.cleanup = slot.effect(); }); page.render(); };
  page.render(); page.effects(); page.render();
  return { page, requests, failures, recoveries, navigation, forbidden };
}

test('risk tables request real pages independently, show server totals and fixed ten-row page size', async () => {
  const h = harness(); await loaded(h, 23);
  for (const kind of keys) {
    assert.deepEqual(h.requests[kind][0].params, { page: 1, page_size: 10, keyword: '' });
    assert.match(total(h, kind), /共 23 条 · 第 1 \/ 3 页 · 每页 10 条/);
    const counts = Object.fromEntries(Object.entries(h.requests).map(([key, value]) => [key, value.length]));
    click(h, `${labels[kind]}下一页`, true);
    for (const key of Object.keys(h.requests)) assert.equal(h.requests[key].length, counts[key] + (key === kind ? 1 : 0));
    assert.deepEqual(h.requests[kind].at(-1).params, { page: 2, page_size: 10, keyword: '' });
    assert.match(textOf(table(h, kind)), /加载中/); assert.doesNotMatch(textOf(table(h, kind)), /昵称1/);
    await resolveList(h, kind, Array.from({ length: 10 }, (_, i) => i + 11), 23);
    assert.match(textOf(table(h, kind)), /昵称11/);
    click(h, `${labels[kind]}下一页`, true); await resolveList(h, kind, [21, 22, 23], 23);
    assert.equal(named(h.page, `${labels[kind]}下一页`).props.disabled, true);
    assert.match(total(h, kind), /第 3 \/ 3 页/);
    click(h, `${labels[kind]}上一页`, true); await resolveList(h, kind, [11], 23);
  }
  assert.equal(h.requests.overview.length, 1); assert.deepEqual(metrics(h), ['3', '42', '5', '6 / 7']);
});

test('search/Enter and reset apply to all tables, reset all pages and leave the global overview independent', async () => {
  const h = harness(); await loaded(h);
  for (const key of keys) { click(h, `${labels[key]}下一页`, true); await resolveList(h, key, [11], 23); }
  fill(h, '  device-42  '); assert.equal(h.requests.groups.length, 2, 'typing alone does not apply filters');
  input(h.page).props.onKeyDown({ key: 'Enter' }); h.page.render();
  for (const key of keys) { assert.deepEqual(h.requests[key].at(-1).params, { page: 1, page_size: 10, keyword: 'device-42' }); await resolveList(h, key, [42], 11); }
  assert.equal(h.requests.overview.length, 1); assert.match(textOf(h.page.tree), /关键词：device-42/);
  click(h, '风险用户下一页', true); await resolveList(h, 'users', [43], 11);
  assert.equal(h.requests.users.at(-1).params.keyword, 'device-42');
  search(h, 'device-42'); // Same keyword still intentionally reloads all first pages.
  for (const key of keys) assert.equal(h.requests[key].at(-1).params.page, 1);
  click(h, '重置筛选');
  for (const key of keys) assert.deepEqual(h.requests[key].at(-1).params, { page: 1, page_size: 10, keyword: '' });
  assert.equal(input(h.page).props.value, ''); assert.equal(h.requests.overview.length, 1);
});

test('one failed table never hides successful modules; scoped retry recovers only its own failure source', async () => {
  const h = harness(); h.requests.overview[0].resolve(overview);
  for (const key of ['groups', 'changes', 'users']) await resolveList(h, key, [1], 12);
  h.requests.alerts[0].reject(new Error('alerts offline')); await settle(h);
  assert.match(textOf(table(h, 'alerts')), /风险告警加载失败：alerts offline/);
  assert.doesNotMatch(textOf(table(h, 'alerts')), /暂无风险告警/); assert.match(total(h, 'alerts'), /共 — 条/);
  for (const key of ['groups', 'changes', 'users']) assert.match(textOf(table(h, key)), /昵称1/);
  assert.deepEqual(metrics(h), ['3', '42', '5', '6 / 7']);
  const counts = Object.fromEntries(Object.entries(h.requests).map(([key, value]) => [key, value.length]));
  click(h, '重试风险告警');
  for (const key of Object.keys(h.requests)) assert.equal(h.requests[key].length, counts[key] + (key === 'alerts' ? 1 : 0));
  await resolveList(h, 'alerts', [], 0);
  assert.match(textOf(table(h, 'alerts')), /暂无风险告警/); assert.match(total(h, 'alerts'), /共 0 条/);
  assert.equal(h.failures[0].source, '加载风险告警失败'); assert.equal(h.recoveries.at(-1), h.failures[0].source);
});

test('loading, genuine zero, failed overview and successful empty tables remain distinct', async () => {
  const h = harness();
  assert.deepEqual(metrics(h), ['—', '—', '—', '— / —']);
  for (const key of keys) { assert.match(textOf(table(h, key)), /加载中/); assert.doesNotMatch(textOf(table(h, key)), /暂无/); assert.equal(named(h.page, `${labels[key]}下一页`).props.disabled, true); }
  h.requests.overview[0].reject(new Error('overview offline'));
  for (const key of keys) await resolveList(h, key, [], 0);
  assert.deepEqual(metrics(h), ['—', '—', '—', '— / —']); assert.match(textOf(h.page.tree), /全局概览加载失败/);
  for (const key of keys) assert.match(total(h, key), /共 0 条/);
  click(h, '重试全局概览'); h.requests.overview[1].resolve(Object.fromEntries(Object.keys(overview).map(key => [key, 0]))); await settle(h);
  assert.deepEqual(metrics(h), ['0', '0', '0', '0 / 0']); assert.equal(h.recoveries.at(-1), '加载风控全局概览失败');
  assert.equal(h.requests.groups.length, 1);
});

test('malformed list/count/page/item responses fail explicitly instead of looking empty, then retry normally', async () => {
  const malformed = [null, {}, { list: null, total: 0 }, { list: [], total: '0' }, { list: [], total: -1 }, { list: [], total: NaN }, { list: [], total: 0, page: 2 }, { list: [], total: 0, page_size: 20 }, { list: [null], total: 1 }, { list: [], total: 3 }, { list: [item('groups', 1)], total: 0 }, { list: [{ ...item('groups', 1), users: null }], total: 1 }];
  for (const value of malformed) {
    const h = harness(); const request = h.requests.groups[0];
    request.resolve(value && { page: 1, page_size: 10, ...value }); await settle(h);
    assert.match(textOf(table(h, 'groups')), /加载失败/); assert.doesNotMatch(textOf(table(h, 'groups')), /暂无同设备/);
    assert.match(total(h, 'groups'), /共 — 条/); assert.equal(named(h.page, '同设备账号下一页').props.disabled, true);
    click(h, '重试同设备账号'); await resolveList(h, 'groups', [], 0); assert.match(textOf(table(h, 'groups')), /暂无同设备/);
  }
  for (const kind of ['changes', 'alerts', 'users']) {
    const h = harness(); const request = h.requests[kind][0]; request.resolve(envelope(request, [{ ...item(kind, 1), nickname: {} }], 1)); await settle(h);
    assert.match(textOf(table(h, kind)), /返回数据格式异常/);
  }
});

test('missing or malformed global metrics are unknown while list data remains usable', async () => {
  for (const bad of [null, {}, { ...overview, shared_devices: '3' }, { ...overview, failed_tasks: -1 }, { ...overview, abnormal_payments: Infinity }]) {
    const h = harness(); h.requests.overview[0].resolve(bad); await resolveList(h, 'users', [7], 1);
    assert.deepEqual(metrics(h), ['—', '—', '—', '— / —']); assert.match(textOf(h.page.tree), /全局概览加载失败/); assert.match(textOf(table(h, 'users')), /昵称7/);
  }
});

test('filters immediately invalidate old success/failure and old pagination/retry handlers before rendering', async () => {
  const h = harness(); await loaded(h, 23);
  const oldPage = named(h.page, '同设备账号下一页').props.onClick;
  oldPage(); oldPage(); assert.equal(h.requests.groups.length, 2, 'same-frame pagination is single-flight'); h.page.render();
  h.requests.alerts[0].reject(new Error('already completed promise'));
  fill(h, 'newest'); const apply = button(h.page, '搜索风险').props.onClick; apply();
  const reads = h.requests.groups.length; oldPage(); assert.equal(h.requests.groups.length, reads);
  h.requests.groups[1].resolve(envelope(h.requests.groups[1], [item('groups', 99)], 23));
  await settle(h); assert.match(textOf(table(h, 'groups')), /加载中/); assert.doesNotMatch(textOf(table(h, 'groups')), /昵称99/);
  await resolveList(h, 'groups', [7], 1); assert.match(textOf(table(h, 'groups')), /昵称7/);
  search(h, 'older'); const old = Object.fromEntries(keys.map(key => [key, h.requests[key].at(-1)])); search(h, 'latest');
  for (const key of keys) await resolveList(h, key, [8], 1);
  old.groups.reject(new Error('old failure')); old.changes.resolve(envelope(old.changes, [item('changes', 99)], 1)); old.alerts.reject(new Error('old alerts')); old.users.resolve(envelope(old.users, [item('users', 99)], 1)); await settle(h);
  for (const key of keys) { assert.match(textOf(table(h, key)), /昵称8/); assert.doesNotMatch(textOf(table(h, key)), /昵称99|old failure|old alerts/); }
  assert.equal(h.failures.length, 0);
  search(h, 'failed'); h.requests.alerts.at(-1).reject(new Error('retryable')); await settle(h); const retry = button(h.page, '重试风险告警').props.onClick;
  fill(h, 'replacement'); button(h.page, '搜索风险').props.onClick(); const count = h.requests.alerts.length; retry(); assert.equal(h.requests.alerts.length, count);
});

test('shrinking totals correct only the affected table to a valid last page and retain its keyword', async () => {
  const h = harness(); await loaded(h, 23); search(h, 'target');
  for (const key of keys) await resolveList(h, key, [1], 23);
  click(h, '风险用户下一页', true); await resolveList(h, 'users', [11], 23);
  click(h, '风险用户下一页', true); const before = h.requests.users.length; await resolveList(h, 'users', [], 12);
  assert.equal(h.requests.users.length, before + 1); assert.deepEqual(h.requests.users.at(-1).params, { page: 2, page_size: 10, keyword: 'target' });
  await resolveList(h, 'users', [11, 12], 12); assert.match(total(h, 'users'), /共 12 条 · 第 2 \/ 2 页/);
  for (const key of ['groups', 'changes', 'alerts']) assert.equal(h.requests[key].length, 2);
  click(h, '刷新'); await resolveList(h, 'users', [], 0); assert.equal(h.requests.users.at(-1).params.page, 1); await resolveList(h, 'users', [], 0);
  assert.match(total(h, 'users'), /共 0 条 · 第 1 \/ 1 页/);
});

test('refresh preserves each page/filter, clears only recovered sources and supports repeated retries', async () => {
  const h = harness(); await loaded(h); search(h, 'scope');
  for (const key of keys) await resolveList(h, key, [1], 23);
  click(h, '设备换绑下一页', true); await resolveList(h, 'changes', [11], 23);
  click(h, '刷新'); assert.equal(h.requests.overview.length, 2);
  for (const key of keys) assert.deepEqual(h.requests[key].at(-1).params, { page: key === 'changes' ? 2 : 1, page_size: 10, keyword: 'scope' });
  h.requests.alerts.at(-1).reject(new Error('alerts')); h.requests.groups.at(-1).reject(new Error('groups')); await settle(h);
  click(h, '重试风险告警'); h.requests.alerts.at(-1).reject(new Error('retry failed')); await settle(h); assert.match(textOf(table(h, 'alerts')), /retry failed/);
  const count = h.recoveries.length; click(h, '重试风险告警'); await resolveList(h, 'alerts', [2], 1);
  assert.deepEqual(h.recoveries.slice(count), ['加载风险告警失败']); assert.match(textOf(table(h, 'groups')), /同设备账号加载失败/);
});

test('Strict Mode replay and unmount ignore every late request without state updates, reports or corrective reads', async () => {
  const h = harness(); h.page.strictReplay();
  for (const key of Object.keys(h.requests)) assert.equal(h.requests[key].length, 2);
  await loaded(h, 12);
  h.requests.overview[0].resolve({ ...overview, shared_devices: 999 });
  for (const key of keys) if (key === 'groups') h.requests[key][0].reject(new Error('old replay')); else h.requests[key][0].resolve(envelope(h.requests[key][0], [item(key, 99)], 1));
  await settle(h); assert.deepEqual(metrics(h), ['3', '42', '5', '6 / 7']); assert.equal(h.failures.length, 0);
  click(h, '风险用户下一页', true); h.page.unmount();
  const updates = h.page.updates, reports = h.failures.length + h.recoveries.length, reads = h.requests.users.length;
  h.requests.users.at(-1).resolve(envelope(h.requests.users.at(-1), [], 0)); await settle(h);
  assert.equal(h.page.updates, updates); assert.equal(h.failures.length + h.recoveries.length, reports); assert.equal(h.requests.users.length, reads);
  for (const outcome of ['resolve', 'reject']) {
    const fresh = harness(); fresh.page.unmount(); const count = fresh.page.updates;
    for (const key of Object.keys(fresh.requests)) {
      const request = fresh.requests[key][0];
      if (outcome === 'reject') request.reject(new Error('late unmount'));
      else request.resolve(key === 'overview' ? overview : envelope(request, [item(key, 1)], 1));
    }
    await settle(fresh); assert.equal(fresh.page.updates, count); assert.equal(fresh.failures.length + fresh.recoveries.length, 0);
  }
});

test('alert types navigate only to supported read-only destinations and unknown types never fall back to tasks', async () => {
  const h = harness(); await loaded(h, 1);
  search(h, 'routes'); const request = h.requests.alerts.at(-1);
  request.resolve(envelope(request, [
    { ...item('alerts', 1), alert_type: 'payment' },
    { ...item('alerts', 2), alert_type: 'task' },
    { ...item('alerts', 3), alert_type: 'new-kind' },
    { ...item('alerts', 4), alert_type: '' },
  ], 4)); await settle(h);
  const alertRows = nodes(table(h, 'alerts'), node => node.type === 'tr').slice(1);
  assert.match(textOf(alertRows[0]), /支付异常/); assert.match(textOf(alertRows[1]), /任务异常/);
  assert.match(textOf(alertRows[2]), /未知类型（new-kind）/); assert.match(textOf(alertRows[3]), /未知类型（空值）/);
  function rowClick(row, label) { one(row, node => node.type === 'button' && textOf(node) === label, label).props.onClick(); }
  rowClick(alertRows[0], '打开订单列表（未筛选）'); rowClick(alertRows[1], '打开AI任务列表（未筛选）');
  for (const row of alertRows.slice(2)) assert.equal(nodes(row, node => node.type === 'button' && /订单列表|AI任务列表/.test(textOf(node))).length, 0);
  rowClick(alertRows[2], '查看用户详情'); rowClick(alertRows[3], '打开工单中心（未筛选）');
  assert.deepEqual(h.navigation, ['/recharge', '/ai-tasks', '/user-workbench?userId=3', '/support-tickets']);
  await resolveList(h, 'changes', [5], 1); rowClick(nodes(table(h, 'changes'), node => node.type === 'tr')[1], '打开工单中心（未筛选）');
  assert.equal(h.navigation.at(-1), '/support-tickets'); assert.doesNotMatch(textOf(h.page.tree), /转异常工单/); assert.deepEqual(h.forbidden, []);
});

test('copy accurately separates all-time device rows, seven-day alerts, global overview and search scopes', async () => {
  const h = harness(); await loaded(h, 1); const text = textOf(h.page.tree);
  assert.match(text, /全局概览不受关键词筛选影响/); assert.match(text, /已绑定设备用户/); assert.doesNotMatch(text, /设备风险用户/);
  assert.match(text, /近 30 天内/); assert.match(text, /不限近 30 天/); assert.match(text, /近 7 天按用户和类型汇总，次数 ≥ 2/);
  assert.match(text, /失败与待支付/); assert.match(text, /风险告警仅支持用户名或昵称/);
  assert.match(text, /有设备换绑记录/); assert.doesNotMatch(text, /近期换绑设备/); assert.match(text, /不创建工单或执行账号处置/);
});

test('risk pagination and errors stay route-scoped, wrapping and accessible without global styles', () => {
  const css = sass.compile(new URL('../src/pages/riskControl.scss', import.meta.url).pathname).css;
  assert.match(css, /data-admin-page="\/risk-control"/); assert.match(css, /\.risk-pagination[^}]*flex-wrap: wrap/s);
  assert.match(css, /\.risk-pagination-actions button[^}]*min-height: 44px/s);
  const h = harness(); assert.equal(nodes(h.page.tree, node => node.type === 'nav' && node.props?.['aria-label']?.endsWith('分页')).length, 4);
  assert.equal(nodes(h.page.tree, node => node.type === 'table' && node.props['aria-busy'] === true).length, 4);
});
