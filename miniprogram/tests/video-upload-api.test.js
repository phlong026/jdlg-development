'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
test('video upload API signs session and part paths without forcing multipart content type',async()=>{
 const calls=[];let progress;
 const sandbox={module:{exports:{}},require:()=>({generateRequestParams:(_t,_b,p)=>({path:p}),paramsToHeaders:p=>({signed:p.path})}),wx:{getStorageSync:()=> 'session',request:o=>{calls.push(o);o.success({statusCode:200,data:{code:0,data:{upload_id:'session-one'}}})},uploadFile:o=>{calls.push(o);o.success({statusCode:200,data:JSON.stringify({code:0,data:{uploaded_parts:[0]}})});return{onProgressUpdate(fn){fn({progress:50});}}}},Promise,Error,Object,String,Number,JSON,encodeURIComponent};
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../miniprogram/utils/phase3-api.js'),'utf8'),sandbox);const api=sandbox.module.exports;
 await api.beginVideoUpload({size:1000000000,fingerprint:'a'.repeat(40),owner_id:'owner'});await api.videoUploadStatus('session-one');await api.uploadVideoPart('session-one',0,'/tmp/chunk',{onProgress:value=>{progress=value.progress}});await api.completeVideoUpload('session-one');
 assert.equal(calls[0].data.size,1000000000);assert.equal(calls[2].header.signed,'/api/v1/miniprogram/workbench/media-uploads/session-one/parts/0');assert.equal(calls[2].header['Content-Type'],undefined);assert.equal(calls[2].name,'file');assert.equal(progress,50);assert.match(calls[3].url,/session-one\/complete$/);
});
