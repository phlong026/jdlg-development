'use strict';

const positive = number => Number.isFinite(Number(number)) && Number(number) > 0;

function fitNoteMedia(media, windowInfo = {}) {
  const windowWidth = positive(windowInfo.windowWidth) ? Number(windowInfo.windowWidth) : 375;
  const windowHeight = positive(windowInfo.windowHeight) ? Number(windowInfo.windowHeight) : 667;
  // 三个阅读页统一为左右各28rpx外边距、28rpx内边距及1rpx边框。
  const width = Math.max(1, Math.floor(windowWidth * 636 / 750));
  const maxHeight = Math.max(1, Math.floor(windowHeight * 0.62));
  return (media || []).map(asset => {
    const identity = { key: asset.key || asset.media_id };
    if (asset.kind !== 'video') return Object.assign({}, asset, identity);
    const ratio = positive(asset.width) && positive(asset.height) ? Number(asset.width) / Number(asset.height) : 16 / 9;
    const frameWidth = Math.min(width, maxHeight * ratio);
    return Object.assign({}, asset, identity, { frameWidth: Math.max(1, Math.round(frameWidth)), frameHeight: Math.max(1, Math.min(maxHeight, Math.round(frameWidth / ratio))) });
  });
}

function groupNoteImages(items, media, windowInfo = {}) {
  const assets = new Map((media || []).map(asset => [asset.media_id, asset]));
  const width = Math.floor((Number(windowInfo.windowWidth) || 375) * 636 / 750);
  const maxHeight = Math.floor((Number(windowInfo.windowHeight) || 667) * 0.62);
  const output = [];
  for (let index = 0; index < (items || []).length;) {
    const item = items[index];
    if (item.kind !== 'image') { output.push(Object.assign({}, item, { key: item.key || item.media_id || 'note-item-' + index })); index++; continue; }
    const start = index, images = [];
    while (index < items.length && items[index].kind === 'image') images.push(items[index++]);
    if (images.length === 1) { output.push(Object.assign({}, item, { key: item.key || item.media_id || 'note-item-' + start })); continue; }
    const heights = images.map(image => {
      const asset = assets.get(image.media_id) || {};
      return positive(asset.width) && positive(asset.height) ? width * asset.height / asset.width : width * 3 / 4;
    });
    output.push({ kind: 'gallery', key: 'note-gallery-' + start, items: images.map((image, offset) => Object.assign({}, image, { galleryItemKey: 'note-image-' + start + '-' + offset })), frameHeight: Math.max(1, Math.round(Math.min(maxHeight, Math.max(...heights)))) });
  }
  return output;
}

function fitNoteBlocks(blocks, media, windowInfo) {
  const assets = new Map((media || []).map(asset => [asset.media_id, asset]));
  const original = (blocks || []).flatMap(block => block.kind === 'gallery' ? block.items : [block]);
  const fitted = original.map(block => {
    const asset = assets.get(block.media_id);
    return block.kind === 'video' && asset ? Object.assign({}, block, { frameWidth: asset.frameWidth, frameHeight: asset.frameHeight }) : block;
  });
  return groupNoteImages(fitted, media, windowInfo);
}

function noteVideoMetadata(media, event, windowInfo) {
  const id = event.currentTarget && event.currentTarget.dataset && event.currentTarget.dataset.id;
  const detail = event.detail || {};
  const updated = (media || []).map(asset => asset.media_id === id && asset.kind === 'video' && positive(detail.width) && positive(detail.height)
    ? Object.assign({}, asset, { width: Number(detail.width), height: Number(detail.height) }) : asset);
  return fitNoteMedia(updated, windowInfo);
}

module.exports = { fitNoteMedia, fitNoteBlocks, groupNoteImages, noteVideoMetadata };
