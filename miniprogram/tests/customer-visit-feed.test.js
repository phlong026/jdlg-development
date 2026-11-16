'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const root=path.join(__dirname,'../miniprogram/pages/sharevisits');
const source=()=>fs.readFileSync(path.join(root,'sharevisits.ts'),'utf8').replace(/^\/\/ @ts-nocheck\n/,'').replace(/\nexport \{\};\n?$/,'\n');

function runtime(feed,stubs={}){
 let definition;const navigation=[],clipboard=[],queries=[],warnings=[],actionSheets=[];
 let storageToken=stubs.storageToken||'login-token';
 const apiStub=Object.assign({
  ORIGIN:'https://api.jiadilingguang.com',
  listMyVisits:async query=>{queries.push(query);return{items:feed,as_of:'2026-11-13T08:00:00Z',next_cursor:'next-feed',share_window:{next_cursor:'older'}}},
  listMyVisitors:async()=>({items:[]}),listMyContentStats:async()=>({items:[]}),listMyShares:async()=>({items:[]}),
  downloadWorkbenchMedia:async id=>'temp://'+id,
 },stubs);
 const wx={getStorageSync:()=>storageToken,hideShareMenu(){},stopPullDownRefresh(){},navigateTo:o=>navigation.push(o.url),setClipboardData:o=>{clipboard.push(o.data);o.success&&o.success()},showToast(){},showActionSheet(options){actionSheets.push(options.itemList);if(stubs.actionSheetTap!==undefined)options.success({tapIndex:stubs.actionSheetTap})}};
 const protectedMedia=stubs.protectedMedia||{resolveProtectedMediaURL:async url=>'resolved:'+url};
 vm.runInNewContext(source(),{Page:o=>definition=o,wx,require:id=>id.includes('phase3-api')?apiStub:protectedMedia,Promise,Date,Number,String,Math,Set,encodeURIComponent,decodeURIComponent,setTimeout,clearTimeout,console:{log(){},warn(...args){warnings.push(args)},error(){}}});
 const page={...definition,data:structuredClone(definition.data),setData(value){Object.assign(this.data,value)}};
 return{page,navigation,clipboard,queries,warnings,actionSheets,setToken(value){storageToken=value}};
}

const feed=[
 {view_id:'v1',occurred_at:'2026-11-13T06:26:00Z',title:'现代简约庭院别墅设计方案',type:'article',route_key:'pages/contentreader/contentreader',content_id:'content-0001',content_version:2,visible_ms:26000,video_watch_ms:24000,progress_available:true,progress_pct:80,content_page_views:3,cover:{media_id:'cover-1',kind:'image'},source:{kind:'share',channel:'app_message'},visitor:{visitor_key:'u:7',nickname:'张先生',avatar_url:'https://avatar.example/7',identity_state:'identified',intent:{level:'high'}}},
 {view_id:'v2',occurred_at:'2026-11-13T05:48:00Z',title:'原木奶油风客厅',type:'video',visible_ms:18000,video_watch_ms:0,progress_available:false,content_page_views:2,cover:{url:'/media/2',kind:'image'},source:{kind:'share',channel:'timeline'},visitor:{visitor_key:'a:anon',nickname:'',identity_state:'anonymous',intent:{level:'unknown'}}},
];

test('客户查看默认按次加载近30天，并把卡片字段映射为可读文案',async()=>{
 const {page,queries}=runtime(feed);page.onLoad({});page._token='login-token';page._epochCustomers=1;
 assert.equal(page.data.customerViewMode,'visits');
 assert.equal(page.data.rangeIndex,3);
 await page.loadCustomerVisits(true,1);
 assert.equal(queries.length,1);
 assert.ok(queries[0].start&&queries[0].end);
 assert.equal(page.data.customerVisits.length,2);
 const video=page.data.customerVisits[0],article=page.data.customerVisits[1];
 assert.equal(video.nickname,'张先生');
 assert.equal(video.occurred_text,'11-13 14:26');
 assert.equal(video.source_text,'微信分享');
 assert.equal(video.duration_text,'本次观看24秒');
 assert.equal(video.progress_text,'进度80%');
 assert.equal(video.views_text,'累计浏览3次');
 assert.equal(article.nickname,'匿名访客');
 assert.equal(article.can_copy_name,false);
 assert.equal(article.source_text,'朋友圈');
 assert.equal(article.duration_text,'本次停留18秒');
 assert.equal(article.progress_text,'');
 assert.equal(article.media_kind,'video');assert.equal(article.media_icon,'▶');
});

