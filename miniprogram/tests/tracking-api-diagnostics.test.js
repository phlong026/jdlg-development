'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function api(request) {
  const sandbox = { module: { exports: {} }, wx: { request }, Promise, Error, Number };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/utils/tracking/api.js'), 'utf8'), sandbox);
  return sandbox.module.exports;
}

test('tracking failures expose safe status and operation without a token or request body', async () => {
  for (const [statusCode, code] of [[401, 401], [409, 409], [503, 503], [200, 503]]) {
    const tracking = api(options => options.success({ statusCode, data: { code, message: 'unsafe upstream detail token=secret' } }));
    await assert.rejects(tracking.request('/entry?st=secret', 'POST', { share_token: 'secret' }), error => {
      assert.equal(error.statusCode, statusCode);
      assert.equal(error.code, code);
      assert.equal(error.operation, '/entry');
      assert.equal(error.message, '访问记录服务暂不可用');
      assert.doesNotMatch(JSON.stringify(error), /secret|share_token|unsafe/);
      return true;
    });
  }
});

test('network failure is distinguishable without serializing wx error details', async () => {
  const tracking = api(options => options.fail({ errMsg: 'request:fail https://example.test?st=secret' }));
  await assert.rejects(tracking.request('/shares', 'POST', { share_token: 'secret' }), error => {
    assert.equal(error.networkError, true);
    assert.equal(error.operation, '/shares');
    assert.equal(error.statusCode, undefined);
    assert.equal(error.message, '访问记录网络不可用');
    assert.doesNotMatch(JSON.stringify(error), /secret|share_token|https:/);
    return true;
  });
});
