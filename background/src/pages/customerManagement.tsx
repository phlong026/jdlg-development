import React,{useEffect,useRef,useState} from 'react';
import {useSearchParams} from 'react-router-dom';
import {FiUsers,FiClock,FiRefreshCw,FiSearch,FiX,FiChevronRight,FiAlertCircle} from 'react-icons/fi';
import Layout from '../component/layout';
import {useManagementSession} from '../component/ManagementSession';
import {crm,CRMError,inPool,activityNames,downloadCustomers,stages,categories,intents,dateTime,type Customer,type Dashboard,type Detail,type Employee,type Filter,type CustomerPage,type Stage,type SyncState,type Account,type Attachment,lifecycleNames} from '../api/wecomCustomers';
import CustomerLeads from './customerLeads';
import WecomIdentityBinding from '../component/WecomIdentityBinding';
import CustomerReportForm from './customerReportForm';
import {WorkflowDetails,WorkflowWorkbench,NoticePanel,SourceSyncStatus,Evidence,PushStatus} from './customerWorkflow';
import './customerManagement.scss';
import './customerResponsive.scss';

const titleOf:Record<string,string>={workbench:'主管待处理',notices:'我的提醒',home:'客户驾驶舱',customers:'客户档案',pool:'客户公海',tasks:'跟进待办',sync:'企微同步',access:'账号与权限',leads:'小程序咨询线索'};
const queueNames:Record<string,string>={pending:'待完善报备',pool:'客户公海',all:'全部客户',added:'今日新增',new:'新客户',today:'今日待办',overdue:'逾期',unplanned:'未安排',unassigned:'未分配'};
const errText=(e:unknown)=>e instanceof Error?e.message:'操作失败，请重试';
function useResource<T>(path:string,query?:Filter,poll=false){
 const [data,setData]=useState<T|null>(null),[error,setError]=useState(''),[loading,setLoading]=useState(true),[version,reload]=useState(0);
 const key=JSON.stringify(query||{});
 useEffect(()=>{const controller=new AbortController();setData(null);setLoading(true);setError('');
  crm<T>(path,{query,signal:controller.signal}).then(v=>{if(!controller.signal.aborted)setData(v)}).catch(e=>{if(!controller.signal.aborted)setError(errText(e))}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return()=>controller.abort();
 },[path,key,version]);
 useEffect(()=>{if(!poll)return;const id=setInterval(()=>{if(document.visibilityState==='visible')reload(v=>v+1)},30000);return()=>clearInterval(id)},[poll]);
 return {data,error,loading,reload:()=>reload(v=>v+1)};
}
function Feedback({error,retry}:{error:string;retry?:()=>void}){return error?<div className="crm-feedback" role="alert"><FiAlertCircle/><span>{error}</span>{retry&&<button onClick={retry}>重试</button>}</div>:null}
function Empty({text='暂无符合条件的客户',report}:{text?:string;report?:()=>void}){return <div className="crm-empty"><FiUsers/><strong>{text}</strong><span>可以调整筛选条件，或录入新的客户线索。</span>{report&&<button className="crm-primary" onClick={report}>创建新客户</button>}</div>}
function Badge({stage}:{stage:Stage}){return <span className={'crm-badge '+stage}>{stages[stage]||stage}</span>}
function Avatar({customer}:{customer:Customer}){return <span className="crm-avatar">{customer.name?.slice(0,1)||'客'}</span>}
function customerName(c:Customer){return c.name||'待同步客户'}
function customerOwner(c:Customer,employees:Employee[],account?:Account){return inPool(c)?'客户公海':employees.find(e=>e.id===c.ownerId)?.name||(c.ownerUsername===account?.username?account?.displayName:c.ownerUsername)||c.ownerId||'未分配'}

export default function CustomerManagement(){
 const session=useManagementSession();const [params,setParams]=useSearchParams();
 const supervisor=session.crmContext?.account.role==='admin';const platform=session.user?.scope==='platform_admin';
 const allowed=session.crmContext?['home','customers','pool','tasks','notices',...(supervisor?['workbench','sync']:[])]:[];
 if(platform)allowed.push('leads','access');
 const requested=params.get('tab')||'home';const tab=allowed.includes(requested)?requested:(allowed[0]||'home');
 // 无权限的 tab 直达时把地址栏校正为实际栏目，避免刷新/分享后状态不一致
 useEffect(()=>{if(session.crmContext&&requested!==tab){const next:Record<string,string>={};params.forEach((v,k)=>{next[k]=v});next.tab=tab;setParams(next,{replace:true})}},[requested,tab,session.crmContext,params,setParams]);
 const go=(next:string,queue?:string)=>setParams({tab:next,...(queue?{queue}:{})});
 const [reporting,setReporting]=useState(false);
 const [selected,setSelected]=useState<string|null>(null),[refresh,setRefresh]=useState(0);
 useEffect(()=>{setSelected(null)},[tab,session.user?.id]);
 const changed=()=>setRefresh(n=>n+1);
 if(session.crmLoading)return <Layout title="客户管理"><div className="crm-loading">正在读取客户权限…</div></Layout>;
 return <Layout title="客户管理"><div className="crm-page">
  <header className="crm-heading"><div><span className="crm-eyebrow">CUSTOMER MANAGEMENT</span><h1>{titleOf[tab]}</h1><p>{session.crmContext?.account.displayName||session.user?.username}，{supervisor?'关注团队客户与跟进进度':'查看客户需求，安排下一次联系'}</p></div><div className="crm-actions">{session.crmContext&&<button className="crm-primary" onClick={()=>setReporting(true)}>＋ 新增客户报备</button>}<button className="crm-secondary" onClick={changed}><FiRefreshCw/>刷新</button></div></header>
  <nav className="crm-tabs" aria-label="客户管理栏目">{allowed.map(t=><button key={t} className={tab===t?'active':''} onClick={()=>go(t)}>{titleOf[t]}</button>)}</nav>
  {session.crmContext?.demo&&<div className="crm-note">演示环境 · 虚构数据 · 真实企微自动接入尚未启用</div>}
  {!session.crmContext&&tab!=='access'&&tab!=='leads'?<Feedback error={session.crmError||'客户管理接入尚未配置，请联系系统管理员'} retry={session.refresh}/>:<React.Fragment key={session.user?.id}>
   {tab==='home'&&<Home key={'h'+refresh} supervisor={supervisor} open={setSelected} go={go}/>}
   {(tab==='customers'||tab==='tasks'||tab==='pool')&&<Customers key={tab+refresh} supervisor={supervisor} tasks={tab==='tasks'} initialQueue={tab==='pool'?'pool':params.get('queue')||undefined} open={setSelected} report={()=>setReporting(true)}/>}
   {tab==='workbench'&&supervisor&&<WorkflowWorkbench key={'w'+refresh} open={setSelected}/>}
   {tab==='notices'&&<NoticePanel key={'n'+refresh} open={setSelected}/>}
   {tab==='sync'&&supervisor&&<><SyncPanel key={'s'+refresh}/><PushStatus key={'p'+refresh}/></>}
   {tab==='access'&&platform&&<AccessPanel key={'a'+refresh}/>}
   {tab==='leads'&&platform&&<><SourceSyncStatus key={'ss'+refresh}/><CustomerLeads embedded readOnly={!!session.crmContext}/></>}
  </React.Fragment>}
  {reporting&&<div className="crm-overlay"><section className="crm-drawer" role="dialog" aria-modal="true" aria-label="新增客户报备"><header><h2>新增客户报备</h2></header><div className="crm-drawer-body"><CustomerReportForm cancel={()=>setReporting(false)} saved={c=>{setReporting(false);changed();setSelected(c.id)}}/></div></section></div>}
  {selected&&!reporting&&<CustomerDrawer id={selected} supervisor={supervisor} close={()=>setSelected(null)} changed={changed}/>}
 </div></Layout>;
}

function Home({supervisor,open,go}:{supervisor:boolean;open:(id:string)=>void;go:(tab:string,queue?:string)=>void}){
 const {data,error,loading,reload}=useResource<Dashboard>('/dashboard',undefined,true);
 return <><Feedback error={error} retry={reload}/>{loading?<div className="crm-loading">正在汇总客户数据…</div>:data&&<>
  <div className="crm-metrics">{[{label:supervisor?'客户总数':'我的客户',value:data.overview.all,queue:'all'},{label:'今日新增',value:data.todayAdded,queue:'added'},{label:'今日待办',value:data.overview.today,queue:'today'},{label:'逾期客户',value:data.overview.overdue,queue:'overdue'}].map((m,i)=><button key={m.label} onClick={()=>go(i>1?'tasks':'customers',m.queue)} className={i===3?'danger':''}><span>{m.label}</span><strong>{m.value??0}</strong><small>{i===1?'按已知添加时间统计':'查看客户'}<FiChevronRight/></small></button>)}</div>
  <p className="crm-caption">{supervisor?'全部客户':'仅本人负责'} · 今日待办与逾期可能重叠 · 更新于 {dateTime(data.asOf)}{data.unknownAddedTime>0&&` · ${data.unknownAddedTime}位客户添加时间待核实，不计入今日新增`}</p>
  <div className="crm-home-grid"><section className="crm-panel"><h2><FiClock/>待处理事项</h2>{['pending','pool','overdue',...(supervisor?['unassigned']:[]),'unplanned'].map(q=><div className="crm-action-row" key={q}><div><strong>{queueNames[q]}</strong><p>{q==='pending'?'补充资料并确认归属后，开始 90 天保护':q==='pool'?'90 天无有效跟进，可重新报备认领':q==='overdue'?'已超过计划联系时间':q==='unassigned'?'等待主管明确业务负责人':'还没有安排下次联系时间'}</p></div><b className={q==='overdue'?'crm-red':''}>{data.overview[q]||0}</b><button onClick={()=>go(q==='pool'?'pool':'customers',q)}>去处理<FiChevronRight/></button></div>)}</section>
  <section className="crm-panel"><div className="crm-section-heading"><h2>最近新增</h2><button onClick={()=>go('customers')}>全部客户<FiChevronRight/></button></div>{data.recent.length?data.recent.map(c=><button className="crm-recent" key={c.id} onClick={()=>open(c.id)}><Avatar customer={c}/><div><strong>{customerName(c)}</strong><p>{c.category} · {dateTime(c.createdAt)}</p></div><Badge stage={c.stage}/></button>):<Empty text="暂无已知添加时间的客户"/>}</section></div>
  {supervisor&&<section className="crm-panel"><h2>团队跟进概况</h2><div className="crm-table-wrap"><table><thead><tr><th>顾问</th><th>负责客户</th><th>今日待办</th><th>逾期</th><th>未安排</th></tr></thead><tbody>{data.team.map(t=><tr key={t.ownerId}><td>{t.name}</td><td>{t.total}</td><td>{t.today}</td><td className="crm-red">{t.overdue}</td><td>{t.unplanned}</td></tr>)}</tbody></table></div>{!data.team.length&&<p className="crm-caption">暂无已分配客户</p>}</section>}
  <div className="crm-note">支持手动报备客户，或添加企微后自动建档；初次使用需完成企业配置、已有客户同步和员工账号关联。{supervisor&&<button onClick={()=>go('sync')}>查看接入状态<FiChevronRight/></button>}</div>
 </>}</>;
}

function Customers({supervisor,tasks,initialQueue,open,report}:{supervisor:boolean;tasks:boolean;initialQueue?:string;open:(id:string)=>void;report:()=>void}){
 const account=useManagementSession().crmContext?.account;
 const [filter,setFilter]=useState<Filter>({queue:initialQueue||(tasks?'today':'all'),page:1,page_size:20});const [keyword,setKeyword]=useState('');const [actionError,setActionError]=useState(''),[exporting,setExporting]=useState(false);
 const {data,error,loading,reload}=useResource<CustomerPage>('/customers',filter,true);const employees=useResource<Employee[]>('/employees');
 useEffect(()=>{setFilter(f=>({...f,queue:initialQueue||(tasks?'today':'all'),page:1}))},[initialQueue,tasks]);
 const set=(key:keyof Filter,value:string)=>setFilter(f=>({...f,[key]:value,page:1}));
 const owner=(id:string|null)=>employees.data?.find(e=>e.id===id)?.name||id||'未分配';
 const queues=initialQueue==='pool'?['pool']:tasks?['today','overdue','unplanned',...(supervisor?['unassigned']:[])]:['all','pending','added','new','unplanned',...(supervisor?['unassigned']:[])];
 const exportFile=async(kind:'customers'|'followups')=>{setExporting(true);setActionError('');try{await downloadCustomers(kind,filter)}catch(e){setActionError(errText(e))}finally{setExporting(false)}};
 return <section className="crm-panel">{!supervisor&&initialQueue!=='pool'&&account&&!account.employeeId&&<div className="crm-note" role="status">当前账号未绑定企业微信员工，企微自动接入的客户不会出现在列表中（手动报备的客户不受影响）。请联系管理员在「账号与权限」为本账号选择业务员工。</div>}<div className="crm-queue-tabs">{queues.map(q=><button key={q} className={filter.queue===q?'active':''} onClick={()=>set('queue',q)}>{queueNames[q]}</button>)}</div>
 <form className="crm-filters" onSubmit={e=>{e.preventDefault();set('keyword',keyword)}}><label className="crm-search"><FiSearch/><input aria-label="搜索客户" value={keyword} onChange={e=>setKeyword(e.target.value)} placeholder="姓名、电话、编号、微信或地址" maxLength={200}/></label><select aria-label="客户来源" value={filter.source||''} onChange={e=>set('source',e.target.value)}><option value="">全部来源</option><option value="manual">手动报备</option><option value="wecom">企业微信</option><option value="miniprogram">小程序咨询</option></select><select aria-label="咨询类型" value={filter.category||''} onChange={e=>set('category',e.target.value)}><option value="">全部分类</option>{categories.map(c=><option key={c}>{c}</option>)}</select><select aria-label="客户阶段" value={filter.stage||''} onChange={e=>set('stage',e.target.value)}><option value="">全部阶段</option>{Object.entries(stages).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select><select aria-label="意向" value={filter.intent||''} onChange={e=>set('intent',e.target.value)}><option value="">全部意向</option>{Object.entries(intents).map(([k,v])=><option key={k} value={k}>{v}意向</option>)}</select>{supervisor&&<select aria-label="业务负责人" value={filter.ownerId||''} onChange={e=>set('ownerId',e.target.value)}><option value="">全部负责人</option>{employees.data?.map(e=><option key={e.id} value={e.id}>{e.name}{!e.active?'（停用）':''}</option>)}</select>}<button className="crm-primary" type="submit">搜索</button></form>
 <div className="crm-section-heading"><p>{filter.queue==='pool'?'所有已授权渠道均可重新报备认领':supervisor?'全部客户':'仅本人负责的客户'} · {queueNames[filter.queue||'all']}</p><div className="crm-actions"><button disabled={exporting} onClick={()=>exportFile('customers')}>导出客户</button><button disabled={exporting} onClick={()=>exportFile('followups')}>导出跟进</button></div></div>
 <Feedback error={error||actionError} retry={reload}/>{loading?<div className="crm-loading">正在读取客户…</div>:data?.list.length?<><div className="crm-customer-list"><div className="crm-table-wrap crm-customer-desktop"><table className="crm-customers-table"><thead><tr><th>客户</th><th>咨询类型</th><th>意向</th><th>客户阶段</th><th>负责人</th><th>下次跟进</th><th>操作</th></tr></thead><tbody>{data.list.map(c=><tr key={c.id}><td><button className="crm-person" onClick={()=>open(c.id)}><Avatar customer={c}/><span><strong>{customerName(c)}</strong><small>{c.phone||'手机号待补充'}</small>{c.customerNumber&&<small className="crm-customer-number">{c.customerNumber}</small>}</span></button></td><td>{c.category}</td><td><span className={'crm-intent '+c.intent}>{intents[c.intent]}</span></td><td><Badge stage={c.stage}/></td><td>{customerOwner(c,employees.data||[],account)}{c.protectionExpiresAt&&!inPool(c)&&<small className="crm-protection">保护至 {dateTime(c.protectionExpiresAt)}</small>}</td><td className={c.nextFollowupAt&&new Date(c.nextFollowupAt)<new Date()?'crm-red':''}>{dateTime(c.nextFollowupAt)}</td><td><button onClick={()=>open(c.id)}>{inPool(c)?'查看 / 认领':'查看 / 跟进'}</button></td></tr>)}</tbody></table></div>
 <div className="crm-customer-cards">{data.list.map(c=><article className="crm-customer-card" key={c.id}>
  <button className="crm-person" onClick={()=>open(c.id)}><Avatar customer={c}/><span><strong>{customerName(c)}</strong><small>{c.phone||'手机号待补充'}</small></span></button>
  {c.customerNumber&&<div className="crm-card-number"><span>客户编号</span><span title={c.customerNumber}>{c.customerNumber}</span></div>}
  <dl className="crm-card-fields">
   <div><dt>咨询类型</dt><dd>{c.category}</dd></div>
   <div><dt>意向 / 阶段</dt><dd><span className={'crm-intent '+c.intent}>{intents[c.intent]}意向</span> <Badge stage={c.stage}/></dd></div>
   <div><dt>负责人</dt><dd>{customerOwner(c,employees.data||[],account)}</dd></div>
   <div><dt>下次跟进</dt><dd className={c.nextFollowupAt&&new Date(c.nextFollowupAt)<new Date()?'crm-red':''}>{dateTime(c.nextFollowupAt)}</dd></div>
  </dl>
  {c.protectionExpiresAt&&!inPool(c)&&<p className="crm-card-protection">保护至 {dateTime(c.protectionExpiresAt)}</p>}
  <button className="crm-card-open" onClick={()=>open(c.id)}>{inPool(c)?'查看 / 认领':'查看 / 跟进'}<FiChevronRight/></button>
 </article>)}</div></div><footer className="crm-pagination"><span>共 {data.total} 位客户</span><div><button disabled={(filter.page||1)<=1} onClick={()=>setFilter(f=>({...f,page:(f.page||1)-1}))}>上一页</button><span>第 {filter.page||1} 页</span><button disabled={(filter.page||1)*20>=data.total} onClick={()=>setFilter(f=>({...f,page:(f.page||1)+1}))}>下一页</button></div></footer></>:!error&&<Empty text={filter.queue==='pool'?'暂无可认领的公海客户':'暂无符合条件的客户'} report={report}/>}
 </section>;
}

function CustomerDrawer({id,supervisor,close,changed}:{id:string;supervisor:boolean;close:()=>void;changed:()=>void}){
 const account=useManagementSession().crmContext?.account;
 const resource=useResource<Detail>('/customers/'+encodeURIComponent(id));const employees=useResource<Employee[]>('/employees');const [mode,setMode]=useState<'detail'|'edit'|'follow'|'report'>('detail');const closeRef=useRef<HTMLButtonElement>(null);
 useEffect(()=>{const prev=document.activeElement as HTMLElement;closeRef.current?.focus();const esc=(e:KeyboardEvent)=>{if(e.key==='Escape'&&mode==='detail')close();if(e.key==='Tab'){const panel=closeRef.current?.closest('section');const items=Array.from(panel?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href]')||[]).filter(el=>el.getClientRects().length);const first=items[0],last=items[items.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}};window.addEventListener('keydown',esc);return()=>{window.removeEventListener('keydown',esc);prev?.focus()}},[mode]);
 const c=resource.data?.customer;const name=(owner:string|null)=>employees.data?.find(e=>e.id===owner)?.name||owner||'未分配';
 const [claiming,setClaiming]=useState(false),[claimError,setClaimError]=useState('');
 const saved=()=>{setMode('detail');resource.reload();changed()};
 const claim=async()=>{if(!c||claiming)return;setClaiming(true);setClaimError('');try{await crm('/customers/'+encodeURIComponent(c.id)+'/claim',{method:'POST',body:{version:c.version}});saved()}catch(e){setClaimError(errText(e));resource.reload()}finally{setClaiming(false)}};
 return <div className="crm-overlay"><section className="crm-drawer" role="dialog" aria-modal="true" aria-label={mode==='detail'?'客户详情':mode==='edit'?'编辑客户资料':mode==='report'?'编辑报备资料':'记录跟进'}><header><h2>{mode==='detail'?'客户详情':mode==='edit'?'编辑客户资料':mode==='report'?'编辑报备资料':'记录跟进'}</h2><button ref={closeRef} aria-label="关闭客户详情" onClick={close}><FiX/></button></header><div className="crm-drawer-body"><Feedback error={resource.error} retry={resource.reload}/>{resource.loading?<div className="crm-loading">正在读取档案…</div>:c&&<>
 <div className="crm-customer-heading"><Avatar customer={c}/><div><h2>{customerName(c)}</h2><p>{c.phone||'手机号待补充'} · {c.category}</p><Badge stage={c.stage}/> <span className={'crm-intent '+c.intent}>{intents[c.intent]}意向</span></div></div>
 {mode==='report'?<CustomerReportForm customer={c} cancel={()=>setMode('detail')} saved={saved}/>:mode!=='detail'?<CustomerForm files={resource.data?.attachments||[]} customer={c} mode={mode} cancel={()=>setMode('detail')} saved={saved}/>:<>
  <Feedback error={claimError}/>
  {c.lifecycle==='protected'&&<div className="crm-note">{inPool(c)?'保护期已结束，进入公海。认领后可继续跟进，重新获得 90 天保护。':'专属保护至 '+dateTime(c.protectionExpiresAt)+'；有效跟进、沟通、出图、报价后自动续期 90 天。'}</div>}
  <div className="crm-actions crm-detail-actions">{inPool(c)?<button className="crm-primary" disabled={claiming} onClick={claim}>{claiming?'正在认领…':'重新报备并认领'}</button>:<><button onClick={()=>setMode('report')}>{c.lifecycle==='pending'?'完善报备资料':'编辑报备资料'}</button><button onClick={()=>setMode('edit')}>编辑资料</button><button className="crm-primary" onClick={()=>setMode('follow')}>记录跟进</button></>}</div>
  {c.customerNumber&&<dl className="crm-profile"><dt>客户编号</dt><dd className="crm-customer-number">{c.customerNumber}</dd><dt>微信</dt><dd>{c.wechat||'未填写'}</dd><dt>镇村地址</dt><dd>{c.address}</dd><dt>面积 / 需求</dt><dd>{c.buildingArea} ㎡ · {c.buildingType}</dd><dt>预计建房</dt><dd>{c.expectedBuildTime}</dd><dt>其他补充</dt><dd>{c.remark||'无'}</dd></dl>}
  <dl className="crm-profile"><dt>业务负责人</dt><dd>{customerOwner(c,employees.data||[],account)}</dd><dt>添加时间</dt><dd>{c.addedTimeSource==='unknown'?'待核实':dateTime(c.createdAt)}</dd><dt>下次跟进</dt><dd>{dateTime(c.nextFollowupAt)}</dd><dt>最近跟进</dt><dd>{c.lastFollowupAt?dateTime(c.lastFollowupAt):'尚未跟进'}</dd></dl>
  <h3>需求摘要</h3><p className="crm-demand">{c.demand||'待补充客户需求'}</p>
  <h3>企微好友关系</h3>{resource.data?.relations.length?resource.data.relations.map(r=><div className="crm-relation" key={r.eId}><span>{name(r.userId)}</span><span className={'crm-badge '+(r.status===0?'won':'paused')}>{({0:'正常',1:'客户已删除员工',2:'员工已删除客户'} as Record<number,string>)[r.status]||'关系待核实'}</span></div>):<p className="crm-caption">{c.customerNumber?'手动报备客户，尚未关联企微好友':'好友关系待同步'}</p>}
  <p className="crm-caption">调整业务负责人由主管通过「分配 / 交接」处理；交接不转移企微好友关系，删除好友后仍保留历史档案。</p>
  {resource.data&&<WorkflowDetails detail={resource.data} supervisor={supervisor} saved={saved}/>}<h3>跟进与操作记录</h3><div className="crm-timeline">{resource.data?.followups.map(f=><article key={f.id}><small>{dateTime(f.createdAt)} · {f.operatorId} · {activityNames[f.kind]||f.kind}</small><p>{f.content}</p>{['followup','communication','drawing','quotation'].includes(f.kind)&&<small>{f.effective?'有效跟进':'不计入续期'}{f.reviewReason&&' · 审核理由：'+f.reviewReason}</small>}<Badge stage={f.stage}/>{f.nextFollowupAt&&<small>下次联系 {dateTime(f.nextFollowupAt)}</small>}</article>)}{!resource.data?.followups.length&&<p>暂无跟进记录</p>}</div>
 </>}
 </>}</div></section></div>;
}
function localInput(s:string|null){if(!s)return '';const d=new Date(s);const pad=(v:number)=>String(v).padStart(2,'0');return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+'T'+pad(d.getHours())+':'+pad(d.getMinutes())}
function CustomerForm({customer:c,mode,cancel,saved,files}:{files:Attachment[];customer:Customer;mode:'edit'|'follow';cancel:()=>void;saved:()=>void}){
 const [form,setForm]=useState({phone:c.phone||'',demand:c.demand||'',category:c.category,intent:c.intent,stage:c.stage,next:localInput(c.nextFollowupAt),content:'',kind:'followup',outcome:'contacted'});const [busy,setBusy]=useState(false),[error,setError]=useState(''),[conflict,setConflict]=useState(false);const [version,setVersion]=useState(c.version);const [baseline,setBaseline]=useState(c);const [latest,setLatest]=useState<Customer|null>(null);const [evidence,setEvidence]=useState<string[]>([]);
 const field=(k:string,v:string)=>setForm(f=>({...f,[k]:v}));const active=form.stage==='new'||form.stage==='following';
 const submit=async(e:React.FormEvent)=>{e.preventDefault();if(busy||conflict)return;setBusy(true);setError('');
  try {
   if(mode==='follow'){
    const next=active&&form.next?new Date(form.next).toISOString():null;
    if(active&&(!next||new Date(next)<=new Date()))throw new Error('请安排晚于现在的下次联系时间');
    await crm('/customers/'+encodeURIComponent(c.id)+'/followups',{method:'POST',body:{version,kind:form.kind,outcome:form.outcome,attachmentIds:evidence,content:form.content.trim(),stage:form.stage,nextFollowupAt:next}});
   }else await crm('/customers/'+encodeURIComponent(c.id),{method:'PATCH',body:{version,phone:form.phone,demand:form.demand,category:form.category,intent:form.intent,ownerId:baseline.ownerId,stage:baseline.stage,nextFollowupAt:baseline.nextFollowupAt}});
   saved();
  }catch(e){setError(errText(e));if(e instanceof CRMError&&e.status===409)setConflict(true)}finally{setBusy(false)}
 };
 const compare=async()=>{try{const d=await crm<Detail>('/customers/'+encodeURIComponent(c.id));setLatest(d.customer)}catch(e){setError(errText(e))}};
 const confirmLatest=()=>{if(!latest)return;setVersion(latest.version);setBaseline(latest);setConflict(false);setLatest(null);setError('已采用最新版本，请核对当前输入后再次保存。')};
 return <form className="crm-form" onSubmit={submit}>
 <Feedback error={error}/>{conflict&&<div className="crm-note"><p>输入已保留。先读取最新档案，核对后决定是否提交，系统不会自动覆盖。</p><button type="button" onClick={compare}>读取最新版本</button>{latest&&<><p>最新阶段：{stages[latest.stage]}；负责人：{latest.ownerId||'未分配'}；电话：{latest.phone||'未填写'}；需求：{latest.demand||'未填写'}；下次跟进：{dateTime(latest.nextFollowupAt)}</p><button type="button" onClick={confirmLatest}>已核对，保留我的输入继续编辑</button></>}</div>}
 {mode==='edit'?<><label>客户昵称<input value={customerName(c)} readOnly/><small>{c.customerNumber?'在“编辑报备资料”中修改姓名与联系电话':'由企业微信同步维护'}</small></label><label>联系电话<input readOnly={!!c.customerNumber} value={form.phone} maxLength={80} onChange={e=>field('phone',e.target.value)} placeholder="可稍后补充"/></label><label>咨询类型<select value={form.category} onChange={e=>field('category',e.target.value)}>{categories.map(x=><option key={x}>{x}</option>)}</select></label><label>意向程度<select value={form.intent} onChange={e=>field('intent',e.target.value)}>{Object.entries(intents).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label><label>需求摘要<textarea rows={5} value={form.demand} maxLength={2000} onChange={e=>field('demand',e.target.value)}/></label></>:<><label>跟进类型<select value={form.kind} onChange={e=>field('kind',e.target.value)}>{['followup','communication','drawing','quotation'].map(k=><option key={k} value={k}>{activityNames[k]}</option>)}</select></label><label>联系结果<select value={form.outcome} onChange={e=>field('outcome',e.target.value)}><option value="contacted">有效联系 / 已完成</option><option value="no_answer">电话未接通</option><option value="no_reply">消息未回复</option><option value="internal">内部安排</option></select><small>未接通、未回复和内部安排只记记录，不延长保护。</small></label><Evidence files={files} selected={evidence} change={setEvidence}/><label><span>跟进内容 <b className="crm-red">*</b></span><textarea autoFocus required rows={6} value={form.content} maxLength={2000} onChange={e=>field('content',e.target.value)} placeholder="例如：电话沟通，已确认两层建房需求，待补充宅基地照片。"/><small>{form.content.length}/2000</small></label><label>客户阶段<select value={form.stage} onChange={e=>field('stage',e.target.value)}>{Object.entries(stages).filter(([k])=>!['won','invalid'].includes(k)||k===c.stage).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label>{active?<label><span>下次联系时间 <b className="crm-red">*</b></span><input type="datetime-local" required value={form.next} onChange={e=>field('next',e.target.value)}/><small>按当前设备时区填写；保存后统一按北京时间展示。</small></label>:<div className="crm-note">暂缓会结束当前待办，但仍受保护到期规则约束。成交及无效归档请提交主管申请。</div>}</>}
 <footer className="crm-form-footer"><button type="button" disabled={busy} onClick={cancel}>取消</button><button type="submit" className="crm-primary" disabled={busy||conflict}>{busy?'正在保存…':mode==='edit'?'保存资料':'保存跟进'}</button></footer>
 </form>;
}

function SyncPanel(){
 const {data,error,loading,reload}=useResource<SyncState>('/sync',undefined,true);const [busy,setBusy]=useState(false),[message,setMessage]=useState('');
 const act=async(path:string)=>{setBusy(true);setMessage('');try{await crm(path,{method:'POST',body:{}});setMessage('任务已提交，处理结果会自动更新。');reload()}catch(e){setMessage(errText(e))}finally{setBusy(false)}};
 const statuses:Record<string,string>={pending:'等待处理',running:'处理中',done:'已完成',failed:'失败',success:'已完成'};
 return <><Feedback error={error} retry={reload}/>{message&&<div className="crm-note" role="status">{message}</div>}{loading?<div className="crm-loading">读取接入状态…</div>:data&&<>
 <section className="crm-panel"><div className="crm-section-heading"><div><h2>企业微信接入</h2><p>首次配置后，新增客户自动接入；手动同步用于初始化与补同步。</p></div><button className="crm-primary" disabled={busy||!data.configured||data.runs.some(r=>r.status==='pending'||r.status==='running')} onClick={()=>act('/sync')}>发起同步</button></div><div className="crm-status-grid"><div>客户读取参数<strong>{data.configured?'已配置':'未配置'}</strong></div><div>事件回调参数<strong>{data.callbackConfigured?'已配置':'未配置'}</strong></div><div>待处理事件<strong>{data.pending}</strong></div><div>失败事件<strong className="crm-red">{data.failed}</strong></div></div><p className="crm-caption">配置齐备不等于连接成功，请以实际同步结果和真实加好友验证为准。密钥由服务器环境配置。</p></section>
 <section className="crm-panel"><h2>同步任务</h2><div className="crm-table-wrap"><table><thead><tr><th>开始时间</th><th>状态</th><th>已处理</th><th>结束时间</th><th>结果说明</th></tr></thead><tbody>{data.runs.map(r=><tr key={r.id}><td>{dateTime(r.startedAt)}</td><td>{statuses[r.status]||r.status}</td><td>{r.processed}</td><td>{r.finishedAt?dateTime(r.finishedAt):'—'}</td><td>{r.error||'—'}</td></tr>)}</tbody></table></div>{!data.runs.length&&<p className="crm-caption">尚未发起同步，连接待验证。</p>}</section>
 <section className="crm-panel"><h2>未完成事件</h2><div className="crm-table-wrap"><table><thead><tr><th>发生时间</th><th>事件类型</th><th>状态</th><th>失败原因</th><th>操作</th></tr></thead><tbody>{data.events.map(e=><tr key={e.id}><td>{dateTime(e.createdAt)}</td><td>{e.changeType}</td><td>{statuses[e.status]||e.status}</td><td>{e.error||'—'}</td><td><button disabled={busy||e.status!=='failed'} onClick={()=>act('/sync/events/'+encodeURIComponent(e.id)+'/retry')}>重试</button></td></tr>)}</tbody></table></div>{!data.events.length&&<p className="crm-caption">没有待处理或失败事件。</p>}</section>
 </>}</>;
}

interface AccessUser {userId:number;username:string;scope:string;enabled:boolean;version:number;crmUsername:string;state:string;crmEnabled:boolean}
interface AccessData {users:AccessUser[];accounts:Account[];serviceAvailable:boolean}
function AccessPanel(){
 const {data,error,loading,reload}=useResource<AccessData>('/access');const employees=useResource<Employee[]>('/access/employees');
 // 归属绑定完整性：用主管视角的按归属聚合（dashboard.team）对照已绑定账号，
 // 找出"名下有客户但没有后台账号绑定该员工"的归属——顾问看不到这些客户。
 const coverage=useResource<Dashboard>('/dashboard');
 const boundUsernames=new Set((data?.accounts||[]).map(a=>a.username));
 const boundEmployeeIds=new Set((data?.accounts||[]).map(a=>a.employeeId||''));
 const unboundOwners=(coverage.data?.team||[]).filter(t=>t.ownerId&&!boundUsernames.has(t.ownerId)&&!boundEmployeeIds.has(t.ownerId));
 const [selection,setSelection]=useState<AccessUser|null>(null),[form,setForm]=useState({scope:'crm_only',enabled:true,crmEnabled:true,displayName:'',employeeId:'',role:'staff'});const [busy,setBusy]=useState(false),[message,setMessage]=useState(''),[newUser,setNewUser]=useState(false),[username,setUsername]=useState(''),[password,setPassword]=useState('');
 // 保存失败后保留选中与输入；按列表最新版本提交，避免刷新后的旧版本把重新保存卡在 409。
 const current=data?.users.find(u=>u.userId===selection?.userId);
 const choose=(u:AccessUser)=>{const a=data?.accounts.find(a=>a.username===u.crmUsername);setSelection(u);setMessage('');setForm({scope:u.scope,enabled:u.enabled,crmEnabled:u.crmEnabled,displayName:a?.displayName||u.username,employeeId:a?.employeeId||'',role:a?.role||'staff'})};
 const submit=async(e:React.FormEvent)=>{e.preventDefault();if(!selection||busy)return;if(selection.scope==='platform_admin'&&selection.enabled&&(!form.enabled||form.scope!=='platform_admin')&&!window.confirm('该账号当前为平台管理员，保存后将失去平台管理权限。确认继续？'))return;setBusy(true);setMessage('');try{await crm('/access',{method:'POST',body:{...form,userId:selection.userId,version:current?.version??selection.version}});setMessage('授权已保存，后续请求按新权限执行。');setSelection(null);reload()}catch(e){setMessage(errText(e)+'。配置未完成，可重试或修改后重新保存。');reload()}finally{setBusy(false)}};
 const retry=async()=>{if(!selection||busy)return;setBusy(true);setMessage('');try{await crm('/access/retry',{method:'POST',body:{userId:selection.userId}});setMessage('配置已完成。');reload()}catch(e){setMessage(errText(e))}finally{setBusy(false)}};
 const create=async(e:React.FormEvent)=>{e.preventDefault();setBusy(true);try{await crm('/access/users',{method:'POST',body:{username,password}});setPassword('');setUsername('');setNewUser(false);reload();setMessage('后台账号已创建且默认禁用，请选中账号完成关联与授权。')}catch(e){setMessage(errText(e))}finally{setBusy(false)}};
 return <><Feedback error={error||employees.error} retry={()=>{reload();employees.reload()}}/>{message&&<div className="crm-note" role="status">{message}</div>}<div className="crm-note">系统管理员配置平台权限；客户主管只负责客户业务。顾问默认仅访问客户模块。</div>
 <div className="crm-access-grid"><section className="crm-panel"><div className="crm-section-heading"><h2>后台账号</h2><button onClick={()=>setNewUser(v=>!v)}>新增账号</button></div>{newUser&&<form className="crm-form" onSubmit={create}><label>账号<input required minLength={3} maxLength={64} value={username} onChange={e=>setUsername(e.target.value)} autoComplete="off"/></label><label>初始密码<input required type="password" minLength={12} maxLength={72} value={password} onChange={e=>setPassword(e.target.value)} autoComplete="new-password"/></label><button className="crm-primary" disabled={busy}>创建后台账号</button></form>}{loading?<p>读取账号中…</p>:data?.users.map(u=><button className={'crm-account-row '+(selection?.userId===u.userId?'active':'')} onClick={()=>choose(u)} key={u.userId}><strong>{u.username}</strong><span>{u.enabled?'已启用':'已停用'} · {u.state==='active'?'已关联':u.state==='pending'?'待处理（配置未完成）':u.state==='failed'?'关联失败':'未关联'}</span><FiChevronRight/></button>)}</section>
 <section className="crm-panel"><h2>账号关联与授权</h2>{data&&!data.serviceAvailable&&<Feedback error="客户服务不可用，暂不能保存关联。请检查内部接入配置。"/>}{!selection?<p className="crm-caption">选择左侧账号，配置对应的企微员工和数据角色。</p>:<form className="crm-form" onSubmit={submit}><label>后台账号<input readOnly value={selection.username}/></label><label>显示名称<input required maxLength={100} value={form.displayName} onChange={e=>setForm(f=>({...f,displayName:e.target.value}))}/></label><label>平台访问范围<select value={form.scope} onChange={e=>setForm(f=>({...f,scope:e.target.value}))}><option value="crm_only">仅客户管理</option><option value="platform_admin">平台管理员（包含其他后台模块）</option></select></label><label>客户数据角色<select value={form.role} onChange={e=>setForm(f=>({...f,role:e.target.value}))}><option value="staff">顾问 · 仅本人负责的客户</option><option value="admin">客户主管 · 全部客户</option></select></label><label>企业微信员工<select value={form.employeeId} onChange={e=>setForm(f=>({...f,employeeId:e.target.value}))}><option value="">不绑定（渠道账号）</option>{employees.data?.map(e=><option key={e.id} value={e.id} disabled={!e.active}>{e.name}{!e.active?'（已停用）':''}</option>)}</select></label><label className="crm-check"><input type="checkbox" checked={form.enabled} onChange={e=>setForm(f=>({...f,enabled:e.target.checked}))}/>启用后台访问</label><label className="crm-check"><input type="checkbox" checked={form.crmEnabled} onChange={e=>setForm(f=>({...f,crmEnabled:e.target.checked}))}/>启用客户访问</label><p className="crm-caption">企微客户权限与平台管理权限分别授予。统一账号不需要第二份CRM密码。</p><button className="crm-primary" disabled={busy||!data?.serviceAvailable}>{busy?'正在关联…':'保存授权'}</button>{current?.state==='pending'&&<button type="button" disabled={busy} onClick={retry}>重试未完成的配置</button>}</form>}</section></div>{coverage.data&&coverage.data.team.length>0&&<section className="crm-panel"><h2>归属绑定完整性</h2>{unboundOwners.length?<><p className="crm-caption">以下归属名下有客户，但没有后台账号绑定该员工；对应人员登录后看不到这些客户。请为其创建并绑定账号，或由主管转交客户。</p>{unboundOwners.map(t=><div className="crm-action-row" key={t.ownerId}><div><strong>{t.name}</strong><p>归属标识 {t.ownerId} · 名下 {t.total} 位客户</p></div><button onClick={()=>{setNewUser(true);window.scrollTo({top:0,behavior:'smooth'})}}>新增账号绑定</button></div>)}</>:<p className="crm-caption">所有有客户归属的员工均已绑定后台账号。</p>}<p className="crm-caption">按客户主管视角统计，不含公海与已到期客户；渠道账号可以不绑定员工。当前账号需具有客户主管角色才能统计完整归属。</p></section>}{selection&&<WecomIdentityBinding key={selection.userId} userId={selection.userId} username={selection.username} employees={employees.data||[]}/>}
 </>;
}
