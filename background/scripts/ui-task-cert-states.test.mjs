// Production component/handler regressions with controlled hooks and promises.
// No browser, backend, real support tickets, certification actions, or network.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import test from 'node:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('..', import.meta.url));
const require = createRequire(path.join(root, 'package.json'));
const React = require('react');
const ts = require('typescript');
const flush = () => new Promise(resolve => setImmediate(resolve));
const time = '2026-10-10T10:00:00Z';
const taskItem = (id, type = 'image') => ({ id, user_id: id + 1000, task_no: `${type}-${id}`, username: `task-user-${id}`, status: 'failed', stones_used: 12, error_message: 'fixture task error', created_at: time, updated_at: time });
const taskDetail = (id, type = 'image') => ({ ...taskItem(id, type), type });
const application = id => ({ id, user_id: id + 1000, type: 'designer', real_name: `masked-name-${id}`, id_card_no: `masked-card-${id}`, company_name: '', credit_code: '', legal_person: '', aliyun_passed: true, aliyun_msg: '', extra_docs_remark: '', status: 'pending_review', admin_remark: '', created_at: time, updated_at: time });
const certDetail = id => ({ application: application(id), user: { id: id + 1000, username: `cert-user-${id}`, can_withdraw: false } });
const revealedFields = id => ({ id, real_name: `revealed-name-${id}`, id_card_no: `revealed-card-${id}`, credit_code: '', legal_person: '' });
function deferred() { let resolve, reject; const promise = new Promise((res, rej) => { resolve = res; reject = rej; }); return { promise, resolve, reject }; }
function nodes(tree, predicate) {
  const found = [];
  const walk = node => { if (Array.isArray(node)) return node.forEach(walk); if (!node || typeof node !== 'object') return; if (predicate(node)) found.push(node); walk(node.props?.children); };
  walk(tree); return found;
}
function textOf(node) { if (node == null || typeof node === 'boolean') return ''; if (Array.isArray(node)) return node.map(textOf).join(''); return typeof node === 'object' ? textOf(node.props?.children) : String(node); }
const byClass = (tree, value) => nodes(tree, node => String(node.props?.className || '').split(/\s+/).includes(value));
function button(tree, label) { const matches = nodes(tree, node => node.type === 'button' && textOf(node) === label); assert.equal(matches.length, 1, `Expected one ${label} button`); return matches[0]; }
function click(page, node) { assert.ok(node); assert.notEqual(node.props.disabled, true, 'Click target enabled'); node.props.onClick(); page.commit(); }
function row(page, id) { return nodes(page.tree, node => node.type === 'tr' && (node.key === String(id) || node.key?.endsWith(`-${id}`)))[0]; }
const taskModal = page => byClass(page.tree, 'task-detail-modal')[0];
const certDrawer = page => byClass(page.tree, 'cert-drawer')[0];
function mount(kind, initialSearch = '', { emptyList = false, deferLists = false } = {}) {
  const page = { slots: [], cursor: 0, pending: [], tree: null, dirty: false, mounted: true, afterUnmountWrites: 0, detailRequests: [], listRequests: [], listCalls: [], revealRequests: [], actionRequests: [], actions: [], notices: [], navigations: [], errors: [] };
  let searchParams = new URLSearchParams(initialSearch);
  const changed = () => { if (!page.mounted) page.afterUnmountWrites++; page.dirty = true; };
  const memo = (factory, deps) => { const index = page.cursor++; const previous = page.slots[index]; if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) page.slots[index] = { deps, value: factory() }; return page.slots[index].value; };
  const react = { ...React,
    useState(initial) { const index = page.cursor++; if (!(index in page.slots)) page.slots[index] = typeof initial === 'function' ? initial() : initial; return [page.slots[index], value => { const next = typeof value === 'function' ? value(page.slots[index]) : value; if (!Object.is(next, page.slots[index])) { page.slots[index] = next; changed(); } }]; },
    useRef(initial) { const index = page.cursor++; if (!(index in page.slots)) page.slots[index] = { current: initial }; return page.slots[index]; },
    useCallback(callback, deps) { return memo(() => callback, deps); },
    useMemo: memo,
    useEffect(effect, deps) { const index = page.cursor++; const previous = page.slots[index]; if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) { page.slots[index] = { deps, cleanup: previous?.cleanup }; page.pending.push(() => { page.slots[index].cleanup?.(); page.slots[index].cleanup = effect(); }); } },
  };
  react.default = react; react.__esModule = true;
  const request = (bucket, metadata) => { const item = { ...metadata, ...deferred() }; page[bucket].push(item); return item.promise; };
  const action = (method, ...args) => { page.actions.push({ method, args }); return request('actionRequests', { method, args }); };
  const listResponse = type => ({ list: emptyList ? [] : [101, 202].map(id => taskItem(id, type)), total: 2, page: 1, page_size: 20, summary: { total_count: 2 }, queues: {} });
  const loadList = (type, params) => {
    page.listCalls.push({ type, params });
    return deferLists ? request('listRequests', { type, params }) : Promise.resolve(listResponse(type));
  };
  page.listResponse = listResponse;
  const taskApi = {
    getAITaskList: params => loadList('image', params), getAIVideoTaskList: params => loadList('video', params),
    getAITaskDetail: id => request('detailRequests', { id, type: 'image' }), getAIVideoTaskDetail: id => request('detailRequests', { id, type: 'video' }),
    backfillAITaskModels: limit => action('backfill', limit),
    createAITaskSupportTicket: id => action('image-ticket', id), createAIVideoTaskSupportTicket: id => action('video-ticket', id), retryAITask: id => action('retry', id),
  };
  const certApi = {
    getCertificationList: async () => ({ list: [501, 502].map(application), total: 2 }), getCertificationStats: async () => ({ pending_review: 2, total: 2 }),
    getCertificationDetail: id => request('detailRequests', { id }), revealCertificationFields: id => request('revealRequests', { id }), reviewCertification: (...args) => action('review', ...args),
  };
  const supportApi = { getSupportTicketDetail: async id => ({ id: Number(id), status: 'open', resolution_note: 'fixture original note' }), updateSupportTicketResolutionNote: (...args) => action('ticket-note', ...args), updateSupportTicketStatus: (...args) => action('ticket-status', ...args) };
  const module = { exports: {} };
  const sourcePath = (kind === 'task' ? process.env.JDLG_TASK_SOURCE : process.env.JDLG_CERT_SOURCE) || path.join(root, `src/pages/${kind === 'task' ? 'aiTaskCenter' : 'certification'}.tsx`);
  const javascript = ts.transpileModule(fs.readFileSync(sourcePath, 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText;
  const context = vm.createContext({ module, exports: module.exports, URLSearchParams, console: { ...console, error() {} },
    alert: text => page.notices.push(text), window: { location: { pathname: kind === 'task' ? '/ai-tasks' : '/certification' }, confirm: () => true },
    require(name) {
      if (name === 'react') return react;
      if (name === 'react-router-dom') return { useNavigate: () => value => page.navigations.push(value), useSearchParams: () => [searchParams, next => { searchParams = typeof next === 'function' ? next(searchParams) : next; changed(); }] };
      if (name === 'react-icons/fi') return new Proxy({}, { get: (_, key) => key });
      if (name === '../component/managementSearchPanel') return { __esModule: true, default: 'ManagementSearchPanel' };
      if (name === '../api/aiTasks') return taskApi;
      if (name === '../api/certification') return certApi;
      if (name === '../api/supportTickets') return supportApi;
      if (name === '../utils/pageLoadError') return { reportPageLoadError: (...args) => page.errors.push(args), reportPageLoadRecovered() {} };
      if (name.includes('useIsMobile')) return {useIsMobile:()=>false};
  if (name.endsWith('.scss')) return {};
      throw new Error(`Unexpected import: ${name}`);
    },
  });
  vm.runInContext(javascript, context, { filename: sourcePath });
  page.commit = () => { let cycles = 0; do { assert.ok(cycles++ < 30, 'Effects settle'); page.dirty = false; page.cursor = 0; page.tree = module.exports.default(); page.pending.splice(0).forEach(effect => effect()); } while (page.dirty); };
  page.readSearch = () => searchParams;
  page.setSearch = value => { searchParams = new URLSearchParams(value); page.commit(); };
  page.unmount = () => { page.slots.forEach(slot => { if (typeof slot?.cleanup === 'function') slot.cleanup(); }); page.mounted = false; };
  page.commit(); return page;
}
async function boot(kind, initialSearch, options) { const page = mount(kind, initialSearch, options); await flush(); page.commit(); return page; }
async function settle(page, request, value, reject = false) { request[reject ? 'reject' : 'resolve'](value); await flush(); page.commit(); }
function viewTask(page, id) { click(page, button(row(page, id), '查看详情')); }
function viewCert(page, id) { click(page, byClass(row(page, id), 'btn-view')[0]); }
function closeCert(page) { click(page, byClass(certDrawer(page), 'cert-drawer-close')[0]); }

test('loading a video detail cannot create an image support ticket through the footer', async () => {
  const page = await boot('task');
  click(page, button(page.tree, '视频任务')); await flush(); page.commit();
  viewTask(page, 101);
  const controls = nodes(taskModal(page), node => node.type === 'button' && textOf(node) === '转异常工单');
  if (controls[0]) controls[0].props.onClick();
  await flush();
  assert.deepEqual(page.actions, [], 'The loading detail is not an actionable image task');
  assert.ok(!controls[0] || controls[0].props.disabled, 'Footer creation disabled until correct detail is loaded');
});

test('slow task A cannot replace fast task B or send B actions using A detail', async () => {
  const page = await boot('task');
  viewTask(page, 101); viewTask(page, 202);
  await settle(page, page.detailRequests[1], taskDetail(202));
  await settle(page, page.detailRequests[0], taskDetail(101));
  assert.match(textOf(taskModal(page)), /image-202/);
  assert.doesNotMatch(textOf(taskModal(page)), /image-101/);
  click(page, button(taskModal(page), '转异常工单'));
  assert.deepEqual(page.actions, [{ method: 'image-ticket', args: ['202'] }]);
});

test('task response identity includes both type and id before any detail action', async () => {
  for (const wrong of [taskDetail(202), taskDetail(101, 'video')]) {
    const page = await boot('task'); viewTask(page, 101);
    await settle(page, page.detailRequests[0], wrong);
    assert.match(textOf(taskModal(page)), /失败|不一致/);
    const controls = nodes(taskModal(page), node => node.type === 'button' && /^(转异常工单|重新执行（不扣费）|标记已)/.test(textOf(node)));
    assert.ok(controls.every(node => node.props.disabled));
    controls.forEach(node => node.props.onClick()); await flush();
    assert.equal(page.actions.length, 0);
  }
});

test('closing task detail invalidates its response rather than reopening it or retaining actions', async () => {
  const page = await boot('task'); viewTask(page, 101);
  click(page, button(taskModal(page), '关闭'));
  await settle(page, page.detailRequests[0], taskDetail(101));
  assert.equal(taskModal(page), undefined);
  assert.equal(page.readSearch().get('taskId'), null, 'Late response must not restore the closed task in the URL');
  assert.equal(page.actions.length, 0);
});

test('certification loading and failed detail cannot be approved from unverified material', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  const approve = button(certDrawer(page), '通过认证');
  approve.props.onClick(); await flush();
  assert.equal(page.actions.length, 0, 'Handler also blocks approval before material is available');
  assert.equal(approve.props.disabled, true);
  await settle(page, page.detailRequests[0], new Error('fixture detail failed'), true);
  assert.match(textOf(certDrawer(page)), /失败/);
  assert.equal(button(certDrawer(page), '通过认证').props.disabled, true);
});

