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
const sourcePath = process.argv[2] || path.join(appRoot, 'src/pages/users.tsx');
const source = fs.readFileSync(sourcePath, 'utf8');
const sourceJs = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText;
const checks = [];
const flush = () => new Promise(resolve => setImmediate(resolve));
let activeRuntime = null;
let api = {};
let calls = [];
let alerts = [];
let pageErrors = [];
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
  useCallback(callback) { return callback; },
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
    if (name.endsWith('.scss')) return {};
    if (name === '../utils/pageLoadError') return { reportPageLoadError: (...args) => pageErrors.push(args), reportPageLoadRecovered: () => {} };
    if (name === 'react-icons/fi') return new Proxy({}, { get: (_, icon) => icon });
    if (name === '../component/managementSearchPanel') return { __esModule: true, default: 'ManagementSearchPanel' };
    if (name === '../api/users') return new Proxy({}, { get: (_, method) => (...args) => api[method](...args) });
    throw new Error(`Unexpected import: ${name}`);
  },
  console: { ...console, error() {} }, URLSearchParams,
  alert: message => alerts.push(message), window: { confirm: () => true, location: { search: '' } },
});
vm.runInContext(sourceJs, context, { filename: sourcePath });
const { default: Users } = moduleRef.exports;
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
const userRows = [1, 2, 3].map(id => ({ id, username: `User ${id}`, nickname: `User ${id}`, stones: 10, status: id === 3 ? 'banned' : 'active', created_at: '2026-10-01T00:00:00Z' }));
function setup(overrides = {}) {
  calls = []; pageErrors = [];
  api = {
    getUserList: async () => ({ list: userRows, total: 3 }),
    getUserStats: async () => null,
    banUser: async (id, reason) => { calls.push({ action: 'ban', id, reason }); },
    unbanUser: async (id, reason) => { calls.push({ action: 'unban', id, reason }); },
    getUserMergePrecheck: async (from, to) => { calls.push({ action: 'precheck', from, to }); throw new Error('fixture merge failure'); },
    ...overrides,
  };
}
async function boot() {
  const page = mount(Users, {});
  page.effects(); await flush(); page.render();
  return page;
}
function openBan(page, id) {
  const row = nodes(page.tree, node => node.type === 'tr' && node.key === String(id))[0];
  const button = nodes(row, node => node.type === 'button' && textOf(node) === (id === 3 ? '解封' : '封禁'))[0];
  assert.ok(button); button.props.onClick(); page.render();
}
function reason(page, value) {
  const area = nodes(page.tree, node => node.type === 'textarea')[0];
  area.props.onChange({ target: { value } }); page.render();
}
function input(page, placeholder, value) {
  const field = nodes(page.tree, node => node.type === 'input' && node.props.placeholder === placeholder)[0];
  if (value === undefined) return field;
  field.props.onChange({ target: { value } }); page.render();
}
function modal(page, name) { return nodes(page.tree, node => node.props?.className === `modal-content ${name}-modal`)[0]; }
function mergeResult(from, to) {
  const account = id => ({ user_id: Number(id), username: `User ${id}`, stones: 10, orders: 0, stone_records: 0, ai_tasks: 0, ai_video_tasks: 0, templates: 0, memberships: 0, certifications: 0, status: 'active' });
  return { from: account(from), to: account(to), conflicts: [], execution_note: 'Fixture: manual controlled execution only.' };
}
test('User operation failures stay local, preserve retry inputs and cannot leak into a later dialog', async () => {
  setup({ banUser: async (id, reason) => { calls.push({ action: 'ban', id, reason }); throw new Error('fixture ban failure'); } });
  let page = await boot(); openBan(page, 1);
  assert.equal(button(page, '确认封禁').props.disabled, true);
  await button(page, '确认封禁').props.onClick();
  assert.equal(calls.length, 0);
  reason(page, '  必填原因  '); await click(page, '确认封禁'); page.render();
  assert.match(textOf(modal(page, 'ban')), /封禁失败：fixture ban failure/);
  assert.match(textOf(modal(page, 'ban')), /User 1（ID 1）/);
  assert.equal(nodes(modal(page, 'ban'), node => node.type === 'textarea')[0].props.value, '  必填原因  ');
  assert.equal(button(page, '确认封禁').props.disabled, false);
  assert.deepEqual(calls, [{ action: 'ban', id: '1', reason: '必填原因' }]);
  assert.equal(pageErrors.length, 0);
  api.banUser = async (id, reason) => { calls.push({ action: 'ban', id, reason }); };
  const retry = click(page, '确认封禁');
  assert.doesNotMatch(textOf(modal(page, 'ban')), /封禁失败/);
  await retry; page.render(); assert.equal(modal(page, 'ban'), undefined);
  check('ban failure is visible inside the modal, keeps target/reason and retries without a page-load error');

  let rejectBan;
  setup({ banUser: (id, reason) => {
    calls.push({ action: 'ban', id, reason });
    return new Promise((_, reject) => { rejectBan = reject; });
  } });
  page = await boot(); openBan(page, 1); reason(page, 'A reason');
  const pendingBan = click(page, '确认封禁'); click(page, '取消'); openBan(page, 2);
  rejectBan(new Error('late A failure')); await pendingBan; page.render();
  assert.match(textOf(modal(page, 'ban')), /User 2（ID 2）/);
  assert.doesNotMatch(textOf(modal(page, 'ban')), /late A|封禁失败/);
  assert.equal(nodes(modal(page, 'ban'), node => node.type === 'textarea')[0].props.value, '');
  assert.equal(pageErrors.length, 0);
  check('closing pending ban A and opening B isolates late errors and starts B with clean input');

  setup(); page = await boot(); openBan(page, 3);
  assert.equal(button(page, '确认解封').props.disabled, false);
  await click(page, '确认解封'); page.render();
  assert.deepEqual(calls, [{ action: 'unban', id: '3', reason: '' }]);
  check('unban remains allowed with an optional explanation and uses the original API payload');

  setup(); page = await boot(); click(page, '账号合并预检');
  input(page, '例如 25', ' 1 '); input(page, '例如 13', ' 2 ');
  await click(page, '开始预检'); page.render();
  assert.match(textOf(modal(page, 'merge')), /合并预检失败：fixture merge failure/);
  assert.equal(input(page, '例如 25').props.value, ' 1 ');
  assert.equal(input(page, '例如 13').props.value, ' 2 ');
  assert.equal(button(page, '开始预检').props.disabled, false);
  assert.equal(pageErrors.length, 0);
  assert.deepEqual(calls, [{ action: 'precheck', from: '1', to: '2' }]);
  api.getUserMergePrecheck = async (from, to) => mergeResult(from, to);
  const retryMerge = click(page, '开始预检');
  assert.doesNotMatch(textOf(modal(page, 'merge')), /合并预检失败/);
  await retryMerge; page.render();
  assert.match(textOf(modal(page, 'merge')), /源账号 #1（User 1）/);
  input(page, '例如 25', '3');
  assert.equal(nodes(page.tree, node => node.props?.className === 'merge-result').length, 0);
  check('merge precheck errors preserve both IDs, retry locally and remove old comparison when IDs change');

  let resolveMerge;
  setup({ getUserMergePrecheck: () => new Promise(resolve => { resolveMerge = resolve; }) });
  page = await boot(); click(page, '账号合并预检');
  input(page, '例如 25', '1'); input(page, '例如 13', '2');
  const pendingMerge = click(page, '开始预检'); click(page, '关闭');
  click(page, '账号合并预检'); input(page, '例如 25', '3'); input(page, '例如 13', '2');
  resolveMerge(mergeResult('1', '2')); await pendingMerge; page.render();
  assert.equal(nodes(page.tree, node => node.props?.className === 'merge-result').length, 0);
  assert.equal(input(page, '例如 25').props.value, '3');
  assert.equal(button(page, '开始预检').props.disabled, false);
  assert.equal(pageErrors.length, 0);
  check('a cancelled precheck cannot populate a reopened dialog with another pair of accounts');

  setup(); page = await boot(); openBan(page, 1); reason(page, 'once');
  const banHandler = button(page, '确认封禁').props.onClick;
  const firstBan = banHandler(); const duplicateBan = banHandler();
  assert.equal(calls.length, 1); await Promise.all([firstBan, duplicateBan]);
  setup(); page = await boot(); click(page, '账号合并预检');
  input(page, '例如 25', '1'); input(page, '例如 13', '2');
  const mergeHandler = button(page, '开始预检').props.onClick;
  const firstMerge = mergeHandler(); const duplicateMerge = mergeHandler();
  assert.equal(calls.length, 1); await Promise.all([firstMerge, duplicateMerge]);
  check('same-frame duplicate ban and merge-precheck handlers each issue exactly one request');

});
