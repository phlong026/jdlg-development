import React,{useEffect,useRef,useState} from 'react';
import {Link,Navigate,useSearchParams} from 'react-router-dom';
import {FiUsers,FiSearch,FiX,FiChevronRight,FiFilter} from 'react-icons/fi';
import {useManagementSession} from '../component/ManagementSession';
import {crm,CRMError,activityNames,downloadCustomers,stages,categories,intents,dateTime,type Customer,type Detail,type Employee,type Filter,type CustomerPage,type Stage,type Account,type Attachment} from '../api/wecomCustomers';
import CustomerReportForm from './customerReportForm';
import {WorkflowDetails,WorkflowWorkbench,NoticePanel,Evidence,hasRecordedPool,hasPoolEvidence,type PoolEvidence,type CustomerMutationGate} from './customerWorkflow';
import {Feedback,errText,useResource} from './customerResource';
import {customerTabs,customerFilter,resolveCustomerTab,type CustomerTabKey} from './customerTabs';
import {timeAgo,maskPhone} from './salesFormat';
import './customerManagement.scss';
import './customerResponsive.scss';
import './salesModule.scss';

const titleOf:Record<string,string>={customers:'客户',tasks:'跟进待办',notices:'我的提醒',workbench:'主管待处理'};
const extraTabs=[{key:'tasks',label:'待办'},{key:'notices',label:'提醒'},{key:'workbench',label:'待处理',supervisorOnly:true}];
const queueNames:Record<string,string>={pending:'待完善报备',pool:'客户公海',all:'全部客户',added:'今日新增',new:'新客户',today:'今日待办',overdue:'逾期',unplanned:'未安排',unassigned:'未分配'};
// 旧地址兼容：平台管理员访问旧栏目时跳到新位置
const legacyTargets:Record<string,string>={leads:'/customer-leads',sync:'/customer-settings?item=sync',access:'/customer-settings?item=access'};
function Empty({text='暂无符合条件的客户',report}:{text?:string;report?:()=>void}){return <div className="crm-empty"><FiUsers/><strong>{text}</strong>{report&&<button className="crm-primary" onClick={report}>新增客户</button>}</div>}
function Badge({stage}:{stage:Stage}){return <span className={'crm-badge '+stage}>{stages[stage]||stage}</span>}
function Avatar({customer}:{customer:Customer}){return <span className="crm-avatar">{customer.name?.slice(0,1)||'客'}</span>}
// 小程序咨询进来的客户常常没有姓名：显示「微信用户 #编号尾 6 位」，不再用「待同步客户」冒充同步状态
function customerName(c:Customer){return c.name||'微信用户 #'+(c.customerNumber||c.id).slice(-6)}
// 只有明确评过的三档才显示「X 意向」；其余（unknown、空值）一律「未评级」，不把默认值当判断
function intentLabel(intent:string){return ['high','medium','low'].includes(intent)?intents[intent]+'意向':'未评级'}
function customerOwner(c:Customer,employees:Employee[],account?:Account){return hasRecordedPool(c)?'客户公海':employees.find(e=>e.id===c.ownerId)?.name||(c.ownerUsername===account?.username?account?.displayName:c.ownerUsername)||c.ownerId||'未分配'}

