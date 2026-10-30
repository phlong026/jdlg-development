// @ts-nocheck
'use strict';
const tracking=require('../../utils/tracking/index');
const api=require('../../utils/tracking/api');
const {createVideoTracker}=require('../../utils/tracking/video');
const {renderArticleDocument}=require('../../utils/article-document');
const ROUTE='pages/contentreader/contentreader';
Page({
 data:{loading:true,error:'',content:null,contentReady:false,media:[],articleBlocks:[],consentNotice:'',consentAvailable:false,consented:false,mediaErrors:{},sourceOpen:false,guideVisible:false,webLinkBusy:false},
 onLoad(options){this.entryGeneration=(this.entryGeneration||0)+1;this.hasShown=false;this.entryAttributionAllowed=true;this.options=options||{};this.players={};this.playerVisible={};this.observers=[];this.evaluateGuide();this.loadContent();},
 onShow(){this.pageVisible=true;tracking.block(this,false);if(this.hasShown)this.loadContent();this.hasShown=true;this.syncPlayerVisibility();},
 onHide(){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=false;this.pageVisible=false;this.setData({webLinkBusy:false});this.syncPlayerVisibility();this.pausePlayers();},
 onUnload(){if(this.stopObservingTracking)this.stopObservingTracking();this.loadSequence=(this.loadSequence||0)+1;this.pageVisible=false;this.pausePlayers();this.observers.forEach(o=>o.disconnect());this.observers=[];},
 acceptExternalEntry(options){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=true;this.options=options||{};this.setData({content:null,contentReady:false,loading:true});this.loadContent();},
 async loadContent(){
  const sequence=(this.loadSequence||0)+1;this.loadSequence=sequence;
  const entryGeneration=this.entryGeneration,entryValid=tracking.captureEntryValidity?tracking.captureEntryValidity():()=>true;
  this.pausePlayers();if(wx.hideShareMenu)wx.hideShareMenu();this.setData({loading:true,error:'',content:null,media:[],articleBlocks:[],contentReady:false});
  try{
   const resolvedOptions=await api.resolveReaderOptions(this.options);
   if(sequence!==this.loadSequence)return;
   this.options={...resolvedOptions};
   if(entryGeneration!==this.entryGeneration||this.entryAttributionAllowed===false||!entryValid()){delete this.options.st;delete this.options.ht;}
   tracking.resolvePageOptions(this,this.options,this.boundEntryGeneration!==entryGeneration);
   this.boundEntryGeneration=entryGeneration;
   // Only a newly observed external entry may supply attribution. Ordinary
   // foreground content revalidation must not replay a revoked/consumed token.
   this.options={...this.options};delete this.options.st;delete this.options.ht;delete this.options.scene;
   const id=String(this.options.content_id||''),version=Number(this.options.content_version);
   if(!/^[A-Za-z0-9_-]{8,128}$/.test(id)||!Number.isInteger(version)||version<1)throw new Error('内容入口不完整');
   const configuration=await tracking.refreshConfig();
   if(sequence!==this.loadSequence)return;
   this.setData({consentNotice:configuration.privacy_notice||'',consentAvailable:configuration.enabled===true&&!!configuration.consent_version&&!!configuration.privacy_notice,consented:tracking.consented()});
   // Public published content is independently readable, even when analytics is disabled or declined.
   const content=await api.request('/contents/'+encodeURIComponent(id)+'/versions/'+version,'GET',{},undefined,false);
   if(sequence!==this.loadSequence)return;
   const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync(),width=Math.max(240,window.windowWidth-40);
   const media=(content.media||[]).map(asset=>({...asset,url:api.mediaURL(asset.url)})).filter(asset=>['image','video'].includes(asset.kind)&&/^https:\/\//.test(asset.url||'')).map((asset,index)=>({...asset,domId:'media-'+index,playerKey:asset.media_id,errorKey:'legacyMedia'+index,height:Math.max(120,Math.round(width*(asset.height||9)/(asset.width||16)))}));
   const articleBlocks=content.body_format==='article_v2'?renderArticleDocument(content.body,media,width):[];
   if(content.cover)content.cover={...content.cover,url:api.mediaURL(content.cover.url)};
   this.setData({content,media,articleBlocks,loading:false,contentReady:true},()=>{this.observePlayers();if(this.pageVisible&&wx.showShareMenu)wx.showShareMenu({menus:['shareAppMessage','shareTimeline']});tracking.preheatTimelineShare(this,this.timelineShareValue());});
  }catch(error){if(sequence!==this.loadSequence)return;this.setData({loading:false,contentReady:false,error:'内容暂不可访问，可能已撤回、无权限或网络不可用。请重试。'});}
 },
 renderedMedia(){return this.data.content&&this.data.content.body_format==='article_v2'?this.data.articleBlocks.filter(block=>block.kind==='image'||block.kind==='video'):this.data.media;},
 acceptTracking(){if(tracking.setConsent(true)){this.setData({consented:true});}},
 declineTracking(){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=false;tracking.setConsent(false);this.setData({consented:false});},
 observePlayers(){
  if(this.stopObservingTracking)this.stopObservingTracking();
  this.observers.forEach(o=>o.disconnect());this.observers=[];
  this.playerGeneration=(this.playerGeneration||0)+1;this.players={};this.playerVisible={};
  (this.renderedMedia?this.renderedMedia():(this.data.media||[])).filter(m=>m.kind==='video').forEach(media=>{
   const playerKey=media.playerKey||media.media_id;
   const player=createVideoTracker({mono:()=>wx.getPerformance().now(),observation:()=>tracking.observation?tracking.observation(this):null,emit:(type,extra)=>tracking.emit(this,type,extra)},media.media_id,media.domId+'-generation-'+this.playerGeneration);
   this.players[playerKey]=player;
   // If intersection observation is unsupported, playback remains usable but no claimed watch time.
   if(this.createIntersectionObserver){const observer=this.createIntersectionObserver({thresholds:[0,.5,1]});observer.relativeToViewport().observe('#'+media.domId,rect=>{
    this.playerVisible[playerKey]=rect.intersectionRatio>=.5;this.syncPlayerVisibility();
    if(!this.playerVisible[playerKey]&&this.fullscreenMedia!==playerKey)wx.createVideoContext(media.domId,this).pause();
   });this.observers.push(observer);}
  });
  // Subscribe at the SDK boundary so the global consent modal, withdrawal,
  // identity changes and new page views reset the same surviving players.
  if(tracking.observe)this.stopObservingTracking=tracking.observe(this,state=>Object.keys(this.players).forEach(id=>this.players[id].observe(state)));
 },
 syncPlayerVisibility(){Object.keys(this.players||{}).forEach(id=>this.players[id].visible(!!this.pageVisible&&(!!this.playerVisible[id]||this.fullscreenMedia===id)&&!this.data.sourceOpen));},
 pausePlayers(){(this.renderedMedia?this.renderedMedia():(this.data.media||[])).filter(m=>m.kind==='video').forEach(m=>{wx.createVideoContext(m.domId,this).pause();const p=this.players[m.playerKey||m.media_id];if(p)p.state('paused');});},
 videoState(event){const key=event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId,p=this.players[key];if(!p)return;
  const states={play:'playing',pause:'paused',ended:'ended',waiting:'buffering',error:'error'};
  p.state(states[event.type]||'paused');
  if(event.type==='error')this.setData({['mediaErrors.'+event.currentTarget.dataset.errorKey]:'视频源不可播放，请稍后重试。'});
 },
 videoFullscreen(event){this.fullscreenMedia=event.detail.fullScreen?(event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId):'';this.syncPlayerVisibility();},
 videoProgress(event){const p=this.players[event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId];if(p)p.progress(event.detail.currentTime,event.detail.duration);},
 videoRate(event){const p=this.players[event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId];if(p)p.rate(event.detail.playbackRate);},
 openImage(event){const media=this.renderedMedia().find(m=>m.media_id===event.currentTarget.dataset.mediaId);if(!media||media.kind!=='image')return;
  tracking.emit(this,'image_open',{media_id:media.media_id});tracking.block(this,true);this.pausePlayers();
  wx.previewImage({urls:[media.url],current:media.url,showmenu:false});
 },
 retryMedia(){this.setData({mediaErrors:{}});this.loadContent();},
 mediaError(event){this.setData({['mediaErrors.'+event.currentTarget.dataset.errorKey]:'图片加载失败，请重试。'});},
 copySource(){const url=this.data.content&&this.data.content.source_url;if(!url||!/^https:\/\//.test(url))return;
  tracking.emit(this,'outbound_click',{action_key:'copy_original_url'});this.pausePlayers();
  wx.setClipboardData({data:url,success:()=>{tracking.block(this,true);wx.showModal({title:'原文链接已复制',content:'请在浏览器中打开原文。原文中的阅读、滚动和播放不在本系统记录范围内。',showCancel:false,complete:()=>tracking.block(this,false)});}});
 },
 async copyWebLink(){
  if(this.data.webLinkBusy||!this.data.contentReady||!this.data.content)return;
  const token=String(wx.getStorageSync('token')||'');
  if(!token){wx.navigateTo({url:'/pages/login/login'});return;}
  if(!tracking.consented()){tracking.showConsentChoices(this);return;}
  const generation=this.entryGeneration;
  const contentID=String(this.options.content_id||''),version=String(this.options.content_version||'');
  const current=()=>generation===this.entryGeneration&&token===String(wx.getStorageSync('token')||'')&&contentID===String(this.options.content_id||'')&&version===String(this.options.content_version||'');
  this.setData({webLinkBusy:true});
  try{
   const original={title:this.data.content.title,path:'/'+ROUTE+'?content_id='+encodeURIComponent(contentID)+'&content_version='+encodeURIComponent(version)};
   const prepared=await tracking.prepareShare(this,original,'app_message','web:'+contentID+':'+version,false,current);
   if(!current())return;
   const st=prepared&&prepared.path&&tracking.splitPath(prepared.path,ROUTE).query.st;
   if(!st)throw new Error('跟踪入口暂时无法生成，请稍后重试');
   const result=await require('../../utils/phase3-api').request('/api/v1/miniprogram/tracking/share-entries','POST',{share_token:st});
   if(!current())return;
   if(!result.web_url||!String(result.web_url).startsWith('https://'))throw new Error('分享网页暂未开通');
   await new Promise((resolve,reject)=>wx.setClipboardData({data:result.web_url,success:()=>{if(current())tracking.confirmPreparedShare(this,'web:'+contentID+':'+version,'app_message');resolve();},fail:()=>reject(new Error('链接未能复制，请重试'))}));
  }catch(error){if(current())wx.showToast({title:error.message||'分享链接暂不可用',icon:'none'});}
  finally{if(current())this.setData({webLinkBusy:false});}
 },
 onPageScroll(event){const now=Date.now();if(now-(this.lastScroll||0)<2000)return;this.lastScroll=now;
  wx.createSelectorQuery().in(this).select('.reader').boundingClientRect(rect=>{if(!rect)return;const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync();tracking.emit(this,'scroll_depth',{scroll_depth:Math.min(100,Math.max(0,100*(event.scrollTop+window.windowHeight)/Math.max(1,rect.height)))});}).exec();
 },
 goBack(){wx.navigateBack({delta:1,fail:()=>wx.switchTab({url:'/pages/index/index'})});},
 // 软引导：回访的阅读者引导去 AI 建房顾问留资（跳客服页带 source=content_reader 归因）。
 // 阈值只依赖本地存储，独立于访问分析 SDK——关闭"访问分析"的用户同样可见。
 evaluateGuide(){
  const KEY='share_reader_guide_v1';let record={visits:0,dismissedUntil:0};
  try{const saved=wx.getStorageSync(KEY);if(saved&&typeof saved==='object')record={visits:Number(saved.visits)||0,dismissedUntil:Number(saved.dismissedUntil)||0};}catch(e){}
  record.visits+=1;
  try{wx.setStorageSync(KEY,record);}catch(e){}
  if(record.visits>=2&&Date.now()>=record.dismissedUntil){
   this.setData({guideVisible:true});
   tracking.emit(this,'content_click',{target_content_id:'guide',component_key:'reader_guide',action_key:'guide_impression'});
  }
 },
 openGuide(){
  tracking.emit(this,'content_click',{target_content_id:'guide',component_key:'reader_guide',action_key:'guide_consult'});
  const id=String(this.options.content_id||''),version=Number(this.options.content_version);
  const contentRef=/^[A-Za-z0-9_-]{8,128}$/.test(id)&&Number.isInteger(version)&&version>0
   ? '&content_id='+encodeURIComponent(id)+'&content_version='+version
   : '';
  wx.navigateTo({url:'/pages/customerservice/customerservice?source=content_reader'+contentRef});
 },
 dismissGuide(){
  this.setData({guideVisible:false});
  try{const saved=wx.getStorageSync('share_reader_guide_v1');const record=saved&&typeof saved==='object'?saved:{};record.dismissedUntil=Date.now()+7*86400000;wx.setStorageSync('share_reader_guide_v1',record);}catch(e){}
 },
 onShareAppMessage(){const content=this.data.content;if(!this.data.contentReady||!content)return {};return {title:content?content.title:'自有内容',path:'/'+ROUTE+'?content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||''),imageUrl:content&&content.cover?content.cover.url:undefined};},
 onShareTimeline(){return this.timelineShareValue();},
 // 与 onShareTimeline 同源取值：预热（preheatTimelineShare）以此对象为缓存键，首次分享即带 st 归因令牌
 timelineShareValue(){const content=this.data.content;if(!this.data.contentReady||!content)return {};return {title:content?content.title:'自有内容',query:'content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||''),imageUrl:content&&content.cover?content.cover.url:undefined};}
});
export {};
