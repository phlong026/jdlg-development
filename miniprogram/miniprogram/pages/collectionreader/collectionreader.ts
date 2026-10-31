// @ts-nocheck
'use strict';
const api = require('../../utils/project-collections-api');
const tracking = require('../../utils/tracking/index');

Page({
  data: { webLinkBusy: false, contentReady: false, loading: true, opening: -1, error: '', collection: null, items: [] },
  onLoad(options) {
    this.options = options || {};
    this.loadSequence = (this.loadSequence || 0) + 1;
    this.hasShown = false;
    tracking.resolvePageOptions(this, this.options);
    this.loadCollection();
  },
  onShow() { this.visible = true; if (this.hasShown) this.loadCollection(); this.hasShown = true; },
  onHide() { this.visible = false; this.loadSequence += 1; this.setData({ opening: -1, webLinkBusy: false }); },
  onUnload() { this.visible = false; this.loadSequence += 1; },
  async loadCollection() {
    const id = String(this.options.collection_id || ''), version = Number(this.options.collection_version);
    if (!/^[A-Za-z0-9_-]{8,128}$/.test(id) || !Number.isInteger(version) || version < 1) { this.setData({ loading: false, error: '合集入口不完整' }); return; }
    const sequence = ++this.loadSequence;
    wx.hideShareMenu();
    this.setData({ contentReady: false, loading: true, error: '', collection: null, items: [], opening: -1 });
    try {
      const collection = await api.readPublishedCollection(id, version);
      if (sequence !== this.loadSequence) return;
      if (collection.collection_id !== id || Number(collection.version) !== version) throw new Error('合集版本不匹配');
      if (collection.cover) collection.cover = Object.assign({}, collection.cover, { url: api.mediaURL(collection.cover.url) });
      const items = (collection.items || []).map((item, index) => Object.assign({}, item, { index, cover_url: item.cover ? api.mediaURL(item.cover.url) : '' }));
      this.setData({ collection, items, contentReady: true, loading: false }, () => { tracking.preheatTimelineShare(this, this.timelineShareValue()); if (wx.showShareMenu) wx.showShareMenu({ menus: ['shareAppMessage', 'shareTimeline'] }); });
    } catch (error) { if (sequence === this.loadSequence) this.setData({ contentReady: false, loading: false, error: '合集暂不可访问，可能已撤回或网络不可用。' }); }
  },
  async openItem(event) {
    if (this.data.opening >= 0 || !this.data.collection) return;
    const position = Number(event.currentTarget.dataset.index), item = this.data.items[position];
    if (!item || !item.available) return;
    const id = this.data.collection.collection_id, version = Number(this.data.collection.version);
    const sequence = this.loadSequence;
    this.setData({ opening: position, error: '' });
    try {
      const target = await api.openPublishedItem(id, version, position);
      if (sequence !== this.loadSequence) return;
      const parsed = tracking.splitPath(target.path, ''), query = parsed.query;
      const keys = ['content_id', 'content_version', 'collection_id', 'collection_version', 'item_position'];
      if (target.route_key !== 'pages/contentreader/contentreader' || parsed.route !== target.route_key || Object.keys(query).length !== keys.length || keys.some(key => query[key] === undefined) || !/^[A-Za-z0-9_-]{8,128}$/.test(query.content_id) || !/^[1-9]\d*$/.test(query.content_version) || query.collection_id !== id || Number(query.collection_version) !== version || Number(query.item_position) !== position) throw new Error('作品目标无效');
      const token = tracking.collectionToken(this);
      const path = target.path + (token ? '&st=' + encodeURIComponent(token) : '');
      this.setData({ opening: -1 });
      tracking.emit(this, 'content_click', { target_content_id: query.content_id, component_key: 'collection_item', action_key: 'open_content' });
      wx.navigateTo({ url: path, fail: () => this.setData({ error: '作品打开失败，请重试' }) });
    } catch (error) {
      if (sequence !== this.loadSequence) return;
      this.setData({ opening: -1, error: '该作品已不可访问，请刷新合集。' });
      await this.loadCollection();
    }
  },
  async copyWebLink() {
    if (this.data.webLinkBusy || !this.data.contentReady || !this.data.collection) return;
    const account = String(wx.getStorageSync('token') || '');
    if (!account) { wx.navigateTo({url:'/pages/login/login'}); return; }
    if (!tracking.consented()) { tracking.showConsentChoices(this); return; }
    const collection = this.data.collection, sequence = this.loadSequence;
    const current = () => sequence === this.loadSequence && account === String(wx.getStorageSync('token') || '') && this.data.collection === collection;
    const key = 'web:collection:' + collection.collection_id + ':' + collection.version;
    this.setData({webLinkBusy:true});
    try {
      const prepared = await tracking.prepareShare(this, this.shareValue(), 'app_message', key, false, current);
      if (!current()) return;
      const token = prepared && prepared.path && tracking.splitPath(prepared.path, 'pages/collectionreader/collectionreader').query.st;
      if (!token) throw new Error('分享链接暂时无法生成，请重试');
      const result = await require('../../utils/phase3-api').request('/api/v1/miniprogram/tracking/share-entries', 'POST', {share_token:token});
      if (!current()) return;
      if (!result.web_url || !String(result.web_url).startsWith('https://')) throw new Error('分享网页暂未开通');
      await new Promise((resolve,reject) => wx.setClipboardData({data:result.web_url,success:()=>{if(current())tracking.confirmPreparedShare(this,key,'app_message');resolve();},fail:()=>reject(new Error('链接未能复制，请重试'))}));
    } catch (error) { if(current())wx.showToast({title:error.message || '分享链接暂不可用',icon:'none'}); }
    finally { if(current())this.setData({webLinkBusy:false}); }
  },
  goBack() { wx.navigateBack({ delta: 1, fail: () => wx.switchTab({ url: '/pages/index/index' }) }); },
  onShareAppMessage() { return this.shareValue(); },
  shareValue() {
    const collection = this.data.collection;
    if (!collection) return {};
    return { title: collection.title, path: '/pages/collectionreader/collectionreader?collection_id=' + encodeURIComponent(collection.collection_id) + '&collection_version=' + collection.version, imageUrl: collection.cover ? api.mediaURL(collection.cover.url) : undefined };
  },
  onShareTimeline() { return this.timelineShareValue(); },
  timelineShareValue() {
    const collection = this.data.collection;
    if (!collection) return {};
    return { title: collection.title, query: 'collection_id=' + encodeURIComponent(collection.collection_id) + '&collection_version=' + collection.version, imageUrl: collection.cover ? api.mediaURL(collection.cover.url) : undefined };
  },
});
export {};
