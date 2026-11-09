'use strict';

function capture(page) {
  return { token: String(wx.getStorageSync('token') || ''), id: String(page.options.content_id || ''), version: Number(page.options.content_version), sequence: page.loadSequence };
}
function current(page, proof) {
  return page.pageVisible !== false && page.data.contentReady && proof.sequence === page.loadSequence && proof.token === String(wx.getStorageSync('token') || '') && proof.id === String(page.options.content_id || '') && proof.version === Number(page.options.content_version);
}
function managed(page) { return typeof page.posterAccessCurrent === 'function' && page.posterAccessCurrent(); }
function toast(message) { wx.showToast({ title: message, icon: 'none' }); }

async function albumImagePath(source) {
  if (source.startsWith('wxfile://') || source.startsWith('http://tmp/') || source.startsWith('/')) return source;
  const url = await require('./protected-media').resolveProtectedMediaURL(source);
  if (!url.startsWith('https://')) return url;
  return new Promise((resolve, reject) => wx.downloadFile({
    url, timeout: 60000,
    success(result) {
      if (result.statusCode >= 200 && result.statusCode < 300 && result.tempFilePath) resolve(result.tempFilePath);
      else reject(new Error('图片下载失败，请重试'));
    },
    fail: () => reject(new Error('图片下载失败，请重试')),
  }));
}

