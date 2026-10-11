// pages/ordermanagement/ordermanagement.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { requestVirtualPay } from '../../utils/virtualpay';
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint,
} from '../../utils/deviceFingerprint';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const ORDER_DETAIL_SNAPSHOT_STORAGE_KEY = 'order_detail_snapshot';

interface OrderItem {
  id: number;
  orderNo: string;
  type: 'recharge' | 'consume' | 'culture' | 'withdraw' | 'certification';

  orderCategory?: string;
  designerUserId?: number;
  typeText: string;
  title: string;
  description?: string;
  amount: number;
  amountText: string;
  status: 'success' | 'pending' | 'failed' | 'cancelled';
  statusText: string;
  reviewStatus?: string;
  reviewStatusText?: string;
  completedAt?: string;
  canReview: boolean;
  canCancel?: boolean;
  canContinuePay?: boolean;
  canDelete?: boolean;
  time: string;
  canAction: boolean;
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
    loadingMore: false,

    // 统计
    totalOrders: 0,
    totalAmount: '0.00',
    monthOrders: 0,
    monthAmount: '0.00',

    // 筛选
    currentType: 'all' as 'all' | 'recharge' | 'consume' | 'culture' | 'withdraw' | 'certification',

    // 分页
    page: 1,
    pageSize: 20,
    hasMore: true,

    // 数据
    orders: [] as OrderItem[],

