// @ts-nocheck
'use strict';
const api = require('../../utils/ai-video-studio-api');
const modeLabel = mode => ({ text: '文生视频', first_frame: '首帧图生', first_last_frame: '首尾帧', reference: '参考生成' }[mode] || '视频创作');
const statusLabel = status => ({ queued: '等待处理', processing: '生成中', partial_success: '部分完成', completed: '已完成', failed: '生成失败', uncertain: '待核对', cancelled: '已取消' }[status] || '状态待确认');
Page({
  data: { __profileAccessMounted: true, loading: true, error: '', capabilities: {}, drafts: [], batches: [] },
  onLoad() { this._token=String(wx.getStorageSync('token')||'');this.load(); },
  onUnload(){this._unloaded=true;this._epoch=(this._epoch||0)+1},
  current(epoch){return!this._unloaded&&epoch===this._epoch&&this._token===String(wx.getStorageSync('token')||'')},
  onPullDownRefresh() { this.load().finally(() => wx.stopPullDownRefresh()); },
  async load() {
    const epoch=this._epoch=(this._epoch||0)+1;
    this.setData({ loading: true, error: '' });
    const values = await Promise.allSettled([api.capabilities(), api.listDrafts({ page: 1, page_size: 3, status: 'draft' }), api.listBatches({ page: 1, page_size: 3 })]);
    if(!this.current(epoch))return;
    if (values.every(item => item.status === 'rejected')) { this.setData({ loading: false, error: values[0].reason.message || '创作空间加载失败' }); return; }
    const capabilities = values[0].status === 'fulfilled' ? values[0].value : { enabled: false, history_readable: true, disabled_reason: '能力配置暂时无法读取' };
    const drafts = values[1].status === 'fulfilled' ? (values[1].value.items || []).map(item => Object.assign({}, item, { mode_label: modeLabel(item.mode) })) : [];
    const batches = values[2].status === 'fulfilled' ? (values[2].value.items || []).map(item => Object.assign({}, item, { mode_label: modeLabel(item.mode), status_label: statusLabel(item.status), created_at_label: String(item.created_at || '').replace('T', ' ').slice(0, 16) })) : [];
    this.setData({ loading: false, capabilities, drafts, batches, error: '' });
  },
  newCreation(event) { wx.navigateTo({ url: '/pages/aivideostudio/generate?kind=' + encodeURIComponent(event.currentTarget.dataset.kind || 'standard') }); },
  navigate(event) { wx.navigateTo({ url: event.currentTarget.dataset.url }); },
  openDraft(event) { wx.navigateTo({ url: '/pages/aivideostudio/generate?draft_id=' + encodeURIComponent(event.currentTarget.dataset.id) }); },
  openBatch(event) { wx.navigateTo({ url: '/pages/aivideostudio/detail?batch_id=' + encodeURIComponent(event.currentTarget.dataset.id) }); },
});
