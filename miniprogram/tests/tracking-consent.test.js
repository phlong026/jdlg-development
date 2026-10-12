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

function runtime({storage={},configuration,configRequest,offlineEvents=false}={}) {
  let definition,appDefinition,clock=1000;
  let config=configuration||{enabled:true,consent_version:'notice-v1',privacy_notice:notice};
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
  adapter.install();sandbox.App({});appDefinition.onShow({});
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

test('real legacy public entry shows complete optional notice before any analytics and preserves sharing',async()=>{
  const t=runtime(),page=await legacy(t);
  assert.equal(t.modals.length,1);assert.equal(t.entries.length,0);assert.equal(t.events.length,0);
  assert.equal(page.data.contentReady,true);assert.equal(page.data.tools[0].title,'可读工具');
  assert.ok(t.modals[0].content.includes(notice));assert.ok(t.modals[0].content.endsWith('未同意前不采集访问分析。'));
  assert.equal(t.modals[0].showCancel,true);assert.equal(t.modals[0].confirmText,'同意分析');assert.equal(t.modals[0].cancelText,'不同意');
  const share=page.onShareAppMessage();assert.equal(share.path,'/pages/utilitytools/utilitytools');assert.equal(share.promise,undefined);
});

test('decline keeps real reading/sharing available and is remembered across pages and restarts for this notice',async()=>{
  const t=runtime(),page=await legacy(t);t.answer(0,false);await settle();
  assert.equal(page.data.contentReady,true);assert.equal(page.onShareAppMessage().path,'/pages/utilitytools/utilitytools');
  assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,0);assert.equal(t.events.length,0);
  page.onHide();page.onShow();await settle();assert.equal(t.modals.length,1);
  const restarted=runtime({storage:t.storage});await legacy(restarted);assert.equal(restarted.modals.length,0);assert.equal(restarted.entries.length,0);
});

test('only explicit confirm enables collection for the real visible ready public page',async()=>{
  const t=runtime();await legacy(t);assert.equal(t.adapter.getSDK().diagnostics().enabled,false);
  t.answer(0,true);await settle();assert.equal(t.adapter.consented(),true);
  assert.equal(t.entries.length,1);assert.equal(t.entries[0].share_token,'incoming_share');
  assert.equal(t.events.filter(e=>e.event_type==='page_view').length,1);
  assert.equal(t.events.find(e=>e.event_type==='page_view').route_key,'pages/utilitytools/utilitytools');
});

test('every collectable route has the same reachable optional choice; private routes never automatically prompt',async()=>{
  for(const [route,policy] of Object.entries(routes.ROUTES)) {
    const t=runtime(),page=t.register(route,{data:{},onLoad(){},onShow(){return 'business';}});
    t.mount(page);await settle();
    assert.equal(t.modals.length,policy.collect?1:0,route);
    if(!policy.collect)assert.equal(t.adapter.getSDK().get(page),undefined,route+' stays unregistered');
    assert.equal(t.entries.length,0,route+' cannot collect before consent');
  }
  const t=runtime(),own=t.register('pages/designerhome/designerhome',{data:{isSelfHomepage:true}});
  t.mount(own);await settle();assert.equal(t.modals.length,0,'self-management variant stays private');
});

test('async configuration cannot open a public consent dialog after navigating to a private page',async()=>{
  let finish;const t=runtime({configRequest:()=>new Promise(resolve=>{finish=resolve;})});
  const page=t.register('pages/index/index');t.mount(page);page.onHide();t.mount(t.register('pages/login/login'));
  finish({enabled:true,consent_version:'notice-v1',privacy_notice:notice});await settle();
  assert.equal(t.modals.length,0);assert.equal(t.entries.length,0);
});

test('modal failure and disabled or incomplete server configuration cannot grant consent or block business lifecycle',async()=>{
  for(const configuration of [{enabled:false,consent_version:'v',privacy_notice:notice},{enabled:true,privacy_notice:notice},{enabled:true,consent_version:'v'}]){
    const t=runtime({configuration}),page=t.register('pages/index/index',{onShow(){return 'business-result';}});
    page.onLoad({});assert.equal(page.onShow(),'business-result');page.onReady();await settle();
    assert.equal(t.modals.length,0);assert.equal(t.entries.length,0);assert.equal(t.adapter.consented(),false);
  }
  const t=runtime(),page=await legacy(t);t.modals[0].fail({errMsg:'unavailable'});await settle();
  assert.equal(page.data.contentReady,true);assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,0);
});

