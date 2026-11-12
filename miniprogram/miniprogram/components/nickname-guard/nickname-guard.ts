// components/nickname-guard/nickname-guard.ts
// 昵称完善提示：软模式可稍后填写（每日频控）；required 模式（登录即完善）必须填写完成才关闭。
// required 模式要求昵称和已保存头像，软模式保留头像选填。
// 用法：页面 wxml 放置 <nickname-guard id="nicknameGuard" bind:completed="onNicknameCompleted" />，
// 调用方通过 this.selectComponent('#nicknameGuard').show({ nickname, avatar, required }) 唤起。
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
import { blockUnsafeText } from '../../utils/promptReview';
import { hasCompleteProfile, hasProfileNickname, hasSavedAvatar } from './profile-validation';

const API_BASE_URL = 'https://api.jiadilingguang.com';
// 与「我的」页共用同一个标记：同一自然日内两处合计最多提醒一次；保存成功后组件会清除它。
const NICKNAME_PROMPT_DAY_KEY = 'nickname_prompt_day:';

Component({
  properties: {
    // 登录入口复用当前场景，仅将资料表单放入原来的底部卡片。
    embedded: { type: Boolean, value: false },
  },
  data: {
    visible: false,
    required: false,
    nickname: '',
    avatarUrl: '',
    avatarTemp: '',
    uploading: false,
    saving: false,
    promptUserID: 0,
    promptToken: '',
  },

  methods: {
    show(options: { nickname?: string; avatar?: string; userID?: number; required?: boolean } = {}) {
      this.setData({
        visible: true,
        required: !!options.required,
        promptUserID: Number(options.userID || wx.getStorageSync('userInfo')?.id || 0),
        promptToken: wx.getStorageSync('token') || '',
        nickname: String(options.nickname || ''),
        avatarUrl: String(options.avatar || ''),
        avatarTemp: '',
        uploading: false,
        saving: false,
      });
    },

    // 已登录用户的昵称补检：昵称为空时弹窗。软模式每个自然日最多提醒一次；
    // required 模式（登录即完善）不受每日频控约束，直到填写完成才放行。
    // 请求失败一律放行，且不占用当天的提醒次数，避免网络问题挡住用户。
    async checkAndShow(options: { required?: boolean } = {}): Promise<boolean> {
      if (this.data.visible) {
        return false;
      }
      const apiPath = '/api/v1/miniprogram/profile';
      const headers = this.buildAuthHeaders(apiPath);
      if (!headers) {
        return false;
      }
      const tokenAtStart = wx.getStorageSync('token');
      try {
        const res = await new Promise<any>((resolve, reject) => {
          wx.request({ url: `${API_BASE_URL}${apiPath}`, method: 'GET', header: headers, timeout: 15000, success: resolve, fail: reject });
        });
        const body = res.data || {};
        if (tokenAtStart !== wx.getStorageSync('token') || res.statusCode !== 200 || body.code !== 0) {
          return false;
        }
        return this.showIfMissing(body.data || {}, !!options.required);
      } catch (error) {
        console.error('昵称补检失败，放行:', error);
        return false;
      }
    },

    showIfMissing(profile: any, required = false) {
      const nickname = String(profile?.nickname || '').trim();
      const complete = hasCompleteProfile(profile);
      if (this.data.visible || (required ? complete : hasProfileNickname(nickname))) return false;
      const userID = Number(profile?.id || wx.getStorageSync('userInfo')?.id || 0);
      const now = new Date();
      const day = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;
      if (!required && userID && wx.getStorageSync(NICKNAME_PROMPT_DAY_KEY + userID) === day) return false;
      this.show({ userID, nickname, avatar: String(profile?.avatar || ''), required });
      return true;
    },

    onSkip() {
      if (this.data.saving || this.data.required) return;
      const now = new Date();
      const day = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;
      // Only an explicit postponement limits reminders, never merely showing one.
      if (this.data.promptUserID) wx.setStorageSync(NICKNAME_PROMPT_DAY_KEY + this.data.promptUserID, day);
      this.setData({ visible: false });
      this.triggerEvent('skipped');
    },

    stopPropagation() {},

    // 微信官方头像填写能力：e.detail.avatarUrl 为微信头像临时文件
    onChooseAvatar(e: WechatMiniprogram.CustomEvent<{ avatarUrl: string }>) {
      if (this.data.uploading || this.data.saving) {
        return;
      }
      const tempFilePath = e?.detail?.avatarUrl;
      if (!tempFilePath) {
        return;
      }
      this.setData({ avatarTemp: tempFilePath });
      void this.uploadAvatar(tempFilePath);
    },

    assertCurrentAccount(token: string, userID: number) {
      const currentID = Number(wx.getStorageSync('userInfo')?.id || 0);
      if (!token || token !== wx.getStorageSync('token') || (this.data.promptToken && this.data.promptToken !== token) || (userID && currentID && userID !== currentID)) {
        throw new Error('账号已变化，请重新确认资料');
      }
    },

    async readSavedProfile(token: string, userID: number) {
      this.assertCurrentAccount(token, userID);
      const path = '/api/v1/miniprogram/profile';
      const headers = this.buildAuthHeaders(path);
      if (!headers) throw new Error('请重新登录');
      const res: any = await new Promise((resolve, reject) => {
        wx.request({ url: `${API_BASE_URL}${path}`, method: 'GET', header: headers, timeout: 15000, success: resolve, fail: reject, __profileAccessResponseHandled: true } as any);
      });
      this.assertCurrentAccount(token, userID);
      if (res.statusCode === 401 || res.data?.code === 401) this.triggerEvent('authrequired');
      if (res.statusCode !== 200 || res.data?.code !== 0) throw new Error(res.data?.msg || '资料保存结果未确认，请重试');
      const profile = res.data.data || {};
      const id = Number(profile.id);
      if (!Number.isSafeInteger(id) || id <= 0 || (userID && userID !== id)) throw new Error('账号资料不匹配');
      return profile;
    },

    async uploadAvatar(tempFilePath: string) {
      const token = wx.getStorageSync('token');
      const userID = Number(this.data.promptUserID || wx.getStorageSync('userInfo')?.id || 0);
      this.setData({ uploading: true });
      try {
        this.assertCurrentAccount(token, userID);
        const fileUrl = await this.uploadAvatarImage(tempFilePath);
        this.assertCurrentAccount(token, userID);
        if (!hasSavedAvatar(fileUrl)) throw new Error('头像上传结果无效，请重新选择');
        await this.saveAvatar(fileUrl);
        this.assertCurrentAccount(token, userID);
        const profile = await this.readSavedProfile(token, userID);
        if (String(profile.avatar || '').trim() !== fileUrl) throw new Error('头像保存结果未确认，请重试');
        wx.setStorageSync('userInfo', { ...(wx.getStorageSync('userInfo') || {}), avatar: fileUrl });
        this.setData({ avatarUrl: fileUrl, avatarTemp: '' });
      } catch (error: any) {
        console.error('头像上传失败:', error);
        wx.showToast({ title: error.message || '头像上传失败，请重试', icon: 'none' });
      } finally {
        if (token === wx.getStorageSync('token')) this.setData({ uploading: false, avatarTemp: '' });
      }
    },

    uploadAvatarImage(tempFilePath: string): Promise<string> {
      const token = wx.getStorageSync('token');
      if (!token) {
        return Promise.reject(new Error('请先登录'));
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
          timeout: 15000,
          success: (res) => {
            if (res.statusCode !== 200) {
              if (res.statusCode === 401 && token === wx.getStorageSync('token')) this.triggerEvent('authrequired');
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

    saveAvatar(avatarUrl: string): Promise<void> {
      const token = wx.getStorageSync('token');
      const userID = Number(this.data.promptUserID || wx.getStorageSync('userInfo')?.id || 0);
      this.assertCurrentAccount(token, userID);
      const apiPath = '/api/v1/miniprogram/profile/avatar';
      const body = { avatar: avatarUrl };
      const headers = this.buildAuthHeaders(apiPath, body);
      if (!headers) {
        return Promise.reject(new Error('生成请求头失败'));
      }
      return new Promise((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'PUT',
          header: headers,
          data: body,
          timeout: 15000,
          success: (res: any) => {
            try { this.assertCurrentAccount(token, userID); } catch (error) { reject(error); return; }
            if (res?.statusCode === 200 && res.data?.code === 0) {
              resolve();
              return;
            }
            if (res?.statusCode === 401 || res?.data?.code === 401) this.triggerEvent('authrequired');
            reject(new Error(res.data?.msg || '头像保存失败'));
          },
          fail: (err: any) => reject(err),
        });
      });
    },

    onNicknameInput(e: WechatMiniprogram.CustomEvent<{ value: string }>) {
      this.setData({ nickname: e.detail.value });
    },

    async onConfirm(e: WechatMiniprogram.CustomEvent<{ value: { nickname: string } }>) {
      if (this.data.saving) {
        return;
      }
      // 微信昵称点选和安全检测后的最终值以表单为准，不能依赖 input 事件缓存。
      const nickname = String(e.detail.value.nickname || '').trim();
      if (!nickname) {
        wx.showToast({ title: '请填写昵称', icon: 'none' });
        return;
      }
      if (Array.from(nickname).length > 32) {
        wx.showToast({ title: '昵称最多 32 个字', icon: 'none' });
        return;
      }
      if (this.data.uploading) {
        wx.showToast({ title: '头像上传中，请稍候', icon: 'none' });
        return;
      }
      if (this.data.required && !hasSavedAvatar(this.data.avatarUrl)) {
        wx.showToast({ title: '请选择并保存头像后继续', icon: 'none' });
        return;
      }
      if (blockUnsafeText(nickname)) {
        return;
      }

      const tokenAtStart = wx.getStorageSync('token');
      const userID = Number(this.data.promptUserID || wx.getStorageSync('userInfo')?.id || 0);
      const avatarAtStart = this.data.avatarUrl;
      this.setData({ saving: true });
      try {
        this.assertCurrentAccount(tokenAtStart, userID);
        const apiPath = '/api/v1/miniprogram/profile/nickname';
        const body = { nickname };
        const headers = this.buildAuthHeaders(apiPath, body);
        if (!headers) {
          throw new Error('生成请求头失败');
        }
        const res: any = await new Promise((resolve, reject) => {
          wx.request({
            url: `${API_BASE_URL}${apiPath}`,
            method: 'PUT',
            header: headers,
            data: body,
            timeout: 15000,
            success: resolve,
            fail: reject,
          });
        });
        this.assertCurrentAccount(tokenAtStart, userID);
        if (res.statusCode !== 200 || res.data?.code !== 0) {
          if (res.statusCode === 401 || res.data?.code === 401) this.triggerEvent('authrequired');
          throw new Error(res.data?.msg || '保存失败');
        }
        const profile = await this.readSavedProfile(tokenAtStart, userID);
        if (String(profile.nickname || '').trim() !== nickname) throw new Error('昵称保存结果未确认，请重试');
        if (this.data.required && (!hasCompleteProfile(profile) || String(profile.avatar).trim() !== avatarAtStart)) {
          throw new Error('头像保存结果未确认，请重新选择头像');
        }
        const cachedUserInfo = wx.getStorageSync('userInfo') || {};
        wx.setStorageSync('userInfo', { ...cachedUserInfo, id: Number(profile.id), nickname, avatar: String(profile.avatar || '') });
        wx.removeStorageSync(NICKNAME_PROMPT_DAY_KEY + profile.id);
        this.setData({ visible: false, saving: false });
        this.triggerEvent('completed', { nickname, avatar: profile.avatar, token: tokenAtStart, profile });
      } catch (error: any) {
        console.error('保存昵称失败:', error);
        wx.showToast({ title: error.message || '保存失败', icon: 'none' });
        if (tokenAtStart === wx.getStorageSync('token')) this.setData({ saving: false });
      }
    },

    // 软模式可稍后继续；required 模式必须完成昵称和头像后才能关闭。
    onMaskTap() {
      wx.showToast({ title: this.data.required ? '请完善昵称和头像后继续' : '可选择稍后完善，继续浏览', icon: 'none' });
    },

    buildAuthHeaders(apiPath: string, body: any = {}) {
      const token = wx.getStorageSync('token');
      if (!token) {
        return null;
      }
      const params = generateRequestParams(token, body, apiPath, getCachedDeviceFingerprint() || '');
      return {
        ...paramsToHeaders(params),
        'Content-Type': 'application/json',
      };
    },
  },
});
