import { get, post, put, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface UtilityToolItem {
    id: number;
    category: 'local_norm' | 'faq' | 'video_tutorial';
    title: string;
    content: string;
    cover_image?: string;
    video_url?: string;
    file_url?: string;
    sort_order: number;
    is_published: boolean;
    view_count: number;
    created_at: string;
    updated_at: string;
}

export interface UtilityToolListResponse {
    list: UtilityToolItem[];
    total: number;
    page: number;
    page_size: number;
}

export interface UtilityToolListParams {
    category?: string;
    keyword?: string;
    is_published?: boolean;
    page?: number;
    page_size?: number;
}

export interface UtilityToolCreateRequest {
    category: 'local_norm' | 'faq' | 'video_tutorial';
    title: string;
    content?: string;
    cover_image?: string;
    video_url?: string;
    file_url?: string;
    sort_order?: number;
    is_published?: boolean;
}

// 获取实用工具内容列表
export const getUtilityToolList = async (params?: UtilityToolListParams) => {
    const res = await get<UtilityToolListResponse>(API_ENDPOINTS.UTILITY_TOOLS.LIST, params as Record<string, any>);
    return res.data!;
};

// 获取实用工具内容详情
export const getUtilityToolDetail = async (id: string) => {
    const res = await get<UtilityToolItem>(API_ENDPOINTS.UTILITY_TOOLS.DETAIL(id));
    return res.data!;
};

// 创建实用工具内容
export const createUtilityTool = async (data: UtilityToolCreateRequest) => {
    const res = await post<UtilityToolItem>(API_ENDPOINTS.UTILITY_TOOLS.CREATE, data);
    return res.data!;
};

// 更新实用工具内容
export const updateUtilityTool = async (id: string, data: UtilityToolCreateRequest) => {
    const res = await put<UtilityToolItem>(API_ENDPOINTS.UTILITY_TOOLS.UPDATE(id), data);
    return res.data!;
};

// 删除实用工具内容
export const deleteUtilityTool = async (id: string) => {
    await del(API_ENDPOINTS.UTILITY_TOOLS.DELETE(id));
};
