'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const utilsRoot = path.join(__dirname, '../miniprogram/utils');
const sourcePath = path.join(utilsRoot, 'parameter.ts');
const runtimePath = path.join(utilsRoot, 'parameter.js');

function compileSource() {
  return ts.transpileModule(fs.readFileSync(sourcePath, 'utf8'), {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
    },
    fileName: sourcePath,
    reportDiagnostics: true,
  });
}

function loadSourceSigner(outputText) {
  const exports = {};
  vm.runInNewContext(outputText, {
    exports,
    require(id) { return require(path.join(utilsRoot, id)); },
    Date,
    JSON,
    Error,
  });
  return exports;
}

test('authentication signer runtime is the deterministic build of parameter.ts', () => {
  const result = compileSource();
  assert.deepEqual(result.diagnostics, []);
  assert.equal(fs.readFileSync(runtimePath, 'utf8'), result.outputText);
  assert.doesNotMatch(result.outputText, /REPLACE_WITH_LOCAL_(?:HMACKEY|SM4KEY)/);
});

test('authentication signer runtime and TypeScript source sign requests identically', () => {
  const result = compileSource();
  assert.deepEqual(result.diagnostics, []);
  const sourceSigner = loadSourceSigner(result.outputText);
  delete require.cache[require.resolve(runtimePath)];
  const runtimeSigner = require(runtimePath);
  const requests = [
    [{}, '/api/v1/miniprogram/profile'],
    [{}, '/api/v1/miniprogram/templates?page=2&keyword=%E4%B9%A1%E5%A2%85'],
    ['{"identity":"designer","source":"test"}', '/api/v1/miniprogram/profile/identity'],
  ];
  const originalNow = Date.now;
  Date.now = () => 1793491200000;
  try {
    for (const [body, requestPath] of requests) {
      assert.deepEqual(
        runtimeSigner.generateRequestParams('test-token', body, requestPath, 'test-device'),
        JSON.parse(JSON.stringify(sourceSigner.generateRequestParams('test-token', body, requestPath, 'test-device'))),
      );
    }
  } finally {
    Date.now = originalNow;
  }
});
