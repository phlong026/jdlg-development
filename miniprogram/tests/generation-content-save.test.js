'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');
function runtime() {
 const storage = { token: 'account-a' }; let seq = 0;
 const module = { exports: {} };
 const api = { mutationKey: () => 'save-operation-' + (++seq) };
 const wx = { getStorageSync: key => storage[key], setStorageSync: (key,value) => {storage[key]=value;}, removeStorageSync: key => {delete storage[key];} };
 vm.runInNewContext(fs.readFileSync(path.join(root,'utils/generation-content-save.js'),'utf8'), { module, require: () => api, wx });
 return { helper: module.exports, storage };
}
test('generation-content save retries preserve the operation but a successful explicit copy starts another', () => {
 const { helper, storage } = runtime();
 const first = helper.attemptFor('task-0001','结果图');
 const retry = helper.attemptFor('task-0001','结果图');
 assert.equal(retry.key, first.key);
 helper.completeAttempt(retry, 'saved-content');
 assert.equal(helper.savedContent('task-0001'), 'saved-content');
 helper.forgetAttempt('task-0001');
 assert.notEqual(helper.attemptFor('task-0001','结果图').key,first.key);
 storage.token='account-b';
 assert.notEqual(helper.attemptFor('task-0001','结果图').key,first.key);
});

test('generated details retries one save, then offers continue or an explicit new copy', async () => {
 const { helper, storage } = runtime();
 const sent = [], navigations = []; let choice = 0;
 const source = fs.readFileSync(path.join(root,'pages/generatedetails/generatedetails.js'),'utf8');
 const begin = source.indexOf('syncContentSaveAccount() {'), end = source.indexOf('publishToSquare() {',begin);
 const page = vm.runInNewContext('({' + source.slice(begin,end) + '})', {
  require: id => id.includes('generation-content-save') ? helper : { request: async (_url,method,input) => { if(method==='GET') return {content_id:'content-2',status:'draft'}; sent.push(input); if(sent.length===1)throw new Error('lost response'); return {content_id:'content-'+sent.length}; } },
  wx: { getStorageSync: key => storage[key], showToast() {}, navigateTo: options => navigations.push(options.url), showActionSheet: options=>options.success({tapIndex:choice}), showModal: options=>options.success({confirm:true}) },
 });
 page.data={taskData:{task_no:'task-0001',status:'success',user_prompt:'别墅结果图'},contentSaving:false};
 page.setData=patch=>Object.assign(page.data,patch);
 await page.saveAsContent(); await page.saveAsContent(); await page.saveAsContent();
 assert.equal(sent.length,2,'成功返回后继续编辑不能静默新建');
 assert.equal(sent[0].idempotency_key,sent[1].idempotency_key);
 choice=1;await page.saveAsContent();
 assert.equal(sent.length,3);
 assert.notEqual(sent[2].idempotency_key,sent[1].idempotency_key);
 assert.equal(navigations.length,3);
});

test('deleted saved content offers a fresh copy only after explicit confirmation', async () => {
 const { helper, storage } = runtime();
 const pending = helper.attemptFor('task-0001','结果图'); helper.completeAttempt(pending,'deleted-content');
 let confirm = false, posts = 0, prompts = 0;
 const source = fs.readFileSync(path.join(root,'pages/generatedetails/generatedetails.js'),'utf8');
 const begin=source.indexOf('syncContentSaveAccount() {'),end=source.indexOf('publishToSquare() {',begin);
 const page=vm.runInNewContext('({'+source.slice(begin,end)+'})',{
  require:id=>id.includes('generation-content-save')?helper:{request:async(_url,method)=>{if(method==='GET')throw Object.assign(new Error('gone'),{statusCode:404});posts+=1;return {content_id:'new-copy'};}},
  wx:{getStorageSync:key=>storage[key],showActionSheet:options=>options.success({tapIndex:0}),showModal:options=>{prompts+=1;options.success({confirm});},showToast(){},navigateTo(){}},
 });
 page.data={taskData:{task_no:'task-0001',status:'success',user_prompt:'结果图'},contentSaving:false};page.setData=patch=>Object.assign(page.data,patch);
 await page.saveAsContent();assert.equal(posts,0);assert.equal(prompts,1);assert.equal(page.data.contentSaving,false);
 confirm=true;await page.saveAsContent();assert.equal(posts,1);assert.equal(helper.savedContent('task-0001'),'new-copy');
});

