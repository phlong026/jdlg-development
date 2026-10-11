"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.mapAIToolFromResponse = mapAIToolFromResponse;
exports.fetchAIToolList = fetchAIToolList;
exports.fetchAIToolDetail = fetchAIToolDetail;
exports.fetchAIToolByCode = fetchAIToolByCode;
const aiTools_1 = require("./aiTools");
const API_BASE_URL = 'https://api.jiadilingguang.com';
function normalizeCategory(category) {
    if (category === 'interior' || category === 'landscape' || category === 'planning') {
        return category;
    }
    return 'architecture';
}
function normalizePresetReferences(list) {
    return (list || []).map((item, index) => ({
        id: String(item.id || `preset-${index}`),
        name: String(item.name || '预设参考图'),
        description: String(item.description || ''),
        imageUrl: String(item.image_url || ''),
        promptSuffix: String(item.prompt_suffix || ''),
    }));
}
function normalizeStylePresets(list) {
    return (list || []).map((item, index) => {
        const legacyImageUrl = String(item.image_url || '');
        const referenceMode = item.reference_mode === 'preview_only' || (!item.reference_mode && !legacyImageUrl && !item.model_reference_image_url)
            ? 'preview_only'
            : 'preview_and_model';
        const previewImageUrl = String(item.preview_image_url || legacyImageUrl);
        const modelReferenceImageUrl = referenceMode === 'preview_and_model'
            ? String(item.model_reference_image_url || legacyImageUrl)
            : '';
        return {
            id: String(item.id || `style-${index}`),
            name: String(item.name || '默认风格'),
            referenceMode,
            previewImageUrl,
            modelReferenceImageUrl,
            imageUrl: previewImageUrl,
            promptSuffix: String(item.prompt_suffix || ''),
        };
    });
}
function pickFirstImage(...values) {
    for (const value of values) {
        const normalized = String(value || '').trim();
        if (normalized) {
            return normalized;
        }
    }
    return '';
}
function mapAIToolFromResponse(raw) {
    const category = normalizeCategory(String(raw.category || 'architecture'));
    const common = !!raw.is_common;
    const listCoverImage = pickFirstImage(raw.list_cover_image, raw.detail_after_image, raw.detail_before_image);
    const detailBeforeImage = pickFirstImage(raw.detail_before_image, raw.list_cover_image);
    const detailAfterImage = pickFirstImage(raw.detail_after_image, raw.list_cover_image);
    const tags = [(0, aiTools_1.getCategoryLabel)(category), ...(common ? ['常用'] : [])];
    return {
        id: String(raw.id || ''),
        code: String(raw.code || ''),
        name: String(raw.name || ''),
        category,
        shortDescription: String(raw.short_description || ''),
        detailDescription: String(raw.detail_description || ''),
        common,
        promptPlaceholder: String(raw.prompt_placeholder || ''),
        uploadHint: String(raw.upload_hint || ''),
        tags,
        presetReferences: normalizePresetReferences(raw.preset_references),
        stylePresets: normalizeStylePresets(raw.style_presets),
        listCoverImage,
        detailBeforeImage,
        detailAfterImage,
        showUsageTips: raw.show_usage_tips !== false,
        usageTipsTitle: String(raw.usage_tips_title || '使用提示'),
        usageTipsContent: String(raw.usage_tips_content || ''),
        sortOrder: Number(raw.sort_order || 0),
        usageCount: Number(raw.usage_count || 0),
        published: raw.is_published !== false,
    };
}
function request(url) {
    return new Promise((resolve, reject) => {
        wx.request({
            url,
            method: 'GET',
            success: (res) => {
                if (res.statusCode !== 200 || !res.data) {
                    reject(new Error(`请求失败: ${res.statusCode}`));
                    return;
                }
                const response = res.data;
                if (response.code !== 0) {
                    reject(new Error(response.msg || '请求失败'));
                    return;
                }
                resolve(response.data);
            },
            fail: reject,
        });
    });
}
async function fetchAIToolList(params) {
    const query = [
        params?.category ? `category=${encodeURIComponent(String(params.category))}` : '',
        params?.keyword ? `keyword=${encodeURIComponent(String(params.keyword))}` : '',
        `page=${encodeURIComponent(String(params?.page || 1))}`,
        `page_size=${encodeURIComponent(String(params?.pageSize || 100))}`,
    ].filter(Boolean).join('&');
    const data = await request(`${API_BASE_URL}/api/v1/miniprogram/ai-tools${query ? `?${query}` : ''}`);
    const list = (data.list || []).map((item) => mapAIToolFromResponse(item));
    return {
        list,
        total: Number(data.total || list.length),
        page: Number(data.page || params?.page || 1),
        pageSize: Number(data.page_size || params?.pageSize || 100),
    };
}
async function fetchAIToolDetail(id) {
    const data = await request(`${API_BASE_URL}/api/v1/miniprogram/ai-tools/${encodeURIComponent(String(id || ''))}`);
    return mapAIToolFromResponse(data || {});
}
async function fetchAIToolByCode(code) {
    const data = await request(`${API_BASE_URL}/api/v1/miniprogram/ai-tools/code/${encodeURIComponent(String(code || ''))}`);
    return mapAIToolFromResponse(data || {});
}
