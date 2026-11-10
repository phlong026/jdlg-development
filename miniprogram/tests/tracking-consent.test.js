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

function runtime({storage={},configuration,configRequest,offlineEvents=false,accepted=false,failChoiceRead=false,failChoiceWrite=false,failChoiceReadOnce=false}={}) {
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
  const CHOICE='share_tracking_consent_choice_v1';
  let readOnceUsed=false, choiceWriteFails=failChoiceWrite;
  const wx={getPerformance:()=>({now:()=>clock}),
    getStorageSync:key=>{if(failChoiceRead&&key===CHOICE)throw new Error('storage read failed');if(failChoiceReadOnce&&key===CHOICE&&!readOnceUsed){readOnceUsed=true;throw new Error('storage read failed once');}return storage[key];},
    setStorageSync:(key,value)=>{if(choiceWriteFails&&key===CHOICE)throw new Error('storage write failed');storage[key]=value;},removeStorageSync:key=>{delete storage[key];},
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
    updateConfig(value){config=value;},advance(ms){clock+=ms;},setChoiceWriteFail(value){choiceWriteFails=value===true;},
    mount(page,options={}){page.onLoad(options);page.onShow();page.onReady();},
    answer(index,accepted){modals[index].success(accepted?{confirm:true,cancel:false}:{confirm:false,cancel:true});}};
}

async function legacy(t) {
  const page=t.actual('utilitytools');t.mount(page,{st:'incoming_share'});
  assert.equal(t.business.length,1,'real public page onLoad still requests business content');
  t.business[0].success({statusCode:200,data:{code:0,data:{list:[{id:12,title:'可读工具'}]}}});
  await settle();return page;
}

// T7.1：新装/清缓存用户默认开启并静默记录，不再每次弹窗询问。
test('first-time users get default-on analytics without any prompt and can still read and share',async()=>{
  const t=runtime(),page=await legacy(t);
  assert.equal(t.modals.length,0,'默认开启不得再自动弹询问框');
  assert.equal(t.adapter.consented(),true,'从未选择过的用户默认开启');
  const stored=t.storage.share_tracking_consent_choice_v1;
  assert.equal(stored.explicit,true);assert.equal(stored.accepted,true);
  assert.equal(stored.source,'default','默认开启必须记录来源');
  assert.ok(t.entries.length>=1,'开启后访问数据正常上报');
  assert.equal(page.data.contentReady,true);
  const shared=page.onShareAppMessage();
  assert.ok(shared && shared.path,'分享能力保持可用');
});

test('explicit withdrawal survives config refresh and restart',async()=>{
  const t=runtime({accepted:true}),page=await legacy(t);assert.equal(t.entries.length,1);
  t.adapter.setConsent(false);const count=t.entries.length;
  page.onHide();page.onShow();await t.adapter.refreshConfig();await settle();
  assert.equal(t.adapter.consented(),false);assert.equal(t.entries.length,count);
  const restarted=runtime({storage:t.storage});await legacy(restarted);
  assert.equal(restarted.entries.length,0);assert.equal(restarted.adapter.consented(),false);
});

// T7.1：没有 explicit 记录或旧记录回到未决时，由默认开启重新绑定并标记 default。
test('stale or implicit records are rebound by the default-on migration and marked default',async()=>{
  for(const old of [
    {share_tracking_consent_v1:{version:'notice-v1'}},
    {share_tracking_consent_choice_v1:{accepted:true,version:'notice-v1',notice}},
    {share_tracking_consent_choice_v1:{accepted:true,explicit:true,version:'old',notice}},
  ]){
    const t=runtime({storage:old}),page=await legacy(t);
    assert.equal(t.adapter.consented(),true,'默认开启应重新绑定');
    const stored=t.storage.share_tracking_consent_choice_v1;
    assert.equal(stored.explicit,true);assert.equal(stored.accepted,true);
    assert.equal(stored.source,'default');
    assert.ok(t.entries.length>=1);
    assert.equal(page.data.contentReady,true);
  }
  const accepted=runtime({accepted:true});await legacy(accepted);
  assert.equal(accepted.adapter.consented(),true);assert.ok(accepted.entries.length>=1);
  assert.equal(accepted.storage.share_tracking_consent_choice_v1.source,undefined,'存量主动同意不伪造成默认来源');
  // 用户主动开启/关闭都标记为 user 来源
  accepted.adapter.setConsent(false); accepted.adapter.setConsent(true);
  assert.equal(accepted.storage.share_tracking_consent_choice_v1.source,'user');
});

