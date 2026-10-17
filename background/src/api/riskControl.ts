import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface RiskControlOverview {
    shared_devices: number;
    device_risk_users: number;
    recent_device_changes: number;
    abnormal_payments: number;
    failed_tasks: number;
    access_alerts: number;
}

export interface RiskControlListParams {
    page?: number;
    page_size?: number;
    keyword?: string;
}

export interface RiskControlListResponse<T> {
    list: T[];
    total: number;
    page: number;
    page_size: number;
}

export interface RiskControlDeviceGroupUser {
    user_id: string;
    username: string;
    nickname?: string;
}

export interface RiskControlDeviceGroupItem {
    device_id: string;
    user_count: number;
    latest_activity_at?: string;
    users: RiskControlDeviceGroupUser[];
}

export interface RiskControlDeviceChangeItem {
    user_id: number;
    username: string;
    nickname?: string;
    device_id: string;
    device_bind_time?: string;
    last_device_change_time?: string;
}

export interface RiskControlAlertItem {
    user_id: number;
    username: string;
    nickname?: string;
    alert_type: 'payment' | 'task' | 'access' | string;
    alert_count: number;
    latest_time?: string;
    detail?: string;
    /** 访问告警专用：告警来源的追踪访客键（u:<id> 或匿名安装标识） */
    visitor_key?: string;
}

export interface RiskControlUserItem {
    user_id: number;
    username: string;
    nickname?: string;
    device_id: string;
    shared_device_count: number;
    last_device_change_time?: string;
    failed_task_count: number;
    abnormal_payment_count: number;
    risk_tags: string[];
}

export const getRiskControlOverview = async (): Promise<RiskControlOverview> => {
    const response = await get<RiskControlOverview>(API_ENDPOINTS.RISK_CONTROL.OVERVIEW);
    if (!response.data) {
        throw new Error('获取风控台概览失败');
    }
    return response.data;
};

export const getRiskControlDeviceGroups = async (params?: RiskControlListParams): Promise<RiskControlListResponse<RiskControlDeviceGroupItem>> => {
    const response = await get<RiskControlListResponse<RiskControlDeviceGroupItem>>(API_ENDPOINTS.RISK_CONTROL.DEVICE_GROUPS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取同设备账号失败');
    }
    return response.data;
};

export const getRiskControlDeviceChanges = async (params?: RiskControlListParams): Promise<RiskControlListResponse<RiskControlDeviceChangeItem>> => {
    const response = await get<RiskControlListResponse<RiskControlDeviceChangeItem>>(API_ENDPOINTS.RISK_CONTROL.DEVICE_CHANGES, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取设备换绑列表失败');
    }
    return response.data;
};

export const getRiskControlAlerts = async (params?: RiskControlListParams): Promise<RiskControlListResponse<RiskControlAlertItem>> => {
    const response = await get<RiskControlListResponse<RiskControlAlertItem>>(API_ENDPOINTS.RISK_CONTROL.ALERTS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取风险告警失败');
    }
    return response.data;
};

export const getRiskControlUsers = async (params?: RiskControlListParams): Promise<RiskControlListResponse<RiskControlUserItem>> => {
    const response = await get<RiskControlListResponse<RiskControlUserItem>>(API_ENDPOINTS.RISK_CONTROL.USERS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取风险用户失败');
    }
    return response.data;
};

export interface ShareAttributionItem {
    share_id: string;
    content_title: string;
    visitor_count: number;
    identified_count: number;
    latest_entered_at?: string;
}

/** 分享→账号归因（联动C）：追踪未启用时后端返回空列表，前端按"暂无数据"呈现 */
export const getShareAttribution = async (days = 30): Promise<{ list: ShareAttributionItem[]; days: number }> => {
    const response = await get<{ list: ShareAttributionItem[]; days: number }>(API_ENDPOINTS.RISK_CONTROL.SHARE_ATTRIBUTION, { days } as Record<string, any>);
    if (!response.data || !Array.isArray(response.data.list)) {
        throw new Error('获取分享归因失败');
    }
    return response.data;
};
