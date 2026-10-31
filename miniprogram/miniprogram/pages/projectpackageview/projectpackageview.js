'use strict';
const api = require('../../utils/project-packages-api');
const { renderArticleDocument } = require('../../utils/article-document');

const labels = { very_satisfied: '非常满意', needs_adjustment: '基本满意，需要调整', unsatisfied: '不太满意' };
const eventID = () => 'package-read-' + Date.now() + '-' + Math.random().toString(36).slice(2, 10);

Page({
  data: {
    loading: true, busy: false, loginRequired: false, error: '', notice: '', pageView: 'directory',
    directory: null, versionView: null, media: [], articleBlocks: [], feedbackText: '', satisfaction: '',
    readAcknowledged: false, confirmed: false,
  },
  onLoad(options) {
    wx.hideShareMenu();
    this._packageToken = String(options && options.pps || '').trim();
    this._visible = true;
    this._receiptIDs = new Map(); this._acknowledgedVersions = new Set(); this._confirmAttempts = new Map(); this._feedbackAttempt = null;
  },
  onShow() {
    const token = String(wx.getStorageSync('token') || '');
    if (!token) { this.clearProtected(); this.setData({ loading: false, loginRequired: true }); return; }
    const accountChanged = token !== this._accountToken;
    if (accountChanged) {
      this._accountToken = token; this._epoch = (this._epoch || 0) + 1;
      this._receiptIDs = new Map(); this._acknowledgedVersions = new Set(); this._confirmAttempts = new Map(); this._feedbackAttempt = null;
    }
    this._visible = true;
    this.clearProtected(!accountChanged);
    return this.loadDirectory();
  },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(true); },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  clearProtected(preserveInput) {
    this.setData({
      directory: null, versionView: null, media: [], articleBlocks: [], pageView: 'directory',
      feedbackText: preserveInput ? this.data.feedbackText : '', satisfaction: preserveInput ? this.data.satisfaction : '',
      readAcknowledged: false, confirmed: false, notice: '', error: '', loginRequired: false, loading: true, busy: false,
    });
  },
  current(epoch) { return this._visible && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  async loadDirectory() {
    if (!/^[A-Za-z0-9_-]{8,256}$/.test(this._packageToken)) { this.setData({ loading: false, error: '该分享资料暂不可访问。' }); return; }
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this._visible = true; this.setData({ loading: true, loginRequired: false, error: '' });
    try {
      const directory = await api.getSharedProjectPackage(this._packageToken);
      if (!this.current(epoch)) return;
      this.setData({ directory, loading: false, pageView: 'directory' });
    } catch (error) {
      if (this.current(epoch)) this.setData({ loading: false, loginRequired: Number(error.statusCode) === 401, error: Number(error.statusCode) === 401 ? '' : '该分享资料暂不可访问。' });
    }
  },
  async openVersion(event) {
    const versionID = String(event.currentTarget.dataset.id || '');
    if (!versionID || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const versionView = await api.getSharedProjectPackageVersion(this._packageToken, versionID);
      if (!this.current(epoch)) return;
      const assets = [];
      if (versionView.version && versionView.version.cover) assets.push(versionView.version.cover);
      for (const asset of versionView.version && versionView.version.media || []) if (!assets.some(item => item.media_id === asset.media_id)) assets.push(asset);
      const media = await Promise.all(assets.map(async asset => Object.assign({}, asset, { local_path: await api.downloadSharedProjectPackageMedia(this._packageToken, versionID, asset.media_id) })));
      if (!this.current(epoch)) return;
      const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : { windowWidth: 375 };
      const articleBlocks = versionView.version.body_format === 'article_v2' ? renderArticleDocument(versionView.version.body, media, Math.max(240, windowInfo.windowWidth - 40), 'package-version') : [];
      const versionKey = String(versionView.version && versionView.version.version_id || versionID);
      this.setData({ versionView, media, articleBlocks, pageView: 'version', busy: false, confirmed: !!versionView.confirmation, readAcknowledged: this._acknowledgedVersions.has(versionKey) });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '方案加载失败' }); }
  },
  handleBack() {
    if (this.data.pageView === 'version') {
      this.setData({ pageView: 'directory', versionView: null, media: [], articleBlocks: [], feedbackText: '', satisfaction: '', notice: '' });
      return;
    }
    wx.navigateBack();
  },
  setFeedbackText(event) { this.setData({ feedbackText: String(event.detail && event.detail.value || '') }); },
  setSatisfaction(event) { const value = String(event.detail && event.detail.value || ''); if (labels[value]) this.setData({ satisfaction: value }); },
  async submitFeedback() {
    const text = this.data.feedbackText.trim(), version = this.data.versionView && this.data.versionView.version;
    if (!text || !version || this.data.busy) return;
    const epoch = this._epoch;
    const satisfaction = this.data.satisfaction;
    const signature = [version.version_id, text, satisfaction].join(':');
    if (!this._feedbackAttempt || this._feedbackAttempt.signature !== signature) this._feedbackAttempt = { signature, key: api.mutationKey('project-package-feedback'), text, satisfaction };
    const attempt = this._feedbackAttempt;
    this.setData({ busy: true, notice: '' });
    try {
      const feedback = await api.addSharedProjectPackageFeedback(this._packageToken, version.version_id, attempt.text, attempt.satisfaction, attempt.key);
      if (!this.current(epoch)) return;
      const versionView = Object.assign({}, this.data.versionView, { feedbacks: [...(this.data.versionView.feedbacks || []), feedback] });
      if (this._feedbackAttempt === attempt) this._feedbackAttempt = null;
      const unchanged = this.data.feedbackText.trim() === attempt.text && this.data.satisfaction === attempt.satisfaction;
      this.setData({ versionView, feedbackText: unchanged ? '' : this.data.feedbackText, satisfaction: unchanged ? '' : this.data.satisfaction, busy: false, notice: '反馈已提交。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '反馈提交失败' }); }
  },
  async confirmVersion() {
    const version = this.data.versionView && this.data.versionView.version;
    if (!version || !this.data.versionView.can_confirm || this.data.confirmed || this.data.busy) return;
    const epoch = this._epoch;
    const versionID = String(version.version_id || '');
    if (!this._confirmAttempts.has(versionID)) this._confirmAttempts.set(versionID, api.mutationKey('project-package-confirm'));
    const key = this._confirmAttempts.get(versionID);
    this.setData({ busy: true, notice: '' });
    try {
      const confirmation = await api.confirmSharedProjectPackageVersion(this._packageToken, versionID, key);
      if (!this.current(epoch)) return;
      this._confirmAttempts.delete(versionID);
      this.setData({ versionView: Object.assign({}, this.data.versionView, { confirmation }), confirmed: true, busy: false, notice: '已确认当前固定版本。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '版本确认失败' }); }
  },
  async acknowledgeRead() {
    const version = this.data.versionView && this.data.versionView.version;
    if (!version || this.data.readAcknowledged || this.data.busy) return;
    const epoch = this._epoch;
    const versionID = String(version.version_id || '');
    if (!this._receiptIDs.has(versionID)) this._receiptIDs.set(versionID, eventID());
    this.setData({ busy: true, notice: '' });
    try {
      await api.acknowledgeSharedProjectPackage(this._packageToken, versionID, this._receiptIDs.get(versionID));
      if (!this.current(epoch)) return;
      this._acknowledgedVersions.add(versionID);
      this.setData({ readAcknowledged: true, busy: false, notice: '已发送阅读回执。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '回执发送失败' }); }
  },
  openImage(event) {
    const asset = this.data.media.find(item => item.media_id === String(event.currentTarget.dataset.id || '') && item.kind === 'image');
    if (asset) wx.previewImage({ current: asset.local_path, urls: [asset.local_path], showmenu: false });
  },
  async saveImage(event) {
    const asset = this.data.media.find(item => item.media_id === String(event.currentTarget.dataset.id || '') && item.kind === 'image');
    if (!asset || this.data.busy) return;
    this.setData({ busy: true, notice: '' });
    try { await new Promise((resolve, reject) => wx.saveImageToPhotosAlbum({ filePath: asset.local_path, success: resolve, fail: reject })); this.setData({ busy: false, notice: '原图已保存。' }); }
    catch (_) { this.setData({ busy: false, notice: '保存失败，请检查相册权限。' }); }
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  retry() { return this.data.pageView === 'version' && this.data.versionView ? null : this.loadDirectory(); },
});
