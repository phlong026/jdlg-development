import test from 'node:test';
import assert from 'node:assert/strict';
import { page, template, deferred, settle, nodes, textOf } from './template-test-harness.mjs';
const group = (id=7) => ({id,name:`Group${id}`,display_mode:'normal',case1_id:88,case2_id:0,case1_label:'',case2_label:'',sort_order:0,case1:template({id:88})});
const result = (page=1,total=41) => ({list:total?[group(page*10)]:[],total,page,page_size:20});
const pager = h => h.find(n=>n.type==='nav'&&n.props['aria-label']==='精选案例组分页');
const pageButton = (h,label) => nodes(pager(h)).find(n=>n.type==='button'&&textOf(n)===label);
const card = (h,id) => h.find(n=>n.props?.className==='featured-group-item'&&n.key===String(id));
const action = (h,id,title) => nodes(card(h,id)).find(n=>n.type==='button'&&n.props.title===title);
const open = async h => {h.button('精选案例组管理').props.onClick();await h.flush();};

test('featured groups use independent real pages and totals beyond the original first 100',async()=>{
 const reads=[],mainReads=[];const h=await page({api:{getFeaturedCaseGroups:async(p,size)=>{reads.push([p,size]);return result(p,121);},getTemplateList:async p=>{mainReads.push(p);return {list:[template()],total:1};}}});
 await open(h);assert.deepEqual(reads,[[1,20]]);assert.match(textOf(pager(h)),/共 121 组/);assert.match(textOf(pager(h)),/共 7 页/);
 for(let p=2;p<=7;p++){pageButton(h,'下一页').props.onClick();await h.flush();assert.deepEqual(reads.at(-1),[p,20]);assert.ok(card(h,p*10));}
 assert.equal(pageButton(h,'下一页').props.disabled,true);assert.equal(mainReads.length,1);
 pageButton(h,'上一页').props.onClick();await h.flush();assert.deepEqual(reads.at(-1),[6,20]);
});

test('featured: collapse invalidates a pending list and reopen only accepts the fresh response',async()=>{
 const first=deferred(),second=deferred();let calls=0;const h=await page({api:{getFeaturedCaseGroups:()=>++calls===1?first.promise:second.promise}});
 await open(h);h.button('精选案例组管理').props.onClick();await h.flush();await open(h);first.resolve(result(1,999));await h.flush();assert.doesNotMatch(textOf(h.render()),/Group10|999 组/);assert.match(textOf(h.render()),/加载中/);second.resolve(result(1,21));await h.flush();assert.match(textOf(pager(h)),/共 21 组/);
});

test('featured: failed first read and incomplete/wrong-page payloads keep unknown totals',async()=>{
 let data;const h=await page({api:{getFeaturedCaseGroups:async()=>{if(!data)throw Error('offline');return data;}}});await open(h);
 assert.match(textOf(h.render()),/offline/);assert.match(textOf(pager(h)),/共 — 组/);assert.doesNotMatch(textOf(h.render()),/暂无精选案例组/);
 for(const invalid of [{list:[]},{...result(),page:2},{...result(),page_size:100}]){data=invalid;await h.button('重新加载案例组').props.onClick();await h.flush();assert.match(textOf(h.render()),/分页响应不完整或页码不匹配/);assert.equal(pageButton(h,'下一页').props.disabled,true);}
 data=result(1,0);await h.button('重新加载案例组').props.onClick();await h.flush();assert.match(textOf(h.render()),/暂无精选案例组/);assert.match(textOf(pager(h)),/共 0 组/);
});

