// T08 isolated integration: actual TS/TSX session, request, route and tracking code.
// fetch, browser storage and React effects are synthetic. No server, account,
// cookie issuance, mobile device or live authorization acceptance is claimed.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import ts from 'typescript';
import React from 'react';

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src');
const settle = () => new Promise(resolve => setImmediate(resolve));
const normalized = value => JSON.parse(JSON.stringify(value));
const nodes = value => Array.isArray(value) ? value.flatMap(nodes) : value == null || typeof value !== 'object' ? [] : [value, ...nodes(value.props?.children)];
const text = value => Array.isArray(value) ? value.map(text).join('') : value == null || typeof value === 'boolean' ? '' : typeof value === 'object' ? text(value.props?.children) : String(value);
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const response = (status, data, msg = '') => ({ ok: status >= 200 && status < 300, status, json: async () => ({ code: status === 200 ? 0 : status, data, msg }) });
const user = { id: 7001, username: 'synthetic-session-user', scope: 'crm_only', crmEnabled: false };
const capabilities = { can_view: true, can_manage: false, can_crm: false, can_rules: false, can_delete: false, owners: [{ id: 'synthetic-owner-a', name: '合成归属 A' }] };
const page = { items: [], as_of: '2026-10-12T00:00:00Z', metric_version: 'synthetic-v1', identified_count: 0, anonymous_count: 0 };

function storage() {
  const values = new Map();
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, String(value)), removeItem: key => values.delete(key), clear: () => values.clear() };
}
function environment({ width = 390, localStorage = storage(), sessionStorage = storage() } = {}) {
  let active;
  const events = [], calls = [], errors = [], cache = new Map();
  const window = new EventTarget();
  Object.assign(window, { innerWidth: width, location: { origin: 'https://synthetic.example.test', protocol: 'https:', pathname: '/admin/share-tracking', search: '' }, confirm: () => true });
  const dispatch = window.dispatchEvent.bind(window);
  window.dispatchEvent = event => { events.push(event.type); return dispatch(event); };
  const env = {
    window, localStorage, sessionStorage, events, calls, errors, location: { pathname: '/share-tracking' }, session: { user: { ...user }, loading: false, error: '', crmContext: null, crmError: '', refresh() {} },
    fetch: async url => {
      if (url.endsWith('/management/me') || url.endsWith('/management/login')) return response(200, { ...user });
      if (url.endsWith('/management/logout')) return response(200, {});
      if (url.endsWith('/tracking/context')) return response(200, { ...capabilities });
      return response(200, { ...page });
    },
  };
  const react = {
    ...React,
    createContext: () => ({ Provider: 'SyntheticSessionProvider' }),
    useContext: () => env.session,
    useState(initial) {
      const h = active, i = h.cursor++;
      if (!(i in h.slots)) h.slots[i] = { value: typeof initial === 'function' ? initial() : initial };
      return [h.slots[i].value, value => {
        if (h.unmounted) { h.lateUpdates++; return; }
        const next = typeof value === 'function' ? value(h.slots[i].value) : value;
        if (!Object.is(next, h.slots[i].value)) { h.slots[i].value = next; h.dirty = true; }
      }];
    },
    useRef(initial) { const h = active, i = h.cursor++; if (!(i in h.slots)) h.slots[i] = { current: initial }; return h.slots[i]; },
    useMemo(factory, deps) {
      const h = active, i = h.cursor++, previous = h.slots[i];
      if (!previous || deps.some((value, j) => !Object.is(value, previous.deps[j]))) h.slots[i] = { value: factory(), deps };
      return h.slots[i].value;
    },
    useEffect(effect, deps) {
      const h = active, i = h.cursor++, previous = h.slots[i];
      if (!previous || !deps || deps.some((value, j) => !Object.is(value, previous.deps[j]))) {
        h.slots[i] = { deps, cleanup: previous?.cleanup, effect };
        h.pending.push(() => { previous?.cleanup?.(); h.slots[i].cleanup = effect(); });
      }
    },
  };
  const globals = {
    window, localStorage, sessionStorage, document: { querySelector: () => ({ scrollTop: 0, scrollTo() {} }) },
    fetch: async (url, options = {}) => { calls.push({ url, options }); return env.fetch(url, options); },
    Date, URL, URLSearchParams, Event, Error, DOMException, AbortController, AbortSignal, FormData, Blob,
    setTimeout, clearTimeout, console: { ...console, error: (...args) => errors.push(args) },
  };
  const presentation = {
    './shareTracking/details': { ContentDetails: 'ContentDetails', VisitorDetails: 'VisitorDetails' },
    './shareTracking/crmTarget': { CRMTargetButton: 'CRMTargetButton' },
    './shareTracking/visitorContents': { VisitorContentPanel: 'VisitorContentPanel' },
    './shareTracking/contentVisitors': { ContentVisitorPanel: 'ContentVisitorPanel' },
    './shareTracking/editor': { ContentEditor: 'ContentEditor' },
    './shareTracking/rules': { IntentRulesPanel: 'IntentRulesPanel' },
    './evidence': { IntentEvidenceDetails: 'IntentEvidenceDetails' },
  };
  function load(filename) {
    const resolved = path.resolve(root, filename);
    if (cache.has(resolved)) return cache.get(resolved).exports;
    let source = fs.readFileSync(resolved, 'utf8');
    if (filename === 'config/api.ts') source = source.replace('import.meta.env.VITE_API_BASE_URL', "''");
    const module = { exports: {} }; cache.set(resolved, module);
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
    vm.runInNewContext(code, {
      ...globals, module, exports: module.exports,
      require(name) {
        if (name === 'react') return react;
        if (name === 'react-router-dom') return { Navigate: 'SyntheticNavigate', useLocation: () => env.location };
        if (name.endsWith('.scss')) return {};
        if (name in presentation) return presentation[name];
        if (name === './leadOverviewStore') return { resetLeadOverviewSession() {} };
        if (name === '../api/wecomCustomers') return { crm: async () => { throw new Error('CRM is outside this isolated session fixture'); } };
        if (name.startsWith('.')) {
          const base = path.resolve(path.dirname(resolved), name);
          const target = ['.ts', '.tsx'].map(ext => base + ext).find(file => fs.existsSync(file));
          assert.ok(target, `resolved local import ${filename}: ${name}`);
          return load(path.relative(root, target));
        }
        return require(name);
      },
    }, { filename });
    return module.exports;
  }
  env.load = load;
  env.mount = (filename, name = 'default', props = {}) => {
    const Component = typeof filename === 'function' ? filename : load(filename)[name];
    const h = { cursor: 0, slots: [], pending: [], dirty: true, unmounted: false, lateUpdates: 0, tree: null, props };
    h.render = () => {
      let renders = 0;
      do {
        assert.ok(++renders < 30, 'render reaches a stable state');
        h.dirty = false; h.cursor = 0; h.pending = []; active = h;
        h.tree = Component(h.props); active = undefined;
        if (h.tree?.type === 'SyntheticSessionProvider') env.session = h.tree.props.value;
        h.pending.splice(0).forEach(effect => effect());
      } while (h.dirty);
      return h.tree;
    };
    h.flush = async () => { for (let i = 0; i < 4; i++) { await settle(); h.render(); } };
    h.unmount = () => { h.slots.forEach(slot => slot.cleanup?.()); h.unmounted = true; };
    h.strictMount = () => { h.slots.forEach(slot => slot.cleanup?.()); h.slots.forEach(slot => { if (slot.effect) slot.cleanup = slot.effect(); }); h.render(); };
    h.find = (type, label = '') => nodes(h.tree).find(node => node.type === type && text(node).includes(label));
    h.text = () => text(h.tree);
    h.render(); return h;
  };
  env.auth = load('api/auth.ts'); env.tracking = load('api/shareTracking.ts').tracking;
  env.provider = () => env.mount('component/ManagementSession.tsx', 'ManagementSessionProvider', { children: 'protected-content' });
  env.guard = () => env.mount('component/ProtectedRoute.tsx', 'default', { children: 'protected-content' });
  return env;
}

