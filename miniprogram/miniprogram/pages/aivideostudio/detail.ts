// @ts-nocheck
'use strict';
const api = require('../../utils/ai-video-studio-api');
const phase3 = require('../../utils/phase3-api');
const statusText = status => ({ queued: '等待处理', processing: '生成中', partial_success: '部分完成', completed: '已完成', failed: '失败', uncertain: '待核对', cancelled: '已取消' }[status] || '状态待确认');
const modeText = mode => ({ text: '文生视频', first_frame: '首帧图生', first_last_frame: '首尾帧', reference: '参考生成' }[mode] || '视频创作');

Page({
  data: { loading: true, error: '', batch: null, snapshot: {}, results: [], statusLabel: '', adoptingResultID: '' },
  onLoad(options) { this.id = options.batch_id; this._token = String(wx.getStorageSync('token') || ''); this.load(); },
  onHide() { this.abortDownload(); },
  onUnload() { this._unloaded = true; this._epoch = (this._epoch || 0) + 1; this.abortDownload(); },
  current(epoch) { return !this._unloaded && this._token === String(wx.getStorageSync('token') || '') && (epoch === undefined || epoch === this._epoch); },
  abortDownload() { if (this._downloadTask && this._downloadTask.abort) this._downloadTask.abort(); this._downloadTask = null; },
  onPullDownRefresh() { this.load().finally(() => wx.stopPullDownRefresh()); },
  async load() {
    const epoch = this._epoch = (this._epoch || 0) + 1;
    if (!this.id) { this.setData({ loading: false, error: '任务标识无效' }); return; }
    this.setData({ loading: true, error: '' });
    try {
      const batch = await api.getBatch(this.id);
      if (!this.current(epoch)) return;
      const snapshot = Object.assign({}, batch.snapshot || {}, { mode_label: modeText((batch.snapshot || {}).mode) });
      const results = (batch.results || []).map(item => Object.assign({}, item, {
        status_label: statusText(item.status), playback_url: '',
        retained: !!item.stable_media_id || ['retained', 'ready', 'completed'].includes(item.retention_status),
      }));
      this.setData({ loading: false, batch: Object.assign({}, batch, { charge: batch.charge || {} }), snapshot, results, statusLabel: statusText(batch.status) });
    } catch (error) { if (this.current(epoch)) this.setData({ loading: false, error: error.message || '任务加载失败' }); }
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
