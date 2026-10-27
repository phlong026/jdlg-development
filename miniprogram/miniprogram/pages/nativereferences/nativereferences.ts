// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
const empty = () => ({ references: [], candidates: [], referenceCursor: '', candidateCursor: '', loading: false, busy: false, error: '', notice: '', loginRequired: false });
const candidateKey = item => String(item.content_id) + ':' + String(item.content_version);
Page({
  data: empty(),
  onShow() {
    wx.hideShareMenu(); this._visible = true; this._epoch = (this._epoch || 0) + 1;
    const token = String(wx.getStorageSync('token') || '');
    if (this._token !== token) { this._attempts = new Map(); this.setData(empty()); }
    this._token = token;
    if (!token) { this.setData({ loginRequired: true }); return; }
    this.load();
  },
  onHide() { this._visible = false; this._epoch += 1; },
  onUnload() { this._visible = false; this._epoch += 1; this._attempts = new Map(); this.setData(empty()); },
  current(epoch) { return this._visible && epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  async load() {
    const epoch = this._epoch = (this._epoch || 0) + 1; this.setData({ loading: true, busy: false, error: '', notice: '' });
    try {
      const [refs, candidates] = await Promise.all([api.listNativeReferences({ limit: 20 }), api.listNativeReferenceCandidates({ limit: 20 })]);
      if (this.current(epoch)) this.setData({ references: refs.items || [], candidates: candidates.items || [], referenceCursor: refs.next_cursor || '', candidateCursor: candidates.next_cursor || '', loading: false });
    } catch (error) { if (this.current(epoch)) this.setData({ loading: false, error: error.message || '原生内容暂不可用' }); }
  },
  async loadMore(event) {
    if (this.data.busy) return;
    const candidates = event.currentTarget.dataset.kind === 'candidates';
    const listKey = candidates ? 'candidates' : 'references', cursorKey = candidates ? 'candidateCursor' : 'referenceCursor';
    const cursor = this.data[cursorKey]; if (!cursor) return;
    const replaceWindow = this.data[listKey].length >= 200;
    const epoch = this._epoch; this.setData({ busy: true, error: '' });
    try {
      const page = await (candidates ? api.listNativeReferenceCandidates : api.listNativeReferences)({ cursor, limit: 20 });
      if (!this.current(epoch)) return;
      const key = candidates ? candidateKey : item => item.reference_id;
      const existing = replaceWindow ? [] : this.data[listKey];
      const seen = new Set(existing.map(key));
      this.setData({ [listKey]: [...existing, ...(page.items || []).filter(item => !seen.has(key(item)))], [cursorKey]: page.next_cursor || '', busy: false, notice: replaceWindow ? '已切换到后续内容。重新加载可回到第一页。' : '' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '加载失败，可重试' }); }
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
      this.setData({ references: [ref, ...this.data.references.filter(item => item.reference_id !== ref.reference_id)], busy: false, notice: '已选用这一已发布版本。打开后可分享原生卡片。' });
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
  async removeReference(event) {
    if (this.data.busy) return;
    const id = String(event.currentTarget.dataset.id || ''), epoch = this._epoch; this.setData({ busy: true, error: '' });
    try { await api.deleteNativeReference(id); if (this.current(epoch)) this.setData({ references: this.data.references.filter(item => item.reference_id !== id), busy: false, notice: '已移除引用，原作品保持不变。' }); }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '移除失败' }); }
  },
  retry() { this.load(); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  openHistory() { wx.navigateTo({ url: '/pages/generatehistory/generatehistory' }); },
});
export {};