test('synthetic login uses existing cookie API and current /me wins over the login scope', async () => {
  const env = environment(); let signedIn = false;
  env.fetch = async url => {
    if (url.endsWith('/management/login')) { signedIn = true; return response(200, { ...user, scope: 'platform_admin' }); }
    if (url.endsWith('/management/me')) return signedIn ? response(200, user) : response(401, undefined, '未登录');
    throw new Error(`Unexpected synthetic request ${url}`);
  };
  const provider = env.provider(); await provider.flush(); assert.equal(env.session.user, null);
  await env.auth.login({ username: user.username, password: 'not-a-real-password' }); await provider.flush();
  assert.equal(env.session.user.scope, 'crm_only'); assert.equal(env.localStorage.getItem('admin_token'), 'session_active');
  const login = env.calls.find(call => call.url.endsWith('/login'));
  assert.equal(login.options.method, 'POST'); assert.equal(login.options.credentials, 'include');
  assert.equal(login.options.headers.Authorization, undefined);
  assert.deepEqual(JSON.parse(login.options.body), { username: user.username, password: 'not-a-real-password' });
  const me = env.calls.filter(call => call.url.endsWith('/me'));
  assert.equal(me.length, 2); assert.ok(me.every(call => call.options.cache === 'no-store' && call.options.credentials === 'include'));
  provider.unmount();
});

test('failed login creates no session marker or successful-login event', async () => {
  const env = environment(); env.fetch = async () => response(400, undefined, '账号或密码错误');
  await assert.rejects(env.auth.login({ username: 'synthetic', password: 'invalid-fixture' }), /账号或密码错误/);
  assert.equal(env.localStorage.getItem('admin_token'), null); assert.ok(!env.events.includes('management-session-changed'));
});

