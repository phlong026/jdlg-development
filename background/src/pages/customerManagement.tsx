import React,{useEffect,useRef,useState} from 'react';
import {useSearchParams} from 'react-router-dom';
import {FiUsers,FiClock,FiRefreshCw,FiSearch,FiX,FiChevronRight,FiAlertCircle} from 'react-icons/fi';
import {useManagementSession} from '../component/ManagementSession';
import {crm,CRMError,activityNames,downloadCustomers,stages,categories,intents,dateTime,type Customer,type Dashboard,type Detail,type Employee,type Filter,type CustomerPage,type Stage,type SyncState,type Account,type Attachment,lifecycleNames} from '../api/wecomCustomers';
import CustomerLeads from './customerLeads';
import WecomIdentityBinding from '../component/WecomIdentityBinding';
import CustomerReportForm from './customerReportForm';
import {WorkflowDetails,WorkflowWorkbench,NoticePanel,SourceSyncStatus,Evidence,PushStatus,hasRecordedPool,hasPoolEvidence,type PoolEvidence,type CustomerMutationGate} from './customerWorkflow';
import './customerManagement.scss';
import './customerResponsive.scss';

const titleOf:Record<string,string>={workbench:'主管待处理',notices:'我的提醒',home:'客户驾驶舱',customers:'客户档案',pool:'客户公海',tasks:'跟进待办',sync:'企微同步',access:'账号与权限',leads:'小程序咨询线索'};
const queueNames:Record<string,string>={pending:'待完善报备',pool:'客户公海',all:'全部客户',added:'今日新增',new:'新客户',today:'今日待办',overdue:'逾期',unplanned:'未安排',unassigned:'未分配'};
const errText=(e:unknown)=>e instanceof Error?e.message:'操作失败，请重试';
function useResource<T>(path:string,query?:Filter,poll=false){
 const [version,reload]=useState(0);const key=JSON.stringify(query||{});const requestKey=path+':'+key+':'+version;
 const [state,setState]=useState<{key:string;data:T|null;error:string;loading:boolean}>({key:requestKey,data:null,error:'',loading:true});
 useEffect(()=>{const controller=new AbortController();setState({key:requestKey,data:null,error:'',loading:true});
  crm<T>(path,{query,signal:controller.signal}).then(data=>{if(!controller.signal.aborted)setState({key:requestKey,data,error:'',loading:false})}).catch(e=>{if(!controller.signal.aborted)setState({key:requestKey,data:null,error:errText(e),loading:false})});
  return()=>controller.abort();
 },[path,key,version]);
 useEffect(()=>{if(!poll)return;const id=setInterval(()=>{if(document.visibilityState==='visible')reload(v=>v+1)},30000);return()=>clearInterval(id)},[poll]);
 const current=state.key===requestKey?state:{data:null,error:'',loading:true};
 return {...current,reload:()=>reload(v=>v+1)};
}
function Feedback({error,retry}:{error:string;retry?:()=>void}){return error?<div className="crm-feedback" role="alert"><FiAlertCircle/><span>{error}</span>{retry&&<button onClick={retry}>重试</button>}</div>:null}
function Empty({text='暂无符合条件的客户',report}:{text?:string;report?:()=>void}){return <div className="crm-empty"><FiUsers/><strong>{text}</strong><span>可以调整筛选条件，或录入新的客户线索。</span>{report&&<button className="crm-primary" onClick={report}>创建新客户</button>}</div>}
function Badge({stage}:{stage:Stage}){return <span className={'crm-badge '+stage}>{stages[stage]||stage}</span>}
function Avatar({customer}:{customer:Customer}){return <span className="crm-avatar">{customer.name?.slice(0,1)||'客'}</span>}
function customerName(c:Customer){return c.name||'待同步客户'}
function customerOwner(c:Customer,employees:Employee[],account?:Account){return hasRecordedPool(c)?'客户公海':employees.find(e=>e.id===c.ownerId)?.name||(c.ownerUsername===account?.username?account?.displayName:c.ownerUsername)||c.ownerId||'未分配'}