test('按次筛选在服务端分页前传递客户、意向、来源，自定义日期包含结束日',async()=>{
 const {page,queries}=runtime([]);page._token='login-token';page._epochCustomers=1;
 page.setData({customerQuery:'张',customerIntent:'high',customerChannel:'timeline',customStartDate:'2026-11-01',customEndDate:'2026-11-13'});
 await page.applyCustomRange();
 const query=queries.at(-1);
 assert.equal(query.q,'张');assert.equal(query.intent,'high');assert.equal(query.channel,'timeline');
 assert.equal(query.start,'2026-10-31T16:00:00.000Z');
 assert.equal(query.end,'2026-11-13T16:00:00.000Z');
});

test('搜索确认的事件对象不会被误当 epoch，非法自定义日期不发请求',async()=>{
 const {page,queries}=runtime(feed);page._token='login-token';
 await page.reloadCustomerView({type:'confirm'});
 assert.equal(page.data.customerVisits.length,2);
 const before=queries.length;
 page.setData({customStartDate:'2026-02-31',customEndDate:'2026-03-02'});
 await page.applyCustomRange();
 assert.equal(queries.length,before);
});

test('默认近30天在页面初始化时重算，分页按 view_id 去重',async()=>{
 let calls=0;
 const {page}=runtime([],{listMyVisits:async()=>calls++===0?{items:[feed[0]],as_of:'2026-11-13T08:00:00Z',next_cursor:'next'}:{items:[feed[0],feed[1]],as_of:'2026-11-13T08:00:00Z'}});
 page.setData({filterStart:'2000-01-01T00:00:00.000Z',filterEnd:'2000-02-01T00:00:00.000Z'});
 page.onLoad({});
 assert.notEqual(page.data.filterStart,'2000-01-01T00:00:00.000Z');
 page._token='login-token';page._epochCustomers=1;
 await page.loadCustomerVisits(true,1);await page.loadCustomerVisits(false);
 assert.deepEqual(page.data.customerVisits.map(item=>item.visit_key),['v1','v2']);
});

test('快速加载下一页不会作废上一页尚在解析的封面',async()=>{
 let releaseFirstCover,calls=0;
 const firstCover=new Promise(resolve=>{releaseFirstCover=resolve});
 const first={...feed[0],cover:{url:'https://cdn.example/first.jpg',kind:'image'}},second={...feed[1],view_id:'v3',cover:{url:'https://cdn.example/second.jpg',kind:'image'}};
 const protectedMedia={resolveProtectedMediaURL:url=>url.includes('first')?firstCover:Promise.resolve(url)};
 const listMyVisits=async()=>calls++===0?{items:[first],next_cursor:'next',as_of:'2026-11-13T08:00:00Z'}:{items:[second],as_of:'2026-11-13T08:00:00Z'};
 const {page}=runtime([],{protectedMedia,listMyVisits});page._token='login-token';page._epochCustomers=1;
 await page.loadCustomerVisits(true,1);await page.loadCustomerVisits(false);
 releaseFirstCover('https://cdn.example/first.jpg');await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(page.data.customerVisits.find(item=>item.visit_key==='v1').cover,'https://cdn.example/first.jpg');
});

test('客户查看顶部菜单可真实切换查看记录与按客户汇总',async()=>{
 const aggregate=[];
 const {page,actionSheets}=runtime([],{actionSheetTap:1,listMyVisitors:async query=>{aggregate.push(query);return{items:[]}}});
 page._token='login-token';page._epochCustomers=1;
 page.setData({activeTab:'materials'});
 page.chooseCustomerView();await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(JSON.stringify(actionSheets),JSON.stringify([['查看记录','按客户汇总']]));
 assert.equal(page.data.customerViewMode,'people');assert.equal(aggregate.length,1);
 assert.equal(page.data.activeTab,'customers');
});

test('从分享记录选择当前查看模式仍会回到客户查看并加载',async()=>{
 const {page,queries}=runtime(feed,{actionSheetTap:0});
 page._token='login-token';page.setData({activeTab:'shares',customerViewMode:'visits'});
 page.chooseCustomerView();await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(page.data.activeTab,'customers');
 assert.equal(queries.length,1);assert.equal(page.data.customerVisits.length,2);
});

test('访问卡头像进入既有客户详情并携带筛选快照，复制只复制已展示实名',async()=>{
 const {page,navigation,clipboard}=runtime(feed);page._token='login-token';page._epochCustomers=1;await page.loadCustomerVisits(true,1);
 page.openVisitCustomer({currentTarget:{dataset:{index:0}}});
 assert.match(navigation[0],/^\/pages\/visitdetail\/visitdetail\?visitor=u%3A7/);
 assert.match(navigation[0],/&as_of=2026-11-13T08%3A00%3A00Z/);
 page.copyVisitCustomerName({currentTarget:{dataset:{index:0}}});
 page.copyVisitCustomerName({currentTarget:{dataset:{index:1}}});
 assert.deepEqual(clipboard,['张先生']);
});

