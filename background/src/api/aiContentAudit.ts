import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// AI 内容抽检（二期批次 4a）：成功生成结果按比例抽入审核队列，审核不拦截用户结果
export interface AIContentAuditItem {
    id: number;
    task_no: string;
    task_type: 'image' | 'video' | string;
    user_id: number;
    username: string;
    scene: string;
    model: string;
    result_url: string;
    status: 'pending' | 'approved' | 'rejected' | string;
    reviewer: string;
    review_note: string;
    reviewed_at?: string;
    created_at: string;
}

export interface AIContentAuditListResponse {
    list: AIContentAuditItem[];
    total: number;
    page: number;
    page_size: number;
    pending_count: number;
}

export const getAIContentAudits = async (params?: { status?: string; keyword?: string; page?: number; page_size?: number }) => {
    const res = await get<AIContentAuditListResponse>(API_ENDPOINTS.AI_CONTENT_AUDITS.LIST, params);
    return res.data!;
};

export const reviewAIContentAudit = async (id: number | string, status: 'approved' | 'rejected', note?: string) => {
    await post(API_ENDPOINTS.AI_CONTENT_AUDITS.REVIEW(id), { status, note: note || '' });
};
