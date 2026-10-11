import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import ts from 'typescript';
const read = file => fs.readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');
const parse = source => ts.createSourceFile('page.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const find = (node, predicate, out = []) => {
  if (predicate(node)) out.push(node);
  ts.forEachChild(node, child => { find(child, predicate, out); });
  return out;
};

test('technical task payloads stay in a closed diagnostic section after business actions', () => {
  const source = read('src/pages/aiTaskCenter.tsx');
  const tree = parse(source);
  const diagnostics = find(tree, node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'details'
    && node.getText().includes('技术诊断详情'));
  assert.equal(diagnostics.length, 1);
  const diagnostic = diagnostics[0];
  assert.ok(!diagnostic.openingElement.attributes.properties.some(attr => attr.name?.getText() === 'open'));
  for (const field of ['raw_status', 'api_endpoint', 'external_id', 'raw_error_message', 'request_payload', 'result_payload']) {
    assert.ok(diagnostic.getText().includes(field), `${field} remains available to troubleshoot`);
  }
  const business = source.slice(source.indexOf('className="task-detail-body"'), diagnostic.pos);
  for (const term of ['selectedDetail.stones_used', 'selectedDetail.status', 'selectedDetail.error_message', '快捷处理动作']) {
    assert.ok(business.includes(term), `${term} must remain outside the collapsed diagnostics`);
  }
  assert.ok(source.slice(0, diagnostic.pos).includes('已退 {item.refunded_stones}'), 'list refund amounts remain visible');
});

test('refund copy asks operators to verify records rather than promising every failed task was refunded', () => {
  for (const file of ['aiOverview', 'aiTaskCenter']) {
    const source = read(`src/pages/${file}.tsx`);
    assert.doesNotMatch(source, /系统已自动退款|失败任务已由系统自动退还/);
    assert.match(source, /核对/);
  }
});

test('Chinese language and user-detail names are consistent across menus and routes', () => {
  assert.match(read('index.html'), /<html lang="zh-CN">/);
  for (const file of ['src/component/navConfig.ts', 'src/routes/index.tsx']) {
    const source = read(file);
    assert.doesNotMatch(source, /用户360工作台/);
    assert.match(source, /用户详情/);
    assert.match(source, /\/user-workbench/);
  }
});
