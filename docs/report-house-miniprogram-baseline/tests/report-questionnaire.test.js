const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('../../AItestPlatform/node_modules/typescript');

function load(file, globals) {
  const source = fs.readFileSync(path.join(__dirname, '../miniprogram', file), 'utf8');
  const output = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
  const exports = {};
  vm.runInNewContext(output, { exports, ...globals });
  return exports;
}

function questionnaire() {
  const store = new Map();
  const requests = [], redirects = [];
  let definition, loggedIn = true;
  const flow = load('pages/report/flow.ts', {});
  const wx = {
    getStorageSync: key => store.get(key), setStorageSync: (key, value) => store.set(key, value),
    removeStorageSync: key => store.delete(key), pageScrollTo() {}, redirectTo: options => redirects.push(options),
  };
  load('pages/report/questionnaire.ts', {
    wx, Page: value => { definition = value; }, require: id => ({
      './flow': flow,
      './api': { reportRequest: (...args) => new Promise((resolve, reject) => requests.push({ args, resolve, reject })) },
      '../../utils/reportNavigation': { requireReportLogin: () => loggedIn },
    })[id],
  });
  const page = { ...definition, data: JSON.parse(JSON.stringify(definition.data)), setData(patch) {
    for (const [key, value] of Object.entries(patch)) {
      const segments = key.split('.');
      let object = this.data;
      for (const segment of segments.slice(0, -1)) object = object[segment];
      object[segments.at(-1)] = value;
    }
  } };
  return { page, store, requests, redirects, login: value => { loggedIn = value; } };
}
function validInput() {
  return { site: { region: '福建泉州', width_m: 12, depth_m: 10, road_side: '待确认' }, household: { family_type: '三代同堂', residents: '父母常住，夫妻周末回家' }, space: { floors: 3, bedrooms: 5, first_floor_elder_room: true }, preference: { style: '新中式', priorities: ['老人起居', '家庭团聚', '预算控制'], goal: '父母住得方便，一家人有地方团聚' }, budget: { wan: 100, scope: '含室内装修' } };
}

test('questionnaire preserves priority selection order because recommendation weights depend on it', () => {
  const h = questionnaire(); h.page.onLoad();
  h.page.priorities({ detail: { value: ['预算控制'] } });
  h.page.priorities({ detail: { value: ['老人起居', '预算控制'] } });
  h.page.priorities({ detail: { value: ['老人起居', '家庭团聚', '预算控制'] } });
  assert.deepEqual(Array.from(h.page.data.input.preference.priorities), ['预算控制', '老人起居', '家庭团聚']);
  h.page.priorities({ detail: { value: ['老人起居', '家庭团聚', '外观体面', '预算控制'] } });
  assert.equal(h.page.data.input.preference.priorities.length, 3);
  assert.match(h.page.data.error, /最多选择三项/);
});

test('login interruption keeps the draft, while successful idempotent submission removes it', async () => {
  const h = questionnaire(); Object.assign(h.page.data, { step: 4, consent: true, input: validInput() });
  h.login(false); await h.page.next();
  assert.equal(h.requests.length, 0); assert.equal(h.store.size, 1);
  h.login(true); const work = h.page.next(); await h.page.next();
  assert.equal(h.requests.length, 1);
  h.requests[0].resolve({ id: 'r-test' }); await work; h.page.onUnload();
  assert.equal(h.store.size, 0); assert.match(h.redirects[0].url, /preview\?id=r-test$/);
});

test('invalid earlier steps and missing consent cannot create paid or preview work', async () => {
  const h = questionnaire(); Object.assign(h.page.data, { step: 4, input: validInput() });
  await h.page.next(); assert.match(h.page.data.error, /同意/);
  h.page.data.consent = true; h.page.data.input.site.width_m = 2;
  await h.page.next(); assert.equal(h.page.data.step, 0); assert.equal(h.requests.length, 0);
});

test('login returns only to allowlisted native report paths and rejects expired returns', () => {
  const store = new Map(), redirects = [];
  const api = load('utils/reportNavigation.ts', { wx: { getStorageSync: key => store.get(key), setStorageSync: (key, value) => store.set(key, value), removeStorageSync: key => store.delete(key), navigateTo() {}, showToast() {}, redirectTo: options => redirects.push(options), reLaunch() {} } });
  for (const target of ['https://example.com', '/pages/admin/index', '/pages/report/delivery?id=x&paid=true', '/pages/report/../login']) assert.equal(api.validReportPath(target), false);
  const target = '/pages/report/delivery?id=report_1';
  assert.equal(api.requireReportLogin(target), false);
  store.set('token', 'fixture'); assert.equal(api.resumeReportAfterLogin(), true); assert.equal(redirects[0].url, target);
  store.set('report_login_return', { path: target, expires: 1 }); assert.equal(api.resumeReportAfterLogin(), false);
});