test('a local marker cannot authenticate; expired /me sends every protected revisit to login', async () => {
  const env = environment(); env.localStorage.setItem('admin_token', 'session_active'); env.fetch = async () => response(401, undefined, '未登录');
  const provider = env.provider(); await provider.flush();
  assert.equal(env.session.user, null); assert.equal(env.session.error, ''); assert.equal(env.localStorage.getItem('admin_token'), null);
  const guard = env.guard();
  for (const pathname of ['/share-tracking', '/customer-management', '/share-tracking']) {
    env.location = { pathname }; guard.render(); assert.equal(guard.tree.type, 'SyntheticNavigate');
    assert.equal(guard.tree.props.to, '/login'); assert.equal(guard.tree.props.replace, true);
  }
  guard.unmount(); provider.unmount();
});

test('current management 403 and unavailable /me fail closed with an explicit retry', async () => {
  for (const status of [403, 503]) {
    const env = environment(); env.fetch = async () => response(status, undefined, status === 403 ? '账号未授权或已停用' : '管理授权服务不可用');
    const provider = env.provider(); await provider.flush(); const guard = env.guard();
    assert.equal(env.session.user, null); assert.ok(env.session.error); assert.ok(guard.find('button', '重新检查'));
    assert.equal(guard.tree.type, 'div'); assert.doesNotMatch(guard.text(), /protected-content/);
    env.fetch = async () => response(200, user); guard.find('button', '重新检查').props.onClick(); await provider.flush(); guard.render();
    assert.equal(env.session.user.id, user.id); assert.match(guard.text(), /protected-content/);
    guard.unmount(); provider.unmount();
  }
});

test('tracking 401 clears live provider state and blocks a late successful /me from resurrecting it', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); const late = deferred();
  env.fetch = url => url.endsWith('/me') ? late.promise : Promise.resolve(response(401, undefined, '会话已过期'));
  env.session.refresh();
  await assert.rejects(env.tracking.visitors({ owner: 'synthetic-owner-a' }), /重新登录/);
  late.resolve(response(200, user)); await provider.flush();
  assert.equal(env.session.user, null); assert.equal(env.localStorage.getItem('admin_token'), null);
  assert.ok(env.events.includes('management-session-expired'));
  const guard = env.guard(); assert.equal(guard.tree.props.to, '/login'); guard.unmount(); provider.unmount();
});

test('tracking multipart upload 401 uses the same expiry path without a forged bearer token', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); env.fetch = async () => response(401, undefined, '未登录');
  await assert.rejects(env.tracking.upload(new Blob(['synthetic'], { type: 'image/png' }), 'own', 'synthetic-owner-a'), /未登录/);
  await provider.flush(); assert.equal(env.session.user, null);
  const call = env.calls.at(-1); assert.equal(call.options.credentials, 'include'); assert.equal(call.options.cache, 'no-store'); assert.equal(call.options.headers['X-Tracking-Request'], '1');
  assert.equal(call.options.headers.Authorization, undefined); assert.equal(call.options.headers['Content-Type'], undefined);
  assert.equal(call.options.body.get('owner_id'), 'synthetic-owner-a'); provider.unmount();
});

test('read 403 rejects data without declaring the whole session expired or silently retrying', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); const before = env.calls.length;
  env.fetch = async () => response(403, undefined, '归属范围未授权');
  await assert.rejects(env.tracking.visitors({ owner: 'outside-scope' }), /归属范围未授权/); await provider.flush();
  assert.equal(env.calls.length, before + 1); assert.equal(env.session.user.id, user.id);
  assert.ok(!env.events.includes('management-session-expired')); provider.unmount();
});

test('tracking context denial never mounts records or starts visitor and material reads', async () => {
  for (const mode of ['no-grant', '403']) {
    const env = environment(); env.fetch = async () => mode === '403' ? response(403, undefined, '管理账号未授权') : response(200, { ...capabilities, can_view: false, owners: [] });
    const screen = env.mount('pages/shareTracking.tsx'); await screen.flush();
    assert.equal(nodes(screen.tree).filter(node => typeof node.type === 'function' && node.type.name === 'TrackingRecords').length, 0);
    assert.ok(env.calls.every(call => call.url.endsWith('/tracking/context')));
    if (mode === 'no-grant') assert.match(screen.text(), /尚未获得访问记录权限/);
    else { const feedback = nodes(screen.tree).find(node => typeof node.type === 'function' && node.type.name === 'Feedback'); assert.equal(feedback.props.error, '管理账号未授权'); }
    screen.unmount();
  }
});

