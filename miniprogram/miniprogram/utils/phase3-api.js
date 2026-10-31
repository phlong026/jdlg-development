'use strict';

const ORIGIN = 'https://api.jiadilingguang.com';
const WORKBENCH = '/api/v1/miniprogram/workbench';
const IMPORTS = WORKBENCH + '/imports';
const MY_SHARES = '/api/v1/miniprogram/tracking/my-shares';
const PROJECT_NOTES = '/api/v1/miniprogram/project-notes';
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
function deleteContentDraft(id, revision) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id), 'DELETE', { expected_revision: Number(revision) }); }
function previewContent(id, revision) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/preview', 'POST', { expected_revision: revision }); }
function publishContent(id, revision, previewToken, key) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/publish', 'POST', { expected_revision: revision, preview_token: previewToken, idempotency_key: key }); }
function revokeContent(id, revision, reason, key) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/revoke', 'POST', { expected_revision: revision, reason, idempotency_key: key }); }
function mySharesContext() { return request(MY_SHARES + '/context'); }
function listMyShares(query) { return request(MY_SHARES, 'GET', {}, query); }
function listMyShareVisits(id, query) { return request(MY_SHARES + '/' + encodeURIComponent(id) + '/visits', 'GET', {}, query); }
function importCapabilities() { return request(IMPORTS + '/capabilities'); }
function listImports() { return request(IMPORTS); }
function submitImport(input) { return request(IMPORTS, 'POST', input); }
function getImport(id) { return request(IMPORTS + '/' + encodeURIComponent(id)); }
function retryImport(id) { return request(IMPORTS + '/' + encodeURIComponent(id) + '/retry', 'POST', {}); }

function listNativeReferenceCandidates(query) { return request(WORKBENCH + '/native-reference-candidates', 'GET', {}, query); }
function listNativeReferences(query) { return request(WORKBENCH + '/native-references', 'GET', {}, query); }
function saveNativeReference(id, version, key) { return request(WORKBENCH + '/native-references', 'POST', { content_id: id, content_version: Number(version), idempotency_key: key }); }
function deleteNativeReference(id) { return request(WORKBENCH + '/native-references/' + encodeURIComponent(id), 'DELETE'); }
function getNativeReferenceTarget(id) { return request(WORKBENCH + '/native-references/' + encodeURIComponent(id) + '/target'); }

function projectNotePath(path) { return PROJECT_NOTES + path; }
function listProjectNoteProjects(query) { return request(projectNotePath('/projects'), 'GET', {}, query); }
function createProjectNoteProject(input) { return request(projectNotePath('/projects'), 'POST', input); }
function updateProjectNoteProject(id, patch) { return request(projectNotePath('/projects/' + encodeURIComponent(id)), 'PATCH', patch); }
function getProjectNoteHistory(id, type, query) { return request(projectNotePath('/projects/' + encodeURIComponent(id) + '/history/' + encodeURIComponent(type)), 'GET', {}, query); }
function getSharedProjectNoteHistory(token, type, query) { return request(projectNotePath('/shared/' + encodeURIComponent(token) + '/history/' + encodeURIComponent(type)), 'GET', {}, query); }
function getProjectNoteFeedback(id, feedbackID) { return request(projectNotePath('/projects/' + encodeURIComponent(id) + '/feedback/' + encodeURIComponent(feedbackID))); }
function getProjectNote(id, noteID) { return request(projectNotePath('/projects/' + encodeURIComponent(id) + '/notes/' + encodeURIComponent(noteID))); }
function getProjectNoteProject(id) { return request(projectNotePath('/projects/' + encodeURIComponent(id))); }
function getProjectNoteVersion(projectID, versionID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/versions/' + encodeURIComponent(versionID))); }
function inviteProjectNoteMember(projectID, accountID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/members/' + encodeURIComponent(accountID)), 'POST', {}); }
function revokeProjectNoteMember(projectID, accountID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/members/' + encodeURIComponent(accountID)), 'DELETE', {}); }
function setProjectNoteConfirmer(projectID, accountID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/confirmer'), 'PUT', { account_id: Number(accountID) }); }
function createProjectNote(projectID, draft) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/notes'), 'POST', draft); }
function copyProjectNote(noteID, revision) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID) + '/copy'), 'POST', { expected_revision: Number(revision) }); }
function deleteProjectNote(noteID, revision) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID)), 'DELETE', { expected_revision: Number(revision) }); }
function saveProjectNote(noteID, draft) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID)), 'PATCH', draft); }
function freezeProjectNoteVersion(noteID, revision) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID) + '/versions'), 'POST', { expected_revision: Number(revision) }); }
function createProjectNoteShare(versionID, expiresAt, key) { return request(projectNotePath('/versions/' + encodeURIComponent(versionID) + '/shares'), 'POST', { expires_at: expiresAt, idempotency_key: key }); }
function revokeProjectNoteShare(shareID) { return request(projectNotePath('/shares/' + encodeURIComponent(shareID) + '/revoke'), 'POST', {}); }
function resolveProjectNoteFeedback(feedbackID, reply, key) { return request(projectNotePath('/feedback/' + encodeURIComponent(feedbackID) + '/resolve'), 'POST', { reply, idempotency_key: key }); }
function getSharedProjectNote(token) { return request(projectNotePath('/shared/' + encodeURIComponent(token))); }
function openSharedProjectNote(token, eventID) { return request(projectNotePath('/shared/' + encodeURIComponent(token) + '/open'), 'POST', { event_id: eventID, acknowledged: true }); }
function addSharedProjectNoteFeedback(token, text, key, satisfaction) { return request(projectNotePath('/shared/' + encodeURIComponent(token) + '/feedback'), 'POST', { text, idempotency_key: key, satisfaction: satisfaction || '' }); }
function confirmSharedProjectNote(token, key) { return request(projectNotePath('/shared/' + encodeURIComponent(token) + '/confirm'), 'POST', { idempotency_key: key }); }

