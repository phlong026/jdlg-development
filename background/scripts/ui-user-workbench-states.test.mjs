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
const sourcePath = process.argv[2] || path.join(appRoot, 'src/pages/userWorkbench.tsx');
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
    if (name === '../api/users' || name === '../api/certification') return new Proxy({}, { get: (_, method) => (...args) => api[method](...args) });
    throw new Error(`Unexpected import: ${name}`);
  },
  console: { ...console, error() {} }, URLSearchParams,
  localStorage: { getItem: () => null, setItem: () => {} },
  alert: message => alerts.push(message), window: { confirm: () => true, location: { search: '?userId=1', pathname: '/admin/user-workbench' } },
});
vm.runInContext(sourceJs, context, { filename: sourcePath });
const { default: UserWorkbench } = moduleRef.exports;
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
const user = id => ({ id: Number(id), username: `User ${id}`, nickname: `User ${id}`, stones: 37, created_at: '2026-10-01T00:00:00Z' });
const summary = () => ({ membership: null, stone_summary: { current_stones: 37, recent_gain: 5, recent_consume: 2, checkin_total: 3 }, stone_records: [], recent_orders: [], recent_tasks: [], device_risk: { risk_level: 'low', risk_tags: [], has_password: false, same_device_account_count: 1, same_device_other_users: [] } });
const cert = id => ({ id: Number(id) * 10, user_id: Number(id), type: 'designer', status: 'approved', created_at: '2026-10-01T00:00:00Z' });
let primaryRequests;
function setup(overrides = {}) {
  primaryRequests = []; calls = []; alerts = [];
  api = {
    getUserDetail: id => new Promise((resolve, reject) => primaryRequests.push({ id, resolve, reject })),
    getUserEnterpriseWechatVerification: async id => ({ user_id: Number(id), enterprise_wechat_verified: true, enterprise_wechat_contact: `phone-${id}` }),
    getCertificationList: async () => ({ list: [cert(1), cert(2)], total: 2 }),
    getUserWorkbenchSummary: async () => summary(),
    getCertificationDetail: async id => ({ application: cert(Number(id) / 10) }),
    adjustUserStones: async (id, payload) => { calls.push({ action: 'adjust', id, payload }); },
    setUserStones: async (id, payload) => { calls.push({ action: 'set', id, payload }); },
    ...overrides,
  };
}
function boot() { const page = mount(UserWorkbench, {}); page.effects(); return page; }
async function settle(page) { await flush(); await flush(); page.render(); }
function select(page, id) {
  const field = nodes(page.tree, node => node.props?.className === 'workbench-search-input')[0];
  field.props.onChange({ target: { value: String(id) } }); page.render();
  button(page, '搜索').props.onClick(); page.render();
}
function heading(page, name) { return nodes(page.tree, node => node.type === 'h2' && textOf(node) === name); }
function assertUser(page, id) { assert.equal(heading(page, `User ${id}`).length, 1); }
function inputWithPlaceholder(page, value, nextValue) {
  const input = nodes(page.tree, node => node.type === 'input' && node.props.placeholder === value)[0];
  assert.ok(input, value); input.props.onChange({ target: { value: nextValue } }); page.render();
}
test('User detail isolates selections and distinguishes failed modules from true empty or zero data', async () => {
  setup();
  let page = boot();
  select(page, 2);
  primaryRequests[1].resolve(user(2)); await settle(page); assertUser(page, 2);
  primaryRequests[0].resolve(user(1)); await settle(page); assertUser(page, 2);
  assert.equal(heading(page, 'User 1').length, 0);
  check('slow A / fast B: late primary A cannot replace the selected B account');

  setup(); page = boot(); primaryRequests[0].resolve(user(1)); await settle(page); assertUser(page, 1);
  const oldAdjust = button(page, '调整灵石');
  select(page, 2);
  assert.equal(heading(page, 'User 1').length, 0);
  oldAdjust.props.onClick(); page.render();
  assert.equal(nodes(page.tree, node => node.props?.className === 'stones-form').length, 0);
  primaryRequests[1].reject(new Error('B failure')); await settle(page);
  assert.match(textOf(page.tree), /用户 2 的资料未加载，请重试/);
  assert.equal(nodes(page.tree, node => node.type === 'button' && textOf(node) === '调整灵石').length, 0);
  click(page, '重新加载用户'); primaryRequests[2].resolve(user(2)); await settle(page); assertUser(page, 2);
  check('A success / B failure clears A immediately, blocks stale A actions and retries the intended B');

  setup({ getUserWorkbenchSummary: async () => { throw new Error('summary unavailable'); } });
  page = boot(); primaryRequests[0].resolve(user(1)); await settle(page); assertUser(page, 1);
  assert.match(textOf(page.tree), /会员、订单、任务、灵石流水与风险信息未加载/);
  assert.match(textOf(page.tree), /灵石 37/);
  assert.match(textOf(page.tree), /已通过/);
  assert.doesNotMatch(textOf(page.tree), /当前用户暂无订单记录|近30天获得 0 灵石|当前用户还没有会员记录|当前没有明显风险标签/);
  click(page, '灵石流水');
  assert.equal(nodes(page.tree, node => node.props?.className === 'stone-overview-grid').length, 0);
  assert.match(textOf(page.tree), /无法查看金额、灵石流水、会员权益或风险状态/);
  api.getUserWorkbenchSummary = async () => summary();
  click(page, '重新加载详情资料'); await settle(page);
  assert.equal(nodes(page.tree, node => node.props?.className === 'stone-overview-grid').length, 1);
  assert.match(textOf(page.tree), /近30天获得5/);
  check('summary failure never becomes zero finance or empty membership; successful profile/certification survive and summary retry recovers');

  setup({ getCertificationList: async () => { throw new Error('certification unavailable'); } });
  page = boot(); primaryRequests[0].resolve(user(1)); await settle(page);
  assert.match(textOf(page.tree), /认证资料未加载/);
  assert.doesNotMatch(textOf(page.tree), /暂无认证/);
  assert.match(textOf(page.tree), /当前用户暂无订单记录/);
  api.getCertificationList = async () => ({ list: [], total: 0 });
  click(page, '重新加载认证资料'); await settle(page);
  assert.match(textOf(page.tree), /暂无认证/);
  check('certification failure is unknown until a successful empty retry establishes no certification');

  setup({ getUserEnterpriseWechatVerification: async () => { throw new Error('permission unavailable'); } });
  page = boot(); primaryRequests[0].resolve(user(1)); await settle(page);
  assert.match(textOf(page.tree), /下载权限未加载/);
  assert.doesNotMatch(textOf(page.tree), /当前用户还未完成手机号授权验证|用户状态整体正常/);
  click(page, '修正下载权限'); await settle(page);
  assert.match(textOf(page.tree), /授权资料未加载，请重试后再修改/);
  assert.equal(button(page, '保存权限状态').props.disabled, true);
  check('permission failure is not treated as unverified and cannot be saved from an uninitialized form');

  let resolveASummary, rejectACert;
  let certCalls = 0;
  setup({
    getUserWorkbenchSummary: id => id === '1' ? new Promise(resolve => { resolveASummary = resolve; }) : Promise.resolve(summary()),
    getCertificationList: () => ++certCalls === 1 ? new Promise((_, reject) => { rejectACert = reject; }) : Promise.resolve({ list: [cert(2)], total: 1 }),
  });
  page = boot(); primaryRequests[0].resolve(user(1)); await settle(page);
  select(page, 2); primaryRequests[1].resolve(user(2)); await settle(page); assertUser(page, 2);
  resolveASummary({ ...summary(), membership: { plan_title: 'A private membership', status: 'active', is_lifetime: true } });
  rejectACert(new Error('late A certification failure')); await settle(page);
  assertUser(page, 2); assert.doesNotMatch(textOf(page.tree), /A private membership|认证资料未加载/);
  assert.match(textOf(page.tree), /已通过/);
  check('late A module success and failure cannot alter B data or B module error states');

  setup({ getUserWorkbenchSummary: async () => null });
  page = boot(); primaryRequests[0].resolve(user(1)); await settle(page);
  assert.match(textOf(page.tree), /灵石流水与风险信息未加载/);
  assert.doesNotMatch(textOf(page.tree), /当前用户暂无订单记录/);
  check('a missing summary payload is explicitly unavailable rather than a successful empty result');

  let finishAdjustment;
  setup({ adjustUserStones: (id, payload) => {
    calls.push({ action: 'adjust', id, payload });
    return new Promise(resolve => { finishAdjustment = resolve; });
  } });
  page = boot(); primaryRequests[0].resolve(user(1)); await settle(page);
  click(page, '调整灵石'); click(page, '+100');
  const adjustmentHandler = button(page, '确认修改').props.onClick;
  const firstAdjustment = adjustmentHandler(); const duplicateAdjustment = adjustmentHandler();
  assert.equal(calls.length, 1);
  select(page, 2); primaryRequests[1].resolve(user(2)); await settle(page); assertUser(page, 2);
  finishAdjustment(); await Promise.all([firstAdjustment, duplicateAdjustment]); await settle(page);
  assertUser(page, 2); assert.equal(primaryRequests.length, 2);
  assert.equal(calls[0].id, '1'); assert.equal(calls[0].payload.amount, 100);
  check('duplicate same-frame stone handler is locked; completing A write after selecting B never reloads A');

});
