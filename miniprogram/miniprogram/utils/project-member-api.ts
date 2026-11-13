// @ts-nocheck
'use strict';
const api = require('./phase3-api');
const base = projectID => '/api/v1/miniprogram/project-notes/member-projects/' + encodeURIComponent(projectID);
const versionPath = (projectID, versionID) => base(projectID) + '/versions/' + encodeURIComponent(versionID);
const mediaPath = (projectID, versionID, mediaID) => versionPath(projectID, versionID) + '/media/' + encodeURIComponent(mediaID);
module.exports = {
  directory: (projectID, query) => api.request(base(projectID), 'GET', {}, query || {}),
  version: (projectID, versionID) => api.request(versionPath(projectID, versionID)),
  async playback(projectID, versionID, mediaID) {
    const value = await api.request(mediaPath(projectID, versionID, mediaID) + '/playback', 'POST', {});
    return Object.assign({}, value, { url: String(value.url || '').startsWith('/') ? api.ORIGIN + value.url : value.url });
  },
  download(projectID, versionID, mediaID) {
    const route = mediaPath(projectID, versionID, mediaID);
    return new Promise((resolve, reject) => wx.downloadFile({
      url: api.ORIGIN + route, header: api.authHeaders(route, {}), timeout: 15000,
      success(response) { if (response.statusCode === 200 && response.tempFilePath) resolve(response.tempFilePath); else reject(Object.assign(new Error('项目素材加载失败'), { statusCode: response.statusCode })); },
      fail() { reject(new Error('项目素材下载失败，请重试')); },
    }));
  },
};