test('certification close A/open B cannot display late A user material under B', async () => {
  const page = await boot('cert'); viewCert(page, 501); closeCert(page); viewCert(page, 502);
  await settle(page, page.detailRequests[1], certDetail(502));
  await settle(page, page.detailRequests[0], certDetail(501));
  assert.match(textOf(certDrawer(page)), /cert-user-502/);
  assert.doesNotMatch(textOf(certDrawer(page)), /cert-user-501/);
});

test('certification late reveal A cannot overwrite B or clear its pending reveal state', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  click(page, button(certDrawer(page), '查看完整（记审计）'));
  closeCert(page); viewCert(page, 502);
  await settle(page, page.detailRequests[1], certDetail(502));
  // B starts its own reveal; a stale A finally must not make B look finished.
  const reveal = byClass(certDrawer(page), 'reveal-link')[0];
  reveal.props.onClick(); page.commit();
  await settle(page, page.revealRequests[0], revealedFields(501));
  assert.doesNotMatch(textOf(certDrawer(page)), /revealed-(?:name|card)-501/);
  assert.equal(byClass(certDrawer(page), 'reveal-link')[0].props.disabled, true);
  await settle(page, page.revealRequests[1], revealedFields(502));
  assert.match(textOf(certDrawer(page)), /revealed-card-502/);
});

