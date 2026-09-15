import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface SupportTicketOverview {
    total_count: number;
    open_count: number;
    in_progress_count: number;
    closed_count: number;
    high_priority_count: number;
}

export interface SupportTicketItem {
    id: number;
    user_id: number;
    username: string;
    nickname?: string;
    type: 'complaint' | 'order' | 'task' | 'certification' | string;
    source_type: 'manual' | 'order' | 'task' | string;
    source_id: string;
    title: string;
    content: string;
    resolution_note?: string;
    priority: 'high' | 'medium' | 'low' | string;
    status: 'open' | 'in_progress' | 'closed' | string;
    assignee_id: number;
    assignee_name: string;
    created_by: string;
    source_payload?: string;
    closed_at?: string | null;
    created_at: string;
    updated_at: string;
}

export interface SupportTicketListParams {
    keyword?: string;
    status?: string;
    type?: string;
    source_type?: string;
    page?: number;
    page_size?: number;
}

export interface SupportTicketCreateParams {
    user_id?: number;
    type?: string;
    title: string;
    content?: string;
    priority?: string;
}

export interface SupportTicketListResponse {
    list: SupportTicketItem[];
    total: number;
    page: number;
    page_size: number;
}

export const getSupportTicketOverview = async (): Promise<SupportTicketOverview> => {
    const response = await get<SupportTicketOverview>(API_ENDPOINTS.SUPPORT_TICKETS.OVERVIEW);
    if (!response.data) {
        throw new Error('获取工单概览失败');
    }
    return response.data;
};

export const getSupportTicketList = async (params?: SupportTicketListParams): Promise<SupportTicketListResponse> => {
    const response = await get<SupportTicketListResponse>(API_ENDPOINTS.SUPPORT_TICKETS.LIST, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取工单列表失败');
    }
    return response.data;
};

export const getSupportTicketDetail = async (id: string): Promise<SupportTicketItem> => {
    const response = await get<SupportTicketItem>(API_ENDPOINTS.SUPPORT_TICKETS.DETAIL(id));
    if (!response.data) {
        throw new Error('获取工单详情失败');
    }
    return response.data;
};

export const createSupportTicket = async (payload: SupportTicketCreateParams): Promise<number> => {
    const response = await post<{ id: number }>(API_ENDPOINTS.SUPPORT_TICKETS.CREATE, payload);
    if (!response.data) {
        throw new Error('创建工单失败');
    }
    return response.data.id;
};

export const assignSupportTicket = async (id: string): Promise<void> => {
    await post(API_ENDPOINTS.SUPPORT_TICKETS.ASSIGN(id), {});
};

export const updateSupportTicketStatus = async (id: string, status: string): Promise<void> => {
    await post(API_ENDPOINTS.SUPPORT_TICKETS.STATUS(id), { status });
};

export const updateSupportTicketResolutionNote = async (id: string, resolutionNote: string): Promise<void> => {
    await post(API_ENDPOINTS.SUPPORT_TICKETS.RESOLUTION_NOTE(id), { resolution_note: resolutionNote });
};

export const syncSupportTicketSystemExceptions = async (): Promise<number> => {
    const response = await post<{ created_count: number }>(API_ENDPOINTS.SUPPORT_TICKETS.SYNC_SYSTEM_EXCEPTIONS, {});
    return response.data?.created_count || 0;
};
