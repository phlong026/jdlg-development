import { get, post, put, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export type AIToolCategory = 'architecture' | 'interior' | 'landscape' | 'planning';

export interface AIToolPresetReference {
    id: string;
    name: string;
    description?: string;
    image_url?: string;
    prompt_suffix?: string;
}

export interface AIToolStylePreset {
    id: string;
    name: string;
    reference_mode?: 'preview_only' | 'preview_and_model';
    preview_image_url?: string;
    model_reference_image_url?: string;
    /** @deprecated 兼容旧数据：历史上同时用于前台预览和 AI 引用 */
    image_url?: string;
    prompt_suffix?: string;
}

export interface AIToolItem {
    id: number;
    code: string;
    name: string;
    category: AIToolCategory;
    short_description: string;
    detail_description: string;
    list_cover_image?: string;
    detail_before_image?: string;
    detail_after_image?: string;
    prompt_placeholder?: string;
    default_prompt?: string;
    upload_hint?: string;
    show_usage_tips?: boolean;
    usage_tips_title?: string;
    usage_tips_content?: string;
    sort_order: number;
    is_published: boolean;
    is_common: boolean;
    usage_count: number;
    preset_references: AIToolPresetReference[];
    style_presets: AIToolStylePreset[];
    created_at: string;
    updated_at: string;
}

export interface AIToolListResponse {
    list: AIToolItem[];
    total: number;
    page: number;
    page_size: number;
}

export interface AIToolListParams {
    category?: string;
    keyword?: string;
    is_published?: boolean;
    page?: number;
    page_size?: number;
}

export interface AIToolUpsertRequest {
    code: string;
    name: string;
    category: AIToolCategory;
    short_description: string;
    detail_description?: string;
    list_cover_image?: string;
    detail_before_image?: string;
    detail_after_image?: string;
    prompt_placeholder?: string;
    default_prompt?: string;
    upload_hint?: string;
    show_usage_tips?: boolean;
    usage_tips_title?: string;
    usage_tips_content?: string;
    sort_order?: number;
    is_published?: boolean;
    is_common?: boolean;
    preset_references?: AIToolPresetReference[];
    style_presets?: AIToolStylePreset[];
}

export const getAIToolList = async (params?: AIToolListParams) => {
    const res = await get<AIToolListResponse>(API_ENDPOINTS.AI_TOOLS.LIST, params as Record<string, any>);
    return res.data!;
};

export const getAIToolDetail = async (id: string) => {
    const res = await get<AIToolItem>(API_ENDPOINTS.AI_TOOLS.DETAIL(id));
    return res.data!;
};

export const createAITool = async (data: AIToolUpsertRequest) => {
    const res = await post<AIToolItem>(API_ENDPOINTS.AI_TOOLS.CREATE, data);
    return res.data!;
};

export const updateAITool = async (id: string, data: AIToolUpsertRequest) => {
    const res = await put<AIToolItem>(API_ENDPOINTS.AI_TOOLS.UPDATE(id), data);
    return res.data!;
};

export const deleteAITool = async (id: string) => {
    await del(API_ENDPOINTS.AI_TOOLS.DELETE(id));
};
