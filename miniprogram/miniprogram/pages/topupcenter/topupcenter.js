"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/topupcenter/topupcenter.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const asset_1 = require("../../utils/asset");
const virtualpay_1 = require("../../utils/virtualpay");
const enterpriseWechat_1 = require("../../utils/enterpriseWechat");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com';
const WECHAT_PAY_ICON = (0, asset_1.resolveAssetPath)('/assets/topupcenter/wechat-pay.svg');
const ALIPAY_PAY_ICON = (0, asset_1.resolveAssetPath)('/assets/topupcenter/alipay-pay.svg');
const CUSTOM_RECHARGE_TYPE = 'enterprise_custom';
const ENTERPRISE_RECHARGE_MIN_AMOUNT = 500;
const CUSTOM_RECHARGE_TITLE = '钻石会员';
const DEFAULT_ENTERPRISE_WECHAT_TIP = '扫码添加企业微信，备注“钻石会员”，由客服为你确认定制套餐方案。';
const EMPTY_RECHARGE_PLAN = {
    type: '',
    title: '请选择充值方案',
    amount: 0,
    amountFen: 0,
    amountText: '--',
    stones: 0,
    stonesText: '',
};
const DEFAULT_RECHARGE_PLANS = [
    {
        type: 'basic',
        title: '普通会员',
        amount: 50,
        amountFen: 5000,
        amountText: '50',
        stones: 500,
        stonesText: '500灵石',
    },
    {
        type: 'discount',
        title: '白银会员',
        amount: 300,
        amountFen: 30000,
        amountText: '300',
        stones: 3000,
        stonesText: '3000灵石',
        recommended: true,
    },
    {
        type: 'super',
        title: '黄金会员',
        amount: 500,
        amountFen: 50000,
        amountText: '500',
        stones: 5000,
        stonesText: '5000灵石',
    },
];
function buildCustomRechargePlan(customAmount = 0) {
    const amount = Number.isFinite(customAmount) && customAmount > 0 ? customAmount : 0;
    return {
        type: CUSTOM_RECHARGE_TYPE,
        title: CUSTOM_RECHARGE_TITLE,
        amount,
        amountFen: amount > 0 ? Math.round(amount * 100) : 0,
        amountText: amount > 0 ? formatPlanAmount(amount) : '输入金额',
        stones: 0,
        stonesText: '支付后联系客服人工到账',
        custom: true,
    };
}
function appendCustomRechargePlan(plans) {
    const nextPlans = (Array.isArray(plans) ? plans : []).map((item) => ({ ...item }));
    const hasCustomPlan = nextPlans.some((item) => item.type === CUSTOM_RECHARGE_TYPE);
    if (!hasCustomPlan) {
        nextPlans.push(buildCustomRechargePlan());
    }
    return nextPlans;
}
const DEFAULT_PAYMENT_METHODS = [
    { key: 'wechat', label: '微信支付', shortLabel: '微', icon: WECHAT_PAY_ICON, enabled: true },
    { key: 'alipay', label: '支付宝支付', shortLabel: '支', icon: ALIPAY_PAY_ICON, enabled: false, badgeText: '即将开放', badgeTone: 'disabled' },
];
function formatPlanAmount(amount) {
    if (Number.isInteger(amount)) {
        return String(amount);
    }
    return amount.toFixed(2);
}
function normalizeRechargePlan(item, index) {
    const amountValue = Number(item?.amount);
    const amountFenValue = Number(item?.amount_fen);
    const stonesValue = Number(item?.stones);
    const amount = Number.isFinite(amountValue) && amountValue > 0
        ? amountValue
        : Number.isFinite(amountFenValue) && amountFenValue > 0
            ? amountFenValue / 100
            : 0;
    const stones = Number.isFinite(stonesValue) && stonesValue > 0 ? stonesValue : 0;
    if (amount <= 0 || stones <= 0) {
        return null;
    }
    const fallbackTypes = ['basic', 'discount', 'super'];
    return {
        type: String(item?.type || fallbackTypes[index] || `plan_${index + 1}`),
        title: String(item?.title || `充值档位${index + 1}`),
        amount,
        amountFen: Number.isFinite(amountFenValue) && amountFenValue > 0 ? amountFenValue : Math.round(amount * 100),
        amountText: String(item?.amount_text || formatPlanAmount(amount)),
        stones,
        stonesText: String(item?.stones_text || `${stones}灵石`),
        recommended: Boolean(item?.recommended),
        membershipPlanCode: String(item?.membership_plan_code || '').trim(),
        membershipTitle: String(item?.membership_title || '').trim(),
        membershipBadgeText: String(item?.membership_badge_text || '').trim(),
        membershipDescription: String(item?.membership_description || '').trim(),
        membershipDurationDays: Number(item?.membership_duration_days || 0) || 0,
        membershipBenefitText: String(item?.membership_benefit_text || '').trim(),
    };
}
function resolveRechargePlans(rawPlans) {
    if (!Array.isArray(rawPlans)) {
        return appendCustomRechargePlan(DEFAULT_RECHARGE_PLANS.map((item) => ({ ...item })));
    }
    const plans = rawPlans
        .map((item, index) => normalizeRechargePlan(item, index))
        .filter((item) => Boolean(item));
    if (!plans.length) {
        return appendCustomRechargePlan(DEFAULT_RECHARGE_PLANS.map((item) => ({ ...item })));
    }
    return appendCustomRechargePlan(plans);
}
function getExchangeRateText(plan) {
    if (plan?.custom) {
        return plan.amount > 0 ? '支付成功后联系客服确认灵石到账' : '钻石会员定制套餐 · 请联系客服确认';
    }
    const amount = Number(plan?.amount || 0);
    const stones = Number(plan?.stones || 0);
    if (amount > 0 && stones > 0) {
        const rate = stones / amount;
        const rateText = Number.isInteger(rate) ? String(rate) : rate.toFixed(2);
        return `1 元 ≈ ${rateText} 灵石`;
    }
    return '1 元 = 10 灵石';
}
function getDefaultMembershipSummary() {
    return {
        membershipSummaryTitle: '未开通下载会员',
        membershipSummaryHint: '充值后可开通下载权限，详情以当前套餐说明为准。',
        membershipSummaryTag: '未开通',
    };
}
function buildMembershipSummary(status) {
    if (status?.status === 'active' && status.plan_title) {
        if (status.lifetime_membership) {
            return {
                membershipSummaryTitle: status.plan_title,
                membershipSummaryHint: '你当前已开通下载权限，可直接下载模板图片。',
                membershipSummaryTag: '已开通',
            };
        }
        return {
            membershipSummaryTitle: status.plan_title,
            membershipSummaryHint: status.expired_at_text
                ? `会员有效期至 ${status.expired_at_text}`
                : '已开通模板下载会员，可直接下载模板图片。',
            membershipSummaryTag: status.remaining_days ? `剩余${status.remaining_days}天` : '已开通',
        };
    }
    if (status?.legacy_recharge_member) {
        return {
            membershipSummaryTitle: '已开通下载权限',
            membershipSummaryHint: '你当前可直接下载模板图片。',
            membershipSummaryTag: '已开通',
        };
    }
    if (status?.status === 'expired') {
        return {
            membershipSummaryTitle: status.plan_title || '下载会员已过期',
            membershipSummaryHint: '你的模板下载会员已到期，可重新选择充值档位继续开通。',
            membershipSummaryTag: '已过期',
        };
    }
    return getDefaultMembershipSummary();
}
function normalizeRecordTime(value) {
    const text = String(value || '').trim();
    if (!text) {
        return '--';
    }
    return text.length > 16 ? text.slice(0, 16) : text;
}
function formatSignedAmount(value) {
    const num = Number(value || 0);
    if (Number.isNaN(num)) {
        return '0.00';
    }
    const fixed = Math.abs(num).toFixed(2);
    return `${num >= 0 ? '+' : '-'}${fixed}`;
}
function getPaymentModeText(paymentMode) {
    const mode = String(paymentMode || '').trim();
    if (mode === 'wechat_only')
        return '小程序内微信支付';
    if (mode === 'alipay_only')
        return '支付宝支付';
    if (mode === 'wechat_alipay')
        return '微信 / 支付宝';
    if (mode === 'static_qrcode')
        return '收款码支付';
    return '小程序内微信支付';
}
function buildPaymentMethods(paymentMode) {
    const mode = String(paymentMode || '').trim() || 'wechat_only';
    const wechatEnabled = mode === 'wechat_only' || mode === 'wechat_alipay';
    const wechatBadgeText = wechatEnabled ? undefined : mode === 'static_qrcode' ? '当前不可直付' : '当前不可用';
    return [
        {
            key: 'wechat',
            label: '微信支付',
            shortLabel: '微',
            icon: WECHAT_PAY_ICON,
            enabled: wechatEnabled,
            badgeText: wechatBadgeText,
            badgeTone: wechatEnabled ? undefined : 'disabled',
        },
        {
            key: 'alipay',
            label: '支付宝支付',
            shortLabel: '支',
            icon: ALIPAY_PAY_ICON,
            enabled: false,
            badgeText: '即将开放',
            badgeTone: 'disabled',
        },
    ];
}
function getPaymentHintText(paymentMode, configData) {
    const note = String(configData?.note || '').trim();
    if (note) {
        return note;
    }
    const mode = String(paymentMode || '').trim();
    if (mode === 'wechat_only' || !mode) {
        return '当前已开启小程序内微信支付，点击下方按钮即可直接调起微信支付并自动到账。';
    }
    if (mode === 'wechat_alipay') {
        return '当前后台支持微信和支付宝，但小程序端当前直接使用微信支付。';
    }
    if (mode === 'alipay_only') {
        return '当前后台配置为支付宝支付，小程序端暂不支持直接发起，请联系管理员处理。';
    }
    if (mode === 'static_qrcode') {
        return '当前后台配置为收款码支付，小程序端暂不支持直接拉起支付，请联系管理员或切换为微信支付配置。';
    }
    return '支付配置加载成功，请按页面提示操作。';
}
Page({
    /**
     * 页面的初始数据
     */
    data: {
        selectedRecharge: 'discount',
        rechargePlans: appendCustomRechargePlan(DEFAULT_RECHARGE_PLANS),
        selectedPlan: DEFAULT_RECHARGE_PLANS[1],
        paymentMethods: DEFAULT_PAYMENT_METHODS,
        selectedPaymentMethod: 'wechat',
        customRechargeAmountInput: '',
        customRechargeAmount: 0,
        enterpriseRechargeMinAmount: ENTERPRISE_RECHARGE_MIN_AMOUNT,
        previewOrderNo: '',
        rechargeRecords: [],
        recordEmptyText: '登录后可查看充值记录',
        navSafeTop: 0,
        navBarHeight: 96,
        navContentHeight: 44,
        navSideWidth: 88,
        rechargeConfig: null,
        paymentMode: 'wechat_only',
        configData: null,
        paymentModeText: '小程序内微信支付',
        paymentHintText: '当前已开启小程序内微信支付，点击下方按钮即可直接调起微信支付并自动到账。',
        exchangeRateText: getExchangeRateText(DEFAULT_RECHARGE_PLANS[1]),
        enterpriseContactPopupVisible: false,
        supplierWechatQr: enterpriseWechat_1.DEFAULT_ENTERPRISE_WECHAT_QRCODE,
        supplierWechatQrAvailable: true,
        enterpriseWechatTip: DEFAULT_ENTERPRISE_WECHAT_TIP,
        enterpriseWechatContact: '',
        supplierPhone: enterpriseWechat_1.DEFAULT_ENTERPRISE_SERVICE_PHONE,
        enterpriseWechatCustomerServiceCorpId: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_CORP_ID,
        enterpriseWechatCustomerServiceUrl: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_URL,
        enterpriseWechatConfigLoading: false,
        currentMembership: null,
        membershipSummaryTitle: getDefaultMembershipSummary().membershipSummaryTitle,
        membershipSummaryHint: getDefaultMembershipSummary().membershipSummaryHint,
        membershipSummaryTag: getDefaultMembershipSummary().membershipSummaryTag,
    },
    lastEnterpriseTapAt: 0,
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad() {
        this.initNavLayout();
        this.setData({ previewOrderNo: '' });
        this.syncSelectedPlan();
        this.loadRechargeConfig();
        this.loadRechargeRecords();
        this.loadMembershipStatus();
    },
    /**
     * 生命周期函数--监听页面初次渲染完成
     */
    onReady() {
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShow() {
        this.setData({ previewOrderNo: '' });
        this.syncSelectedPlan();
        this.loadRechargeConfig();
        this.loadRechargeRecords();
        this.loadMembershipStatus();
    },
    /**
     * 生命周期函数--监听页面隐藏
     */
    onHide() {
    },
    /**
     * 生命周期函数--监听页面卸载
     */
    onUnload() {
    },
    initNavLayout() {
        try {
            const systemInfo = wx.getSystemInfoSync();
            const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
                ? wx.getMenuButtonBoundingClientRect()
                : null;
            const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
            if (menuRect) {
                const navBarHeight = Number(menuRect.bottom + menuRect.top - safeTop);
                const navContentHeight = Number(menuRect.height);
                const navSideWidth = Number(systemInfo.windowWidth - menuRect.left);
                this.setData({
                    navSafeTop: safeTop,
                    navBarHeight,
                    navContentHeight,
                    navSideWidth,
                });
                return;
            }
            this.setData({
                navSafeTop: safeTop,
                navBarHeight: safeTop + 44,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
        catch (error) {
            this.setData({
                navSafeTop: 20,
                navBarHeight: 64,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
    },
    async loadMembershipStatus() {
        const token = String(wx.getStorageSync('token') || '').trim();
        if (!token) {
            this.setData({
                currentMembership: null,
                ...getDefaultMembershipSummary(),
            });
            return;
        }
        try {
            const apiPath = '/api/v1/miniprogram/user/membership';
            const requestBody = {};
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'GET',
                    header: headers,
                    success: (response) => {
                        if (response.statusCode === 200 && response.data) {
                            const data = response.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '获取会员状态失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${response.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const membershipData = (res || {});
            this.setData({
                currentMembership: membershipData,
                ...buildMembershipSummary(membershipData),
            });
        }
        catch (error) {
            console.error('加载会员状态失败:', error);
            this.setData({
                currentMembership: null,
                ...getDefaultMembershipSummary(),
            });
        }
    },
    findRechargePlan(type) {
        const planType = typeof type === 'string' ? type : this.data.selectedRecharge;
        if (!planType) {
            return null;
        }
        if (planType === CUSTOM_RECHARGE_TYPE) {
            return buildCustomRechargePlan(Number(this.data.customRechargeAmount || 0));
        }
        const list = (this.data.rechargePlans || []);
        return list.find((item) => item.type === planType) || null;
    },
    syncSelectedPlan(type) {
        const nextPlan = this.findRechargePlan(type);
        if (!nextPlan) {
            this.setData({
                selectedRecharge: '',
                selectedPlan: { ...EMPTY_RECHARGE_PLAN },
                exchangeRateText: '请选择充值方案后查看汇率',
            });
            return;
        }
        this.setData({
            selectedRecharge: nextPlan.type,
            selectedPlan: nextPlan,
            exchangeRateText: getExchangeRateText(nextPlan),
        });
    },
    getRecordTitle(item) {
        const type = item.type;
        if (type === 'recharge')
            return '充值到账';
        if (type === 'checkin')
            return '签到奖励';
        if (type === 'task')
            return item.title || '活动奖励';
        if (item.scene === 'ai_draw_single' || item.scene === 'ai_draw_multi')
            return 'AI绘图消耗';
        if (item.scene && item.scene.indexOf('ai_chat') >= 0)
            return 'AI聊天消耗';
        return item.title || '灵石变动';
    },
    /**
     * 页面相关事件处理函数--监听用户下拉动作
     */
    onPullDownRefresh() {
        Promise.allSettled([this.loadRechargeConfig(), this.loadRechargeRecords(), this.loadMembershipStatus()]).finally(() => {
            wx.stopPullDownRefresh();
        });
    },
    /**
     * 页面上拉触底事件的处理函数
     */
    onReachBottom() {
    },
    /**
     * 用户点击右上角分享
     */
    onShareAppMessage() {
        return {
            title: '充值中心',
            path: '/pages/topupcenter/topupcenter'
        };
    },
    /**
     * 选择充值选项
     */
    onSelectRecharge(e) {
        const type = e.currentTarget.dataset.type;
        const isEnterprisePlan = type === CUSTOM_RECHARGE_TYPE;
        const now = Date.now();
        const isDoubleTap = isEnterprisePlan && now - this.lastEnterpriseTapAt <= 360;
        this.lastEnterpriseTapAt = isEnterprisePlan ? now : 0;
        this.setData({
            previewOrderNo: '',
        }, () => this.syncSelectedPlan(type));
        if (isDoubleTap) {
            this.openEnterpriseContactPopup();
        }
    },
    noop() {
    },
    onCustomRechargeAmountInput(e) {
        const rawValue = String(e.detail?.value || '').replace(/\D/g, '').slice(0, 6);
        const amount = rawValue ? Number(rawValue) : 0;
        this.setData({
            customRechargeAmountInput: rawValue,
            customRechargeAmount: amount,
            previewOrderNo: '',
        }, () => {
            if (this.data.selectedRecharge === CUSTOM_RECHARGE_TYPE) {
                this.syncSelectedPlan(CUSTOM_RECHARGE_TYPE);
            }
        });
    },
    onCustomRechargeFocus() {
        if (this.data.selectedRecharge !== CUSTOM_RECHARGE_TYPE) {
            this.setData({
                previewOrderNo: '',
            }, () => this.syncSelectedPlan(CUSTOM_RECHARGE_TYPE));
            return;
        }
        this.setData({
            previewOrderNo: '',
        });
    },
    getValidatedSelectedPlan() {
        const selectedPlan = this.findRechargePlan();
        if (!selectedPlan) {
            wx.showToast({
                title: '请选择充值选项',
                icon: 'none',
            });
            return null;
        }
        if (selectedPlan.custom) {
            const customAmount = Number(this.data.customRechargeAmount || 0);
            if (!Number.isFinite(customAmount) || customAmount <= 0) {
                wx.showToast({
                    title: '请输入自定义金额',
                    icon: 'none',
                });
                return null;
            }
            if (customAmount <= ENTERPRISE_RECHARGE_MIN_AMOUNT) {
                wx.showToast({
                    title: '企业级客户充值金额需大于500元',
                    icon: 'none',
                });
                return null;
            }
            return buildCustomRechargePlan(customAmount);
        }
        if (!selectedPlan.amount) {
            wx.showToast({
                title: '请选择充值选项',
                icon: 'none',
            });
            return null;
        }
        return selectedPlan;
    },
    onRechargeByPlan(e) {
        const type = e.currentTarget.dataset.type;
        this.setData({
            selectedRecharge: type
        }, () => {
            this.syncSelectedPlan(type);
            const nextPlan = this.findRechargePlan(type);
            if (this.data.selectedPaymentMethod !== 'wechat') {
                wx.showToast({
                    title: '当前小程序端仅支持微信支付',
                    icon: 'none',
                });
                return;
            }
            if (type === CUSTOM_RECHARGE_TYPE && !this.data.customRechargeAmount) {
                wx.showToast({
                    title: '请输入自定义金额',
                    icon: 'none',
                });
                return;
            }
            if (!nextPlan || !nextPlan.amount) {
                return;
            }
            this.doWechatJSAPIPay(nextPlan);
        });
    },
    onSelectPayment(e) {
        const key = e.currentTarget.dataset.key;
        const paymentMethods = (this.data.paymentMethods || []);
        const currentMethod = paymentMethods.find((item) => item.key === key);
        if (!currentMethod) {
            return;
        }
        if (!currentMethod.enabled) {
            wx.showToast({
                title: '该支付方式暂未开放',
                icon: 'none'
            });
            return;
        }
        this.setData({
            selectedPaymentMethod: key,
            paymentModeText: currentMethod.label,
        });
    },
    onOpenWalletRecords() {
        wx.navigateTo({
            url: '/pages/wallet/wallet',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none'
                });
            }
        });
    },
    openEnterpriseContactPopup() {
        this.setData({
            enterpriseContactPopupVisible: true,
        });
        this.loadEnterpriseWechatServiceConfig();
    },
    closeEnterpriseContactPopup() {
        this.setData({
            enterpriseContactPopupVisible: false,
        });
    },
    onWechatQrcodeError() {
        this.setData({
            supplierWechatQrAvailable: false,
        });
    },
    saveImageToAlbum(url, loadingTitle, successTitle) {
        wx.showLoading({ title: loadingTitle });
        wx.getImageInfo({
            src: url,
            success: (res) => {
                if (!res.path) {
                    wx.hideLoading();
                    wx.showToast({ title: '保存失败', icon: 'none' });
                    return;
                }
                wx.saveImageToPhotosAlbum({
                    filePath: res.path,
                    success: () => {
                        wx.hideLoading();
                        wx.showToast({ title: successTitle, icon: 'success' });
                    },
                    fail: (err) => {
                        wx.hideLoading();
                        if (err?.errMsg && err.errMsg.includes('auth deny')) {
                            wx.showModal({
                                title: '提示',
                                content: '需要授权相册权限才能保存图片',
                                confirmText: '去设置',
                                success: (modalRes) => {
                                    if (modalRes.confirm) {
                                        wx.openSetting();
                                    }
                                }
                            });
                            return;
                        }
                        wx.showToast({ title: '保存失败', icon: 'none' });
                    }
                });
            },
            fail: () => {
                wx.hideLoading();
                wx.showToast({ title: '保存失败', icon: 'none' });
            }
        });
    },
    saveSupplierWechatQrcode() {
        if (!this.data.supplierWechatQrAvailable || !this.data.supplierWechatQr) {
            wx.showToast({ title: '暂未配置企业微信二维码', icon: 'none' });
            return;
        }
        this.saveImageToAlbum(this.data.supplierWechatQr, '保存二维码中...', '二维码已保存');
    },
    async loadEnterpriseWechatServiceConfig(force = false) {
        if (this.data.enterpriseWechatConfigLoading && !force) {
            return;
        }
        const token = String(wx.getStorageSync('token') || '').trim();
        if (!token) {
            // 未登录时不阻断，使用默认 corpId/url 已在 data 中初始化
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
                    header: { token },
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
                tip: DEFAULT_ENTERPRISE_WECHAT_TIP,
                contact: this.data.enterpriseWechatContact,
                servicePhone: this.data.supplierPhone,
                customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
                customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
            });
            this.setData({
                supplierWechatQr: serviceConfig.qrcodeUrl,
                supplierWechatQrAvailable: !!serviceConfig.qrcodeUrl,
                enterpriseWechatTip: serviceConfig.tip || DEFAULT_ENTERPRISE_WECHAT_TIP,
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
    /**
     * tabbar 切换
     */
    onTabSwitch(_e) {
    },
    /**
     * 加载充值配置
     */
    async loadRechargeConfig() {
        try {
            const url = `${API_BASE_URL}/api/v1/miniprogram/recharge/config`;
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url,
                    method: 'GET',
                    header: {
                        'Content-Type': 'application/json',
                    },
                    success: (response) => {
                        if (response.statusCode === 200 && response.data) {
                            const data = response.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '加载失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${response.statusCode}`));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
            const responseData = (res || {});
            const paymentMode = String(responseData.payment_mode || 'wechat_only').trim() || 'wechat_only';
            const configData = responseData.config_data || null;
            const rechargePlans = resolveRechargePlans(responseData.plans);
            const paymentMethods = buildPaymentMethods(paymentMode);
            const selectedPaymentMethod = paymentMethods.find((item) => item.enabled)?.key || '';
            const currentSelectedType = String(this.data.selectedRecharge || '').trim();
            const nextSelectedType = currentSelectedType
                ? rechargePlans.find((item) => item.type === currentSelectedType)?.type || rechargePlans[0]?.type || ''
                : '';
            this.setData({
                rechargeConfig: res,
                rechargePlans,
                paymentMode,
                configData,
                paymentMethods,
                selectedPaymentMethod,
                paymentModeText: getPaymentModeText(paymentMode),
                paymentHintText: getPaymentHintText(paymentMode, configData),
            }, () => this.syncSelectedPlan(nextSelectedType));
        }
        catch (error) {
            console.error('加载充值配置失败:', error);
            this.setData({
                rechargePlans: appendCustomRechargePlan(DEFAULT_RECHARGE_PLANS.map((item) => ({ ...item }))),
                paymentMode: 'wechat_only',
                configData: null,
                paymentMethods: buildPaymentMethods('wechat_only'),
                selectedPaymentMethod: 'wechat',
                paymentModeText: getPaymentModeText('wechat_only'),
                paymentHintText: getPaymentHintText('wechat_only', null),
            }, () => this.syncSelectedPlan());
        }
    },
    async loadRechargeRecords() {
        const token = wx.getStorageSync('token');
        if (!token) {
            this.setData({
                rechargeRecords: [],
                recordEmptyText: '登录后查看充值记录',
            });
            return;
        }
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/stones/details`,
                    method: 'POST',
                    header: {
                        token,
                        'Content-Type': 'application/json',
                    },
                    data: {
                        page: 1,
                        page_size: 5,
                    },
                    success: (response) => {
                        if (response.statusCode === 200 && response.data) {
                            const data = response.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '获取记录失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${response.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const list = Array.isArray(res.list) ? res.list : [];
            const rechargeList = list.filter((item) => item.type === 'recharge');
            const sourceList = rechargeList.length > 0 ? rechargeList : list;
            const records = sourceList.slice(0, 5).map((item, index) => {
                const amount = Number(item.amount || 0);
                return {
                    id: item.id || `record-${index}`,
                    title: this.getRecordTitle(item),
                    time: normalizeRecordTime(item.created_at || item.time || item.updated_at),
                    amountText: formatSignedAmount(amount),
                    amountClass: amount >= 0 ? 'income' : 'expense',
                };
            });
            this.setData({
                rechargeRecords: records,
                recordEmptyText: records.length ? '' : '暂时还没有充值记录',
            });
        }
        catch (error) {
            console.error('加载充值记录失败:', error);
            this.setData({
                rechargeRecords: [],
                recordEmptyText: '充值记录加载失败，请下拉重试',
            });
        }
    },
    onRecharge() {
        if (this.data.selectedPaymentMethod !== 'wechat') {
            wx.showToast({
                title: '当前小程序端仅支持微信支付',
                icon: 'none',
            });
            return;
        }
        const selectedPlan = this.getValidatedSelectedPlan();
        if (!selectedPlan) {
            return;
        }
        const amount = Number(selectedPlan.amount || 0);
        const isEnterpriseRecharge = Boolean(selectedPlan.custom || selectedPlan.type === CUSTOM_RECHARGE_TYPE);
        if (isEnterpriseRecharge && amount <= ENTERPRISE_RECHARGE_MIN_AMOUNT) {
            wx.showToast({ title: '企业级客户充值金额需大于500元', icon: 'none' });
            return;
        }
        this.doWechatJSAPIPay(selectedPlan);
    },
    async doWechatJSAPIPay(selectedPlan) {
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showModal({
                title: '提示',
                content: '请先登录',
                confirmText: '去登录',
                success: (res) => {
                    if (res.confirm)
                        wx.navigateTo({ url: '/pages/login/login' });
                },
            });
            return;
        }
        const amount = Number(selectedPlan.amount || 0);
        const isEnterpriseRecharge = Boolean(selectedPlan.custom || selectedPlan.type === CUSTOM_RECHARGE_TYPE);
        const amountFen = Math.round(amount * 100);
        if (isEnterpriseRecharge && amount <= ENTERPRISE_RECHARGE_MIN_AMOUNT) {
            wx.showToast({
                title: '企业级客户充值金额需大于500元',
                icon: 'none',
            });
            return;
        }
        // 决策 D4：企业充值改走线下对公 + 客服核对，不再小程序内自助下单
        if (isEnterpriseRecharge) {
            wx.showModal({
                title: '企业充值',
                content: '企业级充值需走对公转账，请联系客服确认金额、抬头与到账。',
                showCancel: true,
                cancelText: '稍后',
                confirmText: '联系客服',
                success: (res) => {
                    if (res.confirm) {
                        this.openEnterpriseCustomerServiceWithFallback(true);
                    }
                },
            });
            return;
        }
        wx.showLoading({ title: '准备支付...', mask: true });
        try {
            // 普通灵石充值改走虚拟支付：1 元 = 10 灵石（取消档位赠送 D1 决策）
            const result = await (0, virtualpay_1.requestVirtualPay)({
                planType: 'recharge',
                amountFen,
            });
            wx.hideLoading();
            if (!result.success) {
                wx.showToast({ title: result.errMsg || '支付未完成', icon: 'none' });
                return;
            }
            this.setData({
                previewOrderNo: String(result.orderNo || ''),
            });
            this.loadRechargeRecords();
            this.loadMembershipStatus();
            this.resetRechargeSelection();
            const payState = String(result.state || '').toUpperCase();
            if (payState !== 'PAID') {
                const title = payState === 'CLOSED' || payState === 'REVOKED' ? '支付未完成' : '支付已提交，到账处理中';
                wx.showToast({ title, icon: 'none', duration: 1800 });
                return;
            }
            // 普通充值支付成功后也尝试拉起客服，失败时显示客服弹窗
            wx.showToast({ title: '支付成功', icon: 'success', duration: 1500 });
            setTimeout(async () => {
                const opened = await (0, enterpriseWechat_1.tryOpenCustomerServiceDirect)({
                    customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
                    customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
                });
                if (!opened) {
                    // 企微客服拉起失败，显示弹窗（弹窗内有小程序原生客服按钮）
                    this.setData({ enterpriseContactPopupVisible: true });
                    this.loadEnterpriseWechatServiceConfig();
                }
            }, 1800);
        }
        catch (e) {
            wx.hideLoading();
            if (e.errMsg && e.errMsg.indexOf('cancel') >= 0) {
                return;
            }
            wx.showToast({ title: e.message || e.errMsg || '支付失败', icon: 'none' });
        }
    },
    resetRechargeSelection() {
        this.lastEnterpriseTapAt = 0;
        this.setData({
            selectedRecharge: '',
            selectedPlan: { ...EMPTY_RECHARGE_PLAN },
            customRechargeAmountInput: '',
            customRechargeAmount: 0,
            previewOrderNo: '',
            exchangeRateText: '请选择充值方案后查看汇率',
            enterpriseContactPopupVisible: false,
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
    async openEnterpriseCustomerServiceWithFallback(forceLoad = false) {
        if (forceLoad) {
            await this.loadEnterpriseWechatServiceConfig(true);
        }
        const serviceConfig = this.getEnterpriseWechatServiceConfig();
        const openResult = await (0, enterpriseWechat_1.openEnterpriseCustomerServiceChat)(serviceConfig);
        if (openResult.opened) {
            this.closeEnterpriseContactPopup();
            return true;
        }
        this.setData({
            enterpriseContactPopupVisible: true,
        });
        if ((0, enterpriseWechat_1.canOpenEnterpriseCustomerService)(serviceConfig)) {
            wx.showToast({
                title: '未拉起微信客服，请使用二维码备用',
                icon: 'none',
            });
        }
        return false;
    },
    async onOpenEnterpriseCustomerService() {
        await this.openEnterpriseCustomerServiceWithFallback(true);
    },
    onCallSupplier() {
        if (!this.data.supplierPhone) {
            wx.showToast({ title: '暂未配置联系电话', icon: 'none' });
            return;
        }
        wx.makePhoneCall({
            phoneNumber: this.data.supplierPhone,
            fail: () => {
                wx.showToast({ title: '拨号失败，请稍后重试', icon: 'none' });
            },
        });
    },
    // 小程序原生客服按钮 bindcontact 回调
    onNativeContactSuccess() {
        this.setData({ enterpriseContactPopupVisible: false });
    },
    // 智能客服：尝试拉起企微客服，失败弹二维码
    async onSmartServiceTap() {
        const config = {
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        };
        const result = await (0, enterpriseWechat_1.openEnterpriseCustomerServiceChat)(config);
        if (!result.opened) {
            this.setData({ enterpriseContactPopupVisible: false });
            if (this.data.supplierWechatQrAvailable && this.data.supplierWechatQr) {
                wx.previewImage({
                    urls: [this.data.supplierWechatQr],
                    current: this.data.supplierWechatQr,
                });
            }
            else {
                wx.showToast({ title: '暂未配置企业微信二维码', icon: 'none' });
            }
        }
    },
});
