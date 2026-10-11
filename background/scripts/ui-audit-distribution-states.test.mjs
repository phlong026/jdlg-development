// Actual TSX event handlers with controlled hooks and delayed fake reads. No real HTTP/API writes.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';
import * as sass from 'sass';
const flush = () => new Promise(resolve => setImmediate(resolve));
const clone = value => JSON.parse(JSON.stringify(value));
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function nodes(tree, predicate) { const found = []; function walk(node) { if (Array.isArray(node)) return node.forEach(walk); if (!node || typeof node !== 'object') return; if (predicate(node)) found.push(node); for (const key of ['children', 'controls', 'actions', 'summary']) walk(node.props?.[key]); } walk(tree); return found; }
function textOf(node) { if (node == null || typeof node === 'boolean') return ''; if (Array.isArray(node)) return node.map(textOf).join(''); if (typeof node !== 'object') return String(node); return ['children', 'controls', 'actions', 'summary'].map(key => textOf(node.props?.[key])).join(''); }
function one(page, predicate, label) { const found = nodes(page.tree, predicate); assert.equal(found.length, 1, label); return found[0]; }
function button(page, label) { return one(page, node => node.type === 'button' && textOf(node) === label, `button ${label}`); }
function named(page, label) { return one(page, node => node.props?.['aria-label'] === label, label); }
function click(page, label) { const node = button(page, label); assert.notEqual(node.props.disabled, true); const promise = node.props.onClick(); page.render(); return promise; }
function press(page, label) { const node = named(page, label); assert.notEqual(node.props.disabled, true); node.props.onClick(); page.render(); page.effects(); page.render(); }
function change(page, label, value) { named(page, label).props.onChange({ target: { value } }); page.render(); page.effects(); page.render(); }
async function settle(page) { await flush(); await flush(); page.render(); }
const audit = (id, extra = {}) => ({ id, user_id: id, username: `admin-${id}`, method: 'POST', path: `/api/v1/management/users/${id}/stones`, target_type: 'users', target_id: String(id), status: 200, duration_ms: 17, ip: '', created_at: '2026-10-11T00:00:00Z', ...extra });
const inviter = id => ({ user_id: id, username: `inviter-${id}`, invite_code: `code-${id}`, invite_count: 2, paid_invite_count: 1, total_reward_amount: 150, month_reward_amount: 30 });
const reward = id => ({ id, user_id: id, username: `reward-${id}`, type: 'invite', amount: 37, scene_desc: 'fixture scene' });
const overview = { inviter_count: 120, total_invite_count: 200, paid_invite_count: 70, total_reward_amount: 12345, month_reward_amount: 678, month_new_invites: 11 };
function harness(name) {
  const page = { slots: [], cursor: 0, pending: [], tree: null, updates: 0 };
  const requests = [], failures = [], recoveries = [];
  const fakeReact = { ...React,
    useState(initial) { const i = page.cursor++; if (!(i in page.slots)) page.slots[i] = typeof initial === 'function' ? initial() : initial; return [page.slots[i], value => { page.updates++; page.slots[i] = typeof value === 'function' ? value(page.slots[i]) : value; }]; },
    useRef(initial) { const i = page.cursor++; if (!(i in page.slots)) page.slots[i] = { current: initial }; return page.slots[i]; },
    useEffect(effect, deps) { const i = page.cursor++, previous = page.slots[i]; if (!previous || deps.some((value, n) => !Object.is(value, previous.deps[n]))) { page.slots[i] = { deps, cleanup: previous?.cleanup }; page.pending.push(() => { page.slots[i].cleanup?.(); page.slots[i].cleanup = effect(); }); } },
  };
  fakeReact.default = fakeReact; fakeReact.__esModule = true;
  const api = {};
  for (const method of ['getDistributionOverview', 'getDistributionInviters', 'getDistributionRewards']) api[method] = params => { const request = { ...deferred(), method, params: params ? clone(params) : undefined }; requests.push(request); return request.promise; };
  const module = { exports: {} };
  const context = vm.createContext({ module, exports: module.exports, Error, URLSearchParams, console,
    fetch(url, options) { assert.equal(options.method, undefined, 'only GET reads expected'); const request = { ...deferred(), url, params: Object.fromEntries(new URL(url).searchParams) }; requests.push(request); return request.promise; },
    require(id) {
      if (id === 'react') return fakeReact;
      if (id.endsWith('.scss')) return {};
      if (id === 'react-icons/fi') return new Proxy({}, { get: (_, name) => name });
      if (id === '../component/managementSearchPanel') return { __esModule: true, default: 'ManagementSearchPanel' };
      if (id === '../utils/pageLoadError') return { reportPageLoadError: (error, source) => failures.push({ error, source }), reportPageLoadRecovered: source => recoveries.push(source) };
      if (id === '../api/distribution') return api;
      if (id === '../config/api') return { API_BASE_URL: 'https://fixture.invalid', API_ENDPOINTS: { AUDIT: { LIST: '/audit' } } };
      throw new Error(`Unexpected import ${id}`);
    },
  });
  const source = fs.readFileSync(new URL(`../src/pages/${name}.tsx`, import.meta.url), 'utf8');
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText, context);
  page.render = () => { page.cursor = 0; page.tree = module.exports.default({}); };
  page.effects = () => page.pending.splice(0).forEach(effect => effect());
  page.unmount = () => page.slots.forEach(slot => { if (slot && typeof slot.cleanup === 'function') slot.cleanup(); });
  page.render(); page.effects(); page.render();
  return { page, requests, failures, recoveries };
}
async function auditResult(h, index, rows, total) { h.requests[index].resolve({ ok: true, json: async () => ({ code: 0, data: { list: rows, total } }) }); await settle(h.page); }
function request(h, kind, offset = -1) { const found = h.requests.filter(req => req.method === `getDistribution${kind}`); return found.at(offset); }
async function tableResult(h, kind, rows, total, req = request(h, kind), extra = {}) { req.resolve({ list: rows, total, page: req.params.page, page_size: req.params.page_size, ...extra }); await settle(h.page); }
async function loadDistribution(h, total = 120) { request(h, 'Overview').resolve(overview); await tableResult(h, 'Inviters', [inviter(1)], total); await tableResult(h, 'Rewards', [reward(1)], total); }
function searchDistribution(h, keyword) { change(h.page, '分销搜索', keyword); click(h.page, '搜索'); h.page.effects(); h.page.render(); }

