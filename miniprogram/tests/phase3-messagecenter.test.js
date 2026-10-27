'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const source=()=>fs.readFileSync(path.join(__dirname,'../miniprogram/pages/messagecenter/messagecenter.js'),'utf8');
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
