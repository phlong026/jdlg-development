'use strict';
// T3.4：AI 文案分包在写定触发页预载（P2：触发页 projectnotes、network=wifi、独立可 revert）。
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const root = path.join(__dirname, '../miniprogram');
const app = JSON.parse(fs.readFileSync(path.join(root, 'app.json'), 'utf8'));

test('the notes page preloads the contentwriting subpackage on wifi', () => {
  const rule = (app.preloadRule || {})['pages/projectnotes/projectnotes'];
  assert.ok(rule, '必须以 projectnotes 为触发页预载内容创作分包');
  assert.equal(rule.network, 'wifi');
  assert.deepEqual(rule.packages, ['pages/contentwriting']);
  assert.ok((app.subPackages || []).some(group => group.root === 'pages/contentwriting'), '分包根必须存在');
});

test('both the trigger page and the target stay in their existing subpackages', () => {
  const byRoot = root => (app.subPackages || []).find(group => group.root === root);
  assert.deepEqual(byRoot('pages/projectnotes').pages, ['projectnotes'], '触发页声明必须存在');
  assert.deepEqual(byRoot('pages/contentwriting').pages, ['contentwriting'], '目标页声明必须存在');
  assert.ok(!(app.pages || []).includes('pages/contentwriting/contentwriting'), '内容创作不得被移入主包');
});
