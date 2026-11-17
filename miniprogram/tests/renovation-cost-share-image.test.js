'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

test('renovation covers render only renovation total, three fees, region and area', async () => {
  const texts = [], sizes = [], writes = [];
  const ctx = {
    beginPath() {}, moveTo() {}, lineTo() {}, arcTo() {}, closePath() {}, fill() {}, fillRect() {},
    fillText(text) { texts.push(String(text)); }, measureText(text) { return { width: String(text).length * 40 }; },
  };
  const wx = {
    env: { USER_DATA_PATH: '/tmp/user' },
    createOffscreenCanvas(options) {
      sizes.push(options);
      return { getContext: () => ctx, toDataURL: () => 'data:image/jpeg;base64,Y292ZXI=' };
    },
    base64ToArrayBuffer: value => value,
    getFileSystemManager: () => ({
      mkdir: options => options.success(),
      writeFile: options => { writes.push(options.filePath); options.success(); },
    }),
  };
  const module = { exports: {} };
  const source = fs.readFileSync(path.join(__dirname, '../miniprogram/utils/renovationCostShareImage.js'), 'utf8');
  vm.runInNewContext(source, { module, exports: module.exports, wx, Promise, console, encodeURIComponent });
  const card = { city: '泉州', area: '90㎡', totalCost: '9.04万元', materialCost: '4.22万元', auxiliaryCost: '1.09万元', laborCost: '2.63万元' };
  const chat = await module.exports.prepareRenovationShareImage(card);
  const timeline = await module.exports.prepareRenovationTimelineImage(card);
  assert.match(chat, /renovation-share/);
  assert.match(timeline, /renovation-share/);
  assert.deepEqual(JSON.parse(JSON.stringify(sizes)), [{ type: '2d', width: 1000, height: 800 }, { type: '2d', width: 1000, height: 1000 }]);
  assert.ok(texts.includes('泉州 · 装修面积90㎡'));
  assert.ok(texts.includes('9.04'));
  assert.ok(texts.includes('4.22万元') && texts.includes('1.09万元') && texts.includes('2.63万元'));
  assert.doesNotMatch(texts.join('|'), /建房|建筑面积|结构形式|门窗/);
  assert.equal(writes.length, 2);
  assert.match(source, /#f4eee5|#fffaf3/);
  assert.match(source, /#8c6944|#a65c25/);
  assert.doesNotMatch(source, /#1f8f5f|#f1f7f0|#dff4e8/);
});