test('identical synthetic user gets identical permission and scope requests at desktop and mobile widths', async () => {
  const results = [];
  for (const width of [1440, 390, 320]) {
    const env = environment({ width }); const provider = env.provider(); await provider.flush();
    const context = await env.tracking.context(); await env.tracking.visitors({ owner: context.owners[0].id, identity: 'identified', limit: 20 });
    const guard = env.guard(); assert.match(guard.text(), /protected-content/);
    assert.ok(env.calls.filter(call => call.url.includes('/tracking/')).every(call => call.options.cache === 'no-store'));
    results.push(normalized({ user: env.session.user, context, requests: env.calls.map(call => ({ url: call.url, credentials: call.options.credentials, authorization: call.options.headers?.Authorization })) }));
    guard.unmount(); provider.unmount();
  }
  assert.deepEqual(results[0], results[1]); assert.deepEqual(results[1], results[2]);
});

test('crm_only route guard permits the shared tracking route while blocking unrelated admin routes', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); const guard = env.guard();
  for (const pathname of ['/share-tracking', '/customer-management']) { env.location = { pathname }; guard.render(); assert.equal(guard.tree.type, React.Fragment); }
  env.location = { pathname: '/users' }; guard.render(); assert.equal(guard.tree.props.to, '/customer-management'); assert.equal(guard.tree.props.replace, true);
  guard.unmount(); provider.unmount();
});

test('account change uses verified identity as the protected subtree key and discards old /me completion', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); const guard = env.guard(); assert.equal(guard.tree.key, String(user.id));
  const late = deferred(); let request = 0; env.fetch = () => ++request === 1 ? late.promise : Promise.resolve(response(200, { ...user, id: 7002 }));
  env.session.refresh(); env.window.dispatchEvent(new Event('management-session-changed')); await provider.flush();
  late.resolve(response(200, user)); await provider.flush(); guard.render();
  assert.equal(env.session.user.id, 7002); assert.equal(guard.tree.key, '7002'); guard.unmount(); provider.unmount();
});

test('logout failure still clears local identity and back-route access', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); env.fetch = async () => response(503, undefined, '登出服务不可用');
  await assert.rejects(env.auth.logout(), /登出服务不可用/); await provider.flush();
  assert.equal(env.session.user, null); assert.equal(env.localStorage.getItem('admin_token'), null);
  const guard = env.guard(); assert.equal(guard.tree.props.to, '/login'); guard.unmount(); provider.unmount();
});

test('owner filters are encoded and every tracking read carries cookies without sending a session marker as bearer', async () => {
  const env = environment(); env.localStorage.setItem('admin_token', 'session_active');
  await env.tracking.visitors({ owner: 'synthetic-owner-a', q: '合成用户', tenant_id: undefined });
  await env.tracking.contentVisitors('synthetic/material', { owner: 'synthetic-owner-a' });
  await env.tracking.visitorContents('u:7001', { owner: 'synthetic-owner-a' });
  await env.tracking.sessions('u:7001', { owner: 'synthetic-owner-a' });
  await env.tracking.events('synthetic/pv', { owner: 'synthetic-owner-a' });
  for (const call of env.calls) {
    const url = new URL(call.url, env.window.location.origin);
    assert.equal(url.searchParams.get('owner'), 'synthetic-owner-a'); assert.equal(url.searchParams.has('tenant_id'), false);
    assert.equal(call.options.credentials, 'include'); assert.equal(call.options.cache, 'no-store'); assert.equal(call.options.headers.Authorization, undefined); assert.equal(call.options.headers['X-Tracking-Request'], '1');
  }
  assert.match(env.calls[1].url, /synthetic%2Fmaterial/); assert.match(env.calls[4].url, /synthetic%2Fpv/);
});

test('unmounted tracking context cannot repaint after a late success', async () => {
  const env = environment(); const late = deferred(); env.fetch = () => late.promise;
  const screen = env.mount('pages/shareTracking.tsx'); const signal = env.calls[0].options.signal; screen.unmount();
  assert.equal(signal.aborted, true); late.resolve(response(200, capabilities)); await settle(); await settle(); assert.equal(screen.lateUpdates, 0);
});

function accessHarness(env, actorId, loader = env.tracking.context) {
  if (arguments.length < 2) actorId = user.id;
  const access = env.load('pages/shareTracking/access.ts');
  const h = env.mount(props => access.useTrackingAccess(props.actorId, props.loader), 'default', { actorId, loader });
  return { h, access };
}

test('HTTP status is preserved with existing messages and request-page provenance', async () => {
  for (const status of [401, 403, 503]) {
    const env = environment(); env.fetch = async () => response(status, undefined, '合成错误');
    await assert.rejects(env.tracking.visitors({}), error => {
      assert.equal(error.status, status); assert.equal(error.requestPage, '/admin/share-tracking');
      assert.equal(error.message, status === 401 ? '未授权，请重新登录' : '合成错误'); return true;
    });
  }
});