    showReviewModal: false,
    reviewSubmitting: false,
    reviewForm: {
      orderId: 0,
      orderTitle: '',
      rating: 5,
      content: '',
    },
  },

  async onLoad() {
    await this.initDeviceId();
    this.initToken();
    await this.loadSummary();
    await this.loadOrders(true);
  },

  onShow() {
    // 返回时可选择刷新
  },

  async initDeviceId() {
    let deviceId = getCachedDeviceFingerprint();
    if (!deviceId) {
      try {
        deviceId = await generateDeviceFingerprint();
        if (deviceId) {
          cacheDeviceFingerprint(deviceId);
        }
      } catch (e) {
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

  getAuthHeaders(apiPath: string, body: any = {}) {
    const token = this.data.token;
    if (!token) return null;
    const params = generateRequestParams(token, body, apiPath, this.data.deviceId);
    return {
      ...paramsToHeaders(params),
      'Content-Type': 'application/json',
    };
  },

  // 加载统计信息
  async loadSummary() {
    const token = this.data.token;
    if (!token) return;

    try {
      const apiPath = '/api/v1/miniprogram/user/orders/summary';
      const headers = this.getAuthHeaders(apiPath, {});
      if (!headers) return;

      const data = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve(d.data);
              } else {
                reject(new Error(d.msg || '获取统计失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      this.setData({
        totalOrders: data.total_orders || 0,
        totalAmount: this.formatAmount(data.total_amount || 0),
        monthOrders: data.month_orders || 0,
        monthAmount: this.formatAmount(data.month_amount || 0),
      });
    } catch (err) {
      console.error('获取订单统计失败:', err);
    }
  },

  // 加载订单列表
  async loadOrders(reset = false) {
    const token = this.data.token;
    if (!token) {
      this.setData({ loading: false });
      return;
    }

    if (reset) {
      this.setData({
        page: 1,
        hasMore: true,
        orders: [],
      });
    } else {
      if (!this.data.hasMore || this.data.loadingMore) return;
    }

    const page = reset ? 1 : this.data.page + 1;

    this.setData({
      loading: reset,
      loadingMore: !reset,
    });

    try {
      const apiPath = '/api/v1/miniprogram/user/orders';
      const body: any = {
        page,
        page_size: this.data.pageSize,
      };
      if (this.data.currentType !== 'all') {
        body.type = this.data.currentType;
      }

      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) return;

      const resp = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: headers,
          data: body,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve(d.data);
              } else {
                reject(new Error(d.msg || '获取订单失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      const list = (resp.list || []) as any[];
      const mapped: OrderItem[] = list.map((item, index) => ({
        id: Number(item.id) > 0 ? Number(item.id) : 0,
        orderNo: item.order_no || item.orderNo || `ORDER${Date.now()}${index}`,
        type: item.type || 'consume',
        orderCategory: item.order_category || '',
        designerUserId: Number(item.designer_user_id) || 0,
        typeText: this.getTypeText(item.type),
        title: this.formatOrderTitle(item.title || item.name || '订单', item.order_category || ''),
        description: this.formatOrderDescription(item.description || item.desc || ''),
        amount: item.amount || 0,
        amountText: this.formatAmount(Math.abs(item.amount || 0)),
        status: item.status || 'success',
        statusText: this.getStatusText(item.status),
        reviewStatus: item.review_status || 'not_applicable',
        reviewStatusText: this.getReviewStatusText(item.review_status || 'not_applicable'),
        completedAt: this.formatOrderTime(item.completed_at || ''),
        canReview: Number(item.id) > 0 && item.status === 'success' && item.review_status === 'pending_review' && Number(item.designer_user_id) > 0,
        canCancel: item.can_cancel === true,
        canContinuePay: item.can_continue_pay === true,
        canDelete: item.can_delete === true,
        time: this.formatOrderTime(item.created_at || item.time || ''),
        canAction: Number(item.id) > 0,
      }));

      const newOrders = reset ? mapped : this.data.orders.concat(mapped);
      const hasMore = newOrders.length < (resp.total || 0);

      this.setData({
        orders: newOrders,
        page,
        hasMore,
      });
    } catch (err) {
      console.error('获取订单列表失败:', err);
      wx.showToast({
        title: '加载失败',
        icon: 'none',
      });
    } finally {
      this.setData({
        loading: false,
        loadingMore: false,
      });
      wx.stopPullDownRefresh();
    }
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

  formatAmount(value: number): string {
    const amount = Number(value) || 0;
    const sign = amount < 0 ? '-' : '';
    const fixedAmount = Math.abs(amount).toFixed(2);
    const [integerPart, decimalPart] = fixedAmount.split('.');
    return `${sign}${integerPart.replace(/\B(?=(\d{3})+(?!\d))/g, ',')}.${decimalPart}`;
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

  formatOrderDescription(description: string): string {
    const safeDescription = String(description || '').trim();
    if (safeDescription) {
      return safeDescription;
    }
    return '';
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

  formatOrderTime(timeValue: string): string {
    if (!timeValue) {
      return '';
    }
    const date = new Date(timeValue);
    if (Number.isNaN(date.getTime())) {
      return timeValue;
    }
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hour = String(date.getHours()).padStart(2, '0');
    const minute = String(date.getMinutes()).padStart(2, '0');
    return `${year}-${month}-${day} ${hour}:${minute}`;
  },

  onFilterTap(e: any) {
    const type = e.currentTarget.dataset.type || 'all';
    if (type === this.data.currentType) return;
    this.setData({ currentType: type });
    this.loadOrders(true);
  },

  onPullDownRefresh() {
    this.loadSummary();
    this.loadOrders(true);
  },

  onReachBottom() {
    this.loadOrders(false);
  },

  openReviewModal(e: any) {
    const id = Number(e.currentTarget.dataset.id) || 0;
    const title = String(e.currentTarget.dataset.title || '订单');
    if (!id) {
      wx.showToast({
        title: '订单信息异常',
        icon: 'none',
      });
      return;
    }
    this.setData({
      showReviewModal: true,
      reviewForm: {
        orderId: id,
        orderTitle: title,
        rating: 5,
        content: '',
      },
    });
  },

  closeReviewModal() {
    if (this.data.reviewSubmitting) {
      return;
    }
    this.setData({
      showReviewModal: false,
      reviewForm: {
        orderId: 0,
        orderTitle: '',
        rating: 5,
        content: '',
      },
    });
  },

  onReviewStarTap(e: any) {
    const rating = Number(e.currentTarget.dataset.rating) || 5;
    this.setData({
      'reviewForm.rating': rating,
    });
  },

  onReviewContentInput(e: any) {
    this.setData({
      'reviewForm.content': e.detail.value || '',
    });
  },

  onReviewModalContentTap() {
  },

  async submitReview() {
    const orderId = Number(this.data.reviewForm.orderId) || 0;
    const rating = Number(this.data.reviewForm.rating) || 5;
    const content = String(this.data.reviewForm.content || '').trim();

    if (!orderId) {
      wx.showToast({
        title: '订单信息异常',
        icon: 'none',
      });
      return;
    }
    if (!content) {
      wx.showToast({
        title: '请填写评价内容',
        icon: 'none',
      });
      return;
    }

    this.setData({ reviewSubmitting: true });

    try {
      const apiPath = `/api/v1/miniprogram/user/orders/${orderId}/review`;
      const body = {
        rating,
        content,
      };
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const resp = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: headers,
          data: body,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve(d.data || {});
              } else {
                reject(new Error(d.msg || '评价失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      this.setData({
        orders: this.data.orders.map((item) => {
          if (item.id !== orderId) {
            return item;
          }
          return {
            ...item,
            reviewStatus: String(resp.review_status || 'reviewed'),
            reviewStatusText: this.getReviewStatusText(String(resp.review_status || 'reviewed')),
            canReview: false,
          };
        }),
        showReviewModal: false,
        reviewForm: {
          orderId: 0,
          orderTitle: '',
          rating: 5,
          content: '',
        },
      });
      wx.showToast({
        title: '评价成功',
        icon: 'success',
      });
    } catch (err: any) {
      wx.showToast({
        title: err?.message || '评价失败',
        icon: 'none',
      });
    } finally {
      this.setData({ reviewSubmitting: false });
    }
  },

  onViewOrder(e: any) {
    const id = e.currentTarget.dataset.id;
    if (!id) {
      wx.showToast({
        title: '订单信息异常',
        icon: 'none',
      });
      return;
    }
    const currentOrder = (this.data.orders || []).find((item) => Number(item.id) === Number(id));
    if (currentOrder) {
      try {
        wx.setStorageSync(ORDER_DETAIL_SNAPSHOT_STORAGE_KEY, currentOrder);
      } catch (error) {
        console.warn('缓存订单详情快照失败:', error);
      }
    }
    wx.navigateTo({
      url: `/pages/orderdetail/orderdetail?id=${id}${currentOrder?.orderNo ? `&order_no=${encodeURIComponent(currentOrder.orderNo)}` : ''}`,
      success: (res) => {
        if (currentOrder) {
          res.eventChannel.emit('orderData', currentOrder);
        }
      },
    });
  },

  async deleteOrderRecord(id: number) {
    const apiPath = `/api/v1/miniprogram/user/orders/${id}`;
    const headers = this.getAuthHeaders(apiPath, {});
    if (!headers) {
      throw new Error('登录态已失效，请重新登录');
    }

    wx.showLoading({ title: '删除中...', mask: true });
    try {
      await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'DELETE',
          header: headers,
          success: (response) => {
            if (response.statusCode === 200 && response.data) {
              const data = response.data as any;
              if (data.code === 0) {
                resolve(data.data || {});
                return;
              }
              reject(new Error(data.msg || '删除订单失败'));
              return;
            }
            reject(new Error(`请求失败: ${response.statusCode}`));
          },
          fail: reject,
        });
      });

      this.setData({
        orders: this.data.orders.filter((item) => item.id !== id),
      });
      await this.loadSummary();
      wx.showToast({
        title: '已删除',
        icon: 'success',
      });
    } finally {
      wx.hideLoading();
    }
  },

  onOrderLongPress(e: any) {
    const id = Number(e.currentTarget.dataset.id) || 0;
    const title = String(e.currentTarget.dataset.title || '该订单');
    const canDelete = e.currentTarget.dataset.canDelete === true || e.currentTarget.dataset.canDelete === 'true';
    const canCancel = e.currentTarget.dataset.canCancel === true || e.currentTarget.dataset.canCancel === 'true';
    const canContinuePay = e.currentTarget.dataset.canContinuePay === true || e.currentTarget.dataset.canContinuePay === 'true';

    if (!id) {
      wx.showToast({
        title: '订单信息异常',
        icon: 'none',
      });
      return;
    }

    if (!canDelete) {
      let message = '当前订单暂不支持删除';
      if (canCancel) {
        message = '处理中订单请先取消后再删除';
      } else if (canContinuePay) {
        message = '可继续支付的订单暂不支持删除';
      }
      wx.showToast({
        title: message,
        icon: 'none',
      });
      return;
    }

    wx.vibrateShort({ type: 'light' });
    wx.showModal({
      title: '删除订单记录',
      content: `确定删除“${title}”吗？删除后仅在你的订单列表中隐藏，不影响实际支付、对账和后台记录。`,
      confirmText: '删除',
      confirmColor: '#e34c4c',
      success: async (res) => {
        if (!res.confirm) {
          return;
        }
        try {
          await this.deleteOrderRecord(id);
        } catch (error: any) {
          wx.showToast({
            title: error?.message || '删除订单失败',
            icon: 'none',
          });
        }
      },
    });
  },

  onContinueCertificationPay(e: any) {
    const id = Number(e.currentTarget.dataset.id) || 0;
    const orderNo = String(e.currentTarget.dataset.orderNo || e.currentTarget.dataset.orderno || '').trim();
    if (!id && !orderNo) {
      wx.showToast({ title: '订单信息异常', icon: 'none' });
      return;
    }
    const token = this.data.token;
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' });
      return;
    }
    wx.showLoading({ title: '拉起支付中', mask: true });
    wx.login({
      success: (loginRes) => {
        const code = (loginRes as any).code || '';
        const apiPath = '/api/v1/miniprogram/certification/continue-pay';
        const body: Record<string, any> = { code };
        if (id > 0) {
          body.order_id = id;
        } else if (orderNo) {
          body.order_no = orderNo;
        }
        const headers = this.getAuthHeaders(apiPath, body);
        if (!headers) {
          wx.hideLoading();
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
              wx.showToast({ title: (d && d.msg) || '继续支付失败', icon: 'none' });
              return;
            }
            // 阶段 5.2：后端在 VirtualPay 启用时返回 pay_channel='virtualpay'，前端走虚拟支付。
            if (d.data.pay_channel === 'virtualpay') {
              const applicationId = Number(d.data.application_id || 0);
              const productId = String(d.data.product_id || '');
              if (!applicationId || !productId) {
                wx.showToast({ title: '订单缺少认证工单信息', icon: 'none' });
                return;
              }
              const result = await requestVirtualPay({
                planType: 'certification',
                productId,
                applicationId,
              });
              if (!result.success) {
                wx.showToast({ title: result.errMsg || '支付未完成', icon: 'none' });
                await this.loadOrders(true);
                return;
              }
              wx.showToast({ title: '支付成功，请等待审核', icon: 'success' });
              await this.loadSummary();
              await this.loadOrders(true);
              return;
            }
            wx.showToast({ title: '认证支付未启用虚拟支付，请联系管理员', icon: 'none' });
          },
          fail: () => {
            wx.hideLoading();
            wx.showToast({ title: '网络错误', icon: 'none' });
          },
        });
      },
      fail: () => {
        wx.hideLoading();
        wx.showToast({ title: '获取登录态失败', icon: 'none' });
      },
    });
  },

  onCancelOrder(e: any) {
    const id = Number(e.currentTarget.dataset.id) || 0;
    if (!id) {
      wx.showToast({
        title: '订单信息异常',
        icon: 'none',
      });
      return;
    }
    wx.showModal({
      title: '确认取消',
      content: '确定要取消这个充值订单吗？取消后需要重新发起支付。',
      success: async (res) => {
        if (res.confirm) {
          try {
            const apiPath = `/api/v1/miniprogram/user/orders/${id}/cancel`;
            const headers = this.getAuthHeaders(apiPath, {});
            if (!headers) {
              throw new Error('登录态已失效，请重新登录');
            }

            await new Promise<any>((resolve, reject) => {
              wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'POST',
                header: headers,
                data: {},
                success: (response) => {
                  if (response.statusCode === 200 && response.data) {
                    const data = response.data as any;
                    if (data.code === 0) {
                      resolve(data.data || {});
                      return;
                    }
                    reject(new Error(data.msg || '取消订单失败'));
                    return;
                  }
                  reject(new Error(`请求失败: ${response.statusCode}`));
                },
                fail: reject,
              });
            });

            this.setData({
              orders: this.data.orders.map((item) => {
                if (item.id !== id) {
                  return item;
                }
                return {
                  ...item,
                  status: 'cancelled',
                  statusText: this.getStatusText('cancelled'),
                  canCancel: false,
                  canDelete: true,
                  canReview: false,
                };
              }),
            });
            wx.showToast({
              title: '订单已取消',
              icon: 'success',
            });
          } catch (error: any) {
            wx.showToast({
              title: error?.message || '取消订单失败',
              icon: 'none',
            });
          }
        }
      },
    });
  },

  onShareAppMessage() {
    return {
      title: '订单管理',
      path: '/pages/ordermanagement/ordermanagement',
    };
  },
});
