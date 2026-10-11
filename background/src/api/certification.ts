import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface CertificationApplication {
    id: number;
    user_id: number;
    type: 'designer' | 'enterprise';
    real_name: string;
    id_card_no: string;
    company_name: string;
    credit_code: string;
    legal_person: string;
    aliyun_passed: boolean;
    aliyun_msg: string;
    extra_docs_remark: string;
    identity_type?: string;
    status: 'pending_payment' | 'pending_review' | 'approved' | 'rejected';
    admin_remark: string;
    reviewed_at: string | null;
    reviewed_by: number;
    created_at: string;
    updated_at: string;
}

export interface CertificationDetailResponse {
    application: CertificationApplication;
    user: {
        id: number;
        username: string;
        user_type: string;
        can_withdraw?: boolean;
        created_at?: string;
    } | null;
}

export interface CertificationListParams {
    status?: string;
    keyword?: string;
    overdue?: boolean; // 超时队列：待审核且提交超 48 小时
    limit?: number;
    offset?: number;
}

// 认证队列全库口径统计
export interface CertificationStats {
    pending_review: number;
    overdue_48h: number;
    pending_payment: number;
    approved: number;
    rejected: number;
    total: number;
}

export interface CertificationRevealFields {
    id: number;
    real_name: string;
    id_card_no: string;
    credit_code: string;
    legal_person: string;
}

export const getCertificationList = async (
    params?: CertificationListParams
): Promise<{ list: CertificationApplication[]; total: number }> => {
    const q: Record<string, string> = {};
    if (params?.status) q.status = params.status;
    if (params?.keyword) q.keyword = params.keyword;
    if (params?.overdue) q.overdue = '1';
    if (params?.limit != null) q.limit = String(params.limit);
    if (params?.offset != null) q.offset = String(params.offset);
    const response = await get<CertificationApplication[]>(API_ENDPOINTS.CERTIFICATION.LIST, q);
    const raw = response as any;
    return {
        list: raw.data ?? [],
        total: raw.total ?? 0,
    };
};

export const getCertificationStats = async (): Promise<CertificationStats | null> => {
    const response = await get<CertificationStats>(API_ENDPOINTS.CERTIFICATION.STATS);
    const raw = response as any;
    return raw.data ?? null;
};

// 查看完整认证材料（列表/详情默认脱敏；POST 形式让审计中间件记录本次查看）
export const revealCertificationFields = async (id: string): Promise<CertificationRevealFields | null> => {
    const response = await post<CertificationRevealFields>(API_ENDPOINTS.CERTIFICATION.REVEAL(id), {});
    const raw = response as any;
    return raw.data ?? null;
};

export const getCertificationDetail = async (
    id: string
): Promise<CertificationDetailResponse | null> => {
    const response = await get<CertificationDetailResponse>(API_ENDPOINTS.CERTIFICATION.DETAIL(id));
    const raw = response as any;
    if (raw.data) return raw.data;
    return null;
};

export const reviewCertification = async (
    id: string,
    action: 'approve' | 'reject',
    admin_remark?: string
): Promise<void> => {
    await post(API_ENDPOINTS.CERTIFICATION.REVIEW(id), { action, admin_remark: admin_remark || '' });
};
