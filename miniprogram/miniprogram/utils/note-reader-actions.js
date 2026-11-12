'use strict';

function capture(page) {
  return { token: String(wx.getStorageSync('token') || ''), id: String(page.options.content_id || ''), version: Number(page.options.content_version), sequence: page.loadSequence };
}
function current(page, proof) {
  return page.pageVisible !== false && page.data.contentReady && proof.sequence === page.loadSequence && proof.token === String(wx.getStorageSync('token') || '') && proof.id === String(page.options.content_id || '') && proof.version === Number(page.options.content_version);
}
function managed(page) { return typeof page.posterAccessCurrent === 'function' && page.posterAccessCurrent(); }
function toast(message) { wx.showToast({ title: message, icon: 'none' }); }
function copyIntent(page, proof, api) {
  const identity = [proof.token, proof.id, proof.version].join('|');
  if (!page._noteCopyIntent || page._noteCopyIntent.identity !== identity) {
    page._noteCopyIntent = { identity, key: api.mutationKey('note-copy') };
  }
  return page._noteCopyIntent;
}

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
  data: { noteMenuOpen: false, noteMenu: [], noteActionBusy: false },
  methods: {
    openNoteMore() {
      if (!this.data.contentReady || this.data.noteActionBusy) return;
      const menu = managed(this) ? [{ key: 'edit', label: '编辑笔记', icon: 'edit' }] : [];
      if (managed(this) && this.data.content.body_format !== 'document_v1') menu.push({ key: 'copy', label: '创建副本', icon: 'copy' });
      menu.push({ key: 'text', label: '复制文案', icon: 'copy', divider: true });
      if ((this.data.media || []).some(item => item.kind === 'image')) menu.push({ key: 'images', label: '保存图片', icon: 'image' });
      if (managed(this)) menu.push({ key: 'word', label: '导出 Word', icon: 'file' });
      if (managed(this) && this.data.posterAvailable) menu.push({ key: 'poster', label: '生成海报', icon: 'image' });
      menu.push({ key: 'link', label: '复制链接', icon: 'link' });
      if (this.data.consentAvailable) menu.push({ key: 'analytics', label: '访问分析设置', icon: 'setting', divider: true });
      if (managed(this)) menu.push({ key: 'settings', label: '笔记设置', icon: 'setting' }, { key: 'delete', label: '删除笔记', icon: 'delete', danger: true });
      this.setData({ noteMenu: menu, noteMenuOpen: !this.data.noteMenuOpen });
    },
    closeNoteMore() { this.setData({ noteMenuOpen: false }); },
    keepNoteMenuOpen() {},
    selectNoteMenu(event) {
      const key = String(event.currentTarget.dataset.key || '');
      const handlers = { edit: 'editCurrentNote', settings: 'editCurrentNote', copy: 'copyNote', text: 'copyNoteText', images: 'saveNoteImages', word: 'exportNoteWord', poster: 'generatePoster', link: 'copyWebLink', analytics: 'openTrackingSettings', delete: 'deleteCurrentNote' };
      if (!this.data.noteMenu.some(item => item.key === key) || !handlers[key]) return;
      this.closeNoteMore();
      return this[handlers[key]]();
    },
    editCurrentNote() {
      if (!this.data.contentReady || !managed(this)) return;
      wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(this.options.content_id), fail: () => toast('编辑页未能打开，请重试') });
    },
    async copyNote() {
      if (!this.data.contentReady || !managed(this) || this.data.noteActionBusy) return;
      const proof = capture(this), content = this.data.content, api = require('./phase3-api');
      const intent = copyIntent(this, proof, api);
      this.setData({ noteActionBusy: true });
      try {
        const saved = await api.saveContent({ title: String(content.title).slice(0, 76) + ' 副本', body: content.body, body_format: content.body_format, owner_id: content.owner_id, rights: content.rights, source_note: content.source_note || '', source_url: content.source_url || '', cover_media_id: content.cover && content.cover.media_id || '', media_ids: (content.media || []).map(item => item.media_id), tags: content.tags || [], expected_revision: 0, idempotency_key: intent.key });
        if (!current(this, proof) || !managed(this)) return;
        this._noteCopyIntent = null;
        wx.navigateTo({ url: '/pages/contentedit/contentedit?id=' + encodeURIComponent(saved.content_id), fail: () => toast('副本已保存，可从工作台打开') });
      } catch (error) { if (current(this, proof)) toast(error.message || '副本创建失败，请重试'); }
      finally { if (current(this, proof)) this.setData({ noteActionBusy: false }); }
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
  },
};
