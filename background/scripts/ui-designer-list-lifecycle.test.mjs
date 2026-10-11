// Real DesignerCenter handlers, deterministic React hooks and deferred fake APIs.
// No network, credentials, browser writes, or backend mutations are used.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';

const flush = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const designer = id => ({ user_id: id, username: `account-${id}`, display_name: `Designer ${id}`, designer_visible: true, service_enabled: true, total_works: 0, follow_count: 0, review_count: 0 });
const detail = id => ({ user_id: id, username: `account-${id}`, profile: { nickname: `Designer ${id}`, designer_visible: true, service_enabled: true }, stats: { total_works: 0, published_works: 0, follow_count: 0, review_count: 0, positive_review_count: 0, negative_review_count: 0, total_orders: 0, total_earnings: 0 }, works: [], reviews: [] });
const zeroSummary = { total_designers: 0, public_designers: 0, approved_designers: 0, designers_with_works: 0, service_enabled_designers: 0 };
const source = fs.readFileSync(new URL('../src/pages/designerCenter.tsx', import.meta.url), 'utf8');
const sourceJs = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText;
function nodes(tree, predicate) {
  const results = [];
  const walk = node => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== 'object') return;
    if (predicate(node)) results.push(node);
    for (const key of ['children', 'controls', 'actions', 'summary']) walk(node.props?.[key]);
  };
  walk(tree); return results;
}
function textOf(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  if (typeof node !== 'object') return String(node);
  return ['children', 'controls', 'actions', 'summary'].map(key => textOf(node.props?.[key])).join('');
}
function one(page, predicate, label) { const found = nodes(page.tree, predicate); assert.equal(found.length, 1, label); return found[0]; }
function button(page, label) { return one(page, node => node.type === 'button' && textOf(node) === label, `button ${label}`); }
function input(page, label) { return one(page, node => node.props?.['aria-label'] === label, `input ${label}`); }
function click(page, label) { const node = button(page, label); assert.notEqual(node.props.disabled, true, label); node.props.onClick(); page.render(); }
function change(page, label, value) { input(page, label).props.onChange({ target: { value } }); page.render(); }
function select(page, id) { one(page, node => node.props?.className?.startsWith('designer-list-item ') && node.key === String(id), `designer ${id}`).props.onClick(); page.render(); }
function actions(page) { return nodes(page.tree, node => node.type === 'button' && /^(?:上架主页|下架主页|暂停接单|恢复接单|提交中\.\.\.)$/.test(textOf(node))); }
function listItems(page) { return nodes(page.tree, node => node.props?.className?.startsWith('designer-list-item ')); }
function summaryValues(page) { return nodes(page.tree, node => node.props?.className === 'designer-summary-value').map(textOf); }
function panel(page) { return one(page, node => node.props?.className === 'designer-detail-panel section-card', 'detail panel'); }
function assertSelected(page, id) { assert.equal(textOf(nodes(panel(page), node => node.type === 'h3')[0]), `Designer ${id}`); }
async function settle(page) { await flush(); await flush(); page.render(); }
function harness() {
  const page = { slots: [], cursor: 0, pending: [], tree: null, updates: 0 };
  const lists = [], details = [], writes = [], errors = [], recovered = [], alerts = [];
  const fakeReact = {
    ...React,
    useState(initial) {
      const index = page.cursor++;
      if (!(index in page.slots)) page.slots[index] = typeof initial === 'function' ? initial() : initial;
      return [page.slots[index], value => { page.updates++; page.slots[index] = typeof value === 'function' ? value(page.slots[index]) : value; }];
    },
    useRef(initial) { const index = page.cursor++; if (!(index in page.slots)) page.slots[index] = { current: initial }; return page.slots[index]; },
    useMemo(factory) { return factory(); },
    useEffect(effect, deps) {
      const index = page.cursor++, previous = page.slots[index];
      if (!previous || !deps || deps.some((value, i) => !Object.is(value, previous.deps[i]))) {
        page.slots[index] = { deps, cleanup: previous?.cleanup };
        page.pending.push(() => { page.slots[index].cleanup?.(); page.slots[index].cleanup = effect(); });
      }
    },
  };
  fakeReact.default = fakeReact; fakeReact.__esModule = true;
  const api = {
    getDesignerManagementList(params) { const request = { ...deferred(), params: { ...params } }; lists.push(request); return request.promise; },
    getDesignerManagementDetail(id) { const request = { ...deferred(), id }; details.push(request); return request.promise; },
    updateDesignerVisibility(id, value) { const request = { ...deferred(), action: 'visibility', id, value }; writes.push(request); return request.promise; },
    updateDesignerServiceStatus(id, value) { const request = { ...deferred(), action: 'service', id, value }; writes.push(request); return request.promise; },
  };
  const module = { exports: {} };
  vm.runInNewContext(sourceJs, {
    module, exports: module.exports, URLSearchParams,
    console: { ...console, error() {} }, alert: message => alerts.push(message),
    window: { location: { search: '', pathname: '/designer-center' } },
    require(id) {
      if (id === 'react') return fakeReact;
      if (id === 'react-router-dom') return { useNavigate: () => () => {} };
      if (id === '../api/designers') return api;
      if (id === '../utils/pageLoadError') return { reportPageLoadError: (error, source) => errors.push({ error, source }), reportPageLoadRecovered: (source, path) => recovered.push({ source, path }) };
      if (id === 'react-icons/fi') return new Proxy({}, { get: (_, key) => key });
      if (id === '../component/managementSearchPanel') return { __esModule: true, default: 'ManagementSearchPanel' };
      if (id.endsWith('.scss')) return {};
      throw new Error(`Unexpected import ${id}`);
    },
  });
  page.render = () => { page.cursor = 0; page.tree = module.exports.default({}); };
  page.effects = () => page.pending.splice(0).forEach(effect => effect());
  page.unmount = () => page.slots.forEach(slot => slot?.cleanup?.());
  page.render(); page.effects(); page.render();
  return { page, lists, details, writes, errors, recovered, alerts };
}
async function resolveList(h, ids = [1, 2], index = h.lists.length - 1, extra = {}) {
  h.lists[index].resolve({ list: ids.map(designer), total: ids.length, page: 1, page_size: 20, ...extra }); await settle(h.page);
}
async function boot(ids = [1, 2], extra = {}) {
  const h = harness(); await resolveList(h, ids, 0, extra); h.page.effects(); h.page.render();
  h.details[0].resolve(detail(ids[0])); await settle(h.page); return h;
}

