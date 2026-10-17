// @ts-nocheck
'use strict';
const { createTracker, HEARTBEAT } = require('./core');
const api = require('./api');
const routes = require('./routes');
const STORAGE = 'share_tracking_queue_v1';
const CONSENT = 'share_tracking_consent_v1';
const CONSENT_CHOICE = 'share_tracking_consent_choice_v1';
let sdk, config = { enabled: false }, installed = false, instance = '', timer;
let consentPage = null, consentRequest = null, consentChoice = null, consentRevision = 0, entryInvalidation = 0;
const safely = (fn, fallback) => { try { return fn(); } catch (_) { return fallback; } };
function id() { return 'e_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2); }
function mono() { const performance = wx.getPerformance && wx.getPerformance(); return performance && typeof performance.now === 'function' ? performance.now() : null; }
function anonymousInstance() {
  // Stored random installation handle only; never device identifiers, fingerprint, location or account-derived ID.
  let value = safely(() => wx.getStorageSync('share_tracking_instance_v1'), '');
  if (value) return Promise.resolve(value);
  if (!wx.getRandomValues) return Promise.reject(new Error('随机标识能力不可用'));
  return new Promise((resolve, reject) => wx.getRandomValues({ length: 24, success(result) {
    value = Array.from(new Uint8Array(result.randomValues)).map(x => x.toString(16).padStart(2, '0')).join('');
    safely(() => wx.setStorageSync('share_tracking_instance_v1', value)); resolve(value);
  }, fail: reject }));
}
function getSDK() {
  if (sdk) return sdk;
  if (mono() == null) return null; // Never substitute user-adjustable wall time for duration.
  sdk = createTracker({ wall: Date.now, mono, id, random: Math.random,
    account: () => safely(() => String(wx.getStorageSync('token') || ''), ''),
    loadDiagnostics:()=>wx.getStorageSync(STORAGE+'_diagnostics'),saveDiagnostics:value=>wx.setStorageSync(STORAGE+'_diagnostics',value),
    load: () => wx.getStorageSync(STORAGE), save: queue => wx.setStorageSync(STORAGE, queue),
    async entry(body) { if (!instance) instance = await anonymousInstance(); return api.request('/entry','POST', { ...body, client_instance: instance, anonymous_id: instance }, undefined, true); },
    send: (body, credential) => api.request('/events/batch','POST',body,credential,false)
  });
  return sdk;
}
function enabledConsent() {
  const choice=consentChoice || safely(()=>wx.getStorageSync(CONSENT_CHOICE),null);
  if(choice) return choice.accepted===true && choice.version===config.consent_version && choice.notice===config.privacy_notice ? choice.version : '';
  const accepted = safely(() => wx.getStorageSync(CONSENT), null);
  return accepted && accepted.version === config.consent_version && config.privacy_notice ? accepted.version : '';
}
function refreshConfig() {
  return api.request('/config').then(value => {
    config = value || {enabled:false}; if(config.enabled!==true)entryInvalidation++;
    // 静默同意：告知见隐私政策与"设置-访问分析"开关，可随时关闭。
    // 只对"从未选择"或"此前同意但告知已更新"的用户生效；明确拒绝过的不覆盖。
    if (config.enabled===true && config.consent_version && config.privacy_notice) {
      const choice = consentChoice || safely(()=>wx.getStorageSync(CONSENT_CHOICE),null);
      if (!choice || (choice.accepted===true && (choice.version!==config.consent_version || choice.notice!==config.privacy_notice))) safely(()=>setConsent(true));
    }
    const s=getSDK(); if (s) s.configure(config.enabled===true, enabledConsent(), {awaitConsent:true});
    return config;
  }).catch(() => { config={enabled:false}; if (sdk) sdk.suspend(); return config; });
}
function setConsent(accepted) {
  accepted=accepted===true;
  if (accepted && (config.enabled!==true || !config.consent_version || !config.privacy_notice)) return false;
  consentRevision++; if(accepted!==true)entryInvalidation++;
  const previous=consentChoice || safely(()=>wx.getStorageSync(CONSENT_CHOICE),null) || safely(()=>wx.getStorageSync(CONSENT),null) || {};
  consentChoice={version:config.consent_version||previous.version||'',notice:config.privacy_notice||previous.notice||'',accepted:accepted===true,at:new Date().toISOString()};
  safely(()=>wx.setStorageSync(CONSENT_CHOICE,consentChoice));
  safely(() => accepted ? wx.setStorageSync(CONSENT,{version:config.consent_version,at:new Date().toISOString()}) : wx.removeStorageSync(CONSENT));
  const s=getSDK(); if (s) s.configure(config.enabled===true,accepted ? config.consent_version : ''); return true;
}
// A versioned, optional choice on every collectable public route. Page business
// lifecycle callbacks never await this promise; declining leaves reading/sharing intact.
function consentPublicPage(page) {
  const policy=routes.policy(page&&page.route);
  return policy.collect && !(policy.excludeSelf&&page.data&&page.data.isSelfHomepage);
}
function showConsentChoices(page, manual=false) {
  if(!manual && !consentPublicPage(page)) return Promise.resolve(false);
  if(consentRequest) return consentRequest;
  const request=refreshConfig().then(value=>{
    const target=manual?page:consentPage;
    // manual 由"我的"页主动触发，页面本身不是采集页，跳过采集页可见性校验。
    if(!target || (!manual&&(target.__trackingConsentVisible!==true||!consentPublicPage(target))))return false;
    if(value.enabled!==true || !value.consent_version || !value.privacy_notice || typeof wx.showModal!=='function')return false;
    const choice=consentChoice || safely(()=>wx.getStorageSync(CONSENT_CHOICE),null);
    if(!manual && (enabledConsent() || (choice&&choice.version===value.consent_version&&(choice.notice===value.privacy_notice||(!choice.notice&&choice.accepted===false)))))return false;
    const version=value.consent_version, notice=value.privacy_notice, revision=consentRevision;
    return new Promise(resolve=>{
      // Pass the complete configured notice, without truncation or implicit consent.
      const active = enabledConsent() === version;
      try { wx.showModal({title:'访问分析说明',
        content:notice+(active
          ?'\n\n当前状态：已开启。关闭后我们不再记录浏览数据，不影响阅读、分享或其他功能，可随时回来开启。'
          :'\n\n当前状态：未开启。开启后我们记录浏览与分享数据用于改进服务，可随时关闭，不影响任何功能。'),
        showCancel:true,confirmText:active?'保持开启':'开启分析',cancelText:active?'关闭分析':'保持关闭',
        success(result){
          if((!manual&&(target.__trackingConsentVisible!==true||!consentPublicPage(target))) || revision!==consentRevision || version!==config.consent_version || notice!==config.privacy_notice || config.enabled!==true){resolve(false);return;}
          if(result.confirm!==true && result.cancel!==true){resolve(false);return;}
          const accepted=result.confirm===true;
          setConsent(accepted);
          safely(()=>{if(target.data&&Object.prototype.hasOwnProperty.call(target.data,'consented'))target.setData({consented:accepted});});
          resolve(true);
        },fail(){resolve(false);}
      }); } catch (_) {resolve(false);}
    });
  }).catch(()=>false);
  consentRequest=request;
  request.then(()=>{if(consentRequest===request)consentRequest=null;});
  return request;
}
function consentPageVisible(page,visible) {
  page.__trackingConsentVisible=visible;
  // 静默模式：不再随页面展示自动弹窗，开关见"设置-访问分析"。
  if(visible){consentPage=page;}
  else if(consentPage===page)consentPage=null;
}
function splitPath(path, fallback) {
  const parts=String(path || ('/'+fallback)).split('?'); const query={};
  (parts[1]||'').split('&').filter(Boolean).forEach(pair => { const index=pair.indexOf('='); try {query[decodeURIComponent(index<0?pair:pair.slice(0,index))]=decodeURIComponent(index<0?'':pair.slice(index+1));}catch(_){} });
  return {route:parts[0].replace(/^\//,''),query};
}
function appendToken(value, token) {
  // Preserve business query byte-for-byte. Only replace this SDK's own tracking parameter.
  const cleaned=String(value||'').replace(/([?&])st=[^&]*(&?)/g,(_,lead,tail)=>tail?lead:'').replace(/[?&]$/,'');
  return cleaned+(cleaned.includes('?')?'&':'?')+'st='+encodeURIComponent(token);
}
async function prepareShare(page, original, channel, cacheKey, emitIntent = true) {
  const s=getSDK(), state=s && s.get(page);
  const accountAtStart=safely(()=>String(wx.getStorageSync('token')||''),'');
  if (!s || !s.diagnostics().enabled) return original;
  const sourceRoute=state ? state.route : String(page.route||'').replace(/^\//,'');
  const target=splitPath(channel==='timeline'?'/'+sourceRoute+'?'+(original.query||''):original.path,sourceRoute);
  if (!routes.policy(target.route).collect) return original;
  const content=routes.binding(target.route,target.query);
  let context=state && state.context;
  try {
    if(!context){if(!instance)instance=await anonymousInstance();context=await api.request('/entry','POST',{route_key:target.route,content_id:content.id,content_version:content.version,client_instance:instance,anonymous_id:instance,consent_version:enabledConsent()},undefined,true);}
    if(!context.entry_token)return original;
    let version=content.version || (state && state.route===target.route && state.content.id===content.id ? context.content_version : 0);
    if(content.id && !version){const targetEntry=await api.request('/entry','POST',{route_key:target.route,content_id:content.id,client_instance:instance,anonymous_id:instance,consent_version:enabledConsent()},undefined,true);version=targetEntry.content_version;}
    const share=await api.request('/shares','POST',{route_key:target.route,content_id:content.id,content_version:version,
      parent_share_id:context.share_id || undefined,channel,idempotency_key:id()},context.entry_token,true);
    if (!share.share_token || accountAtStart!==safely(()=>String(wx.getStorageSync('token')||''),'') || !s.diagnostics().enabled || (state && (s.get(page)!==state || state.context!==context))) return original;
    const cacheState=state || (page.__trackingShareState=page.__trackingShareState||{shareCache:{}});
    if(cacheKey) { cacheState.shareIds=cacheState.shareIds||{}; cacheState.shareIds[cacheKey]={id:share.share_id,context,route:state?state.route:target.route,content:state?state.content:content,expires:share.expires_at ? Date.parse(share.expires_at):Date.now()+300000}; }
    if(emitIntent)s.shareIntent(context,state?state.route:target.route,state?state.content:content,share.share_id,channel);
    return channel==='timeline' ? { ...original,query:appendToken('?'+(original.query||''),share.share_token).replace(/^\?/,'') } : {...original,path:appendToken(original.path||'/'+state.route,share.share_token)};
  } catch (_) { return original; }
}
// Timeline sharing has no async completion (unlike onShareAppMessage's promise),
// so the share-time wrapper cannot wait for token creation and the first tap used
// to go out un-attributed. Call this once page content is ready: it waits for the
// page entry to resolve (keeping parent_share_id linkage), then pre-creates the
// timeline share with a deterministic idempotency key — one share record per
// visitor per content, no share_intent until the user actually shares (the
// share-time cache-hit path emits it).
function preheatTimelineShare(page, original) {
  const s=getSDK();
  if (!s || !s.diagnostics().enabled || !original || typeof original!=='object') return;
  const state=s.get(page);
  if (!state) return;
  const key=JSON.stringify(original);
  const cached=state.shareCache && state.shareCache[key], meta=state.shareIds && state.shareIds[key];
  if(cached&&meta&&meta.expires>Date.now())return;
  const preheatKey='timeline-preheat:'+String(original.query||'');
  let done=false, stop=null;
  const run=()=>{
    if(done)return; done=true;
    if(stop)stop();
    prepareShare(page,original,'timeline',preheatKey,false).then(value=>{if(s.get(page)===state)state.shareCache[key]=value;});
  };
  stop=s.observe(page,()=>{
    const current=s.get(page);
    if(!current||current!==state){if(stop)stop();return;}
    // Token creation must wait for the page entry so the share chains to it.
    if(state.entryReady&&state.context)run();
  });
}
function updateReady(page) {
  const s=getSDK(); if (!s || !s.get(page)) return;
  s.ready(page,routes.ready(page.route,page.data||{},page.__trackingRendered),routes.binding(page.route,page.__trackingOptions||{}));
}
// 全页面默认分享：微信要求"发给朋友"（条件 1）与"朋友圈"（条件 2）成对开启，
// 未自定义分享回调的页面注入产品名 + 当前页路径的默认实现；已自定义的页面不受影响。
const DEFAULT_SHARE_TITLE='甲第灵光 · AI建房助手';
function queryString(options) {
  const pairs=Object.keys(options||{}).filter(key=>options[key]!=null&&options[key]!=='').map(key=>key+'='+encodeURIComponent(String(options[key])));
  return pairs.length?'?'+pairs.join('&'):'';
}
function wrapPage(definition) {
  const result={...definition};
  const invoke=(name,page,args)=>typeof definition[name]==='function'?definition[name].apply(page,args):undefined;
  result.onLoad=function(options) {
    safely(() => {
      this.__trackingOptions=options||{};
      const route=String(this.route||'').replace(/^\//,''); if (!routes.policy(route).collect) return;
      const s=getSDK(); if (!s) return; s.register(this,route,routes.binding(route,options||{}),(options||{}).st);
      const original=this.setData;
      this.setData=function(data,callback) {
        return original.call(this,data,()=>{ safely(()=>updateReady(this)); if(callback)callback.call(this); });
      };
    });
    return invoke('onLoad',this,arguments);
  };
  result.onReady=function() { const output=invoke('onReady',this,arguments); safely(()=>{this.__trackingRendered=true; updateReady(this);}); return output; };
  result.onShow=function() { const output=invoke('onShow',this,arguments); safely(()=>{const s=getSDK();if(s)s.show(this);updateReady(this);consentPageVisible(this,true);}); return output; };
  result.onHide=function() { safely(()=>{consentPageVisible(this,false);if(sdk)sdk.hide(this,false);}); return invoke('onHide',this,arguments); };
  result.onUnload=function() { safely(()=>{consentPageVisible(this,false);if(sdk)sdk.hide(this,true);}); return invoke('onUnload',this,arguments); };
  if (typeof definition.onShareAppMessage==='function') result.onShareAppMessage=function() {
    const original=invoke('onShareAppMessage',this,arguments);
    if (!original || typeof original!=='object' || !sdk || !sdk.diagnostics().enabled) return original;
    // Original callback executes once. Preserve original synchronous fallback, title/image and original promise semantics.
    return {...original,promise:Promise.resolve(original.promise || {}).then(value=>new Promise(resolve=>{
      const fallback={...original,...value}; const timeout=setTimeout(()=>resolve(fallback),1800);
      prepareShare(this,fallback,'app_message').then(result=>{clearTimeout(timeout);resolve(result);},()=>{clearTimeout(timeout);resolve(fallback);});
    })).catch(()=>original)};
  };
  if (typeof definition.onShareTimeline==='function') result.onShareTimeline=function() {
    const original=invoke('onShareTimeline',this,arguments);
    if (!original || !sdk || !sdk.diagnostics().enabled) return original;
    // Timeline has no reliable async completion. No invented delivery status; first call remains un-attributed.
    const state=sdk.get(this)||(this.__trackingShareState=this.__trackingShareState||{shareCache:{}}), key=JSON.stringify(original);
    const cached=state.shareCache[key], meta=state.shareIds&&state.shareIds[key];
    if(cached&&meta&&meta.expires>Date.now()){sdk.shareIntent(meta.context,meta.route,meta.content,meta.id,'timeline');return cached;}
    prepareShare(this,original,'timeline',key).then(value=>{if((sdk.get(this)||this.__trackingShareState)===state)state.shareCache[key]=value;});
    return original;
  };
  // 全页面开放右上角「转发给朋友」与「分享到朋友圈」（官方双条件成对开启）。
  // 默认标题用产品名，path/query 取当前页；素材页等已自定义分享的页面不受影响。
  if (typeof definition.onShareAppMessage!=='function') result.onShareAppMessage=function() {
    return { title:DEFAULT_SHARE_TITLE, path:'/'+String(this.route||'')+queryString(this.__trackingOptions) };
  };
  if (typeof definition.onShareTimeline!=='function') result.onShareTimeline=function() {
    return { title:DEFAULT_SHARE_TITLE, query:queryString(this.__trackingOptions).replace(/^\?/,'') };
  };
  // Preserve each business callback's receiver, arguments, return value and
  // exceptions. Analytics failures must never block an existing action.
  const actions=new Set([].concat(...Object.keys(routes.OBSERVED_ACTIONS).map(route=>routes.OBSERVED_ACTIONS[route])));
  actions.forEach(name=>{
    if(typeof definition[name]!=='function')return;
    result[name]=function(){
      safely(()=>{
        if(!sdk || !sdk.diagnostics().enabled)return;
        const action=routes.observedAction(this.route,name,this,arguments[0]);
        if(action)sdk.emit(this,action.type,action.extra);
      });
      return invoke(name,this,arguments);
    };
  });
  return result;
}
function install() {
  if (installed) return; installed=true;
  const NativePage=Page; Page=function(definition){return NativePage(wrapPage(definition));};
  const NativeApp=App; App=function(definition){
    const wrapped={...definition};
    ['onLaunch','onShow','onHide'].forEach(name=>{wrapped[name]=function(options){
      safely(()=>{
        const s=getSDK();
        if(name==='onLaunch') { refreshConfig(); if (!timer) {timer=setInterval(()=>safely(()=>sdk && sdk.tick()),HEARTBEAT);} if(wx.onNetworkStatusChange)wx.onNetworkStatusChange(status=>{if(status.isConnected&&sdk)sdk.flush();}); }
        if(name==='onShow'){
          if(options&&options.path==='pages/contentreader/contentreader'&&options.query&&(options.query.scene||options.query.content_id)&&typeof getCurrentPages==='function'){
            const pages=getCurrentPages(),page=pages[pages.length-1];
            if(page&&page.route===options.path&&typeof page.acceptExternalEntry==='function')page.acceptExternalEntry(options.query);
          }
          if(s)s.appShow(options);
        }
        if(name==='onHide'&&s)s.appHide();
      });
      return typeof definition[name]==='function'?definition[name].apply(this,arguments):undefined;
    };}); return NativeApp(wrapped);
  };
}
function resolvePageOptions(page, options, applyAttribution = true) {
 page.__trackingOptions=options;
 const s=getSDK(),p=s&&s.get(page);if(p&&applyAttribution){p.share=options.st||'';p.shareRevision=(p.shareRevision||0)+1;}
 updateReady(page);
}
function captureEntryValidity(){const revision=entryInvalidation,account=safely(()=>String(wx.getStorageSync('token')||''),'');return()=>revision===entryInvalidation&&account===safely(()=>String(wx.getStorageSync('token')||''),'');}
module.exports={observation:page=>safely(()=>getSDK().observation(page),null),observe:(page,listener)=>safely(()=>getSDK().observe(page,listener),()=>{}),captureEntryValidity,install,wrapPage,resolvePageOptions,getSDK,refreshConfig,setConsent,showConsentChoices:(page)=>showConsentChoices(page,true),prepareShare,preheatTimelineShare,appendToken,splitPath,config:()=>config,consented:()=>!!enabledConsent(),emit:(page,type,extra)=>safely(()=>sdk&&sdk.emit(page,type,extra)),block:(page,blocked)=>safely(()=>sdk&&sdk.block(page,blocked))};
export {};
