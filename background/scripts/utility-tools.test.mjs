// Production page handlers and API adapter; all transport and writes are fakes.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import ts from 'typescript';
import React from 'react';
const require = createRequire(import.meta.url);
const tick = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => {resolve=a;reject=b;}); return {promise,resolve,reject}; };
const plain = x => JSON.parse(JSON.stringify(x));
const children = ['children','controls','actions','summary'];
const nodes = n => Array.isArray(n) ? n.flatMap(nodes) : n && typeof n === 'object' ? [n,...children.flatMap(k=>nodes(n.props?.[k]))] : [];
const text = n => Array.isArray(n) ? n.map(text).join('') : n == null || typeof n === 'boolean' ? '' : typeof n === 'object' ? children.map(k=>text(n.props?.[k])).join('') : String(n);
const item = (id=1, extra={}) => ({id, category:'local_norm', title:`Tool${id}`, content:'Content',cover_image:'cover',video_url:'video',file_url:'file',sort_order:3,is_published:true,view_count:9,created_at:'',updated_at:'',...extra});
const response = (p=1,total=21) => ({list: total ? [item(p === 1 ? 1 : 21)] : [],total,page:p,page_size:20});
async function page(overrides={}) {
 const state=[],effects=[],cleanups=new Map();let cursor=0,dirty=false,tree;
 const h={reads:[],details:[],writes:[],confirms:[],confirm:true};
 h.api={getUtilityToolList:async p=>response(p.page),getUtilityToolDetail:async id=>item(Number(id)),createUtilityTool:async()=>item(22),updateUtilityTool:async id=>item(Number(id)),deleteUtilityTool:async()=>{},...overrides};
 const api=Object.fromEntries(Object.keys(h.api).map(name=>[name,(...args)=>{if(name==='getUtilityToolList')h.reads.push(plain(args[0]));else if(name==='getUtilityToolDetail')h.details.push(args[0]);else h.writes.push({name,args:plain(args)});return h.api[name](...args);} ]));
 const react={...React,
 useState(initial){const i=cursor++;if(!(i in state))state[i]=typeof initial==='function'?initial():initial;return[state[i],v=>{const next=typeof v==='function'?v(state[i]):v;if(!Object.is(next,state[i]))dirty=true;state[i]=next;}];},
 useRef(initial){const i=cursor++;if(!(i in state))state[i]={current:initial};return state[i];},
 useCallback(fn,deps){const i=cursor++;if(!(i in state)||deps.some((d,j)=>!Object.is(d,state[i].deps[j])))state[i]={fn,deps};return state[i].fn;},
 useEffect(fn,deps){const i=cursor++;if(!(i in state)||deps.some((d,j)=>!Object.is(d,state[i][j]))){state[i]=deps;effects.push({i,fn});}},
 };
 const mod={exports:{}};
 const code=ts.transpileModule(fs.readFileSync(new URL('../src/pages/utilityTools.tsx',import.meta.url),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.CommonJS,jsx:ts.JsxEmit.ReactJSX,esModuleInterop:true}}).outputText;
 vm.runInNewContext(code,{module:mod,exports:mod.exports,console:{error(){}},window:{confirm:m=>{h.confirms.push(m);return h.confirm;}},require(name){if(name==='react')return react;if(name==='react/jsx-runtime')return require(name);if(name==='react-icons/fi')return new Proxy({},{get:()=>()=>null});if(name==='../api/utilityTools')return api;if(name==='../component/managementSearchPanel')return ()=>null;if(name.endsWith('.scss'))return {};throw Error(name);}});
 h.render=()=>{cursor=0;dirty=false;tree=mod.exports.default();return tree;};h.tree=()=>tree;h.nodes=()=>nodes(tree);
 h.find=(predicate,scope=tree)=>{const n=nodes(scope).find(predicate);assert.ok(n,'Expected UI element');return n;};
 h.button=(label,scope)=>h.find(n=>n.type==='button'&&text(n).trim()===label,scope);
 h.click=(label,scope)=>{const result=h.button(label,scope).props.onClick();h.render();return result;};
 h.card=id=>h.find(n=>n.props?.className==='utility-tool-item'&&n.key===String(id));
 h.action=(id,title)=>h.find(n=>n.type==='button'&&n.props.title===title,h.card(id));
 h.field=label=>{const group=h.find(n=>n.props?.className?.includes('form-group')&&nodes(n).some(c=>c.type==='label'&&text(c)===label));return nodes(group).find(n=>['input','select','textarea'].includes(n.type));};
 h.change=(label,value,checked)=>{h.field(label).props.onChange({target:{value,checked}});h.render();};
 h.filter=(value)=>{h.find(n=>n.type==='select'&&n.props.className==='management-search-select'&&nodes(n).some(c=>c.type==='option'&&c.props.value===value)).props.onChange({target:{value}});h.render();};
 h.search=value=>{h.find(n=>n.type==='input'&&n.props.className==='management-search-input').props.onChange({target:{value}});h.render();h.click('搜索内容');};
 h.edit=id=>{const promise=h.action(id,'编辑').props.onClick();h.render();return promise;};
 h.flush=async()=>{for(let i=0;i<30;i++){h.render();for(const e of effects.splice(0)){cleanups.get(e.i)?.();cleanups.set(e.i,e.fn());}await tick();if(!dirty&&!effects.length)return h;}throw Error('did not settle');};
 h.unmount=()=>{for(const fn of cleanups.values())fn?.();};
 await h.flush();return h;
}