export default function CustomerManagement(){
 const session=useManagementSession();const [params,setParams]=useSearchParams();
 const drawerGate=useRef({pending:false,alive:true});const [drawerBusy,setDrawerBusy]=useState(false);
 useEffect(()=>{drawerGate.current.alive=true;return()=>{drawerGate.current.alive=false}},[]);
 const setDrawerPending=(pending:boolean)=>{drawerGate.current.pending=pending;if(drawerGate.current.alive)setDrawerBusy(pending)};
 const supervisor=session.crmContext?.account.role==='admin';const platform=session.user?.scope==='platform_admin';
 const allowed=session.crmContext?['home','customers','pool','tasks','notices',...(supervisor?['workbench','sync']:[])]:[];
 if(platform)allowed.push('leads','access');
 const requested=params.get('tab')||'home';const tab=allowed.includes(requested)?requested:(allowed[0]||'home');
 // 无权限的 tab 直达时把地址栏校正为实际栏目，避免刷新/分享后状态不一致
 useEffect(()=>{if(session.crmContext&&requested!==tab){const next:Record<string,string>={};params.forEach((v,k)=>{next[k]=v});next.tab=tab;setParams(next,{replace:true})}},[requested,tab,session.crmContext,params,setParams]);
 const go=(next:string,queue?:string)=>{if(!drawerGate.current.pending)setParams({tab:next,...(queue?{queue}:{})})};
 const [reporting,setReporting]=useState(false);
 const [selected,setSelected]=useState<{id:string;poolEvidence?:PoolEvidence}|null>(null),[refresh,setRefresh]=useState(0);
 const [completion,setCompletion]=useState('');const openCustomer=(id:string,poolEvidence?:PoolEvidence)=>{if(!drawerGate.current.pending)setSelected({id,poolEvidence})};
 useEffect(()=>{if(!drawerGate.current.pending)setSelected(null)},[tab,session.user?.id]);
 // 窄屏下栏目条横向滚动：直达或切换后把当前栏目带入可视区（只滚动栏目条本身，不移动页面），并用 data-fade 标出两侧还有更多栏目
 const tabsRef=useRef<HTMLElement>(null);const tabKeys=allowed.join('|');
 useEffect(()=>{const track=tabsRef.current;if(!track)return;
  const fade=()=>{const left=track.scrollLeft>1,right=track.scrollWidth-track.clientWidth-track.scrollLeft>1;if(left||right)track.dataset.fade=left&&right?'both':left?'left':'right';else delete track.dataset.fade};
  const active=track.querySelector<HTMLElement>('.active');
  if(active){const viewport=track.getBoundingClientRect(),item=active.getBoundingClientRect(),edge=36;
   if(item.left<viewport.left+edge)track.scrollLeft+=item.left-viewport.left-edge;else if(item.right>viewport.right-edge)track.scrollLeft+=item.right-viewport.right+edge}
  fade();track.addEventListener('scroll',fade,{passive:true});window.addEventListener('resize',fade);
  return()=>{track.removeEventListener('scroll',fade);window.removeEventListener('resize',fade)}},[tab,tabKeys,session.crmLoading]);
 const changed=()=>{if(!drawerGate.current.pending)setRefresh(n=>n+1)};
 if(session.crmLoading)return <><div className="crm-loading">正在读取客户权限…</div></>;
 return <><div className="crm-page">
  <header className="crm-heading"><div><span className="crm-eyebrow">CUSTOMER MANAGEMENT</span><h1>{titleOf[tab]}</h1><p>{session.crmContext?.account.displayName||session.user?.username}，{supervisor?'关注团队客户与跟进进度':'查看客户需求，安排下一次联系'}</p></div><div className="crm-actions">{session.crmContext&&<button className="crm-primary" disabled={drawerBusy} onClick={()=>{if(drawerGate.current.pending)return;setCompletion('');setReporting(true)}}>＋ 新增客户报备</button>}<button className="crm-secondary" disabled={drawerBusy} onClick={changed}><FiRefreshCw/>刷新</button></div></header>
  {completion&&<div className="crm-note" role="status">{completion}</div>}
  <nav className="crm-tabs" aria-label="客户管理栏目" ref={tabsRef}>{allowed.map(t=><button disabled={drawerBusy} key={t} className={tab===t?'active':''} onClick={()=>go(t)}>{titleOf[t]}</button>)}</nav>
  {session.crmContext?.demo&&<div className="crm-note">演示环境 · 虚构数据 · 真实企微自动接入尚未启用</div>}
  {!session.crmContext&&tab!=='access'&&tab!=='leads'?<Feedback error={session.crmError||'客户管理接入尚未配置，请联系系统管理员'} retry={session.refresh}/>:<React.Fragment key={session.user?.id}>
   {tab==='home'&&<Home key={'h'+refresh} supervisor={supervisor} open={openCustomer} go={go}/>}
   {(tab==='customers'||tab==='tasks'||tab==='pool')&&<Customers key={tab+refresh} supervisor={supervisor} tasks={tab==='tasks'} initialQueue={tab==='pool'?'pool':params.get('queue')||undefined} open={openCustomer} report={()=>setReporting(true)}/>}
   {tab==='workbench'&&supervisor&&<WorkflowWorkbench key={'w'+refresh} open={openCustomer}/>}
   {tab==='notices'&&<NoticePanel key={'n'+refresh} open={openCustomer}/>}
   {tab==='sync'&&supervisor&&<><SyncPanel key={'s'+refresh}/><PushStatus key={'p'+refresh}/></>}
   {tab==='access'&&platform&&<AccessPanel key={'a'+refresh}/>}
   {tab==='leads'&&platform&&<><SourceSyncStatus key={'ss'+refresh}/><CustomerLeads embedded/></>}
  </React.Fragment>}
  {reporting&&<div className="crm-overlay"><section className="crm-drawer" role="dialog" aria-modal="true" aria-label="新增客户报备"><header><h2>新增客户报备</h2></header><div className="crm-drawer-body"><CustomerReportForm cancel={()=>setReporting(false)} saved={c=>{setReporting(false);setCompletion('客户报备已保存。若列表读取失败，请重新读取，无需再次报备。');changed();openCustomer(c.id)}}/></div></section></div>}
  {selected&&!reporting&&<CustomerDrawer key={selected.id} id={selected.id} poolEvidence={selected.poolEvidence} supervisor={supervisor} close={()=>{if(!drawerGate.current.pending)setSelected(null)}} changed={changed} pendingChanged={setDrawerPending} checkPool={()=>{if(drawerGate.current.pending)return;setSelected(null);go('pool')}}/>}
 </div></>;
}

