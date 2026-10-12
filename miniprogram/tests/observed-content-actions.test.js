'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const routes=require('../miniprogram/utils/tracking/routes');
const root=path.join(__dirname,'../miniprogram');
function harness(name,{enabled=true,throwEmit=false}={}) {
 const events=[],navigations=[],previews=[];
 const tracker={diagnostics:()=>({enabled}),emit(page,type,extra){if(throwEmit)throw Error('analytics unavailable');events.push({type,...extra});}};
 const adapterContext={module:{exports:{}},require:n=>n==='./core'?{createTracker:()=>tracker}:n==='./routes'?routes:{},wx:{getPerformance:()=>({now:()=>0})},Date,Math,Set};
 vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/index.js'),'utf8'),adapterContext);
 const adapter=adapterContext.module.exports;adapter.getSDK();
 let definition;
 const deps=new Proxy({}, {get:(_,key)=>key==='resolveAssetPath'||key==='normalizeCosUrl'?v=>v:()=>{}});
 const context={exports:{},require:()=>deps,Page:d=>{definition=d;},wx:{getStorageSync:()=>'',navigateTo:o=>navigations.push(o.url),previewImage:o=>previews.push(o),showToast(){}},Set,Promise,Date,Math,setTimeout:()=>0,clearTimeout(){}};
 vm.runInNewContext(fs.readFileSync(path.join(root,`pages/${name}/${name}.js`),'utf8'),context);
 const wrapped=adapter.wrapPage(definition);
 const page={...wrapped,route:`pages/${name}/${name}`,data:{...definition.data},setData(values){Object.assign(this.data,values);}};
 return {page,events,navigations,previews,adapter,tap(method,dataset={}){return page[method]({currentTarget:{dataset}});}};
}
const scenarios=[
 ['index','onFeaturedCaseTap',{id:7},'templatesquaredetails','id','7'],
 ['template','onCardTap',{id:7},'templatesquaredetails','id','7'],
 ['template','onCardTap',{id:7,targetType:'inspiration'},'inspirationdetail','id','7'],
 ['utilitytools','onToolTap',{id:7},'utilitytooldetail','id','7'],
 ['aitools','onToolTap',{id:'architecture_render'},'aitooldetail','id','architecture_render'],
 ['distribution','onWorkTap',{id:7},'templatesquaredetails','id','7'],
 ['distribution','onWorkTap',{id:7,userId:9,publishScope:'homepage'},'designerhome','userId','9'],
 ['distribution','onDesignerTap',{userId:9},'designerhome','userId','9'],
 ['designerhome','onWorkTap',{id:7},'templatesquaredetails','id','7'],
 ['designerhome','onReviewRelatedTap',{id:7},'templatesquaredetails','id','7'],
 ['templatesquaredetails','onDesignerTap',{},'designerhome','userId','9'],
];
for(const [source,method,dataset,target,key,value] of scenarios)test(`actual ${source}.${method} binds observed target ${target}`,()=>{
 const h=harness(source);h.page.data.userInfo={userId:9};h.tap(method,dataset);
 assert.equal(h.navigations[0],`/pages/${target}/${target}?${key}=${value}`);
 assert.equal(h.events.length,1);assert.equal(h.events[0].type,'content_click');
 assert.equal(h.events[0].target_content_id,routes.binding(`pages/${target}/${target}`,{[key]:value}).id);
 assert.equal(h.events[0].action_key,method);assert.ok(h.events[0].component_key);
 assert.deepEqual(Object.keys(h.events[0]).sort(),['action_key','component_key','target_content_id','type']);
});
test('actual rejected/invalid IDs produce no target click',()=>{
 for(const source of ['utilitytools','aitools']){const h=harness(source);h.tap('onToolTap',{});assert.equal(h.navigations.length,0);assert.equal(h.events.length,0);}
 for(const id of ['person@example.com','tel:123','',0,-1,Infinity,{},'a&phone=123']){const h=harness('template');h.tap('onCardTap',{id});assert.equal(h.events.length,0);}
});
test('homepage-only work preview and self-profile never invent public target reads',()=>{
 const h=harness('designerhome');h.tap('onWorkTap',{id:7,publishScope:'homepage',image:'https://private.invalid/photo'});assert.equal(h.previews.length,1);assert.equal(h.events.length,0);
 h.page.data.isSelfHomepage=true;h.tap('onWorkTap',{id:7});assert.equal(h.navigations.length,1);assert.equal(h.events.length,0);
});
for(const source of ['inspirationdetail','templatesquaredetails'])test(`actual ${source} image preview emits only source action keys`,()=>{
 const h=harness(source);h.page.data.detail={images:['https://cdn.invalid/source']};h.page.data.imageList=['https://cdn.invalid/source'];
 h.tap('onPreviewImage',{url:'https://cdn.invalid/source',index:0});assert.equal(h.previews.length,1);
 assert.deepEqual(h.events,[{type:'image_open',component_key:'content_image',action_key:'onPreviewImage'}]);
 h.page.data.detail.images=[];h.page.data.imageList=[];h.tap('onPreviewImage',{});assert.equal(h.events.length,1);
});
test('disabled or broken analytics preserves actual navigation callback',()=>{
 for(const options of [{enabled:false},{throwEmit:true}]){const h=harness('utilitytools',options);h.tap('onToolTap',{id:7});assert.deepEqual(h.navigations,['/pages/utilitytooldetail/utilitytooldetail?id=7']);assert.equal(h.events.length,0);}
});
test('observed wrapper preserves receiver, every argument, return and thrown business error',()=>{
 const h=harness('utilitytools');const result={},args=[{currentTarget:{dataset:{id:7}}},{business:'extra'}];let seen;
 const wrapped=h.adapter.wrapPage({onToolTap(...values){seen={receiver:this,values};return result;}});
 assert.equal(wrapped.onToolTap.apply(h.page,args),result);assert.equal(seen.receiver,h.page);assert.deepEqual(seen.values,args);
 const error=new Error('business');assert.throws(()=>h.adapter.wrapPage({onToolTap(){throw error;}}).onToolTap.apply(h.page,args),e=>e===error);
});
test('allowlist ignores matching private callback names, unrelated methods and list exposure',()=>{
 const h=harness('utilitytools');assert.equal(h.events.length,0);h.page.route='pages/myfavorites/myfavorites';h.tap('onToolTap',{id:7});assert.equal(h.events.length,0);
 assert.equal(routes.observedAction('pages/template/template','onLoad',{data:{}},{currentTarget:{dataset:{id:7}}}),null);
});
test('real SDK records source-bound click only after consent/readiness and never creates target PV',async()=>{
 const {createTracker}=require('../miniprogram/utils/tracking/core');let seq=0;
 const sdk=createTracker({wall:()=>1700000000000,mono:()=>1,id:()=>`action_${++seq}`,random:()=>.5,account:()=>'',load:()=>[],save(){},entry:async()=>({entry_token:'credential',session_id:'source_session',content_accessible:true}),send:async()=>{throw Error('offline');}});
 const page={data:{}},route='pages/template/template';sdk.register(page,route,{});sdk.appShow();sdk.show(page);
 const action=routes.observedAction(route,'onCardTap',page,{currentTarget:{dataset:{id:7}}});
 sdk.emit(page,action.type,action.extra);assert.equal(sdk.queueForTest().length,0);
 sdk.configure(true,'notice-v1');sdk.emit(page,action.type,action.extra);assert.equal(sdk.queueForTest().length,0);
 sdk.ready(page,true);await new Promise(resolve=>setImmediate(resolve));sdk.emit(page,action.type,action.extra);
 const events=sdk.queueForTest().map(r=>r.event),click=events.find(e=>e.event_type==='content_click'),views=events.filter(e=>e.event_type==='page_view');
 assert.equal(views.length,1);assert.equal(views[0].route_key,route);assert.equal(click.route_key,route);assert.equal(click.page_view_id,views[0].page_view_id);assert.equal(click.target_content_id,action.extra.target_content_id);
 sdk.configure(false,'');sdk.emit(page,action.type,action.extra);assert.equal(sdk.queueForTest().length,0);
});
test('raw-ID callbacks preserve target binding bytes while numeric callbacks normalize like navigation',()=>{
 for(const source of ['index','utilitytools']){const h=harness(source);h.tap(source==='index'?'onFeaturedCaseTap':'onToolTap',{id:'007'});const target=source==='index'?'templatesquaredetails':'utilitytooldetail';assert.equal(h.events[0].target_content_id,routes.binding(`pages/${target}/${target}`,{id:'007'}).id);assert.match(h.navigations[0],/id=007$/);}
 const h=harness('template');h.tap('onCardTap',{id:'007'});assert.equal(h.events[0].target_content_id,routes.binding('pages/templatesquaredetails/templatesquaredetails',{id:7}).id);assert.match(h.navigations[0],/id=7$/);
});
