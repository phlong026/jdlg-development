import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const React = require('react');
const root = new URL('../', import.meta.url);
const flush = () => new Promise(resolve => setImmediate(resolve));
function load(file, overrides = {}, globals = {}) {
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(new URL(file, root), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true },
  }).outputText;
  vm.runInNewContext(code, {
    module, exports: module.exports,
    require: name => name in overrides ? overrides[name] : require(name),
    Date, URL, Error, AbortController, AbortSignal, setTimeout, clearTimeout, ...globals,
  });
  return module.exports;
}
const model = load('src/pages/shareTracking/model.ts');
const visitor = { visitor_key: 'visitor-1', identity_state: 'identified', nickname: '客户', page_views: 1, visible_ms: 10, session_count: 1, intent: { level: 'low', reasons: [], rule_version: 'rule-1' } };
const content = { content_id: 'content-1', title: '素材', type: 'article', version: 1, revision: 1, status: 'published', body: '正文', media: [], page_views: 1, visible_ms: 10 };
const nodes = tree => Array.isArray(tree) ? tree.flatMap(nodes) : !tree || typeof tree !== 'object' ? [] : [tree, ...nodes(tree.props?.children)];
const text = tree => Array.isArray(tree) ? tree.map(text).join('') : tree == null || typeof tree === 'boolean' ? '' : typeof tree === 'object' ? text(tree.props?.children) : String(tree);
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }

