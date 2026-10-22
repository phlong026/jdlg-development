// 留资口径修正（2026-10-21）：漏斗"进入客服"标出其中匿名次数；再次留资的老线索在列表里标"再次留资"。
import assert from 'node:assert/strict';
import test from 'node:test';
import { environment } from './helpers/crm-harness.mjs';

const FUNNEL = [
  { key: 'welcome_shown', label: '进入客服', count: 10, conversion_rate: 100, anonymous_sessions: 6 },
  { key: 'lead_submit', label: '提交线索', count: 2, conversion_rate: 20 },
];

test('线索页客服漏斗："进入客服"标出其中匿名次数，无匿名时不加注', async () => {
  const env = environment();
  env.leadApi.getCustomerLeadFunnel = async () => FUNNEL;
  const h = await env.mount('customerLeadHub.tsx', 'ServiceFunnel', { refreshKey: 0 });
  assert.ok(h.text().includes('进入客服（含匿名 6 次）'), h.text());
  assert.ok(h.text().includes('提交线索') && !h.text().includes('提交线索（'));
});

test('总览线索漏斗：传给漏斗图的"进入客服"同样带匿名次数', async () => {
  const env = environment();
  env.session = { ...env.session, user: { id: 7, scope: 'platform_admin', crmEnabled: true }, crmContext: { account: { username: 'crm1', displayName: '林顾问', employeeId: 'wx1', role: 'admin' }, demo: false } };
  env.leadApi.getCustomerLeadFunnel = async () => FUNNEL;
  env.crm = async (url, options = {}) => {
    if (url === '/dashboard') return { overview: { today: 0, overdue: 0, pool: 0 }, todayAdded: 0, team: [] };
    if (url === '/customers') return { list: [], total: 0, page_size: 1 };
    if (url === '/sync') return { configured: true, failed: 0, pending: 0, callbackConfigured: true, runs: [], events: [] };
    throw new Error('Unexpected CRM request: ' + url + JSON.stringify(options));
  };
  const h = await env.mount('salesOverview.tsx');
  const funnel = h.find(n => n.type === 'SalesFunnel');
  assert.deepEqual(funnel.props.steps.map(s => s.label), ['进入客服（含匿名 6 次）', '提交线索']);
});

test('线索列表：再次留资的线索标"再次留资 · N 天前"，只留资一次的不标', async () => {
  const env = environment();
  env.now = Date.parse('2026-10-21T10:00:00Z');
  const base = { user_id: 5, status: 'new', intent_level: 'high', source: 'home_mascot', session_no: '', source_task_no: '', demand_summary: '三层 新闽派' };
  env.leadApi.getCustomerLeadList = async () => ({
    list: [
      { ...base, id: 1, created_at: '2026-08-19T02:00:00Z', last_submitted_at: '2026-10-18T09:00:00Z', submission_count: 11 },
      { ...base, id: 2, created_at: '2026-10-20T02:00:00Z', last_submitted_at: '2026-10-20T02:00:05Z' },
    ],
    total: 2,
  });
  const h = await env.mount('customerLeads.tsx');
  const text = h.text();
  assert.equal(text.split('再次留资 · 3 天前').length - 1, 1, text);
  assert.equal(text.split('再次留资').length - 1, 1, '同一次会话里的连续写入不算再次留资');
  assert.ok(text.includes('11 条线索记录'), '按客户合并后应明确显示合并的线索记录条数');
  assert.equal(h.find(node => node.type === 'button' && node.props.title === '查看线索记录').props.title, '查看线索记录');
  assert.ok(text.includes('按客户合并') && text.includes('全部线索记录'), '应提供合并/全部线索记录切换');
  assert.ok(!text.includes('11 次留资') && !text.includes('全部留资'), '线索行数不能宣称为留资事件次数');
});
