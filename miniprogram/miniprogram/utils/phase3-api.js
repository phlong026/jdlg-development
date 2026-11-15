'use strict';

const ORIGIN = 'https://api.jiadilingguang.com';
const WORKBENCH = '/api/v1/miniprogram/workbench';
const IMPORTS = WORKBENCH + '/imports';
const MY_SHARES = '/api/v1/miniprogram/tracking/my-shares';
const MY_VISITORS = '/api/v1/miniprogram/tracking/my-visitors';
const MY_VISITS = '/api/v1/miniprogram/tracking/my-visits';
const MY_CONTENT_STATS = '/api/v1/miniprogram/tracking/my-content-stats';
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
  const fullPath = path + queryString(query);
  return new Promise((resolve, reject) => wx.request({
    url: ORIGIN + fullPath, method, data: body, header: authHeaders(fullPath, body), timeout: 10000,
    success(response) {
      const data = response.data || {};
      if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
      else reject(responseError(response, '请求失败，请稍后重试'));
    },
    fail() { reject(Object.assign(new Error('网络不可用，请检查连接'), { networkError: true })); },
  }));
}

async function workbenchContext() {
  const context = await request(WORKBENCH + '/context');
  const owners = context.owners || [];
  const labels = new Map((context.owner_choices || []).map(item => [item.owner_id, String(item.display_name || '').trim()]));
  // Keep presentation aligned with authorized IDs, including during a staged server rollout.
  return Object.assign({}, context, {
    owner_choices: owners.map((owner_id, index) => ({
      owner_id,
      display_name: labels.get(owner_id) || (owners.length === 1 ? '我的内容' : '内容归属 ' + (index + 1)),
    })),
  });
}
function listContents(query) { return request(WORKBENCH + '/contents', 'GET', {}, query); }
function getContent(id) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id)); }
function listWorkbenchMedia(query) { return request(WORKBENCH + '/media', 'GET', {}, query); }
function saveContent(draft, id) { return request(WORKBENCH + '/contents' + (id ? '/' + encodeURIComponent(id) : ''), id ? 'PATCH' : 'POST', draft); }
function deleteCreatedContent(id, revision) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/delete', 'POST', { expected_revision: Number(revision) }); }
function deleteContentDraft(id, revision) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id), 'DELETE', { expected_revision: Number(revision) }); }
function previewContent(id, revision) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/preview', 'POST', { expected_revision: revision }); }
function publishContent(id, revision, previewToken, key) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/publish', 'POST', { expected_revision: revision, preview_token: previewToken, idempotency_key: key }); }
function revokeContent(id, revision, reason, key) { return request(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/revoke', 'POST', { expected_revision: revision, reason, idempotency_key: key }); }
function mySharesContext() { return request(MY_SHARES + '/context'); }
function listMyShares(query) { return request(MY_SHARES, 'GET', {}, query); }
function listMyShareVisits(id, query) { return request(MY_SHARES + '/' + encodeURIComponent(id) + '/visits', 'GET', {}, query); }
function listMyVisitors(query) { return request(MY_VISITORS, 'GET', {}, query); }
function listMyVisits(query) { return request(MY_VISITS, 'GET', {}, query); }
function listMyVisitorEntries(visitor, query) { return request(MY_VISITORS + '/entries', 'GET', {}, Object.assign({ visitor: visitor }, query || {})); }
function convertMyVisitorToLead(visitor) { return request(MY_VISITORS + '/lead', 'POST', { visitor }); }
function listMyContentStats(query) { return request(MY_CONTENT_STATS, 'GET', {}, query); }
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
function createProjectInvitation(projectID, expiresAt, key) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/member-invitations'), 'POST', { expires_at: expiresAt, idempotency_key: key }); }
function listProjectInvitations(projectID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/member-invitations')); }
function revokeProjectInvitation(projectID, invitationID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/member-invitations/' + encodeURIComponent(invitationID)), 'DELETE'); }
function getProjectInvitation(token) { return request(projectNotePath('/member-invitations/' + encodeURIComponent(token))); }
function applyProjectInvitation(token, key) { return request(projectNotePath('/member-invitations/' + encodeURIComponent(token) + '/apply'), 'POST', { idempotency_key: key }); }
function listProjectJoinRequests(projectID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/member-requests')); }
function approveProjectJoinRequest(projectID, requestID, key) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/member-requests/' + encodeURIComponent(requestID) + '/approve'), 'POST', { idempotency_key: key }); }
function revokeProjectNoteMember(projectID, accountID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/members/' + encodeURIComponent(accountID)), 'DELETE', {}); }
function setProjectNoteConfirmer(projectID, accountID) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/confirmer'), 'PUT', { account_id: Number(accountID) }); }
function createProjectNote(projectID, draft) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID) + '/notes'), 'POST', draft); }
function copyProjectNote(noteID, revision) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID) + '/copy'), 'POST', { expected_revision: Number(revision) }); }
function deleteProjectNoteProject(projectID, revision) { return request(projectNotePath('/projects/' + encodeURIComponent(projectID)), 'DELETE', { expected_revision: Number(revision) }); }
function deleteProjectNote(noteID, revision) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID)), 'DELETE', { expected_revision: Number(revision) }); }
function saveProjectNote(noteID, draft) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID)), 'PATCH', draft); }
function freezeProjectNoteVersion(noteID, revision) { return request(projectNotePath('/notes/' + encodeURIComponent(noteID) + '/versions'), 'POST', { expected_revision: Number(revision) }); }
function createProjectNoteShare(versionID, expiresAt, key) { return request(projectNotePath('/versions/' + encodeURIComponent(versionID) + '/shares'), 'POST', { expires_at: expiresAt, idempotency_key: key }); }
function revokeProjectNoteShare(shareID) { return request(projectNotePath('/shares/' + encodeURIComponent(shareID) + '/revoke'), 'POST', {}); }
function resolveProjectNoteFeedback(feedbackID, reply, key) { return request(projectNotePath('/feedback/' + encodeURIComponent(feedbackID) + '/resolve'), 'POST', { reply, idempotency_key: key }); }
function getSharedProjectNote(token) { return request(projectNotePath('/shared/' + encodeURIComponent(token))); }
async function getSharedProjectNoteMediaPlayback(token, id) {
  const playback = await request(projectNotePath('/shared/' + encodeURIComponent(token) + '/media/' + encodeURIComponent(id) + '/playback'), 'POST', {});
  const url = String(playback && playback.url || '');
  return Object.assign({}, playback, { url: url.startsWith('/') ? ORIGIN + url : url });
}
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
function downloadContentWord(id, version) {
  return downloadProjectNoteFile(WORKBENCH + '/contents/' + encodeURIComponent(id) + '/versions/' + Number(version) + '/export.docx', 30000, 'Word 导出失败，请重试');
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
    url: ORIGIN + path, filePath, name: 'file', header: authHeaders(path, {}, false), timeout: 900000,
    success(response) {
      let data = {};
      try { data = JSON.parse(response.data || '{}'); } catch (_) { reject(Object.assign(new Error('私有素材上传响应无法读取'), { statusCode: response.statusCode })); return; }
      if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
      else reject(Object.assign(new Error(data.msg || '私有素材上传失败'), { statusCode: response.statusCode }));
    },
    fail(error) { reject(Object.assign(new Error(error && error.errMsg ? '网络异常：' + error.errMsg : '网络异常，请检查网络后重试'), { networkError: true, errMsg: String(error && error.errMsg || '') })); },
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
    fail(error) { reject(new Error(error && error.errMsg || '媒体上传失败，请检查网络后重试')); },
  }));
}

