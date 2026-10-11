"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// index.ts
// 获取应用实例
const asset_1 = require("../../utils/asset");
const homeAssets_1 = require("../../utils/homeAssets");
const costRegions_1 = require("../../utils/costRegions");
const reportNavigation_1 = require("../../utils/reportNavigation");
const perf_1 = require("../../utils/perf");
const enterpriseWechat_1 = require("../../utils/enterpriseWechat");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const HOME_INTRO_POPUP_STORAGE_KEY = 'homeIntroPopupSeen:v2';
const TEMPLATE_DETAIL_CACHE_TTL = 2 * 60 * 1000;
const featuredDetailPrefetching = new Set();
function buildPublicTemplateDetailCacheKey(templateId) {
    return `template-detail:public:${Number(templateId || 0)}`;
}
Page({
    data: {
        featuredGroups: [],
        featuredGroupsLoading: false,
        lastFeaturedGroupsFetchTime: 0,
        homeIntroPopupVisible: false,
        contactPopupVisible: false,
        wechatPopupVisible: false,
        supplierPhone: enterpriseWechat_1.DEFAULT_ENTERPRISE_SERVICE_PHONE,
        supplierWechatQr: enterpriseWechat_1.DEFAULT_ENTERPRISE_WECHAT_QRCODE,
        supplierWechatQrAvailable: true,
        enterpriseWechatTip: '长按识别企业微信二维码，添加后可继续咨询与服务沟通。',
        enterpriseWechatContact: '',
        enterpriseWechatCustomerServiceCorpId: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_CORP_ID,
        enterpriseWechatCustomerServiceUrl: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_URL,
        enterpriseWechatConfigLoading: false,
        quickProjectType: 'rural_villa',
        quickRegionOptions: [Object.keys(costRegions_1.cityOptionsMap), costRegions_1.cityOptionsMap.福建],
        quickRegionIndex: [0, 0],
        quickRegionConfirmedIndex: [0, 0],
        quickRegionText: '',
        quickProvince: '',
        quickCity: '',
        quickArea: '',
        homeAssets: homeAssets_1.HOME_ASSETS,
        brandHeaderTop: 68,
        defaultCoverImage: (0, asset_1.resolveAssetPath)('/assets/images/home.jpg'),
        introLogoImage: (0, asset_1.resolveAssetPath)('/assets/home/intro-logo.png'),
        mascotBouncing: false,
    },
    syncTabBar() {
        const tabBar = typeof this.getTabBar === 'function' ? this.getTabBar() : null;
        if (tabBar && typeof tabBar.setCurrent === 'function') {
            tabBar.setCurrent(0);
        }
    },
    onLoad() {
        this.syncBrandSafeArea();
        this.showHomeIntroPopupOnce();
    },
    syncBrandSafeArea() {
        if (typeof wx.getWindowInfo !== 'function')
            return;
        try {
            const info = wx.getWindowInfo();
            const width = Number(info.windowWidth) || 375;
            const statusBarHeight = Number(info.statusBarHeight) || 44;
            // 至少比旧版下移 24rpx，同时在手机状态栏下留出 12px。
            this.setData({ brandHeaderTop: Math.max(85.55 * width / 750, statusBarHeight + 12) });
        }
        catch (error) {
            console.warn('首页品牌安全距离读取失败，保留默认位置', error);
        }
    },
    onShow() {
        if ((0, reportNavigation_1.resumeReportAfterLogin)())
            return;
        this.syncTabBar();
        this.loadFeaturedGroups();
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · AI设计助手',
            path: '/pages/index/index',
        };
    },
    onOpenPage(e) {
        const { url } = e.currentTarget.dataset;
        if (!url) {
            return;
        }
        const tabPaths = [
            '/pages/index/index',
            '/pages/template/template',
            '/pages/distribution/distribution',
            '/pages/my/my',
        ];
        const options = {
            url,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        };
        if (tabPaths.includes(url.split('?')[0])) {
            wx.switchTab(Object.assign(Object.assign({}, options), { url: url.split('?')[0] }));
        }
        else {
            wx.navigateTo(options);
        }
    },
    onQuickProjectChange(e) {
        const type = e.currentTarget.dataset.type;
        if (type === 'rural_villa' || type === 'urban_renewal') {
            this.setData({ quickProjectType: type });
        }
    },
    onQuickRegionColumnChange(e) {
        const { column, value } = e.detail;
        const indices = [...this.data.quickRegionIndex];
        indices[column] = value;
        if (column === 0) {
            const province = Object.keys(costRegions_1.cityOptionsMap)[value];
            if (!province)
                return;
            indices[1] = 0;
            this.setData({
                quickRegionOptions: [Object.keys(costRegions_1.cityOptionsMap), costRegions_1.cityOptionsMap[province]],
                quickRegionIndex: indices,
            });
        }
        else {
            this.setData({ quickRegionIndex: indices });
        }
    },
    onQuickRegionChange(e) {
        var _a;
        const indices = e.detail.value;
        const province = Object.keys(costRegions_1.cityOptionsMap)[indices[0]];
        const city = (_a = costRegions_1.cityOptionsMap[province]) === null || _a === void 0 ? void 0 : _a[indices[1]];
        if (!province || !city)
            return;
        this.setData({
            quickProvince: province,
            quickCity: city,
            quickRegionText: `${province} / ${city}`,
            quickRegionIndex: indices,
            quickRegionConfirmedIndex: indices,
            quickRegionOptions: [Object.keys(costRegions_1.cityOptionsMap), costRegions_1.cityOptionsMap[province]],
        });
    },
    onQuickRegionCancel() {
        const indices = this.data.quickRegionConfirmedIndex;
        const province = Object.keys(costRegions_1.cityOptionsMap)[indices[0]];
        this.setData({
            quickRegionIndex: indices,
            quickRegionOptions: [Object.keys(costRegions_1.cityOptionsMap), costRegions_1.cityOptionsMap[province]],
        });
    },
    onQuickAreaInput(e) {
        this.setData({ quickArea: e.detail.value });
    },
    onQuickEstimate() {
        if (!this.data.quickRegionText) {
            wx.showToast({ title: '请选择所在地区', icon: 'none' });
            return;
        }
        const area = Number(String(this.data.quickArea).trim());
        if (!Number.isFinite(area) || area <= 0) {
            wx.showToast({ title: '请输入有效的建筑面积', icon: 'none' });
            return;
        }
        // 复用完整造价表单补齐占地、楼层和结构参数，不推导用户尚未提供的成本条件。
        const url = `/pages/aicost/aicost?source=home_quick_estimate&projectType=${this.data.quickProjectType}&province=${encodeURIComponent(this.data.quickProvince)}&city=${encodeURIComponent(this.data.quickCity)}&buildingArea=${area}`;
        this.onOpenPage({ currentTarget: { dataset: { url } } });
    },
    showHomeIntroPopupOnce() {
        try {
            if (wx.getStorageSync(HOME_INTRO_POPUP_STORAGE_KEY)) {
                return;
            }
        }
        catch (_error) {
            // Storage failures should not block the homepage.
        }
        this.setData({
            homeIntroPopupVisible: true,
        });
    },
    rememberHomeIntroPopupSeen() {
        try {
            wx.setStorageSync(HOME_INTRO_POPUP_STORAGE_KEY, true);
        }
        catch (_error) {
            // Ignore storage write failures; closing the popup still works for this session.
        }
    },
    closeHomeIntroPopup() {
        this.rememberHomeIntroPopupSeen();
        this.setData({
            homeIntroPopupVisible: false,
        });
    },
    onHomeIntroTopup() {
        this.closeHomeIntroPopup();
        wx.navigateTo({
            url: '/pages/topupcenter/topupcenter',
            fail: () => {
                wx.showToast({
                    title: '充值中心跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onHomeIntroItemTap(e) {
        const { url } = e.currentTarget.dataset;
        if (!url) {
            return;
        }
        this.closeHomeIntroPopup();
        wx.navigateTo({
            url,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onHomeIntroContact() {
        this.closeHomeIntroPopup();
        void this.onContactService();
    },
    loadFeaturedGroups(force = false) {
        const now = Date.now();
        const hasFeaturedGroups = Array.isArray(this.data.featuredGroups) && this.data.featuredGroups.length > 0;
        const lastFetchTime = Number(this.data.lastFeaturedGroupsFetchTime || 0);
        if (this.data.featuredGroupsLoading) {
            return;
        }
        if (!force && hasFeaturedGroups && now - lastFetchTime < 60000) {
            return;
        }
        this.setData({
            featuredGroupsLoading: true,
        });
        const apiPath = '/api/v1/miniprogram/templates/featured';
        wx.request({
            url: `${API_BASE_URL}${apiPath}`,
            method: 'GET',
            success: (res) => {
                if (res.data && res.data.code === 0 && res.data.data && res.data.data.groups) {
                    const groups = Array.isArray(res.data.data.groups) ? res.data.data.groups : [];
                    this.setData({
                        featuredGroups: groups,
                        featuredGroupsLoading: false,
                        lastFeaturedGroupsFetchTime: now,
                    });
                    this.prefetchFeaturedGroups(groups);
                    return;
                }
                this.setData({
                    featuredGroups: hasFeaturedGroups ? this.data.featuredGroups : [],
                    featuredGroupsLoading: false,
                });
            },
            fail: (_error) => {
                this.setData({
                    featuredGroups: hasFeaturedGroups ? this.data.featuredGroups : [],
                    featuredGroupsLoading: false,
                });
            },
        });
    },
    prefetchFeaturedGroups(groups) {
        const cases = [];
        (Array.isArray(groups) ? groups : []).forEach((group) => {
            if (group === null || group === void 0 ? void 0 : group.case1) {
                cases.push(group.case1);
            }
            if (group === null || group === void 0 ? void 0 : group.case2) {
                cases.push(group.case2);
            }
        });
        const uniqueCases = cases.filter((item, index, list) => {
            const id = Number((item === null || item === void 0 ? void 0 : item.id) || 0);
            return id > 0 && list.findIndex((candidate) => Number((candidate === null || candidate === void 0 ? void 0 : candidate.id) || 0) === id) === index;
        });
        void (0, perf_1.prefetchImages)(uniqueCases.map((item) => (0, asset_1.normalizeCosUrl)(item.thumbnail || item.preview_url || '')), 2);
        uniqueCases.slice(0, 2).forEach((item) => {
            const templateId = Number((item === null || item === void 0 ? void 0 : item.id) || 0);
            const cacheKey = buildPublicTemplateDetailCacheKey(templateId);
            if (!templateId || (0, perf_1.getPageCache)(cacheKey) || featuredDetailPrefetching.has(templateId)) {
                return;
            }
            featuredDetailPrefetching.add(templateId);
            wx.request({
                url: `${API_BASE_URL}/api/v1/miniprogram/templates/${templateId}`,
                method: 'GET',
                success: (response) => {
                    const body = (response === null || response === void 0 ? void 0 : response.data) || {};
                    if ((response === null || response === void 0 ? void 0 : response.statusCode) === 200 && body.code === 0 && body.data) {
                        (0, perf_1.setPageCache)(cacheKey, body.data, TEMPLATE_DETAIL_CACHE_TTL);
                        void (0, perf_1.prefetchImages)([(0, asset_1.normalizeCosUrl)(body.data.preview_url || ''), (0, asset_1.normalizeCosUrl)(body.data.thumbnail || '')], 2);
                    }
                },
                complete: () => {
                    featuredDetailPrefetching.delete(templateId);
                },
            });
        });
    },
    onFeaturedCaseTap(e) {
        const id = e.currentTarget.dataset.id;
        if (!id) {
            return;
        }
        wx.navigateTo({
            url: `/pages/templatesquaredetails/templatesquaredetails?id=${id}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onViewLocalSpec() {
        wx.navigateTo({
            url: '/pages/utilitytools/utilitytools',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    async onContactService() {
        // 优先直接拉起企微客服，失败时再弹出选择弹窗
        const opened = await (0, enterpriseWechat_1.tryOpenCustomerServiceDirect)({
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        });
        if (opened) {
            return;
        }
        // 拉起失败，显示联系服务商弹窗（电话 + 二维码备用）
        this.setData({
            contactPopupVisible: true,
        });
        this.loadEnterpriseWechatServiceConfig();
    },
    closeContactPopup() {
        this.setData({
            contactPopupVisible: false,
        });
    },
    openWechatQrcode(forceReload = false) {
        this.setData({
            contactPopupVisible: false,
            wechatPopupVisible: true,
        });
        this.loadEnterpriseWechatServiceConfig(forceReload);
    },
    closeWechatQrcode() {
        this.setData({
            wechatPopupVisible: false,
        });
    },
    onManualServiceContact(_e) {
        this.setData({
            contactPopupVisible: false,
        });
    },
    onCallSupplier() {
        this.closeContactPopup();
        wx.makePhoneCall({
            phoneNumber: this.data.supplierPhone,
            fail: () => {
                wx.showToast({
                    title: '拨号失败，请稍后重试',
                    icon: 'none',
                });
            },
        });
    },
    previewWechatQrcode() {
        if (!this.data.supplierWechatQrAvailable) {
            wx.showToast({
                title: '请先补充二维码图片',
                icon: 'none',
            });
            return;
        }
        wx.previewImage({
            urls: [this.data.supplierWechatQr],
            current: this.data.supplierWechatQr,
        });
    },
    onWechatQrcodeError() {
        this.setData({
            supplierWechatQrAvailable: false,
        });
    },
    getEnterpriseWechatServiceConfig() {
        return {
            qrcodeUrl: this.data.supplierWechatQr,
            tip: this.data.enterpriseWechatTip,
            contact: this.data.enterpriseWechatContact,
            servicePhone: this.data.supplierPhone,
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        };
    },
    getEnterpriseCustomerServiceFailureMessage(reason) {
        switch (reason) {
            case 'missing_corp_id':
                return '未配置企业微信 CorpID，已切换到二维码备用';
            case 'missing_url':
                return '未配置企业微信客服链接，已切换到二维码备用';
            case 'api_unavailable':
                return '当前环境不支持打开官方企微客服，已切换到二维码备用';
            case 'open_failed':
                return '官方企微客服拉起失败，已切换到二维码备用';
            case 'missing_config':
            default:
                return '未拉起微信客服，已为你切换到二维码备用';
        }
    },
    async onOpenEnterpriseCustomerService() {
        await this.loadEnterpriseWechatServiceConfig(true);
        const serviceConfig = this.getEnterpriseWechatServiceConfig();
        const openResult = await (0, enterpriseWechat_1.openEnterpriseCustomerServiceChat)(serviceConfig);
        if (openResult.opened) {
            this.setData({
                contactPopupVisible: false,
                wechatPopupVisible: false,
            });
            return;
        }
        this.openWechatQrcode(true);
        wx.showToast({
            title: this.getEnterpriseCustomerServiceFailureMessage(openResult.reason),
            icon: 'none',
        });
    },
    async loadEnterpriseWechatServiceConfig(force = false) {
        if (this.data.enterpriseWechatConfigLoading && !force) {
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            // 未登录时也不阻断，使用默认 corpId/url 已在 data 中初始化
            this.setData({ enterpriseWechatConfigLoading: false });
            return;
        }
        this.setData({
            enterpriseWechatConfigLoading: true,
        });
        try {
            const configData = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/download-config`,
                    method: 'GET',
                    header: {
                        token,
                    },
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
                qrcodeUrl: this.data.supplierWechatQr,
                tip: this.data.enterpriseWechatTip,
                contact: this.data.enterpriseWechatContact,
                servicePhone: this.data.supplierPhone,
                customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
                customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
            });
            this.setData({
                supplierWechatQr: serviceConfig.qrcodeUrl,
                supplierWechatQrAvailable: !!serviceConfig.qrcodeUrl,
                enterpriseWechatTip: serviceConfig.tip || this.data.enterpriseWechatTip,
                enterpriseWechatContact: serviceConfig.contact,
                supplierPhone: serviceConfig.servicePhone || enterpriseWechat_1.DEFAULT_ENTERPRISE_SERVICE_PHONE,
                enterpriseWechatCustomerServiceCorpId: serviceConfig.customerServiceCorpId,
                enterpriseWechatCustomerServiceUrl: serviceConfig.customerServiceUrl,
                enterpriseWechatConfigLoading: false,
            });
        }
        catch (error) {
            console.error('加载企业微信人工客服配置失败:', error);
            this.setData({
                enterpriseWechatConfigLoading: false,
            });
        }
    },
    // 小程序原生客服按钮 bindcontact 回调
    onNativeContactSuccess() {
        this.setData({
            contactPopupVisible: false,
            wechatPopupVisible: false,
        });
    },
    // 智能客服：尝试拉起企微客服，失败弹二维码
    async onSmartServiceTap() {
        const { openEnterpriseCustomerServiceChat, getDefaultServiceConfig } = require('../../utils/enterpriseWechat');
        const config = getDefaultServiceConfig({
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        });
        const result = await openEnterpriseCustomerServiceChat(config);
        if (!result.opened) {
            // 企微客服拉起失败，弹出二维码弹窗
            this.setData({ contactPopupVisible: false, wechatPopupVisible: true });
        }
    },
    // 智能客服吉祥物点击：弹跳动画后跳转客服页面
    onMascotTap() {
        if (this.data.mascotBouncing) {
            return;
        }
        this.setData({ mascotBouncing: true });
        setTimeout(() => {
            this.setData({ mascotBouncing: false });
            wx.navigateTo({
                url: '/pages/customerservice/customerservice?source=home_mascot',
                fail: () => {
                    wx.showToast({ title: '页面跳转失败', icon: 'none' });
                },
            });
        }, 400);
    },
    noop() { },
});
