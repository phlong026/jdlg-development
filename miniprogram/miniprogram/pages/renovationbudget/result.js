const { estimateRenovation } = require('../../utils/renovation-pricing');
const { createMeasurementDraft, resolveRenovationGeometry } = require('../../utils/renovation-geometry');
const { readRenovationReport, writeRenovationReport } = require('../../utils/renovation-report');
const { createCostEstimateShare } = require('../../utils/costEstimateShare');
const { DEFAULT_CUSTOMER_SERVICE_CORP_ID, DEFAULT_CUSTOMER_SERVICE_URL, tryOpenCustomerServiceDirect } = require('../../utils/enterpriseWechat');
const roomFields = ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony'];

function clone(value) { return JSON.parse(JSON.stringify(value)); }
function errorMessage(error, fallback) { return String(error && error.message || '').trim() || fallback; }
function modal(options) { return new Promise((resolve) => wx.showModal({ ...options, success: resolve, fail: () => resolve({ confirm: false }) })); }
function feeRows(fees) {
  const names = { management: '管理费', design: '设计费', tax: '税费' };
  const rates = { management: '10%', design: '4%', tax: '3.4%' };
  const defaults = { management: true, design: true, tax: false }; const selected = { ...defaults, ...(fees || {}) };
  return Object.keys(names).map((id) => ({ id, name: names[id], rate: rates[id], included: selected[id] === true }));
}
function resizeParameterFloors(input, previousInput) {
  const count = input.floorCount; const totalArea = input.area; const previous = Array.isArray(previousInput.floors) ? previousInput.floors : [];
  if (previous.length === count) {
    const oldTotal = previous.reduce((sum, floor) => sum + Number(floor.area || 0), 0); let used = 0;
    return previous.map((floor, index) => {
      const area = index === count - 1 ? Number((totalArea - used).toFixed(2)) : Number((oldTotal > 0 ? totalArea * Number(floor.area || 0) / oldTotal : totalArea / count).toFixed(2));
      used += area; return { ...clone(floor), index, area };
    });
  }
  let used = 0;
  return Array.from({ length: count }, (_, index) => {
    const rooms = {}; roomFields.forEach((field) => { const total = Number(input.rooms[field] || 0); rooms[field] = Math.floor(total / count) + (index < total % count ? 1 : 0); });
    const area = index === count - 1 ? Number((totalArea - used).toFixed(2)) : Number((totalArea / count).toFixed(2)); used += area;
    return { index, name: `${index + 1}层`, area, height: null, rooms };
  });
}
function measurementBasisText(record) {
  const snapshotBasis = record && record.report && record.report.renovation && record.report.renovation.quantityBasis;
  const summary = record && record.quantityBasis;
  const basis = snapshotBasis || (summary && summary.mode === 'measured' && summary.estimatedFields && summary.estimatedFields.length ? 'mixed' : summary && summary.mode);
  if (basis === 'mixed') return '空间尺寸（含估算项）';
  if (basis === 'measured') return '已按空间尺寸计算';
  if (basis === 'estimated') return '按房屋参数估算';
  return record && record.input && record.input.measurement ? (record.input.measurement.mode === 'measured' ? '已按空间尺寸计算' : '按房屋参数估算') : '历史参考报价';
}