function beginVideoUpload(input) { return request(WORKBENCH + '/media-uploads', 'POST', input); }
function videoUploadStatus(id) { return request(WORKBENCH + '/media-uploads/' + encodeURIComponent(id)); }
function completeVideoUpload(id) { return request(WORKBENCH + '/media-uploads/' + encodeURIComponent(id) + '/complete', 'POST', {}); }
function uploadVideoPart(id, part, filePath, options = {}) {
  const path = WORKBENCH + '/media-uploads/' + encodeURIComponent(id) + '/parts/' + encodeURIComponent(part);
  return new Promise((resolve, reject) => {
    const task = wx.uploadFile({
      url: ORIGIN + path, filePath, name: 'file', header: authHeaders(path, {}, false), timeout: 120000,
      success(response) {
        let data;
        try { data = JSON.parse(response.data || '{}'); } catch (_) { reject(new Error('分片上传响应无法读取，请重试')); return; }
        if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
        else reject(Object.assign(new Error(data.msg || '分片上传失败，请重试'), { statusCode: response.statusCode }));
      },
      fail(error) { reject(Object.assign(new Error(error && error.errMsg || '分片网络传输失败，请重试'), { networkError: true })); },
    });
    if (task && task.onProgressUpdate && options.onProgress) task.onProgressUpdate(options.onProgress);
  });
}

function mutationKey(prefix) {
  mutationSequence += 1;
  return prefix + '-' + Date.now() + '-' + mutationSequence;
}

// 服务端 Prompt 注册表（B5）：业务措辞可运营调整而无需发版。
async function writingPrompts() {
  return request(WORKBENCH + '/writing-prompts');
}

module.exports = {
  downloadContentWord, ORIGIN, WORKBENCH, MY_SHARES, PROJECT_NOTES, authHeaders, request, beginVideoUpload, videoUploadStatus, uploadVideoPart, completeVideoUpload, workbenchContext, writingPrompts, listContents, getContent, listWorkbenchMedia,
  saveContent, deleteCreatedContent, deleteContentDraft, previewContent, publishContent, revokeContent, mySharesContext, listMyShares, listMyVisitors, listMyVisits, listMyVisitorEntries, convertMyVisitorToLead, listMyContentStats, listMyShareVisits,
  importCapabilities, listImports, submitImport, getImport, retryImport,
  downloadWorkbenchMedia, uploadWorkbenchMedia, mutationKey,
  getProjectNoteHistory, getSharedProjectNoteHistory, getProjectNoteFeedback, getProjectNote, listProjectNoteProjects, createProjectNoteProject, updateProjectNoteProject, getProjectNoteProject,
  createProjectInvitation, listProjectInvitations, revokeProjectInvitation, getProjectInvitation, applyProjectInvitation, listProjectJoinRequests, approveProjectJoinRequest,
  revokeProjectNoteMember,
  setProjectNoteConfirmer, copyProjectNote, deleteProjectNoteProject, deleteProjectNote, createProjectNote, saveProjectNote, freezeProjectNoteVersion, createProjectNoteShare,
  revokeProjectNoteShare, resolveProjectNoteFeedback, getSharedProjectNote, openSharedProjectNote,
  addSharedProjectNoteFeedback, confirmSharedProjectNote, downloadSharedProjectNoteMedia, getSharedProjectNoteMediaPlayback,
  uploadProjectNoteMedia,
  getProjectNoteVersion, downloadProjectNoteMedia, downloadProjectNoteWord, downloadSharedProjectNoteWord,
  listNativeReferenceCandidates, listNativeReferences, saveNativeReference, deleteNativeReference, getNativeReferenceTarget,
};
