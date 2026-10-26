// components/login-guard/login-guard.ts
// 登录门禁：分享出去的内容页（模板广场、公开测算报告）未登录时挡住内容，
// 用户勾选协议并点击微信一键登录后静默完成 wx.login 建号/登录；
// 页面在 loginsucceeded 事件里重新加载内容，昵称引导由 nickname-guard 接管。
import { generateDeviceFingerprint, getCachedDeviceFingerprint, cacheDeviceFingerprint } from '../../utils/deviceFingerprint';

const API_BASE_URL = 'https://api.jiadilingguang.com';

Component({
  data: {
    visible: false,
    agreedPrivacy: false,
    loggingIn: false,
    title: '登录后查看完整内容',
    desc: '微信一键登录后即可查看，无需填写手机号',
  },

  methods: {
    // 已登录返回 true 并透传 authenticated；未登录显示门禁并触发 gated，页面据此暂缓加载。
    check(): boolean {
      const authenticated = !!wx.getStorageSync('token');
      this.setData({ visible: !authenticated });
      this.triggerEvent(authenticated ? 'authenticated' : 'gated');
      return authenticated;
    },

    onTogglePrivacy() {
      this.setData({ agreedPrivacy: !this.data.agreedPrivacy });
    },

    onOpenPrivacy() {
      wx.navigateTo({ url: '/pages/privacy/privacy', fail: () => {} });
    },

    async onWxLogin() {
      if (this.data.loggingIn) return;
      if (!this.data.agreedPrivacy) {
        wx.showToast({ title: '请先阅读并同意用户协议和隐私政策', icon: 'none', duration: 2500 });
        return;
      }
      this.setData({ loggingIn: true });
      try {
        // 与登录页同一登录通道：wx.login 静默换取 code，后端按 openid 建号或登录。
        const loginRes = await new Promise<WechatMiniprogram.LoginSuccessCallbackResult>((resolve, reject) => {
          wx.login({ success: resolve, fail: reject });
        });
        if (!loginRes.code) throw new Error('获取登录code失败');
        let deviceID = getCachedDeviceFingerprint();
        if (!deviceID) {
          try {
            deviceID = await generateDeviceFingerprint();
            if (deviceID) cacheDeviceFingerprint(deviceID);
          } catch (error) {
            console.error('生成设备指纹异常:', error);
          }
        }
        const res: any = await new Promise((resolve, reject) => {
          wx.request({
            url: `${API_BASE_URL}/api/v1/miniprogram/login`,
            method: 'POST',
            data: { code: loginRes.code, device_id: deviceID || undefined },
            header: { 'Content-Type': 'application/json' },
            success: resolve,
            fail: reject,
          });
        });
        const body = res.data || {};
        if (res.statusCode !== 200 || body.code !== 0 || !body.data?.token) {
          throw new Error(body.msg || '登录失败');
        }
        wx.setStorageSync('token', body.data.token);
        wx.setStorageSync('userInfo', { ...body.data, token: body.data.token });
        this.setData({ visible: false });
        this.triggerEvent('loginsucceeded');
      } catch (error: any) {
        console.error('登录门禁登录失败:', error);
        wx.showToast({ title: error.message || '登录失败，请重试', icon: 'none' });
      } finally {
        this.setData({ loggingIn: false });
      }
    },

    stopPropagation() {},
  },
});
