'use strict';
const api = require('../../utils/phase3-api');

const pendingImport = status => status === 'queued' || status === 'resolving' || status === 'downloading';
const statusText = status => ({ queued: '等待处理', resolving: '正在解析', downloading: '正在保存素材', ready: '草稿已生成', failed: '导入失败', uncertain: '结果待人工核对', blocked: '需要处理后重试' }[status] || '状态未知');
const platformText = platform => ({ wechat_article: '微信公众号', douyin: '抖音', xiaohongshu: '小红书', kuaishou: '快手', channels: '视频号' }[platform] || platform || '待识别');
const errorText = code => ({
  browser_required: '公众号文内视频采集服务尚未配置', requires_login: '视频号登录状态已失效', verification_required: '来源平台要求人工验证',
  rate_limited: '来源平台请求频繁，请稍后重试', unsupported_encryption: '该视频暂不支持安全导入', unsupported_embed: '正文含暂不支持的嵌入内容',
  media_source_expired: '媒体临时地址已失效或无法访问，可重新发起导入', unsupported_video: '该文章的视频格式暂不支持', article_too_large: '文章超出当前导入容量，请分篇处理', capture_timeout: '视频采集超时，可稍后重试', capture_failed: '视频采集失败，可稍后重试', risk_control: '来源平台要求人工验证，请联系管理员',
  provider_not_configured: '该平台解析服务尚未配置', article_fetch_failed: '公众号文章暂时无法读取，请稍后重试', invalid_link: '分享链接无法识别',
  private_storage_required: '素材存储私有访问验证未通过，请联系管理员', invalid_xhs_response: '公开页面未返回完整图文，请更新完整分享链接或手动上传素材', xhs_unknown_source: '该小红书内容类型暂不支持', xhs_gallery_incomplete: '图集中的图片未完整取得，请更新链接或手动上传', xhs_page_fetch_failed: '小红书页面暂时无法读取，请稍后重试',
  invalid_media: '素材文件格式校验未通过', invalid_document: '导入内容结构无法识别', invalid_draft: '导入草稿未通过校验', missing_video: '未能取得完整视频', media_too_large: '素材超过当前导入大小限制',
}[code] || '请稍后重试或联系管理员核对');
const decorateImport = item => Object.assign({}, item, { status_text: statusText(item.status), platform_text: platformText(item.platform), error_text: item.error_code ? errorText(item.error_code) : '' });
function contentKind(item) {
  if (item.type === 'note' || item.type === 'project_note') return 'note';
  if (item.type === 'document') return 'document';
  if (item.type === 'video' || (item.media || []).some(asset => asset.kind === 'video')) return 'video';
  return 'article';
}
function contentRow(item) {
  const kind = contentKind(item);
  const date = item.created_at ? new Date(item.created_at) : null;
  return Object.assign({}, item, {
    kind, kind_label: { note: '项目笔记', document: '文档资料', video: '视频', article: '图文' }[kind],
    status_label: { published: '已发布', revoked: '已撤回', draft: '草稿' }[item.status] || '草稿',
    action_label: item.status === 'published' && Number(item.version) > 0 ? '查看并分享' : item.status === 'draft' ? '继续编辑' : '查看内容',
    date_label: date && !Number.isNaN(date.getTime()) ? (date.getMonth() + 1) + '月' + date.getDate() + '日' : '',
    cover_path: '', cover_error: false,
    view_count: Number(item.page_views || 0),
    share_count: Number(item.share_count || 0),
    visitor_count: Number(item.identified_count || 0) + Number(item.anonymous_count || 0),
  });
}

