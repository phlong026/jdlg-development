'use strict';

const api = require('../../utils/ai-video-studio-api');
const rules = require('../../utils/ai-video-studio');

const statusText = status => ({ queued: '等待润色', submitting: '正在润色', uncertain: '结果待核对', completed: '润色完成', failed: '润色失败', cancelled: '已取消' }[status] || '状态待确认');
const terminal = status => ['uncertain', 'completed', 'failed', 'cancelled'].includes(String(status || ''));

Page({
  data: { loading: true, error: '', task: null, statusLabel: '', originalOpen: false, result: '', resultLength: 0, resultEdited: false, canAdopt: false, cancelling: false },
  onLoad(options) {
    this._sessionEpoch = (this._sessionEpoch || 0) + 1;
    this._token = String(wx.getStorageSync('token') || '');
    this._unloaded = false; this._visible = true; this._epoch = (this._epoch || 0) + 1;
    this.id = String((options || {}).task_id || '');
    this.setData({ loading: true, error: '', task: null, statusLabel: '', result: '', resultLength: 0, resultEdited: false, canAdopt: false, cancelling: false });
    this.load();
  },
  onShow() {
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._token) { this._sessionEpoch = (this._sessionEpoch || 0) + 1; this._epoch += 1; this._token = token; this.stopPolling(); this.setData({ cancelling: false, task: null, result: '', resultLength: 0, canAdopt: false, error: token ? '' : '请先登录' }); }
    if (this._visible === false) { this._visible = true; this.setData({ cancelling: false }); if (token) this.load(true); }
  },
  onHide() { this._visible = false; this._epoch += 1; this.stopPolling(); },
  onUnload() { this._unloaded = true; this.onHide(); },
  current(epoch, token) { const owner = token === undefined ? this._token : token; return !this._unloaded && this._visible !== false && owner === this._token && owner === String(wx.getStorageSync('token') || '') && (epoch === undefined || epoch === this._epoch); },
  stopPolling() { if (this._pollTimer) clearTimeout(this._pollTimer); this._pollTimer = null; },
  schedulePoll() {
    this.stopPolling();
    const task = this.data.task || {};
    if (!this.current() || terminal(task.status)) return;
    this._pollTimer = setTimeout(() => { this._pollTimer = null; this.load(true); }, Math.min(20000, 2500 * Math.pow(2, this._pollFailures || 0)));
    if (this._pollTimer && this._pollTimer.unref) this._pollTimer.unref();
  },
  async load(silent) {
    if (!this.id || !this.current()) { if (!this.id) this.setData({ loading: false, error: '润色任务标识无效' }); return; }
    this.stopPolling();
    const epoch = ++this._epoch, token = this._token;
    this.setData(silent ? { error: '' } : { loading: true, error: '' });
    try {
      const task = await api.getPolishTask(this.id);
      if (!this.current(epoch, token)) return;
      this._pollFailures = 0;
      const optimized = String(task.optimized_prompt || '');
      const result = this.data.resultEdited ? this.data.result : optimized;
      const resultLength = Array.from(result).length;
      this.setData({ loading: false, task, statusLabel: statusText(task.status), result, resultLength, canAdopt: task.status === 'completed' && !!result.trim() && resultLength <= 7000 });
    } catch (error) {
      if (this.current(epoch, token)) { this._pollFailures = Math.min(3, (this._pollFailures || 0) + 1); this.setData({ loading: false, error: error.message || '润色任务读取失败' }); }
    }
    if (this.current(epoch, token)) this.schedulePoll();
  },
  toggleOriginal() { this.setData({ originalOpen: !this.data.originalOpen }); },
  inputResult(event) {
    const result = String(event.detail.value || ''), resultLength = Array.from(result).length, tooLong = resultLength > 7000;
    this.setData({ result, resultLength, resultEdited: true, canAdopt: !tooLong && (this.data.task || {}).status === 'completed' && !!result.trim(), error: tooLong ? '润色结果不能超过7000字' : '' });
  },
  retainOriginal() { wx.navigateBack(); },
  adopt() {
    const task = this.data.task || {}, prompt = String(this.data.result || '').trim();
    if (!this.data.canAdopt || !prompt) return;
    const snapshot = task.input_snapshot || {}, references = snapshot.references || [];
    if (Array.from(prompt).length > 7000 || (snapshot.mode === 'reference' && Array.from(rules.providerVisibleReferencePrompt(prompt, references)).length > 7000)) {
      this.setData({ error: snapshot.mode === 'reference' ? '润色结果与素材用途合计不能超过7000字' : '润色结果不能超过7000字' }); return;
    }
    const channel = this.getOpenerEventChannel && this.getOpenerEventChannel();
    if (!channel || typeof channel.emit !== 'function') { this.setData({ error: '请从创作页打开润色结果后采用' }); return; }
    channel.emit('adopted', { task_id: task.task_id || task.id, draft_id: task.draft_id, draft_revision: task.draft_revision, input_fingerprint: task.input_fingerprint || task.input_hash || '', prompt });
    wx.navigateBack();
  },
  async cancel() {
    if (this.data.cancelling || (this.data.task || {}).status !== 'queued') return;
    const epoch = this._epoch, token = this._token, sessionEpoch = this._sessionEpoch;
    this.setData({ cancelling: true, error: '' });
    const ownsAttempt = () => sessionEpoch === this._sessionEpoch && this.current(epoch, token);
    try {
      await api.cancelPolishTask(this.id);
      if (ownsAttempt()) { this.setData({ cancelling: false }); await this.load(true); }
    }
    catch (error) { if (ownsAttempt()) this.setData({ error: error.message || '取消失败，任务可能已经开始' }); }
    finally { if (ownsAttempt()) this.setData({ cancelling: false }); }
  },
});
