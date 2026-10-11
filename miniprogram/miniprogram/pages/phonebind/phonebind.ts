// pages/phonebind/phonebind.ts
export {};

const API_BASE_URL = 'https://api.jiadilingguang.com';

Page({
  data: {
    phone: '',
    smsCode: '',
    smsCountdown: 0,
    loading: false,
    boundPhone: '',
  },

  onLoad() {
    // 加载已绑定手机号
    this.loadBoundPhone();
  },

  loadBoundPhone() {
    const token = wx.getStorageSync('token');
    if (!token) return;
    wx.request({
      url: `${API_BASE_URL}/api/v1/miniprogram/profile`,
      method: 'GET',
      header: { token },
      success: (res: any) => {
        const data = res.data;
        if (data && data.code === 0 && data.data) {
          const phone: string = data.data.phone || '';
          if (phone) {
            // 脱敏显示
            this.setData({
              boundPhone: phone.substring(0, 3) + '****' + phone.substring(7),
            });
          }
        }
      },
    });
  },

  onPhoneInput(e: any) {
    this.setData({ phone: e.detail.value });
  },

  onSmsCodeInput(e: any) {
    this.setData({ smsCode: e.detail.value });
  },

  async sendSmsCode() {
    const phone = (this.data.phone || '').trim();
    if (!/^1\d{10}$/.test(phone)) {
      wx.showToast({ title: '请输入正确的11位手机号', icon: 'none' });
      return;
    }
    if (this.data.smsCountdown > 0) return;

    try {
      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/sms/send`,
          method: 'POST',
          data: { phone },
          header: { 'Content-Type': 'application/json' },
          success: resolve,
          fail: reject,
        });
      });
      const data = res.data as any;
      if (res.statusCode === 200 && data && data.code === 0) {
        wx.showToast({ title: '验证码已发送', icon: 'success' });
        this.startCountdown();
      } else {
        wx.showToast({ title: (data && data.msg) || '发送失败', icon: 'none' });
      }
    } catch {
      wx.showToast({ title: '发送失败', icon: 'none' });
    }
  },

  startCountdown() {
    this.setData({ smsCountdown: 60 });
    (this as any)._smsTimer = setInterval(() => {
      if (this.data.smsCountdown <= 1) {
        clearInterval((this as any)._smsTimer);
        this.setData({ smsCountdown: 0 });
      } else {
        this.setData({ smsCountdown: this.data.smsCountdown - 1 });
      }
    }, 1000);
  },

  async handleBind() {
    if (this.data.loading) return;
    const phone = (this.data.phone || '').trim();
    const code = (this.data.smsCode || '').trim();
    if (phone.length !== 11 || !code) return;

    this.setData({ loading: true });
    try {
      const token = wx.getStorageSync('token');
      const res: any = await new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/phone/bind`,
          method: 'POST',
          data: { phone, code },
          header: { 'Content-Type': 'application/json', token },
          success: resolve,
          fail: reject,
        });
      });
      const data = res.data as any;
      if (res.statusCode === 200 && data && data.code === 0) {
        wx.showToast({ title: '绑定成功', icon: 'success' });
        setTimeout(() => wx.navigateBack(), 1500);
      } else {
        wx.showToast({ title: (data && data.msg) || '绑定失败', icon: 'none' });
      }
    } catch {
      wx.showToast({ title: '绑定失败', icon: 'none' });
    } finally {
      this.setData({ loading: false });
    }
  },

  onUnload() {
    if ((this as any)._smsTimer) {
      clearInterval((this as any)._smsTimer);
    }
  },
});
