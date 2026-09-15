import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface DashboardOverview {
    total_users: number;
    today_new_users: number;
    today_orders: number;
    today_success_orders: number;
    today_success_amount: number;
    today_image_tasks: number;
    today_video_tasks: number;
    today_failed_tasks: number;
    pending_certifications: number;
    pending_exceptions: number;
}

export interface DashboardTrendItem {
    date: string;
    label: string;
    new_users: number;
    order_count: number;
    success_orders: number;
    success_amount: number;
    image_tasks: number;
    video_tasks: number;
    failed_tasks: number;
}

export interface DashboardTodoUserItem {
    user_id: number;
    username: string;
    nickname?: string;
    stones?: number;
    enterprise_wechat_contact?: string;
}

export interface DashboardTodoCertificationItem {
    id: number;
    user_id: number;
    type: string;
    identity_type?: string;
    created_at: string;
}

export interface DashboardFailedTaskItem {
    user_id: number;
    username: string;
    nickname?: string;
    task_no: string;
    task_type: string;
    scene: string;
    model?: string;
    error_message?: string;
    created_at: string;
}

export interface DashboardTodos {
    counts: {
        pending_certifications: number;
        zero_stones_users: number;
        pending_wechat_users: number;
        failed_tasks: number;
    };
    pending_certifications: DashboardTodoCertificationItem[];
    zero_stones_users: DashboardTodoUserItem[];
    pending_wechat_users: DashboardTodoUserItem[];
    failed_tasks: DashboardFailedTaskItem[];
}

export const getDashboardOverview = async (): Promise<DashboardOverview> => {
    const response = await get<DashboardOverview>(API_ENDPOINTS.STATS.DASHBOARD_OVERVIEW);
    if (!response.data) {
        throw new Error('获取总控台总览失败');
    }
    return response.data;
};

export const getDashboardTrends = async (): Promise<DashboardTrendItem[]> => {
    const response = await get<DashboardTrendItem[]>(API_ENDPOINTS.STATS.DASHBOARD_TRENDS);
    return response.data || [];
};

export const getDashboardTodos = async (): Promise<DashboardTodos> => {
    const response = await get<DashboardTodos>(API_ENDPOINTS.STATS.DASHBOARD_TODOS);
    if (!response.data) {
        throw new Error('获取总控台待办失败');
    }
    return response.data;
};
