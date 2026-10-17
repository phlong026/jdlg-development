'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const root=path.join(__dirname,'../miniprogram');
const route='pages/contentreader/contentreader';
const settle=()=>new Promise(resolve=>setImmediate(resolve));
const material={content_id:'material_one',content_version:2,title:'已发布文章',status:'published',route_key:route,cover:{url:'/api/v1/miniprogram/tracking/media/image_one'}};
function runtime(token='account-a'){
 const storage={token,device_fingerprint:'cached-login-device'},requests=[],navigations=[],toasts=[],signatures=[];let definition;
 const wx={getStorageSync:key=>storage[key],setStorageSync:(key,value)=>storage[key]=value,removeStorageSync:key=>delete storage[key],
 request:r=>requests.push(r),navigateTo:r=>navigations.push(r),showToast:r=>toasts.push(r),setBackgroundColor(){},stopPullDownRefresh(){}};
 const signer={generateRequestParams:(token,body,p,device)=>{signatures.push({token,body,path:p,device});return{token};},paramsToHeaders:x=>({...x,pass:'signed'})};
 const sandbox={wx,module:{exports:{}},require:()=>signer,Promise};
 vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/api.js'),'utf8'),sandbox);const api=sandbox.module.exports;
 const mixin={wx,module:{exports:{}},require:()=>api,Promise};vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/materials.js'),'utf8'),mixin);
 const tracking={consented:()=>false,refreshConfig:()=>Promise.resolve({enabled:false})};
 vm.runInNewContext(fs.readFileSync(path.join(root,'pages/my/my.js'),'utf8'),{wx,exports:{},Page:value=>definition=value,Promise,Date,console:{log(){},warn(){},error(){}},
 require:name=>name.endsWith('/tracking/materials')?mixin.module.exports:name.endsWith('/tracking/index')?tracking:name.endsWith('/parameter')?signer:name.endsWith('/asset')?{resolveAssetPath:x=>x}:{getCachedDeviceFingerprint:()=>'',generateDeviceFingerprint:async()=>'',cacheDeviceFingerprint(){}}});
 const page={...definition,data:JSON.parse(JSON.stringify(definition.data)),setData:value=>Object.assign(page.data,value)};
 const matching=()=>requests.filter(r=>r.url.includes('/tracking/materials'));
 return{page,storage,requests,navigations,toasts,signatures,matching,ok:(index,data)=>matching()[index].success({statusCode:200,data:{code:0,data}}),fail:(index,status=503)=>matching()[index].success({statusCode:status,data:{code:status}})};
}
test('existing My page loads signed published cards and opens the server-verified fixed reader version',async()=>{
 const t=runtime();t.page.onShow();assert.equal(t.page.data.shareMaterialsState,'loading');assert.equal(t.matching().length,1);
 const r=t.matching()[0];assert.equal(r.header.token,'account-a');assert.equal(r.header.pass,'signed');
 assert.ok(t.signatures.some(s=>s.path==='/api/v1/miniprogram/tracking/materials'&&s.device==='cached-login-device'));
 t.ok(0,{items:[material]});await settle();assert.equal(t.page.data.shareMaterialsState,'ready');assert.equal(t.page.data.shareMaterials[0].title,material.title);assert.match(t.page.data.shareMaterials[0].coverURL,/^https:\/\/api\.jiadilingguang.com/);
 const click={currentTarget:{dataset:{id:material.content_id}}};t.page.openShareMaterial(click);t.page.openShareMaterial(click);assert.equal(t.matching().length,2);assert.equal(t.navigations.length,0);
 t.ok(1,material);await settle();assert.equal(t.navigations.length,1);assert.equal(t.navigations[0].url,'/'+route+'?content_id=material_one&content_version=2');
});
test('anonymous My page never requests private materials',async()=>{const t=runtime('');t.page.onShow();await settle();assert.equal(t.matching().length,0);assert.equal(t.page.data.shareMaterialsState,'hidden');});
for(const status of [401,403,404])test('denied materials hide and clear cards: '+status,async()=>{const t=runtime();t.page.onShow();t.fail(0,status);await settle();assert.equal(t.page.data.shareMaterialsState,'hidden');assert.equal(t.page.data.shareMaterials.length,0);});
test('network failure has retry and authorized empty state is distinct',async()=>{const t=runtime();t.page.onShow();t.fail(0);await settle();assert.equal(t.page.data.shareMaterialsState,'error');t.page.refreshShareMaterials();t.ok(1,{items:[]});await settle();assert.equal(t.page.data.shareMaterialsState,'empty');});
test('list pagination retains authorized cards, and refresh clears stale revoked entries',async()=>{const t=runtime();t.page.onShow();t.ok(0,{items:[material],next_cursor:material.content_id});await settle();t.page.refreshShareMaterials({currentTarget:{dataset:{more:true}}});assert.match(t.matching()[1].url,/cursor=material_one/);t.ok(1,{items:[{...material,content_id:'material_two'}]});await settle();assert.equal(t.page.data.shareMaterials.length,2);t.page.refreshShareMaterials();assert.equal(t.page.data.shareMaterials.length,0);t.ok(2,{items:[]});await settle();assert.equal(t.page.data.shareMaterialsState,'empty');});
test('revocation between listing and opening never navigates and reloads current authorization',async()=>{const t=runtime();t.page.onShow();t.ok(0,{items:[material]});await settle();t.page.openShareMaterial({currentTarget:{dataset:{id:material.content_id}}});t.fail(1,404);await settle();assert.equal(t.navigations.length,0);assert.equal(t.matching().length,3);t.fail(2,403);await settle();assert.equal(t.page.data.shareMaterialsState,'hidden');});
for(const boundary of ['switch','logout','hide'])test('late private list response is discarded after '+boundary,async()=>{const t=runtime();t.page.onShow();if(boundary==='hide')t.page.onHide();else{t.storage.token=boundary==='switch'?'account-b':'';t.page.loadUserData();}t.ok(0,{items:[material]});await settle();assert.equal(t.page.data.shareMaterials.length,0);});
for(const boundary of ['switch','hide'])test('late open response cannot navigate after '+boundary,async()=>{const t=runtime();t.page.onShow();t.ok(0,{items:[material]});await settle();t.page.openShareMaterial({currentTarget:{dataset:{id:material.content_id}}});if(boundary==='hide')t.page.onHide();else{t.storage.token='account-b';t.page.onShow();}t.ok(1,material);await settle();assert.equal(t.navigations.length,0);assert.equal(t.page.data.shareMaterials.length,0);});
test('new account list wins over an older account response',async()=>{const t=runtime();t.page.onShow();t.storage.token='account-b';t.page.onShow();t.ok(1,{items:[{...material,content_id:'material_two',title:'账户B'}]});await settle();t.ok(0,{items:[material]});await settle();assert.equal(t.page.data.shareMaterials[0].title,'账户B');});
test('unexpected target cannot redirect outside the registered reader',async()=>{const t=runtime();t.page.onShow();t.ok(0,{items:[material]});await settle();t.page.openShareMaterial({currentTarget:{dataset:{id:material.content_id}}});t.ok(1,{...material,route_key:'pages/my/my'});await settle();assert.equal(t.navigations.length,0);});
test('My template provides visible title cover status empty/error/retry without creating a page',()=>{const s=fs.readFileSync(path.join(root,'pages/my/my.wxml'),'utf8');for(const value of ['可分享素材','item.title','item.coverURL','已发布','refreshShareMaterials','openShareMaterial','shareMaterialsState'])assert.ok(s.includes(value));const app=JSON.parse(fs.readFileSync(path.join(root,'app.json')));const pages=app.pages.concat(...app.subPackages.map(p=>p.pages.map(page=>p.root+'/'+page)));assert.equal(pages.filter(p=>p===route).length,1);});

