'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function reader(options = {}) {
  let definition;
  const sheets = [];
  const account = options.account || { token: '' };
  const wx = {
    showActionSheet(options) { sheets.push(options); },
    getStorageSync(key) { return key === 'token' ? account.token : null; },
    setClipboardData(value) { value.success(); },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.js'), 'utf8'), {
    Page: value => { definition = value; },
    wx, Promise, Object, Array, String, Number, Math, Error, Set, encodeURIComponent,
    require: name => name.includes('tracking/index') ? { consentChoice: () => ({ resolved: true, accepted: false }), ...options.tracking }
      : name.includes('content-presentation') ? { buildContentPresentation: () => ({}) }
      : name.includes('protected-media') ? { resolveProtectedMediaURL: async value => value }
      : name.includes('tracking/video') ? { createVideoTracker: () => ({}) }
      : name.includes('phase3-api') ? options.phase3API || {}
      : {},
  });
  const calls = [];
  const page = Object.assign({}, definition, {
    data: { ...definition.data, contentReady: true, consentAvailable: false, presentationView: { kind: 'video' } },
    setData(values, callback) { Object.assign(this.data, values); if (callback) callback.call(this); },
    copyWebLink: options.realCopy ? definition.copyWebLink : function () { calls.push('link'); },
    generatePoster() { calls.push('poster'); },
    openTrackingSettings() { calls.push('tracking'); },
  });
  return { page, sheets, calls, account };
}

test('video footer replaces the duplicate copy button while non-video actions stay intact', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.wxml'), 'utf8');
  assert.doesNotMatch(wxml, /video-copy-action|复制视频转发文案/);
  const start = wxml.indexOf('class="reader-actions video-reader-actions"');
  const end = wxml.indexOf('</view>', start);
  const footer = wxml.slice(start, end);
  assert.ok(start >= 0, 'video footer missing');
  assert.ok(footer.indexOf('分享给好友') < footer.indexOf('复制文案'));
  assert.ok(footer.indexOf('复制文案') < footer.indexOf('更多'));
  assert.match(wxml, /class="note-interaction-dock"/);
  assert.match(wxml, /bindtap="openNoteMore"/);
  assert.match(wxml, /class="note-toolbar-action" open-type="share"/);
  assert.doesNotMatch(wxml, /class="poster-action"/);

});

test('video more menu reuses existing actions and only offers analytics settings when available', () => {
  const { page, sheets, calls } = reader({ account: { token: 'account-one' } });
  page.options = { content_id: 'content-one', content_version: '3' };
  page.data.content = { owner_id: 'owner-a' };
  page.openVideoMore();
  assert.deepEqual(Array.from(sheets[0].itemList), ['复制链接']);
  sheets[0].success({ tapIndex: 0 });
  assert.deepEqual(calls, ['link']);

  page.data.consentAvailable = true;
  page.openVideoMore();
  assert.deepEqual(Array.from(sheets[1].itemList), ['复制链接', '访问分析设置']);
  sheets[1].success({ tapIndex: 1 });
  assert.deepEqual(calls, ['link', 'tracking']);

  page.posterCapabilitySequence = 1;
  page.posterCapability = { token: 'account-one', contentID: 'content-one', version: '3', owner: 'owner-a', sequence: 1 };
  page.data.posterAvailable = true;
  page.openVideoMore();
  assert.deepEqual(Array.from(sheets[2].itemList), ['复制链接', '生成海报', '访问分析设置']);
  sheets[2].success({ tapIndex: 1 });
  assert.deepEqual(calls, ['link', 'tracking', 'poster']);
});

test('more menu is unavailable outside a ready video reader', () => {
  const { page, sheets } = reader();
  page.data.presentationView.kind = 'article';
  page.openVideoMore();
  page.data.presentationView.kind = 'video';
  page.data.contentReady = false;
  page.openVideoMore();
  assert.equal(sheets.length, 0);
});

