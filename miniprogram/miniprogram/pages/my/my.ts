// pages/my/my.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint
} from '../../utils/deviceFingerprint';
import { resolveAssetPath } from '../../utils/asset';
const shareMaterials = require('../../utils/tracking/materials');

// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com'; // 根据实际情况修改
const PAGE_BACKGROUND_TOP = '#e6daca';
const PAGE_BACKGROUND_BOTTOM = '#ece4d9';
const CERTIFICATION_REFRESH_INTERVAL = 10000;

type MyMenuItem = {
  action: string;
  label: string;
  shortLabel: string;
  meta?: string;
};

function hasDesignerWorkbenchAccess(options: {
  hasLoginToken?: boolean;
  certStatus?: string;
  certIdentityType?: string;
}) {
  return !!options.hasLoginToken;
}

// 格式化灵石显示：只保留前 6 位，后面加 ...
function formatStonesDisplay(stones: number): string {
  const s = String(stones || 0);
  if (s.length <= 6) return s;
  return s.slice(0, 6) + '...';
}

function getFirstDisplayValue(source: any, keys: string[]): string {
  if (!source) return '';

  for (const key of keys) {
    const value = source[key];
    if (value !== undefined && value !== null && String(value).trim() !== '') {
      return String(value).trim();
    }
  }

  return '';
}

function buildDefaultVipDisplays(hasLoginToken: boolean) {
  if (hasLoginToken) {
    return {
      vipLevelShortDisplay: '普通',
      vipLevelText: '普通会员',
      vipLevelIsFallback: true,
    };
  }

  return {
    vipLevelShortDisplay: '--',
    vipLevelText: 'VIP --',
    vipLevelIsFallback: true,
  };
}

function resolveVipDisplays(hasLoginToken: boolean, ...sources: any[]) {
  let raw = '';

  for (const source of sources) {
    raw = getFirstDisplayValue(source, ['vip_level', 'vipLevel', 'member_level', 'memberLevel', 'level']);
    if (raw) {
      break;
    }
  }

  if (!raw || raw === '--') {
    return buildDefaultVipDisplays(hasLoginToken);
  }

  const shortDisplay = raw.replace(/[^0-9]/g, '') || raw;
  const fullDisplay = /^VIP/i.test(raw) ? raw : `VIP ${raw.includes('级') ? raw : `${raw}级`}`;

  return {
    vipLevelShortDisplay: shortDisplay,
    vipLevelText: fullDisplay,
    vipLevelIsFallback: false,
  };
}

function buildCommonMenuItems(): MyMenuItem[] {
  return [
    { action: 'project', label: '生成记录', shortLabel: '记' },
    { action: 'wallet', label: '充值中心', shortLabel: '充' },
    { action: 'orders', label: '我的订单', shortLabel: '单' },
    { action: 'favorite', label: '我的收藏', shortLabel: '藏' },
    { action: 'buildingCodeDocs', label: '规范资料', shortLabel: '规', meta: '全站问答' },
  ];
}

function buildServiceMenuItems(): MyMenuItem[] {
  return [
    { action: 'bindPhone', label: '绑定手机', shortLabel: '机', meta: '' },
    { action: 'settings', label: '设置', shortLabel: '设', meta: '账号与隐私' },
    { action: 'message', label: '消息中心', shortLabel: '信' },
    { action: 'adminLogin', label: '管理员登录', shortLabel: '管' },
  ];
}

function buildWorkbenchMenuItems(options: {
  hasLoginToken?: boolean;
  certStatus?: string;
  certType?: string;
  certIdentityType?: string;
}): MyMenuItem[] {
  const hasLoginToken = !!options.hasLoginToken;
  const certStatus = String(options.certStatus || '');
  const certIdentityType = String(options.certIdentityType || '');
  const hasDesignerAccess = hasDesignerWorkbenchAccess({
    hasLoginToken,
    certStatus,
    certIdentityType,
  });

  let identityMeta = '登录后查看';
  if (hasLoginToken) {
    if (certStatus === 'approved') {
      identityMeta = '已通过';
    } else if (certStatus === 'pending_review') {
      identityMeta = '审核中';
    } else if (certStatus === 'pending_payment') {
      identityMeta = '待支付';
    } else if (certStatus === 'rejected') {
      identityMeta = '未通过';
    } else {
      identityMeta = '可选认证';
    }
  }

  return [
    { action: 'identity', label: '认证资料', shortLabel: '认', meta: identityMeta },
    { action: 'designerHome', label: '设计师主页', shortLabel: '页', meta: hasDesignerAccess ? '进入主页' : '登录后可用' },
    { action: 'publish', label: '我的发布', shortLabel: '发', meta: hasDesignerAccess ? '上传作品' : '登录后可用' },
  ];
}

