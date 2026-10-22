const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { test } = require('node:test');

const root = path.resolve(__dirname, '../miniprogram');
const markup = fs.readFileSync(path.join(root, 'pages/index/index.wxml'), 'utf8');

test('原山水首页保留四大金刚，并把可交互的估算表单放在精选案例前', () => {
  const hero = markup.slice(0, markup.indexOf('<view class="cost-estimator"'));
  const entries = [...hero.matchAll(/class="service-card[^>]+data-url="([^"]+)"[^>]+aria-label="([^"]+)"/g)];
  assert.deepEqual(entries.map(match => [match[2], match[1]]), [
    ['乡村别墅', '/pages/aigenerate/aigenerate?source=rural_villa&amp;showSceneTabs=1&amp;tab=exterior'],
    ['城市更新', '/pages/aigenerate/aigenerate?source=urban_renewal&amp;showSceneTabs=0'],
    ['造价测算', '/pages/aicost/aicost'],
    ['综合设计', '/pages/allrounddesign/allrounddesign'],
  ]);
  assert.match(hero, /src="\{\{heroBgImage\}\}"/);
  assert.match(hero, /src="\{\{coreEntryImage\}\}"/);
  assert.match(hero, /aria-label="每日签到"/);
  assert.match(hero, /aria-label="邀请好友"/);
  const formStart = markup.indexOf('<view class="cost-estimator"');
  const casesStart = markup.indexOf('<view class="case-section"');
  assert.ok(formStart > markup.indexOf('aria-label="综合设计"'));
  assert.ok(casesStart > formStart);
  const form = markup.slice(formStart, casesStart);
  for (const text of ['10秒估算建房报价', '项目类型', '别墅工程', '室内装修', '所在地区', '建筑面积', '立即估算', '《隐私协议》']) {
    assert.ok(form.includes(text), `缺少表单内容: ${text}`);
  }
  for (const handler of ['onQuickProjectChange', 'onQuickRegionChange', 'onQuickAreaInput', 'onQuickEstimate']) {
    assert.ok(form.includes(`="${handler}"`), `缺少交互绑定: ${handler}`);
  }
  assert.doesNotMatch(markup, /building-report-entry|先想清楚怎么住|service-grid|brand-backdrop/);
});
