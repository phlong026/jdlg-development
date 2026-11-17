const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
const pricing = require('../miniprogram/utils/renovation-pricing');
const costRegions = require('../miniprogram/utils/costRegions');

const root = path.join(__dirname, '../miniprogram/pages/renovationbudget');

function loadPage(name, overrides = {}) {
  let definition;
  const writes = [];
  const navigations = [];
  const toasts = [];
  const reportHelpers = {
    getBudgetAccountId: () => 9,
    requestBudgetLogin: () => {},
    readBuildingPrefill: () => null,
    writeRenovationReport: async (estimate, selections) => {
      writes.push({ estimate, selections });
      return { input: estimate.input, selections, report: { version: 5, quoteType: 'renovation' } };
    },
    readRenovationReport: () => { throw new Error('missing'); },
    estimateToSnapshot: estimate => ({ version: 5, quoteType: 'renovation', renovation: estimate }),
    ...overrides.reportHelpers,
  };
  const code = ts.transpileModule(fs.readFileSync(path.join(root, `${name}.ts`), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText;
  vm.runInNewContext(code, {
    exports: {},
    Page(value) { definition = value; },
    require(request) {
      if (request === '../../utils/renovation-pricing') return pricing;
      if (request === '../../utils/renovation-report') return reportHelpers;
      if (request === '../../utils/costRegions') return costRegions;
      if (request === '../../utils/costEstimateShare') return { createCostEstimateShare: async () => ({ report_id: 'public-1' }) };
      if (request === '../../utils/enterpriseWechat') return {
        DEFAULT_CUSTOMER_SERVICE_CORP_ID: 'corp', DEFAULT_CUSTOMER_SERVICE_URL: 'url',
        tryOpenCustomerServiceDirect: async () => true,
      };
      throw new Error(`unexpected import ${request}`);
    },
    wx: {
      showToast(args) { toasts.push(args.title); }, showModal() {},
      navigateTo(args) { navigations.push(args.url); },
      redirectTo(args) { navigations.push(args.url); },
      navigateBack(args = {}) { navigations.push(`back:${args.delta || 1}`); }, getStorageSync() { return 'token'; },
    },
    getCurrentPages: overrides.getCurrentPages || (() => []),
    console,
    setTimeout,
  });
  const page = {
    ...definition,
    data: { ...definition.data },
    setData(values) { Object.assign(this.data, values); },
    selectComponent() { return null; },
  };
  return { page, writes, navigations, toasts, reportHelpers };
}

test('renovation form keeps price fields blank until the user supplies them', () => {
  const { page } = loadPage('index');
  assert.equal(page.data.area, '');
  assert.equal(page.data.width, '');
  assert.equal(page.data.depth, '');
  assert.equal(page.data.houseType, 'new');
  assert.equal(page.data.standard, 'standard');
  assert.equal(page.data.provinceOptions[page.data.regionIndex[0]], '福建');
  assert.equal(page.data.regionOptions[1][page.data.regionIndex[1]], '泉州');
});

test('blank optional width and depth are recorded as null and do not block generation', async () => {
  const { page, writes } = loadPage('index');
  Object.assign(page.data, { area: '100', width: '', depth: '', floorCount: '1', height: '3' });
  await page.onGenerateReport();
  assert.equal(writes.length, 1);
  assert.equal(writes[0].estimate.input.width, null);
  assert.equal(writes[0].estimate.input.depth, null);
});

test('three floor areas keep the exact entered total by assigning the rounding remainder to the last floor', () => {
  const { page } = loadPage('index');
  page.data.floorCount = '3';
  page.onAreaInput({ detail: { value: '100' } });
  assert.equal(page.data.floors.reduce((sum, floor) => sum + Number(floor.area), 0), 100);
  assert.deepEqual(Array.from(page.data.floors, floor => floor.area), [33.33, 33.33, 33.34]);
});

test('floor confirmation rejects a zero-area floor even when the overall sum matches', () => {
  const { page, toasts } = loadPage('index');
  page.data.area = '50'; page.data.floorCount = '2'; page.data.floorDrawerVisible = true;
  page.data.floors = [{ index: 0, name: '1层', area: 50, rooms: { bedroom: 1, living: 1, kitchen: 1, bathroom: 1, balcony: 0 } }, { index: 1, name: '2层', area: 0, rooms: { bedroom: 1, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } }];
  page.confirmFloors();
  assert.equal(page.data.floorDrawerVisible, true);
  assert.match(toasts[0], /每层面积/);
});

test('global room decrement updates the floor that actually owns the room count', () => {
  const { page } = loadPage('index');
  page.data.rooms = { bedroom: 3, living: 1, kitchen: 1, bathroom: 1, balcony: 0 };
  page.data.floors = [{ index: 0, name: '1层', area: 50, rooms: { bedroom: 0, living: 1, kitchen: 1, bathroom: 1, balcony: 0 } }, { index: 1, name: '2层', area: 50, rooms: { bedroom: 3, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } }];
  page.onRoomChange({ currentTarget: { dataset: { field: 'bedroom', delta: '-1' } } });
  assert.equal(page.data.rooms.bedroom, 2);
  assert.equal(page.data.floors[1].rooms.bedroom, 2);
  assert.equal(page.data.floors.reduce((sum, floor) => sum + floor.rooms.bedroom, 0), 2);
});

test('closing floor settings discards the unconfirmed floor draft', () => {
  const { page } = loadPage('index');
  page.data.area = '100'; page.data.floorCount = '2'; page.data.floors = [{ index: 0, name: '1层', area: 50, rooms: { bedroom: 2, living: 1, kitchen: 1, bathroom: 1, balcony: 0 } }, { index: 1, name: '2层', area: 50, rooms: { bedroom: 1, living: 1, kitchen: 0, bathroom: 1, balcony: 1 } }];
  page.openFloorDrawer();
  page.onFloorAreaInput({ detail: { value: '80' } });
  page.closeFloorDrawer();
  assert.deepEqual(Array.from(page.data.floors, floor => Number(floor.area)), [50, 50]);
});

test('valid form estimates, writes the account report, then opens the private result', async () => {
  const { page, writes, navigations } = loadPage('index');
  Object.assign(page.data, { area: '100', width: '10', depth: '10', floorCount: '1', height: '3' });
  await page.onGenerateReport();
  assert.equal(writes.length, 1);
  assert.equal(writes[0].estimate.totalAmount > 0, true);
  assert.equal(writes[0].estimate.input.area, 100);
  assert.deepEqual(navigations, ['/pages/renovationbudget/result']);
});

test('form uses the existing login prompt and does not calculate for an anonymous account', async () => {
  let prompted = 0;
  const { page, writes } = loadPage('index', { reportHelpers: {
    getBudgetAccountId: () => 0,
    requestBudgetLogin: () => { prompted += 1; },
  } });
  Object.assign(page.data, { area: '100', width: '10', depth: '10', floorCount: '1', height: '3' });
  await page.onGenerateReport();
  assert.equal(prompted, 1);
  assert.equal(writes.length, 0);
});

test('private result recalculates item quantity and persists a new private record', async () => {
  const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput(), {
    excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false },
  });
  const writes = [];
  const record = {
    input: estimate.input,
    selections: { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } },
    report: { version: 5, quoteType: 'renovation', renovation: estimate },
  };
  const loaded = loadPage('result', { reportHelpers: {
    readRenovationReport: () => record,
    writeRenovationReport: async (nextEstimate, selections) => {
      writes.push({ nextEstimate, selections });
      return { ...record, selections, report: { ...record.report, renovation: nextEstimate } };
    },
  } });
  loaded.page.onLoad();
  await loaded.page.applyItemEdit({ id: 'bedroom.ceiling.primer', quantity: 25, included: false });
  assert.equal(writes.length, 1);
  assert.equal(writes[0].selections.quantities['bedroom.ceiling.primer'], 25);
  assert.equal(writes[0].selections.excludedItems.includes('bedroom.ceiling.primer'), true);
});

