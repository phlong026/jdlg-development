'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.join(__dirname, '../miniprogram/pages/my');
const read = name => fs.readFileSync(path.join(root, name), 'utf8');

test('我的页按目标图组织账户、工作台、资料和服务', () => {
  const wxml = read('my.wxml');
  const orderedSections = ['我的账户', '{{workbenchSectionTitle}}', '我的资料', '账号与服务'];
  let previous = -1;
  for (const label of orderedSections) {
    const position = wxml.indexOf(label);
    assert.ok(position > previous, `${label} 应按目标顺序出现`);
    previous = position;
  }
  assert.match(wxml, /class="workbench-grid workbench-grid-/);
  assert.match(wxml, /class="account-card"/);
  assert.match(wxml, /class="section-grid section-grid-two"/);
  assert.doesNotMatch(wxml, /待处理反馈|今日回访/);
});

test('工作台入口由后端创作能力控制，普通功能移出设计师分区', () => {
  const source = read('my.ts');
  for (const action of ['myProjects', 'contentWorkbench', 'designerHome']) {
    assert.match(source, new RegExp(`action: '${action}'`));
  }
  for (const action of ['projectNotes', 'aiWriting', 'assetLibrary', 'shareMaterials', 'publish']) {
    assert.doesNotMatch(source.match(/function buildWorkbenchMenuItems[\s\S]+?return items;/)[0], new RegExp(`action: '${action}'`));
  }
  for (const file of ['my.ts', 'my.js']) {
    const built = read(file);
    assert.match(built, /action: 'myProjects'[^\n]+available: true/);
    assert.match(built, /hasContentWorkbenchAccess/);
    assert.match(built, /can_manage === true/);
    assert.match(built, /buildCommonMenuItems[\s\S]+action: 'project'/);
    assert.match(built, /buildServiceMenuItems[\s\S]+action: 'customerManagement'/);
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

test('我的页使用本地 mini-icon 线性图标并保持暖白背景', () => {
  const config = JSON.parse(read('my.json'));
  const wxml = read('my.wxml');
  const scss = read('my.scss');
  const source = read('my.ts');
  // 2026-11-04 主包瘦身：t-icon 换成本地 mini-icon（mask SVG + currentColor），TDesign icon+common 退出主包闭包
  assert.equal(config.usingComponents['mini-icon'], '/components/mini-icon/mini-icon');
  assert.ok(!config.usingComponents['t-icon'], 't-icon 应已从我的页移除');
  const iconStyles = fs.readFileSync(path.join(root, '../../components/mini-icon/mini-icon.wxss'), 'utf8');
  for (const icon of ['folder', 'video', 'usergroup', 'file', 'share', 'user-business', 'star', 'verified', 'mobile', 'notification', 'setting', 'user', 'order-list', 'logout']) {
    assert.ok(source.includes(`icon: '${icon}'`) || wxml.includes(`name="${icon}"`), `应使用已校验图标 ${icon}`);
    assert.ok(iconStyles.includes(`.mi-${icon} `) || new RegExp(`\\.mi-${icon}\\b`).test(iconStyles), `mini-icon 库应包含 ${icon}`);
  }
  assert.doesNotMatch(wxml, /shortLabel|[□▤◇▯♧⚙✎♙➤⌯▧◎⇥]/);
  assert.doesNotMatch(wxml, /page-background/);
  assert.match(scss, /background:\s*#fbfaf7/);
});

test('项目笔记并入我的项目且保留登录门禁', () => {
  const vm = require('node:vm');
  let page;
  let storedToken = '';
  const navigations = [], toasts = [];
  vm.runInNewContext(read('my.js'), {
    exports: {},
    require: () => ({ resolveAssetPath: value => value }),
    Page: definition => { page = definition; },
    wx: { getStorageSync: () => storedToken, navigateTo: options => navigations.push(options.url), showToast: options => toasts.push(options.title) },
  });
  const event = { currentTarget: { dataset: { action: 'myProjects' } } };
  page.data = { ...page.data, token: '', hasLoginToken: false };
  page.onWorkbenchMenuTap(event);
  assert.equal(navigations.length, 0);
  assert.equal(toasts[0], '请先登录');
  storedToken = 'fixture-session';
  page.data.token = 'fixture-session';
  page.data.hasContentWorkbenchAccess = true;
  page.onWorkbenchMenuTap(event);
  assert.deepEqual(navigations, ['/pages/projectnotes/projectnotes']);
  const source = read('my.ts');
  assert.match(source, /action: 'myProjects'[^\n]+available: true/);
  assert.match(source, /\/pages\/projectnotes\/projectnotes/);
  assert.doesNotMatch(source.match(/function buildWorkbenchMenuItems[\s\S]+?return items;/)[0], /action: 'projectNotes'/);
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
  page.refreshWorkbenchMenuItems({ hasLoginToken: true, hasContentWorkbenchAccess: true });
  for (const action of ['myProjects', 'contentWorkbench']) {
    page.onWorkbenchMenuTap({ currentTarget: { dataset: { action } } });
  }
  page.onAccountMenuTap({ currentTarget: { dataset: { action: 'customerManagement' } } });
  assert.deepEqual(navigations, [
    '/pages/projectnotes/projectnotes',
    '/pages/contentworkbench/contentworkbench',
    '/pages/adminhome/adminhome?target=customer-management',
  ]);
  assert.equal(navigations.some(url => /token|session|user_id/i.test(url)), false);
});
