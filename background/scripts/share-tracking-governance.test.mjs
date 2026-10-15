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
  vm.runInNewContext(code, { module, exports: module.exports, require: name => name in overrides ? overrides[name] : name === '../pages/shareTracking/access' ? { captureTrackingAccessBoundary: () => ({}), notifyTrackingAccessDenied() {} } : name === './evidence' ? { IntentEvidenceDetails: 'IntentEvidenceDetails' } : name === '../config/api' ? { API_BASE_URL: '' } : name === '../utils/session' ? { expireSession() {} } : require(name), Date, URL, Error, AbortController, AbortSignal, setTimeout, clearTimeout, window: { location: { origin: 'https://example.test', protocol: 'https:' } }, ...overrides.globals });
  return module.exports;
}
const model = load('src/pages/shareTracking/model.ts');
const normalized = value => JSON.parse(JSON.stringify(value));
const governance = load('src/pages/shareTracking/governance.tsx', {'../../api/shareTracking':{tracking:{}},'./model':model});
test('manual override requires nonblank reason and a future expiry within 30 days', () => {
 const now=Date.parse('2026-10-11T12:00:00Z');
 for(const expiry of ['', 'bad','2026-10-11T12:00:00Z','2026-11-10T12:00:01Z']) assert.ok(governance.validateOverride('reason',expiry,now));
 assert.ok(governance.validateOverride(' ','2026-10-12T12:00:00Z',now));
 assert.equal(governance.validateOverride('verified','2026-11-10T12:00:00Z',now),'');
});
test('governance endpoints use exact visitor target and encode unlink ID', async()=>{
 const calls=[];const api=load('src/api/shareTracking.ts',{'../utils/request':{request:async(...args)=>{calls.push(args);return {data:{}}}}});
 await api.tracking.linkCRM({visitor_key:'u:42',customer_id:'crm',business_number:'B1',basis:'verified'},'owner-b');
 await api.tracking.unlinkCRM('u:42/a','owner-b');await api.tracking.overrideIntent({visitor_key:'u:42',level:'high',reason:'verified',expires_at:'2026-10-12T12:00:00Z'},'owner-b');await api.tracking.deleteVisitor({visitor_key:'u:42',reason:'requested'});
 assert.deepEqual(calls.map(x=>[x[0],x[1].method]),[['/api/v1/management/tracking/identity-links','POST'],['/api/v1/management/tracking/identity-links/u%3A42%2Fa','DELETE'],['/api/v1/management/tracking/intent-overrides','POST'],['/api/v1/management/tracking/deletions','POST']]);
 assert.equal(JSON.parse(calls[3][1].body).visitor_key,'u:42');
 for (const call of calls.slice(0,3)) assert.deepEqual(normalized(call[1].params),{owner:'owner-b'});
 assert.equal(calls[3][1].params,undefined);
});
function harness(context={},overrides={},visitorOverride={}) {
 context={owners:[{id:'owner-a',name:'归属 A'}],...context};
 const React=require('react');let slots=[],index=0,tree;const calls=[];const effects=[];
 const react={...React,useState(init){const i=index++;if(!(i in slots))slots[i]=typeof init==='function'?init():init;return[slots[i],v=>{slots[i]=typeof v==='function'?v(slots[i]):v}]},useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useEffect(effect,deps){const i=index++;if(!slots[i]||deps.some((d,j)=>d!==slots[i].deps[j])){slots[i]?.cleanup?.();slots[i]={deps,cleanup:effect()};effects.push(i)}}};
 const api={linkCRM:async (body,owner)=>{calls.push(['link',body,owner]);return{linked:true}},unlinkCRM:async (key,owner)=>{calls.push(['unlink',key,owner]);return{unlinked:true}},overrideIntent:async (body,owner)=>{calls.push(['override',body,owner]);return{updated:true}},deleteVisitor:async body=>{calls.push(['delete',body]);return{status:'completed'}},...overrides};
 const module=load('src/pages/shareTracking/governance.tsx',{'react':react,'../../api/shareTracking':{tracking:api},'./model':model,globals:{window:{confirm:()=>true,addEventListener(){},removeEventListener(){}}}});
 const visitor={visitor_key:'u:42',identity_state:'identified',intent:{level:'unknown',reasons:[],rule_version:'r1',window_start:'2026-10-01',window_end:'2026-10-11'},...visitorOverride};
 const nodes=x=>Array.isArray(x)?x.flatMap(nodes):!x||typeof x!=='object'?[]:[x,...nodes(x.props?.children)];
 const text=x=>Array.isArray(x)?x.map(text).join(''):x==null||typeof x==='boolean'?'':typeof x==='object'?text(x.props?.children):String(x);
 function render(){index=0;tree=module.VisitorGovernance({visitor,context,onBusyChange:value=>calls.push(['busy',value]),onDirtyChange:value=>calls.push(['dirty',value]),changed:()=>calls.push(['changed']),onDeleted:()=>calls.push(['deleted'])});return tree}render();
 return {calls,context,render,find:(type,label)=>nodes(tree).find(n=>n.type===type&&text(n).includes(label)),field(label,value){const n=nodes(tree).find(n=>n.type==='label'&&text(n).includes(label));nodes(n).find(n=>['input','textarea','select'].includes(n.type)).props.onChange({target:{value}});render()},text:()=>text(tree),unmount(){effects.forEach(i=>slots[i]?.cleanup?.())}};
}
const flush=()=>new Promise(r=>setImmediate(r));
test('manage permission cannot imply CRM, rules, or deletion privileges',()=>{
 const h=harness({can_manage:true});for(const label of ['确认并提交映射','保存人工修正','申请删除此访客数据']) assert.equal(h.find('button',label),undefined);
});
test('anonymous and inconsistent signed-in identities cannot map CRM',()=>{
 for(const visitor of [{identity_state:'anonymous',visitor_key:'a:1'},{identity_state:'identified',visitor_key:'a:1'}]) {const h=harness({can_crm:true},{},visitor);assert.equal(h.find('button','确认并提交映射'),undefined)}
});
test('CRM duplicates are blocked synchronously and errors preserve draft with exact target',async()=>{
 let reject;let body;let count=0;const h=harness({can_crm:true},{linkCRM:data=>{body=data;count++;return new Promise((_,r)=>reject=r)}});
 h.field('CRM 客户 ID','crm1');h.field('业务编号','B1');h.field('已验证业务关系依据','verified');const button=h.find('button','确认并提交映射');button.props.onClick();button.props.onClick();assert.equal(count,1);assert.equal(body.visitor_key,'u:42');
 reject(new Error('CRM业务关系验证尚未接入'));await flush();h.render();assert.match(h.text(),/草稿已保留/);assert.equal(h.find('label','CRM 客户 ID').props.children[1].props.value,'crm1');
});
test('delete is deliberate per-target confirmation and pending clicks produce one request',async()=>{
 let resolve,count=0;const h=harness({can_delete:true},{deleteVisitor:body=>{assert.equal(body.visitor_key,'u:42');count++;return new Promise(r=>resolve=r)}});
 assert.equal(h.find('button','确认删除访问记录与映射'),undefined);h.find('button','申请删除此访客数据').props.onClick();h.render();assert.equal(h.find('button','确认删除访问记录与映射').props.disabled,true);
 h.field('删除原因','用户申请');h.field('输入完整访客标识','wrong');h.find('button','确认删除访问记录与映射').props.onClick();assert.equal(count,0);
 h.field('输入完整访客标识','u:42');const button=h.find('button','确认删除访问记录与映射');button.props.onClick();button.props.onClick();assert.equal(count,1);resolve({status:'completed'});await flush();h.render();assert.ok(h.calls.some(x=>x[0]==='deleted'));assert.match(h.text(),/备份和副本仍按治理策略处理/);
});
test('unconfirmed deletion response preserves reason and does not mark deleted',async()=>{
 const h=harness({can_delete:true},{deleteVisitor:async()=>({status:'queued'})});h.find('button','申请删除此访客数据').props.onClick();h.render();h.field('删除原因','申请');h.field('输入完整访客标识','u:42');h.find('button','确认删除访问记录与映射').props.onClick();await flush();h.render();assert.equal(h.calls.some(x=>x[0]==='deleted'),false);assert.match(h.text(),/未确认操作完成/);
});
test('in-progress deletion keeps the confirmation open and invites an identical retry',async()=>{
 let requests=0;const h=harness({can_delete:true},{deleteVisitor:async()=>{requests++;return{status:requests<3?'in_progress':'completed'}}});
 h.find('button','申请删除此访客数据').props.onClick();h.render();h.field('删除原因','申请');h.field('输入完整访客标识','u:42');
 for(let attempt=0;attempt<2;attempt++){h.find('button','确认删除访问记录与映射').props.onClick();await flush();h.render();assert.equal(h.calls.some(x=>x[0]==='deleted'),false);assert.match(h.text(),/删除仍在进行/);assert.doesNotMatch(h.text(),/不要重复提交/);assert.ok(h.find('button','确认删除访问记录与映射'))}
 h.find('button','确认删除访问记录与映射').props.onClick();await flush();h.render();assert.equal(requests,3);assert.ok(h.calls.some(x=>x[0]==='deleted'));assert.match(h.text(),/备份和副本仍按治理策略处理/);
});
test('missing original rule result is disclosed and overrides do not claim to be original',()=>{
 const h=harness({}, {}, {intent:{level:'high',manual_note:'人工修正',reasons:[]}});assert.match(h.text(),/接口尚未返回独立的原规则结果/);assert.match(h.text(),/人工修正/);
});
test('original rule metadata is shown alongside manual override when provided',()=>{
 const h=harness({}, {}, {intent:{level:'high',reasons:[],original_rule:{level:'low',reasons:['规则证据'],rule_version:'v1'},override:{level:'high',reason:'经核实',actor:'manager',expires_at:'2026-10-20'}}});assert.match(h.text(),/原规则结果/);assert.match(h.text(),/manager/);assert.doesNotMatch(h.text(),/接口尚未返回独立/);
});
test('late mutation after unmount never refreshes or deletes another target',async()=>{
 let resolve;const h=harness({can_crm:true},{linkCRM:()=>new Promise(r=>resolve=r)});h.field('CRM 客户 ID','crm1');h.field('业务编号','B1');h.field('已验证业务关系依据','verified');h.find('button','确认并提交映射').props.onClick();h.unmount();resolve({linked:true});await flush();assert.equal(h.calls.some(x=>x[0]==='changed'),false);
});
test('cancelled deletion never writes and retains the reason for deliberate retry',()=>{
 const h=harness({can_delete:true});h.find('button','申请删除此访客数据').props.onClick();h.render();h.field('删除原因','用户申请');h.field('输入完整访客标识','u:42');h.find('button','取消删除').props.onClick();h.render();assert.equal(h.find('button','确认删除访问记录与映射'),undefined);assert.equal(h.calls.some(x=>x[0]==='delete'),false);h.find('button','申请删除此访客数据').props.onClick();h.render();assert.equal(h.find('button','确认删除访问记录与映射').props.disabled,true);assert.equal(h.find('label','删除原因').props.children[1].props.value,'用户申请');
});
test('changing only an intent level marks draft dirty and stale unmounted button cannot write',()=>{
 const h=harness({can_crm:true,can_rules:true});h.field('修正等级','high');assert.ok(h.calls.some(x=>x[0]==='dirty'&&x[1]===true));h.field('CRM 客户 ID','crm1');h.field('业务编号','B1');h.field('已验证业务关系依据','verified');const oldButton=h.find('button','确认并提交映射');h.unmount();oldButton.props.onClick();assert.equal(h.calls.some(x=>x[0]==='link'),false);
});
test('visitor close and material back navigation retain synchronous governance guards',()=>{
 const details=fs.readFileSync(new URL('src/pages/shareTracking/details.tsx',root),'utf8');const page=fs.readFileSync(new URL('src/pages/shareTracking.tsx',root),'utf8');
 assert.match(page,/editorBusy \|\| governanceState.current.busy/);assert.match(page,/editorDirty \|\| governanceState.current.dirty/);assert.match(details,/if \(busy.current \|\| \(dirty.current/);assert.match(details,/key=\{visitor.visitor_key\}/);
});

const twoOwners=[{id:'owner-a',name:'归属 A'},{id:'owner-b',name:'归属 B'}];
function fillCRM(h){h.field('CRM 客户 ID','crm1');h.field('业务编号','B1');h.field('已验证业务关系依据','verified')}
function fillOverride(h){h.field('修正等级','high');h.field('修正原因','verified');h.field('有效至',new Date(Date.now()+86400000).toISOString())}
test('governance requires explicit selection for multiple authorized owners',()=>{
 const h=harness({can_crm:true,can_rules:true,owners:twoOwners});fillCRM(h);fillOverride(h);
 const select=h.find('label','治理归属范围').props.children[1];assert.equal(select.props.value,'');
 assert.deepEqual(normalized([select.props.children[0],...select.props.children[1]].map(option=>option.props.value)),['','owner-a','owner-b']);
 for(const label of ['确认并提交映射','解除 CRM 映射','保存人工修正']){const button=h.find('button',label);assert.equal(button.props.disabled,true);button.props.onClick()}
 assert.equal(h.calls.some(x=>['link','unlink','override'].includes(x[0])),false);h.render();assert.match(h.text(),/请选择已授权的单一归属范围/);
});
test('all private mutations send the selected owner, including unlink without a mixed-list CRM label',async()=>{
 for(const [kind,label,fill] of [['link','确认并提交映射',fillCRM],['unlink','解除 CRM 映射',()=>{}],['override','保存人工修正',fillOverride]]){
  const h=harness({can_crm:true,can_rules:true,owners:twoOwners});h.field('治理归属范围','owner-b');fill(h);h.find('button',label).props.onClick();await flush();
  const call=h.calls.find(x=>x[0]===kind);assert.ok(call,kind);assert.equal(call[2],'owner-b');assert.equal(kind==='unlink'?call[1]:call[1].visitor_key,'u:42');
 }
});
test('a sole authorized owner defaults automatically but missing authorization cannot write',async()=>{
 const h=harness({can_crm:true});assert.equal(h.find('label','治理归属范围').props.children[1].props.value,'owner-a');fillCRM(h);h.find('button','确认并提交映射').props.onClick();await flush();assert.equal(h.calls.find(x=>x[0]==='link')[2],'owner-a');
 for(const owners of [[],twoOwners]){const blocked=harness({can_crm:true,owners});fillCRM(blocked);blocked.field('治理归属范围','outside');const button=blocked.find('button','确认并提交映射');assert.equal(button.props.disabled,true);button.props.onClick();assert.equal(blocked.calls.some(x=>x[0]==='link'),false)}
});
test('stale owner selection and revoked authorization cannot reuse a ready mutation',()=>{
 const h=harness({can_crm:true,owners:twoOwners});h.field('治理归属范围','owner-a');fillCRM(h);const old=h.find('button','确认并提交映射');h.field('治理归属范围','owner-b');old.props.onClick();assert.equal(h.calls.some(x=>x[0]==='link'),false);
 h.context.owners=[twoOwners[0]];h.render();const revoked=h.find('button','确认并提交映射');assert.equal(revoked.props.disabled,true);revoked.props.onClick();assert.equal(h.calls.some(x=>x[0]==='link'),false);
});
test('deletion remains available without selecting an annotation owner and sends no owner',async()=>{
 let args;const h=harness({can_crm:true,can_delete:true,owners:twoOwners},{deleteVisitor:async(...values)=>{args=values;return{status:'completed'}}});
 h.find('button','申请删除此访客数据').props.onClick();h.render();h.field('删除原因','用户申请');h.field('输入完整访客标识','u:42');h.find('button','确认删除访问记录与映射').props.onClick();await flush();assert.equal(args.length,1);assert.deepEqual(normalized(args[0]),{visitor_key:'u:42',reason:'用户申请'});
});
