const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const indexTs = read('miniprogram/pages/index/index.ts');
const indexJs = read('miniprogram/pages/index/index.js');
const indexWxml = read('miniprogram/pages/index/index.wxml');
const indexScss = read('miniprogram/pages/index/index.scss');

for (const source of [indexTs, indexJs]) {
  assert.match(source, /HOME_INTRO_POPUP_STORAGE_KEY/, 'home intro popup should use a stable storage key');
  assert.match(source, /homeIntroPopupVisible:\s*false/, 'home intro popup should default to hidden');
  assert.match(source, /showHomeIntroPopupOnce\(\)/, 'home intro popup should show once from lifecycle');
  assert.match(source, /closeHomeIntroPopup\(\)/, 'home intro popup should expose a close action');
  assert.match(source, /onHomeIntroTopup\(\)/, 'home intro popup should expose a recharge-rights action');
  assert.match(source, /onHomeIntroItemTap\(/, 'home intro popup should expose item-level navigation');
  assert.match(source, /\/pages\/topupcenter\/topupcenter/, 'home intro popup should route to the existing topup center');
}

assert.match(indexWxml, /home-intro-popup/, 'home intro popup markup should be present');
assert.match(indexWxml, /bindtap="onHomeIntroItemTap"/, 'popup intro items should be tappable');
assert.match(indexWxml, /data-url="\/pages\/aigenerate\/aigenerate\?source=home_intro"/, 'AI intro item should carry its target URL');
assert.match(indexWxml, /data-url="\/pages\/template\/template\?source=home_intro"/, 'template intro item should carry its target URL');
assert.match(indexWxml, /data-url="\/pages\/topupcenter\/topupcenter"/, 'recharge intro item should carry its target URL');
assert.match(indexWxml, /甲第灵光 AI 设计助手/, 'popup should introduce the mini program by name');
assert.match(indexWxml, /AI设计灵感生成/, 'popup should explain AI design generation');
assert.match(indexWxml, /模板下载与灵石权益/, 'popup should explain template and stone rights');
assert.match(indexWxml, /充值前确认金额与权益/, 'popup should provide compliant recharge guidance');
assert.match(indexWxml, /了解充值权益/, 'popup should include the primary recharge-rights CTA');
assert.match(indexWxml, /先逛逛/, 'popup should include the non-blocking browse CTA');
assert.match(indexWxml, /联系客服/, 'popup should include a service contact entry');

assert.match(indexScss, /\.home-intro-popup/, 'popup styles should be scoped to home intro classes');
assert.match(indexScss, /\.home-intro-primary/, 'primary button style should be defined');
assert.match(indexScss, /\.home-intro-secondary/, 'secondary button style should be defined');

console.log('home intro popup checks passed');
