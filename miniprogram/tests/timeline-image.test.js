const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function harness(width = 1200, height = 600) {
  const sizes = [], writes = [], drawn = [], encodings = [], painted = [], fills = [];
  let failWrite = false;
  const ctx = new Proxy({}, { get(target, key) {
    if (key === 'createLinearGradient') return () => ({ addColorStop() {} });
    if (key === 'drawImage') return (...args) => drawn.push(args);
    if (key === 'fillRect') return (...args) => {
      fills.push({ style: target.fillStyle, beforeImage: drawn.length === 0 });
      painted.push([key, ...args]);
    };
    return target[key] || ((...args) => painted.push([key, ...args]));
  } });
  const sandbox = { exports: {}, require: () => ({ normalizeCosUrl: value => value }), console: { warn() {} }, wx: {
    env: { USER_DATA_PATH: 'wxfile://usr' }, base64ToArrayBuffer: value => value,
    getImageInfo({ success }) { success({ path: 'photo', width, height }); },
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
  return { api: sandbox.exports, sizes, writes, drawn, encodings, painted, fills, fail(value) { failWrite = value; } };
}

test('朋友圈方图与好友图分开缓存并用同步Canvas导出', async () => {
  const h = harness();
  assert.equal(typeof h.api.prepareTimelineShareImage, 'function');
  const timeline = await h.api.prepareTimelineShareImage('https://example.com/villa.jpg');
  const friend = await h.api.prepareShareCardImage('https://example.com/villa.jpg');
  assert.notEqual(timeline, friend);
  assert.deepEqual(h.sizes.map(({ width, height }) => [width, height]), [[1000, 1000], [1000, 800]]);
  assert.ok(h.encodings.every(value => value.type === 'image/jpeg'));
  assert.equal(await h.api.prepareTimelineShareImage('https://example.com/villa.jpg'), timeline);
  assert.equal(h.sizes.length, 2);
});

for (const [name, width, height] of [
  ['横屏视频封面', 1920, 1080],
  ['竖屏视频封面', 1080, 1920],
  ['方形作品图', 1000, 1000],
  ['全景作品图', 3000, 500],
]) {
  for (const [method, canvasHeight] of [['prepareShareCardImage', 800], ['prepareTimelineShareImage', 1000]]) {
    test(`${name} ${method} 等比居中铺满，不绘制装饰层或留边`, async () => {
      const h = harness(width, height);
      assert.ok(await h.api[method]('https://example.com/media-cover.jpg'));
      assert.equal(h.drawn.length, 1, '仅绘制作品或视频封面本身');
      assert.deepEqual(h.painted, [['fillRect', 0, 0, 1000, canvasHeight]], '仅预填画布，避免透明图片导出 JPEG 后变黑');
      assert.deepEqual(h.fills, [{ style: '#ffffff', beforeImage: true }], '白底在源图下方，不叠加装饰');
      const [, x, y, drawWidth, drawHeight] = h.drawn[0];
      assert.ok(x <= 0 && y <= 0);
      assert.ok(x + drawWidth >= 1000 && y + drawHeight >= canvasHeight, '画面覆盖四边');
      assert.ok(Math.abs(x + drawWidth / 2 - 500) <= 0.5, '水平居中');
      assert.ok(Math.abs(y + drawHeight / 2 - canvasHeight / 2) <= 0.5, '垂直居中');
      assert.ok(Math.abs(drawWidth / drawHeight - width / height) < 0.002, '保持原始画幅比例');
    });
  }
}

test('邀请海报可显式完整显示，二维码不被封面裁切，缓存不与作品图混用', async () => {
  const h = harness(750, 1000);
  const source = 'https://example.com/invite-poster.jpg';
  const poster = await h.api.prepareShareCardImage(source, 'contain');
  const cover = await h.api.prepareShareCardImage(source);
  assert.notEqual(poster, cover);
  assert.equal(h.drawn.length, 2);
  const [, x, y, width, height] = h.drawn[0];
  assert.ok(x >= 0 && y >= 0 && x + width <= 1000 && y + height <= 800);
  assert.equal(width / height, 0.75);
  assert.deepEqual(h.painted, Array(2).fill(['fillRect', 0, 0, 1000, 800]), '两种模式均只预填白底');
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