function Home({supervisor,open,go}:{supervisor:boolean;open:(id:string)=>void;go:(tab:string,queue?:string)=>void}){
 const {data,error,loading,reload}=useResource<Dashboard>('/dashboard',undefined,true);
 return <><Feedback error={error} retry={reload}/>{loading?<div className="crm-loading">正在汇总客户数据…</div>:data&&<>
  <div className="crm-metrics">{[{label:supervisor?'客户总数':'我的客户',value:data.overview.all,queue:'all'},{label:'今日新增',value:data.todayAdded,queue:'added'},{label:'今日待办',value:data.overview.today,queue:'today'},{label:'逾期客户',value:data.overview.overdue,queue:'overdue'}].map((m,i)=><button key={m.label} onClick={()=>go(i>1?'tasks':'customers',m.queue)} className={i===3?'danger':''}><span>{m.label}</span><strong>{m.value??0}</strong><small>{i===1?'按已知添加时间统计':'查看客户'}<FiChevronRight/></small></button>)}</div>
  <p className="crm-caption">{supervisor?'团队非公海客户':'本人负责的非公海客户'}；公海统计覆盖全部已授权渠道 · 今日按北京时间，待办与逾期可能重叠 · 更新于 {dateTime(data.asOf)}{data.unknownAddedTime>0&&` · ${data.unknownAddedTime}位客户添加时间待核实，不计入今日新增`}</p>
  <div className="crm-home-grid"><section className="crm-panel"><h2><FiClock/>待处理事项</h2>{['pending','pool','overdue',...(supervisor?['unassigned']:[]),'unplanned'].map(q=><div className="crm-action-row" key={q}><div><strong>{queueNames[q]}</strong><p>{q==='pending'?'补充资料并确认归属后，开始 90 天保护':q==='pool'?'90 天无有效跟进，可重新报备认领':q==='overdue'?'已超过计划联系时间':q==='unassigned'?'等待主管明确业务负责人':'还没有安排下次联系时间'}</p></div><b className={q==='overdue'?'crm-red':''}>{data.overview[q]||0}</b><button onClick={()=>go(q==='pool'?'pool':'customers',q)}>去处理<FiChevronRight/></button></div>)}</section>
  <section className="crm-panel"><div className="crm-section-heading"><h2>最近新增</h2><button onClick={()=>go('customers')}>全部客户<FiChevronRight/></button></div>{data.recent.length?data.recent.map(c=><button className="crm-recent" key={c.id} onClick={()=>open(c.id)}><Avatar customer={c}/><div><strong>{customerName(c)}</strong><p>{c.category} · {dateTime(c.createdAt)}</p></div><Badge stage={c.stage}/></button>):<Empty text="暂无已知添加时间的客户"/>}</section></div>
  {supervisor&&<section className="crm-panel"><h2>团队跟进概况</h2><div className="crm-table-wrap"><table><thead><tr><th>顾问</th><th>负责客户</th><th>今日待办</th><th>逾期</th><th>未安排</th></tr></thead><tbody>{data.team.map(t=><tr key={t.ownerId}><td>{t.name}</td><td>{t.total}</td><td>{t.today}</td><td className="crm-red">{t.overdue}</td><td>{t.unplanned}</td></tr>)}</tbody></table></div>{!data.team.length&&<p className="crm-caption">暂无已分配客户</p>}</section>}
  <div className="crm-note">支持手动报备客户，或添加企微后自动建档；初次使用需完成企业配置、已有客户同步和员工账号关联。{supervisor&&<button onClick={()=>go('sync')}>查看接入状态<FiChevronRight/></button>}</div>
 </>}</>;
}

function Customers({supervisor,tasks,initialQueue,open,report}:{supervisor:boolean;tasks:boolean;initialQueue?:string;open:(id:string,poolEvidence?:PoolEvidence)=>void;report:()=>void}){
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
 <Feedback error={error||actionError} retry={reload}/>{loading?<div className="crm-loading">正在读取客户…</div>:data?.list.length?<><div className="crm-customer-list"><div className="crm-table-wrap crm-customer-desktop"><table className="crm-customers-table"><thead><tr><th>客户</th><th>咨询类型</th><th>意向</th><th>客户阶段</th><th>负责人</th><th>下次跟进</th><th>操作</th></tr></thead><tbody>{data.list.map(c=><tr key={c.id}><td><button className="crm-person" onClick={()=>open(c.id,filter.queue==='pool'?{customerId:c.id,version:c.version}:undefined)}><Avatar customer={c}/><span><strong>{customerName(c)}</strong><small>{c.phone||'手机号待补充'}</small>{c.customerNumber&&<small className="crm-customer-number">{c.customerNumber}</small>}</span></button></td><td>{c.category}</td><td><span className={'crm-intent '+c.intent}>{intents[c.intent]}</span></td><td><Badge stage={c.stage}/></td><td>{filter.queue==='pool'?'客户公海':customerOwner(c,employees.data||[],account)}{c.protectionExpiresAt&&!(filter.queue==='pool'||hasRecordedPool(c))&&<small className="crm-protection">保护至 {dateTime(c.protectionExpiresAt)}</small>}</td><td className={c.nextFollowupAt&&new Date(c.nextFollowupAt)<new Date()?'crm-red':''}>{dateTime(c.nextFollowupAt)}</td><td><button onClick={()=>open(c.id,filter.queue==='pool'?{customerId:c.id,version:c.version}:undefined)}>{(filter.queue==='pool'||hasRecordedPool(c))?'查看 / 认领':'查看 / 跟进'}</button></td></tr>)}</tbody></table></div>
 <div className="crm-customer-cards">{data.list.map(c=><article className="crm-customer-card" key={c.id}>
  <button className="crm-person" onClick={()=>open(c.id,filter.queue==='pool'?{customerId:c.id,version:c.version}:undefined)}><Avatar customer={c}/><span><strong>{customerName(c)}</strong><small>{c.phone||'手机号待补充'}</small></span></button>
  {c.customerNumber&&<div className="crm-card-number"><span>客户编号</span><span title={c.customerNumber}>{c.customerNumber}</span></div>}
  <dl className="crm-card-fields">
   <div><dt>咨询类型</dt><dd>{c.category}</dd></div>
   <div><dt>意向 / 阶段</dt><dd><span className={'crm-intent '+c.intent}>{intents[c.intent]}意向</span> <Badge stage={c.stage}/></dd></div>
   <div><dt>负责人</dt><dd>{filter.queue==='pool'?'客户公海':customerOwner(c,employees.data||[],account)}</dd></div>
   <div><dt>下次跟进</dt><dd className={c.nextFollowupAt&&new Date(c.nextFollowupAt)<new Date()?'crm-red':''}>{dateTime(c.nextFollowupAt)}</dd></div>
  </dl>
  {c.protectionExpiresAt&&!(filter.queue==='pool'||hasRecordedPool(c))&&<p className="crm-card-protection">保护至 {dateTime(c.protectionExpiresAt)}</p>}
  <button className="crm-card-open" onClick={()=>open(c.id,filter.queue==='pool'?{customerId:c.id,version:c.version}:undefined)}>{(filter.queue==='pool'||hasRecordedPool(c))?'查看 / 认领':'查看 / 跟进'}<FiChevronRight/></button>
 </article>)}</div></div><footer className="crm-pagination"><span>共 {data.total} 位客户</span><div><button disabled={(filter.page||1)<=1} onClick={()=>setFilter(f=>({...f,page:(f.page||1)-1}))}>上一页</button><span>第 {filter.page||1} 页</span><button disabled={(filter.page||1)*20>=data.total} onClick={()=>setFilter(f=>({...f,page:(f.page||1)+1}))}>下一页</button></div></footer></>:!error&&<Empty text={filter.queue==='pool'?'暂无可认领的公海客户':'暂无符合条件的客户'} report={report}/>}
 </section>;
}

