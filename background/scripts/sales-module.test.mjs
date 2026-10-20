// 销售模块重做：导航、路由、漏斗、客户标签、客户系统设置的权限判定。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import ts from 'typescript';
import { environment } from './helpers/crm-harness.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const eq = (actual, expected) => assert.deepEqual(JSON.parse(JSON.stringify(actual)), expected);
const read = file => fs.readFileSync(path.join(root, file), 'utf8');

// 纯 TS 模块（无 React）直接转译执行
function loadPure(file, stubs = {}) {
  const code = ts.transpileModule(read(file), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(code, { module, exports: module.exports, require: name => { if (name in stubs) return stubs[name]; if (name === 'react-icons/fi') return new Proxy({}, { get: () => 'icon' }); throw new Error('Unexpected dependency: ' + name); } });
  return module.exports;
}

test('销售分组：总览 · 线索 · 客户，取消商机，落地页是总览', () => {
  const nav = loadPure('src/component/navConfig.ts');
  const sales = nav.visibleGroups('platform_admin', true).find(g => g.id === 'sales');
  assert.equal(sales.label, '销售');
  eq(sales.pages.map(p => p.label), ['总览', '线索', '客户']);
  eq(sales.pages.map(p => p.path), ['/sales-overview', '/customer-leads', '/customer-management']);
  assert.equal(sales.landing, '/sales-overview');
  const all = JSON.stringify(nav.navGroups) + JSON.stringify(nav.platformSections);
  assert.ok(!all.includes('/opportunities'), '导航里不应再有商机');
});

test('未接入 CRM 时「客户」卡片灰显但仍可点（与原行为一致）', () => {
  const nav = loadPure('src/component/navConfig.ts');
  const customer = nav.visibleGroups('platform_admin', false).find(g => g.id === 'sales').pages.find(p => p.path === '/customer-management');
  assert.equal(customer.note, '未接入');
});

test('客户系统设置只给 platform_admin，其余账号与未知 scope 一律看不到（fail-closed）', () => {
  const nav = loadPure('src/component/navConfig.ts');
  const paths = groups => groups.flatMap(g => [...g.pages, ...(g.related || [])].map(p => p.path));
  assert.ok(paths(nav.visibleGroups('platform_admin', true)).includes('/customer-settings'));
  assert.ok(paths(nav.visibleGroups('platform_admin', false)).includes('/customer-settings'));
  for (const scope of ['crm_only', undefined, '', 'admin', 'PLATFORM_ADMIN']) {
    const groups = nav.visibleGroups(scope, true);
    assert.ok(!paths(groups).includes('/customer-settings'), `scope=${scope}`);
    assert.ok(!paths(groups).includes('/customer-leads'), `scope=${scope}`);
    // 总览只对 crm_only（各看各的）开放；缺失/未知 scope 仍然看不到
    assert.equal(paths(groups).includes('/sales-overview'), scope === 'crm_only', `scope=${scope}`);
  }
  const sectionPaths = scope => nav.visibleSections(scope, true).flatMap(s => s.items.map(i => i.path));
  assert.ok(sectionPaths('platform_admin').includes('/customer-settings'));
  assert.ok(!sectionPaths('crm_only').includes('/customer-settings'));
  assert.ok(!sectionPaths(undefined).includes('/customer-settings'));
});

test('路由：新增 /sales-overview 与 /customer-settings，/opportunities 与旧栏目地址重定向', () => {
  const routes = read('src/routes/index.tsx');
  assert.match(routes, /path: '\/sales-overview'/);
  assert.match(routes, /path: '\/customer-settings'/);
  assert.match(read('src/App.tsx'), /path="\/opportunities" element=\{<Navigate to="\/customer-leads" replace \/>\}/);
  assert.ok(!fs.existsSync(path.join(root, 'src/pages/opportunities.tsx')), '商机页面应删除');
});

test('漏斗：最窄不小于 18%，转化率按上一层计算', () => {
  const { funnelLayers } = loadPure('src/component/salesFunnelMath.ts');
  const layers = funnelLayers([{ key: 'a', label: 'A', count: 120 }, { key: 'b', label: 'B', count: 80 }, { key: 'c', label: 'C', count: 35 }, { key: 'd', label: 'D', count: 0 }]);
  eq(layers.map(l => Math.round(l.width)), [100, 67, 29, 18]);
  eq(layers.map(l => l.rate), [null, 67, 44, 0]);
  assert.ok(layers.every(l => l.width >= 18));
  eq(funnelLayers([{ key: 'a', label: 'A', count: 0 }]).map(l => l.width), [100]);
  eq(funnelLayers([]), []);
  // 下一层比上一层大（口径不一致）时不放大：宽度单调不增
  const odd = funnelLayers([{ key: 'a', label: 'A', count: 10 }, { key: 'b', label: 'B', count: 30 }]);
  assert.ok(odd[1].width <= odd[0].width);
});

test('客户标签：全部 · 跟进中 · 已成交 · 暂缓 · 无效 · 公海，沿用现有阶段枚举', () => {
  const { customerTabs, customerFilter, resolveCustomerTab } = loadPure('src/pages/customerTabs.ts');
  eq(customerTabs.map(t => t.label), ['全部', '跟进中', '已成交', '暂缓', '无效', '公海']);
  eq(customerTabs.map(t => t.key), ['all', 'following', 'won', 'paused', 'invalid', 'pool']);
  eq({ ...customerFilter('following') }, { queue: 'all', stage: 'following' });
  eq({ ...customerFilter('pool') }, { queue: 'pool' });
  eq({ ...customerFilter('all') }, { queue: 'all' });
  assert.equal(resolveCustomerTab('bogus'), 'all');
  assert.equal(resolveCustomerTab('won'), 'won');
});

test('客户系统设置页：非 platform_admin 不渲染任何设置内容', async () => {
  for (const scope of ['crm_only', undefined, 'unknown']) {
    const env = environment(); env.stubPanels = true;
    env.session = { ...env.session, user: scope === undefined ? { id: 7 } : { id: 7, scope, crmEnabled: true }, crmContext: { account: { role: 'admin', username: 'c' } } };
    const h = await env.mount('customerSettings.tsx');
    assert.ok(!/企微同步|账号与权限|归属绑定/.test(h.text()), `scope=${scope}`);
    assert.ok(h.all(n => n.type === 'Navigate').length === 1, `scope=${scope} 应重定向`);
  }
});

test('客户系统设置页：platform_admin 看到三项，默认企微同步，可切换', async () => {
  const env = environment(); env.stubPanels = true;
  env.session = { ...env.session, user: { id: 7, scope: 'platform_admin', crmEnabled: true }, crmContext: { account: { role: 'admin', username: 'c' } } };
  const h = await env.mount('customerSettings.tsx');
  for (const label of ['企微同步与接入', '账号与权限', '归属绑定检查']) assert.ok(h.text().includes(label), label);
  assert.ok(h.all(n => n.type === 'SyncPanel').length === 1);
  h.button('账号与权限').props.onClick(); h.render();
  assert.ok(h.all(n => n.type === 'AccessPanel').length === 1);
  h.button('归属绑定检查').props.onClick(); h.render();
  assert.ok(h.all(n => n.type === 'BindingCheck').length === 1);
});

test('客户页不再承载企微同步 / 账号权限 / 小程序线索栏目', () => {
  const page = read('src/pages/customerManagement.tsx');
  assert.ok(!/function SyncPanel|function AccessPanel|<CustomerLeads|SourceSyncStatus/.test(page));
  assert.ok(!/企微同步|账号与权限/.test(page.replace(/[^\n]*「账号与权限」[^\n]*/g, '')), '页面内不应再有这两个栏目名');
});

test('相对时间与公海电话脱敏', () => {
  const { timeAgo, maskPhone } = loadPure('src/pages/salesFormat.ts');
  const now = Date.parse('2026-10-20T12:00:00Z');
  assert.equal(timeAgo('2026-10-20T11:55:00Z', now), '5 分钟前');
  assert.equal(timeAgo('2026-10-20T09:00:00Z', now), '3 小时前');
  assert.equal(timeAgo('2026-10-17T12:00:00Z', now), '3 天前');
  assert.equal(timeAgo(null, now), '尚未跟进');
  assert.equal(timeAgo('not-a-date', now, '—'), '—');
  assert.equal(maskPhone('13812345678'), '138****5678');
  assert.equal(maskPhone(''), '');
  assert.equal(maskPhone('123'), '****');
});

test('公海客户认领前只显示脱敏电话，已在保护期的客户显示完整电话', async () => {
  const detail = c => ({ customer: c, relations: [], followups: [], attachments: [], sources: [], cases: [] });
  const base = { version: 1, name: 'A', phone: '13812345678', stage: 'following', category: '建房咨询', intent: 'high', customerNumber: null };
  for (const [fixture, expected, hidden] of [[{ ...base, id: 'A', poolAt: '2026-10-01T00:00:00Z', lifecycle: 'pool' }, '138****5678', '13812345678'], [{ ...base, id: 'A', poolAt: null, lifecycle: 'protected' }, '13812345678', '138****5678']]) {
    const env = environment(); env.crm = async url => url === '/employees' ? [] : detail(fixture);
    const h = await env.mount('customerManagement.tsx', 'CustomerDrawer', { id: 'A', supervisor: true, close() {}, changed() {}, checkPool() {} });
    assert.ok(h.text().includes(expected), expected);
    assert.ok(!h.text().includes(hidden), hidden);
  }
});

function overviewEnv(role, employeeId = 'wx1') {
  const env = environment(); env.calls = [];
  env.session = { ...env.session, user: { id: 7, scope: 'platform_admin', crmEnabled: true }, crmContext: { account: { username: 'crm1', displayName: '林顾问', employeeId, role }, demo: false } };
  env.leadApi.getCustomerLeadOverview = async () => ({ today_new_count: 5, overdue_count: 2 });
  env.crm = async (url, options = {}) => {
    env.calls.push({ url, query: options.query });
    if (url === '/dashboard') return { overview: { today: 4, overdue: 3, pool: 6 }, todayAdded: 2, team: [] };
    if (url === '/customers') return { list: [], total: options.query?.ownerId ? 1 : 9, page_size: 1 };
    if (url === '/sync') return { configured: true, failed: 0, pending: 0, callbackConfigured: true, runs: [{ id: 1, status: 'success', processed: 1, error: null, startedAt: 'x', finishedAt: new Date(Date.now() - 5 * 60000).toISOString() }], events: [] };
    throw new Error('Unexpected CRM request: ' + url);
  };
  return env;
}

test('总览：主管可切团队 / 个人并看到只读接入状态；只有数字与图表，没有新增入口', async () => {
  const h = await overviewEnv('admin').mount('salesOverview.tsx');
  assert.ok(h.text().includes('团队简报') && h.text().includes('个人简报'));
  assert.match(h.text(), /企微接入正常 · 最近同步 5 分钟前/);
  assert.ok(h.text().includes('今日待办') && h.text().includes('公海待认领'));
  assert.ok(!/新增线索[^\d]*按钮|＋|新增客户/.test(h.text().replace('新增线索', '').replace('新增客户', '')), '总览不放新增按钮');
  assert.ok(h.all(n => n.type === 'table' || n.type === 'tbody').length === 0, '总览不放列表');
  // 团队口径：数字来自 dashboard 与线索总览
  assert.match(h.text(), /5新增线索2新增客户4待跟进/);
  const links = h.all(n => n.type === 'a').map(n => n.props.to);
  assert.ok(links.includes('/customer-management?view=pool'));
  assert.ok(links.includes('/customer-leads?queue=overdue'));
  assert.ok(links.includes('/customer-management?tab=tasks&queue=overdue'));
});

test('总览：顾问没有团队切换，客户数字按本人 ownerId 统计，也不读接入状态', async () => {
  const env = overviewEnv('staff'); const h = await env.mount('salesOverview.tsx');
  assert.ok(!h.text().includes('团队简报') && !h.text().includes('个人简报'));
  assert.ok(!/企微接入/.test(h.text()));
  const customerCalls = env.calls.filter(c => c.url === '/customers');
  assert.ok(customerCalls.length > 0 && customerCalls.every(c => c.query.ownerId === 'wx1'));
  assert.ok(!env.calls.some(c => c.url === '/sync'));
});


// 三种账号的导航与页面访问：用真实的 ProtectedRoute 与 navConfig 判定
function routeDecision(scope, pathname) {
  const code = ts.transpileModule(read('src/component/ProtectedRoute.tsx'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
  const module = { exports: {} };
  const nodeRequire = createRequire(import.meta.url);
  vm.runInNewContext(code, { module, exports: module.exports, require: name => {
    if (name === 'react') return nodeRequire('react');
    if (name === 'react/jsx-runtime') return nodeRequire(name);
    if (name === 'react-router-dom') return { Navigate: 'Navigate', useLocation: () => ({ pathname }) };
    if (name === './ManagementSession') return { useManagementSession: () => ({ loading: false, error: '', user: { id: 1, scope } }) };
    throw new Error(name);
  } });
  const tree = module.exports.default({ children: 'page' });
  return tree.type === 'Navigate' ? 'redirect:' + tree.props.to : 'allow';
}

test('三种账号的页面访问：platform_admin 全开，crm 主管/顾问只多一个总览', () => {
  const pages = ['/sales-overview', '/customer-management', '/share-tracking', '/customer-leads', '/customer-settings', '/dashboard', '/users'];
  for (const page of pages) assert.equal(routeDecision('platform_admin', page), 'allow', 'platform_admin ' + page);
  for (const role of ['crm_only']) { // crm 主管与顾问的 scope 相同，角色差异只在页内数据
    for (const page of ['/sales-overview', '/customer-management', '/share-tracking']) assert.equal(routeDecision(role, page), 'allow', page);
    for (const page of ['/customer-leads', '/customer-settings', '/dashboard', '/users', '/opportunities']) assert.equal(routeDecision(role, page), 'redirect:/customer-management', page);
  }
});

test('三种账号的导航：crm 账号只有 总览 · 客户（无线索、无系统设置）', () => {
  const nav = loadPure('src/component/navConfig.ts');
  const labels = scope => nav.visibleGroups(scope, true).flatMap(g => g.pages.map(p => p.label));
  const platform = labels('platform_admin');
  for (const l of ['总览', '线索', '客户', '客户系统设置']) assert.ok(platform.includes(l), 'platform_admin ' + l);
  eq(labels('crm_only'), ['总览', '客户', '分享访问记录']);
  const sales = nav.visibleGroups('crm_only', true).find(g => g.id === 'sales');
  assert.equal(sales.landing, '/sales-overview');
});

test('总览：crm 账号不请求线索接口，只显示客户数据；主管可切团队、顾问只看个人', async () => {
  for (const [role, toggle] of [['admin', true], ['staff', false]]) {
    const env = overviewEnv(role);
    env.session.user = { id: 8, scope: 'crm_only', crmEnabled: true };
    let leadCalls = 0;
    for (const name of ['getCustomerLeadOverview', 'getCustomerLeadList', 'getCustomerLeadFunnel']) { const original = env.leadApi[name]; env.leadApi[name] = async (...a) => { leadCalls++; return original(...a); }; }
    const h = await env.mount('salesOverview.tsx');
    assert.equal(leadCalls, 0, role);
    assert.ok(!/线索/.test(h.text()), role + ' 不显示线索内容');
    assert.ok(h.text().includes('新增客户') && h.text().includes('公海待认领'), role);
    assert.equal(h.text().includes('个人简报'), toggle, role);
    assert.ok(h.all(n => n.type === 'SalesFunnel').length === 0, role);
  }
});