test('a current 403 clears capabilities immediately and refreshes read-only subpermission changes', async () => {
  const env = environment(); let contextReads = 0;
  env.fetch = async url => url.endsWith('/context')
    ? response(200, { ...capabilities, can_crm: ++contextReads === 1 })
    : response(403, undefined, 'CRM权限已撤销');
  const { h } = accessHarness(env); await h.flush(); assert.equal(h.tree.data.can_crm, true); const originalRevision = h.tree.revision;
  await assert.rejects(env.tracking.crmTarget('u:7001', 'synthetic-owner-a'), /CRM权限已撤销/);
  h.render(); assert.equal(h.tree.data, undefined); assert.equal(h.tree.loading, true);
  await h.flush(); assert.equal(contextReads, 2); assert.equal(h.tree.data.can_view, true); assert.equal(h.tree.data.can_crm, false);
  assert.notEqual(h.tree.revision, originalRevision); assert.match(h.tree.notice, /清除旧记录/); h.unmount();
});

test('context 403 is terminal feedback for that check, never an automatic revalidation loop', async () => {
  const env = environment(); env.fetch = async () => response(403, undefined, '管理账号未授权');
  const { h } = accessHarness(env); await h.flush();
  assert.equal(env.calls.length, 1); assert.equal(h.tree.data, undefined); assert.equal(h.tree.loading, false); assert.equal(h.tree.error, '管理账号未授权');
  assert.ok(!env.events.includes('tracking-access-denied')); h.unmount();
});

test('aborted, old-actor, and old-permission-generation 403 cannot clear current capabilities', async () => {
  const env = environment(); const { h, access } = accessHarness(env); await h.flush(); const first = access.captureTrackingAccessBoundary();
  const cancelled = new AbortController(); cancelled.abort();
  access.notifyTrackingAccessDenied({ status: 403 }, first, cancelled.signal); h.render(); assert.ok(h.tree.data); assert.equal(env.calls.length, 1);
  h.props.actorId = 7002; h.render(); await h.flush(); const actorBRevision = h.tree.revision;
  access.notifyTrackingAccessDenied({ status: 403 }, first); h.render(); assert.ok(h.tree.data); assert.equal(h.tree.revision, actorBRevision); assert.equal(env.calls.length, 2);
  const previous = access.captureTrackingAccessBoundary(); h.tree.reload(); h.render(); await h.flush(); const refreshed = h.tree.revision;
  access.notifyTrackingAccessDenied({ status: 403 }, previous); h.render(); assert.ok(h.tree.data); assert.equal(h.tree.revision, refreshed); assert.equal(env.calls.length, 3);
  h.unmount();
});

test('real tracking API ignores a late old-actor 403 and aborted 403', async () => {
  for (const mode of ['actor-changed', 'aborted']) {
    const env = environment(); const { h } = accessHarness(env); await h.flush(); const late = deferred();
    env.fetch = url => url.includes('/visitors') ? late.promise : Promise.resolve(response(200, capabilities));
    const cancelled = new AbortController(); const read = env.tracking.visitors({}, cancelled.signal);
    const rejection = assert.rejects(read, /旧请求无权/);
    if (mode === 'actor-changed') { h.props.actorId = 7002; h.render(); await h.flush(); } else cancelled.abort();
    const readsBefore = env.calls.length; const revision = h.tree.revision;
    late.resolve(response(403, undefined, '旧请求无权')); await rejection; await h.flush();
    assert.ok(h.tree.data); assert.equal(h.tree.revision, revision); assert.equal(env.calls.length, readsBefore); h.unmount();
  }
});

test('parallel 403 responses for the same boundary trigger only one context refresh', async () => {
  const env = environment(); const { h, access } = accessHarness(env); await h.flush(); const started = access.captureTrackingAccessBoundary();
  access.notifyTrackingAccessDenied({ status: 403 }, started); access.notifyTrackingAccessDenied({ status: 403 }, started);
  h.render(); await h.flush(); assert.equal(env.calls.length, 2); assert.equal(env.events.filter(name => name === 'tracking-access-denied').length, 1); h.unmount();
});

test('permission hook discards delayed context after A to B to A identity transitions', async () => {
  const env = environment(); const old = deferred(); let reads = 0;
  env.fetch = () => ++reads === 1 ? old.promise : Promise.resolve(response(200, { ...capabilities, owners: [{ id: `scope-${reads}`, name: '合成归属' }] }));
  const { h } = accessHarness(env); h.props.actorId = 7002; h.render(); await h.flush();
  h.props.actorId = 7001; h.render(); await h.flush(); assert.equal(h.tree.data.owners[0].id, 'scope-3');
  old.resolve(response(200, { ...capabilities, owners: [{ id: 'stale-scope', name: '旧归属' }] })); await h.flush();
  assert.equal(h.tree.data.owners[0].id, 'scope-3'); h.unmount();
});

