'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function navigation(customBack) {
  let definition;
  const events = [], backs = [], tabs = [];
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/components/navigation-bar/navigation-bar.ts'), 'utf8'), {
    Component(value) { definition = value; },
    wx: { navigateBack(value) { backs.push(value); }, switchTab(value) { tabs.push(value); } },
  });
  return { page: { ...definition.methods, data: { delta: 1, customBack }, triggerEvent(...args) { events.push(args); } }, definition, events, backs, tabs };
}

test('navigation keeps ordinary page return and home fallback by default', () => {
  const result = navigation(false);
  assert.equal(result.definition.properties.customBack.value, false);
  result.page.onBack();
  assert.equal(result.events[0][0], 'back');
  assert.equal(result.backs.length, 1);
  result.backs[0].fail();
  assert.equal(result.tabs[0].url, '/pages/index/index');
});

test('a page with inner views owns back without an extra navigateBack', () => {
  const result = navigation(true);
  result.page.onBack();
  assert.equal(result.events[0][0], 'back');
  assert.equal(result.backs.length, 0);
  assert.equal(result.tabs.length, 0);
});
