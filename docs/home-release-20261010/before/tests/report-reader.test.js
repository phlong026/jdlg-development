const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
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
 const exports = compile('pages/report/reader.ts', { wx, require: id => modules[id], setTimeout: (callback, delay) => { const timer = { callback, delay, cleared: false }; timers.push(timer); return timer; }, clearTimeout: timer => { if (timer) timer.cleared = true; } });
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

function housePreview(extra = {}) { return { ...preview(), protocol_version:'report-house-v1', input_hash:'a'.repeat(64), confirmed:false, can_pay:false, next_action:'confirm', preview_snapshot:{meta:{protocol_version:'report-house-v1'},facts:{'intent.priorities':'老人起居、预算控制'},diagnosis:{id:'diagnosis-1',recipe:'space'},pages:['house_profile','house_constraints','house_goals','house_confirmation'].map((kind,i)=>({number:i+1,kind,title:kind,blocks:[{title:'依据',body:'用户输入'}]}))},...extra}; }

test('house confirmation posts the frozen identity then obtains payment eligibility from server', async () => {
 const h = harness(); h.instance.onShow(); h.requests[0].resolve(housePreview()); await tick();
 assert.equal(h.instance.data.needsConfirmation,true);
 h.instance.chooseConfirmation({currentTarget:{dataset:{choice:'confirm'}}});
 const work=h.instance.confirm(); await tick();
 assert.equal(h.requests[1].args[0],'/r1/confirmation');
 assert.equal(h.requests[1].args[2].diagnosis_id,'diagnosis-1');
 assert.equal(h.requests[1].args[2].input_hash,'a'.repeat(64));
 assert.equal(h.requests[1].args[2].choice,'confirm');
 h.requests[1].resolve({confirmed:true}); await tick();
 assert.equal(h.instance.data.report.can_pay,false);
 h.requests[2].resolve(housePreview({confirmed:true,can_pay:true,next_action:'pay'})); await work;
 assert.equal(h.instance.data.report.can_pay,true);
});

test('uncertain image submissions expose support and cannot retry from the reader', async () => {
 const h=harness();h.instance.onShow();h.requests[0].resolve(housePreview({status:'failed',paid:true,next_action:'contact_support',error_code:'image_submission_uncertain'}));await tick();
 assert.equal(h.instance.data.canRetry,false);assert.match(h.instance.data.deliveryHint,/核对|顾问/);
 await h.instance.retry();assert.equal(h.requests.length,1);
});

test('paid-ready house PDF opens without enterprise binding and never promises fourteen pages', async () => {
 const h=harness();h.instance.onShow();h.requests[0].resolve(housePreview({status:'ready',paid:true,bound:false,confirmed:true,next_action:'download'}));await tick();
 assert.equal(h.instance.data.canDownload,true);assert.equal(h.instance.data.modern,true);
 assert.doesNotMatch(h.instance.data.deliveryHint,/绑定/);
});

test('leaving confirmation releases busy state without allowing an old completion to affect a newer attempt',async()=>{
 const h=harness();h.instance.onShow();h.requests[0].resolve(housePreview());await tick();
 h.instance.chooseConfirmation({currentTarget:{dataset:{choice:'confirm'}}});const first=h.instance.confirm();
 h.instance.onHide();h.instance.onShow();h.requests[2].resolve(housePreview());await tick();
 assert.equal(h.instance.data.confirming,false);
 const second=h.instance.confirm();assert.equal(h.instance.data.confirming,true);
 h.requests[1].reject(new Error('old error'));await first;
 assert.equal(h.instance.data.confirming,true);assert.equal(h.instance.data.error,'');
 h.requests[3].reject(new Error('current error'));await second;
 assert.equal(h.instance.data.confirming,false);assert.equal(h.instance.data.error,'current error');
});
test('modern generation continues polling beyond five minutes and stops when hidden or ready',async()=>{
 const h=harness();h.instance.onShow();
 for(let index=0;index<65;index++) {
  h.requests[index].resolve(housePreview({status:'full_generating',paid:true,confirmed:true}));await tick();
  assert.equal(h.timers.length,index+1,'must keep a timer while modern report is generating');
  assert.equal(h.timers[index].delay,index<12?5000:15000);
  h.timers[index].callback();
 }
 h.requests[65].resolve(housePreview({status:'ready',paid:true,confirmed:true}));await tick();
 assert.equal(h.timers.length,65);assert.equal(h.instance.data.canDownload,true);
 h.instance.onHide();assert.equal(h.timers[64].cleared,true);
});
test('house confirmation exposes required rooms and core design constraints from frozen facts',async()=>{
 const h=harness(),report=housePreview();Object.assign(report.preview_snapshot.facts,{
  'intent.floors':'2','intent.bedrooms':'4','house.first_floor_elder_room':'yes','house.style':'新中式',
  'house.room.study':JSON.stringify({id:'study',name:'书房',count:1,preferred_floor:2,required:true})
 });h.instance.onShow();h.requests[0].resolve(report);await tick();
 assert.ok(h.instance.data.houseInputs.some(item=>item.title==='书房'&&/必须满足/.test(item.body)&&/2层/.test(item.body)));
 assert.ok(h.instance.data.houseInputs.some(item=>/层数/.test(item.title)&&item.body==='2'));
});

test('server omits unrestricted room floor and preserves neighbor facts in confirmation',async()=>{
 const h=harness(),report=housePreview();Object.assign(report.preview_snapshot.facts,{
  'house.neighbors':'东侧紧邻邻房',
  'house.room.study':JSON.stringify({id:'study',name:'书房',count:1,required:true})
 });h.instance.onShow();h.requests[0].resolve(report);await tick();
 assert.equal(h.instance.data.error,'');assert.equal(h.instance.data.sections.length,4);
 assert.ok(h.instance.data.houseInputs.some(item=>item.title==='书房'&&/楼层不限/.test(item.body)));
 assert.ok(h.instance.data.houseInputs.some(item=>item.body==='东侧紧邻邻房'));
});

test('transient polling errors recover and authorization errors stop requests',async()=>{
 const h=harness();h.instance.onShow();h.requests[0].resolve(housePreview({paid:true,status:'full_generating'}));await tick();
 h.timers[0].callback();h.requests[1].reject(new Error('网络中断'));await tick();
 assert.equal(h.instance.data.error,'网络中断');assert.equal(h.timers.length,2);assert.equal(h.timers[1].delay,15000);
 h.timers[1].callback();h.requests[2].resolve(housePreview({paid:true,status:'ready'}));await tick();
 assert.equal(h.instance.data.error,'');assert.equal(h.instance.data.canDownload,true);assert.equal(h.timers.length,2);
 const initial=harness();initial.instance.onShow();initial.requests[0].reject(new Error('暂时超时'));await tick();assert.equal(initial.timers.length,1);
 initial.timers[0].callback();initial.requests[1].reject({message:'请登录',statusCode:401});await tick();assert.equal(initial.timers.length,1);
});

test('modern payment reasons show customer copy rather than internal codes',async()=>{
 for(const [reason,expected] of [['confirmation_required','请先确认'],['payment_disabled','暂未开放'],['provider_internal_new_reason','稍后重试']]) {
  const h=harness();h.instance.onShow();h.requests[0].resolve(housePreview({payment_block_reason:reason}));await tick();
  assert.match(h.instance.data.paymentBlockHint,new RegExp(expected));assert.doesNotMatch(h.instance.data.paymentBlockHint,/_/);
 }
});