test('unmount aborts context and invalidates boundary before any delayed denial', async () => {
  const env = environment(); let signal;
  const { h, access } = accessHarness(env, user.id, incoming => { signal = incoming; return env.tracking.context(incoming); });
  await h.flush(); const started = access.captureTrackingAccessBoundary(); h.unmount(); assert.equal(signal.aborted, true);
  access.notifyTrackingAccessDenied({ status: 403 }, started); await settle(); assert.equal(h.lateUpdates, 0);
  assert.ok(!env.events.includes('tracking-access-denied'));
});

test('only a safe verified actor may load or persist tracking view state', async () => {
  for (const actorId of [undefined, 0, -1, NaN, 1.5]) {
    const env = environment(); const { h, access } = accessHarness(env, actorId); await h.flush();
    assert.equal(env.calls.length, 0);
    assert.equal(h.tree.data, undefined); assert.match(h.tree.error, /管理账号未确认/);
    access.saveTrackingView(actorId, capabilities.owners, { filters: { owner: 'synthetic-owner-a', q: 'must-not-persist' }, view: 'visitors' });
    assert.equal(access.loadTrackingView(actorId, capabilities.owners).filters.q, ''); h.unmount();
  }
});

test('view storage is isolated by verified actor and never imports legacy shared-account state', () => {
  const env = environment(); const access = env.load('pages/shareTracking/access.ts');
  env.sessionStorage.setItem('tracking-view-v1', JSON.stringify({ filters: { q: 'legacy-private-search', owner: 'synthetic-owner-a' }, view: 'contents' }));
  access.saveTrackingView(7001, capabilities.owners, { filters: { owner: 'synthetic-owner-a', q: 'actor-a-search' }, view: 'visitors' });
  assert.equal(access.loadTrackingView(7001, capabilities.owners).filters.q, 'actor-a-search');
  const accountB = access.loadTrackingView(7002, capabilities.owners); assert.equal(accountB.filters.q, ''); assert.equal(accountB.filters.owner, ''); assert.equal(accountB.view, 'visitors');
  access.saveTrackingView(7002, capabilities.owners, { filters: { q: 'actor-b-search' }, view: 'contents' });
  assert.equal(access.loadTrackingView(7001, capabilities.owners).filters.q, 'actor-a-search'); assert.equal(access.loadTrackingView(7002, capabilities.owners).filters.q, 'actor-b-search');
});

test('revoked owner is dropped on restore while authorized filters remain and malformed storage fails safely', () => {
  const env = environment(); const access = env.load('pages/shareTracking/access.ts');
  access.saveTrackingView(7001, capabilities.owners, { filters: { owner: 'synthetic-owner-a', q: 'same-actor-search', intent: 'high' }, view: 'contents' });
  const revoked = access.loadTrackingView(7001, [{ id: 'synthetic-owner-b', name: 'B' }]);
  assert.equal(revoked.filters.owner, ''); assert.equal(revoked.filters.q, 'same-actor-search'); assert.equal(revoked.filters.intent, '');
  env.sessionStorage.setItem('tracking-view-v2:7001', '{broken-json'); assert.equal(access.loadTrackingView(7001, capabilities.owners).filters.q, '');
  env.sessionStorage.setItem('tracking-view-v2:7001', JSON.stringify({ filters: { q: { bad: true }, owner: 123, content_q: 5 } }));
  const safe = access.loadTrackingView(7001, capabilities.owners); assert.equal(safe.filters.q, ''); assert.equal(safe.filters.owner, ''); assert.equal(safe.filters.content_q, undefined);
});


test('malformed permission responses remain retryable errors and never become confirmed denial or authority', async () => {
  const invalid = [
    {}, { can_manage: false, owners: [] }, { can_view: false, can_manage: false },
    { ...capabilities, can_view: 'true' }, { ...capabilities, can_crm: 'true' },
    { ...capabilities, owners: [] }, { ...capabilities, owners: [null] },
    ...['', ' ', 'invalid\nowner', 'x'.repeat(65), 123].map(id => ({ ...capabilities, owners: [{ id, name: 'invalid' }] })),
    { ...capabilities, owners: [{ id: 'same', name: 'A' }, { id: 'same', name: 'B' }] },
  ];
  for (const value of invalid) {
    const env = environment(); env.fetch = async () => response(200, value); const { h } = accessHarness(env); await h.flush();
    assert.equal(h.tree.data, undefined); assert.equal(h.tree.loading, false); assert.match(h.tree.error, /权限响应无效/); assert.equal(env.calls.length, 1);
    env.fetch = async () => response(200, capabilities); h.tree.reload(); h.render(); await h.flush();
    assert.equal(h.tree.data.can_view, true); h.unmount();
  }
});


const recordsNode = screen => nodes(screen.tree).find(node => typeof node.type === 'function' && node.type.name === 'TrackingRecords');

