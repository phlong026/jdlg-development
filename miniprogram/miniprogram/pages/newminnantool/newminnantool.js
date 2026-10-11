"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const aiError_1 = require("../../utils/aiError");
const promptReview_1 = require("../../utils/promptReview");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const TEXT_TO_IMAGE_SCENE = 'new_minnan_tool_text_to_image';
const IMAGE_TO_IMAGE_SCENE = 'new_minnan_tool_image_to_image';
const DEFAULT_GENERATE_COUNT = 1;
const NEW_MINNAN_TEMPLATE_TOOL = {
    id: 'new_minnan_template_tool',
    code: 'new_minnan_tool',
    name: '新闽派工具',
    category: 'architecture',
    shortDescription: '用文字或参考图快速生成新闽派建筑风格画面',
    detailDescription: '模板广场新闽派专项生成入口',
    common: true,
    promptPlaceholder: '描述你想生成的新闽派建筑、场景、视角、材质和氛围',
    uploadHint: '图生图需上传原图和参考图。',
    tags: ['新闽派', '文生图', '图生图'],
    presetReferences: [],
    stylePresets: [],
};
function normalizeDimension(value) {
    const num = Number(String(value || '').trim());
    if (!Number.isFinite(num) || num <= 0) {
        return 0;
    }
    return Math.floor(num);
}
Page({
    data: {
        tool: NEW_MINNAN_TEMPLATE_TOOL,
        loadingTool: false,
        generating: false,
        activeMode: 'text_to_image',
        promptText: '',
        optimizingPrompt: false,
        originalImageUrl: '',
        referenceImageUrl: '',
        uploadingOriginal: false,
        uploadingReference: false,
        canvasOptions: [
            { key: 'portrait', label: '竖版', desc: '848 × 1280', width: 848, height: 1280 },
            { key: 'landscape', label: '横版', desc: '1280 × 848', width: 1280, height: 848 },
            { key: 'custom', label: '自定义', desc: '输入尺寸' },
        ],
        selectedCanvasKey: 'portrait',
        customWidth: '',
        customHeight: '',
        generateCountOptions: [1, 2, 3],
        selectedGenerateCount: DEFAULT_GENERATE_COUNT,
        unitCost: 0,
        currentCost: 0,
        pricingLoading: false,
        pricingLoaded: false,
        pricingError: '',
        stoneBalance: 0,
        balanceLoaded: false,
        bottomTipText: '',
    },
    onLoad() {
        this.loadAIPricing();
        this.loadStoneBalance();
    },
    onShow() {
        this.loadStoneBalance();
    },
    onHide() {
        wx.hideLoading();
    },
    onUnload() {
        wx.hideLoading();
    },
    getToken() {
        return String(wx.getStorageSync('token') || '').trim();
    },
    ensureToken() {
        const token = this.getToken();
        if (token) {
            return token;
        }
        wx.showModal({
            title: '提示',
            content: '请先登录后再使用新闽派工具',
            confirmText: '去登录',
            success: (res) => {
                if (res.confirm) {
                    wx.navigateTo({ url: '/pages/login/login' });
                }
            },
        });
        return '';
    },
    getCurrentGenerateCount() {
        const currentValue = Number(this.data.selectedGenerateCount || DEFAULT_GENERATE_COUNT);
        if (!Number.isFinite(currentValue) || currentValue <= 0) {
            return DEFAULT_GENERATE_COUNT;
        }
        return Math.min(3, Math.max(1, Math.floor(currentValue)));
    },
    getCurrentScene() {
        return this.data.activeMode === 'image_to_image' ? IMAGE_TO_IMAGE_SCENE : TEXT_TO_IMAGE_SCENE;
    },
    getSelectedCanvas() {
        if (this.data.selectedCanvasKey === 'custom') {
            return {
                key: 'custom',
                label: '自定义',
                desc: '输入尺寸',
                width: normalizeDimension(this.data.customWidth),
                height: normalizeDimension(this.data.customHeight),
            };
        }
        return (this.data.canvasOptions || []).find((item) => item.key === this.data.selectedCanvasKey) || this.data.canvasOptions[0];
    },
    buildBottomTipText(options) {
        const pricingLoaded = typeof options?.pricingLoaded === 'boolean' ? options.pricingLoaded : this.data.pricingLoaded;
        const unitCost = typeof options?.unitCost === 'number' ? options.unitCost : Number(this.data.unitCost || 0);
        const currentCost = typeof options?.currentCost === 'number' ? options.currentCost : Number(this.data.currentCost || 0);
        const balanceLoaded = typeof options?.balanceLoaded === 'boolean' ? options.balanceLoaded : this.data.balanceLoaded;
        const stoneBalance = typeof options?.stoneBalance === 'number' ? options.stoneBalance : Number(this.data.stoneBalance || 0);
        if (this.data.pricingError) {
            return '计费信息暂不可用，请稍后重试。';
        }
        if (!pricingLoaded || unitCost <= 0) {
            return '正在获取生成费用，请稍候。';
        }
        if (balanceLoaded) {
            if (stoneBalance >= currentCost) {
                return `当前余额 ${stoneBalance} 灵石，生成后预计剩余 ${stoneBalance - currentCost} 灵石。`;
            }
            return `当前余额 ${stoneBalance} 灵石，本次需 ${currentCost} 灵石，还差 ${currentCost - stoneBalance} 灵石。`;
        }
        return `本次需 ${currentCost} 灵石。`;
    },
    syncCurrentCost(callback) {
        const nextGenerateCount = this.getCurrentGenerateCount();
        const nextUnitCost = Number(this.data.unitCost || 0);
        const nextCost = nextUnitCost * nextGenerateCount;
        this.setData({
            selectedGenerateCount: nextGenerateCount,
            currentCost: nextCost,
            bottomTipText: this.buildBottomTipText({
                unitCost: nextUnitCost,
                currentCost: nextCost,
            }),
        }, callback);
    },
    async loadAIPricing() {
        this.setData({
            pricingLoading: true,
            pricingError: '',
            bottomTipText: '',
        });
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/ai/pricing?scenes=${TEXT_TO_IMAGE_SCENE},${IMAGE_TO_IMAGE_SCENE}`,
                    method: 'GET',
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                            }
                            else {
                                reject(new Error(data.msg || '获取 AI 计费失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const price = Number(res?.prices?.[this.getCurrentScene()]);
            const validPrice = Number.isFinite(price) && price > 0 ? price : 0;
            this.setData({
                unitCost: validPrice,
                pricingLoading: false,
                pricingLoaded: validPrice > 0,
                pricingError: validPrice > 0 ? '' : 'missing_pricing',
            }, () => this.syncCurrentCost());
        }
        catch (_error) {
            this.setData({
                pricingLoading: false,
                pricingLoaded: false,
                pricingError: 'request_failed',
            }, () => this.syncCurrentCost());
        }
    },
    async requestStoneBalance(token) {
        const res = await new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}/api/v1/miniprogram/user/stones`,
                method: 'GET',
                header: {
                    token,
                    'Content-Type': 'application/json',
                },
                success: (requestRes) => {
                    if (requestRes.statusCode === 200 && requestRes.data) {
                        const data = requestRes.data;
                        if (data.code === 0) {
                            resolve(data.data || {});
                        }
                        else {
                            reject(new Error(data.msg || '获取余额失败'));
                        }
                    }
                    else {
                        reject(new Error(`请求失败: ${requestRes.statusCode}`));
                    }
                },
                fail: reject,
            });
        });
        return Number(res?.stones || 0);
    },
    async loadStoneBalance() {
        const token = this.getToken();
        if (!token) {
            this.setData({
                stoneBalance: 0,
                balanceLoaded: false,
                bottomTipText: this.buildBottomTipText({ stoneBalance: 0, balanceLoaded: false }),
            });
            return;
        }
        try {
            const stones = await this.requestStoneBalance(token);
            this.setData({
                stoneBalance: stones,
                balanceLoaded: true,
                bottomTipText: this.buildBottomTipText({
                    stoneBalance: stones,
                    balanceLoaded: true,
                }),
            });
        }
        catch (_error) {
            this.setData({
                balanceLoaded: false,
                bottomTipText: this.buildBottomTipText({ balanceLoaded: false }),
            });
        }
    },
    async uploadImageFile(tempFilePath, token) {
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
                        if (data.code === 0 && data.data && data.data.url) {
                            resolve(String(data.data.url));
                        }
                        else {
                            reject(new Error(data.msg || '上传失败'));
                        }
                    }
                    catch (_error) {
                        reject(new Error('解析响应失败'));
                    }
                },
                fail: reject,
            });
        });
    },
    onUploadImage(e) {
        const target = String(e.currentTarget.dataset.target || '');
        if (target !== 'original' && target !== 'reference') {
            return;
        }
        const token = this.ensureToken();
        if (!token) {
            return;
        }
        wx.chooseMedia({
            count: 1,
            mediaType: ['image'],
            sourceType: ['camera', 'album'],
            success: async (res) => {
                const tempFiles = (res.tempFiles || []);
                const tempFilePath = String(tempFiles[0]?.tempFilePath || '');
                if (!tempFilePath) {
                    wx.showToast({ title: '请选择图片', icon: 'none' });
                    return;
                }
                const uploadingKey = target === 'original' ? 'uploadingOriginal' : 'uploadingReference';
                const imageKey = target === 'original' ? 'originalImageUrl' : 'referenceImageUrl';
                this.setData({ [uploadingKey]: true });
                wx.showLoading({ title: '上传中...', mask: true });
                try {
                    const uploadedURL = await this.uploadImageFile(tempFilePath, token);
                    this.setData({ [imageKey]: uploadedURL });
                }
                catch (error) {
                    wx.showToast({ title: error?.message || '上传失败', icon: 'none' });
                }
                finally {
                    wx.hideLoading();
                    this.setData({ [uploadingKey]: false });
                }
            },
            fail: (err) => {
                if (String(err?.errMsg || '').includes('cancel')) {
                    return;
                }
                wx.showToast({ title: '选择图片失败', icon: 'none' });
            },
        });
    },
    onTapUploadImage(e) {
        const url = String(e.currentTarget.dataset.url || '');
        if (url) {
            this.onPreviewUploadImage(e);
            return;
        }
        this.onUploadImage(e);
    },
    onPreviewUploadImage(e) {
        const url = String(e.currentTarget.dataset.url || '');
        if (!url) {
            return;
        }
        wx.previewImage({ urls: [url], current: url });
    },
    onRemoveUploadImage(e) {
        const target = String(e.currentTarget.dataset.target || '');
        if (target === 'original') {
            this.setData({ originalImageUrl: '' });
        }
        else if (target === 'reference') {
            this.setData({ referenceImageUrl: '' });
        }
    },
    onSelectMode(e) {
        const mode = String(e.currentTarget.dataset.mode || '');
        if (!mode || mode === this.data.activeMode) {
            return;
        }
        this.setData({ activeMode: mode }, () => {
            this.loadAIPricing();
            this.syncCurrentCost();
        });
    },
    onSelectCanvas(e) {
        const key = String(e.currentTarget.dataset.key || '');
        if (!key || key === this.data.selectedCanvasKey) {
            return;
        }
        this.setData({ selectedCanvasKey: key });
    },
    onCustomDimensionInput(e) {
        const field = String(e.currentTarget.dataset.field || '');
        const value = String(e.detail.value || '').replace(/[^\d]/g, '');
        if (field === 'width') {
            this.setData({ customWidth: value });
        }
        else if (field === 'height') {
            this.setData({ customHeight: value });
        }
    },
    onSelectGenerateCount(e) {
        const value = Number(e.currentTarget.dataset.count);
        if (!Number.isFinite(value) || value <= 0) {
            return;
        }
        const nextGenerateCount = Math.min(3, Math.max(1, Math.floor(value)));
        if (nextGenerateCount === this.data.selectedGenerateCount) {
            return;
        }
        this.setData({ selectedGenerateCount: nextGenerateCount }, () => this.syncCurrentCost());
    },
    onPromptInput(e) {
        this.setData({ promptText: String(e.detail.value || '') });
    },
    async onOptimizePrompt() {
        if (this.data.optimizingPrompt)
            return;
        const rawPrompt = (this.data.promptText || '').trim();
        if (!rawPrompt) {
            wx.showToast({ title: '请先输入提示词', icon: 'none' });
            return;
        }
        this.setData({ optimizingPrompt: true });
        const { optimizePrompt } = require('../../utils/promptOptimize');
        const result = await optimizePrompt(rawPrompt, 'architecture');
        this.setData({ optimizingPrompt: false });
        if (result.success && result.optimizedPrompt) {
            this.setData({ promptText: result.optimizedPrompt });
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
    async onStartCreate() {
        if (this.data.generating) {
            return;
        }
        const promptText = String(this.data.promptText || '').trim();
        if (!promptText) {
            wx.showToast({
                title: '请先输入画面描述',
                icon: 'none',
            });
            return;
        }
        if ((0, promptReview_1.blockUnsafeText)(promptText, { scene: 'prompt' })) {
            return;
        }
        const token = this.ensureToken();
        if (!token) {
            return;
        }
        if (!this.data.pricingLoaded || this.data.unitCost <= 0 || this.data.currentCost <= 0) {
            wx.showToast({
                title: '计费信息暂不可用',
                icon: 'none',
            });
            this.loadAIPricing();
            return;
        }
        try {
            const stones = await this.requestStoneBalance(token);
            this.setData({ stoneBalance: stones, balanceLoaded: true }, () => this.syncCurrentCost());
            if (stones < this.data.currentCost) {
                wx.showModal({
                    title: '余额不足',
                    content: `当前余额：${stones}灵石，需要：${this.data.currentCost}灵石`,
                    confirmText: '去充值',
                    success: (res) => {
                        if (res.confirm) {
                            wx.navigateTo({ url: '/pages/topupcenter/topupcenter' });
                        }
                    },
                });
                return;
            }
        }
        catch (_error) {
            wx.showToast({
                title: '检查余额失败',
                icon: 'none',
            });
            return;
        }
        const isImageToImage = this.data.activeMode === 'image_to_image';
        const originalImageUrl = String(this.data.originalImageUrl || '').trim();
        const referenceImageUrl = String(this.data.referenceImageUrl || '').trim();
        const canvas = this.getSelectedCanvas();
        if (isImageToImage) {
            if (!originalImageUrl) {
                wx.showToast({ title: '请先上传原图', icon: 'none' });
                return;
            }
            if (!referenceImageUrl) {
                wx.showToast({ title: '请先上传参考图', icon: 'none' });
                return;
            }
        }
        else if (!canvas.width || !canvas.height) {
            wx.showToast({
                title: '请输入自定义尺寸',
                icon: 'none',
            });
            return;
        }
        const generateCount = this.getCurrentGenerateCount();
        const tool = this.data.tool || NEW_MINNAN_TEMPLATE_TOOL;
        const payload = {
            tool_code: tool.code,
            tool_name: tool.name,
            user_prompt: promptText,
            batch_size: generateCount,
            generate_count: generateCount,
            main_tab: 'style',
            sub_tab: 'new_minnan',
        };
        if (isImageToImage) {
            payload.original_image_url = originalImageUrl;
            payload.reference_image_url = referenceImageUrl;
            payload.image_url = originalImageUrl;
            payload.images = [originalImageUrl, referenceImageUrl];
        }
        else {
            payload.width = canvas.width;
            payload.height = canvas.height;
        }
        const scene = this.getCurrentScene();
        const requestBody = { scene, payload };
        const apiPath = '/api/v1/miniprogram/ai/draw';
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
        const headers = (0, parameter_1.paramsToHeaders)(params);
        this.setData({ generating: true });
        wx.showLoading({ title: '创作中...', mask: true });
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: {
                        ...headers,
                        'Content-Type': 'application/json',
                    },
                    data: requestBody,
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '生成失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            if (!res?.task_no) {
                throw new Error('生成失败');
            }
            const taskNo = encodeURIComponent(String(res.task_no));
            const safePrompt = encodeURIComponent(promptText);
            wx.navigateTo({
                url: `/pages/generatedetails/generatedetails?task_no=${taskNo}&prompt=${safePrompt}&source=new_minnan_tool`,
                success: (navRes) => {
                    navRes.eventChannel.emit('taskData', {
                        id: 0,
                        task_no: res.task_no,
                        scene,
                        status: 'pending',
                        requested_count: generateCount,
                        generated_count: 0,
                        stones_used: this.data.currentCost,
                        result: {},
                        error_message: '',
                        created_at: new Date().toISOString(),
                        updated_at: new Date().toISOString(),
                        task_type: 'ai_draw',
                        prompt: promptText,
                        user_prompt: promptText,
                        quality: 'uhd',
                        canvas: isImageToImage ? 'image_to_image' : `${canvas.width}x${canvas.height}`,
                        tool_name: tool.name,
                    });
                },
                fail: () => {
                    wx.showToast({
                        title: '页面跳转失败',
                        icon: 'none',
                    });
                },
            });
        }
        catch (error) {
            wx.showToast({
                title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(error?.message || '生成失败'),
                icon: 'none',
                duration: 2000,
            });
        }
        finally {
            wx.hideLoading();
            this.setData({ generating: false });
            this.loadStoneBalance();
        }
    },
    onShareAppMessage() {
        const tool = this.data.tool;
        return {
            title: tool ? `${tool.name} · 开始使用` : '新闽派工具',
            path: '/pages/newminnantool/newminnantool?source=template_square',
        };
    },
});
