"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.reportRequest = reportRequest;
exports.reportID = reportID;
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const ROOT = '/api/v1/miniprogram/reports';
async function reportRequest(suffix, method = 'GET', body = {}) {
    const token = String(wx.getStorageSync('token') || '');
    if (!token && suffix !== '/config')
        throw new Error('请先登录后查看您的报告。');
    const path = ROOT + suffix.split('?')[0];
    const params = token ? await (0, parameter_1.generateRequestParams)(token, body, path, (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '') : null;
    return new Promise((resolve, reject) => wx.request({
        url: 'https://api.jiadilingguang.com' + ROOT + suffix, method, data: body, timeout: 30000,
        header: { 'Content-Type': 'application/json', ...(params ? (0, parameter_1.paramsToHeaders)(params) : {}) },
        success: (response) => {
            const result = response.data;
            if (response.statusCode === 401)
                wx.removeStorageSync('token');
            if (response.statusCode === 200 && result?.code === 0)
                resolve(result.data);
            else
                reject(Object.assign(new Error(result?.msg || '暂时无法读取报告，请稍后重试。'), { statusCode: response.statusCode }));
        },
        fail: () => reject(new Error('网络连接中断，您的信息不会被重复提交，请重试。')),
    }));
}
function reportID(value) {
    if (typeof value !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(value))
        throw new Error('报告链接不完整，请从建房报告入口重新进入。');
    return value;
}
