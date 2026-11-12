'use strict';
const { renderArticleDocument } = require('./article-document');

const PRESENTATIONS = new Set(['article', 'gallery', 'video']);
const STRUCTURED_TEXT = new Set(['heading', 'quote', 'list']);

function articleDocument(content) {
  if (!content || content.body_format !== 'article_v2') return null;
  try {
    const document = JSON.parse(content.body);
    return document && document.schema === 1 && Array.isArray(document.blocks) ? document : null;
  } catch (_) {
    return null;
  }
}

function meaningfulParagraph(block) {
  return block && block.kind === 'paragraph' && String(block.text || '').trim() !== '';
}

function inferredFromBlocks(blocks) {
  if (!blocks.length || blocks.some(block => STRUCTURED_TEXT.has(block.kind))) return 'article';
  if (blocks.some(block => block.style && Object.keys(block.style).length)) return 'article';
  if (blocks.some(block => !['paragraph', 'image', 'video'].includes(block.kind))) return 'article';
  const mediaIndexes = blocks.map((block, index) => ['image', 'video'].includes(block.kind) ? index : -1).filter(index => index >= 0);
  if (!mediaIndexes.length) return 'article';
  for (let index = 1; index < mediaIndexes.length; index += 1) {
    if (mediaIndexes[index] !== mediaIndexes[index - 1] + 1) return 'article';
  }
  const images = blocks.filter(block => block.kind === 'image').length;
  const videos = blocks.filter(block => block.kind === 'video').length;
  if (images && !videos) return 'gallery';
  if (videos === 1 && !images) return 'video';
  return 'article';
}

function inferContentPresentation(content) {
  const document = articleDocument(content);
  if (document && document.layout_html) return 'article';
  if (document && document.presentation === 'article') return 'article';
  if (document && PRESENTATIONS.has(document.presentation)) return document.presentation;
  if (PRESENTATIONS.has(content && content.presentation)) return content.presentation;
  if (document) return inferredFromBlocks(document.blocks);
  if (content && ['rich_text', 'html'].includes(content.body_format)) return 'article';
  const media = Array.isArray(content && content.media) ? content.media.filter(item => item && ['image', 'video'].includes(item.kind)) : [];
  const images = media.filter(item => item.kind === 'image').length;
  const videos = media.filter(item => item.kind === 'video').length;
  if (images && !videos) return 'gallery';
  if (videos === 1 && !images) return 'video';
  return 'article';
}

function textBody(content, document) {
  if (!document) return String(content && content.body || '').trim();
  return document.blocks.filter(meaningfulParagraph).map(block => String(block.text).trim()).join('\n\n');
}

function referencedMediaIDs(document) {
  if (!document) return null;
  const ids = new Set();
  for (const block of document.blocks) if (['image', 'video'].includes(block.kind) && block.media_id) ids.add(block.media_id);
  if (document.layout_html) {
    const pattern = /data-media-id="([A-Za-z0-9_-]{8,128})"/g;
    let match;
    while ((match = pattern.exec(document.layout_html))) ids.add(match[1]);
  }
  return ids;
}

function sourceMedia(content, media, document) {
  const coverID = content && content.cover && content.cover.media_id;
  const assets = (media || []).filter(item => item && ['image', 'video'].includes(item.kind));
  if (!document) return assets.filter(item => item.media_id !== coverID);
  const byID = new Map(assets.map(item => [item.media_id, item]));
  const ordered = document.blocks.filter(block => ['image', 'video'].includes(block.kind)).map(block => byID.get(block.media_id) || { media_id: block.media_id, kind: block.kind, url: '', preview_error: '素材暂不可用' });
  const orderedIDs = new Set(ordered.map(item => item.media_id));
  for (const id of referencedMediaIDs(document)) if (!orderedIDs.has(id) && byID.has(id)) ordered.push(byID.get(id));
  return ordered;
}

function mediaFrame(asset, index, frameWidth, windowHeight, prefix, caption, maxHeightOverride) {
  const intrinsicWidth = Math.max(1, Number(asset.width) || (asset.kind === 'video' ? 16 : 3));
  const intrinsicHeight = Math.max(1, Number(asset.height) || (asset.kind === 'video' ? 9 : 4));
  const maxHeight = Math.max(240, Number(maxHeightOverride) || Math.round(windowHeight * 0.68));
  const naturalHeight = Math.round(frameWidth * intrinsicHeight / intrinsicWidth);
  const height = Math.max(120, Math.min(naturalHeight, maxHeight));
  const displayWidth = Math.min(frameWidth, Math.round(height * intrinsicWidth / intrinsicHeight));
  return {
    ...asset,
    key: `${prefix}-media-${index}-${asset.media_id || index}`,
    domId: asset.domId || `${prefix}-media-${index}`,
    playerKey: asset.playerKey || `${prefix}-player-${index}`,
    errorKey: asset.errorKey || `${prefix}Media${index}`,
    caption: caption === undefined ? String(asset.caption || '') : caption,
    width: displayWidth,
    height,
    intrinsicWidth,
    intrinsicHeight,
  };
}

function imageCaptions(document, images) {
  const blockCaptions = new Map();
  if (document) {
    for (const block of document.blocks) {
      if (block.kind === 'image' && block.media_id && block.caption) blockCaptions.set(block.media_id, String(block.caption).trim());
    }
  }
  return images.map(item => blockCaptions.get(item.media_id) || String(item.caption || '').trim());
}