Page({
  ...shareMaterials.methods,
  /**
   * 页面的初始数据
   */
  data: {
    ...shareMaterials.data,
    token: '',
    userInfo: null as any,
    userProfile: {
      nickname: '',
      avatar: '',
      identityType: '',
    } as any,
    defaultAvatarImage: resolveAssetPath('/assets/images/home.jpg'),
    pageBgImage: resolveAssetPath('/assets/my/页面背景.png'),
    projectMenuImage: resolveAssetPath('/assets/my/我的项目.png'),
    accountMenuImage: resolveAssetPath('/assets/my/账号设置.png'),
    commonMenuItems: buildCommonMenuItems(),
    workbenchMenuItems: buildWorkbenchMenuItems({}),
    serviceMenuItems: buildServiceMenuItems(),
    stones: 0,
    stonesDisplay: '0',
    taskCount: 0,
    deviceId: '',
    hasLoginToken: false,
    // 认证相关
    certStatus: '',          // pending_payment / pending_review / approved / rejected
    certStage: '',           // 阶段文案
    certType: '',            // designer / enterprise
    certIdentityType: '',    // 设计师/施工队/企业主 等
    certRealName: '',        // 实名（个人：姓名，企业：企业名）
    certRealNameMasked: '',  // 脱敏后的实名
    vipLevelShortDisplay: '--',
    vipLevelText: 'VIP --',
    vipLevelIsFallback: true,
    lastProfileRefreshTime: 0,
    lastProfileRefreshToken: '',
    lastProfileRefreshInterval: 60000, // 1分钟
    lastCertificationRefreshTime: 0,
    lastCertificationRefreshToken: '',
    certificationRefreshInterval: CERTIFICATION_REFRESH_INTERVAL,
  },

  /**
   * 生命周期函数--监听页面加载
   */
  onLoad() {
    this.syncWindowBackground();
    this.initDeviceId();
    this.loadUserData(true);
  },

  /**
   * 生命周期函数--监听页面显示
   */
  syncTabBar() {
    const tabBar = typeof (this as any).getTabBar === 'function' ? (this as any).getTabBar() : null
    if (tabBar && typeof tabBar.setCurrent === 'function') {
      tabBar.setCurrent(3)
    }
  },

  syncWindowBackground() {
    if (typeof wx.setBackgroundColor !== 'function') {
      return;
    }

    wx.setBackgroundColor({
      backgroundColor: PAGE_BACKGROUND_BOTTOM,
      backgroundColorTop: PAGE_BACKGROUND_TOP,
      backgroundColorBottom: PAGE_BACKGROUND_BOTTOM,
    });
  },

  onHide() { this.hideShareMaterials(); },
  onUnload() { this.hideShareMaterials(); },
  onShow() {
    this.refreshShareMaterials();
    this.syncWindowBackground();
    this.syncTabBar();
    this.loadUserData();
    if (this.isLoggedIn()) {
      this.loadCertificationStatus();
    }
  },

  /**
   * 初始化设备ID
   */
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

  /**
   * 生成带签名的请求头
   */
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

  isLoggedIn() {
    return !!(this.data.token || wx.getStorageSync('token'));
  },

  refreshWorkbenchMenuItems(overrides: Record<string, any> = {}) {
    const nextState = {
      hasLoginToken: this.data.hasLoginToken,
      certStatus: this.data.certStatus,
      certType: this.data.certType,
      certIdentityType: this.data.certIdentityType,
      ...overrides,
    };
    this.setData({
      workbenchMenuItems: buildWorkbenchMenuItems(nextState),
    });
  },

  /**
   * 加载用户数据
   */
  loadUserData(forceRemote = false) {
    const token = wx.getStorageSync('token');
    if (this.__materialsAccount !== String(token || '')) this.resetShareMaterials();
    const userInfo = wx.getStorageSync('userInfo') || null;
    const currentUserInfo = this.data.userInfo || null;
    const displayUserInfo = userInfo || currentUserInfo;
    const vipDisplays = resolveVipDisplays(!!token, displayUserInfo);

    if (token) {
      this.setData({
        token: token,
        userInfo: displayUserInfo,
        hasLoginToken: true,
        commonMenuItems: buildCommonMenuItems(),
        serviceMenuItems: buildServiceMenuItems(),
        workbenchMenuItems: buildWorkbenchMenuItems({
          hasLoginToken: true,
          certStatus: this.data.certStatus,
          certType: this.data.certType,
          certIdentityType: this.data.certIdentityType,
        }),
        stonesDisplay: formatStonesDisplay(this.data.stones || 0),
        vipLevelShortDisplay: vipDisplays.vipLevelShortDisplay,
        vipLevelText: vipDisplays.vipLevelText,
        vipLevelIsFallback: vipDisplays.vipLevelIsFallback,
      });

      const lastProfileRefreshTime = Number(this.data.lastProfileRefreshTime || 0);
      const refreshInterval = Number(this.data.lastProfileRefreshInterval || 60000);
      const shouldRefreshRemote = forceRemote || !userInfo || this.data.lastProfileRefreshToken !== token || Date.now() - lastProfileRefreshTime > refreshInterval;

      if (shouldRefreshRemote) {
        this.setData({
          lastProfileRefreshTime: Date.now(),
          lastProfileRefreshToken: token,
        });
        this.loadStones();
        this.loadTaskHistory();
        this.loadUserProfile();
        this.loadCertificationStatus(true);
      }
    } else {
      this.setData({
        token: '',
        userInfo: null,
        userProfile: { nickname: '', avatar: '', identityType: '' },
        hasLoginToken: false,
        stones: 0,
        taskCount: 0,
        stonesDisplay: '0',
        certStatus: '',
        certStage: '',
        certType: '',
        certIdentityType: '',
        certRealName: '',
        certRealNameMasked: '',
        commonMenuItems: buildCommonMenuItems(),
        workbenchMenuItems: buildWorkbenchMenuItems({ hasLoginToken: false }),
        serviceMenuItems: buildServiceMenuItems(),
        vipLevelShortDisplay: '--',
        vipLevelText: 'VIP --',
        vipLevelIsFallback: true,
        lastProfileRefreshTime: 0,
        lastProfileRefreshToken: '',
        lastCertificationRefreshTime: 0,
        lastCertificationRefreshToken: '',
      });
    }
  },

  /**
   * 加载认证状态与实名信息
   */
  async loadCertificationStatus(force = false) {
    const token = this.data.token;
    if (!token) return;

    const lastRefreshTime = Number(this.data.lastCertificationRefreshTime || 0);
    const refreshInterval = Number(this.data.certificationRefreshInterval || CERTIFICATION_REFRESH_INTERVAL);
    if (
      !force
      && this.data.lastCertificationRefreshToken === token
      && Date.now() - lastRefreshTime < refreshInterval
    ) {
      return;
    }

    this.setData({
      lastCertificationRefreshTime: Date.now(),
      lastCertificationRefreshToken: token,
    });

    try {
      const apiPath = '/api/v1/miniprogram/certification/status';
      const headers = this.getAuthHeaders(apiPath);
      if (!headers) {
        return;
      }
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (resp) => {
            if (resp.statusCode === 200 && resp.data) {
              const data = resp.data as any;
              if (data.code === 0) {
                resolve(data.data || {});
              } else {
                reject(new Error(data.msg || '获取认证状态失败'));
              }
            } else {
              reject(new Error(`请求失败: ${resp.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      this.setData({
        certStatus: res.status || '',
        certStage: res.stage || '',
        certType: res.cert_type || '',
        certIdentityType: res.identity_type || '',
        certRealName: res.real_name || '',
        certRealNameMasked: res.real_name_masked || '',
      });
      this.refreshWorkbenchMenuItems({
        hasLoginToken: true,
        certStatus: res.status || '',
        certType: res.cert_type || '',
        certIdentityType: res.identity_type || '',
      });
    } catch (e) {
      console.warn('获取认证状态失败:', e);
      this.setData({
        certStatus: '',
        certStage: '',
        certType: '',
        certIdentityType: '',
        certRealName: '',
        certRealNameMasked: '',
      });
      this.refreshWorkbenchMenuItems({
        hasLoginToken: this.data.hasLoginToken,
        certStatus: '',
        certType: '',
        certIdentityType: '',
      });
    }
  },

  /**
   * 加载用户资料
   */
  async loadUserProfile() {
    const token = this.data.token;
    if (!token) {
      return;
    }

    try {
      const apiPath = '/api/v1/miniprogram/profile';
      const headers = this.getAuthHeaders(apiPath);
      if (!headers) {
        return;
      }

      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            const data = (res.data || {}) as any;
            if (res.statusCode === 200 && data.code === 0) {
              resolve(data.data);
              return;
            }
            // 401/404 或“用户不存在”视为登录失效，清 token 并跳转登录
            if (res.statusCode === 401 || res.statusCode === 404 || (data.msg && (String(data.msg).includes('用户不存在') || String(data.msg).includes('请重新登录')))) {
              wx.removeStorageSync('token');
              wx.removeStorageSync('userInfo');
              wx.showToast({ title: '登录已失效，请重新登录', icon: 'none', duration: 2000 });
              setTimeout(() => {
                wx.reLaunch({ url: '/pages/login/login' });
              }, 500);
              reject(new Error(data.msg || '登录已失效，请重新登录'));
              return;
            }
            reject(new Error(data.msg || `请求失败: ${res.statusCode}`));
          },
          fail: (err) => {
            reject(err);
          },
        });
      });

      const cachedUserInfo = (wx.getStorageSync('userInfo') || {}) as any;
      const mergedUserInfo = {
        ...cachedUserInfo,
        ...(this.data.userInfo || {}),
        ...res,
      };

      if (!mergedUserInfo.username && res.nickname) {
        mergedUserInfo.username = res.nickname;
      }
      if (!mergedUserInfo.name && res.nickname) {
        mergedUserInfo.name = res.nickname;
      }
      if (!mergedUserInfo.avatarUrl && res.avatar) {
        mergedUserInfo.avatarUrl = res.avatar;
      }

      wx.setStorageSync('userInfo', mergedUserInfo);

      this.setData({
        userInfo: mergedUserInfo,
        userProfile: {
          nickname: res.nickname || mergedUserInfo.username || mergedUserInfo.name || '',
          avatar: res.avatar || mergedUserInfo.avatar || mergedUserInfo.avatarUrl || '',
          identityType: res.identity_type || mergedUserInfo.identity_type || '',
        },
        ...resolveVipDisplays(true, res, mergedUserInfo),
      });

      // 存量已登录用户昵称补检（登录流程已拦截新登录，这里兜底老用户）
      this.maybePromptNickname(res);
    } catch (error: any) {
      console.error('获取用户资料失败:', error);
    }
  },

  /**
   * 存量已登录用户昵称补检：昵称为空时弹出完善弹窗，
   * 每个自然日最多提醒一次（保存成功后组件会清除该标记）。
   */
  maybePromptNickname(profile: any) {
    if (String(profile?.nickname || '').trim()) {
      return;
    }
    const now = new Date();
    const dayKey = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;
    if (wx.getStorageSync('nickname_prompt_day') === dayKey) {
      return;
    }
    const guard = this.selectComponent('#nicknameGuard') as any;
    if (!guard || typeof guard.show !== 'function') {
      return;
    }
    wx.setStorageSync('nickname_prompt_day', dayKey);
    guard.show({
      nickname: '',
      avatar: String(profile?.avatar || ''),
    });
  },

  // 昵称弹窗保存成功后刷新资料展示
  onNicknameGuardCompleted() {
    void this.loadUserProfile();
  },

  /**
   * 加载灵石余额
   */
  async loadStones() {
    const token = this.data.token;
    if (!token) {
      return;
    }

    try {
      // 使用简化的token认证（只需要token header）
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/user/stones`,
          method: 'GET',
          header: {
            'token': token,
            'Content-Type': 'application/json',
          },
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const data = res.data as any;
              if (data.code === 0) {
                resolve(data.data);
              } else {
                reject(new Error(data.msg || '获取余额失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: (err) => {
            reject(err);
          },
        });
      });

      this.setData({
        stones: res.stones || 0,
        stonesDisplay: formatStonesDisplay(res.stones || 0),
      });
    } catch (error: any) {
      console.error('获取灵石余额失败:', error);
      // 静默失败，不显示错误提示
    }
  },

  /**
   * 加载任务历史
   */
  async loadTaskHistory() {
    const token = this.data.token;
    if (!token) {
      return;
    }

    try {
      // 使用简化的token认证（只需要token header）
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/user/tasks?page=1&page_size=1`,
          method: 'GET',
          header: {
            'token': token,
            'Content-Type': 'application/json',
          },
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const data = res.data as any;
              if (data.code === 0) {
                resolve(data.data);
              } else {
                reject(new Error(data.msg || '获取任务历史失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: (err) => {
            reject(err);
          },
        });
      });

      this.setData({
        taskCount: res.total || 0,
      });
    } catch (error: any) {
      console.error('获取任务历史失败:', error);
      // 静默失败，不显示错误提示
    }
  },

  /**
   * 页面相关事件处理函数--监听用户下拉动作
   */
  onPullDownRefresh() {
    this.refreshShareMaterials();
    this.loadUserData();
    wx.stopPullDownRefresh();
  },

  /**
   * 用户点击右上角分享
   */
  onShareAppMessage() {
    return {
      title: '个人中心',
      path: '/pages/my/my'
    }
  },

  /**
   * 点击用户资料
   */
  onProfileTap() {
    if (!this.isLoggedIn()) {
      // 未登录，跳转到登录页
      wx.navigateTo({
        url: '/pages/login/login',
      });
    } else {
      // 已登录，跳转到个人信息修改页面
      wx.navigateTo({
        url: '/pages/myInformationmodification/myInformationmodification',
      });
    }
  },

  /**
   * 跳转到生成历史页面
   */
  goToGenerateHistory() {
    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '请先登录',
        icon: 'none'
      });
      return;
    }
    wx.navigateTo({
      url: '/pages/generatehistory/generatehistory',
    });
  },

  /**
   * 跳转到钱包页面
   */
  goToWallet() {
    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      });
      return;
    }
    wx.navigateTo({
      url: '/pages/wallet/wallet',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  /**
   * 点击收藏项
   */
  onFavoriteTap(e: any) {
    const type = e.currentTarget.dataset.type
    console.log('点击收藏', type)

    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      })
      return
    }

    wx.navigateTo({
      url: '/pages/myfavorites/myfavorites',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  /**
   * 点击发布项
   */
  onPostTap(e: any) {
    const type = e.currentTarget.dataset.type
    console.log('点击发布', type)

    // 我的发布：跳转到我的发布页面
    if (type === 'publish') {
      if (!this.isLoggedIn()) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        })
        return
      }
      wx.navigateTo({
        url: '/pages/release/release',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
      return
    }

    // 灵石明细：跳到灵石明细页面
    if (type === 'stones') {
      wx.navigateTo({
        url: '/pages/lingshidetails/lingshidetails',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
      return
    }

    // 订单管理：跳转到订单管理页面
    if (type === 'orders') {
      if (!this.isLoggedIn()) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        })
        return
      }
      wx.navigateTo({
        url: '/pages/ordermanagement/ordermanagement',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
      return
    }
  },

  /**
   * 点击设置项
   */
  onSettingTap(e: any) {
    const type = e.currentTarget.dataset.type
    console.log('点击设置', type)

    // 基础设置：跳转到基础设置页面
    if (type === 'identity') {
      if (!this.isLoggedIn()) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        })
        return
      }
      wx.navigateTo({
        url: '/pages/settings/settings',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
      return
    }

    // 实用工具：跳转到实用工具页面
    if (type === 'tools2') {
      wx.navigateTo({
        url: '/pages/utilitytools/utilitytools',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
      return
    }

    // 其他设置项暂时保持为提示
    const map: Record<string, string> = {
      service: '联系客服',
    }
    wx.showToast({
      title: map[type] || '设置',
      icon: 'none',
    })
  },

  onProjectMenuTap(e: any) {
    const action = e.currentTarget.dataset.action;

    if (action === 'project') {
      this.goToGenerateHistory();
      return;
    }

    if (action === 'favorite') {
      this.onFavoriteTap({ currentTarget: { dataset: { type: 'favorite' } } })
      return;
    }

    if (action === 'buildingCodeDocs') {
      if (!this.isLoggedIn()) {
        wx.showToast({ title: '请先登录', icon: 'none' });
        return;
      }
      wx.navigateTo({
        url: '/pages/buildingcodedocs/buildingcodedocs',
        fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
      });
      return;
    }

    if (action === 'orders') {
      this.onPostTap({ currentTarget: { dataset: { type: 'orders' } } });
      return;
    }

    if (action === 'wallet') {
      this.goToTopupCenter();
      return;
    }

  },

  onWorkbenchMenuTap(e: any) {
    const action = e.currentTarget.dataset.action;

    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      });
      return;
    }

    if (action === 'identity') {
      this.onSettingTap({ currentTarget: { dataset: { type: 'tools' } } });
      return;
    }

    if (action === 'designerHome') {
      const hasDesignerAccess = hasDesignerWorkbenchAccess({
        hasLoginToken: this.data.hasLoginToken,
        certStatus: this.data.certStatus,
        certIdentityType: this.data.certIdentityType,
      });
      if (!hasDesignerAccess) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        });
        return;
      }
      const userId = Number((this.data.userInfo || {}).id || 0);
      if (!userId) {
        wx.showToast({
          title: '暂未获取到设计师信息',
          icon: 'none',
        });
        return;
      }
      wx.navigateTo({
        url: `/pages/designerhome/designerhome?userId=${userId}`,
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          });
        },
      });
      return;
    }

    if (action === 'publish') {
      this.onPostTap({ currentTarget: { dataset: { type: 'publish' } } });
      return;
    }

    if (action === 'settings') {
      this.goToSettings();
      return;
    }

    if (action === 'tools') {
      this.onSettingTap({ currentTarget: { dataset: { type: 'tools2' } } });
      return;
    }
  },

  onAccountMenuTap(e: any) {
    const action = e.currentTarget.dataset.action;

    if (action === 'bindPhone') {
      if (!this.isLoggedIn()) {
        wx.showToast({ title: '请先登录', icon: 'none' });
        return;
      }
      wx.navigateTo({
        url: '/pages/phonebind/phonebind',
        fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
      });
      return;
    }

    if (action === 'message') {
      if (!this.isLoggedIn()) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        });
        return;
      }
      wx.navigateTo({
        url: '/pages/messagecenter/messagecenter',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          });
        },
      });
      return;
    }

    if (action === 'adminLogin') {
      this.goToAdminLogin();
      return;
    }

    if (action === 'settings') {
      this.goToSettings();
      return;
    }

    if (action === 'tools') {
      this.onSettingTap({ currentTarget: { dataset: { type: 'tools2' } } });
      return;
    }
  },

  goToSettings() {
    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      });
      return;
    }
    wx.navigateTo({
      url: '/pages/settings/settings',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  goToTopupCenter() {
    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      });
      return;
    }
    wx.navigateTo({
      url: '/pages/topupcenter/topupcenter',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  goToAdminLogin() {
    wx.navigateTo({
      url: '/pages/adminlogin/adminlogin',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  onLogoutTap() {
    if (!this.isLoggedIn()) {
      wx.showToast({
        title: '当前未登录',
        icon: 'none',
      });
      return;
    }

    wx.showModal({
      title: '提示',
      content: '确认退出当前账号吗？',
      success: (res) => {
        if (!res.confirm) {
          return;
        }

        wx.removeStorageSync('token');
        wx.removeStorageSync('userInfo');
        this.loadUserData();
        wx.showToast({
          title: '已退出登录',
          icon: 'none',
        });
      },
    });
  },

  /**
   * tabbar 切换
   */
  onTabSwitch(e: any) {
    console.log('切换 tab', e.detail)
  },
});
