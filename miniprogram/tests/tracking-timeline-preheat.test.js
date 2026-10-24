'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const ts=require('./report-runtime')('typescript');
const root=path.join(__dirname,'../miniprogram');
const settle=()=>new Promise(resolve=>setImmediate(resolve));
const notice='Optional public visit analytics, account nickname and avatar for authorized operators.';
function runtime({accepted=true,declined=false,enabled=true,account='',deferShares=false,failShares=false,attributionStatus,shareLifetime=60000}={}) {
  let definition,app,clock=1000,wall=1700000000000,shareSequence=0,entrySequence=0;
  const storage={token:account,share_tracking_instance_v1:'random-installation-only'};
  if(declined)storage.share_tracking_consent_choice_v1={accepted:false,explicit:true,version:'v1',notice};
  else if(accepted)storage.share_tracking_consent_choice_v1={accepted:true,explicit:true,version:'v1',notice};
  const entries=[],shares=[],events=[],pending=[],menus=[],timers=new Map();
  let nextTimer=0;
  const schedule=(callback,delay)=>{const timer=++nextTimer;timers.set(timer,{callback,at:wall+delay});return timer;};
  const cancel=timer=>timers.delete(timer);
  const config={enabled,consent_version:'v1',privacy_notice:notice};
  const api={async request(suffix,method,body){
    if(suffix==='/config')return config;
    if(suffix==='/entry'){
      entries.push(body);entrySequence++;
      return{entry_token:'entry_'+entrySequence,session_id:'session_'+entrySequence,content_version:body.content_version||1,content_accessible:true,attribution_status:attributionStatus,share_id:body.share_token?'incoming-share-id':undefined};
    }
    if(suffix==='/shares'){
      shares.push(body);const n=++shareSequence;
      if(failShares)throw Object.assign(Error('fixture share failure'),{statusCode:503,code:503});
      const value={share_id:'share_'+n,share_token:'token_'+n,expires_at:new Date(wall+shareLifetime).toISOString()};
      if(deferShares)return new Promise(resolve=>pending.push(()=>resolve(value)));
      return value;
    }
    if(suffix==='/events/batch'){events.push(...body.events);return{results:body.events.map(e=>({event_id:e.event_id,status:'accepted'}))};}
    throw Error('unexpected tracking endpoint '+suffix);
  }};
  class TestDate extends Date{constructor(...args){super(...(args.length?args:[wall]));}static now(){return wall;}}
  const wx={getPerformance:()=>({now:()=>clock}),getStorageSync:k=>storage[k],setStorageSync:(k,v)=>storage[k]=v,removeStorageSync:k=>delete storage[k],
    showShareMenu:o=>menus.push({show:[...o.menus]}),hideShareMenu:o=>menus.push({hide:[...o.menus]}),getLaunchOptionsSync:()=>({scene:1001}),
    onNetworkStatusChange(){},showToast(){},setBackgroundColor(){},getMenuButtonBoundingClientRect:()=>({top:24,height:32}),
    getSystemInfoSync:()=>({statusBarHeight:20,windowWidth:375}),request:o=>o.success({statusCode:200,data:{code:0,data:{list:[]}}}),env:{USER_DATA_PATH:'wxfile://usr'}};
  const sandbox={module:{exports:{}},require:n=>n==='./api'?api:require(path.join(root,'utils/tracking',n.slice(2))),wx,
    Page:d=>definition=d,App:d=>app=d,setInterval:()=>1,setTimeout:schedule,clearTimeout:cancel,Date:TestDate,Math,Promise,Uint8Array,console};
  vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/index.js'),'utf8'),sandbox);
  const adapter=sandbox.module.exports;adapter.install();sandbox.App({});app.onLaunch({});app.onShow({});
  const detail={id:21,name:'公开模板',description:'模板描述',preview_url:'https://static.example/first.jpg',images:['https://static.example/first.jpg','https://static.example/second.jpg'],is_free:true};
  function actual(name,ext='js'){
    const deps=new Proxy({}, {get:(_,key)=>{
      if(key==='resolveAssetPath'||key==='normalizeCosUrl')return x=>x;
      if(key==='prepareCloudImage')return async(_url,file)=>'wxfile://usr/'+file;
      if(key==='prepareShareCardImage')return async x=>'friend:'+x;
      if(key==='prepareTimelineShareImage')return async x=>'timeline:'+x;
      if(key==='getPageCache')return()=>detail;
      if(key==='getCachedDeviceFingerprint')return()=>'';
      if(key==='reportRequest')return async suffix=>suffix==='/config'?{enabled:true,house_enabled:true}:null;
      return()=>{};
    }});
    let code=fs.readFileSync(path.join(root,'pages',name+'.'+ext),'utf8');
    if(ext==='ts')code=ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
    vm.runInNewContext(code,{exports:{},Page:sandbox.Page,require:n=>n.endsWith('costRegions')?require(path.join(root,'utils/costRegions.js')):deps,wx,
      getApp:()=>({globalData:{}}),console:{log(){},warn(){},error(){}},setTimeout,clearTimeout,Promise,Date,Math,Set});
    return make('pages/'+name,definition);
  }
  function make(route,wrapped){return{...wrapped,route,data:JSON.parse(JSON.stringify(wrapped.data||{})),setData(v,done){Object.assign(this.data,v);if(done)done.call(this);},selectComponent:()=>({checkAndShow:async()=>false})};}
  function custom(route='pages/index/index'){
    sandbox.Page({data:{contentReady:true,title:'Public page',cover:'local:first',query:'from=share'},
      getTimelineSharePayload(){return{title:this.data.title,imageUrl:this.data.cover,query:this.data.query};},
      onShareTimeline(){this.gestures=(this.gestures||0)+1;return this.getTimelineSharePayload();}});
    return make(route,definition);
  }
  return{adapter,storage,entries,shares,events,pending,menus,actual,custom,
    async mount(page,options={}){page.onLoad(options);page.onShow();page.onReady();for(let i=0;i<10;i++)await settle();},
    async ready(page){for(let i=0;i<10;i++){await settle();if(adapter.getSDK().get(page)?.timelineStatus==='ready')return;}},
    advance(ms){clock+=ms;wall+=ms;},advanceWall(ms){wall+=ms;},timerDelays:()=>[...timers.values()].map(t=>t.at-wall),async tick(){adapter.getSDK().tick();await settle();},
    async runTimers(){for(const [key,value]of [...timers])if(value.at<=wall){timers.delete(key);value.callback();}for(let i=0;i<10;i++)await settle();},
    flush:()=>adapter.getSDK().flush(),resolve(index=0){assert.ok(pending[index]);pending[index]();},
    close(page){page.onUnload();},config,
  };
}
for(const ext of ['ts','js'])for(const [name,options]of[
  ['index/index',{}],['aicost/aicost',{}],['templatesquaredetails/templatesquaredetails',{id:'21'}],['report/landing',{}]
])test(`${ext} actual ${name}: first native timeline callback synchronously returns prepared token`,async()=>{
  const t=runtime(),page=t.actual(name,ext);await t.mount(page,{...options,st:'incoming-token'});await t.ready(page);
  const state=t.adapter.getSDK().get(page);
  assert.equal(state.timelineStatus,'ready',JSON.stringify(t.adapter.shareDiagnostics(page)));
  assert.equal(t.events.filter(e=>e.event_type==='share_intent').length,0,'preparation is not a gesture');
  const share=page.onShareTimeline();assert.equal(typeof share.then,'undefined');assert.match(share.query,/(?:^|&)st=token_\d+/);
  assert.ok(t.shares.every(s=>s.prepared===true));assert.equal(t.shares.at(-1).parent_share_id,'incoming-share-id');
  assert.equal(share.title,page.getTimelineSharePayload().title);assert.equal(share.imageUrl,page.getTimelineSharePayload().imageUrl);
  await t.flush();assert.equal(t.events.filter(e=>e.event_type==='share_intent').length,1);
  const repeated=page.onShareTimeline();assert.equal(repeated.query,share.query);await t.flush();
  assert.equal(t.events.filter(e=>e.event_type==='share_intent').length,2,'each actual callback is an intent, not delivery');
  t.close(page);
});
test('one pending request prepares the same payload; payload and metadata use the same key',async()=>{
  const t=runtime({deferShares:true}),page=t.custom();await t.mount(page);
  const payload=page.getTimelineSharePayload(),a=t.adapter.preheatTimelineShare(page,payload),b=t.adapter.preheatTimelineShare(page,payload);
  assert.equal(a,b);assert.equal(t.shares.length,1);assert.equal(page.gestures,undefined);
  assert.ok(t.menus.some(m=>m.hide?.includes('shareTimeline')),'native menu waits for synchronous payload');
  t.resolve();await a;await t.ready(page);
  const state=t.adapter.getSDK().get(page),key=JSON.stringify(payload);
  assert.ok(state.shareCache[key]);assert.ok(state.shareIds[key]);assert.match(page.onShareTimeline().query,/st=token_1/);
  t.close(page);
});
test('content, expiry, account and consent boundaries never return a stale token',async()=>{
  const t=runtime(),page=t.custom();await t.mount(page);
  const first=page.onShareTimeline().query;
  page.setData({cover:'local:second',query:'image_ref=second'});await t.ready(page);
  const second=page.onShareTimeline();assert.notEqual(second.query,first);assert.equal(second.imageUrl,'local:second');
  t.advance(60001);page.onHide();page.onShow();await t.ready(page);
  assert.notEqual(page.onShareTimeline().query,second.query,'expired token is replaced');
  t.storage.token='different-account';await t.tick();await t.ready(page);
  const accountQuery=page.onShareTimeline().query;assert.notEqual(accountQuery,second.query);
  t.adapter.setConsent(false);assert.doesNotMatch(page.onShareTimeline().query,/(?:^|&)st=/);
  t.adapter.setConsent(true);await t.ready(page);assert.notEqual(page.onShareTimeline().query,accountQuery);
  t.close(page);
});
test('late preparation after hide/reopen cannot replace the newer request metadata',async()=>{
  const t=runtime({deferShares:true}),page=t.custom();await t.mount(page);
  page.onHide();page.onShow();await settle();assert.equal(t.shares.length,2);
  t.resolve(1);await t.ready(page);const newest=page.onShareTimeline().query;
  t.resolve(0);await settle();assert.equal(page.onShareTimeline().query,newest);
  const state=t.adapter.getSDK().get(page),key=JSON.stringify(page.getTimelineSharePayload());assert.equal(state.shareIds[key].id,'share_2');
  t.close(page);
});
test('disabled or declined analytics keeps normal business sharing and makes no tracking calls',async()=>{
  for(const options of [{enabled:false},{declined:true}]){
    const t=runtime(options),page=t.custom();await t.mount(page);
    assert.equal(page.onShareTimeline().query,'from=share');assert.equal(t.entries.length,0);assert.equal(t.shares.length,0);
    assert.ok(t.menus.some(m=>m.show?.includes('shareTimeline')));t.close(page);
  }
});
test('share API failure restores ordinary sharing with a bounded diagnostic',async()=>{
  const t=runtime({failShares:true}),page=t.custom();await t.mount(page);
  assert.equal(page.onShareTimeline().query,'from=share');
  const failure=t.adapter.shareDiagnostics(page);assert.equal(failure.stage,'share_prepare');assert.equal(failure.statusCode,503);
  assert.doesNotMatch(JSON.stringify(failure),/entry_|token_|random-installation/);
  assert.ok(t.menus.some(m=>m.show?.includes('shareTimeline')));t.close(page);
});

