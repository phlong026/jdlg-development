'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');
const { fitNoteMedia, fitNoteBlocks, groupNoteImages, noteVideoMetadata } = require('../../utils/project-note-media');

// T7.8：按后端语义映射错误文案，不按状态码一刀切；不暴露设计师个人联系方式。
const SHARE_ERROR = {
  forbidden: '当前账号没有该项目的访问权限，请联系设计师。',
  changed: '分享内容已更新，请重新打开。',
  gone: '分享不存在或已失效，请让设计师重新分享。',
  projectGone: '该方案暂不可访问，请联系设计师。',
  media: '部分素材加载失败，可重试（分享本身有效）。',
  network: '加载失败，请重试。',
};
const mediaFailureCount = media => (media || []).filter(item => item && (item.preview_error || !item.local_path)).length;
const shareErrorOf = error => {
  const status = Number(error && error.statusCode);
  const message = String(error && (error.message || error.msg) || '');
  if (status === 401) return { loginRequired: true, text: '' };
  if (message.includes('分享已变更')) return { loginRequired: false, text: SHARE_ERROR.changed };
  if (message.includes('仅受邀') || status === 403) return { loginRequired: false, text: SHARE_ERROR.forbidden };
  if (message.includes('私人项目不可访问')) return { loginRequired: false, text: SHARE_ERROR.projectGone };
  if (message.includes('分享不存在')) return { loginRequired: false, text: SHARE_ERROR.gone };
  if (message.includes('媒体不可访问') || message.includes('素材不可访问')) return { loginRequired: false, text: SHARE_ERROR.media };
  return { loginRequired: false, text: SHARE_ERROR.network };
};
const satisfactionLabels = { very_satisfied: '非常满意', needs_adjustment: '基本满意，需要调整', unsatisfied: '不太满意' };
const eventID = () => 'project-note-open-' + Date.now() + '-' + Math.random().toString(36).slice(2, 10);
const feedbackRows = view => {
  const replies = new Map();
  for (const resolution of view && view.resolutions || []) replies.set(resolution.feedback_id, String(resolution.reply || ''));
  return (view && view.feedbacks || []).map(feedback => Object.assign({}, feedback, { resolution_reply: feedback.reply || replies.get(feedback.feedback_id) || '', satisfaction_label: satisfactionLabels[feedback.satisfaction] || '' }));
};
const loadSharedMedia = async (token, asset) => {
  if (asset && asset.kind === 'video') {
    const playback = await api.getSharedProjectNoteMediaPlayback(token, asset.media_id);
    const url = String(playback && playback.url || '');
    if (!url) throw new Error('视频播放地址获取失败');
    return Object.assign({}, asset, { local_path: url, playback_expires_at: playback.expires_at || '', preview_error: '' });
  }
  return Object.assign({}, asset, { local_path: await api.downloadSharedProjectNoteMedia(token, asset.media_id), preview_error: '' });
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
  onSharedNoteVideoError(event) {
    const mediaID = String(event.currentTarget && event.currentTarget.dataset && event.currentTarget.dataset.id || '');
    const failedURL = String(event.currentTarget && event.currentTarget.dataset && event.currentTarget.dataset.src || '');
    const currentAsset = (this.data.media || []).find(item => item.media_id === mediaID && item.kind === 'video');
    if (!mediaID || !this.data.view || !this.current(this._epoch) || this._accessVerified === false || !currentAsset || (failedURL && failedURL !== currentAsset.local_path)) return;
    const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
    const media = fitNoteMedia((this.data.media || []).map(item => item.media_id === mediaID && item.kind === 'video'
      ? Object.assign({}, item, { local_path: '', playback_expires_at: '', preview_error: '视频播放失败' }) : item), windowInfo);
    const version = this.data.view.version || {};
    const articleBlocks = version.body_format === 'article_v2' ? fitNoteBlocks(renderArticleDocument(version.body, media, windowInfo.windowWidth * 636 / 750, 'private-note'), media, windowInfo) : [];
    this.setData({ media, mediaGroups: groupNoteImages(media, media, windowInfo), articleBlocks, mediaFailuresCount: mediaFailureCount(media), notice: '视频播放失败，请重新加载。' });
  },
  data: {
    loading: true, busy: false, loginRequired: false, error: '', notice: '', memberMode: false, memberDirectory: null, memberNextCursor: '',
    activityTypes: [{ key: 'feedbacks', label: '反馈意见' }, { key: 'resolutions', label: '处理记录' }, { key: 'confirmations', label: '确认记录' }], activityType: 'feedbacks', activityIndex: 0, activityItems: [], activityPage: 0, activityCursors: [''], activityNextCursor: '', activityLoaded: false, activityLoading: false,
    satisfaction: '', readAcknowledged: false, acknowledgingRead: false,
    view: null, memberDirectory: null, articleBlocks: [], media: [], attachments: [], feedbackRows: [], feedbackText: '', canConfirm: false, confirmed: false, shareReady: false,
    mediaGroups: [], galleryPositions: {},
  },
  onLoad(options) {
    wx.hideShareMenu();
    this._shareToken = String(options && options.pns || '').trim();
    this._memberProjectID = String(options && options.project_id || '').trim();
    this.setData({ memberMode: !!this._memberProjectID });
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
    if (this._memberProjectID && !this._memberVersionID) return this.loadMemberDirectory();
    return this.data.view ? this.revalidateShared() : this.loadShared();
  },
  onHide() {
    this._visible = false; this._epoch = (this._epoch || 0) + 1;
    // 切后台后旧操作不会再写回，三个操作锁必须复位，避免返回时永久 loading。
    this.setData({ acknowledgingRead: false, activityLoading: false, busy: false });
    wx.hideShareMenu();
  },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  // 操作阶段失败：401 统一进入登录恢复；其它保留可重试提示。
  readerFailure(error, fallback) {
    const status = Number(error && error.statusCode);
    if (status === 401) { this.clearProtected(); this.setData({ loginRequired: true, error: '', busy: false, activityLoading: false, acknowledgingRead: false }); return; }
    this.setData({ busy: false, activityLoading: false, notice: (error && error.message) || fallback });
  },
  clearProtected() {
    this._accessAttempt = null;
    this._accessVerified = false;
    this.setData({ activityType: 'feedbacks', activityIndex: 0, activityItems: [], activityPage: 0, activityCursors: [''], activityNextCursor: '', activityLoaded: false, activityLoading: false, view: null, memberDirectory: null, articleBlocks: [], media: [], mediaGroups: [], galleryPositions: {}, attachments: [], feedbackRows: [], canConfirm: false, confirmed: false, shareReady: false, feedbackText: '', satisfaction: '', readAcknowledged: !!this._openSent, acknowledgingRead: false, notice: '', busy: false });
  },
  async loadMemberDirectory(event) {
    const append = !!(event && event.currentTarget);
    if (append && !this.data.memberNextCursor) return;
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this.setData({ loading: true, error: '' });
    try {
      const directory = await require('../../utils/project-member-api').directory(this._memberProjectID, { limit: 30, cursor: append ? this.data.memberNextCursor : '' });
      if (!this.current(epoch)) return;
      const versions = append && this.data.memberDirectory ? this.data.memberDirectory.versions.concat(directory.versions || []) : directory.versions || [];
      this.setData({ loading: false, memberDirectory: Object.assign({}, directory, { versions }), memberNextCursor: directory.next_cursor || '' });
    } catch (error) {
      if (!this.current(epoch)) return;
      this.clearProtected(); this.setData({ loading: false, error: error.message || '项目暂不可访问', loginRequired: Number(error.statusCode) === 401 });
    }
  },
  openMemberVersion(event) {
    this._memberVersionID = String(event.currentTarget.dataset.id || '');
    return this.loadShared();
  },
  backToMemberDirectory() { this._memberVersionID = ''; this.clearProtected(); return this.loadMemberDirectory(); },
  readSharedView() {
    return this._memberProjectID
      ? require('../../utils/project-member-api').version(this._memberProjectID, this._memberVersionID)
      : api.getSharedProjectNote(this._shareToken);
  },
  async loadSharedAsset(asset) {
    if (!this._memberProjectID) return loadSharedMedia(this._shareToken, asset);
    const member = require('../../utils/project-member-api');
    if (asset.kind === 'video') {
      const playback = await member.playback(this._memberProjectID, this._memberVersionID, asset.media_id);
      if (!playback.url) throw new Error('视频播放地址获取失败');
      return Object.assign({}, asset, { local_path: playback.url, preview_error: '' });
    }
    return Object.assign({}, asset, { local_path: await member.download(this._memberProjectID, this._memberVersionID, asset.media_id), preview_error: '' });
  },
  async loadShared() {
    const accountToken = String(wx.getStorageSync('token') || '');
    if (!accountToken) { this.clearProtected(); this.setData({ loading: false, loginRequired: true, error: '' }); return; }
    if (!this._memberProjectID && !/^[A-Za-z0-9_-]{8,256}$/.test(this._shareToken || '')) { this.clearProtected(); this.setData({ loading: false, loginRequired: false, error: SHARE_ERROR.gone }); return; }
    this._accountToken = accountToken;
    const epoch = this._epoch = (this._epoch || 0) + 1;
    wx.hideShareMenu(); this.clearProtected(); this.setData({ loading: true, loginRequired: false, error: '' });
    try {
      const view = await this.readSharedView();
      if (!this.current(epoch)) return;
      const assets = [];
      if (view.version && view.version.cover) assets.push(view.version.cover);
      for (const asset of view.version && view.version.media || []) if (!assets.some(item => item.media_id === asset.media_id)) assets.push(asset);
      // 分享读取与媒体下载分开：素材 404 只做素材级失败隔离，401/403 仍按撤权处理。
      // 媒体接口会先校验分享，404 也可能代表分享已失效：此时必须重新核验分享。
      let mediaFailed = false, shareValidity = null;
      // T3.2：素材下载并发上限 4，避免弱网下同时发起过多请求。
      let media = new Array(assets.length);
      let mediaCursor = 0;
      const worker = async () => {
        while (mediaCursor < assets.length) {
          const index = mediaCursor++;
          const asset = assets[index];
          try { media[index] = await this.loadSharedAsset(asset); continue; }
          catch (error) {
            const status = Number(error && error.statusCode);
            if ([401, 403].includes(status)) throw error;
            if (status === 404) {
              shareValidity = shareValidity || (async () => {
                try {
                  const fresh = await this.readSharedView();
                  const same = this._memberProjectID
                    ? !!(fresh && fresh.project && fresh.version && String(fresh.project.project_id || '') === this._memberProjectID && String(fresh.version.version_id || '') === this._memberVersionID)
                    : !!(fresh && fresh.share && String(fresh.share.share_id || '') === String(view.share && view.share.share_id || ''));
                  return same ? { ok: true } : { ok: false, error: Object.assign(new Error('分享内容已更新'), { statusCode: 403 }) };
                } catch (error) { return { ok: false, error }; }
              })();
              // 分享已失效：按真实错误语义清空受保护数据，不能继续展示正文/反馈。
              const verdict = await shareValidity;
              if (!verdict.ok) throw verdict.error || Object.assign(new Error('分享不存在或已失效'), { statusCode: 404 });
            }
            mediaFailed = true;
            media[index] = Object.assign({}, asset, { local_path: '', preview_error: '素材加载失败' });
          }
        }
      };
      await Promise.all(Array.from({ length: Math.min(4, Math.max(1, assets.length)) }, () => worker()));
      if (!this.current(epoch)) return;
      const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
      media = fitNoteMedia(media, windowInfo);
      const width = windowInfo.windowWidth * 636 / 750;
      const articleBlocks = view.version.body_format === 'article_v2' ? fitNoteBlocks(renderArticleDocument(view.version.body, media, width, 'private-note'), media, windowInfo) : [];
      const attachments = media.filter(asset => asset.kind !== 'image' && asset.kind !== 'video');
      const canConfirm = view.can_confirm === true;
      const confirmed = !!view.confirmation;
      this._accessVerified = true;
      this.setData({ view, media, mediaGroups: groupNoteImages(media, media, windowInfo), galleryPositions: {}, attachments, articleBlocks, feedbackRows: feedbackRows(view), canConfirm, confirmed, loading: false, notice: mediaFailed ? SHARE_ERROR.media : '', mediaFailuresCount: mediaFailureCount(media), shareReady: !!view.share && !!this._shareToken }, () => {
        if (!this.current(epoch)) return;
        if (!this._memberProjectID && wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage'] });

      });
    } catch (error) {
      if (!this.current(epoch)) return;
      const copy = shareErrorOf(error);
      wx.hideShareMenu(); this.clearProtected();
      this.setData({ loading: false, loginRequired: copy.loginRequired, error: copy.text });
    }
  },
  async revalidateShared() {
    const cached = this.data.view;
    if (!cached) return this.loadShared();
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this._accessVerified = false;
    wx.hideShareMenu(); this.setData({ loading: true, loginRequired: false, error: '', notice: '' });
    try {
      const view = await this.readSharedView();
      if (!this.current(epoch)) return;
      const sameShare = this._memberProjectID
        ? String(view.project && view.project.project_id || '') === this._memberProjectID
        : String(view.share && view.share.share_id || '') === String(cached.share && cached.share.share_id || '');
      const sameVersion = String(view.version && view.version.version_id || '') === String(cached.version && cached.version.version_id || '');
      if (!sameShare || !sameVersion) throw Object.assign(new Error('分享已变更'), { statusCode: 403 });
      const unchanged = String(view.version && view.version.revision || '') === String(cached.version && cached.version.revision || '') && (this.data.media || []).length >= 0;
      const hasVideos = (this.data.media || []).some(item => item && item.kind === 'video');
      let media = this.data.media || [], articleBlocks = this.data.articleBlocks || [], videoRefreshFailed = false;
      if (hasVideos) {
        media = await Promise.all(media.map(async item => {
          if (!item || item.kind !== 'video') return item;
          try { return await this.loadSharedAsset(item); }
          catch (error) {
            if ([401, 403].includes(Number(error && error.statusCode))) throw error;
            videoRefreshFailed = true;
            return Object.assign({}, item, { local_path: '', preview_error: '视频加载失败' });
          }
        }));
        if (!this.current(epoch)) return;
        const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
        media = fitNoteMedia(media, windowInfo);
        const width = windowInfo.windowWidth * 636 / 750;
        articleBlocks = view.version.body_format === 'article_v2' ? fitNoteBlocks(renderArticleDocument(view.version.body, media, width, 'private-note'), media, windowInfo) : [];
      }
      this._accessVerified = true;
      if (unchanged) {
        // 图片/文档复用本地文件；视频签名地址短时有效，每次回到页面单独刷新。
        const mediaFailures = media.some(item => item && (item.preview_error || !item.local_path));
        const mediaPatch = hasVideos ? { media, mediaGroups: groupNoteImages(media, media, wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync()), articleBlocks } : {};
        this.setData(Object.assign({ view, feedbackRows: feedbackRows(view), canConfirm: view.can_confirm === true, confirmed: !!view.confirmation, loading: false, acknowledgingRead: false, activityLoading: false, busy: false, notice: mediaFailures || videoRefreshFailed ? SHARE_ERROR.media : '', mediaFailuresCount: mediaFailureCount(media), shareReady: !!view.share && !!this._shareToken }, mediaPatch), () => {
          if (this.current(epoch) && !this._memberProjectID && wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage'] });
        });
        return;
      }
      const pendingMediaFailures = media.some(item => item && (item.preview_error || !item.local_path));
      const mediaPatch = hasVideos ? { media, mediaGroups: groupNoteImages(media, media, wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync()), articleBlocks } : {};
      this.setData(Object.assign({ view, feedbackRows: feedbackRows(view), canConfirm: view.can_confirm === true, confirmed: !!view.confirmation, loading: false, acknowledgingRead: false, activityLoading: false, busy: false, notice: pendingMediaFailures || videoRefreshFailed ? SHARE_ERROR.media : '', mediaFailuresCount: mediaFailureCount(media), shareReady: !!view.share && !!this._shareToken }, mediaPatch), () => {
        if (this.current(epoch) && !this._memberProjectID && wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage'] });
      });
    } catch (error) {
      if (!this.current(epoch)) return;
      const status = Number(error.statusCode);
      const copy = shareErrorOf(error);
      if ([401, 403, 404].includes(status) || copy.loginRequired) {
        wx.hideShareMenu(); this.clearProtected(); this.setData({ loading: false, loginRequired: copy.loginRequired, error: copy.loginRequired ? '' : copy.text });
        return;
      }
      this.setData({ loading: false, shareReady: false, canConfirm: false, error: copy.text });
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
    } catch (error) { if (this.current(epoch) && request === this._activityRequest) { const status = Number(error && error.statusCode); if (status === 401) this.readerFailure(error, '历史记录加载失败'); else this.setData({ activityLoading: false, notice: error.message || '历史记录加载失败，可重试' }); } }
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
    } catch (error) { if (this.current(epoch)) this.readerFailure(error, '反馈提交失败'); }
  },
  async confirmVersion() {
    if (!this.data.canConfirm || this.data.confirmed || this.data.busy || !this.data.view || this._accessVerified === false) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const confirmation = await api.confirmSharedProjectNote(this._shareToken, api.mutationKey('project-note-confirm'));
      if (!this.current(epoch)) return;
      this.setData({ view: Object.assign({}, this.data.view, { confirmation }), confirmed: true, busy: false, notice: '已确认当前固定版本。设计师后续新版本需另行确认。' });
    } catch (error) { if (this.current(epoch)) this.readerFailure(error, '版本确认失败'); }
  },
  async exportSharedProjectNoteWord() {
    if (!this.data.view || this.data.busy || this._accessVerified === false) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const filePath = await api.downloadSharedProjectNoteWord(this._shareToken);
      if (!this.current(epoch)) return;
      await new Promise((resolve, reject) => wx.openDocument({ filePath, fileType: 'docx', showMenu: true, success: resolve, fail: () => reject(new Error('Word 文件打开失败，请重试')) }));
      if (this.current(epoch)) this.setData({ busy: false, notice: '已打开当前固定版本的 Word 文件。' });
    } catch (error) { if (this.current(epoch)) this.readerFailure(error, 'Word 导出失败'); }
  },
  async saveOriginalImage(event) {
    if (!this.data.view || this.data.busy || this._accessVerified === false) return;
    const mediaID = String(event.currentTarget.dataset.id || '');
    if (!this.data.media.some(item => item.media_id === mediaID && item.kind === 'image')) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      const filePath = this._memberProjectID
        ? await require('../../utils/project-member-api').download(this._memberProjectID, this._memberVersionID, mediaID)
        : await api.downloadSharedProjectNoteMedia(this._shareToken, mediaID);
      if (!this.current(epoch)) return;
      await new Promise((resolve, reject) => wx.saveImageToPhotosAlbum({ filePath, success: resolve, fail: () => reject(new Error('原图未保存，请检查相册权限后重试')) }));
      if (this.current(epoch)) this.setData({ busy: false, notice: '原图已保存到相册。' });
    } catch (error) { if (this.current(epoch)) this.readerFailure(error, '原图保存失败，请确认分享仍有效'); }
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
  goLogin() { wx.navigateTo({ url: '/pages/login/login?from=share-view' }); },
  // T7.8 / P9：错误区联系客服跳平台客服页，不暴露设计师个人联系方式。
  openCustomerService() { wx.navigateTo({ url: '/pages/customerservice/customerservice' }); },
  retry() { return this._memberProjectID && !this._memberVersionID ? this.loadMemberDirectory() : this.data.view ? this.revalidateShared() : this.loadShared(); },
  // D16：分享有效时定向重试失败素材，只重下失败项（并发上限 4）。
  async retryMediaDownloads() {
    const failures = (this.data.media || []).filter(item => item && (item.preview_error || !item.local_path));
    if (!failures.length || this._mediaRetrying) return;
    const epoch = this._epoch;
    this._mediaRetrying = true;
    this.setData({ notice: '正在重新加载素材…' });
    const updated = new Map();
    let cursor = 0, accessError, validity;
    const worker = async () => {
      while (cursor < failures.length) {
        const item = failures[cursor++];
        try { updated.set(item.media_id, await this.loadSharedAsset(item)); }
        catch (error) {
          if ([401, 403].includes(Number(error.statusCode))) accessError = error;
          if (Number(error.statusCode) === 404) {
            validity = validity || this.readSharedView();
            try { await validity; } catch (failure) { accessError = failure; }
          }
        }
      }
    };
    await Promise.all([worker(), worker(), worker(), worker()]);
    this._mediaRetrying = false;
    if (!this.current(epoch)) return;
    if (accessError) { const failure = shareErrorOf(accessError); this.clearProtected(); this.setData({ loading: false, loginRequired: failure.loginRequired, error: failure.text }); return; }
    const media = (this.data.media || []).map(item => updated.get(item.media_id) || item);
    const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
    const failuresLeft = mediaFailureCount(media);
    const version = this.data.view && this.data.view.version || {};
    const articleBlocks = version.body_format === 'article_v2' ? fitNoteBlocks(renderArticleDocument(version.body, media, windowInfo.windowWidth * 636 / 750, 'private-note'), media, windowInfo) : [];
    this.setData({ media, mediaGroups: groupNoteImages(media, media, windowInfo), articleBlocks, mediaFailuresCount: failuresLeft, notice: failuresLeft ? SHARE_ERROR.media : '素材已重新加载。' });
  },
  onShareAppMessage() {
    if (!this.data.shareReady || !this.data.view || !this._shareToken || this._accessVerified === false) return {};
    const version = this.data.view.version;
    const coverID = version.cover && version.cover.media_id;
    const cover = this.data.media.find(item => item.kind === 'image' && item.media_id === coverID);
    return { imageUrl: cover && cover.local_path || '', title: version.title || '项目方案', path: '/pages/projectnoteview/projectnoteview?pns=' + encodeURIComponent(this._shareToken) };
  },
});
