const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { createMeasurementDraft, resolveRenovationGeometry } = require('../miniprogram/utils/renovation-geometry');

function measuredInput(space, extra = {}) {
  return {
    area: 99, width: 10, depth: 9, floorCount: 1, height: 3,
    rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 },
    measurement: { version: 1, mode: 'measured', floors: [{ index: 0, height: null, spaces: [space] }] },
    ...extra,
  };
}

test('measured geometry uses actual dimensions, openings and derives area', () => {
  const result = resolveRenovationGeometry(measuredInput({
    id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 4, width: 3, height: 3,
    doorWidth: 0.9, doorArea: 1.8, windowWidth: 1.5, windowArea: 2.25,
    curtainBoxLength: null, tvBackgroundArea: null, cabinetLength: null,
  }));
  const bedroom = result.metrics.byType.bedroom;
  assert.deepEqual([bedroom.floorArea, bedroom.ceilingArea, bedroom.perimeter, bedroom.wallArea], [12, 12, 14, 37.95]);
  assert.equal(bedroom.skirtingLength, 13.1);
  assert.equal(result.input.area, 12);
  assert.equal(result.input.floors[0].area, 12);
  assert.equal(result.summary.mode, 'measured');
});

test('measured floor height is inherited per floor and dimensions are immutable', () => {
  const source = measuredInput({
    id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 4, width: 3, height: null,
    doorWidth: null, doorArea: null, windowWidth: null, windowArea: null,
    curtainBoxLength: null, tvBackgroundArea: null, cabinetLength: null,
  });
  source.measurement.floors[0].height = 3.2;
  const result = resolveRenovationGeometry(source);
  assert.equal(result.metrics.byType.bedroom.wallArea, 44.8);
  assert.equal(source.measurement.floors[0].spaces[0].height, null);
});

test('draft preserves user-entered values by stable id without filling estimates', () => {
  const input = { area: 40, floorCount: 2, rooms: { bedroom: 2, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } };
  const previous = createMeasurementDraft(input);
  previous.floors[0].spaces[0].length = 4;
  const next = createMeasurementDraft(input, previous);
  assert.equal(next.floors[0].spaces[0].length, 4);
  assert.equal(next.floors[1].spaces[0].length, null);
});

test('draft can be opened before total area is entered and measured area replaces stale area', () => {
  const draft = createMeasurementDraft({
    area: 0, floorCount: 1,
    rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 },
  });
  assert.equal(draft.floors[0].spaces[0].id, '0.bedroom.0');
  const input = measuredInput({
    id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 4, width: 3, height: 3,
    doorWidth: 0, doorArea: 0, windowWidth: 0, windowArea: 0,
    curtainBoxLength: 0, tvBackgroundArea: null, cabinetLength: null,
  }, { area: 0 });
  assert.equal(resolveRenovationGeometry(input).input.area, 12);
});

test('measured floor count is derived from measurement floors', () => {
  const space = (floor, length) => ({
    id: `${floor}.bedroom.0`, type: 'bedroom', name: `卧室${floor + 1}`, length, width: 3, height: 3,
    doorWidth: 0, doorArea: 0, windowWidth: 0, windowArea: 0,
    curtainBoxLength: 0, tvBackgroundArea: null, cabinetLength: null,
  });
  const input = measuredInput(space(0, 4));
  input.floorCount = 1;
  input.measurement.floors.push({ index: 1, height: null, spaces: [space(1, 5)] });
  const result = resolveRenovationGeometry(input);
  assert.equal(result.input.floorCount, 2);
  assert.equal(result.input.area, 27);
});