test('designer list distinguishes initial loading, failure, retry recovery and a successful empty result', async () => {
  const h = harness(), { page } = h;
  assert.match(textOf(page.tree), /设计师列表加载中/);
  assert.doesNotMatch(textOf(page.tree), /当前没有符合条件/);
  assert.deepEqual(summaryValues(page), Array(5).fill('—'));
  h.lists[0].reject(new Error('offline')); await settle(page);
  assert.match(textOf(page.tree), /设计师列表加载失败，请重试/);
  assert.doesNotMatch(textOf(page.tree), /当前没有符合条件/);
  assert.equal(listItems(page).length, 0); assert.equal(actions(page).length, 0);
  assert.deepEqual(summaryValues(page), Array(5).fill('—')); assert.equal(h.errors.length, 1);
  click(page, '重试列表'); await resolveList(h, [], 1, { summary: zeroSummary });
  assert.match(textOf(page.tree), /当前没有符合条件/);
  assert.doesNotMatch(textOf(page.tree), /设计师列表加载失败/);
  assert.deepEqual(summaryValues(page), Array(5).fill('0'));
  assert.deepEqual(h.recovered, [{ source: '加载设计师列表失败', path: '/designer-center' }]);
});

test('missing designer summary or individual metrics stay unknown while real zero remains zero', async () => {
  const h = harness(), { page } = h;
  await resolveList(h);
  assert.deepEqual(summaryValues(page), Array(5).fill('—'));
  click(page, '刷新'); await resolveList(h, [1], 1, { summary: { total_designers: 1, public_designers: 0, approved_designers: 0, designers_with_works: 0 } });
  assert.deepEqual(summaryValues(page), ['1', '0', '0', '0', '—']);
  assert.match(textOf(page.tree), /暂未获取统计数据/);
  click(page, '刷新'); await resolveList(h, [], 2, { summary: zeroSummary });
  assert.deepEqual(summaryValues(page), Array(5).fill('0'));
  assert.doesNotMatch(textOf(page.tree), /暂未获取统计数据/);
});