test('certification revealed fields remain in their drawer and never replace masked list rows', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  click(page, button(certDrawer(page), '查看完整（记审计）'));
  await settle(page, page.revealRequests[0], revealedFields(501));
  assert.match(textOf(certDrawer(page)), /revealed-card-501/);
  assert.match(textOf(row(page, 501)), /masked-card-501/);
  assert.match(textOf(row(page, 502)), /masked-card-502/);
  assert.doesNotMatch(textOf(row(page, 502)), /revealed-card-501/);
});

test('certification wrong detail ID cannot become reviewable material', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(502));
  assert.doesNotMatch(textOf(certDrawer(page)), /cert-user-502/);
  assert.match(textOf(certDrawer(page)), /失败|不一致/);
  button(certDrawer(page), '通过认证').props.onClick(); await flush();
  assert.equal(page.actions.length, 0);
});

test('certification duplicate review calls in one render submit once and finishing A never closes B', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  const approve = button(certDrawer(page), '通过认证').props.onClick;
  approve(); approve(); page.commit();
  assert.deepEqual(page.actions, [{ method: 'review', args: ['501', 'approve', undefined] }]);
  closeCert(page); viewCert(page, 502);
  await settle(page, page.detailRequests[1], certDetail(502));
  await settle(page, page.actionRequests[0], undefined);
  assert.match(textOf(certDrawer(page)), /认证申请 #502/);
  assert.match(textOf(certDrawer(page)), /cert-user-502/);
});

