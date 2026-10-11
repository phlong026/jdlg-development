const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const assert=require('node:assert/strict');const {test}=require('node:test');const ts=require('./report-runtime')('typescript');
function api(token='fixture') { const calls=[];const exports={};const js=ts.transpileModule(fs.readFileSync(path.join(__dirname,'../miniprogram/pages/report/api.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;vm.runInNewContext(js,{exports,require:id=>id.endsWith('/parameter')?{generateRequestParams:async()=>({}),paramsToHeaders:()=>({})}:{getCachedDeviceFingerprint:()=>''},wx:{getStorageSync:()=>token,removeStorageSync(){},request:options=>calls.push(options)}});return {exports,calls};}
test('asynchronous 202 house creation returns the report identity',async()=>{const h=api();const pending=h.exports.reportRequest('/house','POST',{primary_goal:'space'});await new Promise(r=>setImmediate(r));h.calls[0].success({statusCode:202,data:{code:0,data:{id:'house-1'}}});assert.equal((await pending).id,'house-1');});
test('public house questions do not require login while customer reports remain private',async()=>{const h=api('');const pending=h.exports.reportRequest('/house/questions');await new Promise(r=>setImmediate(r));assert.equal(h.calls.length,1);h.calls[0].success({statusCode:200,data:{code:0,data:{version:'report-house-v1'}}});await pending;await assert.rejects(h.exports.reportRequest('/house','POST',{}),/登录/);});

test('private PDF signatures include the exact query required by the server middleware',async()=>{
 const signed=[];const calls=[];const exports={};
 const source=ts.transpileModule(fs.readFileSync(path.join(__dirname,'../miniprogram/pages/report/api.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
 vm.runInNewContext(source,{exports,require:id=>id.endsWith('/parameter')?{generateRequestParams:async(...args)=>{signed.push(args);return {};},paramsToHeaders:()=>({})}:{getCachedDeviceFingerprint:()=>''},wx:{getStorageSync:()=> 'owner',request:options=>calls.push(options)}});
 const pending=exports.reportRequest('/r1/document?mode=full');await new Promise(r=>setImmediate(r));
 assert.equal(signed[0][2],'/api/v1/miniprogram/reports/r1/document?mode=full');
 calls[0].success({statusCode:200,data:{code:0,data:{document_url:'https://example.test/private.pdf'}}});await pending;
});
test('the existing V5 question endpoint is public',async()=>{const h=api('');const work=h.exports.reportRequest('/v5/questions');await new Promise(r=>setImmediate(r));assert.equal(h.calls.length,1);h.calls[0].success({statusCode:200,data:{code:0,data:{}}});await work;});