test('late list success or failure cannot replace the new scope, selection, loading or recovery state', async () => {
  const h = harness(), { page } = h;
  change(page, '主页公开状态', 'hidden'); page.effects(); page.render();
  await resolveList(h, [1], 0, { summary: zeroSummary });
  assert.match(textOf(page.tree), /设计师列表加载中/); assert.equal(listItems(page).length, 0);
  assert.equal(h.recovered.length, 0); assert.equal(h.details.length, 0);
  await resolveList(h, [2], 1); page.effects(); page.render();
  h.details[0].resolve(detail(2)); await settle(page); assertSelected(page, 2);
  change(page, '认证状态', 'approved'); page.effects(); page.render();
  change(page, '认证状态', 'rejected'); page.effects(); page.render();
  await resolveList(h, [3], 3); page.effects(); page.render();
  h.details[1].resolve(detail(3)); await settle(page); assertSelected(page, 3);
  h.lists[2].reject(new Error('obsolete error')); await settle(page);
  assertSelected(page, 3); assert.equal(h.errors.length, 0); assert.equal(h.recovered.length, 2);
  assert.equal(listItems(page)[0].key, '3');
});

test('filter handlers invalidate old list success and write handlers before rerender or effects', async () => {
  for (const filter of ['认证状态', '主页公开状态', '接单状态', 'keyword', 'reset', 'refresh']) {
    const h = await boot(), { page } = h;
    const staleAction = button(page, '下架主页').props.onClick;
    const staleSelect = listItems(page)[1].props.onClick;
    if (filter === 'keyword') {
      change(page, '设计师关键词', 'new scope'); button(page, '搜索设计师').props.onClick();
    } else if (filter === 'reset') button(page, '重置筛选').props.onClick();
    else if (filter === 'refresh') button(page, '刷新').props.onClick();
    else input(page, filter).props.onChange({ target: { value: filter === '认证状态' ? 'approved' : filter === '主页公开状态' ? 'hidden' : 'paused' } });
    staleAction(); staleSelect();
    assert.equal(h.writes.length, 0, filter);
    page.render(); page.effects(); page.render();
    await resolveList(h, [3]); page.effects(); page.render();
    h.details.at(-1).resolve(detail(3)); await settle(page);
    assertSelected(page, 3); staleAction(); staleSelect(); page.render();
    assert.equal(h.writes.length, 0, filter); assertSelected(page, 3);
    h.page.unmount();
  }

  const h = harness(), { page } = h;
  input(page, '主页公开状态').props.onChange({ target: { value: 'hidden' } });
  h.lists[0].resolve({ list: [designer(9)], summary: zeroSummary }); await settle(page);
  assert.equal(listItems(page).length, 0); assert.equal(h.recovered.length, 0); assert.equal(h.details.length, 0);
  page.effects(); page.render(); await resolveList(h, [2], 1);
  assert.equal(listItems(page)[0].key, '2');
});

test('a failed refresh removes previously actionable designers and does not manufacture zero statistics', async () => {
  const h = await boot(), { page } = h;
  const staleAction = button(page, '暂停接单').props.onClick;
  click(page, '刷新'); h.lists[1].reject(new Error('refresh failed')); await settle(page);
  staleAction(); assert.equal(h.writes.length, 0);
  assert.equal(actions(page).length, 0); assert.equal(listItems(page).length, 0);
  assert.deepEqual(summaryValues(page), Array(5).fill('—'));
  assert.match(textOf(page.tree), /设计师列表加载失败/); assert.doesNotMatch(textOf(page.tree), /当前没有符合条件/);
  click(page, '重试列表'); await resolveList(h, [2], 2); page.effects(); page.render();
  h.details.at(-1).resolve(detail(2)); await settle(page); assertSelected(page, 2);
});

