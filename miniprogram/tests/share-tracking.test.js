'use strict';
const test=require('node:test'); const assert=require('node:assert/strict');
const {createTracker,TTL}=require('../miniprogram/utils/tracking/core');
const {createVideoTracker}=require('../miniprogram/utils/tracking/video');
const routes=require('../miniprogram/utils/tracking/routes');
const {appendToken,splitPath,wrapPage}=require('../miniprogram/utils/tracking/index');
function setup(extra={}) {
 let time=1000,wall=1700000000000,seq=0,account='',session='',saved=[],requests=[];
 const io={wall:()=>wall,mono:()=>time,id:()=>`id_${String(++seq).padStart(12,'0')}`,random:()=>.5,account:()=>account,
 load:()=>saved,save:v=>{saved=v.slice();},entry:async body=>{requests.push(body);return {entry_token:'restricted_credential',session_id:body.previous_entry_token?session:(session='session_'+seq),share_id:body.share_token?'share_current':undefined,content_accessible:true};},send:async()=>{throw new Error('offline');},...extra};
 const sdk=createTracker(io);
 return {sdk,io,requests,advance:n=>{time+=n;wall+=n;},wall:n=>wall=n,account:v=>account=v,events:()=>sdk.queueForTest().map(r=>r.event),saved:()=>saved};
}
async function mount(t,key='a',share) {t.sdk.register(key,'pages/index/index',{},share);t.sdk.appShow();t.sdk.configure(true,'notice-v1');t.sdk.show(key);t.sdk.ready(key,true);await new Promise(resolve=>setImmediate(resolve));}
const types=(t,type)=>t.events().filter(e=>e.event_type===type);
test('default off and absent consent emit no records',async()=>{const t=setup();t.sdk.register('a','pages/index/index',{});t.sdk.appShow();t.sdk.show('a');t.sdk.ready('a',true);t.sdk.tick();assert.equal(t.events().length,0);t.sdk.configure(true,'');assert.equal(t.sdk.diagnostics().enabled,false);});
test('loaded and visible are both required for PV',async()=>{const t=setup();t.sdk.configure(true,'v');t.sdk.register('a','pages/index/index',{});t.sdk.appShow();t.sdk.show('a');await Promise.resolve();assert.equal(types(t,'page_view').length,0);t.sdk.ready('a',true);await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,1);});
test('duplicate show/ready produces one PV and one segment',async()=>{const t=setup();await mount(t);t.sdk.show('a');t.sdk.show('a');t.sdk.ready('a',true);t.advance(10000);t.sdk.tick();assert.equal(types(t,'page_view').length,1);assert.equal(types(t,'visible_segment').length,1);assert.equal(types(t,'visible_segment')[0].visible_delta_ms,10000);});
test('30 visible, 60 background, 20 visible totals 50 seconds',async()=>{const t=setup();await mount(t);for(let i=0;i<3;i++){t.advance(10000);t.sdk.tick();}t.sdk.appHide();t.sdk.hide('a',false);t.advance(60000);t.sdk.tick();t.sdk.appShow();t.sdk.show('a');for(let i=0;i<2;i++){t.advance(10000);t.sdk.tick();}assert.equal(types(t,'visible_segment').reduce((n,e)=>n+e.visible_delta_ms,0),50000);assert.equal(types(t,'page_view').length,1);});
test('only one page active and cross-page previous is explicit',async()=>{const t=setup();await mount(t);t.advance(1000);t.sdk.hide('a',false);t.sdk.register('b','pages/aicost/aicost',{});t.sdk.show('b');t.sdk.ready('b',true);await new Promise(r=>setImmediate(r));const pvs=types(t,'page_view');assert.equal(pvs.length,2);assert.equal(pvs[1].previous_page_view_id,pvs[0].page_view_id);t.advance(10000);t.sdk.tick();assert.equal(types(t,'visible_segment').reduce((n,e)=>n+e.visible_delta_ms,0),11000);t.sdk.hide('b',true);t.sdk.show('a');assert.equal(types(t,'page_view').length,3);});
test('unknown long clock gap never fabricates visible interval',async()=>{const t=setup();await mount(t);t.advance(90000);t.sdk.tick();assert.equal(types(t,'visible_segment')[0].visible_delta_ms,0);assert.deepEqual(types(t,'visible_segment')[0].quality_flags,['sampling_gap']);});
test('wall clock changes do not change monotonic duration',async()=>{const t=setup();await mount(t);t.wall(1700000001000);t.advance(1000);t.sdk.tick();assert.equal(types(t,'visible_segment')[0].visible_delta_ms,1000);});
test('blocking stops timing until explicitly resumed',async()=>{const t=setup();await mount(t);t.advance(1000);t.sdk.block('a',true);t.advance(10000);t.sdk.show('a');t.sdk.tick();t.sdk.block('a',false);t.advance(1000);t.sdk.tick();assert.equal(types(t,'visible_segment').reduce((n,e)=>n+e.visible_delta_ms,0),2000);});
test('failed content never produces PV',async()=>{const t=setup();await mount(t,'a');t.sdk.register('failed','pages/inspirationdetail/inspirationdetail',{});t.sdk.show('failed');t.sdk.ready('failed',false);await Promise.resolve();assert.equal(types(t,'page_view').length,1);});
test('entry failure is fail-open with zero telemetry',async()=>{const t=setup({entry:async()=>{throw Error('unavailable');}});await mount(t);assert.equal(types(t,'page_view').length,0);});
test('identity switch makes boundary and never reuses old credential',async()=>{let entry=0;const t=setup({entry:async()=>({entry_token:'credential_'+(++entry),session_id:'session_'+entry,content_accessible:true})});await mount(t);t.advance(1000);t.account('new_login');t.sdk.tick();await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,2);assert.notEqual(types(t,'page_view')[0].session_id,types(t,'page_view')[1].session_id);assert.equal(types(t,'page_view')[1].previous_page_view_id,undefined);const queue=t.sdk.queueForTest();assert.equal(queue[0].credential,'credential_1');assert.equal(queue[queue.length-1].credential,'credential_2');});
test('new share and later direct external entry clear stale attribution',async()=>{const t=setup();await mount(t,'a','opaque_share');assert.equal(types(t,'page_view')[0].share_id,'share_current');t.sdk.appHide();t.sdk.appShow({path:'pages/index/index',scene:1001,query:{}});await new Promise(r=>setImmediate(r));const pvs=types(t,'page_view');assert.equal(pvs.length,2);assert.equal(pvs[1].share_id,undefined);assert.equal(pvs[1].previous_page_view_id,undefined);});
test('retry event IDs remain stable and ack only deletes accepted records',async()=>{let calls=0,batches=[];const t=setup({send:async body=>{batches.push(body.events);if(++calls===1)throw Error('offline');return {results:[{event_id:body.events[0].event_id,status:'accepted'}]};}});await mount(t);const first=t.events()[0].event_id;t.advance(5000);await t.sdk.flush();assert.equal(batches[1][0].event_id,first);assert.equal(t.events().some(e=>e.event_id===first),false);});
test('queue is bounded to 500 with PV retained over low-priority records',async()=>{const t=setup();await mount(t);for(let i=0;i<550;i++)t.sdk.emit('a','scroll_depth',{scroll_depth:.2});assert.equal(t.events().length,500);assert.equal(types(t,'page_view').length,1);assert.equal(t.sdk.diagnostics().dropped.capacity,51);});
test('72 hour expiry has explicit diagnostics',async()=>{const t=setup();await mount(t);t.advance(TTL+1);await t.sdk.flush();assert.equal(t.events().length,0);assert.ok(t.sdk.diagnostics().dropped.expired>=1);});
test('revoking consent clears persisted pending analytics',async()=>{const t=setup();await mount(t);t.sdk.configure(true,'');assert.equal(t.events().length,0);assert.equal(t.saved().length,0);});
test('server rejected records do not retry indefinitely',async()=>{const t=setup({send:async body=>({results:body.events.map(e=>({event_id:e.event_id,status:'rejected',reason:'deleted'}))})});await mount(t);await t.sdk.flush();assert.equal(t.events().length,0);assert.ok(t.sdk.diagnostics().dropped.rejected>0);});
test('segment sequence is ordered and idempotency identifiers unique',async()=>{const t=setup();await mount(t);for(let i=0;i<3;i++){t.advance(10000);t.sdk.tick();}const e=types(t,'visible_segment');assert.deepEqual(e.map(x=>x.seq),[1,2,3]);assert.equal(new Set(e.map(x=>x.segment_id)).size,1);assert.equal(new Set(t.events().map(x=>x.event_id)).size,t.events().length);});
test('public readiness rejects error/default/loading-only states',()=>{assert.equal(routes.ready('pages/inspirationdetail/inspirationdetail',{loading:false,detail:null},true),false);assert.equal(routes.ready('pages/utilitytools/utilitytools',{loading:false,tools:[]},true),false);assert.equal(routes.ready('pages/inspirationdetail/inspirationdetail',{loading:false,detail:{id:1}},true),true);assert.equal(routes.ready('pages/login/login',{},true),false);assert.equal(routes.ready('pages/contentreader/contentreader',{contentReady:true,error:'revoked'},true),false);});
test('all 57 baseline routes and reader have explicit route policy',()=>{const app=require('../miniprogram/app.json');const all=app.pages.concat(...app.subPackages.map(p=>p.pages.map(x=>p.root+'/'+x)));assert.equal(all.length,58);all.forEach(route=>assert.ok(routes.ROUTES[route],route));});
test('original share query and image/title remain untouched by token utility',()=>{assert.equal(appendToken('/pages/x?prompt=a%26b&x=1','opaque'),'/pages/x?prompt=a%26b&x=1&st=opaque');assert.equal(appendToken('/pages/x?st=old&x=1','new'),'/pages/x?x=1&st=new');assert.deepEqual(splitPath('/pages/x?id=12&prompt=secret',''),{route:'pages/x',query:{id:'12',prompt:'secret'}});assert.equal(routes.binding('pages/aitooldetail/aitooldetail',{id:'12',prompt:'secret'}).id.startsWith('legacy:'),true);});
test('share callback is invoked once and unchanged when SDK disabled',()=>{let called=0;const value={title:'original',path:'/pages/index/index?business=1',imageUrl:'cover'};const page=wrapPage({onShareAppMessage(){called++;return value;}});assert.equal(page.onShareAppMessage(),value);assert.equal(called,1);});
function video(){let time=0,events=[];const v=createVideoTracker({mono:()=>time,emit:(type,event)=>events.push({type,...event})},'media','player');return{v,events,advance:n=>time+=n};}
test('video 2x playback separates wall time and media coverage',()=>{const t=video();t.v.visible(true);t.v.rate(2);t.v.state('playing');t.v.progress(0,100);for(let i=1;i<=10;i++){t.advance(1000);t.v.progress(i*2,100);}assert.equal(t.events.reduce((n,e)=>n+e.video_watch_ms,0),10000);assert.equal(t.v.snapshot().unique_played_ms,20000);});
test('video seek to end is not 90 percent coverage',()=>{const t=video();t.v.visible(true);t.v.state('playing');t.v.progress(0,100);t.advance(1000);t.v.progress(1,100);t.advance(1000);t.v.progress(100,100);t.v.state('ended');assert.equal(t.v.snapshot().unique_played_ms,1000);assert.ok(t.events.some(e=>(e.quality_flags||[]).includes('inferred_seek')));});
test('video pause/buffer/background never count watch time',()=>{const t=video();t.v.visible(true);t.v.state('playing');t.v.progress(0,100);t.advance(1000);t.v.progress(1,100);t.v.state('buffering');t.advance(1000);t.v.progress(1,100);t.v.state('paused');t.advance(1000);t.v.progress(1,100);t.v.visible(false);t.v.state('playing');t.advance(1000);t.v.progress(2,100);assert.equal(t.events.reduce((n,e)=>n+e.video_watch_ms,0),1000);});
test('video replay increases wall time but deduplicates covered intervals',()=>{const t=video();t.v.visible(true);t.v.state('playing');t.v.progress(0,100);t.advance(1000);t.v.progress(1,100);t.advance(1000);t.v.progress(0,100);t.advance(1000);t.v.progress(1,100);assert.equal(t.events.reduce((n,e)=>n+e.video_watch_ms,0),2000);assert.equal(t.v.snapshot().unique_played_ms,1000);});
test('async entry result after logout cannot revive old identity',async()=>{const pending=[];const t=setup({entry:()=>new Promise(resolve=>pending.push(resolve))});t.sdk.register('a','pages/index/index',{});t.sdk.appShow();t.sdk.configure(true,'v');t.sdk.show('a');t.sdk.ready('a',true);t.account('new_account');t.sdk.tick();assert.equal(pending.length,2,'the new identity need not wait for an obsolete entry');pending[0]({entry_token:'old',session_id:'old_session',content_accessible:true});await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,0);pending[1]({entry_token:'new',session_id:'new_session',content_accessible:true});await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,1);assert.equal(types(t,'page_view')[0].session_id,'new_session');});
test('repeat unchanged remote config does not manufacture a session',async()=>{const t=setup();await mount(t);t.sdk.configure(true,'notice-v1');await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,1);});
test('invalid storage payload cannot break page business',()=>{const t=setup({load:()=>({malformed:true})});assert.equal(t.sdk.diagnostics().queued,0);});
test('storage full records diagnostics instead of throwing',async()=>{const t=setup({save:()=>{throw Error('quota');}});await mount(t);assert.equal(types(t,'page_view').length,1);assert.ok(t.sdk.diagnostics().dropped.storage>0);});
test('two active media instances never share counters',()=>{const a=video(),b=video();a.v.visible(true);a.v.state('playing');a.v.progress(0,100);a.advance(1000);a.v.progress(1,100);assert.equal(a.v.snapshot().unique_played_ms,1000);assert.equal(b.v.snapshot().unique_played_ms,0);});
test('all event payloads exclude direct login and tenant identifiers',async()=>{const t=setup();t.account('secret_login_token');await mount(t);for(const event of t.events()){assert.equal('user_id' in event,false);assert.equal('tenant_id' in event,false);assert.equal('owner_id' in event,false);assert.equal(JSON.stringify(event).includes('secret_login_token'),false);}});
test('public media proxy paths normalize only against approved API origin',()=>{const {mediaURL}=require('../miniprogram/utils/tracking/api');assert.equal(mediaURL('/api/v1/miniprogram/tracking/media/asset'),'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/media/asset');assert.equal(mediaURL('//evil.invalid/asset'),'');assert.equal(mediaURL('javascript:alert(1)'),'');});
test('private source can record intent without manufacturing a page view',()=>{const t=setup();t.sdk.configure(true,'notice-v1');t.sdk.shareIntent({session_id:'session_share',entry_token:'entry_share'},'pages/index/index',{},'share_new','app_message');assert.equal(types(t,'share_intent').length,1);assert.equal(types(t,'page_view').length,0);assert.equal(types(t,'share_intent')[0].page_view_id,undefined);});
test('only anonymous-to-login sends current-session linkage, logout does not',async()=>{const t=setup();await mount(t);t.account('verified_login');t.sdk.tick();await new Promise(r=>setImmediate(r));assert.equal(t.requests[t.requests.length-1].previous_entry_token,'restricted_credential');t.account('');t.sdk.tick();await new Promise(r=>setImmediate(r));assert.equal(t.requests[t.requests.length-1].previous_entry_token,undefined);});
test('video state before duration metadata does not submit invalid fabricated duration',()=>{const t=video();t.v.visible(true);t.v.state('playing');assert.equal(t.events.length,0);t.v.progress(0,100);assert.equal(t.events.length,1);assert.equal(t.events[0].video_duration_ms,100000);});
test('page adapter wraps lifecycle without changing original receiver, args or return',()=>{let seen;const result={business:true};const wrapped=wrapPage({onLoad(options){seen={receiver:this,options};return result;}});const page={route:'pages/login/login',data:{}};const options={business:'unchanged'};assert.equal(wrapped.onLoad.call(page,options),result);assert.equal(seen.receiver,page);assert.equal(seen.options,options);});
test('every page exposes share handlers: pages without one get the product default (all-page Moments entry)',()=>{const wrapped=wrapPage({onLoad(){}});assert.equal('onShareAppMessage' in wrapped,true);assert.equal('onShareTimeline' in wrapped,true);const timeline=wrapped.onShareTimeline.call({route:'pages/index/index'});assert.equal(timeline.title,'甲第灵光 · AI建房助手');const app=wrapped.onShareAppMessage.call({route:'pages/my/my'});assert.equal(app.path,'/pages/my/my');});test('pages with custom share handlers keep their own implementation',()=>{const custom={title:'custom',query:'a=1'};const wrapped=wrapPage({onLoad(){},onShareTimeline(){return custom;}});assert.equal(wrapped.onShareTimeline(),custom);});
test('acknowledged batches drain backlog beyond 50 without waiting another heartbeat',async()=>{let online=false,sent=[];const t=setup({send:async body=>{if(!online)throw Error('offline');sent.push(body.events);return{results:body.events.map(e=>({event_id:e.event_id,status:'accepted'}))};}});await mount(t);for(let i=0;i<120;i++)t.sdk.emit('a','scroll_depth',{scroll_depth:.1});online=true;t.advance(5000);await t.sdk.flush();assert.equal(t.events().length,0);assert.equal(sent.length,3);assert.ok(sent.every(batch=>batch.length<=50));});
test('reader scene resolver accepts only a bound opaque QR token',async()=>{const api=require('../miniprogram/utils/tracking/api');let requested,body,method;global.wx={request(options){requested=options.url;body=options.data;method=options.method;options.success({statusCode:200,data:{code:0,data:{route_key:'pages/contentreader/contentreader',content_id:'content_valid',content_version:2,share_token:'share_opaque'}}});}};const result=await api.resolveReaderOptions({scene:'A'.repeat(32)});assert.equal(requested.endsWith('/share-entry'),true);assert.equal(body.scene,'A'.repeat(32));assert.equal(method,'POST');assert.equal(result.content_version,2);assert.equal(result.st,'share_opaque');await assert.rejects(()=>api.resolveReaderOptions({scene:'https://untrusted.invalid'}));delete global.wx;});
test('content reader never stores share QR as a legacy invitation code',()=>{const fs=require('node:fs');for(const ext of ['js','ts']){const source=fs.readFileSync(require('node:path').join(__dirname,'../miniprogram/app.'+ext),'utf8');assert.match(source,/options\?\.path !== 'pages\/contentreader\/contentreader' && scene/);}});
test('WeChat adapter preserves original async share data and emits intent, not another PV',async()=>{
 const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');let appDefinition,registered,clock=0,callbackCalls=0;const entries=[],events=[],storage={'share_tracking_consent_v1':{version:'v'},'share_tracking_instance_v1':'random_installation_opaque'};
 const fakeAPI={request:async(suffix,method,body)=>{if(suffix==='/config')return{enabled:true,consent_version:'v',privacy_notice:'purpose and categories'};if(suffix==='/entry'){entries.push(body);return{entry_token:'entry_opaque_token',session_id:'session_valid_id',content_accessible:true};}if(suffix==='/shares')return{share_id:'share_valid_id',share_token:'opaque_share_token'};if(suffix==='/events/batch'){events.push(...body.events);return{results:body.events.map(e=>({event_id:e.event_id,status:'accepted'}))};}throw Error('unknown');}};
 const sandbox={module:{exports:{}},require:name=>name==='./api'?fakeAPI:require('../miniprogram/utils/tracking/'+name.slice(2)),wx:{getPerformance:()=>({now:()=>clock}),getStorageSync:key=>storage[key],setStorageSync:(key,v)=>storage[key]=v,removeStorageSync:key=>delete storage[key],onNetworkStatusChange(){}},Page:d=>{registered=d;},App:d=>{appDefinition=d;},setInterval:()=>1,setTimeout,clearTimeout,Date,Math,Promise,Uint8Array};
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../miniprogram/utils/tracking/index.js'),'utf8'),sandbox);const adapter=sandbox.module.exports;adapter.install();sandbox.App({});appDefinition.onLaunch({});appDefinition.onShow({});await new Promise(r=>setImmediate(r));
 sandbox.Page({onShareAppMessage(){callbackCalls++;return{title:'base title',imageUrl:'base image',path:'/pages/aicost/aicost?business=a%26b',promise:Promise.resolve({title:'prepared title',imageUrl:'prepared image'})};}});
 const page={route:'pages/index/index',data:{},setData(values,done){Object.assign(this.data,values);if(done)done();}};registered.onLoad.call(page,{});registered.onShow.call(page);registered.onReady.call(page);await new Promise(r=>setImmediate(r));const share=registered.onShareAppMessage.call(page);const final=await share.promise;
 assert.equal(callbackCalls,1);assert.equal(final.title,'prepared title');assert.equal(final.imageUrl,'prepared image');assert.equal(final.path,'/pages/aicost/aicost?business=a%26b&st=opaque_share_token');clock=1000;await adapter.getSDK().flush();assert.equal(events.filter(e=>e.event_type==='page_view').length,1);assert.equal(events.filter(e=>e.event_type==='share_intent').length,1);assert.equal(events.find(e=>e.event_type==='share_intent').route_key,'pages/index/index');assert.equal(entries[0].client_instance,'random_installation_opaque');
});
test('WeChat repeated launch query on background recovery does not invent a new PV',async()=>{const t=setup();const launch={path:'pages/index/index',scene:1007,query:{st:'opaque_share'}};t.sdk.appShow(launch);await mount(t,'a','opaque_share');t.sdk.appHide();t.sdk.hide('a',false);t.advance(60000);t.sdk.appShow(launch);t.sdk.show('a');await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,1);});
test('logged-in API reuses existing session signing without collecting a new fingerprint',async()=>{const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');let signed,request;const sandbox={module:{exports:{}},require:name=>{assert.equal(name,'../parameter');return{generateRequestParams:(...args)=>{signed=args;return{token:'existing-token',pass:'existing-pass'};},paramsToHeaders:v=>v};},wx:{getStorageSync:key=>({token:'existing-token',device_fingerprint:'cached-login-device'})[key],request:options=>{request=options;options.success({statusCode:200,data:{code:0,data:{ok:true}}});}},Promise,Error};vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../miniprogram/utils/tracking/api.js'),'utf8'),sandbox);await sandbox.module.exports.request('/entry','POST',{route_key:'pages/index/index'},undefined,true);assert.equal(signed[3],'cached-login-device');assert.equal('device_fingerprint' in request.data,false);assert.equal(request.header.token,'existing-token');});

