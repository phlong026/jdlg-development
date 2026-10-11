// pages/myplan/myplan.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint,
} from '../../utils/deviceFingerprint';
import { normalizeCosUrl, resolveAssetPath } from '../../utils/asset';
import { prepareShareCardImage } from '../../utils/shareImage';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_PLAN_IMAGE = resolveAssetPath('/assets/images/home.jpg');

interface PlanItem {
  id: number;
  name: string;
  category: string;
  categoryName: string;
  description: string;
  thumbnail: string;
  preview_url: string;
  download_count: number;
  like_count: number;
  status: 'published' | 'pending' | 'draft';
  statusText: string;
  created_at: string;
  is_free?: number | boolean;
  price?: number;
}

function base64Encode(input: string): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
  const str = encodeURIComponent(input).replace(/%([0-9A-F]{2})/g, (_, p1) =>
    String.fromCharCode(parseInt(p1, 16)),
  );
  let output = '';
  let i = 0;
  while (i < str.length) {
    const chr1 = str.charCodeAt(i++);
    const chr2 = str.charCodeAt(i++);
    const chr3 = str.charCodeAt(i++);

    const enc1 = chr1 >> 2;
    const enc2 = ((chr1 & 3) << 4) | (chr2 >> 4);
    let enc3 = ((chr2 & 15) << 2) | (chr3 >> 6);
    let enc4 = chr3 & 63;

    if (isNaN(chr2)) {
      enc3 = enc4 = 64;
    } else if (isNaN(chr3)) {
      enc4 = 64;
    }

    output +=
      chars.charAt(enc1) +
      chars.charAt(enc2) +
      chars.charAt(enc3) +
      chars.charAt(enc4);
  }
  return output;
}

interface CategoryGroup {
  category: string;
  categoryName: string;
  plans: PlanItem[];
}

