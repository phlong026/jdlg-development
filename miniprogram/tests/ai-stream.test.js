'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../miniprogram/utils/ai-stream.js'), 'utf8');

function harness() {
  let requestOptions;
  let chunkHandler;
  const task = { onChunkReceived(handler) { chunkHandler = handler; }, abort() {} };
  const wx = {
    getStorageSync: key => key === 'token' ? 'test-token' : '',
    request(options) { requestOptions = options; return task; },
  };
  const module = { exports: {} };
  vm.runInNewContext(source, {
    module,
    exports: module.exports,
    wx,
    JSON,
    String,
    Number,
    Uint8Array,
    require(id) {
      if (id.endsWith('/parameter')) return { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) };
      if (id.endsWith('/deviceFingerprint')) return { getCachedDeviceFingerprint: () => 'test-device' };
      throw new Error(`unexpected require ${id}`);
    },
  });
  const chunks = [];
  const done = [];
  const errors = [];
  module.exports.startAIStream({
    messages: [{ role: 'user', content: 'test' }],
    onChunk: value => chunks.push(value),
    onDone: value => done.push(value),
    onError: (message, status) => errors.push({ message, status }),
  });
  const push = value => {
    const bytes = typeof value === 'string' ? Uint8Array.from(Buffer.from(value)) : Uint8Array.from(value);
    chunkHandler({ data: bytes.buffer });
  };
  return {
    chunks,
    done,
    errors,
    decode: value => module.exports.arrayBufferToString(Uint8Array.from(value).buffer),
    push,
    success: (statusCode = 200, data = '') => requestOptions.success({ statusCode, data }),
    fail: () => requestOptions.fail({ errMsg: 'request:fail network' }),
  };
}

test('UTF-8 decoder preserves emoji and supplementary Chinese characters', () => {
  const h = harness();
  const text = '庭院🌿𠮷';
  assert.equal(h.decode(Buffer.from(text)), text);
});

test('four-byte UTF-8 split across chunks survives a final SSE frame without newline', () => {
  const h = harness();
  const expected = '方案🎉𠮷';
  const payload = Buffer.from(`data: {"type":"content","content":"${expected}"}`);
  const emojiStart = payload.indexOf(Buffer.from('🎉'));
  const extendedStart = payload.indexOf(Buffer.from('𠮷'));
  h.push(payload.subarray(0, emojiStart + 1));
  h.push(payload.subarray(emojiStart + 1, extendedStart + 2));
  h.push(payload.subarray(extendedStart + 2));
  h.success();
  assert.deepEqual(h.chunks, [expected]);
  assert.deepEqual(h.done, [expected]);
  assert.deepEqual(h.errors, []);
});

test('invalid UTF-8 uses safe replacement for overlong surrogate and out-of-range sequences', () => {
  const h = harness();
  const invalid = Uint8Array.from([
    0xC0, 0xAF,
    0xED, 0xA0, 0x80,
    0xF4, 0x90, 0x80, 0x80,
    0xF5, 0x80, 0x80, 0x80,
    0xE2, 0x82,
  ]);
  const expected = new TextDecoder().decode(invalid);
  assert.equal(h.decode(invalid), expected);
  assert.ok(expected.includes('\uFFFD'));
});

test('HTTP success flushes a final content frame without a trailing newline', () => {
  const h = harness();
  h.push('data: {"type":"content","content":"末尾正文"}');
  h.success();
  assert.deepEqual(h.chunks, ['末尾正文']);
  assert.deepEqual(h.done, ['末尾正文']);
  assert.deepEqual(h.errors, []);
});

test('split UTF-8 and event boundaries preserve content and a final done frame settles once', () => {
  const h = harness();
  const payload = Buffer.from('data: {"type":"content","content":"庭院中文"}\n\ndata: {"type":"done"}');
  const chineseByte = payload.indexOf(Buffer.from('中')) + 1;
  h.push(payload.subarray(0, chineseByte));
  h.push(payload.subarray(chineseByte, payload.length - 7));
  h.push(payload.subarray(payload.length - 7));
  h.success();
  h.success();
  assert.deepEqual(h.chunks, ['庭院中文']);
  assert.deepEqual(h.done, ['庭院中文']);
  assert.deepEqual(h.errors, []);
});

test('[DONE] without a trailing newline completes once and ignores late duplicate frames', () => {
  const h = harness();
  h.push('data: {"type":"content","content":"完整结果"}\n\ndata: [DONE]');
  h.success();
  h.push('\ndata: {"type":"content","content":"重复结果"}\n');
  assert.deepEqual(h.chunks, ['完整结果']);
  assert.deepEqual(h.done, ['完整结果']);
  assert.deepEqual(h.errors, []);
});

test('a final SSE error is not converted into正文 and cannot later complete', () => {
  const h = harness();
  h.push('data: {"type":"error","error":"生成中断"}');
  h.success();
  h.fail();
  assert.deepEqual(h.chunks, []);
  assert.deepEqual(h.done, []);
  assert.deepEqual(h.errors, [{ message: '生成中断', status: undefined }]);
});

test('malformed final JSON is ignored instead of becoming generated text', () => {
  const h = harness();
  h.push('data: {not-json}');
  h.success();
  assert.deepEqual(h.chunks, []);
  assert.deepEqual(h.done, ['']);
  assert.deepEqual(h.errors, []);
});

test('HTTP 200 remains the completion fallback when the stream has no sentinel', () => {
  const h = harness();
  h.push('data: {"type":"content","content":"正常正文"}\n\n');
  h.success();
  assert.deepEqual(h.done, ['正常正文']);
  assert.deepEqual(h.errors, []);
});

test('402 keeps the signed billing error and settles exactly once', () => {
  const h = harness();
  h.push('data: {"type":"content","content":"计费前半截"}\n\n');
  h.success(402, { data: { required: 12, current: 3 } });
  h.success();
  h.fail();
  assert.deepEqual(h.done, []);
  assert.equal(h.errors.length, 1);
  assert.equal(h.errors[0].status, 402);
  assert.match(h.errors[0].message, /12/);
  assert.match(h.errors[0].message, /3/);
  assert.match(h.errors[0].message, /充值/);
});
