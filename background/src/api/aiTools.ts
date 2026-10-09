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
    // 运营数据（列表接口附带；仅成功任务计收入）
    usage_7d?: number;
    success_7d?: number;
    failed_rate_7d?: number;
    stones_7d?: number;
    daily_series_7d?: number[];
    // 草稿-发布分离（二期批次2）
    has_draft?: boolean;      // 有草稿
    draft_differs?: boolean;  // 草稿与线上不同（待发布）
    is_draft?: boolean;       // 详情返回的是草稿态内容
    latest_version_no?: number;
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
    /** 草稿/发布备注：写入版本记录，不属于生效内容 */
    note?: string;
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

// ───────────── 草稿-发布分离（二期批次2） ─────────────

export interface AIToolDiffItem {
    field: string;
    label: string;
    kind: 'changed' | 'added' | 'removed';
    before: string;
    after: string;
}

export interface AIToolDiffResult {
    has_draft: boolean;
    items: AIToolDiffItem[];
    draft_note?: string;
    published_version_no?: number;
}

export interface AIToolVersionStats {
    after: { total: number; success_rate: number };
    before: { total: number; success_rate: number };
}

export interface AIToolVersion {
    id: number;
    version_no: number;
    status: string;
    note: string;
    created_by: string;
    created_at: string;
    published_at?: string;
    stats?: AIToolVersionStats;
}

/** 保存（PUT）现在写入草稿；此接口把当前草稿发布到线上 */
export const publishAITool = async (id: string | number, note?: string) => {
    const res = await post<{ version_no: number; published_at: string }>(API_ENDPOINTS.AI_TOOLS.PUBLISH(String(id)), { note: note || '' });
    return res.data!;
};

/** 草稿与线上的差异（发布确认弹窗用） */
export const getAIToolDiff = async (id: string | number) => {
    const res = await get<AIToolDiffResult>(API_ENDPOINTS.AI_TOOLS.DIFF(String(id)));
    return res.data!;
};

/** 版本列表（含每版发布前后 7 日效果对比） */
export const getAIToolVersions = async (id: string | number) => {
    const res = await get<AIToolVersion[]>(API_ENDPOINTS.AI_TOOLS.VERSIONS(String(id)));
    return res.data || [];
};

/** 回滚到历史版本（= 用旧快照再发布一次，同样留痕） */
export const rollbackAITool = async (id: string | number, versionId: number) => {
    const res = await post<{ version_no: number; rolled_back_to: number }>(API_ENDPOINTS.AI_TOOLS.ROLLBACK(String(id)), { version_id: versionId });
    return res.data!;
};
