const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('../../AItestPlatform/node_modules/typescript');
const tick = () => new Promise(resolve => setImmediate(resolve));
function compile(file, globals) {
 const source = ts.transpileModule(fs.readFileSync(path.join(__dirname, '../miniprogram/' + file), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
 const exports = {}; vm.runInNewContext(source, { exports, ...globals }); return exports;
}
function harness() {
 const requests = [], patches = [], navigations = [], timers = [], paymentCalls = [];
 let payment = { success: true, state: 'PENDING' };
 const wx = { navigateTo: options => navigations.push(options), showModal: options => options.success({ confirm: true }), previewImage: () => {} };
 const modules = {
  './api': { reportID: value => value, reportRequest: (...args) => new Promise((resolve, reject) => requests.push({ args, resolve, reject })) },
  './flow': compile('pages/report/flow.ts', {}),
  '../../utils/reportNavigation': { requireReportLogin: () => true },
  '../../utils/virtualpay': { requestVirtualPay: async options => { paymentCalls.push(options); if (payment instanceof Error) throw payment; return payment; } },
 };
 const exports = compile('pages/report/reader.ts', { wx, require: id => modules[id], setTimeout: callback => { const timer = { callback, cleared: false }; timers.push(timer); return timer; }, clearTimeout: timer => { if (timer) timer.cleared = true; } });
 const definition = exports.readerPage(false);
 const instance = { ...definition, data: { ...definition.data, id: 'r1' }, setData(patch) { patches.push(patch); Object.assign(this.data, patch); } };
 return { instance, requests, patches, navigations, timers, paymentCalls, setPayment(value) { payment = value; } };
}
const preview = () => ({ status: 'preview_ready', paid: false, bound: false, can_pay: true, quote: { price_fen: 9900 }, preview_snapshot: { facts: { region: '测试地区' }, pages: ['cover','evidence','profile','priorities'].map((kind, i) => ({ number: i + 1, kind })) } });

test('hide and unload discard actual late network responses and cancel polling', async () => {
 for (const lifecycle of ['onHide', 'onUnload']) {
  const h = harness(); h.instance.onShow();
  assert.equal(h.requests.length, 1);
  h.instance[lifecycle](); const count = h.patches.length;
  h.requests[0].resolve(preview()); await tick();
  assert.equal(h.patches.length, count); assert.equal(h.timers.length, 0);
 }
 const h = harness(); h.instance.onShow(); h.requests[0].resolve({ status: 'full_generating', paid: true }); await tick();
 assert.equal(h.timers.length, 1); h.instance.onHide(); assert.equal(h.timers[0].cleared, true);
});
test('successful payment-sheet result with unconfirmed backend never unlocks PDF', async () => {
 const h = harness(); h.instance._visible = true; h.instance.data.report = preview(); h.instance.data.price = '99.00';
 const paying = h.instance.pay(); await tick();
 assert.equal(h.paymentCalls.length, 1); assert.equal(h.navigations.length, 1);
 assert.equal(h.requests.length, 1);
 h.requests[0].resolve(preview()); await paying;
 assert.equal(h.instance.data.report.paid, false); assert.equal(h.instance.data.canDownload, false);
});
test('cancelled payment does not navigate and can be retried', async () => {
 const h = harness(); h.instance._visible = true; h.instance.data.report = preview();
 h.setPayment({ success: false, errMsg: '支付已取消' });
 for (let attempt = 0; attempt < 2; attempt++) {
  const work = h.instance.pay(); await tick(); h.requests[attempt].resolve(preview()); await work;
  assert.equal(h.instance.data.paying, false); assert.equal(h.navigations.length, 0);
  assert.equal(h.instance.data.error, '支付已取消');
 }
 assert.equal(h.paymentCalls.length, 2);
});
test('unexpected payment failure stays visible and releases the retry button', async () => {
 const h = harness(); h.instance._visible = true; h.instance.data.report = preview();
 h.setPayment(new Error('支付连接中断'));
 const work = h.instance.pay(); await tick(); h.requests[0].resolve(preview()); await work;
 assert.equal(h.instance.data.error, '支付连接中断');
 assert.equal(h.instance.data.paying, false); assert.equal(h.navigations.length, 0);
});
test('failed refresh is visible and a subsequent refresh recovers', async () => {
 const h = harness(); h.instance.onShow(); h.requests[0].reject(new Error('网络中断')); await tick();
 assert.equal(h.instance.data.error, '网络中断');
 const next = h.instance.refresh(); h.requests[1].resolve(preview()); await next;
 assert.equal(h.instance.data.error, ''); assert.equal(h.instance.data.sections.length, 4);
});
test('verified existing contact response does not require a new QR code', async () => {
 const h = harness(); h.instance._visible = true; h.instance.data.report = { ...preview(), paid: true };
 const work = h.instance.contact(); h.requests[0].resolve({ bound: true }); await tick();
 assert.equal(h.requests.length, 2); h.requests[1].resolve({ ...preview(), paid: true, bound: true, status: 'ready' }); await work;
 assert.equal(h.instance.data.error, ''); assert.equal(h.instance.data.canDownload, true);
});

test('delivery copy must not say a failed or unpaid PDF is still being produced', async () => {
 const cases = [
  { paid: true, status: 'failed', label: '生成未完成', hint: /重试/, forbidden: /3–5/ },
  { paid: false, status: 'preview_ready', label: '付款确认后开始', hint: /付款/, forbidden: /3–5/ },
  { paid: true, status: 'full_generating', label: '制作中', hint: /3–5/ },
  { paid: true, status: 'ready', label: '已准备好', hint: /私有云存储/ },
 ];
 for (const item of cases) {
  const h = harness(); h.instance.onShow();
  h.requests[0].resolve({ ...preview(), paid: item.paid, status: item.status }); await tick();
  assert.equal(h.instance.data.pdfStatusLabel, item.label);
  assert.match(h.instance.data.deliveryHint, item.hint);
  if (item.forbidden) assert.doesNotMatch(h.instance.data.deliveryHint, item.forbidden);
 }
});
