import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface AITaskSummary {
    total_count: number;
    pending_count: number;
    running_count: number;
    failed_count: number;
}

export interface AITaskItem {
    id: number;
    task_no: string;
    user_id: number;
    username: string;
    scene?: string;
    model?: string;
    api_endpoint?: string;
    prompt?: string;
    status: string;
    raw_status?: string;
    stones_used: number;
    error_message: string;
    raw_error_message?: string;
    segment_count?: number;
    duration?: number;
    resolution?: string;
    created_at: string;
    updated_at: string;
}

export interface AITaskDetail {
    id: number;
    type: 'image' | 'video';
    task_no: string;
    user_id: number;
    scene?: string;
    model?: string;
    api_endpoint?: string;
    prompt?: string;
    status: string;
    raw_status?: string;
    stones_used: number;
    error_message?: string;
    raw_error_message?: string;
    request_payload?: string;
    result_payload?: string;
    external_id?: string;
    oss_url?: string;
    segment_count?: number;
    duration?: number;
    resolution?: string;
    created_at: string;
    updated_at: string;
}

export interface AITaskListResponse {
    list: AITaskItem[];
    summary: AITaskSummary;
    total: number;
    page: number;
    page_size: number;
}

export interface AITaskListParams {
    keyword?: string;
    status?: string;
    scene?: string;
    page?: number;
    page_size?: number;
}

export interface CreateSupportTicketResult {
    id: number;
    existed: boolean;
}

export interface BackfillAITaskModelsResult {
    updated_count: number;
    inspected_count: number;
    limit: number;
}

export const getAITaskList = async (params?: AITaskListParams) => {
    const res = await get<AITaskListResponse>(API_ENDPOINTS.AI_TASKS.LIST, params);
    return res.data!;
};

export const backfillAITaskModels = async (limit = 500) => {
    const res = await post<BackfillAITaskModelsResult>(API_ENDPOINTS.AI_TASKS.BACKFILL_MODELS, { limit });
    return res.data!;
};

export const getAITaskDetail = async (id: string) => {
    const res = await get<AITaskDetail>(API_ENDPOINTS.AI_TASKS.DETAIL(id));
    return res.data!;
};

export const getAIVideoTaskList = async (params?: AITaskListParams) => {
    const res = await get<AITaskListResponse>(API_ENDPOINTS.AI_TASKS.VIDEO_LIST, params);
    return res.data!;
};

export const getAIVideoTaskDetail = async (id: string) => {
    const res = await get<AITaskDetail>(API_ENDPOINTS.AI_TASKS.VIDEO_DETAIL(id));
    return res.data!;
};

export const createAITaskSupportTicket = async (id: string) => {
    const res = await post<CreateSupportTicketResult>(API_ENDPOINTS.AI_TASKS.SUPPORT_TICKET(id), {});
    return res.data!;
};

export const createAIVideoTaskSupportTicket = async (id: string) => {
    const res = await post<CreateSupportTicketResult>(API_ENDPOINTS.AI_TASKS.VIDEO_SUPPORT_TICKET(id), {});
    return res.data!;
};
