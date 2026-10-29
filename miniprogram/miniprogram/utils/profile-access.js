'use strict';
const routes = require('./tracking/routes');
let installed = false;

// Run outside the analytics wrapper: content access must not depend on analytics
// availability or consent, and an unverified page must not create a visit.
function wrapProfileAccess(definition) {
  const signature = options => JSON.stringify(Object.keys(options || {}).sort().map(key => [key, options[key]]));
  const invoke = (name, page, args = []) => typeof definition[name] === 'function'
    ? definition[name].apply(page, args) : undefined;
  const protectedPage = page => routes.policy(page.route).collect === true;
  const lock = page => {
    page.__accessRevision = (page.__accessRevision || 0) + 1;
    if (page.__accessActive) invoke('onHide', page);
    page.__accessActive = false;
    page.setData({ __profileAccessReady: false });
    if (typeof wx.hideShareMenu === 'function') wx.hideShareMenu({ menus: ['shareAppMessage', 'shareTimeline'] });
  };
  const verify = page => {
    if (!page.__accessMounted || !page.__accessVisible || page.__accessDestroyed) return;
    const guard = typeof page.selectComponent === 'function' && page.selectComponent('#profileAccessGuard');
    if (!guard || typeof guard.check !== 'function') {
      console.error('个人资料门禁组件未就绪', page.route);
      return;
    }
    Promise.resolve(guard.check()).catch(error => console.error('个人资料核验失败', page.route, error));
  };
  const result = { ...definition, data: { ...definition.data, __profileAccessReady: false, __profileAccessMounted: false } };
  result.onLoad = function(options) {
    if (!protectedPage(this)) return invoke('onLoad', this, arguments);
    this.__accessOptions = options || {};
    this.__accessEntrySignature = signature(options);
    this.__accessDestroyed = false;
    this.__accessLoaded = false;
    this.__accessRendered = false;
    this.__accessActive = false;
  };
  result.onShow = function() {
    if (!protectedPage(this)) return invoke('onShow', this, arguments);
    if (this.__accessDestroyed) return;
    this.__accessVisible = true;
    lock(this);
    verify(this);
  };
  result.onReady = function() {
    if (!protectedPage(this)) return invoke('onReady', this, arguments);
    this.__accessMounted = true;
    verify(this);
  };
  result.onProfileAccessBlocked = function() {
    if (protectedPage(this)) lock(this);
  };
  result.onProfileAccessAppHide = function() {
    if (!protectedPage(this)) return;
    this.__accessVisible = false;
    lock(this);
  };
  result.onProfileAccessGranted = function(event) {
    const token = wx.getStorageSync('token');
    if (!protectedPage(this) || this.__accessDestroyed || !this.__accessVisible || this.__accessActive
      || !token || event?.detail?.token !== token) return;
    const revision = this.__accessRevision = (this.__accessRevision || 0) + 1;
    const userID = Number(event?.detail?.profile?.id || 0);
    if (this.__accessUserID && userID && this.__accessUserID !== userID && !this.__accessReload) {
      this.__accessReload = true;
      this.__accessOptions = { ...this.__accessOptions };
      delete this.__accessOptions.st;
      delete this.__accessOptions.scene;
    }
    this.__accessUserID = userID;
    this.__accessActive = true;
    if (this.__accessReload && this.__accessLoaded) {
      invoke('onUnload', this);
      // Keep request generations monotonic so a late response for the previous
      // item cannot become valid again after this warm navigation.
      this.setData({ ...JSON.parse(JSON.stringify(definition.data || {})), __profileAccessReady: false });
      this.__accessLoaded = false;
      this.__accessRendered = false;
    }
    this.__accessReload = false;
    if (!this.__accessLoaded) {
      this.__accessLoaded = true;
      invoke('onLoad', this, [this.__accessOptions]);
    }
    this.setData({ __profileAccessReady: true, __profileAccessMounted: true }, () => {
      if (revision !== this.__accessRevision || !this.__accessActive || !this.__accessVisible || this.__accessDestroyed) return;
      if (token !== wx.getStorageSync('token')) { lock(this); verify(this); return; }
      invoke('onShow', this);
      if (!this.__accessRendered) { this.__accessRendered = true; invoke('onReady', this); }
    });
  };
  result.queueProfileEntry = function(options) {
    if (!protectedPage(this) || this.__accessDestroyed) return;
    const nextSignature = signature(options);
    if (nextSignature === this.__accessEntrySignature) return;
    this.__accessEntrySignature = nextSignature;
    this.__accessOptions = { ...(options || {}) };
    this.__accessReload = this.__accessLoaded === true;
    lock(this);
    verify(this);
  };
  if (typeof definition.acceptExternalEntry === 'function') {
    result.acceptExternalEntry = function(options) { this.queueProfileEntry(options); };
  }
  result.onHide = function() {
    if (!protectedPage(this)) return invoke('onHide', this, arguments);
    this.__accessVisible = false;
    lock(this);
  };
  result.onUnload = function() {
    if (!protectedPage(this)) return invoke('onUnload', this, arguments);
    this.__accessDestroyed = true;
    this.__accessVisible = false;
    this.__accessActive = false;
    if (this.__accessLoaded) return invoke('onUnload', this, arguments);
  };
  for (const name of ['onShareAppMessage', 'onShareTimeline', 'onPullDownRefresh', 'onReachBottom', 'onPageScroll']) {
    if (typeof definition[name] !== 'function') continue;
    result[name] = function() {
      if (protectedPage(this) && !this.__accessActive) return name.startsWith('onShare') ? {} : undefined;
      return invoke(name, this, arguments);
    };
  }
  return result;
}

function install() {
  if (installed) return;
  installed = true;
  const NativePage = Page;
  Page = definition => NativePage(wrapProfileAccess(definition));
  const NativeApp = App;
  App = definition => NativeApp({ ...definition, onHide() {
    // Stop visible tracking before App.onShow can resume a cached credential.
    const pages = typeof getCurrentPages === 'function' ? getCurrentPages() : [];
    for (const page of pages) if (typeof page.onProfileAccessAppHide === 'function') page.onProfileAccessAppHide();
    return typeof definition.onHide === 'function' ? definition.onHide.apply(this, arguments) : undefined;
  } });
}
module.exports = { install, wrapProfileAccess };
