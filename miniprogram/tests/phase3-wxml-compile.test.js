'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
const available = fs.existsSync(compiler);
const root = path.resolve(__dirname, '../miniprogram');
function compile(file) {
  const result = spawnSync(compiler, [file], { cwd: root, encoding: 'utf8', timeout: 10000, maxBuffer: 4 * 1024 * 1024 });
  if (result.error) throw result.error;
  return result;
}

test('native WXML compiler reproduces the escaped-operator runtime error', { skip: !available }, () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'phase3-wxml-'));
  try {
    const file = path.join(directory, 'conditional.wxml');
    fs.writeFileSync(file, '<view wx:if="{{ready &amp;&amp; enabled}}">内容</view>');
    assert.notEqual(compile(file).status, 0);
    fs.writeFileSync(file, '<view wx:if="{{ready && enabled}}">内容</view>');
    assert.equal(compile(file).status, 0);
  } finally { fs.rmSync(directory, { recursive: true, force: true }); }
});

for (const page of ['my', 'chat', 'contentworkbench', 'contentedit', 'contentreader', 'contentwriting', 'sharevisits', 'visitdetail', 'messagecenter', 'projectnotes', 'projectnoteview']) {
  test('native WXML compiler accepts ' + page, { skip: !available }, () => {
    const result = compile(`./pages/${page}/${page}.wxml`);
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });
}