module.exports = {
  data: { noteMenuOpen: false, noteMenu: [], noteActionBusy: false, collectionsOpen: false, noteCollections: [], noteCollectionCursor: '', noteCollectionTitle: '', noteCollectionError: '' },
  methods: {
    openNoteMore() {
      if (!this.data.contentReady || this.data.noteActionBusy) return;
      const menu = managed(this) ? [
        { key: 'edit', label: '编辑笔记', icon: 'edit' },
        { key: 'copy', label: '创建副本', icon: 'copy' },
        { key: 'collection', label: '加入合集', icon: 'folder' },
      ] : [];
      menu.push({ key: 'text', label: '复制文案', icon: 'copy', divider: true });
      if ((this.data.media || []).some(item => item.kind === 'image')) menu.push({ key: 'images', label: '保存图片', icon: 'image' });
      if (managed(this)) menu.push({ key: 'word', label: '导出 Word', icon: 'word' });
      if (managed(this) && this.data.posterAvailable) menu.push({ key: 'poster', label: '生成海报', icon: 'image' });
      menu.push({ key: 'link', label: '复制链接', icon: 'link' });
      if (this.data.consentAvailable) menu.push({ key: 'analytics', label: '访问分析设置', icon: 'settings', divider: true });
      if (managed(this)) menu.push({ key: 'settings', label: '笔记设置', icon: 'settings' }, { key: 'delete', label: '删除笔记', icon: 'delete', danger: true });
      this.setData({ noteMenu: menu, noteMenuOpen: !this.data.noteMenuOpen });
    },
    closeNoteMore() { this.setData({ noteMenuOpen: false }); },
    keepNoteMenuOpen() {},
    selectNoteMenu(event) {
      const key = String(event.currentTarget.dataset.key || '');
      const handlers = { edit: 'editCurrentNote', settings: 'editCurrentNote', copy: 'copyNote', collection: 'openNoteCollections', text: 'copyNoteText', images: 'saveNoteImages', word: 'exportNoteWord', poster: 'generatePoster', link: 'copyWebLink', analytics: 'openTrackingSettings', delete: 'deleteCurrentNote' };
      if (!this.data.noteMenu.some(item => item.key === key) || !handlers[key]) return;
      this.closeNoteMore();
      return this[handlers[key]]();
    },
    editCurrentNote() {
      if (!this.data.contentReady || !managed(this)) return;
      wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(this.options.content_id), fail: () => toast('编辑页未能打开，请重试') });
    },
    copyNoteText() {
      const view = this.data.presentationView;
      if (!this.data.contentReady || !view) return;
      const body = view.kind === 'article' && Array.isArray(view.blocks)
        ? view.blocks.map(block => block.text || (block.items || []).join('\n') || String(block.html || '').replace(/<br\s*\/?>|<\/(?:p|div|section)>/gi, '\n').replace(/<[^<>]*>/g, '').replace(/&(nbsp|amp|lt|gt|quot|#39);/g, (_, entity) => ({nbsp:' ',amp:'&',lt:'<',gt:'>',quot:'"','#39':"'"}[entity]))).filter(Boolean).join('\n\n')
        : view.bodyText;
      const text = [view.title, body].filter(Boolean).join('\n\n');
      wx.setClipboardData({ data: text, fail: () => toast('文案未能复制，请重试') });
    },
    async copyNote() {
      if (!this.data.contentReady || !managed(this) || this.data.noteActionBusy) return;
      const proof = capture(this), content = this.data.content;
      this.setData({ noteActionBusy: true });
      try {
        const api = require('./phase3-api');
        const saved = await api.saveContent({ title: String(content.title).slice(0, 76) + ' 副本', body: content.body, body_format: content.body_format, owner_id: content.owner_id, rights: content.rights, source_note: content.source_note || '', source_url: content.source_url || '', cover_media_id: content.cover && content.cover.media_id || '', media_ids: (content.media || []).map(item => item.media_id), tags: content.tags || [], expected_revision: 0 });
        if (!current(this, proof) || !managed(this)) return;
        wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(saved.content_id), fail: () => toast('副本已保存，可从工作台打开') });
      } catch (error) { if (current(this, proof)) toast(error.message || '副本创建失败，请重试'); }
      finally { if (current(this, proof)) this.setData({ noteActionBusy: false }); }
    },
    async saveNoteImages() {
      if (!this.data.contentReady || this.data.noteActionBusy) return;
      const images = (this.data.media || []).filter(item => item.kind === 'image' && item.url);
      if (!images.length) { toast('暂无可保存图片'); return; }
      const proof = capture(this);
      this.setData({ noteActionBusy: true });
      try {
        for (const item of images) {
          const url = await albumImagePath(item.url);
          if (!current(this, proof)) return;
          await new Promise((resolve, reject) => wx.saveImageToPhotosAlbum({ filePath: url, success: resolve, fail: () => reject(new Error('图片未能保存，请检查相册权限后重试')) }));
          if (!current(this, proof)) return;
        }
        toast('图片已保存到相册');
      } catch (error) { if (current(this, proof)) toast(error.message || '图片保存失败，请重试'); }
      finally { if (current(this, proof)) this.setData({ noteActionBusy: false }); }
    },
    async exportNoteWord() {
      if (!this.data.contentReady || !managed(this) || this.data.noteActionBusy) return;
      const proof = capture(this);
      this.setData({ noteActionBusy: true });
      try {
        const filePath = await require('./phase3-api').downloadContentWord(proof.id, proof.version);
        if (!current(this, proof) || !managed(this)) return;
        await new Promise((resolve, reject) => wx.openDocument({ filePath, fileType: 'docx', showMenu: true, success: resolve, fail: () => reject(new Error('Word 文件打开失败，请重试')) }));
      } catch (error) { if (current(this, proof)) toast(error.message || 'Word 导出失败，请重试'); }
      finally { if (current(this, proof)) this.setData({ noteActionBusy: false }); }
    },
    async deleteCurrentNote() {
      if (!this.data.contentReady || !managed(this) || this.data.noteActionBusy) return;
      const proof = capture(this);
      const confirmed = await new Promise(resolve => wx.showModal({ title: '删除这篇笔记？', content: '删除后原分享入口将不可访问。', confirmText: '删除', confirmColor: '#b3483e', success: result => resolve(result.confirm === true), fail: () => resolve(false) }));
      if (!confirmed || !current(this, proof) || !managed(this)) return;
      this.setData({ noteActionBusy: true });
      try {
        const api = require('./phase3-api'), latest = await api.getContent(proof.id);
        if (!current(this, proof) || !managed(this)) return;
        await api.deleteCreatedContent(proof.id, latest.revision);
        if (!current(this, proof)) return;
        this.setData({ contentReady: false, content: null, presentationView: null, posterAvailable: false, error: '笔记已删除，原分享入口已关闭。' });
        if (wx.hideShareMenu) wx.hideShareMenu();
      } catch (error) { if (current(this, proof)) toast(error.message || '删除失败，请刷新后重试'); }
      finally { if (proof.token === String(wx.getStorageSync('token') || '') && proof.sequence === this.loadSequence) this.setData({ noteActionBusy: false }); }
    },
    async openNoteCollections(more = false) {
      if (!this.data.contentReady || !managed(this) || this.data.noteActionBusy) return;
      const proof = capture(this);
      this.setData({ noteActionBusy: true, collectionsOpen: true, noteCollectionError: '' });
      try {
        const page = await require('./project-collections-api').listCollections({ cursor: more === true ? this.data.noteCollectionCursor : '', limit: 20 });
        if (!current(this, proof) || !managed(this)) return;
        const items = (page.items || []).filter(item => item.owner_id === this.data.content.owner_id && item.status !== 'revoked');
        this.setData({ noteCollections: more === true ? this.data.noteCollections.concat(items) : items, noteCollectionCursor: page.next_cursor || '' });
      } catch (error) { if (current(this, proof)) this.setData({ noteCollectionError: error.message || '合集读取失败，请重试' }); }
      finally { if (current(this, proof)) this.setData({ noteActionBusy: false }); }
    },
    loadMoreNoteCollections() { return this.openNoteCollections(true); },
    closeNoteCollections() { this.setData({ collectionsOpen: false }); },
    setNoteCollectionTitle(event) { this.setData({ noteCollectionTitle: event.detail.value }); },
    async addNoteCollection(event) {
      if (!this.data.contentReady || !managed(this) || this.data.noteActionBusy) return;
      const proof = capture(this), id = String(event.currentTarget.dataset.id || '');
      if (id && !this.data.noteCollections.some(item => item.collection_id === id)) return;
      if (!id && !this.data.noteCollectionTitle.trim()) { this.setData({ noteCollectionError: '请填写合集名称' }); return; }
      this.setData({ noteActionBusy: true, noteCollectionError: '' });
      try {
        const api = require('./project-collections-api');
        const collection = id ? await api.getCollection(id) : null;
        if (!current(this, proof) || !managed(this)) return;
        const items = collection ? (collection.items || []).map(item => ({ content_id: item.content_id, content_version: item.content_version })) : [];
        if (!items.some(item => item.content_id === proof.id && Number(item.content_version) === proof.version)) items.push({ content_id: proof.id, content_version: proof.version });
        if (!id) {
          const identity = JSON.stringify([proof.token, proof.id, proof.version, this.data.noteCollectionTitle.trim()]);
          if (!this._noteCollectionAttempt || this._noteCollectionAttempt.identity !== identity) this._noteCollectionAttempt = { identity, key: api.mutationKey('note-collection') };
        }
        await api.saveCollection({ title: collection ? collection.title : this.data.noteCollectionTitle.trim(), description: collection && collection.description || '', owner_id: this.data.content.owner_id, items, expected_revision: collection ? collection.revision : 0, idempotency_key: id ? '' : this._noteCollectionAttempt.key, ...(collection && collection.cover_item_index != null ? { cover_item_index: collection.cover_item_index } : {}) }, id);
        if (!current(this, proof) || !managed(this)) return;
        this._noteCollectionAttempt = null;
        this.setData({ collectionsOpen: false, noteCollectionTitle: '' });
        toast('已加入合集草稿');
      } catch (error) { if (current(this, proof)) this.setData({ noteCollectionError: error.message || '加入合集失败，请重试' }); }
      finally { if (current(this, proof)) this.setData({ noteActionBusy: false }); }
    },
  },
};
