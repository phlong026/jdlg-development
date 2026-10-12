// @ts-nocheck
'use strict';
const ROOT = '/api/v1/miniprogram/tracking';
const ORIGIN = 'https://api.jiadilingguang.com';
function request(suffix, method = 'GET', body = {}, credential, authenticated = false) {
  const path = ROOT + suffix.split('?')[0];
  let header = { 'Content-Type': 'application/json' };
  if (credential) header['X-Tracking-Token'] = credential;
  if (authenticated) {
    const token = String(wx.getStorageSync('token') || '');
    if (token) {
      // Existing session authentication binds pass to the already-cached login device value.
      // Never generate/fingerprint a device here; this value never enters analytics or anonymous IDs.
      const auth = require('../parameter');
      const loginDevice = String(wx.getStorageSync('device_fingerprint') || '');
      header = Object.assign(header, auth.paramsToHeaders(auth.generateRequestParams(token, body, path, loginDevice)));
    }
  }
  return new Promise((resolve, reject) => wx.request({
    url: ORIGIN + ROOT + suffix, method, data: body, header, timeout: 8000,
    success(response) {
      const result = response.data || {};
      if (response.statusCode >= 200 && response.statusCode < 300 && result.code === 0) resolve(result.data || {});
      else reject(Object.assign(new Error('访问记录服务暂不可用'), { statusCode: response.statusCode }));
    }, fail: () => reject(new Error('访问记录网络不可用'))
  }));
}
function mediaURL(value) { const url=String(value||''); return url.startsWith(ROOT+'/media/') ? ORIGIN+url : /^https:\/\//.test(url) ? url : ''; }
async function resolveReaderOptions(options) {
  if (options.content_id || !options.scene) return options;
  let scene; try { scene=decodeURIComponent(String(options.scene)); } catch (_) { throw new Error('扫码入口无效'); }
  if (!/^[A-Za-z0-9_-]{32}$/.test(scene)) throw new Error('扫码入口无效');
  const entry=await request('/share-entry','POST',{scene});
  if(entry.route_key!=='pages/contentreader/contentreader'||!entry.content_id||!entry.content_version||!entry.share_token)throw new Error('扫码内容不可访问');
  return {...options,content_id:entry.content_id,content_version:entry.content_version,st:entry.share_token};
}
module.exports = { request, ROOT, ORIGIN, mediaURL, resolveReaderOptions };
export {};
