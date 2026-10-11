const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
const costRegions = require('../miniprogram/utils/costRegions');

function formShare(overrides = {}, cover = 'wxfile://form-share.jpg') {
  let definition;
  const cards = [];
  const code = ts.transpileModule(fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicost/aicost.ts'), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(code, {
    exports: {},
    Page(value) { definition = value; },
    require(request) {
      if (request === '../../utils/costRegions') return costRegions;
      return { prepareCostEstimateShareImage: async card => { cards.push(card); return cover; } };
    },
    wx: { getStorageSync() { throw new Error('must not reuse an earlier estimate'); } },
  });
  const page = { ...definition, data: { ...definition.data, ...overrides } };
  return { share: page.onShareAppMessage(), cards };
}

test('blank form shares the same poster without calculating or claiming a price', async () => {
  const { share, cards } = formShare();
  assert.equal(share.title, '10秒估算建房报价 平台大数据，造价更精准');
  assert.equal(share.path, '/pages/aicost/aicost');
  const result = await share.promise;
  assert.equal(result.imageUrl, 'wxfile://form-share.jpg');
  assert.equal(result.title, share.title);
  assert.equal(result.path, share.path);
  assert.equal(cards[0].totalCost, undefined);
  assert.equal(cards[0].buildingArea, '');
  assert.equal(cards[0].city, '');
});

test('form invitation does not present unfinished or default selections as report facts', async () => {
  const { share, cards } = formShare({ city: '厦门', buildingArea: '440', footprintArea: '', structureType: '钢结构', windowStandard: '品质型' });
  await share.promise;
  assert.equal(share.title, '10秒估算建房报价 平台大数据，造价更精准');
  assert.equal(cards[0].buildingArea, '');
  assert.equal(cards[0].structureType, '');
  assert.equal(cards[0].windowStandard, '');
  assert.equal(cards[0].totalCost, undefined);
});

test('unavailable cover retains the invitation title and form route', async () => {
  const { share, cards } = formShare({ buildingArea: '440' }, '');
  const result = await share.promise;
  assert.equal(cards[0].buildingArea, '');
  assert.equal(result.title, share.title);
  assert.equal(result.path, '/pages/aicost/aicost');
  assert.equal(result.imageUrl, undefined);
});
