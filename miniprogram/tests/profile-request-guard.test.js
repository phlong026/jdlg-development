'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../miniprogram/utils/profile-request-guard.js'), 'utf8');

function harness({ token = 'account-token', route = 'pages/index/index', inlineGate = true } = {}) {
  const calls = [], navigations = [], removed = [], blocked = [], toasts = [], guardChecks = [];
  const storage = { token, device_fingerprint: 'login-device', userInfo: { id: 9 } };
  const gate = { check() { guardChecks.push(route); return Promise.resolve(false); } };
  const page = { route, onProfileAccessBlocked() { blocked.push(route); }, selectComponent: () => inlineGate ? gate : null };
  const wx = {
    request(options) { calls.push(options); return { abort() {} }; },
    getStorageSync: key => storage[key],
    removeStorageSync: key => { removed.push(key); delete storage[key]; },
    navigateTo(options) { navigations.push(options); if (options.success) options.success(); },
    showToast(options) { toasts.push(options.title); },
  };
  const module = { exports: {} };
  vm.runInNewContext(source, {
    module, exports: module.exports, wx, getCurrentPages: () => [page], console,
    require: name => {
      assert.equal(name, './parameter');
      return {
        generateRequestParams: (value, body, apiPath, device) => ({ value, body, apiPath, device }),
        paramsToHeaders: params => ({ token: params.value, signature_path: params.apiPath, signature_device: params.device, signature_body: JSON.stringify(params.body) }),
      };
    },
  });
  return { guard: module.exports, wx, calls, navigations, removed, blocked, toasts, guardChecks, storage, page };
}

test('same-origin miniprogram business requests receive the current signed session', () => {
  const h = harness(); h.guard.install();
  h.guard.install();
  const success = () => {};
  const task = wxCall(h, {
    url: 'https://api.jiadilingguang.com/api/v1/miniprogram/templates/7?preview=1',
    method: 'POST', data: { title: '方案' }, header: { 'X-Business': 'kept' }, success,
  });
  assert.equal(h.calls.length, 1, 'install is idempotent');
  assert.equal(h.calls[0].header.token, 'account-token');
  assert.equal(h.calls[0].header.signature_path, '/api/v1/miniprogram/templates/7?preview=1');
  assert.equal(h.calls[0].header.signature_device, 'login-device');
  assert.equal(h.calls[0].header['X-Business'], 'kept');
  assert.equal(h.calls[0].timeout, 15000);
  assert.notEqual(h.calls[0].success, success, 'guard observes auth failures before preserving the caller callback');
  assert.equal(typeof task.abort, 'function');
});

test('GET data is encoded into the signed URL and is not appended a second time by wx', () => {
  const h = harness(); h.guard.install();
  wxCall(h, {
    url: 'https://api.jiadilingguang.com/api/v1/miniprogram/templates?preview=1',
    method: 'GET', data: { page: 2, keyword: '中 式', empty: undefined },
  });
  assert.equal(h.calls[0].url, 'https://api.jiadilingguang.com/api/v1/miniprogram/templates?preview=1&page=2&keyword=%E4%B8%AD%20%E5%BC%8F');
  assert.deepEqual({ ...h.calls[0].data }, {});
  assert.equal(h.calls[0].header.signature_path, '/api/v1/miniprogram/templates?preview=1&page=2&keyword=%E4%B8%AD%20%E5%BC%8F');
  assert.equal(h.calls[0].header.signature_body, '{}');
});