// T7.1：config.enabled=false 时不启用、不弹、不报错。
test('a disabled server configuration never enables default-on analytics',async()=>{
  const t=runtime({configuration:{enabled:false,consent_version:'notice-v1',privacy_notice:notice}});
  const page=await legacy(t);
  assert.equal(t.adapter.consented(),false);
  assert.equal(t.entries.length,0);
  assert.equal(t.modals.length,0);
  assert.equal(t.storage.share_tracking_consent_choice_v1,undefined,'未启用不得写入默认同意');
  assert.equal(page.data.contentReady,true);
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

// T7.1：旧的明确同意（version/notice 不匹配）回到未决后，由默认开启重新绑定并标记 default。
test('a stale explicit acceptance is rebound by the default-on migration and marked default',async()=>{
  const stored={accepted:true,explicit:true,version:'notice-v1',notice:'old notice'};
  const t=runtime({storage:{share_tracking_consent_choice_v1:{...stored}},configuration:{enabled:true,consent_version:'notice-v1',privacy_notice:notice+' 改版'}});
  await legacy(t);
  assert.equal(t.adapter.consentChoice().resolved,true);
  const bound=t.storage.share_tracking_consent_choice_v1;
  assert.equal(bound.accepted,true);assert.equal(bound.source,'default');
  assert.equal(t.adapter.consented(),true);
  assert.ok(t.entries.length>=1);
});

// T7.1：告知变化后旧的主动同意不再继承为 user 来源，而是按默认开启重新绑定。
test('a changed declaration re-binds collection as default-on instead of inheriting the old acceptance',async()=>{
  const t=runtime({accepted:true}),page=await legacy(t);assert.ok(t.entries.length>0);
  assert.equal(t.storage.share_tracking_consent_choice_v1.source,undefined,'存量主动同意保留原样');
  t.updateConfig({enabled:true,consent_version:'notice-v2',privacy_notice:notice+' changed'});
  await t.adapter.refreshConfig();await settle();
  const rebound=t.storage.share_tracking_consent_choice_v1;
  assert.equal(rebound.version,'notice-v2');
  assert.equal(rebound.source,'default','告知变化后按默认开启重新绑定，不继承旧主动同意');
  assert.equal(t.adapter.consented(),true);
  assert.equal(t.modals.length,0,'默认开启不弹询问框');
  assert.equal(page.data.contentReady,true);
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

// T7.1：存储读/写异常必须失败关闭，不得误开启或覆盖已明确关闭。
test('storage failures never silently enable analytics or override an explicit decline',async()=>{
  const declined=runtime({storage:{share_tracking_consent_choice_v1:{accepted:false,explicit:true,version:'notice-v1',notice}},failChoiceRead:true});
  await legacy(declined);
  assert.equal(declined.adapter.consented(),false,'读取失败必须失败关闭');
  assert.equal(declined.entries.length,0,'读取失败不得采集');

  const writeFail=runtime({failChoiceWrite:true});
  await legacy(writeFail);
  assert.equal(writeFail.adapter.consented(),false,'写入失败不得在内存开启');
  assert.equal(writeFail.entries.length,0);
  assert.equal(writeFail.storage.share_tracking_consent_choice_v1,undefined,'写入失败不得留下内存同意');
});

// T7.1：配置未就绪时要有非阻塞提示，不能静默失败。
test('a not-ready consent configuration surfaces a non-blocking notice',()=>{
  const wxml=fs.readFileSync(path.join(root,'pages/contentreader/contentreader.wxml'),'utf8');
  assert.match(wxml,/consentUnavailable/,'阅读页必须有配置未就绪提示');
  assert.match(wxml,/访问分析设置暂时未就绪/);
  const code=fs.readFileSync(path.join(root,'pages/contentreader/contentreader.ts'),'utf8');
  assert.match(code,/configError/,'请求失败要与服务明确关闭区分');
});

// T7.2：隐私页与设置弹窗改为「默认开启、可关闭」，不再声称需要先开启。
test('privacy copy and the settings modal no longer claim opt-in or a disabled default', () => {
  for (const file of ['pages/privacy/privacy.ts', 'pages/privacy/privacy.js']) {
    const code = fs.readFileSync(path.join(root, file), 'utf8');
    assert.match(code, /默认记录/, file);
    assert.match(code, /关闭后不再记录/, file);
    assert.doesNotMatch(code, /在你开启“访问分析”后/, file);
  }
  const tracking = fs.readFileSync(path.join(root, 'utils/tracking/index.ts'), 'utf8');
  assert.doesNotMatch(tracking, /分析为自愿选择/, '设置弹窗不得再称自愿选择');
  assert.doesNotMatch(tracking, /当前状态：未开启。开启后我们记录/, '设置弹窗不得再声称默认未开启');
  assert.match(tracking, /默认记录/);
  assert.match(tracking, /当前状态：已关闭（你已选择关闭）/, '已关闭状态与默认开启区分');
  const ops = fs.readFileSync(path.join(root, '../../docs/phase3/tracking-privacy-notice-ops-20261110.md'), 'utf8');
  assert.match(ops, /TRACKING_PRIVACY_NOTICE/);
  assert.match(ops, /不要.*修改\s*`?TRACKING_CONSENT_VERSION`?/);
});

// T7.1：一次读取异常后恢复，仍必须尊重存量「明确关闭」，不能被默认开启覆盖。
test('a transient consent read failure never overrides an explicit decline',async()=>{
  const t=runtime({storage:{share_tracking_consent_choice_v1:{accepted:false,explicit:true,version:'notice-v1',notice}},failChoiceReadOnce:true});
  await legacy(t);
  assert.equal(t.adapter.consented(),false,'恢复读取后必须看到明确关闭');
  assert.equal(t.entries.length,0,'不得采集');
  assert.equal(t.storage.share_tracking_consent_choice_v1.accepted,false,'存量关闭记录不得被改写');
});

// T7.1：关闭写入失败时必须立即停止采集并保留本会话关闭意图。
test('a failed close write stops runtime collection immediately',async()=>{
  const t=runtime({accepted:true,failChoiceWrite:true});const page=await legacy(t);
  assert.ok(t.entries.length>=1,'默认开启后应有上报');
  const before=t.entries.length;
  const ok=t.adapter.setConsent(false);
  assert.equal(ok,false,'写入失败必须返回 false');
  assert.equal(t.adapter.consented(),false,'本会话应显示已关闭');
  page.onHide();page.onShow();await t.adapter.refreshConfig();
  assert.equal(t.adapter.consented(),false,'刷新后不得重新开启');
  assert.equal(t.entries.length,before,'关闭后不得继续采集');
});

// T7.2：阅读页提示块与隐私页口径一致，不得出现「明确同意后才记录」。
test('reader consent copy no longer claims recording requires an explicit opt-in',()=>{
  const wxml=fs.readFileSync(path.join(root,'pages/contentreader/contentreader.wxml'),'utf8');
  assert.doesNotMatch(wxml,/只有明确同意后才会记录/,'提示块不得再声称需要主动同意');
  assert.match(wxml,/默认开启/);
  assert.match(wxml,/关闭后不再记录/);
});

// T7.1：设置弹窗必须如实返回保存结果，写入失败不得显示已开启。
test('the consent modal reports a failed save instead of claiming success',async()=>{
  const t=runtime({storage:{share_tracking_consent_choice_v1:{accepted:false,explicit:true,version:'notice-v1',notice}},failChoiceWrite:true});
  const page=t.register('pages/my/my',{data:{consented:false}});
  const choosing=t.adapter.showConsentChoices(page);await settle();
  assert.equal(t.modals.length,1);
  t.answer(0,true);
  const result=await choosing;
  assert.equal(result,false,'写入失败时必须返回 false');
  assert.equal(page.data.consented,false,'页面不得显示已开启');
});

// T7.1：设置页必须消费关闭保存结果，失败时不得提示「已关闭」。
test('the settings page consumes the close result and warns when it was not saved',()=>{
  for(const file of ['settings.ts','settings.js']){
    const code=fs.readFileSync(path.join(root,'pages/settings',file),'utf8');
    assert.match(code,/const saved = trackingConsent\.setConsent\(false\)/,file);
    assert.match(code,/关闭暂未保存成功/,file);
    assert.doesNotMatch(code,/setConsent\(false\);[\s\S]{0,80}已关闭访问分析/,file+' 不得无条件提示已关闭');
  }
});

// T7.1：存储恢复后保存成功，阅读页的未决提示必须同步清掉。
test('a successful save clears the pending prompt on the reader page',async()=>{
  const t=runtime({failChoiceWrite:true});
  const page=t.register('pages/my/my',{data:{consented:false,consentAvailable:true,consentChoicePending:true,consentNotice:notice}});
  t.setChoiceWriteFail(false);
  const choosing=t.adapter.showConsentChoices(page);await settle();
  t.answer(0,true);
  const ok=await choosing;
  assert.equal(ok,true,'存储恢复后应保存成功');
  assert.equal(page.data.consented,true);
  assert.equal(page.data.consentChoicePending,false,'未决提示必须清掉');
});

// T7.1：弹窗异常出口（success({})、fail）也要同步真实状态与最新告知。
test('abnormal modal exits still sync consent state and the latest notice',async()=>{
  const t=runtime({failChoiceWrite:true,configuration:{enabled:true,consent_version:'notice-v1',privacy_notice:notice+' 新版'}});
  const page=t.register('pages/my/my',{data:{consented:false,consentAvailable:true,consentChoicePending:true,consentNotice:'old notice'}});
  t.setChoiceWriteFail(false);
  const choosing=t.adapter.showConsentChoices(page);await settle();
  assert.equal(page.data.consented,true,'刷新配置并默认开启后页面应同步');
  assert.equal(page.data.consentChoicePending,false);
  assert.equal(page.data.consentNotice,notice+' 新版','告知必须刷新到最新配置');
  t.modals[0].success({});
  await choosing;
  assert.equal(page.data.consented,true);
  assert.equal(page.data.consentChoicePending,false);
});

test('a failed modal still syncs the real consent state',async()=>{
  const t=runtime({failChoiceWrite:true});
  const page=t.register('pages/my/my',{data:{consented:false,consentAvailable:true,consentChoicePending:true,consentNotice:notice}});
  t.setChoiceWriteFail(false);
  const choosing=t.adapter.showConsentChoices(page);await settle();
  t.modals[0].fail();
  await choosing;
  assert.equal(page.data.consented,true);
  assert.equal(page.data.consentChoicePending,false);
});

// T7.1：配置失效时也必须把页面同步到最新状态（不保留旧询问块）。
test('an unusable configuration syncs the page instead of leaving stale consent state',async()=>{
  const t=runtime({configuration:{enabled:true,privacy_notice:notice}});
  const page=t.register('pages/my/my',{data:{consented:false,consentAvailable:true,consentChoicePending:true,consentUnavailable:false,consentNotice:'old notice'}});
  const result=await t.adapter.showConsentChoices(page);
  assert.equal(result,false);
  assert.equal(page.data.consentAvailable,false,'配置不可用必须同步为 false');
  assert.equal(page.data.consentChoicePending,false,'不得继续显示询问块');
  assert.equal(page.data.consentUnavailable,true,'给出配置未就绪提示');
  assert.equal(page.data.consentNotice,notice,'告知用最新配置');
});

// T7.2：弹窗必须区分「已开启 / 用户关闭 / 未决」，不能把未决说成用户关闭。
test('the consent modal distinguishes an unresolved state from a user decline',async()=>{
  const t=runtime({failChoiceWrite:true});
  const page=t.register('pages/my/my',{data:{consented:false,consentAvailable:true,consentChoicePending:true,consentUnavailable:false,consentNotice:notice}});
  const choosing=t.adapter.showConsentChoices(page);await settle();
  assert.equal(t.modals.length,1);
  assert.match(t.modals[0].content,/当前状态：未设置/);
  assert.doesNotMatch(t.modals[0].content,/你已选择关闭/,'未决不得描述成用户主动关闭');
  t.modals[0].fail();
  await choosing;
});

// T7.1③：弹窗期间告知更新，用户点「关闭分析」仍必须生效。
test('declining still wins when the notice updated while the modal was open',async()=>{
  const t=runtime({configuration:{enabled:true,consent_version:'notice-v1',privacy_notice:notice}});
  const page=t.register('pages/my/my',{data:{consented:false,consentAvailable:true,consentChoicePending:true,consentUnavailable:false,consentNotice:notice}});
  const choosing=t.adapter.showConsentChoices(page);await settle();
  assert.equal(t.modals.length,1);
  t.updateConfig({enabled:true,consent_version:'notice-v2',privacy_notice:notice+' v2'});
  t.adapter.setConsent(true,'default');
  assert.equal(t.adapter.consented(),true);
  t.answer(0,false);
  await choosing;
  assert.equal(t.adapter.consented(),false,'过期 guard 不得吞掉关闭');
});

// T7.1⑤：迟到的旧 enabled 配置不得覆盖较新的 disabled 配置。
test('a stale enabled config cannot override a newer disabled config',async()=>{
  let calls=0,resolveOld;
  const t=runtime({configRequest:()=>{calls+=1;if(calls===1)return new Promise(resolve=>{resolveOld=resolve;});return Promise.resolve({enabled:false});}});
  if(!calls)t.adapter.refreshConfig();
  const newer=await t.adapter.refreshConfig();
  assert.equal(newer.enabled,false);
  assert.equal(t.adapter.consented(),false);
  if(resolveOld)resolveOld({enabled:true,consent_version:'notice-v1',privacy_notice:notice});
  await settle();await settle();
  assert.equal(t.adapter.consented(),false,'旧响应不得启用采集');
  assert.equal(t.entries.length,0);
});