test('featured: failed next page never displays prior-page cards as current, refresh failure retains only matching page',async()=>{
 let fail=false;const h=await page({api:{getFeaturedCaseGroups:async p=>{if(fail)throw Error('offline');return result(p);}}});await open(h);fail=true;
 pageButton(h,'下一页').props.onClick();await h.flush();assert.doesNotMatch(textOf(h.render()),/Group10/);assert.match(textOf(pager(h)),/第 2 页/);
 fail=false;await h.button('重新加载案例组').props.onClick();await h.flush();assert.ok(card(h,20));
 h.button('精选案例组管理').props.onClick();await h.flush();fail=true;await open(h);assert.ok(card(h,20));assert.match(textOf(h.render()),/保留上次成功结果/);
});

test('featured: last-page deletion returns to the last valid page',async()=>{
 let deleted=false;const reads=[];const h=await page({api:{getFeaturedCaseGroups:async p=>{reads.push(p);return result(p,deleted?20:21);},deleteFeaturedCaseGroup:async()=>{deleted=true;}}});await open(h);pageButton(h,'下一页').props.onClick();await h.flush();await action(h,20,'删除').props.onClick();await h.flush();assert.equal(reads.at(-1),1);assert.match(textOf(pager(h)),/第 1 页/);assert.ok(card(h,10));
});

test('featured: write completion refreshes the current page instead of the page captured by the editor',async()=>{
 const pending=deferred(),reads=[];const h=await page({api:{getFeaturedCaseGroups:async p=>{reads.push(p);return result(p);},updateFeaturedCaseGroup:()=>pending.promise}});await open(h);action(h,10,'编辑').props.onClick();await h.flush();h.change('组名称 *','Changed');const save=h.button('保存').props.onClick();pageButton(h,'下一页').props.onClick();await h.flush();pending.resolve();await save;await h.flush();assert.equal(reads.at(-1),2);assert.match(textOf(pager(h)),/第 2 页/);assert.ok(card(h,20));
});

test('featured: successful delete with failed refresh remains explicit and single-flight',async()=>{
 let fail=false,writes=0;const pending=deferred();const h=await page({api:{getFeaturedCaseGroups:async p=>{if(fail)throw Error('refresh failed');return result(p);},deleteFeaturedCaseGroup:()=>{writes++;return pending.promise;}}});await open(h);const del=action(h,10,'删除').props.onClick;const request=del();del();assert.equal(writes,1);fail=true;pending.resolve();await request;await h.flush();assert.match(textOf(h.render()),/案例组已删除，但列表刷新失败/);assert.doesNotMatch(textOf(h.render()),/Group10/);
});

test('featured: cached old-page row actions cannot mutate or open after page change or collapse',async()=>{
 let writes=0;const h=await page({api:{getFeaturedCaseGroups:async p=>result(p),deleteFeaturedCaseGroup:async()=>{writes++;}}});await open(h);const edit=action(h,10,'编辑').props.onClick,del=action(h,10,'删除').props.onClick;pageButton(h,'下一页').props.onClick();await del();edit();await h.flush();assert.equal(writes,0);assert.equal(h.nodes().some(n=>n.props?.className==='modal-content modal-form'),false);
 const nextDel=action(h,20,'删除').props.onClick;h.button('精选案例组管理').props.onClick();await nextDel();assert.equal(writes,0);
});

test('featured: unmount discards pending list and prevents post-write background refresh',async()=>{
 const pending=deferred();let reads=0;const h=await page({api:{getFeaturedCaseGroups:async p=>{reads++;return result(p);},deleteFeaturedCaseGroup:()=>pending.promise}});await open(h);const del=action(h,10,'删除').props.onClick;const request=del();h.unmount();pending.resolve();await request;assert.equal(reads,1);await del();assert.equal(reads,1);
});

test('featured: failed writes after unmount do not show alerts on another page',async()=>{
 const pending=deferred();const h=await page({api:{getFeaturedCaseGroups:async p=>result(p),deleteFeaturedCaseGroup:()=>pending.promise}});await open(h);const request=action(h,10,'删除').props.onClick();h.unmount();pending.reject(Error('late write error'));await request;assert.equal(h.alerts.length,0);
});
