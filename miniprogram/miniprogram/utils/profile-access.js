'use strict';
const { hasCompleteProfile } = require('../components/nickname-guard/profile-validation');
let installed = false;
const ACCESS_FREE_ROUTES = new Set([
  'pages/login/login',
  'pages/privacy/privacy',
  'pages/passwordrecovery/passwordrecovery',
]);
// 只有用户明确指定的三个入口允许匿名浏览；其它业务页必须先完成资料门禁。
// 这些页面仍被包装（wxml 的 guard 结构不变），匿名浏览继续由 tracking 记录，
// 登录后的真实核验会把页面与归因切换到账号身份。
const PUBLIC_BROWSE_ROUTES = new Set([
  'pages/index/index',
  'pages/template/template',
  'pages/distribution/distribution',
]);

// 分享查看页保留登录后返回原页的场景参数，资料门禁与其它受保护页一致。
const SHARE_PROFILE_ROUTES = new Set([
  'pages/projectnoteview/projectnoteview',
]);

// Run outside the analytics wrapper: content access must not depend on analytics
// availability or consent, and an unverified page must not create a visit.
function wrapProfileAccess(definition) {
  const signature = options => JSON.stringify(Object.keys(options || {}).sort().map(key => [key, options[key]]));
  const invoke = (name, page, args = []) => typeof definition[name] === 'function'
    ? definition[name].apply(page, args) : undefined;
  const protectedPage = page => !ACCESS_FREE_ROUTES.has(String(page.route || '').replace(/^\//, ''));
  const publicBrowse = page => PUBLIC_BROWSE_ROUTES.has(String(page.route || '').replace(/^\//, ''));
  const shareProfile = page => SHARE_PROFILE_ROUTES.has(String(page.route || '').replace(/^\//, ''));
  const lock = page => {
    page.__accessRevision = (page.__accessRevision || 0) + 1;
    if (page.__accessActive) invoke('onHide', page);
    page.__accessActive = false;
    page.setData({ __profileAccessReady: false, __profileAccessMounted: false });
    if (typeof wx.hideShareMenu === 'function') wx.hideShareMenu({ menus: ['shareAppMessage', 'shareTimeline'] });
  };
  const redirectToLogin = page => {
    if (page.__accessRedirecting || page.__accessDestroyed || !page.__accessVisible) return;
    page.__accessRedirecting = true;
    // 分享页兜底跳登录时保留返回场景，但不放宽昵称和头像要求。
    wx.navigateTo({
      url: '/pages/login/login?from=' + (shareProfile(page) ? 'share-view' : 'profile-gate'),
      fail() {
        page.__accessRedirecting = false;
        wx.showToast({ title: '请先登录并补充头像、昵称', icon: 'none' });
      },
    });
  };
  const verifyWithoutComponent = page => {
    const token = String(wx.getStorageSync('token') || '');
    if (!token) { redirectToLogin(page); return; }
    const revision = page.__accessRevision;
    const path = '/api/v1/miniprogram/profile';
    const auth = require('./parameter');
    const device = String(wx.getStorageSync('device_fingerprint') || '');
    const header = auth.paramsToHeaders(auth.generateRequestParams(token, {}, path, device));
    wx.request({ url: 'https://api.jiadilingguang.com' + path, method: 'GET', header, timeout: 15000, __profileAccessResponseHandled: true,
      success(response) {
        if (revision !== page.__accessRevision || token !== wx.getStorageSync('token') || !page.__accessVisible || page.__accessDestroyed) return;
        const data = response.data || {};
        if (response.statusCode === 401 || data.code === 401) {
          wx.removeStorageSync('token'); wx.removeStorageSync('userInfo');
          redirectToLogin(page); return;
        }
        const profile = data.data || {};
        const id = Number(profile.id || 0);
        if (response.statusCode !== 200 || data.code !== 0) {
          wx.showToast({ title: data.msg || '资料读取失败，请重试', icon: 'none' }); return;
        }
        if (!Number.isSafeInteger(id) || id <= 0) {
          wx.removeStorageSync('token'); wx.removeStorageSync('userInfo');
          redirectToLogin(page); return;
        }
        const cachedProfile = wx.getStorageSync('userInfo') || {};
        const sameAccount = Number(cachedProfile.id || 0) === id;
        wx.setStorageSync('userInfo', { ...(sameAccount ? cachedProfile : {}), ...profile, id, token });
        if (!hasCompleteProfile(profile)) { redirectToLogin(page); return; }
        page.onProfileAccessGranted({ detail: { token, profile } });
      },
      fail() {
        if (revision === page.__accessRevision && page.__accessVisible && !page.__accessDestroyed) {
          wx.showToast({ title: '资料读取失败，请重试', icon: 'none' });
        }
      },
    });
  };
  const verify = (page, silent) => {
    if (!page.__accessMounted || !page.__accessVisible || page.__accessDestroyed) return;
    const guard = typeof page.selectComponent === 'function' && page.selectComponent('#profileAccessGuard');
    if (!guard || typeof guard.check !== 'function') {
      verifyWithoutComponent(page); return;
    }
    Promise.resolve(guard.check(silent ? { silent: true } : undefined))
      .catch(error => console.error('个人资料核验失败', page.route, error));
  };
  // 公开浏览页乐观放行：复用 granted 解锁路径但不做服务端资料核验，页面
  // 内容与分享能力对未登录用户直接开放（微信审核：先体验，登录由用户
  // 自行触发；匿名浏览经 tracking 匿名池记录，登录后归并到账号）。
  const optimisticGrant = page => {
    if (page.__accessActive || page.__accessDestroyed || !page.__accessVisible) return;
    const token = String(wx.getStorageSync('token') || '');
    // 匿名浏览用哨兵 id=-1：非 falsy 且不等于任何真实账号，后续登录核验
    // （id>=1）到达时触发 reload 分支，把页面数据与 tracking 观察切到登录
    // 账号（转发归因、实名 entry 都依赖这次重载）。已登录用户保持 id=0
    // （falsy）：同账号后台核验通过时不 reload，内容保持挂载不重复采集。
    page.onProfileAccessGranted({ detail: { token, profile: { id: token ? 0 : -1 }, optimistic: true } });
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
    this.__accessRedirecting = false;
    const token = String(wx.getStorageSync('token') || '');
    // 同一账号被覆盖后返回：内容保持挂载、后台静默核验，核验通过后由放行回调
    // 恢复页面生命周期。整页上锁只保留给首次进入、换号和外部入口变化。
    // 公开浏览页恢复时不重核验（乐观放行），直接走下方公开页分支。
    if (this.__accessSuspended && this.__accessActiveToken === token && !publicBrowse(this)) {
      this.__accessSuspended = false;
      verify(this, true);
      return;
    }
    // 公开浏览页乐观放行：无论登录与否先解锁内容，不锁页、不跳登录、
    // 不在此触发资料核验弹窗（资料登记由其它受保护业务页承担）。
    if (publicBrowse(this)) {
      this.__accessSuspended = false;
      if (!this.__accessActive) optimisticGrant(this);
      return;
    }
    lock(this);
    verify(this);
  };
  result.onReady = function() {
    if (!protectedPage(this)) return invoke('onReady', this, arguments);
    this.__accessMounted = true;
    // 公开浏览页不核验：核验会对未登录用户弹登录面板/对资料不全用户弹
    // 强制补全，把"先浏览"又变成"先授权"。身份页行为不变。
    if (!publicBrowse(this)) verify(this);
  };
  result.onProfileAccessBlocked = function() {
    if (!protectedPage(this)) return;
    // 公开浏览页即使收到 gated 也不锁内容（乐观放行语义）。
    if (!publicBrowse(this)) lock(this);
  };
  result.onProfileAccessAppHide = function() {
    if (!protectedPage(this)) return;
    this.__accessVisible = false;
    if (!publicBrowse(this)) lock(this);
  };
  result.onProfileAccessGranted = function(event) {
    const token = wx.getStorageSync('token');
    // optimistic=公开浏览页乐观放行：detail.token 即构造时的当前 token
    // （含未登录的空串），不要求服务端核验结果，其余守卫不变。
    // 边界：optimistic 只服务于三类公开浏览页；其余页面收到该事件一律忽略。
    if (event?.detail?.optimistic === true && !publicBrowse(this)) return;
    const optimistic = event?.detail?.optimistic === true && publicBrowse(this) && event?.detail?.token === String(token || '');
    // upgrade=真实核验升级乐观放行：页面已被乐观放行（__accessOptimistic）
    // 时，服务端 authenticated 事件仍需进入以触发换号 reload（-1→真实id），
    // 让数据与 tracking 观察切到登录账号，否则登录后的转发丢失归因。
    const upgrade = !optimistic && this.__accessOptimistic === true;
    if (!protectedPage(this) || this.__accessDestroyed || !this.__accessVisible
      || (!optimistic && !upgrade && this.__accessActive)
      || (!optimistic && (!token || event?.detail?.token !== token))) return;
    this.__accessOptimistic = optimistic;
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
    this.__accessActiveToken = token;
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
      if (token !== wx.getStorageSync('token')) {
        // 公开浏览页 token 变化（如前台登录）不锁内容：下次 onShow 会以
        // 新 token 乐观恢复；身份页维持原安全行为（上锁并重新核验）。
        if (!publicBrowse(this)) { lock(this); verify(this); }
        return;
      }
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
    if (publicBrowse(this)) {
      // 公开浏览页收到新分享入口：乐观重载生命周期（granted 的 reload
      // 分支会重跑 onLoad），不锁页、不触发核验弹窗。
      this.__accessActive = false;
      optimisticGrant(this);
      return;
    }
    lock(this);
    verify(this);
  };
  if (typeof definition.acceptExternalEntry === 'function') {
    result.acceptExternalEntry = function(options) { this.queueProfileEntry(options); };
  }
  result.onHide = function() {
    if (!protectedPage(this)) return invoke('onHide', this, arguments);
    this.__accessVisible = false;
    // 被其他页面覆盖时不再整页卸载：已放行内容留在层栈里，返回时走静默核验，
    // 避免每次子页面往返都出现白屏闪烁。安全上锁仍由门禁失败与 App 隐藏负责。
    if (this.__accessActive) {
      this.__accessActive = false;
      this.__accessSuspended = true;
      invoke('onHide', this, arguments);
    }
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
// login-guard 等组件用它判断宿主页是否公开浏览页（决定"暂不登录"的
// 行为：公开页仅关浮层继续浏览，身份页返回上一页）。纯函数，无副作用。
const isPublicBrowseRoute = route => PUBLIC_BROWSE_ROUTES.has(String(route || '').replace(/^\//, ''));
module.exports = { install, wrapProfileAccess, isPublicBrowseRoute };
