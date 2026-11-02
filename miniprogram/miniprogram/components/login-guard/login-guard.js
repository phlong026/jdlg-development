"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// components/login-guard/login-guard.ts
// 分享内容门禁：每次以服务器资料确认有效登录，昵称和头像均已保存后才放行。
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const parameter_1 = require("../../utils/parameter");
const profile_validation_1 = require("../nickname-guard/profile-validation");
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
        optimistic: false,
        title: '登录后查看完整内容',
        desc: '登录并完善昵称、头像后即可查看，无需填写手机号',
    },
    lifetimes: {
        detached() {
            this._detached = true;
            this._checkRevision = (this._checkRevision || 0) + 1;
        },
    },
    methods: {
        async check(options) {
            var _a, _b, _c, _d;
            const silent = !!(options && options.silent);
            const revision = (this._checkRevision || 0) + 1;
            this._checkRevision = revision;
            const token = wx.getStorageSync('token');
            // 缓存只用于预填资料，不能替代服务器确认；否则旧包或换号缓存会先加载内容并写入访问记录。
            // silent 供已放行页面的后台重验使用：不弹核验面板、不提前上锁，仅在真正失败时 gated。
            this.setData({ visible: false, checking: !silent && !!token, needsLogin: !token, completingProfile: false, error: '' });
            const editor = this.selectComponent('#nicknameGuard');
            if (!token) {
                editor === null || editor === void 0 ? void 0 : editor.setData({ visible: false });
                this.triggerEvent('gated');
                this.setData({ visible: true, checking: false });
                return false;
            }
            const current = () => this._checkRevision === revision && wx.getStorageSync('token') === token;
            try {
                const path = '/api/v1/miniprogram/profile';
                const headers = (0, parameter_1.paramsToHeaders)((0, parameter_1.generateRequestParams)(token, {}, path, (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || ''));
                const res = await new Promise((resolve, reject) => {
                    wx.request({ url: `${API_BASE_URL}${path}`, method: 'GET', header: headers, timeout: 15000, success: resolve, fail: reject, __profileAccessResponseHandled: true });
                });
                if (!current())
                    return false;
                if (res.statusCode === 401 || ((_a = res.data) === null || _a === void 0 ? void 0 : _a.code) === 401) {
                    wx.removeStorageSync('token');
                    wx.removeStorageSync('userInfo');
                    editor === null || editor === void 0 ? void 0 : editor.setData({ visible: false });
                    this.triggerEvent('gated');
                    this.setData({ visible: true, checking: false, needsLogin: true, error: '登录已过期，请重新登录' });
                    return false;
                }
                if (res.statusCode !== 200 || ((_b = res.data) === null || _b === void 0 ? void 0 : _b.code) !== 0)
                    throw new Error(((_c = res.data) === null || _c === void 0 ? void 0 : _c.msg) || '资料读取失败，请重试');
                const profile = res.data.data || {};
                const id = Number(profile.id);
                if (!Number.isSafeInteger(id) || id <= 0) {
                    wx.removeStorageSync('token');
                    wx.removeStorageSync('userInfo');
                    editor === null || editor === void 0 ? void 0 : editor.setData({ visible: false });
                    this.triggerEvent('gated');
                    this.setData({ visible: true, checking: false, needsLogin: true, error: '账号资料无法确认，请重新登录' });
                    return false;
                }
                const cachedProfile = wx.getStorageSync('userInfo') || {};
                const sameAccount = Number(cachedProfile.id || 0) === id;
                wx.setStorageSync('userInfo', { ...(sameAccount ? cachedProfile : {}), ...profile, id, token });
                if (!(0, profile_validation_1.hasCompleteProfile)(profile)) {
                    if (!editor || typeof editor.show !== 'function')
                        throw new Error('资料填写暂不可用，请重试');
                    this.setData({ visible: false, checking: false, completingProfile: true });
                    this.triggerEvent('gated');
                    // 同一账号正在填写时保留尚未提交的输入和上传状态。
                    if (!((_d = editor.data) === null || _d === void 0 ? void 0 : _d.visible) || editor.data.promptToken !== token) {
                        editor.show({ required: true, userID: id, nickname: String(profile.nickname || ''), avatar: String(profile.avatar || '') });
                    }
                    return false;
                }
                const verified = { ...profile, id, nickname: String(profile.nickname).trim(), avatar: String(profile.avatar).trim() };
                wx.setStorageSync('userInfo', { ...(wx.getStorageSync('userInfo') || {}), ...verified, token });
                editor === null || editor === void 0 ? void 0 : editor.setData({ visible: false });
                this.setData({ visible: false, checking: false, completingProfile: false });
                this.triggerEvent('authenticated', { token, profile: verified });
                return true;
            }
            catch (error) {
                if (current()) {
                    console.error('分享资料核验失败:', error);
                    this.triggerEvent('gated');
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
            wx.navigateTo({ url: '/pages/privacy/privacy', fail: () => { } });
        },
        async onWxLogin() {
            var _a;
            if (this.data.loggingIn || this._detached)
                return;
            if (!this.data.agreedPrivacy) {
                wx.showToast({ title: '请先阅读并同意用户协议和隐私政策', icon: 'none', duration: 2500 });
                return;
            }
            const tokenAtStart = wx.getStorageSync('token');
            const current = () => !this._detached && tokenAtStart === wx.getStorageSync('token');
            this.setData({ loggingIn: true, error: '' });
            try {
                // 与登录页同一登录通道：wx.login 静默换取 code，后端按 openid 建号或登录。
                const loginRes = await new Promise((resolve, reject) => {
                    wx.login({ success: resolve, fail: reject });
                });
                if (!current())
                    return;
                if (!loginRes.code)
                    throw new Error('获取登录code失败');
                let deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)();
                if (!deviceID) {
                    try {
                        deviceID = await (0, deviceFingerprint_1.generateDeviceFingerprint)();
                        if (deviceID)
                            (0, deviceFingerprint_1.cacheDeviceFingerprint)(deviceID);
                    }
                    catch (error) {
                        console.error('生成设备指纹异常:', error);
                    }
                }
                if (!current())
                    return;
                const res = await new Promise((resolve, reject) => {
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
                if (!current())
                    return;
                const body = res.data || {};
                if (res.statusCode !== 200 || body.code !== 0 || !((_a = body.data) === null || _a === void 0 ? void 0 : _a.token)) {
                    throw new Error(body.msg || '登录失败');
                }
                wx.setStorageSync('token', body.data.token);
                wx.setStorageSync('userInfo', { ...body.data, token: body.data.token });
                await this.check();
            }
            catch (error) {
                if (!current())
                    return;
                console.error('登录门禁登录失败:', error);
                wx.showToast({ title: error.message || '登录失败，请重试', icon: 'none' });
            }
            finally {
                if (!this._detached)
                    this.setData({ loggingIn: false });
            }
        },
        stopPropagation() { },
    },
});
