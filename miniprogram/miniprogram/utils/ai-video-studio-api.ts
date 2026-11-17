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
function idempotencyKey() { sequence += 1; return 'aivs-' + Date.now() + '-' + sequence + '-' + Math.random().toString(36).slice(2, 14); }
function submissionStorageKey() {
  const info = wx.getStorageSync('userInfo') || {};
  const userID = Number(info.id);
  if (!currentToken() || !Number.isSafeInteger(userID) || userID < 1 || (info.token && info.token !== currentToken())) throw new Error('账号资料尚未就绪，请重新登录后生成');
  return 'aivs:pending:' + userID;
}
function pendingSubmission() {
  const value = wx.getStorageSync(submissionStorageKey());
  return value && typeof value.quote_id === 'string' && typeof value.idempotency_key === 'string'
    ? { quote_id: value.quote_id, idempotency_key: value.idempotency_key } : null;
}
function clearSubmission(storageKey, value) {
  const pending = wx.getStorageSync(storageKey);
  if (pending && pending.quote_id === value.quote_id && pending.idempotency_key === value.idempotency_key) wx.removeStorageSync(storageKey);
}
function polishStorageKey() {
  const info = wx.getStorageSync('userInfo') || {};
  const userID = Number(info.id);
  if (!currentToken() || !Number.isSafeInteger(userID) || userID < 1 || (info.token && info.token !== currentToken())) throw new Error('账号资料尚未就绪，请重新登录后润色');
  return 'aivs:polish:' + userID;
}
function pendingPolish() {
  const value = wx.getStorageSync(polishStorageKey());
  return value && typeof value.quote_id === 'string' && typeof value.idempotency_key === 'string'
    ? { quote_id: value.quote_id, idempotency_key: value.idempotency_key, task_id: String(value.task_id || '') } : null;
}
function clearPolish(value) {
  const storageKey = polishStorageKey(), pending = wx.getStorageSync(storageKey);
  if (pending && pending.quote_id === value.quote_id && pending.idempotency_key === value.idempotency_key) wx.removeStorageSync(storageKey);
}
function terminalPolish(status) { return ['completed', 'failed', 'cancelled'].includes(String(status || '')); }
async function getPolishSubmission(input) {
  const task = await request(ROOT + '/prompt-polish/submissions/' + encodeURIComponent(input.idempotency_key));
  if (!task.task_id && !task.id) throw new Error('润色任务暂不可读，请稍后查询');
  if (terminalPolish(task.status)) clearPolish(input);
  else wx.setStorageSync(polishStorageKey(), Object.assign({}, input, { task_id: task.task_id || task.id }));
  return task;
}
async function createPolishTask(input) {
  const storageKey = polishStorageKey(), previous = pendingPolish();
  if (previous && previous.quote_id !== input.quote_id) throw Object.assign(new Error('上次润色结果待核对，请先查询'), { pendingPolish: true });
  const value = previous || { quote_id: input.quote_id, idempotency_key: input.idempotency_key, task_id: '' };
  wx.setStorageSync(storageKey, value);
  try {
    const task = await request(ROOT + '/prompt-polish/tasks', 'POST', { quote_id: value.quote_id, idempotency_key: value.idempotency_key });
    if (!task.task_id && !task.id) throw Object.assign(new Error('润色结果待核对，请稍后查询'), { networkError: true });
    wx.setStorageSync(storageKey, Object.assign({}, value, { task_id: task.task_id || task.id }));
    if (terminalPolish(task.status)) clearPolish(value);
    return task;
  } catch (error) {
    if ([400, 402, 409, 410, 422].includes(Number(error.statusCode))) clearPolish(value);
    throw error;
  }
}
async function getPolishTask(id) {
  const task = await request(ROOT + '/prompt-polish/tasks/' + encodeURIComponent(id));
  let pending = null;
  try { pending = pendingPolish(); } catch (_) { return task; }
  if (pending && terminalPolish(task.status)
      && pending.quote_id === String(task.quote_id || '')
      && (!pending.task_id || pending.task_id === String(id))) clearPolish(pending);
  return task;
}
async function getSubmission(input) {
  const storageKey = submissionStorageKey();
  const batch = await request(ROOT + '/submissions/' + encodeURIComponent(input.idempotency_key));
  if (!batch.batch_id && !batch.id) throw new Error('提交记录暂不可读，请稍后查询');
  clearSubmission(storageKey, input);
  return batch;
}
async function createBatch(input) {
  const storageKey = submissionStorageKey();
  const previous = pendingSubmission();
  if (previous && previous.quote_id !== input.quote_id) throw Object.assign(new Error('上次提交结果待核对，请先查询提交结果'), { pendingSubmission: true });
  const value = previous || { quote_id: input.quote_id, idempotency_key: input.idempotency_key };
  // Persist before sending: a killed page can replay this exact request safely.
  wx.setStorageSync(storageKey, value);
  try {
    const batch = await request(ROOT + '/batches', 'POST', value);
    if (!batch.batch_id && !batch.id) throw Object.assign(new Error('提交结果待核对，请查询提交结果'), { networkError: true });
    clearSubmission(storageKey, value);
    return batch;
  } catch (error) {
    if ([400, 402, 409, 410, 422].includes(Number(error.statusCode))) clearSubmission(storageKey, value);
    throw error;
  }
}
function adoptionStorageKey(kind, mediaID) {
  const info = wx.getStorageSync('userInfo') || {};
  const account = Number(info.id);
  if (!currentToken() || !Number.isSafeInteger(account) || account < 1 || (info.token && info.token !== currentToken())) throw new Error('账号资料尚未就绪，请重新登录后采用视频');
  return 'aivs:adopt:' + account + ':' + kind + ':' + encodeURIComponent(mediaID);
}
function pendingAdoption(kind, mediaID) {
  const value = wx.getStorageSync(adoptionStorageKey(kind, mediaID));
  return value && value.input && typeof value.input.draft_instance_id === 'string' ? Object.assign({}, value.input) : null;
}
async function adoptMedia(kind, mediaID, input) {
  const storageKey = adoptionStorageKey(kind, mediaID), token = currentToken();
  const payload = Object.assign({}, input); delete payload.draft_instance_id;
  const signature = JSON.stringify(payload), previous = wx.getStorageSync(storageKey);
  if (previous && previous.signature !== signature) throw new Error('上次采用结果待核对，请先重试原目标');
  const attempt = previous || { signature, input: Object.assign({}, payload, { draft_instance_id: input.draft_instance_id || idempotencyKey() }) };
  wx.setStorageSync(storageKey, attempt);
  const clear = () => {
    if (token !== currentToken()) return;
    const current = wx.getStorageSync(storageKey);
    if (current && current.input && current.input.draft_instance_id === attempt.input.draft_instance_id) wx.removeStorageSync(storageKey);
  };
  try {
    const result = await request(ROOT + '/media/' + encodeURIComponent(mediaID) + '/adopt/' + kind, 'POST', attempt.input);
    clear(); return result;
  } catch (error) {
    if ([400, 403, 404, 409, 410, 422].includes(Number(error.statusCode))) clear();
    throw error;
  }
}
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
  ORIGIN, ROOT, request, idempotencyKey, uploadMedia, pendingSubmission, getSubmission, pendingPolish, getPolishSubmission,
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
  polishCapabilities: () => request(ROOT + '/prompt-polish/capabilities'),
  polishQuote: input => request(ROOT + '/prompt-polish/quotes', 'POST', input),
  createPolishTask,
  getPolishTask,
  cancelPolishTask: id => request(ROOT + '/prompt-polish/tasks/' + encodeURIComponent(id) + '/cancel', 'POST', {}),
  createBatch,
  listBatches: query => request(ROOT + '/batches', 'GET', {}, query),
  getBatch: id => request(ROOT + '/batches/' + encodeURIComponent(id)).then(normalizeBatch),
  cancelBatch: id => request(ROOT + '/batches/' + encodeURIComponent(id) + '/cancel', 'POST', {}),
  playback: id => request(ROOT + '/results/' + encodeURIComponent(id) + '/playback', 'POST', {}),
  retain: id => request(ROOT + '/results/' + encodeURIComponent(id) + '/retain', 'POST', {}),
  adoptContent: (mediaID, input) => adoptMedia('content', mediaID, input),
  adoptProjectNote: (mediaID, input) => adoptMedia('project-note', mediaID, input),
  pendingAdoption,
  getProjectNoteAdoptionTarget: (projectID, noteID) => request(ROOT + '/adoption-targets/projects/' + encodeURIComponent(projectID) + '/notes/' + encodeURIComponent(noteID)),
  normalizeBatch,
};
