import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint,
} from '../../utils/deviceFingerprint';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const EXCHANGE_RATIO = 10;

interface WithdrawRecord {
  id: number;
  title: string;
  amount: number;
  status: 'pending' | 'success' | 'failed';
  statusText: string;
  time: string;
}

Page({
  data: {
    token: '',
    deviceId: '',
    loading: true,
    canWithdraw: false,
    stones: 0,
    rmbAmount: '0.00',
    frozenAmount: '0.00',
    withdrawOptions: [10, 20, 50, 100, 200, 500],
    selectedWithdrawAmount: 100,
    withdrawRecords: [] as WithdrawRecord[],
    withdrawRecordEmptyText: '暂时还没有提现记录',
  },

  async onLoad() {
    await this.initDeviceId();
    this.initToken();
    await this.initIdentityStatus();
    await this.loadStones();
    this.syncWithdrawRecordState();
  },

  async initDeviceId() {
    let deviceId = getCachedDeviceFingerprint();
    if (!deviceId) {
      try {
        deviceId = await generateDeviceFingerprint();
        if (deviceId) {
          cacheDeviceFingerprint(deviceId);
        }
      } catch (error) {
        console.error('获取设备ID失败:', error);
      }
    }
    this.setData({ deviceId: deviceId || '' });
  },

  initToken() {
    const token = wx.getStorageSync('token');
    this.setData({ token: token || '' });
  },

  getAuthHeaders(apiPath: string, body: any = {}) {
    const token = this.data.token;
    if (!token) {
      return null;
    }
    const params = generateRequestParams(token, body, apiPath, this.data.deviceId);
    return {
      ...paramsToHeaders(params),
      'Content-Type': 'application/json',
    };
  },

  async initIdentityStatus() {
    const apiPath = '/api/v1/miniprogram/certification/status';
    const headers = this.getAuthHeaders(apiPath);
    if (!headers) {
      this.setData({
        canWithdraw: false,
      });
      return;
    }

    try {
      const data = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            const body = (res.data || {}) as any;
            if (res.statusCode === 200 && body.code === 0) {
              resolve(body.data || {});
              return;
            }
            reject(new Error(body.msg || `请求失败: ${res.statusCode}`));
          },
          fail: reject,
        });
      });

      this.setData({
        canWithdraw: !!data.can_withdraw,
      });
    } catch (error) {
      this.setData({
        canWithdraw: false,
      });
    }
  },

  async loadStones() {
    const apiPath = '/api/v1/miniprogram/user/stones';
    const headers = this.getAuthHeaders(apiPath);
    if (!headers) {
      this.setData({ loading: false });
      return;
    }

    try {
      const data = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            const body = (res.data || {}) as any;
            if (res.statusCode === 200 && body.code === 0) {
              resolve(body.data || {});
              return;
            }
            reject(new Error(body.msg || `请求失败: ${res.statusCode}`));
          },
          fail: reject,
        });
      });

      const stones = Number(data.stones || 0);
      this.setData({
        stones,
        rmbAmount: (stones / EXCHANGE_RATIO).toFixed(2),
        loading: false,
      });
    } catch (error: any) {
      this.setData({ loading: false });
      wx.showToast({
        title: error?.message || '加载余额失败',
        icon: 'none',
      });
    }
  },

  syncWithdrawRecordState() {
    this.setData({
      withdrawRecords: [],
      withdrawRecordEmptyText: this.data.canWithdraw
        ? '暂时还没有提现记录'
        : '完成认证后即可使用提现功能',
    });
  },

  onSelectWithdrawAmount(e: any) {
    const amount = Number(e.currentTarget.dataset.amount || 0);
    if (!amount) {
      return;
    }
    this.setData({
      selectedWithdrawAmount: amount,
    });
  },

  onWithdraw() {
    // 阶段 5.3 / D3 决策：提现功能在虚拟支付迁移期间暂停。
    // 后端 /wechatpay/withdraw 在 VirtualPay.Enabled=true 时返回 403。
    // 前端先弹模态提示，避免用户走完整流程后再被服务端拒绝。
    // 资金路径调研落地（阶段 0.6）后，删除这段提示即可恢复旧链路。
    wx.showModal({
      title: '提现维护中',
      content: '受支付通道升级影响，提现功能暂时关闭，恢复时间将另行通知。',
      showCancel: false,
      confirmText: '我知道了',
    });
  },

  // onWithdrawLegacy 是旧的提现链路实现，已被 onWithdraw 上的模态拦截屏蔽。
  // 保留代码便于阶段 0.6 资金路径理顺后回滚启用：把 onWithdraw 改回直接调用本函数即可。
  onWithdrawLegacy() {
    if (!this.data.canWithdraw) {
      wx.showToast({
        title: '请先完成认证开通提现',
        icon: 'none',
      });
      return;
    }

    const amount = this.data.selectedWithdrawAmount;
    const balance = parseFloat(this.data.rmbAmount || '0');
    if (amount > balance) {
      wx.showToast({
        title: '可提现金额不足',
        icon: 'none',
      });
      return;
    }

    const now = Date.now();
    const oneHourAgo = now - 60 * 60 * 1000;
    const history: { ts: number; amount: number }[] = wx.getStorageSync('withdraw_history') || [];
    const recent = history.filter((item) => item.ts >= oneHourAgo);
    const recentTotal = recent.reduce((sum, item) => sum + (item.amount || 0), 0);
    if (recentTotal + amount > 1000) {
      wx.showToast({
        title: '1小时内累计提现不得超过1000元',
        icon: 'none',
      });
      return;
    }

    const apiPath = '/api/v1/miniprogram/wechatpay/withdraw';
    const headers = this.getAuthHeaders(apiPath, {
      amount_fen: amount * 100,
    });
    if (!headers) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      });
      return;
    }

    wx.showLoading({ title: '发起提现中...', mask: true });
    wx.request({
      url: `${API_BASE_URL}${apiPath}`,
      method: 'POST',
      header: headers,
      data: {
        amount_fen: amount * 100,
      },
      success: (res) => {
        wx.hideLoading();
        const body = (res.data || {}) as any;
        if (res.statusCode === 200 && body.code === 0) {
          const newHistory = recent.concat([{ ts: now, amount }]);
          wx.setStorageSync('withdraw_history', newHistory);
          wx.showToast({
            title: '提现已发起，请在微信中确认',
            icon: 'none',
          });
          this.loadStones();
          this.syncWithdrawRecordState();
          return;
        }
        wx.showToast({
          title: body.msg || `请求失败: ${res.statusCode}`,
          icon: 'none',
        });
      },
      fail: () => {
        wx.hideLoading();
        wx.showToast({
          title: '网络异常，请稍后重试',
          icon: 'none',
        });
      },
    });
  },

  onWithdrawRules() {
    wx.showModal({
      title: '提现说明',
      content: '1. 仅认证设计师/机构可发起提现。\n2. 当前提现按 10 灵石折算 1 元人民币。\n3. 提现审核通过后预计 1-3 个工作日到账。',
      showCancel: false,
    });
  },

  goToCertification() {
    wx.navigateTo({
      url: '/pages/Identityauthen/Identityauthen',
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
      title: '提现中心',
      path: '/pages/withdrawcenter/withdrawcenter',
    };
  },
});
