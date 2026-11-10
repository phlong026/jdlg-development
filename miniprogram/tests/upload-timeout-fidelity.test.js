'use strict';
// T5.1 / T5.4：大文件上传超时 900s；网络失败保留 errMsg 且明确「网络异常」，可重试。
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');
const source = fs.readFileSync(path.join(root, 'utils/phase3-api.js'), 'utf8');

function loadApi(uploadFile) {
  const mod = { exports: {} };
  const wx = {
    getStorageSync: () => 'session-token',
    uploadFile, downloadFile: options => options.fail({ errMsg: 'downloadFile:fail' }),
    request: options => options.fail({ errMsg: 'request:fail' }),
  };
  vm.runInNewContext(source, {
    module: mod, exports: mod.exports, wx, console, Promise, Object, String, Number, JSON, Array, Date, encodeURIComponent, decodeURIComponent, setTimeout, clearTimeout,
    require: id => id.includes('parameter') ? { paramsToHeaders: () => ({}), generateRequestParams: () => ({}) } : require(path.join(root, 'utils', id.replace('./', '') + '.js')),
  });
  return mod.exports;
}

test('the media upload uses the 900s timeout agreed for large files', async () => {
  let seen;
  const api = loadApi(options => { seen = options; options.success({ statusCode: 200, data: JSON.stringify({ code: 0, data: { asset: { media_id: 'm1' } } }) }); });
  await api.uploadProjectNoteMedia('p1', '/tmp/a.jpg');
  assert.equal(seen.timeout, 900000, '客户端上传超时须与服务端 Read/Write 900s 对齐');
});

test('a network upload failure keeps errMsg and reads as a retryable network error', async () => {
  const api = loadApi(options => options.fail({ errMsg: 'uploadFile:fail timeout' }));
  await assert.rejects(api.uploadProjectNoteMedia('p1', '/tmp/a.jpg'), error => {
    assert.equal(error.networkError, true);
    assert.equal(error.errMsg, 'uploadFile:fail timeout');
    assert.match(error.message, /网络异常/);
    return true;
  });
});

test('a server rejection keeps its message and status code', async () => {
  const api = loadApi(options => options.success({ statusCode: 413, data: JSON.stringify({ msg: '媒体体积超过限制' }) }));
  await assert.rejects(api.uploadProjectNoteMedia('p1', '/tmp/a.jpg'), error => {
    assert.equal(error.statusCode, 413);
    assert.match(error.message, /媒体体积超过限制/);
    return true;
  });
});
