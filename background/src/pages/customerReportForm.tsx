import React,{useEffect,useRef,useState} from 'react';
import {crm,CRMError,type Customer,type Detail} from '../api/wecomCustomers';
import type {CustomerMutationGate} from './customerWorkflow';

const reportForm=(customer?:Customer)=>({name:customer?.name||'',wechat:customer?.wechat||'',phone:customer?.phone||'',address:customer?.address||'',buildingArea:customer?.buildingArea?.toString()||'',buildingType:customer?.buildingType||'新建',expectedBuildTime:customer?.expectedBuildTime||'',remark:customer?.remark||''});

export default function CustomerReportForm({customer,cancel,saved,beginMutation}:{customer?:Customer;cancel:()=>void;saved:(c:Customer)=>void;beginMutation?:CustomerMutationGate}) {
 const [form,setForm]=useState(()=>reportForm(customer));const [formTarget,setFormTarget]=useState(customer?.id);
 const [duplicate,setDuplicate]=useState(false),[reviewReason,setReviewReason]=useState('');
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[success,setSuccess]=useState('');
 const [version,setVersion]=useState(customer?.version),[conflict,setConflict]=useState(false),[latest,setLatest]=useState<Customer|null>(null);
 const operation=useRef({busy:false,completed:false,alive:true,conflict:false,target:customer?.id,epoch:0});
 if(operation.current.target!==customer?.id){operation.current.target=customer?.id;operation.current.epoch++;operation.current.busy=false;operation.current.completed=false;}
 operation.current.conflict=conflict;const epoch=operation.current.epoch;
 const current=()=>operation.current.alive&&operation.current.epoch===epoch;
 useEffect(()=>{setFormTarget(customer?.id);setForm(reportForm(customer));setVersion(customer?.version);setConflict(false);setLatest(null);setDuplicate(false);setReviewReason('');setBusy(false);setError('');setSuccess('')},[customer?.id]);
 useEffect(()=>{operation.current.alive=true;return()=>{operation.current.alive=false;operation.current.epoch++}},[]);
 const formRef=useRef<HTMLFormElement>(null);
 useEffect(()=>{const previous=document.activeElement as HTMLElement;const key=(e:KeyboardEvent)=>{
  if(e.key==='Tab'){const items=Array.from(formRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])')||[]).filter(el=>el.getClientRects().length);const first=items[0],last=items[items.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}
 };window.addEventListener('keydown',key);return()=>{window.removeEventListener('keydown',key);previous?.focus()}},[]);
 const field=(key:keyof typeof form,value:string)=>setForm(f=>({...f,[key]:value}));
 const submit=async(e:React.FormEvent)=>{
  e.preventDefault();if(!current()||formTarget!==customer?.id||operation.current.busy||operation.current.completed||operation.current.conflict)return;
  const release=beginMutation?.();if(beginMutation&&!release)return;operation.current.busy=true;setBusy(true);setError('');let result:Customer;
  try{const report={...form,buildingArea:Number(form.buildingArea)};result=await crm<Customer>(customer?'/customers/'+encodeURIComponent(customer.id)+'/report':'/customers',{method:customer?'PATCH':'POST',body:customer?{version,report}:report})}
  catch(e){if(current()){setDuplicate(!customer&&e instanceof CRMError&&e.status===409);operation.current.conflict=!!customer&&e instanceof CRMError&&(e.status===409||e.status===403);setConflict(operation.current.conflict);setError(e instanceof Error?e.message:'报备失败，请重试');operation.current.busy=false;setBusy(false)}return}finally{release?.()}
  if(!current())return;operation.current.completed=true;operation.current.busy=false;setBusy(false);setSuccess('资料已保存，无需重复提交。');
  try{saved(result)}catch{setError('资料已保存，但界面更新失败，请重新读取核对。')}
 };
 const compare=async()=>{if(!customer||!current()||operation.current.busy)return;operation.current.busy=true;setBusy(true);setError('');setLatest(null);
  try{const data=await crm<Detail>('/customers/'+encodeURIComponent(customer.id));if(!current())return;if(data.customer.id!==customer.id)throw new Error('档案已合并或目标已变化，请返回客户列表打开当前主档核对');setLatest(data.customer)}catch(e){if(current())setError(e instanceof Error?e.message:'读取最新资料失败')}finally{if(current()){operation.current.busy=false;setBusy(false)}}
 };
 const adopt=(keep:boolean)=>{if(!current()||operation.current.busy||!latest)return;setVersion(latest.version);if(!keep)setForm(reportForm(latest));setConflict(false);setLatest(null);setError('');setSuccess(keep?'已采用核对后的版本，请确认当前输入再保存。':'已重新载入最新资料。')};
 const review=async()=>{if(!current()||operation.current.busy||operation.current.completed||!duplicate||!reviewReason.trim())return;const release=beginMutation?.();if(beginMutation&&!release)return;operation.current.busy=true;setBusy(true);setError('');
  try{await crm('/report-review',{method:'POST',body:{report:{...form,buildingArea:Number(form.buildingArea)},reason:reviewReason.trim()}});if(!current())return;operation.current.completed=true;setSuccess('已提交主管核实，结果会通过我的提醒通知，无需重复提交。');setDuplicate(false)}catch(e){if(current())setError(e instanceof Error?e.message:'提交失败')}finally{release?.();if(current()){operation.current.busy=false;setBusy(false)}}
 };
 return <form ref={formRef} className="crm-form crm-report-form" onSubmit={submit}>
  <p className="crm-report-tip">{customer?'修改资料不延长保护期':'快速录入，报备后享 90 天专属保护'}</p>
  {error&&<div className="crm-feedback" role="alert">{error}</div>}
  {success&&<div className="crm-note" role="status">{success}</div>}
  {conflict&&<div className="crm-note"><p>保存存在冲突，可能是资料版本、联系电话、访问权限或归属状态已变化。输入已保留，请读取最新资料并核对。</p><button type="button" disabled={busy} onClick={compare}>读取最新报备资料</button>{latest&&<><p>最新版本 {latest.version}：{latest.name}；微信 {latest.wechat||'未填写'}；电话 {latest.phone}；地址 {latest.address}；面积 {latest.buildingArea} ㎡；需求 {latest.buildingType}；预计建房 {latest.expectedBuildTime}；备注 {latest.remark||'无'}；归属 {latest.ownerUsername||latest.ownerId||'未分配'}；状态 {latest.lifecycle}。</p><button type="button" disabled={busy} onClick={()=>adopt(true)}>已核对，保留输入继续编辑</button><button type="button" disabled={busy} onClick={()=>adopt(false)}>重新载入最新资料</button></>}</div>}
  <div className="crm-report-fields">
   <div className="crm-report-row"><label className="crm-report-label" htmlFor="report-name">客户姓名 / 微信</label><div className="crm-report-pair"><input id="report-name" aria-label="客户姓名" autoFocus disabled={busy||operation.current.completed} required maxLength={200} value={form.name} onChange={e=>field('name',e.target.value)} placeholder="客户姓名"/><input aria-label="微信号（选填）" disabled={busy||operation.current.completed} maxLength={80} value={form.wechat} onChange={e=>field('wechat',e.target.value)} placeholder="微信号（选填）"/></div></div>
   <label className="crm-report-row"><span className="crm-report-label">联系电话</span><input disabled={busy||operation.current.completed} required type="tel" maxLength={80} value={form.phone} onChange={e=>field('phone',e.target.value)} placeholder="手机号或带区号的固定电话"/></label>
   <label className="crm-report-row"><span className="crm-report-label">镇村地址</span><input disabled={busy||operation.current.completed} required maxLength={500} value={form.address} onChange={e=>field('address',e.target.value)} placeholder="省市县、乡镇、村组及门牌"/></label>
   <div className="crm-report-row"><label className="crm-report-label" htmlFor="report-area">面积 / 建房需求</label><div className="crm-report-pair"><div className="crm-report-area"><input id="report-area" aria-label="建房面积（平方米）" disabled={busy||operation.current.completed} required type="number" min="0.01" max="1000000" step="0.01" value={form.buildingArea} onChange={e=>field('buildingArea',e.target.value)} placeholder="建房面积"/><span>㎡</span></div><select aria-label="建房需求" disabled={busy||operation.current.completed} value={form.buildingType} onChange={e=>field('buildingType',e.target.value)}>{['新建','翻建','改造','装修'].map(t=><option key={t}>{t}</option>)}</select></div></div>
   <label className="crm-report-row"><span className="crm-report-label">预计建房时间</span><input disabled={busy||operation.current.completed} required maxLength={100} value={form.expectedBuildTime} onChange={e=>field('expectedBuildTime',e.target.value)} placeholder="例如：明年春天、2027 年 3 月"/></label>
  </div>
  <details className="crm-report-extra" open={form.remark?true:undefined}><summary>补充备注（选填）</summary><textarea aria-label="补充备注" disabled={busy||operation.current.completed} rows={3} maxLength={2000} value={form.remark} onChange={e=>field('remark',e.target.value)} placeholder="预算、宅基地情况等"/></details>
  {duplicate&&!customer&&<details><summary>申请主管核实共用电话 / 不同客户</summary><label>核实说明<textarea maxLength={1000} value={reviewReason} onChange={e=>setReviewReason(e.target.value)} placeholder="请说明不同姓名、地址及共用电话原因"/></label><button type="button" disabled={busy||!reviewReason.trim()} onClick={review}>提交核实申请</button></details>}
  <footer className="crm-form-footer"><button type="button" disabled={busy} onClick={cancel}>取消</button><button className="crm-primary" disabled={busy||conflict||operation.current.completed||formTarget!==customer?.id}>{busy?'正在保存…':customer?'保存资料':'提交报备'}</button></footer>
 </form>;
}