test('write completion refreshes only current filters and preserves a newer selected designer', async () => {
  const h = await boot(), { page } = h;
  click(page, '下架主页'); assert.equal(h.writes[0].id, 1);
  change(page, '设计师关键词', 'new keyword'); click(page, '搜索设计师'); page.effects(); page.render();
  await resolveList(h, [2, 3], 1); page.effects(); page.render();
  h.details.at(-1).resolve(detail(2)); await settle(page);
  select(page, 3); page.effects(); page.render(); h.details.at(-1).resolve(detail(3)); await settle(page);
  h.writes[0].resolve(); await settle(page);
  assert.equal(h.lists.length, 3);
  assert.equal(h.lists[2].params.keyword, 'new keyword');
  await resolveList(h, [2, 3], 2); page.effects(); page.render();
  assertSelected(page, 3); assert.deepEqual(h.details.map(request => request.id), [1, 2, 3]);
  assert.equal(actions(page).every(node => !node.props.disabled), true);
  assert.equal(h.writes.length, 1); assert.equal(h.writes[0].value, false);
});

test('write completion superseded by another filter cannot commit its refresh or reload the old detail', async () => {
  const h = await boot(), { page } = h;
  click(page, '暂停接单'); h.writes[0].resolve(); await settle(page);
  assert.equal(h.lists.length, 2);
  change(page, '接单状态', 'paused'); page.effects(); page.render();
  await resolveList(h, [2], 2); page.effects(); page.render();
  h.details.at(-1).resolve(detail(2)); await settle(page);
  await resolveList(h, [1], 1);
  assertSelected(page, 2); assert.deepEqual(h.details.map(request => request.id), [1, 2]);
  assert.equal(h.lists[2].params.service_state, 'paused');
});

test('same-target writes reload matching details, block repeated submissions and recover after scoped inline failure', async () => {
  for (const label of ['下架主页', '暂停接单']) {
    const h = await boot(), { page } = h;
    const submit = button(page, label).props.onClick;
    submit(); submit(); assert.equal(h.writes.length, 1);
    h.writes[0].reject(new Error('write unavailable')); await settle(page);
    assert.match(textOf(panel(page)), /更新设计师.*状态失败，请重试/);
    assert.equal(h.alerts.length, 0); assert.equal(h.errors.length, 0);
    assert.equal(actions(page).every(node => !node.props.disabled), true);
    click(page, label); assert.equal(h.writes.length, 2);
    assert.doesNotMatch(textOf(panel(page)), /状态失败/);
    h.writes[1].resolve(); await settle(page); await resolveList(h, [1, 2], 1);
    assert.deepEqual(h.details.map(request => request.id), [1, 1]);
    h.details[1].resolve(detail(1)); await settle(page); assertSelected(page, 1);
    assert.equal(actions(page).every(node => !node.props.disabled), true);
    h.page.unmount();
  }
});

test('write failures are scoped to the original selection and filter generation', async () => {
  for (const mode of ['selection', 'filter', 'return to original']) {
    const h = await boot(), { page } = h;
    click(page, '下架主页');
    if (mode === 'filter') {
      change(page, '主页公开状态', 'hidden'); page.effects(); page.render(); await resolveList(h, [1], 1);
    } else select(page, 2);
    page.effects(); page.render(); h.details.at(-1).resolve(detail(mode === 'filter' ? 1 : 2)); await settle(page);
    if (mode === 'return to original') {
      select(page, 1); page.effects(); page.render(); h.details.at(-1).resolve(detail(1)); await settle(page);
    }
    h.writes[0].reject(new Error('old target failed')); await settle(page);
    assert.doesNotMatch(textOf(panel(page)), /状态失败/); assert.equal(h.alerts.length, 0);
    assert.equal(actions(page).every(node => !node.props.disabled), true);
    h.page.unmount();
  }
});

