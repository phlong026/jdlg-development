'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');

for (const extension of ['ts', 'js']) {
  const file = path.join(__dirname, '../miniprogram/components/nickname-guard/profile-validation.' + extension);
  const source = fs.readFileSync(file, 'utf8');
  const code = extension === 'ts' ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText : source;
  const context = { exports: {} }; vm.runInNewContext(code, context);
  const { hasCompleteProfile } = context.exports;
  test(`${extension}: only persisted public HTTPS domain avatars complete the profile`, () => {
    for (const avatar of ['https://cdn.example.com/avatar.jpg', 'https://thirdwx.qlogo.cn/mmopen/example/132', 'https://bucket.cos.ap-guangzhou.myqcloud.com/a.jpg?sign=fixture']) {
      assert.equal(hasCompleteProfile({ nickname: '访客', avatar }), true, avatar);
    }
  });
  test(`${extension}: avatar sources hidden by admin never count as complete`, () => {
    for (const avatar of ['', '/assets/images/home.jpg', 'wxfile://tmp', 'http://cdn.example.com/a.jpg',
      'https://127.0.0.1/a.jpg', 'https://10.1.2.3/a.jpg', 'https://localhost/a.jpg', 'https://host.local/a.jpg',
      'https://user:password@example.com/a.jpg', 'https://example.com/a.jpg#fragment', 'https://0x7f.0.0.1/a.jpg',
      'https://0177.1/a.jpg', 'https://[::1]/a.jpg', 'https://example.com/%broken',
      'https://cdn.example.com/assets/images/default-avatar.png']) {
      assert.equal(hasCompleteProfile({ nickname: '访客', avatar }), false, avatar);
    }
  });
}
