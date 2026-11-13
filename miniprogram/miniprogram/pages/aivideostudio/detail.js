'use strict';
const api = require('../../utils/ai-video-studio-api');
const phase3 = require('../../utils/phase3-api');
const statusText = status => ({ queued: '等待处理', submitting: '提交中', submitted: '已受理', running: '生成中', processing: '生成中', partial_success: '部分完成', completed: '已完成', failed: '失败', uncertain: '待核对', cancelled: '已取消' }[status] || '状态待确认');
const modeText = mode => ({ text: '文生视频', first_frame: '首帧图生', first_last_frame: '首尾帧', reference: '参考生成' }[mode] || '视频创作');

Page({
  data: { loading: true, error: '', batch: null, snapshot: {}, results: [], statusLabel: '', adoptingResultID: '', cancelling: false, noteAdoptionOpen: false, adoptionStep: 'project', adoptionProjects: [], adoptionNotes: [], adoptionPositions: [], adoptionPosition: 0, adoptionCursor: '', adoptionLoading: false, adoptionError: '', adoptionProject: null, adoptionTarget: null, adoptionPending: false },
  onLoad(options) { this.id = options.batch_id; this._token = String(wx.getStorageSync('token') || ''); this._visible = true; this.load(); },
  onShow() {
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._token) {
      this.stopPolling(); this.abortDownload(); this.closeNoteAdoption();
      this._token = token; this._epoch = (this._epoch || 0) + 1; this._visible = true;
      this.setData({ batch: null, snapshot: {}, results: [], loading: !!token, error: token ? '' : '请先登录', adoptingResultID: '', cancelling: false });
      if (token) return this.load();
      return;
    }
    if (this._visible === false) { this._visible = true; return this.load(true); }
  },
  onHide() { this.closeNoteAdoption(); this._visible = false; this._epoch = (this._epoch || 0) + 1; this.stopPolling(); this.abortDownload(); },
  onUnload() { this._unloaded = true; this.onHide(); },
  current(epoch) { return !this._unloaded && this._token === String(wx.getStorageSync('token') || '') && (epoch === undefined || epoch === this._epoch); },
  abortDownload() { if (this._downloadTask && this._downloadTask.abort) this._downloadTask.abort(); this._downloadTask = null; },
  stopPolling() { if (this._pollTimer) clearTimeout(this._pollTimer); this._pollTimer = null; },
  schedulePoll() {
    this.stopPolling();
    if (!this.current() || !this._visible || ['completed', 'partial_success', 'failed', 'cancelled'].includes((this.data.batch || {}).status)) return;
    this._pollTimer = setTimeout(() => { this._pollTimer = null; return this.load(true); }, Math.min(30000, 3000 * Math.pow(2, this._pollFailures || 0)));
  },
  onPullDownRefresh() { this.load().finally(() => wx.stopPullDownRefresh()); },
  async load(silent) {
    if (this._visible === false || !this.current()) return;
    this.stopPolling();
    const epoch = this._epoch = (this._epoch || 0) + 1;
    if (!this.id) { this.setData({ loading: false, error: '任务标识无效' }); return; }
    this.setData(silent === true ? { error: '' } : { loading: true, error: '' });
    try {
      const batch = await api.getBatch(this.id);
      if (!this.current(epoch)) return;
      this._pollFailures = 0;
      const snapshot = Object.assign({}, batch.snapshot || {}, { mode_label: modeText((batch.snapshot || {}).mode) });
      const results = (batch.results || []).map(item => Object.assign({}, item, {
        status_label: statusText(item.status), playback_url: ((this.data.results || []).find(previous => previous.result_id === item.result_id) || {}).playback_url || '',
        retained: !!item.stable_media_id || ['retained', 'ready', 'completed'].includes(item.retention_status),
      }));
      this.setData({ loading: false, batch: Object.assign({}, batch, { charge: batch.charge || {} }), snapshot, results, statusLabel: statusText(batch.status) });
    } catch (error) { if (this.current(epoch)) { this._pollFailures = Math.min(4, (this._pollFailures || 0) + 1); this.setData({ loading: false, error: error.message || '任务加载失败' }); } }
    if (this.current(epoch)) this.schedulePoll();
  },
  async cancelQueued() {
    if (this.data.cancelling || !(this.data.batch || {}).can_cancel) return;
    this.setData({ cancelling: true, error: '' });
    try {
      await api.cancelBatch(this.id);
      if (this.current()) await this.load(true);
    } catch (error) { if (this.current()) this.setData({ error: error.message || '取消失败，任务可能已开始，请刷新状态' }); }
    finally { if (this.current()) this.setData({ cancelling: false }); }
  },
  async play(event) {
    const index = Number(event.currentTarget.dataset.index), item = this.data.results[index];
    try {
      const value = await api.playback(item.result_id);
      if (!this.current()) return;
      const results = this.data.results.slice(), url = String(value.url || value.playback_url || '');
      results[index] = Object.assign({}, item, { playback_url: url.startsWith('/') ? api.ORIGIN + url : url });
      this.setData({ results, error: '' });
    } catch (error) { if (this.current()) this.setData({ error: error.message || '播放地址获取失败' }); }
  },
  download(event) {
    const index = Number(event.currentTarget.dataset.index), item = this.data.results[index], token = this._token;
    if (!item.playback_url) return;
    this.abortDownload(); wx.showLoading({ title: '正在下载' });
    this._downloadTask = wx.downloadFile({
      url: item.playback_url,
      success: response => {
        this._downloadTask = null; wx.hideLoading();
        if (!this.current() || token !== String(wx.getStorageSync('token') || '')) return;
        if (response.statusCode !== 200) { this.setData({ error: '视频下载失败，请刷新播放地址后重试' }); return; }
        wx.saveVideoToPhotosAlbum({
          filePath: response.tempFilePath,
          success: () => { if (this.current() && token === String(wx.getStorageSync('token') || '')) wx.showToast({ title: '已保存到相册', icon: 'success' }); },
          fail: error => { if (this.current() && token === String(wx.getStorageSync('token') || '')) this.setData({ error: /auth deny|authorize/i.test(String(error && error.errMsg || '')) ? '未获得相册权限，请在设置中允许后重试' : '保存相册失败，请重试' }); },
        });
      },
      fail: error => { this._downloadTask = null; wx.hideLoading(); if (this.current() && !/abort/i.test(String(error && error.errMsg || ''))) this.setData({ error: '网络下载失败，请重试' }); },
    });
  },
  async retain(event) {
    const index = Number(event.currentTarget.dataset.index), item = this.data.results[index];
    if (item.retained) return;
    try {
      const value = await api.retain(item.result_id);
      if (!this.current()) return;
      const results = this.data.results.slice();
      results[index] = Object.assign({}, item, { retained: true, retention_status: 'retained', stable_media_id: value.media_id || item.stable_media_id });
      this.setData({ results, error: '' });
    } catch (error) { if (this.current()) this.setData({ error: error.message || '素材留存失败，可稍后重试' }); }
  },
  adoptionCurrent(epoch) { return this.current() && this._visible !== false && epoch === this._adoptionEpoch; },
  closeNoteAdoption() {
    this._adoptionEpoch = (this._adoptionEpoch || 0) + 1;
    this._adoptionInput = null; this._adoptionMediaID = '';
    this.setData({ noteAdoptionOpen: false, adoptionProjects: [], adoptionNotes: [], adoptionPositions: [], adoptionTarget: null, adoptionProject: null, adoptionLoading: false, adoptionError: '', adoptionPending: false });
  },
  async startNoteAdoption(event) {
    const item = this.data.results[Number(event.currentTarget.dataset.index)];
    if (!item || !item.stable_media_id || this.data.adoptingResultID) return;
    this.closeNoteAdoption();
    this._adoptionMediaID = item.stable_media_id;
    const epoch = this._adoptionEpoch;
    this.setData({ noteAdoptionOpen: true, adoptionStep: 'project', adoptionPosition: 0, adoptionCursor: '' });
    try {
      const pending = api.pendingAdoption('project-note', this._adoptionMediaID);
      if (pending) {
        const target = await api.getProjectNoteAdoptionTarget(pending.project_id, pending.note_id);
        if (!this.adoptionCurrent(epoch)) return;
        this._adoptionInput = pending;
        this.setData({ adoptionStep: 'position', adoptionTarget: target, adoptionPending: true, adoptionPosition: pending.insert_at, adoptionPositions: [{ label: '重试上次选定位置', value: pending.insert_at }] });
        return;
      }
      return this.loadAdoptionProjects();
    } catch (error) { if (this.adoptionCurrent(epoch)) this.setData({ adoptionError: error.message || '项目加载失败' }); }
  },
  async loadAdoptionProjects(event) {
    const append = !!(event && event.currentTarget), epoch = this._adoptionEpoch;
    if (append && !this.data.adoptionCursor) return;
    this.setData({ adoptionLoading: true, adoptionError: '' });
    try {
      const page = await phase3.listProjectNoteProjects({ limit: 30, cursor: append ? this.data.adoptionCursor : '' });
      if (!this.adoptionCurrent(epoch)) return;
      this.setData({ adoptionLoading: false, adoptionProjects: append ? this.data.adoptionProjects.concat(page.items || []) : page.items || [], adoptionCursor: page.next_cursor || '' });
    } catch (error) { if (this.adoptionCurrent(epoch)) this.setData({ adoptionLoading: false, adoptionError: error.message || '项目加载失败' }); }
  },
  selectAdoptionProject(event) {
    const project = this.data.adoptionProjects.find(item => item.project_id === String(event.currentTarget.dataset.id));
    if (!project || this.data.adoptionLoading) return;
    this.setData({ adoptionProject: project, adoptionStep: 'note', adoptionCursor: '', adoptionNotes: [] });
    return this.loadAdoptionNotes();
  },
  async loadAdoptionNotes(event) {
    const append = !!(event && event.currentTarget), epoch = this._adoptionEpoch;
    if (!this.data.adoptionProject || (append && !this.data.adoptionCursor)) return;
    this.setData({ adoptionLoading: true, adoptionError: '' });
    try {
      const page = await phase3.getProjectNoteHistory(this.data.adoptionProject.project_id, 'notes', { limit: 30, cursor: append ? this.data.adoptionCursor : '' });
      if (!this.adoptionCurrent(epoch)) return;
      this.setData({ adoptionLoading: false, adoptionNotes: append ? this.data.adoptionNotes.concat(page.items || []) : page.items || [], adoptionCursor: page.next_cursor || '' });
    } catch (error) { if (this.adoptionCurrent(epoch)) this.setData({ adoptionLoading: false, adoptionError: error.message || '笔记加载失败' }); }
  },
  async selectAdoptionNote(event) {
    if (this.data.adoptionLoading || !this.data.adoptionProject) return;
    const noteID = String(event.currentTarget.dataset.id || ''), epoch = this._adoptionEpoch;
    if (!this.data.adoptionNotes.some(item => item.note_id === noteID)) return;
    this.setData({ adoptionLoading: true, adoptionError: '' });
    try {
      const target = await api.getProjectNoteAdoptionTarget(this.data.adoptionProject.project_id, noteID);
      if (!this.adoptionCurrent(epoch)) return;
      const blocks = target.blocks || [], positions = [{ value: 0, label: '放在正文最前面' }];
      blocks.forEach((block, index) => positions.push({ value: index + 1, label: '放在第 ' + (index + 1) + ' 块后：' + (block.text ? String(block.text).slice(0, 24) : block.kind === 'image' ? '图片' : block.kind === 'video' ? '视频' : '原内容') }));
      this._adoptionInput = null;
      this.setData({ adoptionLoading: false, adoptionStep: 'position', adoptionTarget: target, adoptionPositions: positions, adoptionPosition: positions.length - 1, adoptionPending: false });
    } catch (error) { if (this.adoptionCurrent(epoch)) this.setData({ adoptionLoading: false, adoptionError: error.message || '笔记读取失败' }); }
  },
  setAdoptionPosition(event) { if (!this.data.adoptionPending && !this.data.adoptionLoading) this.setData({ adoptionPosition: Number(event.detail.value) || 0 }); },
  async confirmNoteAdoption() {
    const target = this.data.adoptionTarget, epoch = this._adoptionEpoch;
    if (!target || this.data.adoptionLoading || !this._adoptionMediaID) return;
    const position = this.data.adoptionPositions[this.data.adoptionPosition];
    const input = this._adoptionInput || { project_id: target.project_id, note_id: target.note_id, expected_revision: target.revision, insert_at: position ? position.value : 0 };
    this._adoptionInput = input;
    this.setData({ adoptionLoading: true, adoptionError: '' });
    try {
      const saved = await api.adoptProjectNote(this._adoptionMediaID, input);
      if (!this.adoptionCurrent(epoch)) return;
      this.closeNoteAdoption();
      wx.navigateTo({ url: '/pages/projectnotes/projectnotes?project_id=' + encodeURIComponent(input.project_id) + '&note_id=' + encodeURIComponent(saved.note_id || input.note_id) + '&view=editor' });
    } catch (error) {
      if (!this.adoptionCurrent(epoch)) return;
      this.setData({ adoptionLoading: false, adoptionError: error.message || '插入失败，可重试原位置' });
    }
  },
  async adoptContent(event) {
    const item = this.data.results[Number(event.currentTarget.dataset.index)];
    if (!item || !item.stable_media_id || this.data.adoptingResultID) return;
    this.setData({ adoptingResultID: item.result_id, error: '' });
    try {
      const context = await phase3.workbenchContext();
      if (!this.current()) return;
      const owners = context.owner_choices || (context.owners || []).map(owner_id => ({ owner_id, display_name: owner_id }));
      if (!owners.length) throw new Error('当前账号没有可用的内容归属');
      let owner = owners[0];
      if (owners.length > 1) owner = await new Promise((resolve, reject) => wx.showActionSheet({ itemList: owners.map(row => row.display_name), success: result => resolve(owners[result.tapIndex]), fail: reject }));
      if (!this.current() || !owner) return;
      const saved = await api.adoptContent(item.stable_media_id, { draft_instance_id: api.idempotencyKey(), owner_id: owner.owner_id, title: String(this.data.snapshot.name || this.data.batch.name || 'AI 视频创作').slice(0, 80) });
      if (!this.current()) return;
      this.setData({ adoptingResultID: '' });
      wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(saved.content_id) });
    } catch (error) { if (this.current()) this.setData({ adoptingResultID: '', error: /cancel/i.test(String(error && error.errMsg || '')) ? '已取消选择，视频保持不变' : (error.message || '内容草稿创建失败') }); }
  },
});
