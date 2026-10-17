import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';
const require=createRequire(import.meta.url),React=require('react'),flush=()=>new Promise(r=>setImmediate(r));
const nodes=x=>Array.isArray(x)?x.flatMap(nodes):!x||typeof x!=='object'?[]:[x,...nodes(x.props?.children)];
const share={share_id:'share-1',occurred_at:'2026-10-16T14:46:11Z',route_key:'pages/contentreader/contentreader',content_id:'material-a',content_version:1,title:'600㎡别墅全案设计',type:'article',status:'published',channel:'app_message',brought_visitors:2,brought_views:3};
function harness(resolve,wrapper=false){
 const calls=[];let slots=[],index=0,pending=[],dirty=false,tree;
 const react={...React,useState(init){const i=index++;if(!(i in slots))slots[i]={value:typeof init==='function'?init():init};return[slots[i].value,v=>{const value=typeof v==='function'?v(slots[i].value):v;if(value!==slots[i].value){slots[i].value=value;dirty=true}}]},useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useEffect(fn,deps){const i=index++;const old=slots[i];if(!old||deps.some((v,j)=>v!==old.deps[j])){slots[i]={deps,cleanup:old?.cleanup};pending.push(()=>{slots[i].cleanup?.();slots[i].cleanup=fn()})}}};
 function load(file,deps){const module={exports:{}};const code=ts.transpileModule(fs.readFileSync(new URL('../src/pages/shareTracking/'+file,import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,jsx:ts.JsxEmit.ReactJSX,esModuleInterop:true,target:ts.ScriptTarget.ES2020}}).outputText;vm.runInNewContext(code,{module,exports:module.exports,AbortController,Error,Date,require:n=>n==='react'?react:n in deps?deps[n]:require(n)});return module.exports}
 const model={duration:x=>String(x),time:x=>String(x),intentLabels:{},identityBadge:()=>null,safeMediaURL:x=>x};
 const real=load('components.tsx',{'./model':model});
 const components={...real,Feedback:'Feedback',MediaPreview:'MediaPreview',Pager:'Pager',SnapshotInfo:'SnapshotInfo'};
 const sharesModel={'channelLabel':c=>c==='app_message'?'聊天私发':(c||'渠道未记录'),'pageLabel':k=>k||'其他页面','time':x=>String(x)};
 const api={visitorShares:async(visitor,params,signal)=>{calls.push({visitor,params,signal});return resolve?resolve(calls.at(-1)):{items:[share],as_of:'2026-10-17T00:00:00Z',metric_version:'v1',first_cursor:'first-fixed',next_cursor:'next-fixed'}}};
 const mod=load('visitorShares.tsx',{'../../api/shareTracking':{tracking:api},'./components':components,'./model':sharesModel});
 const props={visitor:'u:42',query:{owner:'owner-a',as_of:'2026-10-17T00:00:00Z'}};
 const render=()=>{const top=wrapper?mod.VisitorSharesPanel:mod.VisitorSharesList;let n=0;do{assert.ok(++n<15);dirty=false;index=0;tree=top(props);pending.splice(0).forEach(fn=>fn())}while(dirty);h.tree=tree;return tree};
 const h={calls,tree:undefined,render(){return render()},async settle(){for(let i=0;i<4;i++){h.render();await flush()}h.render()},find:type=>nodes(h.tree).find(n=>n.type===type),all:type=>nodes(h.tree).filter(n=>n.type===type),unmount(){slots.forEach(x=>x?.cleanup?.())}};
 h.render();return h;
}
const toggle=h=>h.all('button')[0];
const text=h=>JSON.stringify(h.tree);

test('转发面板默认收起，收起时不挂载列表也不发请求',()=>{
 const h=harness(undefined,true);
 assert.equal(h.calls.length,0);
 assert.equal(toggle(h).props['aria-expanded'],false);
 toggle(h).props.onClick();h.render();
 assert.equal(toggle(h).props['aria-expanded'],true);
 assert.equal(h.calls.length,0);
 toggle(h).props.onClick();h.render();
 assert.equal(toggle(h).props['aria-expanded'],false);
 assert.equal(h.calls.length,0);
});

test('转发列表拉取一次并继承筛选范围，返回保持 first_cursor/as_of/owner',async()=>{
 const h=harness();await h.settle();
 assert.equal(h.calls.length,1);
 assert.equal(h.calls[0].visitor,'u:42');
 assert.equal(h.calls[0].params.owner,'owner-a');
 assert.equal(h.calls[0].params.as_of,'2026-10-17T00:00:00Z');
 h.find('Pager').props.onNext('next-fixed');await h.settle();
 assert.equal(h.calls.at(-1).params.cursor,'next-fixed');
 h.find('Pager').props.onPrevious();await h.settle();
 assert.equal(h.calls.at(-1).params.cursor,'first-fixed');
 assert.equal(h.calls.at(-1).params.owner,'owner-a');
 assert.equal(h.calls.at(-1).params.as_of,'2026-10-17T00:00:00Z');
});

test('转发记录展示素材、渠道与下游流量文案',async()=>{
 const h=harness();await h.settle();
 assert.match(text(h),/600㎡别墅全案设计/);
 assert.match(text(h),/聊天私发/);
 assert.match(text(h),/带来访客/);
 assert.match(text(h),/人 · 浏览 /);
 assert.ok(text(h).includes('"brought_visitors":2') && text(h).includes('"brought_views":3'));
 assert.match(text(h),/多次点击分别计/);
});

test('未知渠道显示未记录而非空白，撤回素材带状态标注',async()=>{
 const h=harness(async()=>({items:[{...share,channel:undefined,status:'revoked'}],as_of:'2026-10-17T00:00:00Z',metric_version:'v1'}));
 await h.settle();
 assert.match(text(h),/渠道未记录/);
 assert.match(text(h),/素材已撤回/);
});

test('空转发记录显示空态；失败后重试保留筛选',async()=>{
 let fail=true;
 const h=harness(async()=>{if(fail)throw new Error('读取失败');return{items:[],as_of:'2026-10-17T00:00:00Z',metric_version:'v1'}});
 await h.settle();
 assert.match(h.find('Feedback').props.error,/读取失败/);
 assert.equal(h.find('Feedback').props.empty,false);
 fail=false;
 h.find('Feedback').props.retry();await h.settle();
 assert.equal(h.find('Feedback').props.empty,true);
});
