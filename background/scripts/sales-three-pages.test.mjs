// 销售三页第一批（2026-10-21 方案）：线索页交接态、「新增线索」入口、总览待办只放能处理的事、客户显示名与意向。
import assert from 'node:assert/strict';
import test from 'node:test';
import { environment, account, textOf, nodes } from './helpers/crm-harness.mjs';

const CUSTOMERS = '/customer-management?tab=customers';
const takeover = env => {
  env.session = { ...env.session, user: { ...env.session.user, crmEnabled: true }, crmContext: { account: account(7, { role: 'admin' }), demo: false } };
  return env;
};
const links = (h, to) => h.all(n => n.type === 'a' && n.props.to === to);

test('线索页（CRM 接管且有客户权限）：没有长提示，没有禁用的跟进按钮和勾选框，每条线索直达客户档案', async () => {
  for (const embedded of [false, true]) {
    const env = takeover(environment());
    env.leadApi.getCustomerLeadList = async () => ({ list: [{ id: 1, user_id: 5, status: 'new', intent_level: 'high', source: 'home_mascot', session_no: 'cs_1787116843988_333134', source_task_no: '', demand_summary: '四层 新闽派', created_at: '2026-10-21T01:00:00Z' }], total: 1 });
    const h = await env.mount('customerLeads.tsx', 'default', { embedded });
    const text = h.text();
    assert.doesNotMatch(text, /旧线索仅供查看|统一客户档案接管/, `embedded=${embedded}`);
    assert.equal(h.all(n => n.type === 'button' && textOf(n).trim() === '跟进').length, 0, '不再出现永远点不了的跟进按钮');
    assert.equal(h.all(n => n.type === 'input' && n.props.type === 'checkbox').length, 0, '只读时不放勾选框');
    assert.ok(links(h, CUSTOMERS).some(n => textOf(n).includes('去客户档案跟进')), '每条线索给出可点的去处');
    assert.ok(text.includes('首页客服入口'));
    assert.ok(!text.includes('cs_1787116843988_333134'), '列表不显示会话号');
  }
});

test('线索页交接态：详情里也只有去客户档案的链接，没有「记录跟进」', async () => {
  const env = takeover(environment());
  const h = await env.mount('customerLeads.tsx');
  await h.button('详情').props.onClick(); await h.flush();
  assert.equal(h.all(n => n.type === 'button' && textOf(n).trim() === '记录跟进').length, 0);
  assert.ok(links(h, CUSTOMERS).length >= 2, '列表行与详情都有去处');
  assert.equal(env.leadWrites.length, 0);
});

test('线索页：可写时缓存的「跟进」处理函数，切到交接态后调用也打不开编辑、写不进旧线索', async () => {
  const env = environment();
  const h = await env.mount('customerLeads.tsx');
  const cachedOpen = h.button('跟进').props.onClick;
  takeover(env); h.render();
  assert.equal(h.all(n => n.type === 'button' && textOf(n).trim() === '跟进').length, 0);
  cachedOpen(); await h.flush();
  assert.equal(h.all(n => n.type === 'textarea').length, 0, '不能打开跟进编辑');
  assert.equal(env.leadWrites.length, 0);
});

test('线索页：CRM 接管但客户权限没就绪时，仍提示原因并保留重新检查', async () => {
  const env = environment();
  env.session = { ...env.session, user: { ...env.session.user, crmEnabled: true }, crmError: '该账号未启用客户访问' };
  const h = await env.mount('customerLeads.tsx');
  assert.ok(h.text().includes('该账号未启用客户访问'));
  assert.ok(h.button('重新检查会话与客户权限'));
  assert.equal(links(h, CUSTOMERS).length, 0);
});

