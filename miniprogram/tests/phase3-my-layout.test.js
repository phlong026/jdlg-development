'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.join(__dirname, '../miniprogram/pages/my');
const read = name => fs.readFileSync(path.join(root, name), 'utf8');

test('我的页按目标图组织账户、工作台、资料和服务', () => {
  const wxml = read('my.wxml');
  const orderedSections = ['我的账户', '设计师工作台', '我的资料', '账号与服务'];
  let previous = -1;
  for (const label of orderedSections) {
    const position = wxml.indexOf(label);
    assert.ok(position > previous, `${label} 应按目标顺序出现`);
    previous = position;
  }
  assert.match(wxml, /class="workbench-grid"/);
  assert.match(wxml, /class="account-card"/);
  assert.match(wxml, /class="section-grid section-grid-two"/);
  assert.doesNotMatch(wxml, /待处理反馈|今日回访/);
});

test('工作台保留八个目标入口并复用已有准入边界', () => {
  for (const file of ['my.ts', 'my.js']) {
    const source = read(file);
    for (const action of ['myProjects', 'projectNotes', 'contentWorkbench', 'customerManagement', 'project', 'publish', 'shareMaterials', 'designerHome']) {
      assert.match(source, new RegExp(`action: '${action}'`));
    }
    assert.match(source, /action: 'myProjects'[^\n]+available: true/);
    assert.match(source, /action: 'contentWorkbench'[^\n]+available: hasLoginToken/);
    assert.match(source, /action: 'customerManagement'[^\n]+available: hasLoginToken/);
  }
});

test('三期真实入口继续使用登录门禁和现有页面', () => {
  const source = read('my.ts');
  assert.match(source, /isLoggedIn\(\)/);
  assert.match(source, /\/pages\/contentworkbench\/contentworkbench/);
  assert.match(source, /\/pages\/sharevisits\/sharevisits/);
  assert.match(source, /\/pages\/messagecenter\/messagecenter/);
  assert.match(source, /\/pages\/Identityauthen\/Identityauthen/);
});

test('我的页使用已有 TDesign 细线图标并保持暖白背景', () => {
  const config = JSON.parse(read('my.json'));
  const wxml = read('my.wxml');
  const scss = read('my.scss');
  const source = read('my.ts');
  const iconStyles = fs.readFileSync(path.join(root, '../../components/tdesign-miniprogram/icon/icon.wxss'), 'utf8');
  assert.equal(config.usingComponents['t-icon'], '/components/tdesign-miniprogram/icon/icon');
  for (const icon of ['folder', 'file-edit', 'video', 'usergroup', 'file', 'send', 'share', 'user-business', 'star', 'verified', 'mobile', 'notification', 'setting', 'user', 'order-list', 'logout']) {
    assert.ok(source.includes(`icon: '${icon}'`) || wxml.includes(`name="${icon}"`), `应使用已校验图标 ${icon}`);
    assert.ok(iconStyles.includes(`.t-icon-${icon}:before`), `TDesign 本地字体应包含 ${icon}`);
  }
  assert.doesNotMatch(wxml, /shortLabel|[□▤◇▯♧⚙✎♙➤⌯▧◎⇥]/);
  assert.doesNotMatch(wxml, /page-background/);
  assert.match(scss, /background:\s*#fbfaf7/);
});

test('项目笔记已有页面时我的入口可用且保留登录门禁', () => {
  const vm = require('node:vm');
  let page;
  const navigations = [], toasts = [];
  vm.runInNewContext(read('my.js'), {
    exports: {},
    require: () => ({ resolveAssetPath: value => value }),
    Page: definition => { page = definition; },
    wx: { getStorageSync: () => '', navigateTo: options => navigations.push(options.url), showToast: options => toasts.push(options.title) },
  });
  const event = { currentTarget: { dataset: { action: 'projectNotes' } } };
  page.data = { ...page.data, token: '', hasLoginToken: false };
  page.onWorkbenchMenuTap(event);
  assert.equal(navigations.length, 0);
  assert.equal(toasts[0], '请先登录');
  page.data.token = 'fixture-session';
  page.onWorkbenchMenuTap(event);
  assert.deepEqual(navigations, ['/pages/projectnotes/projectnotes']);
  for (const file of ['my.ts', 'my.js']) {
    const source = read(file);
    assert.match(source, /action: 'projectNotes'[^\n]+available: true/);
    assert.match(source, /\/pages\/projectnotes\/projectnotes/);
  }
});

test('我的项目复用私人项目页，客户管理只打开既有后台深链', () => {
  const vm = require('node:vm');
  let page;
  const navigations = [];
  vm.runInNewContext(read('my.js'), {
    exports: {},
    require: () => ({ resolveAssetPath: value => value }),
    Page: definition => { page = definition; },
    wx: { getStorageSync: () => 'mini-session', navigateTo: options => navigations.push(options.url), showToast() {} },
    setTimeout,
  });
  page.data = { ...page.data, token: 'mini-session', hasLoginToken: true };
  page.setData = patch => Object.assign(page.data, patch);
  page.refreshWorkbenchMenuItems({ hasLoginToken: true });
  for (const action of ['myProjects', 'customerManagement', 'contentWorkbench']) {
    page.onWorkbenchMenuTap({ currentTarget: { dataset: { action } } });
  }
  assert.deepEqual(navigations, [
    '/pages/projectnotes/projectnotes',
    '/pages/adminhome/adminhome?target=customer-management',
    '/pages/contentworkbench/contentworkbench',
  ]);
  assert.equal(navigations.some(url => /token|session|user_id/i.test(url)), false);
});
