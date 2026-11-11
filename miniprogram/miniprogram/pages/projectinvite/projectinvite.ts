// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');

const initial = () => ({ loading: true, busy: false, loginRequired: false, error: '', preview: null, applied: false, requestStatus: '', projectID: '' });

Page({
  data: initial(),
  onLoad(options) {
    wx.hideShareMenu();
    this._invitationToken = String(options && options.pni || '').trim();
    this._visible = true;
  },
  onShow() {
    wx.hideShareMenu();
    this._visible = true;
    if (!/^[A-Za-z0-9_-]{8,128}$/.test(this._invitationToken || '')) {
      this.setData({ loading: false, error: '该邀请已失效。' }); return;
    }
    // 登录与头像昵称由 login-guard 统一核验（与普通用户一致，用于用户跟踪）。
    const accountToken = String(wx.getStorageSync('token') || '');
    if (!accountToken) { this.setData({ loading: false, loginRequired: true }); return; }
    if (accountToken !== this._accountToken) {
      this._accountToken = accountToken;
      this._epoch = (this._epoch || 0) + 1;
      this._applyAttempt = null;
      this.setData(initial());
    }
    if (!this.data.preview) return this.loadInvitation();
  },
  onHide() {
    this._visible = false;
    this._epoch = (this._epoch || 0) + 1;
    // 重试键跨前后台保留，私有项目数据不保留。
    this.setData(initial());
  },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  current(epoch) { return this._visible && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  clearProtected() { this._applyAttempt = null; this.setData(initial()); },
  async loadInvitation() {
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this.setData({ loading: true, error: '', preview: null, applied: false, requestStatus: '', projectID: '' });
    try {
      const preview = await api.getProjectInvitation(this._invitationToken);
      if (!this.current(epoch)) return;
      const requestStatus = preview.request_status === 'pending' || preview.request_status === 'approved' ? preview.request_status : '';
      this.setData({ loading: false, preview, requestStatus, applied: !!requestStatus, projectID: String(preview.project_id || '') });
      // 邀请即成员：拿到有效邀请后自动确认，无需再点一次或等待设计师审批。
      if (!requestStatus && typeof api.applyProjectInvitation === 'function') void this.applyInvitation();
    } catch (error) {
      if (!this.current(epoch)) return;
      if (Number(error.statusCode) === 401) {
        wx.removeStorageSync('token'); this._accountToken = '';
        this.setData({ loading: false, loginRequired: true, error: '' });
        return;
      }
      this.setData({ loading: false, error: '该邀请已失效。' });
    }
  },
  async applyInvitation() {
    if (this.data.busy) return;
    if (!this._applyAttempt) this._applyAttempt = { key: api.mutationKey('project-join-request') };
    const attempt = this._applyAttempt;
    this.setData({ busy: true, error: '' });
    try {
      const result = await api.applyProjectInvitation(this._invitationToken, attempt.key);
      if (this._applyAttempt === attempt) this._applyAttempt = null;
      if (!this._visible) return;
      this.setData({
        loading: false, busy: false, applied: true,
        requestStatus: result && result.status === 'pending' ? 'pending' : 'approved',
        projectID: String(result && result.project_id || this.data.projectID || ''),
      });
    } catch (error) {
      if (!this._visible) return;
      this.setData({ busy: false, error: error.message || '加入失败，请重试。' });
    }
  },
  enterProject() {
    const projectID = String(this.data.projectID || '');
    if (!projectID) { void this.loadInvitation(); return; }
    wx.navigateTo({ url: '/pages/projectnotes/projectnotes?project_id=' + encodeURIComponent(projectID) });
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  handleBack() { wx.navigateBack(); },
});
