import React,{useEffect,useRef,useState} from 'react';
import {FiChevronRight} from 'react-icons/fi';
import {crm,CRMError,dateTime,type Dashboard,type Employee,type SyncState,type Account} from '../api/wecomCustomers';
import WecomIdentityBinding from '../component/WecomIdentityBinding';
import {Feedback,errText,useResource} from './customerResource';

export function SyncPanel(){
 const {data,error,loading,reload}=useResource<SyncState>('/sync',undefined,true);const [busy,setBusy]=useState(false),[message,setMessage]=useState('');
 const operation=useRef({busy:false,alive:true,accepted:new Set<string>()}),currentData=useRef(data);currentData.current=data;
 useEffect(()=>{if(data)operation.current.accepted.clear()},[data]);useEffect(()=>{operation.current.alive=true;return()=>{operation.current.alive=false}},[]);
 const act=async(path:string)=>{const state=currentData.current;const eventId=path.startsWith('/sync/events/')?decodeURIComponent(path.slice('/sync/events/'.length,-'/retry'.length)):null;
  if(operation.current.busy||!operation.current.alive||operation.current.accepted.has(path)||!state)return;
  if(path==='/sync'&&(!state.configured||state.runs.some(r=>r.status==='pending'||r.status==='running')))return;
  if(eventId&&!state.events.some(e=>e.id===eventId&&e.status==='failed'))return;operation.current.busy=true;setBusy(true);setMessage('');try{await crm(path,{method:'POST',body:{}});if(operation.current.alive){operation.current.accepted.add(path);setMessage('任务已提交；若接入状态读取失败，请重试读取，无需重复提交。');reload()}}catch(e){if(operation.current.alive)setMessage(errText(e))}finally{operation.current.busy=false;if(operation.current.alive)setBusy(false)}};
 const statuses:Record<string,string>={pending:'等待处理',running:'处理中',done:'已完成',failed:'失败',success:'已完成'};
 return <><Feedback error={error} retry={reload}/>{message&&<div className="crm-note" role="status">{message}</div>}{loading?<div className="crm-loading">读取接入状态…</div>:data&&<>
 <section className="crm-panel"><div className="crm-section-heading"><h2>企业微信接入</h2><button className="crm-primary" disabled={busy||!data.configured||data.runs.some(r=>r.status==='pending'||r.status==='running')} onClick={()=>act('/sync')}>发起同步</button></div><div className="crm-status-grid"><div>客户读取<strong>{data.configured?'已配置':'未配置'}</strong></div><div>事件回调<strong>{data.callbackConfigured?'已配置':'未配置'}</strong></div><div>待处理事件<strong>{data.pending}</strong></div><div>失败事件<strong className="crm-red">{data.failed}</strong></div></div></section>
 <section className="crm-panel"><h2>同步任务</h2><div className="crm-table-wrap"><table><thead><tr><th>开始时间</th><th>状态</th><th>已处理</th><th>结束时间</th><th>结果说明</th></tr></thead><tbody>{data.runs.map(r=><tr key={r.id}><td>{dateTime(r.startedAt)}</td><td>{statuses[r.status]||r.status}</td><td>{r.processed}</td><td>{r.finishedAt?dateTime(r.finishedAt):'—'}</td><td>{r.error||'—'}</td></tr>)}</tbody></table></div>{!data.runs.length&&<p className="crm-caption">尚未发起同步，连接待验证。</p>}</section>
 <section className="crm-panel"><h2>未完成事件</h2><div className="crm-table-wrap"><table><thead><tr><th>发生时间</th><th>事件类型</th><th>状态</th><th>失败原因</th><th>操作</th></tr></thead><tbody>{data.events.map(e=><tr key={e.id}><td>{dateTime(e.createdAt)}</td><td>{e.changeType}</td><td>{statuses[e.status]||e.status}</td><td>{e.error||'—'}</td><td><button disabled={busy||e.status!=='failed'} onClick={()=>act('/sync/events/'+encodeURIComponent(e.id)+'/retry')}>重试</button></td></tr>)}</tbody></table></div>{!data.events.length&&<p className="crm-caption">没有待处理或失败事件。</p>}</section>
 </>}</>;
}