test('线索页标题栏：有客户权限时显示「＋ 新增线索」，保存后给出客户档案入口', async () => {
  const env = takeover(environment());
  const h = await env.mount('customerLeadsPage.tsx');
  assert.ok(!h.text().includes('跟进在客户档案'), '标题栏不放交接说明');
  h.button('＋ 新增线索').props.onClick(); h.render();
  const dialog = h.find(n => n.props?.role === 'dialog');
  assert.equal(dialog.props['aria-label'], '新增线索');
  // 测试替身只渲染一层：表单组件本身的提交已有专门用例，这里只验证保存回调之后的页面状态
  const form = h.find(n => typeof n.props?.saved === 'function' && typeof n.props?.cancel === 'function');
  assert.equal(form.props.customer, undefined, '新增，不是编辑已有客户');
  assert.ok(h.all(n => typeof n.props?.className === 'string' && n.props.className.split(' ').includes('crm-page') && nodes(n).includes(dialog)).length === 1, '抽屉包在 .crm-page 里，复用客户页样式');
  form.props.cancel(); h.render();
  assert.equal(h.all(n => n.props?.role === 'dialog').length, 0, '取消即关闭');
  h.button('＋ 新增线索').props.onClick(); h.render();
  h.find(n => typeof n.props?.saved === 'function').props.saved({ id: 'C9' }); await h.flush();
  assert.equal(h.all(n => n.props?.role === 'dialog').length, 0, '保存后关闭');
  assert.match(h.text(), /已新建客户档案/, '说清楚写到了哪里：线索列表只显示小程序留资');
  assert.ok(links(h, '/customer-management?tab=customers&customer=C9').length === 1, '直达新建的客户档案');
});

test('线索页标题栏：没有客户权限时不显示「新增线索」', async () => {
  const env = environment();
  const h = await env.mount('customerLeadsPage.tsx');
  assert.equal(h.all(n => n.type === 'button' && textOf(n).includes('新增线索')).length, 0);
  assert.ok(!h.text().includes('跟进在客户档案'));
});

const hubProps = { overview: { total_count: 28, new_count: 28, todo_count: 28, today_new_count: 0 }, loading: false, updatedAt: new Date('2026-10-21T00:36:00Z'), onRefresh() {}, activeQueue: 'all', queueHref: key => `/customer-leads?queue=${key}` };

test('线索简报与线索阶段：不显示「全量概览 · 更新时间」，阶段卡常开且没有「共 N 条」摘要', async () => {
  const h = await environment().mount('customerLeadHub.tsx', 'default', hubProps);
  const text = h.text();
  assert.ok(!/全量概览|更新/.test(text), text);
  assert.ok(!/线索阶段 · 共/.test(text));
  assert.equal(h.all(n => n.type === 'details' || n.type === 'summary').length, 0, '阶段卡不再折叠');
  const titles = h.all(n => n.type === 'h2').map(textOf);
  assert.ok(titles.includes('线索简报') && titles.includes('线索阶段'), titles.join());
  assert.ok(h.button('刷新'));
});

test('客服服务漏斗：常开，只留标题和数字，不放备注', async () => {
  const h = await environment().mount('customerLeadHub.tsx', 'ServiceFunnel', { refreshKey: 0 });
  const text = h.text();
  assert.equal(h.all(n => n.type === 'details' || n.type === 'summary').length, 0);
  assert.ok(h.all(n => n.type === 'h2').some(n => textOf(n) === '客服服务漏斗'));
  assert.ok(text.includes('进入客服') && text.includes('提交线索'));
  assert.ok(!/暂只统计|转化率偏高|按账号去重|近30天/.test(text), text);
});

function overviewEnv({ crmEnabled, role = 'admin', withAccount = true }) {
  const env = environment();
  env.session = { ...env.session, user: { id: 7, scope: 'platform_admin', crmEnabled }, crmContext: withAccount ? { account: { username: 'crm1', displayName: '林顾问', employeeId: 'wx1', role }, demo: false } : null };
  env.leadApi.getCustomerLeadOverview = async () => ({ today_new_count: 5, overdue_count: 28 });
  env.crm = async (url, options = {}) => {
    if (url === '/dashboard') return { overview: { today: 4, overdue: 3, pool: 6, unassigned: 7 }, todayAdded: 2, team: [] };
    if (url === '/customers') return { list: [], total: 1, page_size: 1 };
    if (url === '/sync') return { configured: true, failed: 0, pending: 0, callbackConfigured: true, runs: [], events: [] };
    throw new Error('Unexpected CRM request: ' + url);
  };
  return env;
}

