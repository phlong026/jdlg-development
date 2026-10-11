import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const events = [];
const sandbox = {
  exports: {},
  window: { location: { pathname: '/admin/users' }, dispatchEvent: (event) => events.push(event) },
  CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
};
const source = fs.readFileSync(new URL('../src/utils/pageLoadError.ts', import.meta.url), 'utf8');
vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText, sandbox);
const { reportPageLoadError, reportPageLoadRecovered, updatePageLoadFailures } = sandbox.exports;

test('recovering one request does not erase a separate failed business data source', () => {
  let current = {};
  current = updatePageLoadFailures(current, { source: '用户列表', message: '列表失败' });
  current = updatePageLoadFailures(current, { source: '用户统计', message: '统计失败' });
  const recovered = updatePageLoadFailures(current, { source: '用户列表', message: '', cleared: true });
  assert.deepEqual(Object.keys(recovered), ['用户统计']);
  assert.equal(recovered['用户统计'], '统计失败');
  assert.equal(current['用户列表'], '列表失败', 'updates must not mutate React state');
});

test('failure and recovery use the same named source and preserve request route', () => {
  reportPageLoadError({ message: '连接中断', requestPage: '/admin/recharge' }, '加载订单中心失败');
  reportPageLoadRecovered('加载订单中心失败', '/admin/recharge');
  const [failed, recovered] = events.slice(-2).map((event) => event.detail);
  assert.equal(failed.source, recovered.source);
  assert.equal(failed.path, '/admin/recharge');
  assert.equal(recovered.path, '/admin/recharge');
  assert.equal(recovered.cleared, true);
});

test('retry success clears its old warning while a new failure replaces only its source', () => {
  let current = updatePageLoadFailures({}, { source: '列表', message: '首次失败' });
  current = updatePageLoadFailures(current, { source: '列表', message: '重试失败' });
  assert.deepEqual(Object.values(current), ['重试失败']);
  current = updatePageLoadFailures(current, { source: '列表', message: '', cleared: true });
  assert.equal(Object.keys(current).length, 0);
});
