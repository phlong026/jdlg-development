'use strict';
// 评审 P0-2/P1-1 教训：改了 .ts 忘编译 .js 导致线上功能整体缺失。
// 本测试锁定"剥壳后逐字节一致"的页面集合；新增同构页面时把路径加进 PAIRS。
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.join(__dirname, '../miniprogram');

// strip：去 @ts-nocheck 首行与尾部 export {};
const stripTS = source => source.replace('// @ts-nocheck\n', '').replace(/\nexport \{\};\n?$/, '\n');

const PAIRS = [
  'utils/phase3-api',
  'pages/contentedit/contentedit',
  'pages/contentwriting/contentwriting',
  'pages/contentworkbench/contentworkbench',
  'pages/sharevisits/sharevisits',
  'pages/projectcollections/projectcollections',
  'pages/messagecenter/messagecenter',
];

test('strip-normalized ts sources stay in lockstep with committed js artifacts', () => {
  for (const pair of PAIRS) {
    const ts = stripTS(fs.readFileSync(path.join(root, `${pair}.ts`), 'utf8'));
    const js = fs.readFileSync(path.join(root, `${pair}.js`), 'utf8');
    assert.equal(ts.trim(), js.trim(), `${pair}.js 与 .ts 不同步——改完 ts 必须重建 js 产物`);
  }
});

test('tsc-compiled generatedetails artifact carries the new save-as-content entry', () => {
  // generatedetails 是 tsc 编译产物，无法逐字节对比；用关键符号存在性做下限校验。
  const js = fs.readFileSync(path.join(root, 'pages/generatedetails/generatedetails.js'), 'utf8');
  assert.match(js, /saveAsContent/, 'saveAsContent 必须存在于编译产物');
  assert.match(js, /workbench\/contents\/from-generation/, '直连管线端点必须存在于编译产物');
});