// Execute the actual components and useResource hook. The lightweight React dispatcher
// commits effects after rendering, including cleanup, dependency changes, and unmount.
function harness(kind = 'records', overrides = {}, initial = {}) {
  const slots = []; let index = 0, dirty = true, tree, pending = [], serial = 0;
  const calls = [];
  const react = {
    ...React,
    useState(init) {
      const i = index++;
      if (!(i in slots)) slots[i] = { value: typeof init === 'function' ? init() : init };
      return [slots[i].value, value => {
        const next = typeof value === 'function' ? value(slots[i].value) : value;
        if (!Object.is(next, slots[i].value)) { slots[i].value = next; dirty = true; }
      }];
    },
    useRef(init) { const i = index++; if (!(i in slots)) slots[i] = { current: init }; return slots[i]; },
    useMemo(factory, deps) {
      const i = index++;
      if (!slots[i] || deps.some((value, j) => !Object.is(value, slots[i].deps[j]))) slots[i] = { value: factory(), deps };
      return slots[i].value;
    },
    useEffect(effect, deps) {
      const i = index++;
      if (!slots[i] || !deps || deps.some((value, j) => !Object.is(value, slots[i].deps[j]))) {
        const previous = slots[i]; slots[i] = { deps, cleanup: previous?.cleanup };
        pending.push(() => { previous?.cleanup?.(); slots[i].cleanup = effect(); });
      }
    },
  };
  const actual = load('src/pages/shareTracking/components.tsx', { react, './model': model, './evidence': { IntentEvidenceDetails: 'IntentEvidenceDetails' } });
  const components = Object.fromEntries(Object.keys(actual).map(name => [name, name === 'useResource' ? actual[name] : name]));
  function page(method, params = {}) {
    const as_of = params.as_of || `2026-10-11T00:00:${String(++serial).padStart(2, '0')}Z`;
    return { as_of, metric_version: 'v1', first_cursor: method === 'contents' ? undefined : `${method}:${as_of}:first`, next_cursor: `${method}:${as_of}:next`, items: [method === 'contents' ? content : visitor], identified_count: 1, anonymous_count: 0 };
  }
  const api = Object.fromEntries(['visitors', 'contents', 'contentVisitors', 'content'].map(method => [method, (...args) => {
    const params = method.startsWith('content') && method !== 'contents' ? args[1] : args[0];
    const signal = args[args.length - 1];
    const call = { method, params: { ...params }, signal, id: method.startsWith('content') && method !== 'contents' ? args[0] : undefined };
    calls.push(call);
    return overrides[method] ? overrides[method](call, () => page(method, params)) : Promise.resolve(method === 'content' ? content : page(method, params));
  }]));
  const context = { can_view: true, can_manage: true, owners: [{ id: 'owner-1', name: '销售一' }, { id: 'owner-2', name: '销售二' }] };
  const props = kind === 'records' ? { canManage: true, context } : { content, query: {}, manage: true, edit() {}, context, changed() {}, ...initial };
  const saved = JSON.stringify({ filters: { ...model.defaultFilters, range: 'all', ...initial.filters }, view: initial.view || 'visitors' });
  const globals = { sessionStorage: { getItem: () => saved, setItem() {} }, document: { querySelector: () => ({ scrollTop: 5, scrollTo() {} }) }, window: { confirm: () => true } };
  const module = kind === 'records'
    ? load('src/pages/shareTracking.tsx', { react, '../api/shareTracking': { tracking: api }, './shareTracking/components': components, './shareTracking/details': { VisitorDetails: 'VisitorDetails', ContentDetails: 'ContentDetails' }, './shareTracking/crmTarget': { CRMTargetButton: 'CRMTargetButton' },
    './shareTracking/visitorContents': { VisitorContentPanel: 'VisitorContentPanel' },
    './shareTracking/editor': { ContentEditor: 'ContentEditor' }, './shareTracking/rules': { IntentRulesPanel: 'IntentRulesPanel' }, './shareTracking/model': model, './shareTracking.scss': {} }, globals)
    : load('src/pages/shareTracking/details.tsx', { react, '../../api/shareTracking': { tracking: api }, './components': components, './crmTarget': { CRMTargetButton: 'CRMTargetButton' }, './governance': { VisitorGovernance: 'VisitorGovernance' }, './model': model }, globals);
  function render() {
    let renders = 0;
    do {
      assert.ok(++renders < 20, 'component render settles'); dirty = false; index = 0; pending = [];
      tree = kind === 'records' ? module.TrackingRecords(props) : module.ContentDetails(props);
      pending.forEach(effect => effect());
    } while (dirty);
    return tree;
  }
  const find = (type, label = '') => nodes(tree).find(node => node.type === type && text(node).includes(label));
  const pager = () => find('Pager').props;
  render();
  return {
    calls, page, render, props, find, pager,
    last: (method = kind === 'records' ? 'visitors' : 'contentVisitors') => calls.filter(call => call.method === method).at(-1),
    async settle() { await flush(); render(); },
    async act(action) { action(); render(); await flush(); render(); },
    async next() { await this.act(() => pager().onNext(pager().next)); },
    async previous() { await this.act(() => pager().onPrevious()); },
    async click(label) { await this.act(() => { const button = find('button', label); assert.ok(button, label); button.props.onClick(); }); },
    async filter(label, value) { await this.act(() => { const parent = find('label', label); assert.ok(parent, label); nodes(parent).find(node => node.type === 'select').props.onChange({ target: { value } }); }); },
    unmount() { slots.forEach(slot => slot.cleanup?.()); },
  };
}

test('visitor return to page one replays original as_of and first_cursor, including repeated trips', async () => {
  const h = harness(); await h.settle();
  const first = h.find('SnapshotInfo').props.data;
  assert.equal(h.last().params.as_of, undefined); assert.equal(h.last().params.cursor, undefined);
  for (let trip = 0; trip < 2; trip++) {
    await h.next(); assert.equal(h.last().params.as_of, first.as_of); assert.equal(h.last().params.cursor, first.next_cursor);
    await h.previous(); assert.equal(h.last().params.as_of, first.as_of); assert.equal(h.last().params.cursor, first.first_cursor); assert.equal(h.pager().cursor.length, 0);
  }
});

test('material view pins as_of when returning to page one without first_cursor', async () => {
  const h = harness('records', {}, { view: 'contents' }); await h.settle(); const first = h.find('SnapshotInfo').props.data;
  await h.next(); assert.equal(h.last('contents').params.as_of, first.as_of);
  await h.previous(); assert.equal(h.last('contents').params.as_of, first.as_of); assert.equal(h.last('contents').params.cursor, undefined);
});