export default function CustomerManagement(){
 const session=useManagementSession();const [params,setParams]=useSearchParams();
 const drawerGate=useRef({pending:false,alive:true});const [drawerBusy,setDrawerBusy]=useState(false);
 useEffect(()=>{drawerGate.current.alive=true;return()=>{drawerGate.current.alive=false}},[]);
 const setDrawerPending=(pending:boolean)=>{drawerGate.current.pending=pending;if(drawerGate.current.alive)setDrawerBusy(pending)};
 const supervisor=session.crmContext?.account.role==='admin';const platform=session.user?.scope==='platform_admin';
 const allowed=session.crmContext?['customers','tasks','notices',...(supervisor?['workbench']:[])]:[];
 const requested=params.get('tab')||'customers';
 const alias:Record<string,string>={home:'customers',pool:'customers'};
 const resolved=alias[requested]||requested;const tab=allowed.includes(resolved)?resolved:(allowed[0]||'customers');
 const view:CustomerTabKey=resolveCustomerTab(requested==='pool'?'pool':params.get('view'));
 const legacy=platform?legacyTargets[requested]:undefined;
 // 无权限的 tab 直达时把地址栏校正为实际栏目，避免刷新/分享后状态不一致
 useEffect(()=>{if(session.crmContext&&!legacy&&requested!==tab&&!(requested in alias)){const next:Record<string,string>={};params.forEach((v,k)=>{next[k]=v});next.tab=tab;setParams(next,{replace:true})}},[requested,tab,legacy,session.crmContext,params,setParams]);
 const go=(next:string,extra?:Record<string,string>)=>{if(!drawerGate.current.pending)setParams({tab:next,...extra})};
 const setView=(next:CustomerTabKey)=>go('customers',next==='all'?undefined:{view:next});
 const [reporting,setReporting]=useState(false);
 const [selected,setSelected]=useState<{id:string;poolEvidence?:PoolEvidence}|null>(null),[refresh,setRefresh]=useState(0);
 const [completion,setCompletion]=useState('');const openCustomer=(id:string,poolEvidence?:PoolEvidence)=>{if(drawerGate.current.pending)return;setSelected({id,poolEvidence});if(params.has('customer')){const next=new URLSearchParams(params);next.set('customer',id);setParams(next)}};
 useEffect(()=>{if(!drawerGate.current.pending)setSelected(null)},[tab,view,session.user?.id]);
 // 窄屏下标签条横向滚动：直达或切换后把当前标签带入可视区（只滚动标签条本身，不移动页面），并用 data-fade 标出两侧还有更多标签
 const tabsRef=useRef<HTMLElement>(null);const tabKeys=allowed.join('|');
 useEffect(()=>{const track=tabsRef.current;if(!track)return;
  const fade=()=>{const left=track.scrollLeft>1,right=track.scrollWidth-track.clientWidth-track.scrollLeft>1;if(left||right)track.dataset.fade=left&&right?'both':left?'left':'right';else delete track.dataset.fade};
  const active=track.querySelector<HTMLElement>('.active');
  if(active){const viewport=track.getBoundingClientRect(),item=active.getBoundingClientRect(),edge=36;
   if(item.left<viewport.left+edge)track.scrollLeft+=item.left-viewport.left-edge;else if(item.right>viewport.right-edge)track.scrollLeft+=item.right-viewport.right+edge}
  fade();track.addEventListener('scroll',fade,{passive:true});window.addEventListener('resize',fade);
  return()=>{track.removeEventListener('scroll',fade);window.removeEventListener('resize',fade)}},[tab,view,tabKeys,session.crmLoading]);
 const routeCustomer=params.get('customer');
 const validRouteCustomer=routeCustomer!==null&&routeCustomer.length>0&&routeCustomer.length<=128&&!/[\/\\%;\r\n]/.test(routeCustomer)&&routeCustomer!=='.'&&routeCustomer!=='..';
 const linkedSelection:{id:string;poolEvidence?:PoolEvidence}|null=routeCustomer!==null ? (validRouteCustomer&&session.crmContext&&tab==='customers'?{id:routeCustomer}:null) : selected;
 const closeCustomer=()=>{if(drawerGate.current.pending)return;setSelected(null);if(routeCustomer!==null){const next=new URLSearchParams(params);next.delete('customer');setParams(next,{replace:true})}};
 const changed=()=>{if(!drawerGate.current.pending)setRefresh(n=>n+1)};
 if(legacy)return <Navigate to={legacy} replace/>;
 if(session.crmLoading)return <><div className="crm-loading">正在读取客户权限…</div></>;
 return <><div className="crm-page sx-customers">
  <header className="sx-page-head"><h1>{titleOf[tab]}</h1><div className="crm-actions">{session.crmContext&&<button className="crm-primary" disabled={drawerBusy} onClick={()=>{if(drawerGate.current.pending)return;setCompletion('');setReporting(true)}}>＋ 新增客户</button>}</div></header>
  {params.get('from')==='share-tracking'&&<p className="crm-note"><Link to="/share-tracking">返回分享访问记录</Link> · 关联为人工核实关系，客户权限在此重新检查</p>}
  {params.get('from')==='customer-leads'&&<p className="crm-note"><Link to="/customer-leads">返回销售线索</Link> · 已按线索同步到的客户档案打开，客户权限在此重新检查</p>}
  {routeCustomer!==null&&!validRouteCustomer&&<p role="alert" className="crm-error">客户目标无效，请返回{params.get('from')==='customer-leads'?'销售线索':'分享访问记录'}重新打开。</p>}
  {completion&&<div className="crm-note" role="status">{completion}</div>}
  {session.crmContext&&<nav className="crm-tabs sx-tabs" aria-label="客户栏目" ref={tabsRef}>{customerTabs.map(t=><button disabled={drawerBusy} key={t.key} className={tab==='customers'&&view===t.key?'active':''} onClick={()=>setView(t.key)}>{t.label}</button>)}<span className="sx-tabs-gap" aria-hidden/>{extraTabs.filter(t=>!t.supervisorOnly||supervisor).map(t=><button disabled={drawerBusy} key={t.key} className={'sx-tab-extra '+(tab===t.key?'active':'')} onClick={()=>go(t.key)}>{t.label}</button>)}</nav>}
  {session.crmContext?.demo&&<div className="crm-note">演示环境 · 虚构数据</div>}
  {!session.crmContext?<Feedback error={session.crmError||'客户管理接入尚未配置，请联系系统管理员'} retry={session.refresh}/>:<React.Fragment key={session.user?.id}>
   {(tab==='customers'||tab==='tasks')&&<Customers key={tab+view+refresh} supervisor={supervisor} tasks={tab==='tasks'} view={view} initialQueue={params.get('queue')||undefined} open={openCustomer} report={()=>setReporting(true)}/>}
   {tab==='workbench'&&supervisor&&<WorkflowWorkbench key={'w'+refresh} open={openCustomer}/>}
   {tab==='notices'&&<NoticePanel key={'n'+refresh} open={openCustomer}/>}
  </React.Fragment>}
  {reporting&&<div className="crm-overlay"><section className="crm-drawer" role="dialog" aria-modal="true" aria-label="新增客户报备"><header><h2>新增客户报备</h2></header><div className="crm-drawer-body"><CustomerReportForm cancel={()=>setReporting(false)} saved={c=>{setReporting(false);setCompletion('客户报备已保存。若列表读取失败，请重新读取，无需再次报备。');changed();openCustomer(c.id)}}/></div></section></div>}
  {linkedSelection&&!reporting&&<CustomerDrawer key={linkedSelection.id} id={linkedSelection.id} poolEvidence={linkedSelection.poolEvidence} supervisor={supervisor} close={closeCustomer} changed={changed} pendingChanged={setDrawerPending} checkPool={()=>{if(drawerGate.current.pending)return;setSelected(null);setView('pool')}}/>}
 </div></>;
}

