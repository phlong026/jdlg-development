// Runs the production component with controlled hook state and deferred API results.
// No browser or backend calls; display assertions are React element-tree assertions.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
import React from 'react';
import ts from 'typescript';
const source = fs.readFileSync(new URL('../src/pages/aiOverview.tsx', import.meta.url), 'utf8');
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, esModuleInterop: true, target: ts.ScriptTarget.ES2020 } }).outputText;
const flush = () => new Promise(resolve => setImmediate(resolve));
function textOf(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  return typeof node === 'object' ? textOf(node.props?.children) : String(node);
}
function nodes(node, predicate) {
  if (Array.isArray(node)) return node.flatMap(item => nodes(item, predicate));
  if (!node || typeof node !== 'object') return [];
  return [...(predicate(node) ? [node] : []), ...nodes(node.props?.children, predicate)];
}
function mount() {
  let cursor = 0, dirty = false, tree;
  const slots = [], effects = [], requests = [], reports = [], recoveries = [];
  const hooks = {
    ...React,
    useState(initial) {
      const index = cursor++;
      if (!(index in slots)) slots[index] = typeof initial === 'function' ? initial() : initial;
      return [slots[index], value => { const next = typeof value === 'function' ? value(slots[index]) : value; if (!Object.is(next, slots[index])) { slots[index] = next; dirty = true; } }];
    },
    useMemo(fn) { cursor++; return fn(); },
    useEffect(fn, deps) {
      const index = cursor++, previous = slots[index];
      if (!previous || deps.some((value, n) => !Object.is(value, previous.deps[n]))) {
        slots[index] = { deps, cleanup: previous?.cleanup };
        effects.push(() => { slots[index].cleanup?.(); slots[index].cleanup = fn(); });
      }
    },
  };
  const module = { exports: {} };
  vm.runInNewContext(js, { module, exports: module.exports, console: { error() {} }, require(name) {
    if (name === 'react') return hooks;
    if (name === 'react-router-dom') return { useNavigate: () => () => {} };
    if (name === '../api/aiOverview') return { getAIOverview: () => new Promise((resolve, reject) => requests.push({ resolve, reject })) };
    if (name === '../utils/pageLoadError') return { reportPageLoadError: (...args) => reports.push(args), reportPageLoadRecovered: (...args) => recoveries.push(args) };
    if (name.includes('useIsMobile')) return {useIsMobile:()=>false};
  if (name.endsWith('.scss')) return {};
    throw new Error(name);
  } });
  const page = {
    requests, reports, recoveries,
    get tree() { return tree; },
    render() {
      let cycles = 0;
      do { assert.ok(cycles++ < 20); cursor = 0; dirty = false; tree = module.exports.default(); effects.splice(0).forEach(fn => fn()); } while (dirty);
    },
    click(label) {
      const button = nodes(tree, node => node.type === 'button' && textOf(node) === label)[0];
      assert.ok(button, `Missing button ${label}`); assert.ok(!button.props.disabled); button.props.onClick(); page.render();
    },
    unmount() { slots.forEach(slot => slot?.cleanup?.()); },
  };
  page.render(); return page;
}
const fixture = {
  kpi: { today: { total: 0, success_rate: 0, stones_revenue: 0, cost_yuan: 0, margin_rate: null }, yesterday: { total: 0, success_rate: 0 }, cost_note: 'fixture cost basis', cost_landed_ratio: 0 },
  queues: { image: { failed: 0, timeout: 0 } }, trend: [], scenes: [], suppliers: [], chain: [], fail_reasons: [], tools_top: [], alerts: [],
};

test('AI overview never presents failed/loading data as zero revenue or no failures', async () => {
  const page = mount();
  assert.match(textOf(page.tree), /正在加载 AI 运营数据/);
  assert.doesNotMatch(textOf(page.tree), /近 7 日无失败任务|今日灵石收入/);
  page.requests[0].reject(new Error('fixture unavailable')); await flush(); page.render();
  assert.match(textOf(page.tree), /无法判断调用量、收入或异常数量/);
  assert.doesNotMatch(textOf(page.tree), /近 7 日无失败任务|今日灵石收入/);
  assert.equal(page.reports.length, 1);
  page.click('重新加载');
  assert.equal(page.requests.length, 2);
  page.requests[1].resolve(fixture); await flush(); page.render();
  assert.match(textOf(page.tree), /今日灵石收入/);
  assert.match(textOf(page.tree), /近 7 日无失败任务/);
  assert.equal(page.recoveries.length, 1);
});

test('AI overview refresh hides the previous report until current data is known', async () => {
  const page = mount(); page.requests[0].resolve(fixture); await flush(); page.render();
  page.click('刷新');
  assert.match(textOf(page.tree), /正在加载 AI 运营数据/);
  assert.doesNotMatch(textOf(page.tree), /近 7 日无失败任务/);
  page.unmount(); page.requests[1].reject(new Error('late failure')); await flush();
  assert.equal(page.reports.length, 0, 'a departed page cannot surface a late error');
});