test('素材榜先进入受固定版本约束的客户列表，分享批次保留为次入口',async()=>{
 const {page,navigation}=runtime([]);page.onLoad({});page.setData({materials:[{content_id:'content-0001',content_version:2,route_key:'pages/contentreader/contentreader',title:'庭院方案'}],rangeIndex:2,filterStart:'2026-11-01T00:00:00Z',filterEnd:'2026-11-08T00:00:00Z',materials_as_of:'2026-11-08T00:00:00Z',shareWindowCursor:'parent-window',shareWindowIndex:2});
 page.openMaterial({currentTarget:{dataset:{index:0}}});page.openMaterialShares({currentTarget:{dataset:{index:0}}});
 assert.match(navigation[0],/tab=customers&view_mode=people/);assert.match(navigation[0],/route_key=pages%2Fcontentreader%2Fcontentreader/);assert.match(navigation[0],/content_version=2/);
 assert.match(navigation[0],/share_cursor=parent-window/);assert.doesNotMatch(navigation[0],/share_window_index=/);assert.match(navigation[0],/as_of=2026-11-08T00%3A00%3A00Z/);
 assert.match(navigation[1],/tab=shares/);
 page.onLoad({tab:'customers',view_mode:'people',share_cursor:'parent-window',share_window_index:'2'});
 page.acceptShareWindow({as_of:'2026-11-08T00:00:00Z',share_window:{current_cursor:'material-window'}});
 assert.equal(page.data.shareWindowCursor,'material-window');assert.equal(page._shareWindows[0],'material-window');
 page.setData({activeTab:'customers',customerViewMode:'people',contentFilter:'content-0001',contentVersion:2,contentRoute:'pages/contentreader/contentreader'});
 await page.clearContentFilter();assert.equal(page.data.shareWindowCursor,'');assert.deepEqual(Array.from(page._shareWindows),['']);
});

test('素材访客查询和客户详情持续携带route、content与version，标题进入管理预览',async()=>{
 const visitorQueries=[];const {page,navigation}=runtime(feed,{listMyVisitors:async query=>{visitorQueries.push(query);return{items:[{visitor_key:'u:7',nickname:'张先生'}],as_of:'2026-11-13T08:00:00Z'}}});
 page.onLoad({tab:'customers',view_mode:'people',route_key:'pages%2Fcontentreader%2Fcontentreader',content_id:'content-0001',content_version:'2',title:'庭院方案'});page._token='login-token';page._epochCustomers=1;
 await page.loadCustomers(true,1);assert.equal(visitorQueries[0].route_key,'pages/contentreader/contentreader');assert.equal(visitorQueries[0].content_id,'content-0001');assert.equal(visitorQueries[0].content_version,2);
 page.openCustomer({currentTarget:{dataset:{index:0}}});assert.match(navigation[0],/content_id=content-0001/);assert.match(navigation[0],/route_key=pages%2Fcontentreader%2Fcontentreader/);
 page.setData({customerVisits:[{...page.data.customerVisits[0],route_key:'pages/contentreader/contentreader',content_id:'content-0001',content_version:2,can_manage_preview:true}]});page.openVisitContent({currentTarget:{dataset:{index:0}}});assert.match(navigation[1],/management_preview=1/);assert.match(navigation[1],/content_version=2/);
});

test('首次挂载保留素材路由的as_of，实际切换账号才清理窗口与旧数据',async()=>{
 const visitorQueries=[];let calls=0;
 const routeAsOf='2026-11-15T02:15:47Z';
 const harness=runtime([],{listMyVisitors:async query=>{visitorQueries.push(query);calls+=1;return{items:[{visitor_key:calls===1?'u:7':'u:8',nickname:calls===1?'张先生':'李先生'}],as_of:query.as_of||'2026-11-15T02:15:52Z'}}});
 harness.page.onLoad({tab:'customers',view_mode:'people',content_id:'content-0001',content_version:'2',as_of:routeAsOf,share_cursor:'material-window'});
 harness.page.onShow();await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(visitorQueries[0].as_of,routeAsOf);
 assert.equal(harness.page.data.shareWindowCursor,'material-window');
 assert.equal(harness.page.data.customers.length,1);

 harness.setToken('account-two');
 harness.page.onShow();
 assert.equal(harness.page.data.customers.length,0,'切账号时先清除旧账号列表');
 await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(visitorQueries[1].as_of,undefined);
 assert.equal(harness.page.data.shareWindowCursor,'');
 assert.equal(harness.page._windowAsOf,'2026-11-15T02:15:52Z');
 assert.deepEqual(Array.from(harness.page.data.customers.map(item=>item.visitor_key)),['u:8'],'切账号后只能显示新请求结果');
});