function downloadProjectNoteFile(path, timeout, message) {
  return new Promise((resolve, reject) => wx.downloadFile({
    url: ORIGIN + path, header: authHeaders(path, {}), timeout,
    success(response) {
      if (response.statusCode >= 200 && response.statusCode < 300 && response.tempFilePath) resolve(response.tempFilePath);
      else reject(responseError(response, message));
    },
    fail() { reject(new Error(message)); },
  }));
}
function downloadProjectNoteMedia(projectID, mediaID) {
  return downloadProjectNoteFile(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/media/' + encodeURIComponent(mediaID)), 15000, '固定版本素材加载失败');
}
function downloadProjectNoteWord(projectID, versionID) {
  return downloadProjectNoteFile(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/versions/' + encodeURIComponent(versionID) + '/export.docx'), 30000, 'Word 导出失败，请重试');
}
function downloadSharedProjectNoteWord(token) {
  return downloadProjectNoteFile(projectNotePath('/shared/' + encodeURIComponent(token) + '/export.docx'), 30000, 'Word 导出失败，请确认分享仍有效');
}

function uploadProjectNoteMedia(projectID, filePath) {
  const path = projectNotePath('/projects/' + encodeURIComponent(projectID) + '/media');
  return new Promise((resolve, reject) => wx.uploadFile({
    url: ORIGIN + path, filePath, name: 'file', header: authHeaders(path, {}, false), timeout: 60000,
    success(response) {
      let data = {};
      try { data = JSON.parse(response.data || '{}'); } catch (_) { reject(new Error('私有素材上传响应无法读取')); return; }
      if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
      else reject(Object.assign(new Error(data.msg || '私有素材上传失败'), { statusCode: response.statusCode }));
    },
    fail() { reject(new Error('私有素材上传失败')); },
  }));
}

function downloadSharedProjectNoteMedia(token, id) {
  const path = projectNotePath('/shared/' + encodeURIComponent(token) + '/media/' + encodeURIComponent(id));
  return new Promise((resolve, reject) => wx.downloadFile({
    url: ORIGIN + path, header: authHeaders(path, {}), timeout: 15000,
    success(response) {
      if (response.statusCode >= 200 && response.statusCode < 300 && response.tempFilePath) resolve(response.tempFilePath);
      else reject(responseError(response, '私有附件加载失败'));
    },
    fail() { reject(new Error('私有附件加载失败')); },
  }));
}

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

function uploadWorkbenchMedia(filePath, kind, owner, rights, originalName) {
  const path = WORKBENCH + '/media';
  return new Promise((resolve, reject) => wx.uploadFile({
    url: ORIGIN + path, filePath, name: 'file', header: authHeaders(path, {}, false),
    formData: Object.assign({ owner_id: owner, rights, kind }, originalName ? { original_name: String(originalName) } : {}), timeout: 60000,
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
  ORIGIN, WORKBENCH, MY_SHARES, PROJECT_NOTES, authHeaders, request, workbenchContext, listContents, getContent, listWorkbenchMedia,
  saveContent, deleteContentDraft, previewContent, publishContent, revokeContent, mySharesContext, listMyShares, listMyShareVisits,
  importCapabilities, listImports, submitImport, getImport, retryImport,
  downloadWorkbenchMedia, uploadWorkbenchMedia, mutationKey,
  getProjectNoteHistory, getSharedProjectNoteHistory, getProjectNoteFeedback, getProjectNote, listProjectNoteProjects, createProjectNoteProject, updateProjectNoteProject, getProjectNoteProject, inviteProjectNoteMember, revokeProjectNoteMember,
  setProjectNoteConfirmer, copyProjectNote, deleteProjectNote, createProjectNote, saveProjectNote, freezeProjectNoteVersion, createProjectNoteShare,
  revokeProjectNoteShare, resolveProjectNoteFeedback, getSharedProjectNote, openSharedProjectNote,
  addSharedProjectNoteFeedback, confirmSharedProjectNote, downloadSharedProjectNoteMedia,
  uploadProjectNoteMedia,
  getProjectNoteVersion, downloadProjectNoteMedia, downloadProjectNoteWord, downloadSharedProjectNoteWord,
  listNativeReferenceCandidates, listNativeReferences, saveNativeReference, deleteNativeReference, getNativeReferenceTarget,
};
