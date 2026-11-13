import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const read = (path) => fs.readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

test('线索工作台提供三类互斥筛选与队列', () => {
  const page = read('pages/customerLeads.tsx');
  const nav = read('component/navConfig.ts');
  for (const type of ['sales', 'partner', 'identity_pending']) {
    assert.match(page, new RegExp(`value=["']${type}["']`));
  }
  assert.match(nav, /key: 'partner'/);
  assert.match(nav, /key: 'identity_pending'/);
});

test('转化完成按服务端 lead_type 进入对应队列', () => {
  const source = read('pages/shareTracking/convertLead.tsx');
  assert.match(source, /kind: result\.lead_type \|\| kind/);
  assert.match(source, /queue=partner/);
  assert.match(source, /queue=identity_pending/);
  assert.match(source, /身份待确认线索/);
});
