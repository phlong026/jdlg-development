import { get } from '../utils/request';
import { API_ENDPOINTS, API_BASE_URL } from '../config/api';
import { tokenUtils } from '../utils/token';

export interface ReportCenterOverview {
    total_users: number;
    total_revenue: number;
    total_tasks: number;
    total_templates: number;
    designer_count: number;
}

export interface ReportCenterColumn {
    key: string;
    label: string;
}

export interface ReportCenterResponse {
    report_type: string;
    period: string;
    start_date: string;
    end_date: string;
    columns: ReportCenterColumn[];
    rows: Record<string, any>[];
    summary: Record<string, any>;
}

export interface ReportCenterParams {
    report_type: string;
    period: string;
    start_date?: string;
    end_date?: string;
}

export const getReportCenterOverview = async (): Promise<ReportCenterOverview> => {
    const response = await get<ReportCenterOverview>(API_ENDPOINTS.REPORT_CENTER.OVERVIEW);
    if (!response.data) {
        throw new Error('获取报表中心概览失败');
    }
    return response.data;
};

export const getReportCenterReport = async (params: ReportCenterParams): Promise<ReportCenterResponse> => {
    const response = await get<ReportCenterResponse>(API_ENDPOINTS.REPORT_CENTER.REPORTS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取报表数据失败');
    }
    return response.data;
};

export const getReportCenterExportUrl = (params: ReportCenterParams): string => {
    const searchParams = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined && value !== null && value !== '') {
            searchParams.append(key, String(value));
        }
    });
    const token = tokenUtils.getToken();
    if (token) {
        searchParams.append('_token', token);
    }
    const queryString = searchParams.toString();
    return `${API_BASE_URL}${API_ENDPOINTS.REPORT_CENTER.EXPORT}${queryString ? `?${queryString}` : ''}`;
};
