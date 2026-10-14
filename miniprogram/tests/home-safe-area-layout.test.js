const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');
const markup = read('miniprogram/pages/index/index.wxml');
const styles = read('miniprogram/pages/index/index.scss');
const script = read('miniprogram/pages/index/index.js');
const original = path.resolve(__dirname, '../../docs/home-refinement-20261011/before/miniprogram/miniprogram/pages/index');

assert.match(markup, /class="brand-header" style="top: \{\{brandHeaderTop\}\}px;"/);
const grid = markup.split('<view class="service-grid">')[1].split('<image class="ai-panel"')[0];
const entries = [...grid.matchAll(/<view class="service-entry"[^>]*>[\s\S]*?<\/text>\s*<\/view>/g)].map(match => {
  const block = match[0];
  return {
    label: block.match(/aria-label="([^"]+)"/)[1],
    url: block.match(/data-url="([^"]+)"/)[1],
    image: block.match(/homeAssets\.([a-zA-Z]+)/)[1],
  };
});
assert.deepEqual(entries, [
  { label: '模板广场', url: '/pages/template/template', image: 'templates' },
  { label: 'AI工具', url: '/pages/aitools/aitools', image: 'aiTools' },
  { label: '别墅设计', url: '/pages/aigenerate/aigenerate?source=rural_villa&amp;showSceneTabs=1&amp;tab=exterior', image: 'villa' },
  { label: '装修设计', url: '/pages/aigenerate/aigenerate?source=rural_villa&amp;showSceneTabs=1&amp;tab=interior', image: 'interior' },
  { label: '庭院设计', url: '/pages/aigenerate/aigenerate?source=courtyard&amp;prompt=庭院景观设计', image: 'courtyard' },
  { label: '旧居翻新', url: '/pages/buildingreplacement/buildingreplacement', image: 'renovation' },
  { label: '造价测算', url: '/pages/aicost/aicost', image: 'calculator' },
  { label: '建房顾问', url: '/pages/specadvisor/specadvisor', image: 'advisor' },
]);

function pageFor(windowInfo, getWindowInfo) {
  let config, route, warnings = [];
  const wx = {
    getStorageSync: () => true,
    navigateTo: options => { route = options.url; },
    showToast() {},
  };
  if (windowInfo) wx.getWindowInfo = () => windowInfo;
  if (getWindowInfo) wx.getWindowInfo = getWindowInfo;
  vm.runInNewContext(script, {
    exports: {},
    require: name => {
      if (name.endsWith('costRegions')) return require('../miniprogram/utils/costRegions.js');
      if (name.endsWith('homeAssets')) return require('../miniprogram/utils/homeAssets.js');
      return { resolveAssetPath: value => value };
    },
    Page: value => { config = value; },
    wx,
    console: { warn: (...values) => warnings.push(values) },
  });
  const page = { ...config, data: { ...config.data }, setData: values => Object.assign(page.data, values) };
  page.onLoad();
  return { page, route: () => route, warnings };
}

for (const windowWidth of [320, 375, 390, 393, 414, 430]) {
  for (const statusBarHeight of [20, 44, 54, 59, 62]) {
    const { page } = pageFor({ windowWidth, statusBarHeight });
    const top = page.data.brandHeaderTop;
    assert.ok(top >= statusBarHeight + 12, 'Logo must clear the status bar');
    assert.ok(top >= 85.55 * windowWidth / 750, 'Logo must move down from the old position');
    assert.ok(top + 153.16 * windowWidth / 750 < 349.06 * windowWidth / 750, 'Brand must not overlap the eight-service panel');
  }
}
assert.equal(pageFor().page.data.brandHeaderTop, 68, 'Older base libraries retain a safe fallback');
const failed = pageFor(null, () => { throw new Error('window info unavailable'); });
assert.equal(failed.page.data.brandHeaderTop, 68);
assert.equal(failed.warnings.length, 1);