test('audit renders neutral HTTP methods with real badge styles and owns its cold-entry search styles', async () => {
  const h = harness('logs'); await auditResult(h, 0, [audit(1), audit(2, { status: 403, method: 'PATCH' }), audit(3, { status: 500, method: 'DELETE' }), audit(4, { method: 'OPTIONS' })], 4);
  const cells = nodes(h.page.tree, node => node.type === 'td').map(textOf);
  assert.ok(cells.includes('POST')); assert.ok(cells.includes('OPTIONS')); assert.equal(cells.some(value => value === '创建'), false);
  assert.equal(nodes(h.page.tree, node => node.props?.className === 'level-badge level-info').length, 2);
  assert.equal(nodes(h.page.tree, node => node.props?.className === 'level-badge level-warning').length, 1);
  assert.equal(nodes(h.page.tree, node => node.props?.className === 'level-badge level-error').length, 1);
  assert.equal(nodes(h.page.tree, node => node.type === 'ManagementSearchPanel').length, 1);
  const source = fs.readFileSync(new URL('../src/pages/logs.tsx', import.meta.url), 'utf8');
  const panel = fs.readFileSync(new URL('../src/component/managementSearchPanel.tsx', import.meta.url), 'utf8');
  assert.match(source, /import ManagementSearchPanel from '\.\.\/component\/managementSearchPanel'/); assert.match(panel, /import '\.\/managementSearchPanel.scss'/);
  const css = sass.compileString(fs.readFileSync(new URL('../src/pages/logs.scss', import.meta.url), 'utf8')).css;
  assert.match(css, /\.level-badge\.level-error/); assert.match(css, /data-admin-page="\/logs".*\.audit-path/);
});

test('audit filters apply server parameters, same-query search retries, and older results never win', async () => {
  const h = harness('logs');
  change(h.page, '审计操作人', ' alice '); click(h.page, '查询'); h.page.effects(); h.page.render();
  assert.equal(h.requests[1].params.username, 'alice');
  change(h.page, '审计资源', 'ai'); change(h.page, '审计请求方法', 'POST');
  assert.equal(h.requests[3].params.target_type, 'ai'); assert.equal(h.requests[3].params.method, 'POST');
  await auditResult(h, 3, [audit(7)], 1);
  h.requests[0].reject(new Error('late error')); await auditResult(h, 1, [audit(1)], 1); await auditResult(h, 2, [audit(2)], 1);
  assert.match(textOf(h.page.tree), /admin-7/); assert.doesNotMatch(textOf(h.page.tree), /admin-1|admin-2/); assert.equal(h.failures.length, 0);
  click(h.page, '查询'); assert.equal(h.requests.length, 5); assert.equal(h.requests[4].params.username, 'alice');
  await auditResult(h, 4, [], 0); click(h.page, '重置筛选'); h.page.effects(); h.page.render();
  assert.deepEqual(h.requests[5].params, { page: '1', page_size: '20' });
});