interface AccessUser {userId:number;username:string;scope:string;enabled:boolean;version:number;crmUsername:string;state:string;crmEnabled:boolean}
interface AccessData {users:AccessUser[];accounts:Account[];serviceAvailable:boolean}
const accessForm=(u:AccessUser,accounts:Account[])=>{const a=accounts.find(a=>a.username===u.crmUsername);return {scope:u.scope,enabled:u.enabled,crmEnabled:u.crmEnabled,displayName:a?.displayName||u.username,employeeId:a?.employeeId||'',role:a?.role||'staff'}};
type AccessSnapshot={user:AccessUser;form:ReturnType<typeof accessForm>};
export function AccessPanel(){
 const {data,error,loading,reload}=useResource<AccessData>('/access');const employees=useResource<Employee[]>('/access/employees');
 const [selection,setSelection]=useState<AccessUser|null>(null),[form,setForm]=useState({scope:'crm_only',enabled:true,crmEnabled:true,displayName:'',employeeId:'',role:'staff'});const [busy,setBusy]=useState(false),[message,setMessage]=useState(''),[newUser,setNewUser]=useState(false),[username,setUsername]=useState(''),[password,setPassword]=useState('');
 // The editor owns its version. List refreshes must not rebase an old permission draft.
 const [conflict,setConflict]=useState(false),[latest,setLatest]=useState<AccessSnapshot|null>(null);
 const operation=useRef({id:0,busy:false});
 const editor=useRef({epoch:0,conflict:false,open:false});const editorEpoch=editor.current.epoch;
 editor.current.conflict=conflict;editor.current.open=!!selection;
 useEffect(()=>()=>{operation.current={id:operation.current.id+1,busy:false};editor.current.epoch++;editor.current.open=false},[]);
 const current=data?.users.find(u=>u.userId===selection?.userId);
 const resetOperation=()=>{editor.current.epoch++;operation.current={id:operation.current.id+1,busy:false};setBusy(false)};
 const beginOperation=()=>{if(editor.current.epoch!==editorEpoch||operation.current.busy)return null;const id=operation.current.id+1;operation.current={id,busy:true};setBusy(true);setMessage('');return id};
 const isCurrent=(id:number)=>operation.current.id===id;
 const finishOperation=(id:number)=>{if(isCurrent(id)){operation.current.busy=false;setBusy(false)}};
 const choose=(u:AccessUser)=>{resetOperation();editor.current.open=true;editor.current.conflict=false;setSelection(u);setMessage('');setConflict(false);setLatest(null);setForm(accessForm(u,data?.accounts||[]))};
 const submit=async(e:React.FormEvent)=>{
  e.preventDefault();if(!selection||!editor.current.open||editor.current.epoch!==editorEpoch||operation.current.busy||editor.current.conflict||!data?.serviceAvailable)return;
  if(selection.scope==='platform_admin'&&selection.enabled&&(!form.enabled||form.scope!=='platform_admin')&&!window.confirm('该账号当前为平台管理员，保存后将失去平台管理权限。确认继续？'))return;
  const id=beginOperation();if(id===null)return;
  try{await crm('/access',{method:'POST',body:{...form,userId:selection.userId,version:selection.version}});if(!isCurrent(id))return;setMessage('授权已保存，后续请求按新权限执行。');editor.current.open=false;setSelection(null);setLatest(null);reload()}
  catch(e){if(!isCurrent(id))return;const changed=e instanceof CRMError&&e.status===409;editor.current.conflict=changed;setConflict(changed);setLatest(null);setMessage(errText(e)+(changed?'。输入已保留，请读取最新权限并核对版本与员工绑定后再决定。':'。配置未完成，输入与编辑版本已保留。'));reload()}
  finally{finishOperation(id)}
 };
 const compare=async()=>{
  if(!selection||!conflict)return;const id=beginOperation();if(id===null)return;
  try{const updated=await crm<AccessData>('/access');if(!isCurrent(id))return;if(!updated.serviceAvailable)throw new Error('客户账号信息读取不完整，请恢复客户服务后重试');const user=updated.users.find(u=>u.userId===selection.userId);if(!user)throw new Error('该账号已不在最新列表中，请刷新账号列表后重新选择');setLatest({user,form:accessForm(user,updated.accounts)})}
  catch(e){if(isCurrent(id)){setLatest(null);setMessage('读取最新权限失败：'+errText(e))}}
  finally{finishOperation(id)}
 };
 const adoptLatest=(keepInput:boolean)=>{if(!latest||editor.current.epoch!==editorEpoch||operation.current.busy)return;setSelection(latest.user);if(!keepInput)setForm(latest.form);setConflict(false);setLatest(null);setMessage(keepInput?'已采用核对后的版本，请再次确认当前输入后保存授权。':'已重新载入最新权限，原输入已替换。')};
 const retry=async()=>{if(!selection||editor.current.conflict)return;const id=beginOperation();if(id===null)return;try{await crm('/access/retry',{method:'POST',body:{userId:selection.userId}});if(!isCurrent(id))return;setMessage('配置已完成。');reload()}catch(e){if(isCurrent(id))setMessage(errText(e))}finally{finishOperation(id)}};
 const create=async(e:React.FormEvent)=>{e.preventDefault();const id=beginOperation();if(id===null)return;try{await crm('/access/users',{method:'POST',body:{username,password}});if(!isCurrent(id))return;setPassword('');setUsername('');setNewUser(false);reload();setMessage('后台账号已创建且默认禁用，请选中账号完成关联与授权。')}catch(e){if(isCurrent(id))setMessage(errText(e))}finally{finishOperation(id)}};
 return <><Feedback error={error||employees.error} retry={()=>{reload();employees.reload()}}/>{message&&<div className="crm-note" role="status">{message}</div>}
 <div className="crm-access-grid"><section className="crm-panel"><div className="crm-section-heading"><h2>后台账号</h2><button onClick={()=>{resetOperation();setMessage('');setNewUser(v=>!v)}}>新增账号</button></div>{newUser&&<form className="crm-form" onSubmit={create}><label>账号<input required minLength={3} maxLength={64} value={username} onChange={e=>setUsername(e.target.value)} autoComplete="off"/></label><label>初始密码<input required type="password" minLength={12} maxLength={72} value={password} onChange={e=>setPassword(e.target.value)} autoComplete="new-password"/></label><button className="crm-primary" disabled={busy}>创建后台账号</button></form>}{loading?<p>读取账号中…</p>:data?.users.map(u=><button className={'crm-account-row '+(selection?.userId===u.userId?'active':'')} onClick={()=>choose(u)} key={u.userId}><strong>{u.username}</strong><span>{u.enabled?'已启用':'已停用'} · {u.state==='active'?'已关联':u.state==='pending'?'待处理（配置未完成）':u.state==='failed'?'关联失败':'未关联'}</span><FiChevronRight/></button>)}</section>
 <section className="crm-panel"><h2>账号关联与授权</h2>{data&&!data.serviceAvailable&&<Feedback error="客户服务不可用，暂不能保存关联。请检查内部接入配置。"/>}{!selection?<p className="crm-caption">选择左侧账号进行配置。</p>:<form className="crm-form" onSubmit={submit}>
 {conflict&&<div className="crm-note" role="alert"><p>授权存在版本或员工绑定冲突。当前输入仍绑定原版本，核对前不能再次保存。</p><button type="button" disabled={busy} onClick={compare}>{busy?'正在读取…':'读取最新权限'}</button>{latest&&<><p>最新权限（版本 {latest.user.version}）：{latest.form.scope==='platform_admin'?'平台管理员':'仅客户管理'}；后台访问{latest.form.enabled?'启用':'停用'}；客户访问{latest.form.crmEnabled?'启用':'停用'}；客户角色{latest.form.role==='admin'?'客户主管':'顾问'}；显示名称：{latest.form.displayName}；企微员工：{employees.data?.find(e=>e.id===latest.form.employeeId)?.name||latest.form.employeeId||'不绑定'}。</p><p>保留输入继续编辑会以以上版本提交当前表单，请逐项核对权限差异。</p><button type="button" disabled={busy} onClick={()=>adoptLatest(true)}>已核对，保留我的输入继续编辑</button><button type="button" disabled={busy} onClick={()=>adoptLatest(false)}>重新载入最新权限，替换我的输入</button></>}</div>}
 <label>后台账号<input disabled={busy} readOnly value={selection.username}/></label><label>显示名称<input disabled={busy} required maxLength={100} value={form.displayName} onChange={e=>setForm(f=>({...f,displayName:e.target.value}))}/></label><label>平台访问范围<select disabled={busy} value={form.scope} onChange={e=>setForm(f=>({...f,scope:e.target.value}))}><option value="crm_only">仅客户管理</option><option value="platform_admin">平台管理员（包含其他后台模块）</option></select></label><label>客户数据角色<select disabled={busy} value={form.role} onChange={e=>setForm(f=>({...f,role:e.target.value}))}><option value="staff">顾问 · 仅本人负责的客户</option><option value="admin">客户主管 · 全部客户</option></select></label><label>企业微信员工<select disabled={busy} value={form.employeeId} onChange={e=>setForm(f=>({...f,employeeId:e.target.value}))}><option value="">不绑定（渠道账号）</option>{employees.data?.map(e=><option key={e.id} value={e.id} disabled={!e.active}>{e.name}{!e.active?'（已停用）':''}</option>)}</select></label><label className="crm-check"><input disabled={busy} type="checkbox" checked={form.enabled} onChange={e=>setForm(f=>({...f,enabled:e.target.checked}))}/>启用后台访问</label><label className="crm-check"><input disabled={busy} type="checkbox" checked={form.crmEnabled} onChange={e=>setForm(f=>({...f,crmEnabled:e.target.checked}))}/>启用客户访问</label><button className="crm-primary" disabled={busy||conflict||!data?.serviceAvailable}>{busy?'正在关联…':'保存授权'}</button>{current?.state==='pending'&&<button type="button" disabled={busy||conflict} onClick={retry}>重试未完成的配置</button>}</form>}</section></div>{selection&&<WecomIdentityBinding key={selection.userId} userId={selection.userId} username={selection.username} employees={employees.data||[]}/>}
 </>;
}

