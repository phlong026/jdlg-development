// @ts-nocheck
'use strict';
const API_ORIGIN = 'https://api.jiadilingguang.com';
const API_PREFIX = '/api/v1/miniprogram/';
const OPEN_PATHS = new Set([
  '/api/v1/miniprogram/login',
  '/api/v1/miniprogram/login/password',
  '/api/v1/miniprogram/login/phone',
  '/api/v1/miniprogram/sms/send',
  '/api/v1/miniprogram/profile/password/recover',
  '/api/v1/miniprogram/tracking/config',
  '/api/v1/miniprogram/tracking/share-entry',
]);
const GATE_ROUTES = new Set(['pages/login/login', 'pages/privacy/privacy', 'pages/passwordrecovery/passwordrecovery']);
const PUBLIC_BROWSE_ROUTES = new Set([
  'pages/index/index',
  'pages/template/template',
  'pages/distribution/distribution',
  'pages/templatesquaredetails/templatesquaredetails',
  'pages/contentreader/contentreader',
  'pages/collectionreader/collectionreader',
  'pages/designerhome/designerhome',
  'pages/inspirationdetail/inspirationdetail',
  'pages/aigenerate/aigenerate',
  'pages/aivideo/aivideo',
  'pages/aitools/aitools',
  'pages/aitooldetail/aitooldetail',
  'pages/aitoolworkbench/aitoolworkbench',
  'pages/aicost/aicost',
  'pages/newminnantool/newminnantool',
  'pages/allrounddesign/allrounddesign',
]);
const ACCOUNT_ONLY_ROUTES = new Set([
  'pages/generatedetails/generatedetails',
  'pages/generatehistory/generatehistory',
  'pages/generateprogress/generateprogress',
  'pages/aicostresult/aicostresult',
]);
const AUTH_HEADERS = ['token', 'token-signature', 'sin', 'md5-signature', 'pass', 'tm'];

let installed = false;

function requestPath(url) {
  const value = String(url || '');
  return value.startsWith(API_ORIGIN + API_PREFIX) ? value.slice(API_ORIGIN.length) : '';
}

function isProtected(path) {
  return !!path && !OPEN_PATHS.has(path.split('?')[0]);
}

function hasHeader(header, name) {
  const lower = name.toLowerCase();
  return Object.keys(header).some(key => key.toLowerCase() === lower);
}

function headerValue(header, name) {
  const lower = name.toLowerCase();
  const key = Object.keys(header).find(value => value.toLowerCase() === lower);
  return key ? String(header[key] || '') : '';
}

function hasCompleteSignature(header) {
  return AUTH_HEADERS.every(name => hasHeader(header, name));
}

function withoutAuthHeaders(header) {
  return Object.keys(header).reduce((clean, key) => {
    if (!AUTH_HEADERS.includes(key.toLowerCase())) clean[key] = header[key];
    return clean;
  }, {});
}

