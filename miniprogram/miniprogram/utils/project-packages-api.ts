// @ts-nocheck
'use strict';
const api = require('./phase3-api');

const MANAGE = '/api/v1/miniprogram/project-notes';
const SHARED = '/api/v1/miniprogram/project-packages';
const pathID = value => encodeURIComponent(String(value || ''));

function getProjectPackageProject(projectID) {
  return api.request(MANAGE + '/projects/' + pathID(projectID));
}
function saveProjectPackageDraft(projectID, input) {
  return api.request(MANAGE + '/projects/' + pathID(projectID) + '/packages/draft', 'PUT', input);
}
function previewProjectPackage(packageID) {
  return api.request(MANAGE + '/packages/' + pathID(packageID) + '/preview');
}
function publishProjectPackage(packageID, input) {
  return api.request(MANAGE + '/packages/' + pathID(packageID) + '/publish', 'POST', input);
}
function revokeProjectPackageDelivery(deliveryID) {
  return api.request(MANAGE + '/package-deliveries/' + pathID(deliveryID) + '/revoke', 'POST', {});
}
function getProjectPackageHistory(projectID) {
  return api.request(MANAGE + '/projects/' + pathID(projectID) + '/package-history');
}
function sharedPath(token, suffix = '') {
  return SHARED + '/shared/' + pathID(token) + suffix;
}
function getSharedProjectPackage(token) {
  return api.request(sharedPath(token));
}
function getSharedProjectPackageVersion(token, versionID) {
  return api.request(sharedPath(token, '/versions/' + pathID(versionID)));
}
async function getSharedProjectPackageMediaPlayback(token, versionID, mediaID) {
  const playback = await api.request(sharedPath(token, '/versions/' + pathID(versionID) + '/media/' + pathID(mediaID) + '/playback'), 'POST', {});
  const url = String(playback && playback.url || '');
  return Object.assign({}, playback, { url: url.startsWith('/') ? api.ORIGIN + url : url });
}
function addSharedProjectPackageFeedback(token, versionID, text, satisfaction, key) {
  return api.request(sharedPath(token, '/versions/' + pathID(versionID) + '/feedback'), 'POST', {
    text, satisfaction: satisfaction || '', idempotency_key: key,
  });
}
function confirmSharedProjectPackageVersion(token, versionID, key) {
  return api.request(sharedPath(token, '/versions/' + pathID(versionID) + '/confirm'), 'POST', { idempotency_key: key });
}
function acknowledgeSharedProjectPackage(token, versionID, eventID) {
  return api.request(sharedPath(token, '/receipts'), 'POST', { version_id: versionID || '', event_id: eventID, acknowledged: true });
}
function downloadSharedProjectPackageMedia(token, versionID, mediaID) {
  const route = sharedPath(token, '/versions/' + pathID(versionID) + '/media/' + pathID(mediaID));
  return new Promise((resolve, reject) => wx.downloadFile({
    url: api.ORIGIN + route, header: api.authHeaders(route, {}), timeout: 15000,
    success(response) {
      if (response.statusCode >= 200 && response.statusCode < 300 && response.tempFilePath) resolve(response.tempFilePath);
      else reject(Object.assign(new Error('私密素材加载失败'), { statusCode: response.statusCode }));
    },
    fail() { reject(new Error('私密素材加载失败')); },
  }));
}

module.exports = {
  MANAGE, SHARED, mutationKey: api.mutationKey,
  getProjectPackageProject, saveProjectPackageDraft, previewProjectPackage, publishProjectPackage,
  revokeProjectPackageDelivery, getProjectPackageHistory,
  getSharedProjectPackage, getSharedProjectPackageVersion, downloadSharedProjectPackageMedia, getSharedProjectPackageMediaPlayback,
  addSharedProjectPackageFeedback, confirmSharedProjectPackageVersion, acknowledgeSharedProjectPackage,
};