// 归属绑定检查：用客户主管视角的按归属聚合（dashboard.team）对照已绑定账号，
// 找出"名下有客户但没有后台账号绑定该员工"的归属——顾问看不到这些客户。
export function BindingCheck({goAccess}:{goAccess:()=>void}){
 const access=useResource<{accounts:Account[]}>('/access');const coverage=useResource<Dashboard>('/dashboard');
 const accounts=access.data?.accounts||[];
 const boundUsernames=new Set(accounts.map(a=>a.username));
 const boundEmployeeIds=new Set(accounts.map(a=>a.employeeId||''));
 const team=coverage.data?.team||[];
 const unboundOwners=team.filter(t=>t.ownerId&&!boundUsernames.has(t.ownerId)&&!boundEmployeeIds.has(t.ownerId));
 const retry=()=>{access.reload();coverage.reload()};
 return <><Feedback error={access.error||coverage.error} retry={retry}/>{(access.loading||coverage.loading)?<div className="crm-loading">正在检查…</div>:coverage.data&&<section className="crm-panel"><h2>归属绑定检查</h2>{unboundOwners.length?<><p className="crm-caption">以下归属名下有客户，但没有后台账号绑定该员工，对应人员登录后看不到这些客户。</p>{unboundOwners.map(t=><div className="crm-action-row" key={t.ownerId}><div><strong>{t.name}</strong><p>归属标识 {t.ownerId} · 名下 {t.total} 位客户</p></div><button onClick={goAccess}>去绑定账号</button></div>)}</>:<p className="crm-caption">{team.length?'所有有客户归属的员工均已绑定后台账号。':'暂无已分配的客户归属。'}</p>}<p className="crm-caption">不含公海与已到期客户；渠道账号可以不绑定员工。</p></section>}</>;
}
