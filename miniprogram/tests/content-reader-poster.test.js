'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram/pages/contentreader');

test('reader requests a poster for the exact published content version', () => {
  for (const extension of ['ts', 'js']) {
    const source = fs.readFileSync(path.join(root, 'contentreader.' + extension), 'utf8');
    const start = source.indexOf('async generatePoster()');
    const block = source.slice(start, source.indexOf('\n }\n});', start));
    assert.ok(start >= 0 && block.length > 0, extension + ' poster block missing');
    assert.match(block, /contentID=String\(this\.options\.content_id/, extension + ' must capture the content before async work');
    assert.match(block, /version=String\(this\.options\.content_version/, extension + ' must capture the published version before async work');
    assert.match(block, /content_id:contentID,content_version:version/, extension + ' must request the captured immutable content version');
    assert.doesNotMatch(block, /\bpage\s*:/, extension + ' must not choose an arbitrary QR page');
  }
});

function posterReader(options = {}) {
  let definition;
  const account = { token: 'account-one' };
  const downloads = [];
  let exports = 0;
  let saves = 0;
  const wx = {
    getStorageSync(key) { return key === 'token' ? account.token : null; },
    downloadFile(request) { downloads.push(request); },
    createSelectorQuery() { return { in() { return this; }, select() { return this; }, fields(_fields, callback) { callback({ node: options.canvas }); return this; }, exec() {} }; },
    canvasToTempFilePath(request) { exports += 1; request.success({ tempFilePath: '/tmp/poster.png' }); },
    saveImageToPhotosAlbum(request) { saves += 1; request.success(); },
    showToast() {},
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'contentreader.js'), 'utf8'), {
    Page(value) { definition = value; }, wx, Promise, Object, Array, String, Number, Math, Error, Set, Date, encodeURIComponent,
    require(id) {
      if (['../../utils/note-reader-actions', '../../utils/note-interactions'].includes(id)) return { data: {}, methods: {} };
      if (id === '../../utils/phase3-api') return { request: options.request || (async () => ({ poster_url: 'https://example.test/qr.png' })) };
      if (id === '../../utils/tracking/index') return {};
      if (id === '../../utils/tracking/api') return {};
      if (id === '../../utils/tracking/video') return { createVideoTracker: () => ({}) };
      if (id === '../../utils/content-presentation') return { buildContentPresentation: () => ({}) };
      if (id === '../../utils/protected-media') return { resolveProtectedMediaURL: async value => value };
      throw new Error('unexpected require ' + id);
    },
  });
  const page = Object.assign({}, definition, {
    data: { ...definition.data, contentReady: true, content: { title: '测试海报', owner_id: 'owner-a', cover: { url: 'wxfile://cover.jpg' } }, posterAvailable: true },
    options: { content_id: 'content-one', content_version: '3' },
    posterCapabilitySequence: 1,
    posterCapability: { token: 'account-one', contentID: 'content-one', version: '3', owner: 'owner-a', sequence: 1 },
    setData(values) { Object.assign(this.data, values); },
  });
  return { page, account, downloads, exports: () => exports, saves: () => saves };
}

test('poster generation uses a local cover directly and aborts before export when QR download fails', async () => {
  const { page, downloads, exports, saves } = posterReader();
  const pending = page.generatePoster();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(downloads.length, 1, '本地封面不应再调用 downloadFile');
  downloads[0].fail({ errMsg: 'downloadFile:fail timeout' });
  await pending;
  assert.equal(exports(), 0);
  assert.equal(saves(), 0);
  assert.equal(page.data.posterBusy, false);
});

test('account change invalidates an in-flight poster before canvas export or album save', async () => {
  const { page, account, downloads, exports, saves } = posterReader();
  const pending = page.generatePoster();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(downloads.length, 1);
  account.token = 'account-two';
  page.invalidatePosterAvailability();
  downloads[0].success({ statusCode: 200, tempFilePath: '/tmp/qr.png' });
  await pending;
  assert.equal(exports(), 0);
  assert.equal(saves(), 0);
  assert.equal(page.data.posterBusy, false, '账号切换负责解锁，旧任务不能再改写状态');
});

test('an obsolete poster request cannot unlock the next account request', async () => {
  const requests = [];
  const { page, account, downloads } = posterReader({ request: () => new Promise(resolve => requests.push(resolve)) });
  const oldTask = page.generatePoster();
  await new Promise(resolve => setImmediate(resolve));
  account.token = 'account-two';
  page.invalidatePosterAvailability();
  page.posterCapability = { token: 'account-two', contentID: 'content-one', version: '3', owner: 'owner-a', sequence: page.posterCapabilitySequence };
  page.data.posterAvailable = true;
  const newTask = page.generatePoster();
  await new Promise(resolve => setImmediate(resolve));
  requests[0]({ poster_url: 'https://example.test/old-qr.png' });
  await oldTask;
  assert.equal(page.data.posterBusy, true, '旧任务 finally 不得解锁新任务');
  requests[1]({ poster_url: 'https://example.test/new-qr.png' });
  await new Promise(resolve => setImmediate(resolve));
  downloads[0].fail({ errMsg: 'downloadFile:fail timeout' });
  await newTask;
  assert.equal(page.data.posterBusy, false);
});
