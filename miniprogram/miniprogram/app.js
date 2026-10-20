"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// Unified fail-open tracking; feature flag and independent consent default off.
require('./utils/tracking/index').install();
// app.ts
const promptReview_1 = require("./utils/promptReview");
const TABBAR_IMAGE_PATHS = [
    '/assets/tabbar/未选中态@2x.png',
    '/assets/tabbar/首页导航栏@2x.png',
    '/assets/tabbar/模板广场导航栏@2x.png',
    '/assets/tabbar/设计师导航栏@2x.png',
    '/assets/tabbar/我的导航栏@2x.png'
];
// 服务端邀请码固定 6 位字母数字。options.scene 是微信启动场景值（1007/1047 等），不是邀请码，
// 小程序码携带的参数在 options.query.scene 里。格式不符一律忽略，避免把无关数字绑成邀请人。
const INVITE_CODE_PATTERN = /^[A-Za-z0-9]{6}$/;
function captureInviteCode(options) {
    if (options?.path === 'pages/contentreader/contentreader')
        return;
    const query = options?.query || {};
    let code = String(query.invite_code || query.scene || '');
    try {
        code = decodeURIComponent(code);
    }
    catch (e) { /* 保留原值，交给格式校验 */ }
    if (INVITE_CODE_PATTERN.test(code)) {
        wx.setStorageSync('pending_invite_code', code.toUpperCase());
    }
}
let interceptModalHost = null;
App({
    globalData: {
        blockUnsafeText: promptReview_1.blockUnsafeText,
    },
    registerInterceptModal(host) {
        interceptModalHost = host;
    },
    unregisterInterceptModal(host) {
        if (interceptModalHost === host) {
            interceptModalHost = null;
        }
    },
    showInterceptModal(payload) {
        const host = interceptModalHost;
        if (host && typeof host.show === 'function') {
            host.show(payload);
            return true;
        }
        return false;
    },
    // 热启动（小程序在后台时点分享卡片）不会触发 onLaunch
    onShow(options) {
        captureInviteCode(options);
    },
    onLaunch(options) {
        // 展示本地存储能力
        const logs = wx.getStorageSync('logs') || [];
        logs.unshift(Date.now());
        wx.setStorageSync('logs', logs);
        captureInviteCode(options);
        TABBAR_IMAGE_PATHS.forEach((src) => {
            wx.getImageInfo({
                src,
                fail: () => { }
            });
        });
        // 本地审核无需初始化，保留空调用兼容旧缓存
        void (0, promptReview_1.initPromptReview)();
    },
});
