'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');

function pageDefinition(name, wx) {
  const file = require.resolve('../miniprogram/pages/aivideostudio/' + name);
  const original = global.Page;
  let definition;
  global.Page = value => { definition = value; };
  global.wx = wx;
  delete require.cache[file];
  try { require(file); } finally { global.Page = original; delete require.cache[file]; }
  return definition;
}

function mount(name, wx, patch) {
  const definition = pageDefinition(name, wx);
  const page = Object.assign({}, definition);
  page.data = JSON.parse(JSON.stringify(Object.assign({}, definition.data, patch || {})));
  page.setData = value => Object.assign(page.data, value);
  return page;
}

test.afterEach(() => { delete global.wx; });

test('tutorial covers three writing formulas with a copyable courtyard example', () => {
  const { lessons, getLesson } = require('../miniprogram/utils/ai-video-tutorial');
  assert.deepEqual(lessons.map(item => item.id), ['text', 'image', 'reference']);
  for (const lesson of lessons) {
    assert.ok(lesson.summary.length > 0);
    assert.ok(lesson.formula.includes('＋'));
    assert.equal(lesson.tips.length, 2);
    assert.ok(lesson.example.prompt.length > 50 && lesson.example.prompt.length < 7000);
    assert.equal(lesson.example.title, '庭院漫游');
  }
  assert.equal(getLesson('first_frame').id, 'image');
  assert.equal(getLesson('first_last_frame').id, 'image');
  assert.equal(getLesson('unknown').id, 'text');
  const reference = getLesson('reference');
  for (const label of ['图片1', '视频1', '音频1']) assert.ok(reference.example.prompt.includes(label));
  assert.match(reference.example.note, /编号|顺序/);
});

test('tutorial opens at the requested mode and switches examples without a server request', () => {
  const wx = { getStorageSync: () => 'account-a', request() { assert.fail('tutorial must remain local'); } };
  const page = mount('tutorial', wx);
  page.onLoad({ mode: 'first_last_frame' });
  assert.equal(page.data.lesson.id, 'image');
  page.selectMode({ currentTarget: { dataset: { mode: 'reference' } } });
  assert.equal(page.data.lesson.id, 'reference');
  assert.match(page.data.lesson.example.prompt, /图片1/);
  page.selectMode({ currentTarget: { dataset: { mode: 'text' } } });
  assert.equal(page.data.lesson.id, 'text');
});

test('copy writes only the selected case and reports a recoverable clipboard error', () => {
  const copies = [];
  const wx = { getStorageSync: () => 'account-a', setClipboardData: options => copies.push(options) };
  const page = mount('tutorial', wx);
  page.onLoad({ mode: 'reference' });
  page.copyExample();
  assert.equal(copies[0].data, page.data.lesson.example.prompt);
  copies[0].fail({ errMsg: 'clipboard unavailable' });
  assert.match(page.data.error, /复制失败/);
  page.copyExample();
  assert.equal(page.data.error, '');
  assert.equal(copies[1].data, copies[0].data);
});

test('late clipboard callbacks cannot change another account or a reloaded page', () => {
  let token = 'account-a';
  const copies = [];
  const wx = { getStorageSync: () => token, setClipboardData: options => copies.push(options) };
  const page = mount('tutorial', wx);
  page.onLoad({ mode: 'text' });
  page.copyExample();
  page.onUnload();
  token = 'account-b';
  page.onLoad({ mode: 'image' });
  copies[0].fail({ errMsg: 'old callback' });
  assert.equal(page.data.error, '');
  page.copyExample();
  page.onUnload();
  copies[1].fail({ errMsg: 'closed page' });
  assert.equal(page.data.error, '');
});

test('switching lessons or retrying copy invalidates an earlier clipboard callback', () => {
  const copies = [];
  const wx = { getStorageSync: () => 'account-a', setClipboardData: options => copies.push(options) };
  const page = mount('tutorial', wx);
  page.onLoad({ mode: 'text' });
  page.copyExample();
  page.selectMode({ currentTarget: { dataset: { mode: 'image' } } });
  copies[0].fail({ errMsg: 'old lesson' });
  assert.equal(page.data.error, '');
  page.copyExample();
  page.copyExample();
  copies[1].fail({ errMsg: 'old attempt' });
  assert.equal(page.data.error, '');
});

test('writing and saved-prompt pages open the contextual tutorial without changing their inputs', () => {
  const navigations = [];
  const wx = { getStorageSync: () => 'account-a', navigateTo: options => navigations.push(options) };
  const generator = mount('generate', wx, { mode: 'first_frame', prompt: '我的原稿' });
  generator.openTutorial();
  assert.equal(navigations[0].url, '/pages/aivideostudio/tutorial?mode=first_frame');
  assert.equal(generator.data.prompt, '我的原稿');
  const prompts = mount('prompts', wx, { mode: 'reference', prompt: '另一个原稿' });
  prompts.openTutorial();
  assert.equal(navigations[1].url, '/pages/aivideostudio/tutorial?mode=reference');
  assert.equal(prompts.data.prompt, '另一个原稿');
});

test('tutorial is registered as a private workspace route excluded from tracking', () => {
  const fs = require('node:fs');
  const root = path.resolve(__dirname, '..');
  const app = JSON.parse(fs.readFileSync(path.join(root, 'miniprogram/app.json'), 'utf8'));
  const studio = app.subPackages.find(item => item.root === 'pages/aivideostudio');
  assert.ok(studio.pages.includes('tutorial'));
  const policy = require('../miniprogram/utils/tracking/routes').policy('pages/aivideostudio/tutorial');
  assert.equal(policy.collect, false);
  assert.equal(policy.exclusionReason, 'private-workbench');
});
