"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const aiError_1 = require("../../utils/aiError");
const API_BASE_URL = 'https://api.jiadilingguang.com';
function getTaskStatusPollDelay(attempt) {
    if (attempt < 2) {
        return 3000;
    }
    if (attempt < 6) {
        return 4000;
    }
    return 5000;
}
Page({
    data: {
        navSafeTop: 0,
        navBarHeight: 96,
        navContentHeight: 44,
        navSideWidth: 88,
        entrySource: '',
        showSceneTabs: false,
        activeTab: 'exterior',
        taskNo: '',
        loadingText: '正在加速生成中，请稍后...',
        spinnerItems: Array.from({ length: 12 }, (_, index) => index),
    },
    pollingTimer: null,
    pollingAttempt: 0,
    onLoad(options) {
        const taskNo = options.task_no ? decodeURIComponent(options.task_no) : '';
        const source = String(options.source || '');
        const showSceneTabs = source === 'rural_villa'
            ? true
            : source === 'index' || source === 'urban_renewal'
                ? false
                : options.showSceneTabs === '1';
        const activeTab = options.tab === 'interior' ? 'interior' : 'exterior';
        this.setData({
            entrySource: source,
            showSceneTabs,
            activeTab,
            taskNo,
        });
        if (!taskNo) {
            wx.showToast({
                title: '任务编号缺失',
                icon: 'none'
            });
            setTimeout(() => {
                this.onBack();
            }, 800);
            return;
        }
        const sourceQuery = source
            ? `&source=${encodeURIComponent(source)}`
            : '';
        wx.redirectTo({
            url: `/pages/generatedetails/generatedetails?task_no=${encodeURIComponent(taskNo)}&tab=${activeTab}&showSceneTabs=${showSceneTabs ? 1 : 0}${sourceQuery}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none'
                });
            }
        });
    },
    onUnload() {
        this.stopPolling();
    },
    initNavLayout() {
        try {
            const systemInfo = wx.getSystemInfoSync();
            const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
                ? wx.getMenuButtonBoundingClientRect()
                : null;
            const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
            if (menuRect) {
                const navBarHeight = Number(menuRect.bottom + menuRect.top - safeTop);
                const navContentHeight = Number(menuRect.height);
                const navSideWidth = Number(systemInfo.windowWidth - menuRect.left);
                this.setData({
                    navSafeTop: safeTop,
                    navBarHeight,
                    navContentHeight,
                    navSideWidth,
                });
                return;
            }
            this.setData({
                navSafeTop: safeTop,
                navBarHeight: safeTop + 44,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
        catch (error) {
            this.setData({
                navSafeTop: 20,
                navBarHeight: 64,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
    },
    onSelectTab(e) {
        const tab = e.currentTarget.dataset.tab === 'interior' ? 'interior' : 'exterior';
        this.setData({
            activeTab: tab,
        });
    },
    onBack() {
        this.stopPolling();
        wx.navigateBack({
            fail: () => {
                wx.reLaunch({
                    url: '/pages/index/index'
                });
            }
        });
    },
    startPolling(taskNo) {
        this.stopPolling();
        this.pollingAttempt = 0;
        this.loadTaskStatus(taskNo);
    },
    scheduleNextPoll(taskNo) {
        this.stopPolling();
        const delay = getTaskStatusPollDelay(this.pollingAttempt);
        this.pollingAttempt += 1;
        this.pollingTimer = setTimeout(() => {
            this.loadTaskStatus(taskNo, true);
        }, delay);
    },
    stopPolling() {
        if (this.pollingTimer) {
            clearTimeout(this.pollingTimer);
            this.pollingTimer = null;
        }
    },
    async getAuthHeaders(body, path) {
        const token = wx.getStorageSync('token');
        if (!token) {
            throw new Error('未登录');
        }
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const params = (0, parameter_1.generateRequestParams)(token, body, path, deviceID);
        return (0, parameter_1.paramsToHeaders)(params);
    },
    async loadTaskStatus(taskNo, silent = false) {
        try {
            const body = JSON.stringify({
                task_no: taskNo,
                task_type: 'ai_draw'
            });
            const headers = await this.getAuthHeaders(body, '/api/v1/miniprogram/ai/task/status');
            wx.request({
                url: `${API_BASE_URL}/api/v1/miniprogram/ai/task/status`,
                method: 'POST',
                header: {
                    ...headers,
                    'Content-Type': 'application/json'
                },
                data: {
                    task_no: taskNo,
                    task_type: 'ai_draw'
                },
                success: (res) => {
                    if (res.statusCode !== 200 || !res.data || res.data.code !== 0) {
                        if (!silent) {
                            wx.showToast({
                                title: res.data?.msg || '查询任务失败',
                                icon: 'none'
                            });
                        }
                        return;
                    }
                    const taskData = res.data.data || {};
                    if (taskData.status === 'success') {
                        this.stopPolling();
                        const sourceQuery = this.data.entrySource
                            ? `&source=${encodeURIComponent(this.data.entrySource)}`
                            : '';
                        wx.redirectTo({
                            url: `/pages/generatedetails/generatedetails?task_no=${encodeURIComponent(taskNo)}&tab=${this.data.activeTab}&showSceneTabs=${this.data.showSceneTabs ? 1 : 0}${sourceQuery}`
                        });
                        return;
                    }
                    if (taskData.status === 'failed') {
                        this.stopPolling();
                        wx.showModal({
                            title: '生成失败',
                            content: (0, aiError_1.sanitizeAIGenerationErrorMessage)(taskData.error_message),
                            showCancel: false,
                            success: () => {
                                this.onBack();
                            }
                        });
                        return;
                    }
                    this.scheduleNextPoll(taskNo);
                },
                fail: () => {
                    if (!silent) {
                        wx.showToast({
                            title: (0, aiError_1.sanitizeAIGenerationErrorMessage)('网络异常，请稍后重试'),
                            icon: 'none'
                        });
                    }
                    else {
                        this.scheduleNextPoll(taskNo);
                    }
                }
            });
        }
        catch (error) {
            if (!silent) {
                wx.showToast({
                    title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(error.message || '查询任务失败'),
                    icon: 'none'
                });
            }
            else {
                this.scheduleNextPoll(taskNo);
            }
        }
    }
});