test('分享Tab首次保留路由as_of，切账号后不得复用旧时间快照',async()=>{
 const shareQueries=[];
 const routeAsOf='2026-11-15T02:15:47Z';
 const harness=runtime([],{listMyShares:async query=>{shareQueries.push(query);return{items:[],as_of:query.as_of||'2026-11-15T02:15:52Z'}}});
 harness.page.onLoad({tab:'shares',as_of:routeAsOf});
 harness.page.onShow();await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(shareQueries[0].as_of,routeAsOf);

 harness.setToken('account-two');
 harness.page.onShow();await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(shareQueries[1].as_of,undefined);
 assert.equal(harness.page._drillAsOf,'');
});

test('合集和缺少固定版本的内容不绑定管理预览，内容区域回退客户卡入口',()=>{
 const {page,navigation}=runtime([]);page.setData({customerVisits:[
  {route_key:'pages/collectionreader/collectionreader',content_id:'collection-1',content_version:2,can_manage_preview:false},
  {route_key:'pages/contentreader/contentreader',content_id:'content-1',content_version:0,can_manage_preview:false},
 ]});
 page.openVisitContent({currentTarget:{dataset:{index:0}}});page.openVisitContent({currentTarget:{dataset:{index:1}}});
 assert.equal(navigation.length,0);
 const wxml=fs.readFileSync(path.join(root,'sharevisits.wxml'),'utf8');
 assert.match(wxml,/catchtap="{{item\.can_manage_preview \? 'openVisitContent' : 'openVisitCustomer'}}"/);
});

test('封面解析失败只降级单卡，不阻塞访问列表',async()=>{
 const {page,warnings}=runtime(feed,{downloadWorkbenchMedia:async()=>{throw Object.assign(new Error('private url must not log'),{statusCode:403})}});page._token='login-token';page._epochCustomers=1;await page.loadCustomerVisits(true,1);
 await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(page.data.customerVisits.length,2);
 assert.equal(page.data.customerVisits[0].cover_error,true);
 assert.equal(warnings[0][1].view_id,'v1');assert.equal(warnings[0][1].status,403);assert.doesNotMatch(JSON.stringify(warnings),/private url/);
});

test('封面同时有 url 和 media_id 时优先读取可发布URL，不要求素材管理权限',async()=>{
 let downloads=0,resolvedURL='';
 const relativeURL='/api/v1/miniprogram/tracking/media/1234567890abcdef1234567890abcdef';
 const withPublishedCover=[{...feed[0],cover:{media_id:'private-media',url:relativeURL,kind:'image'}}];
 const protectedMedia={resolveProtectedMediaURL:async url=>{resolvedURL=url;if(!url.startsWith('https://'))throw new Error('素材地址无效');return url}};
 const {page}=runtime(withPublishedCover,{protectedMedia,downloadWorkbenchMedia:async()=>{downloads++;throw new Error('403')}});page._token='login-token';page._epochCustomers=1;await page.loadCustomerVisits(true,1);
 await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(downloads,0);assert.equal(resolvedURL,'https://api.jiadilingguang.com'+relativeURL);assert.equal(page.data.customerVisits[0].cover,resolvedURL);
});

test('WXML保留三个业务视角并以逐次卡为主视图',()=>{
 const wxml=fs.readFileSync(path.join(root,'sharevisits.wxml'),'utf8');
 assert.match(wxml,/>客户查看</);assert.match(wxml,/>素材跟踪</);assert.match(wxml,/>分享记录</);
 assert.match(wxml,/customerVisits/);assert.match(wxml,/item\.progress_text/);assert.match(wxml,/item\.source_text/);
 assert.match(wxml,/item\.media_icon/);
 assert.match(wxml,/binderror="onVisitCoverError"/);
 assert.match(wxml,/catchtap="chooseCustomerView"/);assert.doesNotMatch(wxml,/class="feed-toolbar"/);
 assert.match(wxml,/class="customer-filters"[\s\S]*customerViewMode === 'visits'[\s\S]*sourceOptions[\s\S]*sortOptions/);
 assert.match(wxml,/wx:for="{{rangeOptions}}"/);assert.match(source(),/['"]自定义['"]/);assert.match(source(),/按客户汇总/);
});
