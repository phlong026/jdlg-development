// pages/myInformationmodification/myInformationmodification.ts
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint
} from '../../utils/deviceFingerprint';
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { resolveAssetPath } from '../../utils/asset';

const API_BASE_URL = 'https://api.jiadilingguang.com';

function resolveEntryDisplay(section: string) {
  if (section === 'profile') {
    return {
      navTitle: '个人资料',
      navSubtitle: '头像 · 昵称 · 账号安全',
      showProfileSection: true,
    };
  }

  return {
    navTitle: '个人信息',
    navSubtitle: '头像 · 昵称 · 账号安全',
    showProfileSection: true,
  };
}

function buildPasswordValidation(form: Record<string, any>, hasPassword: boolean) {
  const username = String(form?.username || '').trim();
  const password = String(form?.password || '');
  const confirmPassword = String(form?.confirmPassword || '');
  const oldPassword = String(form?.oldPassword || '');
  const usernameValid = username.length >= 4;
  const passwordValid = password.length >= 6;
  const confirmValid = !!confirmPassword && password === confirmPassword;
  const oldPasswordValid = !hasPassword || oldPassword.length > 0;
  return {
    usernameValid,
    usernameTip: username ? (usernameValid ? '用户名可用于账号密码登录' : '用户名至少 4 位') : '用户名用于账号密码登录，不直接对外展示',
    passwordValid,
    passwordTip: password ? (passwordValid ? '密码长度符合要求' : '密码至少 6 位') : '建议使用你容易记住但不容易被猜到的密码',
    confirmValid,
    confirmTip: confirmPassword ? (confirmValid ? '两次密码输入一致' : '两次密码输入不一致') : '再次输入一遍密码，避免输错',
    oldPasswordValid,
    oldPasswordTip: hasPassword ? (oldPasswordValid ? '已填写原密码' : '修改密码前需要先填写原密码') : '',
    canSubmit: usernameValid && passwordValid && confirmValid && oldPasswordValid,
  };
}

function getFirstPasswordValidationMessage(validation: ReturnType<typeof buildPasswordValidation>, hasPassword: boolean) {
  if (!validation.usernameValid) {
    return '用户名至少4个字符';
  }
  if (!validation.passwordValid) {
    return '密码至少6个字符';
  }
  if (!validation.confirmValid) {
    return '两次密码不一致';
  }
  if (hasPassword && !validation.oldPasswordValid) {
    return '请输入原密码';
  }
  return '';
}