test('unmount suppresses list/detail/write results, errors, refreshes, and state cleanup', async () => {
  for (const outcome of ['resolve', 'reject']) {
    const h = harness(); h.page.unmount(); const updates = h.page.updates;
    h.lists[0][outcome](outcome === 'resolve' ? { list: [designer(1)], summary: zeroSummary } : new Error('late list'));
    await settle(h.page); assert.equal(h.page.updates, updates); assert.equal(h.errors.length, 0); assert.equal(h.recovered.length, 0);
    assert.equal(h.details.length, 0);
  }
  for (const outcome of ['resolve', 'reject']) {
    const h = harness(); await resolveList(h); h.page.effects(); h.page.render(); h.page.unmount(); const updates = h.page.updates;
    h.details[0][outcome](outcome === 'resolve' ? detail(1) : new Error('late detail'));
    await settle(h.page); assert.equal(h.page.updates, updates);
  }
  for (const outcome of ['resolve', 'reject']) {
    const h = await boot(); click(h.page, '下架主页'); h.page.unmount(); const updates = h.page.updates;
    h.writes[0][outcome](outcome === 'resolve' ? undefined : new Error('late write'));
    await settle(h.page); assert.equal(h.page.updates, updates); assert.equal(h.lists.length, 1); assert.equal(h.details.length, 1);
    assert.equal(h.alerts.length, 0); assert.equal(h.errors.length, 0);
  }
});


test('designer pagination uses server total with 20-row requests and keeps global statistics distinct', async () => {
  const h = await boot([1, 2], { total: 45, summary: { ...zeroSummary, total_designers: 120 } }), { page } = h;
  assert.equal(h.lists[0].params.page, 1); assert.equal(h.lists[0].params.page_size, 20);
  assert.match(textOf(page.tree), /共 45 位 · 第 1 \/ 3 页/);
  assert.match(textOf(page.tree), /全局设计师统计（不随筛选条件变化）/);
  assert.equal(summaryValues(page)[0], '120'); assert.equal(button(page, '上一页').props.disabled, true);
  const next = button(page, '下一页').props.onClick;
  const staleAction = button(page, '下架主页').props.onClick;
  next(); next(); staleAction(); page.render(); page.effects(); page.render();
  assert.equal(h.lists.length, 2); assert.equal(h.lists[1].params.page, 2); assert.equal(h.writes.length, 0);
  assert.equal(listItems(page).length, 0); assert.equal(actions(page).length, 0);
  await resolveList(h, [21, 22], 1, { total: 45 }); page.effects(); page.render();
  h.details.at(-1).resolve(detail(21)); await settle(page); assertSelected(page, 21);
  assert.match(textOf(page.tree), /共 45 位 · 第 2 \/ 3 页/);
  click(page, '下一页'); page.effects(); page.render(); await resolveList(h, [41], 2, { total: 45 });
  assert.equal(h.lists[2].params.page, 3); assert.equal(button(page, '下一页').props.disabled, true);
  assert.equal(button(page, '上一页').props.disabled, false);
  click(page, '上一页'); page.effects(); page.render(); assert.equal(h.lists[3].params.page, 2);
});

test('each designer filter, keyword search and reset returns pagination to page one', async () => {
  for (const filter of ['认证状态', '主页公开状态', '接单状态', 'keyword', 'same keyword', 'reset']) {
    const h = await boot([1], { total: 45 }), { page } = h;
    click(page, '下一页'); page.effects(); page.render(); await resolveList(h, [21], 1, { total: 45 });
    if (filter === 'keyword') { change(page, '设计师关键词', 'matched'); click(page, '搜索设计师'); }
    else if (filter === 'same keyword') click(page, '搜索设计师');
    else if (filter === 'reset') click(page, '重置筛选');
    else change(page, filter, filter === '认证状态' ? 'approved' : filter === '主页公开状态' ? 'hidden' : 'paused');
    page.effects(); page.render();
    assert.equal(h.lists.at(-1).params.page, 1, filter);
    assert.equal(listItems(page).length, 0, filter);
    await resolveList(h, [], h.lists.length - 1, { total: 0 });
    assert.match(textOf(page.tree), /共 0 位 · 第 1 \/ 1 页/);
    assert.equal(button(page, '上一页').props.disabled, true); assert.equal(button(page, '下一页').props.disabled, true);
    h.page.unmount();
  }
});

