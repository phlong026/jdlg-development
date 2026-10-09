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
    refunded_stones?: number;
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
    queues?: AIQueueCounts;
    total: number;
    page: number;
    page_size: number;
}

/** 运营队列计数（后端 QueueCounts 聚合，角标与队列 Tab 共用） */
export interface AIQueueCounts {
    failed: number;
    timeout: number;
    running: number;
    today_new: number;
    total_open: number;
}

export type AITaskQueue = 'failed' | 'timeout' | 'running' | 'today' | 'all';

export interface AITaskListParams {
    keyword?: string;
    status?: string;
    scene?: string;
    queue?: AITaskQueue;
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

export interface BatchSupportTicketResult {
    created_count: number;
    existed_count: number;
    created_ids: number[];
    first_error: string;
}

/** 批量转异常工单：失败队列勾选多条一次性建单，已有未关闭工单的自动跳过 */
export const batchCreateAITaskSupportTickets = async (ids: number[]) => {
    const res = await post<BatchSupportTicketResult>(API_ENDPOINTS.AI_TASKS.BATCH_SUPPORT_TICKETS, { ids });
    return res.data!;
};

export interface RetryAITaskResult {
    new_task_id: number;
    new_task_no: string;
    retry_of: string;
    note: string;
}

/** 重新执行失败任务：以当前启用配置重新入队，不重复扣费（管理员补偿动作，仅图片任务） */
export const retryAITask = async (id: string | number) => {
    const res = await post<RetryAITaskResult>(API_ENDPOINTS.AI_TASKS.RETRY(id), {});
    return res.data!;
};