function queryString(data) {
  if (typeof data === 'string') return data.replace(/^[?&]/, '');
  if (!data || typeof data !== 'object' || Array.isArray(data)) return '';
  const parts = [];
  Object.keys(data).forEach(key => {
    const value = data[key];
    if (value !== undefined) parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(value == null ? '' : String(value))}`);
  });
  return parts.join('&');
}

function normalizeProtectedRequest(options) {
  const method = String(options.method || 'GET').toUpperCase();
  if (method !== 'GET' && method !== 'HEAD') return { ...options };
  const query = queryString(options.data);
  if (!query) return { ...options, data: {} };
  return {
    ...options,
    url: String(options.url) + (String(options.url).includes('?') ? '&' : '?') + query,
    data: {},
  };
}

function activePage() {
  const pages = typeof getCurrentPages === 'function' ? getCurrentPages() : [];
  return pages[pages.length - 1];
}

function isPublicBrowsePage(page) {
  return PUBLIC_BROWSE_ROUTES.has(String(page?.route || '').replace(/^\//, ''));
}

function clearSessionStorage() {
  wx.removeStorageSync('token');
  wx.removeStorageSync('userInfo');
}

function checkInlineProfileGate(page, requireFullProfile = false) {
  const guard = typeof page.selectComponent === 'function' && page.selectComponent('#profileAccessGuard');
  if (!guard || typeof guard.check !== 'function') return false;
  const token = String(wx.getStorageSync('token') || '');
  if (page.__profileGateChecking && page.__profileGateCheckToken === token) return true;
  const revision = Number(page.__profileGateCheckRevision || 0) + 1;
  page.__profileGateChecking = true;
  page.__profileGateCheckToken = token;
  page.__profileGateCheckRevision = revision;
  const done = () => {
    if (page.__profileGateCheckRevision !== revision) return;
    page.__profileGateChecking = false;
    page.__profileGateCheckToken = undefined;
  };
  try {
    const route = String(page.route || '').replace(/^\//, '');
    Promise.resolve(guard.check(!requireFullProfile && ACCOUNT_ONLY_ROUTES.has(route) ? { accountOnly: true } : undefined)).then(done, done);
  } catch (_) {
    done();
    return false;
  }
  return true;
}

function openProfileGate(clearSession, requireFullProfile = false) {
  if (clearSession) {
    clearSessionStorage();
  }
  const page = activePage();
  if (!page || GATE_ROUTES.has(page.route)) return;
  if (typeof page.onProfileAccessBlocked === 'function') page.onProfileAccessBlocked();
  // 分享页已经挂载同一套登录与资料补全卡片，优先原地恢复，避免再叠加独立登录页。
  if (checkInlineProfileGate(page, requireFullProfile)) return;
  if (page.__accessRedirecting) return;
  page.__accessRedirecting = true;
  wx.navigateTo({
    url: '/pages/login/login?from=profile-gate',
    fail: () => {
      page.__accessRedirecting = false;
      wx.showToast({ title: '请先登录并补充头像、昵称', icon: 'none' });
      const guard = typeof page.selectComponent === 'function' && page.selectComponent('#profileAccessGuard');
      if (guard && typeof guard.check === 'function') Promise.resolve(guard.check()).catch(() => {});
    },
  });
}

function handleProfileAccessResponse(statusCode, body, requestToken) {
  if (String(wx.getStorageSync('token') || '') !== String(requestToken || '')) return false;
  const data = body || {};
  if (statusCode === 401 || data.code === 401) {
    if (isPublicBrowsePage(activePage())) {
      // 公开浏览页的后台请求失败不应变成强制登录。仅当请求确实带了
      // 已过期会话时清理缓存；纯匿名 tracking entry 失败不制造会话变化。
      if (String(requestToken || '')) clearSessionStorage();
      return true;
    }
    openProfileGate(true);
    return true;
  }
  if (statusCode === 428 || data.code === 428) {
    if (isPublicBrowsePage(activePage())) return true;
    openProfileGate(false, true);
    return true;
  }
  return false;
}

function install() {
  if (installed || !wx || typeof wx.request !== 'function') return;
  installed = true;
  const nativeRequest = wx.request;
  wx.request = function guardedRequest(options) {
    let requestOptions = options;
    const path = requestPath(options && options.url);
    if (!isProtected(path)) return nativeRequest.call(wx, options);

    const originalHeader = { ...((options && options.header) || {}) };
    const alreadySigned = hasCompleteSignature(originalHeader);
    requestOptions = alreadySigned ? { ...(options || {}) } : normalizeProtectedRequest(options || {});
    const signedPath = requestPath(requestOptions.url);
    const callerHandlesProfile = requestOptions.__profileAccessResponseHandled === true
      && String(requestOptions.method || 'GET').toUpperCase() === 'GET'
      && signedPath.split('?')[0] === '/api/v1/miniprogram/profile';
    const softFullProfileAction = requestOptions.__profileAccessResponseMode === 'soft-full-profile';
    delete requestOptions.__profileAccessResponseHandled;
    delete requestOptions.__profileAccessResponseMode;
    const header = alreadySigned ? { ...(requestOptions.header || {}) } : withoutAuthHeaders(requestOptions.header || {});
    const token = String(wx.getStorageSync('token') || '');
    if (token && !alreadySigned) {
      const auth = require('./parameter');
      const multipart = /^multipart\/form-data\b/i.test(headerValue(header, 'content-type'));
      const body = multipart || requestOptions.data == null ? {} : requestOptions.data;
      const device = String(wx.getStorageSync('device_fingerprint') || '');
      Object.assign(header, auth.paramsToHeaders(auth.generateRequestParams(token, body, signedPath, device)));
    }
    const dispatchToken = headerValue(header, 'token');
    // 采集/心跳类请求自带临时的 X-Tracking-Token 凭据，其过期 401 是常规业务
    // 状态（SDK 会自行重新建立 entry），绝不能清掉主账号会话或弹登录门禁。
    const backgroundTracking = hasHeader(header, 'x-tracking-token');
    const success = requestOptions.success;
    const fail = requestOptions.fail;
    const complete = requestOptions.complete;
    const sameSession = () => dispatchToken === String(wx.getStorageSync('token') || '');
    const staleFailure = () => ({ errMsg: 'request:fail session changed', sessionChanged: true });
    return nativeRequest.call(wx, {
      ...requestOptions,
      header,
      timeout: typeof requestOptions.timeout === 'number' ? requestOptions.timeout : 15000,
      success(response) {
        if (!sameSession()) {
          if (typeof fail === 'function') fail(staleFailure());
          return;
        }
        const profileRequired = response?.statusCode === 428 || response?.data?.code === 428;
        if (!callerHandlesProfile && !backgroundTracking && !(softFullProfileAction && profileRequired)) {
          handleProfileAccessResponse(response && response.statusCode, response && response.data, dispatchToken);
        }
        if (typeof success === 'function') success(response);
      },
      fail(error) {
        if (typeof fail === 'function') fail(sameSession() ? error : staleFailure());
      },
      complete(result) {
        if (typeof complete === 'function') complete(sameSession() ? result : staleFailure());
      },
    });
  };
}

module.exports = { install, handleProfileAccessResponse };
export {};
