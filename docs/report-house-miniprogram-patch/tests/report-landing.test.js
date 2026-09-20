const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

function harness() {
  let definition, token = 'owner-a';
  const requests = [], routes = [];
  const code = ts.transpileModule(fs.readFileSync(path.join(__dirname, '../miniprogram/pages/report/landing.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
  vm.runInNewContext(code, {
    exports: {}, Page: value => { definition = value; },
    wx: { getStorageSync: () => token, navigateTo: options => routes.push(options.url) },
    require: id => id === './api' ? { reportID: value => value, reportRequest: suffix => new Promise((resolve, reject) => requests.push({ suffix, resolve, reject })) } : { resolveAssetPath: value => value },
  });
  const page = { ...definition, data: { ...definition.data }, setData(patch) { Object.assign(this.data, patch); } };
  return { page, requests, routes, token(value) { token = value; } };
}

test('a paid recent report resumes delivery instead of asking for another purchase', async () => {
  const h = harness(); const work = h.page.loadRecent();
  assert.equal(h.requests[0].suffix, '/recent');
  h.requests[0].resolve({ id: 'paid-report', paid: true }); await work; h.page.resume();
  assert.equal(h.routes[0], '/pages/report/delivery?id=paid-report');
});

test('an account change discards late report data and a first-time customer sees no error', async () => {
  const h = harness(); const old = h.page.loadRecent(); h.token('owner-b');
  h.requests[0].resolve({ id: 'owner-a-report' }); await old;
  assert.equal(h.page.data.recent, null);
  const first = h.page.loadRecent(); h.requests[1].reject({ statusCode: 404 }); await first;
  assert.equal(h.page.data.recentError, '');
});

test('logged-out visitors do not request private report history', async () => {
  const h = harness(); h.token(''); await h.page.loadRecent();
  assert.equal(h.requests.length, 0);
});
