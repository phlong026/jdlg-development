// components/login-guard/login-guard.ts
// 分享内容门禁：每次以服务器资料确认有效登录，昵称和头像均已保存后才放行。
import { generateDeviceFingerprint, getCachedDeviceFingerprint, cacheDeviceFingerprint } from '../../utils/deviceFingerprint';

import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { hasCompleteProfile } from '../nickname-guard/profile-validation';
// 宿主页是否公开浏览页（决定"暂不登录"的导航行为），require 避免 TS 循环依赖。
const { isPublicBrowseRoute } = require('../../utils/profile-access');

const API_BASE_URL = 'https://api.jiadilingguang.com';

Component({
  data: {
    // 初始隐藏：公开浏览页乐观放行后不会调用 check()，组件必须默认不盖
    // 浮层（否则"正在核验登录资料"常驻首页，公开页等于没放开）。check()
    // 的每个需要展示的分支（未登录/过期/资料不全/失败）都显式设置 visible。
    visible: false,
    checking: false,
    needsLogin: false,
    completingProfile: false,
    error: '',
    agreedPrivacy: false,
    loggingIn: false,
    optimistic: false,
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
    // 用户拒绝登录（"暂不登录"/关闭按钮）。微信审核要求登录环节提供显著
    // 的可取消/拒绝或返回按钮：公开浏览页仅关闭浮层继续浏览；身份功能页
    // 返回上一页（无上一页则回首页），拒绝登录不等于被留在空白页。
    onDismiss() {
      (this as any)._dismissed = true;
      this.setData({ visible: false, checking: false, loggingIn: false });
      this.triggerEvent('dismissed');
      try {
        const pages = getCurrentPages();
        const current = pages[pages.length - 1];
        const route = current ? String(current.route || '') : '';
        if (!isPublicBrowseRoute(route)) {
          if (pages.length > 1) wx.navigateBack({ delta: 1 });
          else wx.switchTab({ url: '/pages/index/index' });
        }
      } catch (e) { /* 导航失败时保持当前页，浮层已关闭 */ }
    },
    async check(options?: { silent?: boolean; anonymous?: boolean }): Promise<boolean> {
      const silent = !!(options && options.silent);
      const revision = ((this as any)._checkRevision || 0) + 1;
      (this as any)._checkRevision = revision;
      const token = wx.getStorageSync('token');
      // 曾拒绝登录且为静默核验失败（切后台恢复等自动触发）：不再弹浮层
      // （审核"不得反复弹窗"），直接按拒绝处理返回，等用户主动再触发。
      const deniedAgain = () => {
        if (silent && (this as any)._dismissed) { this.onDismiss(); return true; }
        return false;
      };
      // 缓存只用于预填资料，不能替代服务器确认；否则旧包或换号缓存会先加载内容并写入访问记录。
      // silent 供已放行页面的后台重验使用：不弹核验面板、不提前上锁，仅在真正失败时 gated。
      this.setData({ visible: false, checking: !silent && !!token, needsLogin: !token, completingProfile: false, error: '' });
      const editor = this.selectComponent('#nicknameGuard') as any;
      if (!token) {
        editor?.setData({ visible: false });
        // 匿名说明态（分享页未登录）：页面本体负责解释与去登录，这里不得弹登录浮层。
        if (options?.anonymous === true) {
          this.setData({ visible: false, checking: false });
          return false;
        }
        this.setData({ visible: true, checking: false });
        this.triggerEvent('gated');
        if (deniedAgain()) return false;
        return false;
      }
      const current = () => (this as any)._checkRevision === revision && wx.getStorageSync('token') === token;
      try {
        const path = '/api/v1/miniprogram/profile';
        const headers = paramsToHeaders(generateRequestParams(token, {}, path, getCachedDeviceFingerprint() || ''));
        const res: any = await new Promise((resolve, reject) => {
          wx.request({ url: `${API_BASE_URL}${path}`, method: 'GET', header: headers, timeout: 15000, success: resolve, fail: reject, __profileAccessResponseHandled: true } as any);
        });
        if (!current()) return false;
        if (res.statusCode === 401 || res.data?.code === 401) {
          wx.removeStorageSync('token');
          wx.removeStorageSync('userInfo');
          editor?.setData({ visible: false });
          // 先写浮层状态再抛 gated：分享页匿名降级会在 gated 处理里隐藏浮层，反向顺序会重开浮层。
          this.setData({ visible: true, checking: false, needsLogin: true, error: '登录已过期，请重新登录' });
          this.triggerEvent('gated');
          if (deniedAgain()) return false;
          return false;
        }
        if (res.statusCode !== 200 || res.data?.code !== 0) throw new Error(res.data?.msg || '资料读取失败，请重试');
        const profile = res.data.data || {};
        const id = Number(profile.id);
        if (!Number.isSafeInteger(id) || id <= 0) {
          wx.removeStorageSync('token');
          wx.removeStorageSync('userInfo');
          editor?.setData({ visible: false });
          this.setData({ visible: true, checking: false, needsLogin: true, error: '账号资料无法确认，请重新登录' });
          this.triggerEvent('gated');
          if (deniedAgain()) return false;
          return false;
        }
        const cachedProfile = wx.getStorageSync('userInfo') || {};
        const sameAccount = Number(cachedProfile.id || 0) === id;
        wx.setStorageSync('userInfo', { ...(sameAccount ? cachedProfile : {}), ...profile, id, token });
        // 单篇分享和其它受保护页统一要求服务器已保存的昵称与头像。
        if (!hasCompleteProfile(profile)) {
          if (!editor || typeof editor.show !== 'function') throw new Error('资料填写暂不可用，请重试');
          this.setData({ visible: false, checking: false, completingProfile: true });
          this.triggerEvent('gated');
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
          this.triggerEvent('gated');
          if (deniedAgain()) return false;
          this.setData({ visible: true, checking: false, error: error.message || '资料读取失败，请重试' });
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
