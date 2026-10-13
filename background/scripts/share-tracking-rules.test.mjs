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
  vm.runInNewContext(code, { module, exports: module.exports, require: name => name in overrides ? overrides[name] : name === '../pages/shareTracking/access' ? { captureTrackingAccessBoundary: () => ({}), notifyTrackingAccessDenied() {} } : name === '../config/api' ? { API_BASE_URL: '' } : name === '../utils/session' ? { expireSession() {} } : require(name), Date, URL, Error, AbortController, AbortSignal, setTimeout, clearTimeout, ...overrides.globals });
  return module.exports;
}
const model = load('src/pages/shareTracking/model.ts');
const normalized = value => JSON.parse(JSON.stringify(value));
const base = { rule_version:'followup-v1',revision:0,owner_id:'owner-a',effective_at:'2026-10-01T00:00:00Z',actor:0,reason:'默认规则',timezone:'Asia/Shanghai',window_days:30,medium_sessions:2,medium_favorites:1,high_business_results:1,daily_session_cap:3,daily_action_cap:2,participating_metrics:['sessions','favorites','business_results'],exclusions:['preview','self','test'] };
const result = (rule=base) => ({ current:{...rule},history:[] });
const recomputed = version => ({rule_version:version,evaluated_count:12,changed_count:3,as_of:'2026-10-11T12:00:00Z'});
const rules = load('src/pages/shareTracking/rules.tsx', {'../../api/shareTracking':{tracking:{}},'./model':model});
const flush = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve,reject; const promise=new Promise((a,b)=>{resolve=a;reject=b}); return{resolve,reject,promise}; };
const nodes = x => Array.isArray(x) ? x.flatMap(nodes) : !x || typeof x !== 'object' ? [] : [x,...nodes(x.props?.children)];
const text = x => Array.isArray(x) ? x.map(text).join('') : x == null || typeof x === 'boolean' ? '' : typeof x === 'object' ? text(x.props?.children) : String(x);
function harness({ mode='editor', overrides={}, props={}, confirm=()=>true }={}) {
  const React=require('react'); const calls=[]; const slots=[]; let index=0,tree,effects=[],uuid=0,active=true;
  const react={...React,useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useState(init){const i=index++;if(!(i in slots)) slots[i]=typeof init==='function'?init():init;return[slots[i],value=>{slots[i]=typeof value==='function'?value(slots[i]):value}]},useEffect(effect,deps){const i=index++;if(!slots[i]||!deps||deps.some((d,j)=>!Object.is(d,slots[i].deps[j]))){const old=slots[i];slots[i]={deps,effect,cleanup:old?.cleanup};effects.push(()=>{slots[i].cleanup?.();slots[i].cleanup=effect()})}}};
  const api={intentRules:async owner=>{calls.push(['read',owner]);return result({...base,owner_id:owner})},saveIntentRules:async (body,owner)=>{calls.push(['save',body,owner]);return {...result({...base,...body,owner_id:owner,rule_version:'r2',revision:1}),recomputed:recomputed('r2')}},recomputeIntent:async (body,owner)=>{calls.push(['recompute',body,owner]);return recomputed(body.rule_version)},...overrides};
  const module=load('src/pages/shareTracking/rules.tsx',{'react':react,'../../api/shareTracking':{tracking:api},'./model':model,globals:{crypto:{randomUUID:()=>`request-number-${++uuid}`},window:{confirm,addEventListener(){},removeEventListener(){}}}});
  const options={owner:'owner-a',canWrite:true,isCurrent:()=>active,context:{can_view:true,can_manage:false,can_rules:true,owners:[{id:'owner-a',name:'归属 A'},{id:'owner-b',name:'归属 B'}]},changed:()=>calls.push(['changed']),onBusyChange:v=>calls.push(['busy',v]),onDirtyChange:v=>calls.push(['dirty',v]),...props};
  function render(){index=0;effects=[];tree=mode==='panel'?module.IntentRulesPanel(options):module.IntentRuleEditor(options);effects.forEach(fn=>fn());return tree}
  const find=(type,label='')=>nodes(tree).find(n=>(n.type===type||typeof n.type==='function'&&n.type.name===type)&&text(n).includes(label));
  function field(label,value){const labelNode=find('label',label);assert.ok(labelNode,`missing ${label}`);const input=nodes(labelNode).find(n=>['input','textarea','select'].includes(n.type));input.props.onChange({target:{value}});render()}
  render();return{calls,options,render,find,field,text:()=>text(tree),get:label=>nodes(find('label',label)).find(n=>['input','textarea','select'].includes(n.type)).props.value,unmount(){active=false;slots.forEach(slot=>slot?.cleanup?.())},invalidate(){active=false},strictMount(){slots.forEach(slot=>{if(slot?.effect){slot.cleanup?.();slot.cleanup=slot.effect()}})}};
}
async function ready(options){const h=harness(options);await flush();h.render();return h}
function click(h,label){const button=h.find('button',label);assert.ok(button,`missing ${label}`);button.props.onClick();return button}

