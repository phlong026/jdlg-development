"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.isCostEstimateShareResult = isCostEstimateShareResult;
exports.sanitizeCostEstimateShareResult = sanitizeCostEstimateShareResult;
exports.createCostEstimateShare = createCostEstimateShare;
exports.getCostEstimateShare = getCostEstimateShare;
const deviceFingerprint_1 = require("./deviceFingerprint");
const parameter_1 = require("./parameter");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const CREATE_PATH = '/api/v1/miniprogram/ai/cost/shares';
const RENOVATION_CREATE_PATH = '/api/v1/miniprogram/ai/renovation-cost/shares';
const REPORT_ID_PATTERN = /^[A-Za-z0-9_-]{32}$/;
function validText(value) {
    return typeof value === 'string' && value.trim().length > 0;
}
function isBasicInfo(item) {
    if (!item || typeof item !== 'object')
        return false;
    const value = item;
    return validText(value.label) && validText(value.value)
        && (value.fullWidth === undefined || typeof value.fullWidth === 'boolean');
}
function isBreakdown(item) {
    if (!item || typeof item !== 'object')
        return false;
    const value = item;
    return validText(value.name)
        && typeof value.amount === 'number' && Number.isFinite(value.amount) && value.amount >= 0
        && validText(value.amountText)
        && typeof value.percent === 'number' && Number.isFinite(value.percent) && value.percent >= 0 && value.percent <= 100
        && validText(value.note)
        && typeof value.estimable === 'boolean';
}
const RENOVATION_INFO_LABELS = ['所在地区', '装修面积', '房屋状态', '楼层数', '层高', '面宽', '进深', '户型', '室内风格', '装修标准'];
const RENOVATION_BREAKDOWN_NAMES = ['主材费', '辅材费', '人工费', '管理费', '设计费', '税费'];
const RENOVATION_UNITS = new Set(['㎡', 'm', '项', '套', '个', '位', '块', '根']);
const RENOVATION_AUXILIARY_REFERENCE = 12058;
const RENOVATION_LABOR_REFERENCE = 28952;
const RENOVATION_SPACES = [
    { id: 'bedroom', name: '卧室', groups: { ceiling: '天花板', wall: '墙面', floor: '地面', door: '室内门' } },
    { id: 'living', name: '客餐厅', groups: { ceiling: '天花板', wall: '墙面', floor: '地面', threshold: '门槛石' } },
    { id: 'kitchen', name: '厨房', groups: { ceiling: '天花板', wall: '墙面', floor: '地面', kitchenware: '厨具', door: '室内门', lighting: '灯具' } },
    { id: 'bathroom', name: '卫生间', groups: { ceiling: '天花板', wall: '墙面', floor: '地面', sanitary: '卫浴用具', door: '室内门' } },
    { id: 'balcony', name: '阳台', groups: { ceiling: '天花板', wall: '墙面', floor: '地面' } },
    { id: 'common', name: '全屋公共', groups: { utilities: '水电改造', handling: '搬运清洁', demolition: '拆改与保护' } },
];
function validNumber(value, max = 1e12) {
    return typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= max;
}
function amountsEqual(left, right) {
    return Math.abs(left - right) <= 0.011;
}
function isRenovationResult(result) {
    if (result.version !== 5 || result.quoteType !== 'renovation' || result.totalLabel !== '装修参考总价' || result.unitLabel !== '参考单价'
        || !validText(result.estimatedTotalCost) || !validText(result.estimatedUnitCost)
        || !Array.isArray(result.basicInfo) || result.basicInfo.length !== RENOVATION_INFO_LABELS.length
        || !Array.isArray(result.costBreakdown) || result.costBreakdown.length !== RENOVATION_BREAKDOWN_NAMES.length
        || !Array.isArray(result.dynamicNotes) || result.dynamicNotes.length !== 0 || !result.renovation)
        return false;
    if (!result.basicInfo.every((item, index) => isBasicInfo(item) && item.label === RENOVATION_INFO_LABELS[index] && (item.fullWidth === true) === (index === 0)))
        return false;
    if (!result.costBreakdown.every((item, index) => isBreakdown(item) && item.name === RENOVATION_BREAKDOWN_NAMES[index]))
        return false;
    const renovation = result.renovation;
    if (!['renovation-reference-v1', 'renovation-dynamic-v2'].includes(renovation.pricingVersion) || !validNumber(renovation.totalAmount)
        || !Array.isArray(renovation.spaces) || renovation.spaces.length !== RENOVATION_SPACES.length)
        return false;
    if (renovation.pricingVersion === 'renovation-dynamic-v2') {
        if (!['estimated', 'measured', 'mixed'].includes(String(renovation.quantityBasis)))
            return false;
    }
    else if (renovation.quantityBasis !== undefined)
        return false;
    let engineering = 0;
    let materialAmount = 0;
    let itemCount = 0;
    const itemIds = new Set();
    for (let spaceIndex = 0; spaceIndex < renovation.spaces.length; spaceIndex += 1) {
        const space = renovation.spaces[spaceIndex];
        const definition = RENOVATION_SPACES[spaceIndex];
        if (!space || space.id !== definition.id || space.name !== definition.name || !Number.isInteger(space.count) || space.count < 0 || space.count > 50
            || !validNumber(space.amount) || !Array.isArray(space.groups) || space.groups.length !== Object.keys(definition.groups).length)
            return false;
        const groupIds = new Set();
        let spaceTotal = 0;
        for (const group of space.groups) {
            if (!group || definition.groups[group.id] !== group.name || groupIds.has(group.id) || !validNumber(group.amount) || !Array.isArray(group.items))
                return false;
            groupIds.add(group.id);
            let groupTotal = 0;
            for (const item of group.items) {
                itemCount += 1;
                if (!item || itemCount > 128 || !validText(item.id) || !validText(item.name) || !RENOVATION_UNITS.has(item.unit)
                    || itemIds.has(item.id) || !validNumber(item.quantity, 1e6) || !validNumber(item.materialUnitPrice, 1e8)
                    || !validNumber(item.laborAuxUnitPrice, 1e8) || !validNumber(item.amount) || typeof item.included !== 'boolean')
                    return false;
                const expectedAmount = item.included ? Math.round(item.quantity * (item.materialUnitPrice + item.laborAuxUnitPrice)) : 0;
                if (!amountsEqual(item.amount, expectedAmount))
                    return false;
                if (item.included)
                    materialAmount += Math.round(item.quantity * item.materialUnitPrice);
                itemIds.add(item.id);
                groupTotal += item.amount;
            }
            if (!amountsEqual(groupTotal, group.amount))
                return false;
            spaceTotal += group.amount;
        }
        if (!amountsEqual(spaceTotal, space.amount))
            return false;
        engineering += space.amount;
    }
    const amounts = result.costBreakdown.map(item => item.amount);
    const nonMaterialAmount = engineering - materialAmount;
    const auxiliaryAmount = Math.round(nonMaterialAmount * RENOVATION_AUXILIARY_REFERENCE / (RENOVATION_AUXILIARY_REFERENCE + RENOVATION_LABOR_REFERENCE));
    const laborAmount = nonMaterialAmount - auxiliaryAmount;
    if (!amountsEqual(amounts[0], materialAmount) || !amountsEqual(amounts[1], auxiliaryAmount) || !amountsEqual(amounts[2], laborAmount))
        return false;
    for (const [index, rate] of [0.1, 0.04, 0.034].entries()) {
        if (amounts[index + 3] !== 0 && !amountsEqual(amounts[index + 3], Math.round(engineering * rate)))
            return false;
    }
    return amountsEqual(amounts.reduce((sum, amount) => sum + amount, 0), renovation.totalAmount);
}
function isCostEstimateShareResult(value) {
    if (!value || typeof value !== 'object')
        return false;
    const result = value;
    if (result.version === 5)
        return isRenovationResult(result);
    return result.version === 4
        && validText(result.estimatedTotalCost)
        && validText(result.estimatedUnitCost)
        && validText(result.totalLabel)
        && validText(result.unitLabel)
        && Array.isArray(result.basicInfo) && result.basicInfo.length === 14 && result.basicInfo.every(isBasicInfo)
        && Array.isArray(result.costBreakdown) && (result.costBreakdown.length === 6 || result.costBreakdown.length === 7) && result.costBreakdown.every(isBreakdown)
        && Array.isArray(result.dynamicNotes) && result.dynamicNotes.every(note => typeof note === 'string');
}
function sanitizeCostEstimateShareResult(value) {
    if (!isCostEstimateShareResult(value)) {
        throw new Error('测算报告数据无效，请重新测算');
    }
    const common = {
        version: value.version,
        estimatedTotalCost: value.estimatedTotalCost,
        estimatedUnitCost: value.estimatedUnitCost,
        totalLabel: value.totalLabel,
        unitLabel: value.unitLabel,
        basicInfo: value.basicInfo.map(item => ({
            label: item.label,
            value: item.value,
            ...(item.fullWidth === true ? { fullWidth: true } : {}),
        })),
        costBreakdown: value.costBreakdown.map(item => ({
            name: item.name,
            amount: item.amount,
            amountText: item.amountText,
            percent: item.percent,
            note: item.note,
            estimable: item.estimable,
        })),
        dynamicNotes: value.dynamicNotes.slice(),
    };
    if (value.version !== 5 || !value.renovation)
        return common;
    return {
        ...common,
        quoteType: 'renovation',
        renovation: {
            pricingVersion: value.renovation.pricingVersion,
            ...(value.renovation.quantityBasis ? { quantityBasis: value.renovation.quantityBasis } : {}),
            totalAmount: value.renovation.totalAmount,
            spaces: value.renovation.spaces.map(space => ({
                id: space.id,
                name: space.name,
                count: space.count,
                amount: space.amount,
                groups: space.groups.map(group => ({
                    id: group.id,
                    name: group.name,
                    amount: group.amount,
                    items: group.items.map(item => ({
                        id: item.id,
                        name: item.name,
                        unit: item.unit,
                        quantity: item.quantity,
                        materialUnitPrice: item.materialUnitPrice,
                        laborAuxUnitPrice: item.laborAuxUnitPrice,
                        amount: item.amount,
                        included: item.included,
                    })),
                })),
            })),
        },
    };
}
function request(options) {
    return new Promise((resolve, reject) => {
        wx.request({
            ...options,
            timeout: 8000,
            success: (response) => {
                const body = response.data;
                if (response.statusCode === 200 && body && body.code === 0) {
                    resolve(body.data);
                    return;
                }
                const error = new Error(String(body?.msg || `请求失败: ${response.statusCode}`));
                error.statusCode = Number(response.statusCode || 0);
                error.code = Number(body?.code || 0);
                reject(error);
            },
            fail: () => reject(new Error('网络连接失败，请稍后重试')),
        });
    });
}
function validShareIdentity(value) {
    return !!value && REPORT_ID_PATTERN.test(String(value.report_id || '')) && validText(value.expires_at);
}
async function createCostEstimateShare(result) {
    const token = String(wx.getStorageSync('token') || '').trim();
    if (!token)
        throw new Error('请先登录后分享报告');
    const body = sanitizeCostEstimateShareResult(result);
    const createPath = body.version === 5 && body.quoteType === 'renovation' ? RENOVATION_CREATE_PATH : CREATE_PATH;
    const params = (0, parameter_1.generateRequestParams)(token, body, createPath, (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '');
    const response = await request({
        url: `${API_BASE_URL}${createPath}`,
        method: 'POST',
        data: body,
        header: { ...(0, parameter_1.paramsToHeaders)(params), 'Content-Type': 'application/json' },
        __profileAccessResponseMode: 'soft-full-profile',
    });
    if (!validShareIdentity(response))
        throw new Error('分享报告创建失败，请重试');
    return response;
}
async function getCostEstimateShare(reportId) {
    const id = String(reportId || '').trim();
    if (!REPORT_ID_PATTERN.test(id))
        throw new Error('无效的分享报告链接');
    const path = `${CREATE_PATH}/${id}`;
    const response = await request({
        url: `${API_BASE_URL}${path}`,
        method: 'GET',
    });
    if (!validShareIdentity(response) || response.report_id !== id || !isCostEstimateShareResult(response.result)) {
        throw new Error('分享报告数据无效，请重新测算');
    }
    return { ...response, result: sanitizeCostEstimateShareResult(response.result) };
}
