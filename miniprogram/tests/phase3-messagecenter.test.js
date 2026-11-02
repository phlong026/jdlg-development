'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const source=()=>fs.readFileSync(path.join(__dirname,'../miniprogram/pages/messagecenter/messagecenter.ts'),'utf8').replace(/^\/\/ @ts-nocheck\n/,'').replace(/\nexport \{\};\n?$/,'\n');
function runtime(handler,token='login-token'){
 let definition;const calls=[],navigation=[];const storage={token};
 const wx={getStorageSync:k=>storage[k],hideShareMenu(){},navigateTo:o=>navigation.push(o.url),showModal(){},showToast(){}};
 vm.runInNewContext(source(),{Page:o=>definition=o,wx,require:()=>({request:async(...args)=>{calls.push(args);return handler(...args);}}),Promise,Date,encodeURIComponent,console});
 const page={...definition,data:structuredClone(definition.data),setData(o){Object.assign(this.data,o)}};return {page,calls,storage,navigation};
}
const tick=()=>new Promise(resolve=>setImmediate(resolve));
test('message center requires login and does not share private records',async()=>{
 const {page,calls}=runtime(()=>{throw Error('must not call')},'');page.onShow();await tick();assert.equal(calls.length,0);assert.equal(page.data.loginRequired,true);assert.equal(typeof page.onShareAppMessage,'undefined');
});
test('message center shows real records and reads only selected notification',async()=>{
 const {page,calls,navigation}=runtime((url)=>url.includes('unread-count')?{unread_count:2}:url.includes('official-account')?{capability:{binding_available:false}}:url.endsWith('/read')?{}:url.endsWith('/notice-one')?{notification_id:'notice-one',share_id:'share-one',body:'有人浏览了分享内容'}:{items:[{notification_id:'notice-one',share_id:'share-one',title:'浏览提醒',created_at:'2026-10-27T01:00:00Z'}],next_cursor:'next-page'});
 page.onShow();await tick();assert.equal(page.data.messages.length,1);assert.equal(page.data.unread,2);
 await page.onOpenMessage({currentTarget:{dataset:{id:'notice-one'}}});assert.ok(calls.some(c=>c[0].endsWith('/notice-one/read')&&c[1]==='PATCH'));assert.ok(navigation[0].includes('share_id=share-one'));
});
test('account change discards previous account response',async()=>{
 let resolve;const {page,storage}=runtime(url=>url.endsWith('/notifications')?new Promise(r=>resolve=r):{});
 page.onShow();await tick();storage.token='';page.onShow();resolve({items:[{notification_id:'private-old'}]});await tick();assert.equal(page.data.messages.length,0);assert.equal(page.data.loginRequired,true);
});
test('private project notice follows only its fixed management route without exposing share token',async()=>{
 const route='pages/projectnotes/projectnotes?project_id=project_1234&version_id=version_1234';
 const {page,navigation}=runtime(url=>url.endsWith('/private-note')?{notification_id:'private-note',kind:'project_note_open',route_key:route,content_id:'version_1234',share_id:'internal-share'}:url.endsWith('/read')||url.includes('unread-count')?{}:{items:[]});
 page.onShow();await tick();await page.onOpenMessage({currentTarget:{dataset:{id:'private-note'}}});
 assert.deepEqual(navigation,['/'+route]);assert.doesNotMatch(navigation[0],/pns=|token=/);
});
test('project feedback notice opens the exact project version and feedback without exposing share credentials',async()=>{
 const route='pages/projectnotes/projectnotes?project_id=project_1234';
 const {page,navigation}=runtime(url=>url.endsWith('/feedback-note')?{notification_id:'feedback:feedback_1234',kind:'project_note_feedback',route_key:route,content_id:'version_1234'}:url.endsWith('/read')||url.includes('unread-count')?{}:{items:[]});
 page.onShow();await tick();await page.onOpenMessage({currentTarget:{dataset:{id:'feedback-note'}}});
 assert.deepEqual(navigation,['/'+route+'&version_id=version_1234&feedback_id=feedback_1234']);assert.doesNotMatch(navigation[0],/pns=|token=|share_id=/);
});
test('project package receipt opens the exact confirmed version instead of public share visits',async()=>{
 const route='pages/projectnotes/projectnotes?project_id=project_1234&version_id=version_1234';
 const {page,navigation}=runtime(url=>url.endsWith('/receipt-note')?{notification_id:'package-receipt:receipt_1234',kind:'project_package_receipt',route_key:route,content_id:'version_1234',share_id:'delivery_1234'}:url.endsWith('/read')||url.includes('unread-count')?{}:{items:[]});
 page.onShow();await tick();await page.onOpenMessage({currentTarget:{dataset:{id:'receipt-note'}}});
 assert.deepEqual(navigation,['/'+route]);assert.doesNotMatch(navigation[0],/visitdetail|share_id=|token=/);
});
test('message rows use the action and category of their real business kind',async()=>{
 const {page}=runtime(url=>url.includes('unread-count')?{unread_count:3}:url.includes('official-account')?{capability:{binding_available:false}}:{
  items:[
   {notification_id:'browse-one',kind:'browse',title:'分享内容有新访问',created_at:'2026-10-27T01:00:00Z'},
   {notification_id:'open-one',kind:'project_note_open',title:'私人项目有新访问',created_at:'2026-10-27T02:00:00Z'},
   {notification_id:'feedback-one',kind:'project_note_feedback',title:'项目收到业主反馈',created_at:'2026-10-27T03:00:00Z'},
   {notification_id:'receipt-one',kind:'project_package_receipt',title:'收到阅读回执',created_at:'2026-10-27T04:00:00Z'},
  ],
 });
 page.onShow();await tick();
 assert.deepEqual(page.data.messages.map(item=>[item.categoryLabel,item.actionLabel]),[
  ['访问提醒','查看访问'],['项目动态','查看方案'],['项目反馈','查看反馈'],['阅读回执','查看方案'],
 ]);
});
test('service account status distinguishes binding from real delivery availability',async()=>{
 const unavailable=runtime(url=>url.includes('official-account')?{bound:true,subscribed:true,capability:{binding_available:true,delivery_available:false}}:{}).page;
 unavailable._token='login-token';unavailable._epoch=1;await unavailable.loadBinding();
 assert.equal(unavailable.data.bindingLabel,'已连接，提醒暂不可用');
 const available=runtime(url=>url.includes('official-account')?{bound:true,subscribed:true,capability:{binding_available:true,delivery_available:true}}:{}).page;
 available._token='login-token';available._epoch=1;await available.loadBinding();
 assert.equal(available.data.bindingLabel,'已连接，可接收提醒');
});
test('message template keeps service status compact and uses a neutral empty state',()=>{
 const wxml=fs.readFileSync(path.join(__dirname,'../miniprogram/pages/messagecenter/messagecenter.wxml'),'utf8');
 assert.match(wxml,/notice-strip/);
 assert.match(wxml,/item\.categoryLabel/);
 assert.match(wxml,/item\.actionLabel/);
 assert.match(wxml,/暂时没有消息/);
 assert.doesNotMatch(wxml,/class="binding-card"|暂时没有浏览提醒|查看访问记录/);
});

