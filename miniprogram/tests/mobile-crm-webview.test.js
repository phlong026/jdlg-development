'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram/pages/adminhome');

function loadPage() {
  let definition;
  vm.runInNewContext(fs.readFileSync(path.join(root, 'adminhome.js'), 'utf8'), {
    Page: value => { definition = value; },
  });
  const page = { ...definition, data: { ...definition.data }, setData(patch) { Object.assign(this.data, patch); } };
  return page;
}

test('customer management web-view uses the existing production admin deep link without mini-program identity', () => {
  const page = loadPage();
  page.onLoad({ target: 'customer-management', token: 'must-not-forward', user_id: '41' });
  assert.equal(page.data.webUrl, 'https://api.jiadilingguang.com/admin/customer-management');
  assert.doesNotMatch(page.data.webUrl, /token|user_id|must-not-forward/);
});

test('admin web-view rejects arbitrary targets and falls back to the existing admin home', () => {
  const page = loadPage();
  page.onLoad({ target: 'https://evil.invalid/path' });
  assert.equal(page.data.webUrl, 'https://api.jiadilingguang.com/admin/');
});
