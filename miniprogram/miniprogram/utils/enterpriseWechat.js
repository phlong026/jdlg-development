"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.DEFAULT_CUSTOMER_SERVICE_URL = exports.DEFAULT_CUSTOMER_SERVICE_CORP_ID = exports.DEFAULT_ENTERPRISE_WECHAT_QRCODE = exports.DEFAULT_ENTERPRISE_SERVICE_PHONE = void 0;
exports.normalizeEnterpriseWechatImageUrl = normalizeEnterpriseWechatImageUrl;
exports.resolveEnterpriseWechatServiceConfig = resolveEnterpriseWechatServiceConfig;
exports.canOpenEnterpriseCustomerService = canOpenEnterpriseCustomerService;
exports.getDefaultServiceConfig = getDefaultServiceConfig;
exports.openEnterpriseCustomerServiceChat = openEnterpriseCustomerServiceChat;
exports.tryOpenCustomerServiceDirect = tryOpenCustomerServiceDirect;
const asset_1 = require("./asset");
const API_BASE_URL = 'https://api.jiadilingguang.com';
exports.DEFAULT_ENTERPRISE_SERVICE_PHONE = '13959877676';
exports.DEFAULT_ENTERPRISE_WECHAT_QRCODE = (0, asset_1.resolveAssetPath)('/assets/企业微信二维码.png');
// 默认企微客服 CorpID 和客服链接，确保即使后端配置未加载也能拉起客服
exports.DEFAULT_CUSTOMER_SERVICE_CORP_ID = 'ww673b3a4edf114110';
exports.DEFAULT_CUSTOMER_SERVICE_URL = 'https://work.weixin.qq.com/kfid/kfccb23bfff32bc9c6f';
function normalizeEnterpriseWechatImageUrl(url, fallback = '') {
    const cleanUrl = String(url || '').trim();
    if (!cleanUrl) {
        return fallback;
    }
    if (/^(https?:\/\/|wxfile:\/\/|file:\/\/|data:)/i.test(cleanUrl)) {
        return cleanUrl;
    }
    if (cleanUrl.startsWith('//')) {
        return `https:${cleanUrl}`;
    }
    if (cleanUrl.startsWith('/')) {
        return `${API_BASE_URL}${cleanUrl}`;
    }
    return `${API_BASE_URL}/${cleanUrl}`;
}
function resolveEnterpriseWechatServiceConfig(configData = {}, defaults = {}) {
    const qrcodeUrl = String(configData.enterprise_wechat_qrcode_url || '').trim();
    const tip = String(configData.enterprise_wechat_tip || '').trim();
    const contact = String(configData.enterprise_wechat_contact || '').trim();
    const servicePhone = String(configData.enterprise_wechat_service_phone || '').trim();
    const customerServiceCorpId = String(configData.enterprise_wechat_customer_service_corp_id || '').trim();
    const customerServiceUrl = String(configData.enterprise_wechat_customer_service_url || '').trim();
    const fallbackQr = String(defaults.qrcodeUrl || exports.DEFAULT_ENTERPRISE_WECHAT_QRCODE).trim();
    return {
        qrcodeUrl: qrcodeUrl ? normalizeEnterpriseWechatImageUrl(qrcodeUrl, fallbackQr) : fallbackQr,
        tip: tip || String(defaults.tip || '').trim(),
        contact: contact || String(defaults.contact || '').trim(),
        servicePhone: servicePhone || String(defaults.servicePhone || exports.DEFAULT_ENTERPRISE_SERVICE_PHONE).trim(),
        customerServiceCorpId: customerServiceCorpId || String(defaults.customerServiceCorpId || '').trim(),
        customerServiceUrl: customerServiceUrl || String(defaults.customerServiceUrl || '').trim(),
    };
}
function canOpenEnterpriseCustomerService(config) {
    if (!config) {
        return false;
    }
    return !!String(config.customerServiceCorpId || '').trim() && !!String(config.customerServiceUrl || '').trim();
}
// 获取带默认值的客服配置，保证 corpId/url 始终有值
function getDefaultServiceConfig(overrides) {
    return {
        qrcodeUrl: overrides?.qrcodeUrl || exports.DEFAULT_ENTERPRISE_WECHAT_QRCODE,
        tip: overrides?.tip || '',
        contact: overrides?.contact || '',
        servicePhone: overrides?.servicePhone || exports.DEFAULT_ENTERPRISE_SERVICE_PHONE,
        customerServiceCorpId: overrides?.customerServiceCorpId || exports.DEFAULT_CUSTOMER_SERVICE_CORP_ID,
        customerServiceUrl: overrides?.customerServiceUrl || exports.DEFAULT_CUSTOMER_SERVICE_URL,
    };
}
// 检测是否在开发者工具中运行（仅用于日志诊断，不再用于屏蔽客服调用）
function isDevtoolsEnvironment() {
    try {
        const systemInfo = typeof wx !== 'undefined' && typeof wx.getSystemInfoSync === 'function'
            ? wx.getSystemInfoSync()
            : null;
        return String(systemInfo?.platform || '').toLowerCase() === 'devtools';
    }
    catch (error) {
        return false;
    }
}
function openEnterpriseCustomerServiceChat(config) {
    return new Promise((resolve) => {
        // 使用传入配置，不足时回退到默认 corpId/url
        const corpId = String(config?.customerServiceCorpId || exports.DEFAULT_CUSTOMER_SERVICE_CORP_ID).trim();
        const url = String(config?.customerServiceUrl || exports.DEFAULT_CUSTOMER_SERVICE_URL).trim();
        const apiAvailable = typeof wx?.openCustomerServiceChat === 'function';
        const devtoolsEnv = isDevtoolsEnvironment();
        const diagnostics = {
            hasCorpId: !!corpId,
            hasUrl: !!url,
            apiAvailable,
            isDevtools: devtoolsEnv,
        };
        if (!corpId || !url) {
            resolve({
                opened: false,
                reason: !corpId ? 'missing_corp_id' : 'missing_url',
                diagnostics,
            });
            return;
        }
        if (!apiAvailable) {
            resolve({
                opened: false,
                reason: 'api_unavailable',
                diagnostics,
            });
            return;
        }
        // 直接通过 wx 对象调用，避免 this 上下文丢失导致调用失败
        wx.openCustomerServiceChat({
            extInfo: { url },
            corpId,
            success: () => resolve({ opened: true, diagnostics }),
            fail: (error) => {
                console.warn('[企微客服] openCustomerServiceChat fail:', JSON.stringify(error));
                resolve({
                    opened: false,
                    reason: 'open_failed',
                    error,
                    diagnostics,
                });
            },
        });
    });
}
// 快捷方法：使用默认配置直接尝试拉起企微客服，返回是否成功
async function tryOpenCustomerServiceDirect(overrides) {
    const config = getDefaultServiceConfig(overrides);
    const result = await openEnterpriseCustomerServiceChat(config);
    if (!result.opened) {
        console.log('[企微客服] 拉起失败:', result.reason, result.diagnostics);
    }
    return result.opened;
}
