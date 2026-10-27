'use strict';
const LAYOUT_TAG=/^<\/?(?:section|div|p|span|strong|b|em|i|u|s|br|h[1-6]|blockquote|ul|ol|li|figure|figcaption|table|thead|tbody|tfoot|tr|td|th|hr|code|pre)(?: (?:style="[^"]*"|colspan="\d+"|rowspan="\d+"))*?>$/;
const LAYOUT_IMAGE=/^<img data-media-id="([A-Za-z0-9_-]{8,128})"(?: alt="[^"]*")?(?: style="[^"]*")?>$/;
const LAYOUT_VIDEO_TAG=/^(?:<jd-video data-media-id="[A-Za-z0-9_-]{8,128}"(?: style="[^"]*")?>|<\/jd-video>)$/;
function safeLayout(layout){if(/(?:\b(?:src|href|on[a-z]+)=|<\/?(?:script|style|iframe|object|embed|svg|audio|link|meta)\b|url\s*\(|expression\s*\(|@import|\\|(?:position|z-index|transform|filter|visibility|opacity|content|behavior)\s*:)/i.test(layout))throw new Error('原排版正文含不安全内容');const tokens=layout.match(/<[^>]*>/g)||[];for(const token of tokens){if(!LAYOUT_TAG.test(token)&&!LAYOUT_IMAGE.test(token)&&!LAYOUT_VIDEO_TAG.test(token))throw new Error('原排版正文布局无效');}}
function escapeAttribute(value){return String(value).replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');}
function layoutBlocks(document,assets,width){
 const layout=String(document.layout_html||'');safeLayout(layout);
 const media=id=>{const asset=assets.get(id);if(!asset||!/^https:\/\//.test(asset.url||''))throw new Error('原排版正文媒体不完整');return asset;};
 const withImages=layout.replace(/<img data-media-id="([A-Za-z0-9_-]{8,128})"((?: alt="[^"]*")?(?: style="[^"]*")?)>/g,(_,id,attrs)=>{const asset=media(id);if(asset.kind!=='image')throw new Error('原排版正文媒体类型无效');return '<img src="'+escapeAttribute(asset.url)+'"'+attrs+'>';});
 const videoPattern=/<jd-video data-media-id="([A-Za-z0-9_-]{8,128})"(?: style="([^"]*)")?><\/jd-video>/g;
 const out=[];let offset=0,index=0,match;
 while((match=videoPattern.exec(withImages))){if(match.index>offset)out.push({kind:'layout',html:withImages.slice(offset,match.index),key:'article-layout-'+index++});const asset=media(match[1]);if(asset.kind!=='video')throw new Error('原排版正文媒体类型无效');out.push({...asset,kind:'video',key:'article-layout-'+index,domId:'article-layout-media-'+index,playerKey:'article-layout-player-'+index,errorKey:'articleLayoutMedia'+index,height:Math.max(120,Math.round(width*(asset.height||9)/(asset.width||16))),style:match[2]||''});index++;offset=videoPattern.lastIndex;}
 if(offset<withImages.length)out.push({kind:'layout',html:withImages.slice(offset),key:'article-layout-'+index});
 if(!out.length||out.some(block=>block.kind==='layout'&&/data-media-id|jd-video/.test(block.html)))throw new Error('原排版正文媒体不完整');
 return out;
}
function renderArticleDocument(body,media,width){
 const document=JSON.parse(body);
 if(document.schema!==1||!Array.isArray(document.blocks)||!document.blocks.length)throw new Error('原排版正文无效');
 const assets=new Map((media||[]).map(asset=>[asset.media_id,asset]));
 if(document.layout_html)return layoutBlocks(document,assets,width);
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
