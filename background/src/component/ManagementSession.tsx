import React,{createContext,useContext,useEffect,useRef,useState} from 'react';
import {getMe,type LoginResponse} from '../api/auth';
import {crm,type Context} from '../api/wecomCustomers';
import {tokenUtils} from '../utils/token';
import {resetLeadOverviewSession} from './leadOverviewStore';
interface State {user:LoginResponse|null;crmContext:Context|null;crmError:string;crmLoading?:boolean;loading:boolean;error:string;refresh:()=>void}
const Session=createContext<State>({user:null,crmContext:null,crmError:'',loading:true,error:'',refresh:()=>{}});
export const useManagementSession=()=>useContext(Session);
export function ManagementSessionProvider({children}:{children:React.ReactNode}) {
 const [state,setState]=useState<Omit<State,'refresh'>>({user:null,crmContext:null,crmError:'',loading:true,error:''});const seq=useRef(0);
 const refresh=async()=>{
  const id=++seq.current;resetLeadOverviewSession();
  setState({user:null,crmContext:null,crmError:'',loading:true,error:''});
  try {
   const user=await getMe();let crmContext:Context|null=null,crmError='';
   if(seq.current!==id)return;
   tokenUtils.setToken('session_active');resetLeadOverviewSession(user.id);
   setState({user,crmContext:null,crmError:'',crmLoading:!!user.crmEnabled,loading:false,error:''});
   if(user.crmEnabled){try{crmContext=await crm<Context>('/context')}catch(e){crmError=e instanceof Error?e.message:'客户账号不可用'}}
   if(seq.current===id)setState({user,crmContext,crmError,loading:false,error:''});
  }catch(e){if(seq.current===id){resetLeadOverviewSession();tokenUtils.removeToken();setState({user:null,crmContext:null,crmError:'',loading:false,error:(e as {status?:number})?.status===401?'':e instanceof Error?e.message:'会话检查失败'})}}
 };
 useEffect(()=>{
  refresh();const expire=()=>{++seq.current;resetLeadOverviewSession();tokenUtils.removeToken();setState({user:null,crmContext:null,crmError:'',loading:false,error:''})};
  window.addEventListener('management-session-changed',refresh);window.addEventListener('management-session-expired',expire);
  return()=>{++seq.current;resetLeadOverviewSession();window.removeEventListener('management-session-changed',refresh);window.removeEventListener('management-session-expired',expire)};
 },[]);
 return <Session.Provider value={{...state,refresh}}>{children}</Session.Provider>;
}
