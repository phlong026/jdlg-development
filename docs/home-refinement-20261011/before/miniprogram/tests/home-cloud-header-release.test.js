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
assert.match(markup, /class="brand-backdrop"/);
assert.match(markup, /src="\{\{homeAssets\.brandScene\}\}"/);
assert.doesNotMatch(markup, /src="\/assets\/(?:home-reference|smart-service-icon)/);

for (const property of [
  'backdrop', 'brandSymbol', 'brandName', 'brandEnglish', 'customerService',
  'advisor', 'calculator', 'aiTools', 'villa', 'interior', 'courtyard',
  'renovation', 'templates', 'aiPanel', 'aiOrb', 'camera', 'sparkles',
  'costStats', 'ruralVilla', 'urbanBuilding', 'location', 'ruler', 'estimateButton',
]) {
  assert.match(markup, new RegExp(`src="\\{\\{homeAssets\\.${property}\\}\\}"`));
}

assert.match(styles, /\.brand-backdrop\s*\{[^}]*pointer-events:\s*none;/s);
assert.match(styles, /\.brand-symbol\s*\{\s*left:\s*38\.69rpx;\s*top:\s*-43\.96rpx;/);
assert.match(styles, /\.brand-slogan\s*\{\s*left:\s*34\.29rpx;\s*top:\s*61\.55rpx;/);

// Existing routes, event bindings and core geometry are release invariants.
for (const route of [
  '/pages/specadvisor/specadvisor', '/pages/aicost/aicost', '/pages/aitools/aitools',
  '/pages/buildingreplacement/buildingreplacement', '/pages/template/template',
]) {
  assert.match(markup, new RegExp(route.replace(/[?]/g, '\\?')));
}
for (const binding of ['onMascotTap', 'onOpenPage', 'onQuickProjectChange', 'onQuickRegionChange', 'onQuickAreaInput', 'onQuickEstimate']) {
  assert.match(markup, new RegExp(`(?:bindtap|bindchange|bindinput)="${binding}"`));
}
for (const invariant of [
  '.service-grid { left: 22.86rpx; top: 258.50rpx; width: 703.40rpx; height: 305.98rpx;',
  '.ai-hotspot { left: 327.56rpx; top: 572.40rpx; width: 94rpx; height: 92.59rpx;',
  '.cost-panel { left: 29.02rpx; top: 847.60rpx; width: 691.09rpx; height: 511.72rpx;',
]) {
  assert.ok(styles.includes(invariant), `missing geometry invariant: ${invariant}`);
}

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