test('explicit refresh, range, owner, sort, search, clear, and business changes start fresh snapshots', async () => {
  const h = harness(); await h.settle();
  const assertFresh = () => { assert.equal(h.last().params.cursor, undefined); assert.equal(h.last().params.as_of, undefined); assert.equal(h.pager().cursor.length, 0); };
  await h.next(); await h.click('刷新记录'); assertFresh();
  await h.next(); await h.click('近3天'); assertFresh(); assert.ok(h.last().params.start);
  await h.next(); await h.filter('跟进归属', 'owner-2'); assertFresh(); assert.equal(h.last().params.owner, 'owner-2');
  await h.next(); await h.filter('排序', 'visible_ms'); assertFresh(); assert.equal(h.last().params.sort, 'visible_ms');
  await h.next(); await h.act(() => h.find('input').props.onChange({ target: { value: '客户编号' } }));
  await h.act(() => h.find('form').props.onSubmit({ preventDefault() {} })); assertFresh(); assert.equal(h.last().params.q, '客户编号');
  await h.next(); await h.click('清除筛选'); assertFresh(); assert.equal(h.last().params.owner, '');
  await h.next(); await h.click('意向规则与重算'); await h.act(() => h.find('IntentRulesPanel').props.changed()); assertFresh();
});

test('view switches reset cursor while retaining supported identity and omitting customer-only intent', async () => {
  const h = harness(); await h.settle(); await h.filter('身份', 'anonymous'); await h.filter('意向参考', 'high'); await h.next();
  await h.click('素材视角');
  assert.equal(h.last('contents').params.cursor, undefined); assert.equal(h.last('contents').params.as_of, undefined);
  assert.equal(h.last('contents').params.identity, 'anonymous'); assert.equal(h.last('contents').params.intent, undefined); assert.equal(h.last('contents').params.q, undefined);
  await h.next(); await h.click('客户视角');
  assert.equal(h.last().params.cursor, undefined); assert.equal(h.last().params.as_of, undefined); assert.equal(h.pager().cursor.length, 0);
});

test('failed pagination and page-one retry keep the same snapshot; refresh failure retries fresh', async () => {
  let fail = false;
  const h = harness('records', { visitors: (_call, result) => fail ? Promise.reject(new Error('读取失败')) : Promise.resolve(result()) });
  await h.settle(); const first = h.find('SnapshotInfo').props.data;
  fail = true; await h.next(); assert.equal(h.find('Feedback').props.error, '读取失败');
  fail = false; await h.act(() => h.find('Feedback').props.retry());
  assert.equal(h.last().params.as_of, first.as_of); assert.equal(h.last().params.cursor, first.next_cursor);
  fail = true; await h.previous(); assert.equal(h.find('Feedback').props.error, '读取失败');
  fail = false; await h.act(() => h.find('Feedback').props.retry());
  assert.equal(h.last().params.as_of, first.as_of); assert.equal(h.last().params.cursor, first.first_cursor);
  fail = true; await h.click('刷新记录'); fail = false; await h.act(() => h.find('Feedback').props.retry());
  assert.equal(h.last().params.as_of, undefined); assert.equal(h.last().params.cursor, undefined);
});

test('late superseded response cannot change visible data or pollute fresh snapshot', async () => {
  const old = deferred(); let firstRequest = true;
  const h = harness('records', { visitors: (_call, result) => { if (firstRequest) { firstRequest = false; return old.promise; } return Promise.resolve(result()); } });
  const oldCall = h.last(); await h.filter('跟进归属', 'owner-2'); const current = h.find('SnapshotInfo').props.data;
  assert.equal(oldCall.signal.aborted, true);
  old.resolve({ ...h.page('visitors'), as_of: 'stale-as-of', first_cursor: 'stale-first' }); await h.settle();
  assert.equal(h.find('SnapshotInfo').props.data.as_of, current.as_of);
  await h.next(); await h.previous(); assert.equal(h.last().params.as_of, current.as_of); assert.equal(h.last().params.cursor, current.first_cursor);
});

