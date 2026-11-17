const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { createDefaultRenovationInput, estimateRenovation } = require('../miniprogram/utils/renovation-pricing');

function allItems(result) { return result.spaces.flatMap(space => space.groups.flatMap(group => group.items)); }
function quantity(result, id) { return allItems(result).find(item => item.id === id).quantity; }
function assertConsistent(result) {
  assert.equal(result.engineeringAmount, result.spaces.reduce((sum, space) => sum + space.amount, 0));
  assert.equal(result.engineeringAmount, result.materialAmount + result.auxiliaryAmount + result.laborAmount);
  assert.equal(result.totalAmount, result.engineeringAmount + result.fees.reduce((sum, fee) => sum + fee.amount, 0));
}

test('v2 uses dynamic geometry while preserving the quotation tree and fee rules', () => {
  const result = estimateRenovation(createDefaultRenovationInput());
  assert.equal(result.pricingVersion, 'renovation-dynamic-v2');
  assert.equal(result.geometrySummary.mode, 'estimated');
  assert.equal(result.geometrySummary.area, 100);
  assert.deepEqual(result.spaces.map(space => space.id), ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony', 'common']);
  assert.ok(quantity(result, 'bedroom.floor.tile') > 0);
  assert.equal(quantity(result, 'common.utilities.network_tv'), 5);
  assert.equal(quantity(result, 'common.utilities.electric_box'), 2);
  assertConsistent(result);
});

test('area, room counts, layer height and plan ratio change their related quantities', () => {
  const input = createDefaultRenovationInput();
  const base = estimateRenovation(input);
  const double = estimateRenovation({ ...input, area: 200 });
  const taller = estimateRenovation({ ...input, height: 4 });
  const longer = estimateRenovation({ ...input, width: 20, depth: 5 });
  const moreBedrooms = estimateRenovation({ ...input, rooms: { ...input.rooms, bedroom: 4 } });
  assert.ok(quantity(double, 'bedroom.floor.tile') > quantity(base, 'bedroom.floor.tile'));
  assert.equal(quantity(taller, 'bedroom.floor.tile'), quantity(base, 'bedroom.floor.tile'));
  assert.ok(quantity(taller, 'bedroom.wall.primer') > quantity(base, 'bedroom.wall.primer'));
  assert.notEqual(quantity(longer, 'bedroom.ceiling.gypsum_line'), quantity(base, 'bedroom.ceiling.gypsum_line'));
  assert.equal(quantity(moreBedrooms, 'bedroom.door.bedroom_door'), 4);
});

test('measured dimensions drive exact floor and wall quantities and area', () => {
  const input = createDefaultRenovationInput();
  input.rooms = { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 };
  input.measurement = { version: 1, mode: 'measured', floors: [{ index: 0, height: null, spaces: [{
    id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 4, width: 3, height: 3,
    doorWidth: 0.9, doorArea: 1.8, windowWidth: 1.5, windowArea: 2.25,
    curtainBoxLength: null, tvBackgroundArea: null, cabinetLength: null,
  }] }] };
  const result = estimateRenovation(input);
  assert.equal(result.input.area, 12);
  assert.equal(quantity(result, 'bedroom.floor.tile'), 12);
  assert.equal(quantity(result, 'bedroom.wall.primer'), 37.95);
  assert.equal(quantity(result, 'bedroom.floor.skirting'), 13.1);
});

test('per-floor areas and heights stay independent', () => {
  const input = createDefaultRenovationInput();
  input.area = 50; input.floorCount = 2; input.rooms = { bedroom: 2, living: 0, kitchen: 0, bathroom: 0, balcony: 0 };
  input.floors = [
    { index: 0, area: 20, rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } },
    { index: 1, area: 30, rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } },
  ];
  input.measurement = { version: 1, mode: 'measured', floors: [
    { index: 0, height: 3, spaces: [{ id: '0.bedroom.0', type: 'bedroom', name: '一层卧室', length: 5, width: 4, height: null, doorWidth: 0, doorArea: 0, windowWidth: 0, windowArea: 0, curtainBoxLength: 0, tvBackgroundArea: null, cabinetLength: null }] },
    { index: 1, height: 4, spaces: [{ id: '1.bedroom.0', type: 'bedroom', name: '二层卧室', length: 6, width: 5, height: null, doorWidth: 0, doorArea: 0, windowWidth: 0, windowArea: 0, curtainBoxLength: 0, tvBackgroundArea: null, cabinetLength: null }] },
  ] };
  const result = estimateRenovation(input);
  assert.equal(result.input.area, 50);
  assert.deepEqual(result.input.floors.map(floor => [floor.area, floor.height]), [[20, null], [30, null]]);
  assert.equal(quantity(result, 'bedroom.wall.primer'), 142);
  assert.equal(quantity(result, 'common.utilities.electric_box'), 4);
});

