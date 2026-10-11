import React,{useEffect,useRef,useState} from 'react';
import {crm,CRMError,type Customer} from '../api/wecomCustomers';

export default function CustomerReportForm({customer,cancel,saved}:{customer?:Customer;cancel:()=>void;saved:(c:Customer)=>void}) {
 const [form,setForm]=useState({name:customer?.name||'',wechat:customer?.wechat||'',phone:customer?.phone||'',address:customer?.address||'',buildingArea:customer?.buildingArea?.toString()||'',buildingType:customer?.buildingType||'新建',expectedBuildTime:customer?.expectedBuildTime||'',remark:customer?.remark||''});
 const [duplicate,setDuplicate]=useState(false),[reviewReason,setReviewReason]=useState('');
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const formRef=useRef<HTMLFormElement>(null);
 useEffect(()=>{const previous=document.activeElement as HTMLElement;const key=(e:KeyboardEvent)=>{
  if(e.key==='Tab'){const items=Array.from(formRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])')||[]).filter(el=>el.getClientRects().length);const first=items[0],last=items[items.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}
 };window.addEventListener('keydown',key);return()=>{window.removeEventListener('keydown',key);previous?.focus()}},[]);
 const field=(key:keyof typeof form,value:string)=>setForm(f=>({...f,[key]:value}));
 const submit=async(e:React.FormEvent)=>{e.preventDefault();if(busy)return;setBusy(true);setError('');
  try{const report={...form,buildingArea:Number(form.buildingArea)};
   const result=await crm<Customer>(customer?'/customers/'+encodeURIComponent(customer.id)+'/report':'/customers',{method:customer?'PATCH':'POST',body:customer?{version:customer.version,report}:report});saved(result);
  }catch(e){setDuplicate(e instanceof CRMError&&e.status===409);setError(e instanceof Error?e.message:'报备失败，请重试')}finally{setBusy(false)}
 };
 return <form ref={formRef} className="crm-form crm-report-form" onSubmit={submit}>
  <p className="crm-report-tip">{customer?'修改资料不延长保护期':'快速录入，报备后享 90 天专属保护'}</p>
  {error&&<div className="crm-feedback" role="alert">{error}</div>}
  <div className="crm-report-fields">
   <div className="crm-report-row"><label className="crm-report-label" htmlFor="report-name">客户姓名 / 微信</label><div className="crm-report-pair"><input id="report-name" aria-label="客户姓名" autoFocus disabled={busy} required maxLength={200} value={form.name} onChange={e=>field('name',e.target.value)} placeholder="客户姓名"/><input aria-label="微信号（选填）" disabled={busy} maxLength={80} value={form.wechat} onChange={e=>field('wechat',e.target.value)} placeholder="微信号（选填）"/></div></div>
   <label className="crm-report-row"><span className="crm-report-label">联系电话</span><input disabled={busy} required type="tel" maxLength={80} value={form.phone} onChange={e=>field('phone',e.target.value)} placeholder="手机号或带区号的固定电话"/></label>
   <label className="crm-report-row"><span className="crm-report-label">镇村地址</span><input disabled={busy} required maxLength={500} value={form.address} onChange={e=>field('address',e.target.value)} placeholder="省市县、乡镇、村组及门牌"/></label>
   <div className="crm-report-row"><label className="crm-report-label" htmlFor="report-area">面积 / 建房需求</label><div className="crm-report-pair"><div className="crm-report-area"><input id="report-area" aria-label="建房面积（平方米）" disabled={busy} required type="number" min="0.01" max="1000000" step="0.01" value={form.buildingArea} onChange={e=>field('buildingArea',e.target.value)} placeholder="建房面积"/><span>㎡</span></div><select aria-label="建房需求" disabled={busy} value={form.buildingType} onChange={e=>field('buildingType',e.target.value)}>{['新建','翻建','改造','装修'].map(t=><option key={t}>{t}</option>)}</select></div></div>
   <label className="crm-report-row"><span className="crm-report-label">预计建房时间</span><input disabled={busy} required maxLength={100} value={form.expectedBuildTime} onChange={e=>field('expectedBuildTime',e.target.value)} placeholder="例如：明年春天、2027 年 3 月"/></label>
  </div>
  <details className="crm-report-extra" open={form.remark?true:undefined}><summary>补充备注（选填）</summary><textarea aria-label="补充备注" disabled={busy} rows={3} maxLength={2000} value={form.remark} onChange={e=>field('remark',e.target.value)} placeholder="预算、宅基地情况等"/></details>
  {duplicate&&!customer&&<details><summary>申请主管核实共用电话 / 不同客户</summary><label>核实说明<textarea maxLength={1000} value={reviewReason} onChange={e=>setReviewReason(e.target.value)} placeholder="请说明不同姓名、地址及共用电话原因"/></label><button type="button" disabled={busy||!reviewReason.trim()} onClick={async()=>{setBusy(true);try{await crm('/report-review',{method:'POST',body:{report:{...form,buildingArea:Number(form.buildingArea)},reason:reviewReason}});setError('已提交主管核实，结果会通过我的提醒通知。');setDuplicate(false)}catch(e){setError(e instanceof Error?e.message:'提交失败')}finally{setBusy(false)}}}>提交核实申请</button></details>}
  <footer className="crm-form-footer"><button type="button" disabled={busy} onClick={cancel}>取消</button><button className="crm-primary" disabled={busy}>{busy?'正在保存…':customer?'保存资料':'提交报备'}</button></footer>
 </form>;
}
