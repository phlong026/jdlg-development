// @ts-nocheck
const { isCostEstimateShareResult, sanitizeCostEstimateShareResult } = require('./costEstimateShare');

function clone(value) { return JSON.parse(JSON.stringify(value)); }

function getBudgetAccountId() {
  const token = String(wx.getStorageSync('token') || '');
  const profile = wx.getStorageSync('userInfo') || {};
  const id = Number(profile.id || 0);
  const profileToken = String(profile.token || '');
  return token && (!profileToken || profileToken === token) && Number.isInteger(id) && id > 0 ? id : 0;
}

function requestBudgetLogin() {
  wx.showModal({
    title: '提示', content: '登录后可保存本次测算结果', confirmText: '去登录',
    success(answer) { if (answer.confirm) wx.navigateTo({ url: '/pages/login/login?from=ai-action' }); },
  });
}

function formatAmount(value) {
  if (!Number.isFinite(value)) return '--';
  return value.toLocaleString('zh-CN', { maximumFractionDigits: 2 });
}

function estimateToSnapshot(estimate) {
  const input = estimate.input;
  const rooms = input.rooms;
  const basicInfo = [
    { label: '所在地区', value: `${input.province} / ${input.city}`, fullWidth: true },
    { label: '装修面积', value: `${input.area}㎡` },
    { label: '房屋状态', value: input.houseType === 'old' ? '旧房改造' : '新房' },
    { label: '楼层数', value: `${input.floorCount}层` },
    { label: '层高', value: `${input.height}m` },
    { label: '面宽', value: input.width == null ? '未填写' : `${input.width}m` },
    { label: '进深', value: input.depth == null ? '未填写' : `${input.depth}m` },
    { label: '户型', value: `${rooms.bedroom}室${rooms.living}厅${rooms.kitchen}厨${rooms.bathroom}卫${rooms.balcony}阳台` },
    { label: '室内风格', value: input.style },
    { label: '装修标准', value: { economy: '经济型', standard: '标准型', quality: '品质型' }[input.standard] },
  ];
  const amounts = [estimate.materialAmount, estimate.auxiliaryAmount, estimate.laborAmount];
  ['management', 'design', 'tax'].forEach(id => {
    const fee = estimate.fees.find(item => item.id === id);
    amounts.push(fee ? fee.amount : 0);
  });
  const names = ['主材费', '辅材费', '人工费', '管理费', '设计费', '税费'];
  const costBreakdown = names.map((name, index) => ({
    name, amount: amounts[index],
    amountText: index > 2 && !amounts[index] ? '未计入' : `${formatAmount(amounts[index])}元`,
    percent: estimate.totalAmount ? Number((amounts[index] / estimate.totalAmount * 100).toFixed(2)) : 0,
    note: index < 3 ? '工程费' : '附加费', estimable: true,
  }));
  return sanitizeCostEstimateShareResult({
    version: 5, quoteType: 'renovation', totalLabel: '装修参考总价', unitLabel: '参考单价',
    estimatedTotalCost: `${(estimate.totalAmount / 10000).toFixed(2)}万元`,
    estimatedUnitCost: `${estimate.unitAmount.toFixed(2)}元/㎡`,
    basicInfo, costBreakdown, dynamicNotes: [],
    renovation: { pricingVersion: estimate.pricingVersion, totalAmount: estimate.totalAmount, spaces: clone(estimate.spaces) },
  });
}

function writeRenovationReport(estimate, selections) {
  const userID = getBudgetAccountId();
  if (!userID) throw new Error('请先登录后再保存报价');
  const record = {
    version: 1, userID, input: clone(estimate.input), selections: clone(selections || {}),
    report: estimateToSnapshot(estimate),
  };
  try {
    wx.setStorageSync(`renovationBudgetResult:${userID}`, record);
  } catch (error) {
    console.error('保存装修报价失败:', error);
    throw new Error('报价保存失败，请重试');
  }
  return clone(record);
}

function readRenovationReport() {
  const userID = getBudgetAccountId();
  if (!userID) throw new Error('请登录后重新测算');
  const record = wx.getStorageSync(`renovationBudgetResult:${userID}`);
  if (!record || record.version !== 1 || record.userID !== userID || !record.input || !record.selections
    || !isCostEstimateShareResult(record.report) || record.report.version !== 5) {
    throw new Error('报价已过期，请重新测算');
  }
  return clone(record);
}

function readBuildingPrefill() {
  const userID = getBudgetAccountId();
  if (!userID) return null;
  const report = wx.getStorageSync(`aicostEstimateResult:${userID}`);
  if (!isCostEstimateShareResult(report) || report.version !== 4) return null;
  const info = label => {
    const row = report.basicInfo.find(item => item.label === label);
    return row ? row.value : '';
  };
  const region = info('所在地区').split('/').map(value => value.trim());
  if (region.length !== 2 || !region.every(Boolean)) return null;
  return {
    province: region[0], city: region[1], area: parseFloat(info('总建筑面积')),
    floorCount: parseFloat(info('楼层数')), height: parseFloat(info('首层高度')),
  };
}

module.exports = {
  getBudgetAccountId, requestBudgetLogin, formatAmount, estimateToSnapshot,
  writeRenovationReport, readRenovationReport, readBuildingPrefill,
};
