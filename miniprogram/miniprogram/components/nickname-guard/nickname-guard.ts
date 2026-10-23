// components/nickname-guard/nickname-guard.ts
// 昵称完善提示：用户可稍后填写；保存时昵称必填，头像选填。
// 用法：页面 wxml 放置 <nickname-guard id="nicknameGuard" bind:completed="onNicknameCompleted" />，
// 调用方通过 this.selectComponent('#nicknameGuard').show({ nickname, avatar }) 唤起。
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
import { blockUnsafeText } from '../../utils/promptReview';

const API_BASE_URL = 'https://api.jiadilingguang.com';
// 与「我的」页共用同一个标记：同一自然日内两处合计最多提醒一次；保存成功后组件会清除它。
const NICKNAME_PROMPT_DAY_KEY = 'nickname_prompt_day:';

Component({
  data: {
    visible: false,
    nickname: '',
    avatarUrl: '',
    avatarTemp: '',
    uploading: false,
    saving: false,
    promptUserID: 0,
  },

  methods: {
    show(options: { nickname?: string; avatar?: string; userID?: number } = {}) {
      this.setData({
        visible: true,
        promptUserID: Number(options.userID || wx.getStorageSync('userInfo')?.id || 0),
        nickname: String(options.nickname || ''),
        avatarUrl: String(options.avatar || ''),
        avatarTemp: '',
        uploading: false,
        saving: false,
      });
    },

    // 已登录用户的昵称补检：昵称为空时弹窗，每个自然日最多提醒一次。
    // 请求失败一律放行，且不占用当天的提醒次数，避免网络问题挡住用户。
    async checkAndShow(): Promise<boolean> {
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
          wx.request({ url: `${API_BASE_URL}${apiPath}`, method: 'GET', header: headers, success: resolve, fail: reject });
        });
        const body = res.data || {};
        if (tokenAtStart !== wx.getStorageSync('token') || res.statusCode !== 200 || body.code !== 0) {
          return false;
        }
        return this.showIfMissing(body.data || {});
      } catch (error) {
        console.error('昵称补检失败，放行:', error);
        return false;
      }
    },

    showIfMissing(profile: any) {
      if (this.data.visible || String(profile?.nickname || '').trim()) return false;
      const userID = Number(profile?.id || wx.getStorageSync('userInfo')?.id || 0);
      const now = new Date();
      const day = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;
      if (userID && wx.getStorageSync(NICKNAME_PROMPT_DAY_KEY + userID) === day) return false;
      this.show({ userID, avatar: String(profile?.avatar || '') });
      return true;
    },

    onSkip() {
      if (this.data.saving) return;
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

    async uploadAvatar(tempFilePath: string) {
      this.setData({ uploading: true });
      try {
        const fileUrl = await this.uploadAvatarImage(tempFilePath);
        await this.saveAvatar(fileUrl);
        this.setData({ avatarUrl: fileUrl, avatarTemp: '' });
      } catch (error: any) {
        console.error('头像上传失败:', error);
        wx.showToast({ title: error.message || '头像上传失败，可稍后在资料页重试', icon: 'none' });
      } finally {
        this.setData({ uploading: false, avatarTemp: '' });
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

    saveAvatar(avatarUrl: string): Promise<void> {
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
          success: (res: any) => {
            if (res.statusCode === 200 && res.data.code === 0) {
              resolve();
              return;
            }
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
      if (blockUnsafeText(nickname)) {
        return;
      }

      const tokenAtStart = wx.getStorageSync('token');
      this.setData({ saving: true });
      try {
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
            success: resolve,
            fail: reject,
          });
        });
        if (res.statusCode !== 200 || res.data.code !== 0) {
          throw new Error(res.data?.msg || '保存失败');
        }
        const profilePath = '/api/v1/miniprogram/profile';
        const readback: any = await new Promise((resolve, reject) => {
          wx.request({ url: `${API_BASE_URL}${profilePath}`, method: 'GET',
            header: this.buildAuthHeaders(profilePath) || {}, success: resolve, fail: reject });
        });
        if (tokenAtStart !== wx.getStorageSync('token')) throw new Error('账号已变化，请重新确认资料');
        if (readback.statusCode !== 200 || readback.data?.code !== 0 || String(readback.data?.data?.nickname || '').trim() !== nickname) {
          throw new Error('昵称保存结果未确认，请重试');
        }
        const cachedUserInfo = wx.getStorageSync('userInfo') || {};
        if (readback.data.data.id && cachedUserInfo.id && readback.data.data.id !== cachedUserInfo.id) throw new Error('账号资料不匹配');
        wx.setStorageSync('userInfo', { ...cachedUserInfo, nickname });
        if (cachedUserInfo.id) wx.removeStorageSync(NICKNAME_PROMPT_DAY_KEY + cachedUserInfo.id);
        this.setData({ visible: false, saving: false });
        this.triggerEvent('completed', { nickname });
      } catch (error: any) {
        console.error('保存昵称失败:', error);
        wx.showToast({ title: error.message || '保存失败', icon: 'none' });
        this.setData({ saving: false });
      }
    },

    // 资料填写可以稍后继续，不要求为浏览公开页面披露昵称。
    onMaskTap() {
      wx.showToast({ title: '可选择稍后完善，继续浏览', icon: 'none' });
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