function CustomerDrawer({id,poolEvidence,supervisor,close,changed,checkPool,pendingChanged}:{id:string;poolEvidence?:PoolEvidence;supervisor:boolean;close:()=>void;changed:()=>void;checkPool:()=>void;pendingChanged?:(pending:boolean)=>void}){
 const account=useManagementSession().crmContext?.account;
 const resource=useResource<Detail>('/customers/'+encodeURIComponent(id));const employees=useResource<Employee[]>('/employees');const [mode,setMode]=useState<'detail'|'edit'|'follow'|'report'>('detail');const closeRef=useRef<HTMLButtonElement>(null);
 // Detail reads may resolve a merged record to another master. Never silently retarget a write.
 const returnedCustomer=resource.data?.customer;const identityMismatch=!!returnedCustomer&&returnedCustomer.id!==id;
 const c=returnedCustomer?.id===id?returnedCustomer:undefined;const pool=!!c&&(hasRecordedPool(c)||hasPoolEvidence(c,poolEvidence));const claimTarget=useRef(c);claimTarget.current=c;const name=(owner:string|null)=>employees.data?.find(e=>e.id===owner)?.name||owner||'未分配';
 const [claiming,setClaiming]=useState(false),[claimError,setClaimError]=useState(''),[savedNotice,setSavedNotice]=useState('');const claimLock=useRef(false),activeId=useRef(id);activeId.current=id;
 const [mutationPending,setMutationPending]=useState(false);const mutation=useRef({pending:false,token:0,alive:true});
 const beginMutation:CustomerMutationGate=()=>{
  if(mutation.current.pending||!mutation.current.alive||activeId.current!==id||claimTarget.current?.id!==id)return null;
  const token=++mutation.current.token;mutation.current.pending=true;setMutationPending(true);pendingChanged?.(true);let released=false;
  return()=>{if(released||mutation.current.token!==token)return;released=true;mutation.current.pending=false;if(mutation.current.alive)setMutationPending(false);pendingChanged?.(false)};
 };
 const requestClose=()=>{if(!mutation.current.pending&&mutation.current.alive)close()};
 const changeMode=(next:typeof mode)=>{if(mutation.current.pending)return;setSavedNotice('');setMode(next)};
 const inspectPool=()=>{if(!mutation.current.pending)checkPool()};
 useEffect(()=>{mutation.current.alive=true;return()=>{mutation.current.alive=false}},[]);
 useEffect(()=>{const prev=document.activeElement as HTMLElement;closeRef.current?.focus();const esc=(e:KeyboardEvent)=>{if(e.key==='Escape'&&mode==='detail')requestClose();if(e.key==='Tab'){const panel=closeRef.current?.closest('section');const items=Array.from(panel?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href]')||[]).filter(el=>el.getClientRects().length);const first=items[0],last=items[items.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}};window.addEventListener('keydown',esc);return()=>{window.removeEventListener('keydown',esc);prev?.focus()}},[mode,id]);
 const saved=()=>{setSavedNotice('本次操作已保存。');setMode('detail');resource.reload();changed()};
 const claim=async()=>{if(!c||claimTarget.current!==c||!pool||claimLock.current||activeId.current!==id)return;const release=beginMutation();if(!release)return;claimLock.current=true;setClaiming(true);setClaimError('');
  try{await crm('/customers/'+encodeURIComponent(c.id)+'/claim',{method:'POST',body:{version:c.version}});if(activeId.current===id){release();saved()}}catch(e){if(activeId.current===id){setClaimError(errText(e)+'。请重新读取核对当前归属，认领以服务端校验为准。');resource.reload()}}finally{release();claimLock.current=false;if(activeId.current===id)setClaiming(false)}
 };
 useEffect(()=>{activeId.current=id;return()=>{activeId.current=''}},[]);
 return <div className="crm-overlay"><section className="crm-drawer" role="dialog" aria-modal="true" aria-label={mode==='detail'?'客户详情':mode==='edit'?'编辑客户资料':mode==='report'?'编辑报备资料':'记录跟进'}><header><h2>{mode==='detail'?'客户详情':mode==='edit'?'编辑客户资料':mode==='report'?'编辑报备资料':'记录跟进'}</h2><button ref={closeRef} aria-label="关闭客户详情" disabled={mutationPending} onClick={requestClose}><FiX/></button></header><div className="crm-drawer-body">{mutationPending&&<div className="crm-note" role="status">正在提交，请等待结果后关闭或切换操作。</div>}{savedNotice&&<div className="crm-note" role="status">{savedNotice}{resource.error?'最新档案读取失败，请重试读取，无需重复提交。':resource.loading?'正在读取最新档案…':''}</div>}<Feedback error={claimError}/><Feedback error={resource.error} retry={resource.reload}/>{identityMismatch&&<div className="crm-feedback" role="alert"><p>读取到的客户与当前打开对象不一致（请求 {id}，返回 {returnedCustomer?.id}）。档案可能已合并，本窗口已停止编辑和认领。请返回客户列表核对主档后重新打开。</p><button disabled={mutationPending} onClick={requestClose}>关闭并核对客户列表</button><button disabled={mutationPending} onClick={()=>{if(!mutation.current.pending)resource.reload()}}>重新读取当前档案</button></div>}{resource.loading?<div className="crm-loading">正在读取档案…</div>:c&&<>
 <div className="crm-customer-heading"><Avatar customer={c}/><div><h2>{customerName(c)}</h2><p>{c.phone||'手机号待补充'} · {c.category}</p><Badge stage={c.stage}/> <span className={'crm-intent '+c.intent}>{intents[c.intent]}意向</span></div></div>
 {mode==='report'?<CustomerReportForm key={c.id} customer={c} cancel={()=>changeMode('detail')} saved={saved} beginMutation={beginMutation}/>:mode!=='detail'?<CustomerForm key={c.id+mode} files={resource.data?.attachments||[]} customer={c} mode={mode} cancel={()=>changeMode('detail')} saved={saved} beginMutation={beginMutation}/>:<>
  {c.lifecycle==='protected'&&<div className="crm-note">{pool?'服务端公海查询已返回此客户，认领时仍会核验最新归属。':'档案记录的保护截止时间：'+dateTime(c.protectionExpiresAt)+'。当前公海与归属状态由服务端核验。'}</div>}
  <div className="crm-actions crm-detail-actions">{pool?<button className="crm-primary" disabled={claiming||mutationPending} onClick={claim}>{claiming?'正在认领…':'重新报备并认领'}</button>:<><button disabled={mutationPending} onClick={()=>changeMode('report')}>{c.lifecycle==='pending'?'完善报备资料':'编辑报备资料'}</button><button disabled={mutationPending} onClick={()=>changeMode('edit')}>编辑资料</button><button className="crm-primary" disabled={mutationPending} onClick={()=>changeMode('follow')}>记录跟进</button></>}</div>
  <p className="crm-caption">若到期状态尚未同步或操作被拒绝，可到客户公海核对；不会依据本机时间自动授予认领权限。<button disabled={mutationPending} onClick={inspectPool}>查看客户公海核对状态</button></p>
  {c.customerNumber&&<dl className="crm-profile"><dt>客户编号</dt><dd className="crm-customer-number">{c.customerNumber}</dd><dt>微信</dt><dd>{c.wechat||'未填写'}</dd><dt>镇村地址</dt><dd>{c.address}</dd><dt>面积 / 需求</dt><dd>{c.buildingArea} ㎡ · {c.buildingType}</dd><dt>预计建房</dt><dd>{c.expectedBuildTime}</dd><dt>其他补充</dt><dd>{c.remark||'无'}</dd></dl>}
  <dl className="crm-profile"><dt>业务负责人</dt><dd>{customerOwner(c,employees.data||[],account)}</dd><dt>添加时间</dt><dd>{c.addedTimeSource==='unknown'?'待核实':dateTime(c.createdAt)}</dd><dt>下次跟进</dt><dd>{dateTime(c.nextFollowupAt)}</dd><dt>最近跟进</dt><dd>{c.lastFollowupAt?dateTime(c.lastFollowupAt):'尚未跟进'}</dd></dl>
  <h3>需求摘要</h3><p className="crm-demand">{c.demand||'待补充客户需求'}</p>
  <h3>企微好友关系</h3>{resource.data?.relations.length?resource.data.relations.map(r=><div className="crm-relation" key={r.eId}><span>{name(r.userId)}</span><span className={'crm-badge '+(r.status===0?'won':'paused')}>{({0:'正常',1:'客户已删除员工',2:'员工已删除客户'} as Record<number,string>)[r.status]||'关系待核实'}</span></div>):<p className="crm-caption">{c.customerNumber?'手动报备客户，尚未关联企微好友':'好友关系待同步'}</p>}
  <p className="crm-caption">调整业务负责人由主管通过「分配 / 交接」处理；交接不转移企微好友关系，删除好友后仍保留历史档案。</p>
  {resource.data&&<WorkflowDetails detail={resource.data} poolEvidence={pool?{customerId:c.id,version:c.version}:undefined} supervisor={supervisor} starting={()=>setSavedNotice('')} saved={saved} beginMutation={beginMutation} parentPending={mutationPending} canEdit={()=>!mutation.current.pending}/>}<h3>跟进与操作记录</h3><div className="crm-timeline">{resource.data?.followups.map(f=><article key={f.id}><small>{dateTime(f.createdAt)} · {f.operatorId} · {activityNames[f.kind]||f.kind}</small><p>{f.content}</p>{['followup','communication','drawing','quotation'].includes(f.kind)&&<small>{f.effective?'有效跟进':'不计入续期'}{f.reviewReason&&' · 审核理由：'+f.reviewReason}</small>}<Badge stage={f.stage}/>{f.nextFollowupAt&&<small>下次联系 {dateTime(f.nextFollowupAt)}</small>}</article>)}{!resource.data?.followups.length&&<p>暂无跟进记录</p>}</div>
 </>}
 </>}</div></section></div>;
}
function localInput(s:string|null){if(!s)return '';const d=new Date(s);const pad=(v:number)=>String(v).padStart(2,'0');return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+'T'+pad(d.getHours())+':'+pad(d.getMinutes())}
function CustomerForm({customer:c,mode,cancel,saved,files,beginMutation}:{files:Attachment[];customer:Customer;mode:'edit'|'follow';cancel:()=>void;saved:()=>void;beginMutation?:CustomerMutationGate}){
 const [form,setForm]=useState({phone:c.phone||'',demand:c.demand||'',category:c.category,intent:c.intent,stage:c.stage,next:localInput(c.nextFollowupAt),content:'',kind:'followup',outcome:'contacted'});const [busy,setBusy]=useState(false),[error,setError]=useState(''),[conflict,setConflict]=useState(false);const [version,setVersion]=useState(c.version);const [baseline,setBaseline]=useState(c);const [latest,setLatest]=useState<Customer|null>(null);const [evidence,setEvidence]=useState<string[]>([]);
 const operation=useRef({busy:false,completed:false,alive:true,conflict:false,target:c.id});operation.current.conflict=conflict;const target=c.id;
 useEffect(()=>{operation.current.alive=true;return()=>{operation.current.alive=false}},[]);
 const current=()=>operation.current.alive&&operation.current.target===target;
 const field=(k:string,v:string)=>setForm(f=>({...f,[k]:v}));const active=form.stage==='new'||form.stage==='following';
 const submit=async(e:React.FormEvent)=>{e.preventDefault();if(!current()||operation.current.busy||operation.current.completed||operation.current.conflict)return;const release=beginMutation?.();if(beginMutation&&!release)return;operation.current.busy=true;setBusy(true);setError('');
  try {
   if(mode==='follow'){
    const next=active&&form.next?new Date(form.next).toISOString():null;
    if(active&&!next)throw new Error('请安排下次联系时间，是否晚于当前时间由服务端校验');
    await crm('/customers/'+encodeURIComponent(c.id)+'/followups',{method:'POST',body:{version,kind:form.kind,outcome:form.outcome,attachmentIds:evidence,content:form.content.trim(),stage:form.stage,nextFollowupAt:next}});
   }else await crm('/customers/'+encodeURIComponent(c.id),{method:'PATCH',body:{version,phone:form.phone,demand:form.demand,category:form.category,intent:form.intent,ownerId:baseline.ownerId,stage:baseline.stage,nextFollowupAt:baseline.nextFollowupAt}});
  }catch(e){if(current()){setError(errText(e));if(e instanceof CRMError&&(e.status===409||e.status===403)){operation.current.conflict=true;setConflict(true)}operation.current.busy=false;setBusy(false)}return}finally{release?.()}
  if(!current())return;operation.current.completed=true;operation.current.busy=false;setBusy(false);setError('本次操作已保存，无需重复提交。');
  try{saved()}catch{setError('本次操作已保存，但界面更新失败，请重新读取核对。')}
 };
 const compare=async()=>{if(!current()||operation.current.busy)return;operation.current.busy=true;setBusy(true);setLatest(null);try{const d=await crm<Detail>('/customers/'+encodeURIComponent(c.id));if(!current())return;if(d.customer.id!==c.id)throw new Error('档案已合并或目标变化，请返回列表打开当前主档');setLatest(d.customer)}catch(e){if(current())setError(errText(e))}finally{if(current()){operation.current.busy=false;setBusy(false)}}};
 const confirmLatest=(keep=true)=>{if(!latest||!current()||operation.current.busy)return;setVersion(latest.version);setBaseline(latest);if(!keep){setForm({phone:latest.phone||'',demand:latest.demand||'',category:latest.category,intent:latest.intent,stage:latest.stage,next:localInput(latest.nextFollowupAt),content:'',kind:'followup',outcome:'contacted'});setEvidence([])}setConflict(false);setLatest(null);setError(keep?'已采用最新版本，请核对当前输入后再次保存。':'已重新载入最新资料。')};
 return <form className="crm-form" onSubmit={submit}>
 <Feedback error={error}/>{conflict&&<div className="crm-note"><p>输入已保留。先读取最新档案核对版本与归属；若仍被拒绝，可返回详情到客户公海核对。系统不会自动覆盖。</p><button type="button" disabled={busy} onClick={compare}>读取最新版本</button>{latest&&<><p>最新阶段：{stages[latest.stage]}；负责人：{latest.ownerId||'未分配'}；电话：{latest.phone||'未填写'}；需求：{latest.demand||'未填写'}；下次跟进：{dateTime(latest.nextFollowupAt)}</p><button type="button" disabled={busy} onClick={()=>confirmLatest(true)}>已核对，保留我的输入继续编辑</button><button type="button" disabled={busy} onClick={()=>confirmLatest(false)}>重新载入最新资料，清空当前输入</button></>}</div>}
 {mode==='edit'?<><label>客户昵称<input disabled={busy||operation.current.completed} value={customerName(c)} readOnly/><small>{c.customerNumber?'在“编辑报备资料”中修改姓名与联系电话':'由企业微信同步维护'}</small></label><label>联系电话<input disabled={busy||operation.current.completed} readOnly={!!c.customerNumber} value={form.phone} maxLength={80} onChange={e=>field('phone',e.target.value)} placeholder="可稍后补充"/></label><label>咨询类型<select disabled={busy||operation.current.completed} value={form.category} onChange={e=>field('category',e.target.value)}>{categories.map(x=><option key={x}>{x}</option>)}</select></label><label>意向程度<select disabled={busy||operation.current.completed} value={form.intent} onChange={e=>field('intent',e.target.value)}>{Object.entries(intents).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label><label>需求摘要<textarea disabled={busy||operation.current.completed} rows={5} value={form.demand} maxLength={2000} onChange={e=>field('demand',e.target.value)}/></label></>:<><label>跟进类型<select disabled={busy||operation.current.completed} value={form.kind} onChange={e=>field('kind',e.target.value)}>{['followup','communication','drawing','quotation'].map(k=><option key={k} value={k}>{activityNames[k]}</option>)}</select></label><label>联系结果<select disabled={busy||operation.current.completed} value={form.outcome} onChange={e=>field('outcome',e.target.value)}><option value="contacted">有效联系 / 已完成</option><option value="no_answer">电话未接通</option><option value="no_reply">消息未回复</option><option value="internal">内部安排</option></select><small>未接通、未回复和内部安排只记记录，不延长保护。</small></label><Evidence files={files} selected={evidence} change={setEvidence}/><label><span>跟进内容 <b className="crm-red">*</b></span><textarea disabled={busy||operation.current.completed} autoFocus required rows={6} value={form.content} maxLength={2000} onChange={e=>field('content',e.target.value)} placeholder="例如：电话沟通，已确认两层建房需求，待补充宅基地照片。"/><small>{form.content.length}/2000</small></label><label>客户阶段<select disabled={busy||operation.current.completed} value={form.stage} onChange={e=>field('stage',e.target.value)}>{Object.entries(stages).filter(([k])=>!['won','invalid'].includes(k)||k===c.stage).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label>{active?<label><span>下次联系时间 <b className="crm-red">*</b></span><input disabled={busy||operation.current.completed} type="datetime-local" required value={form.next} onChange={e=>field('next',e.target.value)}/><small>按当前设备时区填写；保存后统一按北京时间展示。</small></label>:<div className="crm-note">暂缓会结束当前待办，但仍受保护到期规则约束。成交及无效归档请提交主管申请。</div>}</>}
 <footer className="crm-form-footer"><button type="button" disabled={busy} onClick={cancel}>取消</button><button type="submit" className="crm-primary" disabled={busy||conflict||operation.current.completed}>{busy?'正在保存…':mode==='edit'?'保存资料':'保存跟进'}</button></footer>
 </form>;
}

function SyncPanel(){
 const {data,error,loading,reload}=useResource<SyncState>('/sync',undefined,true);const [busy,setBusy]=useState(false),[message,setMessage]=useState('');
 const operation=useRef({busy:false,alive:true,accepted:new Set<string>()}),currentData=useRef(data);currentData.current=data;
 useEffect(()=>{if(data)operation.current.accepted.clear()},[data]);useEffect(()=>{operation.current.alive=true;return()=>{operation.current.alive=false}},[]);
 const act=async(path:string)=>{const state=currentData.current;const eventId=path.startsWith('/sync/events/')?decodeURIComponent(path.slice('/sync/events/'.length,-'/retry'.length)):null;
  if(operation.current.busy||!operation.current.alive||operation.current.accepted.has(path)||!state)return;
  if(path==='/sync'&&(!state.configured||state.runs.some(r=>r.status==='pending'||r.status==='running')))return;
  if(eventId&&!state.events.some(e=>e.id===eventId&&e.status==='failed'))return;operation.current.busy=true;setBusy(true);setMessage('');try{await crm(path,{method:'POST',body:{}});if(operation.current.alive){operation.current.accepted.add(path);setMessage('任务已提交；若接入状态读取失败，请重试读取，无需重复提交。');reload()}}catch(e){if(operation.current.alive)setMessage(errText(e))}finally{operation.current.busy=false;if(operation.current.alive)setBusy(false)}};
 const statuses:Record<string,string>={pending:'等待处理',running:'处理中',done:'已完成',failed:'失败',success:'已完成'};
 return <><Feedback error={error} retry={reload}/>{message&&<div className="crm-note" role="status">{message}</div>}{loading?<div className="crm-loading">读取接入状态…</div>:data&&<>
 <section className="crm-panel"><div className="crm-section-heading"><div><h2>企业微信接入</h2><p>首次配置后，新增客户自动接入；手动同步用于初始化与补同步。</p></div><button className="crm-primary" disabled={busy||!data.configured||data.runs.some(r=>r.status==='pending'||r.status==='running')} onClick={()=>act('/sync')}>发起同步</button></div><div className="crm-status-grid"><div>客户读取参数<strong>{data.configured?'已配置':'未配置'}</strong></div><div>事件回调参数<strong>{data.callbackConfigured?'已配置':'未配置'}</strong></div><div>待处理事件<strong>{data.pending}</strong></div><div>失败事件<strong className="crm-red">{data.failed}</strong></div></div><p className="crm-caption">配置齐备不等于连接成功，请以实际同步结果和真实加好友验证为准。密钥由服务器环境配置。</p></section>
 <section className="crm-panel"><h2>同步任务</h2><div className="crm-table-wrap"><table><thead><tr><th>开始时间</th><th>状态</th><th>已处理</th><th>结束时间</th><th>结果说明</th></tr></thead><tbody>{data.runs.map(r=><tr key={r.id}><td>{dateTime(r.startedAt)}</td><td>{statuses[r.status]||r.status}</td><td>{r.processed}</td><td>{r.finishedAt?dateTime(r.finishedAt):'—'}</td><td>{r.error||'—'}</td></tr>)}</tbody></table></div>{!data.runs.length&&<p className="crm-caption">尚未发起同步，连接待验证。</p>}</section>
 <section className="crm-panel"><h2>未完成事件</h2><div className="crm-table-wrap"><table><thead><tr><th>发生时间</th><th>事件类型</th><th>状态</th><th>失败原因</th><th>操作</th></tr></thead><tbody>{data.events.map(e=><tr key={e.id}><td>{dateTime(e.createdAt)}</td><td>{e.changeType}</td><td>{statuses[e.status]||e.status}</td><td>{e.error||'—'}</td><td><button disabled={busy||e.status!=='failed'} onClick={()=>act('/sync/events/'+encodeURIComponent(e.id)+'/retry')}>重试</button></td></tr>)}</tbody></table></div>{!data.events.length&&<p className="crm-caption">没有待处理或失败事件。</p>}</section>
 </>}</>;
}

interface AccessUser {userId:number;username:string;scope:string;enabled:boolean;version:number;crmUsername:string;state:string;crmEnabled:boolean}
interface AccessData {users:AccessUser[];accounts:Account[];serviceAvailable:boolean}
const accessForm=(u:AccessUser,accounts:Account[])=>{const a=accounts.find(a=>a.username===u.crmUsername);return {scope:u.scope,enabled:u.enabled,crmEnabled:u.crmEnabled,displayName:a?.displayName||u.username,employeeId:a?.employeeId||'',role:a?.role||'staff'}};
type AccessSnapshot={user:AccessUser;form:ReturnType<typeof accessForm>};
function AccessPanel(){
 const {data,error,loading,reload}=useResource<AccessData>('/access');const employees=useResource<Employee[]>('/access/employees');
 // 归属绑定完整性：用主管视角的按归属聚合（dashboard.team）对照已绑定账号，
 // 找出"名下有客户但没有后台账号绑定该员工"的归属——顾问看不到这些客户。
 const coverage=useResource<Dashboard>('/dashboard');
 const boundUsernames=new Set((data?.accounts||[]).map(a=>a.username));
 const boundEmployeeIds=new Set((data?.accounts||[]).map(a=>a.employeeId||''));
 const unboundOwners=(coverage.data?.team||[]).filter(t=>t.ownerId&&!boundUsernames.has(t.ownerId)&&!boundEmployeeIds.has(t.ownerId));
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
 return <><Feedback error={error||employees.error} retry={()=>{reload();employees.reload()}}/>{message&&<div className="crm-note" role="status">{message}</div>}<div className="crm-note">系统管理员配置平台权限；客户主管只负责客户业务。顾问默认仅访问客户模块。</div>
 <div className="crm-access-grid"><section className="crm-panel"><div className="crm-section-heading"><h2>后台账号</h2><button onClick={()=>{resetOperation();setMessage('');setNewUser(v=>!v)}}>新增账号</button></div>{newUser&&<form className="crm-form" onSubmit={create}><label>账号<input required minLength={3} maxLength={64} value={username} onChange={e=>setUsername(e.target.value)} autoComplete="off"/></label><label>初始密码<input required type="password" minLength={12} maxLength={72} value={password} onChange={e=>setPassword(e.target.value)} autoComplete="new-password"/></label><button className="crm-primary" disabled={busy}>创建后台账号</button></form>}{loading?<p>读取账号中…</p>:data?.users.map(u=><button className={'crm-account-row '+(selection?.userId===u.userId?'active':'')} onClick={()=>choose(u)} key={u.userId}><strong>{u.username}</strong><span>{u.enabled?'已启用':'已停用'} · {u.state==='active'?'已关联':u.state==='pending'?'待处理（配置未完成）':u.state==='failed'?'关联失败':'未关联'}</span><FiChevronRight/></button>)}</section>
 <section className="crm-panel"><h2>账号关联与授权</h2>{data&&!data.serviceAvailable&&<Feedback error="客户服务不可用，暂不能保存关联。请检查内部接入配置。"/>}{!selection?<p className="crm-caption">选择左侧账号，配置对应的企微员工和数据角色。</p>:<form className="crm-form" onSubmit={submit}>
 {conflict&&<div className="crm-note" role="alert"><p>授权存在版本或员工绑定冲突。当前输入仍绑定原版本，核对前不能再次保存。</p><button type="button" disabled={busy} onClick={compare}>{busy?'正在读取…':'读取最新权限'}</button>{latest&&<><p>最新权限（版本 {latest.user.version}）：{latest.form.scope==='platform_admin'?'平台管理员':'仅客户管理'}；后台访问{latest.form.enabled?'启用':'停用'}；客户访问{latest.form.crmEnabled?'启用':'停用'}；客户角色{latest.form.role==='admin'?'客户主管':'顾问'}；显示名称：{latest.form.displayName}；企微员工：{employees.data?.find(e=>e.id===latest.form.employeeId)?.name||latest.form.employeeId||'不绑定'}。</p><p>保留输入继续编辑会以以上版本提交当前表单，请逐项核对权限差异。</p><button type="button" disabled={busy} onClick={()=>adoptLatest(true)}>已核对，保留我的输入继续编辑</button><button type="button" disabled={busy} onClick={()=>adoptLatest(false)}>重新载入最新权限，替换我的输入</button></>}</div>}
 <label>后台账号<input disabled={busy} readOnly value={selection.username}/></label><label>显示名称<input disabled={busy} required maxLength={100} value={form.displayName} onChange={e=>setForm(f=>({...f,displayName:e.target.value}))}/></label><label>平台访问范围<select disabled={busy} value={form.scope} onChange={e=>setForm(f=>({...f,scope:e.target.value}))}><option value="crm_only">仅客户管理</option><option value="platform_admin">平台管理员（包含其他后台模块）</option></select></label><label>客户数据角色<select disabled={busy} value={form.role} onChange={e=>setForm(f=>({...f,role:e.target.value}))}><option value="staff">顾问 · 仅本人负责的客户</option><option value="admin">客户主管 · 全部客户</option></select></label><label>企业微信员工<select disabled={busy} value={form.employeeId} onChange={e=>setForm(f=>({...f,employeeId:e.target.value}))}><option value="">不绑定（渠道账号）</option>{employees.data?.map(e=><option key={e.id} value={e.id} disabled={!e.active}>{e.name}{!e.active?'（已停用）':''}</option>)}</select></label><label className="crm-check"><input disabled={busy} type="checkbox" checked={form.enabled} onChange={e=>setForm(f=>({...f,enabled:e.target.checked}))}/>启用后台访问</label><label className="crm-check"><input disabled={busy} type="checkbox" checked={form.crmEnabled} onChange={e=>setForm(f=>({...f,crmEnabled:e.target.checked}))}/>启用客户访问</label><p className="crm-caption">企微客户权限与平台管理权限分别授予。统一账号不需要第二份CRM密码。</p><button className="crm-primary" disabled={busy||conflict||!data?.serviceAvailable}>{busy?'正在关联…':'保存授权'}</button>{current?.state==='pending'&&<button type="button" disabled={busy||conflict} onClick={retry}>重试未完成的配置</button>}</form>}</section></div>{coverage.data&&coverage.data.team.length>0&&<section className="crm-panel"><h2>归属绑定完整性</h2>{unboundOwners.length?<><p className="crm-caption">以下归属名下有客户，但没有后台账号绑定该员工；对应人员登录后看不到这些客户。请为其创建并绑定账号，或由主管转交客户。</p>{unboundOwners.map(t=><div className="crm-action-row" key={t.ownerId}><div><strong>{t.name}</strong><p>归属标识 {t.ownerId} · 名下 {t.total} 位客户</p></div><button onClick={()=>{setNewUser(true);window.scrollTo({top:0,behavior:'smooth'})}}>新增账号绑定</button></div>)}</>:<p className="crm-caption">所有有客户归属的员工均已绑定后台账号。</p>}<p className="crm-caption">按客户主管视角统计，不含公海与已到期客户；渠道账号可以不绑定员工。当前账号需具有客户主管角色才能统计完整归属。</p></section>}{selection&&<WecomIdentityBinding key={selection.userId} userId={selection.userId} username={selection.username} employees={employees.data||[]}/>}
 </>;
}
