"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const api_1 = require("./api");
const asset_1 = require("../../utils/asset");
Page({
    data: { enabled: false, loading: true, error: '', recent: null, recentError: '', background: (0, asset_1.resolveAssetPath)('/assets/home/logo+背景图.png') },
    onLoad() { this.loadConfig(); },
    onShow() {
        if (this.data.enabled)
            this.loadRecent();
    },
    async loadConfig() {
        this.setData({ loading: true, error: '' });
        try {
            const config = await (0, api_1.reportRequest)('/config');
            this.setData({ enabled: config.enabled === true, error: config.enabled ? '' : '建房报告服务正在准备中，开放后即可填写。' });
            if (config.enabled)
                this.loadRecent();
        }
        catch (error) {
            this.setData({ error: error.message });
        }
        finally {
            this.setData({ loading: false });
        }
    },
    async loadRecent() {
        const token = wx.getStorageSync('token');
        if (!token) {
            this.setData({ recent: null, recentError: '' });
            return;
        }
        try {
            const recent = await (0, api_1.reportRequest)('/recent');
            if (wx.getStorageSync('token') === token)
                this.setData({ recent, recentError: '' });
        }
        catch (error) {
            if (wx.getStorageSync('token') === token)
                this.setData({ recent: null, recentError: error.statusCode === 404 ? '' : '暂时无法读取上次报告，请点击重试。' });
        }
    },
    resume() {
        try {
            const id = (0, api_1.reportID)(this.data.recent?.id);
            wx.navigateTo({ url: '/pages/report/' + (this.data.recent.paid ? 'delivery' : 'preview') + '?id=' + id, fail: () => this.setData({ recentError: '打开报告失败，请重试。' }) });
        }
        catch (error) {
            this.setData({ recentError: error.message });
        }
    },
    start() {
        if (this.data.enabled)
            wx.navigateTo({ url: '/pages/report/questionnaire', fail: () => this.setData({ error: '页面打开失败，请重试。' }) });
    },
    onShareAppMessage() { return { title: '先想清楚怎么住，再决定怎么建', path: '/pages/report/landing' }; },
});
