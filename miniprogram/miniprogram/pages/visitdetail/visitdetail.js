'use strict';
const api = require('../../utils/phase3-api');

function displayTime(value) {
  if (!value) return '--';
  const date = new Date(value); if (Number.isNaN(date.getTime())) return String(value);
  const two = number => String(number).padStart(2, '0');
  return date.getFullYear() + '-' + two(date.getMonth() + 1) + '-' + two(date.getDate()) + ' ' + two(date.getHours()) + ':' + two(date.getMinutes());
}
function duration(ms) { const seconds = Math.max(0, Math.round(Number(ms || 0) / 1000)); return seconds < 60 ? seconds + '秒' : Math.floor(seconds / 60) + '分' + (seconds % 60) + '秒'; }

Page({
  data: { id: '', title: '分享访问详情', loading: true, loadingMore: false, error: '', denied: false, loginRequired: false, items: [], next_cursor: '', as_of: '', metric_version: '', identified_count: 0, anonymous_count: 0 },
  onLoad(options) {
    const id = String(options.id || options.share_id || ''); if (!id) { this.setData({ loading: false, error: '缺少分享记录标识' }); return; }
    this.setData({ id, title: String(options.title || '分享访问详情') });
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    this._token = token; this._epoch = (this._epoch || 0) + 1; this._morePending = false;
    this.setData({ items: [], next_cursor: '', as_of: '', metric_version: '', identified_count: 0, anonymous_count: 0, error: '', denied: false, loginRequired: !token, loading: !!token, loadingMore: false });
    if (!this.data.id) { this.setData({ loading: false, error: '缺少分享记录标识' }); return; }
    if (token) this.load(true, this._epoch);
  },
  onUnload() { this._epoch = (this._epoch || 0) + 1; },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() { const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this._morePending = false; this.load(true, epoch).finally(() => wx.stopPullDownRefresh()); },
  async load(reset, suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token || !this.data.id || !reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (!reset) this._morePending = true;
    if (reset) this.setData({ loading: true, error: '', denied: false, items: [], next_cursor: '', as_of: '', metric_version: '' });
    else this.setData({ loadingMore: true, error: '' });
    try {
      const query = { limit: 20 };
      if (!reset) Object.assign(query, { cursor: this.data.next_cursor, as_of: this.data.as_of, metric_version: this.data.metric_version });
      const page = await api.listMyShareVisits(this.data.id, query);
      if (!this.current(epoch)) return;
      const rows = (page.items || []).map(item => ({ ...item, visitor_key: item.visitor && item.visitor.visitor_key || '', first_visit_text: displayTime(item.first_visit), last_visit_text: displayTime(item.last_visit), visible_text: duration(item.visible_ms), visitor_name: item.visitor && item.visitor.nickname || (item.visitor && item.visitor.identity_state === 'identified' ? '已识别访客' : '匿名访客') }));
      this.setData({ items: reset ? rows : this.data.items.concat(rows), next_cursor: page.next_cursor || '', as_of: page.as_of || this.data.as_of, metric_version: page.metric_version || this.data.metric_version, identified_count: page.identified_count || 0, anonymous_count: page.anonymous_count || 0, loading: false, loadingMore: false });
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '分享访问详情加载失败', denied: error.statusCode === 403 || error.statusCode === 404, loginRequired: error.statusCode === 401, loading: false, loadingMore: false }); }
    finally { if (this.current(epoch)) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  retry() { this.load(true); },
  loadMore() { this.load(false, this._epoch); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
});
