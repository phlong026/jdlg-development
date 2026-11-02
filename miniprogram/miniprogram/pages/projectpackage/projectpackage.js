'use strict';
const api = require('../../utils/project-packages-api');

const picked = (items, ids) => items.map(item => Object.assign({}, item, { selected: ids.includes(String(item.version_id || item.note_id || item.actor_id)) }));

Page({
  data: {
    loading: true, busy: false, error: '', notice: '', step: 'select',
    projectName: '', project: null, notes: [], versions: [], members: [], selectedNoteIDs: [], selectedVersionIDs: [], selectedRecipientIDs: [],
    draft: null, preview: null, delivery: null, sharePath: '', historyDeliveries: [], historyReceipts: [], historyTruncated: false,
  },
  onLoad(options) {
    wx.hideShareMenu();
    this._projectID = String(options && options.project_id || '').trim();
    this._visible = true;
  },
  onShow() {
    const token = String(wx.getStorageSync('token') || '');
    if (!token) { this.clearProtected(); this.setData({ loading: false, error: '请先登录后制作分享资料。' }); return; }
    const accountChanged = token !== this._accountToken;
    const selections = accountChanged ? null : this.selectionSnapshot();
    if (accountChanged) { this._accountToken = token; this._epoch = (this._epoch || 0) + 1; this._loadedOnce = false; }
    this._visible = true;
    // 同一账号且已成功渲染过：就地静默刷新，不清空已展示的资料，返回时不再闪屏。
    const quiet = !accountChanged && this._loadedOnce === true && !!this.data.project;
    if (!quiet) this.clearProtected(!accountChanged);
    else { wx.hideShareMenu(); this.setData({ busy: false, error: '', notice: '' }); }
    return this.loadProject(selections, quiet);
  },
  onHide() {
    this._visible = false; this._epoch = (this._epoch || 0) + 1;
    // 离开即作废未收口的分享入口与预览态；列表与选择状态保留，返回时静默重验不闪屏。
    this._draftAttempt = null; this._publishAttempt = null;
    wx.hideShareMenu();
    this.setData({ step: 'select', preview: null, delivery: null, sharePath: '', busy: false });
  },
  onUnload() { this._visible = false; this._epoch = (this._epoch || 0) + 1; this.clearProtected(); },
  selectionSnapshot() {
    return { noteIDs: [...this.data.selectedNoteIDs], versionIDs: [...this.data.selectedVersionIDs], recipientIDs: [...this.data.selectedRecipientIDs] };
  },
  clearProtected(preserveSelection) {
    const selections = preserveSelection ? this.selectionSnapshot() : { noteIDs: [], versionIDs: [], recipientIDs: [] };
    wx.hideShareMenu();
    this._draftAttempt = null; this._publishAttempt = null;
    this.setData({ projectName: '', project: null, notes: [], versions: [], members: [], selectedNoteIDs: selections.noteIDs, selectedVersionIDs: selections.versionIDs, selectedRecipientIDs: selections.recipientIDs, draft: null, preview: null, delivery: null, sharePath: '', historyDeliveries: [], historyReceipts: [], historyTruncated: false, step: 'select', notice: '', error: '', loading: true, busy: false });
  },
  current(epoch) { return this._visible && epoch === this._epoch && this._accountToken === String(wx.getStorageSync('token') || ''); },
  async loadProject(selections, quiet) {
    if (!this._projectID) { this.setData({ loading: false, error: '项目不存在。' }); return; }
    const epoch = this._epoch = (this._epoch || 0) + 1;
    this._visible = true;
    // 静默刷新保留已渲染内容；首次进入仍显示加载态。
    this.setData(quiet ? { error: '' } : { loading: true, error: '' });
    try {
      const [detail, historyResult] = await Promise.all([
        api.getProjectPackageProject(this._projectID),
        api.getProjectPackageHistory(this._projectID).then(history => ({ history }), error => ({ error })),
      ]);
      if (!this.current(epoch)) return;
      this.applyProject(detail, selections);
      if (historyResult.history) this.applyHistory(historyResult.history);
      this._loadedOnce = true;
      this.setData({ loading: false, notice: historyResult.error ? '分享记录暂时无法加载，请稍后刷新。' : '' });
    } catch (error) {
      if (this.current(epoch)) { this.clearProtected(); this.setData({ loading: false, error: error.message || '项目加载失败' }); }
    }
  },
  applyProject(detail, selections) {
    const project = detail && detail.project || {};
    const notes = (detail && detail.notes || []).filter(item => item && item.note_id && Number(item.revision) > 0);
    const versions = (detail && detail.versions || []).filter(item => item && item.version_id);
    const members = (detail && detail.members || []).filter(item => item && item.active !== false && Number(item.actor_id) > 0);
    const confirmer = Number(project.confirmer_id || 0);
    const selectedNoteIDs = selections ? selections.noteIDs.filter(id => notes.some(item => String(item.note_id) === String(id))) : [];
    const selectedVersionIDs = selections ? selections.versionIDs.filter(id => versions.some(item => String(item.version_id) === String(id))) : [];
    const selectedRecipientIDs = selections ? selections.recipientIDs.map(Number).filter(id => members.some(item => Number(item.actor_id) === id)) : confirmer && members.some(item => Number(item.actor_id) === confirmer) ? [confirmer] : [];
    this.setData({
      project, projectName: project.name || '项目分享资料',
      notes: picked(notes, selectedNoteIDs), versions: picked(versions, selectedVersionIDs), members: picked(members, selectedRecipientIDs.map(String)),
      selectedNoteIDs, selectedVersionIDs, selectedRecipientIDs,
    });
  },
  applyHistory(history) {
    const receipts = history && history.receipts || [];
    const receiptCounts = new Map();
    for (const receipt of receipts) receiptCounts.set(receipt.delivery_id, (receiptCounts.get(receipt.delivery_id) || 0) + 1);
    const historyDeliveries = (history && history.deliveries || []).map(item => this.presentDelivery(item, receiptCounts.get(item.delivery_id) || 0));
    this.setData({ historyDeliveries, historyReceipts: receipts, historyTruncated: !!(history && history.truncated) });
  },
  presentDelivery(item, receiptCount) {
    return Object.assign({}, item, {
      itemCount: (item.items || []).length, recipientCount: (item.recipient_ids || []).length, receiptCount,
      historyMeta: [String(item.created_at || '').replace('T', ' ').slice(0, 16), receiptCount ? receiptCount + ' 条回执' : ''].filter(Boolean).join(' · '),
    });
  },
  openProjectMembers() {
    if (!this._projectID || this.data.busy) return;
    wx.navigateTo({
      url: '/pages/projectnotes/projectnotes?project_id=' + encodeURIComponent(this._projectID) + '&view=members&return_to=package',
      fail: () => this.setData({ notice: '暂时无法打开项目成员，请重试。' }),
    });
  },
  toggleNote(event) {
    const id = String(event.currentTarget.dataset.id || '');
    const ids = this.data.selectedNoteIDs.includes(id) ? this.data.selectedNoteIDs.filter(value => value !== id) : [...this.data.selectedNoteIDs, id];
    this.setData({ selectedNoteIDs: ids, notes: picked(this.data.notes, ids) });
  },
  toggleVersion(event) {
    const id = String(event.currentTarget.dataset.id || '');
    const ids = this.data.selectedVersionIDs.includes(id) ? this.data.selectedVersionIDs.filter(value => value !== id) : [...this.data.selectedVersionIDs, id];
    this.setData({ selectedVersionIDs: ids, versions: picked(this.data.versions, ids) });
  },
  toggleRecipient(event) {
    const id = Number(event.currentTarget.dataset.id || 0);
    if (!id) return;
    const ids = this.data.selectedRecipientIDs.includes(id) ? this.data.selectedRecipientIDs.filter(value => value !== id) : [...this.data.selectedRecipientIDs, id];
    this.setData({ selectedRecipientIDs: ids, members: picked(this.data.members, ids.map(String)) });
  },
  async previewPackage() {
    if (this.data.busy) return;
    if (!this.data.selectedVersionIDs.length && !this.data.selectedNoteIDs.length) { this.setData({ notice: '请选择至少一个方案。' }); return; }
    if (!this.data.selectedRecipientIDs.length) { this.setData({ notice: '请选择本次接收人。' }); return; }
    const epoch = this._epoch;
    const noteSelections = this.data.notes.filter(item => this.data.selectedNoteIDs.includes(String(item.note_id))).map(item => ({ note_id: item.note_id, revision: Number(item.revision) }));
    const signature = [this.data.draft && this.data.draft.package_id || '', this.data.draft && this.data.draft.revision || 0, ...this.data.selectedVersionIDs, '|', ...noteSelections.map(item => item.note_id + '@' + item.revision), '|', ...this.data.selectedRecipientIDs].join(':');
    if (!this._draftAttempt || this._draftAttempt.signature !== signature) this._draftAttempt = { signature, key: api.mutationKey('project-package-draft') };
    const attempt = this._draftAttempt;
    this.setData({ busy: true, notice: '', error: '' });
    try {
      const previous = this.data.draft;
      const draft = await api.saveProjectPackageDraft(this._projectID, {
        package_id: previous && previous.package_id || '', expected_revision: previous && previous.revision || 0,
        version_ids: this.data.selectedVersionIDs, note_selections: noteSelections, recipient_ids: this.data.selectedRecipientIDs,
        idempotency_key: attempt.key,
      });
      if (!this.current(epoch)) return;
      const preview = await api.previewProjectPackage(draft.package_id);
      if (!this.current(epoch)) return;
      const memberByActor = new Map(this.data.members.map(member => [Number(member.actor_id), member]));
      preview.recipients = (preview.recipients || []).map(recipient => Object.assign({}, recipient, memberByActor.get(Number(recipient.actor_id)) || {}));
      if (this._draftAttempt === attempt) this._draftAttempt = null;
      this.setData({ draft, preview, step: 'preview', busy: false });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '预览生成失败' }); }
  },
  editSelection() { this.setData({ step: 'select', preview: null, notice: '' }); },
  async publishPackage() {
    if (this.data.busy || !this.data.draft || !this.data.preview) return;
    const epoch = this._epoch;
    const signature = [this.data.draft.package_id, this.data.draft.revision, this.data.preview.preview_signature].join(':');
    if (!this._publishAttempt || this._publishAttempt.signature !== signature) this._publishAttempt = { signature, key: api.mutationKey('project-package-publish'), expiresAt: new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString() };
    const attempt = this._publishAttempt;
    this.setData({ busy: true, notice: '' });
    try {
      const delivery = await api.publishProjectPackage(this.data.draft.package_id, {
        expected_revision: this.data.draft.revision, preview_signature: this.data.preview.preview_signature,
        expires_at: attempt.expiresAt,
        idempotency_key: attempt.key,
      });
      if (!this.current(epoch)) return;
      const token = String(delivery.package_token || '');
      if (!token) throw new Error('分享已生成，但本次未返回新链接。请返回项目后重新生成。');
      if (this._publishAttempt === attempt) this._publishAttempt = null;
      const sharePath = '/pages/projectpackageview/projectpackageview?pps=' + encodeURIComponent(token);
      const historyDelivery = this.presentDelivery(delivery, 0);
      this.setData({ delivery, sharePath, step: 'shared', busy: false, historyDeliveries: [historyDelivery, ...this.data.historyDeliveries.filter(item => item.delivery_id !== delivery.delivery_id)] }, () => wx.showShareMenu && wx.showShareMenu({ menus: ['shareAppMessage'] }));
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '分享生成失败' }); }
  },
  async revokeDelivery(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if (!id || this.data.busy) return;
    const epoch = this._epoch; this.setData({ busy: true, notice: '' });
    try {
      await api.revokeProjectPackageDelivery(id);
      if (!this.current(epoch)) return;
      this.setData({ busy: false, historyDeliveries: this.data.historyDeliveries.map(item => item.delivery_id === id ? Object.assign({}, item, { revoked: true }) : item), notice: '分享已撤回，原入口立即失效。' });
    } catch (error) { if (this.current(epoch)) this.setData({ busy: false, notice: error.message || '撤回失败，请重试' }); }
  },
  handleBack() {
    if (this.data.step === 'preview') { this.editSelection(); return; }
    if (this.data.step === 'shared') { this.setData({ step: 'preview' }); wx.hideShareMenu(); return; }
    wx.navigateBack();
  },
  onShareAppMessage() {
    if (!this.data.sharePath || !this.data.delivery) return {};
    return { title: this.data.projectName + ' · 分享资料', path: this.data.sharePath };
  },
});
