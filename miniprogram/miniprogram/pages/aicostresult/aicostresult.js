"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/aicostresult/aicostresult.ts
const enterpriseWechat_1 = require("../../utils/enterpriseWechat");
const costEstimateShareImage_1 = require("../../utils/costEstimateShareImage");
const RESULT_STORAGE_KEY = 'aicostEstimateResult';
const RESULT_SCHEMA_VERSION = 4;
const API_BASE_URL = 'https://api.jiadilingguang.com';
const chartColors = ['#c39b65', '#ab8154', '#d0af7b', '#94704d', '#bc9362', '#d7b989'];
function isBasicInfoItem(item) {
    if (!item || typeof item !== 'object')
        return false;
    const value = item;
    return typeof value.label === 'string' && !!value.label.trim()
        && typeof value.value === 'string' && !!value.value.trim()
        && (value.fullWidth === undefined || typeof value.fullWidth === 'boolean');
}
function isCostBreakdownItem(item) {
    if (!item || typeof item !== 'object')
        return false;
    const value = item;
    return typeof value.name === 'string' && !!value.name.trim()
        && typeof value.amount === 'number' && Number.isFinite(value.amount) && value.amount >= 0
        && typeof value.amountText === 'string' && !!value.amountText.trim()
        && typeof value.percent === 'number' && Number.isFinite(value.percent) && value.percent >= 0 && value.percent <= 100
        && typeof value.note === 'string' && !!value.note.trim()
        && typeof value.estimable === 'boolean';
}
function isCurrentResult(result) {
    if (!result || typeof result !== 'object')
        return false;
    const value = result;
    return value.version === RESULT_SCHEMA_VERSION
        && typeof value.estimatedTotalCost === 'string'
        && typeof value.estimatedUnitCost === 'string'
        && typeof value.totalLabel === 'string'
        && typeof value.unitLabel === 'string'
        && Array.isArray(value.basicInfo) && value.basicInfo.length === 14 && value.basicInfo.every(isBasicInfoItem)
        && Array.isArray(value.costBreakdown) && (value.costBreakdown.length === 6 || value.costBreakdown.length === 7) && value.costBreakdown.every(isCostBreakdownItem)
        && Array.isArray(value.dynamicNotes) && value.dynamicNotes.every(note => typeof note === 'string');
}
function withChartColors(items) {
    return items.map((item, index) => ({ ...item, color: chartColors[index % chartColors.length] }));
}
function findBasicInfoValue(items, label) {
    return items.find((item) => item.label === label)?.value || '';
}
function getCity(region) {
    const parts = String(region || '').split('/').map((part) => part.trim()).filter(Boolean);
    return parts[parts.length - 1] || '建房';
}
Page({
    data: {
        estimatedTotalCost: '',
        estimatedUnitCost: '',
        totalLabel: '参考总造价',
        unitLabel: '参考单价',
        basicInfo: [],
        costBreakdown: [],
        dynamicNotes: [],
        showCostDetails: false,
        enterpriseWechatCustomerServiceCorpId: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_CORP_ID,
        enterpriseWechatCustomerServiceUrl: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_URL,
        enterpriseWechatConfigLoading: false,
    },
    onLoad() {
        let result;
        try {
            result = wx.getStorageSync(RESULT_STORAGE_KEY);
        }
        catch (error) {
            console.error('读取造价测算结果失败:', error);
        }
        if (!isCurrentResult(result)) {
            wx.showToast({ title: '测算数据已更新，请重新测算', icon: 'none' });
            setTimeout(() => wx.navigateBack(), 800);
            return;
        }
        this.setData({
            estimatedTotalCost: result.estimatedTotalCost,
            estimatedUnitCost: result.estimatedUnitCost,
            totalLabel: result.totalLabel,
            unitLabel: result.unitLabel,
            basicInfo: result.basicInfo,
            costBreakdown: withChartColors(result.costBreakdown),
            dynamicNotes: result.dynamicNotes,
        });
        this.loadEnterpriseWechatServiceConfig();
    },
    onShareAppMessage() {
        const city = getCity(findBasicInfoValue(this.data.basicInfo, '所在地区'));
        const buildingArea = findBasicInfoValue(this.data.basicInfo, '总建筑面积') || '--㎡';
        const structureType = findBasicInfoValue(this.data.basicInfo, '结构形式') || '建筑结构';
        const windowStandard = findBasicInfoValue(this.data.basicInfo, '门窗标准') || '标准型';
        const totalCost = this.data.estimatedTotalCost || '--万元';
        const title = `${city}${buildingArea}建房参考造价${totalCost.replace('元', '')}，查看测算报告`;
        const path = '/pages/aicost/aicost?source=cost-share';
        return {
            title,
            path,
            promise: (0, costEstimateShareImage_1.prepareCostEstimateShareImage)({
                totalCost,
                city,
                buildingArea,
                structureType,
                windowStandard,
            }).then((imageUrl) => imageUrl ? { title, path, imageUrl } : { title, path }),
        };
    },
    onViewCostDetails() {
        this.setData({ showCostDetails: !this.data.showCostDetails });
    },
    async loadEnterpriseWechatServiceConfig(force = false) {
        if (this.data.enterpriseWechatConfigLoading && !force)
            return;
        const token = wx.getStorageSync('token');
        if (!token) {
            this.setData({ enterpriseWechatConfigLoading: false });
            return;
        }
        this.setData({ enterpriseWechatConfigLoading: true });
        try {
            const configData = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/download-config`,
                    method: 'GET',
                    header: { token },
                    timeout: 8000,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '获取企业微信配置失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const serviceConfig = (0, enterpriseWechat_1.resolveEnterpriseWechatServiceConfig)(configData, {
                customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
                customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
            });
            this.setData({
                enterpriseWechatCustomerServiceCorpId: serviceConfig.customerServiceCorpId,
                enterpriseWechatCustomerServiceUrl: serviceConfig.customerServiceUrl,
                enterpriseWechatConfigLoading: false,
            });
        }
        catch (error) {
            console.error('加载企业微信人工客服配置失败:', error);
            this.setData({ enterpriseWechatConfigLoading: false });
        }
    },
    async onContactService() {
        const opened = await (0, enterpriseWechat_1.tryOpenCustomerServiceDirect)({
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        });
        if (opened)
            return;
        wx.navigateTo({
            url: '/pages/customerservice/customerservice?source=aicost',
            fail: () => wx.showToast({ title: '联系设计师入口打开失败', icon: 'none' }),
        });
    },
});
