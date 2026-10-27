'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');

const GENERIC_ERROR = '该私密方案暂不可访问，请确认已使用受邀账号登录。';
const eventID = () => 'project-note-open-' + Date.now() + '-' + Math.random().toString(36).slice(2, 10);
const feedbackRows = view => {
  const replies = new Map();
  for (const resolution of view && view.resolutions || []) replies.set(resolution.feedback_id, String(resolution.reply || ''));
  return (view && view.feedbacks || []).map(feedback => Object.assign({}, feedback, { resolution_reply: replies.get(feedback.feedback_id) || '' }));
};

Page({
  data: {
    loading: true, busy: false, loginRequired: false, error: '', notice: '',
    view: null, articleBlocks: [], media: [], attachments: [], feedbackRows: [], feedbackText: '', canConfirm: false, confirmed: false, shareReady: false,
  },
  onLoad(options) {
    wx.hideShareMenu();
    this._shareToken = String(options && options.pns || '').trim();
    this._openEventID = eventID();
    this._visible = true;
  },
  onShow() {
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._accountToken) {
      this._accountToken = token; this._epoch = (this._epoch || 0) + 1;
      this._openEventID = eventID(); this._openSent = false; this._openPending = null; this._feedbackAttempt = null;
      this.clearProtected();
    }
    this._visible = true;
    if (!token) { wx.hideShareMenu(); this.setData({ loading: false, loginRequired: true, error: '' }); return; }
    this.loadShared();
  },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; wx.hideShareMenu(); },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  clearProtected() {
    this.setData({ view: null, articleBlocks: [], media: [], attachments: [], feedbackRows: [], canConfirm: false, confirmed: false, shareReady: false, feedbackText: '', notice: '', busy: false });
  },
  async loadShared() {
    const accountToken = String(wx.getStorageSync('token') || '');
    if (!accountToken) { this.clearProtected(); this.setData({ loading: false, loginRequired: true, error: '' }); return; }
    if (!/^[A-Za-z0-9_-]{8,256}$/.test(this._shareToken || '')) { this.clearProtected(); this.setData({ loading: false, loginRequired: false, error: GENERIC_ERROR }); return; }
    this._accountToken = accountToken;
    const epoch = this._epoch = (this._epoch || 0) + 1;
    wx.hideShareMenu(); this.clearProtected(); this.setData({ loading: true, loginRequired: false, error: '' });
    try {
      const view = await api.getSharedProjectNote(this._shareToken);
      if (!this.current(epoch)) return;
      const assets = [];
      if (view.version && view.version.cover) assets.push(view.version.cover);
      for (const asset of view.version && view.version.media || []) if (!assets.some(item => item.media_id === asset.media_id)) assets.push(asset);
      const media = await Promise.all(assets.map(async asset => Object.assign({}, asset, { local_path: await api.downloadSharedProjectNoteMedia(this._shareToken, asset.media_id) })));
      if (!this.current(epoch)) return;
      const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
      const width = Math.max(240, windowInfo.windowWidth - 40);
      const articleBlocks = view.version.body_format === 'article_v2' ? renderArticleDocument(view.version.body, media, width, 'private-note') : [];
      const attachments = media.filter(asset => asset.kind !== 'image' && asset.kind !== 'video');
      const canConfirm = view.can_confirm === true;
      const confirmed = !!view.confirmation;
      this.setData({ view, media, attachments, articleBlocks, feedbackRows: feedbackRows(view), canConfirm, confirmed, loading: false, shareReady: !!view.share && !!this._shareToken }, () => {
        if (!this.current(epoch)) return;
        if (wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage'] });
        this.reportOpen();
      });
    } catch (_) {
      if (!this.current(epoch)) return;
      wx.hideShareMenu(); this.clearProtected(); this.setData({ loading: false, loginRequired: false, error: GENERIC_ERROR });
    }
  },
  async reportOpen() {
    if (this._openSent || !this.data.view || !this._accountToken || this._accountToken !== String(wx.getStorageSync('token') || '')) return;
    const accountToken = this._accountToken, openEventID = this._openEventID;
    if (this._openPending && this._openPending.accountToken === accountToken && this._openPending.eventID === openEventID) return this._openPending.promise;
    const epoch = this._epoch, attempt = { accountToken, eventID: openEventID, promise: null };
    attempt.promise = (async () => {
      for (let retry = 0; retry < 2; retry += 1) {
        try {
          await api.openSharedProjectNote(this._shareToken, openEventID);
          if (this.current(epoch) && this._accountToken === accountToken && this._openEventID === openEventID) this._openSent = true;
          return;
        } catch (_) { if (!this.current(epoch)) return; }
      }
    })();
    this._openPending = attempt;
    try { await attempt.promise; } finally { if (this._openPending === attempt) this._openPending = null; }
  },
  setFeedbackText(event) {
    const feedbackText = String(event.detail && event.detail.value || '');
    if (this._feedbackAttempt && this._feedbackAttempt.text !== feedbackText.trim()) this._feedbackAttempt = null;
    this.setData({ feedbackText });
  },
  async submitFeedback() {
    const text = this.data.feedbackText.trim();
    if (!text || this.data.busy || !this.data.view) return;
    const shareID = String(this.data.view.share && this.data.view.share.share_id || '');
    const versionID = String(this.data.view.version && this.data.view.version.version_id || '');
    const signature = [shareID, versionID, text].join('\n');
    if (!this._feedbackAttempt || this._feedbackAttempt.signature !== signature) this._feedbackAttempt = { signature, text, key: api.mutationKey('project-note-feedback') };
    const attempt = this._feedbackAttempt;
    const epoch = this._epoch; this.setData({ busy: true, notice: '', error: '' });
    try {
      const feedback = await api.addSharedProjectNoteFeedback(this._shareToken, text, attempt.key);
      if (!this.current(epoch)) return;
      const view = Object.assign({}, this.data.view, { feedbacks: [...(this.data.view.feedbacks || []), feedback] });
      const sameAttempt = this._feedbackAttempt === attempt;
      if (sameAttempt) this._feedbackAttempt = null;
      this.setData(Object.assign({ view, feedbackRows: feedbackRows(view), busy: false, notice: '反馈已提交给设计师。' }, sameAttempt ? { feedbackText: '' } : {}));
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '反馈提交失败' }); }
  },
  async confirmVersion() {
    if (!this.data.canConfirm || this.data.confirmed || this.data.busy || !this.data.view) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const confirmation = await api.confirmSharedProjectNote(this._shareToken, api.mutationKey('project-note-confirm'));
      if (!this.current(epoch)) return;
      this.setData({ view: Object.assign({}, this.data.view, { confirmation }), confirmed: true, busy: false, notice: '已确认当前固定版本。设计师后续新版本需另行确认。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '版本确认失败' }); }
  },
  openImage(event) {
    const mediaID = String(event.currentTarget.dataset.id || '');
    const asset = this.data.media.find(item => item.media_id === mediaID && item.kind === 'image');
    if (asset && asset.local_path) wx.previewImage({ current: asset.local_path, urls: [asset.local_path], showmenu: false });
  },
  openAttachment(event) {
    const mediaID = String(event.currentTarget.dataset.id || '');
    const asset = this.data.media.find(item => item.media_id === mediaID);
    if (asset && asset.local_path && asset.kind !== 'image' && asset.kind !== 'video') wx.openDocument({ filePath: asset.local_path, showMenu: false, fail: () => wx.showToast({ title: '附件暂无法打开', icon: 'none' }) });
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  retry() { this.loadShared(); },
  onShareAppMessage() {
    if (!this.data.shareReady || !this.data.view || !this._shareToken) return {};
    return { title: this.data.view.version.title || '项目方案', path: '/pages/projectnoteview/projectnoteview?pns=' + encodeURIComponent(this._shareToken) };
  },
});
