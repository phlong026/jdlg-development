'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');
const { fitNoteMedia, fitNoteBlocks, groupNoteImages, noteVideoMetadata } = require('../../utils/project-note-media');

const GENERIC_ERROR = '该私密方案暂不可访问，请确认已使用受邀账号登录。';
const satisfactionLabels = { very_satisfied: '非常满意', needs_adjustment: '基本满意，需要调整', unsatisfied: '不太满意' };
const eventID = () => 'project-note-open-' + Date.now() + '-' + Math.random().toString(36).slice(2, 10);
const feedbackRows = view => {
  const replies = new Map();
  for (const resolution of view && view.resolutions || []) replies.set(resolution.feedback_id, String(resolution.reply || ''));
  return (view && view.feedbacks || []).map(feedback => Object.assign({}, feedback, { resolution_reply: feedback.reply || replies.get(feedback.feedback_id) || '', satisfaction_label: satisfactionLabels[feedback.satisfaction] || '' }));
};

Page({
  onNoteGalleryChange(event) {
    this.setData({ galleryPositions: Object.assign({}, this.data.galleryPositions, { [event.currentTarget.dataset.key]: Number(event.detail.current) || 0 }) });
  },
  onResize(event) {
    const windowInfo = event.size || {}, media = fitNoteMedia(this.data.media, windowInfo);
    this.setData({ media, mediaGroups: groupNoteImages(media, media, windowInfo), articleBlocks: fitNoteBlocks(this.data.articleBlocks, media, windowInfo) });
  },
  onNoteVideoMetadata(event) {
    const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
    const media = noteVideoMetadata(this.data.media, event, windowInfo);
    this.setData({ media, mediaGroups: groupNoteImages(media, media, windowInfo), articleBlocks: fitNoteBlocks(this.data.articleBlocks, media, windowInfo) });
  },
  data: {
    loading: true, busy: false, loginRequired: false, error: '', notice: '',
    activityTypes: [{ key: 'feedbacks', label: '反馈意见' }, { key: 'resolutions', label: '处理记录' }, { key: 'confirmations', label: '确认记录' }], activityType: 'feedbacks', activityIndex: 0, activityItems: [], activityPage: 0, activityCursors: [''], activityNextCursor: '', activityLoaded: false, activityLoading: false,
    satisfaction: '', readAcknowledged: false, acknowledgingRead: false,
    view: null, articleBlocks: [], media: [], attachments: [], feedbackRows: [], feedbackText: '', canConfirm: false, confirmed: false, shareReady: false,
    mediaGroups: [], galleryPositions: {},
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
    return this.data.view ? this.revalidateShared() : this.loadShared();
  },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; wx.hideShareMenu(); },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  clearProtected() {
    this._accessVerified = false;
    this.setData({ activityType: 'feedbacks', activityIndex: 0, activityItems: [], activityPage: 0, activityCursors: [''], activityNextCursor: '', activityLoaded: false, activityLoading: false, view: null, articleBlocks: [], media: [], mediaGroups: [], galleryPositions: {}, attachments: [], feedbackRows: [], canConfirm: false, confirmed: false, shareReady: false, feedbackText: '', satisfaction: '', readAcknowledged: !!this._openSent, acknowledgingRead: false, notice: '', busy: false });
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
      let media = await Promise.all(assets.map(async asset => Object.assign({}, asset, { local_path: await api.downloadSharedProjectNoteMedia(this._shareToken, asset.media_id) })));
      if (!this.current(epoch)) return;
      const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
      media = fitNoteMedia(media, windowInfo);
      const width = windowInfo.windowWidth * 636 / 750;
      const articleBlocks = view.version.body_format === 'article_v2' ? fitNoteBlocks(renderArticleDocument(view.version.body, media, width, 'private-note'), media, windowInfo) : [];
      const attachments = media.filter(asset => asset.kind !== 'image' && asset.kind !== 'video');
      const canConfirm = view.can_confirm === true;
      const confirmed = !!view.confirmation;
      this._accessVerified = true;
      this.setData({ view, media, mediaGroups: groupNoteImages(media, media, windowInfo), galleryPositions: {}, attachments, articleBlocks, feedbackRows: feedbackRows(view), canConfirm, confirmed, loading: false, shareReady: !!view.share && !!this._shareToken }, () => {
        if (!this.current(epoch)) return;
        if (wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage'] });

      });
    } catch (_) {
      if (!this.current(epoch)) return;
      wx.hideShareMenu(); this.clearProtected(); this.setData({ loading: false, loginRequired: false, error: GENERIC_ERROR });
    }
  },
  async revalidateShared() {
    const cached = this.data.view;
    if (!cached) return this.loadShared();
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this._accessVerified = false;
    wx.hideShareMenu(); this.setData({ loading: true, loginRequired: false, error: '', notice: '' });
    try {
      const view = await api.getSharedProjectNote(this._shareToken);
      if (!this.current(epoch)) return;
      const sameShare = String(view.share && view.share.share_id || '') === String(cached.share && cached.share.share_id || '');
      const sameVersion = String(view.version && view.version.version_id || '') === String(cached.version && cached.version.version_id || '');
      if (!sameShare || !sameVersion) throw Object.assign(new Error('分享已变更'), { statusCode: 403 });
      this._accessVerified = true;
      this.setData({ view, feedbackRows: feedbackRows(view), canConfirm: view.can_confirm === true, confirmed: !!view.confirmation, loading: false, shareReady: !!view.share && !!this._shareToken }, () => {
        if (this.current(epoch) && wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage'] });
      });
    } catch (error) {
      if (!this.current(epoch)) return;
      const status = Number(error.statusCode);
      if ([401, 403, 404].includes(status)) {
        wx.hideShareMenu(); this.clearProtected(); this.setData({ loading: false, loginRequired: status === 401, error: status === 401 ? '' : GENERIC_ERROR });
        return;
      }
      this.setData({ loading: false, shareReady: false, canConfirm: false, error: '权限重验失败，已保留待提交内容，请重试。' });
    }
  },
  async setActivityType(event) {
    const index = Number(event.detail.value), type = this.data.activityTypes[index]; if (!type) return;
    this._activityRequest = (this._activityRequest || 0) + 1;
    this.setData({ activityType: type.key, activityIndex: index, activityItems: [], activityPage: 0, activityCursors: [''], activityNextCursor: '', activityLoaded: false, activityLoading: false });
    await this.loadActivityHistory();
  },
  async loadActivityHistory(event) {
    if (!this.data.view || this.data.activityLoading || this._accessVerified === false) return;
    const direction = event && event.currentTarget && event.currentTarget.dataset.direction;
    let page = this.data.activityPage; const cursors = this.data.activityCursors.slice();
    if (direction === 'next') { if (!this.data.activityNextCursor) return; page++; cursors[page] = this.data.activityNextCursor; }
    if (direction === 'previous') { if (!page) return; page--; }
    const epoch = this._epoch, type = this.data.activityType, request = this._activityRequest = (this._activityRequest || 0) + 1;
    this.setData({ activityLoading: true, notice: '' });
    try {
      const result = await api.getSharedProjectNoteHistory(this._shareToken, type, { limit: 30, cursor: cursors[page] || '' });
      if (!this.current(epoch) || request !== this._activityRequest) return;
      const items = (result.items || []).map((item, index) => Object.assign({}, item, {
        key: item.feedback_id || item.resolution_id || item.confirmation_id || String(index),
        displayText: item.text || item.reply || '已确认此方案版本', satisfaction_label: satisfactionLabels[item.satisfaction] || '',
        displayTime: item.resolved_at || item.confirmed_at || item.created_at || '',
      }));
      this.setData({ activityItems: items, activityPage: page, activityCursors: cursors, activityNextCursor: result.next_cursor || '', activityLoaded: true, activityLoading: false });
    } catch (error) { if (this.current(epoch) && request === this._activityRequest) this.setData({ activityLoading: false, notice: error.message || '历史记录加载失败，可重试' }); }
  },
  async acknowledgeRead() {
    if (!this.data.view || this.data.acknowledgingRead || this._openSent || this._accessVerified === false) return;
    const epoch = this._epoch;
    this.setData({ acknowledgingRead: true, notice: '' });
    await this.reportOpen();
    if (this.current(epoch)) this.setData({ acknowledgingRead: false, readAcknowledged: !!this._openSent, notice: this._openSent ? '已将当前账号的阅读回执发送给设计师。' : '回执发送失败，可重试；不影响继续查看方案。' });
  },
  async reportOpen() {
    if (this._openSent || !this.data.view || this._accessVerified === false || !this._accountToken || this._accountToken !== String(wx.getStorageSync('token') || '')) return;
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
  setSatisfaction(event) {
    const satisfaction = String(event.detail && event.detail.value || '');
    if (!Object.prototype.hasOwnProperty.call(satisfactionLabels, satisfaction)) return;
    if (this.data.satisfaction !== satisfaction) this._feedbackAttempt = null;
    this.setData({ satisfaction });
  },
  async submitFeedback() {
    const text = this.data.feedbackText.trim();
    if (!text || this.data.busy || !this.data.view || this._accessVerified === false) return;
    const shareID = String(this.data.view.share && this.data.view.share.share_id || '');
    const versionID = String(this.data.view.version && this.data.view.version.version_id || '');
    const signature = [shareID, versionID, text, this.data.satisfaction].join('\n');
    if (!this._feedbackAttempt || this._feedbackAttempt.signature !== signature) this._feedbackAttempt = { signature, text, key: api.mutationKey('project-note-feedback') };
    const attempt = this._feedbackAttempt;
    const epoch = this._epoch; this.setData({ busy: true, notice: '', error: '' });
    try {
      const feedback = await api.addSharedProjectNoteFeedback(this._shareToken, text, attempt.key, this.data.satisfaction);
      if (!this.current(epoch)) return;
      const view = Object.assign({}, this.data.view, { feedbacks: [...(this.data.view.feedbacks || []), feedback] });
      const sameAttempt = this._feedbackAttempt === attempt;
      if (sameAttempt) this._feedbackAttempt = null;
      this.setData(Object.assign({ view, feedbackRows: feedbackRows(view), busy: false, notice: '反馈已提交给设计师。' }, sameAttempt ? { feedbackText: '', satisfaction: '' } : {}));
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '反馈提交失败' }); }
  },
  async confirmVersion() {
    if (!this.data.canConfirm || this.data.confirmed || this.data.busy || !this.data.view || this._accessVerified === false) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const confirmation = await api.confirmSharedProjectNote(this._shareToken, api.mutationKey('project-note-confirm'));
      if (!this.current(epoch)) return;
      this.setData({ view: Object.assign({}, this.data.view, { confirmation }), confirmed: true, busy: false, notice: '已确认当前固定版本。设计师后续新版本需另行确认。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '版本确认失败' }); }
  },
  async exportSharedProjectNoteWord() {
    if (!this.data.view || this.data.busy || this._accessVerified === false) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const filePath = await api.downloadSharedProjectNoteWord(this._shareToken);
      if (!this.current(epoch)) return;
      await new Promise((resolve, reject) => wx.openDocument({ filePath, fileType: 'docx', showMenu: true, success: resolve, fail: () => reject(new Error('Word 文件打开失败，请重试')) }));
      if (this.current(epoch)) this.setData({ busy: false, notice: '已打开当前固定版本的 Word 文件。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || 'Word 导出失败' }); }
  },
  async saveOriginalImage(event) {
    if (!this.data.view || this.data.busy || this._accessVerified === false) return;
    const mediaID = String(event.currentTarget.dataset.id || '');
    if (!this.data.media.some(item => item.media_id === mediaID && item.kind === 'image')) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const filePath = await api.downloadSharedProjectNoteMedia(this._shareToken, mediaID);
      if (!this.current(epoch)) return;
      await new Promise((resolve, reject) => wx.saveImageToPhotosAlbum({ filePath, success: resolve, fail: () => reject(new Error('原图未保存，请检查相册权限后重试')) }));
      if (this.current(epoch)) this.setData({ busy: false, notice: '原图已保存到相册。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '原图保存失败，请确认分享仍有效' }); }
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
  retry() { return this.data.view ? this.revalidateShared() : this.loadShared(); },
  onShareAppMessage() {
    if (!this.data.shareReady || !this.data.view || !this._shareToken || this._accessVerified === false) return {};
    const version = this.data.view.version;
    const coverID = version.cover && version.cover.media_id;
    const cover = this.data.media.find(item => item.kind === 'image' && item.media_id === coverID);
    return { imageUrl: cover && cover.local_path || '', title: version.title || '项目方案', path: '/pages/projectnoteview/projectnoteview?pns=' + encodeURIComponent(this._shareToken) };
  },
});
