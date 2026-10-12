'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const root=path.join(__dirname,'../miniprogram');
function loadHeaders({consent=true,enabled=true,context={entry_token:'opaque'},broken=false}={}) {
 const module={exports:{}};
 vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/consultation.js'),'utf8'),{module,require(){if(broken)throw Error('analytics');return {consented:()=>consent,getSDK:()=>({diagnostics:()=>({enabled}),context:()=>context})};}});
 return module.exports;
}
test('consultation analytics header is limited to the explicit business command',()=>{
 const helper=loadHeaders(),original={Authorization:'signed'};
 const out=helper.optionalConsultationHeaders(helper.REQUEST_PATH,original);
 assert.equal(out['X-Tracking-Token'],'opaque');assert.equal(out.Authorization,'signed');assert.deepEqual(original,{Authorization:'signed'});
 for(const url of ['/api/v1/miniprogram/customer-service/leads','/api/v1/miniprogram/customer-service/events','/api/v1/miniprogram/customer-service/events/anonymous'])assert.equal(helper.optionalConsultationHeaders(url,original),original);
});
for(const options of [{consent:false},{enabled:false},{context:null},{broken:true}])test('missing or failed analytics leaves business headers untouched '+JSON.stringify(options),()=>{const h=loadHeaders(options),headers={signed:'ok'};assert.equal(h.optionalConsultationHeaders(h.REQUEST_PATH,headers),headers);});
function pageHarness(){
 let definition;const requests=[],toasts=[];const dependencies=new Proxy({generateRequestParams:()=>({sign:'ok'}),paramsToHeaders:()=>({signed:'ok'}),getCachedDeviceFingerprint:()=>'',blockUnsafeText:()=>false,openEnterpriseCustomerServiceChat:async()=>({opened:false})},{get:(target,key)=>target[key]||(()=>{})});
 const helper=loadHeaders();const ctx={exports:{},require:n=>n.endsWith('tracking/consultation')?helper:dependencies,Page:d=>definition=d,wx:{getStorageSync:()=> 'signed-account',request:o=>{requests.push(o);o.success({statusCode:200,data:{code:0,data:{id:73}}});},showToast:o=>toasts.push(o.title)},console,Date,Math,Set,Promise,setTimeout,clearTimeout};
 vm.runInNewContext(fs.readFileSync(path.join(root,'pages/customerservice/customerservice.js'),'utf8'),ctx);
 const p={...definition,data:{...definition.data,demandForm:{},consultationPlan:{}},buildCompleteDemandText:()=> 'test',setData(v){Object.assign(this.data,v);}};
 return {p,requests,toasts};
}
test('actual page routes only explicit human request to consultation endpoint',async()=>{
 const h=pageHarness();for(const reason of ['structured_requirement','generate_click','handoff_human'])await h.p.createLead(reason,{silent:true});
 assert.equal(h.requests.length,3);
 for(const req of h.requests.slice(0,2)){assert.ok(req.url.endsWith('/customer-service/leads'));assert.equal(req.header['X-Tracking-Token'],undefined);}
 assert.ok(h.requests[2].url.endsWith('/customer-service/consultation-requests'));assert.equal(h.requests[2].header['X-Tracking-Token'],'opaque');assert.equal(h.requests[2].header.signed,'ok');
 assert.equal(Object.keys(h.requests[2].header).sort().join(','),'Content-Type,X-Tracking-Token,signed');
});
test('source TypeScript uses same explicit endpoint and optional header bridge',()=>{const source=fs.readFileSync(path.join(root,'pages/customerservice/customerservice.ts'),'utf8');assert.match(source,/reason === 'handoff_human' \? '\/api\/v1\/miniprogram\/customer-service\/consultation-requests'/);assert.match(source,/optionalConsultationHeaders\(apiPath,/);});

test('actual handoff does not claim request saved when business submit fails',async()=>{
 for(const saved of [true,false]){const h=pageHarness();h.p.touchFlow=()=>{};h.p.recordEvent=()=>{};h.p.createLead=async()=>{if(!saved){const e=new Error('missing route');e.statusCode=404;throw e;}return{id:73};};await h.p.handoffHuman();assert.equal(h.toasts.length,1);assert.match(h.toasts[0],saved?/已为你记录需求/:/请求暂未保存/);}
});
