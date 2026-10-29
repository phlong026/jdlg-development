// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
const { preserveLayoutBody, renderArticleDocument } = require('../../utils/article-document');

function textBlock(text = '') { return { kind: 'paragraph', text }; }
function mediaIDs(blocks) { return [...new Set(blocks.filter(block => block.media_id).map(block => block.media_id))]; }
function writingFacts(blocks) {
  return blocks.map(block => {
    if (block.kind === 'list') return (block.items || []).filter(Boolean).join('\n');
    if (block.kind === 'image' || block.kind === 'video') return String(block.caption || '').trim();
    return String(block.text || '').trim();
  }).filter(Boolean).join('\n\n').slice(0, 6000);
}
function articleBody(blocks) {
  return JSON.stringify({ schema: 1, blocks: blocks.map(block => {
    if (block.kind === 'image' || block.kind === 'video') return { kind: block.kind, media_id: block.media_id, caption: block.caption || '' };
    if (block.kind === 'heading') return { kind: 'heading', level: block.level === 3 ? 3 : 2, text: String(block.text || '').trim() };
    if (block.kind === 'quote') return { kind: 'quote', text: String(block.text || '').trim() };
    if (block.kind === 'list') return { kind: 'list', ordered: !!block.ordered, items: (block.items || []).filter(Boolean) };
    return { kind: 'paragraph', text: String(block.text || '').trim() };
  }) });
}
function initialBlocks(content) {
  if (!content) return [textBlock('')];
  if (content.body_format !== 'article_v2') return [textBlock(content.body || '')];
  try { const doc = JSON.parse(content.body); return Array.isArray(doc.blocks) && doc.blocks.length ? doc.blocks : [textBlock('')]; } catch (_) { return [textBlock(content.body || '')]; }
}
function decorateBlocks(blocks, media) {
  const byID = new Map((media || []).map(asset => [asset.media_id, asset]));
  return blocks.map((block, index) => ({ ...block, index, list_text: (block.items || []).join('、'), asset: block.media_id ? byID.get(block.media_id) || null : null }));
}
function mergeAssets(...groups) {
  const assets = new Map();
  groups.forEach(group => (Array.isArray(group) ? group : [group]).filter(Boolean).forEach(asset => assets.set(asset.media_id, { ...(assets.get(asset.media_id) || {}), ...asset })));
  return [...assets.values()];
}
function documentTitle(name) { return String(name || '').replace(/\.(pdf|docx)$/i, '').trim().slice(0, 80) || '文档资料'; }

