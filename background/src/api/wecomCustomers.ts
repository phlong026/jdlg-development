import { API_BASE_URL } from '../config/api';
const BASE = '/api/v1/management/crm';
export type Stage = 'new'|'following'|'won'|'paused'|'invalid';
export interface Customer { id:string; name:string; avatar:string; ownerId:string|null; stage:Stage; category:string; intent:string; phone:string; demand:string; createdAt:string; updatedAt:string; addedTimeSource:string; nextFollowupAt:string|null; lastFollowupAt:string|null; version:number; customerNumber:string|null;ownerUsername:string|null;wechat:string;address:string;buildingArea:number|null;buildingType:string;expectedBuildTime:string;remark:string;protectionExpiresAt:string|null;poolAt:string|null; lifecycle:string; historicalReviewed:boolean; mergedInto:string|null }
export const inPool=(c:Customer)=>!!c.poolAt||!!(c.protectionExpiresAt&&new Date(c.protectionExpiresAt)<=new Date());
export const activityNames:Record<string,string>={source_history:'小程序历史（不计续期）',followup:'跟进',communication:'沟通',drawing:'出图',quotation:'报价',profile:'资料调整',report:'首次报备',release:'进入公海',claim:'重新认领'};
export interface Employee { id:string; name:string; active:boolean }
export interface Account { username:string; displayName:string; employeeId:string|null; role:'admin'|'staff'; enabled:boolean; managementUserId?:number }
export interface Context {account:Account; demo:boolean}
export interface Followup {id:number; content:string; kind:string; operatorId:string; stage:Stage; createdAt:string; nextFollowupAt:string|null;effective:boolean;outcome:string;reviewReason:string;attachmentIds:string}
export interface Relation {eId:string; userId:string; addTime:string; status:number; customerName:string}
export interface Detail {customer:Customer; relations:Relation[]; followups:Followup[];sources:Source[];attachments:Attachment[];cases:ReviewCase[]}
export interface CustomerPage {list:Customer[]; total:number; page_size:number}
export interface Filter { source?:string; keyword?:string; category?:string; intent?:string; stage?:string; ownerId?:string; queue?:string; page?:number; page_size?:number }
export interface Dashboard {overview:Record<string,number>; todayAdded:number; unknownAddedTime:number; recent:Customer[]; team:{ownerId:string;name:string;total:number;today:number;overdue:number;unplanned:number}[]; asOf:string}
export interface SyncRun {id:number; status:string; processed:number; error:string|null; startedAt:string; finishedAt:string|null}
export interface SyncState {configured:boolean;callbackConfigured:boolean;runs:SyncRun[];pending:number;failed:number;events:{id:string;changeType:string;status:string;attempts:number;error:string;createdAt:string}[]}
export class CRMError extends Error {constructor(public status:number,message:string){super(message)}}
export async function crm<T>(path:string,options:{method?:string;body?:unknown;query?:Filter;signal?:AbortSignal;download?:boolean}={}):Promise<T> {
 const query=new URLSearchParams();Object.entries(options.query||{}).forEach(([k,v])=>{if(v!==undefined&&v!=='')query.set(k,String(v))});
 const response=await fetch(API_BASE_URL+BASE+path+(query.size?'?'+query:''),{method:options.method||'GET',credentials:'include',signal:options.signal,headers:{'Content-Type':'application/json','X-CRM-Request':'1'},body:options.body===undefined?undefined:JSON.stringify(options.body)});
 if(response.status===401){window.dispatchEvent(new Event('management-session-expired'));throw new CRMError(401,'登录已失效，请重新登录')}
 if(options.download&&response.ok)return await response.blob() as T;
 let payload:any;try{payload=await response.json()}catch{throw new CRMError(response.status,'服务响应异常，请稍后重试')}
 if(!response.ok||(payload.code!==0&&payload.code!==200))throw new CRMError(response.status,payload.msg||payload.message||'操作失败');
 return payload.data as T;
}
export async function downloadCustomers(kind:'customers'|'followups',query:Filter){
 const {page,page_size,...filter}=query;const blob=await crm<Blob>('/export/'+kind,{query:filter,download:true});
 const url=URL.createObjectURL(blob);const link=document.createElement('a');link.href=url;link.download=(kind==='customers'?'企微客户':'企微跟进')+'.csv';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
}
export const stages:Record<Stage,string>={new:'新客户',following:'跟进中',won:'已成交',paused:'暂缓',invalid:'无效'};
export const categories=['待确认','建房咨询','设计咨询','造价咨询','合作咨询'];
export const intents:Record<string,string>={high:'高',medium:'中',low:'低'};
export const dateTime=(s?:string|null)=>s?new Date(s).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}):'未安排';

export interface Source {id:string;customerId:string;originCustomerId:string;kind:string;externalId:string;name:string;phone:string;wechat:string;summary:string;submittedBy:string;channel:string;createdAt:string}
export interface Attachment {id:string;name:string;size:number;createdAt:string}
export interface ReviewCase {id:number;customerId:string;kind:string;status:string;content:string;applicant:string;decision:string;attachmentIds:string}
export const lifecycleNames:Record<string,string>={pending:"待完善报备",legacy:"历史客户待核实",protected:"专属保护",pool:"客户公海",won:"成交已确认",archived:"已归档"};
export const sourceNames:Record<string,string>={manual:"手动报备",wecom:"企业微信",miniprogram:"小程序咨询",archive:"原始档案"};
