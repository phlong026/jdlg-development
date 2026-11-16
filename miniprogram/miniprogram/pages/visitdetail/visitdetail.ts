// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
function routeValue(value) { try { return decodeURIComponent(String(value || '')); } catch (_) { return String(value || ''); } }

function displayTime(value) {
  if (!value) return '--';
  const date = new Date(value); if (Number.isNaN(date.getTime())) return String(value);
  const two = number => String(number).padStart(2, '0');
  return date.getFullYear() + '-' + two(date.getMonth() + 1) + '-' + two(date.getDate()) + ' ' + two(date.getHours()) + ':' + two(date.getMinutes());
}
function duration(ms) { const seconds = Math.max(0, Math.round(Number(ms || 0) / 1000)); return seconds < 60 ? seconds + '秒' : Math.floor(seconds / 60) + '分' + (seconds % 60) + '秒'; }

function compactTime(value) {
  const date = new Date(value), now = new Date();
  if (Number.isNaN(date.getTime())) return '--';
  const day = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const visitDay = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  const days = Math.round((day - visitDay) / 86400000);
  const two = number => String(number).padStart(2, '0');
  const clock = two(date.getHours()) + ':' + two(date.getMinutes());
  return (days === 0 ? '今天 ' : days === 1 ? '昨天 ' : two(date.getMonth() + 1) + '-' + two(date.getDate()) + ' ') + clock;
}

