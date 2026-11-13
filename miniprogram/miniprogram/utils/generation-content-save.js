'use strict';
const api = require('./phase3-api');

function accountKey() {
  const token = String(wx.getStorageSync('token') || '');
  if (!token) throw new Error('请先登录');
  const profile = wx.getStorageSync('userInfo') || {};
  if (profile.id && profile.token === token) return 'user-' + String(profile.id);
  // 尚未拿到本人资料时隔离当前会话；不把登录凭据写入缓存。
  let hash = 2166136261;
  for (let index = 0; index < token.length; index += 1) hash = Math.imul(hash ^ token.charCodeAt(index), 16777619);
  return 'session-' + (hash >>> 0).toString(16);
}
function storageKeyFor(taskNo) { return 'generation-content-attempt:' + accountKey() + ':' + taskNo; }
function savedContent(taskNo) {
  const record = wx.getStorageSync(storageKeyFor(taskNo));
  return record && record.taskNo === taskNo && typeof record.contentID === 'string' ? record.contentID : '';
}
function forgetAttempt(taskNo) { wx.removeStorageSync(storageKeyFor(taskNo)); }
function attemptFor(taskNo, title) {
  const account = accountKey(), storageKey = storageKeyFor(taskNo);
  const previous = wx.getStorageSync(storageKey);
  if (previous && previous.contentID) throw new Error('已有保存内容，请选择继续编辑或另存新草稿');
  if (previous && previous.taskNo === taskNo && previous.title === title && typeof previous.key === 'string') return { ...previous, storageKey, account };
  const attempt = { taskNo, title, key: api.mutationKey('generation-content') };
  // 发送前落盘；请求失去响应或页面重建仍复用同一个保存操作。
  wx.setStorageSync(storageKey, attempt);
  return { ...attempt, storageKey, account };
}
function completeAttempt(attempt, contentID) {
  if (attempt.account !== accountKey()) return;
  const current = wx.getStorageSync(attempt.storageKey);
  if (current && current.key === attempt.key) wx.setStorageSync(attempt.storageKey, { taskNo: attempt.taskNo, title: attempt.title, key: attempt.key, contentID });
}
module.exports = { attemptFor, completeAttempt, savedContent, forgetAttempt };