test('utility: real pagination and server filters reset to page one',async()=>{
 const h=await page();assert.deepEqual(h.reads[0],{page:1,page_size:20});assert.match(text(h.tree()),/当前筛选共 21/);
 h.click('下一页');await h.flush();assert.equal(h.reads.at(-1).page,2);assert.match(text(h.tree()),/Tool21/);
 h.filter('faq');await h.flush();assert.deepEqual(h.reads.at(-1),{page:1,page_size:20,category:'faq'});
 h.filter('published');h.search('  keyword  ');await h.flush();assert.deepEqual(h.reads.at(-1),{page:1,page_size:20,category:'faq',keyword:'keyword',is_published:true});
 h.click('重置筛选');await h.flush();assert.deepEqual(h.reads.at(-1),{page:1,page_size:20});
});

test('utility API adapter sends 1/0 for published filters and omits unknown',async()=>{
 const calls=[],mod={exports:{}};const code=ts.transpileModule(fs.readFileSync(new URL('../src/api/utilityTools.ts',import.meta.url),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.CommonJS}}).outputText;
 vm.runInNewContext(code,{module:mod,exports:mod.exports,require(name){if(name==='../utils/request')return{get:async(...a)=>{calls.push(a);return{data:{}};}};if(name==='../config/api')return{API_ENDPOINTS:{UTILITY_TOOLS:{LIST:'/tools'}}};throw Error(name);}});
 for(const val of [true,false,undefined])await mod.exports.getUtilityToolList({is_published:val,page:2});
 assert.deepEqual(calls.map(c=>c[1].is_published),[1,0,undefined]);assert.equal(calls[0][1].page,2);
});

test('utility: old filter/page responses cannot replace the current page or its loading state',async()=>{
 const old=deferred(),latest=deferred();const h=await page({getUtilityToolList:p=>p.category==='faq'?latest.promise:old.promise});
 h.filter('faq');await h.flush();old.resolve(response());await h.flush();assert.doesNotMatch(text(h.tree()),/Tool1/);assert.match(text(h.tree()),/加载中/);
 latest.resolve({...response(),list:[item(8)]});await h.flush();assert.match(text(h.tree()),/Tool8/);
});

test('utility: failed first load stays unknown, malformed or wrong-page responses are not empty success',async()=>{
 const h=await page({getUtilityToolList:async()=>{throw Error('offline');}});assert.match(text(h.tree()),/offline|列表尚未成功加载/);assert.doesNotMatch(text(h.tree()),/暂无内容|当前筛选共 0/);
 for(const bad of [{list:[]},{...response(),page:2},{...response(),page_size:100}]){h.api.getUtilityToolList=async()=>bad;await h.click('重试加载');await h.flush();assert.match(text(h.tree()),/分页响应不完整或页码不匹配/);assert.equal(h.button('下一页').props.disabled,true);}
 h.api.getUtilityToolList=async()=>response(1,0);await h.click('重试加载');await h.flush();assert.match(text(h.tree()),/暂无内容/);assert.match(text(h.tree()),/当前筛选共 0/);
});

