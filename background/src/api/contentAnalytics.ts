import { request } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface ContentAnalyticsOverview {
    total_templates: number;
    published_templates: number;
    total_downloads: number;
    total_unlocks: number;
    total_interactions: number;
    week_new_templates: number;
    featured_case_group_count: number;
}

export interface ContentAnalyticsTemplateItem {
    id: number;
    name: string;
    category: string;
    creator: string;
    source_type?: string;
    is_free: boolean;
    price: number;
    download_count: number;
    like_count: number;
    comment_count: number;
    share_count: number;
    unlock_count: number;
    engagement_score: number;
    created_at: string;
    conversion_type?: 'unlock' | 'download';
    conversion_count?: number;
    conversion_rate?: number;
}

export interface ContentAnalyticsFeaturedCaseItem {
    id: number;
    name: string;
    display_mode: string;
    combined_download_count: number;
    combined_engagement_score: number;
    updated_at: string;
    case1: {
        id: number;
        name: string;
        download_count: number;
        engagement_score: number;
    };
    case2?: {
        id: number;
        name: string;
        download_count: number;
        engagement_score: number;
    };
}

export interface ContentAnalyticsListResponse<T> {
    list: T[];
    total: number;
    page: number;
    page_size: number;
}

export interface ContentAnalyticsListParams {
    page?: number;
    page_size?: number;
    keyword?: string;
}

const overviewFields: (keyof ContentAnalyticsOverview)[] = [
    'total_templates', 'published_templates', 'total_downloads', 'total_unlocks',
    'total_interactions', 'week_new_templates', 'featured_case_group_count',
];

export const getContentAnalyticsOverview = async (signal?: AbortSignal) => {
    const response = await request<ContentAnalyticsOverview>(API_ENDPOINTS.CONTENT_ANALYTICS.OVERVIEW, { method: 'GET', signal });
    const data = response.data;
    if (!data || overviewFields.some(field => !Number.isSafeInteger(data[field]) || data[field] < 0)) {
        throw new Error('内容运营分析概览数据不完整，请重试');
    }
    return data;
};

const getAnalyticsList = async <T>(endpoint: string, label: string, params?: ContentAnalyticsListParams, signal?: AbortSignal) => {
    const response = await request<ContentAnalyticsListResponse<T>>(endpoint, { method: 'GET', params, signal });
    const data = response.data;
    // The service returns an array, including [] for an empty result. Missing
    // lists must remain an error rather than becoming a believable empty table.
    if (!data || !Array.isArray(data.list) || !Number.isSafeInteger(data.total) || data.total < 0 ||
        !Number.isSafeInteger(data.page) || data.page < 1 || !Number.isSafeInteger(data.page_size) || data.page_size < 1 ||
        data.list.some(item => !item || typeof item !== 'object' || Array.isArray(item))) {
        throw new Error(`${label}数据不完整，请重试`);
    }
    return data;
};

export const getContentAnalyticsDownloadRanking = (params?: ContentAnalyticsListParams, signal?: AbortSignal) =>
    getAnalyticsList<ContentAnalyticsTemplateItem>(API_ENDPOINTS.CONTENT_ANALYTICS.DOWNLOAD_RANKING, '模板下载排行', params, signal);

export const getContentAnalyticsEngagementRanking = (params?: ContentAnalyticsListParams, signal?: AbortSignal) =>
    getAnalyticsList<ContentAnalyticsTemplateItem>(API_ENDPOINTS.CONTENT_ANALYTICS.ENGAGEMENT_RANKING, '互动排行', params, signal);

export const getContentAnalyticsNewTemplates = (params?: ContentAnalyticsListParams, signal?: AbortSignal) =>
    getAnalyticsList<ContentAnalyticsTemplateItem>(API_ENDPOINTS.CONTENT_ANALYTICS.NEW_TEMPLATES, '新创建模板表现', params, signal);

export const getContentAnalyticsLowConversion = (params?: ContentAnalyticsListParams, signal?: AbortSignal) =>
    getAnalyticsList<ContentAnalyticsTemplateItem>(API_ENDPOINTS.CONTENT_ANALYTICS.LOW_CONVERSION, '低转化模板', params, signal);

export const getContentAnalyticsFeaturedCases = (params?: ContentAnalyticsListParams, signal?: AbortSignal) =>
    getAnalyticsList<ContentAnalyticsFeaturedCaseItem>(API_ENDPOINTS.CONTENT_ANALYTICS.FEATURED_CASES, '精选案例观察', params, signal);