Page({
  data: {
    defaultAvatarImage: resolveAssetPath('/assets/images/home.jpg'),
    // 用户信息
    userInfo: {
      id: 0,
      username: '',
      nickname: '',
      avatar: '',
      designerBio: '',
      specialtyStyles: '',
      designerExperienceYears: 0,
      designerVisible: true,
      serviceTitle: '',
      serviceQuote: 0,
      serviceIntro: '',
      serviceEnabled: false,
      hasPassword: false,
    },
    currentDeviceId: '',
    // 加载状态
    loading: true,
    saving: false,
    savingNickname: false,
    savingPassword: false,
    manageDesignerInProfile: false,
    designerManageSection: '',
    entrySection: '',
    navTitle: '个人信息',
    navSubtitle: '头像 · 昵称 · 账号安全',
    showProfileSection: true,
    // 编辑状态
    editingNickname: false,
    tempNickname: '',
    designerForm: {
      designerBio: '',
      specialtyStyles: '',
      designerExperienceYears: '',
      designerVisible: true,
      serviceTitle: '',
      serviceQuote: '',
      serviceIntro: '',
      serviceEnabled: false,
    },
    // 密码设置弹窗
    showPasswordModal: false,
    passwordForm: {
      username: '',
      password: '',
      confirmPassword: '',
      oldPassword: '',
    },
    passwordValidation: buildPasswordValidation({}, false),
  },

  onLoad(options: any) {
    const section = String(options?.section || '').trim()
    const source = String(options?.source || '').trim()
    const entrySection = source === 'settings' && section === 'profile'
      ? section
      : ''
    const entryDisplay = resolveEntryDisplay(entrySection)
    this.setData({
      manageDesignerInProfile: source === 'designerhome',
      designerManageSection: source === 'designerhome' ? section : '',
      entrySection,
      navTitle: entryDisplay.navTitle,
      navSubtitle: entryDisplay.navSubtitle,
      showProfileSection: entryDisplay.showProfileSection,
    })
    this.loadUserInfo();
    this.getCurrentDeviceId();
  },

  onShow() {
    this.loadUserInfo();
    this.getCurrentDeviceId();
  },

  // 获取当前设备ID
  async getCurrentDeviceId() {
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
    this.setData({
      currentDeviceId: String(deviceId || '').trim(),
    });
  },

  // 生成带签名的请求头
  getAuthHeaders(apiPath: string, body: any = {}) {
    const token = wx.getStorageSync('token');
    if (!token) {
      return null;
    }
    const params = generateRequestParams(token, body, apiPath, this.data.currentDeviceId);
    return {
      ...paramsToHeaders(params),
      'Content-Type': 'application/json',
    };
  },

  // 加载用户信息
  async loadUserInfo() {
    const token = wx.getStorageSync('token');
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' });
      setTimeout(() => {
        wx.navigateTo({ url: '/pages/login/login' });
      }, 1500);
      return;
    }

    this.setData({ loading: true });

    try {
      const apiPath = '/api/v1/miniprogram/profile';
      const headers = this.getAuthHeaders(apiPath);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => resolve(res),
          fail: (err) => reject(err),
        });
      });

      if (res.statusCode === 200 && res.data.code === 0) {
        const data = res.data.data;
        this.setData({
          userInfo: {
            id: data.id,
            username: data.username || '',
            nickname: data.nickname || '',
            avatar: data.avatar || '',
            designerBio: data.designer_bio || '',
            specialtyStyles: data.specialty_styles || '',
            designerExperienceYears: Number(data.designer_experience_years) || 0,
            designerVisible: data.designer_visible !== false,
            serviceTitle: data.service_title || '',
            serviceQuote: Number(data.service_quote) || 0,
            serviceIntro: data.service_intro || '',
            serviceEnabled: data.service_enabled === true,
            hasPassword: data.has_password || false,
          },
          designerForm: {
            designerBio: data.designer_bio || '',
            specialtyStyles: data.specialty_styles || '',
            designerExperienceYears: String(Number(data.designer_experience_years) || 0),
            designerVisible: data.designer_visible !== false,
            serviceTitle: data.service_title || '',
            serviceQuote: String(Number(data.service_quote) || 0),
            serviceIntro: data.service_intro || '',
            serviceEnabled: data.service_enabled === true,
          },
          loading: false,
        });
      } else {
        throw new Error(res.data?.msg || '获取用户信息失败');
      }
    } catch (error: any) {
      console.error('加载用户信息失败:', error);
      wx.showToast({ title: error.message || '加载失败', icon: 'none' });
      this.setData({ loading: false });
    }
  },

  goToDesignerHomepageManager() {
    const userInfo = wx.getStorageSync('userInfo') || this.data.userInfo || {}
    const userId = Number(userInfo.id || userInfo.userId || 0)
    if (!userId) {
      wx.showToast({
        title: '暂未获取到设计师信息',
        icon: 'none',
      })
      return
    }

    wx.navigateTo({
      url: `/pages/designerhome/designerhome?userId=${userId}`,
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  // 选择头像
  async chooseAvatar() {
    try {
      const res = await new Promise<WechatMiniprogram.ChooseMediaSuccessCallbackResult>((resolve, reject) => {
        wx.chooseMedia({
          count: 1,
          mediaType: ['image'],
          sourceType: ['album', 'camera'],
          success: resolve,
          fail: reject,
        });
      });

      if (res.tempFiles && res.tempFiles.length > 0) {
        const tempFilePath = res.tempFiles[0].tempFilePath;
        wx.showLoading({ title: '上传头像中...', mask: true });
        try {
          const avatarUrl = await this.uploadAvatarImage(tempFilePath);
          await this.updateAvatar(avatarUrl);
        } finally {
          wx.hideLoading();
        }
      }
    } catch (error) {
      console.error('选择头像失败:', error);
    }
  },

  async uploadAvatarImage(tempFilePath: string): Promise<string> {
    const token = wx.getStorageSync('token');
    if (!token) {
      throw new Error('请先登录');
    }
    const apiPath = '/api/v1/miniprogram/ai/upload-reference-image';
    const deviceID = getCachedDeviceFingerprint() || '';
    const params = generateRequestParams(token, '{}', apiPath, deviceID);
    const headers = paramsToHeaders(params);
    return new Promise((resolve, reject) => {
      wx.uploadFile({
        url: `${API_BASE_URL}${apiPath}`,
        filePath: tempFilePath,
        name: 'file',
        header: headers,
        success: (res) => {
          if (res.statusCode !== 200) {
            reject(new Error(`上传失败: ${res.statusCode}`));
            return;
          }
          try {
            const data = JSON.parse(res.data) as any;
            if (data.code === 0 && data.data?.url) {
              resolve(String(data.data.url));
              return;
            }
            reject(new Error(data.msg || '头像上传失败'));
          } catch (error) {
            reject(new Error('头像上传响应解析失败'));
          }
        },
        fail: (err) => reject(err),
      });
    });
  },

  // 更新头像
  async updateAvatar(avatarUrl: string) {
    this.setData({ saving: true });

    try {
      const apiPath = '/api/v1/miniprogram/profile/avatar';
      const body = { avatar: avatarUrl };
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'PUT',
          header: headers,
          data: body,
          success: resolve,
          fail: reject,
        });
      });

      if (res.statusCode === 200 && res.data.code === 0) {
        const cachedUserInfo = wx.getStorageSync('userInfo') || {};
        wx.setStorageSync('userInfo', {
          ...cachedUserInfo,
          avatar: avatarUrl,
          avatarUrl,
        });
        wx.showToast({ title: '头像更新成功', icon: 'success' });
        this.setData({
          'userInfo.avatar': avatarUrl,
        });
      } else {
        throw new Error(res.data?.msg || '更新失败');
      }
    } catch (error: any) {
      wx.showToast({ title: error.message || '更新失败', icon: 'none' });
    } finally {
      this.setData({ saving: false });
    }
  },

  // 开始编辑昵称
  startEditNickname() {
    this.setData({
      editingNickname: true,
      tempNickname: this.data.userInfo.nickname,
    });
  },

  // 昵称输入
  onNicknameInput(e: any) {
    this.setData({ tempNickname: String(e.detail.value || '') });
  },

  // 取消编辑昵称
  cancelEditNickname() {
    this.setData({
      editingNickname: false,
      tempNickname: '',
    });
  },

  onDesignerFieldInput(e: any) {
    const field = String(e.currentTarget.dataset.field || '');
    this.setData({
      [`designerForm.${field}`]: e.detail.value,
    });
  },

  onServiceEnabledChange(e: any) {
    this.setData({
      'designerForm.serviceEnabled': !!e.detail.value,
    });
  },

  onDesignerVisibleChange(e: any) {
    this.setData({
      'designerForm.designerVisible': !!e.detail.value,
    });
  },

  async saveDesignerProfile() {
    const designerBio = String(this.data.designerForm.designerBio || '').trim();
    const specialtyStyles = String(this.data.designerForm.specialtyStyles || '').trim();
    const designerExperienceYears = Math.max(0, Number(this.data.designerForm.designerExperienceYears) || 0);
    const designerVisible = this.data.designerForm.designerVisible !== false;
    const serviceTitle = String(this.data.designerForm.serviceTitle || '').trim();
    const serviceQuote = Math.max(0, Number(this.data.designerForm.serviceQuote) || 0);
    const serviceIntro = String(this.data.designerForm.serviceIntro || '').trim();
    const serviceEnabled = !!this.data.designerForm.serviceEnabled;

    this.setData({ saving: true });

    try {
      const apiPath = '/api/v1/miniprogram/profile/designer';
      const body = {
        designer_bio: designerBio,
        specialty_styles: specialtyStyles,
        designer_experience_years: designerExperienceYears,
        designer_visible: designerVisible,
        service_title: serviceTitle,
        service_quote: serviceQuote,
        service_intro: serviceIntro,
        service_enabled: serviceEnabled,
      };
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'PUT',
          header: headers,
          data: body,
          success: resolve,
          fail: reject,
        });
      });

      if (res.statusCode === 200 && res.data.code === 0) {
        wx.showToast({ title: '设计师资料已保存', icon: 'success' });
        this.setData({
          'userInfo.designerBio': designerBio,
          'userInfo.specialtyStyles': specialtyStyles,
          'userInfo.designerExperienceYears': designerExperienceYears,
          'userInfo.designerVisible': designerVisible,
          'userInfo.serviceTitle': serviceTitle,
          'userInfo.serviceQuote': serviceQuote,
          'userInfo.serviceIntro': serviceIntro,
          'userInfo.serviceEnabled': serviceEnabled,
          'designerForm.designerExperienceYears': String(designerExperienceYears),
          'designerForm.serviceQuote': String(serviceQuote),
        });
      } else {
        throw new Error(res.data?.msg || '保存失败');
      }
    } catch (error: any) {
      wx.showToast({ title: error.message || '保存失败', icon: 'none' });
    } finally {
      this.setData({ saving: false });
    }
  },

  async saveServiceConfig() {
    const serviceTitle = String(this.data.designerForm.serviceTitle || '').trim();
    const serviceQuote = Math.max(0, Number(this.data.designerForm.serviceQuote) || 0);
    const serviceIntro = String(this.data.designerForm.serviceIntro || '').trim();
    const serviceEnabled = !!this.data.designerForm.serviceEnabled;

    this.setData({ saving: true });

    try {
      const apiPath = '/api/v1/miniprogram/profile/designer/service-config';
      const body = {
        service_title: serviceTitle,
        service_quote: serviceQuote,
        service_intro: serviceIntro,
        service_enabled: serviceEnabled,
      };
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'PUT',
          header: headers,
          data: body,
          success: resolve,
          fail: reject,
        });
      });

      if (res.statusCode === 200 && res.data.code === 0) {
        wx.showToast({ title: '服务配置已保存', icon: 'success' });
        this.setData({
          'userInfo.serviceTitle': serviceTitle,
          'userInfo.serviceQuote': serviceQuote,
          'userInfo.serviceIntro': serviceIntro,
          'userInfo.serviceEnabled': serviceEnabled,
          'designerForm.serviceQuote': String(serviceQuote),
        });
      } else {
        throw new Error(res.data?.msg || '保存失败');
      }
    } catch (error: any) {
      wx.showToast({ title: error.message || '保存失败', icon: 'none' });
    } finally {
      this.setData({ saving: false });
    }
  },

  // 保存昵称
  async saveNickname() {
    const nickname = this.data.tempNickname.trim();
    if (!nickname) {
      wx.showToast({ title: '昵称不能为空', icon: 'none' });
      return;
    }
    if (nickname === String(this.data.userInfo.nickname || '').trim()) {
      this.setData({
        editingNickname: false,
        tempNickname: '',
      });
      return;
    }

    this.setData({ savingNickname: true });

    try {
      const apiPath = '/api/v1/miniprogram/profile/nickname';
      const body = { nickname };
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'PUT',
          header: headers,
          data: body,
          success: resolve,
          fail: reject,
        });
      });

      if (res.statusCode === 200 && res.data.code === 0) {
        const cachedUserInfo = wx.getStorageSync('userInfo') || {};
        wx.setStorageSync('userInfo', {
          ...cachedUserInfo,
          nickname,
        });
        wx.showToast({ title: '修改成功', icon: 'success' });
        this.setData({
          'userInfo.nickname': nickname,
          editingNickname: false,
          tempNickname: '',
        });
      } else {
        throw new Error(res.data?.msg || '修改失败');
      }
    } catch (error: any) {
      wx.showToast({ title: error.message || '修改失败', icon: 'none' });
    } finally {
      this.setData({ savingNickname: false });
    }
  },

  // 打开密码设置弹窗
  openPasswordModal() {
    const nextPasswordForm = {
      username: this.data.userInfo.username || '',
      password: '',
      confirmPassword: '',
      oldPassword: '',
    };
    this.setData({
      showPasswordModal: true,
      passwordForm: nextPasswordForm,
      passwordValidation: buildPasswordValidation(nextPasswordForm, !!this.data.userInfo.hasPassword),
    });
  },

  // 关闭密码设置弹窗
  closePasswordModal() {
    this.setData({
      showPasswordModal: false,
      savingPassword: false,
    });
  },

  // 密码表单输入
  onPasswordFormInput(e: any) {
    const field = String(e.currentTarget.dataset.field || '');
    const nextPasswordForm = {
      ...this.data.passwordForm,
      [field]: String(e.detail.value || ''),
    };
    this.setData({
      passwordForm: nextPasswordForm,
      passwordValidation: buildPasswordValidation(nextPasswordForm, !!this.data.userInfo.hasPassword),
    });
  },

  // 保存密码设置
  async savePassword() {
    const { username, password, confirmPassword, oldPassword } = this.data.passwordForm;
    const { hasPassword } = this.data.userInfo;
    const normalizedUsername = String(username || '').trim();
    const normalizedPassword = String(password || '');
    const normalizedConfirmPassword = String(confirmPassword || '');
    const normalizedOldPassword = String(oldPassword || '');
    const validation = buildPasswordValidation({
      username: normalizedUsername,
      password: normalizedPassword,
      confirmPassword: normalizedConfirmPassword,
      oldPassword: normalizedOldPassword,
    }, !!hasPassword);

    if (!validation.canSubmit) {
      wx.showToast({ title: getFirstPasswordValidationMessage(validation, !!hasPassword), icon: 'none' });
      this.setData({ passwordValidation: validation });
      return;
    }

    this.setData({ savingPassword: true });

    try {
      const apiPath = '/api/v1/miniprogram/profile/password';
      const body: any = {
        username: normalizedUsername,
        password: normalizedPassword,
      };
      if (normalizedOldPassword) {
        body.old_password = normalizedOldPassword;
      }
      const headers = this.getAuthHeaders(apiPath, body);
      if (!headers) {
        throw new Error('生成请求头失败');
      }

      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: headers,
          data: body,
          success: resolve,
          fail: reject,
        });
      });

      if (res.statusCode === 200 && res.data.code === 0) {
        const cachedUserInfo = wx.getStorageSync('userInfo') || {};
        wx.setStorageSync('userInfo', {
          ...cachedUserInfo,
          username: normalizedUsername,
          hasPassword: true,
        });
        wx.showToast({ title: '设置成功', icon: 'success' });
        this.setData({
          showPasswordModal: false,
          'userInfo.username': normalizedUsername,
          'userInfo.hasPassword': true,
          passwordValidation: buildPasswordValidation({}, true),
        });
      } else {
        throw new Error(res.data?.msg || '设置失败');
      }
    } catch (error: any) {
      wx.showToast({ title: error.message || '设置失败', icon: 'none' });
    } finally {
      this.setData({ savingPassword: false });
    }
  },

  // 退出登录
  logout() {
    wx.showModal({
      title: '确认退出',
      content: '确定要退出登录吗？',
      success: (res) => {
        if (res.confirm) {
          wx.clearStorageSync();
          wx.reLaunch({ url: '/pages/login/login' });
        }
      },
    });
  },

  // 阻止弹窗内容区域点击冒泡，避免误触关闭
  onModalContentTap() {
    // noop
  },

  onShareAppMessage() {
    return {
      title: '甲第灵光 · 个人信息',
      path: '/pages/my/my',
    };
  },
});