test('content visitor detail returns to first_cursor with original snapshot and passes scope to visitor details', async () => {
  const h = harness('detail', {}, { query: { owner: 'owner-1', as_of: '2026-10-11T00:00:00Z', limit: 20 } }); await h.settle();
  const first = h.find('SnapshotInfo').props.data;
  await h.next(); await h.previous();
  assert.equal(h.last().params.as_of, first.as_of); assert.equal(h.last().params.cursor, first.first_cursor); assert.equal(h.last().params.owner, 'owner-1'); assert.equal(h.last().params.limit, 20);
  await h.act(() => h.find('VisitorCard').props.onOpen());
  // This is the actual VisitorDetails function as a React element, with the pinned query.
  const child = nodes(h.render()).find(node => typeof node.type === 'function' && node.type.name === 'VisitorDetails');
  assert.equal(child.props.query.as_of, first.as_of); assert.equal(child.props.query.owner, 'owner-1');
});

test('content visitor scope and material changes discard old cursor immediately, with safe failed retries', async () => {
  let fail = false;
  const h = harness('detail', { contentVisitors: (_call, result) => fail ? Promise.reject(new Error('列表失败')) : Promise.resolve(result()) }); await h.settle();
  const first = h.find('SnapshotInfo').props.data; await h.next(); fail = true; await h.previous();
  fail = false; await h.act(() => nodes(h.render()).filter(node => node.type === 'Feedback').at(-1).props.retry());
  assert.equal(h.last().params.as_of, first.as_of); assert.equal(h.last().params.cursor, first.first_cursor);
  await h.next(); await h.act(() => { h.props.query = { owner: 'owner-2', as_of: '2026-10-11T01:00:00Z', limit: 20 }; });
  assert.equal(h.last().params.cursor, undefined); assert.equal(h.last().params.as_of, h.props.query.as_of); assert.equal(h.last().params.owner, 'owner-2'); assert.equal(h.pager().cursor.length, 0);
  await h.next(); await h.act(() => { h.props.content = { ...content, content_id: 'content-2' }; });
  assert.equal(h.last().id, 'content-2'); assert.equal(h.last().params.cursor, undefined); assert.equal(h.pager().cursor.length, 0);
});

test('detail pending response is aborted after scope change and cannot replace current snapshot', async () => {
  const old = deferred(); let initial = true;
  const h = harness('detail', { contentVisitors: (_call, result) => { if (initial) { initial = false; return old.promise; } return Promise.resolve(result()); } });
  const oldCall = h.last(); await h.act(() => { h.props.query = { owner: 'owner-2' }; });
  const current = h.find('SnapshotInfo').props.data; assert.equal(oldCall.signal.aborted, true);
  old.resolve({ ...h.page('contentVisitors'), as_of: 'stale-as-of', first_cursor: 'stale-first' }); await h.settle();
  await h.next(); await h.previous(); assert.equal(h.last().params.as_of, current.as_of); assert.equal(h.last().params.cursor, current.first_cursor);
  const last = h.last(); h.unmount(); assert.equal(last.signal.aborted, true);
});

test('detail scope round trips cannot resurrect an earlier page stack or snapshot', async () => {
  const h = harness('detail'); await h.settle(); await h.next();
  const originalScope = { ...h.props.query };
  await h.act(() => { h.props.query = { owner: 'owner-2' }; });
  await h.act(() => { h.props.query = originalScope; });
  assert.equal(h.last().params.as_of, undefined); assert.equal(h.last().params.cursor, undefined); assert.equal(h.pager().cursor.length, 0);
  await h.next();
  await h.act(() => { h.props.content = { ...content, content_id: 'content-2' }; });
  await h.act(() => { h.props.content = content; });
  assert.equal(h.last().params.as_of, undefined); assert.equal(h.last().params.cursor, undefined); assert.equal(h.pager().cursor.length, 0);
});

