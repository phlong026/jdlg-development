import React,{useEffect,useState} from 'react';
import {crm,type Employee} from '../api/wecomCustomers';
interface Binding {userId:number;corpId:string;employeeId:string;employeeName:string;enabled:boolean;version:number}
interface Data {binding:Binding;configured:boolean;corpId:string}
export default function WecomIdentityBinding({userId,username,employees}:{userId:number;username:string;employees:Employee[]}) {
 const [data,setData]=useState<Data|null>(null),[employee,setEmployee]=useState(''),[error,setError]=useState(''),[notice,setNotice]=useState(''),[busy,setBusy]=useState(false),[revision,setRevision]=useState(0);
 useEffect(()=>{const controller=new AbortController();setData(null);setError('');
  crm<Data>('/access/wecom/'+userId,{signal:controller.signal}).then(d=>{if(!controller.signal.aborted){setData(d);setEmployee(d.binding.employeeId)}}).catch(e=>{if(!controller.signal.aborted)setError(e.message)});
  return()=>controller.abort();
 },[userId,revision]);
 const save=async(enabled:boolean)=>{if(!data||busy)return;setBusy(true);setError('');setNotice('');try{
  const binding=await crm<Binding>('/access/wecom',{method:'POST',body:{userId,version:data.binding.version,employeeId:employee,enabled}});
  setData({...data,binding});setEmployee(binding.employeeId);setNotice(enabled?'身份已核验并绑定。企微登录将进入此后台账号，沿用其已有角色。':'企微登录已停用，已签发的企微会话将在下一次请求失效。账号密码登录由后台访问开关控制。');
 }catch(e){setError(e instanceof Error?e.message:'绑定失败')}finally{setBusy(false)}};
 return <section className="crm-panel" aria-label="企微登录身份绑定"><h2>企微登录身份</h2><p>当前后台账号：<strong>{username}</strong>。登录身份与客户业务负责人分别管理，角色沿用上方授权。</p>
 {error&&<div className="crm-feedback" role="alert">{error} <button disabled={busy} onClick={()=>setRevision(v=>v+1)}>重新读取</button></div>}{notice&&<p className="crm-note" role="status">{notice}</p>}
 {data?<form className="crm-form" onSubmit={e=>{e.preventDefault();save(true)}}>
 <p>本企业：{data.corpId||'未配置'}；状态：{data.binding.version?(data.binding.enabled?'已启用':'已停用'):'未绑定'}</p>
 {data.binding.employeeName&&<p>已核验成员：{data.binding.employeeName}（{data.binding.employeeId}）</p>}
 {!data.configured&&<p className="crm-note">服务器尚未配置企微员工登录。可停用已有绑定；启用前需配置自建应用。</p>}
 <label>企微员工 UserID<input required maxLength={128} list={'wecom-members-'+userId} value={employee} onChange={e=>setEmployee(e.target.value)} placeholder="按企微通讯录填写员工UserID" autoComplete="off"/><datalist id={'wecom-members-'+userId}>{employees.map(e=><option key={e.id} value={e.id}>{e.name} · {e.id}</option>)}</datalist><small>同一成员只能绑定一个账号；老板也需填写其本人UserID。保存时向企微核验成员状态，并检查客户库企业及顾问归属。</small></label>
 <div className="crm-form-footer"><button type="button" disabled={busy||!data.binding.enabled} onClick={()=>save(false)}>停用企微登录</button><button className="crm-primary" disabled={busy||!data.configured}>{busy?'正在核验…':'核验并保存绑定'}</button></div>
 </form>:!error&&<p>读取身份绑定中…</p>}</section>;
}