test('utility: failed refresh preserves known results; failed new page does not relabel old rows',async()=>{
 const h=await page();h.api.getUtilityToolList=async()=>{throw Error('down');};await h.click('刷新列表');await h.flush();assert.match(text(h.tree()),/Tool1/);assert.match(text(h.tree()),/保留上次成功结果/);
 h.click('下一页');await h.flush();assert.doesNotMatch(text(h.tree()),/Tool1/);assert.match(text(h.tree()),/第 2 页/);assert.match(text(h.tree()),/列表尚未成功加载/);
});

test('utility: last page disappearing returns to valid page after delete',async()=>{
 let deleted=false;const h=await page({getUtilityToolList:async p=>response(p.page,deleted?20:21),deleteUtilityTool:async()=>{deleted=true;}});
 h.click('下一页');await h.flush();await h.action(21,'删除').props.onClick();await h.flush();assert.equal(h.reads.at(-1).page,1);assert.match(text(h.tree()),/第 1 页/);assert.match(text(h.tree()),/内容已删除/);
});

test('utility: detail cancellation and a newer add editor isolate late detail success/error',async()=>{
 for(const fail of [false,true]){const pending=deferred();const h=await page({getUtilityToolDetail:()=>pending.promise});h.edit(1);assert.equal(h.button('保存').props.disabled,true);h.click('取消');h.click('添加内容');h.change('标题 *','Fresh');if(fail)pending.reject(Error('old detail error'));else pending.resolve(item(1));await h.flush();assert.equal(h.field('标题 *').props.value,'Fresh');assert.doesNotMatch(text(h.tree()),/old detail error/);}
});

test('utility: mismatched detail is blocked rather than editing another ID',async()=>{
 const h=await page({getUtilityToolDetail:async()=>item(2)});await h.edit(1);await h.flush();assert.match(text(h.tree()),/详情对象不匹配/);await h.button('保存').props.onClick();assert.equal(h.writes.length,0);
});

test('utility: newer details win out of order including same-ID reopen',async()=>{
 const a=deferred(),b=deferred();let calls=0;const h=await page({getUtilityToolDetail:()=>++calls===1?a.promise:b.promise});h.edit(1);h.click('取消');h.edit(1);b.resolve(item(1,{title:'Latest'}));await h.flush();a.resolve(item(1,{title:'Old'}));await h.flush();assert.equal(h.field('标题 *').props.value,'Latest');
});

test('utility: create retains published=true and locks duplicate save/close/form changes until settled',async()=>{
 const pending=deferred();const h=await page({createUtilityTool:()=>pending.promise});h.click('添加内容');assert.equal(h.field('发布').props.checked,true);h.change('标题 *','Created');const save=h.button('保存').props.onClick;const close=h.button('取消').props.onClick;const change=h.field('标题 *').props.onChange;const promise=save();save();close();change({target:{value:'Lost'}});h.render();assert.equal(h.writes.length,1);assert.equal(h.writes[0].args[0].is_published,true);assert.equal(h.field('标题 *').props.value,'Created');assert.equal(h.button('取消').props.disabled,true);
 pending.resolve(item(22));await promise;await h.flush();assert.equal(h.nodes().some(n=>n.props?.role==='dialog'),false);assert.match(text(h.tree()),/内容已创建/);await save();assert.equal(h.writes.length,1);
});

test('utility: failed save preserves draft and retries explicitly, including cleared optional fields',async()=>{
 const h=await page({updateUtilityTool:async()=>{throw Error('write failed');}});await h.edit(1);await h.flush();h.change('标题 *','Draft');h.change('文件 URL（如PDF）','');h.change('发布','',false);await h.click('保存');await h.flush();assert.equal(h.field('标题 *').props.value,'Draft');assert.match(text(h.tree()),/write failed/);assert.equal(h.writes[0].args[1].file_url,'');assert.equal(h.writes[0].args[1].is_published,false);
 h.api.updateUtilityTool=async()=>item(1);await h.click('保存');await h.flush();assert.equal(h.writes.length,2);assert.equal(h.nodes().some(n=>n.props?.role==='dialog'),false);
});

