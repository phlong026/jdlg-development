'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const routes=require('../miniprogram/utils/tracking/routes');
const root=path.join(__dirname,'../miniprogram');
const settle=()=>new Promise(resolve=>setImmediate(resolve));
const notice='为分析分享内容的访问效果，记录公开页面访问、可见时长、明确点击、分享发起和可观测媒体播放。使用随机安装标识，登录后仅由服务端关联账号。\n不记录输入正文、联系人、位置或设备指纹；分享不代表送达，时长不代表理解。\n原始数据保留90天，汇总180天。处理者与删除渠道：本次测试配置的完整告知末尾。';

function runtime({storage={},configuration,configRequest,offlineEvents=false,accepted=false}={}) {
  let definition,appDefinition,clock=1000;
  let config=configuration||{enabled:true,consent_version:'notice-v1',privacy_notice:notice};
  if(accepted) storage.share_tracking_consent_choice_v1={accepted:true,explicit:true,version:config.consent_version,notice:config.privacy_notice};
  const modals=[],business=[],entries=[],events=[],toasts=[];
  const api={async request(suffix,method,body){
    if(suffix==='/config')return configRequest?configRequest():config;
    if(suffix==='/entry'){entries.push(body);return{entry_token:'credential_'+entries.length,session_id:'session_'+entries.length,content_accessible:true};}
    if(suffix==='/events/batch'){if(offlineEvents)throw new Error('offline');events.push(...body.events);return{results:body.events.map(e=>({event_id:e.event_id,status:'accepted'}))};}
    if(suffix==='/shares')return{share_token:'new_share_token',share_id:'new_share_id'};
    throw new Error('unexpected API '+suffix);
  }};
  const wx={getPerformance:()=>({now:()=>clock}),getStorageSync:key=>storage[key],
    setStorageSync:(key,value)=>{storage[key]=value;},removeStorageSync:key=>{delete storage[key];},
    getRandomValues:({success})=>success({randomValues:new Uint8Array(24).fill(7).buffer}),
    showModal:options=>modals.push(options),showToast:options=>toasts.push(options),
    request:options=>business.push(options),onNetworkStatusChange(){},setBackgroundColor(){}};
  const sandbox={module:{exports:{}},require:name=>name==='./api'?api:require(path.join(root,'utils/tracking',name.slice(2))),wx,
    Page:value=>{definition=value;},App:value=>{appDefinition=value;},setInterval:()=>1,setTimeout,clearTimeout,Date,Math,Promise,Uint8Array};
  vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/index.js'),'utf8'),sandbox);
  const adapter=sandbox.module.exports;
  adapter.install();sandbox.App({});appDefinition.onLaunch({});appDefinition.onShow({});
  function register(route,custom={}) {
    sandbox.Page(custom);
    const page={...definition,route,data:JSON.parse(JSON.stringify(definition.data||{})),setData(value,done){Object.assign(this.data,value);if(done)done.call(this);}};
    return page;
  }
  const materialSandbox={module:{exports:{}},require:()=>api,wx,Promise};
  vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/materials.js'),'utf8'),materialSandbox);
  function actual(name) {
    const source=fs.readFileSync(path.join(root,'pages',name,name+'.js'),'utf8');
    vm.runInNewContext(source,{Page:sandbox.Page,exports:{},require:name=>name.endsWith('/tracking/index')?adapter:name.endsWith('/tracking/materials')?materialSandbox.module.exports:new Proxy({},{get:(_,key)=>key==='resolveAssetPath'?v=>v:()=>''}),wx,console:{log(){},error(){}},Promise,Date,Math,Set,setTimeout,clearTimeout});
    return{...definition,route:'pages/'+name+'/'+name,data:JSON.parse(JSON.stringify(definition.data||{})),setData(value,done){Object.assign(this.data,value);if(done)done.call(this);}};
  }
  return{adapter,modals,business,entries,events,toasts,storage,register,actual,
    updateConfig(value){config=value;},advance(ms){clock+=ms;},
    mount(page,options={}){page.onLoad(options);page.onShow();page.onReady();},
    answer(index,accepted){modals[index].success(accepted?{confirm:true,cancel:false}:{confirm:false,cancel:true});}};
}

