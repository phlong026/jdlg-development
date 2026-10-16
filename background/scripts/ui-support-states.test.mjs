// Production TSX handlers with fake network only; no backend writes or browser claims.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import ts from 'typescript';
import React from 'react';
const require = createRequire(import.meta.url);
const tick = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((y,n) => { resolve=y; reject=n; }); return { promise, resolve, reject }; };
const item = (id, extra={}) => ({id,user_id:id,username:`User${id}`,type:'complaint',source_type:'manual',source_id:'',title:`Ticket${id}`,content:'Description',resolution_note:'Saved note',priority:'medium',status:'open',assignee_id:0,assignee_name:'',created_by:'Fixture',created_at:'2026-10-11',updated_at:'2026-10-11',...extra});
const overview = {total_count:21,open_count:20,in_progress_count:1,closed_count:0,high_priority_count:1,today_count:2,overdue_count:3};
const text = node => Array.isArray(node) ? node.map(text).join('') : node == null || typeof node==='boolean' ? '' : typeof node!=='object' ? String(node) : ['children','controls','actions','summary'].map(k=>text(node.props?.[k])).join('');
function nodes(tree) { if(Array.isArray(tree)) return tree.flatMap(nodes); if(!tree || typeof tree!=='object') return []; return [tree,...['children','controls','actions','summary'].flatMap(k=>nodes(tree.props?.[k]))]; }
function runtime(overrides={}) {
 const h={slots:[],cursor:0,pending:[],tree:null,reads:[],details:[],writes:[],notices:[],recovered:0,confirm:true,confirms:[]};
 const api={
  getSupportTicketOverview:async()=>overview,
  getSupportTicketList:async p=>({list:p.page===1?[item(1),item(2)]:[item(21)],total:21,page:p.page,page_size:20}),
  getSupportTicketDetail:async id=>item(Number(id)),
  assignSupportTicket:async()=>{},updateSupportTicketStatus:async()=>{},updateSupportTicketResolutionNote:async()=>{},createSupportTicket:async()=>31,syncSupportTicketSystemExceptions:async()=>2,
  ...overrides,
 };
 h.api=api;
 const wrapped=Object.fromEntries(Object.keys(api).map(name=>[name,(...args)=>{
  if(name==='getSupportTicketList') h.reads.push(args[0]);
  else if(name==='getSupportTicketDetail') h.details.push(args[0]);
  else if(!name.startsWith('get')) h.writes.push({name,args});
  return api[name](...args);
 }]));
 const react={...React,
  useState(initial) { const i=h.cursor++;if(!(i in h.slots)) h.slots[i]=typeof initial==='function'?initial():initial;return [h.slots[i],v=>h.slots[i]=typeof v==='function'?v(h.slots[i]):v]; },
  useRef(initial) { const i=h.cursor++;if(!(i in h.slots)) h.slots[i]={current:initial};return h.slots[i]; },
  useEffect(fn,deps) { const i=h.cursor++,prior=h.slots[i];if(!prior||deps.some((v,j)=>!Object.is(v,prior.deps[j]))) {h.slots[i]={deps,cleanup:prior?.cleanup};h.pending.push(()=>{h.slots[i].cleanup?.();h.slots[i].cleanup=fn();});} },
 };
 const mod={exports:{}};
 const code=ts.transpileModule(fs.readFileSync(new URL('../src/pages/supportTickets.tsx',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020,jsx:ts.JsxEmit.ReactJSX,esModuleInterop:true}}).outputText;
 vm.runInNewContext(code,{module:mod,exports:mod.exports,console:{error(){}},window:{confirm:m=>{h.confirms.push(m);return h.confirm;}},require(name){
  if(name==='react')return react;if(name==='react/jsx-runtime')return require(name);
  if(name==='react-router-dom')return {useNavigate:()=>()=>{}};
  if(name==='react-icons/fi')return new Proxy({},{get:()=>()=>null});
  if(name==='../api/supportTickets')return wrapped;
  if(name==='../utils/pageLoadError')return {reportPageLoadError:()=>h.notices.push('load-error'),reportPageLoadRecovered:()=>h.recovered++};
  if(name==='../component/managementSearchPanel')return ()=>null;
  if(name.includes('useIsMobile'))return {useIsMobile:()=>false};
  if(name.endsWith('.scss'))return {};
  throw Error(name);
 }});
 h.render=()=>{h.cursor=0;h.tree=mod.exports.default();for(const f of h.pending.splice(0)) f();return h.tree;};
 h.settle=async()=>{for(let i=0;i<3;i++){await tick();h.render();}};
 h.find=(predicate,scope=h.tree)=>{const found=nodes(scope).filter(predicate);assert.equal(found.length,1,`Expected one node, found ${found.length}`);return found[0];};
 h.button=(label,scope)=>h.find(n=>n.type==='button'&&text(n).trim()===label,scope);
 h.row=id=>h.find(n=>n.type==='tr'&&text(n).includes(`Ticket${id}`));
 h.dialog=()=>h.find(n=>n.props?.['aria-labelledby']==='support-detail-title');
 h.create=()=>h.find(n=>n.props?.['aria-labelledby']==='support-create-title');
 h.click=(label,scope)=>{const n=h.button(label,scope);assert.notEqual(n.props.disabled,true,label);const promise=n.props.onClick();h.render();return promise;};
 h.note=value=>{h.find(n=>n.type==='textarea'&&n.props.className==='detail-note-textarea').props.onChange({target:{value}});h.render();};
 h.noteValue=()=>h.find(n=>n.type==='textarea'&&n.props.className==='detail-note-textarea').props.value;
 h.open=async id=>{h.click('详情',h.row(id));await h.settle();};
 h.close=()=>{h.find(n=>n.type==='button'&&n.props['aria-label']==='关闭工单详情').props.onClick();h.render();};
 h.unmount=()=>{for(const slot of h.slots) if(slot?.cleanup)slot.cleanup();};
 h.render();return h;
}
async function ready(overrides){const h=runtime(overrides);await h.settle();return h;}

