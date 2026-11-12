'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

test('created item deletion signs the actual scoped URL and only sends the expected revision', async () => {
  const calls = [], module = {exports:{}};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../miniprogram/utils/phase3-api.js'),'utf8'), {
    module, exports:module.exports,
    wx:{getStorageSync:key=>key==='token'?'current-account':'',request:request=>{calls.push(request);request.success({statusCode:200,data:{code:0,data:{}}});}},
    require:name=>({generateRequestParams:(_token,_body,route)=>({route}),paramsToHeaders:value=>({'signed-path':value.route})}),
    Promise, JSON, Object, String, Number, Error, encodeURIComponent,
  });
  await module.exports.deleteCreatedContent('content-001',4);
  await module.exports.deleteProjectNoteProject('project-001',3);
  await module.exports.deleteContentDraft('draft-001',2);
  assert.deepEqual(calls.map(x=>[x.method,x.header['signed-path'],JSON.stringify(x.data)]),[
    ['POST','/api/v1/miniprogram/workbench/contents/content-001/delete','{"expected_revision":4}'],
    ['DELETE','/api/v1/miniprogram/project-notes/projects/project-001','{"expected_revision":3}'],
    ['DELETE','/api/v1/miniprogram/workbench/contents/draft-001','{"expected_revision":2}'],
  ]);
});
