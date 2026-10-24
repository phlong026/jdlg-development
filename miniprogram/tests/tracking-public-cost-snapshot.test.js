'use strict';
const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const ts = require('./report-runtime')('typescript');
const root = path.join(__dirname, '../miniprogram');
const route = 'pages/aicostshare/aicostshare';
const id = 'AbcdEFghIJklMNopQRstUVwxYZ012345';
const notice = 'Optional public visit analytics and account nickname for authorized operators.';
const result = {version:4, estimatedTotalCost:'58.36万元', estimatedUnitCost:'1823.75元/㎡', totalLabel:'参考总造价', unitLabel:'参考单价',
  basicInfo: Array.from({length:14}, (_,i)=>({label:i===0?'所在地区':i===1?'总建筑面积':'公开项目'+i,value:i===0?'福建 / 泉州':i===1?'320㎡':'普通配置'})),
  costBreakdown:Array.from({length:6},(_,i)=>({name:'费用'+i,amount:100,amountText:'0.01万元',percent:10,note:'公开参考',estimable:true})),dynamicNotes:[]};
const settle = () => new Promise(resolve => setImmediate(resolve));
function runtime({accepted=true, declined=false, enabled=true, account='', ext='ts', badSnapshot=false}={}) {
  let definition,app,sequence=0,revoked=false,clock=100;
  const requests=[],menus=[],storage={token:account,share_tracking_instance_v1:'random-installation-only'};
  if(declined)storage.share_tracking_consent_choice_v1={accepted:false,explicit:true,version:'v1',notice};
  else if(accepted)storage.share_tracking_consent_choice_v1={accepted:true,explicit:true,version:'v1',notice};
  const wx={getStorageSync:k=>storage[k],setStorageSync:(k,v)=>storage[k]=v,removeStorageSync:k=>delete storage[k],
    getPerformance:()=>({now:()=>clock}),onNetworkStatusChange(){},getLaunchOptionsSync:()=>({scene:1001}),
    showShareMenu:o=>menus.push({show:o.menus}),hideShareMenu:o=>menus.push({hide:o.menus}),navigateTo(){},
    request(o){
      const suffix=new URL(o.url).pathname;requests.push({path:suffix,method:o.method,body:o.data,header:o.header});
      let data,code=0,statusCode=200,msg='';
      if(suffix==='/api/v1/miniprogram/ai/cost/shares/'+id){
        if(revoked){code=404;statusCode=404;msg='分享报告已失效或不存在';}
        else data={report_id:badSnapshot?'different-invalid-id':id,expires_at:'2026-11-22T00:00:00Z',result};
      }else if(suffix.endsWith('/tracking/config')) data={enabled,consent_version:'v1',privacy_notice:notice};
      else if(suffix.endsWith('/tracking/entry')) data={entry_token:'entry_'+ ++sequence,session_id:'session_'+sequence,content_version:5123,content_accessible:true,share_id:o.data.share_token?'upstream-share':undefined};
      else if(suffix.endsWith('/tracking/shares')) data={share_token:'prepared_'+ ++sequence,share_id:'share_'+sequence,expires_at:new Date(Date.now()+60000).toISOString()};
      else if(suffix.endsWith('/tracking/events/batch')) data={results:o.data.events.map(e=>({event_id:e.event_id,status:'accepted'}))};
      else throw Error('Unexpected transport '+suffix);
      queueMicrotask(()=>o.success({statusCode,data:{code,data,msg}}));
    },
  };
  const auth={generateRequestParams:()=>({}),paramsToHeaders:()=>({token:storage.token})};
  function moduleFile(relative,dependencies){const module={exports:{}};vm.runInNewContext(fs.readFileSync(path.join(root,relative),'utf8'),{module,exports:module.exports,require:dependencies,wx,Promise,console,setTimeout,clearTimeout});return module.exports;}
  const api=moduleFile('utils/tracking/api.js',()=>auth);
  const sdkContext={module:{exports:{}},wx,require:n=>n==='./api'?api:require(path.join(root,'utils/tracking',n.slice(2))),
    Page:d=>definition=d,App:d=>app=d,Promise,Math,Date,Uint8Array,console,setInterval:()=>1,setTimeout,clearTimeout};
  vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/index.js'),'utf8'),sdkContext);
  const adapter=sdkContext.module.exports;adapter.install();sdkContext.App({});app.onLaunch({});app.onShow({});
  const costApi=moduleFile('utils/costEstimateShare.js',n=>n==='./parameter'?auth:{getCachedDeviceFingerprint:()=>''});
  let code=fs.readFileSync(path.join(root,'pages/aicostshare/aicostshare.'+ext),'utf8');
  if(ext==='ts')code=ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
  vm.runInNewContext(code,{exports:{},Page:sdkContext.Page,require:n=>n.endsWith('costEstimateShare')?costApi:{prepareCostEstimateShareImage:async()=> 'local:chat',prepareCostEstimateTimelineImage:async()=> 'local:timeline'},wx,console});
  const page={...definition,route,data:JSON.parse(JSON.stringify(definition.data)),setData(patch,done){Object.assign(this.data,patch);if(done)done.call(this);}};
  return{page,adapter,requests,menus,storage,revoke(){revoked=true;},entries:()=>requests.filter(r=>r.path.endsWith('/tracking/entry')),shares:()=>requests.filter(r=>r.path.endsWith('/tracking/shares')),
    async mount(options={report_id:id,st:'incoming-token'}){page.onLoad(options);page.onShow();page.onReady();for(let i=0;i<12;i++)await settle();},
    async settle(){for(let i=0;i<12;i++)await settle();},close(){page.onUnload();},advance(){clock+=5000;}};
}
for(const ext of ['ts','js'])for(const account of ['', 'existing-account'])test(`${ext} public immutable snapshot: ${account?'account':'anonymous'} first open and re-forward use actual SDK and API adapters`,async()=>{
  const h=runtime({ext,account});await h.mount();
  assert.equal(h.page.data.contentReady,true);
  assert.equal(h.page.data.result.estimatedTotalCost,result.estimatedTotalCost);
  assert.equal(h.entries().length,1);
  assert.equal(h.entries()[0].body.content_id,'costsnapshot:'+id);
  assert.equal(h.entries()[0].body.share_token,'incoming-token');
  const first=h.page.onShareTimeline();assert.equal(typeof first.then,'undefined');
  assert.match(first.query,new RegExp('^report_id='+id+'&st=prepared_'));
  assert.equal(h.shares()[0].body.content_id,'costsnapshot:'+id);
  assert.equal(h.shares()[0].body.content_version,5123);
  assert.equal(h.shares()[0].body.parent_share_id,'upstream-share');
  assert.equal(h.shares()[0].body.prepared,true);
  const direct=h.page.onShareAppMessage();assert.ok(direct.promise);const shared=await direct.promise;
  assert.match(shared.path,new RegExp('^/'+route+'\\?report_id='+id+'&st=prepared_'));
  assert.doesNotMatch(JSON.stringify(h.entries().map(r=>r.body)),/basicInfo|estimatedTotalCost|福建|58\.36/);
  h.page.onHide();h.advance();h.page.onShow();await h.settle();
  assert.equal(h.requests.filter(r=>r.path.includes('/ai/cost/shares/')).length,2,'repeat show revalidates snapshot availability');
  assert.match(h.page.onShareTimeline().query,/st=prepared_/);
  h.revoke();h.page.onHide();h.page.onShow();await h.settle();
  assert.equal(h.page.data.contentReady,false);assert.equal(h.page.data.result,null);
  assert.equal(h.adapter.getSDK().get(h.page).ready,false);
  assert.deepEqual(Object.keys(h.page.getTimelineSharePayload()),[]);
  assert.doesNotMatch(h.page.onShareTimeline().query||'',/st=/);
  h.close();
});
test('disabled or declined analytics leaves explicit public report reading and ordinary forwarding available',async()=>{
  for(const options of [{enabled:false},{declined:true}]){
    const h=runtime(options);await h.mount();assert.equal(h.page.data.contentReady,true);
    assert.equal(h.entries().length,0);assert.equal(h.shares().length,0);
    assert.equal(h.page.onShareTimeline().query,'report_id='+id);
    assert.equal(h.page.onShareAppMessage().path,'/'+route+'?report_id='+id);h.close();
  }
});
test('invalid, foreign and expired links never use local private data or acquire tracking identity',async()=>{
  for(const options of [{badSnapshot:true},{}]){
    const h=runtime(options);if(!options.badSnapshot)h.revoke();await h.mount();
    assert.equal(h.page.data.contentReady,false);assert.equal(h.page.data.result,null);
    assert.equal(h.entries().length,0);assert.equal(h.shares().length,0);assert.ok(h.page.data.error);h.close();
  }
  const h=runtime();await h.mount({report_id:'private-task-id'});
  assert.equal(h.requests.filter(r=>r.path.includes('/ai/cost/shares/')).length,0);
  assert.equal(h.entries().length,0);h.close();
});
