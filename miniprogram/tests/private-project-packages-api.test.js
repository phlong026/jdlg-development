'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function loadAPI() {
  const calls = [];
  const base = {
    ORIGIN: 'https://api.example.test',
    authHeaders(route) { return { signedPath: route }; },
    request(route, method = 'GET', body = {}, query = {}) { calls.push({ kind: 'request', route, method, body, query }); return Promise.resolve({}); },
    mutationKey(prefix) { return prefix + '-key'; },
  };
  const source = fs.readFileSync(path.join(root, 'utils/project-packages-api.ts'), 'utf8');
  const module = { exports: {} };
  const wx = { downloadFile(options) { calls.push({ kind: 'download', options }); options.success({ statusCode: 200, tempFilePath: '/tmp/package-media' }); } };
  vm.runInNewContext(source, { module, exports: module.exports, wx, require(id) { if (id === './phase3-api') return base; throw new Error(id); }, Promise, Error, Object, String, Number, Date, encodeURIComponent });
  return { api: module.exports, calls };
}

test('project package manager and invited APIs use separate namespaces', async () => {
  const { api, calls } = loadAPI();
  await api.getProjectPackageProject('project-one');
  await api.saveProjectPackageDraft('project-one', { version_ids: ['version-one'], recipient_ids: [101] });
  await api.previewProjectPackage('package-one');
  await api.publishProjectPackage('package-one', { expected_revision: 1 });
  await api.getSharedProjectPackage('package-token');
  await api.getSharedProjectPackageVersion('package-token', 'version-one');
  assert.deepEqual(calls.map(call => call.route), [
    '/api/v1/miniprogram/project-notes/projects/project-one',
    '/api/v1/miniprogram/project-notes/projects/project-one/packages/draft',
    '/api/v1/miniprogram/project-notes/packages/package-one/preview',
    '/api/v1/miniprogram/project-notes/packages/package-one/publish',
    '/api/v1/miniprogram/project-packages/shared/package-token',
    '/api/v1/miniprogram/project-packages/shared/package-token/versions/version-one',
  ]);
  assert.ok(calls.slice(4).every(call => !call.route.includes('/project-notes/shared/')));
});

test('package feedback confirmation receipt and media remain exact-version bound', async () => {
  const { api, calls } = loadAPI();
  await api.addSharedProjectPackageFeedback('package-token', 'version-one', '调整门厅', 'needs_adjustment', 'feedback-key');
  await api.confirmSharedProjectPackageVersion('package-token', 'version-one', 'confirm-key');
  await api.acknowledgeSharedProjectPackage('package-token', 'version-one', 'receipt-key');
  const file = await api.downloadSharedProjectPackageMedia('package-token', 'version-one', 'media-one');
  assert.equal(file, '/tmp/package-media');
  assert.deepEqual(calls.slice(0, 3).map(call => call.route), [
    '/api/v1/miniprogram/project-packages/shared/package-token/versions/version-one/feedback',
    '/api/v1/miniprogram/project-packages/shared/package-token/versions/version-one/confirm',
    '/api/v1/miniprogram/project-packages/shared/package-token/receipts',
  ]);
  assert.equal(calls[2].body.acknowledged, true);
  assert.equal(calls[2].body.version_id, 'version-one');
  assert.equal(calls[3].options.header.signedPath, '/api/v1/miniprogram/project-packages/shared/package-token/versions/version-one/media/media-one');
  assert.doesNotMatch(calls[3].options.url, /[?&](?:token|authorization)=/i);
});