const scopeQueues=['all','pending','added','unplanned','unassigned'];
function Customers({supervisor,tasks,view,initialQueue,open,report}:{supervisor:boolean;tasks:boolean;view:CustomerTabKey;initialQueue?:string;open:(id:string,poolEvidence?:PoolEvidence)=>void;report:()=>void}){
 const account=useManagementSession().crmContext?.account;
 const [filter,setFilter]=useState<Filter>({...(tasks?{queue:initialQueue||'today'}:customerFilter(view)),page:1,page_size:20});const [keyword,setKeyword]=useState('');const [actionError,setActionError]=useState(''),[exporting,setExporting]=useState(false),[filtersOpen,setFiltersOpen]=useState(false);
 const {data,error,loading,reload}=useResource<CustomerPage>('/customers',filter,true);const employees=useResource<Employee[]>('/employees');
 useEffect(()=>{setFilter(f=>({...f,...(tasks?{queue:initialQueue||'today'}:customerFilter(view)),page:1}))},[view,tasks,initialQueue]);
 const set=(key:keyof Filter,value:string)=>setFilter(f=>({...f,[key]:value,page:1}));
 const pool=filter.queue==='pool';
 const taskQueues=['today','overdue','unplanned',...(supervisor?['unassigned']:[])];
 const exportFile=async(kind:'customers'|'followups')=>{setExporting(true);setActionError('');try{await downloadCustomers(kind,filter)}catch(e){setActionError(errText(e))}finally{setExporting(false)}};
 const activeFilters=(['source','category','intent','ownerId'] as (keyof Filter)[]).filter(k=>filter[k]).length+(!tasks&&!pool&&filter.queue!=='all'?1:0);
 return <section className="sx-customer-panel">{!supervisor&&!pool&&account&&!account.employeeId&&<div className="crm-note" role="status">当前账号未绑定企业微信员工，企微自动接入的客户不会出现在列表中（手动报备的客户不受影响）。请联系平台管理员绑定。</div>}
 {tasks&&<div className="crm-queue-tabs sx-queue-tabs">{taskQueues.map(q=><button key={q} className={filter.queue===q?'active':''} onClick={()=>set('queue',q)}>{queueNames[q]}</button>)}</div>}
 <form className="sx-toolbar" onSubmit={e=>{e.preventDefault();set('keyword',keyword)}}><label className="crm-search"><FiSearch/><input aria-label="搜索客户" value={keyword} onChange={e=>setKeyword(e.target.value)} placeholder="搜索姓名、电话、编号" maxLength={200}/></label><button type="button" className="sx-filter-toggle" aria-expanded={filtersOpen} onClick={()=>setFiltersOpen(v=>!v)}><FiFilter/>筛选{activeFilters>0&&<b>{activeFilters}</b>}</button></form>
 {filtersOpen&&<div className="sx-filters"><select aria-label="客户来源" value={filter.source||''} onChange={e=>set('source',e.target.value)}><option value="">全部来源</option><option value="manual">手动报备</option><option value="wecom">企业微信</option><option value="miniprogram">小程序咨询</option></select><select aria-label="咨询类型" value={filter.category||''} onChange={e=>set('category',e.target.value)}><option value="">全部分类</option>{categories.map(c=><option key={c}>{c}</option>)}</select><select aria-label="意向" value={filter.intent||''} onChange={e=>set('intent',e.target.value)}><option value="">全部意向</option>{Object.keys(intents).map(k=><option key={k} value={k}>{intentLabel(k)}</option>)}</select>{supervisor&&<select aria-label="业务负责人" value={filter.ownerId||''} onChange={e=>set('ownerId',e.target.value)}><option value="">全部负责人</option>{employees.data?.map(e=><option key={e.id} value={e.id}>{e.name}{!e.active?'（停用）':''}</option>)}</select>}{!tasks&&!pool&&<select aria-label="客户范围" value={filter.queue||'all'} onChange={e=>set('queue',e.target.value)}>{scopeQueues.filter(q=>q!=='unassigned'||supervisor).map(q=><option key={q} value={q}>{q==='all'?'全部范围':queueNames[q]}</option>)}</select>}<div className="sx-filters-export"><button disabled={exporting} onClick={()=>exportFile('customers')}>导出客户</button><button disabled={exporting} onClick={()=>exportFile('followups')}>导出跟进</button></div></div>}
 <Feedback error={error||actionError} retry={reload}/>{loading?<div className="crm-loading">正在读取客户…</div>:data?.list.length?<><div className="sx-customer-list" role="list"><div className="sx-customer-head" aria-hidden><span>客户</span><span>意向 / 阶段</span><span>需求</span><span>负责人 / 最近跟进</span><span/></div>{data.list.map(c=>{const inPool=pool||hasRecordedPool(c);return <button role="listitem" className="sx-customer-row" key={c.id} onClick={()=>open(c.id,pool?{customerId:c.id,version:c.version}:undefined)}>
  <span className="sx-c-name"><Avatar customer={c}/><strong>{customerName(c)}</strong></span>
  <span className="sx-c-tags"><span className={'crm-intent '+c.intent}>{intentLabel(c.intent)}</span><Badge stage={c.stage}/></span>
  <span className="sx-c-demand">{c.demand||'待补充客户需求'}</span>
  <span className="sx-c-meta"><span className="sx-c-owner">{customerOwner(c,employees.data||[],account)}</span><span className={'sx-c-recent'+(c.nextFollowupAt&&new Date(c.nextFollowupAt)<new Date()?' crm-red':'')}>{inPool?'认领后可跟进':c.nextFollowupAt&&new Date(c.nextFollowupAt)<new Date()?'已超时 · ':''}{!inPool&&timeAgo(c.lastFollowupAt)}</span></span>
  <FiChevronRight className="sx-c-arrow" aria-hidden/>
 </button>})}</div><footer className="crm-pagination"><span>共 {data.total} 位客户</span><div><button disabled={(filter.page||1)<=1} onClick={()=>setFilter(f=>({...f,page:(f.page||1)-1}))}>上一页</button><span>第 {filter.page||1} 页</span><button disabled={(filter.page||1)*20>=data.total} onClick={()=>setFilter(f=>({...f,page:(f.page||1)+1}))}>下一页</button></div></footer></>:!error&&<Empty text={pool?'暂无可认领的公海客户':'暂无符合条件的客户'} report={pool||tasks?undefined:report}/>}
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
 <div className="crm-customer-heading"><Avatar customer={c}/><div><h2>{customerName(c)}</h2><p>{(pool?(c.phone?maskPhone(c.phone)+'（认领后显示完整电话）':''):c.phone)||'手机号待补充'} · {c.category}</p><Badge stage={c.stage}/> <span className={'crm-intent '+c.intent}>{intentLabel(c.intent)}</span></div></div>
 {mode==='report'?<CustomerReportForm key={c.id} customer={c} cancel={()=>changeMode('detail')} saved={saved} beginMutation={beginMutation}/>:mode!=='detail'?<CustomerForm key={c.id+mode} files={resource.data?.attachments||[]} customer={c} mode={mode} cancel={()=>changeMode('detail')} saved={saved} beginMutation={beginMutation}/>:<>
  {c.lifecycle==='protected'&&<div className="crm-note">{pool?'此客户在公海，认领后可跟进。':'保护至 '+dateTime(c.protectionExpiresAt)}</div>}
  <div className="crm-actions crm-detail-actions">{pool?<button className="crm-primary" disabled={claiming||mutationPending} onClick={claim}>{claiming?'正在认领…':'重新报备并认领'}</button>:<><button disabled={mutationPending} onClick={()=>changeMode('report')}>{c.lifecycle==='pending'?'完善报备资料':'编辑报备资料'}</button><button disabled={mutationPending} onClick={()=>changeMode('edit')}>编辑资料</button><button className="crm-primary" disabled={mutationPending} onClick={()=>changeMode('follow')}>记录跟进</button></>}</div>
  <p className="crm-caption">到期状态未同步时，可到公海核对。<button disabled={mutationPending} onClick={inspectPool}>查看客户公海核对状态</button></p>
  {c.customerNumber&&<dl className="crm-profile"><dt>客户编号</dt><dd className="crm-customer-number">{c.customerNumber}</dd><dt>微信</dt><dd>{c.wechat||'未填写'}</dd><dt>镇村地址</dt><dd>{c.address}</dd><dt>面积 / 需求</dt><dd>{c.buildingArea} ㎡ · {c.buildingType}</dd><dt>预计建房</dt><dd>{c.expectedBuildTime}</dd><dt>其他补充</dt><dd>{c.remark||'无'}</dd></dl>}
  <dl className="crm-profile"><dt>业务负责人</dt><dd>{customerOwner(c,employees.data||[],account)}</dd><dt>添加时间</dt><dd>{c.addedTimeSource==='unknown'?'待核实':dateTime(c.createdAt)}</dd><dt>下次跟进</dt><dd>{dateTime(c.nextFollowupAt)}</dd><dt>最近跟进</dt><dd>{c.lastFollowupAt?dateTime(c.lastFollowupAt):'尚未跟进'}</dd></dl>
  <h3>需求摘要</h3><p className="crm-demand">{c.demand||'待补充客户需求'}</p>
  <h3>企微好友关系</h3>{resource.data?.relations.length?resource.data.relations.map(r=><div className="crm-relation" key={r.eId}><span>{name(r.userId)}</span><span className={'crm-badge '+(r.status===0?'won':'paused')}>{({0:'正常',1:'客户已删除员工',2:'员工已删除客户'} as Record<number,string>)[r.status]||'关系待核实'}</span></div>):<p className="crm-caption">{c.customerNumber?'手动报备客户，尚未关联企微好友':'好友关系待同步'}</p>}
  <p className="crm-caption">负责人调整由主管在「分配 / 交接」处理。</p>
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
 {mode==='edit'?<><label>客户昵称<input disabled={busy||operation.current.completed} value={c.name} placeholder="未填写" readOnly/><small>{c.customerNumber?'在“编辑报备资料”中修改姓名与联系电话':'由企业微信同步维护'}</small></label><label>联系电话<input disabled={busy||operation.current.completed} readOnly={!!c.customerNumber} value={form.phone} maxLength={80} onChange={e=>field('phone',e.target.value)} placeholder="可稍后补充"/></label><label>咨询类型<select disabled={busy||operation.current.completed} value={form.category} onChange={e=>field('category',e.target.value)}>{categories.map(x=><option key={x}>{x}</option>)}</select></label><label>意向程度<select disabled={busy||operation.current.completed} value={form.intent} onChange={e=>field('intent',e.target.value)}>{Object.keys(intents).map(k=><option key={k} value={k}>{intentLabel(k)}</option>)}</select></label><label>需求摘要<textarea disabled={busy||operation.current.completed} rows={5} value={form.demand} maxLength={2000} onChange={e=>field('demand',e.target.value)}/></label></>:<><label>跟进类型<select disabled={busy||operation.current.completed} value={form.kind} onChange={e=>field('kind',e.target.value)}>{['followup','communication','drawing','quotation'].map(k=><option key={k} value={k}>{activityNames[k]}</option>)}</select></label><label>联系结果<select disabled={busy||operation.current.completed} value={form.outcome} onChange={e=>field('outcome',e.target.value)}><option value="contacted">有效联系 / 已完成</option><option value="no_answer">电话未接通</option><option value="no_reply">消息未回复</option><option value="internal">内部安排</option></select><small>未接通、未回复和内部安排只记记录，不延长保护。</small></label><Evidence files={files} selected={evidence} change={setEvidence}/><label><span>跟进内容 <b className="crm-red">*</b></span><textarea disabled={busy||operation.current.completed} autoFocus required rows={6} value={form.content} maxLength={2000} onChange={e=>field('content',e.target.value)} placeholder="例如：电话沟通，已确认两层建房需求，待补充宅基地照片。"/><small>{form.content.length}/2000</small></label><label>客户阶段<select disabled={busy||operation.current.completed} value={form.stage} onChange={e=>field('stage',e.target.value)}>{Object.entries(stages).filter(([k])=>!['won','invalid'].includes(k)||k===c.stage).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label>{active?<label><span>下次联系时间 <b className="crm-red">*</b></span><input disabled={busy||operation.current.completed} type="datetime-local" required value={form.next} onChange={e=>field('next',e.target.value)}/><small>按当前设备时区填写；保存后统一按北京时间展示。</small></label>:<div className="crm-note">暂缓会结束当前待办，但仍受保护到期规则约束。成交及无效归档请提交主管申请。</div>}</>}
 <footer className="crm-form-footer"><button type="button" disabled={busy} onClick={cancel}>取消</button><button type="submit" className="crm-primary" disabled={busy||conflict||operation.current.completed}>{busy?'正在保存…':mode==='edit'?'保存资料':'保存跟进'}</button></footer>
 </form>;
}
