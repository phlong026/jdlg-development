"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/aicostresult/aicostresult.ts
const enterpriseWechat_1 = require("../../utils/enterpriseWechat");
const costEstimateShareImage_1 = require("../../utils/costEstimateShareImage");
const costEstimateShare_1 = require("../../utils/costEstimateShare");
const RESULT_STORAGE_KEY = 'aicostEstimateResult';
const API_BASE_URL = 'https://api.jiadilingguang.com';
const chartColors = ['#c39b65', '#ab8154', '#d0af7b', '#94704d', '#bc9362', '#d7b989'];
function withChartColors(items) {
    return items.map((item, index) => (Object.assign(Object.assign({}, item), { color: chartColors[index % chartColors.length] })));
}
function findBasicInfoValue(items, label) {
    var _a;
    return ((_a = items.find((item) => item.label === label)) === null || _a === void 0 ? void 0 : _a.value) || '';
}
function getCity(region) {
    const parts = String(region || '').split('/').map((part) => part.trim()).filter(Boolean);
    return parts[parts.length - 1] || '建房';
}
function getErrorMessage(error, fallback) {
    const message = String((error === null || error === void 0 ? void 0 : error.message) || '').trim();
    return message || fallback;
}
Page({
    pageDestroyed: false,
    pageReady: false,
    requestedReportId: '',
    reportSnapshot: null,
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
        reportLoading: false,
        reportError: '',
        sharePreparing: false,
        shareReady: false,
        shareReportId: '',
        shareChatImage: '',
        shareTimelineImage: '',
        contentReady: false,
    },
    onLoad(options = {}) {
        this.pageDestroyed = false;
        this.pageReady = false;
        const reportId = String(options.report_id || '').trim();
        if (Object.prototype.hasOwnProperty.call(options, 'report_id')) {
            this.requestedReportId = reportId;
            this.setData({ reportLoading: true });
            void this.loadSharedReport(reportId);
            return;
        }
        let result;
        try {
            result = wx.getStorageSync(RESULT_STORAGE_KEY);
        }
        catch (error) {
            console.error('读取造价测算结果失败:', error);
        }
        if (!(0, costEstimateShare_1.isCostEstimateShareResult)(result)) {
            wx.showToast({ title: '测算数据已更新，请重新测算', icon: 'none' });
            setTimeout(() => wx.navigateBack(), 800);
            return;
        }
        this.applyResult((0, costEstimateShare_1.sanitizeCostEstimateShareResult)(result));
        this.loadEnterpriseWechatServiceConfig();
    },
    onReady() {
        this.pageReady = true;
        this.maybePrepareShareReport();
    },
    onShow() {
        this.maybePrepareShareReport();
    },
    onUnload() {
        this.pageDestroyed = true;
    },
    applyResult(result) {
        this.reportSnapshot = result;
        this.setData({
            estimatedTotalCost: result.estimatedTotalCost,
            estimatedUnitCost: result.estimatedUnitCost,
            totalLabel: result.totalLabel,
            unitLabel: result.unitLabel,
            basicInfo: result.basicInfo,
            costBreakdown: withChartColors(result.costBreakdown),
            dynamicNotes: result.dynamicNotes,
            reportLoading: false,
            reportError: '',
        });
    },
    async loadSharedReport(reportId) {
        try {
            const snapshot = await (0, costEstimateShare_1.getCostEstimateShare)(reportId);
            if (this.pageDestroyed || this.requestedReportId !== reportId)
                return;
            this.applyResult(snapshot.result);
            this.setData({ shareReportId: snapshot.report_id });
            this.loadEnterpriseWechatServiceConfig();
            this.maybePrepareShareReport();
        }
        catch (error) {
            console.error('加载分享测算报告失败:', error);
            if (this.pageDestroyed || this.requestedReportId !== reportId)
                return;
            this.reportSnapshot = null;
            this.setData({
                reportLoading: false,
                reportError: getErrorMessage(error, '分享报告已失效，请重新测算'),
            });
        }
    },
    onRetrySharedReport() {
        if (this.data.reportLoading)
            return;
        this.setData({ reportLoading: true, reportError: '' });
        void this.loadSharedReport(this.requestedReportId);
    },
    onRecalculate() {
        wx.redirectTo({ url: '/pages/aicost/aicost' });
    },
    getShareCard() {
        const city = getCity(findBasicInfoValue(this.data.basicInfo, '所在地区'));
        const buildingArea = findBasicInfoValue(this.data.basicInfo, '总建筑面积') || '--㎡';
        const structureType = findBasicInfoValue(this.data.basicInfo, '结构形式') || '建筑结构';
        const windowStandard = findBasicInfoValue(this.data.basicInfo, '门窗标准') || '标准型';
        const totalCost = this.data.estimatedTotalCost || '--万元';
        return { city, buildingArea, structureType, windowStandard, totalCost, totalLabel: this.data.totalLabel };
    },
    getShareTitle() {
        const card = this.getShareCard();
        const costLabel = this.data.totalLabel === '已测算部分合计' ? '建房已测算部分合计' : '建房参考造价';
        return `${card.city}${card.buildingArea}${costLabel}${card.totalCost.replace('元', '')}，查看测算报告`;
    },
    maybePrepareShareReport() {
        if (!this.pageReady || this.pageDestroyed || this.data.sharePreparing || this.data.shareReady || !this.reportSnapshot)
            return;
        if (!this.data.shareReportId && !wx.getStorageSync('token'))
            return;
        void this.onPrepareShareReport();
    },
    async onPrepareShareReport() {
        if (this.data.sharePreparing || this.data.shareReady || !this.reportSnapshot)
            return;
        if (!this.data.shareReportId && !wx.getStorageSync('token'))
            return;
        this.setData({ sharePreparing: true });
        try {
            const identity = this.data.shareReportId
                ? { report_id: this.data.shareReportId }
                : await (0, costEstimateShare_1.createCostEstimateShare)(this.reportSnapshot);
            if (this.pageDestroyed)
                return;
            // Keep the first immutable snapshot when cover generation needs a retry.
            this.setData({ shareReportId: identity.report_id });
            const card = this.getShareCard();
            const [shareChatImage, shareTimelineImage] = await Promise.all([
                (0, costEstimateShareImage_1.prepareCostEstimateShareImage)(card),
                (0, costEstimateShareImage_1.prepareCostEstimateTimelineImage)(card),
            ]);
            if (this.pageDestroyed)
                return;
            if (!shareChatImage || !shareTimelineImage)
                throw new Error('分享封面生成失败，请重试');
            this.setData({
                sharePreparing: false,
                shareReady: true,
                shareChatImage,
                shareTimelineImage,
                contentReady: true,
            });
        }
        catch (error) {
            console.error('准备预算报告分享失败:', error);
            if (this.pageDestroyed)
                return;
            this.setData({ sharePreparing: false, shareReady: false, contentReady: false });
        }
    },
    onShareAppMessage() {
        if (!this.data.shareReady || !this.data.shareReportId)
            return {};
        const title = this.getShareTitle();
        const path = `/pages/aicostresult/aicostresult?report_id=${encodeURIComponent(this.data.shareReportId)}`;
        return {
            title,
            path,
            imageUrl: this.data.shareChatImage || undefined,
        };
    },
    onShareTimeline() {
        if (!this.data.contentReady || !this.data.shareReportId || !this.data.shareTimelineImage)
            return {};
        return {
            title: this.getShareTitle(),
            query: `report_id=${encodeURIComponent(this.data.shareReportId)}`,
            imageUrl: this.data.shareTimelineImage,
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