test('rule validation covers bounds, integer-only input, reason and impossible daily contributions',()=>{
 const valid=rules.ruleDraft(base); assert.equal(rules.validateRuleDraft(valid,'经验证'), '');
 for(const [key,values] of Object.entries({window_days:['0','91','1.5','','x'],medium_sessions:['1','101'],medium_favorites:['0','31'],high_business_results:['0','31'],daily_session_cap:['0','11'],daily_action_cap:['0','6']})) for(const value of values) assert.ok(rules.validateRuleDraft({...valid,[key]:value},'原因'));
 assert.ok(rules.validateRuleDraft(valid,' '));assert.ok(rules.validateRuleDraft(valid,'x'.repeat(501)));
 assert.ok(rules.validateRuleDraft({...valid,window_days:'1',medium_sessions:'4',daily_session_cap:'3'},'原因'));
 assert.ok(rules.validateRuleDraft({...valid,window_days:'1',medium_favorites:'3',daily_action_cap:'2'},'原因'));
 assert.equal(rules.validateRuleDraft({...valid,window_days:'1',high_business_results:'6',daily_action_cap:'2'},'原因'),'');
 assert.ok(rules.validateRuleDraft({...valid,window_days:'1',high_business_results:'7',daily_action_cap:'2'},'原因'));
 assert.equal(rules.validateRuleDraft({...valid,window_days:'90',medium_sessions:'100',medium_favorites:'30',high_business_results:'30'},'原因'),'');
});
test('rules API uses exact scoped endpoints and fixed version/request ID',async()=>{
 const calls=[];const api=load('src/api/shareTracking.ts',{'../utils/request':{request:async(...args)=>{calls.push(args);return {data:result()}}}});
 const body={...base,expected_rule_version:'r1',reason:'变更'};
 await api.tracking.intentRules('owner-a');await api.tracking.saveIntentRules(body,'owner-a');await api.tracking.recomputeIntent({request_id:'stable-id',reason:'核对',rule_version:'r1'},'owner-a');
 assert.deepEqual(calls.map(c=>[c[0],c[1].method]),[['/api/v1/management/tracking/intent-rules','GET'],['/api/v1/management/tracking/intent-rules','POST'],['/api/v1/management/tracking/intent-rules/recompute','POST']]);
 for(const [,request] of calls){assert.deepEqual(normalized(request.params),{owner:'owner-a'});assert.equal(request.headers['X-Tracking-Request'],'1')}
 assert.equal(JSON.parse(calls[2][1].body).request_id,'stable-id');
});
test('read-only rule access has no write buttons even when can_manage is granted',async()=>{
 const h=await ready({props:{canWrite:false,context:{can_manage:true}}});assert.ok(h.find('section','当前生效规则'));assert.equal(h.find('button','保存新版本'),undefined);assert.equal(h.find('button','按当前版本重算'),undefined);assert.match(h.text(),/没有规则配置或重算权限/);
});
test('explicit authorized owner is required even with one owner and unauthorized selection is rejected',()=>{
 const h=harness({mode:'panel',props:{context:{can_view:true,can_rules:true,owners:[{id:'owner-a',name:'归属 A'}]}}});
 assert.equal(h.get('规则归属范围'),'');assert.equal(h.find('IntentRuleEditor'),undefined);
 h.field('规则归属范围','not-authorized');assert.equal(h.find('IntentRuleEditor'),undefined);
 h.field('规则归属范围','owner-a');assert.equal(h.find('IntentRuleEditor').props.owner,'owner-a');
});
test('scope changes revoke old child callbacks; pending and dirty guards protect owner selection',()=>{
 let confirms=0,accept=false;const h=harness({mode:'panel',confirm:()=>{confirms++;return accept}});
 h.field('规则归属范围','owner-a');const old=h.find('IntentRuleEditor').props;old.onBusyChange(true);h.render();h.field('规则归属范围','owner-b');assert.equal(h.get('规则归属范围'),'owner-a');
 old.onBusyChange(false);old.onDirtyChange(true);h.render();h.field('规则归属范围','owner-b');assert.equal(confirms,1);assert.equal(h.get('规则归属范围'),'owner-a');
 accept=true;h.field('规则归属范围','owner-b');assert.equal(old.isCurrent(),false);assert.equal(h.find('IntentRuleEditor').props.owner,'owner-b');
 const before=h.calls.length;old.changed();old.onBusyChange(true);old.onDirtyChange(true);assert.equal(h.calls.length,before);
});
test('permission loss invalidates old scope immediately and replaces editable child with read-only',()=>{
 const h=harness({mode:'panel'});h.field('规则归属范围','owner-a');const old=h.find('IntentRuleEditor').props;
 h.options.context={...h.options.context,can_rules:false};h.render();assert.equal(old.isCurrent(),false);assert.equal(h.find('IntentRuleEditor').props.canWrite,false);
 h.options.context={...h.options.context,owners:[]};h.render();assert.equal(h.find('IntentRuleEditor'),undefined);assert.equal(h.get('规则归属范围'),'');
});
test('save locks duplicate clicks synchronously, pins owner/version and refreshes after server confirmation',async()=>{
 const request=deferred();let writes=0,readCount=0,body,owner;const h=await ready({overrides:{intentRules:async()=>{readCount++;return result(readCount>1?{...base,rule_version:'r2',revision:1}:base)},saveIntentRules:(b,o)=>{writes++;body=b;owner=o;return request.promise}}});
 h.field('评估窗口','45');h.field('变更原因','业务验证');const button=click(h,'保存新版本');button.props.onClick();assert.equal(writes,1);assert.equal(owner,'owner-a');assert.equal(body.window_days,45);assert.equal(body.expected_rule_version,'followup-v1');assert.ok(h.calls.some(c=>c[0]==='busy'&&c[1]===true));
 request.resolve({...result({...base,rule_version:'r2',revision:1,window_days:45}),recomputed:recomputed('r2')});await flush();h.render();assert.equal(readCount,2);assert.ok(h.calls.some(c=>c[0]==='changed'));assert.match(h.text(),/已保存新版本 r2/);assert.match(h.text(),/已评估 12 位访客/);assert.equal(h.get('变更原因'),'');button.props.onClick();assert.equal(writes,1);
});
test('save failure retains draft, blocks stale retries, and changed version needs explicit reconciliation',async()=>{
 let reads=0,writes=0,lastBody;const h=await ready({overrides:{intentRules:async()=>result(reads++?{...base,rule_version:'r2'}:base),saveIntentRules:async body=>{writes++;lastBody=body;throw new Error('409 rule_version conflict')}}});
 h.field('评估窗口','60');h.field('变更原因','保留此原因');const stale=click(h,'保存新版本');await flush();stale.props.onClick();assert.equal(writes,1);h.render();assert.equal(h.get('评估窗口'),'60');assert.equal(h.get('变更原因'),'保留此原因');assert.equal(h.find('button','保存新版本').props.disabled,true);
 click(h,'读取最新规则');await flush();h.render();assert.match(h.text(),/规则版本已变化/);click(h,'保存新版本');assert.equal(writes,1);
 click(h,'已核对，保留输入');h.render();click(h,'保存新版本');await flush();assert.equal(writes,2);assert.equal(lastBody.expected_rule_version,'r2');assert.equal(lastBody.window_days,60);
});
test('read refresh failure cannot manufacture success or erase inputs and can be retried',async()=>{
 let reads=0;const h=await ready({overrides:{intentRules:async()=>{if(reads++)throw new Error('离线');return result()}}});
 h.field('变更原因','不能丢');click(h,'读取最新规则');await flush();h.render();assert.match(h.text(),/离线/);assert.equal(h.get('变更原因'),'不能丢');assert.equal(h.calls.some(c=>c[0]==='changed'),false);
});
test('wrong owner read and malformed save result are rejected rather than treated as confirmed',async()=>{
 const wrong=await ready({overrides:{intentRules:async()=>result({...base,owner_id:'owner-b'})}});assert.match(wrong.text(),/范围或内容不匹配/);assert.equal(wrong.find('button','保存新版本'),undefined);
 const h=await ready({overrides:{saveIntentRules:async()=>({...result({...base,rule_version:'r2'}),recomputed:{...recomputed('r2'),changed_count:999}})}});h.field('变更原因','原因');click(h,'保存新版本');await flush();h.render();assert.match(h.text(),/未确认新规则与重算结果/);assert.equal(h.get('变更原因'),'原因');assert.equal(h.calls.some(c=>c[0]==='changed'),false);
});
test('confirmed mutation remains successful when the subsequent verification read fails',async()=>{
 let reads=0;const h=await ready({overrides:{intentRules:async()=>{if(reads++)throw new Error('读取失败');return result()}}});h.field('变更原因','原因');click(h,'保存新版本');await flush();h.render();assert.match(h.text(),/已保存新版本 r2/);assert.match(h.text(),/操作已确认，但最新规则读取失败/);assert.equal(h.calls.filter(c=>c[0]==='save').length,1);assert.equal(h.calls.filter(c=>c[0]==='changed').length,1);
});
test('recompute retries retain request ID and reason after verification; changed reason uses a new ID',async()=>{
 const bodies=[];const h=await ready({overrides:{recomputeIntent:async body=>{bodies.push({...body});throw new Error('timeout')}}});
 h.field('重算原因','核对删除后证据');click(h,'按当前版本重算');await flush();h.render();assert.equal(h.get('重算原因'),'核对删除后证据');click(h,'按当前版本重算');assert.equal(bodies.length,1);
 click(h,'读取最新规则');await flush();h.render();click(h,'按当前版本重算');await flush();h.render();assert.equal(bodies[1].request_id,bodies[0].request_id);assert.equal(bodies[1].rule_version,'followup-v1');
 click(h,'读取最新规则');await flush();h.render();h.field('重算原因','新核对');click(h,'按当前版本重算');await flush();assert.notEqual(bodies[2].request_id,bodies[0].request_id);
});
test('recompute refuses unsaved rule input and successful result refreshes records',async()=>{
 const h=await ready();h.field('评估窗口','45');h.field('重算原因','核对');click(h,'按当前版本重算');await flush();h.render();assert.equal(h.calls.some(c=>c[0]==='recompute'),false);assert.match(h.text(),/请先保存规则修改/);
 click(h,'恢复当前生效规则');h.render();click(h,'按当前版本重算');await flush();h.render();assert.equal(h.calls.filter(c=>c[0]==='recompute').length,1);assert.match(h.text(),/已评估 12 位访客/);assert.equal(h.get('重算原因'),'');assert.ok(h.calls.some(c=>c[0]==='changed'));
});
test('unmount and scope invalidation ignore late reads and mutations and block stale actions',async()=>{
 for(const finish of ['unmount','invalidate']) {
  const request=deferred();const h=await ready({overrides:{saveIntentRules:()=>request.promise}});h.field('变更原因','原因');const old=click(h,'保存新版本');h[finish]();request.resolve({...result({...base,rule_version:'r2'}),recomputed:recomputed('r2')});await flush();old.props.onClick();assert.equal(h.calls.some(c=>c[0]==='changed'),false);
 }
 const read=deferred();const h=harness({overrides:{intentRules:()=>read.promise}});h.unmount();read.resolve(result());await flush();h.render();assert.equal(h.find('button','保存新版本'),undefined);
});
test('StrictMode effect replay aborts first read and accepts only the latest response',async()=>{
 const first=deferred(),second=deferred();const signals=[];let reads=0;const h=harness({overrides:{intentRules:(_owner,signal)=>{signals.push(signal);return reads++?second.promise:first.promise}}});h.strictMount();assert.equal(signals[0].aborted,true);second.resolve(result({...base,rule_version:'r2'}));await flush();first.resolve(result());await flush();h.render();h.field('变更原因','验证');click(h,'保存新版本');await flush();assert.equal(h.calls.find(c=>c[0]==='save')[1].expected_rule_version,'r2');
});
test('old rendered submit cannot commit obsolete form values',async()=>{
 const h=await ready();h.field('变更原因','原因');const old=h.find('button','保存新版本');h.field('评估窗口','45');old.props.onClick();assert.equal(h.calls.some(c=>c[0]==='save'),false);
});
test('evidence displays real metrics thresholds exclusions calculation override and capped source records',()=>{
 const {IntentEvidenceDetails}=load('src/pages/shareTracking/evidence.tsx',{'./model':model});const {renderToStaticMarkup}=require('react-dom/server');
 const evidence={level:'high',rule_version:'r2',window_start:'2026-09-11',window_end:'2026-10-11',calculated_at:'2026-10-11T12:00:00Z',timezone:'Asia/Shanghai',reasons:['服务端已确认预约'],metrics:{sessions:3,favorites:1,business_results:1},thresholds:base,exclusions:['测试流量'],participating_metrics:['sessions','favorites','business_results'],override_state:'active',evidence_truncated:true,evidence:[{kind:'business',record_id:'verified-42',session_id:'session-42',page_view_id:'pv-42',action:'appointment',occurred_at:'2026-10-10',content_id:'content-42',content_version:2}]};
 const html=renderToStaticMarkup(IntentEvidenceDetails({evidence,onOpenRecords(){}}));for(const value of ['计算时间','有效人工覆盖','有效会话 3 次','已确认收藏 1 次','测试流量','预约成功','verified-42','session-42','pv-42','content-42','v2','部分证据','查看有权访问'])assert.ok(html.includes(value),value);
 const scope=renderToStaticMarkup(IntentEvidenceDetails({evidence:{...evidence,scope_required:true,level:'unknown',override_state:'expired'}}));assert.match(scope,/请选择单一跟进归属/);assert.match(scope,/人工覆盖已过期/);
});
test('rule panel shares the single tracking menu and existing guarded dialog',()=>{
 const page=fs.readFileSync(new URL('src/pages/shareTracking.tsx',root),'utf8');assert.match(page,/setSelected\(\{ rules: true \}\)/);assert.match(page,/<IntentRulesPanel context=\{context\} changed=\{changed\} onBusyChange=\{governanceBusy\} onDirtyChange=\{governanceDirty\}/);assert.match(page,/editorBusy \|\| governanceState.current.busy/);assert.match(page,/editorDirty \|\| governanceState.current.dirty/);
});

