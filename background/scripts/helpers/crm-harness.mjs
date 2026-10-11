// Real TSX handlers with controlled hooks and API I/O; no production requests.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const settle = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const nodes = node => Array.isArray(node) ? node.flatMap(nodes) : !node || typeof node !== 'object' ? [] : [node, ...nodes(node.props?.children)];
const textOf = node => Array.isArray(node) ? node.map(textOf).join('') : node == null || typeof node === 'boolean' ? '' : typeof node === 'object' ? textOf(node.props?.children) : String(node);
class CRMError extends Error { constructor(status, message) { super(message); this.status = status; } }
const baseSession = () => ({ user: { id: 7, username: 'platform', scope: 'platform_admin', crmEnabled: false }, loading: false, error: '', crmContext: null, crmError: '', refresh() {} });
const account = (id, overrides = {}) => ({ username: `crm${id}`, displayName: `Employee ${id}`, employeeId: `wx${id}`, role: 'staff', enabled: true, ...overrides });
const user = (id, overrides = {}) => ({ userId: id, username: `User ${id}`, scope: 'crm_only', enabled: true, version: 1, crmUsername: `crm${id}`, state: 'active', crmEnabled: true, ...overrides });
const lead = { id: 1, user_id: 5, name: 'Lead A', status: 'new', intent_level: 'high', assigned_to: '', next_followup_at: null };

