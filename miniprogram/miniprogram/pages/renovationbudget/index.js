const { cityOptionsMap } = require('../../utils/costRegions');
const { estimateRenovation } = require('../../utils/renovation-pricing');
const { createMeasurementDraft, resolveRenovationGeometry } = require('../../utils/renovation-geometry');
const { getBudgetAccountId, requestBudgetLogin, writeRenovationReport, readRenovationReport, readBuildingPrefill } = require('../../utils/renovation-report');

const provinceOptions = Object.keys(cityOptionsMap);
const roomFields = ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony'];
const roomLabels = { bedroom: '卧室', living: '客餐厅', kitchen: '厨房', bathroom: '卫生间', balcony: '阳台' };
const styleOptions = ['现代简约', '新中式', '奶油风', '轻奢'];
const standardOptions = [
  { id: 'economy', name: '经济型' },
  { id: 'standard', name: '标准型' },
  { id: 'quality', name: '品质型' },
];

function cities(province) { return cityOptionsMap[province] || cityOptionsMap['福建']; }
function numeric(value) {
  const result = Number(String(value === undefined ? '' : value).trim());
  return Number.isFinite(result) ? result : 0;
}
function optionalNumeric(value) {
  if (value === null || value === undefined || String(value).trim() === '') return null;
  const result = Number(String(value).trim());
  return Number.isFinite(result) ? result : NaN;
}
function positiveInteger(value) {
  const result = numeric(value);
  return Number.isInteger(result) && result > 0 && result <= 20 ? result : 0;
}
function clone(value) { return JSON.parse(JSON.stringify(value)); }
function defaultSelections() { return { excludedItems: [], quantities: {}, fees: { management: true, design: true, tax: false } }; }
function makeFloors(count, area, rooms) {
  const floorCount = Math.max(1, count || 1);
  const totalArea = numeric(area);
  const baseArea = totalArea ? Number((totalArea / floorCount).toFixed(2)) : '';
  const floors = [];
  for (let index = 0; index < floorCount; index += 1) {
    const floorRooms = {};
    roomFields.forEach((field) => {
      const total = Math.max(0, Number(rooms[field] || 0));
      floorRooms[field] = Math.floor(total / floorCount) + (index < total % floorCount ? 1 : 0);
    });
    const floorArea = totalArea && index === floorCount - 1 ? Number((totalArea - Number(baseArea) * (floorCount - 1)).toFixed(2)) : baseArea;
    floors.push({ index, name: `${index + 1}层`, area: floorArea, height: null, rooms: floorRooms });
  }
  return floors;
}
function resizeFloorAreas(floors, count, area, rooms) {
  if (!Array.isArray(floors) || floors.length !== count) return makeFloors(count, area, rooms);
  const totalArea = numeric(area); const oldTotal = floors.reduce((sum, floor) => sum + numeric(floor.area), 0); let used = 0;
  return floors.map((floor, index) => {
    if (!totalArea) return { ...clone(floor), area: '' };
    const nextArea = index === count - 1 ? Number((totalArea - used).toFixed(2)) : Number((oldTotal > 0 ? totalArea * numeric(floor.area) / oldTotal : totalArea / count).toFixed(2));
    used += nextArea; return { ...clone(floor), area: nextArea };
  });
}
function measurementStatus(input, summary) {
  if (!input || !input.measurement) return '按房屋参数估算';
  if (input.measurement.mode !== 'measured') return '按房屋参数估算';
  try {
    const resolvedSummary = summary || resolveRenovationGeometry(input).summary;
    return resolvedSummary.estimatedFields && resolvedSummary.estimatedFields.length ? '已按空间尺寸计算 · 含估算项' : '已按空间尺寸计算';
  } catch (error) { return '空间尺寸待补充'; }
}
function restoredData(record) {
  if (!record || !record.input || !record.selections) throw new Error('报价记录不完整');
  const input = record.input; const provinceIndex = provinceOptions.indexOf(input.province); const availableCities = provinceIndex >= 0 ? cities(input.province) : cities('福建');
  const cityIndex = availableCities.indexOf(input.city); const safeProvinceIndex = provinceIndex >= 0 ? provinceIndex : provinceOptions.indexOf('福建'); const safeCityIndex = cityIndex >= 0 ? cityIndex : availableCities.indexOf('泉州');
  const floorCount = positiveInteger(input.floorCount) || 1; const rooms = clone(input.rooms); const floors = Array.isArray(input.floors) && input.floors.length === floorCount ? clone(input.floors) : makeFloors(floorCount, input.area, rooms);
  floors.forEach((floor, index) => { floor.height = input.measurement && input.measurement.floors && input.measurement.floors[index] ? input.measurement.floors[index].height : (floor.height == null ? null : floor.height); });
  return {
    province: provinceOptions[safeProvinceIndex], city: availableCities[Math.max(0, safeCityIndex)], cityOptions: availableCities,
    regionOptions: [provinceOptions, availableCities], regionIndex: [safeProvinceIndex, Math.max(0, safeCityIndex)],
    houseType: input.houseType, area: String(input.area), width: input.width == null ? '' : String(input.width), depth: input.depth == null ? '' : String(input.depth),
    floorCount: String(floorCount), height: String(input.height), rooms, style: input.style, standard: input.standard,
    floors, measurement: input.measurement ? clone(input.measurement) : null, measurementBasisText: measurementStatus(input, record.quantityBasis), selections: clone(record.selections),
  };
}

