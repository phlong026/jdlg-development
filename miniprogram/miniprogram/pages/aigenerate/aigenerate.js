"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/aigenerate/aigenerate.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const asset_1 = require("../../utils/asset");
const aiError_1 = require("../../utils/aiError");
const promptReview_1 = require("../../utils/promptReview");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_SERVICE_TYPE = 'normal';
const DEFAULT_SERVICE = 'normal_style_change';
const DEFAULT_SERVICE_LABEL = '效果图';
const DEFAULT_DRAW_SINGLE_COST = 0;
const DEFAULT_DRAW_MULTI_COST = 0;
const DEFAULT_VIDEO_COST = 30;
const DEFAULT_SELECTED_GENERATE_COUNT = 1;
const MAX_REFERENCE_IMAGE_COUNT = 6;
const FIXED_ORIGINAL_IMAGE_COUNT = 2;
const MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT = 4;
const GENERATE_DRAFT_STORAGE_KEY = 'jdlg_ai_generate_local_draft_v1';
function getTaskStatusPollDelay(attempt) {
    if (attempt < 2) {
        return 3000;
    }
    if (attempt < 6) {
        return 4000;
    }
    return 5000;
}
const EXTERIOR_STYLE_OPTIONS = [
    { name: '新闽派', subtitle: '地域外立面', accent: '#c88d67' },
    { name: '新中式', subtitle: '东方雅致', accent: '#b77c54' },
    { name: '现代简约', subtitle: '利落克制', accent: '#8d7b70' },
    { name: '海派', subtitle: '都市融合', accent: '#b56f5a' },
    { name: '欧式', subtitle: '经典庄重', accent: '#9e8360' },
    { name: '地域特色', subtitle: '本土表达', accent: '#a77a54' },
];
const INTERIOR_STYLE_OPTIONS = [
    { name: '新中式', subtitle: '东方意境', accent: '#b67b53' },
    { name: '现代简约', subtitle: '纯净留白', accent: '#8c7c72' },
    { name: '轻奢', subtitle: '精致质感', accent: '#b59273' },
    { name: '奶油', subtitle: '柔和温润', accent: '#d0ad89' },
    { name: '工业', subtitle: '个性结构', accent: '#7d756e' },
    { name: '侘寂', subtitle: '安静自然', accent: '#95856f' },
    { name: '原木', subtitle: '温暖天然', accent: '#aa845c' },
    { name: '中古', subtitle: '复古格调', accent: '#8f6c55' },
    { name: '日式', subtitle: '简净松弛', accent: '#b79a78' },
    { name: '欧式', subtitle: '典雅层次', accent: '#a38764' },
    { name: '意式', subtitle: '现代艺术', accent: '#8d6b58' },
    { name: '法式', subtitle: '浪漫优雅', accent: '#be8a74' },
    { name: '自然', subtitle: '松弛通透', accent: '#90a07e' },
];
function sanitizeIncomingPrompt(rawPrompt) {
    const text = String(rawPrompt || '').trim();
    if (!text) {
        return '';
    }
    return text
        .replace(/请帮我生成图片，如果用户上传了参考图，同时你自己的库里面也有用户上传的类似地标，或建筑，或什么别的东西的图，以用户上传的为主/g, '')
        .replace(/，?生成方向：[^，。；;]+/g, '')
        .replace(/，?画面风格：[^，。；;]+/g, '')
        .replace(/，?画面清晰度：[^，。；;]+/g, '')
        .replace(/，?画布大小：[^，。；;]+/g, '')
        .replace(/[，。；;\s]+$/g, '')
        .replace(/^[，。；;\s]+/g, '')
        .replace(/[，。；;]{2,}/g, '，')
        .trim();
}
function normalizeImageUrls(values, maxCount = MAX_REFERENCE_IMAGE_COUNT) {
    return values
        .map((item) => (typeof item === 'string' ? item.trim() : ''))
        .filter((item, index, array) => {
        if (!item) {
            return false;
        }
        if (!item.startsWith('http://') && !item.startsWith('https://')) {
            return false;
        }
        return array.indexOf(item) === index;
    })
        .slice(0, maxCount);
}
function normalizeReferenceImageUrls(values, maxCount = MAX_REFERENCE_IMAGE_COUNT) {
    return normalizeImageUrls(values, maxCount);
}
function normalizeOriginalImageUrls(values) {
    return normalizeImageUrls(values, FIXED_ORIGINAL_IMAGE_COUNT);
}
function buildOriginalImageSlots(values) {
    const normalized = normalizeOriginalImageUrls(values);
    const slots = new Array(FIXED_ORIGINAL_IMAGE_COUNT).fill('');
    normalized.forEach((item, index) => {
        if (index < FIXED_ORIGINAL_IMAGE_COUNT) {
            slots[index] = item;
        }
    });
    return slots;
}
function buildOrderedImageUrls(originalImages, referenceImages) {
    return normalizeImageUrls([
        ...normalizeOriginalImageUrls(originalImages),
        ...normalizeReferenceImageUrls(referenceImages, MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT),
    ], MAX_REFERENCE_IMAGE_COUNT);
}
function extractImageSlotsPrefill(source = {}) {
    const originalImages = buildOriginalImageSlots(Array.isArray(source.original_image_urls) ? source.original_image_urls : []);
    const orderedImages = normalizeImageUrls([
        ...(Array.isArray(source.ordered_image_urls) ? source.ordered_image_urls : []),
        ...(Array.isArray(source.image_urls) ? source.image_urls : []),
        ...(Array.isArray(source.reference_image_urls) ? source.reference_image_urls : []),
        source.reference_image_url,
    ]);
    const fallbackReferenceImages = orderedImages.filter((item) => originalImages.indexOf(item) === -1);
    const referenceImages = normalizeReferenceImageUrls(Array.isArray(source.reference_image_urls) && source.reference_image_urls.length > 0
        ? source.reference_image_urls
        : fallbackReferenceImages, MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT);
    return {
        originalImages,
        referenceImages,
        orderedImages: buildOrderedImageUrls(originalImages, referenceImages).length > 0
            ? buildOrderedImageUrls(originalImages, referenceImages)
            : orderedImages,
    };
}
function parseIncomingReferenceImageUrls(options) {
    const values = [];
    if (options.reference_image_urls) {
        try {
            const parsed = JSON.parse(decodeURIComponent(options.reference_image_urls));
            if (Array.isArray(parsed)) {
                values.push(...parsed);
            }
        }
        catch (error) {
            console.warn('解析 reference_image_urls 失败:', error);
        }
    }
    if (options.reference_image_url) {
        try {
            values.push(decodeURIComponent(options.reference_image_url));
        }
        catch (error) {
            console.warn('解析 reference_image_url 失败:', error);
        }
    }
    return normalizeReferenceImageUrls(values);
}
function mergeReferenceImageUrls(...groups) {
    const merged = [];
    groups.forEach((group) => {
        if (Array.isArray(group)) {
            merged.push(...group);
        }
    });
    return normalizeReferenceImageUrls(merged);
}
function isReferencePrioritySource(source) {
    return source === 'customer_style_reference';
}
function pickPreferredPrompt(currentPrompt, incomingPrompt) {
    const current = sanitizeIncomingPrompt(currentPrompt);
    const incoming = sanitizeIncomingPrompt(incomingPrompt);
    if (!incoming) {
        return current;
    }
    if (!current) {
        return incoming;
    }
    return incoming.length >= current.length ? incoming : current;
}
function moveImageItem(list, fromIndex, toIndex) {
    const nextList = [...list];
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= nextList.length || toIndex >= nextList.length) {
        return nextList;
    }
    const [target] = nextList.splice(fromIndex, 1);
    nextList.splice(toIndex, 0, target);
    return nextList;
}
Page({
    data: {
        selectedType: DEFAULT_SERVICE_TYPE,
        selectedService: DEFAULT_SERVICE,
        styleOptions: EXTERIOR_STYLE_OPTIONS,
        selectedStyle: '',
        qualityOptions: [
            { label: '2K', value: 'hd', desc: '适合展示' },
            { label: '4K', value: 'uhd', desc: '适合打印' }
        ],
        selectedQuality: 'uhd',
        canvasOptions: [
            { size: '16:9', value: '16:9', desc: '横屏' },
            { size: '1:1', value: '1:1', desc: '方图' },
            { size: '9:16', value: '9:16', desc: '竖屏' },
        ],
        selectedCanvas: '16:9',
        uploadedImages: [],
        originalImages: buildOriginalImageSlots([]),
        referenceImages: [],
        useFixedSlots: false,
        originalSlotIndices: [0, 1],
        originalSlotLabels: ['原1', '原2'],
        referenceSlotIndices: [0, 1, 2, 3],
        referenceSlotLabels: ['参1', '参2', '参3', '参4'],
        uploadingRefs: false,
        draggingImageIndex: -1,
        dragOverImageIndex: -1,
        promptText: '',
        promptDirty: false,
        optimizingPrompt: false,
        templateId: 0,
        navSafeTop: 0,
        navBarHeight: 96,
        navContentHeight: 44,
        navSideWidth: 88,
        entrySource: '',
        showSceneTabs: false,
        selectedSceneTab: 'exterior',
        generating: false,
        drawSingleCost: DEFAULT_DRAW_SINGLE_COST,
        drawMultiCost: DEFAULT_DRAW_MULTI_COST,
        videoCost: DEFAULT_VIDEO_COST,
        currentUnitCost: DEFAULT_DRAW_SINGLE_COST,
        pricingLoaded: false,
        selectedGenerateCount: DEFAULT_SELECTED_GENERATE_COUNT,
        generateCountOptions: [1, 2, 3],
        currentCost: DEFAULT_DRAW_SINGLE_COST * DEFAULT_SELECTED_GENERATE_COUNT,
        hasShownGenerateTip: false,
        showResultModal: false,
        resultImageUrl: '',
        resultTaskNo: '',
        showProgressModal: false,
        progressPercent: 0,
        uploadCardImage: (0, asset_1.resolveAssetPath)('/assets/aigenerate/拍照上传.png'),
        styleCardImage: (0, asset_1.resolveAssetPath)('/assets/aigenerate/选个风格.png'),
    },
    pollingTimer: null,
    pollingAttempt: 0,
    progressTimer: null,
    lastPreviewTapAt: 0,
    lastPreviewTapIndex: -1,
    lastPreviewTapScope: 'uploaded',
    imageDragActive: false,
    imageDragStartIndex: -1,
    imageGridRect: null,
    imageItemRect: null,
    async onLoad(options) {
        this.initNavLayout();
        this.loadAIPricing();
        const source = String(options.source || '');
        const useFixedSlots = source === 'make_same';
        const referencePriorityMode = isReferencePrioritySource(source);
        const showSceneTabs = source === 'rural_villa'
            ? true
            : source === 'index' || source === 'urban_renewal'
                ? false
                : options.showSceneTabs === '1';
        const selectedSceneTab = options.tab === 'interior' ? 'interior' : 'exterior';
        this.setData({
            entrySource: source,
            showSceneTabs,
            selectedSceneTab,
            useFixedSlots,
            originalImages: buildOriginalImageSlots([]),
            referenceImages: [],
            uploadedImages: [],
        }, () => this.syncStyleOptions());
        const eventChannel = this.getOpenerEventChannel();
        eventChannel.on('prefillGenerateData', (prefill) => {
            this.applyPrefillData(prefill);
        });
        if (options.draft === '1' && this.restoreGenerateDraft()) {
            return;
        }
        if (options.prompt && !this.data.promptDirty) {
            this.setData({
                promptText: pickPreferredPrompt(this.data.promptText, decodeURIComponent(options.prompt))
            }, () => this.saveGenerateDraft());
        }
        const incomingReferenceImages = parseIncomingReferenceImageUrls(options);
        if (incomingReferenceImages.length > 0) {
            this.applyPrefillData({
                reference_image_url: incomingReferenceImages[0] || '',
                reference_image_urls: incomingReferenceImages,
                style_reference_mode: referencePriorityMode,
                style_reference_priority: referencePriorityMode ? 'high' : '',
            });
        }
        if (options.templateId) {
            const templateId = parseInt(options.templateId);
            if (templateId > 0) {
                this.setData({ templateId });
                await this.loadTemplateOriginalTask(templateId);
            }
        }
        if (options.taskId) {
            const taskId = parseInt(options.taskId);
            if (taskId > 0) {
                await this.loadTaskInfo(taskId);
            }
        }
    },
    buildGenerateDraftData() {
        return {
            updated_at: new Date().toISOString(),
            source: String(this.data.entrySource || ''),
            showSceneTabs: !!this.data.showSceneTabs,
            selectedSceneTab: this.data.selectedSceneTab === 'interior' ? 'interior' : 'exterior',
            selectedType: String(this.data.selectedType || DEFAULT_SERVICE_TYPE),
            selectedService: String(this.data.selectedService || DEFAULT_SERVICE),
            selectedStyle: String(this.data.selectedStyle || ''),
            selectedQuality: String(this.data.selectedQuality || 'uhd'),
            selectedCanvas: String(this.data.selectedCanvas || '16:9'),
            selectedGenerateCount: this.getCurrentGenerateCount(),
            promptText: String(this.data.promptText || '').trim(),
            useFixedSlots: !!this.data.useFixedSlots,
            originalImages: buildOriginalImageSlots(this.data.originalImages || []),
            referenceImages: normalizeReferenceImageUrls(this.data.referenceImages || [], MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT),
            uploadedImages: normalizeReferenceImageUrls(this.data.uploadedImages || []),
            templateId: Number(this.data.templateId || 0),
        };
    },
    hasGenerateDraftContent(draft) {
        const hasImages = [
            ...draft.originalImages,
            ...draft.referenceImages,
            ...draft.uploadedImages,
        ].some((item) => typeof item === 'string' && item.trim() !== '');
        return !!draft.promptText
            || hasImages
            || !!draft.selectedStyle
            || draft.selectedQuality !== 'uhd'
            || draft.selectedCanvas !== '16:9'
            || draft.selectedGenerateCount !== DEFAULT_SELECTED_GENERATE_COUNT
            || draft.useFixedSlots
            || draft.templateId > 0;
    },
    saveGenerateDraft() {
        if (this.data.generating) {
            return;
        }
        const draft = this.buildGenerateDraftData();
        if (!this.hasGenerateDraftContent(draft)) {
            wx.removeStorageSync(GENERATE_DRAFT_STORAGE_KEY);
            return;
        }
        wx.setStorageSync(GENERATE_DRAFT_STORAGE_KEY, draft);
    },
    clearGenerateDraft() {
        wx.removeStorageSync(GENERATE_DRAFT_STORAGE_KEY);
    },
    restoreGenerateDraft() {
        const rawDraft = wx.getStorageSync(GENERATE_DRAFT_STORAGE_KEY);
        if (!rawDraft || typeof rawDraft !== 'object') {
            wx.showToast({ title: '暂无可恢复草稿', icon: 'none' });
            return false;
        }
        const useFixedSlots = !!rawDraft.useFixedSlots;
        const originalImages = buildOriginalImageSlots(rawDraft.originalImages || []);
        const referenceImages = normalizeReferenceImageUrls(rawDraft.referenceImages || [], MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT);
        const uploadedImages = useFixedSlots
            ? buildOrderedImageUrls(originalImages, referenceImages)
            : normalizeReferenceImageUrls(rawDraft.uploadedImages || rawDraft.referenceImages || []);
        this.setData({
            entrySource: String(rawDraft.source || ''),
            showSceneTabs: !!rawDraft.showSceneTabs,
            selectedSceneTab: rawDraft.selectedSceneTab === 'interior' ? 'interior' : 'exterior',
            selectedType: String(rawDraft.selectedType || DEFAULT_SERVICE_TYPE),
            selectedService: String(rawDraft.selectedService || DEFAULT_SERVICE),
            selectedStyle: String(rawDraft.selectedStyle || ''),
            selectedQuality: String(rawDraft.selectedQuality || 'uhd'),
            selectedCanvas: String(rawDraft.selectedCanvas || '16:9'),
            selectedGenerateCount: Math.min(3, Math.max(1, Math.floor(Number(rawDraft.selectedGenerateCount || DEFAULT_SELECTED_GENERATE_COUNT)))),
            promptText: String(rawDraft.promptText || ''),
            promptDirty: !!String(rawDraft.promptText || '').trim(),
            useFixedSlots,
            originalImages,
            referenceImages,
            uploadedImages,
            templateId: Number(rawDraft.templateId || 0),
        }, () => {
            this.syncStyleOptions();
            this.syncCurrentCost();
        });
        return true;
    },
    getOrderedImages() {
        if (this.data.useFixedSlots) {
            return buildOrderedImageUrls(this.data.originalImages, this.data.referenceImages);
        }
        return normalizeReferenceImageUrls(this.data.uploadedImages);
    },
    applyImageCollectionState(nextValues, callback) {
        const nextUseFixedSlots = typeof nextValues.useFixedSlots === 'boolean'
            ? nextValues.useFixedSlots
            : this.data.useFixedSlots;
        const nextOriginalImages = nextUseFixedSlots
            ? buildOriginalImageSlots(nextValues.originalImages ?? this.data.originalImages)
            : buildOriginalImageSlots([]);
        const nextReferenceImages = nextUseFixedSlots
            ? normalizeReferenceImageUrls(nextValues.referenceImages ?? this.data.referenceImages, MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT)
            : normalizeReferenceImageUrls(nextValues.referenceImages ?? nextValues.uploadedImages ?? this.data.uploadedImages);
        const nextUploadedImages = nextUseFixedSlots
            ? buildOrderedImageUrls(nextOriginalImages, nextReferenceImages)
            : normalizeReferenceImageUrls(nextValues.uploadedImages ?? nextReferenceImages);
        this.setData({
            ...nextValues,
            useFixedSlots: nextUseFixedSlots,
            originalImages: nextOriginalImages,
            referenceImages: nextReferenceImages,
            uploadedImages: nextUploadedImages,
        }, () => {
            this.syncCurrentCost();
            this.saveGenerateDraft();
            if (callback) {
                callback();
            }
        });
    },
    applyPrefillData(prefill = {}) {
        const nextData = {};
        const prompt = typeof prefill.prompt === 'string' ? sanitizeIncomingPrompt(prefill.prompt) : '';
        if (prompt && !this.data.promptDirty) {
            nextData.promptText = pickPreferredPrompt(this.data.promptText, prompt);
        }
        if (this.data.useFixedSlots) {
            const slots = extractImageSlotsPrefill(prefill);
            const mergedOriginalImages = buildOriginalImageSlots([
                ...(Array.isArray(this.data.originalImages) ? this.data.originalImages : []),
                ...slots.originalImages,
            ]);
            const mergedReferenceImages = normalizeReferenceImageUrls([
                ...(Array.isArray(this.data.referenceImages) ? this.data.referenceImages : []),
                ...slots.referenceImages,
            ], MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT);
            this.applyImageCollectionState({
                ...nextData,
                originalImages: mergedOriginalImages,
                referenceImages: mergedReferenceImages,
            });
            return;
        }
        const incomingImages = normalizeReferenceImageUrls([
            ...(Array.isArray(prefill.reference_image_urls) ? prefill.reference_image_urls : []),
            prefill.reference_image_url,
        ]);
        if (incomingImages.length > 0) {
            if (prefill.style_reference_mode || isReferencePrioritySource(String(this.data.entrySource || ''))) {
                nextData.referenceImages = mergeReferenceImageUrls(this.data.referenceImages, incomingImages);
                nextData.uploadedImages = mergeReferenceImageUrls(this.data.uploadedImages, incomingImages);
            }
            else {
                nextData.uploadedImages = mergeReferenceImageUrls(this.data.uploadedImages, incomingImages);
            }
        }
        if (Object.keys(nextData).length > 0) {
            this.applyImageCollectionState(nextData);
        }
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
    getCurrentGenerateCount() {
        const currentValue = Number(this.data.selectedGenerateCount);
        if (!Number.isFinite(currentValue) || currentValue <= 0) {
            return DEFAULT_SELECTED_GENERATE_COUNT;
        }
        return Math.min(3, Math.max(1, Math.floor(currentValue)));
    },
    getAvailableStyleOptions(source, sceneTab) {
        const currentSource = String(source || this.data.entrySource || '').trim();
        const currentSceneTab = sceneTab || this.data.selectedSceneTab || 'exterior';
        if (currentSource === 'rural_villa' && currentSceneTab === 'interior') {
            return INTERIOR_STYLE_OPTIONS;
        }
        return EXTERIOR_STYLE_OPTIONS;
    },
    syncStyleOptions(source, sceneTab) {
        const options = this.getAvailableStyleOptions(source, sceneTab);
        const selectedStyle = options.some((item) => item.name === this.data.selectedStyle)
            ? this.data.selectedStyle
            : '';
        this.setData({
            styleOptions: options,
            selectedStyle,
        });
    },
    getCurrentSceneUnitCost() {
        const uploadedCount = (this.data.uploadedImages || []).length;
        return uploadedCount > 1 ? this.data.drawMultiCost : this.data.drawSingleCost;
    },
    getCurrentSceneCost() {
        return this.getCurrentSceneUnitCost() * this.getCurrentGenerateCount();
    },
    syncCurrentCost() {
        const nextGenerateCount = this.getCurrentGenerateCount();
        const nextUnitCost = this.getCurrentSceneUnitCost();
        const nextCost = this.getCurrentSceneCost();
        if (nextCost !== this.data.currentCost || nextUnitCost !== this.data.currentUnitCost || nextGenerateCount !== this.data.selectedGenerateCount) {
            this.setData({
                selectedGenerateCount: nextGenerateCount,
                currentUnitCost: nextUnitCost,
                currentCost: nextCost,
            });
        }
    },
    async loadAIPricing() {
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/ai/pricing?scenes=ai_draw_single,ai_draw_multi,ai_video_1`,
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
            const prices = res?.prices || {};
            const nextSingleCost = Number(prices.ai_draw_single);
            const nextMultiCost = Number(prices.ai_draw_multi);
            const nextVideoCost = Number(prices.ai_video_1);
            this.setData({
                drawSingleCost: Number.isFinite(nextSingleCost) && nextSingleCost > 0 ? nextSingleCost : DEFAULT_DRAW_SINGLE_COST,
                drawMultiCost: Number.isFinite(nextMultiCost) && nextMultiCost > 0 ? nextMultiCost : DEFAULT_DRAW_MULTI_COST,
                videoCost: Number.isFinite(nextVideoCost) && nextVideoCost > 0 ? nextVideoCost : DEFAULT_VIDEO_COST,
                pricingLoaded: Number.isFinite(nextSingleCost) && nextSingleCost > 0 && Number.isFinite(nextMultiCost) && nextMultiCost > 0,
            }, () => this.syncCurrentCost());
        }
        catch (error) {
            this.setData({ pricingLoaded: false });
            this.syncCurrentCost();
        }
    },
    onSelectStyle(e) {
        const style = String(e.currentTarget.dataset.style || '');
        this.setData({
            selectedStyle: style
        }, () => this.saveGenerateDraft());
    },
    onSelectQuality(e) {
        const value = e.currentTarget.dataset.value;
        this.setData({
            selectedQuality: value
        }, () => this.saveGenerateDraft());
    },
    onSelectCanvas(e) {
        const value = e.currentTarget.dataset.value;
        this.setData({
            selectedCanvas: value
        }, () => this.saveGenerateDraft());
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
        this.setData({
            selectedGenerateCount: nextGenerateCount,
        }, () => {
            this.syncCurrentCost();
            this.saveGenerateDraft();
        });
    },
    onSelectSceneTab(e) {
        const value = e.currentTarget.dataset.tab === 'interior' ? 'interior' : 'exterior';
        this.setData({
            selectedSceneTab: value,
        }, () => {
            this.syncStyleOptions(undefined, value);
            this.saveGenerateDraft();
        });
    },
    onOpenVideoPage() {
        const prompt = (this.data.promptText || '').trim();
        if (prompt && (0, promptReview_1.blockUnsafeText)(prompt, { scene: 'prompt' })) {
            return;
        }
        const url = prompt
            ? `/pages/aivideo/aivideo?prompt=${encodeURIComponent(prompt)}`
            : '/pages/aivideo/aivideo';
        wx.navigateTo({
            url,
            fail: () => {
                wx.showToast({
                    title: '跳转失败',
                    icon: 'none'
                });
            }
        });
    },
    async uploadReferenceImage(tempFilePath) {
        const token = wx.getStorageSync('token');
        if (!token)
            throw new Error('未登录');
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
    onUploadImage() {
        this.onUploadReferenceImage();
    },
    onUploadReferenceImage() {
        const maxCount = this.data.useFixedSlots ? MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT : MAX_REFERENCE_IMAGE_COUNT;
        const currentCount = this.data.useFixedSlots
            ? (this.data.referenceImages || []).length
            : (this.data.uploadedImages || []).length;
        const left = maxCount - currentCount;
        if (left <= 0) {
            wx.showToast({ title: `最多只能上传${maxCount}张图片`, icon: 'none' });
            return;
        }
        this.doUploadImages(left, 'reference');
    },
    onUploadOriginalImage(e) {
        const index = Number(e.currentTarget.dataset.index);
        if (!this.data.useFixedSlots || !Number.isFinite(index) || index < 0 || index >= FIXED_ORIGINAL_IMAGE_COUNT) {
            return;
        }
        this.doUploadImages(1, 'original', index);
    },
    onOriginalSlotTap(e) {
        const index = Number(e.currentTarget.dataset.index);
        if (!Number.isFinite(index) || index < 0 || index >= FIXED_ORIGINAL_IMAGE_COUNT) {
            return;
        }
        if (String((this.data.originalImages || [])[index] || '').trim()) {
            this.onPreviewImageTap({
                currentTarget: {
                    dataset: {
                        index,
                        scope: 'original',
                    }
                }
            });
            return;
        }
        this.onUploadOriginalImage(e);
    },
    onReferenceSlotTap(e) {
        const index = Number(e.currentTarget.dataset.index);
        if (!Number.isFinite(index) || index < 0) {
            return;
        }
        if (String((this.data.referenceImages || [])[index] || '').trim()) {
            this.onPreviewImageTap({
                currentTarget: {
                    dataset: {
                        index,
                        scope: 'reference',
                        type: 'reference',
                    }
                }
            });
            return;
        }
        this.onUploadReferenceImage();
    },
    async doUploadImages(maxCount, target = 'reference', slotIndex = -1) {
        wx.chooseMedia({
            count: target === 'original' ? 1 : maxCount,
            mediaType: ['image'],
            sourceType: ['camera', 'album'],
            success: async (res) => {
                const tempFiles = res.tempFiles;
                const pickedFiles = tempFiles.slice(0, target === 'original' ? 1 : maxCount);
                this.setData({
                    uploadingRefs: true,
                });
                wx.showLoading({ title: target === 'original' ? '上传原始图中...' : '上传参考图中...', mask: true });
                const urls = [];
                for (let i = 0; i < pickedFiles.length; i++) {
                    try {
                        const url = await this.uploadReferenceImage(pickedFiles[i].tempFilePath);
                        urls.push(url);
                    }
                    catch (e) {
                        console.error('参考图上传失败', e);
                        wx.showToast({ title: e.message || '上传失败', icon: 'none' });
                        break;
                    }
                }
                wx.hideLoading();
                if (urls.length > 0) {
                    if (target === 'original' && this.data.useFixedSlots) {
                        const nextOriginalImages = buildOriginalImageSlots(this.data.originalImages);
                        if (slotIndex >= 0 && slotIndex < FIXED_ORIGINAL_IMAGE_COUNT) {
                            nextOriginalImages[slotIndex] = urls[0];
                        }
                        this.applyImageCollectionState({
                            originalImages: nextOriginalImages,
                            uploadingRefs: false,
                        });
                        return;
                    }
                    if (this.data.useFixedSlots) {
                        const nextReferenceImages = normalizeReferenceImageUrls([
                            ...(this.data.referenceImages || []),
                            ...urls,
                        ], MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT);
                        this.applyImageCollectionState({
                            referenceImages: nextReferenceImages,
                            uploadingRefs: false,
                        });
                        return;
                    }
                    const newImages = normalizeReferenceImageUrls([
                        ...(this.data.uploadedImages || []),
                        ...urls,
                    ]);
                    this.applyImageCollectionState({
                        uploadedImages: newImages,
                        uploadingRefs: false,
                    });
                }
                else {
                    this.setData({ uploadingRefs: false });
                }
            },
            fail: (err) => {
                console.error('选择图片失败', err);
                wx.showToast({ title: '选择图片失败', icon: 'none' });
            },
        });
    },
    measureUploadedImageGrid() {
        const gridSelector = this.data.useFixedSlots ? '.reference-uploaded-images' : '.uploaded-images';
        const itemSelector = `${gridSelector} .image-item`;
        return new Promise((resolve) => {
            wx.createSelectorQuery()
                .in(this)
                .select(gridSelector)
                .boundingClientRect()
                .select(itemSelector)
                .boundingClientRect()
                .exec((result) => {
                resolve({
                    gridRect: (result?.[0] || null),
                    itemRect: (result?.[1] || null),
                });
            });
        });
    },
    getDragTouchPoint(e) {
        return (e?.touches && e.touches[0]) || (e?.changedTouches && e.changedTouches[0]) || null;
    },
    resolveDragTargetIndex(touch) {
        const images = this.data.useFixedSlots ? (this.data.referenceImages || []) : (this.data.uploadedImages || []);
        const gridRect = this.imageGridRect;
        const itemRect = this.imageItemRect;
        if (!touch || !gridRect || !itemRect || !images.length) {
            return this.imageDragStartIndex;
        }
        const pointX = Number(touch.pageX ?? touch.clientX ?? 0);
        const pointY = Number(touch.pageY ?? touch.clientY ?? 0);
        const relativeX = Math.max(0, pointX - Number(gridRect.left || 0));
        const relativeY = Math.max(0, pointY - Number(gridRect.top || 0));
        const columnWidth = Math.max(Number(itemRect.width || 0), Number(gridRect.width || 0) / 3 || 1);
        const rowHeight = Math.max(Number(itemRect.height || 0) + 14, 1);
        const col = Math.max(0, Math.min(2, Math.floor(relativeX / columnWidth)));
        const row = Math.max(0, Math.floor(relativeY / rowHeight));
        return Math.max(0, Math.min(images.length - 1, row * 3 + col));
    },
    resetImageDragState() {
        this.imageDragActive = false;
        this.imageDragStartIndex = -1;
        this.imageGridRect = null;
        this.imageItemRect = null;
        if (this.data.draggingImageIndex !== -1 || this.data.dragOverImageIndex !== -1) {
            this.setData({
                draggingImageIndex: -1,
                dragOverImageIndex: -1,
            });
        }
    },
    async onImageLongPress(e) {
        const index = Number(e.currentTarget.dataset.index);
        const type = String(e.currentTarget.dataset.type || (this.data.useFixedSlots ? 'reference' : 'uploaded'));
        if (!Number.isFinite(index) || index < 0 || this.data.uploadingRefs) {
            return;
        }
        if (type === 'reference' && !String((this.data.referenceImages || [])[index] || '').trim()) {
            return;
        }
        if (this.data.useFixedSlots && type !== 'reference') {
            return;
        }
        const rects = await this.measureUploadedImageGrid();
        if (!rects.gridRect || !rects.itemRect) {
            return;
        }
        this.imageDragActive = true;
        this.imageDragStartIndex = index;
        this.imageGridRect = rects.gridRect;
        this.imageItemRect = rects.itemRect;
        this.setData({
            draggingImageIndex: index,
            dragOverImageIndex: index,
        });
        if (typeof wx.vibrateShort === 'function') {
            wx.vibrateShort({ type: 'light' });
        }
    },
    onImageDragMove(e) {
        if (!this.imageDragActive) {
            return;
        }
        const targetIndex = this.resolveDragTargetIndex(this.getDragTouchPoint(e));
        if (targetIndex !== this.data.dragOverImageIndex && Number.isFinite(targetIndex) && targetIndex >= 0) {
            this.setData({
                dragOverImageIndex: targetIndex,
            });
        }
    },
    onImageDragEnd() {
        if (!this.imageDragActive) {
            return;
        }
        const fromIndex = this.imageDragStartIndex;
        const toIndex = Number(this.data.dragOverImageIndex);
        if (Number.isFinite(fromIndex) && Number.isFinite(toIndex) && fromIndex >= 0 && toIndex >= 0 && fromIndex !== toIndex) {
            if (this.data.useFixedSlots) {
                this.applyImageCollectionState({
                    referenceImages: moveImageItem(this.data.referenceImages || [], fromIndex, toIndex),
                }, () => this.resetImageDragState());
                return;
            }
            this.applyImageCollectionState({
                uploadedImages: moveImageItem(this.data.uploadedImages || [], fromIndex, toIndex),
            }, () => this.resetImageDragState());
            return;
        }
        this.resetImageDragState();
    },
    onDeleteImage(e) {
        const index = Number(e.currentTarget.dataset.index);
        const type = String(e.currentTarget.dataset.type || (this.data.useFixedSlots ? 'reference' : 'uploaded'));
        if (!Number.isFinite(index) || index < 0) {
            return;
        }
        if (this.data.useFixedSlots && type === 'reference') {
            const images = [...(this.data.referenceImages || [])];
            images.splice(index, 1);
            this.applyImageCollectionState({ referenceImages: images });
            return;
        }
        const images = [...(this.data.uploadedImages || [])];
        images.splice(index, 1);
        this.applyImageCollectionState({ uploadedImages: images });
    },
    onPreviewImageTap(e) {
        const index = Number(e.currentTarget.dataset.index);
        const scope = String(e.currentTarget.dataset.scope || e.currentTarget.dataset.type || (this.data.useFixedSlots ? 'reference' : 'uploaded'));
        if (!Number.isFinite(index) || index < 0) {
            return;
        }
        const now = Date.now();
        if (this.lastPreviewTapScope === scope && this.lastPreviewTapIndex === index && now - this.lastPreviewTapAt <= 320) {
            this.lastPreviewTapAt = 0;
            this.lastPreviewTapIndex = -1;
            this.lastPreviewTapScope = 'uploaded';
            this.openImagePreview(index, scope);
            return;
        }
        this.lastPreviewTapAt = now;
        this.lastPreviewTapIndex = index;
        this.lastPreviewTapScope = scope;
    },
    openImagePreview(index, scope = 'uploaded') {
        let urls = [];
        let currentIndex = index;
        if (scope === 'original') {
            const slotImages = Array.isArray(this.data.originalImages) ? this.data.originalImages : [];
            urls = normalizeOriginalImageUrls(slotImages);
            if (index >= 0) {
                currentIndex = Math.max(0, slotImages.slice(0, index + 1).filter((item) => typeof item === 'string' && item.trim() !== '').length - 1);
            }
        }
        else if (scope === 'reference') {
            urls = normalizeReferenceImageUrls(this.data.referenceImages || [], MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT);
        }
        else {
            urls = normalizeReferenceImageUrls(this.data.uploadedImages || []);
        }
        if (!urls.length || currentIndex < 0 || currentIndex >= urls.length) {
            return;
        }
        wx.previewImage({
            current: urls[currentIndex],
            urls,
            fail: () => {
                wx.showToast({
                    title: '预览失败',
                    icon: 'none'
                });
            }
        });
    },
    onPromptInput(e) {
        this.setData({
            promptText: e.detail.value,
            promptDirty: true
        }, () => this.saveGenerateDraft());
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
        const scene = this.data.selectedSceneTab === 'interior' ? 'interior' : 'architecture';
        const result = await optimizePrompt(rawPrompt, scene);
        this.setData({ optimizingPrompt: false });
        if (result.success && result.optimizedPrompt) {
            this.setData({ promptText: result.optimizedPrompt, promptDirty: true }, () => this.saveGenerateDraft());
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
    async onGenerate() {
        if (this.data.generating) {
            return;
        }
        const promptText = String(this.data.promptText || '').trim();
        if (promptText && (0, promptReview_1.blockUnsafeText)(promptText, { scene: 'prompt' })) {
            return;
        }
        if (!this.data.selectedService) {
            wx.showToast({ title: '生成服务异常', icon: 'none' });
            return;
        }
        if (!this.data.pricingLoaded || this.data.currentUnitCost <= 0 || this.data.currentCost <= 0) {
            wx.showToast({ title: '计费信息加载中，请稍后再试', icon: 'none' });
            this.loadAIPricing();
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showModal({
                title: '提示',
                content: '请先登录',
                confirmText: '去登录',
                success: (res) => {
                    if (res.confirm) {
                        wx.navigateTo({ url: '/pages/login/login' });
                    }
                }
            });
            return;
        }
        try {
            const stonesRes = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/stones`,
                    method: 'GET',
                    header: {
                        'token': token,
                        'Content-Type': 'application/json',
                    },
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '获取余额失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            if (stonesRes.stones < this.data.currentCost) {
                wx.showModal({
                    title: '余额不足',
                    content: `当前余额：${stonesRes.stones}灵石，需要：${this.data.currentCost}灵石`,
                    confirmText: '去充值',
                    success: (res) => {
                        if (res.confirm) {
                            wx.navigateTo({ url: '/pages/topupcenter/topupcenter' });
                        }
                    }
                });
                return;
            }
        }
        catch (error) {
            console.error('检查余额失败:', error);
            wx.showToast({ title: '检查余额失败', icon: 'none' });
            return;
        }
        this.setData({ generating: true });
        if (!this.data.hasShownGenerateTip) {
            this.setData({ hasShownGenerateTip: true });
            wx.showModal({
                title: '提示',
                content: 'AI 正在生成作品，通常需要几秒钟。生成完成后会自动弹出预览弹窗，你可以在里面重新生成、保存为模板或保存到相册。',
                showCancel: false,
            });
        }
        wx.showLoading({ title: '生成中...', mask: true });
        try {
            const originalImageUrls = normalizeOriginalImageUrls(this.data.originalImages || []);
            const referencePriorityMode = isReferencePrioritySource(String(this.data.entrySource || ''));
            const referenceImageUrls = this.data.useFixedSlots || referencePriorityMode
                ? normalizeReferenceImageUrls(this.data.referenceImages || [], MAX_MAKE_SAME_REFERENCE_IMAGE_COUNT)
                : normalizeReferenceImageUrls(this.data.uploadedImages || []);
            if (this.data.useFixedSlots && !originalImageUrls[0]) {
                wx.hideLoading();
                this.setData({ generating: false });
                wx.showToast({ title: '请先上传原1', icon: 'none' });
                return;
            }
            const orderedImageUrls = this.data.useFixedSlots
                ? buildOrderedImageUrls(originalImageUrls, referenceImageUrls)
                : referenceImageUrls;
            const generateCount = this.getCurrentGenerateCount();
            const selectedStyle = String(this.data.selectedStyle || '').trim();
            const sceneLabel = this.data.selectedSceneTab === 'interior' ? '室内' : '外立面';
            const defaultServiceLabel = this.data.showSceneTabs ? `${sceneLabel}${DEFAULT_SERVICE_LABEL}` : DEFAULT_SERVICE_LABEL;
            const isTemplateFlow = Number(this.data.templateId || 0) > 0;
            const userPrompt = isTemplateFlow
                ? String(this.data.promptText || '').trim()
                : (this.data.promptText || `生成${defaultServiceLabel}`);
            const referencePriorityInstruction = '以参考图的建筑风格、屋顶形式、立面比例、材质色彩、门窗关系为最高优先级；文字需求仅用于层数、尺寸、功能和居住需求补充。若文字与参考图冲突，以参考图为准。';
            const finalUserPrompt = referencePriorityMode && userPrompt
                ? `${userPrompt}，${referencePriorityInstruction}`
                : userPrompt;
            const payload = {
                service_type: this.data.selectedType,
                service: this.data.selectedService,
                quality: this.data.selectedQuality,
                canvas: this.data.selectedCanvas,
                generate_count: generateCount,
            };
            if (isTemplateFlow) {
                payload.template_id = Number(this.data.templateId || 0);
                payload.user_prompt = finalUserPrompt;
            }
            else {
                payload.prompt = finalUserPrompt;
                payload.user_prompt = finalUserPrompt;
            }
            if (this.data.showSceneTabs) {
                payload.scene_direction = sceneLabel;
            }
            if (selectedStyle) {
                payload.style = selectedStyle;
            }
            if (orderedImageUrls.length > 0) {
                if (orderedImageUrls.length === 1) {
                    payload.image_url = orderedImageUrls[0];
                }
                payload.images = orderedImageUrls;
            }
            if (this.data.useFixedSlots || referencePriorityMode) {
                payload.original_image_urls = originalImageUrls;
                payload.reference_image_urls = referenceImageUrls;
                payload.ordered_image_urls = orderedImageUrls;
            }
            if (referencePriorityMode) {
                payload.style_reference_mode = true;
                payload.style_reference_priority = 'high';
                payload.reference_priority_instruction = referencePriorityInstruction;
            }
            const scene = orderedImageUrls.length > 1 ? 'ai_draw_multi' : 'ai_draw_single';
            const requestBody = { scene, payload };
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = '/api/v1/miniprogram/ai/draw';
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
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
            if (res.task_no) {
                wx.hideLoading();
                this.clearGenerateDraft();
                this.setData({ generating: false });
                const sourceQuery = this.data.entrySource
                    ? `&source=${encodeURIComponent(this.data.entrySource)}`
                    : '';
                const taskNo = encodeURIComponent(res.task_no);
                const promptText = String(this.data.promptText || '').trim();
                const promptQuery = promptText ? `&prompt=${encodeURIComponent(promptText)}` : '';
                wx.navigateTo({
                    url: `/pages/generatedetails/generatedetails?task_no=${taskNo}&tab=${this.data.selectedSceneTab}&showSceneTabs=${this.data.showSceneTabs ? 1 : 0}${sourceQuery}${promptQuery}${orderedImageUrls[0] ? `&reference_image_url=${encodeURIComponent(orderedImageUrls[0])}` : ''}`,
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
                            reference_image_url: referenceImageUrls[0] || '',
                            reference_image_urls: referenceImageUrls,
                            original_image_urls: originalImageUrls,
                            ordered_image_urls: orderedImageUrls,
                        });
                    },
                    fail: () => {
                        wx.showToast({ title: '页面跳转失败', icon: 'none' });
                    }
                });
            }
            else {
                wx.hideLoading();
                this.setData({ generating: false });
                this.stopProgress();
                wx.showToast({ title: '生成失败', icon: 'none' });
            }
        }
        catch (error) {
            wx.hideLoading();
            console.error('生成失败:', error);
            wx.showToast({
                title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(error.message || '生成失败'),
                icon: 'none',
                duration: 2000
            });
            this.stopProgress();
            this.setData({ generating: false });
        }
    },
    startTaskPolling(taskNo) {
        this.stopTaskPolling();
        this.pollingAttempt = 0;
        this.scheduleNextTaskPoll(taskNo);
    },
    scheduleNextTaskPoll(taskNo) {
        this.stopTaskPolling();
        const delay = getTaskStatusPollDelay(this.pollingAttempt);
        this.pollingAttempt += 1;
        this.pollingTimer = setTimeout(() => {
            this.checkTaskStatus(taskNo);
        }, delay);
    },
    stopTaskPolling() {
        if (this.pollingTimer) {
            clearTimeout(this.pollingTimer);
            this.pollingTimer = null;
        }
    },
    startProgress() {
        this.stopProgress();
        this.setData({
            showProgressModal: true,
            progressPercent: 0,
        });
        this.progressTimer = setInterval(() => {
            const p = this.data.progressPercent;
            if (p >= 90) {
                return;
            }
            const inc = p < 50 ? 5 : 2;
            const next = Math.min(90, p + inc);
            this.setData({ progressPercent: next });
        }, 400);
    },
    stopProgress() {
        if (this.progressTimer) {
            clearInterval(this.progressTimer);
            this.progressTimer = null;
        }
        if (!this.data.showProgressModal)
            return;
        this.setData({ progressPercent: 100 });
        setTimeout(() => {
            this.setData({
                showProgressModal: false,
                progressPercent: 0,
            });
        }, 300);
    },
    async checkTaskStatus(taskNo) {
        const token = wx.getStorageSync('token');
        if (!token) {
            this.stopTaskPolling();
            wx.hideLoading();
            this.stopProgress();
            this.setData({ generating: false });
            return;
        }
        try {
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = '/api/v1/miniprogram/ai/task/status';
            const body = {
                task_no: taskNo,
                task_type: 'ai_draw',
            };
            const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: {
                        ...headers,
                        'Content-Type': 'application/json',
                    },
                    data: body,
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '查询任务失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            if (!res || !res.status) {
                return;
            }
            if (res.status === 'pending' || res.status === 'processing') {
                this.scheduleNextTaskPoll(taskNo);
                return;
            }
            this.stopTaskPolling();
            wx.hideLoading();
            this.stopProgress();
            this.setData({ generating: false });
            if (res.status === 'failed') {
                wx.showToast({
                    title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(res.error_message),
                    icon: 'none',
                });
                return;
            }
            const result = res.result || {};
            const imageUrl = result.url_raw || result.url || result.image_url || (Array.isArray(result.images) && result.images.length > 0 ? result.images[0] : '');
            if (!imageUrl) {
                wx.showToast({ title: '未获取到生成图片', icon: 'none' });
                return;
            }
            this.setData({
                showResultModal: true,
                resultImageUrl: imageUrl,
                resultTaskNo: taskNo,
            });
        }
        catch (err) {
            console.error('查询任务状态失败:', err);
            this.stopTaskPolling();
            wx.hideLoading();
            this.stopProgress();
            this.setData({ generating: false });
            wx.showToast({
                title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(err.message || '查询任务失败'),
                icon: 'none',
            });
        }
    },
    closeResultModal() {
        this.setData({
            showResultModal: false,
        });
    },
    async onResultRegenerate() {
        this.setData({
            showResultModal: false,
        });
        await this.onGenerate();
    },
    onResultSaveAlbum() {
        const taskNo = this.data.resultTaskNo;
        if (taskNo) {
            this.setData({ showResultModal: false });
            const sourceQuery = this.data.entrySource
                ? `&source=${encodeURIComponent(this.data.entrySource)}`
                : '';
            wx.navigateTo({
                url: `/pages/generatedetails/generatedetails?task_no=${encodeURIComponent(taskNo)}&tab=${this.data.selectedSceneTab}&showSceneTabs=${this.data.showSceneTabs ? 1 : 0}${sourceQuery}`,
                fail: () => {
                    wx.showToast({ title: '跳转失败', icon: 'none' });
                },
            });
            return;
        }
        wx.showModal({
            title: '下载保存需验证',
            content: '生成结果默认仅支持查看，下载保存需先添加企业微信并留下电话号码。',
            showCancel: false,
        });
    },
    onResultSaveTemplate() {
        const taskNo = this.data.resultTaskNo;
        if (!taskNo) {
            wx.showToast({ title: '任务信息缺失', icon: 'none' });
            return;
        }
        this.setData({ showResultModal: false });
        const sourceQuery = this.data.entrySource
            ? `&source=${encodeURIComponent(this.data.entrySource)}`
            : '';
        wx.navigateTo({
            url: `/pages/generatedetails/generatedetails?task_no=${encodeURIComponent(taskNo)}&tab=${this.data.selectedSceneTab}&showSceneTabs=${this.data.showSceneTabs ? 1 : 0}${sourceQuery}`,
            fail: () => {
                wx.showToast({ title: '跳转失败', icon: 'none' });
            },
        });
    },
    async loadTemplateOriginalTask(templateId) {
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                wx.showToast({ title: '请先登录', icon: 'none' });
                return;
            }
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = `/api/v1/miniprogram/templates/${templateId}/original-task`;
            const params = (0, parameter_1.generateRequestParams)(token, {}, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'GET',
                    header: headers,
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '获取失败'));
                            }
                        }
                        else if (requestRes.statusCode === 404) {
                            resolve({});
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject
                });
            });
            this.applyPrefillData(res);
        }
        catch (error) {
            console.error('加载模板原始任务失败:', error);
        }
    },
    async loadTaskInfo(taskId) {
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                wx.showToast({ title: '请先登录', icon: 'none' });
                return;
            }
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = `/api/v1/miniprogram/ai/task/${taskId}/info`;
            const params = (0, parameter_1.generateRequestParams)(token, {}, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'GET',
                    header: headers,
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '获取失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject
                });
            });
            this.applyPrefillData(res);
        }
        catch (error) {
            console.error('加载任务信息失败:', error);
            wx.showToast({ title: error.message || '加载失败', icon: 'none' });
        }
    },
    onShareAppMessage() {
        return {
            title: '生成效果图',
            path: '/pages/aigenerate/aigenerate'
        };
    }
});
