// @ts-nocheck
'use strict';
const tracking=require('../../utils/tracking/index');
const api=require('../../utils/tracking/api');
const {createVideoTracker}=require('../../utils/tracking/video');
const {renderArticleDocument}=require('../../utils/article-document');
const {resolveProtectedMediaURL}=require('../../utils/protected-media');
const ROUTE='pages/contentreader/contentreader';
const INVALID_ENTRY_ERRORS=new Set(['扫码入口无效','扫码内容不可访问','内容入口不完整','合集入口不完整','合集作品已改变']);
function clearsVerifiedContent(error){const status=Number(error&&error.statusCode);return [401,403,404,410].includes(status)||[401,403,404,410].includes(Number(error&&error.code))||error&&error.code==='MEDIA_IDENTITY_CHANGED'||INVALID_ENTRY_ERRORS.has(String(error&&error.message||''));}
function refreshErrorMessage(error){if(error&&error.code==='MEDIA_DOWNLOAD_FAILED')return '素材刷新失败，请检查网络后重试。';if(error&&error.networkError)return '内容刷新失败，网络不可用，请检查连接后重试。';if(Number(error&&error.statusCode)>=500)return '内容刷新失败，服务暂不可用，请稍后重试。';return '内容刷新失败，请稍后重试。';}
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
  const quiet=this.data.contentReady===true&&!!this.data.content;
  this.pausePlayers();if(wx.hideShareMenu)wx.hideShareMenu();this.setData(quiet?{error:''}:{loading:true,error:'',content:null,media:[],articleBlocks:[],contentReady:false});
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
   let readPath='/contents/'+encodeURIComponent(id)+'/versions/'+version;
   const collectionID=this.options.collection_id, collectionVersion=Number(this.options.collection_version), position=Number(this.options.item_position);
   const inCollection=collectionID!==undefined||this.options.collection_version!==undefined||this.options.item_position!==undefined;
   if(inCollection){
    if(!/^[A-Za-z0-9_-]{8,128}$/.test(String(collectionID||''))||!Number.isInteger(collectionVersion)||collectionVersion<1||this.options.item_position===undefined||!Number.isInteger(position)||position<0)throw new Error('合集入口不完整');
    readPath='/project-collections/'+encodeURIComponent(collectionID)+'/versions/'+collectionVersion+'/items/'+position+'/content';
   }
   const content=await api.request(readPath,'GET',{},undefined,true);
   if(inCollection&&(content.content_id!==id||Number(content.version)!==version))throw new Error('合集作品已改变');
   if(sequence!==this.loadSequence)return;
   const window=wx.getWindowInfo?wx.getWindowInfo():wx.getSystemInfoSync(),width=Math.max(240,window.windowWidth-40);
   const media=await Promise.all((content.media||[]).map(asset=>({...asset,url:api.mediaURL(asset.url)})).filter(asset=>['image','video'].includes(asset.kind)&&/^https:\/\//.test(asset.url||'')).map(async(asset,index)=>({...asset,url:await resolveProtectedMediaURL(asset.url),domId:'media-'+index,playerKey:asset.media_id,errorKey:'legacyMedia'+index,height:Math.max(120,Math.round(width*(asset.height||9)/(asset.width||16)))})));
   if(sequence!==this.loadSequence)return;
   const articleBlocks=content.body_format==='article_v2'?renderArticleDocument(content.body,media,width):[];
   if(content.cover){const coverURL=api.mediaURL(content.cover.url);content.cover={...content.cover,url:coverURL?await resolveProtectedMediaURL(coverURL):''};}
   if(sequence!==this.loadSequence)return;
   this.setData({content,media,articleBlocks,loading:false,contentReady:true},()=>{this.observePlayers();if(this.pageVisible&&wx.showShareMenu)wx.showShareMenu({menus:['shareAppMessage','shareTimeline']});tracking.preheatTimelineShare(this,this.timelineShareValue());});
  }catch(error){if(sequence!==this.loadSequence)return;if(quiet&&!clearsVerifiedContent(error)){this.setData({loading:false,error:refreshErrorMessage(error)});return;}this.setData({loading:false,content:null,media:[],articleBlocks:[],contentReady:false,error:'内容暂不可访问，可能已撤回或无权限。请重试。'});}
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
 timelineShareValue(){const content=this.data.content;if(!this.data.contentReady||!content)return {};return {title:content?content.title:'自有内容',query:'content_id='+encodeURIComponent(this.options.content_id||'')+'&content_version='+encodeURIComponent(this.options.content_version||''),imageUrl:content&&content.cover?content.cover.url:undefined};},
 // B14: 生成分享海报——封面+标题+小程序码 canvas 合成保存相册
 async generatePoster(){
  if(this.data.posterBusy)return;
  const content=this.data.content;if(!this.data.contentReady||!content)return;
  this.setData({posterBusy:true});
  try{
   const api=require('../../utils/phase3-api');
   const result=await api.request('/api/v1/miniprogram/workbench/content-poster','GET',{},{content_id:this.options.content_id});
   const posterURL=String(result&&result.poster_url||'');
   if(!posterURL)throw new Error('海报码生成失败');
   const [coverPath,qrPath]=await Promise.all([
    new Promise(resolve=>{const url=content&&content.cover?content.cover.url:'';if(!url)return resolve('');wx.downloadFile({url,success:res=>resolve(res.statusCode===200?res.tempFilePath:''),fail:()=>resolve('')});}),
    new Promise(resolve=>{wx.downloadFile({url:posterURL,success:res=>resolve(res.statusCode===200?res.tempFilePath:''),fail:()=>resolve('')});}),
   ]);
   const canvas=await new Promise(resolve=>{wx.createSelectorQuery().in(this).select('#posterCanvas').fields({node:true,size:true},res=>resolve(res&&res.node)).exec();});
   if(!canvas)throw new Error('画布不可用');
   const ctx=canvas.getContext('2d');
   const W=750,H=1000;canvas.width=W;canvas.height=H;
   // 背景
   ctx.fillStyle='#fbfaf7';ctx.fillRect(0,0,W,H);
   // 封面（上半区，保持比例）
   if(coverPath){
    const img=canvas.createImage();
    await new Promise(resolve=>{img.onload=resolve;img.onerror=resolve;img.src=coverPath;});
    const ratio=Math.min(W/img.width,(H*0.5)/img.height);
    const dw=img.width*ratio,dh=img.height*ratio;
    ctx.drawImage(img,(W-dw)/2,40,dw,dh);
   }else{ctx.fillStyle='#eee9e2';ctx.fillRect(40,40,W-80,H*0.5);}
   // 标题（中部）
   ctx.fillStyle='#2b211b';ctx.font='bold 36px sans-serif';ctx.textAlign='center';
   const title=String(content.title||'分享内容').slice(0,24);
   ctx.fillText(title,W/2,H*0.58,W-80);
   // 品牌行
   ctx.fillStyle='#a87943';ctx.font='28px sans-serif';
   ctx.fillText('甲第灵光 · 扫码查看完整内容',W/2,H*0.64);
   // 小程序码（底部居中）
   if(qrPath){
    const qr=canvas.createImage();
    await new Promise(resolve=>{qr.onload=resolve;qr.onerror=resolve;qr.src=qrPath;});
    const qrSize=280;
    ctx.drawImage(qr,(W-qrSize)/2,H-qrSize-80,qrSize,qrSize);
   }
   // 导出保存
   const tempPath=await new Promise((resolve,reject)=>{wx.canvasToTempFilePath({canvas,success:res=>resolve(res.tempFilePath),fail:reject});});
   wx.saveImageToPhotosAlbum({filePath:tempPath,success:()=>{this.setData({posterBusy:false});wx.showToast({title:'海报已保存到相册',icon:'success'});},fail:()=>{this.setData({posterBusy:false});wx.showToast({title:'保存失败，请授权相册',icon:'none'});}});
  }catch(error){this.setData({posterBusy:false});wx.showToast({title:error.message||'海报生成失败',icon:'none'});}
 }
});
export {};
