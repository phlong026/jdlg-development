'use strict';
// AI 流式生成通道（B6 抽取自 chat 页）：复用 /ai/chat/stream SSE 端点与跨块解析，
// 供聊天页与文案页等就地生成场景共用。计费确认、余额不足提示由调用方处理。
const { generateRequestParams, paramsToHeaders } = require('./parameter');
const { getCachedDeviceFingerprint } = require('./deviceFingerprint');

const API_BASE_URL = 'https://api.jiadilingguang.com';
const STREAM_PATH = '/api/v1/miniprogram/ai/chat/stream';

function arrayBufferToString(buffer) {
  const bytes = new Uint8Array(buffer);
  // UTF-8 解码：微信基础库 TextDecoder 可用性不稳定，手写多字节还原。
  let out = '';
  let i = 0;
  while (i < bytes.length) {
    const first = bytes[i];
    if (first < 0x80) { out += String.fromCharCode(first); i += 1; }
    else if (first < 0xE0) { out += String.fromCharCode(((first & 0x1F) << 6) | (bytes[i + 1] & 0x3F)); i += 2; }
    else if (first < 0xF0) { out += String.fromCharCode(((first & 0x0F) << 12) | ((bytes[i + 1] & 0x3F) << 6) | (bytes[i + 2] & 0x3F)); i += 3; }
    else { i += 1; }
  }
  return out;
}

/**
 * 就地发起 AI 流式生成。回调均为可选；返回 requestTask（含 abort）。
 * events: { onChunk(text), onDone(fullText), onError(message, statusCode?) }
 */
function startAIStream(options) {
  const messages = options.messages || [];
  const token = String(wx.getStorageSync('token') || '');
  const deviceID = getCachedDeviceFingerprint() || '';
  const params = generateRequestParams(token, JSON.stringify({ messages, system_prompt: '' }), STREAM_PATH, deviceID);
  const headers = paramsToHeaders(params);

  let fullContent = '';
  let pendingBytes = new Uint8Array(0);
  let settled = false;

  const finish = (ok, message, statusCode) => {
    if (settled) return;
    settled = true;
    if (ok && options.onDone) options.onDone(fullContent);
    if (!ok && options.onError) options.onError(message || '生成失败，请重试', statusCode);
  };

  const requestTask = wx.request({
    url: `${API_BASE_URL}${STREAM_PATH}`,
    method: 'POST',
    header: { ...headers, 'Content-Type': 'application/json', Accept: 'text/event-stream' },
    data: { messages, system_prompt: '' },
    enableChunked: true,
    success: res => {
      if (res.statusCode === 402 && res.data) {
        const data = res.data || {};
        const required = Number((data.data || {}).required || 0);
        const current = Number((data.data || {}).current || 0);
        finish(false, `当前生成需要 ${required} 灵石，你的余额为 ${current} 灵石，请先充值后再试。`, 402);
        return;
      }
      if (res.statusCode === 200 && res.data && typeof res.data === 'object' && res.data.code !== undefined && res.data.code !== 0) {
        finish(false, res.data.msg || '生成失败，请重试', res.statusCode);
        return;
      }
      if (res.statusCode !== 200) {
        finish(false, `生成失败：${res.statusCode}`, res.statusCode);
        return;
      }
      finish(true);
    },
    fail: err => finish(false, `网络错误：${(err && err.errMsg) || '请稍后重试'}`),
  });

  requestTask.onChunkReceived(res => {
    try {
      const bytes = new Uint8Array(res.data);
      const combined = new Uint8Array(pendingBytes.length + bytes.length);
      combined.set(pendingBytes);
      combined.set(bytes, pendingBytes.length);
      const lines = [];
      let start = 0;
      for (let index = 0; index < combined.length; index++) {
        if (combined[index] === 10) {
          lines.push(arrayBufferToString(combined.slice(start, index).buffer));
          start = index + 1;
        }
      }
      pendingBytes = combined.slice(start);
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue;
        const payload = line.substring(6).trim();
        if (!payload || payload === '[DONE]') continue;
        try {
          const data = JSON.parse(payload);
          if (data.type === 'content' && data.content) {
            fullContent += data.content;
            if (options.onChunk) options.onChunk(fullContent);
          } else if (data.type === 'done') {
            finish(true);
          } else if (data.type === 'error') {
            finish(false, data.error || '生成失败，请重试');
          }
        } catch (_) { /* 忽略无法解析的单行 */ }
      }
    } catch (_) { /* 网络块异常不中断整体流 */ }
  });

  return requestTask;
}

module.exports = { startAIStream, arrayBufferToString };
