'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const root=path.join(__dirname,'../miniprogram/pages/sharevisits');
const source=()=>fs.readFileSync(path.join(root,'sharevisits.ts'),'utf8').replace(/^\/\/ @ts-nocheck\n/,'').replace(/\nexport \{\};\n?$/,'\n');
function runtime(items){
 let definition;const navigation=[];
 const wx={getStorageSync:()=> 'login-token',hideShareMenu(){},navigateTo:o=>navigation.push(o.url)};
 vm.runInNewContext(source(),{Page:o=>definition=o,wx,require:()=>({listMyShares:async()=>({items})}),Promise,Date,Number,String,encodeURIComponent,console});
 const page={...definition,data:structuredClone(definition.data),setData(value){Object.assign(this.data,value)}};
 return {page,navigation};
}
test('share rows translate status and channel without exposing raw values',async()=>{
 const {page}=runtime([
  {share_id:'friend',status:'published',channel:'app_message',visitor_count:2,page_views:3,created_at:'2026-10-27T01:00:00Z',last_visit:'2026-10-27T02:00:00Z'},
  {share_id:'timeline',status:'published',channel:'timeline',visitor_count:0,page_views:0,created_at:'2026-10-27T01:00:00Z'},
  {share_id:'revoked',revoked:true,status:'published',channel:'server_internal',visitor_count:0,page_views:0},
  {share_id:'invalid-version',status:'expired',expires_at:'2099-10-27T01:00:00Z',visitor_count:0,page_views:0},
  {share_id:'expired-share',status:'published',expires_at:'2000-10-27T01:00:00Z',visitor_count:0,page_views:0},
 ]);
 page._token='login-token';page._epoch=1;await page.load(true,1);
 assert.deepEqual(page.data.items.map(item=>[item.status_text,item.channel_text,item.has_visits]),[
  ['分享中','微信好友',true],['分享中','朋友圈',false],['已撤回','分享方式未记录',false],
  ['内容版本已失效','分享方式未记录',false],['分享已过期','分享方式未记录',false],
 ]);
});
test('share list template is compact and never prints raw status or channel',()=>{
 const wxml=fs.readFileSync(path.join(root,'sharevisits.wxml'),'utf8');
 assert.match(wxml,/item\.status_text/);
 assert.match(wxml,/item\.channel_text/);
 assert.match(wxml,/分享于/);
 assert.match(wxml,/暂时没有访问/);
 assert.doesNotMatch(wxml,/item\.status\s*\|\||item\.channel\s*\|\||仅显示由当前登录账号/);
});