test('result parameter update keeps optional width and depth blank', async () => {
  const estimate = pricing.estimateRenovation({ ...pricing.createDefaultRenovationInput(), width: null, depth: null }, {
    excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false },
  });
  const writes = [];
  const record = { input: estimate.input, selections: { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } }, report: { version: 5, quoteType: 'renovation', renovation: estimate } };
  const { page } = loadPage('result', { reportHelpers: {
    readRenovationReport: () => record,
    writeRenovationReport: async (nextEstimate, selections) => {
      writes.push(nextEstimate); return { ...record, input: nextEstimate.input, selections, report: { ...record.report, renovation: nextEstimate } };
    },
  } });
  page.onLoad();
  page.onParametersEdit();
  page.data.parameterDraft = { ...page.data.parameterDraft, area: '100', width: '', depth: '', floorCount: '1', height: '3' };
  await page.confirmParameters();
  assert.equal(writes.length, 1);
  assert.equal(writes[0].input.width, null);
  assert.equal(writes[0].input.depth, null);
});

test('missing fee selections preserve the approved management and design defaults with tax off', () => {
  const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput(), {
    excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false },
  });
  const record = { input: estimate.input, selections: { excludedItems: [], quantities: {}, fees: {} }, report: { version: 5, quoteType: 'renovation', renovation: estimate } };
  const { page } = loadPage('result', { reportHelpers: { readRenovationReport: () => record } });
  page.onLoad();
  assert.deepEqual(Array.from(page.data.feeRows, fee => [fee.id, fee.included]), [['management', true], ['design', true], ['tax', false]]);
});

