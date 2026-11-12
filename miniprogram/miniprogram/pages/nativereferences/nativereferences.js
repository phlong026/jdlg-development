'use strict';
const api = require('../../utils/phase3-api');
const pageTitle = pickerMode => pickerMode === 'media' ? '选择素材' : pickerMode === 'content' ? '选择内容' : '资料库';
const empty = (pickerMode = '', mediaKinds = '') => ({ pickerMode, mediaKinds, navigationTitle: pageTitle(pickerMode), libraryTab: 'selected', search: '', references: [], candidates: [], media: [], referenceCursor: '', candidateCursor: '', mediaCursor: '', loading: false, busy: false, searching: false, error: '', notice: '', loginRequired: false });
const candidateKey = item => String(item.content_id) + ':' + String(item.content_version);
const searchQuery = (search, extra = {}) => ({ limit: 20, ...extra, ...(search ? { q: search } : {}) });
const mediaRow = item => {
  const mime = String(item.mime || item.mime_type || '');
  const format = mime.includes('/') ? mime.split('/').pop().replace('jpeg', 'jpg').toUpperCase() : '';
  const size = Number(item.width) > 0 && Number(item.height) > 0 ? item.width + '×' + item.height : '';
  return Object.assign({}, item, { preview_path: '', meta_label: [format, size].filter(Boolean).join(' · ') || ({ image: '图片', video: '视频', document: '文件' }[item.kind] || '素材') });
};
Page({
  data: empty(),
  onLoad(options) {
    const pickerMode = options && (options.picker === 'media' || options.picker === 'content') ? options.picker : '';
    const mediaKinds = pickerMode === 'media' ? String(options.kind || '') : '';
    this._pickerMode = pickerMode; this._mediaKinds = mediaKinds;
    this._pickerChannel = this.getOpenerEventChannel ? this.getOpenerEventChannel() : null;
    this.setData(empty(pickerMode, mediaKinds));
  },
  onShow() {
    wx.hideShareMenu(); this._visible = true; this._epoch = (this._epoch || 0) + 1;
    const token = String(wx.getStorageSync('token') || '');
    if (this._token !== token) { this._loaded = false; this._attempts = new Map(); this.setData(empty(this._pickerMode, this._mediaKinds)); }
    this._token = token;
    if (!token) { this.setData({ loginRequired: true }); return; }
    this.load();
  },
  onHide() { this._loadPending = 0; this._visible = false; this._epoch += 1; },
  onUnload() { this._loadPending = 0; this._visible = false; this._epoch += 1; this._attempts = new Map(); this.setData(empty(this._pickerMode, this._mediaKinds)); },
  current(epoch) { return this._visible && epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  async load() {
    const key = JSON.stringify([this._token, this.data.pickerMode, this.data.mediaKinds, this.data.search]);
    if (this._loadPending && this._loadKey === key) return;
    this._loadKey = key;
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this._loadPending = epoch;
    const quiet = this._loaded === true;
    this.setData({ loading: !quiet, busy: false, searching: true, error: '', notice: '' });
    try {
      if (this.data.pickerMode === 'media') {
        const media = await api.listWorkbenchMedia(searchQuery(this.data.search, this.data.mediaKinds ? { kind: this.data.mediaKinds } : {}));
        if (this.current(epoch)) {
          this._loaded = true;
          const rows = (media.items || []).map(mediaRow);
          this.setData({ media: rows, mediaCursor: media.next_cursor || '', loading: false });
          await this.loadMediaPreviews(epoch, rows);
        }
        return;
      }
      const query = searchQuery(this.data.search);
      const [refs, candidates] = await Promise.all([this.data.pickerMode ? Promise.resolve({ items: [] }) : api.listNativeReferences({ limit: 20 }), api.listNativeReferenceCandidates(query)]);
      if (this.current(epoch)) { this._loaded = true; this.setData({ references: refs.items || [], candidates: candidates.items || [], referenceCursor: refs.next_cursor || '', candidateCursor: candidates.next_cursor || '', loading: false }); }
    } catch (error) {
      if (!this.current(epoch)) return;
      const authFailed = error.statusCode === 401 || error.statusCode === 403;
      if (authFailed) { this._loaded = false; this.setData(empty(this._pickerMode, this._mediaKinds)); }
      this.setData({ loading: false, loginRequired: error.statusCode === 401, error: error.statusCode === 403 ? '暂无资料库权限' : error.statusCode === 401 ? '' : (error.message || '原生内容暂不可用') });
    } finally {
      if (this._loadPending === epoch) this._loadPending = 0;
      if (this.current(epoch)) this.setData({ searching: false });
    }
  },
  async loadMore(event) {
    if (this.data.busy || this.data.searching) return;
    if (event.currentTarget.dataset.kind === 'media') {
      const cursor = this.data.mediaCursor; if (!cursor) return;
      const epoch = this._epoch; this.setData({ busy: true, error: '' });
      try {
        const page = await api.listWorkbenchMedia(searchQuery(this.data.search, { cursor, ...(this.data.mediaKinds ? { kind: this.data.mediaKinds } : {}) }));
        if (!this.current(epoch)) return;
        const seen = new Set(this.data.media.map(item => item.media_id));
        const incoming = (page.items || []).filter(item => !seen.has(item.media_id)).map(mediaRow);
        this.setData({ media: [...this.data.media, ...incoming], mediaCursor: page.next_cursor || '', busy: false });
        await this.loadMediaPreviews(epoch, incoming);
      } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '加载失败，可重试' }); }
      return;
    }
    const candidates = event.currentTarget.dataset.kind === 'candidates';
    const listKey = candidates ? 'candidates' : 'references', cursorKey = candidates ? 'candidateCursor' : 'referenceCursor';
    const cursor = this.data[cursorKey]; if (!cursor) return;
    const replaceWindow = this.data[listKey].length >= 200;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const page = await (candidates ? api.listNativeReferenceCandidates : api.listNativeReferences)({ cursor, limit: 20, q: candidates ? this.data.search : '' });
      if (!this.current(epoch)) return;
      const key = candidates ? candidateKey : item => item.reference_id;
      const existing = replaceWindow ? [] : this.data[listKey];
      const seen = new Set(existing.map(key));
      this.setData({ [listKey]: [...existing, ...(page.items || []).filter(item => !seen.has(key(item)))], [cursorKey]: page.next_cursor || '', busy: false, notice: replaceWindow ? '已切换到后续内容。重新加载可回到第一页。' : '' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '加载失败，可重试' }); }
  },
  onSearchInput(event) { this.setData({ search: String(event.detail.value || '').slice(0, 100) }); },
  submitSearch() { return this.load(); },
  async loadMediaPreviews(epoch, rows) {
    if (!api.downloadWorkbenchMedia) return;
    const queue = rows.filter(item => item.kind === 'image');
    const consume = async () => {
      while (queue.length && this.current(epoch)) {
        const item = queue.shift();
        let path = '';
        try { path = await api.downloadWorkbenchMedia(item.media_id); } catch (_) { /* 缩略图失败不影响选择。 */ }
        if (!this.current(epoch)) return;
        if (this.data.media.some(asset => asset.media_id === item.media_id)) this.setData({ media: this.data.media.map(asset => asset.media_id === item.media_id ? Object.assign({}, asset, { preview_path: path }) : asset) });
      }
    };
    await Promise.all([consume(), consume(), consume()]);
  },
  selectLibraryTab(event) {
    const tab = String(event.currentTarget.dataset.tab || '');
    if (tab === 'selected' || tab === 'available') this.setData({ libraryTab: tab });
  },
  selectMedia(event) {
    const id = String(event.currentTarget.dataset.id || '');
    const row = this.data.media.find(item => item.media_id === id); if (!row || !this._pickerChannel) return;
    const { preview_path, meta_label, ...asset } = row;
    this._pickerChannel.emit('selectMaterial', { kind: 'media', asset }); wx.navigateBack();
  },
  selectContent(event) {
    const id = String(event.currentTarget.dataset.id || ''), version = Number(event.currentTarget.dataset.version);
    const target = this.data.candidates.find(item => item.content_id === id && item.content_version === version); if (!target || !this._pickerChannel) return;
    this._pickerChannel.emit('selectMaterial', { kind: 'content', target }); wx.navigateBack();
  },
  async saveReference(event) {
    if (this.data.busy) return;
    const id = String(event.currentTarget.dataset.id || ''), version = Number(event.currentTarget.dataset.version);
    const source = this.data.candidates.find(item => item.content_id === id && item.content_version === version); if (!source) return;
    const signature = candidateKey(source);
    if (!this._attempts.has(signature)) this._attempts.set(signature, api.mutationKey('native-reference'));
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const ref = await api.saveNativeReference(id, version, this._attempts.get(signature));
      if (!this.current(epoch)) return;
      this._attempts.delete(signature);
      this.setData({ references: [ref, ...this.data.references.filter(item => item.reference_id !== ref.reference_id)], busy: false, notice: '已选用' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '选用失败，可重试' }); }
  },
  async openReference(event) {
    if (this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const target = await api.getNativeReferenceTarget(String(event.currentTarget.dataset.id || ''));
      if (!this.current(epoch)) return;
      if (!/^\/pages\/contentreader\/contentreader\?content_id=[A-Za-z0-9_-]{8,128}&content_version=[1-9]\d{0,8}$/.test(target.path || '')) throw new Error('内容目标无效，请刷新后重试');
      this.setData({ busy: false });
      wx.navigateTo({ url: target.path, fail: () => { if (this.current(epoch)) this.setData({ error: '目标页面打开失败，请重试' }); } });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '来源已失效，无法打开' }); }
  },
  removeReference(event) {
    if (this.data.busy) return;
    const id = String(event.currentTarget.dataset.id || ''), epoch = this._epoch;
	if (!this.data.references.some(item => item.reference_id === id)) return;
	wx.showModal({ title: '移除引用', content: '仅从我的资料库移除，不会删除原作品。', confirmText: '移除', success: result => {
		if (result.confirm && this.current(epoch) && this.data.references.some(item => item.reference_id === id)) this.performRemoveReference(id, epoch);
	} });
  },
  referenceDeleteMenu(event) {
	if (this.data.busy) return;
	const id = String(event.currentTarget.dataset.id || ''), epoch = this._epoch;
	if (!this.data.references.some(item => item.reference_id === id)) return;
	wx.showActionSheet({ itemList: ['移除引用'], success: result => {
		if (result.tapIndex === 0 && this.current(epoch)) this.removeReference({ currentTarget: { dataset: { id } } });
	} });
  },
  async performRemoveReference(id, epoch) {
	if (!this.current(epoch)) return;
	this.setData({ busy: true, error: '' });
	try { await api.deleteNativeReference(id); if (this.current(epoch)) this.setData({ references: this.data.references.filter(item => item.reference_id !== id), busy: false, notice: '已移除，原作品保留' }); }
	catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '移除失败' }); }
  },
  retry() { this.load(); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  openHistory() { wx.navigateTo({ url: '/pages/generatehistory/generatehistory' }); },
});