function environment() {
  const events = new Map();
  const env = { events, updatesAfterUnmount: 0, session: baseSession(), leadWrites: [], accessWrites: [], reports: [], alerts: [], confirmations: [], access: { users: [user(1), user(2)], accounts: [account(1), account(2)], serviceAvailable: true } };
  env.crm = async (url, options = {}) => {
    if (options.method === 'POST') { env.accessWrites.push({ url, body: options.body }); return; }
    if (url === '/access') return env.access;
    if (url === '/access/employees') return [{ id: 'wx1', name: 'Employee one', active: true }, { id: 'wx2', name: 'Employee two', active: true }];
    if (url === '/dashboard') return { team: [] };
    throw new Error(`Unexpected CRM request: ${url}`);
  };
  let active;
  env.leadApi = {getCustomerLeadList: async () => ({list:[lead],total:1}),getCustomerLeadOverview: async () => ({}),getCustomerLeadDetail: async () => ({lead,events:[],followups:[]}),updateCustomerLeadStatus: async (...args) => env.leadWrites.push({action:'status',args}),createCustomerLeadFollowup: async (...args) => env.leadWrites.push({action:'followup',args}),batchUpdateCustomerLeads: async (...args) => env.leadWrites.push({action:'batch',args})};
  env.getMe = async () => env.session.user;
  const window = {alert: value => env.alerts.push(value), confirm: value => {env.confirmations.push(value);return env.confirmResult!==false}, scrollTo(){},addEventListener(name,fn){if(!events.has(name))events.set(name,new Set());events.get(name).add(fn)},removeEventListener(name,fn){events.get(name)?.delete(fn)},dispatchEvent(event){events.get(event.type)?.forEach(fn=>fn(event))}};
  env.dispatch = name => window.dispatchEvent({type:name});
  const react = {
    ...React,
    useState(initial) { const r = active, index = r.cursor++; if (!(index in r.slots)) r.slots[index] = typeof initial === 'function' ? initial() : initial; return [r.slots[index], next => { if(r.unmounted)env.updatesAfterUnmount++; r.slots[index] = typeof next === 'function' ? next(r.slots[index]) : next; }]; },
    useRef(initial) { const r = active, index = r.cursor++; if (!(index in r.slots)) r.slots[index] = { current: initial }; return r.slots[index]; },
    useSyncExternalStore(subscribe,getSnapshot) { return getSnapshot(); },
    useMemo(factory) { return factory(); },
    useEffect(effect, deps) {
      const r = active, index = r.cursor++, previous = r.slots[index];
      if (!previous || !deps || deps.some((dep, i) => !Object.is(dep, previous.deps[i]))) {
        r.slots[index] = { deps, cleanup: previous?.cleanup };
        r.pending.push(() => { r.slots[index].cleanup?.(); r.slots[index].cleanup = effect(); });
      }
    },
  };
  const cache = {};
  function load(filename) {
    if (cache[filename]) return cache[filename];
    const source = fs.readFileSync(path.join(root, 'src', filename.includes('/') ? filename : 'pages/'+filename), 'utf8') + (filename === 'customerManagement.tsx' ? '\nexport { AccessPanel, CustomerDrawer, CustomerForm, Customers, SyncPanel, Home };' : '');
    const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
    const module = { exports: {} };
    vm.runInNewContext(code, {
      module, exports: module.exports, Error, AbortController, URLSearchParams, console, window, setTimeout, setInterval:()=>0, clearInterval(){}, document:{activeElement:{focus(){}},visibilityState:'visible'}, Date:class extends Date {constructor(...args){super(...(args.length?args:[env.now??Date.now()]))}static now(){return env.now??Date.now()}},
      require(name) {
        if (name === 'react') return react;
        if (name === 'react/jsx-runtime') return require(name);
        if (name === 'react-router-dom') return { Link: 'a', useSearchParams: () => [env.params || new URLSearchParams(), () => {}] };
        if (name === 'react-icons/fi') return new Proxy({}, { get: () => 'icon' });
        if (name === '../component/ManagementSession') return { useManagementSession: () => env.session };
        if (name === '../api/customerLeads') return env.leadApi;
        if (name === '../api/wecomCustomers') return {crm:(...args)=>env.crm(...args),CRMError,dateTime:value=>value||'未安排',stages:{new:'新客户',following:'跟进中',won:'已成交',paused:'暂缓',invalid:'无效'},categories:['待确认','建房咨询'],intents:{high:'高',medium:'中',low:'低'},lifecycleNames:{pending:'待完善报备',legacy:'历史客户',protected:'专属保护',pool:'客户公海',won:'成交已确认',archived:'已归档'},sourceNames:{manual:'手动报备'},activityNames:{followup:'跟进'}};
        if (name === '../api/auth') return {getMe:()=>env.getMe()};
        if (name === '../utils/token') return {tokenUtils:{setToken(){},removeToken(){}}};
        if (name === '../component/navConfig') return { DEFAULT_LEAD_QUEUE: 'all', leadQueues: [{ key: 'all', label: '全部', countKey: 'total_count' }] };
        if (name === '../component/leadOverviewStore' || name === './leadOverviewStore') return load('component/leadOverviewStore.ts');
        if (name === '../utils/pageLoadError') return { reportPageLoadError: (...args) => env.reports.push(args) };
        if (name === './customerLeads') return load('customerLeads.tsx');
        if (name === './customerWorkflow') return load('customerWorkflow.tsx');
        if (name === './customerReportForm') return load('customerReportForm.tsx');
        if (name === './customerLeadHub' || name === '../component/WecomIdentityBinding') return { __esModule: true, default: name };
        if (name.endsWith('.scss')) return {};
        throw new Error(`Unexpected dependency: ${name}`);
      },
      alert: value => env.alerts.push(value),
    }, { filename });
    cache[filename] = module.exports;
    return module.exports;
  }
  env.mount = async (filename, name = 'default', props = {}) => {
    const Component = load(filename)[name];
    const h = { slots: [], pending: [], cursor: 0, tree: null, props };
    h.render = () => { active = h; h.cursor = 0; h.tree = Component(h.props); active = null; return h.tree; };
    h.flush = async () => { for (let i = 0; i < 4; i++) { h.render(); h.pending.splice(0).forEach(effect => effect()); await settle(); } h.render(); };
    h.all = predicate => nodes(h.tree).filter(predicate);
    h.find = predicate => { const result = h.all(predicate)[0]; assert.ok(result, 'Expected rendered element'); return result; };
    h.button = label => h.find(n => n.type === 'button' && textOf(n).trim() === label);
    h.label = name => h.find(n => n.type === 'label' && textOf(n).startsWith(name));
    h.field = name => nodes(h.label(name)).find(n => n.type === 'input' || n.type === 'select');
    h.change = (name, value) => { const field = h.field(name); field.props.onChange({ target: { value, checked: value } }); h.render(); };
    h.choose = id => { h.find(n => n.type === 'button' && n.key === String(id) && n.props.className.includes('crm-account-row')).props.onClick(); h.render(); };
    h.submit = () => h.find(n => n.type === 'form' && nodes(n).some(x => x.type === 'input' && x.props.readOnly)).props.onSubmit({ preventDefault() {} });
    h.text = () => textOf(h.tree);
    h.unmount = () => {h.slots.forEach(slot => slot?.cleanup?.());h.unmounted=true;};
    await h.flush();
    return h;
  };
  env.store = load('component/leadOverviewStore.ts');env.store.resetLeadOverviewSession(env.session.user.id);
  return env;
}

export { environment, CRMError, baseSession, account, user, lead, deferred, nodes, textOf, settle };


