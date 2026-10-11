import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import * as sass from 'sass';
import postcss from 'postcss';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const pages = {
  dashboard: '/dashboard', users: '/users', userWorkbench: '/user-workbench',
  designerCenter: '/designer-center', certification: '/certification', membershipOperations: '/membership-operations',
  orderCenter: '/recharge', virtualPay: '/virtualpay', pricingConfig: '/pricing-config',
  rechargeConfig: '/recharge-config', membershipPlans: '/membership-plans',
  aiOverview: '/ai-overview', aiTaskCenter: '/ai-tasks', aiContentAudits: '/ai-content-audits',
  aiTools: '/ai-tools', aiPricing: '/ai-pricing', 'ai-config': '/ai-config',
};
const compile = (file) => postcss.parse(sass.compile(path.join(root, file), { logger: sass.Logger.silent }).css);
const withinKeyframes = (node) => {
  for (let parent = node.parent; parent; parent = parent.parent) {
    if (parent.type === 'atrule' && /keyframes$/.test(parent.name)) return true;
  }
  return false;
};

for (const [page, route] of Object.entries(pages)) {
  test(`${route}: lazy-loaded styles cannot change another page`, () => {
    compile(`src/pages/${page}.scss`).walkRules((rule) => {
      if (withinKeyframes(rule)) return;
      assert.ok(rule.selector.includes(`data-admin-page="${route}"`), rule.selector);
    });
  });
}

test('every in-scope table has a dedicated scroll parent, not its heading/action panel', () => {
  for (const page of Object.keys(pages)) {
    const source = fs.readFileSync(path.join(root, `src/pages/${page}.tsx`), 'utf8');
    const tree = ts.createSourceFile('page.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const visit = (node) => {
      if (ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'table') {
        assert.ok(ts.isJsxElement(node.parent), `${page}: table needs its own element parent`);
        const children = node.parent.children.filter((child) => !ts.isJsxText(child) || child.text.trim());
        assert.equal(children.length, 1, `${page}: table must not share a scroller with headings/buttons`);
      }
      ts.forEachChild(node, visit);
    };
    visit(tree);
  }
});

test('mobile AI rank cards carry real labels for all three business values', () => {
  const source = fs.readFileSync(path.join(root, 'src/pages/aiOverview.tsx'), 'utf8');
  for (const label of ['调用量', '成功率', '灵石收入']) assert.ok(source.includes(`className="aio-mobile-label">${label}</span>`));
  const css = compile('src/responsive-priority.scss');
  const rules = [];
  css.walkRules((rule) => {
    if (rule.selector.includes('.aio-rank-row') && rule.parent.type === 'atrule') rules.push(rule.toString());
  });
  assert.ok(rules.some((rule) => rule.includes('repeat(3, minmax(0, 1fr))')));
});

test('mobile list actions stay reachable without dropping financial or status columns', () => {
  const css = compile('src/responsive-priority.scss').toString();
  assert.match(css, /td:last-child:not\(\[colspan\]\)/);
  assert.match(css, /position: sticky/);
  assert.match(css, /min-height: 44px/);
  for (const page of ['orderCenter', 'aiTaskCenter', 'membershipOperations']) {
    const source = fs.readFileSync(path.join(root, `src/pages/${page}.tsx`), 'utf8');
    assert.match(source, /状态/);
    assert.match(source, /查看详情|用户360|用户详情/);
  }
});


test('AI split-card ranks shrink above the mobile breakpoint rather than keeping fixed column widths', () => {
  const css = compile('src/responsive-priority.scss');
  const rules = [];
  css.walkRules(rule => { if (rule.selector.includes('.aio-rank-row') && rule.parent.type === 'root') rules.push(rule.toString()); });
  assert.ok(rules.some(rule => rule.includes('minmax(88px, 1.5fr)') && rule.includes('minmax(16px, 1fr)')));
  assert.ok(rules.every(rule => !rule.includes('130px 1fr 52px 56px 70px')));
});