Page({
  data: {
    id: '', loading: true, busy: false, error: '', denied: false, disabled: false, loginRequired: false, context: null,
    title: '', owner_id: '', rights: 'own', source_note: '', source_url: '', cover_media_id: '', revision: 0, version: 0, status: 'draft', body_format: 'article_v2',
    blocks: [textBlock('')], renderedBlocks: [], media: [], preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false,
    legacyReadOnly: false, legacyBody: '', legacyMedia: [], notice: '',
    layoutReadOnly: false, layoutBody: '', layoutMediaIDs: [], layoutBlocks: [],
    documentReadOnly: false, documentAsset: null, documentCandidates: [], deleted: false, serverReadOnly: false,
  },
  onLoad(options) {
    this.setData({ id: String(options.id || '') });
    const channel = this.getOpenerEventChannel && this.getOpenerEventChannel();
    const sourceToken = String(wx.getStorageSync('token') || '');
    if (channel && channel.on) channel.on('prefillContent', payload => this.receivePrefill(payload, sourceToken));
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || ''), changed = token !== this._token;
    if (changed) {
      this.setData({ context: null, title: '', owner_id: '', rights: 'own', source_note: '', source_url: '', cover_media_id: '', revision: 0, version: 0, status: 'draft', body_format: 'article_v2', blocks: [textBlock('')], renderedBlocks: [], media: [], preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, legacyReadOnly: false, legacyBody: '', legacyMedia: [], layoutReadOnly: false, layoutBody: '', layoutMediaIDs: [], layoutBlocks: [], documentReadOnly: false, documentAsset: null, documentCandidates: [], deleted: false, serverReadOnly: false, notice: '', error: '', denied: false, disabled: false, busy: false });
      this._publishSignature = ''; this._publishKey = ''; this._revokeSignature = ''; this._revokeKey = '';
      this._loaded = false;
    }
    this._token = token; this._epoch = (this._epoch || 0) + 1;
    this.setData({ loginRequired: !token, loading: !!token && !this._loaded, busy: false });
    if (token && !this._loaded) this.load(this._epoch);
  },
  onHide() { this.pauseLayoutVideos(); },
  onUnload() { this._epoch = (this._epoch || 0) + 1; this.pauseLayoutVideos(); },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  retry() { this._loaded = false; const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this.load(epoch); },
  async load(suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token) return;
    const epoch = suppliedEpoch || (this._epoch = (this._epoch || 0) + 1);
    this.setData({ loading: true, error: '', denied: false, disabled: false, loginRequired: false });
    try {
      const context = await api.workbenchContext();
      if (!this.current(epoch)) return;
      if (!context.enabled || context.can_manage === false) { this.setData({ context, denied: context.can_manage === false, disabled: !context.enabled, loading: false }); return; }
      const values = await Promise.all([api.listWorkbenchMedia(), this.data.id ? api.getContent(this.data.id) : Promise.resolve(null)]);
      if (!this.current(epoch)) return;
      const mediaPage = values[0] || {}, content = values[1];
      const bodyFormat = content ? content.body_format || 'plain' : 'article_v2';
      const documentReadOnly = !!content && bodyFormat === 'document_v1' && content.type === 'document';
      const legacyReadOnly = !!content && bodyFormat !== 'article_v2' && !documentReadOnly;
      let layoutReadOnly = false;
      if (content && bodyFormat === 'article_v2') { try { const document = JSON.parse(content.body); layoutReadOnly = typeof document.layout_html === 'string' && !!document.layout_html; } catch (_) {} }
      const blocks = initialBlocks(content);
      const associated = mergeAssets(content && content.media || [], content && content.cover || []);
      const assets = mergeAssets(mediaPage.items || [], associated);
      const previews = new Set(legacyReadOnly || layoutReadOnly ? associated.map(asset => asset.media_id) : mediaIDs(blocks));
      if (content && content.cover) previews.add(content.cover.media_id);
      const loadedMedia = await this.loadPrivatePaths(assets, previews);
      if (!this.current(epoch)) return;
      this._loaded = true;
      const legacyIDs = new Set(associated.map(asset => asset.media_id));
      const media = loadedMedia.filter(asset => asset.kind === 'image' || asset.kind === 'video');
      const documentCandidates = loadedMedia.filter(asset => asset.kind === 'document');
      const documentAsset = documentReadOnly ? loadedMedia.find(asset => associated.some(item => item.media_id === asset.media_id) && asset.kind === 'document') || null : null;
      const layoutBlocks = layoutReadOnly ? renderArticleDocument(content.body, media, this.editorWidth(), 'contentedit-layout') : [];
      this.setData({ context, media, documentCandidates, title: content ? content.title : '', owner_id: content ? content.owner_id : (context.owners || [])[0] || '', rights: content ? content.rights : 'own', source_note: content ? content.source_note || '' : '', source_url: content ? content.source_url || '' : '', cover_media_id: content && content.cover ? content.cover.media_id : '', revision: content ? content.revision : 0, version: content ? content.version : 0, status: content ? content.status : 'draft', body_format: bodyFormat, legacyReadOnly, legacyBody: legacyReadOnly ? content.body || '' : '', legacyMedia: legacyReadOnly ? loadedMedia.filter(asset => legacyIDs.has(asset.media_id)) : [], layoutReadOnly, layoutBody: layoutReadOnly ? content.body : '', layoutMediaIDs: layoutReadOnly ? (content.media || []).map(asset => asset.media_id) : [], layoutBlocks, documentReadOnly, documentAsset, serverReadOnly: !!(content && content.read_only), blocks, renderedBlocks: decorateBlocks(blocks, media), loading: false });
      this.applyPendingPrefill();
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '编辑器加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false }); }
  },
  editorWidth() { const window = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync ? wx.getSystemInfoSync() : { windowWidth: 375 }; return Math.max(240, Number(window.windowWidth || 375) - 64); },
  pauseLayoutVideos() { [...(this.data.layoutBlocks || []), ...(this.data.previewLayoutBlocks || [])].filter(block => block.kind === 'video').forEach(block => wx.createVideoContext(block.domId, this).pause()); },
  draftSignature() {
    return JSON.stringify({ id: this.data.id, title: this.data.title, owner_id: this.data.owner_id, rights: this.data.rights, source_note: this.data.source_note, source_url: this.data.source_url, cover_media_id: this.data.cover_media_id, revision: this.data.revision, blocks: this.data.blocks });
  },
  rejectWritingReturn(message) {
    this.setData({ notice: message });
    wx.showToast({ title: '文案未采用', icon: 'none' });
  },
  receiveWritingReturn(payload, launch) {
    if (launch.handled) { this.setData({ notice: 'AI 文案已处理，未重复追加' }); return; }
    launch.handled = true;
    if (launch.token !== String(wx.getStorageSync('token') || '')) { this.rejectWritingReturn('账号已变化，AI 文案未采用；原内容保持不变'); return; }
    if (this.data.busy) { this.rejectWritingReturn('草稿正在保存或处理，AI 文案未采用；原内容保持不变'); return; }
    if (launch.contentID !== String(this.data.id || '') || launch.revision !== Number(this.data.revision || 0)) { this.rejectWritingReturn('草稿版本已变化，AI 文案未采用；请重新发起'); return; }
    if (launch.signature !== this.draftSignature()) { this.rejectWritingReturn('编辑内容已发生变化，AI 文案未采用；原内容保持不变'); return; }
    const title = String(payload && payload.title || '').trim();
    const text = String(payload && payload.text || '').trim();
    if (!title && !text) { this.rejectWritingReturn('返回的 AI 文案为空，未采用'); return; }
    const blocks = this.validBlocks();
    if (!String(this.data.title || '').trim() && !blocks.length) { this.receivePrefill({ title, text }, launch.token); return; }
    if (!text) { this.rejectWritingReturn('AI 文案没有正文，未采用；原内容保持不变'); return; }
    if (blocks.some(block => String(block.text || '').trim() === text)) { this.setData({ notice: 'AI 文案已处理，正文中已存在，未重复追加' }); return; }
    const next = blocks.concat(textBlock(text));
    this.setData({ blocks: next, renderedBlocks: decorateBlocks(next, this.data.media), preview: null, notice: 'AI 文案已追加到正文末尾，请核对后保存' });
  },
  receivePrefill(payload, sourceToken) {
    if (this.data.id || this.data.busy) return;
    const title = String(payload && payload.title || '').trim();
    const text = String(payload && payload.text || '').trim();
    if (!title && !text) return;
    this._pendingPrefill = { title, text, token: String(sourceToken === undefined ? wx.getStorageSync('token') || '' : sourceToken) };
    this.applyPendingPrefill();
  },
  applyPendingPrefill() {
    const pending = this._pendingPrefill;
    if (!pending || !this._loaded || !this.data.context) return;
    delete this._pendingPrefill;
    if (this.data.id || pending.token !== String(wx.getStorageSync('token') || '') || String(this.data.title || '').trim() || this.validBlocks().length) return;
    const blocks = pending.text ? [textBlock(pending.text)] : this.data.blocks;
    this.setData({ title: pending.title, blocks, renderedBlocks: decorateBlocks(blocks, this.data.media), preview: null, notice: 'AI 文案已带入，请核对后保存' });
  },
  openAIWriting() {
    if (this.data.busy || this.data.legacyReadOnly || this.data.layoutReadOnly) return;
    const launch = { token: String(wx.getStorageSync('token') || ''), contentID: String(this.data.id || ''), revision: Number(this.data.revision || 0), signature: this.draftSignature(), handled: false };
    const context = { source: 'editor', content_id: launch.contentID, revision: launch.revision, title: String(this.data.title || '').trim(), facts: writingFacts(this.data.blocks || []) };
    wx.navigateTo({
      url: '/pages/contentwriting/contentwriting?source=editor',
      events: { prefillContent: payload => this.receiveWritingReturn(payload, launch) },
      success: result => result.eventChannel.emit('writingContext', context),
      fail: () => this.setData({ notice: 'AI 文案页无法打开，草稿已保留，请重试' }),
    });
  },
  async loadPrivatePaths(items, previews) {
    return Promise.all(items.map(async asset => {
      if (!previews.has(asset.media_id)) return { ...asset, local_path: '' };
      try { return { ...asset, local_path: await api.downloadWorkbenchMedia(asset.media_id) }; }
      catch (_) { return { ...asset, local_path: '', preview_error: '预览暂不可用' }; }
    }));
  },
  async previewMedia(event) {
    const index = Number(event.currentTarget.dataset.index), asset = this.data.media[index];
    if (!asset || asset.local_path || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const media = this.data.media.slice();
      media[index] = { ...asset, local_path: await api.downloadWorkbenchMedia(asset.media_id), preview_error: '' };
      if (!this.current(epoch)) return;
      this.setData({ media, renderedBlocks: decorateBlocks(this.data.blocks, media), busy: false });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '私有媒体预览加载失败' }); }
  },
  setField(event) { this.setData({ [event.currentTarget.dataset.field]: event.detail.value, preview: null, notice: '有未保存修改' }); },
  setRights(event) { this.setData({ rights: event.detail.value, preview: null }); },
  setOwner(event) { const owner = (this.data.context.owners || [])[Number(event.detail.value)] || ''; this.setData({ owner_id: owner, preview: null }); },
  editBlock(event) {
    if (this.data.layoutReadOnly) return;
    const blocks = this.data.blocks.slice(), index = Number(event.currentTarget.dataset.index);
    blocks[index] = { ...blocks[index], text: event.detail.value };
    this.setData({ blocks, renderedBlocks: decorateBlocks(blocks, this.data.media), preview: null, notice: '有未保存修改' });
  },
  addParagraph() { if (this.data.layoutReadOnly) return; const blocks = this.data.blocks.concat(textBlock('')); this.setData({ blocks, renderedBlocks: decorateBlocks(blocks, this.data.media), preview: null }); },
  addMediaBlock(event) {
    if (this.data.layoutReadOnly) return;
    const asset = this.data.media[Number(event.currentTarget.dataset.index)];
    if (!asset) return;
    const blocks = this.data.blocks.concat({ kind: asset.kind, media_id: asset.media_id, caption: '' });
    this.setData({ blocks, renderedBlocks: decorateBlocks(blocks, this.data.media), preview: null });
  },
  setCover(event) { const asset = this.data.media[Number(event.currentTarget.dataset.index)]; if (asset && asset.kind === 'image') this.setData({ cover_media_id: asset.media_id, preview: null }); },
  moveBlock(event) {
    if (this.data.layoutReadOnly) return;
    const from = Number(event.currentTarget.dataset.index), to = from + Number(event.currentTarget.dataset.step), blocks = this.data.blocks.slice();
    if (to < 0 || to >= blocks.length) return;
    const item = blocks.splice(from, 1)[0]; blocks.splice(to, 0, item);
    this.setData({ blocks, renderedBlocks: decorateBlocks(blocks, this.data.media), preview: null });
  },
  removeBlock(event) {
    if (this.data.layoutReadOnly) return;
    const blocks = this.data.blocks.filter((_, index) => index !== Number(event.currentTarget.dataset.index));
    const next = blocks.length ? blocks : [textBlock('')];
    this.setData({ blocks: next, renderedBlocks: decorateBlocks(next, this.data.media), preview: null });
  },
  chooseMedia() {
    if (!this.data.context.media_upload_configured || this.data.busy) { wx.showToast({ title: '媒体上传尚未配置', icon: 'none' }); return; }
    const epoch = this._epoch;
    wx.chooseMedia({ count: 1, mediaType: ['image', 'video'], sourceType: ['album', 'camera'], success: result => {
      if (!this.current(epoch)) return;
      const file = result.tempFiles && result.tempFiles[0];
      if (file) this.uploadMedia(file.tempFilePath, file.fileType || 'image');
    } });
  },
  chooseDocument() {
    if (!this.data.context || !this.data.context.media_upload_configured || this.data.busy) { wx.showToast({ title: '文档上传尚未配置', icon: 'none' }); return; }
    if (!this.data.owner_id || (this.data.rights !== 'own' && this.data.rights !== 'authorized')) { this.setData({ error: '请先选择内容归属并确认文档权属' }); return; }
    const epoch = this._epoch;
    wx.chooseMessageFile({ count: 1, type: 'file', extension: ['pdf', 'docx'], success: result => {
      if (!this.current(epoch)) return;
      const file = result.tempFiles && result.tempFiles[0];
      if (file) this.uploadDocument(file);
    }, fail: result => {
      if (this.current(epoch) && !/cancel/i.test(String(result && result.errMsg || ''))) this.setData({ error: '文档选择失败，请重试' });
    } });
  },
  async uploadDocument(file) {
    const name = String(file && file.name || '');
    if (!/\.(pdf|docx)$/i.test(name) || Number(file && file.size || 0) > 20 * 1024 * 1024) { this.setData({ error: '首批仅支持20MB以内的 PDF 和无宏 DOCX 原文件' }); return; }
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const asset = await api.uploadWorkbenchMedia(file.path, 'document', this.data.owner_id, this.data.rights, name);
      if (!this.current(epoch)) return;
      if (!asset || asset.kind !== 'document' || (asset.mime !== 'application/pdf' && asset.mime !== 'application/vnd.openxmlformats-officedocument.wordprocessingml.document')) throw new Error('服务端未返回可用的 PDF/DOCX 文档');
      const candidate = { ...asset, local_path: file.path };
      this.setData({ documentCandidates: mergeAssets(this.data.documentCandidates, [candidate]), busy: false, notice: '原文件已上传，正在保存文档资料' });
      await this.archiveDocumentAsset(candidate, epoch);
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '文档资料上传失败' }); }
  },
  async archiveDocument(event) {
    const asset = this.data.documentCandidates[Number(event.currentTarget.dataset.index)];
    if (!asset || this.data.busy) return;
    await this.archiveDocumentAsset(asset, this._epoch);
  },
  async archiveDocumentAsset(asset, epoch) {
    this.setData({ busy: true, error: '' });
    try {
      const title = String(this.data.title || '').trim() || documentTitle(asset.title);
      const body = JSON.stringify({ schema: 1, media_id: asset.media_id });
      const saved = await api.saveContent({ title, body, body_format: 'document_v1', source_url: '', source_note: '', rights: this.data.rights, owner_id: this.data.owner_id, cover_media_id: '', media_ids: [asset.media_id], expected_revision: 0 });
      if (!this.current(epoch)) return;
      this._loaded = true;
      this.setData({ id: saved.content_id, title, revision: saved.revision, version: saved.version, status: saved.status, body_format: 'document_v1', documentReadOnly: true, documentAsset: asset, legacyReadOnly: false, layoutReadOnly: false, busy: false, notice: '文档资料已保存，仅授权账号可取得原文件' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '原文件已上传，保存资料失败，可直接重试建档' }); }
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
  async uploadMedia(filePath, kind) {
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const asset = await api.uploadWorkbenchMedia(filePath, kind, this.data.owner_id, this.data.rights);
      if (!this.current(epoch)) return;
      const media = this.data.media.concat({ ...asset, local_path: filePath });
      const blocks = this.data.blocks.concat({ kind: asset.kind || kind, media_id: asset.media_id, caption: '' });
      this.setData({ media, blocks, renderedBlocks: decorateBlocks(blocks, media), busy: false, preview: null, notice: '已上传并加入正文，请保存预览' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '媒体上传失败' }); }
  },
  validBlocks() {
    return this.data.blocks.filter(block => block.kind === 'image' || block.kind === 'video' || block.kind === 'list' && (block.items || []).some(Boolean) || String(block.text || '').trim());
  },
  draftInput() {
    if (this.data.documentReadOnly) { this.setData({ error: '文档资料保留原文件，小程序不提供在线编辑或发布' }); return null; }
    if (this.data.legacyReadOnly) { this.setData({ error: '旧格式内容仅支持查看，请前往后台编辑，避免自动转换破坏原文。' }); return null; }
    const title = String(this.data.title || '').trim(), blocks = this.validBlocks();
    if (!title || !this.data.owner_id || !blocks.length) { this.setData({ error: '请填写标题、归属并保留至少一个正文块' }); return null; }
    const body = preserveLayoutBody(this.data.layoutBody, articleBody(blocks));
    return { blocks, draft: { title, body, body_format: 'article_v2', source_url: String(this.data.source_url || '').trim(), source_note: String(this.data.source_note || '').trim(), rights: this.data.rights, owner_id: this.data.owner_id, cover_media_id: this.data.cover_media_id, media_ids: this.data.layoutReadOnly ? this.data.layoutMediaIDs : mediaIDs(blocks), expected_revision: this.data.revision } };
  },
  async persistDraft(input, epoch) {
    const saved = await api.saveContent(input.draft, this.data.id);
    if (!this.current(epoch)) return null;
    this.setData({ id: saved.content_id, revision: saved.revision, version: saved.version, status: saved.status, blocks: input.blocks, renderedBlocks: decorateBlocks(input.blocks, this.data.media) });
    return saved;
  },
  async saveDraft() {
    if (this.data.busy) return;
    const input = this.draftInput();
    if (!input) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = await this.persistDraft(input, epoch);
      if (!saved || !this.current(epoch)) return;
      this.setData({ preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, busy: false, notice: '草稿已保存，可继续编辑或生成发布预览' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '保存草稿失败' }); }
  },
  publishKey() {
    const signature = JSON.stringify([this.data.id, this.data.revision, this.data.preview && this.data.preview.preview_token]);
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
    const cover = content.cover ? paths.find(asset => asset.media_id === content.cover.media_id) || null : null;
    let layoutReadOnly = false;
    if (content.body_format === 'article_v2') { try { const document = JSON.parse(content.body); layoutReadOnly = typeof document.layout_html === 'string' && !!document.layout_html; } catch (_) {} }
    const layoutBlocks = layoutReadOnly ? renderArticleDocument(content.body, paths, this.editorWidth(), 'contentedit-preview') : [];
    return { blocks: decorateBlocks(initialBlocks(content), paths), cover, layoutReadOnly, layoutBlocks };
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
      this.setData({ previewBlocks, previewCover, busy: false });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '预览媒体重试失败' }); }
  },
  async saveAndPreview() {
    if (this.data.busy) return;
    const input = this.draftInput();
    if (!input) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = await this.persistDraft(input, epoch);
      if (!saved || !this.current(epoch)) return;
      const preview = await api.previewContent(saved.content_id, saved.revision);
      if (!this.current(epoch)) return;
      const view = await this.previewView(preview);
      if (!this.current(epoch)) return;
      this.setData({ preview, previewBlocks: view.blocks, previewCover: view.cover, previewLayoutReadOnly: view.layoutReadOnly, previewLayoutBlocks: view.layoutBlocks, busy: false, notice: '预览已生成，请核对后发布' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '保存预览失败' }); }
  },
  async publish() {
    if (this.data.busy || !this.data.preview) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const result = await api.publishContent(this.data.id, this.data.revision, this.data.preview.preview_token, this.publishKey());
      if (!this.current(epoch)) return;
      this._publishSignature = ''; this._publishKey = '';
      this.setData({ revision: result.content.revision, version: result.content.version, status: result.content.status, preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, busy: false, notice: '已发布。分享请从已发布阅读页使用小程序原生入口。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '发布失败' }); }
  },
  openPublished() {
    if (!this.data.id || this.data.status !== 'published' || this.data.version < 1) return;
    wx.navigateTo({ url: '/pages/contentreader/contentreader?content_id=' + encodeURIComponent(this.data.id) + '&content_version=' + encodeURIComponent(String(this.data.version)) });
  },
  revoke() {
    if (!this.data.id || this.data.version < 1 || this.data.busy) return;
    const epoch = this._epoch;
    wx.showModal({ title: '撤回内容', content: '撤回后旧分享入口将不能继续阅读，请填写原因。', editable: true, placeholderText: '撤回原因', success: result => { if (this.current(epoch) && result.confirm && String(result.content || '').trim()) this.revokeContent(String(result.content).trim()); } });
  },
  async revokeContent(reason) {
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try { const content = await api.revokeContent(this.data.id, this.data.revision, reason, this.revokeKey(reason)); if (this.current(epoch)) { this._revokeSignature = ''; this._revokeKey = ''; this.setData({ revision: content.revision, status: content.status, preview: null, previewBlocks: [], previewCover: null, previewLayoutBlocks: [], previewLayoutReadOnly: false, busy: false, notice: '已撤回' }); } }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '撤回失败' }); }
  },
  deleteDraft() {
    if (!this.data.id || this.data.status !== 'draft' || this.data.version !== 0 || this.data.busy) return;
    const epoch = this._epoch;
    wx.showModal({ title: '删除草稿', content: '只删除这份从未发布的草稿，不会撤回已发布内容，也不会删除原媒体文件。', confirmText: '删除草稿', confirmColor: '#a12622', success: result => {
      if (this.current(epoch) && result.confirm) this.deleteDraftConfirmed();
    } });
  },
  async deleteDraftConfirmed() {
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
