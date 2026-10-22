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
function isCostEstimateShareResult(value) {
    if (!value || typeof value !== 'object')
        return false;
    const result = value;
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
    return {
        version: value.version,
        estimatedTotalCost: value.estimatedTotalCost,
        estimatedUnitCost: value.estimatedUnitCost,
        totalLabel: value.totalLabel,
        unitLabel: value.unitLabel,
        basicInfo: value.basicInfo.map(item => (Object.assign({ label: item.label, value: item.value }, (item.fullWidth === true ? { fullWidth: true } : {})))),
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
}
function request(options) {
    return new Promise((resolve, reject) => {
        wx.request(Object.assign(Object.assign({}, options), { timeout: 8000, success: (response) => {
                const body = response.data;
                if (response.statusCode === 200 && body && body.code === 0) {
                    resolve(body.data);
                    return;
                }
                reject(new Error(String((body === null || body === void 0 ? void 0 : body.msg) || `请求失败: ${response.statusCode}`)));
            }, fail: () => reject(new Error('网络连接失败，请稍后重试')) }));
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
    const params = (0, parameter_1.generateRequestParams)(token, body, CREATE_PATH, (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '');
    const response = await request({
        url: `${API_BASE_URL}${CREATE_PATH}`,
        method: 'POST',
        data: body,
        header: Object.assign(Object.assign({}, (0, parameter_1.paramsToHeaders)(params)), { 'Content-Type': 'application/json' }),
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
    return Object.assign(Object.assign({}, response), { result: sanitizeCostEstimateShareResult(response.result) });
}
