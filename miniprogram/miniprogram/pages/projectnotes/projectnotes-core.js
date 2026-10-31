// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');

const blankProtected = () => ({
  pageView: 'list', navigationTitle: '我的项目', projectSettingsOpen: false,
  historyType: 'versions', historyIndex: 0, historyItems: [], historyPage: 0, historyCursors: [''], historyNextCursor: '', historyLoaded: false, historyLoading: false,
  projectEdit: {}, projectStatusLabel: '', notes: [], noteIndex: 0, versionChoices: [], selectedFeedback: null,
  projects: [], owners: [], ownerIndex: 0, ownerID: '', projectName: '', crmID: '', selectedProject: null,
  members: [], activeMembers: [], confirmerChoices: [], confirmerIndex: 0, confirmerAccount: '', memberAccount: '',
  note: null, noteTitle: '', noteBody: '', bodyReadOnly: false, noteMedia: [], frozenVersion: null, activeShare: null,
  feedbacks: [], confirmations: [], feedbackID: '', feedbackReply: '', truncationNotice: '', historyNotice: '',
  historyMode: false, versionView: null, versionBlocks: [], versionMedia: [],
});
const value = event => String(event.detail && event.detail.value || '').trim();
const mediaID = item => String(item && (item.media_id || item.id || item.asset && item.asset.media_id) || '');
const assetOf = item => item && item.asset ? item.asset : item;
const activeMembers = members => (members || []).filter(item => item && item.active !== false);
const statusLabels = { initial: '初步方案', revising: '修改中', confirmed: '已确认' };
const satisfactionLabels = { very_satisfied: '非常满意', needs_adjustment: '基本满意，需要调整', unsatisfied: '不太满意' };
const projectForm = project => ({ name: project.name || '', location: project.location || '', building_area: project.building_area || '', floors: project.floors || '', style: project.style || '' });
const truncationText = flags => Object.keys(flags || {}).some(key => flags[key]) ? '部分历史记录超过50条，当前仅显示最近50条。' : '';

