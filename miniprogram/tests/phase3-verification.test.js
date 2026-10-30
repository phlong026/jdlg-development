'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const vm = require('node:vm');
const { spawnSync } = require('node:child_process');
const script = path.resolve(__dirname, '../verification/phase3-frontend.cjs');

test('native console fixtures reject unknown requests and restore the original session', async () => {
  const output = fs.mkdtempSync(path.join(os.tmpdir(), 'phase3-console-'));
  try {
    const generated = spawnSync(process.execPath, [script, '--console-source'], { encoding: 'utf8', env: { ...process.env, PHASE3_UI_EVIDENCE_DIR: output } });
    assert.equal(generated.status, 0, generated.stderr);
    const app = {}, storage = { token: 'original-test-session' };
    const request = () => {}, clipboard = () => {};
    const wx = { request, getClipboardData: clipboard, getStorageSync: key => storage[key], setStorageSync: (key, value) => { storage[key] = value; }, removeStorageSync: key => { delete storage[key]; } };
    vm.runInNewContext(generated.stdout, { wx, getApp: () => app });
    assert.equal(storage.token, 'phase3-ui-fixture');
    let response;
    wx.request({ url: 'https://invalid.example/unexpected', success(value) { response = value; } });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(response.statusCode, 501);
    assert.equal(app.phase3UIUnknownRequests.length, 1);
    app.phase3UINativeRestore();
    assert.equal(wx.request, request);
    assert.equal(wx.getClipboardData, clipboard);
    assert.equal(storage.token, 'original-test-session');
    assert.equal(app.phase3UIFixture, undefined);
    assert.equal(app.phase3UINativeRestore, undefined);
  } finally { fs.rmSync(output, { recursive: true, force: true }); }
});

test('failed mock cleanup cannot produce passed native evidence', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'phase3-cleanup-'));
  try {
    const fakeCLI = path.join(directory, 'wechatide');
    fs.writeFileSync(fakeCLI, `#!${process.execPath}\n
      const args=process.argv.slice(2), tool=args[2], option=name=>args[args.indexOf('--'+name)+1];
      let result={success:true};
      if(tool==='automation_evaluate')result.result={result:option('fn-source').includes('typeof wx.getStorageSync')?'function':true};
      if(tool==='automation_page_action'){
        result.elements=[];
        if(option('action')==='getData')result.data={id:'fixture-content-01',preview:'fixture-preview',status:'published',title:'新中式庭院设计说明',importURL:'mp.weixin.qq.com/s/fixture'}[option('data-path')];
      }
      if(tool==='automation_wx_api'&&option('action')==='restore')process.stdout.write(JSON.stringify({ok:false,message:'simulated cleanup failure'}));
      else process.stdout.write(JSON.stringify({ok:true,result}));
    `);
    fs.chmodSync(fakeCLI, 0o755);
    const run = spawnSync(process.execPath, [script], { encoding: 'utf8', env: { ...process.env, WECHATIDE_CLI: fakeCLI, PHASE3_UI_EVIDENCE_DIR: directory } });
    assert.equal(run.status, 1);
    const evidence = JSON.parse(fs.readFileSync(path.join(directory, 'native-verification.json'), 'utf8'));
    assert.equal(evidence.steps.length, 4, run.stderr);
    assert.equal(evidence.status, 'incomplete');
    assert.equal(evidence.cleanupErrors.length, 2);
  } finally { fs.rmSync(directory, { recursive: true, force: true }); }
});