async function legacy(t) {
  const page=t.actual('utilitytools');t.mount(page,{st:'incoming_share'});
  assert.equal(t.business.length,1,'real public page onLoad still requests business content');
  t.business[0].success({statusCode:200,data:{code:0,data:{list:[{id:12,title:'可读工具'}]}}});
  await settle();return page;
}

test('unselected users can read and share without analytics or implicit attribution',async()=>{
  const t=runtime(),page=await legacy(t);
  assert.equal(t.modals.length,0,'the page uses its inline first-choice controls instead of an automatic modal');
  assert.equal(t.adapter.consented(),false);
  assert.equal(t.entries.length,0,'no analytics entry is created before an explicit choice');
  assert.equal(t.storage.share_tracking_consent_choice_v1,undefined,'reading alone writes no choice record');
  assert.equal(t.adapter.consentChoice().resolved,false);assert.equal(t.adapter.consentChoice().accepted,false);
  assert.equal(page.data.contentReady,true);
  const shared=page.onShareAppMessage();
  assert.doesNotMatch(shared.path,/st=/,'ordinary sharing remains available without an analytics token');
});

test('explicit withdrawal survives config refresh and restart',async()=>{
  const t=runtime({accepted:true}),page=await legacy(t);assert.equal(t.entries.length,1);
  t.adapter.setConsent(false);const count=t.entries.length;
  page.onHide();page.onShow();await t.adapter.refreshConfig();await settle();
  assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,count);
  const restarted=runtime({storage:t.storage});await legacy(restarted);
  assert.equal(restarted.entries.length,0);assert.equal(restarted.adapter.consented(),false);
});

test('only acceptance of the current version and notice enables collection',async()=>{
  for(const old of [
    {share_tracking_consent_v1:{version:'notice-v1'}},
    {share_tracking_consent_choice_v1:{accepted:true,version:'notice-v1',notice}},
    {share_tracking_consent_choice_v1:{accepted:true,explicit:true,version:'old',notice}},
  ]){
    const t=runtime({storage:old}),page=await legacy(t);
    assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,0);
    assert.equal(t.adapter.consentChoice().resolved,false,'a stale choice must return to the unresolved state');
    assert.equal(page.data.contentReady,true);
  }
  const accepted=runtime({accepted:true});await legacy(accepted);
  assert.equal(accepted.adapter.consented(),true);assert.ok(accepted.entries.length>=1);
});

// T7.3：改 notice 不升 version 时，明确关闭的存量记录不能被默认开启覆盖。
test('a notice-only change keeps an explicit decline closed and never re-enables collection',async()=>{
  const stored={accepted:false,explicit:true,version:'notice-v1',notice:'old notice',at:'2026-01-01T00:00:00.000Z'};
  const t=runtime({storage:{share_tracking_consent_choice_v1:{...stored}},configuration:{enabled:true,consent_version:'notice-v1',privacy_notice:notice+' 改版'}});
  const page=await legacy(t);
  assert.equal(t.adapter.consentChoice().resolved,true,'明确关闭必须保持 resolved');
  assert.equal(t.adapter.consentChoice().accepted,false);
  assert.equal(t.adapter.consented(),false);
  assert.equal(t.entries.length,0,'关闭状态不得因 notice 变更而恢复采集');
  assert.equal(t.modals.length,0,'默认开启阶段不得再自动询问');
  assert.equal(page.data.contentReady,true,'阅读能力不受影响');
});

// T7.3：明确关闭是撤回语义，版本变化也不能静默开启。
test('an explicit decline stays closed even if the consent version changes',async()=>{
  const stored={accepted:false,explicit:true,version:'old-version',notice:'old notice'};
  const t=runtime({storage:{share_tracking_consent_choice_v1:{...stored}}});
  await legacy(t);
  assert.equal(t.adapter.consentChoice().resolved,true);
  assert.equal(t.adapter.consented(),false);
  assert.equal(t.entries.length,0);
});