test('task and certification detail responses do not write after unmount', async () => {
  for (const kind of ['task', 'cert']) {
    const page = await boot(kind);
    if (kind === 'task') viewTask(page, 101); else viewCert(page, 501);
    page.unmount(); page.detailRequests[0].resolve(kind === 'task' ? taskDetail(101) : certDetail(501)); await flush();
    assert.equal(page.afterUnmountWrites, 0);
  }
});

test('URL selection with the same numeric task ID still isolates image from video requests and actions', async () => {
  const page = await boot('task');
  page.setSearch('tab=image&taskId=101');
  page.setSearch('tab=video&taskId=101');
  assert.deepEqual(page.detailRequests.map(({ id, type }) => ({ id, type })), [{ id: '101', type: 'image' }, { id: '101', type: 'video' }]);
  await settle(page, page.detailRequests[1], taskDetail(101, 'video'));
  await settle(page, page.detailRequests[0], taskDetail(101));
  assert.match(textOf(taskModal(page)), /video-101/);
  assert.doesNotMatch(textOf(taskModal(page)), /image-101|重新执行（不扣费）/);
  click(page, button(taskModal(page), '转异常工单'));
  assert.deepEqual(page.actions, [{ method: 'video-ticket', args: ['101'] }]);
});

test('failed task detail stays local and retries the explicit video target without defaulting to image', async () => {
  const page = await boot('task', 'tab=video&taskId=202');
  assert.deepEqual(page.detailRequests.map(({ id, type }) => ({ id, type })), [{ id: '202', type: 'video' }]);
  await settle(page, page.detailRequests[0], new Error('fixture video offline'), true);
  assert.match(textOf(taskModal(page)), /fixture video offline/);
  assert.equal(page.actions.length, 0);
  click(page, button(taskModal(page), '重新加载'));
  assert.equal(page.detailRequests[1].type, 'video');
  assert.equal(page.detailRequests[1].id, '202');
  await settle(page, page.detailRequests[1], taskDetail(202, 'video'));
  assert.match(textOf(taskModal(page)), /video-202/);
});