test('evidence window query preserves authorized owner and precise as_of while clearing list/content filters',()=>{
 const query={owner:'owner-a',start:'2026-10-09',end:'2026-10-12',as_of:'old',content_id:'selected-content',cursor:'old-page',identity:'identified',intent:'high',q:'客户',limit:20,metric_version:'metric-v1'};
 const evidence={window_start:'2026-09-11T12:00:00Z',window_end:'2026-10-11T12:00:00.123456789Z'};
 assert.deepEqual(normalized(model.intentWindowQuery(query,evidence)),{owner:'owner-a',start:evidence.window_start,as_of:evidence.window_end,limit:20,metric_version:'metric-v1'});
 for(const invalid of [undefined,{...evidence,scope_required:true},{...evidence,window_start:'bad'},{...evidence,window_start:evidence.window_end}])assert.equal(model.intentWindowQuery(query,invalid),undefined);
});
test('detail evidence navigation uses its own window and restores list scope without altering owner',()=>{
 const React=require('react');let index=0,slots=[],tree;const calls=[];
 const react={...React,useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useState(init){const i=index++;if(!(i in slots))slots[i]=typeof init==='function'?init():init;return[slots[i],value=>{slots[i]=typeof value==='function'?value(slots[i]):value}]}};
 const session={session_id:'source-session',page_views:[{page_view_id:'source-pv'}]};
 const components={ContentBody:'ContentBody',Counts:'Counts',Feedback:'Feedback',MediaPreview:'MediaPreview',Pager:'Pager',SnapshotInfo:'SnapshotInfo',VisitorCard:'VisitorCard',useResource:loader=>{void loader(new AbortController().signal);return{data:{items:[session]}}}};
 const {VisitorDetails}=load('src/pages/shareTracking/details.tsx',{'react':react,'../../api/shareTracking':{tracking:{sessions:async(key,query)=>{calls.push([key,query]);return{items:[]}}}},'./model':model,'./components':components,'./crmTarget':{CRMTargetButton:'CRMTargetButton'},'./governance':{VisitorGovernance:'VisitorGovernance'}});
 const visitor={visitor_key:'u:42',identity_state:'identified',intent:{window_start:'2026-09-11',window_end:'2026-10-11',evidence:[{session_id:'source-session'}]}};
 const query={owner:'owner-a',start:'2026-10-09',end:'2026-10-12',as_of:'2026-10-11',limit:20};
 function render(){index=0;tree=VisitorDetails({visitor,query,contentId:'content-filter'});return tree}render();assert.equal(calls.at(-1)[1].content_id,'content-filter');
 nodes(tree).find(node=>node.type==='VisitorGovernance').props.onOpenEvidence();render();assert.equal(calls.at(-1)[1].owner,'owner-a');assert.equal(calls.at(-1)[1].start,'2026-09-11');assert.equal(calls.at(-1)[1].end,undefined);assert.equal(calls.at(-1)[1].as_of,'2026-10-11');assert.equal(calls.at(-1)[1].content_id,undefined);assert.match(text(tree),/意向来源范围/);
 const page=nodes(tree).find(node=>typeof node.type==='function'&&node.type.name==='PageVisit');assert.ok(page);assert.equal(page.props.query.start,'2026-09-11');assert.equal(page.props.query.owner,'owner-a');
 nodes(tree).find(node=>node.type==='button'&&text(node)==='返回列表筛选范围').props.onClick();render();assert.equal(calls.at(-1)[1].start,'2026-10-09');assert.equal(calls.at(-1)[1].end,'2026-10-12');assert.equal(calls.at(-1)[1].content_id,'content-filter');
 visitor.intent.scope_required=true;render();assert.equal(nodes(tree).find(node=>node.type==='VisitorGovernance').props.onOpenEvidence,undefined);
});

