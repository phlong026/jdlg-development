// @ts-nocheck
const {request}=require('../../utils/phase3-api');
const ROOT='/api/v1/miniprogram/notifications';
const BINDING='/api/v1/miniprogram/official-account/binding';
Page({
 data:{loading:false,messages:[],cursor:'',unread:0,error:'',loginRequired:false,bindingLabel:'正在检查提醒状态',canBind:false,bindingBusy:false},
 onLoad(options){this._pendingNotice=String(options.notification_id||'');},
 onShow(){
  wx.hideShareMenu();this._epoch=(this._epoch||0)+1;this._token=String(wx.getStorageSync('token')||'');
  this.setData({messages:[],cursor:'',unread:0,error:'',loading:false,loginRequired:!this._token,canBind:false});
  if(!this._token){this.setData({bindingLabel:'登录后查看浏览提醒'});return;}
  this.loadMessages(false);this.loadBinding();
 },
 onUnload(){this._epoch=(this._epoch||0)+1;},
 current(epoch){return epoch===this._epoch&&this._token===String(wx.getStorageSync('token')||'');},
 async loadMessages(more){
  if(this.data.loading||!this._token)return;const epoch=this._epoch;this.setData({loading:true,error:''});
  try{
   const page=await request(ROOT,'GET',{}, {limit:20,cursor:more?this.data.cursor:''});
   if(!this.current(epoch))return;
   const rows=Array.isArray(page.items)?page.items:[];
   const items=rows.map(row=>({...row,timeLabel:String(row.created_at||'').replace('T',' ').slice(0,16)}));
   const seen=new Set();const messages=(more?this.data.messages.concat(items):items).filter(row=>!seen.has(row.notification_id)&&seen.add(row.notification_id));
   this.setData({messages,cursor:page.next_cursor||''});
   await this.loadUnread(epoch);
   if(this._pendingNotice){const id=this._pendingNotice;this._pendingNotice='';await this.onOpenMessage({currentTarget:{dataset:{id}}});}
  }catch(error){if(this.current(epoch))this.setData({error:error.statusCode===401?'登录已失效，请重新登录':'消息暂时无法加载，请重试'});}
  finally{if(this.current(epoch))this.setData({loading:false});}
 },
 async loadUnread(epoch){try{const result=await request(ROOT+'/unread-count');if(this.current(epoch))this.setData({unread:Number(result.unread_count)||0});}catch(error){if(this.current(epoch))this.setData({error:'未读状态暂时无法更新，请刷新'});}},
 async loadBinding(){const epoch=this._epoch;try{const status=await request(BINDING);if(!this.current(epoch))return;
  const available=!!(status.capability&&status.capability.binding_available);
  this.setData({canBind:available&&!(status.bound&&status.subscribed),bindingLabel:status.bound&&status.subscribed?'已连接华匠邦建科浏览提醒':available?'连接华匠邦建科，离开小程序也能接收提醒':'华匠邦建科浏览提醒暂未开通'});
 }catch(error){if(this.current(epoch))this.setData({bindingLabel:'服务号提醒状态暂时无法读取',canBind:false});}},
 onRetry(){this.loadMessages(false);},onLoadMore(){if(this.data.cursor)this.loadMessages(true);},
 onLogin(){wx.navigateTo({url:'/pages/login/login'});},
 async onOpenMessage(event){const id=String(event.currentTarget.dataset.id||'');if(!id||!this._token)return;const epoch=this._epoch;
  try{const item=await request(ROOT+'/'+encodeURIComponent(id));if(!this.current(epoch))return;
   await request(ROOT+'/'+encodeURIComponent(id)+'/read','PATCH');if(!this.current(epoch))return;
   this.setData({messages:this.data.messages.map(row=>row.notification_id===id?{...row,read_at:row.read_at||new Date().toISOString()}:row)});await this.loadUnread(epoch);if(!this.current(epoch))return;
   if(item.cancelled_at){wx.showToast({title:'这条提醒已失效',icon:'none'});return;}
   if(item.share_id)wx.navigateTo({url:'/pages/visitdetail/visitdetail?share_id='+encodeURIComponent(item.share_id)});
   else wx.showModal({title:item.title||'浏览提醒',content:item.body||'有人浏览了你分享的内容',showCancel:false});
  }catch(error){if(this.current(epoch))wx.showToast({title:'消息不可查看，请刷新后重试',icon:'none'});}
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
 }
});
export {};
