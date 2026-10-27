'use strict';
const api = require('../../utils/phase3-api');

Page({
  data: { loading: true, loadingMore: false, error: '', denied: false, disabled: false, loginRequired: false, context: null, items: [], next_cursor: '', snapshot: null },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    this._token = token; this._epoch = (this._epoch || 0) + 1; this._morePending = false;
    this.setData({ context: null, items: [], next_cursor: '', snapshot: null, error: '', denied: false, disabled: false, loginRequired: !token, loading: !!token, loadingMore: false });
    if (token) this.load(true, this._epoch);
  },
  onUnload() { this._epoch = (this._epoch || 0) + 1; },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() { const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this._morePending = false; this.load(true, epoch).finally(() => wx.stopPullDownRefresh()); },
  async load(reset, suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token || !reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (!reset) this._morePending = true;
    if (reset) this.setData({ loading: true, error: '', denied: false, disabled: false, items: [], next_cursor: '', snapshot: null });
    else this.setData({ loadingMore: true, error: '' });
    try {
      let context = this.data.context;
      if (reset || !context) context = await api.workbenchContext();
      if (!this.current(epoch)) return;
      if (!context.enabled || context.can_manage === false) {
        this.setData({ context, disabled: !context.enabled, denied: context.can_manage === false, loading: false, loadingMore: false });
        return;
      }
      const query = { limit: 20 };
      if (!reset && this.data.next_cursor) query.cursor = this.data.next_cursor;
      if (!reset && this.data.snapshot) {
        query.as_of = this.data.snapshot.as_of;
        query.metric_version = this.data.snapshot.metric_version;
      }
      const page = await api.listContents(query);
      if (!this.current(epoch)) return;
      const snapshot = page.as_of ? { as_of: page.as_of, metric_version: page.metric_version } : this.data.snapshot;
      this.setData({ context, items: reset ? (page.items || []) : this.data.items.concat(page.items || []), next_cursor: page.next_cursor || '', snapshot, loading: false, loadingMore: false });
    } catch (error) {
      if (this.current(epoch)) this.setData({ error: error.message || '内容工作台加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false, loadingMore: false });
    } finally { if (this.current(epoch)) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  retry() { this.load(true); },
  loadMore() { this.load(false, this._epoch); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  createContent() { wx.navigateTo({ url: '/pages/contentedit/contentedit' }); },
  editContent(event) { wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(event.currentTarget.dataset.id) }); },
});