test('saved list pagination callbacks cannot inject old tokens after a new filter or refresh, even before render', async () => {
  const h = harness(); await h.settle(); const stale = h.pager();
  await h.act(() => { h.find('button', '刷新记录').props.onClick(); stale.onNext(stale.next); });
  assert.equal(h.last().params.cursor, undefined); assert.equal(h.pager().cursor.length, 0);
  const priorOwner = h.pager(); await h.filter('跟进归属', 'owner-2'); const requestCount = h.calls.length;
  await h.act(() => priorOwner.onNext(priorOwner.next));
  assert.equal(h.calls.length, requestCount); assert.equal(h.pager().cursor.length, 0);
});

test('saved material visitor pagination stays invalid after scope round trips', async () => {
  const h = harness('detail'); await h.settle(); const stale = h.pager();
  await h.act(() => { h.props.query = { owner: 'owner-2' }; });
  await h.act(() => { h.props.query = {}; }); const requestCount = h.calls.length;
  await h.act(() => stale.onNext(stale.next));
  assert.equal(h.calls.length, requestCount); assert.equal(h.pager().cursor.length, 0);
});

test('material title and visitor inputs submit distinct AND filters, preserve identity and reset snapshot',async()=>{
 const h=harness();await h.settle();await h.filter('身份','anonymous');await h.click('素材视角');await h.next();
 const form=h.find('form');const inputs=nodes(form).filter(n=>n.type==='input');assert.equal(inputs.length,2);
 await h.act(()=>inputs[0].props.onChange({target:{value:'合成文章'}}));await h.act(()=>nodes(h.find('form')).filter(n=>n.type==='input')[1].props.onChange({target:{value:'匿名标识'}}));
 await h.act(()=>h.find('form').props.onSubmit({preventDefault(){}}));const p=h.last('contents').params;
 assert.equal(p.content_q,'合成文章');assert.equal(p.visitor_q,'匿名标识');assert.equal(p.identity,'anonymous');assert.equal(p.q,undefined);assert.equal(p.cursor,undefined);assert.equal(p.as_of,undefined);
});
test('visitor card material drilldown carries actual version and fixed observation scope',async()=>{
 const h=harness();await h.settle();const panel=h.find('VisitorContentPanel');assert.equal(panel.props.visitor,'visitor-1');assert.ok(panel.props.query.as_of);
 await h.act(()=>panel.props.onOpen({content_id:'visited-villa',content_version:3}));const detail=h.find('VisitorDetails');assert.equal(detail.props.contentId,'visited-villa');assert.equal(detail.props.query.content_version,3);assert.equal(detail.props.query.as_of,panel.props.query.as_of);
});

test('profile revision conflict offers an explicit fresh snapshot rather than replaying rejected cursor',async()=>{
 let fail=false;const h=harness('records',{visitors:(_call,result)=>fail?Promise.reject(new Error('访客资料或素材搜索范围已变化，请刷新')):Promise.resolve(result())});await h.settle();fail=true;await h.next();assert.match(h.find('Feedback').props.error,/请刷新/);fail=false;await h.act(()=>h.find('Feedback').props.resetSnapshot());assert.equal(h.last().params.cursor,undefined);assert.equal(h.last().params.as_of,undefined);assert.equal(h.pager().cursor.length,0);
});
test('content visitor profile refresh clears rejected first cursor while retaining content scope',async()=>{
 let fail=false;const h=harness('details',{contentVisitors:(_call,result)=>fail?Promise.reject(new Error('访客资料或素材搜索范围已变化，请刷新')):Promise.resolve(result())},{query:{owner:'owner-1',as_of:'2026-10-12T00:00:00Z'}});await h.settle();fail=true;await h.next();fail=false;await h.act(()=>nodes(h.render()).find(n=>n.type==='Feedback'&&n.props.error).props.resetSnapshot());assert.equal(h.last('contentVisitors').params.cursor,undefined);assert.equal(h.last('contentVisitors').params.owner,'owner-1');
});
