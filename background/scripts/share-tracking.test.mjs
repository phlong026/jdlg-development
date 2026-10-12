import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const root = new URL('../', import.meta.url);
function load(file, overrides = {}) {
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(new URL(file, root), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, require: name => name in overrides ? overrides[name] : name === './evidence' ? { IntentEvidenceDetails: 'IntentEvidenceDetails' } : name === '../config/api' ? { API_BASE_URL: '' } : name === '../utils/session' ? { expireSession() {} } : require(name), Date, URL, Error, AbortController, AbortSignal, setTimeout, clearTimeout, window: { location: { origin: 'https://example.test', protocol: 'https:' } }, ...overrides.globals });
  return module.exports;
}
const model = load('src/pages/shareTracking/model.ts');
const normalized = value => JSON.parse(JSON.stringify(value));
test('Shanghai natural days cross UTC midnight and use exclusive next-day end', () => {
  const f = { ...model.defaultFilters, range: '3' };
  assert.deepEqual(normalized(model.dateRange(f, new Date('2026-10-11T15:59:59Z'))), { start: '2026-10-08T16:00:00.000Z', end: '2026-10-11T16:00:00.000Z' });
  assert.deepEqual(normalized(model.dateRange(f, new Date('2026-10-11T16:00:00Z'))), { start: '2026-10-09T16:00:00.000Z', end: '2026-10-12T16:00:00.000Z' });
});
test('custom dates reject impossible, missing and reverse dates; leap day is valid', () => {
  for (const [start,end] of [['2026-02-30','2026-03-01'],['','2026-01-01'],['2026-10-12','2026-10-11']]) assert.throws(() => model.dateRange({ ...model.defaultFilters, range: 'custom', start, end }));
  assert.equal(model.dateRange({ ...model.defaultFilters, range: 'custom', start: '2024-02-29', end: '2024-02-29' }).end, '2024-02-29T16:00:00.000Z');
  assert.deepEqual(normalized(model.dateRange({ ...model.defaultFilters, range: 'all' })), {});
});
test('query retains identity intent owner sort and authorized search with no client identity override', () => {
 const query = model.queryFor({ ...model.defaultFilters, range:'all', identity:'anonymous',intent:'unknown',owner:'allowed',q:'  名称  ',sort:'visible_ms',direction:'asc' });
 assert.deepEqual(normalized(query),{ identity:'anonymous', intent:'unknown', owner:'allowed', q:'名称',sort:'visible_ms',direction:'asc',limit:20 });
});
test('media URL rendering rejects executable protocols, cleartext external and embedded credentials', () => {
 for (const url of ['javascript:alert(1)','data:image/svg+xml,x','http://remote.test/a','https://user:secret@example.test/x']) assert.equal(model.safeMediaURL(url), undefined);
 assert.equal(model.safeMediaURL('/authorized/media/1'),'https://example.test/authorized/media/1');
});
test('API uses real requests, scoped prefix, CSRF marker and abort signal', async () => {
 const calls=[]; const api=load('src/api/shareTracking.ts',{'../utils/request':{request:async(...args)=>{calls.push(args);return {data:{items:[]}}}}});
 const controller=new AbortController();await api.tracking.visitors({identity:'anonymous'},controller.signal);
 assert.equal(calls[0][0],'/api/v1/management/tracking/visitors');assert.ok(calls[0][1].signal instanceof AbortSignal);assert.equal(calls[0][1].headers['X-Tracking-Request'],'1');
 await api.tracking.publish('opaque/id',3,'preview-proof','one-request');
 assert.equal(calls[1][0],'/api/v1/management/tracking/contents/opaque%2Fid/publish');assert.deepEqual(JSON.parse(calls[1][1].body),{expected_revision:3,preview_token:'preview-proof',idempotency_key:'one-request'});
});
test('missing API payload is an error, never a successful empty record list', async () => {
 const api=load('src/api/shareTracking.ts',{'../utils/request':{request:async()=>({code:0})}});
 await assert.rejects(api.tracking.context(),/有效数据/);
});
test('resource hook aborts and ignores late older responses and post-unmount completions', async () => {
 let slots=[],index=0,pending=[];
 const react={useState(init){const i=index++;if(!(i in slots))slots[i]=init;return[slots[i],v=>{slots[i]=typeof v==='function'?v(slots[i]):v}]},useEffect(effect,deps){const i=index++;if(!slots[i]||deps.some((d,j)=>d!==slots[i].deps[j])){slots[i]?.cleanup?.();slots[i]={deps,cleanup:effect()}}}};
 const api=load('src/pages/shareTracking/components.tsx',{'react':react,'./model':model});
 const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject}};
 const old=deferred(),fresh=deferred();let oldSignal;
 index=0;api.useResource(signal=>{oldSignal=signal;return old.promise},'old');
 index=0;api.useResource(()=>fresh.promise,'fresh');assert.equal(oldSignal.aborted,true);
 fresh.resolve('new');await new Promise(r=>setImmediate(r));old.resolve('stale');await new Promise(r=>setImmediate(r));
 index=0;assert.equal(api.useResource(()=>fresh.promise,'fresh').data,'new');
 const third=deferred();index=0;api.useResource(()=>third.promise,'third');slots[2].cleanup();third.resolve('unmounted');await new Promise(r=>setImmediate(r));assert.equal(slots[0].data,undefined);
});
test('UI preserves scope in same-session exploration and separates metrics and identities', () => {
 const detail=fs.readFileSync(new URL('src/pages/shareTracking/details.tsx',root),'utf8');const page=fs.readFileSync(new URL('src/pages/shareTracking.tsx',root),'utf8');
 assert.match(detail,/content_id: showingEvidence \? undefined : contentId, include_related: otherPages/);assert.match(page,/as_of: result.data\?\.as_of/);assert.match(page,/sessionStorage.setItem/);assert.match(page,/can_view/);assert.doesNotMatch(page,/mockData|fakeData|Math\.random/);
});

