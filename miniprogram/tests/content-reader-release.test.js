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
const editorHint = '链接导入的可编辑草稿，请在发布前核对正文、排版和媒体。';

test('reader release explicitly includes its shared template outside the profile gate', () => {
  const source = fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.wxml'), 'utf8');
  assert.match(source.split('\n')[0], /^<import src=/);
  for (const file of ['../project.config.json', 'project.config.json']) {
    const config = JSON.parse(fs.readFileSync(path.join(root, file)));
    assert.ok(config.packOptions.include.some(rule => rule.type === 'file' && rule.value === 'components/content-presentation/content-presentation.wxml'));
  }
});

test('published rendering hides the import editor hint but preserves actual source attribution', () => {
  const content = { title: '乡墅方案', body_format: 'plain', body: '完整方案正文', source_note: editorHint };
  assert.equal(buildContentPresentation(content, [], {}).sourceNote, '');
  assert.equal(buildContentPresentation({ ...content, source_note: '摄影：设计团队' }, [], {}).sourceNote, '摄影：设计团队');
  assert.equal(content.source_note, editorHint, 'published record is never rewritten');
});

test('native shared reader displays title and body without extra analytics authorization or editor guidance', { skip: !fs.existsSync(compiler) }, () => {
  const file = './pages/contentreader/contentreader.wxml';
  const source = execFileSync(compiler, [file, './components/content-presentation/content-presentation.wxml'], { cwd: root, encoding: 'utf8', timeout: 10000 });
  const runtime = { window: {}, console }; vm.createContext(runtime); vm.runInContext(source, runtime);
  const render = runtime.$gwx(file);
  const content = { title: '乡墅方案标题', body_format: 'plain', body: '房屋方案完整正文', source_note: editorHint, source_url: 'https://example.test/original' };
  const data = { __profileAccessMounted: true, loading: false, contentReady: true, content, presentationView: buildContentPresentation(content, [], {}), consentAvailable: true, consentChoicePending: true, consentNotice: '访问分析说明', guideVisible: true };
  const output = JSON.stringify(render(data));
  assert.match(output, /乡墅方案标题/); assert.match(output, /房屋方案完整正文/);
  assert.doesNotMatch(output, /访问分析说明|是否开启访问分析|同意开启|保持关闭/);
  assert.doesNotMatch(output, /链接导入的可编辑草稿|请在发布前核对/);
  assert.match(output, /复制原文/);
  const gated = JSON.stringify(render({ ...data, __profileAccessMounted: false }));
  assert.doesNotMatch(gated, /乡墅方案标题|房屋方案完整正文/);
  const invalid = JSON.stringify(render({ ...data, contentReady: false, error: '内容数据不完整，请重试。' }));
  assert.doesNotMatch(invalid, /房屋方案完整正文|reader-guide|reader-actions/);
});
