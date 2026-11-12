const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const { compileReader } = require('../scripts/compile-content-reader');

test('reader build preserves runtime behavior while compiling typed entry parameters', () => {
  let page;
  const js = compileReader("Page({ onLoad(options: Record<string, string>) { return options.content_id; } });", 'reader-fixture.ts');
  vm.runInNewContext(js, { Page(value) { page = value; } });
  assert.equal(page.onLoad({ content_id: 'fixed-version-note' }), 'fixed-version-note');
});

test('reader build rejects malformed TypeScript instead of producing an upload artifact', () => {
  assert.throws(() => compileReader('const broken: = ;', 'reader-fixture.ts'), /TypeScript/);
});