test('总览（CRM 接管）：今日待办不再计入无法处理的线索超时，改为待分配客户', async () => {
  const h = await overviewEnv({ crmEnabled: true }).mount('salesOverview.tsx');
  assert.ok(!h.text().includes('线索超时未跟进'));
  assert.equal(links(h, '/customer-leads?queue=overdue').length, 0);
  const unassigned = links(h, '/customer-management?tab=tasks&queue=unassigned');
  assert.equal(unassigned.length, 1);
  assert.match(textOf(unassigned[0]), /待分配客户\s*7/);
});

test('总览（CRM 接管但本账号没有客户档案权限）：今日待办给出说明，不留空白卡片', async () => {
  const h = await overviewEnv({ crmEnabled: true, withAccount: false }).mount('salesOverview.tsx');
  const todo = h.find(n => n.type === 'ul' && n.props.className === 'sx-todo');
  assert.match(textOf(todo), /客户账号未就绪，暂无可处理的待办/);
});

test('总览（CRM 接管）：顾问个人口径不显示待分配', async () => {
  const h = await overviewEnv({ crmEnabled: true, role: 'staff' }).mount('salesOverview.tsx');
  assert.ok(!h.text().includes('待分配客户'));
  assert.ok(!h.text().includes('线索超时未跟进'));
});

test('总览（未接管，旧线索仍可跟进）：保留线索超时未跟进', async () => {
  const h = await overviewEnv({ crmEnabled: false, withAccount: false }).mount('salesOverview.tsx');
  assert.equal(links(h, '/customer-leads?queue=overdue').length, 1);
  assert.match(h.text(), /线索超时未跟进\s*28/);
});

test('客户列表：没有姓名显示「微信用户 #编号」，没有评级显示「未评级」', async () => {
  const env = takeover(environment());
  const base = { stage: 'new', category: '待确认', phone: '', demand: '三层 新闽派', ownerId: null, ownerUsername: null, nextFollowupAt: null, lastFollowupAt: null, poolAt: null, protectionExpiresAt: null, version: 1 };
  env.crm = async url => {
    if (url === '/customers') return { list: [{ ...base, id: 'mini-1', name: '', customerNumber: 'KH1234567890ABCDEF', intent: 'unknown' }, { ...base, id: 'C2', name: '王可心', customerNumber: 'KH0000', intent: 'medium' }], total: 2, page_size: 20 };
    if (url === '/employees') return [];
    throw new Error('Unexpected CRM request: ' + url);
  };
  const h = await env.mount('customerManagement.tsx', 'Customers', { supervisor: true, tasks: false, view: 'all', open() {}, report() {} });
  const text = h.text();
  assert.ok(text.includes('微信用户 #ABCDEF'), text);
  assert.ok(!text.includes('待同步客户'));
  assert.ok(text.includes('未评级'));
  assert.ok(!text.includes('待判断意向') && !text.includes('undefined意向'));
  assert.ok(text.includes('中意向'));
  // 意向筛选的选项与列表同一套叫法
  h.button('筛选').props.onClick(); h.render();
  const intentSelect = h.find(n => n.type === 'select' && n.props['aria-label'] === '意向');
  const options = Object.fromEntries(intentSelect.props.children.flat().filter(n => n?.type === 'option' && n.props.value).map(n => [n.props.value, textOf(n)]));
  assert.deepEqual(options, { high: '高意向', medium: '中意向', low: '低意向', unknown: '未评级' });
});

test('编辑客户资料：昵称框不显示占位名，意向下拉与列表同一套叫法', async () => {
  const env = takeover(environment());
  const customer = { id: 'mini-1', name: '', customerNumber: 'KH1234567890ABCDEF', intent: 'unknown', stage: 'new', category: '待确认', phone: '', demand: '', version: 1 };
  const h = await env.mount('customerManagement.tsx', 'CustomerForm', { customer, mode: 'edit', files: [], cancel() {}, saved() {} });
  const nickname = h.label('客户昵称').props.children.find(n => n?.type === 'input');
  assert.equal(nickname.props.value, '', '没有昵称就留空，不把「微信用户 #…」当成真实昵称');
  assert.equal(nickname.props.placeholder, '未填写');
  const select = nodes(h.label('意向程度')).find(n => n.type === 'select');
  const options = Object.fromEntries(nodes(select).filter(n => n.type === 'option').map(n => [n.props.value, textOf(n)]));
  assert.deepEqual(options, { high: '高意向', medium: '中意向', low: '低意向', unknown: '未评级' });
});