test('a shrinking final designer page automatically reloads its valid predecessor after a write', async () => {
  const h = await boot([1], { total: 41 }), { page } = h;
  click(page, '下一页'); page.effects(); page.render(); await resolveList(h, [21], 1, { total: 41 });
  click(page, '下一页'); page.effects(); page.render(); await resolveList(h, [41], 2, { total: 41 });
  page.effects(); page.render(); h.details.at(-1).resolve(detail(41)); await settle(page); assertSelected(page, 41);
  click(page, '下架主页'); h.writes[0].resolve(); await settle(page);
  assert.equal(h.lists[3].params.page, 3);
  await resolveList(h, [], 3, { total: 40 });
  assert.match(textOf(page.tree), /设计师列表加载中/); assert.doesNotMatch(textOf(page.tree), /当前没有符合条件/);
  page.effects(); page.render(); assert.equal(h.lists[4].params.page, 2);
  await resolveList(h, [21], 4, { total: 40 }); page.effects(); page.render();
  h.details.at(-1).resolve(detail(21)); await settle(page); assertSelected(page, 21);
  assert.match(textOf(page.tree), /共 40 位 · 第 2 \/ 2 页/);
  assert.equal(button(page, '下一页').props.disabled, true); assert.equal(h.writes.length, 1);
});

test('late page results cannot restore an old page and a pending write refreshes the current page', async () => {
  const h = await boot([1], { total: 45 }), { page } = h;
  click(page, '下架主页'); click(page, '下一页'); page.effects(); page.render();
  change(page, '认证状态', 'approved'); page.effects(); page.render();
  await resolveList(h, [3], 2, { total: 30 }); page.effects(); page.render();
  h.details.at(-1).resolve(detail(3)); await settle(page);
  await resolveList(h, [21], 1, { total: 45 }); assertSelected(page, 3);
  assert.match(textOf(page.tree), /第 1 \/ 2 页/);
  click(page, '下一页'); page.effects(); page.render(); await resolveList(h, [23], 3, { total: 30 });
  page.effects(); page.render(); h.details.at(-1).resolve(detail(23)); await settle(page);
  h.writes[0].resolve(); await settle(page);
  assert.equal(h.lists[4].params.page, 2); assert.equal(h.lists[4].params.certification_status, 'approved');
  await resolveList(h, [23], 4, { total: 30 }); assertSelected(page, 23);
  assert.deepEqual(h.details.map(request => request.id), [1, 3, 23]);
});

test('missing total leaves pagination unavailable instead of treating the current slice as all results', async () => {
  const h = harness(); await resolveList(h, [1], 0, { total: undefined });
  assert.match(textOf(h.page.tree), /总数暂不可用/);
  assert.equal(button(h.page, '下一页').props.disabled, true); assert.equal(button(h.page, '上一页').props.disabled, true);
});


test('write completion reloads the same target in a newer current scope without restoring its old filters', async () => {
  const h = await boot(), { page } = h;
  click(page, '下架主页');
  change(page, '认证状态', 'approved'); page.effects(); page.render();
  await resolveList(h, [1], 1); page.effects(); page.render();
  h.details[1].resolve(detail(1)); await settle(page);
  h.writes[0].resolve(); await settle(page);
  assert.equal(h.lists[2].params.certification_status, 'approved');
  await resolveList(h, [1], 2); assert.deepEqual(h.details.map(request => request.id), [1, 1, 1]);
  h.details[2].resolve({ ...detail(1), profile: { ...detail(1).profile, designer_visible: false } });
  await settle(page); assertSelected(page, 1); assert.ok(button(page, '上架主页'));
});