test('stale task success or failure cannot clear a newer loading state or replace its result', async () => {
  for (const reject of [false, true]) {
    const page = await boot('task'); viewTask(page, 101); viewTask(page, 202);
    await settle(page, page.detailRequests[0], reject ? new Error('stale task failure') : taskDetail(101), reject);
    assert.match(textOf(taskModal(page)), /正在加载任务详情/);
    assert.doesNotMatch(textOf(taskModal(page)), /stale task failure|image-101/);
    await settle(page, page.detailRequests[1], taskDetail(202));
    assert.match(textOf(taskModal(page)), /image-202/);
    assert.equal(page.errors.length, 0);
  }
});

test('stale task action callbacks cannot write after switching or reopening the same task', async () => {
  for (const next of [101, 202]) {
    const page = await boot('task'); viewTask(page, 101);
    await settle(page, page.detailRequests[0], taskDetail(101));
    const callbacks = ['转异常工单', '标记已排查', '重新执行（不扣费）'].map(label => button(taskModal(page), label).props.onClick);
    click(page, button(taskModal(page), '关闭')); viewTask(page, next);
    await settle(page, page.detailRequests[1], taskDetail(next));
    callbacks.forEach(callback => callback()); await flush(); page.commit();
    assert.equal(page.actions.length, 0);
    assert.match(textOf(taskModal(page)), new RegExp(`image-${next}`));
  }
});

test('task ticket creation is single-flight and late A success cannot navigate away from B', async () => {
  const page = await boot('task'); viewTask(page, 101);
  await settle(page, page.detailRequests[0], taskDetail(101));
  const create = button(taskModal(page), '转异常工单').props.onClick;
  create(); create(); page.commit();
  assert.deepEqual(page.actions, [{ method: 'image-ticket', args: ['101'] }]);
  page.setSearch('tab=video&taskId=202');
  await settle(page, page.detailRequests[1], taskDetail(202, 'video'));
  await settle(page, page.actionRequests[0], { id: 901, existed: false });
  assert.match(textOf(taskModal(page)), /video-202/);
  assert.deepEqual(page.navigations, []);
  assert.deepEqual(page.notices, []);
});

test('task ticket failure stays with the current task and releases the lock for one retry', async () => {
  const page = await boot('task'); viewTask(page, 101);
  await settle(page, page.detailRequests[0], taskDetail(101));
  click(page, button(taskModal(page), '转异常工单'));
  await settle(page, page.actionRequests[0], new Error('fixture ticket failed'), true);
  assert.match(textOf(taskModal(page)), /fixture ticket failed/);
  const retry = button(taskModal(page), '转异常工单').props.onClick;
  retry(); retry(); page.commit();
  assert.deepEqual(page.actions, [{ method: 'image-ticket', args: ['101'] }, { method: 'image-ticket', args: ['101'] }]);
  await settle(page, page.actionRequests[1], { id: 902, existed: true });
  assert.deepEqual(page.navigations, ['/support-tickets']);
});

test('quick disposition locks same-frame calls and keeps the original ticket protocol when A finishes under B', async () => {
  const page = await boot('task'); viewTask(page, 101);
  await settle(page, page.detailRequests[0], taskDetail(101));
  const quick = button(taskModal(page), '标记已补偿').props.onClick;
  quick(); quick(); page.commit();
  assert.equal(page.actions.length, 1);
  page.setSearch('tab=video&taskId=202');
  await settle(page, page.detailRequests[1], taskDetail(202, 'video'));
  await settle(page, page.actionRequests[0], { id: 903, existed: true });
  assert.equal(page.actions[1].method, 'ticket-note');
  assert.equal(page.actions[1].args[0], '903');
  assert.match(page.actions[1].args[1], /^fixture original note\n\[.+\] 已补偿：来自AI任务中心快捷处理$/);
  await settle(page, page.actionRequests[1], undefined);
  assert.deepEqual(page.actions[2], { method: 'ticket-status', args: ['903', 'in_progress'] });
  await settle(page, page.actionRequests[2], undefined);
  assert.match(textOf(taskModal(page)), /video-202/);
  assert.doesNotMatch(textOf(taskModal(page)), /已补偿已同步/);
  assert.equal(button(taskModal(page), '标记已排查').props.disabled, false);
});

