'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function loadAPI(autoRespond = true) {
  const requests = [];
  const signatures = [];
  const module = { exports: {} };
  const wx = {
    getStorageSync: key => ({ token: 'designer-session', device_fingerprint: 'known-device' })[key] || '',
    request(options) {
      requests.push(options);
      if (autoRespond) options.success({ statusCode: 200, data: { code: 0, data: {} } });
    },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/project-collections-api.js'), 'utf8'), {
    module, exports: module.exports, wx, Promise, Error, Object, String, Number, JSON, encodeURIComponent,
    require() {
      return {
        generateRequestParams(token, body, route, device) {
          signatures.push({ token, body, route, device });
          return { token, pass: 'signed' };
        },
        paramsToHeaders: value => value,
      };
    },
  });
  return { api: module.exports, requests, signatures };
}

test('collection API signs management calls and keeps public reader calls unsigned', async () => {
  const { api, requests, signatures } = loadAPI();
  await api.listCandidates({ cursor: 'next page', limit: 20 });
  await api.saveCollection({ title: '案例合集', owner_id: 'owner-a', items: [] });
  await api.previewCollection('collection-1', 3);
  await api.publishCollection('collection-1', 3, 'preview-token', 'publish-key');
  await api.readPublishedCollection('collection-1', 2);
  await api.openPublishedItem('collection-1', 2, 0);

  assert.equal(signatures.length, 4);
  assert.equal(signatures[0].route, '/api/v1/miniprogram/workbench/project-collections/candidates?cursor=next%20page&limit=20');
  assert.equal(signatures[0].device, 'known-device');
  assert.equal(requests[4].header.token, undefined);
  assert.equal(requests[5].header.token, undefined);
  assert.match(requests[4].url, /\/tracking\/project-collections\/collection-1\/versions\/2$/);
  assert.match(requests[5].url, /\/items\/0$/);
  assert.deepEqual(JSON.parse(JSON.stringify(requests[3].data)), {
    expected_revision: 3,
    preview_token: 'preview-token',
    idempotency_key: 'publish-key',
  });
});

test('collection API exposes the server message and status for conflict recovery', async () => {
  const { api, requests } = loadAPI(false);
  const pending = api.getCollection('collection-1');
  requests[0].success({ statusCode: 409, data: { code: 409, msg: '合集已更新，请重新读取' } });
  await assert.rejects(pending, error => error.statusCode === 409 && /重新读取/.test(error.message));
});