test('poster capability requires server can_manage and the current content owner', async () => {
  for (const [context, expected] of [
    [{ enabled: true, can_manage: true, owners: ['owner-a'] }, true],
    [{ enabled: true, can_manage: false, owners: ['owner-a'] }, false],
    [{ enabled: true, can_manage: true, owners: ['owner-b'] }, false],
  ]) {
    let requests = 0;
    const { page, account } = reader({ account: { token: 'account-one' }, phase3API: { workbenchContext: async () => { requests += 1; return context; } } });
    page.options = { content_id: 'content-one', content_version: '3' };
    page.data.content = { content_id: 'content-one', version: 3, owner_id: 'owner-a' };
    page.data.contentReady = true;
    page.loadSequence = 7;
    page.posterCapabilitySequence = 1;
    await page.refreshPosterAvailability(page.data.content, 7, 1);
    assert.equal(page.data.posterAvailable, expected, JSON.stringify(context));
    assert.equal(requests, 1);
    assert.equal(account.token, 'account-one');
  }
});

test('poster capability never requests without a token and ignores a delayed previous account', async () => {
  let resolveContext;
  let requests = 0;
  const phase3API = { workbenchContext: () => { requests += 1; return new Promise(resolve => { resolveContext = resolve; }); } };
  const { page, account } = reader({ account: { token: '' }, phase3API });
  page.options = { content_id: 'content-one', content_version: '3' };
  page.data.content = { content_id: 'content-one', version: 3, owner_id: 'owner-a' };
  page.data.contentReady = true;
  page.loadSequence = 7;
  page.posterCapabilitySequence = 1;
  await page.refreshPosterAvailability(page.data.content, 7, 1);
  assert.equal(requests, 0);
  assert.equal(page.data.posterAvailable, false);

  account.token = 'account-one';
  const pending = page.refreshPosterAvailability(page.data.content, 7, 2);
  account.token = 'account-two';
  resolveContext({ enabled: true, can_manage: true, owners: ['owner-a'] });
  await pending;
  assert.equal(page.data.posterAvailable, false, 'a previous account response cannot expose poster generation');
});

test('delayed capability cannot cross reader sequence, content or owner boundaries', async () => {
  for (const [label, change] of [
    ['sequence', page => { page.loadSequence = 8; }],
    ['content', page => { page.options.content_id = 'content-two'; }],
    ['owner', page => { page.data.content.owner_id = 'owner-b'; }],
  ]) {
    let resolveContext;
    const { page } = reader({ account: { token: 'account-one' }, phase3API: { workbenchContext: () => new Promise(resolve => { resolveContext = resolve; }) } });
    page.options = { content_id: 'content-one', content_version: '3' };
    page.data.content = { content_id: 'content-one', version: 3, owner_id: 'owner-a' };
    page.data.contentReady = true;
    page.loadSequence = 7;
    page.posterCapabilitySequence = 1;
    const pending = page.refreshPosterAvailability(page.data.content, 7, 1);
    change(page);
    resolveContext({ enabled: true, can_manage: true, owners: ['owner-a'] });
    await pending;
    assert.equal(page.data.posterAvailable, false, label + ' boundary accepted a stale capability');
  }
});

test('generatePoster is inert until a current server capability proof exists', async () => {
  let requests = 0;
  const { page } = reader({ phase3API: { request: async () => { requests += 1; return {}; } } });
  page.options = { content_id: 'content-one', content_version: '3' };
  page.data.content = { owner_id: 'owner-a' };
  page.data.contentReady = true;
  await page.generatePoster();
  assert.equal(requests, 0);
  assert.equal(page.data.posterAvailable, false);
});

test('copying a web link leaves an in-flight note interaction intact', async () => {
  const { page } = reader({ account: { token: 'account-one' }, realCopy: true,
    tracking: { consented: () => true, prepareShare: async () => ({ path: '/reader?st=proof' }), splitPath: () => ({ query: { st: 'proof' } }), confirmPreparedShare() {} },
    phase3API: { request: async () => ({ web_url: 'https://example.test/note' }) } });
  page.options = { content_id: 'content-one', content_version: '3' };
  page.data.content = { title: '笔记' };
  page.data.noteActionBusy = true;
  let invalidations = 0;
  page.invalidateNoteInteractions = () => { invalidations += 1; };
  await page.copyWebLink();
  assert.equal(page.data.webLinkBusy, false);
  assert.equal(page.data.noteActionBusy, true);
  assert.equal(invalidations, 0);
});
