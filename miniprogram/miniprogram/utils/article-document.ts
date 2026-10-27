// @ts-nocheck
'use strict';

function renderArticleDocument(body, media, width) {
 const document=JSON.parse(body);
 if(document.schema!==1||!Array.isArray(document.blocks)||!document.blocks.length)throw new Error('原排版正文无效');
 const assets=new Map((media||[]).map(asset=>[asset.media_id,asset]));
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

module.exports={renderArticleDocument};
