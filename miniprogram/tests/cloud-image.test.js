const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const helperPath = path.join(__dirname, '../miniprogram/utils/cloudImage.ts');

function loadHelper({ firstStatus = 200, holdDownload = false, failDownload = false } = {}) {
  let exists = false;
  let status = firstStatus;
  let releaseDownload = () => {};
  const downloads = [];
  const warnings = [];
  const source = ts.transpileModule(fs.readFileSync(helperPath, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  const context = {
    exports: {},
    console: { warn: (...args) => warnings.push(args) },
    wx: {
      env: { USER_DATA_PATH: 'wxfile://user-data' },
      getFileSystemManager() {
        return {
          access({ success, fail }) { exists ? success() : fail({ errMsg: 'not found' }); },
          mkdir({ success }) { success(); },
          unlink({ success }) { exists = false; success(); },
        };
      },
      downloadFile(options) {
        downloads.push(options);
        exists = true;
        const finish = () => {
          if (failDownload) {
            options.fail({ errMsg: 'network fail' });
            failDownload = false;
            return;
          }
          if (status === 200) exists = true;
          options.success({ statusCode: status, filePath: options.filePath, tempFilePath: options.filePath });
        };
        if (holdDownload) releaseDownload = finish;
        else queueMicrotask(finish);
        return { abort() {} };
      },
    },
  };
  vm.runInNewContext(source, context);
  return {
    prepareCloudImage: context.exports.prepareCloudImage,
    downloads,
    warnings,
    setStatus(value) { status = value; },
    releaseDownload() { releaseDownload(); },
  };
}

test('同一云图并发下载只发起一次请求，后续复用已存在的本地文件', async () => {
  const helper = loadHelper();
  const url = 'https://static.example.com/releases/v1/assets/share/home.jpg';
  const [first, second] = await Promise.all([
    helper.prepareCloudImage(url, 'home.jpg'),
    helper.prepareCloudImage(url, 'home.jpg'),
  ]);

  assert.equal(first, second);
  assert.match(first, /^wxfile:\/\/user-data\/cloud-images\/[a-f0-9]{8}-home\.jpg$/);
  assert.equal(helper.downloads.length, 1);
  assert.equal(helper.downloads[0].url, url);
  assert.equal(helper.downloads[0].filePath, first);
  assert.equal(helper.downloads[0].timeout, 10000);

  assert.equal(await helper.prepareCloudImage(url, 'home.jpg'), first);
  assert.equal(helper.downloads.length, 1, '持久缓存存在时不重复下载');
});

test('非 200 下载不作为缓存，返回空路径并允许下一次重试', async () => {
  const helper = loadHelper({ firstStatus: 403 });
  const url = 'https://static.example.com/releases/v1/assets/share/home.jpg';
  assert.equal(await helper.prepareCloudImage(url, 'home.jpg'), '');
  assert.equal(helper.warnings.length, 1);

  helper.setStatus(200);
  assert.match(await helper.prepareCloudImage(url, 'home.jpg'), /^wxfile:\/\//);
  assert.equal(helper.downloads.length, 2, '失败结果不能永久缓存');
});

test('下载中的半成品文件不会绕过并发去重', async () => {
  const helper = loadHelper({ holdDownload: true });
  const url = 'https://static.example.com/releases/v1/assets/share/home.jpg';
  let secondResolved = false;
  const first = helper.prepareCloudImage(url, 'home.jpg');
  await new Promise(resolve => setImmediate(resolve));
  const second = helper.prepareCloudImage(url, 'home.jpg').then(value => {
    secondResolved = true;
    return value;
  });
  await new Promise(resolve => setImmediate(resolve));

  assert.equal(secondResolved, false, '并发调用必须等待同一个完整下载');
  assert.equal(helper.downloads.length, 1);
  helper.releaseDownload();
  assert.equal(await first, await second);
});

test('网络失败留下的半成品会被删除，下一次调用重新下载', async () => {
  const helper = loadHelper({ failDownload: true });
  const url = 'https://static.example.com/releases/v1/assets/share/home.jpg';
  assert.equal(await helper.prepareCloudImage(url, 'home.jpg'), '');
  assert.equal(helper.warnings.length, 1);

  assert.match(await helper.prepareCloudImage(url, 'home.jpg'), /^wxfile:\/\//);
  assert.equal(helper.downloads.length, 2);
});
