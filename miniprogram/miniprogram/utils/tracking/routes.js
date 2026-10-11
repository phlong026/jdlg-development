'use strict';
// Generated from fixed-baseline app.json; see docs/route-inventory.md.
const ROUTES = {
  "pages/index/index": {
    "kind": "home",
    "readiness": "rendered",
    "collect": true
  },
  "pages/customerservice/customerservice": {
    "kind": "excluded",
    "readiness": "excluded-private-conversation",
    "collect": false,
    "exclusionReason": "private-conversation"
  },
  "pages/my/my": {
    "kind": "excluded",
    "readiness": "excluded-private-account",
    "collect": false,
    "exclusionReason": "private-account"
  },
  "pages/template/template": {
    "kind": "list",
    "readiness": "explicit",
    "collect": true
  },
  "pages/aitools/aitools": {
    "kind": "catalog",
    "readiness": "rendered",
    "collect": true
  },
  "pages/aitooldetail/aitooldetail": {
    "kind": "detail",
    "readiness": "nonempty:tool",
    "field": "tool",
    "collect": true,
    "idKey": "id"
  },
  "pages/aitoolworkbench/aitoolworkbench": {
    "kind": "form",
    "readiness": "nonempty:tool",
    "field": "tool",
    "collect": true,
    "idKey": "id"
  },
  "pages/specadvisor/specadvisor": {
    "kind": "excluded",
    "readiness": "excluded-private-conversation",
    "collect": false,
    "exclusionReason": "private-conversation"
  },
  "pages/buildingcodedocs/buildingcodedocs": {
    "kind": "excluded",
    "readiness": "excluded-private-documents",
    "collect": false,
    "exclusionReason": "private-documents"
  },
  "pages/newminnantool/newminnantool": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/distribution/distribution": {
    "kind": "list",
    "readiness": "explicit",
    "collect": true
  },
  "pages/voicecall/voicecall": {
    "kind": "excluded",
    "readiness": "excluded-private-conversation",
    "collect": false,
    "exclusionReason": "private-conversation"
  },
  "pages/login/login": {
    "kind": "excluded",
    "readiness": "excluded-private-account",
    "collect": false,
    "exclusionReason": "private-account"
  },
  "pages/passwordrecovery/passwordrecovery": {
    "kind": "excluded",
    "readiness": "excluded-private-account",
    "collect": false,
    "exclusionReason": "private-account"
  },
  "pages/phonebind/phonebind": {
    "kind": "excluded",
    "readiness": "excluded-private-account",
    "collect": false,
    "exclusionReason": "private-account"
  },
  "pages/privacy/privacy": {
    "kind": "excluded",
    "readiness": "excluded-privacy-consent",
    "collect": false,
    "exclusionReason": "privacy-consent"
  },
  "pages/aigenerate/aigenerate": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/generateprogress/generateprogress": {
    "kind": "excluded",
    "readiness": "excluded-private-generated-content",
    "collect": false,
    "exclusionReason": "private-generated-content"
  },
  "pages/inspirationdetail/inspirationdetail": {
    "kind": "detail",
    "readiness": "nonempty:detail",
    "field": "detail",
    "collect": true,
    "idKey": "id"
  },
  "pages/templatepreview/templatepreview": {
    "kind": "excluded",
    "readiness": "excluded-private-generated-content",
    "collect": false,
    "exclusionReason": "private-generated-content"
  },
  "pages/settings/settings": {
    "kind": "excluded",
    "readiness": "excluded-private-account",
    "collect": false,
    "exclusionReason": "private-account"
  },
  "pages/aicost/aicost": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/aicostresult/aicostresult": {
    "kind": "excluded",
    "readiness": "excluded-private-estimate",
    "collect": false,
    "exclusionReason": "private-estimate"
  },
  "pages/utilitytools/utilitytools": {
    "kind": "list",
    "readiness": "explicit",
    "collect": true
  },
  "pages/utilitytooldetail/utilitytooldetail": {
    "kind": "detail",
    "readiness": "nonempty:tool",
    "field": "tool",
    "collect": true,
    "idKey": "id"
  },
  "pages/aivideo/aivideo": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/real/real": {
    "kind": "excluded",
    "readiness": "excluded-private-identity",
    "collect": false,
    "exclusionReason": "private-identity"
  },
  "pages/report/landing": {
    "kind": "landing",
    "readiness": "nonempty:enabled",
    "field": "enabled",
    "collect": true
  },
  "pages/report/questionnaire": {
    "kind": "excluded",
    "readiness": "excluded-private-report",
    "collect": false,
    "exclusionReason": "private-report"
  },
  "pages/report/preview": {
    "kind": "excluded",
    "readiness": "excluded-private-report",
    "collect": false,
    "exclusionReason": "private-report"
  },
  "pages/report/delivery": {
    "kind": "excluded",
    "readiness": "excluded-private-report",
    "collect": false,
    "exclusionReason": "private-report"
  },
  "pages/generatedetails/generatedetails": {
    "kind": "excluded",
    "readiness": "excluded-private-generated-content",
    "collect": false,
    "exclusionReason": "private-generated-content"
  },
  "pages/Invitefriends/Invitefriends": {
    "kind": "excluded",
    "readiness": "excluded-private-rewards",
    "collect": false,
    "exclusionReason": "private-rewards"
  },
  "pages/chat/chat": {
    "kind": "excluded",
    "readiness": "excluded-private-conversation",
    "collect": false,
    "exclusionReason": "private-conversation"
  },
  "pages/designerhome/designerhome": {
    "kind": "detail",
    "readiness": "explicit",
    "collect": true,
    "idKey": "userId",
    "idAlias": "id",
    "excludeSelf": true
  },
  "pages/myInformationmodification/myInformationmodification": {
    "kind": "excluded",
    "readiness": "excluded-private-account",
    "collect": false,
    "exclusionReason": "private-account"
  },
  "pages/templatesquaredetails/templatesquaredetails": {
    "kind": "detail",
    "readiness": "nonempty:template",
    "field": "template",
    "collect": true,
    "idKey": "id"
  },
  "pages/myplan/myplan": {
    "kind": "excluded",
    "readiness": "excluded-private-generated-content",
    "collect": false,
    "exclusionReason": "private-generated-content"
  },
  "pages/myfavorites/myfavorites": {
    "kind": "excluded",
    "readiness": "excluded-private-generated-content",
    "collect": false,
    "exclusionReason": "private-generated-content"
  },
  "pages/ordermanagement/ordermanagement": {
    "kind": "excluded",
    "readiness": "excluded-private-finance",
    "collect": false,
    "exclusionReason": "private-finance"
  },
  "pages/wallet/wallet": {
    "kind": "excluded",
    "readiness": "excluded-private-finance",
    "collect": false,
    "exclusionReason": "private-finance"
  },
  "pages/ruralvilladesign/ruralvilladesign": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/Parentchildcreativity/Parentchildcreativity": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/topupcenter/topupcenter": {
    "kind": "excluded",
    "readiness": "excluded-private-finance",
    "collect": false,
    "exclusionReason": "private-finance"
  },
  "pages/allrounddesign/allrounddesign": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/release/release": {
    "kind": "excluded",
    "readiness": "excluded-private-publishing",
    "collect": false,
    "exclusionReason": "private-publishing"
  },
  "pages/release/designerworkpublish": {
    "kind": "excluded",
    "readiness": "excluded-private-publishing",
    "collect": false,
    "exclusionReason": "private-publishing"
  },
  "pages/buildingreplacement/buildingreplacement": {
    "kind": "form",
    "readiness": "rendered",
    "collect": true
  },
  "pages/checkin/checkin": {
    "kind": "excluded",
    "readiness": "excluded-private-rewards",
    "collect": false,
    "exclusionReason": "private-rewards"
  },
  "pages/Identityauthen/Identityauthen": {
    "kind": "excluded",
    "readiness": "excluded-private-identity",
    "collect": false,
    "exclusionReason": "private-identity"
  },
  "pages/generatehistory/generatehistory": {
    "kind": "excluded",
    "readiness": "excluded-private-generated-content",
    "collect": false,
    "exclusionReason": "private-generated-content"
  },
  "pages/lingshidetails/lingshidetails": {
    "kind": "excluded",
    "readiness": "excluded-private-finance",
    "collect": false,
    "exclusionReason": "private-finance"
  },
  "pages/orderdetail/orderdetail": {
    "kind": "excluded",
    "readiness": "excluded-private-finance",
    "collect": false,
    "exclusionReason": "private-finance"
  },
  "pages/messagecenter/messagecenter": {
    "kind": "excluded",
    "readiness": "excluded-private-notifications",
    "collect": false,
    "exclusionReason": "private-notifications"
  },
  "pages/adminlogin/adminlogin": {
    "kind": "excluded",
    "readiness": "excluded-administration",
    "collect": false,
    "exclusionReason": "administration"
  },
  "pages/adminhome/adminhome": {
    "kind": "excluded",
    "readiness": "excluded-administration",
    "collect": false,
    "exclusionReason": "administration"
  },
  "pages/withdrawcenter/withdrawcenter": {
    "kind": "excluded",
    "readiness": "excluded-private-finance",
    "collect": false,
    "exclusionReason": "private-finance"
  },
  "pages/contentreader/contentreader": {
    "kind": "owned-content",
    "collect": true,
    "readiness": "explicit",
    "idKey": "content_id"
  }
};
function policy(route) { return ROUTES[String(route || '').replace(/^\//, '')] || { collect: false }; }
function ready(route, data, rendered) {
 const p=policy(route); if (!p.collect || !rendered) return false;
 if (data.error || data.loading || data.loadingTool || (p.excludeSelf && data.isSelfHomepage)) return false;
 if (p.readiness==='rendered') return true;
 if (p.readiness==='explicit') return data.contentReady===true;
 const value=data[p.field]; return Array.isArray(value) ? value.length>0 : !!value;
}
function binding(route, options) {
 const p=policy(route); options=options||{}; const value=options[p.idKey] || (p.idAlias && options[p.idAlias]);
 if (!p.idKey || !value) return {};
 const raw=String(value).slice(0,80);
 if (p.kind==='owned-content') return {id:raw,version:Number(options.content_version)||undefined};
 let hash=2166136261; for(let i=0;i<route.length;i++) hash=Math.imul(hash ^ route.charCodeAt(i),16777619);
 return {id:'legacy:'+(hash>>>0).toString(16)+':'+raw.replace(/[^A-Za-z0-9_-]/g,'')};
}
// Explicit public UI callbacks only. A tap is intent, never evidence that the target
// loaded, navigation succeeded, or any item exposed by a list was read.
const OBSERVED_ACTIONS = {
 'pages/index/index':['onFeaturedCaseTap'],
 'pages/template/template':['onCardTap'],
 'pages/utilitytools/utilitytools':['onToolTap'],
 'pages/aitools/aitools':['onToolTap'],
 'pages/distribution/distribution':['onWorkTap','onDesignerTap'],
 'pages/designerhome/designerhome':['onWorkTap','onReviewRelatedTap'],
 'pages/templatesquaredetails/templatesquaredetails':['onDesignerTap','onPreviewImage'],
 'pages/inspirationdetail/inspirationdetail':['onPreviewImage']
};
function observedAction(route, name, page, event) {
 route=String(route||'').replace(/^\//,'');
 if (!(OBSERVED_ACTIONS[route]||[]).includes(name) || !policy(route).collect) return null;
 const data=page.data||{}, ds=event&&event.currentTarget&&event.currentTarget.dataset||{};
 if (policy(route).excludeSelf && data.isSelfHomepage) return null;
 const numeric=value=>{const n=Number(value);return Number.isSafeInteger(n)&&n>0?String(n):'';};
 const numericRaw=value=>/^[0-9]{1,80}$/.test(String(value))&&numeric(value)?String(value):'';
 const identifier=value=>typeof value==='string'||typeof value==='number' ? (/^[A-Za-z0-9_-]{1,80}$/.test(String(value))&&String(value)!=='0'?String(value):'') : '';
 const target=(destination,value,component,key='id')=>{
   if(!value)return null;
   const content=binding(destination,{[key]:value});
   return content.id?{type:'content_click',extra:{target_content_id:content.id,component_key:component,action_key:name}}:null;
 };
 const template=value=>target('pages/templatesquaredetails/templatesquaredetails',numeric(value),'template_card');
 const designer=value=>target('pages/designerhome/designerhome',numeric(value),'designer_card','userId');
 if(name==='onPreviewImage') {
   const valid=route==='pages/inspirationdetail/inspirationdetail'
     ? !!ds.url && Array.isArray(data.detail&&data.detail.images) && data.detail.images.length>0
     : Array.isArray(data.imageList) && data.imageList.length>0;
   return valid?{type:'image_open',extra:{component_key:'content_image',action_key:name}}:null;
 }
 if(route==='pages/index/index')return target('pages/templatesquaredetails/templatesquaredetails',numericRaw(ds.id),'template_card');
 if(route==='pages/template/template')return ds.targetType==='inspiration'
   ?target('pages/inspirationdetail/inspirationdetail',numeric(ds.id),'inspiration_card'):template(ds.id);
 if(route==='pages/utilitytools/utilitytools')return target('pages/utilitytooldetail/utilitytooldetail',numericRaw(ds.id),'utility_tool_card');
 if(route==='pages/aitools/aitools')return target('pages/aitooldetail/aitooldetail',identifier(ds.id),'ai_tool_card');
 if(name==='onDesignerTap')return designer(route==='pages/distribution/distribution'?ds.userId:(data.userInfo||{}).userId);
 if(route==='pages/distribution/distribution') {
   if(String(ds.publishScope||'square')!=='square' && Number(ds.userId))return designer(ds.userId);
   return template(ds.id);
 }
 if(route==='pages/designerhome/designerhome') {
   // Homepage-only works do not have a public template detail binding. Do not
   // invent a template target or export their image URL or profile information.
   if(name==='onWorkTap' && String(ds.publishScope||'square')!=='square')return null;
   return template(ds.id);
 }
 return null;
}
module.exports={ROUTES,policy,ready,binding,OBSERVED_ACTIONS,observedAction};