test('main page removes old records while revalidating and remounts after only CRM permission is revoked', async () => {
  const env = environment(); let contextReads = 0;
  env.fetch = async url => url.endsWith('/context') ? response(200, { ...capabilities, can_crm: ++contextReads === 1 }) : response(403, undefined, 'CRM权限已撤销');
  const screen = env.mount('pages/shareTracking.tsx'); await screen.flush(); const original = recordsNode(screen);
  assert.equal(original.props.actorId, user.id); assert.equal(original.props.context.can_crm, true);
  await assert.rejects(env.tracking.crmTarget('u:7001', 'synthetic-owner-a'), /CRM权限已撤销/);
  screen.render(); assert.equal(recordsNode(screen), undefined); await screen.flush(); const current = recordsNode(screen);
  assert.equal(current.props.context.can_view, true); assert.equal(current.props.context.can_crm, false); assert.notEqual(current.key, original.key); screen.unmount();
});

test('tracking records restore same-account filters on return but discard withdrawn owner and another account search', async () => {
  const env = environment(); const access = env.load('pages/shareTracking/access.ts');
  access.saveTrackingView(user.id, capabilities.owners, { filters: { owner: 'synthetic-owner-a', q: 'same-account-search', range: 'all' }, view: 'visitors' });
  async function records(actorId, owners) {
    const h = env.mount('pages/shareTracking.tsx', 'TrackingRecords', { actorId, canManage: false, context: { ...capabilities, owners } }); await h.flush();
    const last = env.calls.filter(call => call.url.includes('/tracking/visitors?')).at(-1);
    return { h, query: new URL(last.url, env.window.location.origin).searchParams };
  }
  const first = await records(user.id, capabilities.owners); assert.equal(first.query.get('owner'), 'synthetic-owner-a'); assert.equal(first.query.get('q'), 'same-account-search'); first.h.unmount();
  const returned = await records(user.id, capabilities.owners); assert.equal(returned.query.get('q'), 'same-account-search'); returned.h.unmount();
  const afterRevocation = await records(user.id, [{ id: 'synthetic-owner-b', name: 'B' }]); assert.equal(afterRevocation.query.has('owner'), false); assert.equal(afterRevocation.query.get('q'), 'same-account-search'); afterRevocation.h.unmount();
  const other = await records(7002, capabilities.owners); assert.equal(other.query.has('owner'), false); assert.equal(other.query.has('q'), false); other.h.unmount();
});

test('main page switches trusted actors without exposing old records during the new context check', async () => {
  const env = environment(); const screen = env.mount('pages/shareTracking.tsx'); await screen.flush(); assert.equal(recordsNode(screen).props.actorId, user.id);
  const wait = deferred(); env.fetch = () => wait.promise; env.session = { ...env.session, user: { ...user, id: 7002 } }; screen.render();
  assert.equal(recordsNode(screen), undefined); wait.resolve(response(200, capabilities)); await screen.flush(); assert.equal(recordsNode(screen).props.actorId, 7002); screen.unmount();
});

test('StrictMode setup cleanup setup rejects the abandoned context and keeps the live denial listener', async () => {
  const env = environment(); const abandoned = deferred(); let reads = 0;
  env.fetch = () => ++reads === 1 ? abandoned.promise : Promise.resolve(response(200, capabilities));
  const { h, access } = accessHarness(env); h.strictMount(); await h.flush(); assert.equal(reads, 2); assert.ok(h.tree.data);
  abandoned.resolve(response(200, { ...capabilities, owners: [{ id: 'stale-owner', name: 'stale' }] })); await h.flush(); assert.equal(h.tree.data.owners[0].id, 'synthetic-owner-a');
  access.notifyTrackingAccessDenied({ status: 403 }, access.captureTrackingAccessBoundary()); h.render(); await h.flush(); assert.equal(reads, 3); assert.ok(h.tree.data); h.unmount();
});

test('multipart 403 revalidates current access but an old-account upload denial cannot affect the new account', async () => {
  for (const mode of ['current', 'actor-changed']) {
    const env = environment(); const { h } = accessHarness(env); await h.flush(); const late = deferred();
    env.fetch = (url, options) => options.method === 'POST' ? late.promise : Promise.resolve(response(200, capabilities));
    const upload = env.tracking.upload(new Blob(['synthetic'], { type: 'image/png' }), 'own', 'synthetic-owner-a');
    const rejected = assert.rejects(upload, error => error.status === 403);
    if (mode === 'actor-changed') { h.props.actorId = 7002; h.render(); await h.flush(); }
    const before = env.calls.length; late.resolve(response(403, undefined, '上传权限已撤销')); await rejected; h.render(); await h.flush();
    assert.equal(env.calls.length, before + (mode === 'current' ? 1 : 0)); assert.ok(h.tree.data); h.unmount();
  }
});


