// @ts-nocheck
'use strict';

const ORIGIN = 'https://api.jiadilingguang.com';
const ROOT = '/api/v1/miniprogram/ai-video-studio';
let sequence = 0;

function queryString(values) {
  const fields = [];
  Object.keys(values || {}).forEach(key => {
    const value = values[key];
    if (value !== undefined && value !== null && String(value) !== '') fields.push(encodeURIComponent(key) + '=' + encodeURIComponent(String(value)));
  });
  return fields.length ? '?' + fields.join('&') : '';
}
function currentToken() { return String(wx.getStorageSync('token') || ''); }
function headers(path, body, json) {
  const token = currentToken();
  if (!token) throw Object.assign(new Error('请先登录'), { statusCode: 401 });
  const auth = require('./parameter');
  const device = String(wx.getStorageSync('device_fingerprint') || '');
  const value = auth.paramsToHeaders(auth.generateRequestParams(token, body || {}, path, device));
  return json === false ? value : Object.assign({ 'Content-Type': 'application/json' }, value);
}
function request(path, method, body, query) {
  const fullPath = path + queryString(query);
  const token = currentToken();
  return new Promise((resolve, reject) => wx.request({
    url: ORIGIN + fullPath, method: method || 'GET', data: body || {}, header: headers(fullPath, body || {}), timeout: 15000,
    success(response) {
      if (token !== currentToken()) { reject(Object.assign(new Error('账号已切换，请重新进入'), { staleAccount: true })); return; }
      const data = response.data || {};
      if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
      else reject(Object.assign(new Error(data.msg || '请求失败，请稍后重试'), { statusCode: response.statusCode, code: data.code }));
    },
    fail(error) { reject(Object.assign(new Error(error && error.errMsg || '网络不可用，请检查连接'), { networkError: true })); },
  }));
}
function idempotencyKey() { sequence += 1; return 'aivs-' + Date.now() + '-' + sequence; }
function normalizeBatch(value) {
  const batch = value || {};
  const items = Array.isArray(batch.items) ? batch.items : [];
  const results = (Array.isArray(batch.results) ? batch.results : items).map(item => Object.assign({}, item, {
    result_id: item.result_id || item.id,
    retention_status: item.retention_status || (item.stable_media_id ? 'retained' : 'not_retained'),
  }));
  return Object.assign({}, batch, { snapshot: batch.snapshot || batch.input_snapshot || {}, results });
}
function uploadMedia(filePath, onProgress) {
  const path = ROOT + '/media/uploads', token = currentToken();
  let task;
  const promise = new Promise((resolve, reject) => {
    task = wx.uploadFile({ url: ORIGIN + path, filePath, name: 'file', formData: { rights: 'own' }, header: headers(path, {}, false), timeout: 900000,
      success(response) {
        if (token !== currentToken()) { reject(Object.assign(new Error('账号已切换，请重新进入'), { staleAccount: true })); return; }
        let data; try { data = JSON.parse(response.data || '{}'); } catch (_) { reject(new Error('素材上传响应无法读取')); return; }
        if (response.statusCode >= 200 && response.statusCode < 300 && data.code === 0) resolve(data.data || {});
        else reject(Object.assign(new Error(data.msg || '素材上传失败'), { statusCode: response.statusCode, code: data.code }));
      },
      fail(error) { reject(Object.assign(new Error(/abort/i.test(String(error && error.errMsg || '')) ? '已取消上传' : (error && error.errMsg || '素材上传失败')), { cancelled: /abort/i.test(String(error && error.errMsg || '')) })); },
    });
    if (task && task.onProgressUpdate && onProgress) task.onProgressUpdate(onProgress);
  });
  return { promise, cancel() { if (task && task.abort) task.abort(); } };
}

module.exports = {
  ORIGIN, ROOT, request, idempotencyKey, uploadMedia,
  capabilities: () => request(ROOT + '/capabilities'),
  listDrafts: query => request(ROOT + '/drafts', 'GET', {}, query),
  createDraft: input => request(ROOT + '/drafts', 'POST', input),
  getDraft: id => request(ROOT + '/drafts/' + encodeURIComponent(id)),
  updateDraft: (id, input) => request(ROOT + '/drafts/' + encodeURIComponent(id), 'PATCH', input),
  deleteDraft: (id, revision) => request(ROOT + '/drafts/' + encodeURIComponent(id), 'DELETE', { expected_revision: Number(revision) }),
  listPrompts: query => request(ROOT + '/prompts', 'GET', {}, query),
  createPrompt: input => request(ROOT + '/prompts', 'POST', input),
  updatePrompt: (id, input) => request(ROOT + '/prompts/' + encodeURIComponent(id), 'PATCH', input),
  deletePrompt: (id, revision) => request(ROOT + '/prompts/' + encodeURIComponent(id), 'DELETE', { expected_revision: Number(revision) }),
  listMedia: query => request(ROOT + '/media', 'GET', {}, query),
  getMedia: id => request(ROOT + '/media/' + encodeURIComponent(id)),
  registerMedia: mediaID => request(ROOT + '/media/register', 'POST', { media_id: mediaID }),
  quote: input => request(ROOT + '/quotes', 'POST', input),
  createBatch: input => request(ROOT + '/batches', 'POST', { quote_id: input.quote_id, idempotency_key: input.idempotency_key }),
  listBatches: query => request(ROOT + '/batches', 'GET', {}, query),
  getBatch: id => request(ROOT + '/batches/' + encodeURIComponent(id)).then(normalizeBatch),
  playback: id => request(ROOT + '/results/' + encodeURIComponent(id) + '/playback', 'POST', {}),
  retain: id => request(ROOT + '/results/' + encodeURIComponent(id) + '/retain', 'POST', {}),
  adoptContent: (mediaID, input) => request(ROOT + '/media/' + encodeURIComponent(mediaID) + '/adopt/content', 'POST', input),
  normalizeBatch,
};
