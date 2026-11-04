// @ts-nocheck
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
  data: { mode: 'share', visitor: '', id: '', title: '分享访问详情', loading: true, loadingMore: false, error: '', denied: false, loginRequired: false, items: [], entries: [], next_cursor: '', as_of: '', metric_version: '', identified_count: 0, anonymous_count: 0, intent_level: '', visitor_name: '' },
  onLoad(options) {
    const visitorKey = String(options.visitor || '');
    const id = String(options.id || options.share_id || '');
    this._visitor = visitorKey;
    this._mode = visitorKey ? 'customer' : 'share';
    if (this._mode === 'share' && !id) { this.setData({ loading: false, error: '缺少分享记录标识' }); return; }
    this.setData({
      mode: visitor ? 'customer' : 'share',
      visitor,
      id,
      title: String(options.title || '客户访问详情'),
      intent_level: String(options.intent || ''),
      visitor_name: String(options.name || ''),
    });
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    this._token = token; this._epoch = (this._epoch || 0) + 1; this._morePending = false;
    this.setData({ items: [], next_cursor: '', as_of: '', metric_version: '', identified_count: 0, anonymous_count: 0, error: '', denied: false, loginRequired: !token, loading: !!token, loadingMore: false });
    if (this._mode === 'customer' && !this._visitor) { this.setData({ loading: false, error: '缺少访客标识' }); return; }
    if (this._mode === 'share' && !this.data.id) { this.setData({ loading: false, error: '缺少分享记录标识' }); return; }
    if (token) this.load(true, this._epoch);
  },
  onUnload() { this._epoch = (this._epoch || 0) + 1; },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() { const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this._morePending = false; this.load(true, epoch).finally(() => wx.stopPullDownRefresh()); },
  async load(reset, suppliedEpoch) {
    if (this._mode === 'customer') return this.loadCustomerEntries(reset, suppliedEpoch);
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
      const rows = (page.items || []).map(item => {
        const decorated = { ...item, visitor_key: item.visitor && item.visitor.visitor_key || '', first_visit_text: displayTime(item.first_visit), last_visit_text: displayTime(item.last_visit), visible_text: duration(item.visible_ms), visitor_name: item.visitor && item.visitor.nickname || (item.visitor && item.visitor.identity_state === 'identified' ? '已识别访客' : '匿名访客'), expanded: false };
        // B21: 从聚合数据推导简易时间线节点（首次→最近）；逐次明细需后端扩展。
        const sessions = Number(item.session_count || 0);
        if (sessions > 1 && item.first_visit && item.last_visit) {
          decorated.timeline = [
            { label: '首次访问', time: decorated.first_visit_text, detail: '开始关注' },
            { label: '最近访问', time: decorated.last_visit_text, detail: sessions + ' 次会话 · ' + (item.page_views || 0) + ' 次浏览' },
          ];
        } else if (item.first_visit) {
          decorated.timeline = [{ label: '访问', time: decorated.first_visit_text, detail: sessions + ' 次会话 · ' + (item.page_views || 0) + ' 次浏览' }];
        }
        return decorated;
      });
      this.setData({ items: reset ? rows : this.data.items.concat(rows), next_cursor: page.next_cursor || '', as_of: page.as_of || this.data.as_of, metric_version: page.metric_version || this.data.metric_version, identified_count: page.identified_count || 0, anonymous_count: page.anonymous_count || 0, loading: false, loadingMore: false });
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '分享访问详情加载失败', denied: error.statusCode === 403 || error.statusCode === 404, loginRequired: error.statusCode === 401, loading: false, loadingMore: false }); }
    finally { if (this.current(epoch)) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  async loadCustomerEntries(reset, suppliedEpoch) {
    if (this._mode !== 'customer' || !this._visitor) return;
    if (!reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (this._epoch = (this._epoch || 0) + 1);
    if (reset) this.setData({ loading: true, error: '', entries: [], next_cursor: '', as_of: '' });
    else this.setData({ loadingMore: true });
    try {
      const query = { limit: 50, as_of: this.data.as_of || undefined };
      if (!reset) query.cursor = this.data.next_cursor;
      const detail = await api.listMyVisitorEntries(this._visitor, query);
      if (epoch !== this._epoch) return;
      const rows = (detail.items || []).map(entry => ({
        view_id: entry.view_id,
        session_id: entry.session_id,
        occurred_text: displayTime(entry.occurred_at),
        title: entry.title || entry.content_id || '浏览',
        visible_text: duration(entry.visible_ms),
        watch_text: duration(entry.video_watch_ms),
        progress: Number(entry.progress_pct || 0),
      }));
      const info = detail.visitor || {};
      this.setData({
        entries: reset ? rows : this.data.entries.concat(rows),
        next_cursor: detail.next_cursor || '',
        as_of: detail.as_of || this.data.as_of,
        identified_count: info.views || 0,
        anonymous_count: info.visible_ms || 0,
        intent_level: info.intent && info.intent.level ? info.intent.level : this.data.intent_level,
        loading: false, loadingMore: false,
      });
    } catch (error) {
      if (epoch === this._epoch) this.setData({ error: error.message || '客户浏览明细加载失败', loading: false, loadingMore: false });
    } finally { if (epoch === this._epoch) this.setData({ loading: false, loadingMore: false }); }
  },
  retry() { this.load(true); },
  loadMore() { this.load(false, this._epoch); },
  goArchive() { wx.navigateTo({ url: '/pages/projectnotes/projectnotes' }); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  toggleTimeline(event) {
    const key = String(event.currentTarget.dataset.key || '');
    this.setData({ items: this.data.items.map(item => item.visitor_key === key ? { ...item, expanded: !item.expanded } : item) });
  },
});
