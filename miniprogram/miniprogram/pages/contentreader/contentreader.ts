// @ts-nocheck
'use strict';
const tracking=require('../../utils/tracking/index');
const api=require('../../utils/tracking/api');
const {createVideoTracker}=require('../../utils/tracking/video');
const {buildContentPresentation}=require('../../utils/content-presentation');
const {resolveProtectedMediaURL}=require('../../utils/protected-media');
const noteActions=require('../../utils/note-reader-actions');
const noteInteractions=require('../../utils/note-interactions');
const ROUTE='pages/contentreader/contentreader';
const INVALID_ENTRY_ERRORS=new Set(['扫码入口无效','扫码内容不可访问','内容入口不完整','合集入口不完整','合集作品已改变']);
const SUPPORTED_BODY_FORMATS=new Set(['','plain','plain_text','rich_text','article_v2']);
function clearsVerifiedContent(error){const status=Number(error&&error.statusCode);return [401,403,404,410].includes(status)||[401,403,404,410].includes(Number(error&&error.code))||error&&error.code==='MEDIA_IDENTITY_CHANGED'||INVALID_ENTRY_ERRORS.has(String(error&&error.message||''));}
function refreshErrorMessage(error){if(error&&error.code==='CONTENT_INVALID')return error.message;if(error&&error.code==='MEDIA_DOWNLOAD_FAILED')return '素材刷新失败，请检查网络后重试。';if(error&&error.networkError)return '内容刷新失败，网络不可用，请检查连接后重试。';if(Number(error&&error.statusCode)>=500)return '内容刷新失败，服务暂不可用，请稍后重试。';return '内容刷新失败，请稍后重试。';}
function eventTracks(event){const value=event&&event.currentTarget&&event.currentTarget.dataset&&event.currentTarget.dataset.tracking;return value!==false&&value!=='false';}
function invalidContent(message){const error=new Error(message);error.code='CONTENT_INVALID';return error;}
function assertUsableContent(content,view){
 if(!content||typeof content!=='object'||Array.isArray(content)||!String(content.title||'').trim())throw invalidContent('内容数据不完整，请重试。');
 if(!SUPPORTED_BODY_FORMATS.has(String(content.body_format||'')))throw invalidContent('该内容格式暂不支持，请重试。');
 if(!view||!['article','gallery','video'].includes(view.kind))throw invalidContent('该内容格式暂不支持，请重试。');
 if(view.kind==='article'&&(!Array.isArray(view.blocks)||!view.blocks.length))throw invalidContent('内容数据不完整，请重试。');
 if(view.kind!=='article'&&(!Array.isArray(view.media)||!view.media.length))throw invalidContent('内容素材不完整，请重试。');
}
function consentState(){
 if(tracking.consentChoice)return tracking.consentChoice();
 try{const choice=wx.getStorageSync('share_tracking_consent_choice_v1');return{resolved:!!(choice&&choice.explicit===true),accepted:!!(choice&&choice.explicit===true&&choice.accepted===true)};}catch(_){return{resolved:false,accepted:false};}
}
function presentationOptions(window,galleryIndex,descriptionExpanded){const safeBottom=window&&window.safeArea&&window.screenHeight?Math.max(0,window.screenHeight-window.safeArea.bottom):0;return{prefix:'reader',galleryIndex,descriptionExpanded,videoChromeHeight:(Number(window&&window.statusBarHeight)||20)+44+76+safeBottom};}
function nativeShareTitle(value,fallback){return String(value||fallback||'自有内容').replace(/\s+/g,' ').trim().slice(0,80);}
Page({
 ...noteActions.methods,
 ...noteInteractions.methods,
 data:{...noteActions.data,...noteInteractions.data,loading:true,error:'',content:null,contentReady:false,media:[],articleBlocks:[],presentationView:null,videoStarted:{},consentNotice:'',consentAvailable:false,consentChoicePending:false,consented:false,mediaErrors:{},sourceOpen:false,guideVisible:false,webLinkBusy:false,posterBusy:false,posterAvailable:false},
 onLoad(options){this.entryGeneration=(this.entryGeneration||0)+1;this.hasShown=false;this.entryAttributionAllowed=true;this.options=options||{};this.players={};this.playerVisible={};this.observers=[];this.evaluateGuide();this.loadContent();},
 onShow(){this.pageVisible=true;tracking.block(this,false);if(this.hasShown)this.loadContent();this.hasShown=true;this.syncPlayerVisibility();},
 onHide(){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=false;this.pageVisible=false;this.setData({webLinkBusy:false,noteMenuOpen:false,noteActionBusy:false});if(this.invalidateNoteInteractions)this.invalidateNoteInteractions();this.syncPlayerVisibility();this.pausePlayers();},
 onUnload(){if(this.invalidateNoteInteractions)this.invalidateNoteInteractions();if(this.stopObservingTracking)this.stopObservingTracking();this.loadSequence=(this.loadSequence||0)+1;this.posterCapabilitySequence=(this.posterCapabilitySequence||0)+1;this.posterOperation=(this.posterOperation||0)+1;this.posterCapability=null;this.pageVisible=false;this.pausePlayers();this.observers.forEach(o=>o.disconnect());this.observers=[];},
 acceptExternalEntry(options){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=true;this.options=options||{};this.invalidatePosterAvailability();this.setData({content:null,contentReady:false,loading:true,presentationView:null,videoStarted:{}});this.loadContent();},
 async loadContent(){
  const sequence=(this.loadSequence||0)+1;this.loadSequence=sequence;
  const posterCapabilitySequence=this.invalidatePosterAvailability();
  this.setData({noteMenuOpen:false,noteActionBusy:false});if(this.invalidateNoteInteractions)this.invalidateNoteInteractions();
  const entryGeneration=this.entryGeneration,entryValid=tracking.captureEntryValidity?tracking.captureEntryValidity():()=>true;
  const quiet=this.data.contentReady===true&&!!this.data.content;
  this.pausePlayers();if(wx.hideShareMenu)wx.hideShareMenu();this.setData(quiet?{error:'',videoStarted:{}}:{loading:true,error:'',content:null,media:[],articleBlocks:[],presentationView:null,videoStarted:{},contentReady:false});
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
   const configurationPromise=tracking.refreshConfig();
   // Public published content is independently readable, even when analytics is disabled or declined.
   let readPath='/contents/'+encodeURIComponent(id)+'/versions/'+version;
   const collectionID=this.options.collection_id, collectionVersion=Number(this.options.collection_version), position=Number(this.options.item_position);
   const inCollection=collectionID!==undefined||this.options.collection_version!==undefined||this.options.item_position!==undefined;
   if(inCollection){
    if(!/^[A-Za-z0-9_-]{8,128}$/.test(String(collectionID||''))||!Number.isInteger(collectionVersion)||collectionVersion<1||this.options.item_position===undefined||!Number.isInteger(position)||position<0)throw new Error('合集入口不完整');
    readPath='/project-collections/'+encodeURIComponent(collectionID)+'/versions/'+collectionVersion+'/items/'+position+'/content';
   }
   const contentRequest=api.request(readPath,'GET',{},undefined,true);
   const [configuration, content]=await Promise.all([configurationPromise, contentRequest]);
   if(inCollection&&(content.content_id!==id||Number(content.version)!==version))throw new Error('合集作品已改变');
   const consentAvailable=configuration.enabled===true&&!!configuration.consent_version&&!!configuration.privacy_notice;
   // T7.1：默认开启在 refreshConfig 内完成；若静默写入失败（例如存储异常），这里补一次，
   // 仍未决时才降级显示提示，避免静默失败。
   if(consentAvailable&&!consentState().resolved)tracking.ensureDefaultConsent();
   const choice=consentState();
   // 服务明确关闭（enabled=false）不提示；配置未就绪或请求失败时给非阻塞提示，避免静默失败。
   const consentUnavailable=configuration.configError===true||configuration.enabled===true;
   this.setData({consentNotice:configuration.privacy_notice||'',consentAvailable,consentUnavailable:consentUnavailable&&!consentAvailable,consentChoicePending:consentAvailable&&!choice.resolved,consented:choice.accepted});
   if(sequence!==this.loadSequence)return;
   const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync();
   // 封面与正文一起就绪后再渲染：视频走签名直链可立即流式播放，不再整段下载。
   if(content.cover){const coverSource=api.mediaURL(content.cover.url);try{content.cover={...content.cover,url:coverSource?await resolveProtectedMediaURL(coverSource):''};}catch(error){if(error&&error.code==='MEDIA_IDENTITY_CHANGED')throw error;content.cover={...content.cover,url:''};}}
   if(sequence!==this.loadSequence)return;
   const media=await Promise.all((content.media||[]).filter(asset=>['image','video'].includes(asset.kind)).map(async asset=>{
    const source=api.mediaURL(asset.url);
    if(!/^https:\/\//.test(source||''))return {...asset,url:'',preview_error:'素材地址不可用'};
    if(asset.kind==='video'&&typeof api.signedMediaURL==='function'){
     try{const signed=await api.signedMediaURL(asset.media_id);if(signed)return {...asset,url:signed};}catch(_){}
    }
    try{return {...asset,url:await resolveProtectedMediaURL(source)};}
    catch(error){if(error&&error.code==='MEDIA_IDENTITY_CHANGED')throw error;return {...asset,url:'',preview_error:'素材加载失败'};}
   }));
   if(sequence!==this.loadSequence)return;
   const presentationView=buildContentPresentation(content,media,window,presentationOptions(window,this.data.presentationView&&this.data.presentationView.galleryIndex,false));
   assertUsableContent(content,presentationView);
   const articleBlocks=presentationView.blocks;
   const mediaErrors={};presentationView.media.filter(item=>item.preview_error||!item.url).forEach(item=>{mediaErrors[item.errorKey]=item.preview_error||'素材暂不可用，请重试。';});
   this.setData({content,media,articleBlocks,presentationView,mediaErrors,loading:false,contentReady:true},()=>{this.observePlayers();if(this.pageVisible&&wx.showShareMenu)wx.showShareMenu({menus:['shareAppMessage','shareTimeline']});tracking.preheatTimelineShare(this,this.timelineShareValue());this.refreshPosterAvailability(content,sequence,posterCapabilitySequence);if(presentationView.kind!=='video'&&wx.nextTick)wx.nextTick(()=>{if(sequence===this.loadSequence&&this.pageVisible!==false&&this.loadNoteInteractions)this.loadNoteInteractions();});});
  }catch(error){if(sequence!==this.loadSequence)return;if(quiet&&!clearsVerifiedContent(error)){this.setData({loading:false,error:refreshErrorMessage(error)});return;}this.setData({loading:false,content:null,media:[],articleBlocks:[],presentationView:null,contentReady:false,error:error&&error.code==='CONTENT_INVALID'?error.message:'内容暂不可访问，可能已撤回或无权限。请重试。'});}
 },
 loadMoreNoteComments(){return this.loadNoteInteractions(true);},
 scrollToNoteComments(){wx.pageScrollTo({selector:'#note-comments',duration:200,fail:()=>wx.showToast({title:'请向下查看评论',icon:'none'})});},
 renderedMedia(){return this.data.presentationView&&Array.isArray(this.data.presentationView.media)?this.data.presentationView.media:[];},
 rebuildPresentation(galleryIndex){if(!this.data.content)return;const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync();const presentationView=buildContentPresentation(this.data.content,this.data.media,window,presentationOptions(window,galleryIndex,this.data.presentationView&&this.data.presentationView.descriptionExpanded));this.setData({presentationView,articleBlocks:presentationView.blocks});},
 toggleContentDescription(){if(!this.data.presentationView||this.data.presentationView.kind!=='video')return;this.setData({'presentationView.descriptionExpanded':!this.data.presentationView.descriptionExpanded});},
 onContentGalleryChange(event){const index=Math.max(0,Number(event.detail&&event.detail.current)||0);if(this.data.presentationView&&index===this.data.presentationView.galleryIndex)return;this.rebuildPresentation(index);if(eventTracks(event))tracking.emit(this,'content_click',{component_key:'content_gallery',action_key:'swipe',item_position:index});},
 onContentVideoMetadata(event){const width=Number(event.detail&&event.detail.width),height=Number(event.detail&&event.detail.height),id=event.currentTarget.dataset.mediaId;if(!(width>0&&height>0&&id))return;let changed=false;const media=this.data.media.map(item=>{if(item.media_id!==id||item.width===width&&item.height===height)return item;changed=true;return {...item,width,height};});if(!changed)return;this.setData({media},()=>this.rebuildPresentation(this.data.presentationView&&this.data.presentationView.galleryIndex));},
 onResize(){this.rebuildPresentation(this.data.presentationView&&this.data.presentationView.galleryIndex);},
 acceptTracking(){if(tracking.setConsent(true)){this.setData({consented:true,consentChoicePending:false});}},
 declineTracking(){this.entryGeneration=(this.entryGeneration||0)+1;this.entryAttributionAllowed=false;const saved=tracking.setConsent(false);this.setData({consented:false,consentChoicePending:false,consentNotice:saved?this.data.consentNotice:this.data.consentNotice+'（关闭暂未保存成功，本次会话已停止记录）'});},
 async openTrackingSettings(){await tracking.showConsentChoices(this);const choice=consentState();this.setData({consented:choice.accepted,consentChoicePending:this.data.consentAvailable&&!choice.resolved});},
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
 playContentVideo(event){const data=event&&event.currentTarget&&event.currentTarget.dataset||{},id=String(data.domId||''),key=String(data.playerKey||'');if(!id||!key)return;this.setData({videoStarted:{...this.data.videoStarted,[key]:true}},()=>wx.createVideoContext(id,this).play());},
 videoState(event){const key=event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId;if(event.type==='play'&&key&&!this.data.videoStarted[key])this.setData({videoStarted:{...this.data.videoStarted,[key]:true}});if(event.type==='error'){const videoStarted={...this.data.videoStarted};delete videoStarted[key];this.setData({videoStarted,['mediaErrors.'+event.currentTarget.dataset.errorKey]:'视频源不可播放，请稍后重试。'});}if(!eventTracks(event))return;const p=this.players[key];if(!p)return;
  const states={play:'playing',pause:'paused',ended:'ended',waiting:'buffering',error:'error'};
  p.state(states[event.type]||'paused');
 },
 videoFullscreen(event){if(!eventTracks(event))return;this.fullscreenMedia=event.detail.fullScreen?(event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId):'';this.syncPlayerVisibility();},
 videoProgress(event){if(!eventTracks(event))return;const p=this.players[event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId];if(p)p.progress(event.detail.currentTime,event.detail.duration);},
 videoRate(event){if(!eventTracks(event))return;const p=this.players[event.currentTarget.dataset.playerKey||event.currentTarget.dataset.mediaId];if(p)p.rate(event.detail.playbackRate);},
 openImage(event){const media=this.renderedMedia().find(m=>m.media_id===event.currentTarget.dataset.mediaId);if(!media||media.kind!=='image')return;
  if(eventTracks(event)){tracking.emit(this,'image_open',{media_id:media.media_id});tracking.block(this,true);this.pausePlayers();}
  const urls=this.renderedMedia().filter(item=>item.kind==='image').map(item=>item.url);wx.previewImage({urls,current:media.url,showmenu:false});
 },
 retryMedia(){this.setData({mediaErrors:{}});this.loadContent();},
 mediaError(event){this.setData({['mediaErrors.'+event.currentTarget.dataset.errorKey]:'图片加载失败，请重试。'});},
 copySource(){const url=this.data.content&&this.data.content.source_url;if(!url||!/^https:\/\//.test(url))return;
  tracking.emit(this,'outbound_click',{action_key:'copy_original_url'});this.pausePlayers();
  wx.setClipboardData({data:url,success:()=>{tracking.block(this,true);wx.showModal({title:'原文链接已复制',content:'请在浏览器中打开原文。原文中的阅读、滚动和播放不在本系统记录范围内。',showCancel:false,complete:()=>tracking.block(this,false)});}});
 },
 copyVideoCopy(){const view=this.data.presentationView;if(!this.data.contentReady||!view||view.kind!=='video')return;const text=String(view.shareCopy||view.bodyText||view.title||'').trim();if(!text)return;wx.setClipboardData({data:text,success:()=>wx.showToast({title:'视频文案已复制',icon:'success'})});},
 invalidatePosterAvailability(){this.posterCapabilitySequence=(this.posterCapabilitySequence||0)+1;this.posterOperation=(this.posterOperation||0)+1;this.posterCapability=null;if(this.data.posterAvailable||this.data.posterBusy)this.setData({posterAvailable:false,posterBusy:false});return this.posterCapabilitySequence;},
 posterAccessCurrent(){
  const proof=this.posterCapability,content=this.data.content||{};
  return this.data.posterAvailable===true&&!!proof&&proof.sequence===this.posterCapabilitySequence&&proof.token===String(wx.getStorageSync('token')||'')&&proof.contentID===String(this.options&&this.options.content_id||'')&&proof.version===String(this.options&&this.options.content_version||'')&&proof.owner===String(content.owner_id||content.owner||'');
 },
 async refreshPosterAvailability(content,loadSequence,capabilitySequence){
  const token=String(wx.getStorageSync('token')||''),contentID=String(this.options&&this.options.content_id||''),version=String(this.options&&this.options.content_version||''),owner=String(content&& (content.owner_id||content.owner)||'');
  if(!token||!contentID||!version||!owner)return false;
  const current=()=>loadSequence===this.loadSequence&&capabilitySequence===this.posterCapabilitySequence&&token===String(wx.getStorageSync('token')||'')&&contentID===String(this.options&&this.options.content_id||'')&&version===String(this.options&&this.options.content_version||'')&&owner===String(this.data.content&&(this.data.content.owner_id||this.data.content.owner)||'');
  try{
   const context=await require('../../utils/phase3-api').workbenchContext();
   if(!current())return false;
   const owners=Array.isArray(context&&context.owners)?context.owners.map(String):[];
   const available=!!(context&&context.enabled===true&&context.can_manage===true&&owners.includes(owner));
   this.posterCapability=available?{token,contentID,version,owner,sequence:capabilitySequence}:null;
   this.setData({posterAvailable:available});
   return available;
  }catch(_){if(current()){this.posterCapability=null;this.setData({posterAvailable:false});}return false;}
 },
 openVideoMore(){
  const view=this.data.presentationView;
  if(!this.data.contentReady||!view||view.kind!=='video'||typeof wx.showActionSheet!=='function')return;
  const actions=[{label:'复制链接',handler:'copyWebLink'}];
  if(this.posterAccessCurrent())actions.push({label:'生成海报',handler:'generatePoster'});
  if(this.data.consentAvailable)actions.push({label:'访问分析设置',handler:'openTrackingSettings'});
  wx.showActionSheet({itemList:actions.map(action=>action.label),success:result=>{
   if(!this.data.contentReady||!this.data.presentationView||this.data.presentationView.kind!=='video')return;
   const action=actions[Number(result&&result.tapIndex)],handler=action&&this[action.handler];
   if(typeof handler==='function')handler.call(this);
  }});
 },
 async copyWebLink(){
  if(this.data.webLinkBusy||!this.data.contentReady||!this.data.content)return;
  const token=String(wx.getStorageSync('token')||'');
  const generation=this.entryGeneration;
  const contentID=String(this.options.content_id||''),version=String(this.options.content_version||'');
  const currentContent=()=>generation===this.entryGeneration&&contentID===String(this.options.content_id||'')&&version===String(this.options.content_version||'');
  const tracked=tracking.consented()&&String(this.options.untracked_share||'')!=='1';
  if(tracked&&!token){wx.navigateTo({url:'/pages/login/login'});return;}
  const current=()=>currentContent()&&(!tracked||token===String(wx.getStorageSync('token')||''));
  this.setData({webLinkBusy:true});
  try{
   if(!tracked){
    const result=await require('../../utils/phase3-api').request('/api/v1/miniprogram/public-share-links','POST',{kind:'content',id:contentID,version:Number(version)});
    if(!current())return;
    if(!result.web_url||!String(result.web_url).startsWith('https://'))throw new Error('普通分享链接暂未开通');
    await new Promise((resolve,reject)=>wx.setClipboardData({data:result.web_url,success:resolve,fail:()=>reject(new Error('链接未能复制，请重试'))}));
    return;
   }
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
  if(!this.data.contentReady||!this.data.content)return;
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
 onShareAppMessage(){const content=this.data.content,view=this.data.presentationView;if(!this.data.contentReady||!content)return {};const plain=String(this.options.untracked_share||'')==='1'?'&untracked_share=1':'';return {title:nativeShareTitle(view&&view.shareCopy,content.title),path:'/'+ROUTE+'?content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||'')+plain,imageUrl:content.cover?content.cover.url:undefined};},
 onShareTimeline(){return this.timelineShareValue();},
 // 与 onShareTimeline 同源取值：预热（preheatTimelineShare）以此对象为缓存键，首次分享即带 st 归因令牌
 timelineShareValue(){const content=this.data.content,view=this.data.presentationView;if(!this.data.contentReady||!content)return {};const plain=String(this.options.untracked_share||'')==='1'?'&untracked_share=1':'';return {title:nativeShareTitle(view&&view.momentsCopy,content.title),query:'content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||'')+plain,imageUrl:content.cover?content.cover.url:undefined};},
 // B14: 生成分享海报——封面+标题+小程序码 canvas 合成保存相册
 async generatePoster(){
  if(this.data.posterBusy||!this.posterAccessCurrent())return;
  const content=this.data.content;if(!this.data.contentReady||!content)return;
  const proof=this.posterCapability,token=String(wx.getStorageSync('token')||''),contentID=String(this.options.content_id||''),version=String(this.options.content_version||''),operation=(this.posterOperation||0)+1;
  this.posterOperation=operation;
  const current=()=>operation===this.posterOperation&&this.posterCapability===proof&&token===String(wx.getStorageSync('token')||'')&&contentID===String(this.options.content_id||'')&&version===String(this.options.content_version||'')&&this.posterAccessCurrent();
  const download=(url,required)=>new Promise((resolve,reject)=>{
   const source=String(url||'');
   if(!source){if(required)reject(new Error('海报码地址无效'));else resolve('');return;}
   if(!/^https?:\/\//.test(source)){resolve(source);return;}
   wx.downloadFile({url:source,timeout:60000,success:res=>{if(res.statusCode===200&&res.tempFilePath)resolve(res.tempFilePath);else if(required)reject(new Error('海报码下载失败，请重试'));else resolve('');},fail:()=>{if(required)reject(new Error('海报码下载失败，请重试'));else resolve('');}});
  });
  this.setData({posterBusy:true});
  try{
   const api=require('../../utils/phase3-api');
   const result=await api.request('/api/v1/miniprogram/workbench/content-poster','GET',{},{content_id:contentID,content_version:version});
   if(!current())return;
   const posterURL=String(result&&result.poster_url||'');
   if(!posterURL)throw new Error('海报码生成失败');
   const [coverPath,qrPath]=await Promise.all([
    download(content&&content.cover&&content.cover.url,false),
    download(posterURL,true),
   ]);
   if(!current())return;
   const canvas=await new Promise(resolve=>{wx.createSelectorQuery().in(this).select('#posterCanvas').fields({node:true,size:true},res=>resolve(res&&res.node)).exec();});
   if(!current())return;
   if(!canvas)throw new Error('画布不可用');
   const ctx=canvas.getContext('2d');
   const W=750,H=1000;canvas.width=W;canvas.height=H;
   // 背景
   ctx.fillStyle='#fbfaf7';ctx.fillRect(0,0,W,H);
   // 封面（上半区，保持比例）
   if(coverPath){
    const img=canvas.createImage();
    await new Promise(resolve=>{img.onload=resolve;img.onerror=resolve;img.src=coverPath;});
    if(!current())return;
    if(img.width&&img.height){const ratio=Math.min(W/img.width,(H*0.5)/img.height);const dw=img.width*ratio,dh=img.height*ratio;ctx.drawImage(img,(W-dw)/2,40,dw,dh);}
    else{ctx.fillStyle='#eee9e2';ctx.fillRect(40,40,W-80,H*0.5);}
   }else{ctx.fillStyle='#eee9e2';ctx.fillRect(40,40,W-80,H*0.5);}
   // 标题（中部）
   ctx.fillStyle='#2b211b';ctx.font='bold 36px sans-serif';ctx.textAlign='center';
   const title=String(content.title||'分享内容').slice(0,24);
   ctx.fillText(title,W/2,H*0.58,W-80);
   // 品牌行
   ctx.fillStyle='#a87943';ctx.font='28px sans-serif';
   ctx.fillText('甲第灵光 · 扫码查看完整内容',W/2,H*0.64);
   // 小程序码（底部居中）
   const qr=canvas.createImage();
   await new Promise((resolve,reject)=>{qr.onload=resolve;qr.onerror=()=>reject(new Error('海报码加载失败，请重试'));qr.src=qrPath;});
   if(!current())return;
   const qrSize=280;ctx.drawImage(qr,(W-qrSize)/2,H-qrSize-80,qrSize,qrSize);
   // 导出保存
   const tempPath=await new Promise((resolve,reject)=>{wx.canvasToTempFilePath({canvas,success:res=>resolve(res.tempFilePath),fail:reject});});
   if(!current())return;
   await new Promise((resolve,reject)=>wx.saveImageToPhotosAlbum({filePath:tempPath,success:resolve,fail:()=>reject(new Error('保存失败，请授权相册'))}));
   if(current())wx.showToast({title:'海报已保存到相册',icon:'success'});
  }catch(error){if(current())wx.showToast({title:error.message||'海报生成失败',icon:'none'});}
  finally{if(operation===this.posterOperation)this.setData({posterBusy:false});}
 }
});