const privacy = pageFor({ windowWidth: 393, statusBarHeight: 59 });
privacy.page.onOpenPage({ currentTarget: { dataset: { url: '/pages/privacy/privacy?type=privacy' } } });
assert.equal(privacy.route(), '/pages/privacy/privacy?type=privacy');
assert.match(markup, /cost-title">10秒估算建房报价<\/text>/);
assert.match(markup, /aria-label="别墅工程"/);
assert.match(markup, /aria-label="室内装修"/);
assert.match(markup, /点击按钮视为同意/);
assert.match(markup, /data-url="\/pages\/privacy\/privacy\?type=privacy"/);
assert.doesNotMatch(markup, /估算结果仅供参考/);
assert.doesNotMatch(styles, /filter:\s*blur|image-rendering:\s*pixelated/);

const oldMarkup = fs.readFileSync(path.join(original, 'index.wxml'), 'utf8');
const aiSection = value => value.split('<image class="ai-panel"')[1].split('<view class="cost-panel"')[0];
assert.equal(aiSection(markup), aiSection(oldMarkup), 'The confirmed AI pill must remain unchanged');
assert.equal(markup.split('<view class="content">')[1], oldMarkup.split('<view class="content">')[1], 'Cases and popups must remain unchanged');
const rules = value => new Map([...value.matchAll(/([^{}\n]+)\s*\{([^{}]*)\}/g)].map(match => [match[1].trim(), match[2].trim()]));
const oldRules = rules(fs.readFileSync(path.join(original, 'index.scss'), 'utf8'));
const newRules = rules(styles);
const allowed = new Set(['.brand-backdrop::after', '.brand-symbol', '.brand-copy', '.brand-slogan', '.brand-name', '.brand-en', '.brand-slogan text', '.slogan-line', '.cost-title', '.estimate-note', '.photo-hotspot', '.voice-hotspot']); // hotspots enlarged on purpose to fix missed taps
for (const [selector, body] of oldRules) {
  if (!allowed.has(selector)) assert.equal(newRules.get(selector), body, `Protected style changed: ${selector}`);
}
for (const name of ['advisor', 'calculator', 'ai-tools', 'villa', 'interior', 'courtyard', 'renovation', 'templates']) {
  const bytes = fs.readFileSync(path.join(root, 'miniprogram/assets/home-reference/hd', name + '.png'));
  assert.ok(bytes.readUInt32BE(16) >= 384 && bytes.readUInt32BE(20) >= 384, 'Service icons must keep HD source dimensions');
}
console.log('Home status-bar clearance, service order, privacy entry and protected content passed');

assert.match(styles, /\.brand-symbol\s*\{[^}]*width:\s*129\.78rpx;[^}]*height:\s*108\.67rpx;/);
assert.match(styles, /\.brand-name\s*\{[^}]*width:\s*180\.42rpx;[^}]*height:\s*45\.37rpx;/);
assert.match(styles, /\.brand-en\s*\{[^}]*width:\s*180\.42rpx;[^}]*height:\s*35\.87rpx;/);

assert.match(styles, /\.brand-slogan text\s*\{[^}]*font-size:\s*21\.11rpx;[^}]*letter-spacing:\s*12\.66rpx;/);
assert.match(styles, /\.slogan-line\s*\{[^}]*width:\s*31\.66rpx;/);

// 品牌背景只覆盖左侧品牌区，透明边缘不遮挡右侧客服或接收点击。
const brandScrim = newRules.get('.brand-header::before');
assert.ok(brandScrim, 'Brand needs a clean local backdrop');
assert.match(brandScrim, /pointer-events:\s*none/);
assert.match(brandScrim, /linear-gradient\(to right, rgba\(249,245,237,\.76\)/);
assert.match(brandScrim, /-webkit-mask-image:\s*linear-gradient/);
assert.ok(Number(brandScrim.match(/width:\s*([\d.]+)rpx/)[1]) < 598.77, 'Brand scrim must stop before customer service');
assert.doesNotMatch(newRules.get('.brand-symbol'), /opacity:/);
assert.doesNotMatch(newRules.get('.brand-copy'), /opacity:/);
assert.doesNotMatch(newRules.get('.brand-slogan'), /opacity:/);

const firstBrandOpacity = Number(brandScrim.match(/rgba\(249,245,237,([.\d]+)\)/)[1]);
assert.ok(firstBrandOpacity <= .8, 'Brand backdrop must leave the villa visible');
assert.match(brandScrim, /rgba\(249,245,237,\.24\) 74%/);
assert.ok(Number(brandScrim.match(/height:\s*([\d.]+)rpx/)[1]) <= 205, 'Do not wash out the villa below the slogan');
