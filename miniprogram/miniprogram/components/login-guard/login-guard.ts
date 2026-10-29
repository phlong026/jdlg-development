// components/login-guard/login-guard.ts
// 分享内容门禁：每次以服务器资料确认有效登录，昵称和头像均已保存后才放行。
import { generateDeviceFingerprint, getCachedDeviceFingerprint, cacheDeviceFingerprint } from '../../utils/deviceFingerprint';

import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { hasCompleteProfile } from '../nickname-guard/profile-validation';

const API_BASE_URL = 'https://api.jiadilingguang.com';

Component({
  data: {
    visible: true,
    checking: true,
    needsLogin: false,
    completingProfile: false,
    error: '',
    agreedPrivacy: false,
    loggingIn: false,
    title: '登录后查看完整内容',
    desc: '登录并完善昵称、头像后即可查看，无需填写手机号',
  },

  lifetimes: {
    detached() {
      (this as any)._detached = true;
      (this as any)._checkRevision = ((this as any)._checkRevision || 0) + 1;
    },
  },

  methods: {
    async check(): Promise<boolean> {
      const revision = ((this as any)._checkRevision || 0) + 1;
      (this as any)._checkRevision = revision;
      const token = wx.getStorageSync('token');
      const accountID = Number(wx.getStorageSync('userInfo')?.id || 0);
      this.setData({ visible: true, checking: !!token, needsLogin: !token, completingProfile: false, error: '' });
      this.triggerEvent('gated');
      const editor = this.selectComponent('#nicknameGuard') as any;
      if (!token) {
        editor?.setData({ visible: false });
        return false;
      }
      const current = () => (this as any)._checkRevision === revision && wx.getStorageSync('token') === token;
      try {
        const path = '/api/v1/miniprogram/profile';
        const headers = paramsToHeaders(generateRequestParams(token, {}, path, getCachedDeviceFingerprint() || ''));
        const res: any = await new Promise((resolve, reject) => {
          wx.request({ url: `${API_BASE_URL}${path}`, method: 'GET', header: headers, timeout: 15000, success: resolve, fail: reject });
        });
        if (!current()) return false;
        if (res.statusCode === 401 || res.data?.code === 401) {
          wx.removeStorageSync('token');
          wx.removeStorageSync('userInfo');
          editor?.setData({ visible: false });
          this.setData({ checking: false, needsLogin: true, error: '登录已过期，请重新登录' });
          return false;
        }
        if (res.statusCode !== 200 || res.data?.code !== 0) throw new Error(res.data?.msg || '资料读取失败，请重试');
        const profile = res.data.data || {};
        const id = Number(profile.id);
        if (!Number.isSafeInteger(id) || id <= 0 || (accountID && accountID !== id)) throw new Error('账号资料不匹配，请重新登录');
        if (!hasCompleteProfile(profile)) {
          if (!editor || typeof editor.show !== 'function') throw new Error('资料填写暂不可用，请重试');
          this.setData({ checking: false, completingProfile: true });
          // 同一账号正在填写时保留尚未提交的输入和上传状态。
          if (!editor.data?.visible || editor.data.promptToken !== token) {
            editor.show({ required: true, userID: id, nickname: String(profile.nickname || ''), avatar: String(profile.avatar || '') });
          }
          return false;
        }
        const verified = { ...profile, id, nickname: String(profile.nickname).trim(), avatar: String(profile.avatar).trim() };
        wx.setStorageSync('userInfo', { ...(wx.getStorageSync('userInfo') || {}), ...verified, token });
        editor?.setData({ visible: false });
        this.setData({ visible: false, checking: false, completingProfile: false });
        this.triggerEvent('authenticated', { token, profile: verified });
        return true;
      } catch (error: any) {
        if (current()) {
          console.error('分享资料核验失败:', error);
          this.setData({ checking: false, error: error.message || '资料读取失败，请重试' });
        }
        return false;
      }
    },

    onRetry() { return this.check(); },
    onProfileCompleted() { return this.check(); },
    onProfileAuthRequired() { return this.check(); },

    onTogglePrivacy() {
      this.setData({ agreedPrivacy: !this.data.agreedPrivacy });
    },

    onOpenPrivacy() {
      wx.navigateTo({ url: '/pages/privacy/privacy', fail: () => {} });
    },

    async onWxLogin() {
      if (this.data.loggingIn || (this as any)._detached) return;
      if (!this.data.agreedPrivacy) {
        wx.showToast({ title: '请先阅读并同意用户协议和隐私政策', icon: 'none', duration: 2500 });
        return;
      }
      const tokenAtStart = wx.getStorageSync('token');
      const current = () => !(this as any)._detached && tokenAtStart === wx.getStorageSync('token');
      this.setData({ loggingIn: true, error: '' });
      try {
        // 与登录页同一登录通道：wx.login 静默换取 code，后端按 openid 建号或登录。
        const loginRes = await new Promise<WechatMiniprogram.LoginSuccessCallbackResult>((resolve, reject) => {
          wx.login({ success: resolve, fail: reject });
        });
        if (!current()) return;
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
        if (!current()) return;
        const res: any = await new Promise((resolve, reject) => {
          wx.request({
            url: `${API_BASE_URL}/api/v1/miniprogram/login`,
            method: 'POST',
            data: { code: loginRes.code, device_id: deviceID || undefined },
            header: { 'Content-Type': 'application/json' },
            timeout: 15000,
            success: resolve,
            fail: reject,
          });
        });
        if (!current()) return;
        const body = res.data || {};
        if (res.statusCode !== 200 || body.code !== 0 || !body.data?.token) {
          throw new Error(body.msg || '登录失败');
        }
        wx.setStorageSync('token', body.data.token);
        wx.setStorageSync('userInfo', { ...body.data, token: body.data.token });
        await this.check();
      } catch (error: any) {
        if (!current()) return;
        console.error('登录门禁登录失败:', error);
        wx.showToast({ title: error.message || '登录失败，请重试', icon: 'none' });
      } finally {
        if (!(this as any)._detached) this.setData({ loggingIn: false });
      }
    },

    stopPropagation() {},
  },
});