test('retry retains its original image task payload, is single-flight, and never closes newer task B', async () => {
  const page = await boot('task'); viewTask(page, 101);
  await settle(page, page.detailRequests[0], taskDetail(101));
  const retry = button(taskModal(page), '重新执行（不扣费）').props.onClick;
  retry(); retry(); page.commit();
  assert.deepEqual(page.actions, [{ method: 'retry', args: [101] }]);
  page.setSearch('tab=video&taskId=202');
  await settle(page, page.detailRequests[1], taskDetail(202, 'video'));
  await settle(page, page.actionRequests[0], { new_task_no: 'retry-101' });
  assert.match(textOf(taskModal(page)), /video-202/);
  assert.equal(page.readSearch().get('taskId'), '202');
});

test('certification reveal validates its response ID, locks duplicate calls, and retries locally', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  const reveal = button(certDrawer(page), '查看完整（记审计）').props.onClick;
  reveal(); reveal(); page.commit();
  assert.equal(page.revealRequests.length, 1);
  await settle(page, page.revealRequests[0], revealedFields(502));
  assert.doesNotMatch(textOf(certDrawer(page)), /revealed-card-502/);
  assert.match(textOf(certDrawer(page)), /返回的完整材料与当前申请不一致/);
  click(page, button(certDrawer(page), '查看完整（记审计）'));
  assert.equal(page.revealRequests[1].id, '501');
  await settle(page, page.revealRequests[1], revealedFields(501));
  assert.match(textOf(certDrawer(page)), /revealed-card-501/);
});

test('certification late reveal cannot leak into a reopened instance of the same application', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  click(page, button(certDrawer(page), '查看完整（记审计）'));
  closeCert(page); viewCert(page, 501);
  await settle(page, page.detailRequests[1], certDetail(501));
  await settle(page, page.revealRequests[0], revealedFields(501));
  assert.doesNotMatch(textOf(certDrawer(page)), /revealed-card-501/);
  assert.match(textOf(certDrawer(page)), /masked-card-501/);
});

test('certification reject still requires a trimmed reason and failed review preserves it for deliberate retry', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  const reason = () => byClass(certDrawer(page), 'cert-remark-input')[0];
  reason().props.onChange({ target: { value: '   ' } }); page.commit();
  button(certDrawer(page), '拒绝').props.onClick(); await flush();
  assert.equal(page.actions.length, 0);
  assert.equal(button(certDrawer(page), '拒绝').props.disabled, true);
  reason().props.onChange({ target: { value: '  fixture missing document  ' } }); page.commit();
  const reject = button(certDrawer(page), '拒绝').props.onClick;
  reject(); reject(); page.commit();
  assert.deepEqual(page.actions, [{ method: 'review', args: ['501', 'reject', 'fixture missing document'] }]);
  await settle(page, page.actionRequests[0], new Error('fixture review offline'), true);
  assert.match(textOf(certDrawer(page)), /fixture review offline/);
  assert.equal(reason().props.value, '  fixture missing document  ');
  assert.equal(button(certDrawer(page), '拒绝').props.disabled, false);
  click(page, button(certDrawer(page), '拒绝'));
  assert.deepEqual(page.actions[1], { method: 'review', args: ['501', 'reject', 'fixture missing document'] });
  await settle(page, page.actionRequests[1], undefined);
  assert.equal(certDrawer(page), undefined);
});

test('certification stale review and reveal handlers cannot act on the previous application', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], certDetail(501));
  const approve = button(certDrawer(page), '通过认证').props.onClick;
  const reveal = button(certDrawer(page), '查看完整（记审计）').props.onClick;
  closeCert(page); viewCert(page, 502);
  await settle(page, page.detailRequests[1], certDetail(502));
  approve(); reveal(); await flush(); page.commit();
  assert.equal(page.actions.length, 0);
  assert.equal(page.revealRequests.length, 0);
  assert.match(textOf(certDrawer(page)), /cert-user-502/);
});

test('certification uses current response status and never reviews a record that is no longer pending', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  const response = certDetail(501); response.application.status = 'approved';
  await settle(page, page.detailRequests[0], response);
  assert.doesNotMatch(textOf(certDrawer(page)), /通过认证|拒绝/);
  assert.match(textOf(certDrawer(page)), /已通过/);
  assert.equal(page.actions.length, 0);
});

