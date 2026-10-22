const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');

function loadGuard(extension, { saveFails = false, uploadFails = false, deferUpload = false, deferSave = false } = {}) {
  let definition;
  const storage = { token: 'test-token', userInfo: { id: 101, username: 'wx_test' } };
  const requests = [], events = [], toasts = [], uploads = [];
  const source = fs.readFileSync(path.join(root, `miniprogram/components/nickname-guard/nickname-guard.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('parameter')
      ? { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) }
      : name.endsWith('deviceFingerprint')
        ? { getCachedDeviceFingerprint: () => 'test-device' }
        : { blockUnsafeText: () => false },
    Component: value => { definition = value; },
    wx: {
      getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; },
      removeStorageSync: key => { delete storage[key]; },
      request(options) {
        requests.push(options);
        if (deferSave && options.url.endsWith('/profile/nickname')) return;
        options.success({ statusCode: saveFails ? 500 : 200, data: saveFails ? { code: 500, msg: '保存失败' } : { code: 0 } });
      },
      uploadFile(options) {
        uploads.push(options);
        if (deferUpload) return;
        if (uploadFails) options.fail(new Error('upload offline'));
        else options.success({ statusCode: 200, data: JSON.stringify({ code: 0, data: { url: 'https://cdn.example/avatar.png' } }) });
      },
      showToast: options => toasts.push(options.title),
    },
    console: { error() {} },
  });
  const instance = {
    ...definition.methods,
    data: { ...definition.data, visible: true },
    setData(value) { Object.assign(this.data, value); },
    triggerEvent(name, detail) { events.push({ name, detail }); },
  };
  return { instance, storage, requests, events, toasts, uploads };
}

const formEvent = nickname => ({ detail: { value: { nickname } } });

for (const extension of ['ts', 'js']) {
  test(`${extension}: 微信点选昵称没有触发 input 时，提交表单仍保存昵称并完成登录引导`, async () => {
    const { instance, storage, requests, events } = loadGuard(extension);
    await instance.onConfirm(formEvent('  微信选中的昵称  '));
    assert.equal(requests.length, 1);
    assert.equal(requests[0].method, 'PUT');
    assert.ok(requests[0].url.endsWith('/profile/nickname'));
    assert.equal(requests[0].data.nickname, '微信选中的昵称');
    assert.equal(storage.userInfo.nickname, '微信选中的昵称');
    assert.equal(storage.userInfo.id, 101);
    assert.equal(instance.data.visible, false);
    assert.equal(events[0]?.name, 'completed');
    assert.equal(events[0]?.detail.nickname, '微信选中的昵称');
  });

  test(`${extension}: 提交值覆盖旧 input 状态，微信清空昵称时不会保存旧值`, async () => {
    const changed = loadGuard(extension);
    changed.instance.data.nickname = '旧输入';
    await changed.instance.onConfirm(formEvent('新选择'));
    assert.equal(changed.requests[0]?.data.nickname, '新选择');

    const cleared = loadGuard(extension);
    cleared.instance.data.nickname = '未通过检测的旧输入';
    await cleared.instance.onConfirm(formEvent(''));
    assert.equal(cleared.requests.length, 0);
    assert.equal(cleared.instance.data.visible, true);
    assert.equal(cleared.storage.userInfo.nickname, undefined);
  });

  test(`${extension}: 头像上传并保存完成后，可以继续保存昵称`, async () => {
    const { instance, requests, storage } = loadGuard(extension);
    instance.data.avatarTemp = 'wxfile://chosen-avatar';
    instance.data.nickname = '头像用户';
    await instance.uploadAvatar('wxfile://chosen-avatar');
    assert.equal(instance.data.avatarUrl, 'https://cdn.example/avatar.png');
    assert.equal(instance.data.avatarTemp, '');
    assert.equal(instance.data.uploading, false);
    await instance.onConfirm(formEvent('头像用户'));
    assert.equal(requests.filter(request => request.url.endsWith('/profile/nickname')).length, 1);
    assert.equal(storage.userInfo.nickname, '头像用户');
  });

  test(`${extension}: 选填头像上传失败不会永久阻止昵称保存`, async () => {
    const { instance, storage } = loadGuard(extension, { uploadFails: true });
    instance.data.avatarTemp = 'wxfile://failed-avatar';
    instance.data.nickname = '只保存昵称';
    await instance.uploadAvatar('wxfile://failed-avatar');
    await instance.onConfirm(formEvent('只保存昵称'));
    assert.equal(storage.userInfo.nickname, '只保存昵称');
    assert.equal(instance.data.visible, false);
  });

  test(`${extension}: 上传尚未完成时，不提前提交昵称`, async () => {
    const { instance, requests, events } = loadGuard(extension);
    instance.data.uploading = true;
    instance.data.nickname = '等待上传';
    await instance.onConfirm(formEvent('等待上传'));
    assert.equal(requests.length, 0);
    assert.equal(events.length, 0);
    assert.equal(instance.data.visible, true);
  });

  test(`${extension}: 头像上传期间重复选择不会启动并发上传或提前放行昵称`, async () => {
    const { instance, requests, uploads } = loadGuard(extension, { deferUpload: true });
    instance.onChooseAvatar({ detail: { avatarUrl: 'wxfile://first-avatar' } });
    instance.onChooseAvatar({ detail: { avatarUrl: 'wxfile://second-avatar' } });
    assert.equal(uploads.length, 1);
    assert.equal(instance.data.avatarTemp, 'wxfile://first-avatar');
    await instance.onConfirm(formEvent('等待头像'));
    assert.equal(requests.length, 0);
    uploads[0].success({ statusCode: 200, data: JSON.stringify({ code: 0, data: { url: 'https://cdn.example/avatar.png' } }) });
    await new Promise(setImmediate);
    assert.equal(instance.data.uploading, false);
    await instance.onConfirm(formEvent('等待头像'));
    assert.equal(requests.filter(request => request.url.endsWith('/profile/nickname')).length, 1);
  });

  test(`${extension}: 保存接口失败不更新昵称缓存、不放行登录引导`, async () => {
    const { instance, storage, requests, events } = loadGuard(extension, { saveFails: true });
    instance.data.nickname = '待保存';
    await instance.onConfirm(formEvent('待保存'));
    assert.equal(requests.length, 1);
    assert.equal(storage.userInfo.nickname, undefined);
    assert.equal(instance.data.visible, true);
    assert.equal(instance.data.saving, false);
    assert.equal(events.length, 0);
  });

  test(`${extension}: 昵称保存期间选择头像不会启动上传或在离开页面后写资料`, async () => {
    const { instance, requests, uploads, events } = loadGuard(extension, { deferSave: true });
    const saving = instance.onConfirm(formEvent('保存中的昵称'));
    assert.equal(instance.data.saving, true);
    instance.onChooseAvatar({ detail: { avatarUrl: 'wxfile://late-avatar' } });
    assert.equal(uploads.length, 0);
    assert.equal(instance.data.avatarTemp, '');
    assert.equal(requests.length, 1);
    requests[0].success({ statusCode: 200, data: { code: 0 } });
    await saving;
    assert.equal(instance.data.visible, false);
    assert.equal(events.length, 1);
  });
}

test('昵称通过命名表单字段提交，完成按钮触发表单而不是读取旧输入缓存', () => {
  const wxml = fs.readFileSync(path.join(root, 'miniprogram/components/nickname-guard/nickname-guard.wxml'), 'utf8');
  assert.match(wxml, /<form\b[^>]*bindsubmit="onConfirm"/);
  assert.match(wxml, /<input\b[^>]*name="nickname"[^>]*type="nickname"/);
  assert.match(wxml, /<button\b[^>]*form-type="submit"/);
  assert.match(wxml, /<button\b[^>]*open-type="chooseAvatar"[^>]*disabled="{{uploading \|\| saving}}"/);
});