Page({
  data: Object.assign({ historyTypes: [{ key: 'versions', label: '方案版本' }, { key: 'notes', label: '笔记草稿' }, { key: 'feedbacks', label: '业主意见' }, { key: 'resolutions', label: '处理记录' }, { key: 'confirmations', label: '业主确认' }], loading: true, busy: false, error: '', notice: '', loginRequired: false, accessRecheckRequired: false }, blankProtected()),
  onLoad(options) {
    wx.hideShareMenu(); this._visible = true;
    this._targetProjectID = String(options && options.project_id || '');
    this._targetVersionID = String(options && options.version_id || '');
    this._targetFeedbackID = String(options && options.feedback_id || '');
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._token) {
      this._token = token; this._epoch = (this._epoch || 0) + 1;
      this._loaded = false; this._writingLaunch = null; this._selectedNoteID = ''; this._newNote = false; this._resolutionAttempt = null; this._issuedShare = null; this._shareAttempt = null; this._selectedProjectID = ''; this._selectedVersionID = ''; this.clearProtected();
    }
    this._visible = true;
    if (!token) { this.setData({ loading: false, loginRequired: true, error: '' }); return; }
    if (this._writingLaunch && this._writingLaunch.token === token) {
      this._writingLaunch = null;
      return this.revalidateProjectAccess();
    }
    return this._loaded ? this.revalidateProjectAccess() : this.loadProjects(this._epoch);
  },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
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
    const epoch = this._epoch, project = this.data.selectedProject, view = this.data.pageView;
    const unchanged = () => this.current(epoch) && this.data.selectedProject === project && this.data.pageView === view && !this.data.busy;
    if (this.data.pageView !== 'detail') {
      if (!await this.confirmDiscardChanges() || !unchanged()) return;
      this.restoreLoadedForms();
      this.setData({ pageView: 'detail', navigationTitle: this.data.selectedProject && this.data.selectedProject.name || '项目详情', projectSettingsOpen: false, error: '', notice: '' });
      return;
    }
    if (!await this.confirmDiscardChanges() || !unchanged()) return;
    const kept = { projects: this.data.projects, owners: this.data.owners, ownerID: this.data.ownerID, ownerIndex: this.data.ownerIndex };
    this._selectedProjectID = ''; this._selectedNoteID = ''; this._selectedVersionID = ''; this._newNote = false;
    this._noteFormBaseline = null; this._projectFormBaseline = null; this._loadedFormState = null;
    this.setData(Object.assign(blankProtected(), kept));
  },

  openProjectSettings() { this.setData({ projectSettingsOpen: true }); },
  async closeProjectSettings() {
    if (this.data.busy) return;
    const epoch = this._epoch, project = this.data.selectedProject, view = this.data.pageView;
    const changed = this._projectFormBaseline != null && this._projectFormBaseline !== this.projectFormSignature();
    if (!await this.confirmDiscardChanges(changed) || !this.current(epoch) || this.data.selectedProject !== project || this.data.pageView !== view || this.data.busy) return;
    this.restoreLoadedForms(); this.setData({ projectSettingsOpen: false });
  },
  openMembers() { this.setData({ pageView: 'members', navigationTitle: '项目成员', projectSettingsOpen: false }); },
  openHistory() { this.setData({ pageView: 'history', navigationTitle: '项目记录', projectSettingsOpen: false }); if (!this.data.historyLoaded) return this.loadHistory(); },
  openNote(event) {
    const id = String(event.currentTarget.dataset.id || ''), index = this.data.notes.findIndex(item => item.note_id === id);
    if (index >= 0) return this.selectNote({ detail: { value: index } });
  },

  async revalidateProjectAccess() {
    const epoch = this._epoch = (this._epoch || 0) + 1;
    const project = this.data.selectedProject;
    this.setData({ loading: true, loginRequired: false, accessRecheckRequired: false, error: '', notice: '' });
    try {
      if (project) await api.getProjectNoteProject(project.project_id); else await api.workbenchContext();
      if (!this.current(epoch)) return;
      this.setData({ loading: false, accessRecheckRequired: false, notice: '' });
    } catch (error) {
      if (!this.current(epoch)) return;
      const status = Number(error.statusCode);
      if ([401, 403, 404].includes(status)) {
        this._loaded = false; this.clearProtected();
        this.setData({ loginRequired: status === 401, error: status === 401 ? '' : '项目访问权限已变更，请重新进入。' });
        return;
      }
      this.setData({ loading: false, accessRecheckRequired: true, notice: '权限重验失败，未保存内容已保留，请重试。' });
    }
  },
  retryProjectAccess() { return this.revalidateProjectAccess(); },

  async fetchAllProjects() {
    const items = [], seen = new Set(); let cursor = '';
    do {
      const page = await api.listProjectNoteProjects({ limit: 50, cursor });
      for (const item of page.items || []) if (!seen.has(item.project_id)) { seen.add(item.project_id); items.push(item); }
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
      this._loaded = true;
      this.setData({ projects, owners, ownerID: owners[0] || '', ownerIndex: 0, loading: false });
      const selectedID = this._targetProjectID || this._selectedProjectID;
      if (selectedID) { this._targetProjectID = ''; await this.openProject(selectedID, epoch); }
    } catch (error) {
      if (!this.current(epoch)) return;
      this.clearProtected();
      this.setData({ loading: false, loginRequired: error.statusCode === 401, error: error.statusCode === 403 ? '暂无私人项目管理权限。' : '私人项目暂不可用，请稍后重试。' });
    }
  },

  setProjectName(event) { this.setData({ projectName: value(event) }); },
  setOwnerID(event) { const index = Number(event.detail.value) || 0; this.setData({ ownerIndex: index, ownerID: this.data.owners[index] || '' }); },
  setMemberAccount(event) { this.setData({ memberAccount: value(event) }); },
  setConfirmerChoice(event) { const index = Number(event.detail.value) || 0, member = this.data.confirmerChoices[index]; this.setData({ confirmerIndex: index, confirmerAccount: member ? member.actor_id : '' }); },
  setNoteTitle(event) { this.setData({ noteTitle: value(event) }); },
  setNoteBody(event) { if (!this.data.bodyReadOnly) this.setData({ noteBody: String(event.detail && event.detail.value || '') }); },
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
      this.setData({ selectedProject: saved, projectEdit: projectForm(saved), projectStatusLabel: statusLabels[saved.status] || '初步方案', projects: this.data.projects.map(item => item.project_id === saved.project_id ? saved : item), busy: false, notice: '项目资料已保存，已冻结版本仍保留原有资料。' }, () => { this._projectFormBaseline = this.projectFormSignature(); });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '项目资料保存失败，请重新载入后重试' }); }
  },
  async createProjectNoteProject() {
    if (!this.data.projectName || !this.data.ownerID || this.data.busy) return;
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const project = await api.createProjectNoteProject({ name: this.data.projectName, owner_id: this.data.ownerID });
      if (this.current(epoch)) { this._selectedProjectID = project.project_id; this._selectedNoteID = ''; this._selectedVersionID = ''; this._newNote = false; this.setData({ projects: [project, ...this.data.projects], projectName: '', busy: false }); this.applyProjectDetail({ project }, '项目已创建。', null, null, 'detail'); }
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '项目创建失败' }); }
  },
  async selectProject(event) { if (this.data.busy || !await this.confirmDiscardChanges()) return; this._selectedNoteID = ''; this._newNote = false; this._targetVersionID = ''; this._selectedVersionID = ''; return this.openProject(String(event.currentTarget.dataset.id || ''), this._epoch); },
  async openProject(projectID, epoch, notice) {
    if (!projectID) return;
    const request = this._projectRequest = (this._projectRequest || 0) + 1;
    const requested = this._targetVersionID || this._selectedVersionID || '';
    const requestedView = this._nextPageView || (requested ? 'version' : ['editor', 'members', 'history'].includes(this.data.pageView) ? this.data.pageView : 'detail');
    this._nextPageView = '';
    this._selectedProjectID = projectID; this._selectedVersionID = requested; this._targetVersionID = '';
    const kept = { projects: this.data.projects, owners: this.data.owners, ownerID: this.data.ownerID, ownerIndex: this.data.ownerIndex };
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
      if (this.current(epoch) && request === this._projectRequest) this.applyProjectDetail(result, notice, fixed, presentation, requestedView);
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
    const blocks = fixed.body_format === 'article_v2' ? renderArticleDocument(fixed.body, media, Math.max(240, windowInfo.windowWidth - 40), 'fixed-note') : [];
    return { media, blocks };
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
    const feedbacks = targetID ? (result.feedbacks || []).filter(item => item.version_id === targetID).map(item => Object.assign({}, item, { satisfaction_label: satisfactionLabels[item.satisfaction] || '', resolution_reply: item.reply || replies.get(item.feedback_id) || '' })) : [];
    const selectedFeedback = feedbacks.find(item => item.feedback_id === this._targetFeedbackID) || null;
    let share = targetID && result.active_share && result.active_share.version_id === targetID ? result.active_share : targetID ? shares.find(item => !item.revoked && item.version_id === targetID) || null : null;
    if (share && this._issuedShare && this._issuedShare.share_token && share.share_id === this._issuedShare.share_id) share = Object.assign({}, share, { share_token: this._issuedShare.share_token });
    const members = result.members || [], available = activeMembers(members), foundIndex = available.findIndex(item => Number(item.actor_id) === Number(project.confirmer_id));
    const confirmerChoices = [{ actor_id: '', label: '请选择受邀账号' }, ...available.map(item => Object.assign({}, item, { label: '账号 ' + item.actor_id }))];
    const bodyReadOnly = !!note && note.body_format !== 'plain' && note.body_format !== 'plain_text';
    const historyNotice = requested ? '正在查看通知对应的固定版本；当前草稿不会在这里修改。' : version && !note ? '该版本对应的历史草稿未加载，当前仅可查看版本摘要。' : '';
    const pageView = requestedView || (requested || fixed ? 'version' : 'detail');
    const navigationTitles = { detail: project.name || '项目详情', editor: note ? '编辑笔记' : '新建笔记', version: '方案版本', members: '项目成员', history: '项目记录' };
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
      feedbacks, selectedFeedback, feedbackID: selectedFeedback && selectedFeedback.feedback_id || '', feedbackReply: '',
      confirmations: targetID ? (result.confirmations || []).filter(item => item.version_id === targetID) : [],
      truncationNotice: truncationText(result.truncated), historyNotice, busy: false, notice: notice || this.data.notice,
      error: share && !share.share_token ? '该分享入口凭证不再保存在当前会话中，请撤回后重新生成。' : '',
    }, () => this.rememberLoadedForms());
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
        displayTime: item.frozen_at || item.resolved_at || item.confirmed_at || item.updated_at || item.created_at || '',
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
    this.setData({ pageView: 'editor', navigationTitle: '新建笔记', projectSettingsOpen: false, note: null, noteTitle: '', noteBody: '', noteMedia: [], bodyReadOnly: false, frozenVersion: null, versionChoices: [], activeShare: null, historyMode: false, versionView: null, versionMedia: [], versionBlocks: [], historyNotice: '', feedbacks: [], confirmations: [], selectedFeedback: null, feedbackID: '', feedbackReply: '' }, () => { this._noteFormBaseline = this.noteFormSignature(); });
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

  async inviteProjectNoteMember() { return this.memberMutation(false); },
  async revokeProjectNoteMember(event) { return this.memberMutation(true, event && event.currentTarget.dataset.account); },
  async memberMutation(revoke, supplied) {
    const project = this.data.selectedProject, account = Number(supplied || this.data.memberAccount);
    if (!project || !Number.isInteger(account) || account <= 0 || this.data.busy) return;
    if (!await this.confirmDiscardChanges()) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      if (revoke) await api.revokeProjectNoteMember(project.project_id, account); else await api.inviteProjectNoteMember(project.project_id, account);
      if (!this.current(epoch)) return;
      this.setData({ busy: false, memberAccount: '' }); await this.refreshProject(revoke ? '访问权限已移除。' : '业主账号已邀请。');
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
  async deleteProjectNote() {
    const note = this.data.note; if (!note || this.data.busy || this.data.historyMode) return;
    const epoch = this._epoch;
    const confirmed = await new Promise(resolve => wx.showModal({ title: '删除这条草稿？', content: '删除后将从草稿列表移除。已冻结版本、业主反馈和现有分享仍保留；需要停止分享时请另行撤回。', confirmText: '删除草稿', confirmColor: '#a33b31', success: result => resolve(result.confirm === true), fail: () => resolve(false) }));
    if (!confirmed || !this.current(epoch) || !this.data.note || this.data.note.note_id !== note.note_id || this.data.note.revision !== note.revision) return;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      await api.deleteProjectNote(note.note_id, note.revision);
      if (!this.current(epoch)) return;
      this._selectedNoteID = ''; this._newNote = true; this._selectedVersionID = ''; this._shareAttempt = null;
      this.setData({ busy: false });
      await this.refreshProject('草稿已移除，历史版本与业主记录仍保留。');
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '笔记删除失败' }); }
  },
  async createProjectNote() { return this.persistNote(false); },
  async saveProjectNote() { return this.persistNote(true); },
  async persistNote(update) {
    const project = this.data.selectedProject, note = this.data.note, draft = this.noteDraft();
    if (!project || !draft.title || (!this.data.bodyReadOnly && !draft.body.trim()) || update && !note || this.data.busy) return;
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
      for (const file of files) uploaded.push(assetOf(await api.uploadProjectNoteMedia(project.project_id, file.tempFilePath)));
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
});