function rawInput(data, measurement = data.measurement) {
  const floorCount = positiveInteger(data.floorCount);
  const suppliedFloors = Array.isArray(data.floors) ? data.floors : [];
  const floorArea = suppliedFloors.reduce((sum, floor) => sum + numeric(floor.area), 0);
  const floors = suppliedFloors.length === floorCount && suppliedFloors.every((floor) => numeric(floor.area) > 0) && Math.abs(floorArea - numeric(data.area)) <= 0.01
    ? suppliedFloors : makeFloors(floorCount, data.area, data.rooms);
  return {
    province: data.province, city: data.city, area: numeric(data.area),
    width: optionalNumeric(data.width), depth: optionalNumeric(data.depth), floorCount,
    height: numeric(data.height), houseType: data.houseType, standard: data.standard,
    style: data.style, rooms: clone(data.rooms), floors: clone(floors), measurement: measurement ? clone(measurement) : null,
  };
}

function rawFloorsFromResolved(input) {
  const floors = clone(input.floors);
  floors.forEach((floor, index) => {
    const measuredHeight = input.measurement && input.measurement.floors[index] ? input.measurement.floors[index].height : null;
    floor.height = measuredHeight === null || measuredHeight === undefined || measuredHeight === '' ? (floor.height == null ? null : floor.height) : measuredHeight;
  });
  return floors;
}