test('actual version trajectory propagates the clicked page identity through PageVisit into Events',()=>{
 const React=require('react');let index=0,slots=[],eventPhase=false;const calls=[];
 const react={...React,useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useState(init){const i=index++;if(!(i in slots))slots[i]=typeof init==='function'?init():init;return[slots[i],value=>{slots[i]=typeof value==='function'?value(slots[i]):value}]}};
 for(const scenario of [{view:{page_view_id:'pv-A',content_id:'content-A',content_version:3},related:false},{view:{page_view_id:'pv-B',content_id:'content-B',content_version:8},related:true},{view:{page_view_id:'pv-page',route_key:'pages/home/home'},related:true}]){
  slots=[];index=0;eventPhase=false;const session={session_id:'source-session',page_views:[scenario.view]};
  const components={ContentBody:'ContentBody',Counts:'Counts',Feedback:'Feedback',MediaPreview:'MediaPreview',Pager:'Pager',SnapshotInfo:'SnapshotInfo',VisitorCard:'VisitorCard',useResource:loader=>{void loader(new AbortController().signal);return{data:{items:eventPhase?[]:[session]}}}};
  const {VisitorDetails}=load('src/pages/shareTracking/details.tsx',{'react':react,'../../api/shareTracking':{tracking:{sessions:async()=>({items:[session]}),events:async(id,query)=>{calls.push({id,query});return{items:[]}}}},'./model':model,'./components':components,'./crmTarget':{CRMTargetButton:'CRMTargetButton'},'./governance':{VisitorGovernance:'VisitorGovernance'}});
  const props={visitor:{visitor_key:'u:42',identity_state:'identified'},query:{owner:'owner-a',as_of:'2026-10-12T00:00:00Z',content_version:3,content_q:'标题'},contentId:'content-A'};
  let tree=VisitorDetails(props);if(scenario.related){nodes(tree).find(n=>n.type==='input'&&n.props.type==='checkbox').props.onChange({target:{checked:true}});index=0;tree=VisitorDetails(props)}
  nodes(tree).find(n=>n.type==='button'&&n.props['aria-expanded']===false).props.onClick();index=0;tree=VisitorDetails(props);
  const page=nodes(tree).find(n=>typeof n.type==='function'&&n.type.name==='PageVisit');assert.ok(page);slots=[];index=0;let pageTree=page.type(page.props);nodes(pageTree).find(n=>n.type==='button').props.onClick();index=0;pageTree=page.type(page.props);
  const events=nodes(pageTree).find(n=>typeof n.type==='function'&&n.type.name==='Events');assert.ok(events);slots=[];index=0;eventPhase=true;events.type(events.props);
  const last=calls.at(-1);assert.equal(last.id,scenario.view.page_view_id);assert.equal(last.query.content_id,scenario.view.content_id);assert.equal(last.query.content_version,scenario.view.content_version);assert.equal(last.query.owner,'owner-a');assert.equal(last.query.as_of,props.query.as_of);assert.equal(last.query.content_q,scenario.related?undefined:'标题');
 }
});

