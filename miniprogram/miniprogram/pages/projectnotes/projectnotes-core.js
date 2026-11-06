// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');
const { fitNoteMedia, fitNoteBlocks, groupNoteImages, noteVideoMetadata } = require('../../utils/project-note-media');

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
  previewMediaGroups: [], versionMediaGroups: [], galleryPositions: {},
});
const value = event => String(event.detail && event.detail.value || '').trim();
const mediaID = item => String(item && (item.media_id || item.id || item.asset && item.asset.media_id) || '');
const assetOf = item => item && item.asset ? item.asset : item;
const activeMembers = members => (members || []).filter(item => item && item.active !== false);
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
  data: Object.assign({ historyTypes: [{ key: 'versions', label: '方案版本' }, { key: 'notes', label: '笔记草稿' }, { key: 'feedbacks', label: '业主意见' }, { key: 'resolutions', label: '处理记录' }, { key: 'confirmations', label: '业主确认' }], loading: true, busy: false, error: '', notice: '', loginRequired: false, accessRecheckRequired: false }, blankProtected()),
  onLoad(options) {
    wx.hideShareMenu(); this._visible = true;
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
    if (this._writingLaunch && this._writingLaunch.token === token) {
      this._writingLaunch = null;
      return this.revalidateProjectAccess();
    }
    return this._loaded ? this.revalidateProjectAccess() : this.loadProjects(this._epoch);
  },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.editNote(); },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  onPageScroll(event) {
    if (this.data.pageView === 'list') this._listScrollTop = Number(event && event.scrollTop || 0);
    if (this.data.pageView === 'editor' && this.data.editorMode === 'edit') this._editorScrollTop = Number(event && event.scrollTop || 0);
  },
  scrollPage(view) {
    if (typeof wx.pageScrollTo !== 'function') return;
    wx.pageScrollTo({ scrollTop: view === 'list' ? Number(this._listScrollTop || 0) : 0, duration: 0 });
  },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  clearProtected() {
    this._noteFormBaseline = null; this._projectFormBaseline = null; this._loadedFormState = null;
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
    if (state) this.setData({ projectEdit: Object.assign({}, state.projectEdit), noteTitle: state.noteTitle, noteBody: state.noteBody, noteMedia: state.noteMedia.map(item => Object.assign({}, item)) });
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
    const id = String(event.currentTarget.dataset.id || '');
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
      ? { busy: false, loginRequired: false, accessRecheckRequired: false, error: '', notice: '' }
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
  setNoteBody(event) { if (!this.data.bodyReadOnly) this.setData({ noteBody: String(event.detail && event.detail.value || '') }); },
  onNoteKeyboardHeight(event) { this.setData({ keyboardHeight: Math.max(0, Number(event.detail && event.detail.height) || 0) }); },
  onNoteGalleryChange(event) {
    this.setData({ galleryPositions: Object.assign({}, this.data.galleryPositions, { [event.currentTarget.dataset.key]: Number(event.detail.current) || 0 }) });
  },
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
      this.setData({ previewLoading: false, previewBodyFormat: draft.body_format, previewBody: draft.body, previewBlocks: presentation.blocks, previewMedia: presentation.media, previewMediaGroups: presentation.groups });
    } catch (error) {
      if (current()) this.setData({ previewLoading: false, previewError: error.message || '预览加载失败，内容已保留，请重试。' });
    }
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
    const members = result.members || [], available = activeMembers(members), foundIndex = available.findIndex(item => Number(item.actor_id) === Number(project.confirmer_id));
    const confirmerChoices = [{ actor_id: '', label: '选择确认人' }, ...available.map(item => Object.assign({}, item, { label: memberLabel(item) }))];
    const bodyReadOnly = !!note && note.body_format !== 'plain' && note.body_format !== 'plain_text';
    const historyNotice = requested ? '正在查看通知对应的固定版本；当前草稿不会在这里修改。' : version && !note ? '该版本对应的历史草稿未加载，当前仅可查看版本摘要。' : '';
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
      error: share && !share.share_token ? '该分享入口凭证不再保存在当前会话中，请撤回后重新生成。' : '',
    }, () => { this.rememberLoadedForms(); this.scrollPage(pageView); });
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
    this._targetFeedbackID = ''; this._selectedNoteID = note.note_id; this._newNote = false; this._selectedVersionID = ''; this._targetVersionID = '';
    this._nextPageView = 'editor';
    return this.refreshProject();
  },
  async selectVersion(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if (this.data.busy || !this.data.versionChoices.some(item => item.version_id === id)) return;
    if (!await this.confirmDiscardChanges()) return;
    this._targetFeedbackID = ''; this._selectedVersionID = id; this._nextPageView = 'version';
    return this.refreshProject();
  },
  async startNewNote() {
    if (this.data.busy || !await this.confirmDiscardChanges()) return;
    this._newNote = true; this._selectedNoteID = ''; this._selectedVersionID = ''; this._targetVersionID = ''; this._shareAttempt = null;
    this.editNote(); this._editorScrollTop = 0;
    this.setData({ pageView: 'editor', navigationTitle: '新建笔记', projectSettingsOpen: false, note: null, noteTitle: '', noteBody: '', noteMedia: [], bodyReadOnly: false, historyMode: false, versionView: null, versionMedia: [], versionBlocks: [], historyNotice: '', feedbacks: [], confirmations: [], selectedFeedback: null, feedbackID: '', feedbackReply: '' }, () => { this._noteFormBaseline = this.noteFormSignature(); this.scrollPage('editor'); });
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
    this.setData({ requestsLoading: true });
    try {
      const [result, invitations] = await Promise.all([api.listProjectJoinRequests(projectID), api.listProjectInvitations(projectID)]);
      if (!this.current(epoch) || !this.data.selectedProject || this.data.selectedProject.project_id !== projectID) return;
      this.setData({
        joinRequests: (result.items || []).filter(item => item.status === 'pending'),
        memberInvitations: (invitations.items || []).map(item => Object.assign({}, item, { expiryLabel: invitationLabel(item) })),
        requestsLoading: false,
      });
    } catch (error) {
      if (this.current(epoch) && this.data.selectedProject && this.data.selectedProject.project_id === projectID) this.setData({ requestsLoading: false, error: error.message || '加入申请加载失败' });
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
    const ids = (this.data.noteMedia || []).map(mediaID).filter(Boolean), cover = (this.data.noteMedia || []).find(item => item.kind === 'image');
    return { title: this.data.noteTitle, body: this.data.noteBody, body_format: 'plain', cover_media_id: cover ? mediaID(cover) : '', media_ids: ids, expected_revision: note && note.revision || 0 };
  },
  noteWritingSignature() { return JSON.stringify({ project: this.data.selectedProject, note: this.noteDraft() }); },
  openNoteAIWriting() {
    const project = this.data.selectedProject, note = this.data.note;
    if (!project || this.data.busy || this.data.historyMode || this.data.bodyReadOnly) return;
    const launch = { token: this._token, projectID: project.project_id, noteID: note && note.note_id || '', revision: note && note.revision || 0, signature: this.noteWritingSignature(), handled: false };
    this._writingLaunch = launch;
    const facts = [project.name && '项目：' + project.name, project.location && '地点：' + project.location,
      project.building_area > 0 && '建筑面积：' + project.building_area + ' 平方米', project.floors > 0 && '层数：' + project.floors,
      project.style && '风格：' + project.style, this.data.noteBody].filter(Boolean).join('\n').slice(0, 6000);
    wx.navigateTo({
      url: '/pages/contentwriting/contentwriting?source=project_note',
      success: result => result.eventChannel.emit('writingContext', { source: 'project_note', project_id: launch.projectID, note_id: launch.noteID, note_revision: launch.revision, title: this.data.noteTitle, facts }),
      events: { prefillContent: payload => this.receiveNoteWriting(payload, launch) },
      fail: () => { this._writingLaunch = null; this.setData({ notice: 'AI 文案页未能打开，原笔记已保留。' }); },
    });
  },
  receiveNoteWriting(payload, launch) {
    if (launch.handled) return;
    launch.handled = true;
    const note = this.data.note, text = String(payload && payload.text || '').trim();
    const valid = launch.token === String(wx.getStorageSync('token') || '') && !this.data.busy && !this.data.historyMode && !this.data.bodyReadOnly
      && payload && payload.source === 'project_note' && payload.project_id === launch.projectID && payload.note_id === launch.noteID && Number(payload.note_revision) === launch.revision
      && (note && note.note_id || '') === launch.noteID && (note && note.revision || 0) === launch.revision && this.noteWritingSignature() === launch.signature;
    if (!valid) { this.setData({ notice: '笔记、账号或版本已变化，AI 文案未采用，请重新发起。' }); return; }
    if (!text || text === this.data.noteBody.trim()) { this.setData({ notice: '没有新的文案可追加，原笔记已保留。' }); return; }
    const body = this.data.noteBody ? this.data.noteBody + '\n\n' + text : text;
    if (body.length > 8000) { this.setData({ notice: '追加后超过笔记字数上限，AI 文案未采用，请先精简原文。' }); return; }
    this.setData({ noteTitle: this.data.noteTitle || String(payload.title || '').trim(), noteBody: body, notice: 'AI 文案已带回此笔记，请核对后保存新修订。' });
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
      this._shareAttempt = null; this._selectedNoteID = saved.note_id; this._newNote = false; this._selectedVersionID = ''; this.setData({ notes: [saved, ...this.data.notes.filter(item => item.note_id !== saved.note_id)], noteIndex: 0, note: saved, frozenVersion: null, activeShare: null, busy: false });
      await this.refreshProject(update ? '草稿已更新，需重新冻结并分享新版本。' : '笔记草稿已创建。');
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '笔记保存失败' }); }
  },
  chooseNoteMedia() { if (!this.data.selectedProject || this.data.busy) return; wx.chooseMedia({ count: 9, mediaType: ['image', 'video'], sourceType: ['album', 'camera'], success: result => this.uploadNoteMedia(result.tempFiles || []) }); },
  async uploadNoteMedia(files) {
    const project = this.data.selectedProject; if (!project || !files.length) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const uploaded = [];
      for (const file of files) {
        const asset = assetOf(await api.uploadProjectNoteMedia(project.project_id, file.tempFilePath));
        uploaded.push(Object.assign({}, asset, { local_path: file.tempFilePath }));
      }
      if (this.current(epoch)) this.setData({ noteMedia: [...this.data.noteMedia, ...uploaded], busy: false, notice: '私有素材已上传，仅用于当前项目笔记。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '私有素材上传失败' }); }
  },
  removeNoteMedia(event) { if (!this.data.bodyReadOnly) { const id = String(event.currentTarget.dataset.id || ''); this.setData({ noteMedia: this.data.noteMedia.filter(item => mediaID(item) !== id) }); } },
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
      this._issuedShare = share; this._shareAttempt = null; this.setData({ activeShare: share, busy: false, notice: '私密分享入口已生成，只有受邀账号登录后可查看。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '分享创建失败，可直接重试本次请求。' }); }
  },
  async revokeProjectNoteShare() {
    const share = this.data.activeShare; if (!share || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try { await api.revokeProjectNoteShare(share.share_id); if (!this.current(epoch)) return; if (this._issuedShare && this._issuedShare.share_id === share.share_id) this._issuedShare = null; this._shareAttempt = null; this.setData({ activeShare: null, busy: false, notice: '分享已撤回，原入口立即失效。' }); }
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
