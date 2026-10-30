'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
function runtime(prepare,clipboard){let definition;const copied=[],requests=[],confirmations=[],storage={token:'account-b'};
 const tracking={consented:()=>true,prepareShare:prepare,confirmPreparedShare:(...args)=>confirmations.push(args),splitPath:p=>({query:p.includes('st=')?{st:'opaque-share-b'}:{}})};
 const wx={getStorageSync:k=>storage[k],setClipboardData:o=>{copied.push(o.data);if(clipboard)clipboard(o);else if(o.success)o.success();},showToast(){},navigateTo(){}};
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../miniprogram/pages/contentreader/contentreader.js'),'utf8'),{Page:d=>definition=d,wx,require:id=>id.includes('phase3-api')?{request:async(...args)=>{requests.push(args);return {web_url:'https://www.jiadilingguang.com/share/s/opaque-share-b'};}}:id.includes('tracking/index')?tracking:id.includes('article-document')?{renderArticleDocument(){}}:{},Promise,encodeURIComponent});
 const page={...definition,data:{...definition.data,contentReady:true,content:{title:'我的作品'}},options:{content_id:'content-one',content_version:'2'},entryGeneration:1,setData(o){Object.assign(this.data,o)}};return {page,storage,copied,requests,confirmations};
}
test('reader exchanges the SDK prepared actual-sharer token for own-domain URL',async()=>{const r=runtime(async()=>({path:'/reader?st=opaque-share-b'}));await r.page.copyWebLink();assert.equal(r.requests[0][2].share_token,'opaque-share-b');assert.equal(r.copied[0],'https://www.jiadilingguang.com/share/s/opaque-share-b');assert.equal(r.confirmations.length,1);});
test('reader never fabricates a URL when tracking preparation falls back',async()=>{const r=runtime(async()=>({path:'/reader'}));r.page.copyWebLink && (r.page.data.webLinkBusy=false);await r.page.copyWebLink();assert.equal(r.requests.length,0);assert.equal(r.copied.length,0);});
test('reader discards prepared link after account change',async()=>{let resolve;const r=runtime(()=>new Promise(done=>resolve=done));const pending=r.page.copyWebLink();r.storage.token='account-c';resolve({path:'/reader?st=opaque-share-b'});await pending;assert.equal(r.copied.length,0);assert.equal(r.requests.length,0);});

const tick=()=>new Promise(resolve=>setImmediate(resolve));
test('reader keeps copy busy until native clipboard confirms the same Share',async()=>{
 let clipboard,prepares=0;const r=runtime(async()=>{prepares++;return {path:'/reader?st=opaque-share-b'};},o=>clipboard=o);
 const first=r.page.copyWebLink();await tick();assert.equal(r.page.data.webLinkBusy,true);
 await r.page.copyWebLink();assert.equal(prepares,1);assert.equal(r.confirmations.length,0);
 clipboard.success();await first;assert.equal(r.confirmations.length,1);assert.equal(r.page.data.webLinkBusy,false);
});
test('clipboard failure never confirms a prepared Share',async()=>{
 const r=runtime(async()=>({path:'/reader?st=opaque-share-b'}),o=>o.fail({errMsg:'cancelled'}));
 await r.page.copyWebLink();assert.equal(r.confirmations.length,0);assert.equal(r.page.data.webLinkBusy,false);
});