test('support: loading/error/empty are distinct, retry clears the failure',async()=>{
 const first=deferred();const h=runtime({getSupportTicketList:()=>first.promise});
 assert.match(text(h.tree),/正在加载工单/);assert.doesNotMatch(text(h.tree),/暂无工单数据/);
 first.reject(Error('offline'));await h.settle();assert.match(text(h.tree),/工单未能加载/);assert.doesNotMatch(text(h.tree),/暂无工单数据/);
 h.api.getSupportTicketList=async()=>({list:[],total:0});h.click('重试加载');await h.settle();assert.match(text(h.tree),/暂无工单数据/);assert.doesNotMatch(text(h.tree),/工单未能加载/);assert.equal(h.recovered,1);
});
test('support: real pagination and filter reset, repeated same filter still reloads',async()=>{
 const h=await ready();h.click('下一页');await h.settle();assert.equal(h.reads.at(-1).page,2);assert.match(text(h.tree),/Ticket21/);
 h.click('今日新增2');await h.settle();assert.equal(h.reads.at(-1).page,1);assert.equal(h.reads.at(-1).status,'today');
 const select=h.find(n=>n.type==='select'&&n.props.value==='today');assert.ok(nodes(select).some(n=>n.type==='option'&&n.props.value==='today'));
 const count=h.reads.length;h.click('今日新增2');await h.settle();assert.equal(h.reads.length,count+1);assert.doesNotMatch(text(h.tree),/正在加载工单/);
});
test('support: old list response cannot replace a newer filter',async()=>{
 const h=await ready();const a=deferred(),b=deferred();h.api.getSupportTicketList=p=>p.status==='today'?a.promise:b.promise;
 h.click('今日新增2');h.click('超48小时3');b.resolve({list:[item(9)],total:1});await h.settle();a.resolve({list:[item(8)],total:1});await h.settle();assert.match(text(h.tree),/Ticket9/);assert.doesNotMatch(text(h.tree),/Ticket8/);
});
test('support: A detail response cannot replace later B',async()=>{
 const h=await ready();const a=deferred(),b=deferred();h.api.getSupportTicketDetail=id=>id==='1'?a.promise:b.promise;
 h.click('详情',h.row(1));h.click('详情',h.row(2));b.resolve(item(2));await h.settle();a.resolve(item(1));await h.settle();assert.match(text(h.dialog()),/Ticket2/);assert.doesNotMatch(text(h.dialog()),/Ticket1/);
});
test('support: assignment and status refresh preserve unsaved note',async()=>{
 const h=await ready();await h.open(1);h.note('Unsubmitted work');h.api.getSupportTicketDetail=async()=>item(1,{resolution_note:'Server old',assignee_name:'Owner'});
 h.click('分配给我',h.dialog());await h.settle();assert.equal(h.noteValue(),'Unsubmitted work');
 h.click('开始处理',h.dialog());await h.settle();assert.equal(h.noteValue(),'Unsubmitted work');assert.equal(h.writes.length,2);
});
test('support: note save acknowledges only its snapshot and preserves later typing',async()=>{
 const pending=deferred();const h=await ready({updateSupportTicketResolutionNote:()=>pending.promise});await h.open(1);h.note('First draft');h.click('保存备注',h.dialog());h.note('Later draft');
 pending.resolve();await h.settle();assert.equal(h.noteValue(),'Later draft');assert.deepEqual(h.writes[0].args,['1','First draft']);assert.match(text(h.tree),/处理备注已保存/);
});
test('support: write succeeds but reread fails; no duplicate write or false failure',async()=>{
 const h=await ready();await h.open(1);h.note('Draft');h.api.getSupportTicketDetail=async()=>{throw Error('read offline');};h.click('保存备注',h.dialog());await h.settle();
 assert.match(text(h.tree),/处理备注已保存/);assert.match(text(h.tree),/read offline/);assert.doesNotMatch(text(h.tree),/操作未完成/);assert.equal(h.writes.length,1);
 h.api.getSupportTicketDetail=async()=>item(1,{resolution_note:'Draft'});h.click('重新读取详情');await h.settle();assert.equal(h.writes.length,1);assert.equal(h.noteValue(),'Draft');
});
test('support: pending note lock blocks duplicate, close and switching target',async()=>{
 const pending=deferred();const h=await ready({updateSupportTicketResolutionNote:()=>pending.promise});await h.open(1);h.note('Draft');const save=h.button('保存备注',h.dialog()).props.onClick;
 save();save();h.render();h.close();h.button('详情',h.row(2)).props.onClick();h.render();assert.match(text(h.dialog()),/Ticket1/);assert.equal(h.writes.length,1);
 pending.reject(Error('offline'));await h.settle();assert.equal(h.noteValue(),'Draft');assert.match(text(h.tree),/原输入已保留/);assert.equal(h.button('保存备注',h.dialog()).props.disabled,false);
});
test('support: dismiss dirty note has explicit keep/discard behavior',async()=>{
 const h=await ready();await h.open(1);h.note('Draft');h.confirm=false;h.close();assert.equal(h.noteValue(),'Draft');assert.equal(h.confirms.length,1);
 h.confirm=true;h.close();assert.equal(nodes(h.tree).filter(n=>n.props?.['aria-labelledby']==='support-detail-title').length,0);await h.open(2);assert.equal(h.confirms.length,2);assert.equal(h.noteValue(),'Saved note');
});
test('support: created ticket stays successful after list failure; duplicate and close are locked',async()=>{
 const pending=deferred();const h=await ready({createSupportTicket:()=>pending.promise});h.click('新建工单');const title=h.find(n=>n.type==='input'&&n.props.placeholder?.includes('付款后'),h.create());title.props.onChange({target:{value:'New ticket'}});h.render();
 const submit=h.button('创建工单',h.create()).props.onClick;submit();submit();h.render();h.find(n=>n.props?.['aria-label']==='关闭新建工单').props.onClick();h.render();assert.ok(h.create());assert.equal(h.writes.length,1);
 h.api.getSupportTicketList=async()=>{throw Error('refresh offline');};pending.resolve(31);await h.settle();assert.match(text(h.tree),/工单 31 已创建/);assert.match(text(h.tree),/工单未能加载/);assert.doesNotMatch(text(h.tree),/创建工单未完成/);assert.equal(h.writes.length,1);
});
test('support: same-frame sync is issued once and successful refresh cannot look like write failure',async()=>{
 const pending=deferred();const h=await ready({syncSupportTicketSystemExceptions:()=>pending.promise});const click=h.button('同步异常').props.onClick;click();click();h.render();assert.equal(h.writes.length,1);
 h.api.getSupportTicketList=async()=>{throw Error('offline');};pending.resolve(4);await h.settle();assert.match(text(h.tree),/本次新增 4 条/);assert.doesNotMatch(text(h.tree),/同步系统异常工单未完成/);
});
test('support: unmounted pending mutation cannot trigger new reads',async()=>{
 const pending=deferred();const h=await ready({assignSupportTicket:()=>pending.promise});await h.open(1);h.click('分配给我',h.dialog());const reads=h.reads.length,details=h.details.length;h.unmount();pending.resolve();await tick();await tick();assert.equal(h.reads.length,reads);assert.equal(h.details.length,details);
});