test('quote type switch moves only between the two independent quote forms', () => {
  let definition; const navigations = []; let pages = [{ route: 'pages/aicost/aicost' }];
  const source = fs.readFileSync(path.join(__dirname, '../miniprogram/components/quote-type-switch/index.js'), 'utf8');
  vm.runInNewContext(source, {
    Component(value) { definition = value; },
    getCurrentPages() { return pages; },
    wx: {
      navigateBack(args) { navigations.push(`back:${args.delta}`); },
      redirectTo(args) { navigations.push(`redirect:${args.url}`); },
      navigateTo(args) { navigations.push(`push:${args.url}`); },
      showToast() {},
    },
  });
  const component = { ...definition.methods, data: { ...definition.data, kind: 'building' }, setData(values) { Object.assign(this.data, values); } };
  component.select({ currentTarget: { dataset: { kind: 'renovation' } } });
  assert.deepEqual(navigations, ['push:/pages/renovationbudget/index']);
  pages = [{ route: 'pages/aicost/aicost' }, { route: 'pages/renovationbudget/index' }];
  component.data.kind = 'renovation';
  component.select({ currentTarget: { dataset: { kind: 'building' } } });
  assert.equal(navigations[1], 'back:1');
  const buildingMarkup = fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aicost/aicost.wxml'), 'utf8');
  assert.match(buildingMarkup, /<quote-type-switch kind="building"\s*\/>/);
});

test('quote type switch redirects only when a full page stack rejects a push', () => {
  let definition; const calls = [];
  const source = fs.readFileSync(path.join(__dirname, '../miniprogram/components/quote-type-switch/index.js'), 'utf8');
  vm.runInNewContext(source, {
    Component(value) { definition = value; },
    getCurrentPages() { return Array.from({ length: 10 }, (_, index) => ({ route: `pages/other/${index}` })); },
    wx: {
      navigateTo(args) { calls.push('push'); args.fail({ errMsg: 'navigateTo:fail page stack limit exceeded' }); },
      redirectTo(args) { calls.push(`redirect:${args.url}`); }, navigateBack() {}, showToast() {},
    },
  });
  const component = { ...definition.methods, data: { ...definition.data, kind: 'building' }, setData(values) { Object.assign(this.data, values); } };
  component.select({ currentTarget: { dataset: { kind: 'renovation' } } });
  assert.deepEqual(calls, ['push', 'redirect:/pages/renovationbudget/index']);
});

