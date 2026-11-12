'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const file = { path: '/tmp/source.pdf', name: '方案.pdf', size: 1024 };
const asset = { media_id: 'document-one', title: '方案.pdf', kind: 'document', mime: 'application/pdf' };
const saved = { content_id: 'saved-document', revision: 1, version: 0, status: 'draft' };
const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function harness(overrides = {}) {
  let definition, chooser, token = 'account-a';
  const calls = [];
  const api = { uploadWorkbenchMedia: async () => asset, saveContent: async draft => { calls.push(draft); return saved; }, ...overrides };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/pages/contentedit/contentedit.js'), 'utf8'), {
    Page: value => { definition = value; },
    wx: { getStorageSync: () => token, hideShareMenu() {}, showToast() {}, chooseMessageFile: value => { chooser = value; }, createVideoContext: () => ({ pause() {} }), getWindowInfo: () => ({ windowWidth: 375 }) },
    require: id => id.endsWith('phase3-api') ? api : { renderArticleDocument: () => [], preserveLayoutBody: (_layout, body) => body },
  });
  const page = { ...definition, data: JSON.parse(JSON.stringify(definition.data)), _token: token, _epoch: 1, _loaded: true, setData(value) { Object.assign(this.data, value); } };
  page.setData({ context: { media_upload_configured: true }, owner_id: 'owner-a', rights: 'own' });
  return { page, calls, chooser: () => chooser, token: value => { token = value; } };
}

test('document upload survives picker success before onShow and archives exactly once', async () => {
  const upload = deferred();
  const h = harness({ uploadWorkbenchMedia: () => upload.promise });
  h.page.chooseDocument(); h.chooser().success({ tempFiles: [file] });
  h.page.onHide(); h.page.onShow();
  assert.equal(h.page.data.busy, true);
  upload.resolve(asset); await tick(); await tick();
  assert.equal(h.calls.length, 1);
  assert.equal(h.page.data.id, saved.content_id);
  assert.equal(h.page.data.documentReadOnly, true);
  assert.equal(h.page.data.busy, false);
});

test('document archive response survives hide and show without replacing newly typed copy', async () => {
  const archive = deferred(); const h = harness({ saveContent: () => archive.promise });
  const pending = h.page.archiveDocumentAsset(asset);
  h.page.onHide(); h.page.onShow();
  assert.equal(h.page.data.busy, true);
  h.page.editBlock({ currentTarget: { dataset: { index: 0 } }, detail: { value: '尚未保存的正文' } });
  archive.resolve(saved); await pending;
  assert.equal(h.page.data.id, '');
  assert.equal(h.page.data.blocks[0].text, '尚未保存的正文');
  assert.match(h.page.data.notice, /文档资料已单独保存/);
  assert.equal(h.page.data.busy, false);
});

for (const stage of ['upload', 'archive']) for (const boundary of ['account', 'owner', 'rights', 'unload']) {
  test(`document ${stage} ignores completion after ${boundary} changes`, async () => {
    const wait = deferred(); const h = harness(stage === 'upload' ? { uploadWorkbenchMedia: () => wait.promise } : { saveContent: () => wait.promise });
    const pending = stage === 'upload' ? h.page.uploadDocument(file) : h.page.archiveDocumentAsset(asset);
    if (boundary === 'account') h.token('account-b');
    if (boundary === 'owner') h.page.setData({ owner_id: 'owner-b' });
    if (boundary === 'rights') h.page.setData({ rights: 'authorized' });
    if (boundary === 'unload') h.page.onUnload();
    wait.resolve(stage === 'upload' ? asset : saved); await pending;
    assert.equal(h.page.data.id, '');
    assert.equal(h.page.data.documentReadOnly, false);
    if (stage === 'upload') { assert.equal(h.calls.length, 0); assert.equal(h.page.data.documentCandidates.length, 0); }
  });
}

test('late document archive cannot clear a newer operation busy state', async () => {
  const old = deferred(); const next = deferred(); let saves = 0;
  const h = harness({ saveContent: () => (++saves === 1 ? old.promise : next.promise) });
  const first = h.page.archiveDocumentAsset(asset);
  h.token('account-b'); h.page._token = 'account-b'; h.page._mediaUploadActive = null; h.page.setData({ busy: false, owner_id: 'owner-b' });
  const second = h.page.archiveDocumentAsset({ ...asset, media_id: 'document-two' });
  old.reject(new Error('old request failed')); await first;
  assert.equal(h.page.data.busy, true);
  assert.equal(h.page.data.error, '');
  next.resolve({ ...saved, content_id: 'second-document' }); await second;
  assert.equal(h.page.data.id, 'second-document');
  assert.equal(h.page.data.busy, false);
});

for (const boundary of ['account', 'owner', 'rights', 'unload']) test(`document picker rejects stale selection after ${boundary} changes`, () => {
  const h = harness(); let uploads = 0; h.page.uploadDocument = () => { uploads += 1; };
  h.page.chooseDocument();
  if (boundary === 'account') { h.token('account-b'); h.page._token = 'account-b'; }
  if (boundary === 'owner') h.page.setData({ owner_id: 'owner-b' });
  if (boundary === 'rights') h.page.setData({ rights: 'authorized' });
  if (boundary === 'unload') h.page.onUnload();
  h.chooser().success({ tempFiles: [file] });
  assert.equal(uploads, 0);
});

for (const stage of ['upload', 'archive']) test(`document ${stage} failure after onShow releases busy and permits retry`, async () => {
  const wait = deferred();
  const h = harness(stage === 'upload' ? { uploadWorkbenchMedia: () => wait.promise } : { saveContent: () => wait.promise });
  const pending = stage === 'upload' ? h.page.uploadDocument(file) : h.page.archiveDocumentAsset(asset);
  h.page.onHide(); h.page.onShow();
  wait.reject(new Error('文档服务暂不可用')); await pending;
  assert.equal(h.page.data.busy, false);
  assert.equal(h.page._mediaUploadActive, null);
  assert.equal(h.page.data.error, '文档服务暂不可用');
});
