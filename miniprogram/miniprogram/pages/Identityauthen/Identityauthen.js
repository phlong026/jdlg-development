"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/Identityauthen/Identityauthen.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const IDENTITY_AUTHEN_API_BASE_URL = 'https://api.jiadilingguang.com';
const EXCHANGE_RATIO = 10;
Page({
    data: {
        // 顶部概览文案
        summaryText: '当前认证信息将用于展示在个人主页与作品中',
        // 实名认证状态：'未认证' | '已认证'
        realNameStatus: '未认证',
        // 设计师认证状态：'none' | 'review' | 'approved'
        designerStatus: 'none',
        // 施工队认证状态：'none' | 'review' | 'approved'
        contractorStatus: 'none',
        // 企业/机构认证状态：'none' | 'review' | 'approved'
        companyStatus: 'none',
        // 认证详情（脱敏）
        certType: '',
        certIdentityType: '',
        certRealNameMasked: '',
        certIDCardMasked: '',
        canWithdraw: false,
        withdrawRmbAmount: '0.00',
    },
    onLoad() {
        this.loadCertificationStatus();
    },
    onShow() {
        this.loadCertificationStatus();
    },
    // 拉取认证状态与实名详情（含脱敏）
    loadCertificationStatus() {
        const token = wx.getStorageSync('token');
        if (!token) {
            this.setData({
                canWithdraw: false,
                withdrawRmbAmount: '0.00',
            });
            this.updateStatusClasses();
            return;
        }
        const apiPath = '/api/v1/miniprogram/certification/status';
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const params = (0, parameter_1.generateRequestParams)(token, {}, apiPath, deviceID);
        const headers = {
            ...(0, parameter_1.paramsToHeaders)(params),
            'Content-Type': 'application/json',
        };
        wx.request({
            url: `${IDENTITY_AUTHEN_API_BASE_URL}${apiPath}`,
            method: 'GET',
            header: headers,
            success: (resp) => {
                if (resp.statusCode !== 200 || !resp.data) {
                    this.updateStatusClasses();
                    return;
                }
                const body = resp.data;
                if (body.code !== 0) {
                    this.updateStatusClasses();
                    return;
                }
                const data = body.data || {};
                const status = data.status || '';
                const certType = data.cert_type || '';
                const identityType = data.identity_type || '';
                const statusValue = status === 'approved' ? 'approved' : (status === 'pending_review' ? 'review' : 'none');
                const isContractorCertification = certType === 'designer' && identityType === '施工队';
                const realNameStatus = status === 'approved' ? '已认证' : (status ? '审核中' : '未认证');
                const designerStatus = certType === 'designer' && !isContractorCertification
                    ? statusValue
                    : 'none';
                const contractorStatus = isContractorCertification
                    ? statusValue
                    : 'none';
                const companyStatus = certType === 'enterprise'
                    ? statusValue
                    : 'none';
                this.setData({
                    realNameStatus,
                    designerStatus,
                    contractorStatus,
                    companyStatus,
                    certType,
                    certIdentityType: identityType,
                    certRealNameMasked: data.real_name_masked || '',
                    certIDCardMasked: data.id_card_no_masked || '',
                    canWithdraw: !!data.can_withdraw,
                });
                this.loadWalletPreview();
                this.updateStatusClasses();
            },
            fail: () => {
                this.updateStatusClasses();
            },
        });
    },
    // 根据状态计算展示用的 class / 文案
    updateStatusClasses() {
        const designerStatusClass = this.getStatusClass(this.data.designerStatus);
        const designerStatusText = this.getStatusText(this.data.designerStatus, '设计师');
        const contractorStatusClass = this.getStatusClass(this.data.contractorStatus);
        const contractorStatusText = this.getStatusText(this.data.contractorStatus, '施工队');
        const companyStatusClass = this.getStatusClass(this.data.companyStatus);
        const companyStatusText = this.getStatusText(this.data.companyStatus, '机构');
        this.setData({
            designerStatusClass,
            designerStatusText,
            contractorStatusClass,
            contractorStatusText,
            companyStatusClass,
            companyStatusText,
        });
    },
    loadWalletPreview() {
        const token = wx.getStorageSync('token');
        if (!token) {
            this.setData({
                withdrawRmbAmount: '0.00',
            });
            return;
        }
        wx.request({
            url: `${IDENTITY_AUTHEN_API_BASE_URL}/api/v1/miniprogram/user/stones`,
            method: 'GET',
            header: {
                token,
                'Content-Type': 'application/json',
            },
            success: (resp) => {
                const body = resp.data;
                if (resp.statusCode !== 200 || !body || body.code !== 0) {
                    return;
                }
                const stones = Number(body.data?.stones || 0);
                this.setData({
                    withdrawRmbAmount: (stones / EXCHANGE_RATIO).toFixed(2),
                });
            },
        });
    },
    getStatusClass(status) {
        if (status === 'approved')
            return 'status-success';
        if (status === 'review')
            return 'status-review';
        return '';
    },
    getStatusText(status, prefix) {
        void prefix;
        if (status === 'approved')
            return '已认证';
        if (status === 'review')
            return '审核中';
        return '未认证';
    },
    // 跳转到资质认证填写页（个人认证 tab）
    onRealNameTap() {
        wx.navigateTo({
            url: '/pages/real/real?tab=designer',
        });
    },
    onDesignerTap() {
        wx.navigateTo({
            url: '/pages/real/real?tab=designer',
        });
    },
    onContractorTap() {
        wx.navigateTo({
            url: '/pages/real/real?tab=designer&identityType=施工队',
        });
    },
    onCompanyTap() {
        wx.navigateTo({
            url: '/pages/real/real?tab=enterprise',
        });
    },
    onWithdrawEntryTap() {
        if (!this.data.canWithdraw) {
            wx.showToast({
                title: '完成设计师或机构认证后开通',
                icon: 'none',
            });
            return;
        }
        wx.navigateTo({
            url: '/pages/withdrawcenter/withdrawcenter',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · 身份认证中心',
            path: '/pages/Identityauthen/Identityauthen',
        };
    },
});
