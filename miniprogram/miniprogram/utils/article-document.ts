// @ts-nocheck
'use strict';

const LAYOUT_TAG=/^<\/?(?:section|div|p|span|strong|b|em|i|u|s|br|h[1-6]|blockquote|ul|ol|li|figure|figcaption|table|thead|tbody|tfoot|tr|td|th|hr|code|pre)(?: (?:style="[^"]*"|colspan="\d+"|rowspan="\d+"))*?>$/;
const LAYOUT_IMAGE=/^<img data-media-id="([A-Za-z0-9_-]{8,128})"(?: alt="[^"]*")?(?: style="[^"]*")?>$/;
const LAYOUT_VIDEO_TAG=/^(?:<jd-video data-media-id="[A-Za-z0-9_-]{8,128}"(?: style="[^"]*")?>|<\/jd-video>)$/;

function safeLayout(layout) {
 if(/(?:\b(?:src|href|on[a-z]+)=|<\/?(?:script|style|iframe|object|embed|svg|audio|link|meta)\b|url\s*\(|expression\s*\(|@import|\\|(?:position|z-index|transform|filter|visibility|opacity|content|behavior)\s*:)/i.test(layout))throw new Error('原排版正文含不安全内容');
 const tokens=layout.match(/<[^>]*>/g)||[];
 for(const token of tokens){if(!LAYOUT_TAG.test(token)&&!LAYOUT_IMAGE.test(token)&&!LAYOUT_VIDEO_TAG.test(token))throw new Error('原排版正文布局无效');}
}
function escapeAttribute(value){return String(value).replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');}
const VOID_LAYOUT_TAGS=new Set(['br','hr','img']);
function layoutTagName(token){const match=/^<\/?([a-z0-9-]+)/.exec(token);return match&&match[1];}
function layoutStyle(token){const match=/\sstyle="([^"]*)"/.exec(token);return match?match[1]:'';}
function meaningfulLayout(html){return /<img\b/.test(html)||html.replace(/<[^>]*>/g,'').trim()!=='';}
function balancedLayoutParts(layout){
 const tokens=layout.match(/<[^>]*>|[^<]+/g)||[],parts=[];let html='',videoOpen=false;
 const stack=[];
 const pushHTML=value=>{if(meaningfulLayout(value))parts.push({kind:'layout',html:value});};
 for(const token of tokens){
  const video=/^<jd-video data-media-id="([A-Za-z0-9_-]{8,128})"(?: style="([^"]*)")?>$/.exec(token);
  if(video){
   if(videoOpen)throw new Error('原排版正文布局无效');
   pushHTML(html+stack.slice().reverse().map(item=>'</'+item.tag+'>').join(''));
   parts.push({kind:'video-ref',media_id:video[1],style:[...stack.map(item=>item.style),video[2]||''].filter(Boolean).join(';'),className:['article-layout-native',...stack.map(item=>'article-layout-native-'+item.tag)].join(' ')});
   html=stack.map(item=>item.open).join('');videoOpen=true;continue;
  }
  if(token==='</jd-video>'){if(!videoOpen)throw new Error('原排版正文布局无效');videoOpen=false;continue;}
  if(/^<\//.test(token)){
   const tag=layoutTagName(token),top=stack[stack.length-1];if(!top||top.tag!==tag)throw new Error('原排版正文布局无效');
   html+=token;stack.pop();continue;
  }
  if(/^</.test(token)){
   const tag=layoutTagName(token);html+=token;if(tag&&!VOID_LAYOUT_TAGS.has(tag))stack.push({tag,open:token,style:layoutStyle(token)});continue;
  }
  html+=token;
 }
 if(videoOpen||stack.length)throw new Error('原排版正文布局无效');
 pushHTML(html);return parts;
}
function layoutBlocks(document,assets,width,prefix){
 const layout=String(document.layout_html||'');safeLayout(layout);
 const media=id=>{const asset=assets.get(id),url=asset&&(asset.local_path||asset.url);if(!asset||!url||(!asset.local_path&&!/^https:\/\//.test(url)))throw new Error('原排版正文媒体不完整');return {...asset,url};};
 const withImages=layout.replace(/<img data-media-id="([A-Za-z0-9_-]{8,128})"((?: alt="[^"]*")?(?: style="[^"]*")?)>/g,(_,id,attrs)=>{const asset=media(id);if(asset.kind!=='image')throw new Error('原排版正文媒体类型无效');return '<img src="'+escapeAttribute(asset.url)+'"'+attrs+'>';});
 return balancedLayoutParts(withImages).map((part,index)=>{
  if(part.kind==='layout')return {...part,key:prefix+'-'+index};
  const asset=media(part.media_id);if(asset.kind!=='video')throw new Error('原排版正文媒体类型无效');
  return {...asset,kind:'video',key:prefix+'-'+index,domId:prefix+'-media-'+index,playerKey:prefix+'-player-'+index,errorKey:prefix+'Media'+index,height:Math.max(120,Math.round(width*(asset.height||9)/(asset.width||16))),style:part.style,className:part.className};
 });
}

function renderArticleDocument(body, media, width, prefix='article-layout') {
 const document=JSON.parse(body);
 if(document.schema!==1||!Array.isArray(document.blocks)||!document.blocks.length)throw new Error('原排版正文无效');
 const assets=new Map((media||[]).map(asset=>[asset.media_id,asset]));
 if(document.layout_html)return layoutBlocks(document,assets,width,prefix);
 return document.blocks.map((block,index)=>{
  const style=block.style||{},classes=['article-block'];
  if(block.kind==='heading')classes.push('article-heading','article-heading-'+block.level);
  else classes.push('article-'+block.kind);
  if(style.align)classes.push('article-align-'+style.align);
  if(style.font_size)classes.push('article-size-'+style.font_size);
  if(style.border&&style.border!=='none')classes.push('article-border-'+style.border);
  const spacing=[];
  if(style.spacing_before)spacing.push('margin-top:'+style.spacing_before+'px');
  if(style.spacing_after)spacing.push('margin-bottom:'+style.spacing_after+'px');
  const rendered={...block,key:'article-block-'+index,className:classes.join(' '),style:spacing.join(';')};
  if(block.kind==='image'||block.kind==='video'){
   const asset=assets.get(block.media_id);
   if(!asset||asset.kind!==block.kind)throw new Error('原排版正文媒体不完整');
   Object.assign(rendered,asset,{caption:block.caption||'',domId:'article-media-'+index,playerKey:'article-player-'+index,errorKey:'articleMedia'+index,height:Math.max(120,Math.round(width*(asset.height||9)/(asset.width||16)))});
  }
  return rendered;
 });
}

function preserveLayoutBody(body,fallback){try{const document=JSON.parse(body);if(document.schema===1&&typeof document.layout_html==='string'&&document.layout_html)return body;}catch(_){ }return fallback;}

module.exports={renderArticleDocument,preserveLayoutBody};