test('complete existing signatures are preserved while unsigned multipart bodies are signed unchanged', () => {
  const h = harness(); h.guard.install();
  const signed = {
    token: 'already-signed', 'token-signature': 'ts', sin: 'sin',
    'md5-signature': 'md5', pass: 'pass', tm: 'tm',
  };
  wxCall(h, { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/profile?fixed=1', method: 'GET', data: { signed: 'already' }, header: signed });
  const body = '--boundary\r\nContent-Disposition: form-data; name="title"\r\n\r\n方案\r\n--boundary--';
  wxCall(h, {
    url: 'https://api.jiadilingguang.com/api/v1/miniprogram/assets', method: 'POST', data: body,
    header: { 'Content-Type': 'multipart/form-data; boundary=boundary' },
  });
  assert.deepEqual({ ...h.calls[0].header }, signed);
  assert.equal(h.calls[0].url, 'https://api.jiadilingguang.com/api/v1/miniprogram/profile?fixed=1');
  assert.deepEqual({ ...h.calls[0].data }, { signed: 'already' });
  assert.equal(h.calls[1].data, body);
  assert.equal(h.calls[1].header.signature_body, '{}', '服务端 multipart 验签统一使用空对象');
});

test('incomplete mixed-case legacy auth headers are removed before signing with the current account', () => {
  const h = harness({ token: 'current-account' }); h.guard.install();
  wxCall(h, {
    url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects',
    method: 'POST', data: { title: '新项目' },
    header: {
      Token: 'old-account', 'TOKEN-SIGNATURE': 'old-signature', Sin: 'old-sin',
      'Md5-Signature': 'old-md5', PASS: 'old-pass', 'X-Business': 'kept',
    },
  });
  const header = h.calls[0].header;
  assert.equal(header.token, 'current-account');
  assert.equal(header['X-Business'], 'kept');
  for (const name of ['Token', 'TOKEN-SIGNATURE', 'Sin', 'Md5-Signature', 'PASS', 'Tm']) {
    assert.equal(header[name], undefined, `${name} must not coexist with the fresh signature`);
  }
});

test('bootstrap and external requests never receive the account token', () => {
  const h = harness(); h.guard.install();
  for (const url of [
    'https://api.jiadilingguang.com/api/v1/miniprogram/login',
    'https://api.jiadilingguang.com/api/v1/miniprogram/login/password',
    'https://api.jiadilingguang.com/api/v1/miniprogram/login/phone',
    'https://api.jiadilingguang.com/api/v1/miniprogram/sms/send',
    'https://api.jiadilingguang.com/api/v1/miniprogram/profile/password/recover',
    'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/config',
    'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/share-entry',
    'https://third-party.example/api/v1/miniprogram/content',
  ]) wxCall(h, { url, header: {} });
  assert.ok(h.calls.every(call => !call.header.token), 'no bootstrap or external request leaks the token');
});

test('428 keeps the valid token, locks content and opens profile completion only once', () => {
  const delivered = [];
  const h = harness(); h.guard.install();
  wxCall(h, { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/content', success: value => delivered.push(value) });
  wxCall(h, { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/content/2' });
  const response = { statusCode: 428, data: { code: 428, msg: '请先补充头像和昵称', data: { profile_required: true, missing_fields: ['avatar'] } } };
  h.calls[0].success(response); h.calls[1].success(response);
  assert.equal(h.storage.token, 'account-token');
  assert.deepEqual(h.removed, []);
  assert.deepEqual(h.blocked, ['pages/index/index', 'pages/index/index']);
  assert.equal(h.navigations.length, 0);
  assert.deepEqual(h.guardChecks, ['pages/index/index']);
  assert.deepEqual(delivered, [response]);
});

test('401 clears the expired session and reopens the inline login card', () => {
  const h = harness(); h.guard.install();
  wxCall(h, { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects' });
  h.calls[0].success({ statusCode: 401, data: { code: 401 } });
  assert.equal(h.storage.token, undefined);
  assert.deepEqual(h.removed, ['token', 'userInfo']);
  assert.deepEqual(h.blocked, ['pages/index/index']);
  assert.equal(h.navigations.length, 0);
  assert.deepEqual(h.guardChecks, ['pages/index/index']);
});

test('a page without an inline gate still opens the standalone login page', () => {
  const h = harness({ inlineGate: false }); h.guard.install();
  wxCall(h, { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects' });
  h.calls[0].success({ statusCode: 428, data: { code: 428 } });
  assert.equal(h.navigations.length, 1);
  assert.equal(h.navigations[0].url, '/pages/login/login?from=profile-gate');
});

test('ordinary profile readers still receive global session recovery', () => {
  const h = harness(); h.guard.install();
  wxCall(h, {
    url: 'https://api.jiadilingguang.com/api/v1/miniprogram/profile',
    method: 'GET',
  });
  h.calls[0].success({ statusCode: 401, data: { code: 401 } });
  assert.equal(h.storage.token, undefined);
  assert.deepEqual(h.removed, ['token', 'userInfo']);
  assert.deepEqual(h.blocked, ['pages/index/index']);
  assert.deepEqual(h.guardChecks, ['pages/index/index']);
});

test('an explicitly owned GET profile response belongs to the calling login card', () => {
  const delivered = [];
  const h = harness(); h.guard.install();
  wxCall(h, {
    url: 'https://api.jiadilingguang.com/api/v1/miniprogram/profile',
    method: 'GET',
    __profileAccessResponseHandled: true,
    success: response => delivered.push(response),
  });
  const response = { statusCode: 401, data: { code: 401 } };
  h.calls[0].success(response);
  assert.equal(h.storage.token, 'account-token');
  assert.deepEqual(h.removed, []);
  assert.deepEqual(h.blocked, []);
  assert.deepEqual(h.guardChecks, []);
  assert.deepEqual(delivered, [response]);
  assert.equal(h.calls[0].__profileAccessResponseHandled, undefined, 'local ownership marker must not leave the client');
});

test('ownership marker cannot bypass the global gate outside an exact GET profile request', () => {
  for (const request of [
    { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects', method: 'GET' },
    { url: 'https://api.jiadilingguang.com/api/v1/miniprogram/profile', method: 'POST' },
  ]) {
    const h = harness(); h.guard.install();
    wxCall(h, { ...request, __profileAccessResponseHandled: true });
    h.calls[0].success({ statusCode: 401, data: { code: 401 } });
    assert.equal(h.storage.token, undefined);
    assert.deepEqual(h.guardChecks, ['pages/index/index']);
    assert.equal(h.calls[0].__profileAccessResponseHandled, undefined);
  }
});

test('late 401, 428 and 200 responses from an old account cannot affect the new session', () => {
  const h = harness({ token: 'old-account' }); h.guard.install();
  const successes = [], failures = [], completes = [];
  for (let index = 0; index < 3; index++) wxCall(h, {
    url: `https://api.jiadilingguang.com/api/v1/miniprogram/content/${index}`,
    success: value => successes.push(value),
    fail: error => failures.push(error),
    complete: value => completes.push(value),
  });
  h.storage.token = 'new-account'; h.storage.userInfo = { id: 10 };
  h.calls[0].success({ statusCode: 401, data: { code: 401 } });
  h.calls[1].success({ statusCode: 428, data: { code: 428, data: { profile_required: true } } });
  h.calls[2].success({ statusCode: 200, data: { code: 0, data: { private: 'old account data' } } });
  assert.deepEqual(successes, []);
  assert.equal(failures.length, 3);
  assert.ok(failures.every(error => error.sessionChanged === true));
  assert.equal(h.storage.token, 'new-account');
  assert.deepEqual(h.removed, []);
  assert.deepEqual(h.blocked, []);
  assert.deepEqual(h.navigations, []);
  h.calls[0].complete({ statusCode: 200, data: { private: 'old complete data' } });
  assert.equal(completes[0].sessionChanged, true);
  assert.doesNotMatch(JSON.stringify(completes), /old complete data|private/);
});

function wxCall(h, options) {
  return h.wx.request(options);
}
