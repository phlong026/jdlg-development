const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const helperPath = path.join(__dirname, '../miniprogram/utils/costEstimateShareImage.ts');

function loadShareImageHelper() {
  const writes = [];
  const calls = [];
  const imageSources = [];
  const textCalls = [];
  const imageCalls = [];
  const clips = [];
  const strokes = [];
  const gradients = [];
  const sizes = [];
  const scales = [];
  const context = {
    exports: {},
    console: { warn() {} },
    wx: {
      env: { USER_DATA_PATH: 'wxfile://user-data' },
      base64ToArrayBuffer(value) { return value; },
      getImageInfo({ success }) { success({ path: 'wxfile://house.jpg' }); },
      getFileSystemManager() {
        return {
          mkdir({ success }) { success(); },
          writeFile({ filePath, data, success }) {
            writes.push({ filePath, data });
            success();
          },
        };
      },
      createOffscreenCanvas(options) {
        sizes.push(options);
        const ctx = {
          createLinearGradient(...coordinates) {
            const gradient = { coordinates, stops: [], addColorStop(offset, color) { this.stops.push({ offset, color }); } };
            gradients.push(gradient);
            return gradient;
          },
          scale(x, y) { scales.push([x, y]); },
          font: '',
          measureText(value) { return { width: value.length * Number(this.font.match(/(\d+)px/)[1]) * 0.6 }; },
          beginPath() {}, moveTo() {}, lineTo() {}, arcTo() {}, closePath() {},
          fill() {}, stroke() { strokes.push({ color: this.strokeStyle, width: this.lineWidth }); }, save() {}, restore() {}, clip() { clips.push(true); }, fillRect() {},
          drawImage(...args) { imageCalls.push(args); },
          fillText(text, x, y) { textCalls.push({ text, x, y, font: this.font, color: this.fillStyle }); },
        };
        return {
          getContext() { return ctx; },
          createImage() {
            const image = { width: 1280, height: 1280, onload: null, onerror: null };
            Object.defineProperty(image, 'src', { set(value) { imageSources.push(value); image.onload(); } });
            return image;
          },
          toDataURL(type, quality) {
            calls.push({ type, quality });
            return 'data:image/jpeg;base64,c2hhcmUtY2FyZA==';
          },
        };
      },
    },
  };
  const source = ts.transpileModule(fs.readFileSync(helperPath, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(source, context);
  return { prepare: context.exports.prepareCostEstimateShareImage, writes, calls, imageSources, textCalls, imageCalls, clips, strokes, gradients, sizes, scales };
}

test('share card exports a JPEG and returns the local image path to WeChat', async () => {
  const helper = loadShareImageHelper();
  const imageUrl = await helper.prepare({
    totalCost: '71.17万元',
    city: '泉州',
    buildingArea: '440㎡',
    structureType: '框架结构',
    windowStandard: '标准型',
  });

  assert.equal(helper.calls.length, 1);
  assert.deepEqual(helper.calls[0], { type: 'image/jpeg', quality: 0.98 });
  assert.equal(imageUrl.startsWith('wxfile://user-data/cost-estimate-share/'), true);
  assert.equal(helper.writes.length, 1);
  assert.equal(helper.writes[0].data, 'c2hhcmUtY2FyZA==');
  assert.equal(helper.imageSources[0], '/assets/images/cost-estimate-share-house.jpg', 'load from the package root before trying a page-relative path');
  const amount = helper.textCalls.find((item) => item.text === '71.17');
  const unit = helper.textCalls.find((item) => item.text === '万元');
  assert.ok(Number(amount.font.match(/(\d+)px/)[1]) >= 120);
  assert.equal(unit.x > amount.x + '71.17'.length * Number(amount.font.match(/(\d+)px/)[1]) * 0.6, true);
  assert.equal(helper.textCalls.some((item) => item.text === '建房造价测算'), true);
  const button = helper.textCalls.find((item) => item.text === '点击查看测算报告');
  assert.ok(Number(button.font.match(/(\d+)px/)[1]) >= 42);
  assert.equal(helper.clips.length, 1, 'clip the scene to its single gold frame');
  assert.equal(helper.imageCalls[0].length, 5, 'draw the entire villa rather than cropping its source');
  assert.equal(helper.imageCalls[0][3], helper.imageCalls[0][4]);
});

test('price and unit stay separated within the text column for different amount lengths', async () => {
  for (const amountText of ['9.90', '599.51', '1234.56']) {
    const helper = loadShareImageHelper();
    await helper.prepare({ totalCost: `${amountText}万元`, city: '泉州', buildingArea: '360㎡', structureType: '框架结构', windowStandard: '标准型' });
    const amount = helper.textCalls.find((item) => item.text === amountText);
    const unit = helper.textCalls.find((item) => item.text === '万元');
    const amountWidth = amountText.length * Number(amount.font.match(/(\d+)px/)[1]) * 0.6;
    const unitWidth = 2 * Number(unit.font.match(/(\d+)px/)[1]) * 0.6;
    assert.ok(unit.x >= amount.x + amountWidth + 12);
    assert.ok(unit.x + unitWidth <= 582, 'price must not run into the building');
  }
});

test('reference card keeps the villa sharp with only a left transition and white CTA', async () => {
  const helper = loadShareImageHelper();
  await helper.prepare({ totalCost: '59.51万元', city: '泉州', buildingArea: '360㎡', structureType: '框架结构', windowStandard: '标准型' });
  const [, , imageY, , imageHeight] = helper.imageCalls[0];
  assert.ok(imageY + imageHeight >= 632, 'photo reaches the frame bottom without a visible strip');
  assert.equal(helper.sizes[0].width, 2000);
  assert.equal(helper.sizes[0].height, 1600);
  assert.deepEqual(helper.scales, [[2, 2]]);
  assert.ok(helper.strokes.some(item => item.color === '#d8c29e'), 'retain the fine gold frame');
  const masks = helper.gradients.filter(item => item.stops.some(stop => stop.color.includes('rgba')));
  assert.equal(masks.length, 1, 'only the left seam is softened; roof, right facade and steps stay sharp');
  assert.ok(masks[0].coordinates[2] - masks[0].coordinates[0] <= 150, 'keep the transition away from most of the facade');
  assert.ok(helper.gradients.some(item => item.stops.some(stop => stop.color === '#a65c25')), 'match the reference warm gold CTA');
  const button = helper.textCalls.find(item => item.text === '点击查看测算报告');
  assert.equal(button.color, '#ffffff');
  const title = helper.textCalls.find(item => item.text === '建房造价测算');
  const parameter = helper.textCalls.find(item => item.text.includes('建筑面积'));
  assert.ok(Number(title.font.match(/(\d+)px/)[1]) <= 46);
  assert.ok(Number(parameter.font.match(/(\d+)px/)[1]) <= 36);
});

test('unestimated form card uses a question placeholder without claiming a price', async () => {
  const helper = loadShareImageHelper();
  await helper.prepare({ city: '泉州', buildingArea: '', structureType: '框架结构', windowStandard: '标准型' });
  const labels = helper.textCalls.map(item => item.text);
  assert.ok(labels.includes('建房要花'));
  assert.ok(labels.includes('？'));
  assert.ok(labels.includes('元'));
  assert.ok(!labels.includes('多少钱？'));
  assert.ok(labels.includes('开始测算'));
  assert.ok(labels.includes('填写建房参数'));
  assert.ok(labels.includes('查看参考造价'));
  assert.ok(!labels.some(label => label.includes('泉州') || label.includes('框架结构') || label.includes('门窗')));
  const headline = helper.textCalls.find(item => item.text === '建房要花');
  assert.equal(headline.color, '#704416');
  assert.ok(Number(headline.font.match(/(\d+)px/)[1]) <= 56);
  const question = helper.textCalls.find(item => item.text === '？');
  const yuan = helper.textCalls.find(item => item.text === '元');
  const questionWidth = Number(question.font.match(/(\d+)px/)[1]) * 0.6;
  assert.equal(question.color, '#e62c12');
  assert.equal(question.y, yuan.y);
  assert.ok(yuan.x >= question.x + questionWidth + 12);
  assert.ok(yuan.x + 2 * Number(yuan.font.match(/(\d+)px/)[1]) <= 396, 'invitation unit stays before the villa');
  const [, x, , width] = helper.imageCalls[0];
  assert.ok(width < 610);
  assert.ok(x + width <= 966, 'show the right facade inside the frame');
  assert.ok(!labels.includes('万元'));
  assert.ok(!labels.some(label => label.includes('建筑面积') || label.includes('--')));
});
