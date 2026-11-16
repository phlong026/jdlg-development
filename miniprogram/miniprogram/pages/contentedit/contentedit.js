'use strict';
const api = require('../../utils/phase3-api');
const { preserveLayoutBody, renderArticleDocument } = require('../../utils/article-document');
const { startAIStream } = require('../../utils/ai-stream');
const { noteWritingPrompt, socialIntroPrompt } = require('../../utils/writing-policy');
let presentationTools;
let videoUploadTools;
function contentPresentation() { if (!presentationTools) presentationTools = require('../../utils/content-presentation'); return presentationTools; }
function resumableVideoUpload() { if (!videoUploadTools) videoUploadTools = require('../../utils/resumable-video-upload'); return videoUploadTools; }

function textBlock(text = '') { return { kind: 'paragraph', text }; }
function headingBlock(level = 2) { return { kind: 'heading', level: level === 3 ? 3 : 2, text: '' }; }
function quoteBlock() { return { kind: 'quote', text: '' }; }
function listBlock(ordered = false) { return { kind: 'list', ordered: !!ordered, items: [''] }; }
function mediaIDs(blocks) { return [...new Set(blocks.filter(block => block.media_id).map(block => block.media_id))]; }
function writingFacts(blocks) {
  return blocks.map(block => {
    if (block.kind === 'list') return (block.items || []).filter(Boolean).join('\n');
    if (block.kind === 'image' || block.kind === 'video') return String(block.caption || '').trim();
    return String(block.text || '').trim();
  }).filter(Boolean).join('\n\n').slice(0, 6000);
}
function articleBody(blocks, presentation, includePresentation, videoFields, draftOnly) {
  const document = { schema: 1, blocks: blocks.map(block => {
    if (block.kind === 'image' || block.kind === 'video') return { kind: block.kind, media_id: block.media_id, caption: block.caption || '' };
    if (block.kind === 'heading') return { kind: 'heading', level: block.level === 3 ? 3 : 2, text: String(block.text || '').trim() };
    if (block.kind === 'quote') return { kind: 'quote', text: String(block.text || '').trim() };
    if (block.kind === 'list') return { kind: 'list', ordered: !!block.ordered, items: (block.items || []).filter(Boolean) };
    return { kind: 'paragraph', text: String(block.text || '').trim() };
  }) };
  if (includePresentation) document.presentation = presentation;
  if (presentation === 'video' && videoFields) {
    document.video_layout = videoFields.videoLayout;
    document.share_copy = videoFields.shareCopy;
    document.moments_copy = videoFields.momentsCopy;
  }
  if (draftOnly) document.draft_only = true;
  return JSON.stringify(document);
}
function articleDocument(content) {
  if (!content || content.body_format !== 'article_v2') return null;
  try { return JSON.parse(content.body); } catch (_) { return null; }
}
function initialBlocks(content, presentation) {
  if (!content) return [textBlock('')];
  if (content.body_format !== 'article_v2') return [textBlock(content.body || '')];
  const document = articleDocument(content);
  const blocks = document && Array.isArray(document.blocks) && document.blocks.length ? document.blocks : [textBlock('')];
  if (presentation === 'article') return blocks;
  const migrated = migrateRepeatedCaptions(blocks);
  return socialBlocks(presentation === 'video' ? migrateLegacyVideoCaption(migrated) : migrated);
}
function mediaHeight(asset) {
  const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : { windowWidth: 375, windowHeight: 667 };
  const frameWidth = Math.max(240, Number(windowInfo.windowWidth || 375));
  const width = Math.max(1, Number(asset && asset.width) || (asset && asset.kind === 'video' ? 16 : 3));
  const height = Math.max(1, Number(asset && asset.height) || (asset && asset.kind === 'video' ? 9 : 4));
  return Math.max(120, Math.min(Math.round(frameWidth * height / width), Math.round(Number(windowInfo.windowHeight || 667) * 0.68)));
}
function migrateRepeatedCaptions(blocks) {
  const captions = new Map();
  blocks.forEach(block => {
    const caption = block.kind === 'image' || block.kind === 'video' ? String(block.caption || '').trim() : '';
    if (caption.length >= 30) captions.set(caption, (captions.get(caption) || 0) + 1);
  });
  const repeated = [...captions].filter(([, count]) => count > 1).map(([caption]) => caption);
  if (!repeated.length) return blocks;
  const existingText = new Set(blocks.filter(block => block.kind === 'paragraph').map(block => String(block.text || '').trim()));
  const next = blocks.map(block => repeated.includes(String(block.caption || '').trim()) ? { ...block, caption: '' } : block);
  repeated.forEach(text => { if (!existingText.has(text)) next.push(textBlock(text)); });
  return next;
}
function migrateLegacyVideoCaption(blocks) {
  const video = blocks.find(block => block.kind === 'video');
  const caption = String(video && video.caption || '').trim();
  if (!video || caption.length < 30) return blocks;
  const paragraphs = blocks.filter(block => block.kind === 'paragraph' && String(block.text || '').trim());
  if (paragraphs.length && !paragraphs.some(block => String(block.text || '').trim() === caption)) return blocks;
  const next = blocks.map(block => block === video ? { ...block, caption: '' } : block);
  if (!paragraphs.length) next.push(textBlock(caption));
  return next;
}
function socialBlocks(blocks) {
  return blocks.filter(block => block.kind === 'image' || block.kind === 'video').concat(blocks.filter(block => block.kind === 'paragraph'));
}
function decorateBlocks(blocks, media) {
  const byID = new Map((media || []).map(asset => [asset.media_id, asset]));
  return blocks.map((block, index) => {
    const asset = block.media_id ? byID.get(block.media_id) || null : null;
    return { ...block, index, asset, mediaHeight: asset ? mediaHeight(asset) : 0 };
  });
}
function blockViews(blocks, media, selectedMediaID) {
  const renderedBlocks = decorateBlocks(blocks, media);
  const imageBlocks = renderedBlocks.filter(block => block.kind === 'image').map(block => ({ ...block, blockIndex: block.index }));
  return {
    renderedBlocks,
    imageBlocks,
    selectedImageBlock: imageBlocks.find(block => block.media_id === selectedMediaID) || imageBlocks[0] || null,
    videoBlocks: renderedBlocks.filter(block => block.kind === 'video').map(block => ({ ...block, blockIndex: block.index })),
  };
}
function mergeAssets(...groups) {
  const assets = new Map();
  groups.forEach(group => (Array.isArray(group) ? group : [group]).filter(Boolean).forEach(asset => assets.set(asset.media_id, { ...(assets.get(asset.media_id) || {}), ...asset })));
  return [...assets.values()];
}
function documentTitle(name) { return String(name || '').replace(/\.(pdf|docx|pptx)$/i, '').trim().slice(0, 80) || '文档资料'; }
function postText(blocks) { return blocks.filter(block => block.kind === 'paragraph').map(block => String(block.text || '').trim()).filter(Boolean).join('\n\n'); }
function hasStructuredBlocks(blocks) { return blocks.some(block => !['paragraph', 'image', 'video'].includes(block.kind)); }
function mediaBlocks(blocks, kind) { return blocks.filter(block => block.kind === kind); }
function sessionAccountKey(value) {
  let hash = 2166136261;
  const text = String(value || '');
  for (let index = 0; index < text.length; index += 1) hash = Math.imul(hash ^ text.charCodeAt(index), 16777619);
  return `account-${(hash >>> 0).toString(16)}`;
}
function editorActor() {
  try {
    const profile = wx.getStorageSync('userInfo') || {};
    const id = profile && typeof profile === 'object' ? String(profile.id || '') : '';
    if (id) return id;
  } catch (_) { /* 未缓存资料时回退到登录凭证 */ }
  return String(wx.getStorageSync('token') || '');
}
function socialMediaSignature(blocks) {
  return JSON.stringify((blocks || []).filter(block => block.kind === 'image' || block.kind === 'video').map(block => ({ kind: block.kind, media_id: block.media_id, caption: block.caption || '' })));
}
function cloneEditorBlocks(blocks) {
  return (blocks || []).map(block => ({ ...block, ...(Array.isArray(block.items) ? { items: block.items.slice() } : {}) }));
}
function articleAIPlan(blocks) {
  let targetIndex = -1;
  (blocks || []).forEach((block, index) => {
    if (block.kind === 'paragraph' && !String(block.text || '').trim()) targetIndex = index;
  });
  return { targetIndex: targetIndex < 0 ? (blocks || []).length : targetIndex, append: targetIndex < 0 };
}
function articleRestoreMatches(state, currentBlocks) {
  const original = state && state.blocks || [], plan = state && state.articlePlan;
  if (!plan) return false;
  const current = currentBlocks || [];
  if (plan.append) {
    return current.length === original.length + 1 && plan.targetIndex === original.length
      && current[plan.targetIndex] && current[plan.targetIndex].kind === 'paragraph'
      && JSON.stringify(current.slice(0, plan.targetIndex)) === JSON.stringify(original);
  }
  if (current.length !== original.length || !current[plan.targetIndex] || current[plan.targetIndex].kind !== 'paragraph') return false;
  return current.every((block, index) => index === plan.targetIndex || JSON.stringify(block) === JSON.stringify(original[index]));
}
function presentationErrors(view, media) {
  const errors = new Map((media || []).filter(asset => asset.preview_error).map(asset => [asset.media_id, asset.preview_error]));
  return (view.media || []).concat(view.blocks || []).reduce((result, item) => {
    if (item.media_id && item.errorKey && errors.has(item.media_id)) result[item.errorKey] = errors.get(item.media_id);
    return result;
  }, {});
}
function presentationSourceMedia(view) {
  const items = (view && view.media || []).concat(view && view.blocks || []).filter(item => item && ['image', 'video'].includes(item.kind));
  return mergeAssets(items.map(item => ({
    ...item,
    width: Number(item.intrinsicWidth) || Number(item.width) || 0,
    height: Number(item.intrinsicHeight) || Number(item.height) || 0,
  })));
}

