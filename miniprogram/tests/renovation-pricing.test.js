const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const {
  createDefaultRenovationInput,
  estimateRenovation,
} = require('../miniprogram/utils/renovation-pricing');

const BASE_SPACE_AMOUNTS = [16864, 18080, 16512, 20152, 1538, 12656];

function allItems(result) {
  return result.spaces.flatMap(space => space.groups.flatMap(group => group.items));
}

function assertConsistent(result) {
  for (const space of result.spaces) {
    for (const group of space.groups) {
      assert.equal(group.amount, group.items.reduce((sum, item) => sum + item.amount, 0));
    }
    assert.equal(space.amount, space.groups.reduce((sum, group) => sum + group.amount, 0));
  }
  assert.equal(result.engineeringAmount, result.spaces.reduce((sum, space) => sum + space.amount, 0));
  assert.equal(result.engineeringAmount, result.materialAmount + result.auxiliaryAmount + result.laborAmount);
  assert.equal(
    result.totalAmount,
    result.engineeringAmount + result.fees.reduce((sum, fee) => sum + fee.amount, 0),
  );
}

test('standard 100m2 new-house reference reproduces the supplied Tubatu benchmark', () => {
  const input = createDefaultRenovationInput();
  assert.deepEqual(input, {
    province: '福建', city: '泉州', area: 100, width: 10, depth: 10,
    floorCount: 1, height: 3, houseType: 'new', standard: 'standard', style: '现代简约',
    rooms: { bedroom: 3, living: 2, kitchen: 1, bathroom: 2, balcony: 1 },
  });
  const result = estimateRenovation(input);

  assert.equal(result.pricingVersion, 'renovation-reference-v1');
  assert.deepEqual(result.spaces.map(space => space.id), ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony', 'common']);
  assert.deepEqual(result.spaces.map(space => space.name), ['卧室', '客餐厅', '厨房', '卫生间', '阳台', '全屋公共']);
  assert.deepEqual(result.spaces.map(space => space.groups.map(group => [group.id, group.name])), [
    [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['door', '室内门']],
    [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['threshold', '门槛石']],
    [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['kitchenware', '厨具'], ['door', '室内门'], ['lighting', '灯具']],
    [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面'], ['sanitary', '卫浴用具'], ['door', '室内门']],
    [['ceiling', '天花板'], ['wall', '墙面'], ['floor', '地面']],
    [['utilities', '水电改造'], ['handling', '搬运清洁'], ['demolition', '拆改与保护']],
  ]);
  assert.deepEqual([...new Set(allItems(result).map(item => item.unit))].sort(), ['m', '㎡', '个', '位', '块', '套', '项'].sort());
  assert.deepEqual(result.spaces.map(space => space.amount), BASE_SPACE_AMOUNTS);
  assert.deepEqual(result.spaces.map(space => space.groups.map(group => group.amount)), [
    [2776, 3304, 7184, 3600],
    [2600, 8664, 6704, 112],
    [768, 4200, 1320, 8440, 1664, 120],
    [1024, 6552, 3072, 7520, 1984],
    [442, 280, 816],
    [9328, 2400, 928],
  ]);
  assert.equal(result.engineeringAmount, 85802);
  assert.equal(result.materialAmount, 44792);
  assert.equal(result.auxiliaryAmount, 12058);
  assert.equal(result.laborAmount, 28952);
  assert.deepEqual(result.fees.map(fee => [fee.id, fee.amount, fee.included]), [
    ['management', 8580, true],
    ['design', 3432, true],
    ['tax', 0, false],
  ]);
  assert.equal(result.totalAmount, 97814);
  assert.equal(result.unitAmount, 978.14);
  assertConsistent(result);
});

test('standard and old-house multipliers apply to the selected engineering list', () => {
  const input = createDefaultRenovationInput();
  const economy = estimateRenovation({ ...input, standard: 'economy' }, { fees: { management: false, design: false } });
  const quality = estimateRenovation({ ...input, standard: 'quality' }, { fees: { management: false, design: false } });
  const oldHouse = estimateRenovation({ ...input, houseType: 'old' }, { fees: { management: false, design: false } });

  assert.equal(economy.engineeringAmount, 60061);
  assert.equal(quality.engineeringAmount, 102961);
  assert.equal(oldHouse.engineeringAmount, 95125);
  for (const result of [quality, oldHouse]) {
    for (const pricedItem of allItems(result)) {
      assert.equal(
        pricedItem.amount,
        Math.round(pricedItem.quantity * (pricedItem.materialUnitPrice + pricedItem.laborAuxUnitPrice)),
        pricedItem.id,
      );
    }
  }
  const qualityTile = allItems(quality).find(item => item.id === 'bedroom.floor.tile');
  const oldHouseTile = allItems(oldHouse).find(item => item.id === 'bedroom.floor.tile');
  assert.deepEqual(
    [qualityTile.materialUnitPrice, qualityTile.laborAuxUnitPrice, qualityTile.amount],
    [134.4, 96, 6912],
  );
  assert.deepEqual(
    [oldHouseTile.materialUnitPrice, oldHouseTile.laborAuxUnitPrice, oldHouseTile.amount],
    [124.1744, 88.696, 6386],
  );
  assertConsistent(economy);
  assertConsistent(quality);
  assertConsistent(oldHouse);
});

test('fees are optional and use the recalculated engineering subtotal', () => {
  const input = createDefaultRenovationInput();
  const allFees = estimateRenovation(input, { fees: { tax: true } });
  assert.deepEqual(allFees.fees.map(fee => fee.included), [true, true, true]);
  assert.equal(allFees.fees[2].amount, Math.round(allFees.engineeringAmount * 0.034));

  const excluded = estimateRenovation(input, {
    excludedItems: ['bedroom.floor.tile'],
    fees: { management: true, design: false, tax: true },
  });
  const tile = allItems(excluded).find(item => item.id === 'bedroom.floor.tile');
  assert.equal(tile.included, false);
  assert.equal(tile.amount, 0);
  assert.equal(tile.quantity, 30);
  assert.equal(tile.materialUnitPrice, 112);
  assert.equal(tile.laborAuxUnitPrice, 80);
  assert.equal(excluded.engineeringAmount, 85802 - 5760);
  assert.deepEqual(excluded.fees.map(fee => fee.amount), [8004, 0, 2721]);
  assertConsistent(excluded);
});

test('quantity overrides recalculate one item without mutating defaults', () => {
  const input = createDefaultRenovationInput();
  const changed = estimateRenovation(input, { quantities: { 'bedroom.floor.tile': 31 } });
  const changedTile = allItems(changed).find(item => item.id === 'bedroom.floor.tile');
  const freshTile = allItems(estimateRenovation(input)).find(item => item.id === 'bedroom.floor.tile');

  assert.equal(changedTile.quantity, 31);
  assert.equal(changedTile.amount, 5952);
  assert.equal(changed.engineeringAmount, 85994);
  assert.equal(freshTile.quantity, 30);
  assert.equal(freshTile.amount, 5760);
  assertConsistent(changed);
});

test('reference fixed quantities do not silently scale with house area', () => {
  const result = estimateRenovation({ ...createDefaultRenovationInput(), area: 120 });
  const items = allItems(result);
  const quantity = id => items.find(item => item.id === id).quantity;
  assert.equal(quantity('bedroom.ceiling.curtain_box'), 4);
  assert.equal(quantity('kitchen.ceiling.aluminum'), 6);
  assert.equal(quantity('kitchen.floor.tile'), 6);
  assert.equal(quantity('bathroom.ceiling.aluminum'), 8);
  assert.equal(quantity('bathroom.floor.tile'), 8);
  assert.equal(quantity('balcony.ceiling.primer'), 8);
  assert.equal(quantity('balcony.floor.tile'), 6);
  assert.equal(quantity('common.utilities.switch_wiring'), 72);
});

test('sanitary item names follow the supplied reference', () => {
  const result = estimateRenovation(createDefaultRenovationInput());
  const sanitaryItems = result.spaces.find(space => space.id === 'bathroom')
    .groups.find(group => group.id === 'sanitary').items;
  assert.deepEqual(sanitaryItems.map(item => item.name), ['马桶', '洗手盆', '花洒', '风暖浴霸']);
});

test('zero-count rooms contribute no price while common engineering remains', () => {
  const input = createDefaultRenovationInput();
  input.rooms = { bedroom: 0, living: 0, kitchen: 0, bathroom: 0, balcony: 0 };
  const result = estimateRenovation(input, { fees: { management: false, design: false } });

  assert.deepEqual(result.spaces.map(space => space.amount), [0, 0, 0, 0, 0, 12656]);
  for (const space of result.spaces.slice(0, 5)) {
    assert.equal(space.groups.length > 0, true);
    for (const item of space.groups.flatMap(group => group.items)) {
      assert.equal(item.included, false);
      assert.equal(item.amount, 0);
    }
  }
  assert.equal(result.engineeringAmount, 12656);
  assertConsistent(result);
});

test('input and previous results stay isolated from caller mutations', () => {
  const input = createDefaultRenovationInput();
  input.floors = [{ floor: 1, height: 3 }];
  const first = estimateRenovation(input);
  input.rooms.bedroom = 0;
  input.floors[0].height = 9;
  first.input.rooms.living = 0;
  first.spaces[0].groups[0].items[0].quantity = 999;
  const second = estimateRenovation(createDefaultRenovationInput());

  assert.equal(first.input.rooms.bedroom, 3);
  assert.equal(first.input.floors[0].height, 3);
  assert.equal(second.input.rooms.living, 2);
  assert.equal(second.spaces[0].groups[0].items[0].quantity, 30);
});

test('optional width and depth normalize to unfilled values', () => {
  const input = createDefaultRenovationInput();
  for (const value of [null, '', undefined]) {
    const result = estimateRenovation({ ...input, width: value, depth: value, height: value });
    assert.equal(result.input.width, null);
    assert.equal(result.input.depth, null);
    assert.equal(result.input.height, 3);
  }
});

test('TypeScript source and JavaScript runtime artifact stay synchronized', () => {
  const utilsDir = path.join(__dirname, '../miniprogram/utils');
  const tsSource = fs.readFileSync(path.join(utilsDir, 'renovation-pricing.ts'), 'utf8');
  const jsSource = fs.readFileSync(path.join(utilsDir, 'renovation-pricing.js'), 'utf8');
  assert.ok(tsSource.startsWith('// @ts-nocheck\n'));
  assert.equal(tsSource.slice('// @ts-nocheck\n'.length), jsSource);
  assert.doesNotMatch(tsSource, /require\(['"]\.\/renovation-pricing\.js['"]\)/);
});

test('invalid or malicious numeric values and unknown selections are rejected', () => {
  const valid = createDefaultRenovationInput();
  for (const patch of [
    { area: 0 }, { area: 100001 }, { area: NaN }, { width: Infinity },
    { floorCount: 1.5 }, { floorCount: 21 }, { height: -1 },
    { floors: [{ floor: 1, height: NaN }] },
    { rooms: { ...valid.rooms, bathroom: -1 } },
    { rooms: { ...valid.rooms, bedroom: 51 } },
  ]) {
    assert.throws(() => estimateRenovation({ ...valid, ...patch }), /装修报价参数不正确/);
  }
  assert.throws(() => estimateRenovation(valid, { quantities: { 'bedroom.floor.tile': -1 } }), /工程量/);
  assert.throws(() => estimateRenovation(valid, { quantities: { 'bedroom.floor.tile': Infinity } }), /工程量/);
  assert.throws(() => estimateRenovation(valid, { quantities: { 'bedroom.floor.tile': 1000001 } }), /工程量/);
  assert.throws(() => estimateRenovation(valid, { quantities: { 'missing.item': 1 } }), /未知装修报价项目/);
  assert.throws(() => estimateRenovation({ ...valid, standard: 'luxury' }), /装修报价参数不正确/);
});
