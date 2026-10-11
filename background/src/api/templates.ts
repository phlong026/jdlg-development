import { get, post, put, patch, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// 模板分类
export interface TemplateCategoryItem {
    id: string;
    name: string;
    sort_order: number;
}

// 双重 Tab 配置项
export interface TabConfigItem {
    label: string;
    value: string;
    parent?: string; // 二级tab的父tab value
}

export interface TemplateTabConfig {
    main_tabs: TabConfigItem[];
    sub_tabs: TabConfigItem[];
    third_tabs: TabConfigItem[];
}

// 模板接口类型（适配后端）
export interface TemplateItem {
    id: number;
    name: string;
    category: string;
    // 后台用于双重 Tab 归类的字段
    main_tab?: string;
    sub_tab?: string;
    third_tab?: string;
    description: string;
    thumbnail?: string;
    preview_url?: string;
    images?: string;
    image_width?: number;
    image_height?: number;
    price: number;
    is_free: boolean;
    is_featured?: boolean;
    download_count: number;
    like_count: number;
    status: string;
    publish_scope?: 'homepage_only' | 'square';
    reject_reason?: string;
    source_type?: 'admin_upload' | 'ai_generated' | 'album_upload';
    creator: string;
    created_at: string;
    updated_at: string;
}

// 模板列表响应
export interface TemplateListResponse {
    list: TemplateItem[];
    total: number;
    page: number;
    page_size: number;
}

// 模板列表参数
export interface TemplateListParams {
    category?: string;
    status?: string;
    page?: number;
    page_size?: number;
}

// 更新模板请求（审核通过/拒绝时只改 status）
export interface TemplateUpdateRequest {
    name?: string;
    category?: string;
    description?: string;
    // 双重 Tab 字段（可选）
    main_tab?: string;
    sub_tab?: string;
    third_tab?: string;
    thumbnail?: string;
    preview_url?: string;
    images?: string;
    image_width?: number;
    image_height?: number;
    price?: number;
    is_free?: boolean;
    is_featured?: boolean;
    status?: string;
    publish_scope?: 'homepage_only' | 'square';
    reject_reason?: string;
    source_type?: 'admin_upload' | 'ai_generated' | 'album_upload';
}

// 获取模板列表
export const getTemplateList = async (params?: TemplateListParams) => {
    const res = await get<TemplateListResponse>(API_ENDPOINTS.TEMPLATES.LIST, params as Record<string, any>);
    return res.data!;
};

// 获取模板详情
export const getTemplateDetail = async (id: string) => {
    const res = await get<TemplateItem>(API_ENDPOINTS.TEMPLATES.DETAIL(id));
    return res.data!;
};

// 更新模板（用于编辑，需传完整字段）
export const updateTemplate = async (id: string, data: TemplateUpdateRequest) => {
    const res = await put<TemplateItem>(API_ENDPOINTS.TEMPLATES.UPDATE(id), data);
    return res.data!;
};

// 仅更新模板状态（用于审核通过/拒绝）
export const updateTemplateStatus = async (id: string, status: 'published' | 'draft' | 'archived' | 'rejected', rejectReason?: string) => {
    const res = await patch<TemplateItem>(API_ENDPOINTS.TEMPLATES.STATUS(id), {
        status,
        reject_reason: rejectReason,
    });
    return res.data!;
};

// 删除模板
export const deleteTemplate = async (id: string) => {
    await del(API_ENDPOINTS.TEMPLATES.DELETE(id));
};

// ---------- 分类 ----------
export const getTemplateCategories = async () => {
    const res = await get<{ list: TemplateCategoryItem[] }>(API_ENDPOINTS.TEMPLATES.CATEGORIES.LIST);
    return res.data?.list ?? [];
};

export const createTemplateCategory = async (data: { id: string; name: string; sort_order?: number }) => {
    const res = await post<TemplateCategoryItem>(API_ENDPOINTS.TEMPLATES.CATEGORIES.CREATE, data);
    return res.data!;
};

export const deleteTemplateCategory = async (id: string) => {
    await del(API_ENDPOINTS.TEMPLATES.CATEGORIES.DELETE(id));
};

// 移动模板到指定分类
export const updateTemplateCategory = async (templateId: string, category: string) => {
    const res = await patch<TemplateItem>(API_ENDPOINTS.TEMPLATES.CATEGORY(templateId), { category });
    return res.data!;
};

// 创建模板（管理后台添加）
export const createTemplate = async (data: TemplateUpdateRequest & { name: string; category: string }) => {
    const res = await post<TemplateItem>(API_ENDPOINTS.TEMPLATES.CREATE, data);
    return res.data!;
};

// ---------- 双重 Tab 配置 ----------
export const getTemplateTabConfig = async (): Promise<TemplateTabConfig> => {
    const res = await get<{ main_tabs: TabConfigItem[]; sub_tabs: TabConfigItem[]; third_tabs: TabConfigItem[] }>(API_ENDPOINTS.TEMPLATES.TAB_CONFIG);
    return res.data!;
};

export const putTemplateTabConfig = async (data: TemplateTabConfig) => {
    await put(API_ENDPOINTS.TEMPLATES.TAB_CONFIG, data);
};

// ---------- 精选案例管理 ----------
// 设置/取消精选案例
export const setTemplateFeatured = async (id: string, isFeatured: boolean) => {
    const res = await patch<TemplateItem>(API_ENDPOINTS.TEMPLATES.FEATURED(id), { is_featured: isFeatured });
    return res.data!;
};

// 获取精选案例列表
export const getFeaturedTemplates = async (limit?: number) => {
    const res = await get<{ list: TemplateItem[] }>(API_ENDPOINTS.TEMPLATES.FEATURED_LIST, { limit });
    return res.data?.list ?? [];
};

// ---------- 精选案例组管理 ----------
export interface FeaturedCaseGroup {
    id: number;
    name: string;
    display_mode: 'comparison' | 'side_by_side' | 'normal';
    case1_id: number;
    case2_id: number;
    case1_label: string;
    case2_label: string;
    sort_order: number;
    created_at: string;
    updated_at: string;
    case1?: TemplateItem;
    case2?: TemplateItem;
}

export interface FeaturedCaseGroupListResponse {
    list: FeaturedCaseGroup[];
    total: number;
    page: number;
    page_size: number;
}

export interface FeaturedCaseGroupCreateRequest {
    name: string;
    display_mode: 'comparison' | 'side_by_side' | 'normal';
    case1_id: number;
    case2_id?: number;
    case1_label?: string;
    case2_label?: string;
    sort_order?: number;
}

// 获取精选案例组列表
export const getFeaturedCaseGroups = async (page?: number, pageSize?: number) => {
    const res = await get<FeaturedCaseGroupListResponse>(API_ENDPOINTS.TEMPLATES.FEATURED_GROUPS.LIST, {
        page,
        page_size: pageSize,
    });
    return res.data!;
};

// 获取精选案例组详情
export const getFeaturedCaseGroupDetail = async (id: string) => {
    const res = await get<FeaturedCaseGroup>(API_ENDPOINTS.TEMPLATES.FEATURED_GROUPS.DETAIL(id));
    return res.data!;
};

// 创建精选案例组
export const createFeaturedCaseGroup = async (data: FeaturedCaseGroupCreateRequest) => {
    const res = await post<FeaturedCaseGroup>(API_ENDPOINTS.TEMPLATES.FEATURED_GROUPS.CREATE, data);
    return res.data!;
};

// 更新精选案例组
export const updateFeaturedCaseGroup = async (id: string, data: FeaturedCaseGroupCreateRequest) => {
    const res = await put<FeaturedCaseGroup>(API_ENDPOINTS.TEMPLATES.FEATURED_GROUPS.UPDATE(id), data);
    return res.data!;
};

// 删除精选案例组
export const deleteFeaturedCaseGroup = async (id: string) => {
    await del(API_ENDPOINTS.TEMPLATES.FEATURED_GROUPS.DELETE(id));
};
