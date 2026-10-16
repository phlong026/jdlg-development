// components/nickname-guard/nickname-guard.ts
// 登录后强制完善昵称弹窗：昵称必填（微信键盘一键预填），头像选填（open-type=chooseAvatar 一键取微信头像）。
// 用法：页面 wxml 放置 <nickname-guard id="nicknameGuard" bind:completed="onNicknameCompleted" />，
// 调用方通过 this.selectComponent('#nicknameGuard').show({ nickname, avatar }) 唤起。
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
import { blockUnsafeText } from '../../utils/promptReview';

const API_BASE_URL = 'https://api.jiadilingguang.com';

Component({
  data: {
    visible: false,
    nickname: '',
    avatarUrl: '',
    avatarTemp: '',
    uploading: false,
    saving: false,
  },

  methods: {
    show(options: { nickname?: string; avatar?: string } = {}) {
      this.setData({
        visible: true,
        nickname: String(options.nickname || ''),
        avatarUrl: String(options.avatar || ''),
        avatarTemp: '',
        uploading: false,
        saving: false,
      });
    },

    stopPropagation() {},

    // 微信官方头像填写能力：e.detail.avatarUrl 为微信头像临时文件
    onChooseAvatar(e: WechatMiniprogram.CustomEvent<{ avatarUrl: string }>) {
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
        this.setData({ uploading: false });
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

    async onConfirm() {
      if (this.data.saving) {
        return;
      }
      const nickname = String(this.data.nickname || '').trim();
      if (!nickname) {
        wx.showToast({ title: '请填写昵称', icon: 'none' });
        return;
      }
      if (nickname.length > 32) {
        wx.showToast({ title: '昵称最多 32 个字', icon: 'none' });
        return;
      }
      if (this.data.avatarTemp) {
        wx.showToast({ title: '头像上传中，请稍候', icon: 'none' });
        return;
      }
      if (blockUnsafeText(nickname)) {
        return;
      }

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
        const cachedUserInfo = wx.getStorageSync('userInfo') || {};
        wx.setStorageSync('userInfo', { ...cachedUserInfo, nickname });
        // 已完善资料，清除「我的」页当日补检标记
        wx.removeStorageSync('nickname_prompt_day');
        this.setData({ visible: false, saving: false });
        this.triggerEvent('completed', { nickname });
      } catch (error: any) {
        console.error('保存昵称失败:', error);
        wx.showToast({ title: error.message || '保存失败', icon: 'none' });
        this.setData({ saving: false });
      }
    },

    // 强制完善：点遮罩不放行，只做提示（与登录页身份选择弹窗行为一致）
    onMaskTap() {
      wx.showToast({ title: '请完善昵称后继续', icon: 'none' });
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