function editorHarness(overrides = {}, initial = {}) {
 const React=require('react');let slots=[],index=0,tree;const calls=[];const lifecycle=[];
 const react={...React,useState(init){const i=index++;if(!(i in slots))slots[i]=typeof init==='function'?init():init;return[slots[i],v=>{slots[i]=typeof v==='function'?v(slots[i]):v}]},useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useEffect(effect,deps){const i=index++;if(!slots[i]){const cleanup=effect();slots[i]={cleanup};lifecycle.push(()=>{cleanup?.();slots[i].cleanup=effect()})}}};
 const content={content_id:'content1',revision:1,version:1,title:'文章',body:'已授权正文',status:'draft',rights:'own',owner_id:'owner1',cover:{media_id:'image1'},media:[],...initial};
 const api={save:async(...args)=>{calls.push(['save',...args]);return content},preview:async(...args)=>{calls.push(['preview',...args]);return{content,preview_token:'verified'}},publish:async(...args)=>{calls.push(['publish',...args]);return{content:{...content,status:'published'},share_methods:{}}},upload:async(...args)=>{calls.push(['upload',...args]);return{media_id:'uploaded'}},media:async()=>({items:[]}),revoke:async(...args)=>{calls.push(['revoke',...args]);return{...content,status:'revoked'}},...overrides};
 const components={Feedback:'Feedback',MediaPreview:'MediaPreview',Pager:'Pager',useResource:()=>({data:{items:[]},reload(){}})};
 const module=load('src/pages/shareTracking/editor.tsx',{'react':react,'../../api/shareTracking':{tracking:api},'./components':components,'./model':model,globals:{crypto:{randomUUID:()=> 'idempotency-one'},window:{confirm:()=>true}}});
 const nodes=x=>Array.isArray(x)?x.flatMap(nodes):!x||typeof x!=='object'?[]:[x,...nodes(x.props?.children)];
 const text=x=>Array.isArray(x)?x.map(text).join(''):x==null||typeof x==='boolean'?'':typeof x==='object'?text(x.props?.children):String(x);
 function render(){index=0;tree=module.ContentEditor({initial:content,context:{owners:[{id:'owner1',name:'销售'}]},changed(){calls.push(['changed'])},onBusyChange(value){calls.push(['busy',value])}});return tree}
 render();
 return {calls,render,strictMount:()=>lifecycle.forEach(fn=>fn()),find:(type,label)=>nodes(tree).find(n=>n.type===type&&text(n).includes(label)),checkbox:()=>nodes(tree).filter(n=>n.type==='input'&&n.props.type==='checkbox'),input:()=>nodes(tree).find(n=>n.type==='input'&&n.props.value==='文章'),text:()=>text(tree)};
}
const flush=()=>new Promise(resolve=>setImmediate(resolve));
test('editor survives StrictMode remount, saves before preview, invalidates preview on edits',async()=>{
 const h=editorHarness();h.strictMount();h.checkbox()[0].props.onChange({target:{checked:true}});h.render();
 h.find('button','保存草稿并预览').props.onClick();await flush();h.render();
 assert.deepEqual(h.calls.filter(x=>['save','preview'].includes(x[0])).map(x=>x[0]),['save','preview']);assert.match(h.text(),/发布前预览/);
 h.input().props.onChange({target:{value:'修改标题'}});h.render();assert.doesNotMatch(h.text(),/发布前预览/);assert.equal(h.find('button','发布并获取分享方式'),undefined);
});
test('editor prevents duplicate pending saves and retains draft after failure',async()=>{
 let reject;const pending=new Promise((_,r)=>reject=r);let writes=0;const h=editorHarness({save:()=>{writes++;return pending}});
 h.checkbox()[0].props.onChange({target:{checked:true}});h.render();const button=h.find('button','保存草稿并预览');button.props.onClick();button.props.onClick();assert.equal(writes,1);
 reject(new Error('409 草稿版本冲突'));await flush();h.render();assert.match(h.text(),/草稿版本冲突/);assert.equal(h.input().props.value,'文章');
});
test('upload API uses multipart without JSON Content-Type and cookie/CSRF protection',async()=>{
 let captured;const api=load('src/api/shareTracking.ts',{'../utils/request':{request(){}},globals:{FormData,fetch:async(url,config)=>{captured={url,config};return{ok:true,status:200,json:async()=>({code:0,data:{media_id:'asset1'}})}}}});
 await api.tracking.upload(new Blob(['test'],{type:'image/png'}),'authorized');assert.ok(captured.config.body instanceof FormData);assert.equal(captured.config.headers['Content-Type'],undefined);assert.equal(captured.config.headers['X-Tracking-Request'],'1');assert.equal(captured.config.credentials,'include');assert.equal(captured.config.body.get('rights'),'authorized');
});