Page({
  data: { mode: 'share', visitor: '', id: '', title: '分享访问详情', loading: true, loadingMore: false, error: '', denied: false, loginRequired: false, items: [], entries: [], next_cursor: '', as_of: '', metric_version: '', identified_count: 0, anonymous_count: 0, intent_level: '', visitor_name: '', customerViews: 0, customerContents: 0, customerDuration: '0秒', canConvertLead: false, leadBusy: false, leadID: 0, leadConverted: false, handoffText: '', conversionReason: '', projectActivities: [], visibleProjectActivities: [], projectActivitiesExpanded: false, projectActivityCount: 0, contentTitle: '', customerAvatar: '', customerIdentityLabel: '', visibleEntries: [], entriesExpanded: false, relatedProjects: [] },
  onLoad(options) {
    const visitorKey = routeValue(options.visitor);
    const id = routeValue(options.id || options.share_id);
    this._visitor = visitorKey;
    this._entryFilters = {};
    if (options.share_cursor) this._entryFilters.share_cursor = routeValue(options.share_cursor);
    for (const key of ['as_of', 'start', 'end']) if (options[key] && !Number.isNaN(new Date(routeValue(options[key])).getTime())) this._entryFilters[key] = routeValue(options[key]);
    for (const key of ['content_id', 'route_key']) if (options[key]) this._entryFilters[key] = routeValue(options[key]);
    const contentVersion = Number(options.content_version || 0); if (Number.isInteger(contentVersion) && contentVersion > 0) this._entryFilters.content_version = contentVersion;
    this._mode = visitorKey ? 'customer' : 'share';
    if (this._mode === 'share' && !id) { this.setData({ loading: false, error: '缺少分享记录标识' }); return; }
    this.setData({
      mode: visitorKey ? 'customer' : 'share',
      visitor: visitorKey, canLinkProject: /^u:[1-9][0-9]*$/.test(visitorKey),
      id,
      title: routeValue(options.title) || '客户访问详情',
      intent_level: routeValue(options.intent),
      visitor_name: routeValue(options.name),
      contentTitle: routeValue(options.content_title),
    });
    const account = String(wx.getStorageSync('token') || '');
    this._contextAlive = true;
    const channel = this.getOpenerEventChannel && this.getOpenerEventChannel();
    if (channel && channel.on) channel.on('customerContext', context => {
      if (!this._contextAlive || account !== String(wx.getStorageSync('token') || '') || !context || context.visitor_key !== this._visitor) return;
      this._entryCoverContext = context;
      const entries = this.data.entries.map(entry => this.withEntryCover(entry));
      this.setData({ entries, visibleEntries: this.data.entriesExpanded ? entries : entries.slice(0, 2) });
    });
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    if (this._token && this._token !== token) this._entryCoverContext = null;
    this._token = token; this._epoch = (this._epoch || 0) + 1; this._morePending = false;
    this.setData({ customerAvatar: '', customerIdentityLabel: '', visibleEntries: [], entriesExpanded: false, relatedProjects: [], leadConverted: false, handoffText: '', conversionReason: '', projectActivities: [], projectActivityCount: 0, items: [], next_cursor: '', as_of: '', metric_version: '', identified_count: 0, anonymous_count: 0, error: '', denied: false, loginRequired: !token, loading: !!token, loadingMore: false });
    if (this._mode === 'customer' && !this._visitor) { this.setData({ loading: false, error: '缺少访客标识' }); return; }
    if (this._mode === 'share' && !this.data.id) { this.setData({ loading: false, error: '缺少分享记录标识' }); return; }
    if (token) this.load(true, this._epoch);
  },
  onUnload() { this._contextAlive = false; this._entryCoverContext = null; this._epoch = (this._epoch || 0) + 1; },
  withEntryCover(entry) {
    const context = this._entryCoverContext;
    return context && context.cover && context.content_id === entry.content_id && Number(context.content_version) === entry.content_version && context.route_key === entry.route_key ? { ...entry, cover: context.cover } : entry;
  },
  current(epoch) { return epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() { const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this._morePending = false; this._entryFilters = { ...(this._entryFilters || {}) }; delete this._entryFilters.as_of; delete this._entryFilters.share_cursor; this.setData({ as_of: '' }); return this.load(true, epoch).finally(() => wx.stopPullDownRefresh()); },
  async load(reset, suppliedEpoch) {
    if (this._mode === 'customer') return this.loadCustomerEntries(reset, suppliedEpoch);
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token || !this.data.id || !reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (!reset) this._morePending = true;
    if (reset) this.setData({ loading: true, error: '', denied: false, items: [], next_cursor: '', as_of: '', metric_version: '' });
    else this.setData({ loadingMore: true, error: '' });
    try {
      const query = { limit: 20, ...(this._entryFilters || {}) };
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
      const query = { limit: 50, ...(this._entryFilters || {}), as_of: this.data.as_of || (this._entryFilters || {}).as_of };
      if (!reset) query.cursor = this.data.next_cursor;
      const detail = await api.listMyVisitorEntries(this._visitor, query);
      if (!this.current(epoch)) return;
      const rows = (detail.items || []).map(entry => ({
        view_id: entry.view_id,
        session_id: entry.session_id,
        route_key: entry.route_key || '',
        content_id: entry.content_id || '',
        content_version: Number(entry.content_version || 0),
        can_manage_preview: entry.route_key === 'pages/contentreader/contentreader' && !!entry.content_id && Number(entry.content_version || 0) > 0,
        occurred_at: entry.occurred_at, occurred_text: displayTime(entry.occurred_at), occurred_short_text: compactTime(entry.occurred_at), cover: entry.cover && entry.cover.url || '', is_video: entry.type === 'video' || Number(entry.video_watch_ms || 0) > 0,
        title: entry.title && !String(entry.title).startsWith('pages/') ? entry.title : '分享内容',
        visible_text: duration(entry.visible_ms),
        watch_text: duration(entry.video_watch_ms),
        progress: Number(entry.progress_pct || 0),
      })).map(entry => this.withEntryCover(entry));
      const info = detail.visitor || detail;
      const entries = reset ? rows : this.data.entries.concat(rows);
      const projects = new Map();
      for (const activity of detail.project_activities || []) {
        if (activity.project_id && !projects.has(activity.project_id)) projects.set(activity.project_id, { project_id: activity.project_id, project_name: activity.project_name });
      }
      this.setData({
        entries, visibleEntries: this.data.entriesExpanded ? entries : entries.slice(0, 2),
        customerAvatar: info.avatar_url || '', customerIdentityLabel: info.identity_state === 'identified' ? '已识别' : '匿名访客', relatedProjects: Array.from(projects.values()),
        next_cursor: detail.next_cursor || '',
        as_of: detail.as_of || this.data.as_of,
        visitor_name: info.nickname || this.data.visitor_name || '匿名访客',
        customerViews: Number(detail.page_views || info.page_views || 0),
        customerContents: Number(detail.content_count || 0),
        customerDuration: duration(detail.visible_ms || info.visible_ms || 0),
        canConvertLead: detail.can_convert_lead === true && !(detail.customer_handoff && detail.customer_handoff.converted),
        leadConverted: !!(detail.customer_handoff && detail.customer_handoff.converted),
        handoffText: this.handoffLabel(detail.customer_handoff),
        conversionReason: String(detail.conversion_reason || ''),
        projectActivities: (detail.project_activities || []).map(item => ({ ...item, kindText: ({ note_open: '查看方案', note_feedback: '提交反馈', note_confirm: '确认方案', project_linked: '已关联项目' })[item.kind] || '项目动态', occurredText: displayTime(item.occurred_at), occurredShortText: compactTime(item.occurred_at) })),
        visibleProjectActivities: (detail.project_activities || []).slice(0, 3).map(item => ({ ...item, kindText: ({ note_open: '查看方案', note_feedback: '提交反馈', note_confirm: '确认方案', project_linked: '已关联项目' })[item.kind] || '项目动态', occurredText: displayTime(item.occurred_at), occurredShortText: compactTime(item.occurred_at) })),
        projectActivitiesExpanded: false,
        projectActivitiesTruncated: detail.project_activities_truncated === true,
        intent_level: info.intent && info.intent.level ? info.intent.level : this.data.intent_level,
        loading: false, loadingMore: false,
      });
    } catch (error) {
      if (this.current(epoch)) this.setData({ error: error.message || '客户浏览明细加载失败', loading: false, loadingMore: false });
    } finally { if (this.current(epoch)) this.setData({ loading: false, loadingMore: false }); }
  },
  customerAvatarLoadError(event) {
    if (this._token === String(wx.getStorageSync('token') || '') && event.currentTarget.dataset.avatar === this.data.customerAvatar) this.setData({ customerAvatar: '' });
  },
  entryCoverLoadError(event) {
    if (!this._contextAlive || this._token !== String(wx.getStorageSync('token') || '')) return;
    const { id, cover } = event.currentTarget.dataset;
    const failed = this.data.entries.find(entry => entry.view_id === id && entry.cover === cover);
    if (!failed) return;
    const context = this._entryCoverContext;
    if (context && context.cover === cover && context.content_id === failed.content_id && Number(context.content_version) === failed.content_version && String(context.route_key || '') === failed.route_key) this._entryCoverContext = null;
    const fallback = entry => entry.view_id === id && entry.cover === cover ? { ...entry, cover: '' } : entry;
    this.setData({ entries: this.data.entries.map(fallback), visibleEntries: this.data.visibleEntries.map(fallback) });
  },
  showCustomerIdentity() {
    const intent = ({ high: '高意向', medium: '中意向', low: '低意向', unknown: '待判断' })[this.data.intent_level] || '待判断';
    wx.showModal({ title: this.data.customerIdentityLabel, content: '系统意向：' + intent + '。意向仅作参考，需结合实际沟通判断。', showCancel: false });
  },
  toggleEntries() {
    const expanded = !this.data.entriesExpanded;
    this.setData({ entriesExpanded: expanded, visibleEntries: expanded ? this.data.entries : this.data.entries.slice(0, 2) });
  },
  async convertLead() {
    if (!this.data.canConvertLead || this.data.leadBusy || !this._visitor || this._token !== String(wx.getStorageSync('token') || '')) return;
    const epoch = this._epoch;
    this.setData({ leadBusy: true });
    try {
      const result = await api.convertMyVisitorToLead(this._visitor);
      if (!this.current(epoch)) return;
      this.setData({ leadID: Number(result.lead_id || 0), leadConverted: result.converted === true || result.existed === true || Number(result.lead_id || 0) > 0, handoffText: '已提交，等待承接', conversionReason: '', canConvertLead: false });
      wx.showToast({ title: result.existed ? '已在线索中' : '已转为线索', icon: 'success' });
    } catch (error) { if (this.current(epoch)) wx.showToast({ title: error.message || '暂时无法转为线索', icon: 'none' }); }
    finally { if (this.current(epoch)) this.setData({ leadBusy: false }); }
  },
  handoffLabel(state) {
    if (!state || !state.converted) return '';
    const labels = { new: state.assigned ? '已分配，等待跟进' : '已提交，等待承接', contacted: '已开始跟进', converted: '已完成转化', invalid: '本次线索已结束' };
    const label = labels[state.status] || '已提交，承接状态待更新';
    return label + (state.next_followup_at ? ' · 下次跟进 ' + displayTime(state.next_followup_at) : '');
  },
  explainConversion() { wx.showModal({ title: '业务联系授权', content: this.data.conversionReason || '请由客户本人在个人设置绑定手机号并同意业务联系，再返回重试。', showCancel: false }); },
  openCustomerProject(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if ((this.data.projectActivities || []).some(item => item.project_id === id)) wx.navigateTo({ url: '/pages/projectnotes/projectnotes?project_id=' + encodeURIComponent(id) });
  },
  createCustomerProject() { if (/^u:[1-9][0-9]*$/.test(this._visitor || '')) wx.navigateTo({ url: '/pages/projectnotes/projectnotes?customer_ref=' + encodeURIComponent(this._visitor) }); },
  toggleProjectActivities() { const expanded = !this.data.projectActivitiesExpanded; this.setData({ projectActivitiesExpanded: expanded, visibleProjectActivities: expanded ? this.data.projectActivities : this.data.projectActivities.slice(0, 3) }); },
  openEntryContent(event) { const entry = this.data.entries[Number(event.currentTarget.dataset.index)]; if (!entry || entry.can_manage_preview !== true) return; wx.navigateTo({ url: '/pages/contentreader/contentreader?management_preview=1&content_id=' + encodeURIComponent(entry.content_id) + '&content_version=' + encodeURIComponent(String(entry.content_version)) }); },
  goBackToCustomers() { wx.navigateBack({ fail: () => wx.redirectTo({ url: '/pages/sharevisits/sharevisits' }) }); },
  retry() { this.load(true); },
  loadMore() { this.load(false, this._epoch); },
  goArchive() { wx.navigateTo({ url: '/pages/projectnotes/projectnotes' }); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  toggleTimeline(event) {
    const key = String(event.currentTarget.dataset.key || '');
    this.setData({ items: this.data.items.map(item => item.visitor_key === key ? { ...item, expanded: !item.expanded } : item) });
  },
});