test('support: old save handler cannot submit after closing and reopening the same ticket',async()=>{
 const h=await ready();await h.open(1);h.note('Old draft');const oldSave=h.button('保存备注',h.dialog()).props.onClick;h.close();await h.open(1);h.note('New draft');oldSave();await h.settle();assert.equal(h.writes.length,0);assert.equal(h.noteValue(),'New draft');
});
test('support: a now-empty last page returns to the last valid page',async()=>{
 const h=await ready();h.api.getSupportTicketList=async p=>({list:p.page===2?[]:[item(1)],total:20,page:p.page});h.click('下一页');await h.settle();assert.deepEqual(h.reads.slice(-2).map(p=>p.page),[2,1]);assert.match(text(h.tree),/Ticket1/);assert.doesNotMatch(text(h.tree),/正在加载工单/);
});
test('support: source status uses business labels and original values remain in collapsed diagnostics',async()=>{
 const h=await ready({getSupportTicketDetail:async()=>item(1,{source_type:'task',source_id:'image-8',source_payload:JSON.stringify({task_type:'image',status:'failed',raw_error_message:'raw failure fixture',error_message:'Retry after quota refresh'})})});await h.open(1);assert.match(text(h.dialog()),/任务类型图片任务/);assert.match(text(h.dialog()),/任务状态失败/);const debug=h.find(n=>n.type==='details',h.dialog());assert.notEqual(debug.props.open,true);assert.match(text(debug),/raw failure fixture/);assert.match(text(h.dialog()),/Retry after quota refresh/);
});


