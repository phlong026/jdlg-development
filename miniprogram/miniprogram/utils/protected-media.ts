// @ts-nocheck
'use strict';

const { hasCompleteProfile } = require('../components/nickname-guard/profile-validation');
const { handleProfileAccessResponse } = require('./profile-request-guard');
const ORIGIN = 'https://api.jiadilingguang.com';
const PROTECTED_PREFIX = ORIGIN + '/api/v1/miniprogram/';
const MAX_CACHE_ENTRIES = 24;
const cache = new Map();
let activeIdentity = '';

function accessError(message, statusCode) {
  return Object.assign(new Error(message), { statusCode });
}

function identity() {
  const token = String(wx.getStorageSync('token') || '');
  if (!token) throw accessError('请先登录', 401);
  const profile = wx.getStorageSync('userInfo') || {};
  if (!hasCompleteProfile(profile)) throw accessError('请先补充头像和昵称', 428);
  const profileToken = String(profile.token || '');
  if (profileToken && profileToken !== token) throw accessError('登录资料已过期，请重新登录', 401);
  return token + '\n' + String(profile.id || '');
}

function protectedPath(url) {
  if (!url.startsWith(PROTECTED_PREFIX)) return '';
  return url.slice(ORIGIN.length);
}

function currentIdentity(expected) {
  try { return identity() === expected; } catch (_) { return false; }
}

function trimCache() {
  while (cache.size > MAX_CACHE_ENTRIES) cache.delete(cache.keys().next().value);
}

function download(url, path, expectedIdentity) {
  const token = expectedIdentity.split('\n')[0];
  const signer = require('./parameter');
  const device = String(wx.getStorageSync('device_fingerprint') || '');
  const header = signer.paramsToHeaders(signer.generateRequestParams(token, {}, path, device));
  return new Promise((resolve, reject) => wx.downloadFile({
    url,
    header,
    timeout: 60000,
    success(response) {
      if (!currentIdentity(expectedIdentity)) {
        reject(Object.assign(new Error('登录账号已变更'), { code: 'MEDIA_IDENTITY_CHANGED' }));
        return;
      }
      if (response.statusCode >= 200 && response.statusCode < 300 && response.tempFilePath) {
        resolve(response.tempFilePath);
        return;
      }
      handleProfileAccessResponse(response.statusCode, {}, token);
      reject(Object.assign(new Error(response.statusCode === 401 || response.statusCode === 428
        ? '请先登录并补充头像和昵称'
        : '素材加载失败，请重试'), { statusCode: response.statusCode || 0, code: 'MEDIA_DOWNLOAD_FAILED' }));
    },
    fail() {
      reject(Object.assign(new Error('素材加载失败，请重试'), { networkError: true, code: 'MEDIA_DOWNLOAD_FAILED' }));
    },
  }));
}

function resolveProtectedMediaURL(value) {
  const url = String(value || '');
  if (!/^https:\/\//.test(url)) return Promise.reject(new Error('素材地址无效'));
  let expectedIdentity = '';
  try { expectedIdentity = identity(); } catch (error) { return Promise.reject(error); }
  if (activeIdentity !== expectedIdentity) {
    cache.clear();
    activeIdentity = expectedIdentity;
  }
  const path = protectedPath(url);
  if (!path) return Promise.resolve(url);
  const key = expectedIdentity + '\n' + url;
  if (cache.has(key)) return cache.get(key);
  const pending = download(url, path, expectedIdentity).catch(error => {
    if (cache.get(key) === pending) cache.delete(key);
    throw error;
  });
  cache.set(key, pending);
  trimCache();
  return pending;
}

module.exports = { resolveProtectedMediaURL };
export {};
