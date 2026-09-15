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
    limit?: number;
    offset?: number;
}

export const getCertificationList = async (
    params?: CertificationListParams
): Promise<{ list: CertificationApplication[]; total: number }> => {
    const q: Record<string, string> = {};
    if (params?.status) q.status = params.status;
    if (params?.keyword) q.keyword = params.keyword;
    if (params?.limit != null) q.limit = String(params.limit);
    if (params?.offset != null) q.offset = String(params.offset);
    const response = await get<CertificationApplication[]>(API_ENDPOINTS.CERTIFICATION.LIST, q);
    const raw = response as any;
    return {
        list: raw.data ?? [],
        total: raw.total ?? 0,
    };
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