test('open page refreshes an expiring token before the next native share gesture',async()=>{
  const t=runtime(),page=t.custom();await t.mount(page);const first=page.onShareTimeline().query;
  t.advance(59000);await t.runTimers();await t.ready(page);
  assert.equal(t.shares.length,2);assert.notEqual(page.onShareTimeline().query,first);
  t.close(page);t.advance(60000);await t.runTimers();assert.equal(t.shares.length,2,'unload cancels renewal');
});
test('slow preparation restores the native timeline menu and rejects a late token',async()=>{
  const t=runtime({deferShares:true}),page=t.custom();await t.mount(page);
  t.advance(1801);await t.runTimers();
  assert.equal(t.adapter.getSDK().get(page).timelineStatus,'fallback');
  assert.equal(t.adapter.shareDiagnostics(page).code,'prepare_timeout');
  assert.ok(t.menus.at(-1).show?.includes('shareTimeline'));
  t.resolve();await settle();assert.equal(t.adapter.getSDK().get(page).shareIds[JSON.stringify(page.getTimelineSharePayload())],undefined);
  t.close(page);
});
test('empty pure getter keeps unready content out of share preparation',async()=>{
  const t=runtime();
  const original=t.adapter.wrapPage({data:{contentReady:true},getTimelineSharePayload(){return{};},onShareTimeline(){return{};}});
  const empty={...original,route:'pages/index/index',data:{contentReady:true},setData(v,done){Object.assign(this.data,v);if(done)done.call(this);}};
  await t.mount(empty);assert.equal(t.shares.length,0);assert.deepEqual(Object.keys(empty.onShareTimeline()),[]);t.close(empty);
});