test('utility: cancelled create/edit callbacks cannot save, change or close same-ID reopened editor',async()=>{
 for(const editing of [false,true]){const h=await page();if(editing){await h.edit(1);await h.flush();}else h.click('添加内容');h.change('标题 *','Old');const save=h.button('保存').props.onClick,close=h.button('取消').props.onClick,change=h.field('标题 *').props.onChange;close();await save();assert.equal(h.writes.length,0);h.render();if(editing){await h.edit(1);await h.flush();}else h.click('添加内容');h.change('标题 *','New');await save();change({target:{value:'Old'}});close();h.render();assert.equal(h.writes.length,0);assert.equal(h.field('标题 *').props.value,'New');}
});

test('utility: committed save plus failed refresh closes editor and never invites a duplicate write',async()=>{
 const h=await page();h.click('添加内容');h.change('标题 *','Saved');h.api.getUtilityToolList=async()=>{throw Error('read failed');};await h.click('保存');await h.flush();assert.equal(h.writes.length,1);assert.equal(h.nodes().some(n=>n.props?.role==='dialog'),false);assert.match(text(h.tree()),/内容已创建，但列表刷新失败/);assert.match(text(h.tree()),/不要重复提交/);
});

test('utility: completion refreshes latest filters and stale write refresh cannot override newer navigation',async()=>{
 const pending=deferred(),oldRefresh=deferred();const h=await page({updateUtilityTool:()=>pending.promise});await h.edit(1);await h.flush();h.change('标题 *','Saved');const save=h.click('保存');h.filter('faq');await h.flush();h.api.getUtilityToolList=p=>p.category==='faq'?oldRefresh.promise:Promise.resolve(response());pending.resolve(item());await tick();h.filter('local_norm');await h.flush();oldRefresh.resolve({...response(),list:[item(8,{title:'Stale'})]});await save;await h.flush();assert.equal(h.reads.at(-1).category,'local_norm');assert.doesNotMatch(text(h.tree()),/Stale/);assert.ok(h.reads.some(p=>p.category==='faq'));
});

test('utility: delete is synchronous single flight, cancellation writes nothing, failure keeps row',async()=>{
 const pending=deferred();const h=await page({deleteUtilityTool:()=>pending.promise});h.confirm=false;await h.action(1,'删除').props.onClick();assert.equal(h.writes.length,0);h.confirm=true;const del=h.action(1,'删除').props.onClick;const request=del();del();assert.equal(h.writes.length,1);pending.reject(Error('delete failed'));await request;await h.flush();assert.match(text(h.tree()),/Tool1/);assert.match(text(h.tree()),/delete failed/);
});

test('utility: unmount suppresses late reads and successful writes do not trigger background refresh',async()=>{
 const detail=deferred(),save=deferred();const h=await page({getUtilityToolDetail:()=>detail.promise});h.edit(1);h.unmount();detail.resolve(item());await tick();h.render();assert.doesNotMatch(text(h.tree()),/详情对象不匹配/);
 const other=await page({createUtilityTool:()=>save.promise});other.click('添加内容');other.change('标题 *','Saved');const request=other.click('保存');const reads=other.reads.length;other.unmount();save.resolve(item(22));await request;assert.equal(other.reads.length,reads);
});

test('utility: old-page edit/delete and pagination callbacks cannot act after filters change',async()=>{
 const h=await page();const edit=h.action(1,'编辑').props.onClick,del=h.action(1,'删除').props.onClick,next=h.button('下一页').props.onClick;
 h.filter('faq');await edit();await del();next();await h.flush();assert.equal(h.details.length,0);assert.equal(h.writes.length,0);assert.equal(h.reads.at(-1).page,1);assert.equal(h.reads.at(-1).category,'faq');
});

test('utility: an incomplete detail publish state cannot silently publish while changing a title',async()=>{
 const h=await page({getUtilityToolDetail:async()=>item(1,{is_published:undefined})});await h.edit(1);await h.flush();assert.match(text(h.tree()),/详情发布状态不完整/);await h.button('保存').props.onClick();assert.equal(h.writes.length,0);assert.equal(h.button('保存').props.disabled,true);
});
