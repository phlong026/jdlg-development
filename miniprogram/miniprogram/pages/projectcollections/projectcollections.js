'use strict';
const api = require('../../utils/project-collections-api');
const { resolveProtectedMediaURL } = require('../../utils/protected-media');

const initial = () => ({
  loading: false, loadingMore: false, busy: false, loginRequired: false, denied: false,
  error: '', notice: '', collections: [], nextCursor: '', editing: false,
  current: null, title: '', description: '', owners: [], ownerChoices: [], ownerIndex: 0,
  candidates: [], candidateCursor: '', selectedItems: [], preview: null, dirty: false, coverItemIndex: null,
});
const sourceKey = item => String(item.content_id) + ':' + String(item.content_version);

Page({
  data: initial(),
  onLoad(options) { this.requestedID = String((options && options.id) || ''); },
  onShow() {
    wx.hideShareMenu();
    this.visible = true;
    const token = String(wx.getStorageSync('token') || '');
    if (this.account !== token) { this.account = token; this.loaded = false; this.createKey = ''; this.publishKey = ''; this.setData(initial()); }
    this.epoch = (this.epoch || 0) + 1;
    if (!token) { this.setData({ loginRequired: true, loading: false }); return; }
    this.loadHome(this.epoch);
  },
  onHide() { this.visible = false; this.epoch = (this.epoch || 0) + 1; },
  onUnload() { this.visible = false; this.epoch = (this.epoch || 0) + 1; },
  currentRequest(epoch) { return this.visible && epoch === this.epoch && this.account === String(wx.getStorageSync('token') || ''); },
  decorate(item) {
    const cover = item && item.cover ? api.mediaURL(item.cover.url) : '';
    return Object.assign({}, item, { cover_url: cover, status_text: item.status === 'revoked' ? '已撤回' : item.version > 0 ? '已发布 V' + item.version : '草稿' });
  },
  async resolveCovers(items) {
    return Promise.all((items || []).map(async item => { const decorated = this.decorate(item); return decorated.cover_url ? Object.assign({}, decorated, { cover_url: await resolveProtectedMediaURL(decorated.cover_url) }) : decorated; }));
  },
  async loadHome(epoch) {
    const quiet = this.loaded === true;
    this.setData({ loading: !quiet, loginRequired: false, denied: false, error: '', notice: '' });
    try {
      const [context, page] = await Promise.all([api.workbenchContext(), api.listCollections({ limit: 20 })]);
      if (!this.currentRequest(epoch)) return;
      const owners = Array.isArray(context.owners) ? context.owners : [];
      const ownerChoices = Array.isArray(context.owner_choices) ? context.owner_choices : [];
      const selectedChoice = this.data.ownerChoices[this.data.ownerIndex];
      const selectedOwner = this.data.current ? this.data.current.owner_id : selectedChoice && selectedChoice.owner_id;
      const collections = await this.resolveCovers(page.items);
      if (!this.currentRequest(epoch)) return;
      this.setData({ owners, ownerChoices, ownerIndex: Math.max(0, ownerChoices.findIndex(item => item.owner_id === selectedOwner)), busy: false, collections, nextCursor: page.next_cursor || '', loading: false });
      this.loaded = true;
      if (this.requestedID) { const id = this.requestedID; this.requestedID = ''; await this.openEditorByID(id, epoch); }
    } catch (error) {
      if (!this.currentRequest(epoch)) return;
      const denied = error.statusCode === 403, loginRequired = error.statusCode === 401;
      if (denied || loginRequired) {
        this.loaded = false;
        this.epoch = (this.epoch || 0) + 1;
        this.createKey = ''; this.publishKey = '';
        this.setData({ ...initial(), denied, loginRequired });
        return;
      }
      this.setData({
        loading: false, denied, loginRequired,
        error: denied || loginRequired ? '' : (error.message || '合集加载失败'),
      });
    }
  },
  async loadMore() {
    if (this.data.loadingMore || !this.data.nextCursor) return;
    const epoch = this.epoch;
    this.setData({ loadingMore: true, error: '' });
    try {
      const page = await api.listCollections({ cursor: this.data.nextCursor, limit: 20 });
      if (!this.currentRequest(epoch)) return;
      const seen = new Set(this.data.collections.map(item => item.collection_id));
      const incoming = await this.resolveCovers((page.items || []).filter(item => !seen.has(item.collection_id)));
      if (!this.currentRequest(epoch)) return;
      this.setData({ collections: this.data.collections.concat(incoming), nextCursor: page.next_cursor || '', loadingMore: false });
    } catch (error) { if (this.currentRequest(epoch)) this.setData({ loadingMore: false, error: error.message || '加载失败' }); }
  },
  async startCreate() {
    this.createKey = this.createKey || api.mutationKey('collection-create');
    this.publishKey = '';
    this.setData({ editing: true, current: null, title: '', description: '', selectedItems: [], preview: null, error: '', notice: '', ownerIndex: 0, coverItemIndex: null, dirty: true });
    await this.loadCandidates(false);
  },
  openEditor(event) { return this.openEditorByID(String(event.currentTarget.dataset.id || ''), this.epoch); },
  async openEditorByID(id, epoch) {
    if (!id || this.data.busy) return;
    this.setData({ busy: true, error: '', preview: null });
    try {
      const collection = await api.getCollection(id);
      if (!this.currentRequest(epoch)) return;
      const ownerIndex = Math.max(0, this.data.ownerChoices.findIndex(item => item.owner_id === collection.owner_id));
      this.createKey = '';
      this.publishKey = '';
      const selectedItems = await this.resolveCovers(collection.items);
      if (!this.currentRequest(epoch)) return;
      const coverIndex = selectedItems.findIndex(item => collection.cover && collection.cover.media_id && item.cover && item.cover.media_id === collection.cover.media_id);
      this.setData({ editing: true, current: collection, title: collection.title || '', description: collection.description || '', selectedItems, coverItemIndex: coverIndex >= 0 ? coverIndex : null, ownerIndex, busy: false, dirty: false });
      await this.loadCandidates(false);
    } catch (error) { if (this.currentRequest(epoch)) this.setData({ busy: false, error: error.message || '合集不可访问' }); }
  },
  closeEditor() {
    if (this.data.busy) return;
    const epoch = this.epoch;
    const close = () => { if (!this.currentRequest(epoch)) return; this.createKey = ''; this.setData({ editing: false, current: null, preview: null, candidates: [], candidateCursor: '', error: '', notice: '', dirty: false }); };
    if (!this.data.dirty) { close(); return; }
    wx.showModal({ title: '放弃未保存的修改？', confirmText: '放弃修改', cancelText: '继续编辑', success: result => { if (result.confirm) close(); } });
  },
  goBack() { if (this.data.editing) { this.closeEditor(); return; } wx.navigateBack({delta:1}); },
  setTitle(event) { this.setData({ title: event.detail.value, dirty: true, preview: null }); },
  setDescription(event) { this.setData({ description: event.detail.value, dirty: true, preview: null }); },
  setOwner(event) { if (!this.data.current) this.setData({ ownerIndex: Number(event.detail.value) || 0, dirty: true, preview: null }); },
  async loadCandidates(more) {
    if (this.data.busy && more) return;
    const cursor = more ? this.data.candidateCursor : '';
    if (more && !cursor) return;
    const epoch = this.epoch;
    if (more) this.setData({ busy: true });
    try {
      const page = await api.listCandidates({ cursor, limit: 20 });
      if (!this.currentRequest(epoch)) return;
      const decorated = await this.resolveCovers(page.items);
      if (!this.currentRequest(epoch)) return;
      const incoming = decorated.map(item => ({...item, selected: this.data.selectedItems.some(ref => sourceKey(ref) === sourceKey(item))}));
      const base = more ? this.data.candidates : [];
      const seen = new Set(base.map(sourceKey));
      this.setData({ candidates: base.concat(incoming.filter(item => !seen.has(sourceKey(item)))), candidateCursor: page.next_cursor || '', busy: false });
    } catch (error) { if (this.currentRequest(epoch)) this.setData({ busy: false, error: error.message || '可选内容加载失败' }); }
  },
  loadMoreCandidates() { return this.loadCandidates(true); },
  toggleCandidate(event) {
    if (this.data.busy) return;
    const id = String(event.currentTarget.dataset.id || ''), version = Number(event.currentTarget.dataset.version);
    const key = id + ':' + version;
    const selected = this.data.selectedItems.slice();
    const index = selected.findIndex(item => sourceKey(item) === key);
    if (index >= 0) selected.splice(index, 1);
    else {
      const source = this.data.candidates.find(item => sourceKey(item) === key);
      if (source) selected.push(source);
    }
    this.setData({ selectedItems: selected, coverItemIndex: this.reindexCover(selected), preview: null, dirty: true, candidates: this.data.candidates.map(item => ({...item, selected: selected.some(ref => sourceKey(ref) === sourceKey(item))})) });
  },
  removeItem(event) {
    const index = Number(event.currentTarget.dataset.index);
    if (!Number.isInteger(index) || index < 0 || index >= this.data.selectedItems.length) return;
    const items = this.data.selectedItems.slice(); items.splice(index, 1);
    this.setData({ selectedItems: items, coverItemIndex: this.reindexCover(items), preview: null, dirty: true, candidates: this.data.candidates.map(item => ({...item, selected: items.some(ref => sourceKey(ref) === sourceKey(item))})) });
  },
  moveItem(event) {
    const index = Number(event.currentTarget.dataset.index), direction = Number(event.currentTarget.dataset.direction);
    const target = index + direction;
    if (!Number.isInteger(index) || ![-1, 1].includes(direction) || target < 0 || target >= this.data.selectedItems.length) return;
    const items = this.data.selectedItems.slice();
    [items[index], items[target]] = [items[target], items[index]];
    this.setData({ selectedItems: items, coverItemIndex: this.reindexCover(items), preview: null, dirty: true, candidates: this.data.candidates.map(item => ({...item, selected: items.some(ref => sourceKey(ref) === sourceKey(item))})) });
  },
  // B12 置顶：长列表省去逐级上移。条目重排后封面索引跟随条目走。
  topItem(event) {
    const index = Number(event.currentTarget.dataset.index);
    if (!Number.isInteger(index) || index <= 0 || index >= this.data.selectedItems.length) return;
    const items = this.data.selectedItems.slice();
    const [moved] = items.splice(index, 1);
    items.unshift(moved);
    this.setData({ selectedItems: items, coverItemIndex: this.reindexCover(items), preview: null, dirty: true, candidates: this.data.candidates.map(item => ({...item, selected: items.some(ref => sourceKey(ref) === sourceKey(item))})) });
  },
  // B12 封面自选：记录所选条目当前序号；重排时同步平移，移除时清空回退首图。
  setCoverItem(event) {
    const index = Number(event.currentTarget.dataset.index);
    if (!Number.isInteger(index) || index < 0 || index >= this.data.selectedItems.length) return;
    this.setData({ coverItemIndex: this.data.coverItemIndex === index ? null : index, preview: null, dirty: true });
  },
  reindexCover(items) {
    if (this.data.coverItemIndex === null || this.data.coverItemIndex === undefined) return null;
    const current = this.data.selectedItems[this.data.coverItemIndex];
    if (!current) return null;
    const next = items.findIndex(item => sourceKey(item) === sourceKey(current));
    return next >= 0 ? next : null;
  },
  draftInput() {
    const choice = this.data.ownerChoices[this.data.ownerIndex];
    const owner = this.data.current ? this.data.current.owner_id : choice && choice.owner_id;
    const input = {
      title: String(this.data.title || '').trim(), description: String(this.data.description || '').trim(), owner_id: owner || '',
      expected_revision: this.data.current ? Number(this.data.current.revision) : 0,
      idempotency_key: this.data.current ? '' : this.createKey,
      items: this.data.selectedItems.map(item => ({ content_id: item.content_id, content_version: Number(item.content_version) })),
    };
    // B12 封面自选：显式指定时才提交索引，未指定保持服务端"第一个有封面条目"的历史行为。
    if (this.data.coverItemIndex !== null && this.data.coverItemIndex !== undefined) input.cover_item_index = Number(this.data.coverItemIndex);
    return input;
  },
  async saveDraft() {
    if (this.data.busy) return null;
    const input = this.draftInput();
    if (!input.title) { this.setData({ error: '请填写合集名称' }); return null; }
    if (!input.owner_id) { this.setData({ error: '请选择内容归属' }); return null; }
    if (!input.items.length) { this.setData({ error: '请至少选择一个作品' }); return null; }
    const epoch = this.epoch, id = this.data.current && this.data.current.collection_id;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      const saved = await api.saveCollection(input, id || '');
      if (!this.currentRequest(epoch)) return null;
      let changedDuringSave = JSON.stringify(this.draftInput()) !== JSON.stringify(input);
      const selectedItems = changedDuringSave ? [] : await this.resolveCovers(saved.items);
      if (!this.currentRequest(epoch)) return null;
      changedDuringSave = changedDuringSave || JSON.stringify(this.draftInput()) !== JSON.stringify(input);
      this.createKey = '';
      this.setData(Object.assign({ current: saved, busy: false, preview: null, notice: '已保存', dirty: changedDuringSave }, changedDuringSave ? {} : { title: saved.title, description: saved.description || '', selectedItems }));
      return saved;
    } catch (error) { if (this.currentRequest(epoch)) this.setData({ busy: false, error: error.message || '保存失败' }); return null; }
  },
  async previewCurrent() {
    let collection = this.data.current;
    if (!collection || this.data.dirty) collection = await this.saveDraft();
    if (!collection || this.data.busy || this.data.dirty) return;
    const epoch = this.epoch;
    this.setData({ busy: true, error: '' });
    try {
      const preview = await api.previewCollection(collection.collection_id, collection.revision);
      if (!this.currentRequest(epoch)) return;
      const [collections, items] = await Promise.all([this.resolveCovers([preview.collection]), this.resolveCovers(preview.collection.items)]);
      if (this.currentRequest(epoch)) this.setData({ preview: { ...preview, collection: { ...collections[0], items } }, busy: false, notice: '' });
    } catch (error) { if (this.currentRequest(epoch)) this.setData({ busy: false, error: error.message || '预览失败' }); }
  },
  closePreview() { if (!this.data.busy) this.setData({ preview: null }); },
  async publishCurrent() {
    const collection = this.data.current, preview = this.data.preview;
    if (!collection || !preview || this.data.busy) return;
    this.publishKey = this.publishKey || api.mutationKey('collection-publish');
    const epoch = this.epoch;
    this.setData({ busy: true, error: '' });
    try {
      const published = await api.publishCollection(collection.collection_id, collection.revision, preview.preview_token, this.publishKey);
      if (!this.currentRequest(epoch)) return;
      this.publishKey = '';
      const current = Object.assign({}, collection, published);
      this.setData({ current, preview: null, busy: false, notice: '已发布 V' + published.version });
      await this.refreshList(epoch);
    } catch (error) { if (this.currentRequest(epoch)) this.setData({ busy: false, error: error.message || '发布失败' }); }
  },
  async refreshList(epoch) {
    const page = await api.listCollections({ limit: 20 });
    if (!this.currentRequest(epoch)) return;
    const collections = await this.resolveCovers(page.items);
    if (this.currentRequest(epoch)) this.setData({ collections, nextCursor: page.next_cursor || '' });
  },
  openCollectionMore(event) {
    if (this.data.busy) return;
    const id = String(event.currentTarget.dataset.id || '');
    const collection = this.data.collections.find(item => item.collection_id === id);
    if (!collection || !(collection.version === 0 || collection.status === 'published')) return;
    const epoch = this.epoch, draft = collection.version === 0;
    wx.showActionSheet({
      itemList: [draft ? '删除草稿' : '撤回发布'],
      success: result => {
        if (result.tapIndex !== 0 || !this.currentRequest(epoch) || this.data.busy) return;
        const current = this.data.collections.find(item => item.collection_id === id);
        if (!current || current.revision !== collection.revision || current.status !== collection.status) return;
        if (draft) this.deleteDraft(event); else this.revokePublished(event);
      },
    });
  },
  deleteCollectionMenu(event) {
	if (this.data.busy) return;
	const id = String(event.currentTarget.dataset.id || '');
	const collection = this.data.collections.find(item => item.collection_id === id);
	if (!collection) return;
	const epoch = this.epoch, revision = Number(collection.revision);
	wx.showActionSheet({ itemList: ['删除合集'], success: result => {
		if (result.tapIndex !== 0 || !this.currentRequest(epoch)) return;
		const current = this.data.collections.find(item => item.collection_id === id);
		if (!current || Number(current.revision) !== revision) return;
		wx.showModal({ title: '删除合集', content: '删除后已发出的合集将不可访问，原作品保留。', confirmText: '删除', confirmColor: '#c4543a', success: modal => { if (modal.confirm) this.performDelete(current); } });
	} });
  },
  deleteDraft(event) {
    const collection = this.data.collections.find(item => item.collection_id === String(event.currentTarget.dataset.id || ''));
    if (!collection || collection.version > 0 || this.data.busy) return;
    wx.showModal({ title: '删除草稿', content: '原作品不会被删除。', success: result => { if (result.confirm) this.performDelete(collection); } });
  },
  async performDelete(collection) {
    const epoch = this.epoch; this.setData({ busy: true, error: '' });
    try { await api.deleteCollection(collection.collection_id, collection.revision); if (this.currentRequest(epoch)) this.setData({ collections: this.data.collections.filter(item => item.collection_id !== collection.collection_id), busy: false }); }
    catch (error) { if (this.currentRequest(epoch)) { this.setData({ busy: false, error: error.message || '删除失败' }); if (error.statusCode === 409) this.refreshList(epoch); } }
  },
  revokePublished(event) {
    const collection = this.data.collections.find(item => item.collection_id === String(event.currentTarget.dataset.id || ''));
    if (!collection || collection.status !== 'published' || this.data.busy) return;
    wx.showModal({ title: '撤回合集', content: '撤回后已发出的合集将不可访问。', success: result => { if (result.confirm) this.performRevoke(collection); } });
  },
  async performRevoke(collection) {
    const epoch = this.epoch, key = api.mutationKey('collection-revoke'); this.setData({ busy: true, error: '' });
    try { await api.revokeCollection(collection.collection_id, collection.revision, '创建者撤回', key); if (this.currentRequest(epoch)) await this.refreshList(epoch); }
    catch (error) { if (this.currentRequest(epoch)) this.setData({ error: error.message || '撤回失败' }); }
    finally { if (this.currentRequest(epoch)) this.setData({ busy: false }); }
  },
  openPublished() {
    const current = this.data.current;
    if (!current || this.data.dirty || current.status !== 'published' || Number(current.version) < 1) return;
    this.openReader({ currentTarget: { dataset: { id: current.collection_id, version: current.version } } });
  },
  openReader(event) {
    const id = String(event.currentTarget.dataset.id || ''), version = Number(event.currentTarget.dataset.version);
    if (!id || !Number.isInteger(version) || version < 1) return;
    wx.navigateTo({ url: '/pages/collectionreader/collectionreader?collection_id=' + encodeURIComponent(id) + '&collection_version=' + version });
  },
  retry() { this.onShow(); },
  goLogin() { wx.navigateTo({ url: '/pages/login/login' }); },
});