test('returning from result synchronizes edited input and preserves selections on regeneration', async () => {
  const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput(), { excludedItems: ['bedroom.ceiling.primer'], quantities: { 'bedroom.ceiling.primer': 20 }, fees: { management: true, design: false, tax: false } });
  const record = { input: estimate.input, selections: { excludedItems: ['bedroom.ceiling.primer'], quantities: { 'bedroom.ceiling.primer': 20 }, fees: { management: true, design: false, tax: false } }, report: { version: 5, quoteType: 'renovation', renovation: estimate } };
  const formLoaded = loadPage('index', { reportHelpers: { readRenovationReport: () => record } }); const form = formLoaded.page;
  const result = loadPage('result', { reportHelpers: { readRenovationReport: () => record }, getCurrentPages: () => [form, { route: 'pages/renovationbudget/result' }] });
  form.route = 'pages/renovationbudget/index';
  result.page.onLoad(); result.page.onReturnModify();
  assert.equal(form.data.area, '100');
  assert.equal(form.data.selections.quantities['bedroom.ceiling.primer'], 20);
  assert.equal(form.data.selections.fees.design, false);
  assert.equal(result.navigations[0], 'back:1');
  await form.onGenerateReport();
  assert.equal(formLoaded.writes[0].selections.quantities['bedroom.ceiling.primer'], 20);
  assert.equal(formLoaded.writes[0].selections.fees.design, false);
});

test('explicit cold restore reads the private report while ordinary form load never consumes it', () => {
  const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput(), { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } });
  const record = { input: estimate.input, selections: { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } }, report: { version: 5, quoteType: 'renovation', renovation: estimate } };
  let reads = 0;
  const ordinary = loadPage('index', { reportHelpers: { readRenovationReport() { reads += 1; return record; } } }).page;
  ordinary.onLoad(); assert.equal(reads, 0);
  const restored = loadPage('index', { reportHelpers: { readRenovationReport() { reads += 1; return record; } } }).page;
  restored.onLoad({ restore: 'report' });
  assert.equal(reads, 1); assert.equal(restored.data.area, '100');
});

test('result without a form in the stack opens an explicitly restored form', () => {
  const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput(), { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } });
  const record = { input: estimate.input, selections: { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } }, report: { version: 5, quoteType: 'renovation', renovation: estimate } };
  const result = loadPage('result', { reportHelpers: { readRenovationReport: () => record }, getCurrentPages: () => [{ route: 'pages/renovationbudget/result' }] });
  result.page.onLoad(); result.page.onReturnModify();
  assert.equal(result.navigations[0], '/pages/renovationbudget/index?restore=report');
});

