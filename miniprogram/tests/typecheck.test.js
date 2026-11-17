'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const script = path.resolve(__dirname, '../scripts/typecheck.js');
const wechatOptions = {
  lib: ['ES2020'],
  typeRoots: [path.resolve(__dirname, '../typings')],
  types: ['types', 'project_compat'],
  skipLibCheck: true,
};

function checkFixture(t, source, compilerOptions = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'jdlg-typecheck-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  fs.writeFileSync(path.join(directory, 'fixture.ts'), source);
  const config = path.join(directory, 'tsconfig.json');
  fs.writeFileSync(config, JSON.stringify({ compilerOptions: { strict: true, ...compilerOptions }, files: ['fixture.ts'] }));
  return { directory, result: spawnSync(process.execPath, [script, config], { encoding: 'utf8' }) };
}

test('typecheck rejects semantic errors that single-file transpilation accepts', t => {
  const { result } = checkFixture(t, "const amount: number = 'invalid';");
  assert.equal(result.status, 1);
  assert.match(result.stderr, /TS2322/);
  assert.match(result.stderr, /fixture\.ts/);
});

test('typecheck retains strict parameter checks', t => {
  const { result } = checkFixture(t, 'function price(quantity) { return quantity * 2; }');
  assert.equal(result.status, 1);
  assert.match(result.stderr, /TS7006/);
});

test('typecheck accepts valid types without emitting JavaScript', t => {
  const { directory, result } = checkFixture(t, 'const amount: number = 12;', { noEmit: false });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(fs.existsSync(path.join(directory, 'fixture.js')), false);
});

test('WeChat system APIs expose their concrete return types', t => {
  const { result } = checkFixture(t, `
    const windowWidth: number = wx.getWindowInfo().windowWidth;
    const sdkVersion: string = wx.getAppBaseInfo().SDKVersion;
    const model: string = wx.getDeviceInfo().model;
  `, wechatOptions);
  assert.equal(result.status, 0, result.stderr);
});

test('WeChat API declarations reject incorrect arguments and return-value types', t => {
  const { result } = checkFixture(t, `
    const windowWidth: string = wx.getWindowInfo().windowWidth;
    const sdkVersion: number = wx.getAppBaseInfo().SDKVersion;
    const model: number = wx.getDeviceInfo().model;
    wx.getDeviceInfo(123);
  `, wechatOptions);
  assert.equal(result.status, 1);
  assert.equal((result.stderr.match(/TS2322/g) || []).length, 3, result.stderr);
  assert.match(result.stderr, /TS2554/);
  assert.doesNotMatch(result.stderr, /TS2339|TS2551/);
});