Page({
  data: {
    id: '', loading: true, busy: false, mediaUploadMessage: '', error: '', denied: false, disabled: false, loginRequired: false, context: null,
    title: '', owner_id: '', ownerDisplayName: '', ownerIndex: 0, rights: 'own', source_note: '', source_url: '', cover_media_id: '', revision: 0, version: 0, status: 'draft', body_format: 'article_v2',
    presentation: 'article', postText: '', videoLayout: 'channels', shareCopy: '', momentsCopy: '', videoShareExpanded: false, videoDisplayExpanded: false, videoUploadProgress: 0, videoUploadMessage: '', fieldErrors: {}, activeBlockIndex: -1, presentationView: null, presentationMediaErrors: {}, videoStarted: {},
    blocks: [textBlock('')], renderedBlocks: [], imageBlocks: [], selectedImageBlock: null, videoBlocks: [], media: [], preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false,
    legacyReadOnly: false, legacyBody: '', legacyMedia: [], notice: '',
    layoutReadOnly: false, layoutBody: '', layoutMediaIDs: [], layoutBlocks: [],
    documentReadOnly: false, documentAsset: null, documentCandidates: [], deleted: false, serverReadOnly: false,
    settingsExpanded: false, mediaExpanded: false, moreExpanded: false, tagsText: '', pinned: false,
    aiInlineStreaming: false, aiInlineError: '', aiInlineNotice: '', aiInlineCanRestore: false, aiInlineConflict: false,
  },
  onLoad(options) {
    this._unloaded = false;
    this._inlineAIGeneration = 0;
    this._inlineAIActive = null;
    this._inlineAIOriginal = null;
    this._inlineAITask = null;
    this._contentSaveActive = null;
    this._contentPublishActive = null;
    this.setData({ id: String(options.id || '') });
    const channel = this.getOpenerEventChannel && this.getOpenerEventChannel();
    const sourceToken = String(wx.getStorageSync('token') || '');
    this._createToken = sourceToken;
    this._pendingCreateAction = !options.id && ['media', 'document', 'gallery', 'video', 'article'].includes(options.action || options.kind) ? (options.action || options.kind) : '';
    if (channel && channel.on) channel.on('prefillContent', payload => this.receivePrefill(payload, sourceToken));
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || ''), actor = editorActor();
    const changed = token !== this._token || this._actor !== undefined && actor !== this._actor;
    if (changed) {
      this.invalidateInlineSocialAI({ write: false, clearOriginal: true });
      this.setData({ context: null, title: '', owner_id: '', ownerDisplayName: '', ownerIndex: 0, rights: 'own', source_note: '', source_url: '', cover_media_id: '', revision: 0, version: 0, status: 'draft', body_format: 'article_v2', presentation: 'article', postText: '', videoLayout: 'channels', shareCopy: '', momentsCopy: '', videoShareExpanded: false, videoDisplayExpanded: false, videoUploadProgress: 0, videoUploadMessage: '', fieldErrors: {}, activeBlockIndex: -1, presentationView: null, presentationMediaErrors: {}, videoStarted: {}, blocks: [textBlock('')], renderedBlocks: [], imageBlocks: [], selectedImageBlock: null, videoBlocks: [], media: [], preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, legacyReadOnly: false, legacyBody: '', legacyMedia: [], layoutReadOnly: false, layoutBody: '', layoutMediaIDs: [], layoutBlocks: [], documentReadOnly: false, documentAsset: null, documentCandidates: [], deleted: false, serverReadOnly: false, notice: '', error: '', denied: false, disabled: false, busy: false, mediaUploadMessage: '', aiInlineStreaming: false, aiInlineError: '', aiInlineNotice: '', aiInlineCanRestore: false, aiInlineConflict: false });
      this._publishSignature = ''; this._publishKey = ''; this._revokeSignature = ''; this._revokeKey = '';
      this._loaded = false;
      this._savedSignature = '';
      this._mediaUploadActive = null;
      this._contentSaveActive = null;
      this._contentPublishActive = null;
      this._directPublishCheckpoint = null;
      this.setData({ settingsExpanded: false, mediaExpanded: false, moreExpanded: false });
    }
    this._token = token; this._actor = actor; this._epoch = (this._epoch || 0) + 1;
    this.setData({ loginRequired: !token, loading: !!token && !this._loaded, busy: !!this._mediaUploadActive || !!this._contentSaveActive });
    if (token && !this._loaded) this.load(this._epoch);
    else if (token && this._loaded) void this.resumeMediaPreviews(this._epoch);
  },
  onHide() { this.invalidateInlineSocialAI({ restore: true, clearOriginal: false, notice: '' }); this.pauseLayoutVideos(); },
  onUnload() { this._unloaded = true; this.invalidateInlineSocialAI({ write: false, clearOriginal: true }); this._contentPublishActive = null; this._epoch = (this._epoch || 0) + 1; this.pauseLayoutVideos(); },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  retry() { this._loaded = false; const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this.load(epoch); },
  async load(suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token) return;
    const epoch = suppliedEpoch || (this._epoch = (this._epoch || 0) + 1);
    this.setData({ loading: true, error: '', denied: false, disabled: false, loginRequired: false, videoStarted: {} });
    try {
      const context = await api.workbenchContext();
      if (!this.current(epoch)) return;
      if (!context.enabled || context.can_manage === false) { this.setData({ context, denied: context.can_manage === false, disabled: !context.enabled, loading: false }); return; }
      const content = this.data.id ? await api.getContent(this.data.id) : null;
      if (!this.current(epoch)) return;
      const targetOwner = content ? content.owner_id : (context.owners || [])[0] || '';
      const mediaPage = await api.listWorkbenchMedia({ owner: targetOwner });
      if (!this.current(epoch)) return;
      const bodyFormat = content ? content.body_format || 'plain' : 'article_v2';
      const documentReadOnly = !!content && bodyFormat === 'document_v1' && content.type === 'document';
      const legacyReadOnly = !!content && bodyFormat !== 'article_v2' && !documentReadOnly;
      let layoutReadOnly = false;
      if (content && bodyFormat === 'article_v2') { try { const document = JSON.parse(content.body); layoutReadOnly = typeof document.layout_html === 'string' && !!document.layout_html; } catch (_) {} }
      const presentation = content && bodyFormat === 'article_v2' ? contentPresentation().inferContentPresentation(content) : 'article';
      const parsedDocument = articleDocument(content) || {};
      const blocks = initialBlocks(content, presentation);
      const associated = mergeAssets(content && content.media || [], content && content.cover || []);
      const assets = mergeAssets(mediaPage.items || [], associated);
      const previews = new Set(legacyReadOnly || layoutReadOnly ? associated.map(asset => asset.media_id) : mediaIDs(blocks));
      if (content && content.cover) previews.add(content.cover.media_id);
      const waitsForPreviews = legacyReadOnly || layoutReadOnly || documentReadOnly;
      const loadedMedia = waitsForPreviews ? await this.loadPrivatePaths(assets, previews) : assets.map(asset => ({ ...asset, local_path: '' }));
      if (!this.current(epoch)) return;
      this._loaded = true;
      const legacyIDs = new Set(associated.map(asset => asset.media_id));
      const media = loadedMedia.filter(asset => asset.kind === 'image' || asset.kind === 'video');
      const documentCandidates = loadedMedia.filter(asset => asset.kind === 'document');
      const documentAsset = documentReadOnly ? loadedMedia.find(asset => associated.some(item => item.media_id === asset.media_id) && asset.kind === 'document') || null : null;
      const layoutBlocks = layoutReadOnly ? renderArticleDocument(content.body, media, this.editorWidth(), 'contentedit-layout') : [];
      const ownerID = content ? content.owner_id : (context.owners || [])[0] || '';
      const ownerChoices = context.owner_choices || [];
      const ownerIndex = Math.max(0, ownerChoices.findIndex(item => item.owner_id === ownerID));
      this.setData({ context, presentation: layoutReadOnly ? 'article' : presentation, postText: postText(blocks), videoLayout: parsedDocument.video_layout === 'douyin' ? 'douyin' : 'channels', shareCopy: String(parsedDocument.share_copy || ''), momentsCopy: String(parsedDocument.moments_copy || ''), media, documentCandidates, title: content ? content.title : '', tagsText: content && content.tags ? content.tags.join(' ') : '', pinned: !!(content && content.pinned), owner_id: ownerID, ownerDisplayName: ownerChoices[ownerIndex] && ownerChoices[ownerIndex].display_name || '', ownerIndex, rights: content ? content.rights : 'own', source_note: content ? content.source_note || '' : '', source_url: content ? content.source_url || '' : '', cover_media_id: content && content.cover ? content.cover.media_id : '', revision: content ? content.revision : 0, version: content ? content.version : 0, status: content ? content.status : 'draft', body_format: bodyFormat, legacyReadOnly, legacyBody: legacyReadOnly ? content.body || '' : '', legacyMedia: legacyReadOnly ? loadedMedia.filter(asset => legacyIDs.has(asset.media_id)) : [], layoutReadOnly, layoutBody: layoutReadOnly ? content.body : '', layoutMediaIDs: layoutReadOnly ? (content.media || []).map(asset => asset.media_id) : [], layoutBlocks, documentReadOnly, documentAsset, serverReadOnly: !!(content && content.read_only), blocks, ...blockViews(blocks, media), loading: false, aiInlineConflict: false });
      this.applyPendingPrefill();
      this._savedSignature = content ? this.draftSignature() : '';
      this.performCreateAction();
      if (!waitsForPreviews && previews.size) void this.hydratePrivatePaths(epoch, assets, previews);
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '编辑器加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false }); }
  },
  editorWidth() { const window = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync ? wx.getSystemInfoSync() : { windowWidth: 375 }; return Math.max(240, Number(window.windowWidth || 375) - 64); },
  performCreateAction() {
    if (!this._loaded || !this.data.context || !this.data.context.enabled || this.data.context.can_manage === false) return;
    const action = this._pendingCreateAction;
    this._pendingCreateAction = '';
    if (!action || this.data.id || this._createToken !== String(wx.getStorageSync('token') || '')) return;
    if (['gallery', 'video', 'article'].includes(action)) this.setData({ presentation: action, postText: postText(this.data.blocks) });
    if (action === 'media' || action === 'gallery' || action === 'video') this.chooseMedia();
    if (action === 'document') this.chooseDocument();
  },
  toggleSettings() { this.setData({ settingsExpanded: !this.data.settingsExpanded }); },
  toggleMedia() { this.setData({ mediaExpanded: !this.data.mediaExpanded }); },
  selectPresentation(event) {
    if (this.data.busy || this.data.layoutReadOnly || this.data.legacyReadOnly || this.data.documentReadOnly) return;
    const presentation = String(event.currentTarget.dataset.presentation || 'article');
    if (!['article', 'gallery', 'video'].includes(presentation) || presentation === this.data.presentation) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks || [];
    if (presentation !== 'article' && hasStructuredBlocks(blocks)) { wx.showToast({ title: '已有文章排版，请保持文章模式', icon: 'none' }); return; }
    if (presentation === 'gallery' && mediaBlocks(blocks, 'video').length) { wx.showToast({ title: '已有视频，请先移除再切换图集', icon: 'none' }); return; }
    if (presentation === 'video' && (mediaBlocks(blocks, 'image').length || mediaBlocks(blocks, 'video').length > 1)) { wx.showToast({ title: '视频笔记仅保留一个主视频', icon: 'none' }); return; }
    const next = presentation === 'article' ? blocks : socialBlocks(blocks);
    this.setData({ presentation, blocks: next, ...blockViews(next, this.data.media), postText: postText(next), preview: null, notice: '已切换排版，请保存后预览' });
  },
  editPostText(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation({ preserveGenerated: true })) return;
    const text = String(event.detail.value || '');
    const media = this.data.blocks.filter(block => block.kind === 'image' || block.kind === 'video');
    const blocks = text.trim() ? media.concat(textBlock(text)) : media;
    const next = blocks.length ? blocks : [textBlock('')];
    this.setData({ postText: text, blocks: next, ...blockViews(next, this.data.media), preview: null, notice: '有未保存修改' });
  },
  selectBlock(event) { this.setData({ activeBlockIndex: Number(event.currentTarget.dataset.index) }); },
  selectGalleryImage(event) {
    const blockIndex = Number(event.currentTarget.dataset.index);
    const selectedImageBlock = this.data.imageBlocks.find(block => block.blockIndex === blockIndex) || null;
    if (selectedImageBlock) this.setData({ selectedImageBlock });
  },
  sortSelectedGalleryImage() {
    const selected = this.data.selectedImageBlock;
    if (this.data.busy || !selected) return;
    if (!this.prepareInlineEditorMutation()) return;
    const launch = { token: this._token, id: this.data.id, signature: this.draftSignature(), mediaID: selected.media_id };
    wx.showActionSheet({
      itemList: ['前移', '后移'],
      success: result => {
        const current = this.data.selectedImageBlock;
        if (!this.sameSaveAccount(launch.token) || launch.id !== this.data.id || launch.signature !== this.draftSignature()
          || this.data.busy || !current || current.media_id !== launch.mediaID || (result.tapIndex !== 0 && result.tapIndex !== 1)) return;
        this.moveBlock({ currentTarget: { dataset: { index: current.blockIndex, step: result.tapIndex === 0 ? -1 : 1 } } });
      },
    });
  },
  openCoverPicker() {
    if (this.data.busy || this.data.documentReadOnly || this.data.legacyReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const launch = { token: this._token, id: this.data.id, signature: this.draftSignature(), handled: false, cover: true };
    wx.navigateTo({
      url: '/pages/nativereferences/nativereferences?picker=media&kind=image' + (this.data.owner_id ? '&owner=' + encodeURIComponent(this.data.owner_id) : ''),
      events: { selectMaterial: payload => this.receivePickedMedia(payload, launch) },
      fail: () => this.setData({ error: '素材选择页暂时无法打开，请重试' }),
    });
  },
  openMediaPicker() {
    if (this.data.busy) return;
    if (!this.prepareInlineEditorMutation()) return;
    const launch = { token: this._token, id: this.data.id, signature: this.draftSignature(), handled: false, replaceVideo: this.data.presentation === 'video' && mediaBlocks(this.data.blocks, 'video').length > 0 };
    const kinds = this.data.presentation === 'gallery' ? 'image' : this.data.presentation === 'video' ? 'video' : 'image%2Cvideo';
    wx.navigateTo({
      url: '/pages/nativereferences/nativereferences?picker=media&kind=' + kinds + (this.data.owner_id ? '&owner=' + encodeURIComponent(this.data.owner_id) : ''),
      events: { selectMaterial: payload => this.receivePickedMedia(payload, launch) },
    });
  },
  chooseVideoSource() {
    if (this.data.busy || this.data.presentation !== 'video' || this.data.videoBlocks.length) return;
    if (!this.prepareInlineEditorMutation()) return;
    const launch = { token: this._token, id: this.data.id, signature: this.draftSignature() };
    wx.showActionSheet({
      itemList: ['从手机选择', '从素材库选择'],
      success: result => {
        if (!this.sameSaveAccount(launch.token) || launch.id !== this.data.id || launch.signature !== this.draftSignature()
          || this.data.busy || this.data.presentation !== 'video' || this.data.videoBlocks.length) return;
        if (result.tapIndex === 0) this.chooseMedia();
        if (result.tapIndex === 1) this.openMediaPicker();
      },
    });
  },
  async receivePickedMedia(payload, launch) {
    const asset = payload && payload.kind === 'media' ? payload.asset : null;
    if (!asset || !asset.media_id || (asset.kind !== 'image' && asset.kind !== 'video') || launch.cover && asset.kind !== 'image') return;
    // 选择页返回会触发 onShow，改变请求 epoch；用原草稿身份保护跨页素材回填。
    const current = () => !this._unloaded && launch.token === this._token && launch.token === String(wx.getStorageSync('token') || '') && launch.id === this.data.id && launch.signature === this.draftSignature() && !this.data.busy && (!this.data.layoutReadOnly || launch.cover);
    if (launch.handled || !current()) return;
    launch.handled = true;
    let selected = { ...asset };
    try { selected.local_path = await api.downloadWorkbenchMedia(asset.media_id); }
    catch (_) { selected.preview_error = '私有媒体预览加载失败'; }
    if (!current()) return;
    const media = mergeAssets(this.data.media, [selected]);
    if (launch.cover) { this.setData({ media, cover_media_id: asset.media_id, preview: null, notice: '封面已选择，请保存' }); return; }
    if (this.data.presentation === 'gallery' && asset.kind !== 'image') { this.setData({ media, error: '图集仅支持加入图片' }); return; }
    if (this.data.presentation === 'video' && asset.kind !== 'video') { this.setData({ media, error: '视频笔记请选择视频' }); return; }
    const baseBlocks = launch.replaceVideo ? this.data.blocks.filter(block => block.kind !== 'video') : this.data.blocks;
    const appended = baseBlocks.concat({ kind: asset.kind, media_id: asset.media_id, caption: '' });
    const blocks = this.data.presentation === 'article' ? appended : socialBlocks(appended);
    this.setData({ media, blocks, cover_media_id: this.data.cover_media_id || (asset.kind === 'image' ? asset.media_id : ''), ...blockViews(blocks, media), preview: null, notice: '已加入正文' });
  },
  toggleMore() { if (!this.data.aiInlineStreaming && !this.blockInlineAIConflict()) this.setData({ moreExpanded: !this.data.moreExpanded }); },
  returnToEdit() {
    if (this.data.busy) return;
    this.pauseLayoutVideos();
    this.setData({ preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, notice: '' });
  },
  toggleContentDescription() {
    if (!this.data.presentationView) return;
    this.setData({ presentationView: { ...this.data.presentationView, descriptionExpanded: !this.data.presentationView.descriptionExpanded } });
  },
  handleBack() {
    if (this.data.busy) { wx.showToast({ title: '正在处理，请稍候', icon: 'none' }); return; }
    if (this.data.preview) { this.returnToEdit(); return; }
    const leave = () => wx.navigateBack({ fail: () => this.backToWorkbench() });
    const edited = this._savedSignature ? this._savedSignature !== this.draftSignature() : !this.data.id && (String(this.data.title || '').trim() || this.validBlocks().length);
    if (!edited || this.data.deleted || this.data.documentReadOnly || this.data.legacyReadOnly) { leave(); return; }
    const epoch = this._epoch;
    wx.showModal({ title: '离开编辑？', content: '还有未保存的修改', confirmText: '离开', cancelText: '继续编辑', success: result => { if (result.confirm && this.current(epoch)) leave(); } });
  },
  pauseLayoutVideos() {
    const presentationVideos = this.data.presentationView ? (this.data.presentationView.media || []).concat(this.data.presentationView.blocks || []).filter(block => block.kind === 'video') : [];
    [...new Set([...(this.data.layoutBlocks || []), ...(this.data.previewLayoutBlocks || []), ...presentationVideos].filter(block => block.kind === 'video' && block.domId).map(block => block.domId))].forEach(domId => wx.createVideoContext(domId, this).pause());
  },
  draftSignature() {
    return JSON.stringify({ id: this.data.id, title: this.data.title, presentation: this.data.presentation, videoLayout: this.data.videoLayout, shareCopy: this.data.shareCopy, momentsCopy: this.data.momentsCopy, owner_id: this.data.owner_id, rights: this.data.rights, source_note: this.data.source_note, source_url: this.data.source_url, cover_media_id: this.data.cover_media_id, revision: this.data.revision, blocks: this.data.blocks, tagsText: this.data.tagsText, pinned: this.data.pinned });
  },
  rejectWritingReturn(message) {
    this.setData({ notice: message });
    // toast 带原因：只写"未采用"用户不知道下一步该做什么。
    wx.showToast({ title: message, icon: 'none' });
    return message;
  },
  // T1.1 双向确认：回填结果分「已应用 / 暂时拒绝 / 目标失效」三类回传 AI 页。
  // 已应用的重复请求只重放成功确认，不重复追加；暂时拒绝可重试；目标失效须重新发起。
  receiveWritingReturn(payload, launch) {
    const ack = this.settleWritingReturn(payload, launch);
    if (launch && launch.channel && typeof launch.channel.emit === 'function') {
      try { launch.channel.emit('writingAck', ack); } catch (_) { /* 通道已关闭时由 AI 页超时兜底，文案留在 AI 页 */ }
    }
    return ack;
  },
  writingAck(state, message, launch, payload, replayed) {
    return { state, message, replayed, request: Number(payload && payload.request) || 0, operation: String(payload && payload.operation || '') };
  },
  writingReturnFingerprint(payload) {
    const video = payload && payload.video_copy;
    return JSON.stringify({
      title: String(payload && payload.title || ''),
      text: String(payload && payload.text || ''),
      video_copy: video && typeof video === 'object' ? video : null,
    });
  },
  // 已应用后的重放只看「目标是否仍是同一个」：正文已被本次采纳改过，不能再拿旧签名比对。
  sameAppliedWritingTarget(launch) {
    return launch.token === String(wx.getStorageSync('token') || '')
      && launch.contentID === String(this.data.id || '')
      && launch.revision === Number(this.data.revision || 0)
      && !this.data.deleted && !this.data.serverReadOnly;
  },
  settleWritingReturn(payload, launch) {
    const fingerprint = this.writingReturnFingerprint(payload);
    if (launch.state === 'applied') {
      // 账号/草稿实例/修订在应用后发生变化时，旧成功不得再重放。
      if (!this.sameAppliedWritingTarget(launch)) {
        launch.state = 'invalid';
        launch.message = '账号或草稿已变化，AI辅助结果未采用；请重新发起';
        return this.writingAck('invalid', this.rejectWritingReturn(launch.message), launch, payload, false);
      }
      if (launch.appliedFingerprint && launch.appliedFingerprint !== fingerprint) {
        launch.state = 'invalid';
        launch.message = '本次AI辅助结果已采用过，修改后的内容未采用；请返回后重新发起';
        return this.writingAck('invalid', this.rejectWritingReturn(launch.message), launch, payload, false);
      }
      this.setData({ notice: 'AI辅助结果已处理，未重复追加' });
      return this.writingAck('applied', 'AI辅助结果已处理，未重复追加', launch, payload, true);
    }
    if (launch.state === 'invalid') return this.writingAck('invalid', launch.message, launch, payload, true);
    const accountChanged = launch.token !== String(wx.getStorageSync('token') || '');
    if (accountChanged || launch.contentID !== String(this.data.id || '') || launch.revision !== Number(this.data.revision || 0)) {
      launch.state = 'invalid';
      launch.message = accountChanged ? '账号已变化，AI辅助结果未采用；原内容保持不变' : '草稿版本已变化，AI辅助结果未采用；请重新发起';
      return this.writingAck('invalid', this.rejectWritingReturn(launch.message), launch, payload, false);
    }
    if (launch.signature !== this.draftSignature()) {
      launch.state = 'invalid';
      launch.message = '编辑内容已发生变化，AI辅助结果未采用；原内容保持不变';
      return this.writingAck('invalid', this.rejectWritingReturn(launch.message), launch, payload, false);
    }
    if (this.data.busy) {
      return this.writingAck('retryable', this.rejectWritingReturn('草稿正在保存或处理，AI辅助结果未采用；原内容保持不变'), launch, payload, false);
    }
    const videoCopy = payload && payload.video_copy;
    if (this.data.presentation === 'video' && videoCopy && typeof videoCopy === 'object') {
      const changes = {};
      if (Object.prototype.hasOwnProperty.call(videoCopy, 'title')) changes.title = String(videoCopy.title || '').trim().slice(0, 80);
      if (Object.prototype.hasOwnProperty.call(videoCopy, 'share_copy')) changes.shareCopy = String(videoCopy.share_copy || '').trim().slice(0, 2000);
      if (Object.prototype.hasOwnProperty.call(videoCopy, 'moments_copy')) changes.momentsCopy = String(videoCopy.moments_copy || '').trim().slice(0, 2000);
      if (!Object.keys(changes).length) return this.writingAck('retryable', this.rejectWritingReturn('返回的 AI 视频文案为空，未采用'), launch, payload, false);
      launch.state = 'applied';
      launch.message = 'AI辅助结果已填入，请核对后保存';
      launch.appliedFingerprint = fingerprint;
      this.setData({ ...changes, preview: null, notice: launch.message });
      return this.writingAck('applied', launch.message, launch, payload, false);
    }
    const title = String(payload && payload.title || '').trim();
    const text = String(payload && payload.text || '').trim();
    if (!title && !text) return this.writingAck('retryable', this.rejectWritingReturn('返回的AI辅助结果为空，未采用'), launch, payload, false);
    const blocks = this.validBlocks();
    if (!String(this.data.title || '').trim() && !blocks.length) {
      const outcome = this.receivePrefill({ title, text }, launch.token, launch, fingerprint);
      if (outcome === 'applied') { launch.state = 'applied'; launch.message = 'AI辅助结果已带入，请核对后保存'; launch.appliedFingerprint = fingerprint; return this.writingAck('applied', launch.message, launch, payload, false); }
      if (outcome === 'deferred') return this.writingAck('retryable', '编辑器正在载入，AI辅助结果会随后自动带入；请稍候再确认', launch, payload, false);
      return this.writingAck('retryable', '当前草稿已有内容，AI辅助结果未带入；原内容保持不变', launch, payload, false);
    }
    if (!text) return this.writingAck('retryable', this.rejectWritingReturn('AI辅助结果没有正文，未采用；原内容保持不变'), launch, payload, false);
    if (blocks.some(block => String(block.text || '').trim() === text)) {
      launch.state = 'applied';
      launch.message = 'AI辅助结果已处理，正文中已存在，未重复追加';
      launch.appliedFingerprint = fingerprint;
      this.setData({ notice: launch.message });
      return this.writingAck('applied', launch.message, launch, payload, false);
    }
    const next = blocks.concat(textBlock(text));
    launch.state = 'applied';
    launch.message = 'AI辅助结果已追加到正文末尾，请核对后保存';
    launch.appliedFingerprint = fingerprint;
    this.setData({ blocks: next, postText: postText(next), ...blockViews(next, this.data.media), preview: null, notice: launch.message });
    return this.writingAck('applied', launch.message, launch, payload, false);
  },
  receivePrefill(payload, sourceToken, launch, fingerprint) {
    if (this.data.id || this.data.busy) return 'dropped';
    const title = String(payload && payload.title || '').trim();
    const text = String(payload && payload.text || '').trim();
    if (!title && !text) return 'dropped';
    this._pendingPrefill = { title, text, token: String(sourceToken === undefined ? wx.getStorageSync('token') || '' : sourceToken), launch: launch || null, fingerprint: fingerprint || '' };
    return this.applyPendingPrefill();
  },
  applyPendingPrefill() {
    const pending = this._pendingPrefill;
    if (!pending) return 'deferred';
    // 已失效的操作不得靠延后带入复活。
    if (pending.launch && pending.launch.state === 'invalid') { delete this._pendingPrefill; return 'dropped'; }
    if (!this._loaded || !this.data.context) return 'deferred';
    delete this._pendingPrefill;
    const token = String(wx.getStorageSync('token') || '');
    if (this.data.id || pending.token !== token || String(this.data.title || '').trim() || this.validBlocks().length) return 'dropped';
    // 写入前重新核对发起时的账号与草稿身份，避免切到别的草稿后仍被旧 pending 写入。
    if (pending.launch && (pending.launch.token !== token || pending.launch.contentID !== String(this.data.id || '') || pending.launch.revision !== Number(this.data.revision || 0))) return 'dropped';
    const blocks = pending.text ? [textBlock(pending.text)] : this.data.blocks;
    // 延后带入也要回写原操作的终态，否则同请求重试会被正文变化误判成目标失效。
    if (pending.launch && pending.launch.state !== 'applied') {
      pending.launch.state = 'applied';
      pending.launch.message = 'AI辅助结果已带入，请核对后保存';
      pending.launch.appliedFingerprint = pending.fingerprint;
    }
    this.setData({ title: pending.title, blocks, postText: postText(blocks), ...blockViews(blocks, this.data.media), preview: null, notice: 'AI辅助结果已带入，请核对后保存' });
    return 'applied';
  },
  openAIWriting() {
    const context = this.data.context;
    if (this.data.busy || this.data.deleted || this.data.documentReadOnly || this.data.legacyReadOnly || this.data.layoutReadOnly || this.data.serverReadOnly
      || context && (context.enabled === false || context.can_manage === false)) return;
    this.startInlineSocialAI();
  },
  inlineSocialIdentity() {
    return {
      token: String(wx.getStorageSync('token') || ''), actor: editorActor(), contentID: String(this.data.id || ''),
      revision: Number(this.data.revision || 0), ownerID: String(this.data.owner_id || ''), presentation: this.data.presentation,
    };
  },
  inlineSocialIdentityMatches(value) {
    const current = this.inlineSocialIdentity();
    return !!value && !this._unloaded && value.token === current.token && (!this._token || value.token === this._token)
      && value.actor === current.actor && value.contentID === current.contentID && value.revision === current.revision
      && value.ownerID === current.ownerID && value.presentation === current.presentation;
  },
  applyInlineSocialText(text, launch) {
    const value = String(text || '');
    if (this.data.presentation === 'article') {
      const plan = launch && launch.original && launch.original.articlePlan;
      if (!plan) return false;
      const blocks = cloneEditorBlocks(this.data.blocks);
      if (plan.append) {
        if (blocks.length === plan.targetIndex) blocks.push(textBlock(value));
        else if (blocks.length === plan.targetIndex + 1 && blocks[plan.targetIndex] && blocks[plan.targetIndex].kind === 'paragraph') blocks[plan.targetIndex] = textBlock(value);
        else return false;
      } else {
        if (!blocks[plan.targetIndex] || blocks[plan.targetIndex].kind !== 'paragraph') return false;
        blocks[plan.targetIndex] = textBlock(value);
      }
      this.setData({ postText: postText(blocks), blocks, ...blockViews(blocks, this.data.media), preview: null });
      return true;
    }
    const media = (this.data.blocks || []).filter(block => block.kind === 'image' || block.kind === 'video');
    const blocks = value.trim() ? media.concat(textBlock(value)) : (media.length ? media : [textBlock('')]);
    this.setData({ postText: value, blocks, ...blockViews(blocks, this.data.media), preview: null });
    return true;
  },
  restoreInlineSocialOriginal(original) {
    const state = original || this._inlineAIOriginal;
    if (!state || !this.inlineSocialIdentityMatches(state)) return false;
    if (state.presentation === 'article') {
      if (!articleRestoreMatches(state, this.data.blocks)) return false;
    } else if (state.mediaSignature !== socialMediaSignature(this.data.blocks)) return false;
    const blocks = cloneEditorBlocks(state.blocks);
    this.setData({ postText: state.postText, blocks, ...blockViews(blocks, this.data.media), preview: null });
    return true;
  },
  restoreStaleInlineOriginal(launch) {
    if (!launch || !launch.generatedBlocksSignature) return true;
    if (launch.generatedBlocksSignature !== JSON.stringify(this.data.blocks)
      || launch.generatedPostText !== String(this.data.postText || '')) return false;
    const blocks = cloneEditorBlocks(launch.original.blocks);
    this.setData({ postText: launch.original.postText, blocks, ...blockViews(blocks, this.data.media), preview: null });
    return true;
  },
  blockInlineAIConflict() {
    if (!this.data.aiInlineConflict) return false;
    this.setData({ error: 'AI生成期间草稿身份已变化，请重新进入编辑页后再保存' });
    return true;
  },
  invalidateInlineSocialAI(options = {}) {
    const active = this._inlineAIActive;
    if (this._inlineAITask && typeof this._inlineAITask.abort === 'function') { try { this._inlineAITask.abort(); } catch (_) { /* 请求已结束 */ } }
    this._inlineAITask = null;
    this._inlineAIActive = null;
    this._inlineAIGeneration = (this._inlineAIGeneration || 0) + 1;
    if (options.restore && active) this.restoreInlineSocialOriginal(active.original);
    if (options.clearOriginal) this._inlineAIOriginal = null;
    if (options.write !== false && !this._unloaded) this.setData({ aiInlineStreaming: false, aiInlineError: '', aiInlineNotice: options.notice || '', aiInlineCanRestore: options.clearOriginal ? false : this.data.aiInlineCanRestore });
  },
  prepareInlineEditorMutation(options = {}) {
    if (this.data.aiInlineConflict) { this.blockInlineAIConflict(); return false; }
    if (!this.data.aiInlineStreaming && !this.data.aiInlineCanRestore) return true;
    const active = this._inlineAIActive;
    const recovered = !this.data.aiInlineStreaming || options.preserveGenerated || !active || this.restoreStaleInlineOriginal(active);
    this.invalidateInlineSocialAI({ clearOriginal: true, notice: '' });
    if (!recovered) this.setData({ aiInlineConflict: true, aiInlineError: '草稿已变化，请重新进入编辑页后再保存' });
    return recovered;
  },
  restoreInlineAIOriginal() {
    if (!this._inlineAIOriginal) return;
    this.invalidateInlineSocialAI({ clearOriginal: false, notice: '' });
    if (!this.restoreInlineSocialOriginal(this._inlineAIOriginal)) {
      this._inlineAIOriginal = null;
      this.setData({ aiInlineCanRestore: false, aiInlineError: '素材或草稿已变化，无法恢复生成前内容' });
      return;
    }
    this._inlineAIOriginal = null;
    this.setData({ aiInlineCanRestore: false, aiInlineError: '', aiInlineNotice: this.data.presentation === 'article' ? '已恢复生成前的正文' : '已恢复生成前的介绍', notice: '有未保存修改' });
  },
  startInlineSocialAI() {
    if (this.data.busy || this.data.aiInlineStreaming || this.blockInlineAIConflict() || !['article', 'gallery', 'video'].includes(this.data.presentation)) return;
    const title = String(this.data.title || '').trim();
    const description = String(this.data.postText || '').trim();
    const media = (this.data.blocks || []).filter(block => block.kind === 'image' || block.kind === 'video');
    const captions = media.map(block => String(block.caption || '').trim()).filter(Boolean).join('\n');
    const facts = writingFacts(this.data.blocks || []);
    if (!title && !(this.data.presentation === 'article' ? facts : description || captions)) {
      this.setData({ aiInlineError: '请先填写标题或内容要点', aiInlineNotice: '' });
      return;
    }
    const identity = this.inlineSocialIdentity();
    const mediaSignature = socialMediaSignature(this.data.blocks);
    let original = this._inlineAIOriginal;
    if (!original || !this.inlineSocialIdentityMatches(original) || original.mediaSignature !== mediaSignature) {
      original = { ...identity, postText: String(this.data.postText || ''), blocks: cloneEditorBlocks(this.data.blocks), mediaSignature,
        articlePlan: this.data.presentation === 'article' ? articleAIPlan(this.data.blocks) : null };
      this._inlineAIOriginal = original;
    }
    const launch = { ...identity, generation: (this._inlineAIGeneration || 0) + 1, expectedSignature: this.draftSignature(), original };
    this._inlineAIGeneration = launch.generation;
    this._inlineAIActive = launch;
    const current = () => this._inlineAIActive === launch && launch.generation === this._inlineAIGeneration && this.inlineSocialIdentityMatches(launch);
    const abandon = () => {
      if (this._inlineAIActive !== launch) return;
      if (this._inlineAITask && typeof this._inlineAITask.abort === 'function') this._inlineAITask.abort();
      const recovered = this.restoreStaleInlineOriginal(launch);
      this._inlineAITask = null; this._inlineAIActive = null; this._inlineAIOriginal = null; this._inlineAIGeneration += 1;
      if (!this._unloaded) this.setData({ aiInlineStreaming: false, aiInlineCanRestore: false, aiInlineConflict: !recovered,
        aiInlineError: recovered ? '' : '草稿已变化，请重新进入编辑页后再保存', aiInlineNotice: '' });
    };
    const restoreAfterFailure = message => {
      if (!current()) { abandon(); return; }
      this._inlineAIActive = null; this._inlineAITask = null; this._inlineAIGeneration += 1;
      const restored = !launch.generatedBlocksSignature || this.restoreInlineSocialOriginal(original);
      this._inlineAIOriginal = null;
      this.setData({ aiInlineStreaming: false, aiInlineCanRestore: false, aiInlineConflict: !restored,
        aiInlineError: restored ? message || '生成失败，请重试' : '草稿已变化，请重新进入编辑页后再保存', aiInlineNotice: '' });
    };
    const accept = text => {
      if (!current()) { abandon(); return false; }
      if (launch.expectedSignature !== this.draftSignature()) {
        if (this._inlineAITask && typeof this._inlineAITask.abort === 'function') this._inlineAITask.abort();
        this._inlineAITask = null; this._inlineAIActive = null; this._inlineAIGeneration += 1;
        const restored = !launch.generatedBlocksSignature || this.restoreInlineSocialOriginal(original);
        this._inlineAIOriginal = null;
        this.setData({ aiInlineStreaming: false, aiInlineCanRestore: false, aiInlineConflict: !restored,
          aiInlineError: restored ? '' : '草稿已变化，请重新进入编辑页后再保存', aiInlineNotice: restored ? '检测到编辑内容变化，已停止生成并保留你的修改' : '' });
        return false;
      }
      if (!this.applyInlineSocialText(text, launch)) { restoreAfterFailure('正文结构已变化，请重试'); return false; }
      launch.expectedSignature = this.draftSignature();
      launch.generatedBlocksSignature = JSON.stringify(this.data.blocks);
      launch.generatedPostText = String(this.data.postText || '');
      return true;
    };
    const prompt = this.data.presentation === 'article'
      ? noteWritingPrompt({ title, facts, extra: '只输出可直接放入正文的内容，不要输出标题、字段名、Markdown 或解释。' })
      : socialIntroPrompt({ presentation: this.data.presentation, title, description, captions,
        media_summary: this.data.presentation === 'video' ? `${media.length ? 1 : 0} 段视频` : `${media.length} 张图片` });
    this.setData({ aiInlineStreaming: true, aiInlineError: '', aiInlineNotice: this.data.presentation === 'article' ? 'AI 正在生成正文…' : 'AI 正在生成介绍…', aiInlineCanRestore: false });
    try {
      const task = startAIStream({
        messages: [{ role: 'user', content: prompt }],
        onChunk: partial => { accept(String(partial || '')); },
        onDone: full => {
          const text = String(full || '').trim();
          if (!text) { restoreAfterFailure(this.data.presentation === 'article' ? '生成内容为空，原内容已保留' : '生成内容为空，原介绍已保留'); return; }
          if (!accept(text)) return;
          this._inlineAIActive = null; this._inlineAITask = null; this._inlineAIGeneration += 1;
          this.setData({ aiInlineStreaming: false, aiInlineError: '', aiInlineNotice: '已生成', aiInlineCanRestore: this.data.presentation === 'article' || text !== original.postText, notice: '有未保存修改' });
        },
        onError: message => restoreAfterFailure(message),
      });
      if (this._inlineAIActive === launch) this._inlineAITask = task;
      else if (task && typeof task.abort === 'function') task.abort();
    } catch (error) { restoreAfterFailure(error && error.message || '生成失败，请重试'); }
  },
  async loadPrivatePaths(items, previews) {
    return Promise.all(items.map(async asset => {
      if (!previews.has(asset.media_id)) return { ...asset, local_path: '' };
      try { return { ...asset, local_path: await api.downloadWorkbenchMedia(asset.media_id) }; }
      catch (_) { return { ...asset, local_path: '', preview_error: '预览暂不可用' }; }
    }));
  },
  async hydratePrivatePaths(epoch, items, previews) {
    const loaded = await this.loadPrivatePaths(items, previews);
    if (!this.current(epoch) || !this._loaded) return;
    const paths = new Map(loaded.map(asset => [asset.media_id, asset]));
    const mergePaths = assets => assets.map(asset => {
      const preview = paths.get(asset.media_id);
      if (!preview || asset.local_path) return asset;
      return { ...asset, local_path: preview.local_path || '', preview_error: preview.preview_error || '' };
    });
    const media = mergePaths(this.data.media || []);
    this.setData({ media, documentCandidates: mergePaths(this.data.documentCandidates || []), ...blockViews(this.data.blocks, media) });
  },
  async resumeMediaPreviews(epoch) {
    const previews = new Set(mediaIDs(this.data.blocks || []));
    if (this.data.cover_media_id) previews.add(this.data.cover_media_id);
    const missing = (this.data.media || []).filter(asset => previews.has(asset.media_id) && !asset.local_path);
    if (missing.length) await this.hydratePrivatePaths(epoch, missing, previews);
  },
  async previewMedia(event) {
    const mediaID = String(event.currentTarget.dataset.mediaId || '');
    const requestedIndex = Number(event.currentTarget.dataset.index);
    const index = mediaID ? this.data.media.findIndex(item => item.media_id === mediaID) : requestedIndex;
    const asset = this.data.media[index];
    if (!asset || asset.local_path || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const media = this.data.media.slice();
      media[index] = { ...asset, local_path: await api.downloadWorkbenchMedia(asset.media_id), preview_error: '' };
      if (!this.current(epoch)) return;
      this.setData({ media, ...blockViews(this.data.blocks, media), busy: false });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '私有媒体预览加载失败' }); }
  },
  setField(event) { if (!this.prepareInlineEditorMutation()) return; const field = event.currentTarget.dataset.field; this.setData({ [field]: event.detail.value, [`fieldErrors.${field}`]: '', error: '', preview: null, notice: '有未保存修改' }); },
  setVideoLayout(event) { if (!this.prepareInlineEditorMutation()) return; this.setData({ videoLayout: event.currentTarget.dataset.layout === 'douyin' ? 'douyin' : 'channels', error: '', preview: null, notice: '有未保存修改' }); },
  openEditorVideo(event) {
    const asset = this.data.media.find(item => item.media_id === event.currentTarget.dataset.mediaId && item.kind === 'video');
    if (!asset || !asset.local_path) { wx.showToast({ title: '视频未加载，请重试', icon: 'none' }); return; }
    wx.previewMedia({ sources: [{ url: asset.local_path, type: 'video', poster: asset.poster_path || '' }], showmenu: false,
      fail: error => { if (!/cancel/.test(String(error.errMsg || ''))) wx.showToast({ title: '播放失败，请重试', icon: 'none' }); },
    });
  },
  copyVideoField(event) {
    const field = event.currentTarget.dataset.field === 'momentsCopy' ? 'momentsCopy' : 'shareCopy';
    const value = String(this.data[field] || '').trim();
    if (!value) { wx.showToast({ title: '还没有可复制的文案', icon: 'none' }); return; }
    wx.setClipboardData({ data: value, success: () => this.setData({ notice: '文案已复制' }), fail: () => wx.showToast({ title: '复制失败，请重试', icon: 'none' }) });
  },
  toggleVideoShare() { this.setData({ videoShareExpanded: !this.data.videoShareExpanded, videoDisplayExpanded: false }); },
  toggleVideoDisplay() { this.setData({ videoDisplayExpanded: !this.data.videoDisplayExpanded, videoShareExpanded: false }); },
  closeVideoOptions() { this.setData({ videoShareExpanded: false, videoDisplayExpanded: false }); },
  openVideoSettings() { this.setData({ videoShareExpanded: false, videoDisplayExpanded: false, settingsExpanded: true }); },
  setTags(event) { if (!this.prepareInlineEditorMutation()) return; this.setData({ tagsText: String(event.detail.value || ''), preview: null }); },
  togglePinned() { if (!this.data.aiInlineStreaming && !this.blockInlineAIConflict()) this.setData({ pinned: !this.data.pinned, preview: null }); },
  setRights(event) { if (!this.prepareInlineEditorMutation()) return; this.setData({ rights: event.detail.value, preview: null }); },
  setOwner(event) {
    if (this.data.busy || this.data.id) return;
    if (!this.prepareInlineEditorMutation()) return;
    const ownerIndex = Number(event.detail.value) || 0, choice = (this.data.context.owner_choices || [])[ownerIndex];
    if (!choice || choice.owner_id === this.data.owner_id) return;
    const token = this._token, previousOwner = this.data.owner_id;
    const apply = async () => {
      if (!this.sameSaveAccount(token) || this.data.id || this.data.busy || this.data.owner_id !== previousOwner) return;
      const blocks = this.data.blocks.filter(block => !block.media_id);
      const next = blocks.length ? blocks : [textBlock('')];
      this.setData({ owner_id: choice.owner_id, ownerDisplayName: choice.display_name, ownerIndex, blocks: next, ...blockViews(next, []), media: [], documentCandidates: [], cover_media_id: '', 'fieldErrors.owner_id': '', error: '', preview: null });
      try {
        const page = await api.listWorkbenchMedia({ owner: choice.owner_id });
        if (this.sameSaveAccount(token) && !this.data.id && this.data.owner_id === choice.owner_id) {
          const media = mergeAssets(this.data.media, (page.items || []).filter(asset => asset.kind === 'image' || asset.kind === 'video'));
          this.setData({ media, documentCandidates: (page.items || []).filter(asset => asset.kind === 'document'), ...blockViews(this.data.blocks, media) });
        }
      } catch (error) { if (this.sameSaveAccount(token) && this.data.owner_id === choice.owner_id) this.setData({ error: error.message || '素材加载失败，请重试选择素材' }); }
    };
    if (mediaIDs(this.data.blocks).length || this.data.cover_media_id) wx.showModal({ title: '切换内容归属？', content: '将移除当前媒体引用，标题和文字保留。', success: result => { if (result.confirm) void apply(); } });
    else void apply();
  },
  editBlock(event) {
    if (this.data.layoutReadOnly) return;
    const index = Number(event.currentTarget.dataset.index);
    const plan = this._inlineAIActive && this._inlineAIActive.original && this._inlineAIActive.original.articlePlan;
    if (!this.prepareInlineEditorMutation({ preserveGenerated: !!(this.data.aiInlineStreaming && plan && plan.targetIndex === index) })) return;
    const blocks = this.data.blocks.slice();
    blocks[index] = { ...blocks[index], text: event.detail.value };
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null, notice: '有未保存修改' });
  },
  addParagraph() { if (this.data.layoutReadOnly) return; this.insertBlock('paragraph'); },
  showInsertMenu() {
    if (this.data.layoutReadOnly || this.data.busy) return;
    const specs = ['paragraph', 'heading', 'quote', 'list-unordered', 'list-ordered'];
    wx.showActionSheet({
      itemList: ['段落', '标题', '引用', '无序列表', '有序列表'],
      success: result => {
        if (!this.current(this._epoch)) return;
        this.insertBlock(specs[result.tapIndex] || 'paragraph');
      },
      fail: () => {},
    });
  },
  insertBlock(spec) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    let block;
    if (spec === 'heading') block = headingBlock(2);
    else if (spec === 'quote') block = quoteBlock();
    else if (spec === 'list-unordered') block = listBlock(false);
    else if (spec === 'list-ordered') block = listBlock(true);
    else block = textBlock('');
    const blocks = this.data.blocks.concat(block);
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  setHeadingLevel(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index);
    if (!blocks[index] || blocks[index].kind !== 'heading') return;
    blocks[index] = { ...blocks[index], level: Number(event.currentTarget.dataset.level) === 3 ? 3 : 2 };
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  convertBlock(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index);
    const target = String(event.currentTarget.dataset.to || ''), current = blocks[index];
    if (!current || ['paragraph', 'heading', 'quote'].indexOf(target) < 0 || target === current.kind) return;
    const converted = { kind: target, text: String(current.text || '') };
    if (target === 'heading') converted.level = current.kind === 'heading' ? current.level : 2;
    blocks[index] = converted;
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  editCaption(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index);
    if (!blocks[index] || (blocks[index].kind !== 'image' && blocks[index].kind !== 'video')) return;
    blocks[index] = { ...blocks[index], caption: event.detail.value };
    this.setData({ blocks, ...blockViews(blocks, this.data.media, blocks[index].media_id), preview: null, notice: '有未保存修改' });
  },
  editListItem(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index), itemIndex = Number(event.currentTarget.dataset.itemIndex);
    if (!blocks[index] || blocks[index].kind !== 'list' || !Array.isArray(blocks[index].items)) return;
    const items = blocks[index].items.slice();
    items[itemIndex] = event.detail.value;
    blocks[index] = { ...blocks[index], items };
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null, notice: '有未保存修改' });
  },
  addListItem(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index);
    if (!blocks[index] || blocks[index].kind !== 'list') return;
    const items = blocks[index].items || [];
    if (items.length >= 100) { wx.showToast({ title: '列表最多 100 项', icon: 'none' }); return; }
    blocks[index] = { ...blocks[index], items: items.concat('') };
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  removeListItem(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index), itemIndex = Number(event.currentTarget.dataset.itemIndex);
    if (!blocks[index] || blocks[index].kind !== 'list') return;
    const items = blocks[index].items || [];
    blocks[index] = { ...blocks[index], items: items.length > 1 ? items.filter((_, position) => position !== itemIndex) : [''] };
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  toggleListOrdered(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index);
    if (!blocks[index] || blocks[index].kind !== 'list') return;
    blocks[index] = { ...blocks[index], ordered: !blocks[index].ordered };
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  addMediaBlock(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const asset = this.data.media[Number(event.currentTarget.dataset.index)];
    if (!asset) return;
    if (this.data.presentation === 'gallery' && asset.kind !== 'image') { wx.showToast({ title: '图集仅添加图片', icon: 'none' }); return; }
    if (this.data.presentation === 'video' && asset.kind !== 'video') { wx.showToast({ title: '视频笔记请选择视频', icon: 'none' }); return; }
    const baseBlocks = this.data.presentation === 'video' ? this.data.blocks.filter(block => block.kind !== 'video') : this.data.blocks;
    const appended = baseBlocks.concat({ kind: asset.kind, media_id: asset.media_id, caption: '' });
    const blocks = this.data.presentation === 'article' ? appended : socialBlocks(appended);
    this.setData({ blocks, ...blockViews(blocks, this.data.media), preview: null });
  },
  setCover(event) { if (!this.prepareInlineEditorMutation()) return; const asset = this.data.media[Number(event.currentTarget.dataset.index)]; if (asset && asset.kind === 'image') this.setData({ cover_media_id: asset.media_id, preview: null }); },
  setBlockCover(event) { if (!this.prepareInlineEditorMutation()) return; const mediaID = String(event.currentTarget.dataset.mediaId || ''); if (mediaID) this.setData({ cover_media_id: mediaID, preview: null, notice: '已设为封面' }); },
  moveBlock(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const from = Number(event.currentTarget.dataset.index), step = Number(event.currentTarget.dataset.step), blocks = this.data.blocks.slice();
    if (!blocks[from]) return;
    let to = from + step;
    if (this.data.presentation !== 'article' && blocks[from] && ['image', 'video'].includes(blocks[from].kind)) {
      const positions = blocks.map((block, index) => ['image', 'video'].includes(block.kind) ? index : -1).filter(index => index >= 0);
      const mediaPosition = positions.indexOf(from);
      to = positions[mediaPosition + step];
      if (to === undefined) return;
    }
    if (to < 0 || to >= blocks.length) return;
    const item = blocks.splice(from, 1)[0]; blocks.splice(to, 0, item);
    this.setData({ blocks, ...blockViews(blocks, this.data.media, item.media_id), preview: null });
  },
  removeBlock(event) {
    if (this.data.layoutReadOnly) return;
    if (!this.prepareInlineEditorMutation()) return;
    const blocks = this.data.blocks.filter((_, index) => index !== Number(event.currentTarget.dataset.index));
    const next = blocks.length ? blocks : [textBlock('')];
    const coverStillUsed = next.some(block => block.media_id === this.data.cover_media_id);
    const nextCover = coverStillUsed ? this.data.cover_media_id : (next.find(block => block.kind === 'image') || {}).media_id || '';
    this.setData({ blocks: next, postText: postText(next), cover_media_id: nextCover, ...blockViews(next, this.data.media), activeBlockIndex: -1, preview: null });
  },
  chooseMedia() {
    if (this.data.busy) return;
    if (!this.prepareInlineEditorMutation()) return;
    if (!this.data.context || !this.data.context.media_upload_configured) { wx.showToast({ title: '媒体上传尚未配置', icon: 'none' }); return; }
    if (!this.data.owner_id) { this.setData({ error: '请先选择内容归属', 'fieldErrors.owner_id': '请选择内容归属', settingsExpanded: true }); if (wx.pageScrollTo) wx.pageScrollTo({ scrollTop: 0 }); return; }
    if (this.data.presentation === 'video' && !this.data.context.video_resumable_upload) { this.setData({ error: '当前版本暂不支持本地上传，请从素材库选择视频' }); return; }
    // epoch 必须在选图回调内取：相册/相机全屏页会触发页面 onHide/onShow，
    // 提前捕获的代次会被返回时的 onShow 作废，导致选中的文件被静默丢弃。
    const chooserToken = String(wx.getStorageSync('token') || ''), chooserID = String(this.data.id || ''), chooserOwner = String(this.data.owner_id || ''), chooserRights = String(this.data.rights || '');
    const chooserCurrent = () => chooserToken === String(wx.getStorageSync('token') || '') && chooserID === String(this.data.id || '') && chooserOwner === String(this.data.owner_id || '') && chooserRights === String(this.data.rights || '');
    const mediaType = this.data.presentation === 'gallery' ? ['image'] : this.data.presentation === 'video' ? ['video'] : ['image', 'video'];
    wx.chooseMedia({ count: this.data.presentation === 'video' ? 1 : 9, mediaType, sourceType: ['album', 'camera'], success: result => {
      if (!chooserCurrent()) return;
      const epoch = this._epoch;
      if (!this.current(epoch)) return;
      this.uploadMediaBatch(result.tempFiles || [], result.type);
    }, fail: result => {
      if (!chooserCurrent()) return;
      const epoch = this._epoch;
      if (!this.current(epoch)) return;
      if (/cancel/i.test(String(result && result.errMsg || ''))) this.setData({ notice: '已取消选择，原内容保持不变' });
      else this.setData({ error: '无法打开相册或读取视频，请检查权限后重试' });
    } });
  },
  beginDocumentOperation() {
    const operation = { operationID: `document-${this._mediaUploadSequence = (this._mediaUploadSequence || 0) + 1}`, token: this._token, id: this.data.id, ownerID: this.data.owner_id, rights: this.data.rights };
    this._mediaUploadActive = operation;
    this.setData({ busy: true, error: '' });
    return operation;
  },
  documentOperationCurrent(operation) {
    return this._mediaUploadActive === operation && !this._unloaded && operation.token === this._token && operation.token === String(wx.getStorageSync('token') || '') && operation.id === this.data.id && operation.ownerID === this.data.owner_id && operation.rights === this.data.rights;
  },
  finishDocumentOperation(operation) {
    if (this._mediaUploadActive !== operation) return;
    this._mediaUploadActive = null;
    if (!this._unloaded && operation.token === this._token && operation.token === String(wx.getStorageSync('token') || '')) this.setData({ busy: false });
  },
  chooseDocument() {
    if (!this.data.context || !this.data.context.media_upload_configured || this.data.busy) { wx.showToast({ title: '文档上传尚未配置', icon: 'none' }); return; }
    if (!this.data.owner_id || (this.data.rights !== 'own' && this.data.rights !== 'authorized')) { this.setData({ error: '请先选择内容归属并确认文档权属' }); return; }
    const selection = { token: this._token, id: this.data.id, ownerID: this.data.owner_id, rights: this.data.rights };
    const current = () => !this._unloaded && selection.token === this._token && selection.token === String(wx.getStorageSync('token') || '') && selection.id === this.data.id && selection.ownerID === this.data.owner_id && selection.rights === this.data.rights;
    wx.chooseMessageFile({ count: 1, type: 'file', extension: ['pdf', 'docx', 'pptx'], success: result => {
      if (!current()) return;
      const file = result.tempFiles && result.tempFiles[0];
      if (file) this.uploadDocument(file);
    }, fail: result => {
      if (current() && !/cancel/i.test(String(result && result.errMsg || ''))) this.setData({ error: '文档选择失败，请重试' });
    } });
  },
  async uploadDocument(file) {
    if (this.data.busy) return;
    const name = String(file && file.name || '');
    if (!/\.(pdf|docx|pptx)$/i.test(name) || Number(file && file.size || 0) > 20 * 1024 * 1024) { this.setData({ error: '首批仅支持20MB以内的 PDF、无宏 DOCX 和无宏 PPTX 原文件' }); return; }
    const operation = this.beginDocumentOperation();
    this.setData({ notice: '' });
    try {
      const asset = await api.uploadWorkbenchMedia(file.path, 'document', operation.ownerID, operation.rights, name);
      if (!this.documentOperationCurrent(operation)) return;
      if (!asset || asset.kind !== 'document' || (asset.mime !== 'application/pdf' && asset.mime !== 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' && asset.mime !== 'application/vnd.openxmlformats-officedocument.presentationml.presentation')) throw new Error('服务端未返回可用的 PDF/DOCX/PPTX 文档');
      const candidate = { ...asset, local_path: file.path };
      this.setData({ documentCandidates: mergeAssets(this.data.documentCandidates, [candidate]), notice: '原文件已上传，正在保存文档资料' });
      await this.archiveDocumentAsset(candidate, operation);
    } catch (error) { if (this.documentOperationCurrent(operation)) { this.setData({ error: error.message || '文档资料上传失败' }); wx.showToast({ title: error.message || '文档资料上传失败', icon: 'none' }); } }
    finally { this.finishDocumentOperation(operation); }
  },
  async archiveDocument(event) {
    const asset = this.data.documentCandidates[Number(event.currentTarget.dataset.index)];
    if (!asset || this.data.busy) return;
    await this.archiveDocumentAsset(asset);
  },
  async archiveDocumentAsset(asset, operation) {
    // 上传与归档共用一次操作身份，原生选择器返回后的 onShow 不应中断它。
    if (!operation || typeof operation !== 'object') {
      if (this.data.busy) return;
      operation = this.beginDocumentOperation();
    }
    if (!this.documentOperationCurrent(operation)) { this.finishDocumentOperation(operation); return; }
    try {
      const title = documentTitle(asset.title);
      const body = JSON.stringify({ schema: 1, media_id: asset.media_id });
      const saved = await api.saveContent({ title, body, body_format: 'document_v1', source_url: '', source_note: '', rights: operation.rights, owner_id: operation.ownerID, cover_media_id: '', media_ids: [asset.media_id], expected_revision: 0 });
      if (!this.documentOperationCurrent(operation)) return;
      // 文档单独建档，不能替换正在编辑的图文；同时保留请求期间新输入的内容。
      if (this.data.id || String(this.data.title || '').trim() || this.validBlocks().length || this.data.cover_media_id || String(this.data.source_note || '').trim() || String(this.data.source_url || '').trim()) {
        this.setData({ notice: '文档资料已单独保存，可在内容工作台查看；请继续编辑并保存当前图文' });
        return;
      }
      this._loaded = true;
      this.setData({ id: saved.content_id, title, revision: saved.revision, version: saved.version, status: saved.status, body_format: 'document_v1', documentReadOnly: true, documentAsset: asset, legacyReadOnly: false, layoutReadOnly: false, notice: '文档资料已保存，仅授权账号可取得原文件' });
    } catch (error) { if (this.documentOperationCurrent(operation)) this.setData({ error: error.message || '原文件已上传，保存资料失败，可直接重试建档' }); }
    finally { this.finishDocumentOperation(operation); }
  },
  async openDocument() {
    const asset = this.data.documentAsset;
    if (!asset || !asset.media_id || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const filePath = await api.downloadWorkbenchMedia(asset.media_id);
      if (!this.current(epoch)) return;
      wx.openDocument({ filePath, showMenu: true, success: () => this.setData({ busy: false }), fail: () => this.setData({ busy: false, error: '原文件无法打开，请稍后重试' }) });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '原文件下载失败' }); }
  },
  async uploadMediaBatch(files, pickedType) {
    if (!files || !files.length || this.data.busy) return;
    const includesVideo = String(pickedType || '') === 'video' || files.some(file => file.fileType === 'video') || this.data.presentation === 'video';
    if (includesVideo && !(this.data.context && this.data.context.video_resumable_upload)) { this.setData({ error: '当前服务尚未升级视频可续传，暂时无法上传本地视频' }); return; }
    const operationID = `media-${Date.now()}-${this._mediaUploadSequence = (this._mediaUploadSequence || 0) + 1}`;
    // 原生选择器的 success 与 onShow 顺序不固定，操作归属不能依赖页面代次。
    const uploadIdentity = { operationID, token: this._token, id: this.data.id, ownerID: this.data.owner_id, rights: this.data.rights };
    const ownsMediaUpload = () => !!this._mediaUploadActive && this._mediaUploadActive.operationID === uploadIdentity.operationID;
    const sameMediaDraft = () => ownsMediaUpload() && !this._unloaded && uploadIdentity.token === this._token && uploadIdentity.token === String(wx.getStorageSync('token') || '') && uploadIdentity.id === this.data.id && uploadIdentity.ownerID === this.data.owner_id && uploadIdentity.rights === this.data.rights;
    const releaseBusy = () => {
      if (!ownsMediaUpload()) return;
      this._mediaUploadActive = null;
      this.setData({ busy: false, mediaUploadMessage: '' });
    };
    this._mediaUploadActive = uploadIdentity;
    this.setData({ busy: true, error: '', notice: '' });
    const media = [];
    const blocks = [];
    const initialCover = this.data.cover_media_id;
    let coverMediaID = initialCover;
    let failures = 0;
    let lastError = '';
    let coverWarning = '';
    // 串行上传：并发会互相争抢 busy 状态且服务端按 owner 串行建档；中途会话失效立即停止，
    // 已成功的不回滚，失败的计数提示可重试。
    for (let position = 0; position < files.length; position += 1) {
      if (!sameMediaDraft()) { releaseBusy(); return; }
      const file = files[position];
      this.setData({ mediaUploadMessage: `正在上传 ${position + 1}/${files.length}` });
      try {
        const kind = String(pickedType || file.fileType || (this.data.presentation === 'video' ? 'video' : 'image'));
        const asset = kind === 'video' ? await resumableVideoUpload().uploadResumableVideo({
          api,
          wx,
          file,
          accountKey: sessionAccountKey(this._token),
          ownerID: this.data.owner_id,
          rights: this.data.rights,
          isCurrent: sameMediaDraft,
          onProgress: progress => { if (sameMediaDraft()) this.setData({ videoUploadProgress: progress, videoUploadMessage: `正在上传 ${progress}%` }); },
          onPhase: message => { if (sameMediaDraft()) this.setData({ videoUploadMessage: message }); },
        }) : await api.uploadWorkbenchMedia(file.tempFilePath, kind, this.data.owner_id, this.data.rights);
        if (!sameMediaDraft()) { releaseBusy(); return; }
        media.push({ ...asset, local_path: file.tempFilePath });
        const assetKind = asset.kind || kind;
        if (this.data.presentation === 'gallery' && assetKind !== 'image' || this.data.presentation === 'video' && assetKind !== 'video') { failures += 1; continue; }
        if (this.data.presentation === 'video') {
          for (let index = blocks.length - 1; index >= 0; index -= 1) if (blocks[index].kind === 'video') blocks.splice(index, 1);
        }
        blocks.push({ kind: assetKind, media_id: asset.media_id, caption: '' });
        if (!coverMediaID && assetKind === 'image') coverMediaID = asset.media_id;
        if (assetKind === 'video' && file.thumbTempFilePath) {
          try {
            const cover = await api.uploadWorkbenchMedia(file.thumbTempFilePath, 'image', this.data.owner_id, this.data.rights);
            if (sameMediaDraft() && cover && cover.media_id) { media.push({ ...cover, local_path: file.thumbTempFilePath }); coverMediaID = cover.media_id; }
          } catch (_) { coverWarning = '视频已上传，但自动封面生成失败，可手动选择封面'; }
        }
      } catch (error) {
        if (!sameMediaDraft()) { releaseBusy(); return; }
        failures += 1;
        lastError = error && error.message || '媒体上传失败';
      }
    }
    if (!sameMediaDraft()) { releaseBusy(); return; }
    // 上传期间仍可填写正文；仅合并新增媒体，避免用旧快照覆盖刚输入的内容。
    const baseBlocks = this.data.presentation === 'video' && blocks.some(block => block.kind === 'video') ? this.data.blocks.filter(block => block.kind !== 'video') : this.data.blocks;
    const mergedBlocks = baseBlocks.concat(blocks);
    const mergedMedia = mergeAssets(this.data.media, media);
    const arranged = this.data.presentation === 'article' ? mergedBlocks : socialBlocks(mergedBlocks);
    if (this.data.cover_media_id !== initialCover) coverMediaID = this.data.cover_media_id;
    this._mediaUploadActive = null;
    this.setData({ media: mergedMedia, blocks: arranged, postText: postText(arranged), cover_media_id: coverMediaID, ...blockViews(arranged, mergedMedia), busy: false, mediaUploadMessage: '', videoUploadProgress: failures ? this.data.videoUploadProgress : 100, videoUploadMessage: '', 'fieldErrors.video': failures ? this.data.fieldErrors.video : '', error: failures && files.length === 1 ? lastError : '', preview: null, notice: coverWarning || (failures ? `已上传 ${files.length - failures} 个，${failures} 个失败，可重试` : '已上传并加入内容，请保存预览') });
    // 上传按钮在页面底部，仅顶部的错误横幅容易被视口挡住；同步 toast 保证反馈可见。
    if (failures) wx.showToast({ title: `${failures} 个媒体上传失败`, icon: 'none' });
  },
  validBlocks() {
    return this.data.blocks.filter(block => block.kind === 'image' || block.kind === 'video' || block.kind === 'list' && (block.items || []).some(Boolean) || String(block.text || '').trim());
  },
  draftInput(allowIncompleteVideo = false) {
    if (this.data.documentReadOnly) { this.setData({ error: '文档资料保留原文件，小程序不提供在线编辑或发布' }); return null; }
    if (this.data.legacyReadOnly) { this.setData({ error: '旧格式内容仅支持查看，请前往后台编辑，避免自动转换破坏原文。' }); return null; }
    const title = String(this.data.title || '').trim(), blocks = this.validBlocks();
    if (!this.data.owner_id) { this.setData({ error: '请选择内容归属', 'fieldErrors.owner_id': '请选择内容归属', settingsExpanded: true }); if (wx.pageScrollTo) wx.pageScrollTo({ scrollTop: 0 }); return null; }
    const supported = Array.isArray(this.data.context && this.data.context.presentation_formats) && this.data.context.presentation_formats.includes(this.data.presentation);
    if (this.data.presentation !== 'video' && (!title || !blocks.length)) { this.setData({ error: '请填写标题并保留可发布内容', 'fieldErrors.title': title ? '' : '请填写标题' }); return null; }
    if (supported && this.data.presentation === 'gallery' && (!mediaBlocks(blocks, 'image').length || mediaBlocks(blocks, 'video').length || hasStructuredBlocks(blocks))) { this.setData({ error: '图集需要至少一张图片，文案请填在独立正文中' }); return null; }
    if (supported && this.data.presentation === 'video' && (mediaBlocks(blocks, 'image').length || hasStructuredBlocks(blocks) || mediaBlocks(blocks, 'video').length > 1)) { this.setData({ error: '视频内容只保留一个主视频，封面请单独选择' }); return null; }
    const missingVideo = this.data.presentation === 'video' && mediaBlocks(blocks, 'video').length !== 1;
    const incompleteVideo = this.data.presentation === 'video' && (!title || missingVideo);
    if (incompleteVideo && !allowIncompleteVideo) {
      const fieldErrors = { title: title ? '' : '请填写标题', video: missingVideo ? '请添加主视频' : '' };
      this.setData({ error: !title && missingVideo ? '请填写标题并添加主视频' : !title ? '请填写标题' : '请添加主视频', fieldErrors });
      if (wx.pageScrollTo) wx.pageScrollTo({ scrollTop: 0 });
      return null;
    }
    const hasNewVideoFields = this.data.videoLayout === 'douyin' || String(this.data.shareCopy || '').trim() || String(this.data.momentsCopy || '').trim();
    if (this.data.presentation === 'video' && hasNewVideoFields && !this.data.context.video_editor_fields) { this.setData({ error: '当前版本暂不能保存视频文案，内容已保留，请联系管理员更新' }); return null; }
    const videoFields = this.data.presentation === 'video' && this.data.context.video_editor_fields ? { videoLayout: this.data.videoLayout, shareCopy: String(this.data.shareCopy || '').trim(), momentsCopy: String(this.data.momentsCopy || '').trim() } : null;
    const body = preserveLayoutBody(this.data.layoutBody, articleBody(blocks, this.data.presentation, supported, videoFields, incompleteVideo));
    const tags = String(this.data.tagsText || '').split(/[\s,，]+/).map(t => t.trim()).filter(Boolean).slice(0, 10);
    return { blocks, draft: { title, body, body_format: 'article_v2', source_url: String(this.data.source_url || '').trim(), source_note: String(this.data.source_note || '').trim(), rights: this.data.rights, owner_id: this.data.owner_id, cover_media_id: this.data.cover_media_id, media_ids: this.data.layoutReadOnly ? this.data.layoutMediaIDs : mediaIDs(blocks), tags, pinned: this.data.pinned, expected_revision: this.data.revision } };
  },
  sameSaveAccount(token) {
    return !this._unloaded && token === this._token && token === String(wx.getStorageSync('token') || '');
  },
  async persistDraft(input) {
    const signature = this.draftSignature();
    const operation = { token: this._token, id: this.data.id, revision: this.data.revision };
    this._contentSaveActive = operation;
    try {
      const saved = await api.saveContent(input.draft, operation.id);
      // 页面前后台切换只改变读请求代次；同账号同草稿的保存必须收尾。
      if (this._contentSaveActive !== operation || !this.sameSaveAccount(operation.token)
        || operation.id !== this.data.id || operation.revision !== this.data.revision) return null;
      const draftChanged = signature !== this.draftSignature();
      const blocks = draftChanged ? this.data.blocks : input.blocks;
      this.setData({ id: saved.content_id, revision: saved.revision, version: saved.version, status: saved.status, blocks, ...blockViews(blocks, this.data.media) });
      this._savedSignature = JSON.stringify({ ...JSON.parse(signature), id: saved.content_id, revision: saved.revision, blocks: input.blocks });
      return { ...saved, draftChanged, saveEpoch: this._epoch };
    } finally {
      if (this._contentSaveActive === operation) {
        this._contentSaveActive = null;
        if (this.sameSaveAccount(operation.token)) this.setData({ busy: false });
      }
    }
  },
  async saveDraft() {
    if (this.data.busy || this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const input = this.draftInput(this.data.presentation === 'video');
    if (!input) return;
    const token = this._token; this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = await this.persistDraft(input);
      if (!saved || !this.sameSaveAccount(token)) return;
      this.setData({ preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, busy: false, notice: saved.draftChanged ? '当前仍有未保存修改，请再次保存' : '草稿已保存' });
    } catch (error) { if (this.sameSaveAccount(token)) this.setData({ busy: false, error: error.message || '保存草稿失败' }); }
  },
  publishKey(previewToken) {
    const signature = JSON.stringify([this.data.id, this.data.revision, previewToken || this.data.preview && this.data.preview.preview_token]);
    if (signature !== this._publishSignature) { this._publishSignature = signature; this._publishKey = api.mutationKey('publish'); }
    return this._publishKey;
  },
  revokeKey(reason) {
    const signature = JSON.stringify([this.data.id, this.data.revision, reason]);
    if (signature !== this._revokeSignature) { this._revokeSignature = signature; this._revokeKey = api.mutationKey('revoke'); }
    return this._revokeKey;
  },
  async previewView(preview) {
    const content = preview.content || {};
    const assets = mergeAssets(content.media || [], content.cover || []);
    const paths = await this.loadPrivatePaths(assets, new Set(assets.map(asset => asset.media_id)));
    const presentationMedia = paths.map(asset => ({ ...asset, url: asset.local_path || '' }));
    const cover = content.cover ? paths.find(asset => asset.media_id === content.cover.media_id) || null : null;
    const presentationContent = cover ? { ...content, cover: { ...(content.cover || {}), url: cover.local_path || '' } } : content;
    let layoutReadOnly = false;
    if (content.body_format === 'article_v2') { try { const document = JSON.parse(content.body); layoutReadOnly = typeof document.layout_html === 'string' && !!document.layout_html; } catch (_) {} }
    const layoutBlocks = layoutReadOnly ? renderArticleDocument(content.body, paths, this.editorWidth(), 'contentedit-preview') : [];
    const presentation = contentPresentation().buildContentPresentation(presentationContent, presentationMedia, wx.getWindowInfo ? wx.getWindowInfo() : { windowWidth: this.editorWidth() + 64 }, { prefix: 'contentedit-preview' });
    return {
      blocks: decorateBlocks(initialBlocks(content, contentPresentation().inferContentPresentation(content)), paths),
      cover,
      layoutReadOnly,
      layoutBlocks,
      presentation,
      errors: presentationErrors(presentation, presentationMedia),
    };
  },
  async retryPreviewMedia(event) {
    const mediaID = String(event.currentTarget.dataset.mediaId || '');
    if (!mediaID || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const localPath = await api.downloadWorkbenchMedia(mediaID);
      if (!this.current(epoch)) return;
      const previewBlocks = this.data.previewBlocks.map(block => block.media_id === mediaID ? { ...block, asset: { ...(block.asset || {}), local_path: localPath, preview_error: '' } } : block);
      const previewCover = this.data.previewCover && this.data.previewCover.media_id === mediaID ? { ...this.data.previewCover, local_path: localPath, preview_error: '' } : this.data.previewCover;
      const previewMedia = mergeAssets(previewBlocks.map(block => block.asset).filter(Boolean), previewCover || []).map(asset => ({ ...asset, url: asset.local_path || '' }));
      const previewContent = this.data.preview && this.data.preview.content || {};
      const presentationContent = previewCover ? { ...previewContent, cover: { ...(previewContent.cover || {}), url: previewCover.local_path || '' } } : previewContent;
      const presentationView = this.data.preview ? contentPresentation().buildContentPresentation(presentationContent, previewMedia, wx.getWindowInfo ? wx.getWindowInfo() : { windowWidth: this.editorWidth() + 64 }, { prefix: 'contentedit-preview', galleryIndex: this.data.presentationView && this.data.presentationView.galleryIndex || 0, descriptionExpanded: !!(this.data.presentationView && this.data.presentationView.descriptionExpanded) }) : this.data.presentationView;
      this.setData({ previewBlocks, previewCover, presentationView, presentationMediaErrors: presentationErrors(presentationView, previewMedia), busy: false });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '预览媒体重试失败' }); }
  },
  async saveAndPreview() {
    if (this.data.busy || this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const input = this.draftInput();
    if (!input) return;
    let epoch = this._epoch; const token = this._token; this.setData({ busy: true, error: '', notice: '', preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, videoStarted: {} });
    try {
      const saved = await this.persistDraft(input, epoch);
      if (!saved || !this.sameSaveAccount(token)) return;
      epoch = saved.saveEpoch;
      this.setData({ busy: true });
      if (saved.draftChanged) { this.setData({ busy: false, notice: '当前仍有未保存修改，请再次保存并预览' }); return; }
      const signature = this.draftSignature();
      const preview = await api.previewContent(saved.content_id, saved.revision);
      if (!this.current(epoch)) return;
      const view = await this.previewView(preview);
      if (!this.current(epoch)) return;
      if (signature !== this.draftSignature()) { this.setData({ busy: false, notice: '当前仍有未保存修改，请再次保存并预览' }); return; }
      this.setData({ preview, previewBlocks: view.blocks, previewCover: view.cover, previewLayoutReadOnly: view.layoutReadOnly, previewLayoutBlocks: view.layoutBlocks, presentationView: view.presentation, presentationMediaErrors: view.errors, busy: false, notice: '' });
    } catch (error) { if (this.sameSaveAccount(token)) this.setData({ busy: false, error: error.message || '保存预览失败' }); }
  },
  async publish() {
    if (this.data.busy || this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const checkpoint = this._directPublishCheckpoint;
    const reusableCheckpoint = checkpoint && checkpoint.token === this._token && checkpoint.id === this.data.id && checkpoint.revision === this.data.revision && checkpoint.signature === this.draftSignature();
    const input = this.data.preview || reusableCheckpoint ? null : this.draftInput();
    if (!this.data.preview && !reusableCheckpoint && !input) return;
    const operation = { token: this._token, id: this.data.id, revision: this.data.revision, signature: this.draftSignature() };
    this._contentPublishActive = operation;
    const sameTarget = () => this._contentPublishActive === operation && !this._unloaded && operation.token === this._token && operation.token === String(wx.getStorageSync('token') || '') && operation.id === this.data.id && operation.revision === this.data.revision;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      let preview = this.data.preview || (reusableCheckpoint ? { preview_token: checkpoint.previewToken } : null);
      if (!preview && input) {
        const saved = await this.persistDraft(input);
        if (!saved || !this.sameSaveAccount(operation.token)) return;
        operation.id = saved.content_id;
        operation.revision = saved.revision;
        operation.signature = this.draftSignature();
        this.setData({ busy: true });
        if (saved.draftChanged) { this._contentPublishActive = null; this.setData({ busy: false, notice: '当前仍有未保存修改，请再次发布' }); return; }
        const signature = this.draftSignature();
        preview = await api.previewContent(saved.content_id, saved.revision);
        if (!sameTarget()) return;
        if (signature !== this.draftSignature()) { this._contentPublishActive = null; this.setData({ busy: false, notice: '当前仍有未保存修改，请再次发布' }); return; }
      }
      const previewToken = String(preview && preview.preview_token || '');
      if (!previewToken) throw new Error('发布校验未完成，请重试');
      if (!this.data.preview) this._directPublishCheckpoint = { token: operation.token, id: operation.id, revision: operation.revision, signature: operation.signature, previewToken };
      const result = await api.publishContent(this.data.id, this.data.revision, previewToken, this.publishKey(previewToken));
      if (!sameTarget()) return;
      this._contentPublishActive = null;
      this._directPublishCheckpoint = null;
      this._publishSignature = ''; this._publishKey = '';
      const publishedRevision = result.content.revision;
      const publishedSignature = JSON.stringify({ ...JSON.parse(operation.signature), id: result.content.content_id || operation.id, revision: publishedRevision });
      this.setData({ revision: publishedRevision, version: result.content.version, status: result.content.status, preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, busy: false });
      this._savedSignature = publishedSignature;
      const hasNewEdits = publishedSignature !== this.draftSignature();
      this.setData({ notice: hasNewEdits ? '请求时的版本已发布，当前仍有未保存修改' : '已发布，正在打开作品页分享。' });
      if (!hasNewEdits) setTimeout(() => { if (this.sameSaveAccount(operation.token) && this.data.revision === publishedRevision) this.openPublished(); }, 600);
    } catch (error) {
      if (!sameTarget()) return;
      this._contentPublishActive = null;
      const invalidCheckpoint = error && (error.statusCode === 409 || error.statusCode === 401 || error.statusCode === 403);
      if (invalidCheckpoint) { this._directPublishCheckpoint = null; this._publishSignature = ''; this._publishKey = ''; }
      this.setData({
        ...(invalidCheckpoint ? { preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false } : {}),
        busy: false,
        error: error && error.statusCode === 409 ? '内容已变化或发布校验已过期，请重新发布' : error.message || '发布失败，可重试',
      });
    }
  },
  openImage(event) {
    const mediaID = String(event.currentTarget.dataset.mediaId || '');
    const view = this.data.presentationView || {};
    const visibleMedia = (view.media || []).concat((view.blocks || []).filter(block => block.kind === 'image'));
    const currentItem = visibleMedia.find(item => item.media_id === mediaID);
    const current = String(event.currentTarget.dataset.url || currentItem && currentItem.url || '');
    const urls = visibleMedia.map(item => item.url).filter(Boolean);
    if (current && urls.length) wx.previewImage({ current, urls });
  },
  presentationAuthorAvatarError(event) {
    const failedAvatar = String(event && event.currentTarget && event.currentTarget.dataset.avatar || '');
    const author = this.data.presentationView && this.data.presentationView.author;
    if (!failedAvatar || !author || author.avatar !== failedAvatar) return;
    this.setData({ 'presentationView.author.avatar': '' });
  },
  mediaError(event) {
    const key = String(event.currentTarget.dataset.errorKey || event.currentTarget.dataset.mediaId || '');
    if (key) this.setData({ presentationMediaErrors: { ...this.data.presentationMediaErrors, [key]: '媒体加载失败' } });
  },
  retryMedia(event) {
    const mediaID = String(event.currentTarget.dataset.mediaId || event.currentTarget.dataset.errorKey || '');
    if (mediaID) return this.retryPreviewMedia({ currentTarget: { dataset: { mediaId: mediaID } } });
  },
  rebuildPreviewPresentation(sourceMedia, galleryIndex) {
    if (!this.data.preview || !this.data.presentationView) return;
    const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : { windowWidth: this.editorWidth() + 64 };
    const previewContent = this.data.preview.content || {};
    const presentationContent = this.data.presentationView.poster ? { ...previewContent, cover: { ...(previewContent.cover || {}), url: this.data.presentationView.poster } } : previewContent;
    const presentationView = contentPresentation().buildContentPresentation(presentationContent, sourceMedia, windowInfo, { prefix: 'contentedit-preview', galleryIndex, descriptionExpanded: !!this.data.presentationView.descriptionExpanded });
    this.setData({ presentationView, presentationMediaErrors: presentationErrors(presentationView, sourceMedia) });
  },
  onContentGalleryChange(event) {
    if (!this.data.presentationView) return;
    const galleryIndex = Number(event.detail.current) || 0;
    if (galleryIndex === this.data.presentationView.galleryIndex) return;
    this.rebuildPreviewPresentation(presentationSourceMedia(this.data.presentationView), galleryIndex);
  },
  onContentVideoMetadata(event) {
    const detail = event && event.detail || {};
    const width = Number(detail.width || detail.videoWidth || 0), height = Number(detail.height || detail.videoHeight || 0);
    const mediaID = String(event && event.currentTarget && event.currentTarget.dataset.mediaId || '');
    if (!mediaID || width <= 0 || height <= 0) return;
    if (event.currentTarget.dataset.editor) {
      let changed = false;
      const media = (this.data.media || []).map(asset => {
        if (asset.media_id !== mediaID || Number(asset.width) === width && Number(asset.height) === height) return asset;
        changed = true;
        return { ...asset, width, height };
      });
      if (!changed) return;
      this.setData({ media, ...blockViews(this.data.blocks, media) });
      return;
    }
    if (!this.data.preview || !this.data.presentationView) return;
    const sourceMedia = presentationSourceMedia(this.data.presentationView);
    const currentAsset = sourceMedia.find(asset => asset.media_id === mediaID);
    if (!currentAsset || Number(currentAsset.width) === width && Number(currentAsset.height) === height) return;
    const updatedMedia = sourceMedia.map(asset => asset.media_id === mediaID ? { ...asset, width, height } : asset);
    this.rebuildPreviewPresentation(updatedMedia, this.data.presentationView.galleryIndex || 0);
  },
  playContentVideo(event) { const data = event && event.currentTarget && event.currentTarget.dataset || {}, id = String(data.domId || ''), key = String(data.playerKey || ''); if (!id || !key) return; this.setData({ videoStarted: { ...this.data.videoStarted, [key]: true } }, () => wx.createVideoContext(id, this).play()); },
  videoState(event) { const key = String(event && event.currentTarget && (event.currentTarget.dataset.playerKey || event.currentTarget.dataset.mediaId) || ''); if (event && event.type === 'play' && key && !this.data.videoStarted[key]) this.setData({ videoStarted: { ...this.data.videoStarted, [key]: true } }); if (event && event.type === 'error') { const videoStarted = { ...this.data.videoStarted }; delete videoStarted[key]; this.setData({ videoStarted }); this.mediaError(event); } },
  videoProgress() {},
  videoRate() {},
  videoFullscreen() {},
  openPublished() {
    if (!this.data.id || this.data.status !== 'published' || this.data.version < 1) return;
    if (this._savedSignature && this._savedSignature !== this.draftSignature()) { wx.showToast({ title: '当前修改尚未发布，请先保存并发布', icon: 'none' }); return; }
    wx.navigateTo({ url: '/pages/contentreader/contentreader?content_id=' + encodeURIComponent(this.data.id) + '&content_version=' + encodeURIComponent(String(this.data.version)) });
  },
  revoke() {
    if (!this.data.id || this.data.version < 1 || this.data.busy || this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const epoch = this._epoch;
    wx.showModal({ title: '撤回内容', content: '撤回后旧分享入口将不能继续阅读，请填写原因。', editable: true, placeholderText: '撤回原因', success: result => { if (this.current(epoch) && result.confirm && String(result.content || '').trim()) this.revokeContent(String(result.content).trim()); } });
  },
  async revokeContent(reason) {
    if (this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try { const content = await api.revokeContent(this.data.id, this.data.revision, reason, this.revokeKey(reason)); if (this.current(epoch)) { this._revokeSignature = ''; this._revokeKey = ''; this.setData({ revision: content.revision, status: content.status, preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, busy: false, notice: '已撤回' }); } }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '撤回失败' }); }
  },
  deleteDraft() {
    if (!this.data.id || this.data.status !== 'draft' || this.data.version !== 0 || this.data.busy || this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const epoch = this._epoch;
    wx.showModal({ title: '删除草稿', content: '只删除这份从未发布的草稿，不会撤回已发布内容，也不会删除原媒体文件。', confirmText: '删除草稿', confirmColor: '#a12622', success: result => {
      if (this.current(epoch) && result.confirm) this.deleteDraftConfirmed();
    } });
  },
  async deleteDraftConfirmed() {
    if (this.data.aiInlineStreaming || this.blockInlineAIConflict()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const deleted = await api.deleteContentDraft(this.data.id, this.data.revision);
      if (!this.current(epoch)) return;
      this.setData({ deleted: true, status: deleted.status, revision: deleted.revision, busy: false, notice: '草稿已删除，原媒体资料仍保留' });
      this.backToWorkbench();
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '删除草稿失败' }); }
  },
  backToWorkbench() {
    const epoch = this._epoch;
    wx.redirectTo({ url: '/pages/contentworkbench/contentworkbench', fail: () => { if (this.current(epoch)) this.setData({ error: '草稿已删除，请手动返回内容工作台' }); } });
  },
});
