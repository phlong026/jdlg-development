export interface Account {username:string;displayName:string;role:string;employeeId:string;enabled:boolean}
export interface Employee {id:string;name:string;active:boolean}
export interface Customer {id:string;name:string;avatar:string;ownerId:string|null;stage:string;category:string;intent:string;phone:string;demand:string;createdAt:string;updatedAt:string;lastFollowupAt:string|null;nextFollowupAt:string|null;version:number}
export interface Followup {id:number;operatorId:string;content:string;kind:string;createdAt:string;stage:string;nextFollowupAt:string|null}
export interface Relation {userId:string;status:number;addTime:string;state:string}
export interface Detail {customer:Customer;relations:Relation[];followups:Followup[]}
export async function api<T>(path:string,method='GET',body?:unknown):Promise<T> {
 const response=await fetch('/api'+path,{method,headers:{'Content-Type':'application/json','X-CRM-Request':'1'},body:body===undefined?undefined:JSON.stringify(body)})
 if(!response.ok) {
  const error=await response.json().catch(()=>({message:'服务暂时不可用'}))
  if(response.status===401 && path!='/auth/login') window.dispatchEvent(new Event('crm-unauthorized'))
  throw new Error(error.message||'操作失败，请重试')
 }
 const text=await response.text();return text?JSON.parse(text):undefined
}
export async function download(path:string,name:string) {
 const res=await fetch('/api'+path)
 if(!res.ok) {const error=await res.json();throw new Error(error.message||'导出失败')}
 const url=URL.createObjectURL(await res.blob());const a=document.createElement('a');a.href=url;a.download=name;a.click();URL.revokeObjectURL(url)
}

