'use strict';

const ORIGIN = 'https://api.jiadilingguang.com';
const WORKBENCH = '/api/v1/miniprogram/workbench';
const PUBLIC = '/api/v1/miniprogram/tracking';

function queryString(query = {}) {
  const parts = [];
  Object.keys(query).forEach(key => {
    const value = query[key];
    if (value !== undefined && value !== null && value !== '') parts.push(encodeURIComponent(key) + '=' + encodeURIComponent(String(value)));
  });
  return parts.length ? '?' + parts.join('&') : '';
}

function request(path, method = 'GET', body = {}, authenticated = true) {
  let header = { 'Content-Type': 'application/json' };
  if (authenticated) {
    const token = String(wx.getStorageSync('token') || '');
    if (token) {
      const signer = require('./parameter');
      const device = String(wx.getStorageSync('device_fingerprint') || '');
      header = Object.assign(header, signer.paramsToHeaders(signer.generateRequestParams(token, body, path, device)));
    }
  }
  return new Promise((resolve, reject) => wx.request({
    url: ORIGIN + path, method, data: body, header, timeout: 10000,
    success(response) {
      const result = response.data || {};
      if (response.statusCode >= 200 && response.statusCode < 300 && result.code === 0) resolve(result.data || {});
      else reject(Object.assign(new Error(result.msg || '项目合集暂不可用'), { statusCode: response.statusCode, code: result.code }));
    },
    fail() { reject(Object.assign(new Error('网络不可用，请检查连接'), { networkError: true })); },
  }));
}

function collectionPath(id = '') {
  return WORKBENCH + '/project-collections' + (id ? '/' + encodeURIComponent(id) : '');
}

function publicCollectionPath(id, version) {
  return PUBLIC + '/project-collections/' + encodeURIComponent(id) + '/versions/' + Number(version);
}

function workbenchContext() { return request(WORKBENCH + '/context'); }
function listCandidates(query) { return request(collectionPath() + '/candidates' + queryString(query)); }
function listCollections(query) { return request(collectionPath() + queryString(query)); }
function getCollection(id) { return request(collectionPath(id)); }
function saveCollection(input, id) { return request(collectionPath(id), id ? 'PATCH' : 'POST', input); }
function deleteCollectionDraft(id, revision) { return request(collectionPath(id), 'DELETE', { expected_revision: Number(revision) }); }
function deleteCollection(id, revision) { return request(collectionPath(id) + '/delete', 'POST', { expected_revision: Number(revision) }); }
function previewCollection(id, revision) { return request(collectionPath(id) + '/preview', 'POST', { expected_revision: Number(revision) }); }
function publishCollection(id, revision, previewToken, key) {
  return request(collectionPath(id) + '/publish', 'POST', { expected_revision: Number(revision), preview_token: previewToken, idempotency_key: key });
}
function revokeCollection(id, revision, reason, key) {
  return request(collectionPath(id) + '/revoke', 'POST', { expected_revision: Number(revision), reason, idempotency_key: key });
}
function readPublishedCollection(id, version) { return request(publicCollectionPath(id, version)); }
function openPublishedItem(id, version, position) { return request(publicCollectionPath(id, version) + '/items/' + Number(position)); }
function mutationKey(prefix) { return prefix + '-' + Date.now() + '-' + Math.random().toString(36).slice(2, 10); }
function mediaURL(value) {
  const url = String(value || '');
  if (url.startsWith(PUBLIC + '/media/') || /^\/api\/v1\/miniprogram\/tracking\/project-collections\/[A-Za-z0-9_-]{8,128}\/versions\/[1-9]\d*\/items\/\d+\/media\/[A-Za-z0-9_-]+$/.test(url)) return ORIGIN + url;
  return /^https:\/\//.test(url) ? url : '';
}

module.exports = {
  ORIGIN, WORKBENCH, PUBLIC, request, workbenchContext,
  listCandidates, listCollections, getCollection, saveCollection, deleteCollectionDraft, deleteCollection,
  previewCollection, publishCollection, revokeCollection,
  readPublishedCollection, openPublishedItem, mutationKey, mediaURL,
};
