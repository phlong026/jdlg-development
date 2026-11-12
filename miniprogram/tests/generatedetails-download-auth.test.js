const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');
const root = path.join(__dirname, '../miniprogram');
const origin = 'https://api.jiadilingguang.com';

function harness(extension, { denied = false, albumDenied = false } = {}) {
  const storage = { token: 'session-fixture', device_fingerprint: 'device-fixture' };
  const downloads = [], saved = [], requests = [], toasts = [], modals = [];
  let definition;
  const auth = {
    generateRequestParams(token, body, apiPath, device) {
      return { token, 'token-signature': 'signature', sin: JSON.stringify(body), 'md5-signature': apiPath, pass: device, tm: 'timestamp' };
    },
    paramsToHeaders: value => value,
  };
  const wx = {
    getStorageSync: key => storage[key],
    showLoading() {}, hideLoading() {},
    showToast: value => toasts.push(value.title),
    showModal: value => modals.push(value),
    request(options) {
      requests.push(options);
      options.success({ statusCode: 200, data: { code: 0, data: { url: 'https://example.invalid/original.png' } } });
      return {};
    },
    downloadFile(options) {
      const header = options.header || {};
      const valid = ['token', 'token-signature', 'sin', 'md5-signature', 'pass', 'tm'].every(key => header[key])
        && header['md5-signature'] === options.url.slice(origin.length)
        && header.sin === '{}' && header.pass === storage.device_fingerprint;
      downloads.push({ options, statusCode: valid && !denied ? 200 : 401 });
      options.success({ statusCode: downloads.at(-1).statusCode, tempFilePath: '/tmp/generated-media.mp4' });
      return {};
    },
    getFileSystemManager: () => ({ readFile: options => options.success({ data: '{"code":401,"msg":"参数不合法"}' }) }),
    saveImageToPhotosAlbum(options) {
      if (albumDenied) options.fail({ errMsg: 'saveImageToPhotosAlbum:fail auth deny' });
      else { saved.push({ type: 'image', path: options.filePath }); options.success(); }
    },
    saveVideoToPhotosAlbum(options) { saved.push({ type: 'video', path: options.filePath }); options.success(); },
  };
  const guardModule = { exports: {} };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/profile-request-guard.js'), 'utf8'), {
    module: guardModule, wx, require: () => auth, getCurrentPages: () => [],
  });
  guardModule.exports.install();
  let source = fs.readFileSync(path.join(root, `pages/generatedetails/generatedetails.${extension}`), 'utf8');
  if (extension === 'ts') source = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
  vm.runInNewContext(source, {
    exports: {}, wx, console: { error() {} },
    require: name => name.endsWith('/parameter') ? auth : { resolveAssetPath: value => value },
    Page: value => { definition = value; },
  });
  const page = {
    ...definition,
    data: { taskData: { task_no: 'task +/中文', result: { url: 'https://example.invalid/video.mp4' } } },
    setData(patch) { Object.assign(this.data, patch); },
  };
  return { page, storage, downloads, saved, requests, toasts, modals };
}

const settle = () => new Promise(resolve => setImmediate(resolve));

for (const extension of ['ts', 'js']) {
  for (const index of [0, 1]) {
    test(`${extension}: image ${index} downloads through full authentication and saves the selected image`, async () => {
      const h = harness(extension);
      await h.page.performAuthorizedImageDownload(index);
      await settle();
      assert.equal(h.downloads.length, 1);
      assert.equal(h.downloads[0].statusCode, 200);
      assert.equal(h.downloads[0].options.url, `${origin}/api/v1/miniprogram/user/ai/task/download-original-file?task_no=task%20%2B%2F%E4%B8%AD%E6%96%87&image_index=${index}`);
      assert.equal(h.requests[0].data.image_index, index);
      assert.equal(h.saved[0].type, 'image');
      assert.equal(h.storage.token, 'session-fixture');
    });
  }
  test(`${extension}: video file download carries its own complete signature`, async () => {
    const h = harness(extension);
    h.page.performAuthorizedVideoDownload();
    await settle();
    assert.equal(h.downloads.length, 1);
    assert.equal(h.downloads[0].statusCode, 200);
    assert.match(h.downloads[0].options.url, /video\/download-original-file\?task_no=/);
    assert.equal(h.saved[0].type, 'video');
  });
  test(`${extension}: rejected file download never reaches the album`, async () => {
    const h = harness(extension, { denied: true });
    await h.page.performAuthorizedImageDownload();
    await settle();
    assert.equal(h.saved.length, 0);
    assert.ok(h.toasts.includes('参数不合法'));
  });
  test(`${extension}: missing login cannot request or save a file`, async () => {
    const h = harness(extension);
    delete h.storage.token;
    await h.page.performAuthorizedImageDownload();
    assert.equal(h.downloads.length, 0);
    assert.equal(h.saved.length, 0);
    assert.ok(h.toasts.includes('请先登录'));
  });
  test(`${extension}: album denial still offers permission settings after a valid download`, async () => {
    const h = harness(extension, { albumDenied: true });
    await h.page.performAuthorizedImageDownload();
    await settle();
    assert.equal(h.downloads[0].statusCode, 200);
    assert.equal(h.saved.length, 0);
    assert.equal(h.modals[0].confirmText, '去设置');
  });
}