test('join request opens member approval and never treats invitation id as public share',async()=>{
 for(const route of ['pages/projectnotes/projectnotes?project_id=project_1234&view=members','pages/index/index']){
  const {page,navigation}=runtime(url=>url.endsWith('/join-request')?{notification_id:'join:request_1234',kind:'project_note_join_request',route_key:route,share_id:'invitation_1234'}:url.endsWith('/read')||url.includes('unread-count')?{}:{items:[]});
  page.onShow();await tick();await page.onOpenMessage({currentTarget:{dataset:{id:'join-request'}}});
  assert.deepEqual(navigation,route.includes('project_id=')?['/'+route]:[]);
 }
});
test('delivery preference toggles PUT both fields and stay hidden when unavailable',async()=>{
 const {page,calls}=runtime(url=>url.includes('preferences')?{browse_delivery:true,project_delivery:true}:url.includes('unread-count')?{unread_count:0}:url.includes('official-account')?{capability:{binding_available:false}}:{items:[]});
 page.onShow();await tick();
 assert.equal(page.data.prefsVisible,true,'服务端支持偏好接口时展示设置卡');
 await page.onToggleBrowse({detail:{value:false}});await tick();
 const put=calls.find(c=>c[0].endsWith('/preferences')&&c[1]==='PUT');
 assert.equal(put[2].browse_delivery,false,'保存必须整份提交两个字段');
 assert.equal(put[2].project_delivery,true);
 assert.equal(page.data.browseDelivery,false);

 // 服务端未迁移偏好能力（响应无布尔字段）：设置卡隐藏，不影响消息列表
 const legacy=runtime(url=>url.includes('preferences')?{items:[]}:url.includes('unread-count')?{unread_count:0}:url.includes('official-account')?{capability:{binding_available:false}}:{items:[]});
 legacy.page.onShow();await tick();
 assert.equal(legacy.page.data.prefsVisible,false);

 // 保存失败回滚到原值
 let saveOK=false;
 const flaky=runtime(url=>{
  if(!url.includes('preferences'))return url.includes('unread-count')?{unread_count:0}:url.includes('official-account')?{capability:{binding_available:false}}:{items:[]};
  if(!saveOK)return {browse_delivery:true,project_delivery:true};
  throw Error('save failed');
 });
 flaky.page.onShow();await tick();
 flaky.page._epoch=1;saveOK=true;
 await flaky.page.savePrefs(false,true);await tick();
 assert.equal(flaky.page.data.browseDelivery,true,'保存失败必须回滚开关状态');
 const wxml=fs.readFileSync(path.join(__dirname,'../miniprogram/pages/messagecenter/messagecenter.wxml'),'utf8');
 assert.match(wxml,/bindchange="onToggleBrowse"/);
 assert.match(wxml,/bindchange="onToggleProject"/);
 assert.match(wxml,/wx:if="\{\{!loginRequired && prefsVisible\}\}"/);
});