Page({
  pageDestroyed: false,
  data: {
    __profileAccessMounted: true, loading: true, error: '', record: null, report: null, showCostDetails: false,
    standardOptions: [{ id: 'economy', name: '经济型' }, { id: 'standard', name: '标准型' }, { id: 'quality', name: '品质型' }],
    itemDrawerVisible: false, feeDrawerVisible: false, parameterDrawerVisible: false, measurementDrawerVisible: false, measurementEditorInput: null,
    editingItem: null, editingQuantity: '', editingIncluded: true, editingHasOverride: false, feeRows: [], parameterDraft: null, measurementBasisText: '',
    parameterFields: [{ field: 'area', label: '装修面积', unit: '㎡' }, { field: 'width', label: '面宽', unit: 'm' }, { field: 'depth', label: '进深', unit: 'm' }, { field: 'floorCount', label: '楼层数', unit: '层' }, { field: 'height', label: '层高', unit: 'm' }],
    saving: false, sharing: false,
    enterpriseWechatCustomerServiceCorpId: DEFAULT_CUSTOMER_SERVICE_CORP_ID,
    enterpriseWechatCustomerServiceUrl: DEFAULT_CUSTOMER_SERVICE_URL,
  },
  onLoad() { this.loadReport(); },
  onUnload() { this.pageDestroyed = true; },
  onProfileAccessGranted() { this.setData({ __profileAccessMounted: true }); },
  onProfileAccessBlocked() { this.setData({ __profileAccessMounted: true }); },
  loadReport() {
    this.setData({ loading: true, error: '', record: null, report: null });
    try {
      const record = readRenovationReport();
      if (!record || !record.report || !record.input || !record.selections) throw new Error('未找到本次装修报价');
      this.setData({ loading: false, record, report: record.report, feeRows: feeRows(record.selections.fees || {}), measurementBasisText: measurementBasisText(record) });
    } catch (error) {
      console.error('读取装修报价失败:', error);
      this.setData({ loading: false, error: errorMessage(error, '报价已过期，请重新测算'), record: null, report: null });
    }
  },
  onRetry() { this.loadReport(); },
  toggleCostDetails() { this.setData({ showCostDetails: !this.data.showCostDetails }); },
  async onStandardChange(e) {
    const option = this.data.standardOptions[Number(e.detail.value)];
    if (!option || !this.data.record || this.data.saving || option.id === this.data.record.input.standard) return;
    return this.recalculate({ ...this.data.record.input, standard: option.id }, this.data.record.selections);
  },
  onReturnModify() {
    const pages = typeof getCurrentPages === 'function' ? getCurrentPages() : [];
    let formIndex = -1;
    for (let index = pages.length - 2; index >= 0; index -= 1) {
      if (String(pages[index] && pages[index].route || '').replace(/^\//, '') === 'pages/renovationbudget/index') { formIndex = index; break; }
    }
    if (formIndex >= 0 && this.data.record) {
      const form = pages[formIndex];
      if (typeof form.applyReportRecord === 'function') form.applyReportRecord(clone(this.data.record));
      wx.navigateBack({ delta: pages.length - 1 - formIndex, fail: () => wx.showToast({ title: '返回修改失败，请重试', icon: 'none' }) });
      return;
    }
    wx.redirectTo({ url: '/pages/renovationbudget/index?restore=report', fail: () => wx.navigateTo({ url: '/pages/renovationbudget/index?restore=report' }) });
  },
  onItemEdit(e) {
    const item = e.detail.item;
    const quantities = this.data.record && this.data.record.selections.quantities || {};
    this.setData({ itemDrawerVisible: true, editingItem: item, editingQuantity: String(item.quantity), editingIncluded: item.included !== false, editingHasOverride: Object.prototype.hasOwnProperty.call(quantities, item.id) });
  },
  closeDrawers() { this.setData({ itemDrawerVisible: false, feeDrawerVisible: false, parameterDrawerVisible: false, editingItem: null, editingHasOverride: false }); },
  stopPropagation() {},
  onItemQuantityInput(e) { this.setData({ editingQuantity: e.detail.value }); },
  onItemIncludedChange(e) { this.setData({ editingIncluded: e.detail.value.includes('included') }); },
  async confirmItemEdit() {
    const quantity = Number(this.data.editingQuantity); if (!Number.isFinite(quantity) || quantity < 0) { wx.showToast({ title: '请填写有效工程量', icon: 'none' }); return; }
    const record = await this.applyItemEdit({ ...this.data.editingItem, quantity, included: this.data.editingIncluded });
    if (record) this.closeDrawers();
  },
  async applyItemEdit(item) {
    if (!this.data.record || this.data.saving) return;
    const selections = clone(this.data.record.selections);
    selections.quantities = selections.quantities || {}; selections.excludedItems = selections.excludedItems || [];
    selections.quantities[item.id] = Number(item.quantity);
    const excluded = new Set(selections.excludedItems); if (item.included === false) excluded.add(item.id); else excluded.delete(item.id);
    selections.excludedItems = Array.from(excluded);
    return this.recalculate(this.data.record.input, selections);
  },
  async onRestoreAutoQuantity() {
    if (!this.data.record || !this.data.editingItem || this.data.saving) return;
    const selections = clone(this.data.record.selections); selections.quantities = selections.quantities || {};
    delete selections.quantities[this.data.editingItem.id];
    const record = await this.recalculate(this.data.record.input, selections);
    if (record) this.closeDrawers();
  },
  onFeesEdit() { this.setData({ feeDrawerVisible: true, feeRows: feeRows(this.data.record.selections.fees || {}) }); },
  onFeeChange(e) {
    const checked = new Set(e.detail.value); this.setData({ feeRows: this.data.feeRows.map((fee) => ({ ...fee, included: checked.has(fee.id) })) });
  },
  async confirmFees() {
    const selections = clone(this.data.record.selections); selections.fees = {};
    this.data.feeRows.forEach((fee) => { selections.fees[fee.id] = fee.included; });
    const record = await this.recalculate(this.data.record.input, selections); if (record) this.closeDrawers();
  },
  onParametersEdit() { this.setData({ parameterDrawerVisible: true, parameterDraft: clone(this.data.record.input) }); },
  onParameterInput(e) {
    if (e.currentTarget.dataset.field === 'area' && this.data.record.input.measurement && this.data.record.input.measurement.mode === 'measured') return;
    const parameterDraft = { ...this.data.parameterDraft, [e.currentTarget.dataset.field]: e.detail.value }; this.setData({ parameterDraft });
  },
  async confirmParameters() {
    const draft = clone(this.data.parameterDraft); ['area', 'height'].forEach((field) => { draft[field] = Number(draft[field]); }); draft.floorCount = Number(draft.floorCount);
    for (const field of ['width', 'depth']) draft[field] = draft[field] === null || draft[field] === undefined || String(draft[field]).trim() === '' ? null : Number(draft[field]);
    const geometryValid = ['width', 'depth'].every((field) => draft[field] === null || (Number.isFinite(draft[field]) && draft[field] > 0));
    if (!(draft.area > 0 && draft.height > 0 && Number.isInteger(draft.floorCount) && draft.floorCount > 0 && geometryValid)) { wx.showToast({ title: '请检查房屋参数', icon: 'none' }); return; }
    try {
      draft.floors = resizeParameterFloors(draft, this.data.record.input);
      const measurement = createMeasurementDraft(draft, this.data.record.input.measurement || null);
      const input = resolveRenovationGeometry({ ...draft, measurement }).input;
      const record = await this.recalculate(input, this.data.record.selections); if (record) this.closeDrawers();
    } catch (error) {
      console.error('更新房屋参数失败:', error); wx.showToast({ title: String(error && error.message || '请检查房屋参数'), icon: 'none' });
    }
  },
  onMeasurementEdit() {
    if (!this.data.record || this.data.saving) return;
    this.setData({ measurementDrawerVisible: true, measurementEditorInput: clone(this.data.record.input) });
  },
  closeMeasurementDrawer() { this.setData({ measurementDrawerVisible: false }); },
  async onMeasurementConfirm(e) {
    const record = await this.recalculate(e.detail.input, this.data.record.selections);
    if (record) this.setData({ measurementDrawerVisible: false });
  },
  async recalculate(input, selections) {
    this.setData({ saving: true });
    try {
      const estimate = estimateRenovation(input, selections); const record = await writeRenovationReport(estimate, selections);
      if (!record || !record.report) throw new Error('报价保存失败');
      this.setData({ record, report: record.report, feeRows: feeRows(record.selections.fees || {}), measurementBasisText: measurementBasisText(record) });
      return record;
    } catch (error) {
      console.error('更新装修报价失败:', error); wx.showToast({ title: '保存失败，请重试', icon: 'none' });
    } finally { this.setData({ saving: false }); }
  },
  requestFullProfileForShare() {
    const guard = this.selectComponent('#profileAccessGuard');
    if (!guard || typeof guard.check !== 'function') { wx.showToast({ title: '请先补充头像和昵称', icon: 'none' }); return; }
    Promise.resolve(guard.check({ soft: true })).catch(() => wx.showToast({ title: '资料填写暂不可用', icon: 'none' }));
  },
  async onShareReport() {
    if (this.data.sharing || !this.data.record) return;
    const answer = await modal({ title: '公开分享报告', content: '公开后，链接持有人可查看地区、房屋参数和完整报价，有效期30天。', confirmText: '确认公开', cancelText: '取消' });
    if (!answer.confirm) return;
    this.setData({ sharing: true });
    try {
      const identity = await createCostEstimateShare(this.data.record.report);
      wx.navigateTo({ url: `/pages/aicostshare/aicostshare?report_id=${encodeURIComponent(identity.report_id)}` });
    } catch (error) {
      if (Number(error && (error.statusCode || error.code) || 0) === 428) this.requestFullProfileForShare();
      else wx.showToast({ title: errorMessage(error, '公开分享失败，请重试'), icon: 'none' });
    } finally { this.setData({ sharing: false }); }
  },
  onShareAppMessage() { return { title: '测算装修报价', path: '/pages/renovationbudget/index' }; },
  async onContactDesigner() {
    const opened = await tryOpenCustomerServiceDirect({ customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId, customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl });
    if (!opened) wx.navigateTo({ url: '/pages/customerservice/customerservice?source=renovation_budget', fail: () => wx.showToast({ title: '联系设计师入口打开失败', icon: 'none' }) });
  },
});
