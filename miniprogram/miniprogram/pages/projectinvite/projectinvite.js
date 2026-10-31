'use strict';
const api = require('../../utils/phase3-api');

const initial = () => ({ loading: true, busy: false, loginRequired: false, error: '', preview: null, applied: false, requestStatus: '' });

Page({
  data: initial(),
  onLoad(options) {
    wx.hideShareMenu();
    this._invitationToken = String(options && options.pni || '').trim();
    this._visible = true;
  },
  onShow() {
    wx.hideShareMenu();
    const accountToken = String(wx.getStorageSync('token') || '');
    if (accountToken !== this._accountToken) {
      this._accountToken = accountToken;
      this._epoch = (this._epoch || 0) + 1;
      this._applyAttempt = null;
      this.setData(initial());
    }
    this._visible = true;
    if (!accountToken) { this.setData({ loading: false, loginRequired: true }); return; }
    if (!/^[A-Za-z0-9_-]{8,128}$/.test(this._invitationToken || '')) {
      this.setData({ loading: false, error: '该邀请已失效。' }); return;
    }
    if (!this.data.preview) return this.loadInvitation();
  },
  onHide() {
    this._visible = false;
    this._epoch = (this._epoch || 0) + 1;
    // The retry key survives foreground changes, but private project data does not.
    this.setData(initial());
  },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  current(epoch) { return this._visible && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  clearProtected() { this._applyAttempt = null; this.setData(initial()); },
  async loadInvitation() {
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this.setData({ loading: true, loginRequired: false, error: '', preview: null, applied: false, requestStatus: '' });
    try {
      const preview = await api.getProjectInvitation(this._invitationToken);
      if (!this.current(epoch)) return;
      const requestStatus = preview.request_status === 'pending' || preview.request_status === 'approved' ? preview.request_status : '';
      this.setData({ loading: false, preview, requestStatus, applied: !!requestStatus });
    } catch (error) {
      if (!this.current(epoch)) return;
      this.setData({ loading: false, loginRequired: Number(error.statusCode) === 401, error: Number(error.statusCode) === 401 ? '' : '该邀请已失效。' });
    }
  },
  async applyInvitation() {
    if (this.data.busy || this.data.applied || !this.data.preview) return;
    const epoch = this._epoch;
    if (!this._applyAttempt) this._applyAttempt = { key: api.mutationKey('project-join-request') };
    const attempt = this._applyAttempt;
    this.setData({ busy: true, error: '' });
    try {
      await api.applyProjectInvitation(this._invitationToken, attempt.key);
      if (!this.current(epoch)) return;
      if (this._applyAttempt === attempt) this._applyAttempt = null;
      this.setData({ busy: false, applied: true, requestStatus: 'pending' });
    } catch (error) {
      if (this.current(epoch)) this.setData({ busy: false, error: error.message || '申请提交失败，请重试。' });
    }
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  handleBack() { wx.navigateBack(); },
});