test('real My page offers anonymous re-choice and immediate withdrawal without collecting private-page views',async()=>{
  const t=runtime(),publicPage=await legacy(t);t.answer(0,false);await settle();publicPage.onHide();
  const my=t.actual('my');my.onLoad({});my.onShow();my.onReady();await settle();
  assert.equal(my.data.hasLoginToken,false);assert.equal(t.modals.length,1,'no automatic prompt in My');
  const choice=my.onTrackingConsentTap();await settle();assert.equal(t.modals.length,2);
  t.answer(1,true);await choice;assert.equal(my.data.trackingConsented,true);
  assert.equal(t.adapter.getSDK().get(my),undefined);assert.equal(t.events.filter(e=>e.route_key==='pages/my/my').length,0);
  my.onWithdrawTrackingConsent();assert.equal(my.data.trackingConsented,false);assert.equal(t.adapter.consented(),false);
  assert.equal(t.adapter.getSDK().diagnostics().enabled,false);assert.equal(t.adapter.getSDK().diagnostics().queued,0);
  my.onHide();publicPage.onShow();await settle();assert.equal(t.modals.length,2,'withdrawal is remembered, not immediately requested again');
});

test('withdrawal wins over a delayed confirm callback and needs no network request',async()=>{
  const t=runtime();await legacy(t);t.adapter.setConsent(false);t.answer(0,true);await settle();
  assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,0);assert.equal(t.adapter.getSDK().diagnostics().queued,0);
});

test('repeated foreground callbacks do not stack dialogs and ambiguous results never mean acceptance',async()=>{
  const t=runtime(),page=await legacy(t);page.onShow();page.onShow();await settle();assert.equal(t.modals.length,1);
  t.modals[0].success({confirm:'true',cancel:false});await settle();assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,0);
  t.adapter.setConsent('true');assert.equal(t.adapter.consented(),false);assert.equal(t.adapter.getSDK().diagnostics().enabled,false);
});

test('withdrawal during a config outage purges queued analytics and remains declined after recovery/restart',async()=>{
  let outage=false;
  const t=runtime({offlineEvents:true,configRequest:async()=>{if(outage)throw new Error('offline');return{enabled:true,consent_version:'notice-v1',privacy_notice:notice};}});
  const page=await legacy(t);t.answer(0,true);await settle();assert.ok(t.adapter.getSDK().diagnostics().queued>0);
  outage=true;await t.adapter.refreshConfig();t.adapter.setConsent(false);
  assert.equal(t.adapter.getSDK().diagnostics().queued,0);assert.equal(t.adapter.consented(),false);
  const restarted=runtime({storage:t.storage});await legacy(restarted);assert.equal(restarted.modals.length,0);assert.equal(restarted.entries.length,0);
  assert.equal(page.data.contentReady,true);
});

test('new notice version requires a new explicit choice and the unchanged rejection is not retried',async()=>{
  const t=runtime(),page=await legacy(t);t.answer(0,false);await settle();
  page.onHide();t.updateConfig({enabled:true,consent_version:'notice-v2',privacy_notice:notice+'\n第二版新增说明。'});page.onShow();await settle();
  assert.equal(t.modals.length,2);assert.ok(t.modals[1].content.includes('第二版新增说明。'));assert.equal(t.entries.length,0);
  t.answer(1,false);await settle();page.onHide();page.onShow();await settle();assert.equal(t.modals.length,2);
});

test('My template binds visible disclosure, re-choice and withdrawal controls outside login-only conditions',()=>{
  const source=fs.readFileSync(path.join(root,'pages/my/my.wxml'),'utf8');
  const section=source.slice(source.indexOf('<view class="menu-section-title">访问分析隐私'));
  assert.match(section,/bindtap="onTrackingConsentTap"/);assert.match(section,/bindtap="onWithdrawTrackingConsent"/);
  assert.ok(section.indexOf('bindtap="onWithdrawTrackingConsent"')<section.indexOf('wx:if="{{hasLoginToken}}"'));
  for(const ext of ['ts','js']){
    const code=fs.readFileSync(path.join(root,'pages/my/my.'+ext),'utf8');
    const handler=code.slice(code.indexOf('async onTrackingConsentTap()'),code.indexOf('onWithdrawTrackingConsent()'));
    assert.doesNotMatch(handler,/isLoggedIn|ensureLoggedIn/);assert.match(code,/trackingConsent\.setConsent\(false\)/);
  }
});
