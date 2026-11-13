'use strict';
const api = require('../../utils/phase3-api');
function routeValue(value) { try { return decodeURIComponent(String(value || '')); } catch (_) { return String(value || ''); } }
const { resolveProtectedMediaURL } = require('../../utils/protected-media');

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
function displayTitle(item) {
  const title = String(item.title || '').trim();
  if (title && !title.startsWith('pages/')) return title;
  const names = { 'pages/index/index': '首页', 'pages/template/template': '模板广场', 'pages/aigenerate/aigenerate': 'AI设计', 'pages/distribution/distribution': '设计师中心', 'pages/collectionreader/collectionreader': '项目合集' };
  return names[item.route_key || title] || '分享内容';
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
    activeTab: 'customers', navigationTitle: '客户', contentFilter: '', contentVersion: 0, contentTitle: '',
    shareWindowCursor: '', shareWindowNext: '', shareWindowIndex: 0,
    rangeOptions: ['保留期内', '近3天', '近7天', '近30天'], rangeIndex: 0, filterStart: '', filterEnd: '',
    intentOptions: ['全部意向', '高意向', '中意向', '低意向', '待判断'], intentIndex: 0,
    sortOptions: ['最近访问', '浏览次数', '停留时长'], sortIndex: 0,
    customers: [], customers_cursor: '', customers_as_of: '', customerQuery: '', customerIntent: '', customerLoading: true, customerLoadingMore: false, customerError: '',
    materials: [], materials_cursor: '', materials_as_of: '', materialQuery: '', materialLoading: false, materialLoadingMore: false, materialError: '', materialsLoaded: false,
    shareQuery: '', loading: false, loadingMore: false, error: '', denied: false, loginRequired: false,
    items: [], next_cursor: '', as_of: '', metric_version: '', sharesLoaded: false,
  },
  onLoad(options) {
    const tab = ['customers', 'materials', 'shares'].includes(options && options.tab) ? options.tab : 'customers';
    const version = Number(options && options.content_version || 0);
    const validDate = value => value && !Number.isNaN(new Date(routeValue(value)).getTime()) ? routeValue(value) : '';
    const rangeIndex = Number(options && options.range_index || 0);
    this._drillAsOf = validDate(options && options.as_of);
    this.setData({ rangeIndex: Number.isInteger(rangeIndex) && rangeIndex >= 0 && rangeIndex <= 3 ? rangeIndex : 0, filterStart: validDate(options && options.start), filterEnd: validDate(options && options.end) });
    this.setData({ activeTab: tab, navigationTitle: this.tabTitle(tab), contentFilter: routeValue(options && options.content_id), contentVersion: Number.isInteger(version) && version > 0 ? version : 0, contentTitle: routeValue(options && options.title) });
  },
  tabTitle(tab) { return { customers: '客户', materials: '素材热度', shares: '分享记录' }[tab] || '客户'; },
  filterQuery() {
    const query = {};
    if (this.data.shareWindowCursor && this.data.activeTab !== 'shares') query.share_cursor = this.data.shareWindowCursor;
    if (this.data.filterStart) query.start = this.data.filterStart;
    if (this.data.filterEnd) query.end = this.data.filterEnd;
    if (this.data.contentFilter) {
      query.content_id = this.data.contentFilter;
      if (this.data.contentVersion) query.content_version = this.data.contentVersion;
    }
    return query;
  },
  selectRange(event) {
    const index = Number(event.detail.value), days = [0, 3, 7, 30][index];
    if (days === undefined) return;
    this._drillAsOf = ''; this.clearShareWindow();
    const end = new Date(), start = new Date(end.getTime() - days * 86400000);
    ['customers', 'materials', 'shares'].forEach(tab => this.bumpEpoch(tab));
    this._loadedTabs = {};
    this.setData({ rangeIndex: index, filterStart: days ? start.toISOString() : '', filterEnd: days ? end.toISOString() : '', customers: [], materials: [], items: [], customers_cursor: '', customers_as_of: '', materials_cursor: '', materials_as_of: '', next_cursor: '', as_of: '', metric_version: '', customerError: '', materialError: '', error: '' });
    return this.reloadActive();
  },
  selectIntent(event) {
    const index = Number(event.detail.value), intent = ['', 'high', 'medium', 'low', 'unknown'][index];
    if (intent === undefined) return;
    this.setData({ intentIndex: index, customerIntent: intent });
    return this.loadCustomers(true);
  },
  selectSort(event) { const index = Number(event.detail.value); if (index < 0 || index > 2) return; this.setData({ sortIndex: index }); return this.loadCustomers(true); },
  setMaterialQuery(event) { this.setData({ materialQuery: event.detail.value }); },
  goContents() { wx.navigateTo({ url: '/pages/contentworkbench/contentworkbench' }); },
  clearContentFilter() { this.setData({ contentFilter: '', contentVersion: 0, contentTitle: '' }); return this.loadShares(true); },
  openMaterial(event) {
    const item = this.data.materials[Number(event.currentTarget.dataset.index)];
    if (!item || !item.content_id) return;
    wx.navigateTo({ url: '/pages/sharevisits/sharevisits?tab=shares&content_id=' + encodeURIComponent(item.content_id) + '&content_version=' + encodeURIComponent(String(item.content_version || 1)) + '&title=' + encodeURIComponent(item.title) + '&range_index=' + this.data.rangeIndex + '&start=' + encodeURIComponent(this.data.filterStart || '') + '&end=' + encodeURIComponent(this.data.filterEnd || '') + '&as_of=' + encodeURIComponent(this.data.materials_as_of || '') });
  },
  // 每个 Tab 独立 epoch：慢响应不会覆盖其它 Tab 的新响应，Tab 间互不作废。
  bumpEpoch(which) {
    const key = { customers: '_epochCustomers', materials: '_epochMaterials', shares: '_epochShares' }[which];
    this[key] = (this[key] || 0) + 1;
    return this[key];
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    if (this._token !== token) this.clearShareWindow();
    if (this._token !== token) this.setData({ customers: [], materials: [], items: [], customers_cursor: '', customers_as_of: '', materials_cursor: '', materials_as_of: '', next_cursor: '', as_of: '', customerError: '', materialError: '', error: '' });
    this._token = token;
    ['customers', 'materials', 'shares'].forEach(which => this.bumpEpoch(which));
    this.setData({ loginRequired: !this._token });
    if (!this._token) return;
    // 每次 onShow 刷新当前激活 Tab（旧页语义：onShow 必重拉）；其余 Tab 待切换时再拉。
    this._loadedTabs = {};
    this.loadCustomers(true, this._epochCustomers);
    if (this.data.activeTab === 'shares') this.loadShares(true, this._epochShares);
    else if (this.data.activeTab === 'materials') this.loadMaterials(true, this._epochMaterials);
  },
  current(epoch) { return epoch === this._epochCustomers && this._token === String(wx.getStorageSync('token') || ''); },
  onUnload() {
    if (this._shareQueryTimer) clearTimeout(this._shareQueryTimer);
    ['customers', 'materials', 'shares'].forEach(which => this.bumpEpoch(which));
  },
  tokenAlive(epoch, which) {
    const map = { customers: '_epochCustomers', materials: '_epochMaterials', shares: '_epochShares' };
    return epoch === this[map[which]] && this._token === String(wx.getStorageSync('token') || '');
  },
  onPullDownRefresh() {
    this._drillAsOf = ''; if (this.data.activeTab !== 'shares') this.clearShareWindow();
    const done = () => wx.stopPullDownRefresh();
    if (this.data.activeTab === 'customers') this.loadCustomers(true, this.bumpEpoch('customers')).finally(done);
    else if (this.data.activeTab === 'shares') this.loadShares(true, this.bumpEpoch('shares')).finally(done);
    else this.loadMaterials(true, this.bumpEpoch('materials')).finally(done);
  },
  switchTab(event) {
    const tab = event.currentTarget.dataset.tab;
    if (tab === this.data.activeTab) { this.reloadActive(); return; }
    if (!['customers', 'materials', 'shares'].includes(tab)) return;
    this._loadedTabs = this._loadedTabs || {};
    this.setData({ activeTab: tab, navigationTitle: this.tabTitle(tab) });
    if (!this._token) return;
    if (tab === 'customers' && !this.data.customers.length && !this.data.customerError) this.loadCustomers(true);
    if (tab === 'materials' && !this._loadedTabs.materials) { this._loadedTabs.materials = true; this.loadMaterials(true); }
    if (tab === 'shares' && !this._loadedTabs.shares) { this._loadedTabs.shares = true; this.loadShares(true); }
  },
  clearShareWindow() {
    this._shareWindows = ['']; this._windowAsOf = '';
    this.setData({ shareWindowCursor: '', shareWindowIndex: 0, shareWindowNext: '' });
  },
  acceptShareWindow(page) {
    this._windowAsOf = page.as_of || this._windowAsOf;
    this.setData({ projectActivitiesTruncated: page.project_activities_truncated === true, shareWindowNext: page.share_window && page.share_window.next_cursor || '' });
  },
  nextShareWindow() {
    if (!this.data.shareWindowNext) return;
    this._shareWindows = (this._shareWindows || ['']).slice(0, this.data.shareWindowIndex + 1);
    this._shareWindows.push(this.data.shareWindowNext);
    return this.changeShareWindow(this.data.shareWindowIndex + 1);
  },
  previousShareWindow() { if (this.data.shareWindowIndex > 0) return this.changeShareWindow(this.data.shareWindowIndex - 1); },
  resetShareWindow() { this.clearShareWindow(); return this.changeShareWindow(0); },
  changeShareWindow(index) {
    ['customers', 'materials'].forEach(tab => this.bumpEpoch(tab)); this._loadedTabs = {};
    this.setData({ shareWindowIndex: index, shareWindowCursor: (this._shareWindows || [''])[index] || '', shareWindowNext: '', customers: [], materials: [], customers_cursor: '', materials_cursor: '', customers_as_of: '', materials_as_of: '' });
    return this.data.activeTab === 'materials' ? this.loadMaterials(true) : this.loadCustomers(true);
  },
  reloadActive() {
    const tab = this.data.activeTab;
    if (tab === 'customers') this.loadCustomers(true);
    else if (tab === 'materials') this.loadMaterials(true);
    else this.loadShares(true);
  },

  // ===== 客户 Tab（my-visitors 意向榜） =====
  // 操作入口不推进 epoch：loader 在通过锁检查后才自动分配（codex 复审 P1：先 bump 再被锁挡会作废在途请求）。
  setCustomerQuery(e) { this.setData({ customerQuery: e.detail.value }); },
  toggleIntent() { this.setData({ customerIntent: this.data.customerIntent === 'high' ? '' : 'high' }); this.loadCustomers(true); },
  reloadCustomers() { return this.loadCustomers(true); },
  loadMoreCustomers() { return this.loadCustomers(false); },
  async loadCustomers(reset, suppliedEpoch) {
    if (!this._token) return;
    if (!reset && (this._customerMorePending || !this.data.customers_cursor)) return;
    const epoch = suppliedEpoch || this.bumpEpoch('customers');
    if (reset && this._customerMorePending && !this.tokenAlive(this._customerMorePending, 'customers')) this._customerMorePending = false;
    if (reset) this.setData({ customerLoading: true, customerError: '', customers_cursor: '', customers_as_of: '' });
    else { this._customerMorePending = epoch; this.setData({ customerLoadingMore: true }); }
    try {
      const query = { limit: 20, ...this.filterQuery(), sort: ['last_visit', 'page_views', 'visible_ms'][this.data.sortIndex || 0], as_of: this.data.customers_as_of || this._windowAsOf || undefined };
      if (this.data.customerIntent) query.intent = this.data.customerIntent;
      if (this.data.customerQuery) query.q = this.data.customerQuery;
      if (!reset) query.cursor = this.data.customers_cursor;
      const page = await api.listMyVisitors(query);
      if (!this.tokenAlive(epoch, 'customers')) return;
      this.acceptShareWindow(page);
      const rows = (page.items || []).map(item => ({
        visitor_key: item.visitor_key,
        nickname: item.nickname || (item.identity_state === 'identified' ? '已识别访客' : '匿名访客'), avatar: item.avatar_url || '',
        project_activity_count: Number(item.project_activity_count || 0), views: Number(item.page_views || 0),
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
    // 锁只由持有者释放（codex 复审 P2：reset 不持分页锁，不得误清新分页的锁）；loading UI 态仅在请求存活时重置。
    } finally { if (this._customerMorePending === epoch) this._customerMorePending = false; if (this.tokenAlive(epoch, 'customers')) this.setData({ customerLoading: false, customerLoadingMore: false }); }
  },
  openCustomer(event) {
    const item = this.data.customers[Number(event.currentTarget.dataset.index)];
    if (!item) return;
    const level = item.intent_level ? '&intent=' + encodeURIComponent(item.intent_level) : '';
    wx.navigateTo({ url: '/pages/visitdetail/visitdetail?visitor=' + encodeURIComponent(item.visitor_key) + '&name=' + encodeURIComponent(item.nickname || '客户') + '&share_cursor=' + encodeURIComponent(this.data.shareWindowCursor || '') + level + '&as_of=' + encodeURIComponent(this.data.customers_as_of || '') + '&start=' + encodeURIComponent(this.data.filterStart || '') + '&end=' + encodeURIComponent(this.data.filterEnd || '') });
  },

  // ===== 素材 Tab（my-content-stats 热度榜） =====
  reloadMaterials() { return this.loadMaterials(true); },
  loadMoreMaterials() { return this.loadMaterials(false); },
  async loadMaterials(reset, suppliedEpoch) {
    if (!this._token) return;
    if (!reset && (this._materialMorePending || !this.data.materials_cursor)) return;
    const epoch = suppliedEpoch || this.bumpEpoch('materials');
    if (reset && this._materialMorePending && !this.tokenAlive(this._materialMorePending, 'materials')) this._materialMorePending = false;
    if (reset) this.setData({ materialLoading: true, materialError: '', materials_cursor: '', materials_as_of: '' });
    else { this._materialMorePending = epoch; this.setData({ materialLoadingMore: true }); }
    try {
      const query = { limit: 20, ...this.filterQuery(), sort: 'visitor_count', as_of: this.data.materials_as_of || this._windowAsOf || undefined };
      if (this.data.materialQuery) query.q = this.data.materialQuery;
      if (!reset) query.cursor = this.data.materials_cursor;
      const page = await api.listMyContentStats(query);
      if (!this.tokenAlive(epoch, 'materials')) return;
      this.acceptShareWindow(page);
      const rows = (page.items || []).map(item => ({
        content_key: item.route_key + '\x00' + (item.content_id || '') + ':' + (item.content_version || 0),
        content_id: item.content_id, content_version: item.content_version, route_key: item.route_key, coverAsset: item.cover || null, cover: '', coverError: false,
        title: displayTitle(item),
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
      this.loadMaterialCovers(rows, epoch);
    } catch (error) {
      if (this.tokenAlive(epoch, 'materials')) this.setData({ materialError: error.message || '素材热度加载失败', materialLoading: false, materialLoadingMore: false });
    } finally { if (this._materialMorePending === epoch) this._materialMorePending = false; if (this.tokenAlive(epoch, 'materials')) this.setData({ materialLoading: false, materialLoadingMore: false }); }
  },

  // ===== 分享 Tab（原 sharevisits 迁入） =====
  async loadMaterialCovers(rows, epoch) {
    const queue = rows.filter(item => item.coverAsset && item.coverAsset.kind === 'image');
    const worker = async () => {
      while (queue.length && this.tokenAlive(epoch, 'materials')) {
        const item = queue.shift(), asset = item.coverAsset;
        let cover = '';
        try {
          if (asset.media_id) cover = await api.downloadWorkbenchMedia(asset.media_id);
          else if (asset.url) cover = await resolveProtectedMediaURL(asset.url);
        } catch (_) { /* Keep the material usable when its cover cannot be read. */ }
        if (!this.tokenAlive(epoch, 'materials')) return;
        this.setData({ materials: this.data.materials.map(row => row.content_key === item.content_key ? { ...row, cover, coverError: !cover } : row) });
      }
    };
    await Promise.all([worker(), worker()]);
  },
  setShareQuery(e) { this.setData({ shareQuery: e.detail.value }); this._shareQueryTimer && clearTimeout(this._shareQueryTimer); this._shareQueryTimer = setTimeout(() => this.reloadShares(), 400); },
  reloadShares() { return this.loadShares(true); },
  async loadShares(reset, suppliedEpoch) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token) return;
    if (!reset && (this._shareMorePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || this.bumpEpoch('shares');
    if (!reset) this._shareMorePending = epoch;
    if (reset && this._shareMorePending && !this.tokenAlive(this._shareMorePending, 'shares')) this._shareMorePending = false;
    if (reset) this.setData({ loading: true, error: '', denied: false, items: [], next_cursor: '', as_of: '', metric_version: '' });
    else this.setData({ loadingMore: true, error: '' });
    try {
      const query = { limit: 20, ...this.filterQuery() };
      if (reset && this._drillAsOf) query.as_of = this._drillAsOf;
      if (this.data.shareQuery) query.q = this.data.shareQuery;
      if (!reset) Object.assign(query, { cursor: this.data.next_cursor, as_of: this.data.as_of, metric_version: this.data.metric_version });
      const page = await api.listMyShares(query);
      if (!this.tokenAlive(epoch, 'shares')) return;
      const rows = (page.items || []).map(item => {
        const visitorCount = Number(item.visitor_count || 0), pageViews = Number(item.page_views || 0);
        return { ...item, title: displayTitle(item), is_collection: item.route_key === 'pages/collectionreader/collectionreader', directory_views: Number(item.directory_views || 0), content_views: Number(item.content_views || 0), visitor_count: visitorCount, page_views: pageViews, avg_views_per_visitor: Number(item.avg_views_per_visitor || 0) || '', status_text: displayStatus(item), channel_text: displayChannel(item.channel), has_visits: visitorCount > 0 || pageViews > 0 || !!item.last_visit, last_visit_text: displayTime(item.last_visit), created_at_text: displayTime(item.created_at) };
      });
      this._loadedTabs = this._loadedTabs || {};
      this._loadedTabs.shares = true;
      this.setData({ items: reset ? rows : this.data.items.concat(rows), next_cursor: page.next_cursor || '', as_of: page.as_of || this.data.as_of, metric_version: page.metric_version || this.data.metric_version, loading: false, loadingMore: false });
    } catch (error) { if (this.tokenAlive(epoch, 'shares')) this.setData({ error: error.message || '本人分享记录加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false, loadingMore: false }); }
    // 锁只由持有者释放；loading UI 态仅在请求仍存活时重置。
    finally { if (this._shareMorePending === epoch) this._shareMorePending = false; if (this.tokenAlive(epoch, 'shares')) this.setData({ loading: false, loadingMore: false }); }
  },
  retry() { return this.loadShares(true); },
  loadMore() { return this.loadShares(false); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  openVisits(event) {
    const item = this.data.items[Number(event.currentTarget.dataset.index)];
    if (item) wx.navigateTo({ url: '/pages/visitdetail/visitdetail?id=' + encodeURIComponent(item.share_id) + '&title=' + encodeURIComponent(displayTitle(item)) + '&as_of=' + encodeURIComponent(this.data.as_of || '') + '&start=' + encodeURIComponent(this.data.filterStart || '') + '&end=' + encodeURIComponent(this.data.filterEnd || '') });
  },
});
