import { get } from '../utils/request';
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

export const getContentAnalyticsOverview = async () => {
    const response = await get<ContentAnalyticsOverview>(API_ENDPOINTS.CONTENT_ANALYTICS.OVERVIEW);
    if (!response.data) {
        throw new Error('获取内容运营分析概览失败');
    }
    return response.data;
};

export const getContentAnalyticsDownloadRanking = async (params?: ContentAnalyticsListParams) => {
    const response = await get<ContentAnalyticsListResponse<ContentAnalyticsTemplateItem>>(API_ENDPOINTS.CONTENT_ANALYTICS.DOWNLOAD_RANKING, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取模板下载排行失败');
    }
    return response.data;
};

export const getContentAnalyticsEngagementRanking = async (params?: ContentAnalyticsListParams) => {
    const response = await get<ContentAnalyticsListResponse<ContentAnalyticsTemplateItem>>(API_ENDPOINTS.CONTENT_ANALYTICS.ENGAGEMENT_RANKING, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取互动排行失败');
    }
    return response.data;
};

export const getContentAnalyticsNewTemplates = async (params?: ContentAnalyticsListParams) => {
    const response = await get<ContentAnalyticsListResponse<ContentAnalyticsTemplateItem>>(API_ENDPOINTS.CONTENT_ANALYTICS.NEW_TEMPLATES, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取新上架模板表现失败');
    }
    return response.data;
};

export const getContentAnalyticsLowConversion = async (params?: ContentAnalyticsListParams) => {
    const response = await get<ContentAnalyticsListResponse<ContentAnalyticsTemplateItem>>(API_ENDPOINTS.CONTENT_ANALYTICS.LOW_CONVERSION, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取低转化模板失败');
    }
    return response.data;
};

export const getContentAnalyticsFeaturedCases = async (params?: ContentAnalyticsListParams) => {
    const response = await get<ContentAnalyticsListResponse<ContentAnalyticsFeaturedCaseItem>>(API_ENDPOINTS.CONTENT_ANALYTICS.FEATURED_CASES, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取精选案例观察失败');
    }
    return response.data;
};
