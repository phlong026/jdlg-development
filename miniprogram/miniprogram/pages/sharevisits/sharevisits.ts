// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');

function displayTime(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const two = number => String(number).padStart(2, '0');
  return date.getFullYear() + '-' + two(date.getMonth() + 1) + '-' + two(date.getDate()) + ' ' + two(date.getHours()) + ':' + two(date.getMinutes());
}
function formatDuration(ms) {
  const total = Math.round(Number(ms || 0) / 1000);
  if (total <= 0) return '0秒';
  const minutes = Math.floor(total / 60), seconds = total % 60;
  if (minutes <= 0) return seconds + '秒';
  return minutes + '分' + (seconds ? seconds + '秒' : '');
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
  data: {
    activeTab: 'customers',
    customers: [], customers_cursor: '', customers_as_of: '', customerQuery: '', customerIntent: '', customerLoading: true, customerLoadingMore: false, customerError: '',
    materials: [], materials_cursor: '', materials_as_of: '', materialQuery: '', materialLoading: false, materialLoadingMore: false, materialError: '', materialsLoaded: false,
    shareQuery: '', loading: false, loadingMore: false, error: '', denied: false, loginRequired: false,
    items: [], next_cursor: '', as_of: '', metric_version: '', sharesLoaded: false,
  },
  // 每个 Tab 独立 epoch：慢响应不会覆盖其它 Tab 的新响应（codex 批次3 P2）。
  onShow() {
    wx.hideShareMenu();
    this._token = String(wx.getStorageSync('token') || '');
    this._epoch = (this._epoch || 0) + 1;
    this._epoch = (this._epoch || 0) + 1;
    this._epoch = (this._epoch || 0) + 1;
    this.setData({ loginRequired: !this._token });
    if (!this._token) return;
    // 每次 onShow 刷新当前激活 Tab（旧页语义：onShow 必重拉）；其余 Tab 待切换时再拉。
    this._loadedTabs = {};
    this.loadCustomers(true, this._epoch);
    if (this.data.activeTab === 'shares') this.loadShares(true, this._epoch);
    else if (this.data.activeTab === 'materials') this.loadMaterials(true, this._epoch);
  },
  onUnload() {
    this._epoch = (this._epoch || 0) + 1;
    this._epoch = (this._epoch || 0) + 1;
    this._epoch = (this._epoch || 0) + 1;
  },
  tokenAlive(epoch, which) {
    const map = { customers: '_epoch', materials: '_epoch', shares: '_epoch' };
    return epoch === this[map[which]] && this._token === String(wx.getStorageSync('token') || '');
  },
  onPullDownRefresh() {
    const done = () => wx.stopPullDownRefresh();
    if (this.data.activeTab === 'customers') this.loadCustomers(true, ++this._epoch).finally(done);
    else if (this.data.activeTab === 'shares') this.loadShares(true, ++this._epoch).finally(done);
    else this.loadMaterials(true, ++this._epoch).finally(done);
  },
  switchTab(event) {
    const tab = event.currentTarget.dataset.tab;
    if (tab === this.data.activeTab) { this.reloadActive(); return; }
    this.setData({ activeTab: tab });
    if (!this._token) return;
    if (tab === 'customers' && !this.data.customers.length && !this.data.customerError) this.loadCustomers(true, this._epoch);
    if (tab === 'materials' && !this._loadedTabs.materials) { this._loadedTabs.materials = true; this.loadMaterials(true, this._epoch); }
    if (tab === 'shares' && !this._loadedTabs.shares) { this._loadedTabs.shares = true; this.loadShares(true, this._epoch); }
  },
  reloadActive() {
    const tab = this.data.activeTab;
    if (tab === 'customers') this.loadCustomers(true, this._epoch);
    else if (tab === 'materials') this.loadMaterials(true, this._epoch);
    else this.loadShares(true, this._epoch);
  },

  // ===== 客户 Tab（my-visitors 意向榜） =====
  setCustomerQuery(e) { this.setData({ customerQuery: e.detail.value }); },
  toggleIntent() { this.setData({ customerIntent: this.data.customerIntent === 'high' ? '' : 'high' }); this.loadCustomers(true, ++this._epoch); },
  reloadCustomers() { this.loadCustomers(true, ++this._epoch); },
  loadMoreCustomers() { this.loadCustomers(false, ++this._epoch); },
  async loadCustomers(reset, suppliedEpoch) {
    if (!this._token) return;
    if (!reset && (this.data.customerLoadingMore || !this.data.customers_cursor)) return;
    const epoch = suppliedEpoch || ++this._epoch;
    if (reset) this.setData({ customerLoading: true, customerError: '', customers_cursor: '', customers_as_of: '' });
    else this.setData({ customerLoadingMore: true });
    try {
      const query = { limit: 20, as_of: this.data.customers_as_of };
      if (this.data.customerIntent) query.intent = this.data.customerIntent;
      if (this.data.customerQuery) query.q = this.data.customerQuery;
      if (!reset) query.cursor = this.data.customers_cursor;
      const page = await api.listMyVisitors(query);
      if (!this.tokenAlive(epoch, 'customers')) return;
      const rows = (page.items || []).map(item => ({
        visitor_key: item.visitor_key,
        nickname: item.nickname || '',
        views: Number(item.page_views || 0),
        intent_level: item.intent && item.intent.level ? item.intent.level : '',
        last_visit_text: displayTime(item.last_visit),
        duration_text: formatDuration(item.visible_ms),
      }));
      this.setData({
        customers: reset ? rows : this.data.customers.concat(rows),
        customers_cursor: page.next_cursor || '',
        customers_as_of: page.as_of || this.data.customers_as_of,
        customerLoading: false, customerLoadingMore: false, customerError: '',
      });
    } catch (error) {
      if (this.tokenAlive(epoch, 'customers')) this.setData({ customerError: error.message || '客户加载失败', customerLoading: false, customerLoadingMore: false });
    } finally { if (this.tokenAlive(epoch, 'customers')) this.setData({ customerLoading: false, customerLoadingMore: false }); }
  },
  openCustomer(event) {
    const item = this.data.customers[Number(event.currentTarget.dataset.index)];
    if (!item) return;
    const level = item.intent_level ? '&intent=' + encodeURIComponent(item.intent_level) : '';
    wx.navigateTo({ url: '/pages/visitdetail/visitdetail?visitor=' + encodeURIComponent(item.visitor_key) + '&name=' + encodeURIComponent(item.nickname || '客户') + level });
  },

  // ===== 素材 Tab（my-content-stats 热度榜） =====
  reloadMaterials() { this.loadMaterials(true, ++this._epoch); },
  loadMoreMaterials() { this.loadMaterials(false, ++this._epoch); },
  async loadMaterials(reset, suppliedEpoch) {
    if (!this._token) return;
    if (!reset && (this.data.materialLoadingMore || !this.data.materials_cursor)) return;
    const epoch = suppliedEpoch || ++this._epoch;
    if (reset) this.setData({ materialLoading: true, materialError: '', materials_cursor: '', materials_as_of: '' });
    else this.setData({ materialLoadingMore: true });
    try {
      const query = { limit: 20, as_of: this.data.materials_as_of };
      if (!reset) query.cursor = this.data.materials_cursor;
      const page = await api.listMyContentStats(query);
      if (!this.tokenAlive(epoch, 'materials')) return;
      const rows = (page.items || []).map(item => ({
        content_key: item.route_key + '\x00' + (item.content_id || '') + ':' + (item.content_version || 0),
        content_id: item.content_id, route_key: item.route_key,
        title: item.title || '未命名素材',
        visitor_count: Number(item.visitor_count || 0),
        views: Number(item.page_views || 0),
        forwards: Number(item.forward_count || 0),
        last_visit_text: displayTime(item.last_visit) || '',
      }));
      this.setData({
        materials: reset ? rows : this.data.materials.concat(rows),
        materials_cursor: page.next_cursor || '',
        materials_as_of: page.as_of || this.data.materials_as_of,
        materialLoading: false, materialLoadingMore: false, materialError: '',
      });
    } catch (error) {
      if (this.tokenAlive(epoch, 'materials')) this.setData({ materialError: error.message || '素材热度加载失败', materialLoading: false, materialLoadingMore: false });
    } finally { if (this.tokenAlive(epoch, 'materials')) this.setData({ materialLoading: false, materialLoadingMore: false }); }
  },

  // ===== 分享 Tab（原 sharevisits 迁入） =====
  setShareQuery(e) { this.setData({ shareQuery: e.detail.value }); this._shareQueryTimer && clearTimeout(this._shareQueryTimer); this._shareQueryTimer = setTimeout(() => this.reloadShares(), 400); },
  reloadShares() { this.loadShares(true, ++this._epoch); },
  async loadShares(reset, suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token) return;
    if (!reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || ++this._epoch;
    if (!reset) this._morePending = true;
    if (reset) this.setData({ loading: true, error: '', denied: false, items: [], next_cursor: '', as_of: '', metric_version: '' });
    else this.setData({ loadingMore: true, error: '' });
    try {
      const query = { limit: 20 };
      if (this.data.shareQuery) query.q = this.data.shareQuery;
      if (!reset) Object.assign(query, { cursor: this.data.next_cursor, as_of: this.data.as_of, metric_version: this.data.metric_version });
      const page = await api.listMyShares(query);
      if (!this.tokenAlive(epoch, 'shares')) return;
      const rows = (page.items || []).map(item => {
        const visitorCount = Number(item.visitor_count || 0), pageViews = Number(item.page_views || 0);
        return { ...item, is_collection: item.route_key === 'pages/collectionreader/collectionreader', directory_views: Number(item.directory_views || 0), content_views: Number(item.content_views || 0), visitor_count: visitorCount, page_views: pageViews, avg_views_per_visitor: Number(item.avg_views_per_visitor || 0) || '', status_text: displayStatus(item), channel_text: displayChannel(item.channel), has_visits: visitorCount > 0 || pageViews > 0 || !!item.last_visit, last_visit_text: displayTime(item.last_visit), created_at_text: displayTime(item.created_at) };
      });
      this._loadedTabs = this._loadedTabs || {};
      this._loadedTabs.shares = true;
      this.setData({ items: reset ? rows : this.data.items.concat(rows), next_cursor: page.next_cursor || '', as_of: page.as_of || this.data.as_of, metric_version: page.metric_version || this.data.metric_version, loading: false, loadingMore: false });
    } catch (error) { if (this.tokenAlive(epoch, 'shares')) this.setData({ error: error.message || '本人分享记录加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false, loadingMore: false }); }
    finally { if (this.tokenAlive(epoch, 'shares')) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  retry() { this.loadShares(true); },
  loadMore() { this.loadShares(false, ++this._epoch); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  openVisits(event) {
    const item = this.data.items[Number(event.currentTarget.dataset.index)];
    if (item) wx.navigateTo({ url: '/pages/visitdetail/visitdetail?id=' + encodeURIComponent(item.share_id) + '&title=' + encodeURIComponent(item.title || '本人分享') });
  },
});
