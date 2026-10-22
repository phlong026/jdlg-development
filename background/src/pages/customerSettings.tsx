import React from 'react';
import {Navigate,useSearchParams} from 'react-router-dom';
import {useManagementSession} from '../component/ManagementSession';
import {SyncPanel,AccessPanel,BindingCheck} from './customerAdminPanels';
import {PushStatus} from './customerWorkflow';
import PageTitle from '../component/PageTitle';
import './customerManagement.scss';
import './customerResponsive.scss';
import './salesModule.scss';
import './customerSettings.scss';

const items=[{key:'sync',label:'企微同步与接入'},{key:'access',label:'账号与权限'},{key:'binding',label:'归属绑定检查'}];

/** 客户系统设置：只给平台管理员。其余账号（含客户主管、未知 scope）一律重定向，不渲染任何设置内容（fail-closed）。 */
export default function CustomerSettings(){
 const session=useManagementSession();const [params,setParams]=useSearchParams();
 if(session.user?.scope!=='platform_admin')return <Navigate to="/" replace/>;
 const active=items.find(i=>i.key===params.get('item'))?.key||'sync';
 const choose=(key:string)=>setParams({item:key});
 return <div className="crm-page sx-settings">
  <PageTitle title="客户系统设置" className="sx-page-head" />
  <div className="sx-settings-layout">
   <nav className="sx-settings-nav" aria-label="客户系统设置">{items.map(i=><button key={i.key} className={active===i.key?'active':''} aria-current={active===i.key?'page':undefined} onClick={()=>choose(i.key)}>{i.label}</button>)}</nav>
   <div className="sx-settings-body">
    {active==='sync'&&<><SyncPanel/><PushStatus/></>}
    {active==='access'&&<AccessPanel/>}
    {active==='binding'&&<BindingCheck goAccess={()=>choose('access')}/>}
   </div>
  </div>
 </div>;
}
