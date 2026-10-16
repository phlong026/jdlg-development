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

// ─────────── 静默同意模型：从未选择的用户随配置拉取自动开启；明确拒绝过的不覆盖 ───────────

test('silent consent collects the first public visit without any dialog while reading and sharing keep working',async()=>{
  const t=runtime(),page=await legacy(t);
  assert.equal(t.modals.length,0,'no prompt is shown for the silent default');
  assert.equal(page.data.contentReady,true);assert.equal(page.data.tools[0].title,'可读工具');
  assert.equal(t.entries.length,1);assert.equal(t.entries[0].share_token,'incoming_share');
  assert.equal(t.events.filter(e=>e.event_type==='page_view').length,1);
  assert.equal(t.events.find(e=>e.event_type==='page_view').route_key,'pages/utilitytools/utilitytools');
  const share=page.onShareAppMessage();assert.equal(share.path,'/pages/utilitytools/utilitytools');
});

test('the settings switch can stop collection and the rejection survives config refreshes and restarts',async()=>{
  const t=runtime(),page=await legacy(t);
  assert.equal(t.adapter.setConsent(false),true);
  const collected=t.entries.length;
  page.onHide();page.onShow();await t.adapter.refreshConfig();await settle();
  assert.equal(t.adapter.consented(),false);
  assert.equal(t.entries.length,collected,'declined pages do not collect');
  const eventsBeforeDecline=t.events.length;
  page.onHide();page.onShow();await t.adapter.refreshConfig();await settle();
  assert.equal(t.events.length,eventsBeforeDecline,'no new events after the switch is turned off');
  const restarted=runtime({storage:t.storage});await legacy(restarted);
  assert.equal(restarted.modals.length,0);assert.equal(restarted.entries.length,0,'rejection persists across restarts');
});

test('a previously accepted notice is silently re-accepted after an update, and a rejection is never retried',async()=>{
  const t=runtime(),page=await legacy(t);assert.equal(t.adapter.consented(),true);
  t.updateConfig({enabled:true,consent_version:'notice-v2',privacy_notice:notice+'\n第二版新增说明。'});
  await t.adapter.refreshConfig();await settle();
  assert.equal(t.modals.length,0,'updated notice re-accepts silently for prior accepters');
  assert.equal(t.adapter.consented(),true);assert.ok(t.entries.length>0);

  const declined=runtime();assert.equal(declined.adapter.setConsent(false),true);
  declined.updateConfig({enabled:true,consent_version:'notice-v2',privacy_notice:notice+'\n第二版新增说明。'});
  await declined.adapter.refreshConfig();await settle();
  assert.equal(declined.adapter.consented(),false,'explicit rejection is not overridden');
  assert.equal(declined.modals.length,0);assert.equal(declined.entries.length,0);
});

test('incomplete or disabled server configuration cannot grant silent consent',async()=>{
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

test('async configuration with silent consent never collects private pages',async()=>{
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
  const t=runtime({offlineEvents:true,configRequest:async()=>{if(outage)throw new Error('offline');return{enabled:true,consent_version:'notice-v1',privacy_notice:notice};}});
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
  // 编译产物守卫：index.js 必须包含静默同意，且页面展示不再自动弹窗
  const bundled=fs.readFileSync(path.join(root,'utils/tracking/index.js'),'utf8');
  assert.match(bundled,/safely\(\(\)=>setConsent\(true\)\)/,'silent consent must exist in the shipped artifact');
  assert.doesNotMatch(bundled,/if\(visible\)\{consentPage=page;showConsentChoices\(page\);\}/,'auto prompt must stay removed');
});
