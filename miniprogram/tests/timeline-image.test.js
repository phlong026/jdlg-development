const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function harness() {
  const sizes = [], writes = [], drawn = [], encodings = [];
  let failWrite = false;
  const ctx = new Proxy({}, { get(target, key) {
    if (key === 'createLinearGradient') return () => ({ addColorStop() {} });
    if (key === 'drawImage') return (...args) => drawn.push(args);
    return target[key] || (() => {});
  } });
  const sandbox = { exports: {}, require: () => ({ normalizeCosUrl: value => value }), console: { warn() {} }, wx: {
    env: { USER_DATA_PATH: 'wxfile://usr' }, base64ToArrayBuffer: value => value,
    getImageInfo({ success }) { success({ path: 'photo', width: 1200, height: 600 }); },
    getFileSystemManager: () => ({ mkdir({ success }) { success(); }, writeFile(options) {
      writes.push(options.filePath); if (failWrite) options.fail(new Error('disk')); else options.success();
    } }),
    createOffscreenCanvas(options) {
      sizes.push(options);
      return { getContext: () => ctx, createImage() {
        const image = {};
        Object.defineProperty(image, 'src', { set() { image.onload(); } });
        return image;
      }, toDataURL(type, quality) { encodings.push({ type, quality }); return 'data:image/jpeg;base64,aW1n'; } };
    },
  } };
  vm.runInNewContext(fs.readFileSync(require.resolve('../miniprogram/utils/shareImage.js'), 'utf8'), sandbox);
  return { api: sandbox.exports, sizes, writes, drawn, encodings, fail(value) { failWrite = value; } };
}

test('朋友圈方图与好友图分开缓存并用同步Canvas导出，不裁掉模板原图', async () => {
  const h = harness();
  assert.equal(typeof h.api.prepareTimelineShareImage, 'function');
  const timeline = await h.api.prepareTimelineShareImage('https://example.com/villa.jpg');
  const friend = await h.api.prepareShareCardImage('https://example.com/villa.jpg');
  assert.notEqual(timeline, friend);
  assert.deepEqual(h.sizes.map(({ width, height }) => [width, height]), [[1000, 1000], [1000, 800]]);
  assert.ok(h.encodings.every(value => value.type === 'image/jpeg'));
  const foreground = h.drawn[1];
  assert.equal(foreground[3] / foreground[4], 2, '前景完整包含原图');
  assert.equal(await h.api.prepareTimelineShareImage('https://example.com/villa.jpg'), timeline);
  assert.equal(h.sizes.length, 2);
});

test('封面写入失败可重试，不缓存空文件或复用好友图', async () => {
  const h = harness();
  assert.equal(typeof h.api.prepareTimelineShareImage, 'function');
  h.fail(true);
  assert.equal(await h.api.prepareTimelineShareImage('source'), '');
  h.fail(false);
  assert.ok(await h.api.prepareTimelineShareImage('source'));
  assert.equal(h.sizes.length, 2);
});
