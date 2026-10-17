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
const sourcePath = process.argv[2] || path.join(appRoot, 'src/pages/designerCenter.tsx');
const source = fs.readFileSync(sourcePath, 'utf8');
const sourceJs = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText;
const checks = [];
const flush = () => new Promise(resolve => setImmediate(resolve));
let activeRuntime = null;
let api = {};
let calls = [];
let alerts = [];
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
    if (name === 'react-router-dom') return { useNavigate: () => () => {}, useSearchParams: () => [new URLSearchParams(), () => {}] };
    if (name.includes('useIsMobile')) return {useIsMobile:()=>false};
  if (name.endsWith('.scss')) return {};
    if (name === '../utils/pageLoadError') return { reportPageLoadError: () => {}, reportPageLoadRecovered: () => {} };
    if (name === 'react-icons/fi') return new Proxy({}, { get: (_, icon) => icon });
    if (name === '../component/managementSearchPanel') return { __esModule: true, default: 'ManagementSearchPanel' };
    if (name === '../api/designers') return new Proxy({}, { get: (_, method) => (...args) => api[method](...args) });
    throw new Error(`Unexpected import: ${name}`);
  },
  console: { ...console, error() {} }, URLSearchParams,
  alert: message => alerts.push(message), window: { confirm: () => true, location: { search: '' } },
});
vm.runInContext(sourceJs, context, { filename: sourcePath });
const { default: DesignerCenter } = moduleRef.exports;
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
function click(runtime, label) { const node = button(runtime, label); assert.notEqual(node.props.disabled, true, label + ' must be enabled'); const result = node.props.onClick(); runtime.render(); return result; }
function check(name) { checks.push(name); console.log(`PASS ${name}`); }
const designers = [1, 2].map(id => ({ user_id: id, username: `account-${id}`, display_name: `Designer ${id}`, designer_visible: true, service_enabled: true, total_works: 0, follow_count: 0, review_count: 0, positive_review_count: 0, negative_review_count: 0 }));
const makeDetail = id => ({ user_id: id, username: `account-${id}`, profile: { nickname: `Designer ${id}`, designer_visible: true, service_enabled: true }, stats: { total_works: 0, published_works: 0, follow_count: 0, review_count: 0, positive_review_count: 0, negative_review_count: 0, total_orders: 0, total_earnings: 0 }, works: [], reviews: [] });
let requests;
function setup(overrides = {}) {
  requests = []; calls = [];
  api = {
    getDesignerManagementList: async () => ({ list: designers }),
    getDesignerManagementDetail: id => new Promise((resolve, reject) => { requests.push({ id, resolve, reject }); }),
    updateDesignerVisibility: async (id, visible) => { calls.push({ action: 'visibility', id, visible }); },
    updateDesignerServiceStatus: async (id, enabled) => { calls.push({ action: 'service', id, enabled }); },
    ...overrides,
  };
}
async function boot() {
  const page = mount(DesignerCenter, {});
  page.effects(); await flush(); page.render(); page.effects(); page.render();
  assert.equal(requests[0].id, 1);
  return page;
}
function select(page, id) {
  const target = nodes(page.tree, node => node.props?.className?.startsWith('designer-list-item ') && node.key === String(id))[0];
  assert.ok(target, `designer ${id} list button`);
  target.props.onClick(); page.render();
}
function detailPanel(page) { return nodes(page.tree, node => node.props?.className === 'designer-detail-panel section-card')[0]; }
function actionButtons(page) { return nodes(detailPanel(page), node => node.type === 'button' && /^(?:上架主页|下架主页|暂停接单|恢复接单|提交中\.\.\.)$/.test(textOf(node))); }
function assertDesigner(page, id) {
  const panel = detailPanel(page);
  const heading = nodes(panel, node => node.type === 'h3');
  assert.equal(heading.length, 1);
  assert.equal(textOf(heading[0]), `Designer ${id}`);
  assert.equal(actionButtons(page).length, 2);
}
test('Designer selection cannot display or write another user after asynchronous responses', async () => {
  setup();
  let page = await boot();
  select(page, 2); page.effects(); page.render();
  assert.equal(requests[1].id, 2);
  requests[1].resolve(makeDetail(2)); await flush(); page.render();
  assertDesigner(page, 2);
  requests[0].resolve(makeDetail(1)); await flush(); page.render();
  assertDesigner(page, 2);
  click(page, '下架主页'); await flush(); page.render();
  assert.deepEqual(calls, [{ action: 'visibility', id: 2, visible: false }]);
  check('slow A / fast B: A cannot overwrite B; displayed B action targets only B');

  setup(); page = await boot();
  requests[0].resolve(makeDetail(1)); await flush(); page.render(); assertDesigner(page, 1);
  const staleAction = button(page, '下架主页');
  select(page, 2);
  assert.equal(actionButtons(page).length, 0);
  assert.doesNotMatch(textOf(detailPanel(page)), /Designer 1/);
  staleAction.props.onClick(); await flush();
  assert.equal(calls.length, 0);
  page.effects(); page.render();
  requests[1].reject(new Error('fixture B unavailable')); await flush(); page.render();
  assert.equal(actionButtons(page).length, 0);
  assert.doesNotMatch(textOf(detailPanel(page)), /Designer 1/);
  assert.match(textOf(detailPanel(page)), /详情加载失败，请重试/);
  click(page, '重新加载');
  assert.equal(requests[2].id, 2);
  requests[2].resolve(makeDetail(2)); await flush(); page.render(); assertDesigner(page, 2);
  check('A succeeded / B failed: A clears immediately, stale A handler is blocked, B failure is local and retryable');

  setup(); page = await boot();
  select(page, 2); page.effects(); page.render();
  requests[1].resolve(makeDetail(2)); await flush(); page.render(); assertDesigner(page, 2);
  requests[0].reject(new Error('fixture stale A error')); await flush(); page.render(); assertDesigner(page, 2);
  assert.doesNotMatch(textOf(detailPanel(page)), /加载失败/);
  check('late A error cannot replace B success or its loading/error state');

  setup(); page = await boot();
  requests[0].resolve(makeDetail(2)); await flush(); page.render();
  assert.equal(actionButtons(page).length, 0);
  assert.match(textOf(detailPanel(page)), /详情加载失败/);
  check('response identity mismatch hides all write controls instead of binding them to the selected list row');

  let resolveAction;
  setup({ updateDesignerVisibility: (id, visible) => {
    calls.push({ action: 'visibility', id, visible });
    return new Promise(resolve => { resolveAction = resolve; });
  } });
  page = await boot(); requests[0].resolve(makeDetail(1)); await flush(); page.render();
  click(page, '下架主页');
  select(page, 2); page.effects(); page.render();
  requests[1].resolve(makeDetail(2)); await flush(); page.render(); assertDesigner(page, 2);
  assert.equal(actionButtons(page).every(node => node.props.disabled), true);
  resolveAction(); await flush(); await flush(); page.render();
  assertDesigner(page, 2);
  assert.equal(requests.length, 2);
  assert.equal(actionButtons(page).every(node => !node.props.disabled), true);
  assert.deepEqual(calls, [{ action: 'visibility', id: 1, visible: false }]);
  check('finishing A action after selecting B keeps B selected and does not start an obsolete A detail reload');

  setup(); page = await boot(); page.unmount();
  requests[0].resolve(makeDetail(1)); await flush(); page.render();
  assert.equal(actionButtons(page).length, 0);
  check('unmount invalidates pending detail requests');



  for (const label of ['下架主页', '暂停接单']) {
    setup(); page = await boot(); requests[0].resolve(makeDetail(1)); await flush(); page.render();
    const action = button(page, label).props.onClick;
    action(); action();
    assert.equal(calls.length, 1, label);
    await flush();
  }
  check('same-frame repeated visibility/service handlers each issue exactly one write request');

});
