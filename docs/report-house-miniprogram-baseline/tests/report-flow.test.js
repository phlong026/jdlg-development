const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('../../AItestPlatform/node_modules/typescript');

function flow() {
  const file = path.join(__dirname, '../miniprogram/pages/report/flow.ts');
  const exports = {};
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText, { exports });
  return exports;
}

test('five steps collect decisions, not fabricated site facts', () => {
  const f = flow();
  const input = f.emptyInput();
  assert.ok(f.validateStep(input, 0));
  input.site = { region: '福建省 泉州市', width_m: 12, depth_m: 10, road_side: '待确认' };
  assert.equal(f.validateStep(input, 0), '');
  input.site.width_m = Infinity;
  assert.ok(f.validateStep(input, 0));
  input.preference.priorities = ['老人起居', '老人起居', '预算控制'];
  assert.ok(f.validateStep(input, 3));
});

test('a phone reading has four ordered sections, not fourteen squeezed PDF sheets', () => {
  const f = flow();
  const snapshot = { facts: { region: '泉州', family: '三代同堂' }, pages: [
    { number: 1, kind: 'cover', title: '家的方向' },
    { number: 2, kind: 'evidence', title: '判断依据' },
    { number: 3, kind: 'profile', title: '家庭画像' },
    { number: 4, kind: 'priorities', title: '先解决什么' },
  ] };
  assert.equal(f.previewSections(snapshot).length, 4);
  assert.throws(() => f.previewSections({ ...snapshot, pages: snapshot.pages.slice(0, 3) }));
  assert.throws(() => f.previewSections({ ...snapshot, pages: [...snapshot.pages].reverse() }));
});

test('payment callback never grants PDF access: server paid, bound and ready all required', () => {
  const f = flow();
  assert.equal(f.canOpenFull({ paid: false, bound: true, status: 'ready' }), false);
  assert.equal(f.canOpenFull({ paid: true, bound: false, status: 'ready' }), false);
  assert.equal(f.canOpenFull({ paid: true, bound: true, status: 'full_generating' }), false);
  assert.equal(f.canOpenFull({ paid: true, bound: true, status: 'ready' }), true);
  assert.equal(f.shouldPoll('preview_ready'), false);
  assert.equal(f.shouldPoll('failed'), false);
  assert.equal(f.shouldPoll('full_queued'), true);
});
