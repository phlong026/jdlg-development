import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { requestVirtualPay } from '../../utils/virtualpay';
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint,
} from '../../utils/deviceFingerprint';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const ORDER_DETAIL_SNAPSHOT_STORAGE_KEY = 'order_detail_snapshot';

interface OrderDetail {
  id: number;
  orderNo: string;
  type: string;
  typeText: string;
  orderCategory: string;
  orderCategoryText: string;
  designerUserId: number;
  title: string;
  description: string;
  amount: number;
  amountText: string;
  status: string;
  statusText: string;
  reviewStatus: string;
  reviewStatusText: string;
  completedAt: string;
  createdAt: string;
  canContinuePay: boolean;
}

const ORDER_SCENE_TITLE_MAP: Record<string, string> = {
  ai_draw_single: 'AI生成-单图效果图',
  ai_draw_multi: 'AI生成-多图效果图',
  ai_video: 'AI生成-视频',
  ai_cost_doc: 'AI生成-造价文档',
};

Page({
  data: {
    token: '',
    deviceId: '',
    loading: true,
    payLoading: false,
    orderId: 0,
    orderNo: '',
    order: null as OrderDetail | null,
  },

  async onLoad(options: Record<string, string>) {
    await this.initDeviceId();
    this.initToken();
    const eventChannel = this.getOpenerEventChannel();
    eventChannel.on('orderData', (orderData: any) => {
      this.applyFallbackOrder(orderData);
    });
    const orderId = Number(options?.id || 0);
    const orderNo = String(options?.order_no || '').trim();
    if (!orderId && !orderNo) {
      wx.showToast({ title: '订单信息异常', icon: 'none' });
      this.setData({ loading: false });
      return;
    }
    this.setData({ orderId, orderNo });
    const snapshotOrder = this.getStoredOrderSnapshot(orderId, orderNo);
    if (snapshotOrder) {
      this.applyFallbackOrder(snapshotOrder);
      return;
    }
    if (orderId) {
      await this.loadOrderDetail(orderId);
      return;
    }
    this.setData({ loading: false });
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
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' });
    }
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

  getTypeText(type: string): string {
    const map: Record<string, string> = {
      recharge: '充值',
      consume: '消费',
      culture: '文创',
      withdraw: '提现',
      certification: '认证',
    };
    return map[type] || '其他';
  },

  getStatusText(status: string): string {
    const map: Record<string, string> = {
      success: '已完成',
      pending: '处理中',
      failed: '失败',
      cancelled: '已取消',
    };
    return map[status] || '未知';
  },

  getReviewStatusText(status: string): string {
    const map: Record<string, string> = {
      not_applicable: '无需评价',
      pending_review: '待评价',
      reviewed: '已评价',
    };
    return map[status] || '暂无';
  },

  getOrderCategoryText(orderCategory?: string): string {
    const map: Record<string, string> = {
      template: '模板消费',
      service: '设计服务',
      recharge: '账户充值',
      ai: 'AI生成订单',
      withdraw: '提现申请',
      certification: '认证服务',
    };
    const key = String(orderCategory || '').trim();
    return map[key] || '订单详情';
  },

  formatOrderTitle(title: string, orderCategory?: string): string {
    const safeTitle = String(title || '').trim();
    if (!safeTitle) {
      return this.getOrderCategoryText(orderCategory);
    }
    if (safeTitle.includes('-')) {
      const lastPart = safeTitle.split('-').pop() || '';
      if (ORDER_SCENE_TITLE_MAP[lastPart]) {
        return ORDER_SCENE_TITLE_MAP[lastPart];
      }
    }
    return safeTitle;
  },

  formatOrderDescription(description: string, orderCategory?: string): string {
    const safeDescription = String(description || '').trim();
    if (safeDescription) {
      return safeDescription;
    }
    return this.getOrderCategoryText(orderCategory);
  },

  formatOrderTime(timeValue: string): string {
    if (!timeValue) {
      return '';
    }
    const normalizedValue = String(timeValue)
      .trim()
      .replace(/\./g, '-')
      .replace(/T/g, ' ');
    let candidate = normalizedValue;
    if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(candidate)) {
      candidate = `${candidate}:00`;
    }
    const date = new Date(candidate.replace(/-/g, '/'));
    if (Number.isNaN(date.getTime())) {
      return normalizedValue;
    }
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hour = String(date.getHours()).padStart(2, '0');
    const minute = String(date.getMinutes()).padStart(2, '0');
    return `${year}-${month}-${day} ${hour}:${minute}`;
  },

  getStoredOrderSnapshot(orderId: number, orderNo: string) {
    try {
      const snapshot = wx.getStorageSync(ORDER_DETAIL_SNAPSHOT_STORAGE_KEY);
      if (!snapshot) {
        return null;
      }
      const snapshotId = Number(snapshot.id || 0);
      const snapshotOrderNo = String(snapshot.orderNo || snapshot.order_no || '').trim();
      if ((orderId > 0 && snapshotId === orderId) || (orderNo && snapshotOrderNo === orderNo)) {
        return snapshot;
      }
    } catch (error) {
      console.warn('读取订单详情快照失败:', error);
    }
    return null;
  },

  normalizeOrderDetail(detail: any): OrderDetail {
    const orderCategory = String(detail.order_category || detail.orderCategory || '').trim();
    const type = String(detail.type || 'consume');
    const status = String(detail.status || 'success');
    const reviewStatus = String(detail.review_status || detail.reviewStatus || 'not_applicable');
    const amount = Number(detail.amount || 0);
    return {
      id: Number(detail.id || 0),
      orderNo: String(detail.order_no || detail.orderNo || this.data.orderNo || ''),
      type,
      typeText: this.getTypeText(type),
      orderCategory,
      orderCategoryText: this.getOrderCategoryText(orderCategory),
      designerUserId: Number(detail.designer_user_id || detail.designerUserId || 0),
      title: this.formatOrderTitle(String(detail.title || detail.name || ''), orderCategory),
      description: this.formatOrderDescription(String(detail.description || detail.desc || ''), orderCategory),
      amount,
      amountText: Math.abs(amount).toFixed(2),
      status,
      statusText: this.getStatusText(status),
      reviewStatus,
      reviewStatusText: this.getReviewStatusText(reviewStatus),
      completedAt: this.formatOrderTime(String(detail.completed_at || detail.completedAt || '')),
      createdAt: this.formatOrderTime(String(detail.created_at || detail.createdAt || detail.time || '')),
      canContinuePay: detail.can_continue_pay === true || (orderCategory === 'certification' && (status === 'pending' || status === 'failed')),
    };
  },

  applyFallbackOrder(orderData: any) {
    if (!orderData) {
      return;
    }
    const order = this.normalizeOrderDetail(orderData);
    this.setData({
      order,
      loading: false,
      orderId: order.id || this.data.orderId,
      orderNo: order.orderNo || this.data.orderNo,
    });
  },

  async loadOrderDetail(orderId: number) {
    const token = this.data.token;
    if (!token) {
      this.setData({ loading: false });
      return;
    }

    try {
      const apiPath = `/api/v1/miniprogram/user/orders/${orderId}`;
      const headers = this.getAuthHeaders(apiPath, {});
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const detail = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const data = res.data as any;
              if (data.code === 0) {
                resolve(data.data || {});
                return;
              }
              reject(new Error(data.msg || '获取订单详情失败'));
              return;
            }
            reject(new Error(`请求失败: ${res.statusCode}`));
          },
          fail: reject,
        });
      });

      const order = this.normalizeOrderDetail(detail);

      this.setData({ order, loading: false, orderNo: order.orderNo || this.data.orderNo });
    } catch (error: any) {
      this.setData({ loading: false });
      if (this.data.order) {
        return;
      }
      console.error('获取订单详情失败:', error);
      wx.showToast({
        title: error?.message || '获取订单详情失败',
        icon: 'none',
      });
    }
  },

  continuePay() {
    const { token, orderId, orderNo, order, payLoading } = this.data;
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' });
      return;
    }
    if (payLoading) {
      return;
    }
    if (!order || !order.canContinuePay) {
      wx.showToast({ title: '当前订单不可继续支付', icon: 'none' });
      return;
    }
    this.setData({ payLoading: true });
    wx.showLoading({ title: '拉起支付中', mask: true });
    wx.login({
      success: (loginRes) => {
        const code = (loginRes as any).code || '';
        const apiPath = '/api/v1/miniprogram/certification/continue-pay';
        const body: Record<string, any> = { code };
        if (orderId > 0) {
          body.order_id = orderId;
        } else if (orderNo) {
          body.order_no = orderNo;
        }
        const headers = this.getAuthHeaders(apiPath, body);
        if (!headers) {
          wx.hideLoading();
          this.setData({ payLoading: false });
          wx.showToast({ title: '登录态已失效，请重新登录', icon: 'none' });
          return;
        }
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: headers,
          data: body,
          success: async (res) => {
            const d = res.data as any;
            wx.hideLoading();
            if (res.statusCode !== 200 || !d || d.code !== 0 || !d.data) {
              this.setData({ payLoading: false });
              wx.showToast({ title: (d && d.msg) || '继续支付失败', icon: 'none' });
              return;
            }
            // 阶段 5.2：后端在 VirtualPay 启用时返回 pay_channel='virtualpay'，前端走虚拟支付。
            if (d.data.pay_channel === 'virtualpay') {
              const applicationId = Number(d.data.application_id || 0);
              const productId = String(d.data.product_id || '');
              if (!applicationId || !productId) {
                this.setData({ payLoading: false });
                wx.showToast({ title: '订单缺少认证工单信息', icon: 'none' });
                return;
              }
              const result = await requestVirtualPay({
                planType: 'certification',
                productId,
                applicationId,
              });
              this.setData({ payLoading: false });
              if (!result.success) {
                wx.showToast({ title: result.errMsg || '支付未完成', icon: 'none' });
                if (this.data.orderId > 0) {
                  await this.loadOrderDetail(this.data.orderId);
                }
                return;
              }
              wx.showToast({ title: '支付成功，请等待审核', icon: 'success' });
              if (this.data.orderId > 0) {
                await this.loadOrderDetail(this.data.orderId);
              }
              return;
            }
            this.setData({ payLoading: false });
            wx.showToast({ title: '认证支付未启用虚拟支付，请联系管理员', icon: 'none' });
          },
          fail: () => {
            wx.hideLoading();
            this.setData({ payLoading: false });
            wx.showToast({ title: '网络错误', icon: 'none' });
          },
        });
      },
      fail: () => {
        wx.hideLoading();
        this.setData({ payLoading: false });
        wx.showToast({ title: '获取登录态失败', icon: 'none' });
      },
    });
  },

  copyOrderNo() {
    const orderNo = this.data.order?.orderNo || '';
    if (!orderNo) {
      wx.showToast({ title: '暂无订单号', icon: 'none' });
      return;
    }
    wx.setClipboardData({
      data: orderNo,
      success: () => {
        wx.showToast({ title: '订单号已复制', icon: 'success' });
      },
    });
  },

});