test('repeated denial after one automatic context check stops without a request loop and allows manual recovery', async () => {
  const env = environment(); let contexts = 0;
  env.fetch = async url => url.endsWith('/context') ? (contexts++, response(200, capabilities)) : response(403, undefined, '后端仍拒绝');
  const screen = env.mount('pages/shareTracking.tsx'); await screen.flush(); assert.ok(recordsNode(screen)); assert.equal(contexts, 1);
  for (let round = 0; round < 4; round++) {
    await assert.rejects(env.tracking.visitors({}), /后端仍拒绝/); screen.render(); await screen.flush();
    if (round === 0) assert.ok(recordsNode(screen)); else assert.equal(recordsNode(screen), undefined);
  }
  assert.equal(contexts, 2);
  const check = screen.find('button', '重新检查权限'); assert.ok(check); check.props.onClick(); screen.render(); await screen.flush();
  assert.equal(contexts, 3); assert.ok(recordsNode(screen));
  await assert.rejects(env.tracking.visitors({}), /后端仍拒绝/); screen.render(); await screen.flush();
  assert.equal(contexts, 4); assert.ok(recordsNode(screen)); screen.unmount();
});

test('automatic recheck budget belongs to the verified actor and is renewed on account change', async () => {
  const env = environment(); const { h, access } = accessHarness(env); await h.flush();
  for (let round = 0; round < 2; round++) {
    access.notifyTrackingAccessDenied({ status: 403 }, access.captureTrackingAccessBoundary()); h.render(); await h.flush();
  }
  assert.equal(h.tree.permissionBlocked, true); assert.equal(h.tree.data, undefined); assert.equal(env.calls.length, 2);
  const stopped = access.captureTrackingAccessBoundary(); h.props.actorId = 7002; h.render(); await h.flush(); assert.ok(h.tree.data);
  access.notifyTrackingAccessDenied({ status: 403 }, stopped); h.render(); assert.ok(h.tree.data);
  access.notifyTrackingAccessDenied({ status: 403 }, access.captureTrackingAccessBoundary()); h.render(); await h.flush();
  assert.ok(h.tree.data); assert.notEqual(h.tree.permissionBlocked, true); assert.equal(env.calls.length, 4); h.unmount();
});

test('request expiry predicate is internal, leaves current 401 unchanged, and suppresses obsolete expiry only', async () => {
  for (const allowed of [undefined, true, false]) {
    const env = environment(); env.localStorage.setItem('admin_token', 'session_active'); env.fetch = async () => response(401, undefined, '未登录');
    const request = env.load('utils/request.ts').request;
    await assert.rejects(request('/api/v1/management/tracking/visitors', { shouldExpireSession: allowed === undefined ? undefined : () => allowed }), error => error.status === 401 && error.message === '未授权，请重新登录');
    assert.equal('shouldExpireSession' in env.calls[0].options, false);
    assert.equal(env.events.includes('management-session-expired'), allowed !== false);
    assert.equal(env.localStorage.getItem('admin_token'), allowed === false ? 'session_active' : null);
  }
});

test('an old-account or aborted tracking 401 cannot log out the currently verified account', async () => {
  for (const mode of ['actor-changed', 'aborted']) {
    const env = environment(); const provider = env.provider(); await provider.flush(); const { h } = accessHarness(env); await h.flush();
    const late = deferred(); env.fetch = url => url.includes('/visitors') ? late.promise : Promise.resolve(response(200, capabilities));
    const controller = new AbortController(); const read = env.tracking.visitors({}, controller.signal); const rejected = assert.rejects(read, error => error.status === 401);
    if (mode === 'actor-changed') { env.session = { ...env.session, user: { ...user, id: 7002 } }; h.props.actorId = 7002; h.render(); await h.flush(); } else controller.abort();
    late.resolve(response(401, undefined, '旧会话过期')); await rejected;
    assert.ok(!env.events.includes('management-session-expired')); assert.equal(env.localStorage.getItem('admin_token'), 'session_active');
    h.unmount(); provider.unmount();
  }
});

test('a late old-account upload 401 leaves the new session intact', async () => {
  const env = environment(); const provider = env.provider(); await provider.flush(); const { h } = accessHarness(env); await h.flush(); const late = deferred();
  env.fetch = (url, options) => options.method === 'POST' ? late.promise : Promise.resolve(response(200, capabilities));
  const upload = env.tracking.upload(new Blob(['synthetic'], { type: 'image/png' }), 'own', 'synthetic-owner-a'); const rejected = assert.rejects(upload, error => error.status === 401);
  h.props.actorId = 7002; h.render(); await h.flush(); late.resolve(response(401, undefined, '旧上传会话过期')); await rejected;
  assert.ok(!env.events.includes('management-session-expired')); assert.equal(env.localStorage.getItem('admin_token'), 'session_active'); h.unmount(); provider.unmount();
});
