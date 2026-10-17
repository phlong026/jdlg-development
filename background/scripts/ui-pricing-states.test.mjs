// Deterministic component-state tests with controlled hooks; no browser or backend writes.
import test from 'node:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const require = createRequire(import.meta.url);
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const appRoot = fileURLToPath(new URL('..', import.meta.url));
const ts = require(path.join(appRoot, 'node_modules/typescript'));
const React = require(path.join(appRoot, 'node_modules/react'));
const sourcePath = process.argv[2] || path.join(appRoot, 'src/pages/aiPricing.tsx');
const source = fs.readFileSync(sourcePath, 'utf8') + '\nexport { AdjustPricingModal, PricingHistoryModal };\n';
const sourceJs = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText;
const baseItem = { scene: 'ai_draw_single', label: '单张绘画', stones: 10, usage_7d: 20, success_7d: 20, failed_rate_7d: 0, stones_revenue_7d: 200, cost_yuan_est: 0.2, margin_rate: 0.8, updated_at: '2026-10-01T00:00:00Z' };
const version = { id: 1, scene: baseItem.scene, old_stones: 5, new_stones: 10, operator: '测试管理员', note: '测试调价', created_at: '2026-10-01T00:00:00Z' };
const checks = [];
const flush = () => new Promise(resolve => setImmediate(resolve));
let activeRuntime = null;
let api = {};
let calls = [];
let alerts = [];
let navigations = [];
const fakeReact = {
  ...React,
  useState(initial) {
    const runtime = activeRuntime;
    const index = runtime.cursor++;
    if (!(index in runtime.slots)) runtime.slots[index] = typeof initial === 'function' ? initial() : initial;
    return [runtime.slots[index], value => { runtime.slots[index] = typeof value === 'function' ? value(runtime.slots[index]) : value; }];
  },
  useEffect(effect, deps) {
    const runtime = activeRuntime;
    const index = runtime.cursor++;
    const previous = runtime.slots[index];
    if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) {
      runtime.slots[index] = { deps, cleanup: previous?.cleanup };
      runtime.pending.push(() => {
        runtime.slots[index].cleanup?.();
        runtime.slots[index].cleanup = effect();
      });
    }
  },
  useMemo(factory) { return factory(); },
  useRef(initial) {
    const runtime = activeRuntime; const index = runtime.cursor++;
    if (!(index in runtime.slots)) runtime.slots[index] = { current: initial };
    return runtime.slots[index];
  },
};
fakeReact.default = fakeReact;
fakeReact.__esModule = true;
const moduleRef = { exports: {} };
const context = vm.createContext({
  module: moduleRef, exports: moduleRef.exports,
  require(name) {
    if (name === 'react') return fakeReact;
    if (name === 'react-router-dom') return { useNavigate: () => target => navigations.push(target), useSearchParams: () => [new URLSearchParams(), () => {}] };
    if (name.includes('useIsMobile')) return {useIsMobile:()=>false};
  if (name.endsWith('.scss')) return {};
    if (name === '../utils/pageLoadError') return { reportPageLoadError: () => {} };
    if (name === '../api/aiOverview' || name === '../api/ai') return new Proxy({}, { get: (_, method) => (...args) => api[method](...args) });
    throw new Error(`Unexpected import: ${name}`);
  },
  console: { ...console, error() {} }, URLSearchParams,
  alert: message => alerts.push(message), window: { confirm: () => true },
});
vm.runInContext(sourceJs, context, { filename: sourcePath });
const { default: Pricing, AdjustPricingModal: Adjust, PricingHistoryModal: History } = moduleRef.exports;
function resetApi(overrides = {}) {
  calls = []; alerts = []; navigations = [];
  api = {
    saveAIPricing: async payload => { calls.push(JSON.parse(JSON.stringify(payload))); },
    getAIPricingVersions: async () => [version],
    getAIPricingOpsList: async () => [baseItem],
    getAIOverview: async () => ({ suppliers: [], chain: [] }),
    ...overrides,
  };
}
function mount(Component, props) {
  const runtime = { slots: [], cursor: 0, pending: [], tree: null };
  runtime.render = () => {
    activeRuntime = runtime; runtime.cursor = 0;
    runtime.tree = Component(props); activeRuntime = null;
    return runtime.tree;
  };
  runtime.effects = () => { const pending = runtime.pending.splice(0); pending.forEach(effect => effect()); };
  runtime.unmount = () => { runtime.slots.forEach(slot => { if (slot && typeof slot.cleanup === 'function') slot.cleanup(); }); };
  runtime.render();
  return runtime;
}
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
  if (node === null || node === undefined || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  if (typeof node !== 'object') return String(node);
  return textOf(node.props?.children);
}
function button(runtime, label) {
  const match = nodes(runtime.tree, node => node.type === 'button' && textOf(node) === label);
  assert.equal(match.length, 1, `Expected one button ${label}, found ${match.length}`);
  return match[0];
}
function byId(runtime, id) { const result = nodes(runtime.tree, node => node.props?.id === id); assert.equal(result.length, 1, id); return result[0]; }
function fill(runtime, id, value) { byId(runtime, id).props.onChange({ target: { value } }); runtime.render(); }
function click(runtime, label) { const node = button(runtime, label); assert.notEqual(node.props.disabled, true, label + ' must be enabled'); const result = node.props.onClick(); runtime.render(); return result; }
function check(name) { checks.push(name); console.log(`PASS ${name}`); }
test('AI pricing keeps confirmation, version and retry states consistent', async () => {
  resetApi();
  let closes = 0;
  let saved = '';
  let modal = mount(Adjust, { item: baseItem, onClose: () => closes++, onSaved: message => saved = message });
  assert.match(textOf(modal.tree), /定价未变化，无需保存/);
  assert.equal(nodes(modal.tree, node => node.type === 'button' && /下一步|二次确认|保存/.test(textOf(node))).length, 0);
  fill(modal, 'aip-price-note', '仅备注');
  click(modal, '关闭');
  assert.equal(closes, 1); assert.equal(calls.length, 0); assert.equal(saved, '');
  check('unchanged price / note-only change closes without request or a fake version');

  resetApi();
  modal = mount(Adjust, { item: baseItem, onClose: () => {}, onSaved: message => saved = message });
  for (const invalid of ['', '-1', '1.5', 'NaN', '9007199254740992']) {
    fill(modal, 'aip-new-stones', invalid);
    assert.equal(button(modal, '下一步（确认调价）').props.disabled, true);
    assert.equal(byId(modal, 'aip-new-stones').props['aria-invalid'], true);
    assert.match(textOf(modal.tree), /有效整数/);
  }
  assert.equal(calls.length, 0);
  check('empty, negative, fractional, non-finite and unsafe values cannot reach confirmation');

  fill(modal, 'aip-new-stones', '12');
  fill(modal, 'aip-price-note', '  成本调整  ');
  click(modal, '下一步（确认调价）');
  assert.equal(calls.length, 0);
  assert.equal(button(modal, '二次确认：改为 12 灵石').props.disabled, false);
  fill(modal, 'aip-new-stones', '15');
  assert.equal(nodes(modal.tree, node => node.type === 'button' && /二次确认/.test(textOf(node))).length, 0);
  click(modal, '下一步（确认调价）');
  fill(modal, 'aip-price-note', '  更新备注  ');
  assert.equal(nodes(modal.tree, node => node.type === 'button' && /二次确认/.test(textOf(node))).length, 0);
  click(modal, '下一步（确认调价）');
  click(modal, '二次确认：改为 15 灵石');
  await flush(); modal.render();
  assert.deepEqual(calls, [{ scene: baseItem.scene, stones: 15, note: '更新备注' }]);
  assert.match(saved, /10 → 15 灵石，并写入版本记录/);
  check('changed pricing retains two-step confirmation; later value or note edits reset it; payload stays unchanged');

  resetApi();
  modal = mount(Adjust, { item: baseItem, onClose: () => {}, onSaved: () => {} });
  fill(modal, 'aip-new-stones', '12'); click(modal, '下一步（确认调价）');
  fill(modal, 'aip-new-stones', '10');
  assert.equal(nodes(modal.tree, node => node.type === 'button' && /下一步|二次确认/.test(textOf(node))).length, 0);
  button(modal, '关闭'); assert.equal(calls.length, 0);
  fill(modal, 'aip-new-stones', '0'); click(modal, '下一步（确认调价）'); click(modal, '二次确认：改为 0 灵石'); await flush();
  assert.deepEqual(calls, [{ scene: baseItem.scene, stones: 0 }]);
  check('returning to original price removes confirmation; zero remains a valid new price');

  resetApi();
  const parent = mount(Pricing, {}); parent.effects(); await flush(); parent.render();
  click(parent, '历史');
  const historyChild = nodes(parent.tree, node => node.type === History)[0];
  historyChild.props.onRollback(5); parent.render();
  const adjustment = nodes(parent.tree, node => node.type === Adjust)[0];
  assert.equal(adjustment.props.item.stones, 10);
  assert.equal(adjustment.props.initialStones, 5);
  modal = mount(Adjust, adjustment.props);
  assert.match(textOf(modal.tree), /当前定价10 灵石/);
  assert.equal(byId(modal, 'aip-new-stones').props.value, '5');
  assert.equal(calls.length, 0);
  click(modal, '下一步（确认调价）'); assert.equal(calls.length, 0);
  click(modal, '二次确认：改为 5 灵石'); await flush(); parent.render();
  assert.deepEqual(calls, [{ scene: baseItem.scene, stones: 5 }]);
  assert.match(textOf(parent.tree), /10 → 5 灵石，并写入版本记录/);
  check('history rollback separates actual current price 10 from target 5 and submits only after second confirmation');

  resetApi({ getAIPricingVersions: async () => { throw new Error('fixture history unavailable'); } });
  let history = mount(History, { scene: baseItem.scene, label: baseItem.label, onClose() {}, onRollback() {} });
  history.effects(); await flush(); history.render();
  assert.match(textOf(history.tree), /调价历史加载失败/);
  assert.doesNotMatch(textOf(history.tree), /还没有调价记录/);
  button(history, '重新加载');
  api.getAIPricingVersions = async () => [version];
  click(history, '重新加载'); history.effects(); history.render();
  assert.match(textOf(history.tree), /加载中/); assert.doesNotMatch(textOf(history.tree), /加载失败/);
  await flush(); history.render();
  button(history, '回滚到 5');
  assert.doesNotMatch(textOf(history.tree), /加载失败|还没有调价记录/);
  check('history failure is explicit, not an empty list; retry transitions through loading to real records');

  resetApi({ getAIPricingVersions: async () => [] });
  history = mount(History, { scene: baseItem.scene, label: baseItem.label, onClose() {}, onRollback() {} });
  history.effects(); await flush(); history.render();
  assert.match(textOf(history.tree), /该场景还没有调价记录（初始定价不产生版本）/);
  assert.doesNotMatch(textOf(history.tree), /加载失败/);
  check('only successful empty history reports no records and preserves initial-version semantics');

  let resolveHistory;
  resetApi({ getAIPricingVersions: () => new Promise(resolve => { resolveHistory = resolve; }) });
  history = mount(History, { scene: baseItem.scene, label: baseItem.label, onClose() {}, onRollback() {} });
  history.effects(); history.unmount(); resolveHistory([version]); await flush(); history.render();
  assert.match(textOf(history.tree), /加载中/);
  assert.equal(nodes(history.tree, node => node.type === 'table').length, 0);
  check('closing history cancels result application, preventing stale response updates');

  let rejectSave;
  resetApi({ saveAIPricing: payload => { calls.push(JSON.parse(JSON.stringify(payload))); return new Promise((_, reject) => { rejectSave = reject; }); } });
  closes = 0;
  modal = mount(Adjust, { item: baseItem, onClose: () => closes++, onSaved() {} });
  fill(modal, 'aip-new-stones', '12'); click(modal, '下一步（确认调价）'); click(modal, '二次确认：改为 12 灵石');
  assert.equal(button(modal, '保存中…').props.disabled, true);
  assert.equal(byId(modal, 'aip-new-stones').props.disabled, true);
  nodes(modal.tree, node => node.props?.className === 'aip-modal-overlay')[0].props.onClick();
  assert.equal(closes, 0);
  rejectSave(new Error('fixture save failed')); await flush(); modal.render();
  assert.match(alerts[0], /保存失败: fixture save failed/);
  assert.equal(button(modal, '二次确认：改为 12 灵石').props.disabled, false);
  assert.equal(byId(modal, 'aip-new-stones').props.value, '12');
  check('saving disables submission and dismissal; failed save retains the intended value and is retryable');



  resetApi();
  const changingProps = { item: baseItem, onClose() {}, onSaved() {} };
  modal = mount(Adjust, changingProps); modal.effects(); modal.render();
  fill(modal, 'aip-new-stones', '12'); click(modal, '下一步（确认调价）');
  const staleConfirm = button(modal, '二次确认：改为 12 灵石');
  changingProps.item = { ...baseItem, scene: 'scene_b', label: '场景 B', stones: 20 };
  modal.render();
  assert.equal(nodes(modal.tree, node => node.type === 'button' && /二次确认/.test(textOf(node))).length, 0);
  staleConfirm.props.onClick(); await flush(); assert.equal(calls.length, 0);
  modal.effects(); modal.render();
  assert.equal(byId(modal, 'aip-new-stones').props.value, '20');
  button(modal, '关闭');
  fill(modal, 'aip-new-stones', '25'); click(modal, '下一步（确认调价）');
  click(modal, '二次确认：改为 25 灵石'); await flush();
  assert.deepEqual(calls, [{ scene: 'scene_b', stones: 25 }]);
  assert.ok(source.includes('key={`${adjusting.item.scene}:${adjusting.item.stones}:${adjusting.initialStones ?? adjusting.item.stones}`}'));
  check('A confirmation cannot submit B after props change; target-specific key/reset and fresh B confirmation are required');

  let finishPrice;
  resetApi({ saveAIPricing: payload => { calls.push(JSON.parse(JSON.stringify(payload))); return new Promise(resolve => { finishPrice = resolve; }); } });
  modal = mount(Adjust, { item: baseItem, onClose() {}, onSaved() {} });
  fill(modal, 'aip-new-stones', '12'); click(modal, '下一步（确认调价）');
  const priceHandler = button(modal, '二次确认：改为 12 灵石').props.onClick;
  priceHandler(); priceHandler();
  assert.equal(calls.length, 1); finishPrice(); await flush();
  check('two direct confirmation-handler calls before rerender produce only one pricing request');

  let resolveOldSave;
  let oldSavedCalls = 0;
  let oldCloseCalls = 0;
  resetApi({ saveAIPricing: () => new Promise(resolve => { resolveOldSave = resolve; }) });
  const inflightProps = { item: baseItem, onClose: () => oldCloseCalls++, onSaved: () => oldSavedCalls++ };
  modal = mount(Adjust, inflightProps); modal.effects(); modal.render();
  fill(modal, 'aip-new-stones', '12'); click(modal, '下一步（确认调价）');
  const oldClose = button(modal, '取消').props.onClick;
  button(modal, '二次确认：改为 12 灵石').props.onClick();
  oldClose(); assert.equal(oldCloseCalls, 0);
  inflightProps.item = { ...baseItem, scene: 'scene_b', stones: 20 };
  modal.render(); modal.effects(); modal.render();
  resolveOldSave(); await flush(); modal.render();
  assert.equal(oldSavedCalls, 0);
  assert.equal(byId(modal, 'aip-new-stones').props.value, '20');
  check('same-frame stale close is locked; late save A completion cannot notify or close replacement B');

  resetApi({ saveAIPricing: () => new Promise(resolve => { resolveOldSave = resolve; }) });
  modal = mount(Adjust, { item: baseItem, onClose() {}, onSaved: () => oldSavedCalls++ });
  modal.effects(); modal.render();
  fill(modal, 'aip-new-stones', '12'); click(modal, '下一步（确认调价）');
  button(modal, '二次确认：改为 12 灵石').props.onClick(); modal.unmount();
  resolveOldSave(); await flush();
  assert.equal(oldSavedCalls, 0);
  check('unmounted old pricing modal cannot close the parent replacement through late onSaved');

});