test('audit error, retry, genuine empty and zero are distinct; pagination corrects a shrunken final page', async () => {
  const h = harness('logs'); h.requests[0].reject(new Error('offline')); await settle(h.page);
  assert.match(textOf(h.page.tree), /审计加载失败/); assert.doesNotMatch(textOf(h.page.tree), /暂无操作记录/); assert.equal(button(h.page, '下一页').props.disabled, true);
  click(h.page, '重新加载审计'); await auditResult(h, 1, [audit(1)], 21);
  assert.equal(h.failures[0].source, h.recoveries.at(-1)); click(h.page, '下一页'); h.page.effects(); h.page.render();
  assert.equal(h.requests[2].params.page, '2'); await auditResult(h, 2, [], 20); h.page.effects(); h.page.render();
  assert.equal(h.requests[3].params.page, '1'); await auditResult(h, 3, [], 0); assert.match(textOf(h.page.tree), /暂无操作记录/); assert.match(textOf(h.page.tree), /第 1 \/ 1 页/);
});

test('audit ignores malformed/business failure and unmounted requests instead of manufacturing valid rows', async () => {
  const h = harness('logs'); h.requests[0].resolve({ ok: true, json: async () => ({ code: 1, msg: 'business failed' }) }); await settle(h.page);
  assert.match(textOf(h.page.tree), /business failed/); click(h.page, '重新加载审计');
  h.requests[1].resolve({ ok: true, json: async () => ({ code: 0, data: null }) }); await settle(h.page); assert.match(textOf(h.page.tree), /审计列表响应无效/);
  click(h.page, '重新加载审计'); h.page.unmount(); const updates = h.page.updates;
  h.requests[2].resolve({ ok: true, json: async () => ({ code: 0, data: { list: [audit(9)], total: 1 } }) }); await flush(); await flush(); assert.equal(h.page.updates, updates);
});

test('distribution paginates beyond fifty independently, preserving real totals and global stone amounts', async () => {
  const h = harness('distribution'); await loadDistribution(h);
  assert.equal(h.requests.length, 3); assert.match(textOf(h.page.tree), /全局邀请统计.*灵石/); assert.match(textOf(h.page.tree), /累计奖励（灵石）/);
  assert.match(textOf(h.page.tree), /12345/); assert.match(textOf(h.page.tree), /37/);
  press(h.page, '邀请人下一页'); assert.equal(h.requests.length, 4); assert.equal(request(h, 'Inviters').params.page, 2); assert.equal(request(h, 'Inviters').params.page_size, 50);
  assert.match(textOf(h.page.tree), /reward-1/); await tableResult(h, 'Inviters', [inviter(51)], 120);
  press(h.page, '奖励下一页'); assert.equal(h.requests.length, 5); assert.equal(request(h, 'Rewards').params.page, 2); await tableResult(h, 'Rewards', [reward(51)], 120);
  assert.match(textOf(h.page.tree), /inviter-51/); assert.match(textOf(h.page.tree), /reward-51/);
  assert.equal(h.requests.filter(req => req.method === 'getDistributionOverview').length, 1);
});

test('distribution filter changes reset both pages, reject saved page handlers and ignore old responses before render', async () => {
  const h = harness('distribution'); await loadDistribution(h); press(h.page, '邀请人下一页'); await tableResult(h, 'Inviters', [inviter(51)], 120); press(h.page, '奖励下一页'); await tableResult(h, 'Rewards', [reward(51)], 120);
  const oldNext = named(h.page, '邀请人下一页').props.onClick;
  change(h.page, '分销搜索', 'new'); button(h.page, '搜索').props.onClick(); oldNext(); h.page.render(); h.page.effects(); h.page.render();
  assert.equal(request(h, 'Inviters').params.page, 1); assert.equal(request(h, 'Rewards').params.page, 1);
  assert.equal(request(h, 'Inviters').params.keyword, 'new'); assert.equal(request(h, 'Rewards').params.keyword, 'new');
  const oldInviters = request(h, 'Inviters'), oldRewards = request(h, 'Rewards');
  searchDistribution(h, 'newer'); await tableResult(h, 'Inviters', [inviter(9)], 1); await tableResult(h, 'Rewards', [reward(9)], 1);
  oldInviters.resolve({ list: [inviter(8)], total: 1, page: 1, page_size: 50 }); oldRewards.reject(new Error('old error')); await settle(h.page);
  assert.match(textOf(h.page.tree), /inviter-9/); assert.doesNotMatch(textOf(h.page.tree), /inviter-8|old error/); assert.equal(h.failures.length, 0);
});

