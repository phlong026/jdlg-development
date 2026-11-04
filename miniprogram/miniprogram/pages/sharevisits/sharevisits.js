'use strict';
const api = require('../../utils/phase3-api');

function displayTime(value) {
  if (!value) return '暂无访问';
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
    // 客户 Tab
    customers: [], customers_cursor: '', customerQuery: '', customerIntent: '', customerLoading: true, customerLoadingMore: false, customerError: '',
    // 素材 Tab
    materials: [], materials_cursor: '', materialLoading: false, materialLoadingMore: false, materialError: '', materialsLoaded: false,
    // 分享 Tab（原 sharevisits 逻辑迁入）
    shareQuery: '', loading: false, loadingMore: false, error: '', denied: false, loginRequired: false,
    items: [], next_cursor: '', as_of: '', metric_version: '', sharesLoaded: false,
  },
  onShow() {
    wx.hideShareMenu();
    this._token = String(wx.getStorageSync('token') || '');
    this._epoch = (this._epoch || 0) + 1;
    this.setData({ loginRequired: !this._token });
    if (!this._token) return;
    // 每次 onShow 刷新当前激活 Tab（旧页语义：onShow 必重拉）；其余 Tab 待切换时再拉。
    this._loadedTabs = {};
    this.loadCustomers(true, this._epoch);
    if (this.data.activeTab === 'shares') this.loadShares(true, this._epoch);
    else if (this.data.activeTab === 'materials') this.loadMaterials(true, this._epoch);
  },
  onUnload() { this._epoch = (this._epoch || 0) + 1; },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() {
    const epoch = (this._epoch || 0) + 1; this._epoch = epoch;
    const tab = this.data.activeTab;
    const done = () => wx.stopPullDownRefresh();
    if (tab === 'customers') this.loadCustomers(true, epoch).finally(done);
    else if (tab === 'materials') this.loadMaterials(true, epoch).finally(done);
    else this.loadShares(true, epoch).finally(done);
  },
  switchTab(event) {
    const tab = event.currentTarget.dataset.tab;
    if (tab === this.data.activeTab) return;
    this.setData({ activeTab: tab });
    if (!this._token) return;
    if (tab === 'customers' && !this.data.customers.length && !this.data.customerError) this.loadCustomers(true, this._epoch);
    if (tab === 'materials' && !this._loadedTabs.materials) { this._loadedTabs.materials = true; this.loadMaterials(true, this._epoch); }
    if (tab === 'shares' && !this._loadedTabs.shares) { this._loadedTabs.shares = true; this.loadShares(true, this._epoch); }
  },

  // ===== 客户 Tab（my-visitors 意向榜） =====
  setCustomerQuery(e) { this.setData({ customerQuery: e.detail.value }); },
  toggleIntent() { this.setData({ customerIntent: this.data.customerIntent === 'high' ? '' : 'high' }); this.loadCustomers(true, this._epoch); },
  reloadCustomers() { return this.loadCustomers(true, this._epoch); },
  async loadCustomers(reset, suppliedEpoch) {
    if (!this._token) return;
    if (!reset && (this.data.customerLoadingMore || !this.data.customers_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (reset) this.setData({ customerLoading: true, customerError: '', customers: reset && !this.data.customers.length ? [] : this.data.customers, customers_cursor: '' });
    else this.setData({ customerLoadingMore: true });
    try {
      const query = { limit: 20 };
      if (this.data.customerIntent) query.intent = this.data.customerIntent;
      if (this.data.customerQuery) query.q = this.data.customerQuery;
      if (!reset) Object.assign(query, { cursor: this.data.customers_cursor });
      const page = await api.listMyVisitors(query);
      if (!this.current(epoch)) return;
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
        customerLoading: false, customerLoadingMore: false, customerError: '',
      });
    } catch (error) {
      if (this.current(epoch)) this.setData({ customerError: error.message || '客户加载失败', customerLoading: false, customerLoadingMore: false });
    } finally { if (this.current(epoch)) this.setData({ customerLoading: false, customerLoadingMore: false }); }
  },
  loadMoreCustomers() { return this.loadCustomers(false, this._epoch); },
  openCustomer(event) {
    const item = this.data.customers[Number(event.currentTarget.dataset.index)];
    if (!item) return;
    const level = item.intent_level ? '&intent=' + encodeURIComponent(item.intent_level) : '';
    wx.navigateTo({ url: '/pages/visitdetail/visitdetail?visitor=' + encodeURIComponent(item.visitor_key) + '&name=' + encodeURIComponent(item.nickname || '客户') + level });
  },

  // ===== 素材 Tab（my-content-stats 热度榜） =====
  reloadMaterials() { this.loadMaterials(true, this._epoch); },
  loadMoreMaterials() { return this.loadMaterials(false, this._epoch); },
  async loadMaterials(reset, suppliedEpoch) {
    if (!this._token) return;
    if (!reset && (this.data.materialLoadingMore || !this.data.materials_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (reset) this.setData({ materialLoading: true, materialError: '', materials: reset ? [] : this.data.materials, materials_cursor: '' });
    else this.setData({ materialLoadingMore: true });
    try {
      const query = { limit: 20 };
      if (!reset) Object.assign(query, { cursor: this.data.materials_cursor });
      const page = await api.listMyContentStats(query);
      if (!this.current(epoch)) return;
      const rows = (page.items || []).map(item => ({
        content_key: item.route_key + '\x00' + item.content_id,
        content_id: item.content_id, route_key: item.route_key,
        title: item.title || '未命名素材',
        visitor_count: Number(item.visitor_count || 0),
        views: Number(item.views || 0),
        forwards: Number(item.forwards || 0),
        created_at_text: item.created_at_text || '',
      }));
      this.setData({
        materials: reset ? rows : this.data.materials.concat(rows),
        materials_cursor: page.next_cursor || '',
        materialLoading: false, materialLoadingMore: false, materialError: '',
      });
    } catch (error) {
      if (this.current(epoch)) this.setData({ materialError: error.message || '素材热度加载失败', materialLoading: false, materialLoadingMore: false });
    } finally { if (this.current(epoch)) this.setData({ materialLoading: false, materialLoadingMore: false }); }
  },
  openMaterial(event) {
    const item = this.data.materials[Number(event.currentTarget.dataset.index)];
    if (!item || !item.content_id) return;
    // 打开素材详情并触发原生分享（现有 materials 出口）
    wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(item.content_id) });
  },

  // ===== 分享 Tab（原 sharevisits 迁入） =====
  setShareQuery(e) { this.setData({ shareQuery: e.detail.value }); this._shareQueryTimer && clearTimeout(this._shareQueryTimer); this._shareQueryTimer = setTimeout(() => this.reloadShares(), 400); },
  reloadShares() { return this.loadShares(true, this._epoch); },
  async loadShares(reset, suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token) return;
    if (!reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (!reset) this._morePending = true;
    if (reset) this.setData({ loading: true, error: '', denied: false, items: [], next_cursor: '', as_of: '', metric_version: '' });
    else this.setData({ loadingMore: true, error: '' });
    try {
      const query = { limit: 20 };
      if (this.data.shareQuery) query.q = this.data.shareQuery;
      if (!reset) Object.assign(query, { cursor: this.data.next_cursor, as_of: this.data.as_of, metric_version: this.data.metric_version });
      const page = await api.listMyShares(query);
      if (!this.current(epoch)) return;
      const rows = (page.items || []).map(item => {
        const visitorCount = Number(item.visitor_count || 0), pageViews = Number(item.page_views || 0);
        return { ...item, is_collection: item.route_key === 'pages/collectionreader/collectionreader', directory_views: Number(item.directory_views || 0), content_views: Number(item.content_views || 0), visitor_count: visitorCount, page_views: pageViews, avg_views_per_visitor: Number(item.avg_views_per_visitor || 0) || '', status_text: displayStatus(item), channel_text: displayChannel(item.channel), has_visits: visitorCount > 0 || pageViews > 0 || !!item.last_visit, last_visit_text: displayTime(item.last_visit), created_at_text: displayTime(item.created_at) };
      });
      this._loadedTabs = this._loadedTabs || {};
      this._loadedTabs.shares = true;
      this.setData({ items: reset ? rows : this.data.items.concat(rows), next_cursor: page.next_cursor || '', as_of: page.as_of || this.data.as_of, metric_version: page.metric_version || this.data.metric_version, loading: false, loadingMore: false });
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '本人分享记录加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false, loadingMore: false }); }
    finally { if (this.current(epoch)) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  retry() { this.loadShares(true); },
  loadMore() { this.loadShares(false, this._epoch); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  openVisits(event) {
    const item = this.data.items[Number(event.currentTarget.dataset.index)];
    if (item) wx.navigateTo({ url: '/pages/visitdetail/visitdetail?id=' + encodeURIComponent(item.share_id) + '&title=' + encodeURIComponent(item.title || '本人分享') });
  },
});