test('reader native share uses the published target, title and cover, with menus blocked on initial load and read failure',async()=>{
 let definition,resolveRead,rejectRead;const hidden=[],shown=[];
 const wx={hideShareMenu:()=>hidden.push(true),showShareMenu:x=>shown.push(x),getWindowInfo:()=>({windowWidth:375})};
 const api={resolveReaderOptions:async x=>x,mediaURL:x=>'https://api.jiadilingguang.com'+x,request:()=>new Promise((resolve,reject)=>{resolveRead=resolve;rejectRead=reject;})};
 const preheats=[];const tracking={captureEntryValidity:()=>()=>true,resolvePageOptions(){},refreshConfig:async()=>({enabled:false}),consented:()=>false,block(){},preheatTimelineShare(page,value){preheats.push(value);}};
 vm.runInNewContext(fs.readFileSync(path.join(root,route+'.js'),'utf8'),{Page:x=>definition=x,wx,exports:{},require:name=>name.endsWith('/tracking/index')?tracking:name.endsWith('/tracking/api')?api:{},Promise,Date});
 const page={...definition,data:JSON.parse(JSON.stringify(definition.data)),setData(v,callback){Object.assign(this.data,v);if(callback)callback();}};
 page.onLoad({content_id:material.content_id,content_version:2});page.onShow();await settle();assert.equal(Object.keys(page.onShareAppMessage()).length,0);assert.equal(hidden.length,1);
 resolveRead({content_id:material.content_id,version:2,title:material.title,cover:material.cover,media:[]});await settle();
 const share=page.onShareAppMessage();assert.equal(share.title,material.title);assert.equal(share.path,'/'+route+'?content_id=material_one&content_version=2');assert.equal(share.imageUrl,'https://api.jiadilingguang.com'+material.cover.url);assert.equal(shown.length,1);
 assert.equal(page.onShareTimeline().query,'content_id=material_one&content_version=2');
 assert.equal(preheats.length,1);const successShare=page.onShareTimeline();assert.deepEqual(preheats[0],successShare);
 page.loadContent();await settle();rejectRead(new Error('revoked'));await settle();assert.equal(page.data.contentReady,false);assert.equal(page.data.content,null);assert.equal(Object.keys(page.onShareAppMessage()).length,0);assert.equal(Object.keys(page.onShareTimeline()).length,0);assert.equal(hidden.length,2);assert.equal(preheats.length,1,'failed reload must not preheat');
 // 预热缓存键一致性：预热收到的对象必须与 onShareTimeline 返回值完全一致，否则首次分享拿不到缓存令牌
 assert.deepEqual(preheats[0],successShare);
});

test('reader WXML uses evaluated conditions for loading error consent media and sharing visibility',()=>{
 const s=fs.readFileSync(path.join(root,route+'.wxml'),'utf8');
 const conditions=[...s.matchAll(/wx:(?:if|elif)="([^"]*)"/g)].map(m=>m[1]);
 assert.ok(conditions.length>=8);
 conditions.forEach(value=>assert.match(value,/^\{\{.+\}\}$/,'unbound WXML condition would be a truthy literal: '+value));
 assert.ok(s.includes('wx:if="{{loading}}"'));assert.ok(s.includes('wx:elif="{{error}}"'));
});