Page({
  data: {
    view: 'studio', keyword: '', searchTerm: '', category: 'all', visibleItems: [], ownerFilter: '',
    categories: [{ key: 'all', label: '全部' }, { key: 'article', label: '图文' }, { key: 'note', label: '笔记' }, { key: 'video', label: '视频' }, { key: 'document', label: '文档' }],
    loading: true, loadingMore: false, error: '', denied: false, disabled: false, loginRequired: false, hasShareVisitAccess: false,
    context: null, items: [], next_cursor: '', snapshot: null,
    importServiceEnabled: false, importEnabled: false, importCapabilityReason: '', importPlatforms: [], importURL: '', importOwner: '', importOwnerIndex: 0,
    importRights: '', importBusy: false, importError: '', imports: [], importPolling: false,
  },
  onLoad(options) { this.setData({ view: options && options.view === 'import' ? 'import' : 'studio' }); },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    this.stopImportPoll();
    this._visible = true;
    // 同一账号已成功加载过：保留已渲染列表就地静默刷新，返回本页不再整页重载闪屏。
    const quiet = !!token && token === this._token && this._loaded === true;
    this._token = token; this._epoch = (this._epoch || 0) + 1; this._morePending = false;
    if (quiet) {
      this.setData({ loginRequired: false, loading: false, error: '' });
      void this.loadShareVisitAccess(token);
      // 静默刷新也要带项目卡：修改项目后返回工作台时不显示旧单子
      void this.loadActiveProjects(token, this._epoch);
      this.load(true, this._epoch, true);
      return;
    }
    this._loaded = false;
    this.setData({
      context: null, items: [], visibleItems: [], next_cursor: '', snapshot: null, activeProjects: [], error: '', denied: false, disabled: false, hasShareVisitAccess: false,
      loginRequired: !token, loading: !!token, loadingMore: false, importServiceEnabled: false, importEnabled: false, importCapabilityReason: '',
      importPlatforms: [], importURL: '', importOwner: '', importOwnerIndex: 0, importRights: '', importBusy: false,
      importError: '', imports: [], importPolling: false,
    });
    if (token) {
      void this.loadShareVisitAccess(token);
      void this.loadActiveProjects(token, this._epoch);
      this.load(true, this._epoch);
    }
  },
  onHide() { this._shareAccessRequestId = (this._shareAccessRequestId || 0) + 1; this._visible = false; this._epoch = (this._epoch || 0) + 1; this.setData({ hasShareVisitAccess: false }); this.stopImportPoll(); },
  onUnload() { this._shareAccessRequestId = (this._shareAccessRequestId || 0) + 1; this._visible = false; this._epoch = (this._epoch || 0) + 1; this.setData({ hasShareVisitAccess: false }); this.stopImportPoll(); },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  onPullDownRefresh() {
    this.stopImportPoll();
    const epoch = (this._epoch || 0) + 1; this._epoch = epoch; this._morePending = false;
    this.setData({ hasShareVisitAccess: false });
    void this.loadShareVisitAccess(this._token);
    void this.loadActiveProjects(this._token, epoch);
    this.load(true, epoch).finally(() => wx.stopPullDownRefresh());
  },
  async loadShareVisitAccess(token) {
    const requestId = (this._shareAccessRequestId || 0) + 1;
    this._shareAccessRequestId = requestId;
    const current = () => this._visible && requestId === this._shareAccessRequestId && token === this._token && token === String(wx.getStorageSync('token') || '');
    if (!token || token !== String(wx.getStorageSync('token') || '')) return;
    try {
      const context = await api.mySharesContext();
      if (!current()) return;
      const hasShareVisitAccess = context?.can_view === true && Array.isArray(context?.owners) && context.owners.length > 0;
      this.setData({ hasShareVisitAccess });
    } catch (error) {
      if (!current()) return;
      console.warn('获取分享访问权限失败:', error);
      this.setData({ hasShareVisitAccess: false });
    }
  },
  async load(reset, suppliedEpoch, quiet) {
    if (!this._token) this._token = String(wx.getStorageSync('token') || '');
    if (!this._token || !reset && (this._morePending || !this.data.next_cursor)) return;
    const epoch = suppliedEpoch || (reset ? (this._epoch = (this._epoch || 0) + 1) : this._epoch);
    if (!reset) this._morePending = true;
    if (reset) this.setData(quiet
      ? { error: '', denied: false, disabled: false }
      : { loading: true, error: '', denied: false, disabled: false, items: [], visibleItems: [], next_cursor: '', snapshot: null });
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
      if (this.data.searchTerm) query.content_q = this.data.searchTerm;
      if (this.data.ownerFilter) query.owner = this.data.ownerFilter;
      if (!reset && this.data.next_cursor) query.cursor = this.data.next_cursor;
      if (!reset && this.data.snapshot) { query.as_of = this.data.snapshot.as_of; query.metric_version = this.data.snapshot.metric_version; }
      const page = await api.listContents(query);
      if (!this.current(epoch)) return;
      const snapshot = page.as_of ? { as_of: page.as_of, metric_version: page.metric_version } : this.data.snapshot;
      const owners = context.owners || [];
      const selectedOwner = owners.includes(this.data.importOwner) ? this.data.importOwner : (owners[0] || '');
      this.setData({
        context, items: reset ? (page.items || []).map(contentRow) : this.data.items.concat((page.items || []).map(contentRow)), next_cursor: page.next_cursor || '', snapshot,
        importOwner: selectedOwner, importOwnerIndex: Math.max(0, owners.indexOf(selectedOwner)), loading: false, loadingMore: false,
      });
      this._loaded = true;
      this.filterItems();
      this.loadCovers(epoch, page.items || []);
      if (reset) await this.loadImports(epoch);
    } catch (error) {
      if (this.current(epoch)) this.setData({ error: error.message || '内容工作台加载失败', denied: error.statusCode === 403, loginRequired: error.statusCode === 401, loading: false, loadingMore: false });
    } finally { if (this.current(epoch)) { this._morePending = false; this.setData({ loading: false, loadingMore: false }); } }
  },
  filterItems() { this.setData({ visibleItems: this.data.items.filter(item => this.data.category === 'all' || item.kind === this.data.category) }); },
  setKeyword(event) { this.setData({ keyword: String(event.detail.value || '').slice(0, 30) }); },
  searchContents() { this.stopImportPoll(); this.setData({ searchTerm: this.data.keyword.trim() }); return this.load(true); },
  clearSearch() { this.setData({ keyword: '', searchTerm: '' }); return this.load(true); },
  selectCategory(event) {
    const category = this.data.categories.find(item => item.key === event.currentTarget.dataset.key);
    if (!category) return;
    if (category.unavailable) { wx.showToast({ title: category.label + '资料尚未开通', icon: 'none' }); return; }
    this.setData({ category: category.key }); this.filterItems();
  },
  // B13 公司/个人空间 Tab：复用服务端 ?owner= 过滤（带权限交集校验），切换即整表重载。
  selectOwnerTab(event) {
    const ownerID = String(event.currentTarget.dataset.owner || '');
    if (ownerID === this.data.ownerFilter) return;
    const choices = (this.data.context && this.data.context.owner_choices) || [];
    if (ownerID && !choices.some(item => item.owner_id === ownerID)) return;
    this.setData({ ownerFilter: ownerID, items: [], visibleItems: [], next_cursor: '', snapshot: null });
    return this.load(true);
  },
  async loadCovers(epoch, rows) {
    if (!api.downloadWorkbenchMedia) return;
    if (!this._coverState || this._coverState.epoch !== epoch) this._coverState = { epoch, queue: [], seen: new Set(), running: false };
    const state = this._coverState;
    rows.filter(item => item.cover && item.cover.kind === 'image').forEach(item => {
      if (!state.seen.has(item.content_id)) { state.seen.add(item.content_id); state.queue.push(item); }
    });
    if (state.running) return;
    state.running = true;
    // Two workers share the queue across pages; full video files are never fetched as covers.
    const consume = async () => {
      while (state.queue.length && this.current(epoch)) {
        const item = state.queue.shift();
        let local = '';
        try { local = await api.downloadWorkbenchMedia(item.cover.media_id); } catch (_) { /* A failed cover keeps the content row usable. */ }
        if (!this.current(epoch)) return;
        const index = this.data.items.findIndex(row => row.content_id === item.content_id);
        const visible = this.data.visibleItems.findIndex(row => row.content_id === item.content_id);
        const patch = {};
        if (index >= 0) { patch['items.' + index + '.cover_path'] = local; patch['items.' + index + '.cover_error'] = !local; }
        if (visible >= 0) { patch['visibleItems.' + visible + '.cover_path'] = local; patch['visibleItems.' + visible + '.cover_error'] = !local; }
        this.setData(patch);
      }
    };
    await Promise.all([consume(), consume()]);
    state.running = false;
  },
  navigate(url) { wx.navigateTo({ url, fail: () => wx.showToast({ title: '页面打开失败，请重试', icon: 'none' }) }); },
  openImportView() { this.navigate('/pages/contentworkbench/contentworkbench?view=import'); },
  // 引导卡直达新建图文：不经过 openCreateMenu，保持"创建菜单入口唯一"的既有护栏。
  startFirstContent() { this.navigate('/pages/contentedit/contentedit'); },
  async loadActiveProjects(token, epoch) {
    try {
      const page = await api.listProjectNoteProjects({ limit: 3 });
      // 竞态守卫：onShow 快速切换后旧响应不得覆盖新会话的数据
      if (epoch !== undefined && epoch !== this._epoch) return;
      const projects = (page.items || []).filter(p => p.status !== 'archived').slice(0, 2).map(p => ({
        project_id: p.project_id, name: p.name || '未命名项目',
        next_action: p.next_action || '继续推进',
        meta_text: p.location || p.style || '资料待完善',
      }));
      this.setData({ activeProjects: projects });
    } catch (_) { this.setData({ activeProjects: [] }); }
  },
  openProject(event) {
    const id = event.currentTarget.dataset.id;
    // 携带项目 ID，让 projectnotes 直接展开被点击的单子（目标页 onLoad 支持 project_id）
    if (id) this.navigate('/pages/projectnotes/projectnotes?project_id=' + encodeURIComponent(id));
  },
  openCollections() { this.navigate('/pages/projectcollections/projectcollections'); },
  openExistingAssets() { this.navigate('/pages/nativereferences/nativereferences'); },
  openShareVisits() {
    if (!this.data.hasShareVisitAccess || !this.current(this._epoch)) return;
    this.navigate('/pages/sharevisits/sharevisits');
  },
  openNotes() { this.navigate('/pages/projectnotes/projectnotes'); },
  openAIWriting() { this.navigate('/pages/contentwriting/contentwriting'); },
  openCreateMenu() {
    wx.showActionSheet({
      itemList: ['新建项目笔记', 'AI 生成设计', '口述转设计说明', '图文内容', '导入链接'],
      success: result => {
        const routes = ['/pages/projectnotes/projectnotes', '/pages/aigenerate/aigenerate', '/pages/contentwriting/contentwriting', '/pages/contentedit/contentedit', '/pages/contentworkbench/contentworkbench?view=import'];
        const url = routes[Number(result.tapIndex)];
        if (url) this.navigate(url);
      },
    });
  },
  pasteImportLink() { wx.getClipboardData({ success: result => this.setImportURL({ detail: { value: result.data } }), fail: () => wx.showToast({ title: '未能读取剪贴板，请手动粘贴', icon: 'none' }) }); },
  refreshImportList() { return this.loadImports(this._epoch); },
  async loadImports(epoch) {
    try {
      const capability = await api.importCapabilities();
      if (!this.current(epoch)) return;
      const platforms = (capability.platforms || []).map(item => Object.assign({}, item, { label: platformText(item.platform) }));
      const serviceEnabled = capability.enabled === true;
      const available = serviceEnabled && platforms.some(item => item.available);
      const platformReasons = [...new Set(platforms.map(item => String(item.reason || '').trim()).filter(Boolean))];
      const reason = capability.reason || (!available ? platformReasons.join('；') : '');
      this.setData({
        importServiceEnabled: serviceEnabled, importEnabled: available,
        importCapabilityReason: reason || (!serviceEnabled ? '链接导入功能当前未开启' : ''),
        importPlatforms: platforms, importError: '', importPolling: false,
        ...(!serviceEnabled ? { imports: [] } : {}),
      });
      if (!serviceEnabled) { this.stopImportPoll(); return; }
      await this.refreshImports(epoch);
    } catch (error) {
      if (this.current(epoch)) {
        this.stopImportPoll();
        this.setData({ importServiceEnabled: false, importEnabled: false, importCapabilityReason: '链接导入能力暂不可用', importError: '', importPolling: false });
      }
    }
  },
  async refreshImports(epoch) {
    if (!this.current(epoch) || !this.data.importServiceEnabled || this._importPollEpoch === epoch) return;
    this._importPollEpoch = epoch;
    this.setData({ importPolling: true });
    try {
      const writeVersion = this._importWriteVersion || 0;
      const result = await api.listImports();
      if (!this.current(epoch) || writeVersion !== (this._importWriteVersion || 0)) return;
      const imports = (result.items || []).map(decorateImport);
      this.setData({ imports, importError: '', importPolling: false });
      if (imports.some(item => pendingImport(item.status))) this.scheduleImportPoll(epoch);
    } catch (error) {
      if (this.current(epoch)) {
        this.setData({ importError: error.message || '导入进度刷新失败', importPolling: false });
        if (error.statusCode !== 401 && error.statusCode !== 403) this.scheduleImportPoll(epoch);
      }
    } finally { if (this._importPollEpoch === epoch) this._importPollEpoch = null; }
  },
  scheduleImportPoll(epoch) {
    this.stopImportPoll();
    if (!this.current(epoch) || !this.data.importServiceEnabled) return;
    this._importTimer = setTimeout(() => { this._importTimer = null; this.refreshImports(epoch); }, 3000);
  },
  stopImportPoll() { if (this._importTimer) clearTimeout(this._importTimer); this._importTimer = null; },
  resetImportKey() { this._importSignature = ''; this._importKey = ''; },
  setImportURL(event) { const value = String(event.detail.value || ''); if (value !== this.data.importURL) this.resetImportKey(); this.setData({ importURL: value, importError: '' }); },
  setImportOwner(event) {
    const index = Number(event.detail.value || 0); const choices = this.data.context && this.data.context.owner_choices || [];
    const owner = choices[index] && choices[index].owner_id || ''; if (owner !== this.data.importOwner) this.resetImportKey();
    this.setData({ importOwnerIndex: index, importOwner: owner, importError: '' });
  },
  setImportRights(event) { const value = event.detail.value; if (value !== this.data.importRights) this.resetImportKey(); this.setData({ importRights: value, importError: '' }); },
  importKey(signature) {
    if (signature !== this._importSignature) { this._importSignature = signature; this._importKey = api.mutationKey('content-import'); }
    return this._importKey;
  },
  async submitImport() {
    if (!this.current(this._epoch)) return;
    const sourceURL = String(this.data.importURL || '').trim();
    const owner = String(this.data.importOwner || '');
    const rights = String(this.data.importRights || '');
    if (!this.data.importEnabled || !sourceURL || !owner || (rights !== 'own' && rights !== 'authorized') || this.data.importBusy) {
      this.setData({ importError: '请填写分享链接、内容归属并确认素材权属' }); return;
    }
    const epoch = this._epoch; const signature = [sourceURL, owner, rights].join('\n');
    this.setData({ importBusy: true, importError: '' });
    try {
      const job = await api.submitImport({ source_url: sourceURL, owner_id: owner, rights, idempotency_key: this.importKey(signature) });
      if (!this.current(epoch)) return;
      if (signature !== [String(this.data.importURL || '').trim(), this.data.importOwner, this.data.importRights].join('\n')) { this.setData({ importBusy: false }); return; }
      this._importWriteVersion = (this._importWriteVersion || 0) + 1;
      const decorated = decorateImport(job);
      this.setData({ imports: [decorated].concat(this.data.imports.filter(item => item.import_id !== job.import_id)), importBusy: false });
      if (pendingImport(job.status)) this.scheduleImportPoll(epoch);
    } catch (error) { if (this.current(epoch)) this.setData({ importBusy: false, importError: error.message || '链接提交失败，请重试' }); }
  },
  async retryImport(event) {
    if (!this.current(this._epoch)) return;
    const id = String(event.currentTarget.dataset.id || '');
    const current = this.data.imports.find(item => item.import_id === id);
    if (!current || !current.can_retry || current.status === 'uncertain' || current.error_code === 'media_source_expired' || this.data.importBusy) return;
    const epoch = this._epoch; this.setData({ importBusy: true, importError: '' });
    try {
      const job = await api.retryImport(id);
      if (!this.current(epoch)) return;
      this._importWriteVersion = (this._importWriteVersion || 0) + 1;
      this.setData({ importBusy: false, imports: [decorateImport(job)].concat(this.data.imports.filter(item => item.import_id !== job.import_id)) });
      if (pendingImport(job.status)) this.scheduleImportPoll(epoch);
    }
    catch (error) { if (this.current(epoch)) this.setData({ importBusy: false, importError: error.message || '暂时无法重试' }); }
  },
  prepareReimport(event) {
    if (!this.current(this._epoch)) return;
    const id = String(event.currentTarget.dataset.id || '');
    const item = this.data.imports.find(job => job.import_id === id);
    const owners = this.data.context && this.data.context.owners || [];
    if (!item || (item.status !== 'failed' && item.status !== 'blocked') || item.error_code !== 'media_source_expired' || !item.source_url || !owners.includes(item.owner_id)) return;
    this.resetImportKey();
    this.setData({
      importURL: item.source_url, importOwner: item.owner_id, importOwnerIndex: owners.indexOf(item.owner_id), importRights: '',
      importError: '已填入原链接，请确认素材权属后再次开始导入',
    });
  },
  openImport(event) {
    if (!this.current(this._epoch)) return;
    const id = String(event.currentTarget.dataset.id || '');
    const item = this.data.imports.find(job => job.import_id === id);
    if (item && item.status === 'ready' && item.content_id) wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(item.content_id) });
  },
  retry() { this.load(true); },
  loadMore() { this.load(false, this._epoch); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  createContent() { this.navigate('/pages/contentedit/contentedit'); },
  openContent(event) {
    const data = event.currentTarget.dataset || {};
    const id = String(data.id || '');
    const version = Number(data.version || 0);
    if (!id) return;
    if (data.status === 'published' && version > 0 && !data.readOnly && (!data.route || data.route === 'pages/contentreader/contentreader')) {
      this.navigate('/pages/contentreader/contentreader?content_id=' + encodeURIComponent(id) + '&content_version=' + encodeURIComponent(String(version)));
      return;
    }
    this.navigate('/pages/contentedit/contentedit?id=' + encodeURIComponent(id));
  },
  editContent(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if (id) this.navigate('/pages/contentedit/contentedit?id=' + encodeURIComponent(id));
  },
});