test('estimated geometry allocates exact floor area and responds to shape ratio', () => {
  const base = { area: 60, width: 10, depth: 6, floorCount: 1, height: 3, rooms: { bedroom: 2, living: 1, kitchen: 0, bathroom: 0, balcony: 0 } };
  const first = resolveRenovationGeometry(base);
  const doubled = resolveRenovationGeometry({ ...base, area: 120 });
  const square = resolveRenovationGeometry({ ...base, width: 10, depth: 10 });
  assert.equal(first.metrics.byType.bedroom.floorArea + first.metrics.byType.living.floorArea, 60);
  assert.equal(doubled.metrics.byType.bedroom.floorArea + doubled.metrics.byType.living.floorArea, 120);
  assert.notEqual(first.metrics.byType.bedroom.perimeter, square.metrics.byType.bedroom.perimeter);
  assert.ok(first.summary.estimatedFields.length > 0);
});

test('estimated floors inherit configured per-floor heights without materializing inherited values', () => {
  const input = {
    area: 40, floorCount: 2, height: 3,
    rooms: { bedroom: 2, living: 0, kitchen: 0, bathroom: 0, balcony: 0 },
    floors: [
      { index: 0, area: 20, height: null, rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } },
      { index: 1, area: 20, height: 4, rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } },
    ],
  };
  const result = resolveRenovationGeometry(input);
  assert.equal(result.metrics.byType.bedroom.wallArea > 100, true);
  assert.deepEqual(result.input.floors.map(floor => floor.height), [null, 4]);
  assert.deepEqual(result.input.measurement.floors.map(floor => floor.height), [null, null]);
});

test('zero rooms allow common-only geometry and invalid measured openings reject', () => {
  const empty = resolveRenovationGeometry({ area: 50, floorCount: 1, height: 3, rooms: { bedroom: 0, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } });
  assert.equal(empty.summary.spaceCount, 0);
  assert.equal(empty.summary.area, 50);
  assert.throws(() => resolveRenovationGeometry(measuredInput({
    id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 2, width: 2, height: 3,
    doorWidth: 9, doorArea: 30, windowWidth: null, windowArea: null,
    curtainBoxLength: null, tvBackgroundArea: null, cabinetLength: null,
  })), /门宽|洞口/);
  assert.throws(() => resolveRenovationGeometry({ area: 50, floorCount: 1.5, rooms: {} }), /层数/);
  assert.throws(() => resolveRenovationGeometry({ area: 50, floorCount: 1, rooms: [], floors: {} }), /楼层信息|房间数量/);
});

test('TypeScript and JavaScript geometry artifacts stay synchronized', () => {
  const dir = path.join(__dirname, '../miniprogram/utils');
  const ts = fs.readFileSync(path.join(dir, 'renovation-geometry.ts'), 'utf8');
  const js = fs.readFileSync(path.join(dir, 'renovation-geometry.js'), 'utf8');
  assert.ok(ts.startsWith('// @ts-nocheck\n'));
  assert.equal(ts.slice('// @ts-nocheck\n'.length), js);
});

test('each floor estimates its own first living-room feature wall', () => {
  const input = measuredInput({ id: '0.living.0', type: 'living', name: '一层客厅', length: 4, width: 3, height: null });
  input.measurement.floors = [
    { index: 0, height: 3, spaces: [{ id: '0.living.0', type: 'living', name: '一层客厅', length: 4, width: 3, height: null }] },
    { index: 1, height: 4, spaces: [{ id: '1.living.0', type: 'living', name: '二层客厅', length: 4, width: 3, height: null }] },
  ];
  assert.equal(resolveRenovationGeometry(input).metrics.byType.living.tvBackgroundArea, 21);
});

test('measurement confirmation rejects out-of-range geometry before it reaches pricing', () => {
  assert.throws(() => resolveRenovationGeometry(measuredInput({ id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 1000000, width: 0.0001, height: 100 })), /工程量/);
  assert.throws(() => resolveRenovationGeometry(measuredInput({ id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 1000, width: 1000, height: 3 })), /面积/);
  assert.throws(() => resolveRenovationGeometry(measuredInput({ id: '0.bedroom.0', type: 'bedroom', name: '卧室1', length: 4, width: 3, height: 101 })), /层高/);
  assert.throws(() => createMeasurementDraft({ area: 100, floorCount: 1000000000, rooms: { bedroom: 1 } }), /层数/);
});
