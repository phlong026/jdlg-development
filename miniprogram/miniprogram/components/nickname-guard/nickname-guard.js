"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// components/nickname-guard/nickname-guard.ts
// 登录后强制完善昵称弹窗：昵称必填（微信键盘一键预填），头像选填（open-type=chooseAvatar 一键取微信头像）。
// 用法：页面 wxml 放置 <nickname-guard id="nicknameGuard" bind:completed="onNicknameCompleted" />，
// 调用方通过 this.selectComponent('#nicknameGuard').show({ nickname, avatar }) 唤起。
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const promptReview_1 = require("../../utils/promptReview");
const API_BASE_URL = 'https://api.jiadilingguang.com';
// 与「我的」页共用同一个标记：同一自然日内两处合计最多提醒一次；保存成功后组件会清除它。
const NICKNAME_PROMPT_DAY_KEY = 'nickname_prompt_day';
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
        show(options = {}) {
            this.setData({
                visible: true,
                nickname: String(options.nickname || ''),
                avatarUrl: String(options.avatar || ''),
                avatarTemp: '',
                uploading: false,
                saving: false,
            });
        },
        // 已登录用户的昵称补检：昵称为空时弹窗，每个自然日最多提醒一次。
        // 请求失败一律放行，且不占用当天的提醒次数，避免网络问题挡住用户。
        async checkAndShow() {
            if (this.data.visible) {
                return false;
            }
            const now = new Date();
            const dayKey = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;
            if (wx.getStorageSync(NICKNAME_PROMPT_DAY_KEY) === dayKey) {
                return false;
            }
            const apiPath = '/api/v1/miniprogram/profile';
            const headers = this.buildAuthHeaders(apiPath);
            if (!headers) {
                return false;
            }
            try {
                const res = await new Promise((resolve, reject) => {
                    wx.request({ url: `${API_BASE_URL}${apiPath}`, method: 'GET', header: headers, success: resolve, fail: reject });
                });
                const body = res.data || {};
                if (res.statusCode !== 200 || body.code !== 0) {
                    return false;
                }
                if (String(body.data?.nickname || '').trim()) {
                    return false;
                }
                wx.setStorageSync(NICKNAME_PROMPT_DAY_KEY, dayKey);
                this.show({ avatar: String(body.data?.avatar || '') });
                return true;
            }
            catch (error) {
                console.error('昵称补检失败，放行:', error);
                return false;
            }
        },
        stopPropagation() { },
        // 微信官方头像填写能力：e.detail.avatarUrl 为微信头像临时文件
        onChooseAvatar(e) {
            const tempFilePath = e?.detail?.avatarUrl;
            if (!tempFilePath) {
                return;
            }
            this.setData({ avatarTemp: tempFilePath });
            void this.uploadAvatar(tempFilePath);
        },
        async uploadAvatar(tempFilePath) {
            this.setData({ uploading: true });
            try {
                const fileUrl = await this.uploadAvatarImage(tempFilePath);
                await this.saveAvatar(fileUrl);
                this.setData({ avatarUrl: fileUrl, avatarTemp: '' });
            }
            catch (error) {
                console.error('头像上传失败:', error);
                wx.showToast({ title: error.message || '头像上传失败，可稍后在资料页重试', icon: 'none' });
            }
            finally {
                this.setData({ uploading: false });
            }
        },
        uploadAvatarImage(tempFilePath) {
            const token = wx.getStorageSync('token');
            if (!token) {
                return Promise.reject(new Error('请先登录'));
            }
            const apiPath = '/api/v1/miniprogram/ai/upload-reference-image';
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const params = (0, parameter_1.generateRequestParams)(token, '{}', apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
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
                            const data = JSON.parse(res.data);
                            if (data.code === 0 && data.data?.url) {
                                resolve(String(data.data.url));
                                return;
                            }
                            reject(new Error(data.msg || '头像上传失败'));
                        }
                        catch (error) {
                            reject(new Error('头像上传响应解析失败'));
                        }
                    },
                    fail: (err) => reject(err),
                });
            });
        },
        saveAvatar(avatarUrl) {
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
                    success: (res) => {
                        if (res.statusCode === 200 && res.data.code === 0) {
                            resolve();
                            return;
                        }
                        reject(new Error(res.data?.msg || '头像保存失败'));
                    },
                    fail: (err) => reject(err),
                });
            });
        },
        onNicknameInput(e) {
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
            if ((0, promptReview_1.blockUnsafeText)(nickname)) {
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
                const res = await new Promise((resolve, reject) => {
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
            }
            catch (error) {
                console.error('保存昵称失败:', error);
                wx.showToast({ title: error.message || '保存失败', icon: 'none' });
                this.setData({ saving: false });
            }
        },
        // 强制完善：点遮罩不放行，只做提示（与登录页身份选择弹窗行为一致）
        onMaskTap() {
            wx.showToast({ title: '请完善昵称后继续', icon: 'none' });
        },
        buildAuthHeaders(apiPath, body = {}) {
            const token = wx.getStorageSync('token');
            if (!token) {
                return null;
            }
            const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '');
            return {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
        },
    },
});
