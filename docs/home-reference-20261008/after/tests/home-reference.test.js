const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/index/index.js'), 'utf8');
let config, route, toast, api;
vm.runInNewContext(source, {
  exports: {},
  require: name => name.endsWith('costRegions') ? require('../miniprogram/utils/costRegions.js') : ({ resolveAssetPath: p => p }),
  Page: value => { config = value; },
  wx: { navigateTo: options => { api = 'navigateTo'; route = options.url; }, switchTab: options => { api = 'switchTab'; route = options.url; }, showToast: options => { toast = options.title; } },
  console,
});
function page() {
  route = toast = api = undefined;
  return { ...config, data: { ...config.data }, setData(values) { Object.assign(this.data, values); } };
}
const blank = page();
blank.onQuickEstimate();
assert.equal(route, undefined);
assert.equal(toast, '请选择所在地区');
const selected = page();
selected.onQuickRegionChange({ detail: { value: [0, 0] } });
assert.equal(selected.data.quickRegionText, '福建 / 泉州');
selected.onQuickEstimate();
assert.equal(route, undefined);
assert.equal(toast, '请输入有效的建筑面积');
for (const value of ['0', '-1', 'Infinity', 'abc']) {
  selected.onQuickAreaInput({ detail: { value } });
  selected.onQuickEstimate();
  assert.equal(route, undefined);
}
selected.onQuickAreaInput({ detail: { value: '320' } });
selected.onQuickEstimate();
assert.equal(route, '/pages/aicost/aicost?source=home_quick_estimate&projectType=rural_villa&province=%E7%A6%8F%E5%BB%BA&city=%E6%B3%89%E5%B7%9E&buildingArea=320');
selected.onQuickProjectChange({ currentTarget: { dataset: { type: 'urban_renewal' } } });
assert.equal(selected.data.quickProjectType, 'urban_renewal');
selected.onQuickEstimate();
assert.match(route, /projectType=urban_renewal/);
selected.onQuickProjectChange({ currentTarget: { dataset: { type: 'unknown' } } });
assert.equal(selected.data.quickProjectType, 'urban_renewal');
selected.onOpenPage({ currentTarget: { dataset: { url: '/pages/template/template' } } });
assert.equal(api, 'switchTab');
selected.onOpenPage({ currentTarget: { dataset: { url: '/pages/aitools/aitools' } } });
assert.equal(api, 'navigateTo');
const markup = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/index/index.wxml'), 'utf8');
for (const name of ['建房顾问', '造价测算', 'AI工具', '别墅设计', '装修设计', '庭院设计', '旧居翻新', '模板广场', '在线客服', '拍照上传', 'AI生图工具']) {
  assert.match(markup, new RegExp(`aria-label="${name}"`));
}
assert.match(markup, /bindchange="onQuickRegionChange"/);
assert.match(markup, /bindinput="onQuickAreaInput"/);
assert.match(markup, /bindtap="onQuickEstimate"/);
assert.doesNotMatch(markup, /home-art\.png|project-rural\.png|project-urban\.png/);
for (const name of ['advisor', 'calculator', 'ai-tools', 'villa', 'interior', 'courtyard', 'renovation', 'templates']) {
  assert.match(markup, new RegExp(`/assets/home-reference/hd/${name}\\.png`));
}
assert.match(markup, /placeholder="请输入"/);
assert.match(markup, /quickRegionText \|\| '请选择'/);
assert.match(markup, /<button class="hotspot quick-estimate"[^>]*bindtap="onQuickEstimate"/);
console.log('Home reference navigation, project selection, region, validation and estimate handoff passed');