// T7.3：旧的明确同意在 notice/版本变化后回到未决；这里只锁定 resolved 合约，
// 不预设未来 T7.1 是「重新征询」还是「默认开启并标记来源」。
test('a stale explicit acceptance returns to the unresolved state for the default-on migration',async()=>{
  const stored={accepted:true,explicit:true,version:'notice-v1',notice:'old notice'};
  const t=runtime({storage:{share_tracking_consent_choice_v1:{...stored}},configuration:{enabled:true,consent_version:'notice-v1',privacy_notice:notice+' 改版'}});
  await legacy(t);
  assert.equal(t.adapter.consentChoice().resolved,false);
  assert.equal(t.adapter.consented(),false,'未匹配当前告知前不采集');
  assert.equal(t.storage.share_tracking_consent_choice_v1.notice,'old notice','只读流程不得改写存量记录');
});

test('a changed notice collects nothing, asks again, and resumes only after a new explicit acceptance',async()=>{
  const t=runtime({accepted:true}),page=await legacy(t);assert.ok(t.entries.length>0);
  const before=t.entries.length;
  t.updateConfig({enabled:true,consent_version:'notice-v2',privacy_notice:notice+' changed'});
  await t.adapter.refreshConfig();await settle();
  assert.equal(t.adapter.consented(),false);assert.equal(t.adapter.consentChoice().resolved,false);
  assert.equal(t.entries.length,before,'a new declaration cannot inherit the old acceptance');
  const choosing=t.adapter.showConsentChoices(page);await settle();
  assert.equal(t.modals.length,1);assert.match(t.modals[0].content,/changed/);
  t.answer(0,true);await choosing;await settle();
  assert.equal(t.adapter.consented(),true);assert.equal(t.adapter.consentChoice().resolved,true);
  assert.ok(t.entries.length>before,'collection resumes only after accepting the current declaration');
});

test('incomplete or disabled server configuration cannot grant consent',async()=>{
  for(const configuration of [{enabled:false,consent_version:'v',privacy_notice:notice},{enabled:true,privacy_notice:notice},{enabled:true,consent_version:'v'}]){
    const t=runtime({configuration}),page=t.register('pages/index/index',{onShow(){return 'business-result';}});
    page.onLoad({});assert.equal(page.onShow(),'business-result');page.onReady();await settle();
    assert.equal(t.modals.length,0);assert.equal(t.entries.length,0);assert.equal(t.adapter.consented(),false);
  }
});

test('route policy still gates collection: collectable routes register, private and self-homepage never do',async()=>{
  for(const [route,policy] of Object.entries(routes.ROUTES)) {
    const t=runtime(),page=t.register(route,{data:{},onLoad(){},onShow(){return 'business';}});
    t.mount(page);await settle();
    if(policy.collect)assert.notEqual(t.adapter.getSDK().get(page),undefined,route+' is registered');
    else assert.equal(t.adapter.getSDK().get(page),undefined,route+' stays unregistered');
    if(!policy.collect)assert.equal(t.entries.length,0,route+' cannot collect');
  }
  const t=runtime(),own=t.register('pages/designerhome/designerhome',{data:{isSelfHomepage:true}});
  t.mount(own);await settle();await settle();
  assert.equal(t.events.filter(e=>e.route_key==='pages/designerhome/designerhome').length,0,'self-management visits stay unobserved');
});

test('async configuration without consent never collects private pages',async()=>{
  let finish;const t=runtime({configRequest:()=>new Promise(resolve=>{finish=resolve;})});
  const page=t.register('pages/index/index');t.mount(page);page.onHide();t.mount(t.register('pages/login/login'));
  finish({enabled:true,consent_version:'notice-v1',privacy_notice:notice});await settle();
  assert.equal(t.modals.length,0);
  assert.equal(t.events.filter(e=>e.route_key==='pages/login/login').length,0,'login stays private');
});