test('identity changes between heartbeats cannot prepare with the previous account credential',async()=>{
  const t=runtime(),page=t.custom();await t.mount(page);const initialShares=t.shares.length;
  t.storage.token='new-account';assert.equal(page.onShareTimeline().query,'from=share');
  assert.equal(t.shares.length,initialShares,'old context is invalidated before any share request');
  await t.tick();await t.ready(page);assert.equal(t.shares.length,initialShares+1);
  assert.match(page.onShareTimeline().query,/st=token_2/);t.close(page);
});
test('first consent configuration waits for public content readiness before entry',async()=>{
  const t=runtime();
  const wrapped=t.adapter.wrapPage({data:{contentReady:false},getTimelineSharePayload(){return{};}});
  const page={...wrapped,route:'pages/aicostshare/aicostshare',data:{contentReady:false},setData(v,done){Object.assign(this.data,v);if(done)done.call(this);}};
  await t.mount(page,{report_id:'a'.repeat(32)});assert.equal(t.entries.length,0);assert.equal(t.shares.length,0);t.close(page);
});

test('content and version rebinding replaces payload and entry metadata together',async()=>{
  const t=runtime(),page=t.custom('pages/contentreader/contentreader');page.data.query='content_id=first&content_version=1';
  await t.mount(page,{content_id:'first',content_version:1});const first=page.onShareTimeline().query;
  page.data.query='content_id=second&content_version=2';t.adapter.resolvePageOptions(page,{content_id:'second',content_version:2},false);await t.ready(page);
  const current=page.onShareTimeline().query;assert.notEqual(current,first);assert.match(current,/content_id=second&content_version=2&st=/);
  assert.equal(t.shares.at(-1).content_id,'second');assert.equal(t.shares.at(-1).content_version,2);t.close(page);
});
test('unrecognized entry attribution remains safely diagnosable without exposing credentials',async()=>{
  const t=runtime({attributionStatus:'expired_share'}),page=t.custom();await t.mount(page,{st:'secret-expired-token'});
  const diagnostic=t.adapter.shareDiagnostics(page);assert.equal(diagnostic.attribution_status,'expired_share');
  assert.doesNotMatch(JSON.stringify(diagnostic),/secret|entry_|token_|random-installation/);t.close(page);
});

test('production 30-day token expiry uses bounded checks without minting on each check',async()=>{
  const day=24*60*60*1000,t=runtime({shareLifetime:30*day}),page=t.custom();await t.mount(page);
  assert.equal(t.shares.length,1);assert.ok(t.timerDelays().every(delay=>delay>0&&delay<=day));
  t.advanceWall(day);await t.runTimers();assert.equal(t.shares.length,1,'daily check reuses valid 30-day token');
  assert.ok(t.timerDelays().every(delay=>delay>0&&delay<=day));
  t.advanceWall(29*day-1000);await t.runTimers();await t.ready(page);assert.equal(t.shares.length,2,'renew once at expiry margin');
  assert.match(page.onShareTimeline().query,/st=token_2/);t.close(page);
});
