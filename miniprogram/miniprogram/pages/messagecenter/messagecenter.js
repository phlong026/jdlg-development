const {request}=require('../../utils/phase3-api');
const ROOT='/api/v1/miniprogram/notifications';
const BINDING='/api/v1/miniprogram/official-account/binding';
function privateProjectNoteRoute(item){const route=String(item&&item.route_key||'');if(item&&item.kind==='project_note_join_request')return /^pages\/projectnotes\/projectnotes\?project_id=[A-Za-z0-9_-]{8,128}&view=members$/.test(route)?'/'+route:'';if(item&&item.kind==='project_note_feedback'){const id=/^[A-Za-z0-9_-]{8,128}$/;const version=String(item.content_id||''),notice=String(item.notification_id||''),feedback=notice.startsWith('feedback:')?notice.slice(9):'';return /^pages\/projectnotes\/projectnotes\?project_id=[A-Za-z0-9_-]{8,128}$/.test(route)&&id.test(version)&&id.test(feedback)?'/'+route+'&version_id='+encodeURIComponent(version)+'&feedback_id='+encodeURIComponent(feedback):'';}return /^pages\/projectnotes\/projectnotes\?project_id=[A-Za-z0-9_-]{8,128}&version_id=[A-Za-z0-9_-]{8,128}$/.test(route)?'/'+route:'';}
function safeFallbackRoute(item){const route=String(item&&item.route_key||'');if(item&&(item.kind==='collection_directory_browse'||item.kind==='collection_content_browse')&&/^pages\/collectionreader\/collectionreader\?collection_id=[A-Za-z0-9_-]{8,128}&collection_version=\d+$/.test(route))return '/'+route;return '';}
function localDay(date){return new Date(date.getFullYear(),date.getMonth(),date.getDate()).getTime();}
function presentMessages(rows){const today=localDay(new Date()),day=24*60*60*1000;let previous='';return rows.map(row=>{const created=new Date(row.created_at||''),valid=!Number.isNaN(created.getTime()),stamp=valid?localDay(created):0,diff=valid?Math.round((today-stamp)/day):-1;const group=diff===0?'今天':diff===1?'昨天':'更早';const pad=value=>String(value).padStart(2,'0');const clock=valid?pad(created.getHours())+':'+pad(created.getMinutes()):'';const timeLabel=valid&&diff>1?(created.getMonth()+1)+'月'+created.getDate()+'日 '+clock:clock;const groupLabel=group===previous?'':group;previous=group;return {...row,...messagePresentation(row.kind),groupLabel,timeLabel};});}
function messagePresentation(kind){
 if(kind==='project_note_join_request')return {categoryLabel:'项目申请',actionLabel:'处理申请'};
 if(kind==='collection_directory_browse')return {categoryLabel:'合集访问',actionLabel:'查看访问'};
 if(kind==='collection_content_browse')return {categoryLabel:'作品阅读',actionLabel:'查看访问'};
 if(kind==='project_note_feedback')return {categoryLabel:'项目反馈',actionLabel:'查看反馈'};
 if(kind==='project_package_receipt')return {categoryLabel:'阅读回执',actionLabel:'查看方案'};
 if(kind==='project_note_open')return {categoryLabel:'项目动态',actionLabel:'查看方案'};
 if(kind==='browse')return {categoryLabel:'访问提醒',actionLabel:'查看访问'};
 return {categoryLabel:'消息',actionLabel:'查看详情'};
}
Page({
 data:{loading:false,messages:[],cursor:'',unread:0,error:'',unreadError:'',loginRequired:false,settingsOpen:false,settingsLoading:false,settingsError:'',bindingLabel:'提醒状态待读取',bindingError:'',canBind:false,bindingBusy:false,prefsVisible:true,prefsLoaded:false,prefsBusy:false,browseDelivery:true,projectDelivery:true},
 onLoad(options){this._pendingNotice=String(options.notification_id||'');},
 onShow(){
  wx.hideShareMenu();this._epoch=(this._epoch||0)+1;this._token=String(wx.getStorageSync('token')||'');
  this.setData({messages:[],cursor:'',unread:0,error:'',unreadError:'',loading:false,loginRequired:!this._token,settingsOpen:false,settingsLoading:false,settingsError:'',bindingLabel:'提醒状态待读取',bindingError:'',canBind:false,prefsLoaded:false,prefsBusy:false,bindingBusy:false});
  if(!this._token)return;
  this.loadMessages(false);
 },
 onUnload(){this._epoch=(this._epoch||0)+1;},
 current(epoch){return epoch===this._epoch&&this._token===String(wx.getStorageSync('token')||'');},
 async loadMessages(more){
  if(this.data.loading||!this._token)return;const epoch=this._epoch;this.setData({loading:true,error:''});
  try{
   const page=await request(ROOT,'GET',{}, {limit:20,cursor:more?this.data.cursor:''});
   if(!this.current(epoch))return;
   const rows=Array.isArray(page.items)?page.items:[];
   const seen=new Set();const merged=(more?this.data.messages.concat(rows):rows).filter(row=>!seen.has(row.notification_id)&&seen.add(row.notification_id));const messages=presentMessages(merged);
   this.setData({messages,cursor:page.next_cursor||''});
   await this.loadUnread(epoch);
   if(this._pendingNotice){const id=this._pendingNotice;this._pendingNotice='';await this.onOpenMessage({currentTarget:{dataset:{id}}});}
  }catch(error){if(this.current(epoch))this.setData({error:error.statusCode===401?'登录已失效，请重新登录':'消息暂时无法加载，请重试'});}
  finally{if(this.current(epoch))this.setData({loading:false});}
 },
 async loadUnread(epoch){try{const result=await request(ROOT+'/unread-count');if(this.current(epoch))this.setData({unread:Number(result.unread_count)||0,unreadError:''});}catch(error){if(this.current(epoch))this.setData({unreadError:'未读数暂时无法更新'});}},
 async loadBinding(){const epoch=this._epoch;try{const status=await request(BINDING);if(!this.current(epoch))return;
  const capability=status.capability||{};const available=!!capability.binding_available;const connected=!!(status.bound&&status.subscribed);const deliverable=!!capability.delivery_available;
  this.setData({canBind:available&&!connected,bindingError:'',bindingLabel:connected?(deliverable?'已连接，可接收提醒':'已连接，提醒暂不可用'):available?'可选连接服务号提醒':'服务号提醒未开启'});
 }catch(error){if(this.current(epoch))this.setData({bindingLabel:'提醒状态读取失败',bindingError:'提醒状态暂时无法读取',canBind:false});}},
 onRetry(){this.loadMessages(false);},onLoadMore(){if(this.data.cursor)this.loadMessages(true);},
 onLogin(){wx.navigateTo({url:'/pages/login/login'});},
 async onOpenMessage(event){const id=String(event.currentTarget.dataset.id||'');if(!id||!this._token)return;const epoch=this._epoch;
  let item;try{item=await request(ROOT+'/'+encodeURIComponent(id));if(!this.current(epoch))return;}catch(error){if(this.current(epoch))wx.showToast({title:'消息不可查看，请刷新后重试',icon:'none'});return;}
  try{await request(ROOT+'/'+encodeURIComponent(id)+'/read','PATCH');if(this.current(epoch)){this.setData({messages:this.data.messages.map(row=>row.notification_id===id?{...row,read_at:row.read_at||new Date().toISOString()}:row)});await this.loadUnread(epoch);}}catch(error){if(this.current(epoch))this.setData({unreadError:'已读状态暂时无法更新'});}
  if(!this.current(epoch))return;if(item.cancelled_at){wx.showToast({title:'这条提醒已失效',icon:'none'});return;}
  const privateRoute=privateProjectNoteRoute(item),fallback=safeFallbackRoute(item);
  if(item.kind==='project_note_open'||item.kind==='project_note_feedback'||item.kind==='project_package_receipt'||item.kind==='project_note_join_request'){if(privateRoute)wx.navigateTo({url:privateRoute});else wx.showToast({title:'私人项目提醒已失效',icon:'none'});}
  else if(item.share_id)wx.navigateTo({url:'/pages/visitdetail/visitdetail?share_id='+encodeURIComponent(item.share_id)});
  else if(fallback)wx.navigateTo({url:fallback});
  else wx.showModal({title:item.title||'浏览提醒',content:item.body||'有人浏览了你分享的内容',showCancel:false});
 },
 async onReadAll(){if(!this._token||this.data.loading)return;const epoch=this._epoch;
  try{await request(ROOT+'/read-all','PATCH');if(!this.current(epoch))return;this.setData({unread:0,messages:this.data.messages.map(row=>({...row,read_at:row.read_at||new Date().toISOString()}))});}
  catch(error){if(this.current(epoch))wx.showToast({title:'未能更新已读，请重试',icon:'none'});}
 },
 async onBind(){if(!this.data.canBind||this.data.bindingBusy)return;const epoch=this._epoch;this.setData({bindingBusy:true});
  try{const result=await request(BINDING,'POST');if(!this.current(epoch))return;const url=String(result.authorization_url||'');
   if(!url.startsWith('https://open.weixin.qq.com/connect/oauth2/authorize?'))throw Error('invalid authorization');
   wx.setClipboardData({data:url,success:()=>{if(this.current(epoch))wx.showModal({title:'连接浏览提醒',content:'授权链接已复制，请在微信中打开并完成连接，然后返回这里刷新状态。',showCancel:false});}});
  }catch(error){if(this.current(epoch))wx.showToast({title:'暂时无法连接提醒，请稍后重试',icon:'none'});}
  finally{if(this.current(epoch))this.setData({bindingBusy:false});}
 },
 async toggleSettings(){if(this.data.settingsOpen){this.setData({settingsOpen:false});return;}const epoch=this._epoch;this.setData({settingsOpen:true,settingsLoading:true,settingsError:'',bindingError:'',prefsLoaded:false});await Promise.all([this.loadBinding(),this.loadPrefs()]);if(this.current(epoch))this.setData({settingsLoading:false});},
 // 分类外发开关：只控制服务号推送，消息中心始终保留全部提醒。
 async loadPrefs(){const epoch=this._epoch;
  try{const prefs=await request(ROOT+'/preferences');if(!this.current(epoch))return;
   if(typeof prefs.browse_delivery!=='boolean'||typeof prefs.project_delivery!=='boolean')throw Error('invalid preferences');
   this.setData({prefsVisible:true,prefsLoaded:true,browseDelivery:prefs.browse_delivery,projectDelivery:prefs.project_delivery});
  }catch(error){if(this.current(epoch))this.setData({prefsVisible:true,prefsLoaded:false,settingsError:'提醒设置暂时无法读取，请重试'});}
 },
 async savePrefs(browse,project){const epoch=this._epoch;this.setData({prefsBusy:true});
  try{await request(ROOT+'/preferences','PUT',{browse_delivery:browse,project_delivery:project});if(!this.current(epoch))return;
   this.setData({browseDelivery:browse,projectDelivery:project,prefsBusy:false});
  }catch(error){if(this.current(epoch)){this.setData({prefsBusy:false,browseDelivery:this.data.browseDelivery,projectDelivery:this.data.projectDelivery});wx.showToast({title:'设置未保存，请重试',icon:'none'});}}
 },
 onToggleBrowse(event){if(this.data.prefsBusy)return;this.savePrefs(!!event.detail.value,this.data.projectDelivery);},
 onToggleProject(event){if(this.data.prefsBusy)return;this.savePrefs(this.data.browseDelivery,!!event.detail.value);}
});