test('support: cancelled create handler cannot submit or act on a reopened draft',async()=>{
 const h=await ready();h.click('新建工单');h.find(n=>n.type==='input'&&n.props.placeholder?.includes('付款后'),h.create()).props.onChange({target:{value:'Cancelled title'}});h.render();const create=h.button('创建工单',h.create()).props.onClick;h.click('取消',h.create());create();await h.settle();assert.equal(h.writes.length,0);
 h.click('新建工单');create();await h.settle();assert.equal(h.writes.length,0);assert.ok(h.create());
});
test('support: stale assignment/status/close handlers cannot operate after same-ID reopen',async()=>{
 const h=await ready();await h.open(1);const assign=h.button('分配给我',h.dialog()).props.onClick;const status=h.button('开始处理',h.dialog()).props.onClick;const close=h.find(n=>n.props?.['aria-label']==='关闭工单详情').props.onClick;h.close();await h.open(1);assign();status();close();await h.settle();assert.equal(h.writes.length,0);assert.ok(h.dialog());
});


test('support: mismatched detail identity is blocked before any action can target it',async()=>{
 const h=await ready({getSupportTicketDetail:async()=>item(2)});await h.open(1);assert.match(text(h.dialog()),/返回的工单与请求对象不一致/);assert.match(text(h.dialog()),/Ticket1/);assert.doesNotMatch(text(h.dialog()),/Ticket2/);h.button('分配给我',h.dialog()).props.onClick();await h.settle();assert.equal(h.writes.length,0);
});