function galleryCopy(bodyText, captions) {
  const nonEmpty = captions.filter(Boolean);
  if (!nonEmpty.length || new Set(nonEmpty).size !== 1 || nonEmpty.length !== captions.length) return { bodyText, captions };
  const repeated = nonEmpty[0];
  return {
    bodyText: [bodyText, repeated].filter(Boolean).filter((value, index, all) => all.indexOf(value) === index).join('\n\n'),
    captions: captions.map(() => ''),
  };
}

function articleBlocks(content, media, document, frameWidth, windowHeight, prefix) {
  if (document) {
    const assets = new Map(media.map(item => [item.media_id, item]));
    return renderArticleDocument(content.body, media, frameWidth, prefix).map((block, index) => {
      if (!['image', 'video'].includes(block.kind)) return block;
      const framed = mediaFrame(assets.get(block.media_id) || block, index, frameWidth, windowHeight, prefix, block.caption);
      return { ...block, width: framed.width, height: framed.height, intrinsicWidth: framed.intrinsicWidth, intrinsicHeight: framed.intrinsicHeight };
    });
  }
  const blocks = [];
  if (content.body_format === 'rich_text') blocks.push({ kind: 'layout', key: `${prefix}-rich-text`, html: content.body });
  const body = String(content.body || '').trim();
  if (body && content.body_format !== 'rich_text') blocks.push({ kind: 'paragraph', key: `${prefix}-paragraph-0`, text: body, className: 'article-block article-paragraph', style: '' });
  media.forEach((item, index) => blocks.push({ ...mediaFrame(item, index, frameWidth, windowHeight, prefix), kind: item.kind, className: `article-block article-${item.kind}`, style: '' }));
  return blocks;
}

function buildContentPresentation(content, media, windowInfo, options = {}) {
  const safeContent = content || {};
  const document = articleDocument(safeContent);
  const availableMedia = sourceMedia(safeContent, media, document);
  const kind = inferContentPresentation({ ...safeContent, media: availableMedia });
  const frameWidth = Math.max(240, Math.round(Number(windowInfo && windowInfo.windowWidth || 375) - 36));
  const windowHeight = Math.max(480, Number(windowInfo && windowInfo.windowHeight || 667));
  const prefix = String(options.prefix || 'content-presentation').replace(/[^A-Za-z0-9_-]/g, '-') || 'content-presentation';
  const videoLayout = document && document.video_layout === 'douyin' ? 'douyin' : 'channels';
  const videoChromeHeight = Math.max(120, Number(options.videoChromeHeight) || 150);
  // Both layouts keep copy in normal flow so native controls never cover text actions.
  const videoReserve = videoChromeHeight + 160;
  const videoMaxHeight = Math.max(240, windowHeight - videoReserve);
  const poster = String(safeContent.cover && safeContent.cover.url || '');
  let bodyText = textBody(safeContent, document);
  let blocks = [];
  let viewMedia = [];

  if (kind === 'article') {
    blocks = articleBlocks(safeContent, availableMedia, document, frameWidth, windowHeight, prefix);
    viewMedia = blocks.filter(block => ['image', 'video'].includes(block.kind));
  } else if (kind === 'gallery') {
    const images = availableMedia.filter(item => item.kind === 'image');
    const copy = galleryCopy(bodyText, imageCaptions(document, images));
    bodyText = copy.bodyText;
    viewMedia = images.map((item, index) => mediaFrame(item, index, frameWidth, windowHeight, prefix, copy.captions[index]));
  } else {
    const primary = availableMedia.find(item => item.kind === 'video');
    const videoBlock = document && document.blocks.find(block => block.kind === 'video');
    const caption = String(videoBlock && videoBlock.caption || primary && primary.caption || '').trim();
    bodyText = [bodyText, caption].filter(Boolean).filter((value, index, all) => all.indexOf(value) === index).join('\n\n');
    viewMedia = primary ? [mediaFrame(primary, 0, frameWidth, windowHeight, prefix, undefined, videoMaxHeight)] : [];
  }

  const maxGalleryIndex = Math.max(0, viewMedia.length - 1);
  const sourceNote = String(safeContent.source_note || '').trim();
  return {
    kind,
    title: String(safeContent.title || ''),
    // Import guidance belongs to the editor, not the published attribution.
    sourceNote: sourceNote === '链接导入的可编辑草稿，请在发布前核对正文、排版和媒体。' ? '' : sourceNote,
    publishedAt: String(safeContent.published_at || ''),
    bodyText,
    bodyExpandable: kind === 'video' && bodyText.length > 110,
    descriptionExpanded: options.descriptionExpanded === true,
    videoLayout,
    shareCopy: String(document && document.share_copy || ''),
    momentsCopy: String(document && document.moments_copy || ''),
    poster,
    nativePoster: /^https:\/\//.test(poster) ? poster : '',
    blocks,
    media: viewMedia,
    frameWidth,
    galleryIndex: Math.min(maxGalleryIndex, Math.max(0, Number(options.galleryIndex) || 0)),
    galleryHeight: viewMedia.reduce((height, item) => Math.max(height, item.height || 0), 0),
  };
}

module.exports = { inferContentPresentation, buildContentPresentation };
