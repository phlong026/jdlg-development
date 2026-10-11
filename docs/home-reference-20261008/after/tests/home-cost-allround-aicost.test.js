const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const indexWxml = read('miniprogram/pages/index/index.wxml');
const indexScss = read('miniprogram/pages/index/index.scss');
const allroundTs = read('miniprogram/pages/allrounddesign/allrounddesign.ts');
const allroundJs = read('miniprogram/pages/allrounddesign/allrounddesign.js');
const allroundWxml = read('miniprogram/pages/allrounddesign/allrounddesign.wxml');
const aicostTs = read('miniprogram/pages/aicost/aicost.ts');
const aicostJs = read('miniprogram/pages/aicost/aicost.js');
const aicostWxml = read('miniprogram/pages/aicost/aicost.wxml');
const aicostScss = read('miniprogram/pages/aicost/aicost.scss');
const aicostJson = read('miniprogram/pages/aicost/aicost.json');
const appJson = read('miniprogram/app.json');
const aicostResultTs = read('miniprogram/pages/aicostresult/aicostresult.ts');
const aicostResultJs = read('miniprogram/pages/aicostresult/aicostresult.js');
const aicostResultWxml = read('miniprogram/pages/aicostresult/aicostresult.wxml');
const aicostResultScss = read('miniprogram/pages/aicostresult/aicostresult.scss');

assert.match(indexWxml, /data-url="\/pages\/aicost\/aicost"/, 'home cost entry should route to the existing cost page');
assert.match(indexWxml, /aria-label="造价测算"/, 'home cost entry should use the confirmed name');
assert.doesNotMatch(indexScss, /\.cost-entry-label/, 'home cost overlay style should be removed');

for (const source of [allroundTs, allroundJs]) {
  assert.match(source, /亲子工坊/, 'allround design should include the parent-child workshop tab');
  assert.match(source, /parentWorkshop/, 'allround design should define parent workshop behavior');
  assert.match(source, /parent-child-design/, 'parent workshop tab should submit through the parent child design API in the same page');
  assert.doesNotMatch(source, /\/pages\/Parentchildcreativity\/Parentchildcreativity/, 'parent workshop tab should not jump to the old standalone page');
}

assert.match(allroundWxml, /activeTab === 'parentWorkshop'/, 'allround design should render parent workshop tab content');
assert.match(allroundWxml, /亲子用途/, 'parent workshop should expose purpose selection in the single page form');
assert.match(allroundWxml, /创作主题/, 'parent workshop should use a stable single page prompt form');
assert.doesNotMatch(allroundWxml, /segmented-row/, 'parent workshop should not render nested segmented tabs');
assert.doesNotMatch(allroundWxml, /parentDesignType === 'poster'/, 'parent workshop should not branch into secondary forms');

// 价格和报告状态通过 aicost-pricing / aicost-report 的行为测试验证。
for (const source of [aicostTs, aicostJs]) {
  assert.match(source, /estimateTotalCost/);
  assert.doesNotMatch(source, /MARKETING_DISCOUNT_RATE|discountRate|INTERIOR_UNIT_PRICE/);
  assert.match(source, /onRegionColumnChange/);
  assert.match(source, /onRegionCancel/);
  assert.match(source, /windowStandard/);
  assert.match(source, /燕尾脊/);
}
assert.match(appJson, /pages\/aicostresult\/aicostresult/);
assert.match(aicostWxml, /title="造价测算"/);
assert.match(aicostWxml, /mode="multiSelector"/);
assert.match(aicostWxml, /bindcancel="onRegionCancel"/);
assert.match(aicostWxml, /基础地区信息/);
assert.match(aicostWxml, /面积参数/);
assert.match(aicostWxml, /楼层参数/);
assert.match(aicostWxml, /建筑核心属性 \/ 外观装修标准/);
assert.match(aicostWxml, /data-field="windowStandard"/);
assert.match(aicostWxml, /<\/scroll-view>[\s\S]*bottom-action[\s\S]*生成参考造价/);
assert.doesNotMatch(aicostJson, /t-icon/);
assert.match(aicostScss, /\.cost-scroll-wrap[\s\S]*position:\s*fixed;/);
for (const source of [aicostResultTs, aicostResultJs]) {
  assert.match(source, /aicostEstimateResult/);
  assert.match(source, /resolveEnterpriseWechatServiceConfig/);
  assert.match(source, /onViewCostDetails/);
  assert.match(source, /onContactService/);
  assert.match(source, /\/pages\/customerservice\/customerservice\?source=aicost/);
}
assert.match(aicostResultWxml, /基础信息/);
assert.match(aicostResultWxml, /主要工程造价测算/);
assert.match(aicostResultWxml, /联系设计师优化/);
assert.match(aicostResultWxml, /查看费用明细/);
assert.match(aicostResultWxml, /chart-bar-fill/);
assert.doesNotMatch(aicostResultWxml, /cost-donut/);
assert.match(aicostResultScss, /\.report-card/);
console.log('Existing home entry and design tabs, native form, report layout and service route passed');
