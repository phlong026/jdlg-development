const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');
const read = relativePath => fs.readFileSync(path.join(root, relativePath), 'utf8');

const markup = read('miniprogram/pages/index/index.wxml');
const styles = read('miniprogram/pages/index/index.scss');
const pageSource = read('miniprogram/pages/index/index.ts');
const tabBarSource = read('miniprogram/custom-tab-bar/index.ts');
const assetsSource = read('miniprogram/utils/homeAssets.ts');
const { HOME_ASSETS } = require('../miniprogram/utils/homeAssets.js');
const releaseBase = 'https://static.jiadilingguang.com/assets/home-releases/20261010-b52afc7c12b5/';

assert.match(assetsSource, /export const HOME_ASSETS/);
assert.equal((assetsSource.match(new RegExp(releaseBase.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'g')) || []).length, 29);
assert.equal(Object.keys(HOME_ASSETS).length, 29);
assert.equal(Object.isFrozen(HOME_ASSETS), true);
for (const url of Object.values(HOME_ASSETS)) {
  assert.match(url, new RegExp(`^${releaseBase.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`));
}
for (const property of ['brandScene', 'brandSymbol', 'brandName', 'brandEnglish', 'customerService', 'homeTabbar', 'myTabbar']) {
  assert.match(assetsSource, new RegExp(`\\b${property}:\\s*resolveAssetPath\\(`));
}

assert.match(pageSource, /import \{ HOME_ASSETS \} from '\.\.\/\.\.\/utils\/homeAssets'/);
assert.match(pageSource, /homeAssets:\s*HOME_ASSETS/);
assert.match(tabBarSource, /import \{ HOME_ASSETS \} from '\.\.\/utils\/homeAssets'/);
assert.match(markup, /class="bg-image" src="\{\{heroBgImage\}\}"/);
assert.match(pageSource, /heroBgImage: resolveAssetPath\('\/assets\/home\/logo\+背景图\.png'\)/);
assert.doesNotMatch(markup, /src="\/assets\/(?:home-reference|smart-service-icon)/);

for (const property of ['customerService', 'costStats', 'ruralVilla', 'urbanBuilding', 'location', 'ruler', 'estimateButton']) {
  assert.ok(markup.includes('src="{{homeAssets.' + property + '}}"'));
}
for (const route of ['/pages/aicost/aicost', '/pages/allrounddesign/allrounddesign', '/pages/aitools/aitools', '/pages/checkin/checkin', '/pages/Invitefriends/Invitefriends']) {
  assert.ok(markup.includes(route));
}
for (const binding of ['onMascotTap', 'onOpenPage', 'onQuickProjectChange', 'onQuickRegionChange', 'onQuickAreaInput', 'onQuickEstimate']) {
  assert.ok(markup.includes('="' + binding + '"'));
}
assert.match(styles, /\.cost-estimator \{ position: relative;/);

for (const property of ['defaultTabbar', 'homeTabbar', 'templateTabbar', 'designerTabbar', 'myTabbar']) {
  assert.match(tabBarSource, new RegExp(`HOME_ASSETS\\.${property}`));
}

let tabBarConfig;
vm.runInNewContext(read('miniprogram/custom-tab-bar/index.js'), {
  exports: {},
  require: name => name.endsWith('homeAssets') ? { HOME_ASSETS } : {},
  Component: value => { tabBarConfig = value; },
  getApp: () => ({ globalData: {} }),
  getCurrentPages: () => [],
  wx: { switchTab() {}, showToast() {} },
});
assert.equal(tabBarConfig.data.defaultTabbarImage, HOME_ASSETS.defaultTabbar);
assert.equal(tabBarConfig.data.homeTabbarImage, HOME_ASSETS.homeTabbar);
assert.equal(tabBarConfig.data.templateTabbarImage, HOME_ASSETS.templateTabbar);
assert.equal(tabBarConfig.data.designerTabbarImage, HOME_ASSETS.designerTabbar);
assert.equal(tabBarConfig.data.myTabbarImage, HOME_ASSETS.myTabbar);

console.log('Home release uses immutable cloud assets while preserving interactions and non-header geometry');