test('pricing business details remain visible while identifiers are collapsed and advanced settings have a direct entry', async () => {
  resetApi({ getAIPricingOpsList: async () => [{ ...baseItem, margin_rate: 0.2 }] });
  const page = mount(Pricing, {}); page.effects(); await flush(); page.render();
  const diagnostic = nodes(page.tree, n => n.type === 'details' && n.props.className === 'aip-scene-diagnostic');
  assert.equal(diagnostic.length, 1);
  assert.notEqual(diagnostic[0].props.open, true);
  assert.match(textOf(diagnostic[0]), /ai_draw_single/);
  assert.equal(nodes(diagnostic[0], n => n.type === 'summary').length, 1);
  const row = nodes(page.tree, n => n.type === 'tr' && n.key === baseItem.scene)[0];
  assert.ok(row);
  const visible = row.props.children.filter(n => n?.type === 'td').slice(1).map(textOf).join('|');
  assert.match(visible, /10 灵石/); assert.match(visible, /¥0.20/); assert.match(visible, /20.0%/);
  assert.match(visible, /调价历史/);
  assert.match(textOf(page.tree), /10 灵石 = ¥1/);
  assert.match(textOf(page.tree), /二次确认并自动留痕/);
  assert.match(textOf(page.tree), /失败原因及退款记录/);
  assert.doesNotMatch(textOf(page.tree), /extra_config|JSON|行业单条成本/);
  await click(page, '打开高级设置');
  assert.deepEqual(navigations, ['/ai-config']);
  assert.equal(calls.length, 0, 'opening advanced settings must not change a price');
});
