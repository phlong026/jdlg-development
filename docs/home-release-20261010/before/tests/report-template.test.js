const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { execFileSync } = require('node:child_process');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/package.nw/node_modules/wcc-exec/wcc';
const options = { skip: !fs.existsSync(compiler) && 'Set WXML_COMPILER to an existing WeChat WXML compiler' };
function render(page, data) {
  const file = 'miniprogram/pages/report/' + page + '.wxml';
  const source = execFileSync(compiler, [file], { cwd: path.resolve(__dirname, '..'), encoding: 'utf8' });
  const context = { window: {}, console: { log: error => { throw error; } } };
  vm.createContext(context); vm.runInContext(source, context);
  return context.$gwx(file)(data);
}
function nodes(tree) { return typeof tree === 'object' && tree ? [tree, ...(tree.children || []).flatMap(nodes)] : []; }
function text(tree) { return typeof tree === 'string' ? tree : (tree?.children || []).map(text).join('\n'); }

test('compiled house preview renders blocks, requirements and confirmation without legacy promises', options, () => {
  const tree = render('preview', {
    modern: true, houseReport: true, report: { paid: false }, needsConfirmation: true,
    confirmationOptions: [{ value: 'confirm', label: '确认需求' }], confirmationChoice: 'confirm',
    houseInputs: [{ title: '书房', body: '1间；楼层不限；必须满足' }],
    sections: [1,2,3,4].map(number => ({ number, title: '第' + number + '页', blocks: [{ title: '输入依据', body: '来自用户填写' }] }))
  });
  assert.match(text(tree), /书房/); assert.match(text(tree), /必须满足/); assert.match(text(tree), /来自用户填写/);
  assert.match(text(tree), /页数随/); assert.doesNotMatch(text(tree), /14 页|10 页|三种概念方案|需添加企业微信/);
  assert.ok(nodes(tree).some(node => node.attr?.bindtap === 'confirm'));
  assert.ok(nodes(tree).some(node => node.attr?.bindtap === 'pay' && node.attr.disabled === true));
});
test('compiled questionnaire exposes every new input through actual page handlers', options, () => {
  const input = { site: {}, household: {}, space: {}, preference: { priorities: [] }, budget: {} };
  const house = { rooms: [{ id: 'room-1', name: '书房', count: 1, preferred_floor: 0, required: true }], budget_scope: [{ item: '主体建造', status: 'unknown' }] };
  const all = [];
  for (let step=0; step<5; step++) all.push(...nodes(render('questionnaire', {
    step, configReady: true, houseEnabled: true, input, house, titles: [], floors: [1,2,3],
    yesNo: [{ value: 'yes', label: '需要' }], frequencies: [{ value: 'monthly', label: '每月' }],
    scopeStatuses: [{ value: 'included', label: '包含' }]
  })));
  for (const field of ['permanent_count','returning_count','return_frequency','elder_room','courtyard','parking','flexible_use','name','count','preferred_floor','required','start_window'])
    assert.ok(all.some(node => node.attr?.['data-field'] === field), field + ' must be collected in the actual WXML');
  assert.ok(all.some(node => node.attr?.bindtap === 'budgetScope'));
});
test('compiled landing and delivery use content-driven pages only for modern reports', options, () => {
  assert.doesNotMatch(text(render('landing', { houseEnabled: true })), /14 页|10 页/);
  assert.match(text(render('landing', { houseEnabled: false })), /14 页/);
  const delivery = render('delivery', { modern: true, houseReport: true, canDownload: true, report: { paid: true, bound: false } });
  assert.match(text(delivery), /完整 PDF/); assert.match(text(delivery), /可选/);
  assert.doesNotMatch(text(delivery), /14 页|10 页|待添加并确认身份/);
  assert.ok(nodes(delivery).some(node => node.attr?.bindtap === 'document'));
});
