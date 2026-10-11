const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const source = fs.readFileSync(
  path.join(__dirname, '../miniprogram/pages/aicost/aicost.js'),
  'utf8',
);
const costRegions = require('../miniprogram/utils/costRegions');

function loadForm(options) {
  let definition;
  vm.runInNewContext(source, {
    exports: {},
    require(request) {
      if (request === '../../utils/costRegions') return costRegions;
      return { prepareCostEstimateShareImage: async () => '' };
    },
    Page(value) { definition = value; },
    wx: {},
  });

  const updates = [];
  const page = {
    ...definition,
    data: { ...definition.data },
    setData(values) {
      updates.push(values);
      Object.assign(this.data, values);
    },
  };
  page.onLoad(options);
  return { page, updates };
}

test('homepage quick estimate prefills a supported region and positive building area', () => {
  const { page } = loadForm({
    source: 'home_quick_estimate',
    province: '福建',
    city: '泉州',
    buildingArea: '320',
    projectType: 'rural_villa',
  });

  assert.equal(page.data.province, '福建');
  assert.equal(page.data.city, '泉州');
  assert.deepEqual(Array.from(page.data.regionIndex), [0, 0]);
  assert.equal(page.data.buildingArea, '320');
  assert.equal(page.data.footprintArea, '');
});

test('supported non-default region updates picker options and indices together', () => {
  const { page } = loadForm({ province: '广东', city: '深圳' });

  assert.equal(page.data.provinceIndex, page.data.provinceOptions.indexOf('广东'));
  assert.equal(page.data.cityIndex, page.data.cityOptions.indexOf('深圳'));
  assert.deepEqual(Array.from(page.data.regionIndex), [1, 1]);
  assert.deepEqual(Array.from(page.data.regionOptions[1]), Array.from(page.data.cityOptions));
});

test('direct navigation without query preserves every default', () => {
  const { page, updates } = loadForm();

  assert.equal(updates.length, 0);
  assert.equal(page.data.province, '福建');
  assert.equal(page.data.city, '泉州');
  assert.equal(page.data.buildingArea, '');
});

test('unsupported region and invalid building area are ignored independently', () => {
  const invalid = loadForm({
    province: '福建',
    city: '深圳',
    buildingArea: '-20',
    projectType: 'urban_renewal',
  }).page;
  assert.equal(invalid.data.province, '福建');
  assert.equal(invalid.data.city, '泉州');
  assert.equal(invalid.data.buildingArea, '');

  const areaOnly = loadForm({
    province: '未知省',
    city: '未知市',
    buildingArea: ' 440.5 ',
  }).page;
  assert.equal(areaOnly.data.province, '福建');
  assert.equal(areaOnly.data.city, '泉州');
  assert.equal(areaOnly.data.buildingArea, '440.5');
});

test('non-finite and empty areas do not overwrite the blank form field', () => {
  for (const buildingArea of ['', '0', 'Infinity', 'not-a-number']) {
    assert.equal(loadForm({ buildingArea }).page.data.buildingArea, '');
  }
});
