'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const root=path.join(__dirname,'../miniprogram/pages/sharevisits');
const source=()=>fs.readFileSync(path.join(root,'sharevisits.ts'),'utf8').replace(/^\/\/ @ts-nocheck\n/,'').replace(/\nexport {};\n?$/,'\n');
function runtime(items,stubs){
 stubs=stubs||{};
 let definition;const navigation=[];
 const apiStub=Object.assign({listMyShares:async()=>({items}),listMyVisitors:async(query)=>({items:(stubs.__visitors||[]).filter(v=>!query.q||(v.nickname||'').includes(query.q))}),listMyContentStats:async()=>({items:[]}),listMyVisitorEntries:async()=>({items:[]})},stubs);
 vm.runInNewContext(source(),{Page:o=>definition=o,wx:{getStorageSync:()=> 'login-token',hideShareMenu(){},navigateTo:o=>navigation.push(o.url)},require:()=>apiStub,Promise,Date,Number,String,encodeURIComponent,setTimeout,clearTimeout,console});
 const page={...definition,data:structuredClone(definition.data),setData(value){Object.assign(this.data,value)}};
 return {page,navigation};
}
function myVisitor(key,nick,level,views){return{visitor_key:key,nickname:nick,intent:{level},page_views:views,last_visit:'2026-10-27T02:00:00Z',visible_ms:65000};}

test('share rows translate status and channel without exposing raw values',async()=>{
 const {page}=runtime([
  {share_id:'friend',status:'published',channel:'app_message',visitor_count:2,page_views:3,created_at:'2026-10-27T01:00:00Z',last_visit:'2026-10-27T02:00:00Z'},
  {share_id:'timeline',status:'published',channel:'timeline',visitor_count:0,page_views:0,created_at:'2026-10-27T01:00:00Z'},
  {share_id:'revoked',revoked:true,status:'published',channel:'server_internal',visitor_count:0,page_views:0},
  {share_id:'invalid-version',status:'expired',expires_at:'2099-10-27T01:00:00Z',visitor_count:0,page_views:0},
  {share_id:'expired-share',status:'published',expires_at:'2000-10-27T01:00:00Z',visitor_count:0,page_views:0},
 ]);
 page._token='login-token';page._epochShares=1;await page.loadShares(true,1);
assert.deepEqual(page.data.items.map(item=>[item.status_text,item.channel_text,item.has_visits]),[
  ['分享中','微信好友',true],['分享中','朋友圈',false],['已撤回','分享方式未记录',false],
  ['内容版本已失效','分享方式未记录',false],['分享已过期','分享方式未记录',false],
 ]);
});

test('客户Tab聚合榜映射意向标签与时长，且他人不出现',async()=>{
 const visitors=[myVisitor('u:77','陈丽芬','high',2),myVisitor('a:anon','','unknown',0)];
 const {page}=runtime([],{__visitors:visitors,listMyVisitors:async(query)=>{console.log('STUB q=', JSON.stringify(query && query.q)); return {items:(function(){const f=visitors.filter(v=>!query.q||(v.nickname||'').includes(query.q)); console.log('STUB RETURN', f.length); return f})()}}});
 page._token='login-token';page._epochCustomers=1;await page.loadCustomers(true,1);
 const items=page.data.customers;
 assert.equal(items.length,2);
 assert.equal(items[0].intent_level,'high');
 assert.equal(items[0].duration_text,'1分5秒');
 assert.equal(items[1].intent_level,'unknown');
 // q 搜索过滤
 page.setData({customerQuery:'陈'});
 await page.reloadCustomers();
	assert.equal(page.data.customers.length,1);
 assert.equal(page.data.customers[0].nickname,'陈丽芬');
});

test('素材Tab热度榜带去重人数与转发数',async()=>{
 // stub 字段对齐后端 MyContentStat JSON：page_views / forward_count
 const {page}=runtime([],{listMyContentStats:async()=>({items:[{route_key:'pages/contentreader/contentreader',content_id:'c-1',visitor_count:2,page_views:3,forward_count:1,title:'王宅方案'},{route_key:'x',content_id:'c-2',visitor_count:1,page_views:1,forward_count:0,title:'其他'}]})});
 page._token='login-token';page._epochMaterials=1;await page.loadMaterials(true,1);
 assert.equal(page.data.materials.length,2);
 assert.equal(page.data.materials[0].visitor_count,2);
 assert.equal(page.data.materials[0].forwards,1);
});

test('三Tab结构与键集齐备（wxml 契约）',()=>{
 const wxml=fs.readFileSync(path.join(root,'sharevisits.wxml'),'utf8');
 assert.match(wxml,/activeTab === 'customers'/);
 assert.match(wxml,/activeTab === 'materials'/);
 assert.match(wxml,/activeTab === 'shares'/);
 assert.match(wxml,/item\.intent_level/);
 assert.match(wxml,/查看人数/);
 assert.match(wxml,/去重/);
 assert.match(wxml,/item\.status_text/); // 分享Tab 保留原卡片字段
});


