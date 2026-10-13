import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';
import { environment, account } from './helpers/crm-harness.mjs';
const require=createRequire(import.meta.url), React=require('react');
const flush=()=>new Promise(r=>setImmediate(r));
const target={target_kind:'customer',customer_id:'customer-42',business_number:'C42',owner_id:'owner-a'};
const visitor={visitor_key:'u:42',identity_state:'identified',crm_label:'C42'};
function buttonHarness(resolve=async()=>target){
 let index=0,slots=[],pending=[],tree;const calls=[],navigation=[];
 const react={...React,useState(init){const i=index++;if(!(i in slots))slots[i]=typeof init==='function'?init():init;return[slots[i],v=>slots[i]=typeof v==='function'?v(slots[i]):v]},useRef(init){const i=index++;if(!(i in slots))slots[i]={current:init};return slots[i]},useEffect(fn,deps){const i=index++;const old=slots[i];if(!old||deps.some((v,j)=>v!==old.deps[j])){slots[i]={deps,cleanup:old?.cleanup};pending.push(()=>{slots[i].cleanup?.();slots[i].cleanup=fn()})}}};
 const module={exports:{}};const source=fs.readFileSync(new URL('../src/pages/shareTracking/crmTarget.tsx',import.meta.url),'utf8');
 const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,jsx:ts.JsxEmit.ReactJSX,esModuleInterop:true,target:ts.ScriptTarget.ES2020}}).outputText;
 vm.runInNewContext(code,{module,exports:module.exports,AbortController,Error,require(name){if(name==='react')return react;if(name==='react-router-dom')return{useNavigate:()=>path=>navigation.push(path)};if(name==='../../api/shareTracking')return{tracking:{crmTarget:async(...args)=>{calls.push(args);return resolve(...args)}}};return require(name)}});
 const h={props:{visitor,owner:'owner-a',canCRM:true},calls,navigation,render(effects=true){index=0;tree=module.exports.CRMTargetButton(h.props);if(effects)pending.splice(0).forEach(fn=>fn());return tree},button(){return tree?.props.children[0]},unmount(){slots.forEach(slot=>slot?.cleanup?.())}};h.render();h.render();return h;
}
test('CRM CTA revalidates exact mapping before only fixed internal navigation and is single-flight',async()=>{
 let yes;const h=buttonHarness(()=>new Promise(r=>yes=r));const button=h.button();button.props.onClick();button.props.onClick();assert.equal(h.calls.length,1);assert.equal(h.navigation.length,0);assert.equal(h.calls[0][0],'u:42');assert.equal(h.calls[0][1],'owner-a');yes(target);await flush();assert.deepEqual(h.navigation,['/customer-management?tab=customers&customer=customer-42&from=share-tracking']);
});
test('CRM CTA suppresses anonymous, no permission and unmapped visitors',()=>{
 for(const props of [{canCRM:false},{visitor:{...visitor,identity_state:'anonymous'}},{visitor:{...visitor,crm_label:''}}]){const h=buttonHarness();h.props={...h.props,...props};assert.equal(h.render(),null);assert.equal(h.calls.length,0)}
});
test('CRM CTA retains failure for retry and never follows invalid target or revoked source',async()=>{
 for(const resolve of [async()=>{throw new Error('权限已撤销')},async()=>({...target,target_kind:'url'}),async()=>({...target,customer_id:'../other'}),async()=>({...target,owner_id:'other'})]){const h=buttonHarness(resolve);await h.button().props.onClick();h.render();assert.equal(h.navigation.length,0);assert.equal(h.button().props.disabled,false);await h.button().props.onClick();assert.equal(h.calls.length,2)}
});
test('CRM CTA discards late success on owner, identity, permission change or unmount',async()=>{
 for(const change of ['owner','identity','permission','unmount']){let yes;const h=buttonHarness(()=>new Promise(r=>yes=r));h.button().props.onClick();if(change==='unmount')h.unmount();else{h.props={...h.props,...(change==='owner'?{owner:'owner-b'}:change==='permission'?{canCRM:false}:{visitor:{...visitor,identity_state:'deleted'}})};h.render(false)}yes(target);await flush();assert.equal(h.navigation.length,0,change);if(change!=='unmount')h.render()}
});
function crmEnv(query){const env=environment();env.session.crmContext={account:account(7),demo:false};env.params=new URLSearchParams(query);return env}
function drawer(h){return h.all(n=>typeof n.type==='function'&&n.type.name==='CustomerDrawer')[0]}
test('customer route opens canonical internal ID and close removes only customer parameter',async()=>{
 const env=crmEnv('tab=customers&customer=customer-42&from=share-tracking');const h=await env.mount('customerManagement.tsx');assert.equal(drawer(h).props.id,'customer-42');drawer(h).props.close();await h.flush();assert.equal(env.params.has('customer'),false);assert.equal(env.params.get('tab'),'customers');assert.equal(env.params.get('from'),'share-tracking');assert.equal(drawer(h),undefined);assert.match(h.text(),/返回分享访问记录/);
});
test('customer URL back/forward changes exact target; invalid or unauthorized route never mounts drawer',async()=>{
 const env=crmEnv('tab=customers&customer=customer-A');const h=await env.mount('customerManagement.tsx');assert.equal(drawer(h).props.id,'customer-A');env.params=new URLSearchParams('tab=customers&customer=customer-B');h.render();assert.equal(drawer(h).props.id,'customer-B');env.params=new URLSearchParams('tab=customers&customer=customer-A');h.render();assert.equal(drawer(h).props.id,'customer-A');for(const id of ['../other','bad/target','', 'x'.repeat(129)]){env.params=new URLSearchParams({tab:'customers',customer:id});h.render();assert.equal(drawer(h),undefined,id)}env.params=new URLSearchParams({tab:'customers',customer:'customer-A'});env.session.crmContext=null;h.render();assert.equal(drawer(h),undefined);
});