test('distribution partial failures preserve other tables and each retry only recovers its own source', async () => {
  const h = harness('distribution'); request(h, 'Overview').reject(new Error('overview offline')); request(h, 'Inviters').reject(new Error('inviter offline')); await tableResult(h, 'Rewards', [reward(5)], 1);
  assert.match(textOf(h.page.tree), /reward-5/); assert.match(textOf(h.page.tree), /概览加载失败/); assert.match(textOf(h.page.tree), /邀请人排行加载失败/); assert.doesNotMatch(textOf(h.page.tree), /暂无邀请人数据/);
  const values = nodes(h.page.tree, node => node.props?.className === 'stat-value').map(textOf); assert.deepEqual(values, ['—', '—', '—', '—']);
  click(h.page, '重新加载邀请人'); assert.equal(h.requests.length, 4); await tableResult(h, 'Inviters', [], 0); assert.match(textOf(h.page.tree), /暂无邀请人数据/); assert.match(textOf(h.page.tree), /概览加载失败/);
  assert.equal(h.recoveries.at(-1), '获取邀请人排行失败'); click(h.page, '重新加载概览'); request(h, 'Overview').resolve(Object.fromEntries(Object.keys(overview).map(key => [key, 0]))); await settle(h.page);
  assert.deepEqual(nodes(h.page.tree, node => node.props?.className === 'stat-value').map(textOf), ['0', '0', '0', '0']); assert.match(textOf(h.page.tree), /reward-5/);
});

test('distribution newest same-scope reads win and page shrink correction stays isolated to its table', async () => {
  const h = harness('distribution'); await loadDistribution(h, 51);
  press(h.page, '邀请人下一页'); await tableResult(h, 'Inviters', [], 50); h.page.effects(); h.page.render();
  assert.equal(request(h, 'Inviters').params.page, 1); assert.equal(h.requests.filter(req => req.method === 'getDistributionRewards').length, 1);
  await tableResult(h, 'Inviters', [inviter(1)], 50);
  const refresh = button(h.page, '刷新').props.onClick; refresh(); const firstInviters = request(h, 'Inviters'), firstRewards = request(h, 'Rewards'), firstOverview = request(h, 'Overview');
  refresh(); await tableResult(h, 'Inviters', [inviter(7)], 1); await tableResult(h, 'Rewards', [reward(7)], 1); request(h, 'Overview').resolve(overview); await settle(h.page);
  firstInviters.resolve({ list: [inviter(6)], total: 1, page: 1, page_size: 50 }); firstRewards.reject(new Error('late')); firstOverview.reject(new Error('late overview')); await settle(h.page);
  assert.match(textOf(h.page.tree), /inviter-7/); assert.doesNotMatch(textOf(h.page.tree), /inviter-6|late/); assert.equal(h.failures.length, 0);
});

test('distribution invalid response is a failure, missing total stays unknown and wrong page cannot appear as current', async () => {
  const h = harness('distribution'); request(h, 'Overview').resolve(overview);
  request(h, 'Inviters').resolve(null); await tableResult(h, 'Rewards', [reward(1)], undefined);
  assert.match(textOf(h.page.tree), /邀请人排行加载失败/); assert.equal(named(h.page, '奖励下一页').props.disabled, true); assert.match(textOf(h.page.tree), /总数暂不可用/);
  click(h.page, '重新加载邀请人'); await tableResult(h, 'Inviters', [inviter(9)], 1, request(h, 'Inviters'), { page: 2 });
  assert.match(textOf(h.page.tree), /分页响应与当前查询不一致/); assert.doesNotMatch(textOf(h.page.tree), /inviter-9/);
});

test('distribution unmount ignores every pending module result and saved refresh handlers', async () => {
  const h = harness('distribution'), oldRefresh = button(h.page, '刷新中...').props.onClick;
  h.page.unmount(); const updates = h.page.updates, count = h.requests.length; oldRefresh();
  request(h, 'Overview').resolve(overview); request(h, 'Inviters').reject(new Error('late')); request(h, 'Rewards').resolve({ list: [], total: 0, page: 1, page_size: 50 });
  await flush(); await flush(); assert.equal(h.page.updates, updates); assert.equal(h.requests.length, count); assert.equal(h.failures.length, 0);
});