test('report component preserves a legitimate zero total and readonly suppresses every edit event', () => {
  const source = fs.readFileSync(path.join(__dirname, '../miniprogram/components/renovation-report/index.js'), 'utf8');
  assert.match(source, /report:\s*\{\s*type:\s*Object/);
  assert.match(source, /readonly:\s*\{\s*type:\s*Boolean/);
  assert.match(source, /triggerEvent\(['"]itemedit['"]/);
  assert.match(source, /triggerEvent\(['"]feesedit['"]/);
  assert.match(source, /triggerEvent\(['"]parametersedit['"]/);
  let definition;
  vm.runInNewContext(source, { Component(value) { definition = value; } });
  const events = [];
  const component = {
    ...definition.methods,
    data: { ...definition.data, readonly: true, report: { estimatedTotalCost: 'not-a-number', estimatedUnitCost: '0.00元/㎡', basicInfo: Array.from({ length: 10 }, (_, index) => ({ label: `L${index}`, value: `V${index}` })), costBreakdown: [], renovation: { totalAmount: 0, spaces: [] } } },
    setData(values) { Object.assign(this.data, values); }, triggerEvent(name, detail) { events.push({ name, detail }); },
  };
  component.refresh();
  assert.equal(component.data.totalWan, '0.00');
  assert.equal(component.data.totalAmountText, '¥0');
  assert.equal(component.data.basicInfo.length, 10);
  const markup = fs.readFileSync(path.join(__dirname, '../miniprogram/components/renovation-report/index.wxml'), 'utf8');
  assert.match(markup, /readonly && basicInfo\.length/);
  assert.match(markup, /房屋信息/);
  component.onItemTap({ currentTarget: { dataset: { item: { id: 'x' } } } }); component.onFeesTap(); component.onParametersTap();
  assert.deepEqual(events, []);
  component.data.readonly = false; component.onItemTap({ currentTarget: { dataset: { item: { id: 'x' } } } }); component.onFeesTap(); component.onParametersTap();
  assert.deepEqual(events.map(event => event.name), ['itemedit', 'feesedit', 'parametersedit']);

  const input = { ...pricing.createDefaultRenovationInput(), houseType: 'old', standard: 'quality' };
  const oldQuality = pricing.estimateRenovation(input, { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } });
  component.data.report = { estimatedUnitCost: '0元/㎡', basicInfo: [], costBreakdown: [], renovation: oldQuality };
  component.refresh();
  const renderedItem = component.data.viewSpaces[0].groups[0].items[0];
  const sourceItem = oldQuality.spaces[0].groups[0].items[0];
  assert.equal(renderedItem.unitPriceText, Number((sourceItem.materialUnitPrice + sourceItem.laborAuxUnitPrice).toFixed(6)));
  assert.match(String(renderedItem.unitPriceText), /^\d+(?:\.\d{1,6})?$/);
});

test('report starts with compact groups and the detail button opens the selected space without recalculating', () => {
  let definition;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/components/renovation-report/index.js'), 'utf8'), {
    Component(value) { definition = value; },
  });
  const estimate = pricing.estimateRenovation(pricing.createDefaultRenovationInput());
  const component = { ...definition.methods, data: { ...definition.data, detailsExpanded: false,
    report: { costBreakdown: [], renovation: estimate }, activeSpace: 2 }, setData(values) { Object.assign(this.data, values); } };
  component.refresh();
  assert.equal(component.data.viewSpaces.flatMap(space => space.groups).some(group => group.expanded), false);
  component.data.detailsExpanded = true;
  definition.properties.detailsExpanded.observer.call(component);
  assert.equal(component.data.activeSpace, 2);
  assert.equal(component.data.viewSpaces[2].groups[0].expanded, true);
  assert.equal(component.data.viewSpaces[0].groups[0].expanded, false);
  const { page } = loadPage('result');
  page.toggleCostDetails(); assert.equal(page.data.showCostDetails, true);
  page.toggleCostDetails(); assert.equal(page.data.showCostDetails, false);
  const markup = fs.readFileSync(path.join(root, 'result.wxml'), 'utf8');
  assert.match(markup, /details-expanded="\{\{showCostDetails\}\}"/);
  assert.match(markup, /custom-back="\{\{true\}\}" bind:back="onReturnModify"/);
});

test('private result native sharing only shares the blank form entry and never publishes its prices', () => {
  const { page } = loadPage('result');
  page.data.record = { report: { estimatedTotalCost: '9.78万元' } };
  const share = page.onShareAppMessage();
  assert.equal(share.path, '/pages/renovationbudget/index');
  assert.equal(JSON.stringify(share).includes('9.78'), false);
  assert.equal(typeof page.onShareTimeline, 'undefined');
});

test('floor confirmation rejects a room total above the pricing limit before generation', () => {
  const { page } = loadPage('index');
  page.data.area = '100'; page.data.floorCount = '2'; page.data.floorDrawerVisible = true;
  page.data.floors = [0, 1].map(index => ({ index, area: 50, rooms: { bedroom: 30, living: 0, kitchen: 0, bathroom: 0, balcony: 0 } }));
  page.confirmFloors();
  assert.equal(page.data.floorDrawerVisible, true);
  assert.equal(page.data.rooms.bedroom, 3);
});

test('the result standard selector recalculates while preserving selected quantities and fees', async () => {
  const { page, writes } = loadPage('result');
  const input = pricing.createDefaultRenovationInput();
  const selections = { excludedItems: ['bedroom.ceiling.primer'], quantities: { 'bedroom.ceiling.primer': 20 }, fees: { management: true, design: false, tax: false } };
  page.data.record = { input, selections, report: {} };
  await page.onStandardChange({ detail: { value: '2' } });
  assert.equal(writes[0].estimate.input.standard, 'quality');
  assert.equal(writes[0].selections.fees.design, false);
  assert.equal(writes[0].selections.quantities['bedroom.ceiling.primer'], 20);
});
