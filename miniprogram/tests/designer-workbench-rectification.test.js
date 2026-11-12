const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const pageDir = path.join(__dirname, '..', 'miniprogram', 'pages', 'designerhome');
const read = (name) => fs.readFileSync(path.join(pageDir, name), 'utf8');

test('设计师主页提供明确编辑与预览入口并保留服务编辑入口', () => {
  const wxml = read('designerhome.wxml');
  assert.match(wxml, /编辑主页/);
  assert.match(wxml, /预览主页/);
  assert.match(wxml, /bindtap="onEditDesignerService"/);
  assert.match(wxml, />服务</);
  assert.doesNotMatch(wxml, /designer-avatar-manage-hint/);
  assert.doesNotMatch(wxml, /self-manage-card/);
  assert.doesNotMatch(wxml, /工作服务/);
});

test('设计师主页单列作品仍使用可见分页集合，预览模式只切换展示态', () => {
  const wxml = read('designerhome.wxml');
  const ts = read('designerhome.ts');
  const js = read('designerhome.js');
  assert.match(wxml, /designer-works-single/);
  assert.match(wxml, /wx:for="\{\{visibleWorks\}\}"/);
  assert.match(ts, /isPreviewMode/);
  assert.match(js, /isPreviewMode/);
  assert.match(ts, /visibleWorks/);
  assert.match(js, /visibleWorks/);
  assert.match(ts, /preview=1/);
  assert.match(js, /preview=1/);
});

test('我的发布与作品编辑保留审核、展示范围和原有详情分流', () => {
  const release = fs.readFileSync(path.join(__dirname, '..', 'miniprogram', 'pages', 'release', 'release.wxml'), 'utf8');
  const publish = fs.readFileSync(path.join(__dirname, '..', 'miniprogram', 'pages', 'release', 'designerworkpublish.wxml'), 'utf8');
  const releaseTs = fs.readFileSync(path.join(__dirname, '..', 'miniprogram', 'pages', 'release', 'release.ts'), 'utf8');
  assert.match(release, /我的发布/);
  assert.match(release, /提交审核|审核通过/);
  assert.match(publish, /仅主页展示/);
  assert.match(publish, /主页 \+ 模板广场/);
  assert.match(publish, /审核通过后在主页展示/);
  assert.match(publish, /提交审核/);
  assert.match(releaseTs, /pages\/templatesquaredetails\/templatesquaredetails\?id=/);
  assert.match(releaseTs, /wx\.previewImage/);
  assert.match(releaseTs, /pages\/release\/designerworkpublish\?id=.*mode=edit/);
});

test('D04工具页保留工作台衔接、返回语义与必要底部操作', () => {
  const chat = read('../chat/chat.wxml');
  const ai = read('../aigenerate/aigenerate.wxml');
  const history = read('../generatehistory/generatehistory.wxml');
  const customer = read('../customerservice/customerservice.wxml');
  assert.match(chat, /采用后返回内容策划页/);
  assert.match(chat, /quick-actions/);
  assert.match(ai, /图像生成/);
  assert.match(ai, /视频生成/);
  assert.match(history, /去生成/);
  assert.match(customer, /一键生成专属方案/);
  assert.match(customer, /对接设计老师/);
});
