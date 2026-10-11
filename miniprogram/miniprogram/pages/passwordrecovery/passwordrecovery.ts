export {};

const API_BASE_URL = 'https://api.jiadilingguang.com';

function buildCanSubmit(data: Record<string, any>) {
  const username = String(data.username || '').trim();
  const newPassword = String(data.newPassword || '');
  const confirmPassword = String(data.confirmPassword || '');
  return username.length >= 4 && newPassword.length >= 6 && newPassword === confirmPassword;
}

Page({
  data: {
    username: '',
    newPassword: '',
    confirmPassword: '',
    loading: false,
    canSubmit: false,
  },

  onLoad(options: any) {
    const username = String(options?.username || '');
    this.setData({
      username,
      canSubmit: buildCanSubmit({ username }),
    });
  },

  syncCanSubmit(nextState: Record<string, any> = {}) {
    const merged = {
      username: this.data.username,
      newPassword: this.data.newPassword,
      confirmPassword: this.data.confirmPassword,
      ...nextState,
    };
    this.setData({
      canSubmit: buildCanSubmit(merged),
    });
  },

  onUsernameInput(e: any) {
    const username = String(e.detail.value || '');
    this.setData({ username });
    this.syncCanSubmit({ username });
  },

  onNewPasswordInput(e: any) {
    const newPassword = String(e.detail.value || '');
    this.setData({ newPassword });
    this.syncCanSubmit({ newPassword });
  },

  onConfirmPasswordInput(e: any) {
    const confirmPassword = String(e.detail.value || '');
    this.setData({ confirmPassword });
    this.syncCanSubmit({ confirmPassword });
  },

  async handlePhoneRecovery(e: any) {
    if (this.data.loading) {
      return;
    }
    const username = String(this.data.username || '').trim();
    const newPassword = String(this.data.newPassword || '');
    const confirmPassword = String(this.data.confirmPassword || '');
    if (!username) {
      wx.showToast({ title: '请输入用户名', icon: 'none' });
      return;
    }
    if (username.length < 4) {
      wx.showToast({ title: '用户名至少4位', icon: 'none' });
      return;
    }
    if (newPassword.length < 6) {
      wx.showToast({ title: '新密码至少6位', icon: 'none' });
      return;
    }
    if (newPassword !== confirmPassword) {
      wx.showToast({ title: '两次密码不一致', icon: 'none' });
      return;
    }
    const phoneCode = String(e?.detail?.code || '').trim();
    const errMsg = String(e?.detail?.errMsg || '');
    if (!phoneCode) {
      wx.showToast({
        title: errMsg.includes('deny') ? '你已取消手机号验证' : '请先完成手机号验证',
        icon: 'none',
      });
      return;
    }

    this.setData({ loading: true });
    try {
      const result: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/profile/password/recover`,
          method: 'POST',
          header: {
            'Content-Type': 'application/json',
          },
          data: {
            username,
            new_password: newPassword,
            phone_code: phoneCode,
          },
          success: resolve,
          fail: reject,
        });
      });

      const payload = result?.data || {};
      if (result.statusCode !== 200 || payload.code !== 0) {
        throw new Error(payload.msg || '找回密码失败');
      }

      const verifiedPhone = String(payload?.data?.verified_phone || '已验证手机号');
      this.setData({ loading: false });
      wx.showModal({
        title: '找回成功',
        content: `已通过${verifiedPhone}完成验证，请使用新密码重新登录。`,
        showCancel: false,
        success: () => {
          wx.redirectTo({
            url: `/pages/login/login?type=password&username=${encodeURIComponent(username)}`,
          });
        },
      });
    } catch (error: any) {
      wx.showToast({
        title: error?.message || '找回密码失败',
        icon: 'none',
        duration: 2200,
      });
      this.setData({ loading: false });
    }
  },
});