Page({
  data: {
    token: '',
    deviceId: '',
    loading: true,
    loadingMore: false,

    // 统计
    totalPlans: 0,
    totalViews: 0,

    // 分类
    categories: [
      { label: '乡村别墅', value: 'villa' },
      { label: '城市焕新', value: 'urban' },
      { label: '亲子', value: 'family' },
      { label: '文创', value: 'culture' },
    ],
    currentCategory: 'all' as 'all' | string,

    // 分页
    page: 1,
    pageSize: 20,
    hasMore: true,

    // 数据（分组后的）
    groupedPlans: [] as CategoryGroup[],
    pendingSharePlanId: 0,
    pendingShareImageUrl: '',

    // 编辑弹窗
    showEditModal: false,
    editingPlanId: null as number | null,
    editForm: {
      name: '',
      description: '',
      isFree: true,
      price: 0,
    },
  },

  async onLoad() {
    await this.initDeviceId();
    this.initToken();
    await this.loadSummary();
    await this.loadPlans(true);
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
      const apiPath = '/api/v1/miniprogram/user/templates/summary';
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
        totalPlans: data.total_plans || 0,
        totalViews: data.total_views || 0,
      });
    } catch (err) {
      console.error('获取方案统计失败:', err);
    }
  },

  // 加载方案列表
  async loadPlans(reset = false) {
    const token = this.data.token;
    if (!token) {
      this.setData({ loading: false });
      return;
    }

    if (reset) {
      this.setData({
        page: 1,
        hasMore: true,
        groupedPlans: [],
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
      const apiPath = '/api/v1/miniprogram/user/templates';
      const body: any = {
        page,
        page_size: this.data.pageSize,
      };
      if (this.data.currentCategory !== 'all') {
        body.category = this.data.currentCategory;
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
                reject(new Error(d.msg || '获取方案失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      const list = (resp.list || []) as any[];
      const mapped: PlanItem[] = list.map((item, index) => ({
        id: item.id || index,
        name: item.name || '未命名方案',
        category: item.category || 'other',
        categoryName: this.getCategoryName(item.category),
        description: item.description || '',
        thumbnail: normalizeCosUrl(String(item.thumbnail || item.preview_url || DEFAULT_PLAN_IMAGE)),
        preview_url: normalizeCosUrl(String(item.preview_url || item.thumbnail || '')),
        download_count: item.download_count || 0,
        like_count: item.like_count || 0,
        status: item.status || 'draft',
        statusText: this.getStatusText(item.status),
        created_at: item.created_at || '',
        is_free: item.is_free,
        price: item.price,
      }));

      // 合并数据并重新分组
      const allPlans = reset ? mapped : this.getAllPlans().concat(mapped);
      const grouped = this.groupByCategory(allPlans);
      const hasMore = allPlans.length < (resp.total || 0);

      this.setData({
        groupedPlans: grouped,
        page,
        hasMore,
      });
    } catch (err) {
      console.error('获取方案列表失败:', err);
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

  // 获取所有方案（从分组数据中提取）
  getAllPlans(): PlanItem[] {
    const groups = this.data.groupedPlans;
    const all: PlanItem[] = [];
    groups.forEach((group) => {
      all.push(...group.plans);
    });
    return all;
  },

  // 按分类分组
  groupByCategory(plans: PlanItem[]): CategoryGroup[] {
    const groups: Record<string, PlanItem[]> = {};
    plans.forEach((plan) => {
      if (!groups[plan.category]) {
        groups[plan.category] = [];
      }
      groups[plan.category].push(plan);
    });

    const result: CategoryGroup[] = [];
    Object.keys(groups).forEach((category) => {
      result.push({
        category,
        categoryName: this.getCategoryName(category),
        plans: groups[category],
      });
    });

    // 按分类名称排序
    result.sort((a, b) => a.categoryName.localeCompare(b.categoryName));
    return result;
  },

  getCategoryName(category: string): string {
    const map: Record<string, string> = {
      villa: '乡村别墅',
      urban: '城市焕新',
      family: '亲子',
      culture: '文创',
      presentation: '演示文稿',
      poster: '海报',
      banner: '横幅',
      card: '卡片',
      other: '其他',
    };
    return map[category] || '其他';
  },

  getStatusText(status: string): string {
    const map: Record<string, string> = {
      published: '已发布',
      pending: '审核中',
      draft: '草稿',
    };
    return map[status] || '未知';
  },

  onCategoryTap(e: any) {
    const category = e.currentTarget.dataset.category || 'all';
    if (category === this.data.currentCategory) return;
    this.setData({ currentCategory: category });
    this.loadPlans(true);
  },

  onPullDownRefresh() {
    this.loadSummary();
    this.loadPlans(true);
  },

  onReachBottom() {
    this.loadPlans(false);
  },

  onViewPlan(e: any) {
    const id = e.currentTarget.dataset.id;
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

  async onSharePlan(e: any) {
    const id = e.currentTarget.dataset.id;
    const plan = this.findPlanById(id);
    if (!plan) return;

    wx.showLoading({ title: '准备分享图...' });
    const shareSourceUrl = String(plan.preview_url || plan.thumbnail || DEFAULT_PLAN_IMAGE).trim();
    const shareImageUrl = await prepareShareCardImage(shareSourceUrl);
    wx.hideLoading();
    this.setData({
      pendingSharePlanId: Number(id || 0),
      pendingShareImageUrl: shareImageUrl || shareSourceUrl,
    });

    wx.showShareMenu({
      withShareTicket: true,
      menus: ['shareAppMessage', 'shareTimeline'],
    });

    wx.showToast({
      title: '请点击右上角分享',
      icon: 'none',
    });
  },

  onExportPlan(e: any) {
    const id = e.currentTarget.dataset.id;
    const plan = this.findPlanById(id);
    if (!plan) return;

    wx.showModal({
      title: '导出方案',
      content: `确定要导出方案"${plan.name}"吗？`,
      success: (res) => {
        if (res.confirm) {
          // 导出功能：可以下载图片或生成分享链接
          const imageUrl = normalizeCosUrl(String(plan.preview_url || plan.thumbnail || ''));
          if (imageUrl) {
            wx.downloadFile({
              url: imageUrl,
              success: (downloadRes) => {
                wx.saveImageToPhotosAlbum({
                  filePath: downloadRes.tempFilePath,
                  success: () => {
                    wx.showToast({
                      title: '已保存到相册',
                      icon: 'success',
                    });
                  },
                  fail: () => {
                    wx.showToast({
                      title: '保存失败，请检查权限',
                      icon: 'none',
                    });
                  },
                });
              },
              fail: () => {
                wx.showToast({
                  title: '下载失败',
                  icon: 'none',
                });
              },
            });
          } else {
            wx.showToast({
              title: '方案图片不存在',
              icon: 'none',
            });
          }
        }
      },
    });
  },

  onEditPlan(e: any) {
    const id = e.currentTarget.dataset.id;
    const plan = this.findPlanById(id);
    if (!plan) return;

    const isFree = plan.is_free === 1 || plan.is_free === true || plan.is_free === undefined;
    const price = typeof plan.price === 'number' ? plan.price : 0;

    this.setData({
      showEditModal: true,
      editingPlanId: plan.id,
      editForm: {
        name: plan.name || '',
        description: plan.description || '',
        isFree,
        price: isFree ? 0 : (price > 0 ? price : 1),
      },
    });
  },

  onPreviewPlan(e: any) {
    const id = e.currentTarget.dataset.id;
    const plan = this.findPlanById(id);
    if (!plan) return;

    const userInfo = wx.getStorageSync('userInfo') || {};
    const payload = {
      title: this.data.editForm.name || plan.name || '未命名方案',
      description: this.data.editForm.description || plan.description || '',
      imageUrl: plan.preview_url || plan.thumbnail || '',
      userName: userInfo.username || userInfo.name || '预览用户',
      userAvatar: userInfo.avatar || '',
      createdAt: plan.created_at || '',
    };

    try {
      const json = JSON.stringify(payload);
      const b64 = base64Encode(json);
      wx.navigateTo({
        url: `/pages/templatepreview/templatepreview?data=${encodeURIComponent(b64)}`,
        events: {
          previewConfirm: () => {
            // 预览确认后保存编辑
            this.submitEdit();
          },
        },
      });
    } catch (err) {
      console.error('方案预览构造失败:', err);
      wx.showToast({
        title: '预览失败',
        icon: 'none',
      });
    }
  },

  closeEditModal() {
    this.setData({
      showEditModal: false,
      editingPlanId: null,
    });
  },

  onEditInput(e: any) {
    const field = e.currentTarget.dataset.field;
    const value = e.detail.value;
    if (!field) return;
    this.setData({
      [`editForm.${field}`]: value,
    });
  },

  onEditChargeTypeChange(e: any) {
    const free = e.currentTarget.dataset.free === 'true';
    const currentPrice = this.data.editForm.price || 0;
    this.setData({
      'editForm.isFree': free,
      'editForm.price': free ? 0 : (currentPrice > 0 ? currentPrice : 1),
    });
  },

  onEditPriceInput(e: any) {
    const v = parseInt(e.detail.value, 10);
    const price = isNaN(v) || v < 0 ? 0 : v;
    this.setData({
      'editForm.price': price,
    });
  },

  async submitEdit() {
    const { editingPlanId, editForm } = this.data;
    if (!editingPlanId) return;

    if (!editForm.name.trim()) {
      wx.showToast({
        title: '请输入方案名称',
        icon: 'none',
      });
      return;
    }

    wx.showLoading({ title: '保存中...' });

    try {
      const isFree = !!editForm.isFree;
      const price = isFree ? 0 : (editForm.price > 0 ? editForm.price : 1);
      const apiPath = `/api/v1/miniprogram/user/templates/${editingPlanId}`;
      const body: any = {
        name: editForm.name,
        description: editForm.description,
        is_free: isFree,
        price,
      };
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        wx.hideLoading();
        return;
      }

      await new Promise<void>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'PUT',
          header: headers,
          data: body,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve();
              } else {
                reject(new Error(d.msg || '更新失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      wx.showToast({
        title: '保存成功',
        icon: 'success',
      });

      this.setData({
        showEditModal: false,
        editingPlanId: null,
      });

      // 重新加载数据，确保和模板广场同步
      this.loadSummary();
      this.loadPlans(true);
    } catch (err) {
      console.error('更新方案失败:', err);
      wx.showToast({
        title: '保存失败',
        icon: 'none',
      });
    } finally {
      wx.hideLoading();
    }
  },

  onDeletePlan(e: any) {
    const id = e.currentTarget.dataset.id;
    const plan = this.findPlanById(id);
    if (!plan) return;

    wx.showModal({
      title: '删除方案',
      content: `确定要删除方案"${plan.name}"吗？删除后无法恢复。`,
      confirmColor: '#ef4444',
      success: async (res) => {
        if (res.confirm) {
          await this.deletePlan(id);
        }
      },
    });
  },

  async deletePlan(id: number) {
    const token = this.data.token;
    if (!token) return;

    try {
      const apiPath = `/api/v1/miniprogram/user/templates/${id}`;
      const headers = this.getAuthHeaders(apiPath, {});
      if (!headers) return;

      await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'DELETE',
          header: headers,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve(d.data);
              } else {
                reject(new Error(d.msg || '删除失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      wx.showToast({
        title: '删除成功',
        icon: 'success',
      });

      // 重新加载数据
      this.loadSummary();
      this.loadPlans(true);
    } catch (err) {
      console.error('删除方案失败:', err);
      wx.showToast({
        title: '删除失败',
        icon: 'none',
      });
    }
  },

  findPlanById(id: number): PlanItem | null {
    const groups = this.data.groupedPlans;
    for (const group of groups) {
      const plan = group.plans.find((p) => p.id === id);
      if (plan) return plan;
    }
    return null;
  },

  onShareAppMessage(options: any) {
    const planId = Number(options?.target?.dataset?.id || this.data.pendingSharePlanId || 0);
    if (planId) {
      const plan = this.findPlanById(planId);
      if (plan) {
        return {
          title: plan.name,
          path: `/pages/templatesquaredetails/templatesquaredetails?id=${planId}`,
          imageUrl: this.data.pendingShareImageUrl || plan.preview_url || plan.thumbnail || DEFAULT_PLAN_IMAGE,
        };
      }
    }
    return {
      title: '我的方案',
      path: '/pages/myplan/myplan',
    };
  },

  onShareTimeline() {
    const planId = Number(this.data.pendingSharePlanId || 0);
    if (planId) {
      const plan = this.findPlanById(planId);
      if (plan) {
        return {
          title: plan.name,
          query: `id=${planId}`,
          imageUrl: this.data.pendingShareImageUrl || plan.preview_url || plan.thumbnail || DEFAULT_PLAN_IMAGE,
        };
      }
    }
    return {
      title: '鎴戠殑鏂规',
    };
  },
});
