import React,{useEffect,useState} from 'react';
import {FiAlertCircle} from 'react-icons/fi';
import {crm,type Filter} from '../api/wecomCustomers';

export const errText=(e:unknown)=>e instanceof Error?e.message:'操作失败，请重试';
export function useResource<T>(path:string,query?:Filter,poll=false){
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
export function Feedback({error,retry}:{error:string;retry?:()=>void}){return error?<div className="crm-feedback" role="alert"><FiAlertCircle/><span>{error}</span>{retry&&<button onClick={retry}>重试</button>}</div>:null}