test('generation-content cache never reuses another account completed or pending operation', () => {
 const { helper,storage }=runtime();
 storage.userInfo={id:1,token:'account-a'};
 const first=helper.attemptFor('task-0001','结果图');helper.completeAttempt(first,'account-a-content');
 storage.token='account-b';storage.userInfo={id:2,token:'account-b'};
 assert.equal(helper.savedContent('task-0001'),'');
 const other=helper.attemptFor('task-0001','结果图');assert.notEqual(first.key,other.key);
 helper.completeAttempt(first,'late-account-a-content');
 assert.equal(helper.savedContent('task-0001'),'');
});

test('account switch releases only the old save UI lock and its response cannot unlock the new save', async () => {
 const { helper,storage }=runtime(); const requests=[],navigations=[];
 const source=fs.readFileSync(path.join(root,'pages/generatedetails/generatedetails.js'),'utf8');
 const begin=source.indexOf('syncContentSaveAccount() {'),end=source.indexOf('publishToSquare() {',begin);
 const showBegin=source.indexOf('onShow() {'),showEnd=source.indexOf('buildProcessingStageText(',showBegin);
 const page=vm.runInNewContext('({'+source.slice(showBegin,showEnd)+source.slice(begin,end)+'})',{
  require:id=>id.includes('generation-content-save')?helper:{request:(_url,_method,input)=>new Promise(resolve=>requests.push({input,resolve}))},
  wx:{getStorageSync:key=>storage[key],showToast(){},navigateTo:options=>navigations.push(options.url)},
 });
 page.data={taskData:{task_no:'task-A-001',status:'success',user_prompt:'A结果'},contentSaving:false};page.setData=patch=>Object.assign(page.data,patch);
 const first=page.saveAsContent();assert.equal(requests.length,1);
 storage.token='account-b';page.data.taskData={task_no:'task-B-001',status:'success',user_prompt:'B结果'};page.onShow();
 assert.equal(page.data.contentSaving,false,'切号必须释放A的UI锁');
 const second=page.saveAsContent();assert.equal(requests.length,2);
 requests[0].resolve({content_id:'A-private-content'});await first;
 assert.equal(page.data.contentSaving,true,'A旧回包不得解除B在途锁');assert.equal(navigations.length,0);assert.equal(helper.savedContent('task-A-001'),'');
 requests[1].resolve({content_id:'B-content'});await second;
 assert.equal(page.data.contentSaving,false);assert.deepEqual(navigations,['/pages/contentedit/contentedit?id=B-content']);
});


test('warm unload and load reset the save lifecycle without accepting the old response', async () => {
 const { helper,storage }=runtime(); const requests=[],navigations=[];
 const source=fs.readFileSync(path.join(root,'pages/generatedetails/generatedetails.js'),'utf8');
 const begin=source.indexOf('syncContentSaveAccount() {'),end=source.indexOf('publishToSquare() {',begin);
 const loadBegin=source.indexOf('onLoad(options) {'),loadEnd=source.indexOf('initNavLayout() {',loadBegin);
 const unloadBegin=source.indexOf('onUnload() {'),unloadEnd=source.indexOf('formatTime(',unloadBegin);
 const page=vm.runInNewContext('({'+source.slice(loadBegin,loadEnd)+source.slice(unloadBegin,unloadEnd)+source.slice(begin,end)+'})',{
  require:id=>id.includes('generation-content-save')?helper:{request:(_url,_method,input)=>new Promise(resolve=>requests.push({input,resolve}))},
  wx:{getStorageSync:key=>storage[key],showToast(){},navigateTo:options=>navigations.push(options.url)},
  resolveDetailEntrySource:()=>'',parseIncomingReferenceImageUrls:()=>[],
 });
 page.data={taskData:{task_no:'task-warm-001',status:'success',user_prompt:'结果'},contentSaving:false};page.setData=patch=>Object.assign(page.data,patch);
 for(const name of ['initNavLayout','loadTemplateTabConfig','persistCurrentProgressState','stopPolling','stopVisualProgress','stopEnterpriseWechatAutoCheck'])page[name]=()=>{};
 page.getOpenerEventChannel=()=>({on(){}});
 const first=page.saveAsContent();assert.equal(requests.length,1);
 page.onUnload();page.onLoad({});
 assert.equal(page.contentSaveUnloaded,false,'真实重进必须恢复页面生命周期');
 assert.equal(page.data.contentSaving,false,'重进必须释放已卸载请求的UI锁');
 const second=page.saveAsContent();assert.equal(requests.length,2);
 requests[0].resolve({content_id:'stale-content'});await first;
 assert.equal(page.data.contentSaving,true,'旧操作不得解除重进后新操作的锁');assert.equal(navigations.length,0);
 requests[1].resolve({content_id:'resumed-content'});await second;
 assert.equal(page.data.contentSaving,false);assert.equal(helper.savedContent('task-warm-001'),'resumed-content');
 assert.deepEqual(navigations,['/pages/contentedit/contentedit?id=resumed-content']);
});