// Run the actual page callbacks: the flags must be set by real success branches,
// not by loading=false, placeholder objects, or a synthetic tracker-only fixture.
function publicPageRuntime(name) {
 const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');let definition;const requests=[];
 const deps=new Proxy({}, {get:(_,key)=>key==='resolveAssetPath'||key==='normalizeCosUrl'?v=>v:()=>{}});
 const sandbox={exports:{},require:()=>deps,Page:value=>{definition=value;},wx:{request:options=>requests.push(options),getStorageSync:()=>'',showToast(){},stopPullDownRefresh(){}},console:{error(){}},setTimeout:()=>0,clearTimeout(){},Set,Promise,Date,Math};
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,`../miniprogram/pages/${name}/${name}.js`),'utf8'),sandbox);
 const page={...definition,data:JSON.parse(JSON.stringify(definition.data)),route:`pages/${name}/${name}`,setData(values){Object.assign(this.data,values);}};
 return {page,requests,respond(data,statusCode=200,code=0){const request=requests.shift();assert.ok(request,'a real request was issued');request.success({statusCode,data:{code,data}});},fail(){requests.shift().fail(new Error('offline'));}};
}
for(const [name,method] of [['utilitytools','loadTools'],['distribution','loadDesigners'],['template','loadTemplates']]) {
 test(`${name} actual list callbacks only mark successful content (including empty) ready`,async()=>{
  const t=publicPageRuntime(name),p=t.page;
  if(name==='template'){
   p.getCurrentMainTab=()=>({value:'scene',label:'场景'});p.getCurrentSubTab=()=>undefined;p.getCurrentThirdTab=()=>undefined;
   p.syncWaterfallCards=cards=>p.setData({displayCards:cards});p.warmLeadingCards=()=>{};
  }
  const load=()=>p[method](true);
  assert.equal(routes.ready(p.route,p.data,true),false);
  let pending=load();assert.equal(routes.ready(p.route,p.data,true),false);t.respond({list:[],total:0});await pending;
  assert.equal(routes.ready(p.route,p.data,true),true,'valid empty response is a loaded page');
  assert.equal(routes.ready(p.route,p.data,false),false,'render completion is still required');
  pending=load();assert.equal(p.data.contentReady,false);t.fail();await pending;
  assert.equal(routes.ready(p.route,p.data,true),false,'failed refresh cannot reuse prior readiness');
  pending=load();t.respond({list:[]},500);await pending;assert.equal(routes.ready(p.route,p.data,true),false);
  pending=load();t.respond({list:[]},200,500);await pending;assert.equal(routes.ready(p.route,p.data,true),false);
  pending=load();t.respond({});await pending;assert.equal(routes.ready(p.route,p.data,true),false,'malformed success body is not empty content');
 });
}
test('template successful first-page cache is ready and uses real displayCards field',async()=>{
 const t=publicPageRuntime('template'),p=t.page;p.getCurrentMainTab=()=>({value:'scene',label:'场景'});p.getCurrentSubTab=()=>undefined;p.getCurrentThirdTab=()=>undefined;
 p.syncWaterfallCards=cards=>p.setData({displayCards:cards});p.warmLeadingCards=()=>{};
 p.templateFirstPageCache['scene::::::']={cards:[{id:17}],nextPage:2,hasMore:false};await p.loadTemplates(true);
 assert.equal(t.requests.length,0);assert.equal(p.data.displayCards[0].id,17);assert.equal(routes.ready(p.route,p.data,true),true);
});
test('designer public homepage needs a successful real profile; self-management and failed loads stay private',async()=>{
 const t=publicPageRuntime('designerhome'),p=t.page;p.data.userId=12;
 assert.equal(routes.ready(p.route,p.data,true),false,'initial placeholder profile is not content');
 let pending=p.loadDesignerHomepage();t.fail();await pending;assert.equal(routes.ready(p.route,p.data,true),false);
 pending=p.loadDesignerHomepage();t.respond({});await pending;assert.equal(routes.ready(p.route,p.data,true),false);
 pending=p.loadDesignerHomepage();t.respond({profile:{name:'公开设计师'},works:[],reviews:[]});await pending;assert.equal(routes.ready(p.route,p.data,true),true);
 assert.equal(routes.ready(p.route,p.data,false),false);p.data.isSelfHomepage=true;assert.equal(routes.ready(p.route,p.data,true),false);
 p.data.isSelfHomepage=false;pending=p.loadDesignerHomepage();t.respond({},403,403);await pending;assert.equal(routes.ready(p.route,p.data,true),false);
 assert.deepEqual(routes.binding(p.route,{userId:'12'}),routes.binding(p.route,{id:'12'}));
 const share=p.onShareAppMessage();assert.equal(share.path,'/pages/designerhome/designerhome?userId=12');assert.equal(share.title,'公开设计师的主页');
});
test('every excluded route has a specific business privacy reason, not an unverified catch-all',()=>{
 for(const [route,policy] of Object.entries(routes.ROUTES))if(!policy.collect){assert.ok(policy.exclusionReason,route);assert.equal(routes.ready(route,{contentReady:true},true),false);assert.doesNotMatch(policy.readiness,/unverified/);}
});
test('real public list/profile failures never create PV; successful visible retry creates one',async()=>{
 for(const [name,method] of [['utilitytools','loadTools'],['distribution','loadDesigners'],['template','loadTemplates'],['designerhome','loadDesignerHomepage']]){
  const pageRuntime=publicPageRuntime(name),p=pageRuntime.page,t=setup();
  if(name==='template'){p.getCurrentMainTab=()=>({value:'scene',label:'场景'});p.getCurrentSubTab=()=>undefined;p.getCurrentThirdTab=()=>undefined;p.syncWaterfallCards=cards=>p.setData({displayCards:cards});p.warmLeadingCards=()=>{};}
  if(name==='designerhome')p.data.userId=12;
  t.sdk.configure(true,'notice-v1');t.sdk.appShow();t.sdk.register('public',p.route,routes.binding(p.route,{userId:12}));t.sdk.show('public');
  let pending=p[method](true);pageRuntime.fail();await pending;t.sdk.ready('public',routes.ready(p.route,p.data,true));await new Promise(r=>setImmediate(r));
  assert.equal(types(t,'page_view').length,0,name+' failed load');
  pending=p[method](true);pageRuntime.respond(name==='designerhome'?{profile:{name:'公开设计师'},works:[],reviews:[]}:{list:[],total:0});await pending;
  t.sdk.ready('public',routes.ready(p.route,p.data,false));await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,0,name+' not rendered');
  t.sdk.ready('public',routes.ready(p.route,p.data,true));await new Promise(r=>setImmediate(r));assert.equal(types(t,'page_view').length,1,name+' visible successful retry');
 }
});
test('public readiness hooks have matching TypeScript and JavaScript transitions',()=>{
 const fs=require('node:fs'),path=require('node:path');
 for(const name of ['template','distribution','utilitytools','designerhome']){
  const signals=['ts','js'].map(ext=>fs.readFileSync(path.join(__dirname,`../miniprogram/pages/${name}/${name}.${ext}`),'utf8').split(/\r?\n/).filter(line=>line.includes('contentReady')).map(line=>line.trim()));
  assert.deepEqual(signals[0],signals[1],name);
 }
});

