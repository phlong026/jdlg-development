const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

function harness() {
  const file = path.join(__dirname, '../miniprogram/components/report-document/report-document.ts');
  const source = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS },
  }).outputText;
  let definition;
  const downloads = [], opened = [], events = [];
  let aborted = 0;
  const wx = {
    downloadFile(options) { downloads.push(options); return { abort() { aborted++; } }; },
    openDocument(options) { opened.push(options); },
  };
  vm.runInNewContext(source, { Component(d) { definition = d; }, wx, exports: {} });
  const instance = {
    data: { ...definition.data },
    properties: { status: 'ready', mode: 'preview', documentUrl: 'https://example.com/report.pdf' },
    setData(patch) { Object.assign(this.data, patch); },
    triggerEvent(name, payload) { events.push({ name, payload }); },
    ...definition.methods,
  };
  definition.lifetimes.attached.call(instance);
  return { instance, definition, downloads, opened, events, aborted: () => aborted };
}

test('PDF stays a PDF: successful download opens the system document viewer once', () => {
  const h = harness();
  h.instance.openReport(); h.instance.openReport();
  assert.equal(h.downloads.length, 1);
  h.downloads[0].success({ statusCode: 200, tempFilePath: 'wxfile://tmp.pdf' });
  assert.equal(h.opened.length, 1);
  assert.equal(h.opened[0].fileType, 'pdf');
  assert.equal(h.opened[0].showMenu, true);
  h.opened[0].success();
  assert.equal(h.instance.data.busy, false);
});

test('expired links request refresh and never open an error body as PDF', () => {
  const h = harness();
  h.instance.openReport();
  h.downloads[0].success({ statusCode: 403, tempFilePath: 'error-body' });
  assert.equal(h.opened.length, 0);
  assert.ok(h.instance.data.error);
  h.instance.retry();
  assert.equal(h.events.at(-1).name, 'refresh');
});

test('unmount or changed document prevents a late download opening the wrong report', () => {
  for (const detach of [true, false]) {
    const h = harness(); h.instance.openReport();
    if (detach) h.definition.lifetimes.detached.call(h.instance);
    else {
      h.instance.properties.documentUrl = 'https://example.com/new.pdf';
      h.definition.observers['documentUrl,status'].call(h.instance);
    }
    h.downloads[0].success({ statusCode: 200, tempFilePath: 'old.pdf' });
    assert.equal(h.opened.length, 0);
    assert.equal(h.aborted(), 1);
  }
});

test('loading and non-HTTPS links cannot start a download', () => {
  const h = harness();
  h.instance.properties.status = 'loading'; h.instance.openReport();
  h.instance.properties.status = 'ready';
  h.instance.properties.documentUrl = 'http://example.com/file'; h.instance.openReport();
  assert.equal(h.downloads.length, 0);
});

test('download and viewer failures are visible without exposing the private URL', () => {
  const h = harness(); h.instance.openReport();
  h.downloads[0].fail({ errMsg: 'private URL must not be shown' });
  assert.equal(h.instance.data.busy, false);
  assert.ok(h.instance.data.error);
  h.instance.openReport();
  h.downloads[1].success({ statusCode: 200, tempFilePath: 'report.pdf' });
  h.opened[0].fail();
  assert.ok(h.instance.data.error);
  assert.equal(h.instance.data.busy, false);
});
