import React from 'react';
import {Navigate,useLocation} from 'react-router-dom';
import {useManagementSession} from './ManagementSession';

export default function ProtectedRoute({children}:{children:React.ReactNode}) {
 const session=useManagementSession();const location=useLocation();
 if(session.loading)return <div className="route-loading">正在核对账号权限…</div>;
 if(session.error)return <div className="route-loading"><div><p>{session.error}</p><button onClick={session.refresh}>重新检查</button> <a href="/admin/login">返回登录</a></div></div>;
 if(!session.user)return <Navigate to="/login" replace/>;
 if(session.user.scope==='crm_only'&&location.pathname!=='/customer-management')return <Navigate to="/customer-management" replace/>;
 return <React.Fragment key={session.user.id}>{children}</React.Fragment>;
}