// 可重入 deferred：每次 API 调用挂起，按序手动释放（复现慢响应竞态）
function deferredQueue(count) {
 const queue=[];const calls=[];
 for(let i=0;i<count;i++){calls.push(new Promise(resolve=>{queue.push(resolve)}));}
 return {calls,release:(index,payload)=>queue[index](payload)};
}

test('codex P1-3回归：Tab间不作废对方请求，分页锁不卡死',async()=>{
 const first=deferredQueue(1);
 const {page}=runtime([],{
  listMyShares:async()=>{
   if(firstDone){return secondCall()}
   firstDone=true;return first.calls[0];
  },
  listMyVisitors:async()=>({items:[]}),
 });
 let firstDone=false;const second={calls:[null]};let secondResolver;
 function secondCall(){return new Promise(resolve=>{secondResolver=resolve});}
 page._token='login-token';
 // 首屏（reset）拿到 cursor
 first.release(0,{items:[{share_id:'p1',status:'published',channel:'app_message'}],next_cursor:'c1'});
 await page.loadShares(true,page.bumpEpoch('shares'));
 assert.equal(page.data.next_cursor,'c1');
 // 分页请求挂起（锁置位）
 const pendingMore=page.loadShares(false,page.bumpEpoch('shares'));
 assert.ok(page._shareMorePending,'分页锁应置为持有者epoch');
 // 客户Tab并发刷新：不得作废分享请求，也不得卡死其锁
 await page.loadCustomers(true,page.bumpEpoch('customers'));
 assert.ok(page._shareMorePending,'客户Tab刷新不得释放分享分页锁');
 // 释放分享分页：数据写入、锁释放
 secondResolver({items:[{share_id:'p2',status:'published',channel:'timeline'}],next_cursor:''});
 await pendingMore;
 assert.deepEqual(page.data.items.map(i=>i.share_id),['p1','p2']);
 assert.equal(page._shareMorePending,false);
});

test('codex P1-2回归：retry 分配新 epoch，旧慢响应不得覆盖新结果',async()=>{
 const dq=deferredQueue(2);let taken=0;
 const {page}=runtime([],{listMyShares:async()=>{const p=dq.calls[taken];taken++;return p}});
 page._token='login-token';
 const oldRequest=page.loadShares(true);
 const newRequest=page.retry();
 // 新响应先返回并断言生效（retry 复用旧 epoch 或丢守卫则此处即失败）
 dq.release(1,{items:[{share_id:'fresh',status:'published',channel:'app_message'}],next_cursor:''});
 await newRequest;
 assert.deepEqual(page.data.items.map(i=>i.share_id),['fresh']);
 // 旧响应随后才到：不得覆盖已生效的新结果（删除成功分支 tokenAlive 守卫则此处失败）
 dq.release(0,{items:[{share_id:'old',status:'published',channel:'timeline'}],next_cursor:''});
 await oldRequest;
 assert.deepEqual(page.data.items.map(i=>i.share_id),['fresh']);
});

test('codex终审回归：旧分页挂起时reset成功后新分页可用，旧finally不释放新锁',async()=>{
 const pending=[];
 let taken=0;
 const {page}=runtime([],{listMyShares:async()=>{
  const index=taken++;
  if(index===0)return {items:[{share_id:'p1',status:'published',channel:'app_message'}],next_cursor:'c1'}; // 首屏
  if(index===1)return new Promise(resolve=>pending.push({resolve})); // 旧分页挂起
  if(index===2)return {items:[{share_id:'fresh',status:'published',channel:'app_message'}],next_cursor:'c2'}; // reset 立即返回
  return new Promise(resolve=>pending.push({resolve})); // 新分页挂起
 }});
 page._token='login-token';
 await page.loadShares(true);
 assert.equal(page.data.next_cursor,'c1');
 // 旧分页挂起（持有锁）
 const staleMore=page.loadShares(false);
 assert.ok(page._shareMorePending,'旧分页应持锁');
 // reset 成功返回（推进 epoch，废弃已被作废的旧锁）
 await page.loadShares(true);
 assert.equal(page.data.next_cursor,'c2');
 assert.equal(page._shareMorePending,false,'reset应废弃已被作废的旧分页锁');
 // 新分页立即放行并持新锁
 const newMore=page.loadShares(false);
 assert.ok(page._shareMorePending,'新分页应持新锁');
 // 旧分页响应迟到：不得写入数据，也不得释放新分页的锁
 pending[0].resolve({items:[{share_id:'stale',status:'published',channel:'timeline'}],next_cursor:'stale-c'});
 await staleMore;
 assert.deepEqual(page.data.items.map(i=>i.share_id),['fresh']); // reset B 已清掉 p1，旧分页不得追加
 assert.ok(page._shareMorePending,'旧分页finally不得释放新分页锁');
 // 新分页完成：正常写入并释放
 pending[1].resolve({items:[{share_id:'p3',status:'published',channel:'app_message'}],next_cursor:''});
 await newMore;
 assert.deepEqual(page.data.items.map(i=>i.share_id),['fresh','p3']);
 assert.equal(page._shareMorePending,false);
});