test('manual overrides and exclusions remain highest priority without mutation', () => {
  const input = createDefaultRenovationInput();
  const before = JSON.stringify(input);
  const changed = estimateRenovation(input, { quantities: { 'bedroom.floor.tile': 31 }, excludedItems: ['living.floor.tile'] });
  const fresh = estimateRenovation(input);
  assert.equal(quantity(changed, 'bedroom.floor.tile'), 31);
  assert.equal(allItems(changed).find(item => item.id === 'living.floor.tile').included, false);
  assert.notEqual(quantity(fresh, 'bedroom.floor.tile'), 31);
  assert.equal(JSON.stringify(input), before);
  assertConsistent(changed);
});

test('zero rooms retain common engineering only', () => {
  const input = createDefaultRenovationInput();
  input.rooms = { bedroom: 0, living: 0, kitchen: 0, bathroom: 0, balcony: 0 };
  const result = estimateRenovation(input, { fees: { management: false, design: false } });
  assert.deepEqual(result.spaces.slice(0, 5).map(space => space.amount), [0, 0, 0, 0, 0]);
  assert.ok(result.spaces[5].amount > 0);
  assert.equal(result.geometrySummary.spaceCount, 0);
});

test('multipliers, fees and invalid selections keep existing behavior', () => {
  const input = createDefaultRenovationInput();
  const quality = estimateRenovation({ ...input, standard: 'quality' }, { fees: { tax: true } });
  assert.equal(quality.fees[2].amount, Math.round(quality.engineeringAmount * 0.034));
  for (const item of allItems(quality)) assert.equal(item.amount, item.included ? Math.round(item.quantity * (item.materialUnitPrice + item.laborAuxUnitPrice)) : 0);
  assert.throws(() => estimateRenovation(input, { quantities: { 'missing.item': 1 } }), /未知装修报价项目/);
  assert.throws(() => estimateRenovation(input, { quantities: { 'bedroom.floor.tile': -1 } }), /工程量/);
});

test('TypeScript source and JavaScript runtime artifact stay synchronized', () => {
  const dir = path.join(__dirname, '../miniprogram/utils');
  const ts = fs.readFileSync(path.join(dir, 'renovation-pricing.ts'), 'utf8');
  const js = fs.readFileSync(path.join(dir, 'renovation-pricing.js'), 'utf8');
  assert.ok(ts.startsWith('// @ts-nocheck\n'));
  assert.equal(ts.slice('// @ts-nocheck\n'.length), js);
  assert.doesNotMatch(ts, /require\(['"]\.\/renovation-pricing\.js['"]\)/);
});

test('automatically derived quantities obey the same bounds as manually entered quantities', () => {
  const input = createDefaultRenovationInput();
  input.measurement = { version: 1, mode: 'measured', floors: [{ index: 0, height: 100, spaces: [
    { id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 1000000, width: 0.0001, height: null },
  ] }] };
  assert.throws(() => estimateRenovation(input), /工程量/);
});
