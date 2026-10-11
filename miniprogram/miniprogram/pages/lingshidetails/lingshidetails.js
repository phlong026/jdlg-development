"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/lingshidetails/lingshidetails.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const API_BASE_URL = 'https://api.jiadilingguang.com';
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
        loadingMore: false,
        page: 1,
        pageSize: 20,
        hasMore: true,
        currentType: 'all',
        balance: 0,
        recentConsume: 0,
        recentGain: 0,
        checkinTotal: 0,
        records: [],
    },
    async onLoad() {
        await this.initDeviceId();
        this.initToken();
        this.loadSummary();
        this.loadRecords(true);
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
        if (!token) {
            wx.showToast({
                title: '请先登录',
                icon: 'none',
            });
        }
        this.setData({ token: token || '' });
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
    // 顶部统计信息（当前余额 + 近30天统计）
    async loadSummary() {
        const token = this.data.token;
        if (!token)
            return;
        try {
            const apiPath = '/api/v1/miniprogram/user/stones/summary';
            const headers = this.getAuthHeaders(apiPath, {});
            if (!headers)
                return;
            const data = await new Promise((resolve, reject) => {
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
            this.setData({
                balance: data.balance || 0,
                recentConsume: data.recent_consume || 0,
                recentGain: data.recent_gain || 0,
                checkinTotal: data.checkin_total || 0,
            });
        }
        catch (err) {
            console.error('获取灵石统计失败:', err);
        }
    },
    // 加载明细记录
    async loadRecords(reset = false) {
        const token = this.data.token;
        if (!token) {
            this.setData({ loading: false });
            return;
        }
        if (reset) {
            this.setData({
                page: 1,
                hasMore: true,
                records: [],
            });
        }
        else {
            if (!this.data.hasMore || this.data.loadingMore)
                return;
        }
        const page = reset ? 1 : this.data.page + 1;
        this.setData({
            loading: reset,
            loadingMore: !reset,
        });
        try {
            const apiPath = '/api/v1/miniprogram/user/stones/details';
            const body = {
                page,
                page_size: this.data.pageSize,
            };
            if (this.data.currentType !== 'all') {
                body.type = this.data.currentType;
            }
            const headers = this.getAuthHeaders(apiPath, body);
            if (!headers)
                return;
            const resp = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: headers,
                    data: body,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const d = res.data;
                            if (d.code === 0) {
                                resolve(d.data);
                            }
                            else {
                                reject(new Error(d.msg || '获取明细失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const list = (resp.list || []);
            const mapped = list.map((item, index) => {
                const signedAmount = getSignedStoneAmount(item);
                return {
                    id: item.id || index,
                    type: item.type || 'consume',
                    title: this.getTitleByType(item),
                    desc: item.desc || item.scene_desc || '',
                    amount: signedAmount,
                    amountText: formatStoneAmount(signedAmount),
                    amountClass: signedAmount > 0 ? 'plus' : 'minus',
                    time: item.created_at || '',
                    remark: item.remark || '',
                };
            });
            const newRecords = reset ? mapped : this.data.records.concat(mapped);
            const hasMore = newRecords.length < (resp.total || 0);
            this.setData({
                records: newRecords,
                page,
                hasMore,
            });
        }
        catch (err) {
            console.error('获取灵石明细失败:', err);
            wx.showToast({
                title: '加载失败',
                icon: 'none',
            });
        }
        finally {
            this.setData({
                loading: false,
                loadingMore: false,
            });
            wx.stopPullDownRefresh();
        }
    },
    getTitleByType(item) {
        const type = item.type;
        if (type === 'recharge')
            return '充值获得';
        if (type === 'checkin')
            return '签到奖励';
        if (type === 'task')
            return item.title || '活动/任务奖励';
        // 默认消耗
        if (item.scene === 'ai_draw_single' || item.scene === 'ai_draw_multi') {
            return 'AI绘图消耗';
        }
        if (item.scene && item.scene.indexOf('ai_chat') >= 0) {
            return 'AI聊天消耗';
        }
        return item.title || '灵石消耗';
    },
    onFilterTap(e) {
        const type = e.currentTarget.dataset.type || 'all';
        if (type === this.data.currentType)
            return;
        this.setData({ currentType: type });
        this.loadRecords(true);
    },
    onPullDownRefresh() {
        this.loadSummary();
        this.loadRecords(true);
    },
    onReachBottom() {
        this.loadRecords(false);
    },
    onShareAppMessage() {
        return {
            title: '灵石明细',
            path: '/pages/lingshidetails/lingshidetails',
        };
    },
});