Page({
  data: {
    __profileAccessMounted: true,
    province: '福建', city: '泉州', provinceOptions,
    cityOptions: cities('福建'), regionOptions: [provinceOptions, cities('福建')], regionIndex: [provinceOptions.indexOf('福建'), cities('福建').indexOf('泉州')],
    houseType: 'new', area: '', width: '', depth: '', floorCount: '1', height: '3.0',
    rooms: { bedroom: 3, living: 2, kitchen: 1, bathroom: 2, balcony: 1 },
    roomFields, roomLabels, style: '现代简约', styleOptions,
    standard: 'standard', standardOptions,
    selections: defaultSelections(),
    floors: makeFloors(1, '', { bedroom: 3, living: 2, kitchen: 1, bathroom: 2, balcony: 1 }),
    floorDrawerVisible: false, measurementDrawerVisible: false, measurementEditorInput: null, measurement: null, measurementBasisText: '按房屋参数估算', activeFloor: 0, generating: false,
  },

  onLoad(options = {}) {
    if (options.restore === 'report') {
      try { this.applyReportRecord(readRenovationReport()); return; }
      catch (error) { console.error('恢复装修报价失败:', error); wx.showToast({ title: '报价恢复失败，请重新填写', icon: 'none' }); }
    }
    try {
      const prefill = readBuildingPrefill();
      if (!prefill) return;
      const provinceIndex = provinceOptions.indexOf(prefill.province);
      const availableCities = provinceIndex >= 0 ? cities(prefill.province) : [];
      const cityIndex = availableCities.indexOf(prefill.city);
      const updates = {};
      if (provinceIndex >= 0 && cityIndex >= 0) Object.assign(updates, {
        province: prefill.province, city: prefill.city, cityOptions: availableCities,
        regionOptions: [provinceOptions, availableCities], regionIndex: [provinceIndex, cityIndex],
      });
      if (numeric(prefill.area) > 0) updates.area = String(prefill.area);
      if (positiveInteger(prefill.floorCount)) updates.floorCount = String(prefill.floorCount);
      if (numeric(prefill.height) > 0) updates.height = String(prefill.height);
      if (Object.keys(updates).length) {
        const count = positiveInteger(updates.floorCount || this.data.floorCount);
        updates.floors = makeFloors(count, updates.area || this.data.area, this.data.rooms);
        this.setData(updates);
      }
    } catch (error) {
      console.warn('带入建房参数失败:', error);
    }
  },

  applyReportRecord(record) { this.setData(restoredData(record)); },

  onProfileAccessGranted() { this.setData({ __profileAccessMounted: true }); },
  onProfileAccessBlocked() { this.setData({ __profileAccessMounted: true }); },
  onRegionColumnChange(e) {
    const column = Number(e.detail.column); const value = Number(e.detail.value);
    const index = this.data.regionIndex.slice(); index[column] = value;
    if (column === 0) {
      const nextCities = cities(provinceOptions[value]);
      this.setData({ cityOptions: nextCities, regionOptions: [provinceOptions, nextCities], regionIndex: [value, 0] });
    } else this.setData({ regionIndex: index });
  },
  onRegionChange(e) {
    const index = e.detail.value || [0, 0];
    const province = provinceOptions[index[0]] || '福建'; const cityOptions = cities(province);
    this.setData({ province, city: cityOptions[index[1]] || cityOptions[0], cityOptions, regionOptions: [provinceOptions, cityOptions], regionIndex: index });
  },
  onHouseTypeTap(e) { this.setData({ houseType: e.currentTarget.dataset.value }); },
  onInput(e) { this.applyStructureUpdate({ [e.currentTarget.dataset.field]: e.detail.value }); },
  onFloorCountInput(e) {
    const floorCount = e.detail.value; const count = positiveInteger(floorCount);
    this.applyStructureUpdate({ floorCount, ...(count ? { floors: makeFloors(count, this.data.area, this.data.rooms) } : {}) });
  },
  onAreaInput(e) {
    const area = e.detail.value; const count = positiveInteger(this.data.floorCount);
    this.applyStructureUpdate({ area, ...(count ? { floors: resizeFloorAreas(this.data.floors, count, area, this.data.rooms) } : {}) });
  },
  onRoomChange(e) {
    const field = e.currentTarget.dataset.field; const delta = Number(e.currentTarget.dataset.delta);
    const current = Number(this.data.rooms[field] || 0); if ((delta > 0 && current >= 50) || (delta < 0 && current <= 0)) return;
    const floors = clone(this.data.floors);
    if (delta > 0 && floors[0]) floors[0].rooms[field] = Number(floors[0].rooms[field] || 0) + 1;
    if (delta < 0) {
      for (let index = floors.length - 1; index >= 0; index -= 1) {
        if (Number(floors[index].rooms[field] || 0) > 0) { floors[index].rooms[field] -= 1; break; }
      }
    }
    const rooms = { ...this.data.rooms, [field]: floors.reduce((sum, floor) => sum + Number(floor.rooms[field] || 0), 0) };
    this.applyStructureUpdate({ rooms, floors });
  },
  onStyleTap(e) { this.setData({ style: e.currentTarget.dataset.value }); },
  onStandardTap(e) { this.setData({ standard: e.currentTarget.dataset.value }); },
  applyStructureUpdate(updates) {
    const next = { ...this.data, ...updates };
    if (this.data.measurement) {
      try {
        updates.measurement = createMeasurementDraft(rawInput(next, this.data.measurement), this.data.measurement);
        if (Array.isArray(updates.floors)) updates.floors.forEach((floor, index) => {
          const measurementFloor = updates.measurement.floors[index];
          if (measurementFloor && measurementFloor.height !== null && measurementFloor.height !== undefined && measurementFloor.height !== '') floor.height = measurementFloor.height;
        });
        updates.measurementBasisText = measurementStatus(rawInput({ ...next, ...updates }, updates.measurement));
      }
      catch (error) { console.warn('同步空间尺寸结构失败:', error); }
    }
    this.setData(updates);
  },
  openMeasurementDrawer() {
    if (!this.data.province || !this.data.city) { wx.showToast({ title: '请选择所在地区', icon: 'none' }); return; }
    if (optionalNumeric(this.data.width) !== null && !(optionalNumeric(this.data.width) > 0)) { wx.showToast({ title: '面宽需填写大于0的数字', icon: 'none' }); return; }
    if (optionalNumeric(this.data.depth) !== null && !(optionalNumeric(this.data.depth) > 0)) { wx.showToast({ title: '进深需填写大于0的数字', icon: 'none' }); return; }
    if (!positiveInteger(this.data.floorCount)) { wx.showToast({ title: '楼层数请填写1至20的整数', icon: 'none' }); return; }
    if (!(numeric(this.data.height) > 0)) { wx.showToast({ title: '请填写层高', icon: 'none' }); return; }
    this.setData({ measurementDrawerVisible: true, measurementEditorInput: rawInput(this.data) });
  },
  closeMeasurementDrawer() { this.setData({ measurementDrawerVisible: false }); },
  onMeasurementConfirm(e) {
    const input = e.detail.input;
    this.setData({
      measurementDrawerVisible: false, measurement: clone(input.measurement), area: String(input.area),
      width: input.width == null ? '' : String(input.width), depth: input.depth == null ? '' : String(input.depth),
      floorCount: String(input.floorCount), height: String(input.height), rooms: clone(input.rooms), floors: rawFloorsFromResolved(input), measurementBasisText: measurementStatus(input, e.detail.summary),
    });
  },
  openFloorDrawer() {
    const count = positiveInteger(this.data.floorCount);
    if (!count) { wx.showToast({ title: '请先填写1至20层', icon: 'none' }); return; }
    this.floorDrawerSnapshot = clone(this.data.floors);
    this.setData({ floorDrawerVisible: true, activeFloor: 0 });
  },
  closeFloorDrawer() {
    const floors = this.floorDrawerSnapshot ? clone(this.floorDrawerSnapshot) : this.data.floors;
    this.floorDrawerSnapshot = null; this.setData({ floorDrawerVisible: false, floors });
  },
  stopPropagation() {},
  selectFloor(e) { this.setData({ activeFloor: Number(e.currentTarget.dataset.index) }); },
  onFloorAreaInput(e) {
    if (this.data.measurement && this.data.measurement.mode === 'measured') return;
    const floors = clone(this.data.floors); floors[this.data.activeFloor].area = e.detail.value; this.setData({ floors });
  },
  onFloorHeightInput(e) {
    const floors = clone(this.data.floors); floors[this.data.activeFloor].height = e.detail.value; this.setData({ floors });
  },
  onFloorRoomChange(e) {
    const floors = clone(this.data.floors); const field = e.currentTarget.dataset.field; const delta = Number(e.currentTarget.dataset.delta);
    const floor = floors[this.data.activeFloor]; floor.rooms[field] = Math.max(0, Math.min(50, Number(floor.rooms[field] || 0) + delta));
    this.setData({ floors });
  },
  confirmFloors() {
    const floors = this.data.floors; const totalArea = floors.reduce((sum, floor) => sum + numeric(floor.area), 0);
    if (floors.some((floor) => !(numeric(floor.area) > 0))) {
      wx.showToast({ title: '每层面积都需大于0', icon: 'none' }); return;
    }
    if (floors.some((floor) => optionalNumeric(floor.height) !== null && !(optionalNumeric(floor.height) > 0))) {
      wx.showToast({ title: '本层层高需大于0', icon: 'none' }); return;
    }
    if (!totalArea || Math.abs(totalArea - numeric(this.data.area)) > 0.01) {
      wx.showToast({ title: '分层面积合计需与装修面积一致', icon: 'none' }); return;
    }
    const rooms = { bedroom: 0, living: 0, kitchen: 0, bathroom: 0, balcony: 0 };
    floors.forEach((floor) => roomFields.forEach((field) => { rooms[field] += Number(floor.rooms[field] || 0); }));
    const overLimit = roomFields.find((field) => rooms[field] > 50);
    if (overLimit) { wx.showToast({ title: `${roomLabels[overLimit]}合计不能超过50间`, icon: 'none' }); return; }
    const roomTotal = roomFields.reduce((sum, field) => sum + rooms[field], 0);
    const emptyFloor = roomTotal ? floors.findIndex((floor) => roomFields.every((field) => Number(floor.rooms[field] || 0) === 0)) : -1;
    if (emptyFloor >= 0) { wx.showToast({ title: `第${emptyFloor + 1}层请至少设置一个空间`, icon: 'none' }); return; }
    const normalizedFloors = clone(floors); normalizedFloors.forEach((floor) => { floor.height = optionalNumeric(floor.height); });
    const next = { ...this.data, rooms, floors: normalizedFloors };
    const measurement = createMeasurementDraft(rawInput(next, this.data.measurement), this.data.measurement || null);
    measurement.floors.forEach((floor, index) => { floor.height = normalizedFloors[index].height; });
    this.floorDrawerSnapshot = null; this.setData({ rooms, floors: normalizedFloors, measurement, measurementBasisText: measurementStatus(rawInput(next, measurement)), floorDrawerVisible: false });
  },
  validate() {
    if (!this.data.province || !this.data.city) return '请选择所在地区';
    if (!(numeric(this.data.area) > 0)) return '请填写装修面积';
    if (optionalNumeric(this.data.width) !== null && !(optionalNumeric(this.data.width) > 0)) return '面宽需填写大于0的数字';
    if (optionalNumeric(this.data.depth) !== null && !(optionalNumeric(this.data.depth) > 0)) return '进深需填写大于0的数字';
    if (!positiveInteger(this.data.floorCount)) return '楼层数请填写1至20的整数';
    if (!(numeric(this.data.height) > 0)) return '请填写层高';
    return '';
  },
  buildInput() {
    const base = rawInput(this.data);
    const measurement = createMeasurementDraft(base, this.data.measurement || null);
    return resolveRenovationGeometry({ ...base, measurement }).input;
  },
  async onGenerateReport() {
    if (this.data.generating) return;
    const error = this.validate(); if (error) { wx.showToast({ title: error, icon: 'none' }); return; }
    if (!getBudgetAccountId()) { requestBudgetLogin(); return; }
    this.setData({ generating: true });
    try {
      const selections = clone(this.data.selections || defaultSelections());
      const input = this.buildInput();
      const estimate = estimateRenovation(input, selections);
      this.setData({ measurement: clone(input.measurement), measurementBasisText: measurementStatus(input, estimate.geometrySummary), area: String(input.area), floorCount: String(input.floorCount), rooms: clone(input.rooms), floors: rawFloorsFromResolved(input) });
      await writeRenovationReport(estimate, selections);
      wx.navigateTo({ url: '/pages/renovationbudget/result', fail: () => wx.showToast({ title: '报价报告打开失败', icon: 'none' }) });
    } catch (error) {
      console.error('生成装修报价失败:', error);
      wx.showToast({ title: '报价生成失败，请重试', icon: 'none' });
    } finally { this.setData({ generating: false }); }
  },
});