test('reader uses rich-text only for explicit server format and retains escaped legacy text',()=>{
 const fs=require('node:fs'),path=require('node:path');
 const template=fs.readFileSync(path.join(__dirname,'../miniprogram/pages/contentreader/contentreader.wxml'),'utf8');
 assert.match(template,/<rich-text wx:if="\{\{content\.body_format === 'rich_text'\}\}"[^>]+nodes="\{\{content\.body\}\}"/);
 assert.match(template,/<text wx:else class="body"[^>]*>\{\{content\.body\}\}<\/text>/);
});

test('source snapshot version is propagated to observed legacy events',async()=>{
 const t=setup({entry:async()=>({entry_token:'legacy_entry_token',session_id:'legacy_session',content_version:4503599627370495,content_accessible:true})});
 t.sdk.register('legacy','pages/templatesquaredetails/templatesquaredetails',{id:'legacy:example:12'});t.sdk.appShow();t.sdk.configure(true,'v');t.sdk.show('legacy');t.sdk.ready('legacy',true);await new Promise(r=>setImmediate(r));
 assert.equal(types(t,'page_view')[0].content_version,4503599627370495);
});
test('reader scroll reports percent on the server 0-100 scale',()=>{
 const fs=require('node:fs'),vm=require('node:vm');let page,value;
 const sandbox={Page:x=>page=x,require:name=>name.endsWith('/index')?{emit:(_p,_t,data)=>value=data.scroll_depth}:name.endsWith('/video')?{}:{},wx:{getWindowInfo:()=>({windowHeight:200}),createSelectorQuery:()=>({in(){return this},select(){return this},boundingClientRect(callback){callback({height:1000});return this},exec(){}})}};
 vm.runInNewContext(fs.readFileSync(require('node:path').join(__dirname,'../miniprogram/pages/contentreader/contentreader.js'),'utf8'),sandbox);
 page.onPageScroll.call({},{scrollTop:300});assert.equal(value,50);
});
test('recreated reader video players use distinct observation instances',()=>{
 const fs=require('node:fs'),vm=require('node:vm');let page;const instances=[];
 const sandbox={Page:x=>page=x,require:name=>name.endsWith('/video')?{createVideoTracker:(_io,_media,instance)=>{instances.push(instance);return{visible(){}};}}:{},wx:{}};
 vm.runInNewContext(fs.readFileSync(require('node:path').join(__dirname,'../miniprogram/pages/contentreader/contentreader.js'),'utf8'),sandbox);
 const state={observers:[],data:{media:[{kind:'video',media_id:'asset_video',domId:'media-0'}]}};
 page.observePlayers.call(state);page.observePlayers.call(state);assert.notEqual(instances[0],instances[1]);
});
