'use strict';

const ORIGIN = 'https://api.jiadilingguang.com';
const WORKBENCH = '/api/v1/miniprogram/workbench';
const IMPORTS = WORKBENCH + '/imports';
const MY_SHARES = '/api/v1/miniprogram/tracking/my-shares';
let mutationSequence = 0;

function authHeaders(path, body, json = true) {
  const token = String(wx.getStorageSync('token') || '');
  if (!token) throw Object.assign(new Error('请先登录'), { statusCode: 401 });
  const auth = require('./parameter');
  const device = String(wx.getStorageSync('device_fingerprint') || '');
  const headers = auth.paramsToHeaders(auth.generateRequestParams(token, body || {}, path, device));
  if (json) headers['Content-Type'] = 'application/json';
  return headers;
}

function queryString(values) {
  const fields = [];
  Object.keys(values || {}).forEach(key => {
    const value = values[key];
    if (value !== undefined && value !== null && String(value) !== '') fields.push(encodeURIComponent(key) + '=' + encodeURIComponent(String(value)));
  });
  return fields.length ? '?' + fields.join('&') : '';
}

function responseError(response, fallback) {
  const data = response && response.data || {};
  return Object.assign(new Error(data.msg || fallback), { statusCode: response && response.statusCode || 0, code: data.code });
}

function request(path, method = 'GET', body = {}, query = {}) {
  return new Promise((resolve, reject) => wx.request({
    url: ORIGIN + path + queryString(query), method, data: body, header: authHeaders(path, body), timeout: 10000,
    success(response) {
      const data = response.data || {};
      if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
      else reject(responseError(response, '请求失败，请稍后重试'));
    },
    fail() { reject(Object.assign(new Error('网络不可用，请检查连接'), { networkError: true })); },
  }));
}

function workbenchContext() { return request(WORKBENCH + '/context'); }
function listContents(query) { return request(WORKBENCH + '/contents', 'GET', {}, query); }
function getContent(id) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id)); }
function listWorkbenchMedia() { return request(WORKBENCH + '/media'); }
function saveContent(draft, id) { return request(WORKBENCH + '/contents' + (id ? '/' + encodeURIComponent(id) : ''), id ? 'PATCH' : 'POST', draft); }
function previewContent(id, revision) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/preview', 'POST', { expected_revision: revision }); }
function publishContent(id, revision, previewToken, key) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/publish', 'POST', { expected_revision: revision, preview_token: previewToken, idempotency_key: key }); }
function revokeContent(id, revision, reason, key) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/revoke', 'POST', { expected_revision: revision, reason, idempotency_key: key }); }
function listMyShares(query) { return request(MY_SHARES, 'GET', {}, query); }
function listMyShareVisits(id, query) { return request(MY_SHARES + '/' + encodeURIComponent(id) + '/visits', 'GET', {}, query); }
function importCapabilities() { return request(IMPORTS + '/capabilities'); }
function listImports() { return request(IMPORTS); }
function submitImport(input) { return request(IMPORTS, 'POST', input); }
function getImport(id) { return request(IMPORTS + '/' + encodeURIComponent(id)); }
function retryImport(id) { return request(IMPORTS + '/' + encodeURIComponent(id) + '/retry', 'POST', {}); }

function downloadWorkbenchMedia(id) {
  const path = WORKBENCH + '/media/' + encodeURIComponent(id);
  return new Promise((resolve, reject) => wx.downloadFile({
    url: ORIGIN + path, header: authHeaders(path, {}), timeout: 15000,
    success(response) {
      if (response.statusCode >= 200 && response.statusCode < 300 && response.tempFilePath) resolve(response.tempFilePath);
      else reject(responseError(response, '私有媒体预览加载失败'));
    },
    fail() { reject(new Error('私有媒体预览加载失败')); },
  }));
}

function uploadWorkbenchMedia(filePath, kind, owner, rights) {
  const path = WORKBENCH + '/media';
  return new Promise((resolve, reject) => wx.uploadFile({
    url: ORIGIN + path, filePath, name: 'file', header: authHeaders(path, {}, false),
    formData: { owner_id: owner, rights, kind }, timeout: 60000,
    success(response) {
      let data = {};
      try { data = JSON.parse(response.data || '{}'); } catch (_) { reject(new Error('媒体上传响应无法读取')); return; }
      if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
      else reject(Object.assign(new Error(data.msg || '媒体上传失败'), { statusCode: response.statusCode }));
    },
    fail() { reject(new Error('媒体上传失败')); },
  }));
}

function mutationKey(prefix) {
  mutationSequence += 1;
  return prefix + '-' + Date.now() + '-' + mutationSequence;
}

module.exports = {
  ORIGIN, WORKBENCH, MY_SHARES, authHeaders, request, workbenchContext, listContents, getContent, listWorkbenchMedia,
  saveContent, previewContent, publishContent, revokeContent, listMyShares, listMyShareVisits,
  importCapabilities, listImports, submitImport, getImport, retryImport,
  downloadWorkbenchMedia, uploadWorkbenchMedia, mutationKey,
};
