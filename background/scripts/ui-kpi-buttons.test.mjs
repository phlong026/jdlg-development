import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import * as sass from 'sass';
import postcss from 'postcss';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const read = (name) => fs.readFileSync(path.join(root, name), 'utf8');
const attr = (node, name) => node.openingElement.attributes.properties.find((item) => ts.isJsxAttribute(item) && item.name.getText() === name);
function clickableKpis(source) {
  const tree = ts.createSourceFile('page.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const result = [];
  const visit = (node) => {
    if (ts.isJsxElement(node) && attr(node, 'onClick') && /aio-kpi|stat-item-action/.test(attr(node, 'className')?.initializer?.getText() || '')) result.push(node);
    ts.forEachChild(node, visit);
  };
  visit(tree);
  return result;
}
function assertNative(node) {
  assert.equal(node.openingElement.tagName.getText(), 'button', 'clickable KPI must use a native button');
  assert.equal(attr(node, 'type')?.initializer?.text, 'button');
  assert.equal(attr(node, 'role'), undefined, 'native buttons do not need a simulated button role');
  assert.equal(attr(node, 'tabIndex'), undefined, 'native buttons are naturally keyboard focusable');
}
const handler = (node) => attr(node, 'onClick').initializer.expression.getText().replace(/\s+/g, '');

test('KPI contract rejects the original clickable div', () => {
  const [original] = clickableKpis('<div className="aio-kpi" role="button" tabIndex={0} onClick={() => navigate("/ai-tasks")}>Calls</div>');
  assert.throws(() => assertNative(original), /native button/);
});

test('both interactive AI KPI cards are native buttons with their original routes', () => {
  const nodes = clickableKpis(read('src/pages/aiOverview.tsx'));
  assert.equal(nodes.length, 2);
  nodes.forEach(assertNative);
  assert.deepEqual(nodes.map(handler), ["()=>navigate('/ai-tasks')", "()=>navigate('/ai-tasks?queue=failed')"]);
  assert.match(nodes[0].getText(), /今日调用/);
  assert.match(nodes[1].getText(), /待处理图片异常/);
});

test('order exception KPI is a native button and preserves its idempotent filter action', () => {
  const nodes = clickableKpis(read('src/pages/orderCenter.tsx'));
  assert.equal(nodes.length, 1);
  assertNative(nodes[0]);
  assert.equal(handler(nodes[0]), '()=>{if(!onlyExceptions)handleToggleExceptions();}');
  assert.match(nodes[0].getText(), /待处理异常/);
  assert.match(nodes[0].getText(), /fundStats\?\.exception_count/);
});

test('native KPI buttons keep inherited typography, alignment, hover and visible keyboard focus', () => {
  const css = postcss.parse(sass.compile(path.join(root, 'src/responsive-priority.scss'), { logger: sass.Logger.silent }).css);
  for (const selector of ["[data-admin-page='/ai-overview'] button.aio-kpi", "[data-admin-page='/recharge'] button.stat-item-action"]) {
    const declarations = {};
    css.walkRules((rule) => {
      if (rule.selector.replaceAll('"', "'").includes(selector)) rule.walkDecls((decl) => { declarations[decl.prop] = decl.value; });
    });
    assert.equal(declarations.font, 'inherit');
    assert.equal(declarations['text-align'], 'left');
    assert.equal(declarations.cursor, 'pointer');
    assert.match(declarations.outline, /2px solid/);
  }
  assert.match(css.toString(), /button\.aio-kpi:hover/);
});
