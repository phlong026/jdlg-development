"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.fetchFavorites = fetchFavorites;
exports.fetchFavoriteStatus = fetchFavoriteStatus;
exports.addFavorite = addFavorite;
exports.removeFavorite = removeFavorite;
exports.toggleFavorite = toggleFavorite;
const parameter_1 = require("./parameter");
const deviceFingerprint_1 = require("./deviceFingerprint");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const FAVORITES_API_PATH = '/api/v1/miniprogram/favorites';
async function resolveDeviceId() {
    let deviceId = (0, deviceFingerprint_1.getCachedDeviceFingerprint)();
    if (deviceId) {
        return deviceId;
    }
    try {
        deviceId = await (0, deviceFingerprint_1.generateDeviceFingerprint)();
        if (deviceId) {
            (0, deviceFingerprint_1.cacheDeviceFingerprint)(deviceId);
        }
    }
    catch (error) {
        console.warn('获取设备ID失败:', error);
    }
    return deviceId || '';
}
async function buildSignedHeaders(apiPath, body = {}) {
    const token = String(wx.getStorageSync('token') || '').trim();
    if (!token) {
        throw new Error('请先登录');
    }
    const deviceId = await resolveDeviceId();
    const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, deviceId);
    return {
        ...(0, parameter_1.paramsToHeaders)(params),
        'Content-Type': 'application/json',
    };
}
function request(options) {
    const body = options.body || {};
    const signedPath = options.query ? `${options.apiPath}?${options.query}` : options.apiPath;
    return buildSignedHeaders(signedPath, body).then((headers) => new Promise((resolve, reject) => {
        wx.request({
            url: `${API_BASE_URL}${signedPath}`,
            method: options.method,
            header: headers,
            data: body,
            success: (res) => {
                const response = (res.data || {});
                if (res.statusCode === 200 && response.code === 0) {
                    resolve((response.data || {}));
                    return;
                }
                reject(new Error(response.msg || `请求失败: ${res.statusCode}`));
            },
            fail: reject,
        });
    }));
}
function fetchFavorites(params = {}) {
    const page = Math.max(1, Number(params.page || 1));
    const pageSize = Math.max(1, Math.min(50, Number(params.pageSize || 20)));
    const type = params.type || '';
    return request({
        apiPath: FAVORITES_API_PATH,
        method: 'GET',
        query: `type=${encodeURIComponent(type)}&page=${page}&page_size=${pageSize}`,
    });
}
function fetchFavoriteStatus(targetType, targetId) {
    return request({
        apiPath: `${FAVORITES_API_PATH}/status`,
        method: 'GET',
        query: `target_type=${encodeURIComponent(targetType)}&target_id=${Number(targetId || 0)}`,
    });
}
function addFavorite(targetType, targetId) {
    return request({
        apiPath: FAVORITES_API_PATH,
        method: 'POST',
        body: {
            target_type: targetType,
            target_id: Number(targetId || 0),
        },
    });
}
function removeFavorite(targetType, targetId) {
    return request({
        apiPath: FAVORITES_API_PATH,
        method: 'DELETE',
        body: {
            target_type: targetType,
            target_id: Number(targetId || 0),
        },
    });
}
function toggleFavorite(targetType, targetId, favorited) {
    return favorited ? removeFavorite(targetType, targetId) : addFavorite(targetType, targetId);
}
