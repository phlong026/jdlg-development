import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import * as sass from 'sass';
import postcss from 'postcss';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const read = (file) => fs.readFileSync(path.join(root, file), 'utf8');
const parse = (source) => ts.createSourceFile('page.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);

function invalidTableText(source) {
  const problems = [];
  const isText = (expression) => expression && (
    ts.isStringLiteralLike(expression) ||
    (ts.isConditionalExpression(expression) && (isText(expression.whenTrue) || isText(expression.whenFalse)))
  );
  const visit = (node) => {
    if (ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'tr') {
      for (const child of node.children) {
        if ((ts.isJsxText(child) && child.text.trim()) || (ts.isJsxExpression(child) && isText(child.expression))) {
          problems.push(child.getText());
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(parse(source));
  return problems;
}

const css = postcss.parse(sass.compile(path.join(root, 'src/responsive-priority.scss')).css);
function declarations(selectorPart, property) {
  const values = [];
  css.walkRules((rule) => {
    if (rule.selector.includes(selectorPart)) {
      rule.walkDecls(property, (declaration) => values.push(declaration.value));
    }
  });
  return values;
}

test('table guard catches the original conditional text defect', () => {
  assert.equal(invalidTableText("<table><thead><tr>{image ? '错误信息' : '提示词'}</tr></thead></table>").length, 1);
  assert.deepEqual(invalidTableText("<table><thead><tr><th>{image ? '错误信息' : '提示词'}</th></tr></thead></table>"), []);
});

test('all page table rows keep literal/conditional text inside cells', () => {
  for (const filename of fs.readdirSync(path.join(root, 'src/pages')).filter((file) => file.endsWith('.tsx'))) {
    assert.deepEqual(invalidTableText(read(`src/pages/${filename}`)), [], filename);
  }
});

test('order drawer follows dynamic height and gives its body the scroll area', () => {
  assert.ok(declarations('.order-drawer', 'height').includes('100dvh'));
  assert.ok(declarations('.drawer-body', 'min-height').includes('0'));
  assert.ok(declarations('.drawer-footer', 'flex-wrap').includes('wrap'));
  assert.ok(declarations('.drawer-footer', 'padding-bottom').some((value) => value.includes('safe-area-inset-bottom')));
});

test('pricing and review dialogs have bounded height, scrollable body and stable actions', () => {
  for (const prefix of ['aip', 'aca']) {
    assert.ok(declarations(`.${prefix}-modal`, 'max-height').some((value) => value.includes('100dvh')));
    assert.ok(declarations(`.${prefix}-modal-body`, 'overflow-y').includes('auto'));
    assert.ok(declarations(`.${prefix}-modal-body`, 'min-height').includes('0'));
    assert.ok(declarations(`.${prefix}-modal-foot`, 'flex-shrink').includes('0'));
    assert.ok(declarations(`.${prefix}-modal-close`, 'min-height').includes('44px'));
  }
});

test('certification drawer keeps review actions reachable in the safe area', () => {
  assert.ok(declarations('.cert-drawer', 'height').includes('100dvh'));
  assert.ok(declarations('.cert-drawer-body', 'min-height').includes('0'));
  assert.ok(declarations('.cert-drawer-foot', 'flex-wrap').includes('wrap'));
  assert.ok(declarations('.cert-drawer-foot', 'padding-bottom').some((value) => value.includes('safe-area-inset-bottom')));
});

test('refund and review confirmations stay above their underlying drawers', () => {
  for (const drawer of ['.order-drawer', '.cert-drawer']) {
    const levels = declarations(drawer, 'z-index').map(Number);
    assert.ok(levels.every((level) => level > 1200 && level < 1400), drawer);
  }
});

test('custom modal fixes are scoped to registered admin routes', () => {
  const routes = new Set([...read('src/routes/index.tsx').matchAll(/path:\s*'([^']+)'/g)].map((match) => match[1]));
  css.walkRules((rule) => {
    if (!/modal|drawer/.test(rule.selector) || rule.selector.includes(':has(')) return;
    assert.ok(rule.selector.includes('data-admin-page='), rule.selector);
    for (const match of rule.selector.matchAll(/data-admin-page=["']?([^\]"']+)/g)) assert.ok(routes.has(match[1]), match[1]);
  });
});

test('modal fixes are loaded by the application', () => {
  assert.match(read('src/App.tsx'), /import ['"]\.\/responsive-priority\.scss['"]/);
});
