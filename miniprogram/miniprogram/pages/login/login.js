"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/login/login.ts
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const parameter_1 = require("../../utils/parameter");
const asset_1 = require("../../utils/asset");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com'; // 根据实际情况修改
const LOGIN_PRIVACY_AGREEMENT_STORAGE_KEY = 'login_privacy_agreement';
const USER_IDENTITY_OPTIONS = ['乡墅业主', '设计师', '施工队', '建材商', '机构用户'];
Page({
    /**
     * 页面的初始数据
     */
    data: {
        loginPanel: '',
        loginPanelTitle: '',
        loginSheetVisible: false,
        logoImage: (0, asset_1.resolveAssetPath)('/assets/home/intro-logo.png'),
        navSafeTop: 0,
        navBarHeight: 88,
        navContentHeight: 44,
        navSideWidth: 88,
        username: '',
        password: '',
        phone: '',
        smsCode: '',
        smsCountdown: 0,
        loading: false,
        inviteCode: '', // 邀请码（从分享链接或上一页传入，注册时填写可得双方奖励）
        agreedPrivacy: false,
        identityPopupVisible: false,
        identityOptions: USER_IDENTITY_OPTIONS,
        selectedIdentity: '',
        pendingLoginResult: null,
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad(options) {
        this.initNavLayout();
        // 检查是否已登录
        const token = wx.getStorageSync('token');
        const agreedPrivacy = wx.getStorageSync(LOGIN_PRIVACY_AGREEMENT_STORAGE_KEY) === true;
        const username = options.username || '';
        if (token) {
            // 已登录，跳转到首页
            wx.switchTab({
                url: '/pages/index/index',
                fail: () => {
                    wx.reLaunch({
                        url: '/pages/index/index',
                    });
                }
            });
            return;
        }
        // 邀请码：优先从本页参数，否则从全局缓存（分享进入首页后跳转登录时带过来）
        const inviteCode = options.invite_code || wx.getStorageSync('pending_invite_code') || '';
        if (inviteCode) {
            wx.removeStorageSync('pending_invite_code');
        }
        if (options.type === 'password') {
            this.setData({
                username: username || this.data.username,
                inviteCode: inviteCode || this.data.inviteCode,
                agreedPrivacy,
            });
            this.openLoginSheet('password', '账号登录');
        }
        else {
            this.setData({
                username: username || this.data.username,
                inviteCode: inviteCode || this.data.inviteCode,
                agreedPrivacy,
            });
        }
    },
    initNavLayout() {
        try {
            const systemInfo = wx.getSystemInfoSync();
            const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
                ? wx.getMenuButtonBoundingClientRect()
                : null;
            const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
            if (menuRect && menuRect.bottom > 0) {
                const menuTop = Number(menuRect.top || safeTop);
                const menuHeight = Number(menuRect.height || 32);
                const contentHeight = Math.max(menuHeight, 36);
                const bottomGap = 8;
                this.setData({
                    navSafeTop: menuTop,
                    navBarHeight: Math.ceil(menuTop + contentHeight + bottomGap),
                    navContentHeight: contentHeight,
                    navSideWidth: Number(systemInfo.windowWidth - menuRect.left),
                });
                return;
            }
            this.setData({
                navSafeTop: safeTop,
                navBarHeight: safeTop + 52,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
        catch (_error) {
            this.setData({
                navSafeTop: 20,
                navBarHeight: 64,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
    },
    openLoginSheet(panel, title) {
        if (this._sheetCloseTimer) {
            clearTimeout(this._sheetCloseTimer);
            this._sheetCloseTimer = null;
        }
        this.setData({
            loginPanel: panel,
            loginPanelTitle: title,
            loginSheetVisible: false,
        });
        setTimeout(() => {
            if (this.data.loginPanel === panel) {
                this.setData({ loginSheetVisible: true });
            }
        }, 30);
    },
    openPhoneLogin() {
        if (!this.ensureAgreementAccepted()) {
            return;
        }
        this.openLoginSheet('phone', '手机登录');
    },
    openPasswordLogin() {
        if (!this.ensureAgreementAccepted()) {
            return;
        }
        this.openLoginSheet('password', '账号登录');
    },
    openEnterpriseLogin() {
        if (!this.ensureAgreementAccepted()) {
            return;
        }
        this.openLoginSheet('enterprise', '企业登录');
    },
    closeLoginPanel() {
        if (!this.data.loginPanel) {
            return;
        }
        this.setData({ loginSheetVisible: false });
        if (this._sheetCloseTimer) {
            clearTimeout(this._sheetCloseTimer);
        }
        this._sheetCloseTimer = setTimeout(() => {
            this.setData({
                loginPanel: '',
                loginPanelTitle: '',
            });
            this._sheetCloseTimer = null;
        }, 320);
    },
    goBack() {
        wx.navigateBack({
            delta: 1,
            fail: () => {
                wx.switchTab({
                    url: '/pages/index/index',
                    fail: () => {
                        wx.reLaunch({
                            url: '/pages/index/index',
                        });
                    },
                });
            },
        });
    },
    /**
     * 用户名输入
     */
    onUsernameInput(e) {
        this.setData({
            username: e.detail.value,
        });
    },
    /**
     * 密码输入
     */
    onPasswordInput(e) {
        this.setData({
            password: e.detail.value,
        });
    },
    goToPasswordRecovery() {
        const username = String(this.data.username || '').trim();
        const query = username ? `?username=${encodeURIComponent(username)}` : '';
        wx.navigateTo({
            url: `/pages/passwordrecovery/passwordrecovery${query}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onPhoneInput(e) {
        this.setData({ phone: e.detail.value });
    },
    onSmsCodeInput(e) {
        this.setData({ smsCode: e.detail.value });
    },
    async sendSmsCode() {
        const phone = (this.data.phone || '').trim();
        if (!/^1\d{10}$/.test(phone)) {
            wx.showToast({ title: '请输入正确的11位手机号', icon: 'none' });
            return;
        }
        if (this.data.smsCountdown > 0)
            return;
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/sms/send`,
                    method: 'POST',
                    data: { phone },
                    header: { 'Content-Type': 'application/json' },
                    success: resolve,
                    fail: reject,
                });
            });
            const data = res.data;
            if (res.statusCode === 200 && data && data.code === 0) {
                wx.showToast({ title: '验证码已发送', icon: 'success' });
                this.startSmsCountdown();
            }
            else {
                wx.showToast({ title: (data && data.msg) || '发送失败', icon: 'none' });
            }
        }
        catch (err) {
            wx.showToast({ title: '发送失败', icon: 'none' });
        }
    },
    startSmsCountdown() {
        this.setData({ smsCountdown: 60 });
        this._smsTimer = setInterval(() => {
            if (this.data.smsCountdown <= 1) {
                clearInterval(this._smsTimer);
                this.setData({ smsCountdown: 0 });
            }
            else {
                this.setData({ smsCountdown: this.data.smsCountdown - 1 });
            }
        }, 1000);
    },
    async handlePhoneLogin() {
        if (this.data.loading)
            return;
        const phone = (this.data.phone || '').trim();
        const code = (this.data.smsCode || '').trim();
        if (!/^1\d{10}$/.test(phone)) {
            wx.showToast({ title: '请输入正确的11位手机号', icon: 'none' });
            return;
        }
        if (!code) {
            wx.showToast({ title: '请输入验证码', icon: 'none' });
            return;
        }
        if (!this.ensureAgreementAccepted())
            return;
        this.setData({ loading: true });
        try {
            const deviceID = await this.ensureDeviceId();
            const inviteCode = this.data.inviteCode || '';
            const loginResult = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/login/phone`,
                    method: 'POST',
                    data: {
                        phone,
                        code,
                        device_id: deviceID || '',
                        invite_code: inviteCode || undefined,
                    },
                    header: { 'Content-Type': 'application/json' },
                    success: (res) => {
                        const data = res.data;
                        if (res.statusCode === 200 && data && data.code === 0) {
                            resolve(data.data || {});
                        }
                        else {
                            reject(new Error((data && data.msg) || '登录失败'));
                        }
                    },
                    fail: reject,
                });
            });
            const token = loginResult.token;
            if (!token)
                throw new Error('登录结果缺少 token');
            wx.setStorageSync('token', token);
            wx.setStorageSync('userInfo', {
                id: loginResult.id,
                username: loginResult.username,
                token: loginResult.token,
                ...loginResult,
            });
            // 新用户弹出身份选择
            if (loginResult.is_new_user) {
                this.setData({ loading: false, identityPopupVisible: true, pendingLoginResult: loginResult });
                this.closeLoginPanel();
            }
            else {
                this.closeLoginPanel();
                await this.enterAppAfterLogin();
            }
        }
        catch (error) {
            console.error('手机登录失败:', error);
            wx.showToast({ title: error.message || '登录失败', icon: 'none', duration: 2000 });
            this.setData({ loading: false });
        }
    },
    async onIdentitySelect(e) {
        const identity = e.currentTarget.dataset.identity;
        const loginResult = this.data.pendingLoginResult;
        if (!USER_IDENTITY_OPTIONS.includes(identity)) {
            wx.showToast({ title: '请选择有效身份', icon: 'none' });
            return;
        }
        try {
            const token = wx.getStorageSync('token');
            const deviceID = await this.ensureDeviceId();
            const apiPath = '/api/v1/miniprogram/profile/identity';
            const body = { identity_type: identity };
            const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, deviceID);
            const headers = {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'PUT',
                    data: body,
                    header: headers,
                    success: resolve,
                    fail: reject,
                });
            });
            const data = res.data || {};
            if (res.statusCode !== 200 || data.code !== 0) {
                throw new Error(data.msg || '身份保存失败');
            }
            wx.setStorageSync('userInfo', {
                ...(wx.getStorageSync('userInfo') || {}),
                ...(loginResult || {}),
                identity_type: identity,
            });
            this.setData({ identityPopupVisible: false, selectedIdentity: identity, pendingLoginResult: null });
        }
        catch (error) {
            wx.showToast({ title: error.message || '身份保存失败', icon: 'none' });
            return;
        }
        // 选完身份紧接着检查昵称完善（新用户此时昵称必为空，会连续弹出完善弹窗）
        await this.enterAppAfterLogin();
    },
    skipIdentity() {
        wx.showToast({ title: '请选择身份后继续', icon: 'none' });
    },
    /**
     * 登录成功后进入小程序：先检查昵称是否完善，
     * 未完善则弹出强制完善弹窗（保存成功后经 completed 事件回到这里继续跳转）；
     * 检查请求失败时放行，不因网络问题把用户挡在门外。
     */
    async enterAppAfterLogin() {
        try {
            const nickname = await this.fetchProfileNickname();
            if (!String(nickname || '').trim()) {
                const guard = this.selectComponent('#nicknameGuard');
                if (guard && typeof guard.show === 'function') {
                    this.setData({ loading: false });
                    guard.show();
                    return;
                }
            }
        }
        catch (error) {
            console.error('昵称完善检查失败，放行进入:', error);
        }
        this.navigateToIndexAfterLogin();
    },
    async fetchProfileNickname() {
        const token = wx.getStorageSync('token');
        if (!token) {
            return '';
        }
        const apiPath = '/api/v1/miniprogram/profile';
        const deviceID = await this.ensureDeviceId();
        const params = (0, parameter_1.generateRequestParams)(token, {}, apiPath, deviceID);
        const headers = (0, parameter_1.paramsToHeaders)(params);
        const res = await new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'GET',
                header: headers,
                success: resolve,
                fail: reject,
            });
        });
        const data = res.data || {};
        if (res.statusCode !== 200 || data.code !== 0) {
            throw new Error(data.msg || `请求失败: ${res.statusCode}`);
        }
        return String(data.data?.nickname || '');
    },
    navigateToIndexAfterLogin() {
        wx.showToast({ title: '登录成功', icon: 'success', duration: 1000 });
        this.setData({ loading: false });
        setTimeout(() => {
            wx.switchTab({ url: '/pages/index/index', fail: () => wx.reLaunch({ url: '/pages/index/index' }) });
        }, 1000);
    },
    // 昵称弹窗保存成功后的回调：延续登录流程进入首页
    onNicknameCompleted() {
        this.navigateToIndexAfterLogin();
    },
    toggleAgreement() {
        const nextValue = !this.data.agreedPrivacy;
        this.setData({
            agreedPrivacy: nextValue,
        });
        wx.setStorageSync(LOGIN_PRIVACY_AGREEMENT_STORAGE_KEY, nextValue);
    },
    openPrivacyPolicy() {
        wx.navigateTo({
            url: '/pages/privacy/privacy?from=login&type=privacy',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    openUserAgreement() {
        wx.navigateTo({
            url: '/pages/privacy/privacy?from=login&type=agreement',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    ensureAgreementAccepted() {
        if (this.data.agreedPrivacy) {
            return true;
        }
        wx.showToast({
            title: '请先阅读并同意用户协议和隐私政策',
            icon: 'none',
            duration: 2500,
        });
        return false;
    },
    async ensureDeviceId() {
        let deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)();
        if (!deviceID) {
            try {
                deviceID = await (0, deviceFingerprint_1.generateDeviceFingerprint)();
                if (deviceID) {
                    (0, deviceFingerprint_1.cacheDeviceFingerprint)(deviceID);
                }
            }
            catch (error) {
                console.error('生成设备指纹异常:', error);
            }
        }
        return deviceID || '';
    },
    /**
     * 微信登录
     */
    async handleWechatLogin() {
        if (this.data.loading) {
            return;
        }
        if (!this.ensureAgreementAccepted()) {
            return;
        }
        this.setData({ loading: true });
        try {
            // 1. 获取设备指纹（优先使用缓存的）
            let deviceID = await this.ensureDeviceId();
            if (!deviceID) {
                deviceID = 'temp_' + Date.now();
            }
            // 2. 获取微信登录code
            const loginRes = await new Promise((resolve, reject) => {
                wx.login({
                    success: resolve,
                    fail: reject,
                });
            });
            if (!loginRes.code) {
                wx.showToast({
                    title: '获取登录code失败',
                    icon: 'none',
                });
                this.setData({ loading: false });
                return;
            }
            // 3. 调用后端登录API（携带邀请码时，新用户注册双方各得50灵石）
            const inviteCode = this.data.inviteCode || '';
            const loginResult = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/login`,
                    method: 'POST',
                    data: {
                        code: loginRes.code,
                        device_id: deviceID,
                        invite_code: inviteCode || undefined,
                    },
                    header: {
                        'Content-Type': 'application/json',
                    },
                    success: (res) => {
                        const data = res.data;
                        if (res.statusCode === 200 && data.code === 0) {
                            resolve(data.data || {});
                        }
                        else {
                            reject(new Error(data.msg || '登录失败'));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
            // 4. 保存 token 和用户信息（兼容后端只返回 id/username/token）
            const token = loginResult.token;
            if (!token) {
                throw new Error('登录结果缺少 token');
            }
            wx.setStorageSync('token', token);
            wx.setStorageSync('userInfo', {
                id: loginResult.id,
                username: loginResult.username,
                token: loginResult.token,
                ...loginResult,
            });
            if (loginResult.is_new_user) {
                this.setData({ loading: false, identityPopupVisible: true, pendingLoginResult: loginResult });
                this.closeLoginPanel();
                return;
            }
            this.closeLoginPanel();
            // 5. 检查昵称是否完善（未完善则弹强制弹窗，完成后由 completed 事件继续跳转）
            await this.enterAppAfterLogin();
        }
        catch (error) {
            console.error('登录失败:', error);
            wx.showToast({
                title: error.message || '登录失败',
                icon: 'none',
                duration: 2000,
            });
            this.setData({ loading: false });
        }
        finally {
            // 无论成功/失败/异常，都关闭登录中模态框，避免一直旋转
            this.setData({ loading: false });
        }
    },
    /**
     * 账号密码登录
     */
    async handlePasswordLogin() {
        if (this.data.loading || !this.data.username || !this.data.password) {
            return;
        }
        if (!this.ensureAgreementAccepted()) {
            return;
        }
        this.setData({ loading: true });
        try {
            const deviceID = await this.ensureDeviceId();
            if (!deviceID) {
                throw new Error('获取设备信息失败，请稍后重试');
            }
            // 调用后端账号密码登录API
            const loginResult = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/login/password`,
                    method: 'POST',
                    data: {
                        username: this.data.username,
                        password: this.data.password,
                        device_id: deviceID,
                    },
                    header: {
                        'Content-Type': 'application/json',
                    },
                    success: (res) => {
                        const data = res.data;
                        if (res.statusCode === 200 && data) {
                            if (data.code === 0) {
                                resolve(data.data);
                                return;
                            }
                            reject(new Error(data.msg || '登录失败'));
                            return;
                        }
                        reject(new Error(`登录请求失败: ${res.statusCode}`));
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
            // 保存token和用户信息
            const token = loginResult.token;
            wx.setStorageSync('token', token);
            wx.setStorageSync('userInfo', loginResult);
            // 新用户弹出身份选择
            if (loginResult.is_new_user) {
                this.setData({ loading: false, identityPopupVisible: true, pendingLoginResult: loginResult });
                this.closeLoginPanel();
                return;
            }
            this.closeLoginPanel();
            await this.enterAppAfterLogin();
        }
        catch (error) {
            console.error('登录失败:', error);
            wx.showToast({
                title: error.message || '登录失败',
                icon: 'none',
                duration: 2000,
            });
            this.setData({ loading: false });
        }
    },
    onUnload() {
        if (this._smsTimer) {
            clearInterval(this._smsTimer);
        }
        if (this._sheetCloseTimer) {
            clearTimeout(this._sheetCloseTimer);
        }
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · 登录',
            path: '/pages/login/login',
        };
    },
});
