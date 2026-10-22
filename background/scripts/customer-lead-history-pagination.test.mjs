import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, environment, textOf } from './helpers/crm-harness.mjs';

const lead = (id, userId = 5) => ({
  id,
  user_id: userId,
  name: `Lead ${id}`,
  status: 'new',
  intent_level: 'high',
  source: 'home_mascot',
  demand_summary: `Demand ${id}`,
  created_at: `2026-10-22T00:${String(id % 60).padStart(2, '0')}:00Z`,
});

const historyButton = (h, label) => h.all(node => node.type === 'button' && textOf(node).trim() === label).at(-1);
const plain = value => JSON.parse(JSON.stringify(value));

function setup(rows = [lead(1)]) {
  const env = environment();
  const historyReads = [];
  env.leadApi.getCustomerLeadList = params => {
    if (!params.user_id) return Promise.resolve({
      list: rows.map(row => ({ ...row, submission_count: 101 })),
      total: rows.length,
    });
    const read = deferred();
    historyReads.push({ params, ...read });
    return read.promise;
  };
  return { env, historyReads };
}

test('线索记录使用服务端总数并可访问第 101 条，重复翻页不会重复请求', async () => {
  const { env, historyReads } = setup();
  const h = await env.mount('customerLeads.tsx');

  h.find(node => node.type === 'button' && node.props.title === '查看线索记录').props.onClick();
  assert.deepEqual(plain(historyReads[0].params), { user_id: 5, dedupe: 'all', page: 1, page_size: 100 });
  historyReads[0].resolve({ list: Array.from({ length: 100 }, (_, index) => lead(index + 1)), total: 101 });
  await h.flush();

  assert.match(h.text(), /用户5 · 共 101 条，第 1 \/ 2 页/);
  const next = historyButton(h, '下一页');
  assert.equal(next.props.disabled, false);
  next.props.onClick();
  next.props.onClick();
  assert.equal(historyReads.length, 2, '同一页加载中不得重复发请求');
  assert.deepEqual(plain(historyReads[1].params), { user_id: 5, dedupe: 'all', page: 2, page_size: 100 });
  historyReads[1].resolve({ list: [lead(101)], total: 101 });
  await h.flush();

  assert.match(h.text(), /用户5 · 共 101 条，第 2 \/ 2 页/);
  assert.match(h.text(), /Demand 101/);
  assert.equal(historyButton(h, '下一页').props.disabled, true);
});

test('线索记录翻页失败时保留当前页并提示失败', async () => {
  const { env, historyReads } = setup();
  const h = await env.mount('customerLeads.tsx');
  h.find(node => node.type === 'button' && node.props.title === '查看线索记录').props.onClick();
  historyReads[0].resolve({ list: [lead(1)], total: 101 });
  await h.flush();

  historyButton(h, '下一页').props.onClick();
  historyReads[1].reject(new Error('history page unavailable'));
  await h.flush();

  assert.match(h.text(), /用户5 · 共 101 条，第 1 \/ 2 页/);
  assert.match(h.text(), /Demand 1/);
  assert.equal(env.reports.at(-1)[1], 'history page unavailable');
});

test('较早客户的迟到响应不能覆盖当前客户的线索记录', async () => {
  const { env, historyReads } = setup([lead(1, 5), lead(2, 6)]);
  const h = await env.mount('customerLeads.tsx');
  const openButtons = h.all(node => node.type === 'button' && node.props.title === '查看线索记录');

  openButtons[0].props.onClick();
  openButtons[1].props.onClick();
  historyReads[1].resolve({ list: [lead(201, 6)], total: 1 });
  await h.flush();
  historyReads[0].resolve({ list: [lead(101, 5)], total: 1 });
  await h.flush();

  assert.match(h.text(), /用户6 · 共 1 条，第 1 \/ 1 页/);
  assert.match(h.text(), /Demand 201/);
  assert.doesNotMatch(h.text(), /Demand 101/);
});