test('certification detail and reveal failures from A do not change B loading or errors', async () => {
  const page = await boot('cert'); viewCert(page, 501); closeCert(page); viewCert(page, 502);
  await settle(page, page.detailRequests[0], new Error('stale A detail failure'), true);
  assert.match(textOf(certDrawer(page)), /加载中/);
  assert.doesNotMatch(textOf(certDrawer(page)), /stale A detail failure/);
  await settle(page, page.detailRequests[1], certDetail(502));
  click(page, button(certDrawer(page), '查看完整（记审计）'));
  closeCert(page); viewCert(page, 501); await settle(page, page.detailRequests[2], certDetail(501));
  await settle(page, page.revealRequests[0], new Error('stale B reveal failure'), true);
  assert.doesNotMatch(textOf(certDrawer(page)), /stale B reveal failure/);
  assert.equal(page.errors.length, 0);
});

test('certification detail failure can retry the same target and then enable its original approval action', async () => {
  const page = await boot('cert'); viewCert(page, 501);
  await settle(page, page.detailRequests[0], new Error('fixture detail offline'), true);
  assert.equal(button(certDrawer(page), '通过认证').props.disabled, true);
  click(page, button(certDrawer(page), '重新加载'));
  assert.equal(page.detailRequests[1].id, '501');
  await settle(page, page.detailRequests[1], certDetail(501));
  assert.equal(button(certDrawer(page), '通过认证').props.disabled, false);
});

test('certification reveal and review responses never write state after unmount', async () => {
  for (const operation of ['reveal', 'review']) {
    for (const reject of [false, true]) {
      const page = await boot('cert'); viewCert(page, 501);
      await settle(page, page.detailRequests[0], certDetail(501));
      click(page, button(certDrawer(page), operation === 'reveal' ? '查看完整（记审计）' : '通过认证'));
      const request = operation === 'reveal' ? page.revealRequests[0] : page.actionRequests[0];
      page.unmount(); request[reject ? 'reject' : 'resolve'](reject ? new Error('after unmount') : operation === 'reveal' ? revealedFields(501) : undefined);
      await flush();
      assert.equal(page.afterUnmountWrites, 0);
    }
  }
});

test('empty task rows span the actual rendered header for image failed, other image queues, and video', async () => {
  for (const [search, video, expected] of [['', false, 9], ['queue=all', false, 8], ['', true, 8]]) {
    const page = await boot('task', search, { emptyList: true });
    if (video) { click(page, button(page.tree, '视频任务')); await flush(); page.commit(); }
    const table = byClass(page.tree, 'task-table')[0];
    const headings = nodes(table, node => node.type === 'th');
    const empty = nodes(table, node => node.type === 'td' && textOf(node) === '暂无任务记录');
    assert.equal(headings.length, expected);
    assert.equal(empty.length, 1);
    assert.equal(empty[0].props.colSpan, headings.length);
  }
});

test('initial video URL with status loads only video rows and keeps subsequent detail and ticket types consistent', async () => {
  for (const search of ['tab=video&status=failed', 'tab=video&status=failed&queue=failed']) {
    const page = await boot('task', search, { deferLists: true });
    assert.equal(page.listRequests.length, 1);
    assert.equal(page.listRequests[0].type, 'video');
    assert.equal(page.listRequests[0].params.queue, 'all');
    assert.equal(page.listRequests[0].params.status, 'failed');
    assert.equal(page.listCalls.some(call => call.type === 'image'), false);
    await settle(page, page.listRequests[0], page.listResponse('video'));
    assert.match(textOf(row(page, 202)), /video-202/);
    viewTask(page, 202);
    assert.equal(page.detailRequests[0].type, 'video');
    await settle(page, page.detailRequests[0], taskDetail(202, 'video'));
    click(page, button(taskModal(page), '转异常工单'));
    assert.deepEqual(page.actions, [{ method: 'video-ticket', args: ['202'] }]);
  }
});

