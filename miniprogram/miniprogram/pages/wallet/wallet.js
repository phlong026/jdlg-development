"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/wallet/wallet.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const EXCHANGE_RATIO = 10; // 10 灵石 = 1 人民币
function isPositiveStoneRecord(item) {
    const type = String(item?.type || '').toLowerCase();
    if (['recharge', 'checkin', 'invite', 'invite_reward', 'manual_grant'].includes(type)) {
        return true;
    }
    if (['consume', 'manual_deduct', 'withdraw'].includes(type)) {
        return false;
    }
    const detailText = [item?.title, item?.desc, item?.scene_desc, item?.remark]
        .map((value) => String(value || '').toLowerCase())
        .join(' ');
    if (detailText.includes('退回') ||
        detailText.includes('退款') ||
        detailText.includes('奖励') ||
        detailText.includes('返利') ||
        detailText.includes('获得') ||
        detailText.includes('模板付费')) {
        return true;
    }
    if (detailText.includes('扣减') ||
        detailText.includes('扣费') ||
        detailText.includes('消耗') ||
        detailText.includes('提现') ||
        detailText.includes('支付') ||
        detailText.includes('购买')) {
        return false;
    }
    return Number(item?.amount || 0) >= 0;
}
function getSignedStoneAmount(item) {
    const amount = Math.abs(Number(item?.amount || 0));
    if (!amount) {
        return 0;
    }
    return isPositiveStoneRecord(item) ? amount : -amount;
}
function formatStoneAmount(amount) {
    return amount > 0 ? `+${amount}` : `${amount}`;
}
Page({
    data: {
        token: '',
        deviceId: '',
        loading: true,
        // 余额与统计
        stones: 0,
        rmbAmount: '0.00',
        recentGain: 0,
        recentConsume: 0,
        // 最近灵石记录（所有用户可见）
        records: [],
    },
    async onLoad() {
        await this.initDeviceId();
        this.initToken();
        await this.loadWalletData();
    },
    onShow() {
        // 返回时可选择刷新
    },
    async initDeviceId() {
        let deviceId = (0, deviceFingerprint_1.getCachedDeviceFingerprint)();
        if (!deviceId) {
            try {
                deviceId = await (0, deviceFingerprint_1.generateDeviceFingerprint)();
                if (deviceId) {
                    (0, deviceFingerprint_1.cacheDeviceFingerprint)(deviceId);
                }
            }
            catch (e) {
                console.error('获取设备ID失败:', e);
            }
        }
        this.setData({ deviceId: deviceId || '' });
    },
    initToken() {
        const token = wx.getStorageSync('token');
        this.setData({ token: token || '' });
        if (!token) {
            wx.showToast({
                title: '请先登录',
                icon: 'none',
            });
        }
    },
    getAuthHeaders(apiPath, body = {}) {
        const token = this.data.token;
        if (!token)
            return null;
        const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, this.data.deviceId);
        return {
            ...(0, parameter_1.paramsToHeaders)(params),
            'Content-Type': 'application/json',
        };
    },
    async loadWalletData() {
        const token = this.data.token;
        if (!token) {
            this.setData({ loading: false });
            return;
        }
        this.setData({ loading: true });
        try {
            await Promise.all([this.loadStones(), this.loadRecentRecords()]);
        }
        finally {
            this.setData({ loading: false });
        }
    },
    // 加载灵石余额与近30天统计
    async loadStones() {
        const token = this.data.token;
        if (!token)
            return;
        try {
            // 基础余额（已存在的接口）
            const stonesData = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/stones`,
                    method: 'GET',
                    header: {
                        token,
                        'Content-Type': 'application/json',
                    },
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '获取余额失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const stones = stonesData.stones || 0;
            // 统计接口（若后端暂未实现，则静默失败）
            let recentGain = 0;
            let recentConsume = 0;
            try {
                const apiPath = '/api/v1/miniprogram/user/stones/summary';
                const headers = this.getAuthHeaders(apiPath, {});
                if (headers) {
                    const stat = await new Promise((resolve, reject) => {
                        wx.request({
                            url: `${API_BASE_URL}${apiPath}`,
                            method: 'GET',
                            header: headers,
                            success: (res) => {
                                if (res.statusCode === 200 && res.data) {
                                    const d = res.data;
                                    if (d.code === 0) {
                                        resolve(d.data);
                                    }
                                    else {
                                        reject(new Error(d.msg || '获取统计失败'));
                                    }
                                }
                                else {
                                    reject(new Error(`请求失败: ${res.statusCode}`));
                                }
                            },
                            fail: reject,
                        });
                    });
                    recentGain = stat.recent_gain || 0;
                    recentConsume = stat.recent_consume || 0;
                }
            }
            catch (e) {
                console.warn('获取灵石统计失败，使用默认值:', e);
            }
            const rmbAmount = (stones / EXCHANGE_RATIO).toFixed(2);
            this.setData({
                stones,
                rmbAmount,
                recentGain,
                recentConsume,
            });
        }
        catch (err) {
            console.error('获取钱包信息失败:', err);
        }
    },
    // 最近几条灵石明细，用于展示（所有用户可见）
    async loadRecentRecords() {
        const token = this.data.token;
        if (!token)
            return;
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
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '获取明细失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const list = (res.list || []);
            const records = list.map((item, index) => {
                return {
                    id: item.id || index,
                    type: item.type || 'consume',
                    title: this.getTitleByType(item),
                    desc: item.desc || item.scene_desc || '',
                    amount: getSignedStoneAmount(item),
                    amountText: formatStoneAmount(getSignedStoneAmount(item)),
                    amountClass: getSignedStoneAmount(item) > 0 ? 'plus' : 'minus',
                    time: item.created_at || '',
                };
            });
            this.setData({ records });
        }
        catch (err) {
            console.error('获取最近灵石记录失败:', err);
        }
    },
    getTitleByType(item) {
        const type = item.type;
        if (type === 'recharge')
            return '充值获得';
        if (type === 'checkin')
            return '签到奖励';
        if (type === 'invite')
            return '邀请注册奖励';
        if (type === 'invite_reward')
            return '邀请返利';
        if (type === 'manual_grant')
            return '管理员补发';
        if (type === 'manual_deduct')
            return '管理员扣减';
        if (type === 'withdraw')
            return '提现扣减';
        if (type === 'task')
            return item.title || '活动/任务奖励';
        if (item.scene === 'ai_draw_single' || item.scene === 'ai_draw_multi') {
            return 'AI绘图消耗';
        }
        if (item.scene && item.scene.indexOf('ai_chat') >= 0) {
            return 'AI聊天消耗';
        }
        return item.title || '灵石消耗';
    },
    onPullDownRefresh() {
        this.loadWalletData().finally(() => {
            wx.stopPullDownRefresh();
        });
    },
    goToTopupCenter() {
        if (!this.data.token) {
            wx.showToast({
                title: '请先登录',
                icon: 'none',
            });
            return;
        }
        wx.redirectTo({
            url: '/pages/topupcenter/topupcenter',
        });
    },
    goToStoneDetails() {
        wx.navigateTo({
            url: '/pages/lingshidetails/lingshidetails',
        });
    },
    onShareAppMessage() {
        return {
            title: '我的钱包',
            path: '/pages/wallet/wallet',
        };
    },
});
