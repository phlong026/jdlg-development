"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/aivideo/aivideo.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const asset_1 = require("../../utils/asset");
const aiError_1 = require("../../utils/aiError");
const promptReview_1 = require("../../utils/promptReview");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_VIDEO_COST = 30;
const AVAILABLE_DURATIONS = [5, 8];
const POLL_INTERVAL_MS = 3000;
const POLL_TIMEOUT_MS = 5 * 60 * 1000;
const GENERATE_HISTORY_URL = '/pages/generatehistory/generatehistory';
const ACTIVE_VIDEO_TASK_STORAGE_KEY = 'active_video_generation_task';
Page({
    data: {
        mode: 'start_end',
        orientation: 'landscape',
        ratio: '16:9',
        resolution: '720p',
        ratioOptions: [
            { label: '16:9', value: '16:9', icon: 'landscape' },
            { label: '1:1', value: '1:1', icon: 'square' },
            { label: '9:16', value: '9:16', icon: 'portrait' },
        ],
        videoCost: DEFAULT_VIDEO_COST,
        duration: AVAILABLE_DURATIONS[0],
        durationMin: AVAILABLE_DURATIONS[0],
        durationMax: AVAILABLE_DURATIONS[1],
        durationOptions: [
            { label: '5秒', value: AVAILABLE_DURATIONS[0] },
            { label: '8秒', value: AVAILABLE_DURATIONS[1] },
        ],
        segmentCount: 1,
        segmentPrompts: [],
        cameraFixed: false,
        style: '',
        prompt: '',
        optimizingPrompt: false,
        startFrameUrl: '',
        startFramePreview: '',
        endFrameUrl: '',
        endFramePreview: '',
        uploading: false,
        generating: false,
        taskId: 0,
        resultUrl: '',
        resultStatus: '',
        resultStatusDetail: '',
        submitTip: '请先上传首帧和尾帧',
        canSubmit: false,
        pollTimer: null,
        navSafeTop: 0,
        navBarHeight: 96,
        navContentHeight: 44,
        navSideWidth: 88,
        pageBgImage: (0, asset_1.resolveAssetPath)('/assets/aivideo/生成漫游视频背景.png'),
    },
    videoPriceMap: {},
    videoConfigMap: {},
    pollStartedAt: 0,
    onLoad(options) {
        this.initNavLayout();
        this.loadVideoPricing();
        if (options.prompt) {
            this.updateSubmitState({
                prompt: decodeURIComponent(options.prompt),
            });
        }
        if (options.taskId) {
            const taskId = parseInt(options.taskId, 10);
            if (taskId) {
                this.setData({ taskId });
                this.saveActiveTask(taskId);
                this.loadTaskResult(taskId);
            }
        }
    },
    onShow() {
        this.restoreActiveTask();
    },
    onHide() {
        this.stopPoll();
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
    getTaskStatusView(status, errorMessage) {
        if (status === 'queued') {
            return {
                statusText: '排队中',
                statusDetail: '可先返回、去别的页面，或到生成记录查看，任务会在后台继续处理。',
            };
        }
        if (status === 'processing') {
            return {
                statusText: '生成中',
                statusDetail: '可先返回、去别的页面，或到生成记录查看，任务会在后台继续处理。',
            };
        }
        if (status === 'completed') {
            return {
                statusText: '视频已生成完成',
                statusDetail: '',
            };
        }
        if (status === 'failed' || status === 'error') {
            return {
                statusText: errorMessage || '生成失败，请更换图片或提示词后重试',
                statusDetail: '',
            };
        }
        return {
            statusText: status || '',
            statusDetail: '',
        };
    },
    updateSubmitState(partialData = {}) {
        const nextData = { ...this.data, ...partialData };
        let submitTip = `${nextData.videoCost}灵石`;
        let canSubmit = true;
        if (nextData.generating) {
            submitTip = '任务后台生成中，可先查看生成记录';
            canSubmit = false;
        }
        else if (nextData.uploading) {
            submitTip = '图片上传中，请稍候';
            canSubmit = false;
        }
        else if (!String(nextData.startFrameUrl || '').trim() || !String(nextData.endFrameUrl || '').trim()) {
            submitTip = '请先上传首帧和尾帧';
            canSubmit = false;
        }
        else if (!String(nextData.prompt || '').trim()) {
            submitTip = '请补充视频描述';
            canSubmit = false;
        }
        this.setData({
            ...partialData,
            submitTip,
            canSubmit,
        });
    },
    getDraftResetState() {
        if (this.data.generating) {
            return {};
        }
        const currentTaskId = Number(this.data.taskId || 0);
        if (currentTaskId) {
            this.clearActiveTask(currentTaskId);
        }
        if (!this.data.resultUrl && !this.data.resultStatus && !currentTaskId) {
            return {};
        }
        return {
            resultUrl: '',
            resultStatus: '',
            taskId: 0,
        };
    },
    getCurrentVideoScene() {
        const segmentCount = Math.min(4, Math.max(1, Number(this.data.segmentCount) || 1));
        return `ai_video_${segmentCount}`;
    },
    normalizePositiveInt(value, fallback) {
        const parsed = Number(value);
        return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : fallback;
    },
    normalizeDurationValue(value) {
        const parsed = Number(value);
        if (parsed === AVAILABLE_DURATIONS[1]) {
            return AVAILABLE_DURATIONS[1];
        }
        return AVAILABLE_DURATIONS[0];
    },
    applyVideoPricing(scene) {
        const targetScene = scene || this.getCurrentVideoScene();
        const nextCost = this.normalizePositiveInt(this.videoPriceMap[targetScene], DEFAULT_VIDEO_COST);
        const currentDuration = Number(this.data.duration) || AVAILABLE_DURATIONS[0];
        this.updateSubmitState({
            videoCost: nextCost,
            durationMin: AVAILABLE_DURATIONS[0],
            durationMax: AVAILABLE_DURATIONS[1],
            duration: this.normalizeDurationValue(currentDuration),
        });
    },
    async loadVideoPricing() {
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/ai/pricing?scenes=ai_video_1,ai_video_2,ai_video_3,ai_video_4`,
                    method: 'GET',
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                            }
                            else {
                                reject(new Error(data.msg || '获取视频计费失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            this.videoPriceMap = {
                ai_video_1: this.normalizePositiveInt(res?.prices?.ai_video_1, DEFAULT_VIDEO_COST),
                ai_video_2: this.normalizePositiveInt(res?.prices?.ai_video_2, DEFAULT_VIDEO_COST),
                ai_video_3: this.normalizePositiveInt(res?.prices?.ai_video_3, DEFAULT_VIDEO_COST),
                ai_video_4: this.normalizePositiveInt(res?.prices?.ai_video_4, DEFAULT_VIDEO_COST),
            };
            this.videoConfigMap = {
                ai_video_1: res?.configs?.ai_video_1 || {},
                ai_video_2: res?.configs?.ai_video_2 || {},
                ai_video_3: res?.configs?.ai_video_3 || {},
                ai_video_4: res?.configs?.ai_video_4 || {},
            };
        }
        catch (error) {
            this.videoPriceMap = {};
            this.videoConfigMap = {};
        }
        this.applyVideoPricing();
    },
    async loadTaskResult(taskId) {
        try {
            const result = await this.queryTask(taskId);
            const statusView = this.getTaskStatusView(result.status, result.errorMessage);
            this.updateSubmitState({
                taskId,
                resultStatus: statusView.statusText,
                resultStatusDetail: statusView.statusDetail,
                resultUrl: result.url || '',
                generating: result.status === 'processing' || result.status === 'queued',
            });
            if (result.status === 'completed' && result.url) {
                this.clearActiveTask(taskId);
            }
            else if (result.status === 'failed' || result.status === 'error') {
                this.clearActiveTask(taskId);
            }
            else if (result.status === 'processing' || result.status === 'queued') {
                this.saveActiveTask(taskId);
                this.doPoll(taskId);
            }
        }
        catch (e) {
            this.updateSubmitState({ resultStatus: '加载失败' });
        }
    },
    onUnload() {
        this.stopPoll();
    },
    getStoredActiveTaskId() {
        try {
            const cache = wx.getStorageSync(ACTIVE_VIDEO_TASK_STORAGE_KEY);
            const taskId = Number(cache?.taskId || 0);
            return Number.isFinite(taskId) && taskId > 0 ? taskId : 0;
        }
        catch (error) {
            return 0;
        }
    },
    saveActiveTask(taskId) {
        if (!taskId) {
            return;
        }
        try {
            wx.setStorageSync(ACTIVE_VIDEO_TASK_STORAGE_KEY, {
                taskId,
                updatedAt: Date.now(),
            });
        }
        catch (error) {
        }
    },
    clearActiveTask(taskId) {
        try {
            const currentTaskId = this.getStoredActiveTaskId();
            if (taskId && currentTaskId && currentTaskId !== taskId) {
                return;
            }
            wx.removeStorageSync(ACTIVE_VIDEO_TASK_STORAGE_KEY);
        }
        catch (error) {
        }
    },
    restoreActiveTask() {
        const cachedTaskId = this.getStoredActiveTaskId();
        const currentTaskId = Number(this.data.taskId || 0);
        const targetTaskId = currentTaskId || cachedTaskId;
        if (!targetTaskId) {
            return;
        }
        if (this.data.resultUrl && currentTaskId === targetTaskId) {
            return;
        }
        if (this.data.pollTimer && currentTaskId === targetTaskId) {
            return;
        }
        this.updateSubmitState({
            taskId: targetTaskId,
            generating: true,
            resultStatus: this.data.resultUrl ? this.data.resultStatus : (this.data.resultStatus || '正在恢复任务状态...'),
            resultStatusDetail: this.data.resultUrl ? this.data.resultStatusDetail : (this.data.resultStatusDetail || '你可以先返回或去生成记录查看，任务会继续在后台处理。'),
        });
        this.loadTaskResult(targetTaskId);
    },
    onRatioChange(e) {
        const ratio = e.currentTarget.dataset.ratio;
        this.updateSubmitState({
            ...this.getDraftResetState(),
            ratio,
            orientation: ratio === '9:16' ? 'portrait' : 'landscape'
        });
    },
    onDurationSelect(e) {
        const duration = this.normalizeDurationValue(e.currentTarget.dataset.duration);
        this.updateSubmitState({
            ...this.getDraftResetState(),
            duration,
        });
    },
    onPromptInput(e) {
        this.updateSubmitState({
            ...this.getDraftResetState(),
            prompt: e.detail.value,
        });
    },
    async onOptimizePrompt() {
        if (this.data.optimizingPrompt)
            return;
        const rawPrompt = (this.data.prompt || '').trim();
        if (!rawPrompt) {
            wx.showToast({ title: '请先输入提示词', icon: 'none' });
            return;
        }
        this.setData({ optimizingPrompt: true });
        const { optimizePrompt } = require('../../utils/promptOptimize');
        const result = await optimizePrompt(rawPrompt, 'video');
        this.setData({ optimizingPrompt: false });
        if (result.success && result.optimizedPrompt) {
            this.updateSubmitState({ prompt: result.optimizedPrompt });
            wx.showToast({ title: '优化完成', icon: 'success' });
        }
        else {
            if (result.needMembership) {
                wx.showModal({ title: '会员专属', content: result.error || '请开通会员后使用AI优化', confirmText: '去开通', success: (res) => { if (res.confirm)
                        wx.navigateTo({ url: '/pages/topupcenter/topupcenter' }); } });
            }
            else {
                wx.showToast({ title: result.error || '优化失败', icon: 'none' });
            }
        }
    },
    async uploadReferenceImage(tempFilePath) {
        const token = wx.getStorageSync('token');
        if (!token)
            throw new Error('未登录');
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const apiPath = '/api/v1/miniprogram/ai/upload-reference-image';
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
                        if (data.code === 0 && data.data && data.data.url) {
                            resolve(data.data.url);
                        }
                        else {
                            reject(new Error(data.msg || '上传失败'));
                        }
                    }
                    catch (e) {
                        reject(new Error('解析响应失败'));
                    }
                },
                fail: (err) => reject(err),
            });
        });
    },
    onSelectStartFrame() {
        if (this.data.uploading)
            return;
        wx.chooseMedia({
            count: 1,
            mediaType: ['image'],
            sourceType: ['album', 'camera'],
            success: async (res) => {
                const tempFilePath = res.tempFiles[0].tempFilePath;
                this.updateSubmitState({ uploading: true });
                wx.showLoading({ title: '上传首帧...', mask: true });
                try {
                    const url = await this.uploadReferenceImage(tempFilePath);
                    this.updateSubmitState({
                        ...this.getDraftResetState(),
                        startFrameUrl: url,
                        startFramePreview: tempFilePath,
                        uploading: false,
                    });
                    wx.hideLoading();
                }
                catch (e) {
                    wx.hideLoading();
                    this.updateSubmitState({ uploading: false });
                    wx.showToast({ title: e.message || '上传失败', icon: 'none' });
                }
            },
        });
    },
    onSelectEndFrame() {
        if (this.data.uploading)
            return;
        wx.chooseMedia({
            count: 1,
            mediaType: ['image'],
            sourceType: ['album', 'camera'],
            success: async (res) => {
                const tempFilePath = res.tempFiles[0].tempFilePath;
                this.updateSubmitState({ uploading: true });
                wx.showLoading({ title: '上传尾帧...', mask: true });
                try {
                    const url = await this.uploadReferenceImage(tempFilePath);
                    this.updateSubmitState({
                        ...this.getDraftResetState(),
                        endFrameUrl: url,
                        endFramePreview: tempFilePath,
                        uploading: false,
                    });
                    wx.hideLoading();
                }
                catch (e) {
                    wx.hideLoading();
                    this.updateSubmitState({ uploading: false });
                    wx.showToast({ title: e.message || '上传失败', icon: 'none' });
                }
            },
        });
    },
    onDeleteStartFrame() {
        this.updateSubmitState({
            ...this.getDraftResetState(),
            startFrameUrl: '',
            startFramePreview: '',
        });
    },
    onDeleteEndFrame() {
        this.updateSubmitState({
            ...this.getDraftResetState(),
            endFrameUrl: '',
            endFramePreview: '',
        });
    },
    onOpenGenerateHistory() {
        wx.navigateTo({ url: GENERATE_HISTORY_URL });
    },
    stopPoll() {
        if (this.data.pollTimer) {
            clearTimeout(this.data.pollTimer);
            this.setData({ pollTimer: null });
        }
        this.pollStartedAt = 0;
    },
    scheduleNextPoll(taskId, resultStatus, resultStatusDetail = '') {
        if (this.pollStartedAt && Date.now() - this.pollStartedAt >= POLL_TIMEOUT_MS) {
            this.handlePollTimeout();
            return;
        }
        const t = setTimeout(() => this.doPoll(taskId), POLL_INTERVAL_MS);
        this.setData({
            pollTimer: t,
            resultStatus,
            resultStatusDetail,
        });
    },
    handlePollTimeout() {
        this.stopPoll();
        this.updateSubmitState({
            generating: false,
            resultStatus: '生成耗时较长',
            resultStatusDetail: '任务可能仍在后台继续处理。你可以先返回、去别的页面，或到生成记录查看进度。',
        });
        wx.showToast({ title: '可去生成记录查看', icon: 'none' });
    },
    async createTask() {
        const prompt = String(this.data.prompt || '').trim();
        if ((0, promptReview_1.blockUnsafeText)(prompt, { scene: 'prompt' })) {
            throw new Error('内容拦截');
        }
        const token = wx.getStorageSync('token');
        if (!token)
            throw new Error('请先登录');
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const apiPath = '/api/v1/miniprogram/ai/video/create';
        const body = {
            prompt: this.data.prompt.trim(),
            orientation: this.data.orientation,
            ratio: this.data.ratio,
            resolution: this.data.resolution,
            duration: this.data.duration,
            segment_count: 1,
            camera_fixed: false,
            start_frame_url: this.data.startFrameUrl,
            end_frame_url: this.data.endFrameUrl,
        };
        const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, deviceID);
        const headers = (0, parameter_1.paramsToHeaders)(params);
        headers['Content-Type'] = 'application/json';
        return new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'POST',
                data: body,
                header: headers,
                success: (res) => {
                    if (res.statusCode !== 200) {
                        reject(new Error(res.data?.msg || `请求失败 ${res.statusCode}`));
                        return;
                    }
                    const d = res.data;
                    if (d.code !== 0 || !d.data || !d.data.task_id) {
                        reject(new Error(d.msg || '创建任务失败'));
                        return;
                    }
                    resolve(Number(d.data.task_id));
                },
                fail: (err) => reject(err),
            });
        });
    },
    async pollTask(taskId) {
        const token = wx.getStorageSync('token');
        if (!token)
            throw new Error('未登录');
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const apiPath = `/api/v1/miniprogram/ai/video/poll/${taskId}`;
        const params = (0, parameter_1.generateRequestParams)(token, '{}', apiPath, deviceID);
        const headers = (0, parameter_1.paramsToHeaders)(params);
        return new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'GET',
                header: headers,
                success: (res) => {
                    if (res.statusCode !== 200) {
                        reject(new Error(res.data?.msg || '轮询失败'));
                        return;
                    }
                    const d = res.data;
                    if (d.code !== 0 || !d.data) {
                        reject(new Error(d.msg || '轮询失败'));
                        return;
                    }
                    resolve({
                        status: d.data.status || '',
                        errorMessage: d.data.error_message || '',
                    });
                },
                fail: (err) => reject(err),
            });
        });
    },
    async queryTask(taskId) {
        const token = wx.getStorageSync('token');
        if (!token)
            throw new Error('未登录');
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const apiPath = `/api/v1/miniprogram/ai/video/query/${taskId}`;
        const params = (0, parameter_1.generateRequestParams)(token, '{}', apiPath, deviceID);
        const headers = (0, parameter_1.paramsToHeaders)(params);
        return new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'GET',
                header: headers,
                success: (res) => {
                    if (res.statusCode !== 200) {
                        reject(new Error(res.data?.msg || '查询失败'));
                        return;
                    }
                    const d = res.data;
                    if (d.code !== 0 || !d.data) {
                        reject(new Error(d.msg || '查询失败'));
                        return;
                    }
                    resolve({
                        status: d.data.status || '',
                        url: d.data.url,
                        errorMessage: d.data.error_message || '',
                    });
                },
                fail: (err) => reject(err),
            });
        });
    },
    doPoll(taskId) {
        if (!this.pollStartedAt) {
            this.pollStartedAt = Date.now();
        }
        if (Date.now() - this.pollStartedAt >= POLL_TIMEOUT_MS) {
            this.handlePollTimeout();
            return;
        }
        this.pollTask(taskId)
            .then(async (pollResult) => {
            const { status, errorMessage } = pollResult;
            const statusView = this.getTaskStatusView(status, errorMessage);
            this.setData({ resultStatus: statusView.statusText, resultStatusDetail: statusView.statusDetail });
            if (status === 'completed') {
                const result = await this.queryTask(taskId);
                if (result.url) {
                    this.stopPoll();
                    this.clearActiveTask(taskId);
                    this.updateSubmitState({
                        generating: false,
                        resultUrl: result.url,
                        resultStatus: this.getTaskStatusView('completed').statusText,
                        resultStatusDetail: '',
                    });
                    return;
                }
                this.scheduleNextPoll(taskId, '结果整理中...', '视频已生成完成，正在整理结果，你可以稍后回来查看。');
                return;
            }
            if (status === 'failed' || status === 'error') {
                this.stopPoll();
                this.clearActiveTask(taskId);
                this.updateSubmitState({
                    generating: false,
                    resultStatus: this.getTaskStatusView(status, errorMessage).statusText,
                    resultStatusDetail: '',
                });
                wx.showToast({ title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(errorMessage || '生成失败'), icon: 'none' });
                return;
            }
            this.scheduleNextPoll(taskId, statusView.statusText, statusView.statusDetail);
        })
            .catch(async () => {
            if (Date.now() - this.pollStartedAt >= POLL_TIMEOUT_MS) {
                this.handlePollTimeout();
                return;
            }
            try {
                const result = await this.queryTask(taskId);
                const statusView = this.getTaskStatusView(result.status, result.errorMessage);
                if (result.status === 'completed' && result.url) {
                    this.stopPoll();
                    this.clearActiveTask(taskId);
                    this.updateSubmitState({
                        generating: false,
                        resultUrl: result.url,
                        resultStatus: this.getTaskStatusView('completed').statusText,
                        resultStatusDetail: '',
                    });
                    return;
                }
                if (result.status === 'failed' || result.status === 'error') {
                    this.stopPoll();
                    this.clearActiveTask(taskId);
                    this.updateSubmitState({
                        generating: false,
                        resultStatus: statusView.statusText,
                        resultStatusDetail: '',
                    });
                    wx.showToast({ title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(result.errorMessage || '生成失败'), icon: 'none' });
                    return;
                }
                if (statusView.statusText) {
                    this.setData({ resultStatus: statusView.statusText, resultStatusDetail: statusView.statusDetail });
                }
            }
            catch (queryError) {
            }
            this.scheduleNextPoll(taskId, '网络波动，继续查询中...', '任务仍会在后台继续处理，你可以稍后回来查看或去生成记录查看。');
        });
    },
    async onSubmit() {
        const prompt = this.data.prompt.trim();
        if (this.data.uploading) {
            wx.showToast({ title: '图片上传中，请稍候', icon: 'none' });
            return;
        }
        if (!prompt) {
            wx.showToast({ title: '请输入视频描述', icon: 'none' });
            return;
        }
        if (!this.data.startFrameUrl) {
            wx.showToast({ title: '请先上传首帧图片', icon: 'none' });
            return;
        }
        if (!this.data.endFrameUrl) {
            wx.showToast({ title: '请再上传尾帧图片', icon: 'none' });
            return;
        }
        if ((0, promptReview_1.blockUnsafeText)(prompt, { scene: 'prompt' })) {
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showModal({
                title: '提示',
                content: '请先登录',
                confirmText: '去登录',
                success: (res) => {
                    if (res.confirm)
                        wx.navigateTo({ url: '/pages/login/login' });
                },
            });
            return;
        }
        if (this.data.generating)
            return;
        const previousTaskId = Number(this.data.taskId || 0);
        if (previousTaskId) {
            this.clearActiveTask(previousTaskId);
        }
        this.updateSubmitState({
            generating: true,
            resultUrl: '',
            resultStatus: '提交中...',
            resultStatusDetail: '',
            taskId: 0,
        });
        wx.showLoading({ title: '提交中...', mask: true });
        try {
            const taskId = await this.createTask();
            this.pollStartedAt = Date.now();
            this.saveActiveTask(taskId);
            const statusView = this.getTaskStatusView('queued');
            this.updateSubmitState({
                taskId,
                resultStatus: statusView.statusText,
                resultStatusDetail: statusView.statusDetail,
            });
            wx.hideLoading();
            this.doPoll(taskId);
        }
        catch (e) {
            this.updateSubmitState({ generating: false, resultStatus: '', resultStatusDetail: '' });
            wx.hideLoading();
            wx.showToast({ title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(e.message || '创建失败'), icon: 'none' });
        }
    },
    onReset() {
        this.stopPoll();
        this.clearActiveTask(this.data.taskId);
        this.updateSubmitState({
            generating: false,
            resultUrl: '',
            resultStatus: '',
            resultStatusDetail: '',
            taskId: 0,
        });
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · AI生成视频',
            path: '/pages/aivideo/aivideo',
        };
    },
});