test('switching image to video invalidates the old list and cannot display or act on its late image rows', async () => {
  for (const reject of [false, true]) {
    const page = await boot('task', '', { deferLists: true });
    assert.equal(page.listRequests[0].type, 'image');
    click(page, button(page.tree, '视频任务'));
    assert.equal(page.listRequests[1].type, 'video');
    await settle(page, page.listRequests[1], page.listResponse('video'));
    await settle(page, page.listRequests[0], reject ? new Error('late image list') : page.listResponse('image'), reject);
    assert.match(textOf(row(page, 202)), /video-202/);
    assert.doesNotMatch(textOf(page.tree), /image-101|image-202|late image list/);
    click(page, button(row(page, 202), '转异常工单'));
    assert.deepEqual(page.actions, [{ method: 'video-ticket', args: ['202'] }]);
    assert.equal(page.errors.length, 0);
  }
});

test('switching away from an already rendered image list clears its old rows until video is ready', async () => {
  const page = await boot('task', '', { deferLists: true });
  await settle(page, page.listRequests[0], page.listResponse('image'));
  assert.match(textOf(row(page, 101)), /image-101/);
  click(page, button(page.tree, '视频任务'));
  assert.equal(row(page, 101), undefined);
  assert.doesNotMatch(textOf(page.tree), /image-101/);
  await settle(page, page.listRequests[1], page.listResponse('video'));
  assert.match(textOf(row(page, 101)), /video-101/);
});

test('late backfill completion after switching to video cannot reload image rows or misroute a ticket', async () => {
  const page = await boot('task');
  click(page, button(page.tree, '回填历史模型'));
  assert.deepEqual(page.actions, [{ method: 'backfill', args: [500] }]);
  click(page, button(page.tree, '视频任务')); await flush(); page.commit();
  assert.match(textOf(row(page, 101)), /video-101/);
  const listsBeforeCompletion = page.listCalls.length;
  await settle(page, page.actionRequests[0], { inspected_count: 12, updated_count: 3 });
  assert.equal(page.listCalls.length, listsBeforeCompletion, 'A late backfill must not issue a new image list request');
  assert.match(textOf(row(page, 101)), /video-101/);
  assert.deepEqual(page.notices, ['本次检查 12 条成功图片任务，回填 3 条模型/接口信息。']);
  click(page, button(row(page, 101), '转异常工单'));
  assert.deepEqual(page.actions[1], { method: 'video-ticket', args: ['101'] });
});

test('backfill completion also respects a changed generation when the user returns from video to image', async () => {
  const page = await boot('task');
  click(page, button(page.tree, '回填历史模型'));
  click(page, button(page.tree, '视频任务')); await flush(); page.commit();
  click(page, button(page.tree, '图片任务')); await flush(); page.commit();
  const listsBeforeCompletion = page.listCalls.length;
  await settle(page, page.actionRequests[0], { inspected_count: 12, updated_count: 3 });
  assert.equal(page.listCalls.length, listsBeforeCompletion, 'Returning to the same type does not restore old request ownership');
  assert.match(textOf(row(page, 101)), /image-101/);
});

test('backfill still refreshes an unchanged image context and preserves its original API limit and result text', async () => {
  const page = await boot('task');
  click(page, button(page.tree, '回填历史模型'));
  const listsBeforeCompletion = page.listCalls.length;
  await settle(page, page.actionRequests[0], { inspected_count: 12, updated_count: 3 });
  assert.deepEqual(page.actions, [{ method: 'backfill', args: [500] }]);
  assert.equal(page.listCalls.length, listsBeforeCompletion + 1);
  assert.equal(page.listCalls.at(-1).type, 'image');
  assert.match(textOf(page.tree), /本次检查 12 条成功图片任务，回填 3 条模型\/接口信息。/);
  assert.deepEqual(page.notices, ['本次检查 12 条成功图片任务，回填 3 条模型/接口信息。']);
});

test('backfill success and failure after unmount cannot set feedback, notify, or start another list request', async () => {
  for (const reject of [false, true]) {
    const page = await boot('task');
    click(page, button(page.tree, '回填历史模型'));
    const listsBeforeCompletion = page.listCalls.length;
    page.unmount();
    page.actionRequests[0][reject ? 'reject' : 'resolve'](reject ? new Error('unmounted backfill') : { inspected_count: 12, updated_count: 3 });
    await flush();
    assert.equal(page.afterUnmountWrites, 0);
    assert.equal(page.listCalls.length, listsBeforeCompletion);
    assert.deepEqual(page.notices, []);
  }
});
