// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');
const { fitNoteMedia, fitNoteBlocks, groupNoteImages, noteVideoMetadata } = require('../../utils/project-note-media');
const { buildContentPresentation } = require('../../utils/content-presentation');
const { startAIStream } = require('../../utils/ai-stream');
const { ANTI_FABRICATION_CLAUSE, noteWritingPrompt } = require('../../utils/writing-policy');

const blankProtected = () => ({
  pageView: 'list', navigationTitle: '我的项目', projectSettingsOpen: false, projectLoadFailed: false,
  historyType: 'versions', historyIndex: 0, historyItems: [], historyPage: 0, historyCursors: [''], historyNextCursor: '', historyLoaded: false, historyLoading: false,
  projectEdit: {}, projectStatusLabel: '', notes: [], noteIndex: 0, versionChoices: [], selectedFeedback: null,
  projects: [], owners: [], ownerChoices: [], ownerIndex: 0, ownerID: '', projectName: '', crmID: '', selectedProject: null,
  members: [], activeMembers: [], joinRequests: [], memberInvitations: [], requestsLoading: false, confirmerChoices: [], confirmerIndex: 0, confirmerAccount: '',
  memberInvitation: null, memberInvitationPath: '',
  note: null, noteTitle: '', noteBody: '', bodyReadOnly: false, noteMedia: [], frozenVersion: null, activeShare: null,
  feedbacks: [], confirmations: [], feedbackID: '', feedbackReply: '', truncationNotice: '', historyNotice: '',
  historyMode: false, versionView: null, versionBlocks: [], versionMedia: [],
  editorMode: 'edit', previewLoading: false, previewError: '', previewBodyFormat: 'plain', previewBody: '', previewBlocks: [], previewMedia: [], keyboardHeight: 0,
  previewPresentation: null, previewMediaErrors: {}, previewVideoStarted: {},
  previewMediaGroups: [], versionMediaGroups: [], galleryPositions: {}, mediaUploadFailures: [], mediaFailureSummary: '', mediaRetryLabel: '',
  mediaPermissionDenied: false, aiPanelOpen: false, aiPanelStreaming: false, aiPanelText: '', aiPanelPrompt: '', aiPanelError: '', aiPanelNotice: '',
});
const value = event => String(event.detail && event.detail.value || '').trim();
const mediaID = item => String(item && (item.media_id || item.id || item.asset && item.asset.media_id) || '');
// 分享入口凭证只在本机保存（服务端只存 token 哈希），用于同一设备重进页面后继续预览/复制。
const SHARE_TOKEN_CACHE_KEY = 'project_note_share_tokens_v1';
const shareTokenCache = () => {
  try { const value = wx.getStorageSync(SHARE_TOKEN_CACHE_KEY); return value && typeof value === 'object' ? value : {}; } catch (_) { return {}; }
};
const rememberShareToken = share => {
  if (!share || !share.share_id || !share.share_token) return;
  try { const cache = shareTokenCache(); cache[share.share_id] = { token: String(share.share_token), saved_at: Date.now() }; wx.setStorageSync(SHARE_TOKEN_CACHE_KEY, cache); } catch (_) {}
};
const cachedShareToken = shareID => {
  if (!shareID) return '';
  const entry = shareTokenCache()[shareID];
  return entry && typeof entry.token === 'string' ? entry.token : '';
};
const forgetShareToken = shareID => {
  if (!shareID) return;
  try { const cache = shareTokenCache(); delete cache[shareID]; wx.setStorageSync(SHARE_TOKEN_CACHE_KEY, cache); } catch (_) {}
};
const assetOf = item => item && item.asset ? item.asset : item;
// T2.4 客户端预检：与服务端边界对齐（P4：图 20 MiB / 视频 500 MiB / 累计 50 项）。
const NOTE_MEDIA_IMAGE_LIMIT = 20 * 1024 * 1024;
const NOTE_MEDIA_VIDEO_LIMIT = 500 * 1024 * 1024;
const NOTE_MEDIA_TOTAL_LIMIT = 50;
const formatMegabytes = bytes => (Number(bytes) / 1024 / 1024).toFixed(1);
// D1：后台完成的上传素材持久化，重启后仍能补回到原归属草稿（7 天过期，按 owner 隔离）。
const PENDING_MEDIA_KEY = 'project_note_pending_media_v1';
const PENDING_MEDIA_TTL_MS = 7 * 86400000;
const readPendingMediaStore = () => {
  try {
    const raw = wx.getStorageSync(PENDING_MEDIA_KEY);
    if (!raw || typeof raw !== 'object') return {};
    const store = {}, now = Date.now();
    for (const owner of Object.keys(raw)) {
      const entry = raw[owner];
      if (!entry || !Array.isArray(entry.assets) || !entry.at) continue;
      if (now - Number(entry.at) > PENDING_MEDIA_TTL_MS || !entry.assets.length) continue;
      store[owner] = entry.assets;
    }
    return store;
  } catch (_) { return {}; }
};
const writePendingMediaStore = store => {
  try {
    const raw = {};
    for (const owner of Object.keys(store || {})) if (store[owner] && store[owner].length) raw[owner] = { at: Date.now(), assets: store[owner] };
    if (Object.keys(raw).length) wx.setStorageSync(PENDING_MEDIA_KEY, raw);
    else wx.removeStorageSync(PENDING_MEDIA_KEY);
  } catch (_) { /* 存储不可用时仅保留内存态 */ }
};
// T3.1：本地文件仍可读时才复用，失效则单项重下。
const localFileUsable = filePath => {
  const file = String(filePath || '');
  if (!file) return false;
  try {
    const fs = typeof wx.getFileSystemManager === 'function' ? wx.getFileSystemManager() : null;
    if (!fs || typeof fs.accessSync !== 'function') return true;
    fs.accessSync(file);
    return true;
  } catch (_) { return false; }
};
// 只有 media_id 的私有素材：下载后按临时文件扩展名恢复类型，无法识别则保持占位。
const inferNoteMediaKind = (filePath, fallback) => {
  const path = String(filePath || '');
  if (/\.(jpe?g|png|gif|webp|bmp)$/i.test(path)) return 'image';
  if (/\.(mp4|mov|m4v|avi|mkv)$/i.test(path)) return 'video';
  return fallback;
};
// 上传前预检：返回第一条拒绝原因；未超限返回空串。超限不得发起任何上传请求。
const noteMediaPrecheck = (existing, files) => {
  if (existing.length + files.length > NOTE_MEDIA_TOTAL_LIMIT) return `每个笔记最多 ${NOTE_MEDIA_TOTAL_LIMIT} 项素材，当前已有 ${existing.length} 项。`;
  for (const file of files) {
    const declared = String(file && file.fileType || '');
    if (declared && declared !== 'image' && declared !== 'video') return '仅支持上传图片或视频素材。';
    const filePath = String(file && file.tempFilePath || '');
    const imageExt = /\.(jpe?g|png|gif|webp|bmp)$/i.test(filePath);
    const videoExt = /\.(mp4|mov|m4v|avi|mkv)$/i.test(filePath);
    if (filePath && !declared && !imageExt && !videoExt) return '仅支持 PNG/JPEG/GIF/WebP 图片或 MP4/MOV 视频。';
    const kind = declared || (videoExt ? 'video' : 'image');
    const size = Number(file && file.size);
    if (!Number.isFinite(size) || size <= 0) continue;
    const limit = kind === 'video' ? NOTE_MEDIA_VIDEO_LIMIT : NOTE_MEDIA_IMAGE_LIMIT;
    if (size > limit) return `${kind === 'video' ? '视频' : '图片'}不能超过 ${formatMegabytes(limit)} MB，当前文件为 ${formatMegabytes(size)} MB。`;
  }
  return '';
};
// 上传失败要能区分「服务端拒绝」和「网络异常」：HTTP 错误保留 data.msg 与 statusCode。
const uploadFailureMessage = error => {
  const status = Number(error && error.statusCode);
  const message = String(error && (error.message || error.msg) || '').trim();
  if (!status) return '网络异常，请检查网络后重试';
  return message || `私有素材上传失败（${status}）`;
};
const uploadFileName = (file, position) => {
  const path = String(file && file.tempFilePath || '');
  const name = String(file && file.name || path.split('/').pop() || '').trim();
  return name || `第 ${position + 1} 个文件`;
};
// 草稿实例身份：新草稿没有 note_id、修订恒为 0，不能拿它们当唯一键。
// 实例 ID 在草稿实例创建时生成，跨 AI 重进保持稳定；新建/切换/丢弃/保存后重新生成。
const draftAccount = () => {
  try {
    const profile = wx.getStorageSync('userInfo') || {};
    const id = profile && typeof profile === 'object' ? String(profile.id || '') : '';
    if (id) return id;
  } catch (_) { /* 资料未缓存时回退到登录凭证 */ }
  return String(wx.getStorageSync('token') || '');
};
const activeMembers = members => (members || []).filter(item => item && item.active !== false);
const pad2 = value => String(value).padStart(2, '0');
const attributionDate = value => {
  const date = new Date(value);
  if (!value || Number.isNaN(date.getTime())) return '';
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
};
const memberAttribution = item => Object.assign({}, item, {
  joined_label: attributionDate(item && item.joined_at),
  inviter_label: item && item.inviter_name ? '由 ' + item.inviter_name + ' 邀请' : '',
});
const memberLabel = member => String(member && member.display_name || '').trim() || '项目成员';
const invitationLabel = item => {
  const date = new Date(item && item.expires_at || '');
  if (Number.isNaN(date.getTime())) return '有效中';
  const pad = value => String(value).padStart(2, '0');
  return `有效至 ${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
};
const statusLabels = { initial: '初步方案', revising: '修改中', confirmed: '已确认' };
const satisfactionLabels = { very_satisfied: '非常满意', needs_adjustment: '基本满意，需要调整', unsatisfied: '不太满意' };
const localTime = raw => {
  const date = new Date(raw || '');
  if (Number.isNaN(date.getTime())) return String(raw || '');
  const pad = value => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
};
const projectForm = project => ({ name: project.name || '', location: project.location || '', building_area: project.building_area || '', floors: project.floors || '', style: project.style || '' });
const truncationText = flags => Object.keys(flags || {}).some(key => flags[key]) ? '部分历史记录超过50条，当前仅显示最近50条。' : '';

Page({
  data: Object.assign({ historyTypes: [{ key: 'versions', label: '方案版本' }, { key: 'notes', label: '笔记草稿' }, { key: 'feedbacks', label: '业主意见' }, { key: 'resolutions', label: '处理记录' }, { key: 'confirmations', label: '业主确认' }], loading: true, busy: false, error: '', notice: '', loginRequired: false, accessRecheckRequired: false, mediaUploadFailures: [], mediaFailureSummary: '', mediaRetryLabel: '' }, blankProtected()),
  onLoad(options) {
    wx.hideShareMenu(); this._visible = true; this._unloaded = false;
    this._targetProjectID = String(options && options.project_id || '');
    this._targetVersionID = String(options && options.version_id || '');
    this._targetFeedbackID = String(options && options.feedback_id || '');
    this._targetView = String(options && options.view || '') === 'members' ? 'members' : '';
    this._returnToPackage = this._targetView === 'members' && String(options && options.return_to || '') === 'package';
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._token) {
      this._token = token; this._epoch = (this._epoch || 0) + 1;
      this._loaded = false; this._writingLaunch = null; this._selectedNoteID = ''; this._newNote = false; this._resolutionAttempt = null; this._issuedShare = null; this._shareAttempt = null; this._invitationAttempt = null; this._approvalAttempts = new Map(); this._selectedProjectID = ''; this._selectedVersionID = ''; this.clearProtected();
    }
    this._visible = true;
    if (!token) { this.setData({ loading: false, loginRequired: true, error: '' }); return; }
    if (this.data.pageView === 'editor') this.acceptPendingMedia();
    const afterRecheck = () => {
      this.flushPickerFeedback(); this.resumeNoteMediaPreviews();
      if (this.data.pageView === 'version' || this.data.pageView === 'members') {
        if (this.data.requestsLoading) this._joinRequestsRetry = true; else this.loadJoinRequests();
      }
    };
    if (this._writingLaunch && this._writingLaunch.token === token) {
      this._writingLaunch = null;
      return Promise.resolve(this.revalidateProjectAccess()).then(afterRecheck);
    }
    return Promise.resolve(this._loaded ? this.revalidateProjectAccess() : this.loadProjects(this._epoch)).then(afterRecheck);
  },
  onHide() {
    this._visible = false; this._epoch = (this._epoch || 0) + 1;
    // 登录守卫锁定/切后台都走这里：AI 面板属于私密内容，必须立即失效并清空。
    this._aiPanelRequest = (this._aiPanelRequest || 0) + 1;
    this._aiPanelLaunch = null;
    this.abortNoteAI();
    this.setData({ aiPanelOpen: false, aiPanelStreaming: false, aiPanelText: '', aiPanelPrompt: '', aiPanelError: '', aiPanelNotice: '' });
    this.editNote();
  },
  onUnload() { this._unloaded = true; this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  onPageScroll(event) {
    if (this.data.pageView === 'list') this._listScrollTop = Number(event && event.scrollTop || 0);
    if (this.data.pageView === 'editor' && this.data.editorMode === 'edit') this._editorScrollTop = Number(event && event.scrollTop || 0);
  },
  scrollPage(view) {
    if (typeof wx.pageScrollTo !== 'function') return;
    wx.pageScrollTo({ scrollTop: view === 'list' ? Number(this._listScrollTop || 0) : 0, duration: 0 });
  },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  // 选择器反馈跨权限重验保留：重验会清空 error/notice，完成后回填一次。
  flushPickerFeedback() {
    const pending = this._pendingPickerFeedback;
    if (!pending) return;
    this._pendingPickerFeedback = null;
    // 重验失败（如网络/权限）优先展示，不被延迟的取消/预检提示覆盖。
    const recheckFailed = !!this.data.error || /重验失败/.test(String(this.data.notice || ''));
    if (recheckFailed && pending.data && pending.data.notice && !pending.data.error) return;
    const owner = pending.owner || {};
    // 反馈只回填到发起时的账号/项目/草稿实例；换账号、撤权或切草稿后直接丢弃。
    const sameDraft = owner.token === this._token && owner.token === String(wx.getStorageSync('token') || '')
      && (this._protectedGeneration || 0) === owner.generation
      && !!this.data.selectedProject && this.data.selectedProject.project_id === owner.projectID
      && this.currentDraftInstanceID() === owner.instanceID;
    if (!sameDraft) return;
    this.setData(pending.data);
  },
  clearProtected() {
    // 保护性清空递增失效代际：在途上传/回调据此判断自己是否已作废，不得重建已清空的受保护状态。
    this._protectedGeneration = (this._protectedGeneration || 0) + 1;
    this._noteFormBaseline = null; this._projectFormBaseline = null; this._loadedFormState = null; this.resetDraftInstance(); this._noteMediaFailures = null; this._noteMediaPending = null; this._noteMediaUpload = null; this._pendingPickerFeedback = null; this._hydrateResumePending = false; writePendingMediaStore({}); this._aiPanelRequest = (this._aiPanelRequest || 0) + 1; this._aiPanelLaunch = null; this.abortNoteAI();
    this.setData(Object.assign(blankProtected(), { loading: false, loginRequired: false, accessRecheckRequired: false, error: '', notice: '', busy: false }));
  },

  noteFormSignature() {
    return JSON.stringify({ project_id: this.data.selectedProject && this.data.selectedProject.project_id || '', note_id: this.data.note && this.data.note.note_id || '', new_note: !!this._newNote, draft: this.noteDraft() });
  },
  projectFormSignature() { return JSON.stringify(this.data.projectEdit || {}); },
  rememberLoadedForms() {
    this._noteFormBaseline = this.noteFormSignature(); this._projectFormBaseline = this.projectFormSignature();
    this._loadedFormState = { projectEdit: Object.assign({}, this.data.projectEdit), noteTitle: this.data.noteTitle, noteBody: this.data.noteBody, noteMedia: (this.data.noteMedia || []).map(item => Object.assign({}, item)) };
  },
  hasUnsavedChanges() {
    return this._noteFormBaseline != null && this._noteFormBaseline !== this.noteFormSignature()
      || this._projectFormBaseline != null && this._projectFormBaseline !== this.projectFormSignature();
  },
  confirmDiscardChanges(hasChanges) {
    if (typeof hasChanges !== 'boolean') hasChanges = this.hasUnsavedChanges();
    if (!hasChanges) return Promise.resolve(true);
    return new Promise(resolve => wx.showModal({ title: '放弃未保存的修改？', content: '切换后，当前尚未保存的项目或笔记内容将丢失。', confirmText: '放弃修改', confirmColor: '#a33b31', success: result => resolve(result.confirm === true), fail: () => resolve(false) }));
  },

  restoreLoadedForms() {
    const state = this._loadedFormState;
    if (state) this.setData({ aiPanelOpen: false, aiPanelStreaming: false, aiPanelText: '', aiPanelPrompt: '', aiPanelError: '', aiPanelNotice: '', projectEdit: Object.assign({}, state.projectEdit), noteTitle: state.noteTitle, noteBody: state.noteBody, noteMedia: state.noteMedia.map(item => Object.assign({}, item)) });
  },

  async handleBack() {
    if (this.data.busy) return;
    if (this.data.pageView === 'list') { wx.navigateBack({ fail: () => wx.switchTab({ url: '/pages/my/my' }) }); return; }
    if (this.data.pageView === 'create') {
      const epoch = this._epoch, view = this.data.pageView;
      if (!await this.confirmDiscardChanges(!!this.data.projectName)
        || !this.current(epoch) || this.data.pageView !== view || this.data.busy) return;
      this.setData({ pageView: 'list', navigationTitle: '我的项目', projectName: '', ownerIndex: 0, ownerID: this.data.owners[0] || '', error: '', notice: '' }, () => this.scrollPage('list'));
      return;
    }
    if (this.data.pageView === 'members' && this._returnToPackage) {
      wx.navigateBack({ fail: () => this.setData({ pageView: 'detail', navigationTitle: '项目详情', error: '', notice: '' }) });
      return;
    }
    const epoch = this._epoch, project = this.data.selectedProject, view = this.data.pageView;
    const unchanged = () => this.current(epoch) && this.data.selectedProject === project && this.data.pageView === view && !this.data.busy;
    if (this.data.pageView !== 'detail') {
      if (!await this.confirmDiscardChanges() || !unchanged()) return;
      this.restoreLoadedForms();
      this.resetDraftInstance();
      this.setData({ pageView: 'detail', navigationTitle: '项目详情', projectSettingsOpen: false, error: '', notice: '' }, () => this.scrollPage('detail'));
      return;
    }
    if (!await this.confirmDiscardChanges() || !unchanged()) return;
    const kept = { projects: this.data.projects, owners: this.data.owners, ownerChoices: this.data.ownerChoices, ownerID: this.data.ownerID, ownerIndex: this.data.ownerIndex };
    this._selectedProjectID = ''; this._selectedNoteID = ''; this._selectedVersionID = ''; this._newNote = false;
    this._noteFormBaseline = null; this._projectFormBaseline = null; this._loadedFormState = null;
    this.setData(Object.assign(blankProtected(), kept), () => this.scrollPage('list'));
  },

  openProjectSettings() { this.setData({ projectSettingsOpen: true }); },
  openProjectCreation() {
    if (this.data.busy || this.data.loading || this.data.projectLoadFailed) return;
    if (!this.data.owners.length) { this.setData({ projectLoadFailed: true, error: '暂无可用的项目归属，请重新加载或联系管理员。' }); return; }
    this.setData({ pageView: 'create', navigationTitle: '新建项目', projectName: '', ownerIndex: 0, ownerID: this.data.owners[0] || '', error: '', notice: '' }, () => this.scrollPage('create'));
  },
  async closeProjectSettings() {
    if (this.data.busy) return;
    const epoch = this._epoch, project = this.data.selectedProject, view = this.data.pageView;
    const changed = this._projectFormBaseline != null && this._projectFormBaseline !== this.projectFormSignature();
    if (!await this.confirmDiscardChanges(changed) || !this.current(epoch) || this.data.selectedProject !== project || this.data.pageView !== view || this.data.busy) return;
    // 收起项目资料仅恢复项目字段，不能丢弃仍在编辑的笔记。
    if (this._loadedFormState) this.setData({ projectEdit: Object.assign({}, this._loadedFormState.projectEdit) });
    this.setData({ projectSettingsOpen: false });
  },
  openMembers() { this.setData({ pageView: 'members', navigationTitle: '项目成员', projectSettingsOpen: false }, () => this.scrollPage('members')); return this.loadJoinRequests(); },
  openHistory() { this.setData({ pageView: 'history', navigationTitle: '项目记录', projectSettingsOpen: false }, () => this.scrollPage('history')); if (!this.data.historyLoaded) return this.loadHistory(); },
  openProjectPackage() {
    const project = this.data.selectedProject;
    if (!project || this.data.busy) return;
    wx.navigateTo({ url: '/pages/projectpackage/projectpackage?project_id=' + encodeURIComponent(project.project_id) });
  },
  openNote(event) {
    const id = String(event.currentTarget.dataset.id || event.currentTarget.dataset.mediaId || '');
    if (this._noteLongPress && this._noteLongPress.id === id && Date.now() < this._noteLongPress.until) { this._noteLongPress = null; return; }
    const index = this.data.notes.findIndex(item => item.note_id === id);
    if (index >= 0) return this.selectNote({ detail: { value: index } });
  },

  async revalidateProjectAccess() {
    const epoch = this._epoch = (this._epoch || 0) + 1;
    const project = this.data.selectedProject;
    // 已加载过的页面静默重验：返回本页时不再整页切到"正在加载…"造成闪屏。
    // 新 epoch 已废弃上一轮写操作，在发起重验时同步释放旧锁；
    // 重验完成回调不再写 busy，避免清掉新 epoch 后启动的操作。
    this.setData(this._loaded
      ? { busy: !!this._noteMediaUpload, loginRequired: false, accessRecheckRequired: false, error: '', notice: '' }
      : { busy: false, loading: true, loginRequired: false, accessRecheckRequired: false, error: '', notice: '' });
    try {
      if (project) await api.getProjectNoteProject(project.project_id); else await api.workbenchContext();
      if (!this.current(epoch)) return;
      this.setData({ loading: false, accessRecheckRequired: false, notice: '' });
    } catch (error) {
      if (!this.current(epoch)) return;
      const status = Number(error.statusCode);
      if ([401, 403, 404].includes(status)) {
        this._loaded = false; this.clearProtected();
        this.setData({ loginRequired: status === 401, projectLoadFailed: status !== 401, error: status === 401 ? '' : '项目访问权限已变更，请重新加载或联系管理员。' });
        return;
      }
      this.setData({ loading: false, accessRecheckRequired: true, notice: '权限重验失败，未保存内容已保留，请重试。' });
    }
  },
  retryProjectAccess() { return this._loaded ? this.revalidateProjectAccess() : this.loadProjects(); },

  async fetchAllProjects() {
    // 甲第工作流阶段短标签（后端只给 stage 数字，pill 文案在前端映射）
    const stageLabels = { 2: '方案制作', 4: '待转发', 5: '待反馈', 6: '已归档' };
    const items = [], seen = new Set(); let cursor = '';
    do {
      const page = await api.listProjectNoteProjects({ limit: 50, cursor });
      for (const item of page.items || []) {
        if (seen.has(item.project_id)) continue;
        seen.add(item.project_id);
        items.push(Object.assign({}, item, { stage_label: stageLabels[item.stage] || '' }));
      }
      const next = String(page.next_cursor || '');
      if (!next || next === cursor) break;
      cursor = next;
    } while (items.length < 500);
    return items;
  },
  async loadProjects(suppliedEpoch) {
    const epoch = suppliedEpoch || (this._epoch = (this._epoch || 0) + 1);
    this.setData(Object.assign(blankProtected(), { loading: true, loginRequired: false, error: '', notice: '' }));
    try {
      const [projects, context] = await Promise.all([this.fetchAllProjects(), api.workbenchContext()]);
      if (!this.current(epoch)) return;
      const owners = context.owners || [];
      const choices = Array.isArray(context.owner_choices) ? context.owner_choices : [];
      const ownerChoices = owners.map((ownerID, index) => {
        const choice = choices.find(item => item && item.owner_id === ownerID);
        return { owner_id: ownerID, display_name: choice && choice.display_name || (owners.length === 1 ? '当前账号' : `项目归属 ${index + 1}`) };
      });
      this._loaded = true;
      this.setData({ projects, owners, ownerChoices, ownerID: owners[0] || '', ownerIndex: 0, loading: false });
      const selectedID = this._targetProjectID || this._selectedProjectID;
      if (selectedID) { this._targetProjectID = ''; if (this._targetView) { this._nextPageView = this._targetView; this._targetView = ''; } await this.openProject(selectedID, epoch); }
    } catch (error) {
      if (!this.current(epoch)) return;
      this._loaded = false;
      this.clearProtected();
      this.setData({ loading: false, projectLoadFailed: true, loginRequired: error.statusCode === 401, error: error.statusCode === 403 ? (error.message || '暂无私人项目管理权限，请联系管理员。') : '私人项目暂不可用，请稍后重试。' });
    }
  },

  setProjectName(event) { this.setData({ projectName: value(event) }); },
  setOwnerID(event) { const index = Number(event.detail.value) || 0, choice = this.data.ownerChoices[index]; this.setData({ ownerIndex: index, ownerID: choice && choice.owner_id || this.data.owners[index] || '' }); },
  setConfirmerChoice(event) { const index = Number(event.detail.value) || 0, member = this.data.confirmerChoices[index]; this.setData({ confirmerIndex: index, confirmerAccount: member ? member.actor_id : '' }); },
  setNoteTitle(event) { this.setData({ noteTitle: value(event) }); },
  setNoteBody(event) { if (!this.data.bodyReadOnly) { this._noteCursor = Number(event.detail && event.detail.cursor); this.setData({ noteBody: String(event.detail && event.detail.value || '') }); } },
  onNoteBodyBlur(event) { this._noteCursor = Number(event.detail && event.detail.cursor); },
  onNoteKeyboardHeight(event) { this.setData({ keyboardHeight: Math.max(0, Number(event.detail && event.detail.height) || 0) }); },
  onNoteGalleryChange(event) {
    this.setData({ galleryPositions: Object.assign({}, this.data.galleryPositions, { [event.currentTarget.dataset.key]: Number(event.detail.current) || 0 }) });
  },
  onContentGalleryChange(event) {
    if (this.data.previewPresentation) this.setData({ 'previewPresentation.galleryIndex': Number(event.detail.current) || 0 });
  },
  playContentVideo(event) {
    const key = String(event.currentTarget.dataset.playerKey || '');
    if (key) this.setData({ ['previewVideoStarted.' + key]: true });
  },
  mediaError(event) {
    const key = String(event.currentTarget.dataset.errorKey || '');
    if (key) this.setData({ ['previewMediaErrors.' + key]: '素材加载失败，可重试' });
  },
  retryMedia(event) {
    const id = String(event.currentTarget.dataset.mediaId || '');
    if (!id) return;
    this.setData({ noteMedia: (this.data.noteMedia || []).map(item => mediaID(item) === id ? Object.assign({}, item, { local_path: '', preview_error: '', preview_loading: false }) : item) }, () => this.previewNote());
  },
  videoState() {},
  onContentVideoMetadata() {},
  videoProgress() {},
  videoRate() {},
  videoFullscreen() {},
  toggleContentDescription() {},
  onResize(event) {
    const size = event.size || {}, windowInfo = { windowWidth: size.windowWidth, windowHeight: size.windowHeight };
    const previewMedia = fitNoteMedia(this.data.previewMedia, windowInfo), versionMedia = fitNoteMedia(this.data.versionMedia, windowInfo);
    this.setData({ previewMedia, versionMedia, previewMediaGroups: groupNoteImages(previewMedia, previewMedia, windowInfo), versionMediaGroups: groupNoteImages(versionMedia, versionMedia, windowInfo), previewBlocks: fitNoteBlocks(this.data.previewBlocks, previewMedia, windowInfo), versionBlocks: fitNoteBlocks(this.data.versionBlocks, versionMedia, windowInfo) });
  },
  onNoteVideoMetadata(event) {
    const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
    const previewMedia = noteVideoMetadata(this.data.previewMedia, event, windowInfo), versionMedia = noteVideoMetadata(this.data.versionMedia, event, windowInfo);
    this.setData({ previewMedia, versionMedia, previewMediaGroups: groupNoteImages(previewMedia, previewMedia, windowInfo), versionMediaGroups: groupNoteImages(versionMedia, versionMedia, windowInfo), previewBlocks: fitNoteBlocks(this.data.previewBlocks, previewMedia, windowInfo), versionBlocks: fitNoteBlocks(this.data.versionBlocks, versionMedia, windowInfo) });
  },
  editNote() {
    this._notePreviewRequest = (this._notePreviewRequest || 0) + 1;
    this.setData({ editorMode: 'edit', previewLoading: false, previewError: '', previewBody: '', previewBlocks: [], previewMedia: [], previewMediaGroups: [], galleryPositions: {}, keyboardHeight: 0 });
    if (this._visible !== false && typeof wx.pageScrollTo === 'function') wx.pageScrollTo({ scrollTop: this._editorScrollTop || 0, duration: 0 });
  },
  async previewNote() {
    const project = this.data.selectedProject;
    if (!project || this.data.pageView !== 'editor' || this.data.busy || this.data.previewLoading) return;
    const draft = this.noteDraft(), signature = this.noteFormSignature();
    const epoch = this._epoch, projectRequest = this._projectRequest;
    const request = this._notePreviewRequest = (this._notePreviewRequest || 0) + 1;
    this.setData({ editorMode: 'preview', previewLoading: true, previewError: '', previewBody: '', previewBlocks: [], previewMedia: [], previewMediaGroups: [], galleryPositions: {}, keyboardHeight: 0 });
    if (typeof wx.hideKeyboard === 'function') wx.hideKeyboard();
    this.scrollPage('preview');
    const current = () => this.current(epoch) && request === this._notePreviewRequest
      && this.data.pageView === 'editor' && this.data.editorMode === 'preview' && signature === this.noteFormSignature();
    try {
      // 预览复用固定版本的私有媒体与正文渲染，不写草稿或创建对外版本。
      const presentation = await this.fixedVersionPresentation(project.project_id, Object.assign({}, draft, { media: this.data.noteMedia }), epoch, projectRequest);
      if (!current() || !presentation) return;
      // 兜底：结构化正文若没渲染出块但仍有纯文本正文，按段落渲染，避免预览出现空白纸。
      let blocks = presentation.blocks || [];
      if (!blocks.length && draft.body_format === 'article_v2' && draft.body && String(draft.body).trim() && !/^\s*[{\[]/.test(String(draft.body))) {
        blocks = [{ kind: 'paragraph', text: String(draft.body), className: 'article-paragraph', key: 'preview-fallback' }];
      }
      // 图文/视频统一复用内容创作的阅读呈现（小红书图集 / 视频作品），纯文字仍用 note-document。
      const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
      const viewMedia = (this.data.noteMedia || []).map(item => ({ media_id: mediaID(item), kind: item.kind, url: item.local_path || '', width: item.width, height: item.height, caption: item.caption || '' }));
      const images = viewMedia.filter(item => item.kind === 'image');
      const videos = viewMedia.filter(item => item.kind === 'video');
      let previewPresentation = null;
      if (videos.length) {
        const poster = images[0];
        previewPresentation = buildContentPresentation({ title: draft.title || '', body: draft.body || '', body_format: draft.body_format || 'plain', presentation: 'video', cover: poster ? { media_id: poster.media_id, url: poster.url } : null, media: [videos[0]].concat(images) }, [videos[0]].concat(images), windowInfo, { prefix: 'note-preview' });
      } else if (images.length) {
        previewPresentation = buildContentPresentation({ title: draft.title || '', body: draft.body || '', body_format: draft.body_format || 'plain', presentation: 'gallery', media: images }, images, windowInfo, { prefix: 'note-preview' });
      }
      this.setData({ previewLoading: false, previewBodyFormat: draft.body_format, previewBody: draft.body, previewBlocks: blocks, previewMedia: presentation.media, previewMediaGroups: presentation.groups, previewPresentation, previewMediaErrors: {}, previewVideoStarted: {} });
    } catch (error) {
      if (current()) this.setData({ previewLoading: false, previewError: error.message || '预览加载失败，内容已保留，请重试。' });
    }
  },
  // T2.2：重开草稿时按 media_id 补缩略图；只补目标项、并发去重、失败收尾，补下载路径不进入保存载荷。
  async hydrateNoteMedia(epoch, projectID, onlyID) {
    if (!projectID) return;
    const instanceID = this.currentDraftInstanceID();
    const sameDraft = () => this.current(epoch) && !!this.data.selectedProject
      && this.data.selectedProject.project_id === projectID && this.currentDraftInstanceID() === instanceID;
    this._hydratingIDs = this._hydratingIDs || new Set();
    // T3.1：本地文件失效时先清空路径，走单项重下；仍可读则直接复用。
    const stale = (this.data.noteMedia || []).filter(item => item.local_path && !localFileUsable(item.local_path));
    if (stale.length) {
      const staleIds = new Set(stale.map(mediaID));
      this.setData({ noteMedia: this.data.noteMedia.map(item => staleIds.has(mediaID(item)) ? Object.assign({}, item, { local_path: '', preview_error: '', preview_loading: false }) : item) });
    }
    // private = 只有 media_id、没有类型明细的非封面项：下载后用文件扩展名恢复类型。
    const targets = (this.data.noteMedia || []).filter(item => !item.local_path && (item.kind === 'image' || item.kind === 'video' || item.kind === 'private') && mediaID(item)
      && (!onlyID || mediaID(item) === onlyID) && !this._hydratingIDs.has(instanceID + ':' + mediaID(item)));
    for (const item of targets) {
      const id = mediaID(item);
      // 每项发起前重新核对：可能已被单项重试处理或已补到路径。
      const live = (this.data.noteMedia || []).find(entry => mediaID(entry) === id);
      const key = instanceID + ':' + id;
      if (!live || live.local_path || this._hydratingIDs.has(key)) continue;
      this._hydratingIDs.add(key);
      this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => mediaID(entry) === id ? Object.assign({}, entry, { preview_loading: true, preview_error: '' }) : entry) });
      let update;
      try {
        const path = await api.downloadProjectNoteMedia(projectID, id);
        if (item.kind === 'private') {
          const kind = inferNoteMediaKind(path, 'private');
          update = kind === 'private'
            ? { local_path: '', preview_error: '', preview_loading: false }
            : { local_path: path, kind, preview_error: '', preview_loading: false };
        } else update = { local_path: path, preview_error: '', preview_loading: false };
      }
      catch (error) {
        const status = Number(error && error.statusCode);
        this._hydratingIDs.delete(key);
        // 媒体鉴权终态：与上传/重验同口径，仅当前身份才清空并终止；旧身份迟到响应不得动新状态。
        if ([401, 403].includes(status)) {
          if (sameDraft()) this.handleNoteMediaAccessFailure(status);
          else {
            this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => mediaID(entry) === id ? Object.assign({}, entry, { preview_loading: false }) : entry) });
            if (this._hydrateResumePending) { this._hydrateResumePending = false; Promise.resolve().then(() => this.resumeNoteMediaPreviews()); }
          }
          return;
        }
        update = { preview_error: item.kind === 'video' ? '视频加载失败，可重试' : '素材暂时无法显示', preview_loading: false };
      }
      this._hydratingIDs.delete(key);
      // 跨前后台/切页后 epoch 变化：不写旧页面，但要把占位收尾，等回前台重新补下载。
      if (!sameDraft()) {
        this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => mediaID(entry) === id ? Object.assign({}, entry, { preview_loading: false, local_path: entry.local_path || '' }) : entry) });
        if (this._hydrateResumePending) { this._hydrateResumePending = false; Promise.resolve().then(() => this.resumeNoteMediaPreviews()); }
        return;
      }
      this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => mediaID(entry) === id ? Object.assign({}, entry, update) : entry) });
    }
    if (this._hydrateResumePending && this._hydratingIDs.size === 0) {
      this._hydrateResumePending = false;
      Promise.resolve().then(() => this.resumeNoteMediaPreviews());
    }
  },
  // 回前台/重验后恢复未完成的缩略图下载（含被 epoch 中断的「加载中」项）。
  resumeNoteMediaPreviews() {
    const project = this.data.selectedProject;
    if (!project || this.data.pageView !== 'editor') return;
    const items = this.data.noteMedia || [];
    const pending = items.filter(item => !item.local_path && (item.kind === 'image' || item.kind === 'video' || item.kind === 'private') && mediaID(item) && !item.preview_error);
    if (!pending.length) return;
    // 还有下载在途：登记待恢复，等它们收尾后再跑，避免被并发去重直接跳过。
    if (this._hydratingIDs && this._hydratingIDs.size) { this._hydrateResumePending = true; return; }
    const ids = new Set(pending.map(item => mediaID(item)));
    this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => ids.has(mediaID(entry)) ? Object.assign({}, entry, { preview_loading: false }) : entry) });
    void this.hydrateNoteMedia(this._epoch, project.project_id);
  },
  retryNoteMediaPreview(event) {
    const id = String(event.currentTarget.dataset.id || '');
    const project = this.data.selectedProject;
    const item = (this.data.noteMedia || []).find(entry => mediaID(entry) === id);
    if (!project || !item || this.data.busy) return;
    if (this._hydratingIDs && this._hydratingIDs.has(id)) return;
    this.setData({ noteMedia: this.data.noteMedia.map(entry => mediaID(entry) === id ? Object.assign({}, entry, { preview_error: '', preview_loading: false }) : entry) });
    void this.hydrateNoteMedia(this._epoch, project.project_id, id);
  },
  noteMediaLoadError(event) {
    const id = String(event.currentTarget.dataset.id || '');
    this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => mediaID(entry) === id ? Object.assign({}, entry, { local_path: '', preview_error: '素材暂时无法显示', preview_loading: false }) : entry) });
  },
  // T2.3：编辑态点击视频进入原生播放；不可用时明确提示。
  openNoteVideo(event) {
    const id = String(event.currentTarget.dataset.id || '');
    const item = (this.data.noteMedia || []).find(entry => mediaID(entry) === id);
    if (!item || !item.local_path) return;
    const failed = () => this.setData({ noteMedia: (this.data.noteMedia || []).map(entry => mediaID(entry) === id ? Object.assign({}, entry, { local_path: '', preview_error: '视频加载失败', preview_loading: false }) : entry) });
    if (typeof wx.previewMedia === 'function') { wx.previewMedia({ sources: [{ url: item.local_path, type: 'video' }], current: 0, fail: failed }); return; }
    failed();
  },
  openImage(event) {
    const id = String(event.currentTarget.dataset.id || '');
    const media = this.data.pageView === 'version' ? this.data.versionMedia : this.data.editorMode === 'preview' ? this.data.previewMedia : this.data.noteMedia;
    const images = (media || []).filter(item => item.kind === 'image' && item.local_path);
    const selected = images.find(item => mediaID(item) === id);
    if (!selected) return;
    wx.previewImage({ current: selected.local_path, urls: images.map(item => item.local_path), fail: () => this.setData({ error: '图片打开失败，请重新预览。' }) });
  },
  setFeedbackReply(event) { this.setData({ feedbackReply: value(event) }); },

  setProjectField(event) {
    const field = String(event.currentTarget.dataset.field || '');
    if (!['name', 'location', 'building_area', 'floors', 'style'].includes(field)) return;
    this.setData({ projectEdit: Object.assign({}, this.data.projectEdit, { [field]: value(event) }) });
  },
  async saveProjectDetails() {
    const project = this.data.selectedProject, form = this.data.projectEdit;
    if (!project || this.data.busy || !String(form.name || '').trim()) return;
    const area = Number(form.building_area || 0), floors = Number(form.floors || 0);
    if (!Number.isFinite(area) || area < 0 || !Number.isInteger(floors) || floors < 0) { this.setData({ error: '建筑面积须为非负数，层数须为非负整数。' }); return; }
    return this.updateProjectDetails({ name: form.name.trim(), location: String(form.location || '').trim(), building_area: area, floors, style: String(form.style || '').trim() });
  },
  async markProjectRevising() {
    const projectChanged = this._projectFormBaseline != null && this._projectFormBaseline !== this.projectFormSignature();
    if (!await this.confirmDiscardChanges(projectChanged)) return;
    return this.updateProjectDetails({ status: 'revising' });
  },
  async updateProjectDetails(patch) {
    const project = this.data.selectedProject; if (!project || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = await api.updateProjectNoteProject(project.project_id, Object.assign({}, patch, { expected_revision: project.revision }));
      if (!this.current(epoch) || this._selectedProjectID && this._selectedProjectID !== project.project_id) return;
      this.setData({ selectedProject: saved, projectEdit: projectForm(saved), projectStatusLabel: statusLabels[saved.status] || '初步方案', projects: this.data.projects.map(item => item.project_id === saved.project_id ? saved : item), busy: false, notice: '项目资料已保存，已冻结版本仍保留原有资料。' }, () => {
        this._projectFormBaseline = this.projectFormSignature();
        if (this._loadedFormState) this._loadedFormState.projectEdit = Object.assign({}, this.data.projectEdit);
      });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '项目资料保存失败，请重新载入后重试' }); }
  },
  async createProjectNoteProject() {
    if (this.data.busy || this.data.loading || this.data.projectLoadFailed) return;
    if (!this.data.ownerID) { this.setData({ error: '暂无可用的项目归属，请返回重新加载。' }); return; }
    if (!this.data.projectName) { this.setData({ error: '请填写项目名称。' }); return; }
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const project = await api.createProjectNoteProject({ name: this.data.projectName, owner_id: this.data.ownerID });
      if (this.current(epoch)) { this._selectedProjectID = project.project_id; this._selectedNoteID = ''; this._selectedVersionID = ''; this._newNote = false; this.setData({ projects: [project, ...this.data.projects], projectName: '', busy: false }); this.applyProjectDetail({ project }, '项目已创建。', null, null, 'detail'); }
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '项目创建失败' }); }
  },
  async selectProject(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if (this._projectLongPress && this._projectLongPress.id === id && Date.now() < this._projectLongPress.until) { this._projectLongPress = null; return; }
    if (this.data.busy || !await this.confirmDiscardChanges()) return;
    this._selectedNoteID = ''; this._newNote = false; this._targetVersionID = ''; this._selectedVersionID = '';
    return this.openProject(id, this._epoch);
  },
  openProjectDeleteMenu(event) {
    const id = String(event.currentTarget.dataset.id || ''), project = this.data.projects.find(item => item.project_id === id);
    if (!project || this.data.busy) return Promise.resolve();
    const launch = { epoch: this._epoch, token: this._token, id, revision: Number(project.revision) };
    this._projectLongPress = { id, until: Date.now() + 1000 };
    return new Promise(resolve => wx.showActionSheet({
      itemList: ['删除项目'],
      success: result => {
        if (result.tapIndex !== 0 || !this.current(launch.epoch) || this._token !== launch.token) { resolve(); return; }
        this.deleteProjectNoteProject(launch).then(resolve, resolve);
      },
      fail: resolve,
    }));
  },
  async deleteProjectNoteProject(launch) {
    const confirmed = await new Promise(resolve => wx.showModal({ title: '删除这个项目？', content: '删除后将从项目列表移除，原分享和邀请也会立即失效。', confirmText: '删除项目', confirmColor: '#a33b31', success: result => resolve(result.confirm === true), fail: () => resolve(false) }));
    const current = this.data.projects.find(item => item.project_id === launch.id);
    if (!confirmed || !this.current(launch.epoch) || this._token !== launch.token || !current || Number(current.revision) !== launch.revision || this.data.busy) return;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      await api.deleteProjectNoteProject(launch.id, launch.revision);
      if (!this.current(launch.epoch) || this._token !== launch.token) return;
      this.setData({ projects: this.data.projects.filter(item => item.project_id !== launch.id), busy: false, notice: '项目已删除。' });
    } catch (error) {
      if (!this.current(launch.epoch) || this._token !== launch.token) return;
      const message = Number(error && error.statusCode) === 409 ? '项目已更新，请刷新后再删除。' : error.message || '项目删除失败';
      this.setData({ busy: false, error: message });
    }
  },
  async openProject(projectID, epoch, notice) {
    if (!projectID) return;
    const request = this._projectRequest = (this._projectRequest || 0) + 1;
    const requested = this._targetVersionID || this._selectedVersionID || '';
    const requestedView = this._nextPageView || (requested ? 'version' : ['editor', 'members', 'history'].includes(this.data.pageView) ? this.data.pageView : 'detail');
    this._nextPageView = '';
    this._selectedProjectID = projectID; this._selectedVersionID = requested; this._targetVersionID = '';
    const kept = { projects: this.data.projects, owners: this.data.owners, ownerChoices: this.data.ownerChoices, ownerID: this.data.ownerID, ownerIndex: this.data.ownerIndex };
    this.setData(Object.assign(blankProtected(), kept, { busy: true, error: '', notice: notice || '' }));
    try {
      const result = await api.getProjectNoteProject(projectID);
      if (!this.current(epoch) || request !== this._projectRequest) return;
      let fixed = null;
      if (requested) {
        fixed = await api.getProjectNoteVersion(projectID, requested);
        if (!this.current(epoch) || request !== this._projectRequest) return;
        if (fixed.project_id !== projectID || fixed.version_id !== requested) throw new Error('固定版本不匹配');
      }
      if (!requested && this._selectedNoteID && !this._newNote && !(result.notes || []).some(item => item.note_id === this._selectedNoteID)) {
        const note = await api.getProjectNote(projectID, this._selectedNoteID);
        if (!this.current(epoch) || request !== this._projectRequest) return;
        if (note.project_id !== projectID || note.note_id !== this._selectedNoteID) throw new Error('笔记不匹配');
        result.notes = [...(result.notes || []), note];
      }
      if (this._targetFeedbackID && !(result.feedbacks || []).some(item => item.feedback_id === this._targetFeedbackID)) {
        const feedback = await api.getProjectNoteFeedback(projectID, this._targetFeedbackID);
        if (!this.current(epoch) || request !== this._projectRequest) return;
        if (feedback.project_id !== projectID || requested && feedback.version_id !== requested) throw new Error('反馈不匹配');
        result.feedbacks = [...(result.feedbacks || []), feedback];
      }
      const presentation = fixed ? await this.fixedVersionPresentation(projectID, fixed, epoch, request) : null;
      if (this.current(epoch) && request === this._projectRequest) {
        this.applyProjectDetail(result, notice, fixed, presentation, requestedView);
        if (requestedView === 'members') await this.loadJoinRequests();
      }
    } catch (_) {
      if (!this.current(epoch) || request !== this._projectRequest) return;
      this.clearProtected(); this.setData(Object.assign({}, kept, { error: requested ? '目标固定版本暂不可访问，未切换到其他版本。' : '私人项目暂不可访问。', busy: false }));
    }
  },
  async fixedVersionPresentation(projectID, fixed, epoch, request) {
    const assets = [], seen = new Set();
    for (const asset of [fixed.cover, ...(fixed.media || [])]) {
      if (asset && !seen.has(asset.media_id)) { seen.add(asset.media_id); assets.push(asset); }
    }
    const media = new Array(assets.length); let cursor = 0, failed = false;
    const consume = async () => {
      while (cursor < assets.length) {
        if (failed || !this.current(epoch) || request !== this._projectRequest) return;
        const index = cursor++, asset = assets[index];
        if (asset.local_path && localFileUsable(asset.local_path)) { media[index] = Object.assign({}, asset); continue; }
        try { media[index] = Object.assign({}, asset, { local_path: await api.downloadProjectNoteMedia(projectID, asset.media_id) }); }
        catch (error) { failed = true; throw error; }
      }
    };
    await Promise.all([consume(), consume()]);
    if (!this.current(epoch) || request !== this._projectRequest) return null;
    const windowInfo = wx.getWindowInfo ? wx.getWindowInfo() : wx.getSystemInfoSync();
    const fittedMedia = fitNoteMedia(media, windowInfo);
    const blocks = fixed.body_format === 'article_v2' ? fitNoteBlocks(renderArticleDocument(fixed.body, fittedMedia, windowInfo.windowWidth * 636 / 750, 'fixed-note'), fittedMedia, windowInfo) : [];
    return { media: fittedMedia, blocks, groups: groupNoteImages(fittedMedia, fittedMedia, windowInfo) };
  },
  applyProjectDetail(result, notice, fixed, presentation, requestedView) {
    const project = result.project || result, notes = result.notes || [], versions = result.versions || [], shares = result.shares || [];
    const requested = this._selectedVersionID || this._targetVersionID;
    if (fixed) { this._selectedNoteID = fixed.note_id; this._newNote = false; }
    const selectedNote = this._selectedNoteID ? notes.find(item => item.note_id === this._selectedNoteID) : null;
    const defaultVersion = result.latest_version || versions[0] || null;
    const defaultNote = defaultVersion ? notes.find(item => item.note_id === defaultVersion.note_id) || null : null;
    const note = requested || this._newNote ? null : selectedNote || (this._selectedNoteID ? null : defaultNote || notes[0] || result.note || null);
    if (note) this._selectedNoteID = note.note_id;
    const noteID = fixed && fixed.note_id || note && note.note_id || this._selectedNoteID;
    const versionChoices = this._newNote ? [] : versions.filter(item => item.note_id === noteID).map(item => Object.assign({}, item, { label: 'V' + item.revision + ' · ' + (item.title || '方案') }));
    const version = fixed || (requested || this._newNote ? null : versionChoices[0] || (!noteID ? defaultVersion : null));
    const targetID = version && version.version_id || requested;
    const replies = new Map((result.resolutions || []).map(item => [item.feedback_id, item.reply || '']));
    const feedbacks = targetID ? (result.feedbacks || []).filter(item => item.version_id === targetID).map(item => Object.assign({}, item, { satisfaction_label: satisfactionLabels[item.satisfaction] || '', resolution_reply: item.reply || replies.get(item.feedback_id) || '', created_at: localTime(item.created_at) })) : [];
    const selectedFeedback = feedbacks.find(item => item.feedback_id === this._targetFeedbackID) || null;
    let share = targetID && result.active_share && result.active_share.version_id === targetID ? result.active_share : targetID ? shares.find(item => !item.revoked && item.version_id === targetID) || null : null;
    if (share && this._issuedShare && this._issuedShare.share_token && share.share_id === this._issuedShare.share_id) share = Object.assign({}, share, { share_token: this._issuedShare.share_token });
    if (share && !share.share_token) { const saved = cachedShareToken(share.share_id); if (saved) share = Object.assign({}, share, { share_token: saved }); }
    const members = (result.members || []).map(memberAttribution), available = activeMembers(members), foundIndex = available.findIndex(item => Number(item.actor_id) === Number(project.confirmer_id));
    const confirmerChoices = [{ actor_id: '', label: '选择确认人' }, ...available.map(item => Object.assign({}, item, { label: memberLabel(item) }))];
    const bodyReadOnly = !!note && note.body_format !== 'plain' && note.body_format !== 'plain_text';
    const historyNotice = requested ? '通知对应的固定版本，草稿不会在此修改。' : version && !note ? '历史草稿未加载，仅显示版本摘要。' : '';
    const pageView = requestedView || (requested || fixed ? 'version' : 'detail');
    const navigationTitles = { detail: '项目详情', editor: note ? '编辑笔记' : '新建笔记', version: '方案版本', members: '项目成员', history: '项目记录' };
    this._targetVersionID = ''; this._selectedProjectID = project.project_id;
    this._loaded = true;
    this.setData({
      pageView, navigationTitle: navigationTitles[pageView] || project.name || '项目详情', projectSettingsOpen: false,
      selectedProject: project, projectEdit: projectForm(project), projectStatusLabel: statusLabels[project.status] || '初步方案', notes, noteIndex: Math.max(0, notes.findIndex(item => item.note_id === noteID)), versionChoices, members, activeMembers: available, confirmerChoices,
      confirmerIndex: foundIndex < 0 ? 0 : foundIndex + 1, confirmerAccount: foundIndex < 0 ? '' : available[foundIndex].actor_id,
      note, noteTitle: note && note.title || '', noteBody: note && note.body || '', bodyReadOnly,
      noteMedia: note ? ((note.media && note.media.length ? note.media.map(assetOf) : (note.media_ids || []).map(id => ({ media_id: id, kind: id === note.cover_media_id ? 'image' : 'private' }))).map(item => Object.assign({}, item, { media_id: mediaID(item) }))) : [],
      frozenVersion: version, activeShare: share,
      historyMode: !!requested, versionView: fixed || null, versionMedia: presentation && presentation.media || [], versionBlocks: presentation && presentation.blocks || [],
      versionMediaGroups: presentation && presentation.groups || [], galleryPositions: {},
      feedbacks, selectedFeedback, feedbackID: selectedFeedback && selectedFeedback.feedback_id || '', feedbackReply: '',
      confirmations: targetID ? (result.confirmations || []).filter(item => item.version_id === targetID).map(item => Object.assign({}, item, { confirmed_at: localTime(item.confirmed_at) })) : [],
      truncationNotice: truncationText(result.truncated), historyNotice, busy: false, notice: notice || this.data.notice,
      error: share && !share.share_token && pageView === 'version' ? '分享凭证未同步，如需预览请重新生成分享。' : '',
    }, () => {
      // 先用服务端结果建立未修改基线，再接纳后台素材，使其保持「未保存」状态。
      this._noteNewCoverID = '';
      if (pageView === 'version' || pageView === 'members') void this.loadJoinRequests();
      if (pageView === 'editor') this.ensureDraftInstance();
      this.publishMediaFailures();
      this.rememberLoadedForms();
      if (pageView === 'editor') { this.restorePendingMedia(); this.acceptPendingMedia(); }
      if (pageView === 'editor') void this.hydrateNoteMedia(this._epoch, project.project_id);
      this.scrollPage(pageView);
    });
  },
  async setHistoryType(event) {
    if (this.data.busy) return;
    const index = Number(event.detail.value), type = this.data.historyTypes[index]; if (!type) return;
    this._historyRequest = (this._historyRequest || 0) + 1;
    this.setData({ historyType: type.key, historyIndex: index, historyItems: [], historyPage: 0, historyCursors: [''], historyNextCursor: '', historyLoaded: false, historyLoading: false });
    await this.loadHistory();
  },
  async loadHistory(event) {
    const project = this.data.selectedProject; if (!project || this.data.historyLoading || this.data.busy) return;
    const direction = event && event.currentTarget && event.currentTarget.dataset.direction;
    let page = this.data.historyPage;
    const cursors = this.data.historyCursors.slice();
    if (direction === 'next') { if (!this.data.historyNextCursor) return; page++; cursors[page] = this.data.historyNextCursor; }
    if (direction === 'previous') { if (!page) return; page--; }
    const epoch = this._epoch, type = this.data.historyType, request = this._historyRequest = (this._historyRequest || 0) + 1;
    this.setData({ historyLoading: true, error: '' });
    const current = () => this.current(epoch) && request === this._historyRequest && this.data.selectedProject && this.data.selectedProject.project_id === project.project_id;
    try {
      const result = await api.getProjectNoteHistory(project.project_id, type, { limit: 30, cursor: cursors[page] || '' });
      if (!current()) return;
      const items = (result.items || []).map((item, index) => Object.assign({}, item, {
        key: item.version_id || item.note_id || item.feedback_id || item.resolution_id || item.confirmation_id || String(index),
        displayTitle: item.title || (type === 'feedbacks' ? satisfactionLabels[item.satisfaction] || '业主意见' : type === 'resolutions' ? '设计师处理' : '业主确认'),
        displayText: item.text || item.reply || (item.revision ? 'V' + item.revision : '已确认此方案版本'),
        displayTime: localTime(item.frozen_at || item.resolved_at || item.confirmed_at || item.updated_at || item.created_at),
      }));
      this.setData({ historyItems: items, historyPage: page, historyCursors: cursors, historyNextCursor: result.next_cursor || '', historyLoaded: true, historyLoading: false });
    } catch (error) { if (current()) this.setData({ historyLoading: false, error: error.message || '历史记录加载失败，可重试' }); }
  },
  async openHistoryItem(event) {
    const item = this.data.historyItems[Number(event.currentTarget.dataset.index)];
    if (!item || this.data.busy || !await this.confirmDiscardChanges()) return;
    this._targetFeedbackID = this.data.historyType === 'feedbacks' ? item.feedback_id : '';
    if (this.data.historyType === 'notes') { this._selectedNoteID = item.note_id; this._newNote = false; this._selectedVersionID = ''; this._targetVersionID = ''; this._nextPageView = 'editor'; }
    else if (item.version_id) { this._selectedVersionID = item.version_id; this._nextPageView = 'version'; }
    else return;
    await this.refreshProject();
  },
  async selectNote(event) {
    if (this.data.busy) return;
    const note = this.data.notes[Number(event.detail.value)]; if (!note) return;
    if (!await this.confirmDiscardChanges()) return;
    // 已确认放弃当前编辑实例：即使之后回到同一笔记，也必须换新的草稿实例。
    this.resetDraftInstance();
    this._targetFeedbackID = ''; this._selectedNoteID = note.note_id; this._newNote = false; this._selectedVersionID = ''; this._targetVersionID = '';
    this._nextPageView = 'editor';
    return this.refreshProject();
  },
  async selectVersion(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if (this.data.busy || !this.data.versionChoices.some(item => item.version_id === id)) return;
    if (!await this.confirmDiscardChanges()) return;
    // 进入历史版本同样放弃当前编辑实例，returnToDraft 回来必须重新绑定。
    this.resetDraftInstance();
    this._targetFeedbackID = ''; this._selectedVersionID = id; this._nextPageView = 'version';
    return this.refreshProject();
  },
  async startNewNote() {
    if (this.data.busy || !await this.confirmDiscardChanges()) return;
    this._newNote = true; this._selectedNoteID = ''; this._selectedVersionID = ''; this._targetVersionID = ''; this._shareAttempt = null;
    this.editNote(); this._editorScrollTop = 0;
    this.setData({ pageView: 'editor', navigationTitle: '新建笔记', projectSettingsOpen: false, note: null, noteTitle: '', noteBody: '', noteMedia: [], bodyReadOnly: false, historyMode: false, versionView: null, versionMedia: [], versionBlocks: [], historyNotice: '', feedbacks: [], confirmations: [], selectedFeedback: null, feedbackID: '', feedbackReply: '' }, () => { this.ensureDraftInstance(true); this.publishMediaFailures(); this._noteFormBaseline = this.noteFormSignature(); this.scrollPage('editor'); });
  },
  openNoteMore() {
    if (!this.data.note || this.data.busy || this.data.historyMode) return;
    wx.showActionSheet({
      itemList: ['复制笔记', '删除笔记'],
      success: result => {
        if (result.tapIndex === 0) this.copyProjectNote();
        if (result.tapIndex === 1) this.deleteProjectNote();
      },
    });
  },
  openNoteDeleteMenu(event) {
    const id = String(event.currentTarget.dataset.id || ''), note = this.data.notes.find(item => item.note_id === id);
    if (!note || this.data.busy) return Promise.resolve();
    const launch = { epoch: this._epoch, token: this._token, id, revision: Number(note.revision) };
    this._noteLongPress = { id, until: Date.now() + 1000 };
    return new Promise(resolve => wx.showActionSheet({
      itemList: ['删除笔记'],
      success: result => {
        if (result.tapIndex !== 0 || !this.current(launch.epoch) || this._token !== launch.token) { resolve(); return; }
        this.deleteProjectNote(note, true, launch).then(resolve, resolve);
      },
      fail: resolve,
    }));
  },
  selectFeedback(event) {
    const feedback = this.data.feedbacks.find(item => item.feedback_id === String(event.currentTarget.dataset.id || ''));
    if (!feedback || this.data.busy) return;
    this._resolutionAttempt = null;
    this.setData({ selectedFeedback: feedback, feedbackID: feedback.feedback_id, feedbackReply: feedback.resolution_reply || '' });
  },
  async refreshProject(notice) { const project = this.data.selectedProject; if (project) await this.openProject(project.project_id, this._epoch, notice); },
  async returnToDraft() { const project = this.data.selectedProject; if (!project || this.data.busy || !await this.confirmDiscardChanges()) return; this._targetVersionID = ''; this._selectedVersionID = ''; this._nextPageView = 'editor'; await this.openProject(project.project_id, this._epoch); },
  async exportProjectNoteWord() {
    const project = this.data.selectedProject, version = this.data.frozenVersion;
    if (!project || !version || this.data.busy) return;
    const epoch = this._epoch;
    const selected = () => this.current(epoch) && this.data.selectedProject && this.data.selectedProject.project_id === project.project_id && this.data.frozenVersion && this.data.frozenVersion.version_id === version.version_id;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      const filePath = await api.downloadProjectNoteWord(project.project_id, version.version_id);
      if (!selected()) return;
      await new Promise((resolve, reject) => wx.openDocument({ filePath, fileType: 'docx', showMenu: true, success: resolve, fail: () => reject(new Error('Word 文件打开失败，请重试')) }));
      if (selected()) this.setData({ busy: false, notice: '已打开所选固定版本的 Word 文件。' });
    } catch (error) { if (selected()) this.setData({ busy: false, error: error.message || 'Word 导出失败' }); }
  },

  async loadJoinRequests() {
    const project = this.data.selectedProject;
    if (!project || this.data.requestsLoading) return;
    const epoch = this._epoch, projectID = project.project_id;
    const request = this._joinRequestsRequest = (this._joinRequestsRequest || 0) + 1;
    this._joinRequestsRetry = false;
    this.setData({ requestsLoading: true });
    try {
      const [result, invitations] = await Promise.all([api.listProjectJoinRequests(projectID), api.listProjectInvitations(projectID)]);
      if (!this.current(epoch) || request !== this._joinRequestsRequest || !this.data.selectedProject || this.data.selectedProject.project_id !== projectID) { if (request === this._joinRequestsRequest && this.data.requestsLoading) this.setData({ requestsLoading: false }); return; }
      this.setData({
        joinRequests: (result.items || []).filter(item => item.status === 'pending'),
        memberInvitations: (invitations.items || []).map(item => Object.assign({}, item, { expiryLabel: invitationLabel(item) })),
        requestsLoading: false,
      });
      if (this._joinRequestsRetry && this.current(epoch) && !this.data.requestsLoading) { this._joinRequestsRetry = false; void this.loadJoinRequests(); }
    } catch (error) {
      if (request === this._joinRequestsRequest && this.data.requestsLoading) {
        this.setData({ requestsLoading: false });
        if (this.current(epoch) && this.data.selectedProject && this.data.selectedProject.project_id === projectID
          && (this.data.pageView === 'version' || this.data.pageView === 'members')) this.setData({ error: (error && error.message) || '加入申请加载失败' });
      }
    }
  },
  async createProjectInvitation() {
    const project = this.data.selectedProject;
    if (!project || this.data.busy) return;
    if (!this._invitationAttempt || this._invitationAttempt.projectID !== project.project_id) {
      this._invitationAttempt = { projectID: project.project_id, expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(), key: api.mutationKey('project-member-invitation') };
    }
    const attempt = this._invitationAttempt, epoch = this._epoch;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      const invitation = await api.createProjectInvitation(project.project_id, attempt.expiresAt, attempt.key);
      if (!this.current(epoch) || !this.data.selectedProject || this.data.selectedProject.project_id !== project.project_id) return;
      if (!invitation.invitation_token) { this.setData({ busy: false, error: '邀请链接未生成，请重试。' }); return; }
      this._invitationAttempt = null;
      const summary = { invitation_id: invitation.invitation_id, status: 'active', expires_at: invitation.expires_at, expiryLabel: invitationLabel(invitation) };
      const memberInvitations = [summary, ...this.data.memberInvitations.filter(item => item.invitation_id !== summary.invitation_id)];
      this.setData({ busy: false, memberInvitation: invitation, memberInvitationPath: '/pages/projectinvite/projectinvite?pni=' + encodeURIComponent(invitation.invitation_token), memberInvitations, notice: '邀请已生成。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '邀请生成失败' }); }
  },
  async revokeProjectInvitation(event) {
    const project = this.data.selectedProject;
    const invitationID = String(event && event.currentTarget && event.currentTarget.dataset.id || this.data.memberInvitation && this.data.memberInvitation.invitation_id || '');
    if (!project || !invitationID || this.data.busy) return;
    const epoch = this._epoch;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      await api.revokeProjectInvitation(project.project_id, invitationID);
      if (!this.current(epoch)) return;
      const clearsShare = this.data.memberInvitation && this.data.memberInvitation.invitation_id === invitationID;
      this.setData({
        busy: false,
        memberInvitation: clearsShare ? null : this.data.memberInvitation,
        memberInvitationPath: clearsShare ? '' : this.data.memberInvitationPath,
        memberInvitations: this.data.memberInvitations.filter(item => item.invitation_id !== invitationID),
        notice: '邀请已撤销。',
      });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '邀请撤销失败' }); }
  },
  async revokeAllProjectInvitations() {
    if (this.data.busy || !(this.data.memberInvitations || []).length) return;
    const items = this.data.memberInvitations.slice(), epoch = this._epoch;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      for (const item of items) await api.revokeProjectInvitation(this.data.selectedProject && this.data.selectedProject.project_id, item.invitation_id);
      if (!this.current(epoch)) return;
      this.setData({ memberInvitations: [], busy: false, notice: '有效邀请已全部撤销。' });
    } catch (error) {
      if (this.current(epoch)) this.setData({ busy: false, error: error.message || '撤销邀请失败' });
    }
  },
  async approveProjectJoinRequest(event) {
    const project = this.data.selectedProject, requestID = String(event && event.currentTarget.dataset.id || '');
    if (!project || !requestID || this.data.busy) return;
    if (!this._approvalAttempts) this._approvalAttempts = new Map();
    if (!this._approvalAttempts.has(requestID)) this._approvalAttempts.set(requestID, api.mutationKey('project-member-approval'));
    const key = this._approvalAttempts.get(requestID), epoch = this._epoch;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      await api.approveProjectJoinRequest(project.project_id, requestID, key);
      if (!this.current(epoch)) return;
      this._approvalAttempts.delete(requestID);
      this.setData({ busy: false });
      await this.refreshProject('已批准加入项目。');
      if (this.current(epoch)) await this.openMembers();
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '批准失败' }); }
  },
  async revokeProjectNoteMember(event) { return this.memberMutation(event && event.currentTarget.dataset.account); },
  async memberMutation(supplied) {
    const project = this.data.selectedProject, account = Number(supplied);
    if (!project || !Number.isInteger(account) || account <= 0 || this.data.busy) return;
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      await api.revokeProjectNoteMember(project.project_id, account);
      if (!this.current(epoch)) return;
      this.setData({ busy: false }); await this.refreshProject('访问权限已移除。');
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '成员设置失败' }); }
  },
  async setProjectNoteConfirmer() {
    const project = this.data.selectedProject, account = Number(this.data.confirmerAccount);
    if (!project || !Number.isInteger(account) || account <= 0 || this.data.busy) return;
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try { await api.setProjectNoteConfirmer(project.project_id, account); if (!this.current(epoch)) return; this.setData({ busy: false }); await this.refreshProject('指定业主已设为方案确认人。'); }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '确认人设置失败' }); }
  },

  noteDraft() {
    const note = this.data.note;
    if (note && this.data.bodyReadOnly) return { title: this.data.noteTitle, body: note.body, body_format: note.body_format, cover_media_id: note.cover_media_id || '', media_ids: (note.media_ids || []).slice(), expected_revision: note.revision || 0 };
    const items = this.data.noteMedia || [];
    const ids = items.map(mediaID).filter(Boolean);
    const firstImage = items.find(item => item.kind === 'image');
    const savedCover = String(note && note.cover_media_id || '');
    let coverID = '';
    if (note && savedCover) {
      // 已保存封面仍存在则保留；用户删掉封面后才回退首张图片。
      coverID = ids.includes(savedCover) ? savedCover : (firstImage ? mediaID(firstImage) : '');
    } else if (note) {
      // 已保存但没有封面：补缩略图的类型推导不得生成封面；仅用户新增素材后默认取首张。
      coverID = this._noteNewCoverID && ids.includes(this._noteNewCoverID) ? this._noteNewCoverID : '';
    } else {
      coverID = firstImage ? mediaID(firstImage) : '';
    }
    return { title: this.data.noteTitle, body: this.data.noteBody, body_format: 'plain', cover_media_id: coverID, media_ids: ids, expected_revision: note && note.revision || 0 };
  },
  noteWritingSignature() { return JSON.stringify({ project: this.data.selectedProject, note: this.noteDraft() }); },
  newDraftInstanceID() { return 'draft-' + Date.now().toString(36) + '-' + (this._draftSequence = (this._draftSequence || 0) + 1); },
  draftInstanceAccount() { return draftAccount(); },
  ensureDraftInstance(force) {
    const project = this.data.selectedProject;
    if (!project) { this._draftInstance = null; return ''; }
    const note = this.data.note, account = draftAccount();
    const noteID = this._newNote ? '' : String(note && note.note_id || '');
    const revision = this._newNote ? 0 : Number(note && note.revision || 0);
    const current = this._draftInstance;
    if (!force && current && current.account === account && current.projectID === project.project_id && current.noteID === noteID && current.revision === revision) return current.id;
    this._draftInstance = { id: this.newDraftInstanceID(), account, projectID: project.project_id, noteID, revision };
    return this._draftInstance.id;
  },
  currentDraftInstanceID() { return this._draftInstance ? this._draftInstance.id : ''; },
  resetDraftInstance() {
    this._noteCursor = -1; this._draftInstance = null; },
  draftInstanceMatches(instanceID) {
    return !instanceID || !!(this._draftInstance && this._draftInstance.id === instanceID);
  },
  // T4.1/T4.2：编辑页就地 AI 面板，不跳页；结果插入光标处并受 8000 字与身份约束。
  openNoteAIWriting() {
    const project = this.data.selectedProject, note = this.data.note;
    if (!project || this.data.busy || this.data.historyMode || this.data.bodyReadOnly) return;
    const instanceID = this.ensureDraftInstance();
    const launch = { token: this._token, projectID: project.project_id, noteID: note && note.note_id || '', revision: note && note.revision || 0, instanceID,
      structure: this.noteAIStructureSignature(), request: (this._aiPanelRequest = (this._aiPanelRequest || 0) + 1) };
    this._aiPanelLaunch = launch;
    this._aiPanelApplied = '';
    // 兼容：既有 ack 协议调用方仍可通过 receiveNoteWriting(payload, this._writingLaunch) 回传。
    this._writingLaunch = { token: this._token, projectID: project.project_id, noteID: note && note.note_id || '', revision: note && note.revision || 0, signature: this.noteWritingSignature(), instanceID, state: '', message: '', channel: null };
    this.setData({ aiPanelOpen: true, aiPanelText: '', aiPanelError: '', aiPanelNotice: '', aiPanelPrompt: '', aiPanelStreaming: false });
  },
  closeNoteAI() {
    this._aiPanelRequest = (this._aiPanelRequest || 0) + 1;
    this._aiPanelLaunch = null;
    this.abortNoteAI();
    this.setData({ aiPanelOpen: false, aiPanelStreaming: false });
  },
  setNoteAIPrompt(event) { this.setData({ aiPanelPrompt: String(event.detail && event.detail.value || '') }); },
  setNoteAIText(event) { this.setData({ aiPanelText: String(event.detail && event.detail.value || '') }); },
  stopPropagation() {},
  abortNoteAI() {
    if (this._aiPanelTask && typeof this._aiPanelTask.abort === 'function') { try { this._aiPanelTask.abort(); } catch (_) { /* 已结束 */ } }
    this._aiPanelTask = null;
  },
  // 结构签名只包含「不允许被 AI 结果改变」的字段（账号/项目/笔记/修订/实例/格式/封面/媒体）。
  noteAIStructureSignature() {
    const note = this.data.note;
    const ids = (this.data.noteMedia || []).map(mediaID).filter(Boolean);
    return JSON.stringify({
      project: this.data.selectedProject && this.data.selectedProject.project_id || '',
      note: note && note.note_id || '', revision: note && note.revision || 0,
      instance: this.currentDraftInstanceID(), format: note && note.body_format || 'plain',
      cover: this.noteDraft().cover_media_id || '', media: ids,
    });
  },
  noteAIPanelValid() {
    const project = this.data.selectedProject, launch = this._aiPanelLaunch;
    if (!project || !launch) return false;
    return launch.request === (this._aiPanelRequest || 0)
      && launch.token === this._token && launch.token === String(wx.getStorageSync('token') || '')
      && project.project_id === launch.projectID
      && String(this.data.note && this.data.note.note_id || '') === launch.noteID
      && Number(this.data.note && this.data.note.revision || 0) === Number(launch.revision || 0)
      && this.currentDraftInstanceID() === launch.instanceID
      && this.noteAIStructureSignature() === launch.structure
      && !this.data.historyMode && !this.data.bodyReadOnly;
  },
  noteAIFacts() {
    const project = this.data.selectedProject || {};
    return [project.name && '项目：' + project.name, project.location && '地点：' + project.location,
      project.building_area > 0 && '建筑面积：' + project.building_area + ' 平方米', project.floors > 0 && '层数：' + project.floors,
      project.style && '风格：' + project.style, this.data.noteBody].filter(Boolean).join('\n').slice(0, 6000);
  },
  startNoteAI() {
    if (this.data.aiPanelStreaming || !this.noteAIPanelValid()) return;
    const launch = this._aiPanelLaunch, token = this._token, facts = this.noteAIFacts();
    if (!facts) { this.setData({ aiPanelError: '请先填写项目资料或笔记正文，AI 将以这些事实为依据。' }); return; }
    const extra = String(this.data.aiPanelPrompt || '').trim();
    const prompt = noteWritingPrompt({ title: this.data.noteTitle, facts, extra });
    const request = launch.request;
    const current = () => request === this._aiPanelRequest && launch === this._aiPanelLaunch && !this._unloaded
      && token === this._token && token === String(wx.getStorageSync('token') || '') && this.noteAIPanelValid();
    this.setData({ aiPanelStreaming: true, aiPanelText: '', aiPanelError: '', aiPanelNotice: '正在生成…' });
    try {
      this._aiPanelTask = startAIStream({
        messages: [{ role: 'user', content: prompt }],
        onChunk: partial => { if (current()) this.setData({ aiPanelText: String(partial || '') }); },
        onDone: full => {
          if (!current()) return;
          this._aiPanelTask = null;
          const text = String(full || '').trim();
          this.setData({ aiPanelStreaming: false, aiPanelText: text, aiPanelNotice: text ? '已生成，可编辑后插入或复制。' : '生成为空，请调整资料后重试。' });
        },
        onError: message => {
          if (!current()) return;
          this._aiPanelTask = null;
          this.setData({ aiPanelStreaming: false, aiPanelError: message || '生成失败，请重试' });
        },
      });
    } catch (error) { this._aiPanelTask = null; this.setData({ aiPanelStreaming: false, aiPanelError: error.message || '生成失败，请重试' }); }
  },
  whenNoteIdle(launch) {
    if (!this.data.busy) return Promise.resolve(true);
    const epoch = this._epoch, started = Date.now();
    return new Promise(resolve => {
      const check = () => {
        // 关闭面板/切换草稿/超时后立即结束等待，不再轮询。
        if (this._unloaded || epoch !== this._epoch || !launch || launch !== this._aiPanelLaunch
          || !this.data.aiPanelOpen || this.currentDraftInstanceID() !== launch.instanceID) { resolve(false); return; }
        if (!this.data.busy) { resolve(true); return; }
        const timeoutMs = Number(this._aiIdleTimeoutMs) > 0 ? Number(this._aiIdleTimeoutMs) : 5000;
        if (Date.now() - started > timeoutMs) { resolve(false); return; }
        setTimeout(check, 120);
      };
      setTimeout(check, 120);
    });
  },
  async insertNoteAI() {
    let text = String(this.data.aiPanelText || '').trim();
    if (!text || this.data.aiPanelStreaming || !this.noteAIPanelValid()) return;
    if (this._aiPanelApplied && this._aiPanelApplied === text) { this.setData({ aiPanelNotice: '该结果已插入过，可继续编辑或复制。' }); return; }
    const launch = this._aiPanelLaunch;
    const idled = await this.whenNoteIdle(launch);
    if (!idled || launch !== this._aiPanelLaunch || !this.noteAIPanelValid()) {
      if (launch === this._aiPanelLaunch) this.setData({ aiPanelNotice: '笔记已变化，结果未插入，可复制后手动粘贴。' });
      return;
    }
    // 排队期间结果可能已被编辑或已采纳：等待后重新读取并二次去重。
    if (this.data.aiPanelStreaming) { this.setData({ aiPanelNotice: '正在生成，完成后可插入。' }); return; }
    const latest = String(this.data.aiPanelText || '').trim();
    if (!latest) return;
    if (this._aiPanelApplied && this._aiPanelApplied === latest) { this.setData({ aiPanelNotice: '该结果已插入过，可继续编辑或复制。' }); return; }
    text = latest;
    const existing = String(this.data.noteBody || '');
    const cursorNow = this._noteCursor >= 0 ? Math.max(0, Math.min(this._noteCursor, existing.length)) : existing.length;
    const separator = existing && cursorNow >= existing.length ? '\n\n' : '';
    const room = Math.max(0, 8000 - existing.length - separator.length);
    let inserted = room > 0 ? text.slice(0, room) : '';
    // 截断不得产生孤立代理字符。
    if (/[\uD800-\uDBFF]$/.test(inserted)) inserted = inserted.slice(0, -1);
    const cursor = cursorNow;
    let bodyValue;
    if (!inserted) bodyValue = existing;
    else if (cursor < existing.length) bodyValue = (existing.slice(0, cursor) + inserted + existing.slice(cursor)).slice(0, 8000);
    else bodyValue = (existing + separator + inserted).slice(0, 8000);
    const truncated = inserted.length < text.length;
    if (inserted) this._aiPanelApplied = text;
    this._noteCursor = cursor + separator.length + inserted.length;
    this.setData({
      noteBody: bodyValue,
      aiPanelNotice: inserted
        ? (truncated ? `已插入前 ${inserted.length} 字，剩余 ${text.length - inserted.length} 字可复制后手动补充。` : '已插入，可继续编辑。')
        : '正文已满 8000 字，请复制后手动替换。',
    });
  },
  copyNoteAI() { if (!this.noteAIPanelValid()) return; const text = String(this.data.aiPanelText || ''); if (text) wx.setClipboardData({ data: text }); },
  // T1.1 双向确认：回填结果分「已应用 / 暂时拒绝 / 目标失效」三类回传 AI 页。
  // 已应用的重复请求只重放成功确认，不重复追加；暂时拒绝可重试；目标失效须重新发起。
  receiveNoteWriting(payload, launch) {
    const ack = this.settleNoteWriting(payload, launch);
    if (launch && launch.channel && typeof launch.channel.emit === 'function') {
      try { launch.channel.emit('writingAck', ack); } catch (_) { /* 通道已关闭时由 AI 页超时兜底，文案留在 AI 页 */ }
    }
    return ack;
  },
  noteWritingAck(state, message, launch, payload, replayed) {
    return { state, message, replayed, request: Number(payload && payload.request) || 0, operation: String(payload && payload.operation || '') };
  },
  noteWritingPayloadKey(payload) {
    return JSON.stringify({ title: String(payload && payload.title || ''), text: String(payload && payload.text || '') });
  },
  // 已应用后的重放只看「目标是否仍是同一个」：正文已被本次采纳改过，不能再拿旧签名比对。
  sameAppliedNoteTarget(launch) {
    const note = this.data.note, project = this.data.selectedProject;
    return launch.token === String(wx.getStorageSync('token') || '')
      && !!project && project.project_id === launch.projectID
      && (note && note.note_id || '') === launch.noteID && (note && note.revision || 0) === launch.revision
      && this.draftInstanceMatches(launch.instanceID)
      && !this.data.historyMode && !this.data.bodyReadOnly;
  },
  settleNoteWriting(payload, launch) {
    const payloadKey = this.noteWritingPayloadKey(payload);
    const operation = String(payload && payload.operation || '');
    // 回执丢失后重进 AI 页的再次采纳：同一 operation 已在当前实例应用过，只重放成功确认。
    const recorded = this._appliedNoteWriting;
    if (recorded && operation && recorded.operation === operation
      && recorded.account === draftAccount() && recorded.projectID === launch.projectID && recorded.instanceID === launch.instanceID) {
      if (!this.sameAppliedNoteTarget(launch)) {
        launch.state = 'invalid';
        launch.message = '笔记、账号或版本已变化，AI 文案未采用，请重新发起。';
        this.setData({ notice: launch.message });
        return this.noteWritingAck('invalid', launch.message, launch, payload, false);
      }
      if (recorded.payloadKey === payloadKey) {
        const message = 'AI 文案已采纳过，本次不会重复追加。';
        this.setData({ notice: message });
        return this.noteWritingAck('applied', message, launch, payload, true);
      }
      launch.state = 'invalid';
      launch.message = '本次 AI 文案已采纳过，修改后的内容未采纳；请返回后重新发起。';
      this.setData({ notice: launch.message });
      return this.noteWritingAck('invalid', launch.message, launch, payload, false);
    }
    if (launch.state === 'applied') {
      // 账号/草稿实例/修订/受保护状态在应用后变化时，旧成功不得再重放。
      if (!this.sameAppliedNoteTarget(launch)) {
        launch.state = 'invalid';
        launch.message = '笔记、账号或版本已变化，AI 文案未采用，请重新发起。';
        this.setData({ notice: launch.message });
        return this.noteWritingAck('invalid', launch.message, launch, payload, false);
      }
      if (launch.appliedPayloadKey && launch.appliedPayloadKey !== payloadKey) {
        launch.state = 'invalid';
        launch.message = '本次 AI 文案已采纳过，修改后的内容未采纳；请返回后重新发起。';
        this.setData({ notice: launch.message });
        return this.noteWritingAck('invalid', launch.message, launch, payload, false);
      }
      const message = 'AI 文案已处理，本次重复采纳不会重复追加。';
      this.setData({ notice: message });
      return this.noteWritingAck('applied', message, launch, payload, true);
    }
    if (launch.state === 'invalid') return this.noteWritingAck('invalid', launch.message, launch, payload, true);
    const note = this.data.note;
    const accountChanged = launch.token !== String(wx.getStorageSync('token') || '');
    const targetChanged = !payload || payload.source !== 'project_note'
      || payload.project_id !== launch.projectID || payload.note_id !== launch.noteID || Number(payload.note_revision) !== launch.revision
      || !this.draftInstanceMatches(launch.instanceID) || (payload.draft_instance_id && payload.draft_instance_id !== (launch.instanceID || ''))
      || (note && note.note_id || '') !== launch.noteID || (note && note.revision || 0) !== launch.revision;
    // 目标失效（换账号/换草稿实例/修订变化）属永久拒绝，必须重新发起并绑定当前目标。
    if (accountChanged || targetChanged || this.data.historyMode || this.data.bodyReadOnly || this.noteWritingSignature() !== launch.signature) {
      launch.state = 'invalid';
      launch.message = accountChanged ? '账号已变化，AI 文案未采用；请用当前账号重新发起。' : '笔记、账号或版本已变化，AI 文案未采用，请重新发起。';
      this.setData({ notice: launch.message });
      return this.noteWritingAck('invalid', launch.message, launch, payload, false);
    }
    // 暂时拒绝（保存/处理中）不标记失效，解除后可再次采纳成功。
    if (this.data.busy) {
      const message = '笔记正在保存或处理，AI 文案暂未采用；稍后可再次采纳。';
      this.setData({ notice: message });
      return this.noteWritingAck('retryable', message, launch, payload, false);
    }
    const text = String(payload && payload.text || '').trim();
    if (!text || text === this.data.noteBody.trim()) {
      const message = '没有新的文案可追加，原笔记已保留。';
      this.setData({ notice: message });
      return this.noteWritingAck('retryable', message, launch, payload, false);
    }
    const body = this.data.noteBody ? this.data.noteBody + '\n\n' + text : text;
    if (body.length > 8000) {
      const message = '追加后超过笔记字数上限，AI 文案未采用，请先精简原文。';
      this.setData({ notice: message });
      return this.noteWritingAck('retryable', message, launch, payload, false);
    }
    launch.state = 'applied';
    launch.message = 'AI 文案已带回此笔记，请核对后保存新修订。';
    launch.appliedPayloadKey = payloadKey;
    this._appliedNoteWriting = { account: draftAccount(), projectID: launch.projectID, instanceID: launch.instanceID || '', operation, payloadKey };
    this.setData({ noteTitle: this.data.noteTitle || String(payload.title || '').trim(), noteBody: body, notice: launch.message });
    return this.noteWritingAck('applied', launch.message, launch, payload, false);
  },
  async copyProjectNote() {
    const note = this.data.note; if (!note || this.data.busy || this.data.historyMode) return;
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const copy = await api.copyProjectNote(note.note_id, note.revision);
      if (!this.current(epoch)) return;
      this._selectedNoteID = copy.note_id; this._newNote = false; this._selectedVersionID = '';
      this.setData({ busy: false });
      await this.refreshProject('已复制保存过的笔记内容；副本不含历史分享、业主反馈或确认记录。');
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '笔记复制失败' }); }
  },
  async deleteProjectNote(target, fromList, launched) {
    const note = target && target.note_id ? target : this.data.note; if (!note || this.data.busy || this.data.historyMode) return;
    const epoch = launched ? launched.epoch : this._epoch, token = launched ? launched.token : this._token;
    const confirmed = await new Promise(resolve => wx.showModal({ title: '删除这条草稿？', content: '删除后将从草稿列表移除。已冻结版本、业主反馈和现有分享仍保留；需要停止分享时请另行撤回。', confirmText: '删除草稿', confirmColor: '#a33b31', success: result => resolve(result.confirm === true), fail: () => resolve(false) }));
    const current = fromList ? this.data.notes.find(item => item.note_id === note.note_id) : this.data.note;
    if (!confirmed || !this.current(epoch) || this._token !== token || !current || current.note_id !== note.note_id || Number(current.revision) !== Number(note.revision)) return;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      await api.deleteProjectNote(note.note_id, note.revision);
      if (!this.current(epoch) || this._token !== token) return;
      if (fromList) {
        const notes = this.data.notes.filter(item => item.note_id !== note.note_id);
        const values = { notes, busy: false, notice: '笔记已移除。' };
        const selected = this.data.note && this.data.note.note_id === note.note_id;
        if (selected) {
          this._selectedNoteID = ''; this._newNote = false;
          Object.assign(values, { note: null, noteTitle: '', noteBody: '', noteMedia: [], bodyReadOnly: false });
        }
        this.setData(values, () => {
          if (!selected) return;
          this._noteFormBaseline = this.noteFormSignature();
          if (this._loadedFormState) Object.assign(this._loadedFormState, { noteTitle: '', noteBody: '', noteMedia: [] });
        });
        return;
      }
      this._selectedNoteID = ''; this._newNote = true; this._selectedVersionID = ''; this._shareAttempt = null;
      this.resetDraftInstance();
      this.setData({ busy: false });
      await this.refreshProject('草稿已移除，历史版本与业主记录仍保留。');
    } catch (error) { if (this.current(epoch) && this._token === token) this.setData({ busy: false, error: Number(error && error.statusCode) === 409 ? '笔记已更新，请刷新后再删除。' : error.message || '笔记删除失败' }); }
  },
  async createProjectNote() { return this.persistNote(false); },
  async saveProjectNote() { return this.persistNote(true); },
  async persistNote(update) {
    const project = this.data.selectedProject, note = this.data.note, draft = this.noteDraft();
    if (!project || !draft.title || (!this.data.bodyReadOnly && !draft.body.trim()) || update && !note || this.data.busy) return;
    // 创建接口采用严格字段校验，修订号仅用于编辑已有笔记的并发校验。
    if (!update) delete draft.expected_revision;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = update ? await api.saveProjectNote(note.note_id, draft) : await api.createProjectNote(project.project_id, draft);
      if (!this.current(epoch)) return;
      this._shareAttempt = null; this._selectedNoteID = saved.note_id; this._newNote = false; this._selectedVersionID = ''; this.adoptMediaFailureQueue(saved.note_id); this.setData({ notes: [saved, ...this.data.notes.filter(item => item.note_id !== saved.note_id)], noteIndex: 0, note: saved, frozenVersion: null, activeShare: null, busy: false });
      await this.refreshProject(update ? '草稿已更新，需重新冻结并分享新版本。' : '笔记草稿已创建。');
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '笔记保存失败' }); }
  },
  // 失败队列按「账号+项目+草稿」分开保存：切换笔记不丢、连续新建不串、刷新竞态不覆盖。
  mediaFailureOwnerKey() {
    const project = this.data.selectedProject;
    if (!project) return '';
    const noteID = this.data.note && this.data.note.note_id || this._selectedNoteID || '';
    return draftAccount() + '|' + project.project_id + '|' + (noteID ? 'note:' + noteID : 'draft:' + this.currentDraftInstanceID());
  },
  mediaFailureStore() { return this._noteMediaFailures || (this._noteMediaFailures = {}); },
  mediaFailuresFor(ownerKey) {
    if (!ownerKey) return [];
    const store = this.mediaFailureStore();
    return Array.isArray(store[ownerKey]) ? store[ownerKey] : [];
  },
  setMediaFailures(ownerKey, items) {
    if (!ownerKey) return;
    const store = this.mediaFailureStore();
    // 先删再写：刷新该归属的最近性。空队列不占名额，否则成功批次会挤掉真实的失败队列。
    delete store[ownerKey];
    if (!items.length) return;
    store[ownerKey] = items;
    const keys = Object.keys(store);
    // 只保留最近 10 个有失败项的归属，避免随草稿切换无限增长。
    if (keys.length > 10) keys.slice(0, keys.length - 10).forEach(key => { if (key !== ownerKey) delete store[key]; });
  },
  adoptMediaFailureQueue(noteID) {
    const project = this.data.selectedProject;
    if (!project || !noteID) return;
    const store = this.mediaFailureStore();
    const prefix = draftAccount() + '|' + project.project_id + '|';
    const draftKey = prefix + 'draft:' + this.currentDraftInstanceID();
    const noteKey = prefix + 'note:' + noteID;
    const items = store[draftKey];
    if (!items || !items.length) return;
    store[noteKey] = (store[noteKey] || []).concat(items);
    delete store[draftKey];
  },
  // 只读取当前归属的队列，不创建也不销毁其它归属。
  publishMediaFailures() {
    const ownerKey = this.mediaFailureOwnerKey();
    const items = this.mediaFailuresFor(ownerKey);
    // 只看不建：浏览其它笔记不得凭空创建空归属，避免把真实失败挤掉；有关联时刷新最近性。
    if (ownerKey && items.length) {
      const store = this.mediaFailureStore();
      delete store[ownerKey];
      store[ownerKey] = items;
    }
    const retryCount = Math.min(items.length, 9);
    this.setData({
      mediaUploadFailures: items.slice(), mediaFailureSummary: items.map(item => item.name).join('、'),
      mediaRetryLabel: items.length > 9 ? `重试下一批 ${retryCount} 个（共 ${items.length} 个待处理）` : `重试失败的 ${items.length} 个素材`,
    });
  },
  // 失效回调先把已成功素材挂到对应归属，返回该草稿时再接纳，避免「队列删了、素材也没了」。
  stashPendingMedia(ownerKey, uploadedFiles) {
    if (!ownerKey || !uploadedFiles.length) return;
    const store = this._noteMediaPending || (this._noteMediaPending = {});
    const assets = uploadedFiles.map(item => Object.assign({}, item.asset, { local_path: item.file.tempFilePath }));
    store[ownerKey] = (store[ownerKey] || []).concat(assets);
    // 挂起素材同样按归属有界，避免被放弃的草稿长期滞留。
    const keys = Object.keys(store);
    if (keys.length > 10) keys.slice(0, keys.length - 10).forEach(key => delete store[key]);
    writePendingMediaStore(store);
  },
  restorePendingMedia() {
    this._noteMediaPending = Object.assign(readPendingMediaStore(), this._noteMediaPending || {});
  },
  acceptPendingMedia() {
    const ownerKey = this.mediaFailureOwnerKey();
    const store = this._noteMediaPending;
    if (!ownerKey || !store || !store[ownerKey] || !store[ownerKey].length) return;
    const assets = store[ownerKey];
    delete store[ownerKey];
    writePendingMediaStore(store);
    this.setData({ noteMedia: [...this.data.noteMedia, ...assets], notice: '已补回后台完成的上传素材。' });
  },
  chooseNoteMedia() {
    const project = this.data.selectedProject;
    if (!project || this.data.busy || this._noteMediaUpload || this._mediaPicker) return;
    // 选择器发起时绑定身份：原生 success 与 onShow 顺序不固定，迟到回调用原身份校验，避免跨账号上传。
    const picker = { token: this._token, projectID: project.project_id, instanceID: this.currentDraftInstanceID(), generation: this._protectedGeneration || 0 };
    this._mediaPicker = picker;
    const release = () => { if (this._mediaPicker === picker) this._mediaPicker = null; };
    const pickerOwned = () => this._mediaPicker === picker;
    const pickerInDraft = () => picker.token === this._token && picker.token === String(wx.getStorageSync('token') || '')
      && (this._protectedGeneration || 0) === picker.generation
      && !!this.data.selectedProject && this.data.selectedProject.project_id === picker.projectID
      && this.currentDraftInstanceID() === picker.instanceID;
    wx.chooseMedia({
      count: 9, mediaType: ['image', 'video'], sourceType: ['album', 'camera'],
      success: result => {
        if (!pickerOwned()) return;
        release();
        this.setData({ mediaPermissionDenied: false });
        if (!pickerInDraft() || this.data.busy || this.data.historyMode || this.data.bodyReadOnly) return;
        this.uploadNoteMedia(result.tempFiles || []);
      },
      fail: error => {
        if (!pickerOwned()) return;
        release();
        // 失效的旧回调不得污染新账号/新项目的提示。
        if (!pickerInDraft()) return;
        const message = String(error && (error.errMsg || error.message) || '');
        // T2.1：取消与权限拒绝不再静默，且不改变已填内容；提示跨重验保留。
        const feedback = /cancel/i.test(message)
          ? { notice: '已取消，原内容保持不变。', error: '', mediaPermissionDenied: false }
          : { error: '未获得相册/相机权限，请到设置中开启后重试。', notice: '', mediaPermissionDenied: true };
        this._pendingPickerFeedback = { owner: { token: picker.token, projectID: picker.projectID, instanceID: picker.instanceID, generation: picker.generation }, data: feedback };
        this.setData(feedback);
      },
    });
  },
  // 串行逐文件上传：单个失败只进可重试队列，已成功的保留，剩余文件继续尝试。
  async uploadNoteMedia(files, retryPaths) {
    const project = this.data.selectedProject;
    // 不允许并发重入：同一时间只允许一个上传批次持有锁与队列归属。
    if (!project || !files.length || this._noteMediaUpload || this.data.busy) return;
    // 客户端预检先于任何请求：大小/类型/累计数量超限直接提示，不发起注定失败的请求。
    const rejection = noteMediaPrecheck(this.data.noteMedia || [], files);
    if (rejection) {
      const project0 = this.data.selectedProject;
      this._pendingPickerFeedback = { owner: { token: this._token, projectID: project0 && project0.project_id || '', instanceID: this.currentDraftInstanceID(), generation: this._protectedGeneration || 0 }, data: { error: rejection, notice: '' } };
      this.setData({ error: rejection, notice: '' });
      return;
    }
    const ownerKey = this.mediaFailureOwnerKey();
    const queue = this.mediaFailuresFor(ownerKey);
    // 批次身份：账号/项目/草稿实例三元绑定。换账号后旧批次必须停止，不能用新账号凭证发旧账号的文件。
    const operationID = 'note-media-' + Date.now() + '-' + (this._mediaUploadSequence = (this._mediaUploadSequence || 0) + 1);
    const batch = { operationID, token: this._token, projectID: project.project_id, instanceID: this.currentDraftInstanceID(), generation: this._protectedGeneration || 0 };
    const batchValid = () => !this._unloaded && (this._protectedGeneration || 0) === batch.generation;
    const ownsUpload = () => !!(this._noteMediaUpload && this._noteMediaUpload.operationID === operationID);
    const sameAccount = () => batch.token === this._token && batch.token === String(wx.getStorageSync('token') || '');
    const sameBatch = () => batchValid() && sameAccount()
      && !!this.data.selectedProject && this.data.selectedProject.project_id === batch.projectID
      && this.currentDraftInstanceID() === batch.instanceID
      && !this.data.historyMode && !this.data.bodyReadOnly;
    const releaseBusy = () => { if (!ownsUpload()) return; this._noteMediaUpload = null; this.setData({ busy: false }); };
    this._noteMediaUpload = batch;
    this.setData({ busy: true, error: '', notice: '' });
    this.publishMediaFailures();
    const uploadedFiles = [], failures = [];
    const commit = () => {
      // 成功项（含重试成功）从队列移除；失败项合并去重并刷新原因。
      const succeeded = new Set(uploadedFiles.map(item => item.file.tempFilePath));
      let remaining = queue.filter(item => !succeeded.has(item.tempFilePath));
      for (const item of failures) {
        const existing = remaining.find(entry => entry.tempFilePath === item.tempFilePath);
        if (existing) Object.assign(existing, item); else remaining.push(item);
      }
      // 重试轮转：本轮仍失败的项移到未尝试项之后，避免队首持续失败把所有后续项锁死。
      // 轮转集合按操作传入（闭包），旧重试收尾不会清掉新重试的集合。
      const retried = retryPaths;
      if (retried && retried.size) {
        const retriedFailures = remaining.filter(item => retried.has(item.tempFilePath));
        const untouched = remaining.filter(item => !retried.has(item.tempFilePath));
        remaining = untouched.concat(retriedFailures);
      }
      this.setMediaFailures(ownerKey, remaining.slice());
      return remaining;
    };

    for (let position = 0; position < files.length; position += 1) {
      if (!sameBatch()) break;
      const file = files[position];
      try {
        const asset = assetOf(await api.uploadProjectNoteMedia(project.project_id, file.tempFilePath));
        // T2.3：视频用选择器给的缩略图作编辑态封面。
        uploadedFiles.push({ file, asset: Object.assign({}, asset, file.thumbTempFilePath ? { thumb_path: file.thumbTempFilePath } : {}) });
      } catch (error) {
        const status = Number(error && error.statusCode);
        // 上传接口直接返回权限终态：仅当批次仍是当前有效操作时才作废并清空；迟到响应不得动当前状态。
        if ([401, 403, 404].includes(status)) {
          if (sameAccount() && batchValid() && ownsUpload()) this.handleNoteMediaAccessFailure(status);
          break;
        }
        // 身份失效导致的失败不计入失败数；未尝试的文件也不会走到这里。
        if (!sameBatch()) break;
        failures.push({ tempFilePath: file.tempFilePath, fileType: file.fileType || '', thumbTempFilePath: file.thumbTempFilePath || '', name: uploadFileName(file, position), message: uploadFailureMessage(error), statusCode: Number(error && error.statusCode) || 0, networkError: !!(error && error.networkError), errMsg: String(error && error.errMsg || '') });
      }
    }

    // 换账号/卸载后不得再写入任何存储，避免旧回调重建已清空的状态。
    // 卸载/撤权（clearProtected）后批次已废止：不得再写队列、挂起素材、发布或改 busy。
    const accountValid = batchValid() && sameAccount();
    const remaining = accountValid ? commit() : null;
    const sameDraft = accountValid && !!this.data.selectedProject && this.data.selectedProject.project_id === batch.projectID
      && this.currentDraftInstanceID() === batch.instanceID && !this.data.historyMode && !this.data.bodyReadOnly;
    if (sameDraft && ownsUpload()) {
      this._noteMediaUpload = null;
      const uploaded = uploadedFiles.map(item => Object.assign({}, item.asset, { local_path: item.file.tempFilePath }));
      if (!this._noteNewCoverID) {
        const savedCover = String(this.data.note && this.data.note.cover_media_id || '');
        const addedImage = uploaded.find(item => item.kind === 'image') || uploadedFiles.find(item => item.file.fileType === 'image');
        if (addedImage && !savedCover) this._noteNewCoverID = mediaID(addedImage) || mediaID(addedImage.asset || {});
      }
      const historyCount = Math.max(0, remaining.length - failures.length);
      const baseNotice = `已上传 ${uploaded.length} 个，${failures.length} 个失败${failures.length ? '，可重试' : ''}`;
      this.setData({
        noteMedia: [...this.data.noteMedia, ...uploaded], busy: false,
        notice: historyCount ? `${baseNotice}（另有 ${historyCount} 个历史失败待重试）` : baseNotice,
        error: remaining.length && !uploaded.length ? remaining[0].message : '',
      });
      this.publishMediaFailures();
      if (failures.length) wx.showToast({ title: `${failures.length} 个素材上传失败`, icon: 'none' });
      return;
    }
    // 项目/草稿切走但账号未变：已成功素材挂到原草稿名下等待接纳。
    if (accountValid && ownerKey && uploadedFiles.length) this.stashPendingMedia(ownerKey, uploadedFiles);
    if (ownsUpload()) {
      this._noteMediaUpload = null;
      // 页面未卸载时释放自己的锁；换账号但尚未 onShow 也允许解锁（不写受保护数据、不重建队列）。
      if (!this._unloaded) this.setData({ busy: false });
    }
    if (accountValid) this.publishMediaFailures();
  },
  // 上传接口返回权限终态（401/403/404）时与重验失败同口径：失效批次、清空受保护内容。
  handleNoteMediaAccessFailure(status) {
    this._loaded = false;
    this.clearProtected();
    this.setData({ loginRequired: status === 401, projectLoadFailed: status !== 401, error: status === 401 ? '' : '项目访问权限已变更，请重新加载或联系管理员。' });
  },
  async retryNoteMediaFailures() {
    const failures = (this.data.mediaUploadFailures || []).slice();
    if (!failures.length || this.data.busy) return;
    // 单次批量上限 9：历史失败超过 9 项时分轮重试，保证本批 N+M ≤ 9，其余继续作为历史展示。
    const batch = failures.slice(0, 9);
    return this.uploadNoteMedia(batch.map(item => ({ tempFilePath: item.tempFilePath, fileType: item.fileType, thumbTempFilePath: item.thumbTempFilePath || '' })), new Set(batch.map(item => item.tempFilePath)));
  },
  removeNoteMedia(event) {
    if (this.data.bodyReadOnly) return;
    const id = String(event.currentTarget.dataset.id || '');
    const beforeCover = this.noteDraft().cover_media_id;
    const remaining = this.data.noteMedia.filter(item => mediaID(item) !== id);
    // 只有删掉当前有效封面时才回退到剩余首图；删除其它素材必须保留封面意图。
    if (id === beforeCover) {
      const firstImage = remaining.find(item => item.kind === 'image');
      this._noteNewCoverID = firstImage ? mediaID(firstImage) : '';
    }
    this.setData({ noteMedia: remaining });
  },
  async freezeProjectNoteVersion() {
    const note = this.data.note; if (!note || this.data.busy) return;
    const draft = this.noteDraft();
    const sameDraft = draft.title === note.title && draft.body === note.body && draft.body_format === note.body_format
      && (draft.cover_media_id || '') === (note.cover_media_id || '') && JSON.stringify(draft.media_ids) === JSON.stringify(note.media_ids || []);
    if (!sameDraft) { this.setData({ error: '请先保存当前修改，再冻结此方案版本。' }); return; }
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try { await api.freezeProjectNoteVersion(note.note_id, note.revision); if (this.current(epoch)) { this._shareAttempt = null; this.setData({ busy: false }); await this.refreshProject('版本已冻结，后续修改不会改变本次分享。'); } }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '版本冻结失败' }); }
  },
  async createProjectNoteShare() {
    const version = this.data.frozenVersion; if (!version || this.data.busy) return;
    if (!this._shareAttempt || this._shareAttempt.versionID !== version.version_id) this._shareAttempt = { versionID: version.version_id, expiresAt: new Date(Date.now() + 7 * 86400000).toISOString(), key: api.mutationKey('project-note-share') };
    const attempt = this._shareAttempt, epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const share = await api.createProjectNoteShare(version.version_id, attempt.expiresAt, attempt.key);
      if (!this.current(epoch)) return;
      if (!share.share_token) { this.setData({ activeShare: share, busy: false, error: '分享已存在，但首次返回的入口凭证未保存。请撤回该分享后重新生成。' }); return; }
      this._issuedShare = share; this._shareAttempt = null; rememberShareToken(share); this.setData({ activeShare: share, busy: false, notice: '私密分享入口已生成，只有受邀账号登录后可查看。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '分享创建失败，可直接重试本次请求。' }); }
  },
  async revokeProjectNoteShare() {
    const share = this.data.activeShare; if (!share || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try { await api.revokeProjectNoteShare(share.share_id); if (!this.current(epoch)) return; if (this._issuedShare && this._issuedShare.share_id === share.share_id) this._issuedShare = null; forgetShareToken(share.share_id); this._shareAttempt = null; this.setData({ activeShare: null, busy: false, notice: '分享已撤回，原入口立即失效。' }); }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '分享撤回失败' }); }
  },
  copySharePath() { const token = this.data.activeShare && this.data.activeShare.share_token; if (token) wx.setClipboardData({ data: 'pages/projectnoteview/projectnoteview?pns=' + encodeURIComponent(token) }); },
  openSharePreview() { const token = this.data.activeShare && this.data.activeShare.share_token; if (token) wx.navigateTo({ url: '/pages/projectnoteview/projectnoteview?pns=' + encodeURIComponent(token) }); },
  async resolveProjectNoteFeedback() {
    if (!this.data.feedbackID || !this.data.feedbackReply || this.data.busy) return;
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    const signature = [this.data.feedbackID, this.data.feedbackReply].join('\n');
    if (!this._resolutionAttempt || this._resolutionAttempt.signature !== signature) this._resolutionAttempt = { signature, key: api.mutationKey('project-note-feedback') };
    try { await api.resolveProjectNoteFeedback(this.data.feedbackID, this.data.feedbackReply, this._resolutionAttempt.key); if (!this.current(epoch)) return; this._resolutionAttempt = null; this.setData({ busy: false, feedbackID: '', feedbackReply: '', selectedFeedback: null }); await this.refreshProject('反馈已处理；此操作不会代替业主的版本确认。'); }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '反馈处理失败' }); }
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
  onShareAppMessage(event) {
    if (event && event.target && event.target.dataset.kind === 'project-invitation' && this.data.memberInvitationPath) {
      return { title: (this.data.selectedProject && this.data.selectedProject.name || '项目') + ' 邀请你加入', path: this.data.memberInvitationPath };
    }
    return {};
  },
});
