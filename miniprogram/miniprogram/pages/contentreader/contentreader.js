'use strict';
const tracking=require('../../utils/tracking/index');
const api=require('../../utils/tracking/api');
const {createVideoTracker}=require('../../utils/tracking/video');
const ROUTE='pages/contentreader/contentreader';
Page({
 data:{loading:true,error:'',content:null,contentReady:false,media:[],consentNotice:'',consentAvailable:false,consented:false,mediaErrors:{},sourceOpen:false},
 onLoad(options){this.entryGeneration=1;this.entryAttributionAllowed=true;this.options=options||{};this.players={};this.playerVisible={};this.observers=[];this.loadContent();},
 onShow(){this.pageVisible=true;tracking.block(this,false);if(this.hasShown)this.loadContent();this.hasShown=true;this.syncPlayerVisibility();},
 onHide(){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=false;this.pageVisible=false;this.syncPlayerVisibility();this.pausePlayers();},
 onUnload(){this.loadSequence=(this.loadSequence||0)+1;this.pageVisible=false;this.pausePlayers();this.observers.forEach(o=>o.disconnect());this.observers=[];},
 acceptExternalEntry(options){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=true;this.options=options||{};this.setData({content:null,contentReady:false,loading:true});this.loadContent();},
 async loadContent(){
  const sequence=(this.loadSequence||0)+1;this.loadSequence=sequence;
  const entryGeneration=this.entryGeneration,entryValid=tracking.captureEntryValidity?tracking.captureEntryValidity():()=>true;
  this.pausePlayers();if(wx.hideShareMenu)wx.hideShareMenu();this.setData({loading:true,error:'',content:null,media:[],contentReady:false});
  try{
   const resolvedOptions=await api.resolveReaderOptions(this.options);
   if(sequence!==this.loadSequence)return;
   this.options={...resolvedOptions};
   if(entryGeneration!==this.entryGeneration||this.entryAttributionAllowed===false||!entryValid())delete this.options.st;
   tracking.resolvePageOptions(this,this.options,this.boundEntryGeneration!==entryGeneration);
   this.boundEntryGeneration=entryGeneration;
   // Only a newly observed external entry may supply attribution. Ordinary
   // foreground content revalidation must not replay a revoked/consumed token.
   this.options={...this.options};delete this.options.st;delete this.options.scene;
   const id=String(this.options.content_id||''),version=Number(this.options.content_version);
   if(!/^[A-Za-z0-9_-]{8,128}$/.test(id)||!Number.isInteger(version)||version<1)throw new Error('内容入口不完整');
   const configuration=await tracking.refreshConfig();
   if(sequence!==this.loadSequence)return;
   this.setData({consentNotice:configuration.privacy_notice||'',consentAvailable:configuration.enabled===true&&!!configuration.consent_version&&!!configuration.privacy_notice,consented:tracking.consented()});
   // Public published content is independently readable, even when analytics is disabled or declined.
   const content=await api.request('/contents/'+encodeURIComponent(id)+'/versions/'+version,'GET',{},undefined,false);
   if(sequence!==this.loadSequence)return;
   const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync(),width=Math.max(240,window.windowWidth-40);
   const media=(content.media||[]).map(asset=>({...asset,url:api.mediaURL(asset.url)})).filter(asset=>['image','video'].includes(asset.kind)&&/^https:\/\//.test(asset.url||'')).map((asset,index)=>({...asset,domId:'media-'+index,height:Math.max(120,Math.round(width*(asset.height||9)/(asset.width||16)))}));
   if(content.cover)content.cover={...content.cover,url:api.mediaURL(content.cover.url)};
   this.setData({content,media,loading:false,contentReady:true},()=>{this.observePlayers();if(this.pageVisible&&wx.showShareMenu)wx.showShareMenu({menus:['shareAppMessage','shareTimeline']});});
  }catch(error){if(sequence!==this.loadSequence)return;this.setData({loading:false,contentReady:false,error:'内容暂不可访问，可能已撤回、无权限或网络不可用。请重试。'});}
 },
 acceptTracking(){if(tracking.setConsent(true)){this.setData({consented:true});}},
 declineTracking(){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=false;tracking.setConsent(false);this.setData({consented:false});},
 observePlayers(){
  this.observers.forEach(o=>o.disconnect());this.observers=[];
  this.playerGeneration=(this.playerGeneration||0)+1;this.players={};this.playerVisible={};
  this.data.media.filter(m=>m.kind==='video').forEach(media=>{
   const player=createVideoTracker({mono:()=>wx.getPerformance().now(),emit:(type,extra)=>tracking.emit(this,type,extra)},media.media_id,media.domId+'-generation-'+this.playerGeneration);
   this.players[media.media_id]=player;
   // If intersection observation is unsupported, playback remains usable but no claimed watch time.
   if(this.createIntersectionObserver){const observer=this.createIntersectionObserver({thresholds:[0,.5,1]});observer.relativeToViewport().observe('#'+media.domId,rect=>{
    this.playerVisible[media.media_id]=rect.intersectionRatio>=.5;this.syncPlayerVisibility();
    if(!this.playerVisible[media.media_id]&&this.fullscreenMedia!==media.media_id)wx.createVideoContext(media.domId,this).pause();
   });this.observers.push(observer);}
  });
 },
 syncPlayerVisibility(){Object.keys(this.players||{}).forEach(id=>this.players[id].visible(!!this.pageVisible&&(!!this.playerVisible[id]||this.fullscreenMedia===id)&&!this.data.sourceOpen));},
 pausePlayers(){(this.data.media||[]).filter(m=>m.kind==='video').forEach(m=>{wx.createVideoContext(m.domId,this).pause();const p=this.players[m.media_id];if(p)p.state('paused');});},
 videoState(event){const id=event.currentTarget.dataset.mediaId,p=this.players[id];if(!p)return;
  const states={play:'playing',pause:'paused',ended:'ended',waiting:'buffering',error:'error'};
  p.state(states[event.type]||'paused');
  if(event.type==='error')this.setData({['mediaErrors.'+id]:'视频源不可播放，请稍后重试。'});
 },
 videoFullscreen(event){this.fullscreenMedia=event.detail.fullScreen?event.currentTarget.dataset.mediaId:'';this.syncPlayerVisibility();},
 videoProgress(event){const p=this.players[event.currentTarget.dataset.mediaId];if(p)p.progress(event.detail.currentTime,event.detail.duration);},
 videoRate(event){const p=this.players[event.currentTarget.dataset.mediaId];if(p)p.rate(event.detail.playbackRate);},
 openImage(event){const media=this.data.media.find(m=>m.media_id===event.currentTarget.dataset.mediaId);if(!media||media.kind!=='image')return;
  tracking.emit(this,'image_open',{media_id:media.media_id});tracking.block(this,true);this.pausePlayers();
  wx.previewImage({urls:[media.url],current:media.url,showmenu:false});
 },
 retryMedia(){this.setData({mediaErrors:{}});this.loadContent();},
 mediaError(event){this.setData({['mediaErrors.'+event.currentTarget.dataset.mediaId]:'图片加载失败，请重试。'});},
 copySource(){const url=this.data.content&&this.data.content.source_url;if(!url||!/^https:\/\//.test(url))return;
  tracking.emit(this,'outbound_click',{action_key:'copy_original_url'});this.pausePlayers();
  wx.setClipboardData({data:url,success:()=>{tracking.block(this,true);wx.showModal({title:'原文链接已复制',content:'请在浏览器中打开原文。原文中的阅读、滚动和播放不在本系统记录范围内。',showCancel:false,complete:()=>tracking.block(this,false)});}});
 },
 onPageScroll(event){const now=Date.now();if(now-(this.lastScroll||0)<2000)return;this.lastScroll=now;
  wx.createSelectorQuery().in(this).select('.reader').boundingClientRect(rect=>{if(!rect)return;const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync();tracking.emit(this,'scroll_depth',{scroll_depth:Math.min(100,Math.max(0,100*(event.scrollTop+window.windowHeight)/Math.max(1,rect.height)))});}).exec();
 },
 goBack(){wx.navigateBack({delta:1,fail:()=>wx.switchTab({url:'/pages/index/index'})});},
 onShareAppMessage(){const content=this.data.content;if(!this.data.contentReady||!content)return {};return {title:content?content.title:'自有内容',path:'/'+ROUTE+'?content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||''),imageUrl:content&&content.cover?content.cover.url:undefined};},
 onShareTimeline(){const content=this.data.content;if(!this.data.contentReady||!content)return {};return {title:content?content.title:'自有内容',query:'content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||''),imageUrl:content&&content.cover?content.cover.url:undefined};}
});
