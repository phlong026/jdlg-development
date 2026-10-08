import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// 收入口径说明：user_orders.amount 记录的是灵石（充值为正、消费为负），
// 人民币金额在 amount_fen（分）。总控台的收入一律用 income_fen，禁止把灵石当钱展示。
export interface DashboardOverview {
    total_users: number;
    today_new_users: number;
    yesterday_new_users: number;
    today_income_fen: number;
    yesterday_income_fen: number;
    today_orders: number;
    yesterday_orders: number;
    today_success_orders: number;
    yesterday_success_orders: number;
    today_image_tasks: number;
    today_video_tasks: number;
    yesterday_tasks: number;
    today_failed_tasks: number;
    yesterday_failed_tasks: number;
    pending_certifications: number;
}

export interface DashboardTrendItem {
    date: string;
    label: string;
    income_fen: number;
    new_users: number;
    order_count: number;
    image_tasks: number;
    video_tasks: number;
    failed_tasks: number;
}

export interface DashboardTodoItem {
    key: string;
    /** action=需要今天处理；watch=只监控计数，明细在各模块页 */
    kind: 'action' | 'watch';
    label: string;
    count: number;
    path: string;
    /** 一句上下文（构成/积压程度），让待办不用点进去就能判断轻重 */
    hint?: string;
    urgent: boolean;
}

export const getDashboardOverview = async (): Promise<DashboardOverview> => {
    const response = await get<DashboardOverview>(API_ENDPOINTS.STATS.DASHBOARD_OVERVIEW);
    if (!response.data) {
        throw new Error('获取总控台总览失败');
    }
    return response.data;
};

// days 仅接受 7 / 30，其余值后端按 7 处理
export const getDashboardTrends = async (days: 7 | 30 = 7): Promise<DashboardTrendItem[]> => {
    const response = await get<DashboardTrendItem[]>(`${API_ENDPOINTS.STATS.DASHBOARD_TRENDS}?days=${days}`);
    return response.data || [];
};

// 今日待办与异常计数：与各页队列角标同源的唯一数据源
export const getDashboardTodoItems = async (): Promise<DashboardTodoItem[]> => {
    const response = await get<{ items: DashboardTodoItem[] }>(API_ENDPOINTS.STATS.DASHBOARD_TODO);
    return response.data?.items || [];
};