test('manual modal keeps its race guards: withdrawal wins, ambiguous answers never grant, calls do not stack',async()=>{
  const t=runtime(),page=await legacy(t);
  // 手动查看完整告知：弹窗打开后发生撤回时，迟到的确认不能重新开启
  const choosing=t.adapter.showConsentChoices(page);await settle();
  assert.equal(t.modals.length,1);assert.ok(t.modals[0].content.includes(notice),'manual modal shows the complete notice');
  t.adapter.setConsent(false);
  t.answer(0,true);await choosing;await settle();
  assert.equal(t.adapter.consented(),false,'withdrawal wins over a delayed confirm');
  // 含糊的回调结果永远不等于同意
  const second=t.adapter.showConsentChoices(page);await settle();
  t.modals[1].success({confirm:'true',cancel:false});await second;await settle();
  assert.equal(t.adapter.consented(),false);
  // 重复调用复用同一请求，不叠弹窗
  const a=t.adapter.showConsentChoices(page),b=t.adapter.showConsentChoices(page);await settle();
  assert.equal(t.modals.length,3,'deduplicated request adds only one modal');
  t.answer(2,true);await a;await b;await settle();
  assert.equal(t.adapter.consented(),true,'answering the deduplicated modal still grants once');
});

test('withdrawal during a config outage purges queued analytics and remains declined after recovery/restart',async()=>{
  let outage=false;
  const t=runtime({accepted:true,offlineEvents:true,configRequest:async()=>{if(outage)throw new Error('offline');return{enabled:true,consent_version:'notice-v1',privacy_notice:notice};}});
  const page=await legacy(t);assert.ok(t.adapter.getSDK().diagnostics().queued>0);
  outage=true;await t.adapter.refreshConfig();t.adapter.setConsent(false);
  assert.equal(t.adapter.getSDK().diagnostics().queued,0);assert.equal(t.adapter.consented(),false);
  const restarted=runtime({storage:t.storage});await legacy(restarted);assert.equal(restarted.modals.length,0);assert.equal(restarted.entries.length,0);
  assert.equal(page.data.contentReady,true);
});

// ─────────── 产物同步守卫：设置页持有开关，我的页旧授权 UI 与自动弹窗不得回归 ───────────

test('settings page owns the switch and stale consent UI is gone from My and page prompts',()=>{
  const settingsWxml=fs.readFileSync(path.join(root,'pages/settings/settings.wxml'),'utf8');
  assert.match(settingsWxml,/bindchange="onToggleTracking"/);assert.match(settingsWxml,/checked="\{\{trackingEnabled\}\}"/);
  for(const ext of ['ts','js']){
    const code=fs.readFileSync(path.join(root,'pages/settings/settings.'+ext),'utf8');
    assert.match(code,/onToggleTracking/,ext);
    const handler=code.slice(code.indexOf('onToggleTracking('),code.indexOf('ensureLoggedIn'));
    assert.doesNotMatch(handler,/isLoggedIn|ensureLoggedIn/,'switch must work without login requirements');
    assert.match(code,/setConsent\(/,ext);
    assert.match(code,/syncTrackingEnabled/,ext);
  }
  // 我的页不再承载授权 UI 与处理器（ts 与编译产物 js 都要干净）
  const myWxml=fs.readFileSync(path.join(root,'pages/my/my.wxml'),'utf8');
  assert.doesNotMatch(myWxml,/访问分析隐私|onTrackingConsentTap|onWithdrawTrackingConsent/);
  for(const ext of ['ts','js']){
    const code=fs.readFileSync(path.join(root,'pages/my/my.'+ext),'utf8');
    assert.doesNotMatch(code,/onTrackingConsentTap|onWithdrawTrackingConsent|syncTrackingConsent/,ext);
  }
  // 编译产物守卫：index.js 仅在明确同意后记录，且页面展示不自动弹窗。
  const bundled=fs.readFileSync(path.join(root,'utils/tracking/index.js'),'utf8');
  assert.doesNotMatch(bundled,/safely\(\(\)=>setConsent\(true\)\)/,'configuration cannot imply agreement');
  assert.match(bundled,/choice\.version===config\.consent_version/,'choice must match the current consent version');
  assert.match(bundled,/choice\.notice===config\.privacy_notice/,'choice must match the current full notice');
  assert.doesNotMatch(bundled,/if\(visible\)\{consentPage=page;showConsentChoices\(page\);\}/,'auto prompt must stay removed');
});