test('request generation guards overlapping refreshes even when their filters are identical', async () => {
  for (const outcome of ['resolve', 'reject']) {
    const h = await boot(), { page } = h;
    const refresh = button(page, '刷新').props.onClick;
    refresh(); refresh(); page.render(); assert.equal(h.lists.length, 3);
    h.lists[1][outcome](outcome === 'resolve' ? { list: [designer(8)], summary: zeroSummary, total: 1 } : new Error('old refresh failed'));
    await settle(page); assert.match(textOf(page.tree), /设计师列表加载中/);
    assert.equal(h.errors.length, 0); assert.equal(h.recovered.length, 1);
    await resolveList(h, [2], 2); page.effects(); page.render();
    h.details.at(-1).resolve(detail(2)); await settle(page); assertSelected(page, 2);
    h.page.unmount();
  }
});

test('unmount during a successful write refresh suppresses subsequent details and final cleanup', async () => {
  const h = await boot(), { page } = h;
  const visibility = button(page, '下架主页').props.onClick;
  const service = button(page, '暂停接单').props.onClick;
  visibility(); service(); assert.equal(h.writes.length, 1);
  h.writes[0].resolve(); await settle(page); assert.equal(h.lists.length, 2);
  page.unmount(); const updates = page.updates;
  h.lists[1].resolve({ list: [designer(1)], total: 1 }); await settle(page);
  assert.equal(page.updates, updates); assert.equal(h.details.length, 1);
  assert.equal(h.recovered.length, 1); assert.equal(h.errors.length, 0);
});


test('same-ID list status changes invalidate detail actions until refreshed details arrive', async () => {
  for (const [field, action, oldLabel, newLabel] of [
    ['designer_visible', 'visibility', '下架主页', '上架主页'],
    ['service_enabled', 'service', '暂停接单', '恢复接单'],
  ]) {
    const h = await boot([1]), { page } = h;
    const staleAction = button(page, oldLabel).props.onClick;
    click(page, '刷新');
    h.lists[1].resolve({ list: [{ ...designer(1), [field]: false }], total: 1 }); await settle(page);
    assert.equal(h.details.length, 2, field); assert.equal(h.details[1].id, 1);
    assert.equal(actions(page).length, 0, field); assert.match(textOf(panel(page)), /详情加载中/);
    staleAction(); assert.equal(h.writes.length, 0, field);
    h.details[1].resolve({ ...detail(1), profile: { ...detail(1).profile, [field]: false } }); await settle(page);
    assertSelected(page, 1); assert.ok(button(page, newLabel));
    staleAction(); assert.equal(h.writes.length, 0, field);
    click(page, newLabel);
    assert.equal(h.writes.length, 1); assert.equal(h.writes[0].action, action);
    assert.equal(h.writes[0].id, 1); assert.equal(h.writes[0].value, true);
    page.unmount(); h.writes[0].resolve(); await settle(page);
  }
});

test('same-ID list refresh retains unchanged details but replaces a pending pre-refresh detail request', async () => {
  const unchanged = await boot([1]);
  click(unchanged.page, '刷新'); await resolveList(unchanged, [1], 1);
  assert.equal(unchanged.details.length, 1); assertSelected(unchanged.page, 1);
  assert.equal(actions(unchanged.page).every(node => !node.props.disabled), true);
  unchanged.page.unmount();

  const h = harness(), { page } = h;
  await resolveList(h, [1]); page.effects(); page.render(); assert.equal(h.details.length, 1);
  click(page, '刷新');
  h.lists[1].resolve({ list: [{ ...designer(1), designer_visible: false }], total: 1 }); await settle(page);
  assert.equal(h.details.length, 2); assert.equal(actions(page).length, 0);
  h.details[1].resolve({ ...detail(1), profile: { ...detail(1).profile, designer_visible: false } }); await settle(page);
  assert.ok(button(page, '上架主页'));
  h.details[0].resolve(detail(1)); await settle(page);
  assert.ok(button(page, '上架主页')); assertSelected(page, 1);
});
