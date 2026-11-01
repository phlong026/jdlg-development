'use strict';
const api = require('../../utils/phase3-api');

function displayTime(value) {
  if (!value) return '暂无访问';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const two = number => String(number).padStart(2, '0');
  return date.getFullYear() + '-' + two(date.getMonth() + 1) + '-' + two(date.getDate()) + ' ' + two(date.getHours()) + ':' + two(date.getMinutes());
}
function displayChannel(value) {
  if (value === 'app_message') return '微信好友';
  if (value === 'timeline') return '朋友圈';
  return '分享方式未记录';
}
function displayStatus(item) {
  if (item.revoked) return '已撤回';
  const expires = new Date(item.expires_at || '');
  if (!Number.isNaN(expires.getTime()) && expires.getTime() <= Date.now()) return '分享已过期';
  if (item.status === 'expired') return '内容版本已失效';
  if (item.status === 'published') return '分享中';
  if (item.status === 'unavailable' || item.status === 'revoked') return '内容不可用';
  return '分享记录';
}

Page({
  data: { loading: true, loadingMore: false, error: '', denied: false, loginRequired: false, items: [], next_cursor: '', as_of: '', metric_version: '' },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    this._token = token; this._epoch = (this._epoch || 0) + 1; this._morePending = false;
    this.setData({ items: [], next_cursor: '', as_of: '', metric_version: '', error: '', denied: false, loginRequired: !token, loading: !!token, loadingMore: false });
    if (token) this.load(true, this._epoch);
  },
  onUnload() { this._epoch = (this._epoch || 0) + 1; },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() { const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this._morePending = false; this.load(true, epoch).finally(() => wx.stopPullDownRefresh()); },
  async load(reset, suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token) return;
    if (!reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (!reset) this._morePending = true;
    if (reset) this.setData({ loading: true, error: '', denied: false, items: [], next_cursor: '', as_of: '', metric_version: '' });
    else this.setData({ loadingMore: true, error: '' });
    try {
      const query = { limit: 20 };
      if (!reset) Object.assign(query, { cursor: this.data.next_cursor, as_of: this.data.as_of, metric_version: this.data.metric_version });
      const page = await api.listMyShares(query);
      if (!this.current(epoch)) return;
      const rows = (page.items || []).map(item => {
        const visitorCount = Number(item.visitor_count || 0), pageViews = Number(item.page_views || 0);
        return { ...item, is_collection: item.route_key === 'pages/collectionreader/collectionreader', directory_views: Number(item.directory_views || 0), content_views: Number(item.content_views || 0), visitor_count: visitorCount, page_views: pageViews, avg_views_per_visitor: Number(item.avg_views_per_visitor || 0) || '', status_text: displayStatus(item), channel_text: displayChannel(item.channel), has_visits: visitorCount > 0 || pageViews > 0 || !!item.last_visit, last_visit_text: displayTime(item.last_visit), created_at_text: displayTime(item.created_at) };
      });
      this.setData({ items: reset ? rows : this.data.items.concat(rows), next_cursor: page.next_cursor || '', as_of: page.as_of || this.data.as_of, metric_version: page.metric_version || this.data.metric_version, loading: false, loadingMore: false });
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '本人分享记录加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false, loadingMore: false }); }
    finally { if (this.current(epoch)) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  retry() { this.load(true); },
  loadMore() { this.load(false, this._epoch); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  openVisits(event) {
    const item = this.data.items[Number(event.currentTarget.dataset.index)];
    if (item) wx.navigateTo({ url: '/pages/visitdetail/visitdetail?id=' + encodeURIComponent(item.share_id) + '&title=' + encodeURIComponent(item.title || '本人分享') });
  },
});
