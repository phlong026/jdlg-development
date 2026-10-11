const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const serviceWxml = read('miniprogram/pages/customerservice/customerservice.wxml');
const serviceTs = read('miniprogram/pages/customerservice/customerservice.ts');
const serviceJs = read('miniprogram/pages/customerservice/customerservice.js');
const sendIcon = read('miniprogram/assets/icons/send-plane.svg');

const summaryCardMatch = serviceWxml.match(/<view wx:if="\{\{demandSummaryVisible\}\}" class="demand-summary-card">([\s\S]*?)<\/view>\s*<\/view>/);
assert.ok(summaryCardMatch, 'customer service page should render a demand summary card');

const summaryCard = summaryCardMatch[0];
assert.match(summaryCard, /一键生成专属方案/, 'demand summary card should keep the generate CTA');
assert.doesNotMatch(summaryCard, /了解造价/, 'demand summary card should not include cost consultation');
assert.doesNotMatch(summaryCard, /对接设计老师/, 'demand summary card should not include designer handoff');
assert.doesNotMatch(summaryCard, /修改户型/, 'demand summary card should not include floor-plan edit');

assert.match(serviceWxml, /bottom-panel[\s\S]*quick-actions[\s\S]*input-panel/, 'quick actions should stay above the chat input panel');
assert.match(serviceWxml, /class="composer-shell"/, 'chat input should use a rounded composer shell');
assert.match(serviceWxml, /class="composer-add" bindtap="onComposerAdd"/, 'chat input should expose a left add button');
assert.match(serviceWxml, /placeholder="继续补充一句需求"/, 'chat input should use the confirmed supplemental-demand placeholder');
assert.match(serviceWxml, /auto-height="\{\{true\}\}"/, 'chat input should grow only when the user types multiple lines');
assert.match(serviceWxml, /class="composer-action send \{\{inputText \? 'active' : 'disabled'\}\}" bindtap="onSend"/, 'chat input should always show the send button');
assert.match(serviceWxml, /src="\/assets\/icons\/send-plane\.svg"/, 'send button should use the local paper-plane image');
assert.doesNotMatch(serviceWxml, /onVoicePlaceholder|\)\)\)|class="composer-action voice"/, 'chat input should not keep a voice entry');
assert.match(sendIcon, /<svg[\s\S]*viewBox=/, 'send paper-plane icon should be a local SVG image');

for (const source of [serviceTs, serviceJs]) {
  assert.match(source, /label:\s*'了解造价'/, 'bottom quick actions should include cost consultation');
  assert.match(source, /label:\s*'对接设计老师'/, 'bottom quick actions should include designer handoff');
  assert.match(source, /onComposerAdd\(\)/, 'left add button should have a handler');
  assert.doesNotMatch(source, /onVoicePlaceholder\(\)/, 'voice placeholder handler should be removed');
}

console.log('customer service action placement checks passed');
