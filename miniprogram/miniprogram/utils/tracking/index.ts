// @ts-nocheck
'use strict';
const { createTracker, HEARTBEAT } = require('./core');
const api = require('./api');
const routes = require('./routes');
const STORAGE = 'share_tracking_queue_v1';
const CONSENT = 'share_tracking_consent_v1';
const CONSENT_CHOICE = 'share_tracking_consent_choice_v1';
// D6：关闭记录写入失败时记下意图，下次启动重放，避免重启后恢复采集。
const CONSENT_CLOSE_PENDING = 'share_tracking_consent_close_pending_v1';
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
    queueAccount: () => safely(() => String((wx.getStorageSync('userInfo') || {}).id || ''), ''),
    loadDiagnostics:()=>wx.getStorageSync(STORAGE+'_diagnostics'),saveDiagnostics:value=>wx.setStorageSync(STORAGE+'_diagnostics',value),
    load: () => wx.getStorageSync(STORAGE), save: queue => wx.setStorageSync(STORAGE, queue),
    async entry(body) { if (!instance) instance = await anonymousInstance(); return api.request('/entry','POST', { ...body, client_instance: instance, anonymous_id: instance }, undefined, true); },
    send: (body, credential) => api.request('/events/batch','POST',body,credential,true)
  });
  return sdk;
}
function currentConsentChoice() {
  if(config.enabled!==true || !config.consent_version || !config.privacy_notice) return null;
  const choice=consentChoice || safely(()=>wx.getStorageSync(CONSENT_CHOICE),null);
  if(!choice||choice.explicit!==true) return null;
  // 存量迁移规则（P7：CONSENT_VERSION 不升级）：明确关闭是撤回语义，version/notice 变化
  // 都不能把它变回「未选择」，否则默认开启会静默覆盖用户已关闭的选择。
  if(choice.accepted!==true) return choice;
  // 明确开启仍须匹配当前版本与完整告知，才算当前同意。
  return choice.version===config.consent_version&&choice.notice===config.privacy_notice ? choice : null;
}
// 读取存量选择：把「读取异常」与「确实没有记录」分开，异常时必须失败关闭，
// 不能把读不到当成未选择而默认开启（可能覆盖已明确关闭）。
function readStoredChoice() {
  try { return { ok: true, value: wx.getStorageSync(CONSENT_CHOICE) || null }; }
  catch (_) { return { ok: false, value: null }; }
}
// 访问分析默认开启：仅当 config 就绪、且用户从未明确选择时才静默 setConsent(true)。
// 已明确关闭（resolved+accepted=false）不得被覆盖；config.enabled=false 时不得启用。
function ensureDefaultConsent() {
  if (config.enabled !== true || !config.consent_version || !config.privacy_notice) return false;
  if (consentChoiceState().resolved === true) return false;
  // 用一次可靠读取快照做判断：读取失败立即退出；读到明确关闭必须保留，不能被覆盖。
  let stored = consentChoice;
  if (!stored) {
    const read = readStoredChoice();
    if (!read.ok) return false;
    stored = read.value;
  }
  if (stored && stored.explicit === true && stored.accepted !== true) return false;
  return setConsent(true, 'default');
}
// D6：补写上次失败的关闭记录；用户后来主动开启过则丢弃旧意图。
function replayPendingClose() {
  const pending = safely(() => wx.getStorageSync(CONSENT_CLOSE_PENDING), null);
  if (!pending) return;
  const current = currentConsentChoice();
  // 仅当用户后来主动开启过（source=user）才放弃关闭意图；旧的 default 记录不算。
  if (current && current.explicit === true && current.accepted === true && current.source === 'user') { safely(() => wx.removeStorageSync(CONSENT_CLOSE_PENDING)); return; }
  const record = { version: String(pending.version || config.consent_version || ''), notice: String(pending.notice || config.privacy_notice || ''), accepted: false, explicit: true, source: 'user', at: new Date().toISOString() };
  try {
    wx.setStorageSync(CONSENT_CHOICE, record);
    consentChoice = record;
    try { wx.removeStorageSync(CONSENT); } catch (_) { /* 次要标记失败不阻断 */ }
    safely(() => wx.removeStorageSync(CONSENT_CLOSE_PENDING));
    const s = getSDK(); if (s) s.configure(config.enabled === true, '');
  } catch (_) { /* 仍写失败则保留意图，下次再试 */ }
}
function enabledConsent() {
  const choice=currentConsentChoice();
  return choice&&choice.accepted===true ? config.consent_version : '';
}
function consentChoiceState() {
  const choice=currentConsentChoice();
  return {resolved:!!choice,accepted:!!(choice&&choice.accepted===true)};
}
let configSequence=0, appliedConfigSequence=0;
function refreshConfig() {
  // 请求序号：迟到的旧响应不得覆盖较新的配置，否则旧 enabled=true 会把已停用重新打开。
  const sequence=(configSequence+=1);
  return api.request('/config').then(value => {
    if(sequence<appliedConfigSequence)return config;
    appliedConfigSequence=sequence;
    config = value || {enabled:false}; if(config.enabled!==true)entryInvalidation++;
    const s=getSDK(); if (s) s.configure(config.enabled===true, enabledConsent(), {awaitConsent:true});
    // D6：上次关闭写失败的重放必须先于默认开启，否则会先被默认开启覆盖。
    if (config.enabled === true) replayPendingClose();
    // 访问分析默认开启（T7.1）：只在「从未明确选择」时静默记录；不做每次启动强制开启。
    if (config.enabled === true) ensureDefaultConsent();
    return config;
  }).catch(() => {
    if(sequence<appliedConfigSequence)return config;
    appliedConfigSequence=sequence;
    config={enabled:false,configError:true}; if (sdk) sdk.suspend(); return config;
  });
}
function setConsent(accepted, source) {
  accepted=accepted===true;
  if (accepted && (config.enabled!==true || !config.consent_version || !config.privacy_notice)) return false;
  consentRevision++; if(accepted!==true)entryInvalidation++;
  const previous=consentChoice || safely(()=>wx.getStorageSync(CONSENT_CHOICE),null) || safely(()=>wx.getStorageSync(CONSENT),null) || {};
  // source 区分「用户主动选择」与「默认开启迁移」，便于后台核查来源；关闭永远视为用户主动。
  const origin=accepted===true ? (source==='default' ? 'default' : 'user') : 'user';
  const record={version:config.consent_version||previous.version||'',notice:config.privacy_notice||previous.notice||'',accepted:accepted===true,explicit:true,source:origin,at:new Date().toISOString()};
  // 主选择记录写成功后才更新内存并开启 SDK；写入失败保持未决，避免「没落盘却已采集」。
  try { wx.setStorageSync(CONSENT_CHOICE,record); }
  catch (_) {
    if (accepted !== true) {
      // 关闭写失败：先停止运行时采集并保留本会话的关闭意图，调用方仍会收到 false 以便提示未保存。
      // 同时记录待重放意图，下次启动（refreshConfig 成功）优先补写关闭记录。
      safely(() => wx.setStorageSync(CONSENT_CLOSE_PENDING, { version: record.version, notice: record.notice, at: record.at }));
      consentChoice=record;
      const s=getSDK(); if (s) s.configure(false, '');
      if (sdk) sdk.suspend();
      if (typeof wx.showToast === 'function') wx.showToast({ title: '关闭暂未保存成功，本次会话已停止记录', icon: 'none' });
    }
    return false;
  }
  consentChoice=record;
  try { accepted ? wx.setStorageSync(CONSENT,{version:config.consent_version,at:new Date().toISOString()}) : wx.removeStorageSync(CONSENT); }
  catch (_) { /* 主记录已成功，次要标记失败不阻断 */ }
  const s=getSDK(); if (s) s.configure(config.enabled===true,accepted ? config.consent_version : ''); return true;
}
// A versioned, optional choice on every collectable public route. Page business
// lifecycle callbacks never await this promise; declining leaves reading/sharing intact.
function consentPublicPage(page) {
  const policy=routes.policy(page&&page.route);
  return policy.collect && !(policy.excludeSelf&&page.data&&page.data.isSelfHomepage);
}
// 统一同步页面上的同意状态：所有弹窗出口（确认/取消/异常/失败）都走这里，
// 避免存储恢复或告知刷新后页面仍停留在旧的 consented / pending / notice。
function syncConsentPageState(target) {
  if (!target || !target.data) return;
  const available = config.enabled === true && !!config.consent_version && !!config.privacy_notice;
  const unavailable = !available && (config.configError === true || config.enabled === true);
  const actual = consentChoiceState();
  safely(() => {
    const update = {};
    if (Object.prototype.hasOwnProperty.call(target.data, 'consented')) update.consented = actual.accepted;
    if (Object.prototype.hasOwnProperty.call(target.data, 'consentAvailable')) update.consentAvailable = available;
    if (Object.prototype.hasOwnProperty.call(target.data, 'consentChoicePending')) update.consentChoicePending = available && !actual.resolved;
    if (Object.prototype.hasOwnProperty.call(target.data, 'consentUnavailable')) update.consentUnavailable = unavailable;
    if (Object.prototype.hasOwnProperty.call(target.data, 'consentNotice')) update.consentNotice = String(config.privacy_notice || '');
    if (Object.keys(update).length) target.setData(update);
  });
}
function showConsentChoices(page, manual=false) {
  if(!manual && !consentPublicPage(page)) return Promise.resolve(false);
  if(consentRequest) return consentRequest;
  const request=refreshConfig().then(value=>{
    const target=manual?page:consentPage;
    // manual 由"我的"页主动触发，页面本身不是采集页，跳过采集页可见性校验。
    if(!target || (!manual&&(target.__trackingConsentVisible!==true||!consentPublicPage(target))))return false;
    // 配置失效（停用/缺字段/请求失败）也要把页面同步到最新状态，避免保留旧的询问块。
    syncConsentPageState(target);
    if(value.enabled!==true || !value.consent_version || !value.privacy_notice || typeof wx.showModal!=='function')return false;
    if(!manual && consentChoiceState().resolved)return false;
    const version=value.consent_version, notice=value.privacy_notice, revision=consentRevision;
    // 并发刷新可能已应用更新配置：配置快照不一致时不再打开过期弹窗。
    if(config.enabled!==true||config.consent_version!==version||config.privacy_notice!==notice){syncConsentPageState(target);return false;}
    return new Promise(resolve=>{
      // Pass the complete configured notice, without truncation or implicit consent.
      const active = enabledConsent() === version;
      const choiceState = consentChoiceState();
      // 三态：已开启 / 用户明确关闭 / 未决（默认开启尚未写入成功）。不能把未决说成用户关闭。
      const statusCopy = choiceState.resolved
        ? (choiceState.accepted ? '\n\n当前状态：已开启。关闭后不再记录浏览数据，不影响阅读、分享或其他功能。' : '\n\n当前状态：已关闭（你已选择关闭）。开启后将恢复记录，可随时关闭，不影响任何功能。')
        : '\n\n当前状态：未设置（默认开启尚未生效）。可在此手动开启或保持关闭，不影响阅读、分享或其他功能。';
      try { wx.showModal({title:'访问分析说明',
        content:notice+'\n\n登录后，已获访问权限的管理人员可见账号昵称、头像及访问记录；不公开手机号或输入正文。我们默认记录访问与分享数据用于改进服务，可在设置中随时关闭，关闭后不再记录。'+statusCopy,
        showCancel:true,confirmText:active?'保持开启':'开启分析',cancelText:active?'关闭分析':'保持关闭',
        success(result){
          if(result.confirm!==true && result.cancel!==true){syncConsentPageState(target);resolve(false);return;}
          const accepted=result.confirm===true;
          // 明确关闭永远优先：即使弹窗期间告知/版本已变化，也不能让「关闭」被过期 guard 吞掉。
          if(!accepted){
            const closed=setConsent(false);
            syncConsentPageState(target);
            resolve(closed);
            return;
          }
          if((!manual&&(target.__trackingConsentVisible!==true||!consentPublicPage(target))) || revision!==consentRevision || version!==config.consent_version || notice!==config.privacy_notice || config.enabled!==true){syncConsentPageState(target);resolve(false);return;}
          const ok=setConsent(accepted);
          if(!ok&&accepted===true&&typeof wx.showToast==='function')wx.showToast({title:'开启暂未保存成功，请稍后重试',icon:'none'});
          // 页面状态从真实选择派生：写入失败时不得显示成功（关闭失败已在本会话停止采集）；
          // 存储恢复后保存成功也要同步清掉「未决提示」，否则仍显示询问块。
          syncConsentPageState(target);
          resolve(ok);
        },fail(){syncConsentPageState(target);resolve(false);}
      }); } catch (_) {resolve(false);}
    });
  }).catch(()=>false);
  consentRequest=request;
  request.then(()=>{if(consentRequest===request)consentRequest=null;});
  return request;
}
function consentPageVisible(page,visible) {
  page.__trackingConsentVisible=visible;
  // Reading stays available without choosing. Explicit choices live in Settings.
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
  const original=String(value||''),question=original.indexOf('?');
  const path=question<0?original:original.slice(0,question);
  const query=question<0?'':original.slice(question+1).split('&').filter(part=>{
    if(!part)return false;
    try {const key=decodeURIComponent(part.split('=')[0]);return key!=='st'&&key!=='ht';}catch(_){return true;}
  }).join('&');
  const cleaned=path+(query?'?'+query:'');
  return cleaned+(cleaned.includes('?')?'&':'?')+'st='+encodeURIComponent(token);
}
async function prepareShare(page, original, channel, cacheKey, emitIntent = true, isCurrent = () => true) {
  const s=getSDK();
  if(s)s.observation(page);
  const state=s && s.get(page);
  const accountAtStart=safely(()=>String(wx.getStorageSync('token')||''),'');
  const validityAtStart=entryInvalidation;
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
      parent_share_id:context.share_id || undefined,channel,prepared:emitIntent===false,idempotency_key:id()},context.entry_token,true);
    if (!isCurrent() || !share.share_token || validityAtStart!==entryInvalidation || accountAtStart!==safely(()=>String(wx.getStorageSync('token')||''),'') || !s.diagnostics().enabled || (state && (s.get(page)!==state || state.context!==context))) return original;
    const cacheState=state || (page.__trackingShareState=page.__trackingShareState||{shareCache:{}});
    if(cacheKey) { cacheState.shareIds=cacheState.shareIds||{}; cacheState.shareIds[cacheKey]={id:share.share_id,context,route:state?state.route:target.route,content:state?state.content:content,expires:share.expires_at ? Date.parse(share.expires_at):Date.now()+300000,account:accountAtStart,validity:validityAtStart,view:state&&state.view}; }
    if(emitIntent)s.shareIntent(context,state?state.route:target.route,state?state.content:content,share.share_id,channel);
    return channel==='timeline' ? { ...original,query:appendToken('?'+(original.query||''),share.share_token).replace(/^\?/,'') } : {...original,path:appendToken(original.path||'/'+state.route,share.share_token)};
  } catch (error) {
    if(state)state.shareFailure={stage:'share_prepare',statusCode:Number(error&&error.statusCode)||0,code:error&&error.code||'unavailable',networkError:!!(error&&error.networkError)};
    return original;
  }
}
// Confirm an actual copy gesture using its prepared Share; never mint a second
// Share merely to record the action. Account/context changes invalidate it.
function confirmPreparedShare(page, key, channel) {
  return safely(() => {
    const s = getSDK();
    if (!s || !s.diagnostics().enabled) return false;
    s.observation(page);
    const state = s.get(page), meta = state && state.shareIds && state.shareIds[key];
    if (!meta || meta.context !== state.context || meta.view !== state.view || meta.expires <= Date.now() ||
        meta.account !== String(wx.getStorageSync('token') || '') || meta.validity !== entryInvalidation) return false;
    if (!meta.intentConfirmed) {
      s.shareIntent(meta.context, meta.route, meta.content, meta.id, channel);
      meta.intentConfirmed = true;
    }
    return true;
  }, false);
}
// A timeline callback must return synchronously. Prepare at page readiness,
// never by pretending its return value can await a network request.
function timelineCacheKey(original) { return JSON.stringify(original); }
function cachedTimeline(state,key) {
  const value=state&&state.shareCache&&state.shareCache[key],meta=state&&state.shareIds&&state.shareIds[key];
  return value&&meta&&meta.expires>Date.now()&&meta.context===state.context&&meta.view===state.view&&
    meta.account===safely(()=>String(wx.getStorageSync('token')||''),'')&&meta.validity===entryInvalidation ? value:null;
}
function preheatTimelineShare(page, original) {
  const s=getSDK();
  // Observe current identity before reusing a credential, even if login changed
  // storage between SDK heartbeats. Core boundaries are safe under observers.
  if(s)s.observation(page);
  const state=s&&s.get(page);
  if(!s||!s.diagnostics().enabled||!state||page.__trackingConsentVisible===false||!original||typeof original!=='object'||!Object.keys(original).length)return Promise.resolve(null);
  const key=timelineCacheKey(original),account=safely(()=>String(wx.getStorageSync('token')||''),''),validity=entryInvalidation;
  const binding=JSON.stringify(state.content),cached=cachedTimeline(state,key);
  state.timelineKey=key;
  if(cached)return Promise.resolve(cached);
  state.sharePending=state.sharePending||{};
  const pending=state.sharePending[key];
  if(pending&&pending.account===account&&pending.validity===validity&&pending.binding===binding)return pending.promise;
  const work={account,validity,binding,promise:null};
  state.timelineStatus='pending';
  let resolveWork;
  work.promise=new Promise(resolve=>{resolveWork=resolve;});
  // observe() invokes its listener synchronously; publish the in-flight work
  // before subscribing so nested lifecycle observations reuse this request.
  state.sharePending[key]=work;
  {
    let finished=false,started=false,stop=()=>{};
    const finish=(value,reason)=>{
      if(finished)return;finished=true;clearTimeout(timeout);stop();
      const current=state.sharePending[key]===work;
      if(current)delete state.sharePending[key];
      if(current&&s.get(page)===state&&state.timelineKey===key){
        state.timelineStatus=value?'ready':'fallback';
        if(value)state.shareFailure=null;
        else if(reason)state.shareFailure={stage:'timeline_prepare',statusCode:0,code:reason};
      }
      resolveWork(value);
    };
    work.cancel=()=>finish(null,'context_changed');
    // A failed or slow analytics service must leave ordinary sharing available.
    const timeout=setTimeout(()=>finish(null,'prepare_timeout'),1800);
    const run=()=>{
      if(state.sharePending[key]!==work||s.get(page)!==state||state.timelineKey!==key||validity!==entryInvalidation||account!==safely(()=>String(wx.getStorageSync('token')||''),'')||binding!==JSON.stringify(state.content)||page.__trackingConsentVisible===false){finish(null,'context_changed');return;}
      if(started||!state.entryReady||!state.context||!state.ready||!state.view||state.start==null)return;
      started=true;stop();
      const context=state.context,view=state.view;
      if(state.shareIds)delete state.shareIds[key];
      prepareShare(page,original,'timeline',key,false,()=>state.sharePending[key]===work&&state.timelineKey===key&&state.context===context&&state.view===view).then(value=>{
        if(state.sharePending[key]===work&&s.get(page)===state&&state.timelineKey===key&&state.context===context&&state.view===view){
          state.shareCache[key]=value;
          if(cachedTimeline(state,key)){finish(value);return;}
          delete state.shareCache[key];finish(null,state.shareFailure?null:'context_changed');
        }else finish(null,state.shareFailure?null:'context_changed');
      },()=>finish(null,'unavailable'));
    };
    stop=s.observe(page,run);
    if(started||finished)stop();
  }
  return work.promise;
}
function updateReady(page) {
  const s=getSDK(); if (!s || !s.get(page)) return;
  s.ready(page,routes.ready(page.route,page.data||{},page.__trackingRendered),routes.binding(page.route,page.__trackingOptions||{}));
}
const DEFAULT_SHARE_TITLE='甲第灵光 · AI建房助手';
function publicShareQuery(page) {
  const policy=routes.policy(String(page.route||'').replace(/^\//,''));
  if(!policy.collect||!policy.idKey)return '';
  const options=page.__trackingOptions||{};
  const value=options[policy.idKey]??options[policy.idAlias];
  return value==null||value===''?'':policy.idKey+'='+encodeURIComponent(String(value));
}
function timelineShareValue(value) {
  if(!value||typeof value!=='object'||!Object.keys(value).length||Object.prototype.hasOwnProperty.call(value,'query'))return value;
  // An omitted query inherits the current page query in WeChat. Business pages
  // must opt in to each safe parameter instead of exposing all launch options.
  return {...value,query:''};
}
const PLAIN_SHARE_ROUTES=new Set(['pages/contentreader/contentreader','pages/collectionreader/collectionreader']);
function plainShareMode(route,options) {
  return PLAIN_SHARE_ROUTES.has(String(route||'').replace(/^\//,''))&&String(options&&options.untracked_share||'')==='1';
}
function showTimelineShareMenu(page, hasCustomTimelineShare) {
  const launch=safely(()=>typeof wx.getLaunchOptionsSync==='function'?wx.getLaunchOptionsSync():null,null);
  if(launch&&launch.scene===1154)return;
  const contentBlocked=page.data&&Object.prototype.hasOwnProperty.call(page.data,'contentReady')&&page.data.contentReady!==true;
  const route=String(page.route||'').replace(/^\//,'');
  if(contentBlocked||(!routes.policy(route).collect&&!hasCustomTimelineShare)){
    if(typeof wx.hideShareMenu==='function')wx.hideShareMenu({menus:contentBlocked?['shareAppMessage','shareTimeline']:['shareTimeline']});
    return;
  }
  const state=sdk&&sdk.get(page);
  if(sdk&&sdk.diagnostics().enabled&&state&&state.timelineStatus==='pending'){
    if(typeof wx.showShareMenu==='function')wx.showShareMenu({menus:['shareAppMessage']});
    if(typeof wx.hideShareMenu==='function')wx.hideShareMenu({menus:['shareTimeline']});
    return;
  }
  if(typeof wx.showShareMenu==='function')wx.showShareMenu({menus:['shareAppMessage','shareTimeline'],fail(error){console.warn('分享菜单开启失败',page.route,error);}});
}
function wrapPage(definition) {
  const result={...definition};
  const hasCustomAppShare=typeof definition.onShareAppMessage==='function';
  const hasCustomTimelineShare=typeof definition.onShareTimeline==='function';
  const invoke=(name,page,args)=>typeof definition[name]==='function'?definition[name].apply(page,args):undefined;
  const defaultTimeline=page=>{
    const route=String(page.route||'').replace(/^\//,'');
    return routes.policy(route).collect?{title:DEFAULT_SHARE_TITLE,query:publicShareQuery(page)}:{};
  };
  const clearTimelineTimer=page=>{clearTimeout(page.__trackingTimelineTimer);page.__trackingTimelineTimer=null;};
  const cancelTimeline=page=>{
    clearTimelineTimer(page);
    const state=sdk&&sdk.get(page);
    if(state&&state.sharePending)Object.values(state.sharePending).forEach(work=>{if(work.cancel)work.cancel();});
  };
  const warmTimeline=page=>{
    const s=getSDK(),state=s&&s.get(page);
    clearTimelineTimer(page);
    if(!state||!state.ready||page.__trackingConsentVisible!==true||!s.diagnostics().enabled)return;
    // Existing custom callbacks may have side effects. Only pure payload
    // getters and the SDK's own default can run before the share gesture.
    const original=typeof definition.getTimelineSharePayload==='function'
      ? timelineShareValue(invoke('getTimelineSharePayload',page,[]))
      : !hasCustomTimelineShare?defaultTimeline(page):null;
    if(!original||!Object.keys(original).length)return;
    const key=timelineCacheKey(original);
    preheatTimelineShare(page,original).then(value=>{
      if(page.__trackingConsentVisible!==true||s.get(page)!==state||state.timelineKey!==key)return;
      showTimelineShareMenu(page,hasCustomTimelineShare);
      const meta=state.shareIds&&state.shareIds[key];
      if(!value||!cachedTimeline(state,key)||!meta)return;
      // Renew before expiry while the page stays open. A native timeline
      // callback cannot wait for a replacement token after it has expired.
      clearTimelineTimer(page);
      const remaining=meta.expires-Date.now(),renewalAt=meta.expires-Math.min(1000,remaining/10);
      // Share tokens can last 30 days, beyond common 32-bit native timer limits.
      // A bounded check reuses the token until renewal is actually due.
      page.__trackingTimelineTimer=setTimeout(()=>{
        if(page.__trackingConsentVisible!==true||s.get(page)!==state||state.timelineKey!==key)return;
        if(state.shareIds[key]===meta&&Date.now()>=renewalAt){delete state.shareCache[key];delete state.shareIds[key];}
        warmTimeline(page);
      },Math.max(1,Math.min(24*60*60*1000,renewalAt-Date.now())));
      // Node contract harnesses may omit native page destruction. WeChat uses
      // numeric timer handles; a host timer must not keep a finished run alive.
      if(page.__trackingTimelineTimer&&typeof page.__trackingTimelineTimer.unref==='function')page.__trackingTimelineTimer.unref();
    });
    showTimelineShareMenu(page,hasCustomTimelineShare);
  };
  result.onLoad=function(options) {
    safely(() => {
      this.__trackingOptions=options||{};
      const route=String(this.route||'').replace(/^\//,'');
      this.__trackingPlainShare=plainShareMode(route,options);
      if (routes.policy(route).collect&&!this.__trackingPlainShare) {
        const s=getSDK(); if (s) {
          s.register(this,route,routes.binding(route,options||{}),(options||{}).st,(options||{}).ht);
          this.__trackingTimelineStop=s.observe(this,()=>{if(this.__trackingConsentVisible===true){safely(()=>warmTimeline(this));showTimelineShareMenu(this,hasCustomTimelineShare);}});
        }
      } else if (!hasCustomTimelineShare) return;
      const original=this.setData;
      if(typeof original!=='function')return;
      this.setData=function(data,callback) {
        return original.call(this,data,()=>{ safely(()=>{
          // Sharing readiness must work even when analytics is unavailable.
          updateReady(this);
          warmTimeline(this);
          if(Object.prototype.hasOwnProperty.call(data,'contentReady')&&this.__trackingConsentVisible===true)showTimelineShareMenu(this,hasCustomTimelineShare);
        }); if(callback)callback.call(this); });
      };
    });
    return invoke('onLoad',this,arguments);
  };
  result.onReady=function() { const output=invoke('onReady',this,arguments); safely(()=>{this.__trackingRendered=true; updateReady(this);warmTimeline(this);if(this.__trackingConsentVisible===true)showTimelineShareMenu(this,hasCustomTimelineShare);}); return output; };
  result.onShow=function() { const output=invoke('onShow',this,arguments); safely(()=>{consentPageVisible(this,true);const s=getSDK();if(s)s.show(this);updateReady(this);warmTimeline(this);showTimelineShareMenu(this,hasCustomTimelineShare);}); return output; };
  result.onHide=function() { safely(()=>{consentPageVisible(this,false);cancelTimeline(this);if(sdk)sdk.hide(this,false);}); return invoke('onHide',this,arguments); };
  result.onUnload=function() { safely(()=>{consentPageVisible(this,false);cancelTimeline(this);if(this.__trackingTimelineStop)this.__trackingTimelineStop();if(sdk)sdk.hide(this,true);}); return invoke('onUnload',this,arguments); };
  result.onShareAppMessage=function() {
    const route=String(this.route||'').replace(/^\//,'');
    const query=publicShareQuery(this);
    const original=hasCustomAppShare?invoke('onShareAppMessage',this,arguments):{
      title:DEFAULT_SHARE_TITLE,path:routes.policy(route).collect?'/'+route+(query?'?'+query:''):'/pages/index/index'
    };
    if (!original || typeof original!=='object' || this.__trackingPlainShare || !sdk || !sdk.diagnostics().enabled) return original;
    // Original callback executes once. Preserve original synchronous fallback, title/image and original promise semantics.
    return {...original,promise:Promise.resolve(original.promise || {}).then(value=>new Promise(resolve=>{
      const fallback={...original,...value}; const timeout=setTimeout(()=>resolve(fallback),1800);
      prepareShare(this,fallback,'app_message').then(result=>{clearTimeout(timeout);resolve(result);},()=>{clearTimeout(timeout);resolve(fallback);});
    })).catch(()=>original)};
  };
  result.onShareTimeline=function() {
    const original=hasCustomTimelineShare?timelineShareValue(invoke('onShareTimeline',this,arguments)):defaultTimeline(this);
    if(!original||this.__trackingPlainShare||!sdk||!sdk.diagnostics().enabled)return original;
    const state=sdk.get(this),key=timelineCacheKey(original),cached=cachedTimeline(state,key);
    if(cached){const meta=state.shareIds[key];sdk.shareIntent(meta.context,meta.route,meta.content,meta.id,'timeline');return cached;}
    // Normally the native menu opens after preheating. Programmatic calls,
    // expirations and service failures retain the original safe share payload.
    preheatTimelineShare(this,original);
    return original;
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
          if(options&&options.path&&options.query&&typeof getCurrentPages==='function'){
            const pages=getCurrentPages(),page=pages[pages.length-1];
            if(page&&page.route===options.path){
              if(plainShareMode(options.path,options.query)){
                const current=s&&s.get(page);if(current)s.hide(page,true);
                page.__trackingPlainShare=true;
              }
              if(typeof page.queueProfileEntry==='function')page.queueProfileEntry(options.query);
              else if(typeof page.acceptExternalEntry==='function')page.acceptExternalEntry(options.query);
            }
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
 const route=String(page&&page.route||'').replace(/^\//,'');
 const plain=plainShareMode(route,options),s=getSDK();
 let p=s&&s.get(page);
 if(plain){if(p)s.hide(page,true);page.__trackingPlainShare=true;p=null;}
 else {
  page.__trackingPlainShare=false;
  if(s&&routes.policy(route).collect&&!p){p=s.register(page,route,routes.binding(route,options||{}),(options||{}).st,(options||{}).ht);if(page.__trackingConsentVisible===true)s.show(page);}
 }
 if(p&&applyAttribution){p.share=options.st||'';p.handoff=p.share?options.ht||'':'';p.shareRevision=(p.shareRevision||0)+1;}
 updateReady(page);
}
function shareDiagnostics(page){
  const state=sdk&&sdk.get(page);if(!state)return null;
  const status=state.context&&state.context.attribution_status;
  return ['share','unattributed','invalid_share','expired_share','revoked_share','mismatched_share'].includes(status)
    ? {...(state.shareFailure||{}),attribution_status:status} : state.shareFailure||null;
}
function captureEntryValidity(){const revision=entryInvalidation,account=safely(()=>String(wx.getStorageSync('token')||''),'');return()=>revision===entryInvalidation&&account===safely(()=>String(wx.getStorageSync('token')||''),'');}
module.exports={ensureDefaultConsent,collectionToken:page=>safely(()=>getSDK().collectionToken(page),''),observation:page=>safely(()=>getSDK().observation(page),null),observe:(page,listener)=>safely(()=>getSDK().observe(page,listener),()=>{}),captureEntryValidity,install,wrapPage,resolvePageOptions,getSDK,refreshConfig,setConsent,showConsentChoices:(page)=>showConsentChoices(page,true),prepareShare,confirmPreparedShare,preheatTimelineShare,shareDiagnostics:page=>safely(()=>shareDiagnostics(page),null),appendToken,splitPath,config:()=>config,consentChoice:consentChoiceState,consented:()=>!!enabledConsent(),emit:(page,type,extra)=>safely(()=>sdk&&sdk.emit(page,type,extra)),block:(page,blocked)=>safely(()=>sdk&&sdk.block(page,blocked))};
export {};
