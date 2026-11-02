'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');
const completeProfile = { id: 9, nickname: '林间访客', avatar: 'https://cdn.example/avatar.jpg' };

function loadCombined({ profileResponses = [completeProfile], deferProfiles = false } = {}) {
  let componentDefinition;
  const storage = {};
  const requests = [];
  const navigations = [];
  const events = [];
  const editorShows = [];
  const pendingBusiness = [];
  const pendingProfiles = [];
  const editor = {
    data: { visible: false, promptToken: '' },
    setData(value) { Object.assign(this.data, value); },
    show(value) {
      editorShows.push(value);
      Object.assign(this.data, { visible: true, promptToken: storage.token });
    },
  };
  let profileIndex = 0;
  const nativeRequest = options => {
    requests.push(options);
    if (options.url.endsWith('/login')) {
      options.success({ statusCode: 200, data: { code: 0, data: { id: 9, token: 'session-9' } } });
      return { abort() {} };
    }
    if (options.url.endsWith('/profile')) {
      if (deferProfiles) {
        pendingProfiles.push(options);
        return { abort() {} };
      }
      const profile = profileResponses[Math.min(profileIndex++, profileResponses.length - 1)];
      if (profile && profile.statusCode) options.success(profile);
      else options.success({ statusCode: 200, data: { code: 0, data: profile } });
      return { abort() {} };
    }
    pendingBusiness.push(options);
    return { abort() {} };
  };
  const wx = {
    request: nativeRequest,
    getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
    login: options => options.success({ code: 'wx-code' }),
    showToast() {},
    navigateTo: options => { navigations.push(options.url); options.success?.(); },
  };
  const auth = {
    generateRequestParams: token => ({ token }),
    paramsToHeaders: value => ({
      token: value.token,
      'token-signature': 'fixture',
      sin: 'fixture',
      'md5-signature': 'fixture',
      pass: 'fixture',
      tm: 'fixture',
    }),
  };
  let component;
  const page = {
    route: 'pages/contentreader/contentreader',
    onProfileAccessBlocked() { events.push({ name: 'blocked' }); },
    selectComponent() { return component; },
  };
  const requestGuardModule = { exports: {} };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'miniprogram/utils/profile-request-guard.js'), 'utf8'), {
    module: requestGuardModule,
    exports: requestGuardModule.exports,
    wx,
    getCurrentPages: () => [page],
    require: () => auth,
    console,
    Promise,
  });
  requestGuardModule.exports.install();
  vm.runInNewContext(fs.readFileSync(path.join(root, 'miniprogram/components/login-guard/login-guard.js'), 'utf8'), {
    exports: {},
    wx,
    Component: value => { componentDefinition = value; },
    require: name => name.endsWith('profile-validation')
      ? require(path.join(root, 'miniprogram/components/nickname-guard/profile-validation.js'))
      : name.endsWith('deviceFingerprint')
        ? { generateDeviceFingerprint: async () => 'device', getCachedDeviceFingerprint: () => 'device', cacheDeviceFingerprint() {} }
        : auth,
    console: { error() {} },
    Promise,
  });
  component = {
    ...componentDefinition.methods,
    data: { ...componentDefinition.data },
    setData(value) { Object.assign(this.data, value); },
    selectComponent() { return editor; },
    triggerEvent(name, detail) { events.push({ name, detail }); },
  };
  return { component, editor, editorShows, events, navigations, pendingBusiness, pendingProfiles, requests, storage, wx };
}

async function login(fixture) {
  await fixture.component.check();
  fixture.component.onTogglePrivacy();
  await fixture.component.onWxLogin();
}

test('global request guard and inline login card grant a complete account without opening a second page', async () => {
  const fixture = loadCombined();
  await login(fixture);
  assert.equal(fixture.requests.filter(request => request.url.endsWith('/login')).length, 1);
  assert.equal(fixture.events.filter(event => event.name === 'authenticated').length, 1);
  assert.deepEqual(fixture.navigations, []);
});

test('profile 401 returns the same inline card to login instead of opening the standalone page', async () => {
  const fixture = loadCombined({ profileResponses: [{ statusCode: 401, data: { code: 401 } }] });
  await login(fixture);
  assert.equal(fixture.storage.token, undefined);
  assert.equal(fixture.component.data.visible, true);
  assert.equal(fixture.component.data.checking, false);
  assert.equal(fixture.component.data.needsLogin, true);
  assert.deepEqual(fixture.navigations, []);
});

test('business 428 during profile completion reuses the editor and never opens another login page', async () => {
  const incomplete = { ...completeProfile, avatar: '' };
  const fixture = loadCombined({ profileResponses: [incomplete, incomplete] });
  await login(fixture);
  assert.equal(fixture.editorShows.length, 1);
  fixture.wx.request({ url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects' });
  fixture.pendingBusiness[0].success({ statusCode: 428, data: { code: 428 } });
  await new Promise(setImmediate);
  assert.equal(fixture.storage.token, 'session-9');
  assert.equal(fixture.editorShows.length, 1);
  assert.equal(fixture.component.data.completingProfile, true);
  assert.deepEqual(fixture.navigations, []);
});

test('a late rejection from an old account cannot clear or gate the new account', async () => {
  const fixture = loadCombined();
  fixture.storage.token = 'old-session';
  fixture.wx.request({ url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects' });
  fixture.storage.token = 'new-session';
  fixture.storage.userInfo = { id: 10 };
  fixture.pendingBusiness[0].success({ statusCode: 401, data: { code: 401 } });
  assert.equal(fixture.storage.token, 'new-session');
  assert.deepEqual(fixture.navigations, []);
  assert.equal(fixture.events.filter(event => event.name === 'blocked').length, 0);
});

test('a same-session 401 takes over an in-flight 428 profile check and restores the login card', async () => {
  const fixture = loadCombined({ deferProfiles: true });
  fixture.storage.token = 'session-A';
  fixture.storage.userInfo = { id: 9 };
  fixture.wx.request({ url: 'https://api.jiadilingguang.com/api/v1/miniprogram/projects' });
  fixture.wx.request({ url: 'https://api.jiadilingguang.com/api/v1/miniprogram/workbench/context' });

  fixture.pendingBusiness[0].success({ statusCode: 428, data: { code: 428 } });
  assert.equal(fixture.component.data.checking, true);
  assert.equal(fixture.pendingProfiles.length, 1);

  fixture.pendingBusiness[1].success({ statusCode: 401, data: { code: 401 } });
  assert.equal(fixture.storage.token, undefined);
  assert.equal(fixture.pendingProfiles.length, 1, '401 must not start a profile request without a token');
  fixture.pendingProfiles[0].success({ statusCode: 200, data: { code: 0, data: completeProfile } });
  await new Promise(setImmediate);

  assert.equal(fixture.component.data.checking, false);
  assert.equal(fixture.component.data.visible, true);
  assert.equal(fixture.component.data.needsLogin, true);
  assert.equal(fixture.events.filter(event => event.name === 'authenticated').length, 0);
  assert.deepEqual(fixture.navigations, []);
});