test('rich text renders only the explicit persisted format; legacy body remains escaped text', () => {
 const { ContentBody }=load('src/pages/shareTracking/components.tsx',{'./model':model});
 const markup='<strong>内容</strong>';
 assert.equal(ContentBody({content:{body:markup}}).props.children,markup);
 assert.equal(ContentBody({content:{body:markup,body_format:'plain'}}).props.dangerouslySetInnerHTML,undefined);
 assert.equal(ContentBody({content:{body:markup,body_format:'rich_text'}}).props.dangerouslySetInnerHTML.__html,markup);
 const editor=fs.readFileSync(new URL('src/pages/shareTracking/editor.tsx',root),'utf8');
 assert.ok(editor.includes('<ContentBody content={preview.content} />'));
 assert.ok(!editor.includes('dangerouslySetInnerHTML'));
 assert.ok(!editor.includes('<ContentBody content={form}'));
});

test('published draft keeps current public version and revokes without publishing unfinished edits',async()=>{
 const h=editorHarness({}, {status:'draft',version:4,revision:9});
 assert.match(h.text(),/当前公开版本：v4/);assert.match(h.text(),/无需发布草稿即可撤回/);
 const input=h.find('label','撤回原因').props.children.find(n=>n?.type==='input');input.props.onChange({target:{value:'停止旧版分享'}});h.render();
 h.find('button','撤回并停止旧入口阅读').props.onClick();await flush();h.render();
 const writes=h.calls.filter(x=>['publish','save','revoke'].includes(x[0]));assert.deepEqual(normalized(writes),[['revoke','content1',9,'停止旧版分享','idempotency-one']]);
 assert.equal(h.find('button','撤回并停止旧入口阅读'),undefined);assert.match(h.text(),/已撤回/);
});
test('unpublished draft and already revoked material do not offer revocation',()=>{
 for(const initial of [{status:'draft',version:0},{status:'revoked',version:3}]){const h=editorHarness({},initial);assert.equal(h.find('button','撤回并停止旧入口阅读'),undefined);}
});
test('draft revoke is single flight and keeps reason/public controls on failure',async()=>{
 let reject,writes=0;const p=new Promise((_,r)=>reject=r);const h=editorHarness({revoke:()=>{writes++;return p}}, {status:'draft',version:2,revision:5});
 h.find('label','撤回原因').props.children.find(n=>n?.type==='input').props.onChange({target:{value:'复核撤回'}});h.render();
 const button=h.find('button','撤回并停止旧入口阅读');button.props.onClick();button.props.onClick();assert.equal(writes,1);
 reject(new Error('409 当前版本冲突'));await flush();h.render();assert.match(h.text(),/当前版本冲突/);assert.ok(h.find('button','撤回并停止旧入口阅读'));
 assert.equal(h.find('label','撤回原因').props.children.find(n=>n?.type==='input').props.value,'复核撤回');
});
