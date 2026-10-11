// Compiled CSS/JSX contracts only; real viewport and WebView validation is separate.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import * as sass from 'sass';
import postcss from 'postcss';
import ts from 'typescript';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const css = postcss.parse(sass.compile(path.join(root, 'src/pages/supportTickets.scss'), { logger: sass.Logger.silent }).css);
const rules = [];
css.walkRules(rule => rules.push(rule));
const values = (suffix, property) => rules
  .filter(rule => rule.selector.endsWith(suffix))
  .flatMap(rule => rule.nodes.filter(node => node.type === 'decl' && node.prop === property));
const has = (suffix, property, expected) => assert.ok(values(suffix, property).some(node => node.value === expected), `${suffix} ${property}: ${expected}`);

test('support dialogs define their own viewport overlay, independent of previously visited pages', () => {
  has('.modal-overlay', 'position', 'fixed');
  has('.modal-overlay', 'inset', '0');
  has('.modal-overlay', 'display', 'flex');
  has('.modal-overlay', 'z-index', '1400');
  assert.ok(values('.modal-overlay', 'background').length);
  rules.filter(rule => /modal/.test(rule.selector)).forEach(rule => {
    assert.ok(rule.selector.includes('data-admin-page="/support-tickets"'), rule.selector);
  });
});

test('support dialog scroll belongs to its body while the title and action area remain in the flex column', () => {
  has('.support-modal', 'display', 'flex');
  has('.support-modal', 'flex-direction', 'column');
  has('.support-modal', 'max-height', 'calc(100dvh - 32px)');
  has('.support-modal .modal-body', 'min-height', '0');
  has('.support-modal .modal-body', 'overflow-y', 'auto');
  has('.support-modal .modal-footer', 'flex-shrink', '0');
  has('.support-modal .modal-footer', 'flex-wrap', 'wrap');
  assert.ok(values('.support-modal .modal-footer', 'padding-bottom').some(node => node.value.includes('safe-area-inset-bottom')));
  assert.ok(values('.support-modal', 'overflow').some(node => node.value === 'hidden' && node.important), 'route-specific body scrolling must survive the shared mobile sheet override');
});

test('both support dialogs have separate names and labeled close controls', () => {
  const source = fs.readFileSync(path.join(root, 'src/pages/supportTickets.tsx'), 'utf8');
  const ast = ts.createSourceFile('support.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const dialogs = [];
  const visit = node => {
    if (ts.isJsxOpeningElement(node)) {
      const attributes = new Map(node.attributes.properties.filter(ts.isJsxAttribute).map(attr => [attr.name.getText(ast), attr.initializer && ts.isStringLiteral(attr.initializer) ? attr.initializer.text : '']));
      if (attributes.get('className') === 'modal-content support-modal') dialogs.push(attributes);
    }
    ts.forEachChild(node, visit);
  };
  visit(ast);
  assert.equal(dialogs.length, 2);
  assert.deepEqual(dialogs.map(attrs => attrs.get('aria-labelledby')).sort(), ['support-create-title', 'support-detail-title']);
  dialogs.forEach(attrs => { assert.equal(attrs.get('role'), 'dialog'); assert.equal(attrs.get('aria-modal'), 'true'); });
  assert.ok(source.includes('aria-label="关闭新建工单"'));
  assert.ok(source.includes('aria-label="关闭工单详情"'));
});