test('session detail pagination replays first cursor and reset after explicit refresh or scope change',async()=>{
 const React=require('react');let index=0,slots=[],tree;const calls=[];
 const react={...React,useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useState(init){const i=index++;if(!(i in slots))slots[i]=typeof init==='function'?init():init;return[slots[i],value=>{slots[i]=typeof value==='function'?value(slots[i]):value}]}};
 const page={items:[],first_cursor:'bound-first',next_cursor:'bound-next'};
 const components={ContentBody:'ContentBody',Counts:'Counts',Feedback:'Feedback',MediaPreview:'MediaPreview',Pager:'Pager',SnapshotInfo:'SnapshotInfo',VisitorCard:'VisitorCard',useResource:loader=>{void loader(new AbortController().signal);return{data:page,reload(){calls.push(['reload'])}}}};
 const capture=async(id,query)=>{calls.push([id,query]);return page};
 const {VisitorDetails}=load('src/pages/shareTracking/details.tsx',{'react':react,'../../api/shareTracking':{tracking:{sessions:capture,events:capture}},'./model':model,'./components':components,'./crmTarget':{CRMTargetButton:'CRMTargetButton'},'./governance':{VisitorGovernance:'VisitorGovernance'}});
 let props={visitor:{visitor_key:'u:42',identity_state:'identified'},query:{owner:'owner-a',as_of:'2026-10-12T00:00:00Z'},contentId:'content-A'};
 const render=()=>{index=0;tree=VisitorDetails(props)};
 render();await flush();assert.equal(calls.at(-1)[1].cursor,undefined);
 nodes(tree).find(n=>n.type==='Pager').props.onNext('bound-next');render();await flush();assert.equal(calls.at(-1)[1].cursor,'bound-next');
 nodes(tree).find(n=>n.type==='Pager').props.onPrevious();render();await flush();assert.equal(calls.at(-1)[1].cursor,'bound-first');
 nodes(tree).find(n=>n.type==='Feedback').props.resetSnapshot();render();await flush();assert.equal(calls.at(-1)[1].cursor,undefined);
 nodes(tree).find(n=>n.type==='Pager').props.onNext('old-A-next');render();await flush();
 props={...props,visitor:{...props.visitor,visitor_key:'u:43'}};render();await flush();assert.equal(calls.at(-1)[0],'u:43');assert.equal(calls.at(-1)[1].cursor,undefined);
 props={...props,visitor:{...props.visitor,visitor_key:'u:42'}};render();await flush();assert.equal(calls.at(-1)[1].cursor,undefined);
});
