// Execute real report/drawer handlers with controlled state; no browser-layout claims.
import assert from 'node:assert/strict';
import test from 'node:test';
import {environment,deferred} from './helpers/crm-harness.mjs';
const event={preventDefault(){}};
function escape(env,extra={}) {
 const e={key:'Escape',defaultPrevented:false,preventDefault(){this.defaultPrevented=true},stopPropagation(){this.stopped=true},stopImmediatePropagation(){this.immediateStopped=true},...extra};
 for(const fn of env.events.get('keydown')||[]){fn(e);if(e.immediateStopped)break}return e;
}
async function newReport(){
 const env=environment();env.session={...env.session,user:{...env.session.user,crmEnabled:true},crmContext:{account:{role:'admin',username:'crm1',displayName:'Test'},demo:true}};
 const root=await env.mount('customerManagement.tsx');root.button('＋ 新增客户报备').props.onClick();root.render();const node=root.find(n=>typeof n.type==='function'&&n.type.name==='CustomerReportForm');
 const child=await env.mount('customerReportForm.tsx','default',node.props);return {env,root,child};
}
const reportCount=root=>root.all(n=>typeof n.type==='function'&&n.type.name==='CustomerReportForm').length;
const customer=id=>({id,version:1,name:id,phone:'13800000000',lifecycle:'protected',stage:'following',category:'建房咨询',intent:'high'});
const detail=c=>({customer:c,relations:[],followups:[],attachments:[],sources:[],cases:[]});

test('new CRM report: idle Escape invokes the real parent cancel once',async()=>{
 const {env,root}=await newReport();const e=escape(env);root.render();assert.equal(reportCount(root),0);assert.equal(e.defaultPrevented,true);assert.equal(e.immediateStopped,true);
});

test('new CRM report: same-frame submit locks both Escape and cached cancel until rejection',async()=>{
 const {env,root,child}=await newReport(),pending=deferred();let writes=0;env.crm=()=>{writes++;return pending.promise};
 const submit=child.find(n=>n.type==='form').props.onSubmit,cancel=child.button('取消').props.onClick;
 const first=submit(event);await submit(event);escape(env);cancel();root.render();child.render();assert.equal(writes,1);assert.equal(reportCount(root),1);assert.equal(child.button('取消').props.disabled,true);
 pending.reject(new Error('fixture save failed'));await first;escape(env);root.render();assert.equal(reportCount(root),0);
});

test('new CRM report: composition and already-consumed palette keys do not dismiss the report',async()=>{
 const {env,root}=await newReport();escape(env,{isComposing:true});escape(env,{defaultPrevented:true});root.render();assert.equal(reportCount(root),1);
});

test('report keyboard: old target and unmounted handlers cannot dismiss the newer editor',async()=>{
 const env=environment();let cancelled=0;const child=await env.mount('customerReportForm.tsx','default',{customer:customer('A'),cancel(){cancelled++},saved(){}});
 const oldKey=[...env.events.get('keydown')][0],oldCancel=child.button('取消').props.onClick;
 child.props={...child.props,customer:customer('B')};child.render();oldCancel();oldKey({key:'Escape',preventDefault(){},stopPropagation(){}});assert.equal(cancelled,0);
 await child.flush();escape(env);assert.equal(cancelled,1);const currentKey=[...env.events.get('keydown')][0];child.unmount();currentKey({key:'Escape',preventDefault(){},stopPropagation(){}});assert.equal(cancelled,1);
});

test('embedded report Escape goes through the real drawer mode guard and retains the detail drawer',async()=>{
 const env=environment(),pending=deferred();let closed=0,writes=0;env.crm=async(url,o={})=>{if(o.method){writes++;return pending.promise}return url==='/employees'?[]:detail(customer('A'))};
 const drawer=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',supervisor:true,close(){closed++},changed(){},checkPool(){}});
 drawer.button('编辑报备资料').props.onClick();await drawer.flush();const node=drawer.find(n=>typeof n.type==='function'&&n.type.name==='CustomerReportForm');const child=await env.mount('customerReportForm.tsx','default',node.props);
 const first=child.find(n=>n.type==='form').props.onSubmit(event);escape(env);child.button('取消').props.onClick();drawer.render();assert.equal(closed,0);assert.equal(reportCount(drawer),1);assert.equal(writes,1);
 pending.reject(new Error('fixture rejected'));await first;escape(env);drawer.render();assert.equal(reportCount(drawer),0);assert.equal(closed,0);drawer.button('编辑报备资料');
});

for(const success of [false,true])test(`legacy pending close guard: ${success?'success':'failure'} unlocks after all same-frame close/reopen attempts are refused`,async()=>{
 const env=environment(),pending=deferred();let writes=0;env.leadApi.updateCustomerLeadStatus=()=>{writes++;return pending.promise};
 const h=await env.mount('customerLeads.tsx');await h.button('详情').props.onClick();await h.flush();
 const detailClose=h.find(n=>n.type==='button'&&n.props.className==='customer-lead-modal-close').props.onClick,open=h.button('记录跟进').props.onClick,switchDetail=h.button('详情').props.onClick;
 open();h.render();h.find(n=>n.type==='textarea').props.onChange({target:{value:'Keep this draft'}});h.render();
 const cancel=h.button('取消').props.onClick,save=h.button('保存跟进').props.onClick;save();cancel();detailClose();open();await switchDetail();save();h.render();
 assert.equal(writes,1);assert.equal(h.all(n=>n.props?.className==='customer-lead-action-modal').length,1);assert.equal(h.all(n=>n.props?.className==='customer-lead-modal').length,1);
 assert.equal(h.button('取消').props.disabled,true);for(const close of h.all(n=>n.props?.className==='customer-lead-modal-close'))assert.equal(close.props.disabled,true);
 if(success)pending.resolve();else pending.reject(new Error('fixture save failed'));await h.flush();
 if(success)assert.equal(h.all(n=>n.props?.className==='customer-lead-action-modal').length,0);
 else {assert.equal(h.find(n=>n.type==='textarea').props.value,'Keep this draft');cancel();h.render();assert.equal(h.all(n=>n.props?.className==='customer-lead-action-modal').length,0)}
 detailClose();h.render();assert.equal(h.all(n=>n.props?.className==='customer-lead-modal').length,0);assert.equal(writes,1);
});
