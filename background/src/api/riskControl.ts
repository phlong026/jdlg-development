import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface RiskControlOverview {
    shared_devices: number;
    device_risk_users: number;
    recent_device_changes: number;
    abnormal_payments: number;
    failed_tasks: number;
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
    alert_type: 'payment' | 'task' | string;
    alert_count: number;
    latest_time?: string;
    detail?: string;
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
