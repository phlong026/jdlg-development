'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { execFileSync } = require('node:child_process');
const { buildContentPresentation } = require('../miniprogram/utils/content-presentation');
const root = path.resolve(__dirname, '../miniprogram');
const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';

test('generated page templates match their single editable source', () => {
  execFileSync(process.execPath, [path.resolve(__dirname, '../scripts/build-content-templates.js'), '--check']);
});

for (const kind of ['video', 'gallery', 'article']) {
  test(`isolated reader package renders ${kind} without a main-package template registry`, { skip: !fs.existsSync(compiler) }, () => {
    const file = './pages/contentreader/contentreader.wxml';
    const source = execFileSync(compiler, [file], { cwd: root, encoding: 'utf8', timeout: 10000 });
    const warnings = [];
    const runtime = { window: {}, console: { ...console, warn: (...args) => warnings.push(args.join(' ')) } };
    vm.createContext(runtime); vm.runInContext(source, runtime);
    const media = kind === 'article' ? [] : [{ media_id: 'media-one', kind: kind === 'video' ? 'video' : 'image', url: 'https://example.test/media', width: 1080, height: 1920 }];
    const content = { title: '分包内容标题', body: '分包中的完整正文', body_format: 'plain', source_url: 'https://example.test/source', media };
    const output = JSON.stringify(runtime.$gwx(file)({ __profileAccessMounted: true, contentReady: true, loading: false, content, presentationView: buildContentPresentation(content, media, {}), mediaErrors: {}, videoStarted: {}, guideVisible: true }));
    assert.match(output, /分包内容标题/);
    assert.match(output, /分包中的完整正文/);
    if (kind === 'video') assert.match(output, /"tag":"wx-video"/);
    if (kind === 'gallery') assert.match(output, /"tag":"wx-swiper"/);
    assert.deepEqual(warnings, [], 'missing template warnings must fail the release');
  });
}

test('editor preview also renders when compiled without the main-package template', { skip: !fs.existsSync(compiler) }, () => {
  const file = './pages/contentedit/contentedit.wxml';
  const source = execFileSync(compiler, [file], { cwd: root, encoding: 'utf8', timeout: 10000 });
  const runtime = { window: {}, console };
  vm.createContext(runtime); vm.runInContext(source, runtime);
  const output = JSON.stringify(runtime.$gwx(file)({ __profileAccessMounted: true, preview: true, presentationView: { kind: 'article', title: '编辑预览标题', blocks: [{ kind: 'paragraph', text: '编辑预览正文' }] } }));
  assert.match(output, /编辑预览标题/);
  assert.match(output, /编辑预览正文/);
});
