// @ts-nocheck
'use strict';
const api = require('../../utils/phase3-api');
const { renderArticleDocument } = require('../../utils/article-document');

const blankProtected = () => ({
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
const truncationText = flags => Object.keys(flags || {}).some(key => flags[key]) ? '部分历史记录超过50条，当前仅显示最近50条。' : '';

Page({
  data: Object.assign({ loading: true, busy: false, error: '', notice: '', loginRequired: false }, blankProtected()),
  onLoad(options) {
    wx.hideShareMenu(); this._visible = true;
    this._targetProjectID = String(options && options.project_id || '');
    this._targetVersionID = String(options && options.version_id || '');
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._token) {
      this._token = token; this._epoch = (this._epoch || 0) + 1;
      this._issuedShare = null; this._shareAttempt = null; this._selectedProjectID = ''; this._selectedVersionID = ''; this.clearProtected();
    }
    this._visible = true;
    if (!token) { this.setData({ loading: false, loginRequired: true, error: '' }); return; }
    this.loadProjects(this._epoch);
  },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  current(epoch) { return this._visible !== false && epoch === this._epoch && this._token === String(wx.getStorageSync('token') || ''); },
  clearProtected() { this.setData(Object.assign(blankProtected(), { loading: false, loginRequired: false, error: '', notice: '', busy: false })); },

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
  setFeedbackID(event) { this.setData({ feedbackID: value(event) }); },
  setFeedbackReply(event) { this.setData({ feedbackReply: value(event) }); },

  async createProjectNoteProject() {
    if (!this.data.projectName || !this.data.ownerID || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const project = await api.createProjectNoteProject({ name: this.data.projectName, owner_id: this.data.ownerID });
      if (this.current(epoch)) { this._selectedProjectID = project.project_id; this.setData({ projects: [project, ...this.data.projects], selectedProject: project, projectName: '', busy: false, notice: '项目已创建，可邀请业主并建立笔记。' }); }
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '项目创建失败' }); }
  },
  async selectProject(event) { this._targetVersionID = ''; this._selectedVersionID = ''; return this.openProject(String(event.currentTarget.dataset.id || ''), this._epoch); },
  async openProject(projectID, epoch, notice) {
    if (!projectID) return;
    const request = this._projectRequest = (this._projectRequest || 0) + 1;
    const requested = this._targetVersionID || this._selectedVersionID || '';
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
      const presentation = fixed ? await this.fixedVersionPresentation(projectID, fixed, epoch, request) : null;
      if (this.current(epoch) && request === this._projectRequest) this.applyProjectDetail(result, notice, fixed, presentation);
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
  applyProjectDetail(result, notice, fixed, presentation) {
    const project = result.project || result, notes = result.notes || [], versions = result.versions || [], shares = result.shares || [];
    const requested = this._selectedVersionID || this._targetVersionID;
    const version = fixed || (requested ? null : result.latest_version || versions[0] || null);
    const targetID = version && version.version_id || requested;
    const note = requested ? null : version ? notes.find(item => item.note_id === version.note_id) || null : notes[0] || result.note || null;
    let share = result.active_share && (!targetID || result.active_share.version_id === targetID) ? result.active_share : shares.find(item => !item.revoked && (!targetID || item.version_id === targetID)) || null;
    if (share && this._issuedShare && this._issuedShare.share_token && share.share_id === this._issuedShare.share_id) share = Object.assign({}, share, { share_token: this._issuedShare.share_token });
    const members = result.members || [], available = activeMembers(members), foundIndex = available.findIndex(item => Number(item.actor_id) === Number(project.confirmer_id));
    const confirmerChoices = [{ actor_id: '', label: '请选择受邀账号' }, ...available.map(item => Object.assign({}, item, { label: '账号 ' + item.actor_id }))];
    const bodyReadOnly = !!note && note.body_format !== 'plain' && note.body_format !== 'plain_text';
    const historyNotice = requested ? '正在查看通知对应的固定版本；当前草稿不会在这里修改。' : version && !note ? '该版本对应的历史草稿未加载，当前仅可查看版本摘要。' : '';
    this._targetVersionID = ''; this._selectedProjectID = project.project_id;
    this.setData({
      selectedProject: project, members, activeMembers: available, confirmerChoices,
      confirmerIndex: foundIndex < 0 ? 0 : foundIndex + 1, confirmerAccount: foundIndex < 0 ? '' : available[foundIndex].actor_id,
      note, noteTitle: note && note.title || '', noteBody: note && note.body || '', bodyReadOnly,
      noteMedia: note ? ((note.media && note.media.length ? note.media.map(assetOf) : (note.media_ids || []).map(id => ({ media_id: id, kind: id === note.cover_media_id ? 'image' : 'private' }))).map(item => Object.assign({}, item, { media_id: mediaID(item) }))) : [],
      frozenVersion: version, activeShare: share,
      historyMode: !!requested, versionView: fixed || null, versionMedia: presentation && presentation.media || [], versionBlocks: presentation && presentation.blocks || [],
      feedbacks: (result.feedbacks || []).filter(item => !targetID || item.version_id === targetID),
      confirmations: (result.confirmations || []).filter(item => !targetID || item.version_id === targetID),
      truncationNotice: truncationText(result.truncated), historyNotice, busy: false, notice: notice || this.data.notice,
      error: share && !share.share_token ? '该分享入口凭证不再保存在当前会话中，请撤回后重新生成。' : '',
    });
  },
  async refreshProject(notice) { const project = this.data.selectedProject; if (project) await this.openProject(project.project_id, this._epoch, notice); },
  async returnToDraft() { const project = this.data.selectedProject; if (!project || this.data.busy) return; this._targetVersionID = ''; this._selectedVersionID = ''; await this.openProject(project.project_id, this._epoch); },
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
  async createProjectNote() { return this.persistNote(false); },
  async saveProjectNote() { return this.persistNote(true); },
  async persistNote(update) {
    const project = this.data.selectedProject, note = this.data.note, draft = this.noteDraft();
    if (!project || !draft.title || (!this.data.bodyReadOnly && !draft.body.trim()) || update && !note || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = update ? await api.saveProjectNote(note.note_id, draft) : await api.createProjectNote(project.project_id, draft);
      if (!this.current(epoch)) return;
      this._shareAttempt = null; this.setData({ note: saved, frozenVersion: null, activeShare: null, busy: false, notice: update ? '草稿已更新，需重新冻结并分享新版本。' : '笔记草稿已创建。' });
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
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try { const version = await api.freezeProjectNoteVersion(note.note_id, note.revision); if (this.current(epoch)) { this._shareAttempt = null; this.setData({ frozenVersion: version, activeShare: null, busy: false, notice: '版本已冻结，后续修改不会改变本次分享。' }); } }
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
    const epoch = this._epoch; this.setData({ busy: true, error: '', notice: '' });
    try { await api.resolveProjectNoteFeedback(this.data.feedbackID, this.data.feedbackReply, api.mutationKey('project-note-feedback')); if (!this.current(epoch)) return; this.setData({ busy: false, feedbackID: '', feedbackReply: '' }); await this.refreshProject('反馈已处理；此操作不会代替业主的版本确认。'); }
    catch (error) { if (this.current(epoch)) this.setData({ busy: false, error: error.message || '反馈处理失败' }); }
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
});
