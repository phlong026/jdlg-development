'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const modulePath = '../miniprogram/utils/protected-media';
const internalURL = 'https://api.jiadilingguang.com/api/v1/miniprogram/tracking/media/asset-one';
const publicMediaID = 'a'.repeat(32);

function runtime() {
  const storage = {
    token: 'token-a',
    device_fingerprint: 'device-a',
    userInfo: { id: 7, nickname: '设计师', avatar: 'https://cdn.example/avatar.png', token: 'token-a' },
  };
  const downloads = [];
  const navigations = [];
  global.wx = {
    getStorageSync: key => storage[key],
    removeStorageSync: key => { delete storage[key]; },
    navigateTo: options => navigations.push(options.url),
    downloadFile: options => downloads.push(options),
  };
  const parameterPath = require.resolve('../miniprogram/utils/parameter');
  require.cache[parameterPath] = {
    id: parameterPath,
    filename: parameterPath,
    loaded: true,
    exports: {
      generateRequestParams: (token, body, path, device) => ({ token, body, path, device }),
      paramsToHeaders: value => ({ ...value, pass: 'signed' }),
    },
  };
  delete require.cache[require.resolve(modulePath)];
  const media = require(modulePath);
  return { storage, downloads, navigations, media };
}

test.afterEach(() => { delete global.wx; delete global.getCurrentPages; });

test('protected API media is signed, downloaded once and reused only by the same identity', async () => {
  const { downloads, media } = runtime();
  const first = media.resolveProtectedMediaURL(internalURL);
  const second = media.resolveProtectedMediaURL(internalURL);
  assert.equal(downloads.length, 1);
  assert.equal(downloads[0].url, internalURL);
  assert.equal(downloads[0].header.token, 'token-a');
  assert.equal(downloads[0].timeout, 60000);
  downloads[0].success({ statusCode: 200, tempFilePath: 'wxfile://tmp/asset-one' });
  assert.equal(await first, 'wxfile://tmp/asset-one');
  assert.equal(await second, 'wxfile://tmp/asset-one');
  assert.equal(await media.resolveProtectedMediaURL(internalURL), 'wxfile://tmp/asset-one');
  assert.equal(downloads.length, 1);
});

test('external HTTPS media remains remote and never receives account headers', async () => {
  const { downloads, media } = runtime();
  const external = 'https://cdn.example/public.jpg';
  assert.equal(await media.resolveProtectedMediaURL(external), external);
  assert.equal(downloads.length, 0);
});

test('published content and collection media remain remote without an account', async () => {
  const { storage, downloads, media } = runtime();
  storage.token = '';
  storage.userInfo = {};
  const published = `https://api.jiadilingguang.com/api/v1/miniprogram/tracking/media/${publicMediaID}`;
  const collection = `https://api.jiadilingguang.com/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/${publicMediaID}`;
  assert.equal(await media.resolveProtectedMediaURL(published), published);
  assert.equal(await media.resolveProtectedMediaURL(collection), collection);
  assert.equal(downloads.length, 0);
});

test('nearby management and malformed media paths still require a complete account', async () => {
  const { storage, downloads, media } = runtime();
  storage.token = '';
  storage.userInfo = {};
  for (const url of [
    `https://api.jiadilingguang.com/api/v1/miniprogram/workbench/media/${publicMediaID}`,
    `https://api.jiadilingguang.com/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/${publicMediaID}/extra`,
  ]) {
    await assert.rejects(() => media.resolveProtectedMediaURL(url), error => error.statusCode === 401);
  }
  assert.equal(downloads.length, 0);
});

test('missing or incomplete profile cannot resolve media', async () => {
  const missing = runtime();
  missing.storage.token = '';
  await assert.rejects(() => missing.media.resolveProtectedMediaURL(internalURL), error => error.statusCode === 401);
  assert.equal(missing.downloads.length, 0);

  const incomplete = runtime();
  incomplete.storage.userInfo.avatar = '';
  await assert.rejects(() => incomplete.media.resolveProtectedMediaURL(internalURL), error => error.statusCode === 428);
  assert.equal(incomplete.downloads.length, 0);
});

test('a late 401 from the previous account is discarded without clearing or caching for the new account', async () => {
  const { storage, downloads, navigations, media } = runtime();
  const oldAccount = media.resolveProtectedMediaURL(internalURL);
  storage.token = 'token-b';
  storage.userInfo = { id: 8, nickname: '业主', avatar: 'https://cdn.example/owner.png', token: 'token-b' };
  downloads[0].success({ statusCode: 401 });
  await assert.rejects(() => oldAccount, error => error.code === 'MEDIA_IDENTITY_CHANGED');
  assert.equal(storage.token, 'token-b');
  assert.equal(navigations.length, 0);

  const newAccount = media.resolveProtectedMediaURL(internalURL);
  assert.equal(downloads.length, 2);
  assert.equal(downloads[1].header.token, 'token-b');
  downloads[1].success({ statusCode: 200, tempFilePath: 'wxfile://tmp/new-account' });
  assert.equal(await newAccount, 'wxfile://tmp/new-account');
});

test('failed protected downloads are retryable with a readable error', async () => {
  const { downloads, media } = runtime();
  const failed = media.resolveProtectedMediaURL(internalURL);
  downloads[0].fail({ errMsg: 'downloadFile:fail timeout' });
  await assert.rejects(() => failed, /素材加载失败，请重试/);
  const retried = media.resolveProtectedMediaURL(internalURL);
  assert.equal(downloads.length, 2);
  downloads[1].success({ statusCode: 200, tempFilePath: 'wxfile://tmp/retry' });
  assert.equal(await retried, 'wxfile://tmp/retry');
});

test('profile rejection locks a protected page and preserves the session for completion', async () => {
  const { storage, downloads, navigations, media } = runtime();
  let blocked = 0;
  global.getCurrentPages = () => [{ route: 'pages/wallet/wallet', onProfileAccessBlocked: () => { blocked++; } }];
  const pending = media.resolveProtectedMediaURL(internalURL);
  downloads[0].success({ statusCode: 428 });
  await assert.rejects(() => pending, error => error.statusCode === 428);
  assert.equal(blocked, 1);
  assert.equal(storage.token, 'token-a');
  assert.equal(navigations[0], '/pages/login/login?from=profile-gate');
});
