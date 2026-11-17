const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { execFileSync } = require('node:child_process');
const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
const options = { skip: !fs.existsSync(compiler) && 'Set WXML_COMPILER to the installed WeChat compiler' };
const root = path.resolve(__dirname, '..');
const pricing = require('../miniprogram/utils/renovation-pricing');

function render(file, data) {
  const source = execFileSync(compiler, [file], { cwd: root, encoding: 'utf8' });
  const context = { window: {}, console: { log(error) { throw error; } } };
  vm.runInNewContext(source, context);
  return context.$gwx(file)(data);
}
function text(tree) { return typeof tree === 'string' ? tree : (tree?.children || []).map(text).join('\n'); }

test('native WXML compilation accepts the complete renovation pages and shared components', options, () => {
  for (const file of ['pages/renovationbudget/index', 'pages/renovationbudget/result', 'pages/aicost/aicost', 'pages/aicostshare/aicostshare', 'components/quote-type-switch/index', 'components/renovation-measurement-editor/index']) {
    execFileSync(compiler, [`miniprogram/${file}.wxml`], { cwd: root, stdio: 'pipe' });
  }
});

test('compiled report displays numeric totals, hides edit controls in public mode, and opens actual item details', options, () => {
  let definition;
  vm.runInNewContext(fs.readFileSync(path.join(root, 'miniprogram/components/renovation-report/index.js'), 'utf8'), {
    Component(value) { definition = value; },
  });
  const estimate = pricing.estimateRenovation({ ...pricing.createDefaultRenovationInput(), rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 },
    measurement: { version: 1, mode: 'measured', floors: [{ index: 0, height: 3, spaces: [{ id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 4, width: 3, height: null,
      doorWidth: 0.9, doorArea: 1.8, windowWidth: 1.5, windowArea: 2.25, curtainBoxLength: 1.5, tvBackgroundArea: null, cabinetLength: null }] }] } });
  const component = { ...definition.methods, data: { ...definition.data, readonly: true, detailsExpanded: false,
    report: { renovation: { ...estimate, quantityBasis: 'mixed' }, estimatedUnitCost: `${estimate.unitAmount}元/㎡`, costBreakdown: [], basicInfo: [{ label: '所在地区', value: '福建 / 泉州' }] } },
    setData(values) { Object.assign(this.data, values); } };
  component.refresh();
  const file = 'miniprogram/components/renovation-report/index.wxml';
  const overview = text(render(file, component.data));
  assert.ok(overview.includes((estimate.totalAmount / 10000).toFixed(2)));
  assert.match(overview, /福建 \/ 泉州/);
  assert.match(overview, /含估算项/);
  assert.doesNotMatch(overview, /修改参数|附加费用|工程量/);
  component.data.detailsExpanded = true;
  component.refresh();
  const detail = text(render(file, component.data));
  assert.match(detail, /顶面墙固/);
  assert.match(detail, /12㎡/);
  assert.match(detail, /¥115/);
  assert.doesNotMatch(detail, /装修参考总价/);
});
