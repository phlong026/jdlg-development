import { get, patch } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface DesignerManagementRepresentativeWork {
    id?: number;
    title?: string;
    thumbnail?: string;
    status?: string;
    publish_scope?: string;
}

export interface DesignerManagementItem {
    user_id: number;
    username: string;
    display_name?: string;
    avatar?: string;
    service_title?: string;
    service_enabled?: boolean;
    specialty_styles?: string;
    certification_status?: string;
    certification_type?: string;
    designer_visible: boolean;
    can_withdraw: boolean;
    total_works: number;
    representative_work?: DesignerManagementRepresentativeWork;
    follow_count: number;
    review_count: number;
    positive_review_count: number;
    negative_review_count: number;
    recent_active_at?: string;
    created_at?: string;
}

export interface DesignerManagementListSummary {
    total_designers: number;
    public_designers: number;
    approved_designers: number;
    designers_with_works: number;
}

export interface DesignerManagementListResponse {
    list: DesignerManagementItem[];
    total: number;
    page: number;
    page_size: number;
    summary: DesignerManagementListSummary;
}

export interface DesignerManagementListParams {
    page?: number;
    page_size?: number;
    keyword?: string;
    certification_status?: string;
    visible?: string;
    specialty?: string;
}

export interface DesignerManagementWorkItem {
    id: number;
    name: string;
    description?: string;
    thumbnail?: string;
    price: number;
    is_free: boolean;
    status: string;
    publish_scope?: string;
    source_type?: string;
    like_count: number;
    download_count: number;
    main_tab?: string;
    sub_tab?: string;
    reject_reason?: string;
    created_at?: string;
    updated_at?: string;
}

export interface DesignerManagementReviewItem {
    id: number;
    reviewer_user_id: number;
    reviewer_name: string;
    reviewer_avatar?: string;
    rating: number;
    content: string;
    sentiment: string;
    order_id: number;
    order_no: string;
    created_at?: string;
}

export interface DesignerManagementProfile {
    nickname?: string;
    avatar?: string;
    designer_bio?: string;
    specialty_styles?: string;
    designer_experience_years?: number;
    service_title?: string;
    service_quote?: number;
    service_intro?: string;
    service_enabled?: boolean;
    designer_visible?: boolean;
    enterprise_wechat_verified?: boolean;
    enterprise_wechat_contact?: string;
}

export interface DesignerManagementCertification {
    id: number;
    type?: string;
    status?: string;
    identity_type?: string;
    real_name?: string;
    company_name?: string;
    credit_code?: string;
    extra_docs_remark?: string;
    admin_remark?: string;
    created_at?: string;
    reviewed_at?: string;
}

export interface DesignerManagementDetail {
    user_id: number;
    username: string;
    can_withdraw: boolean;
    created_at?: string;
    updated_at?: string;
    profile: DesignerManagementProfile;
    certification?: DesignerManagementCertification | null;
    stats: {
        total_works: number;
        published_works: number;
        total_orders: number;
        month_orders: number;
        total_earnings: number;
        month_earnings: number;
        follow_count: number;
        positive_review_count: number;
        negative_review_count: number;
        review_count: number;
    };
    works: DesignerManagementWorkItem[];
    reviews: DesignerManagementReviewItem[];
    public_preview?: any;
}

export const getDesignerManagementList = async (params?: DesignerManagementListParams) => {
    const response = await get<DesignerManagementListResponse>(API_ENDPOINTS.DESIGNERS.LIST, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取设计师列表失败');
    }
    return response.data;
};

export const getDesignerManagementDetail = async (id: string | number) => {
    const response = await get<DesignerManagementDetail>(API_ENDPOINTS.DESIGNERS.DETAIL(String(id)));
    if (!response.data) {
        throw new Error('获取设计师详情失败');
    }
    return response.data;
};

export const updateDesignerVisibility = async (id: string | number, designerVisible: boolean) => {
    await patch(API_ENDPOINTS.DESIGNERS.VISIBILITY(String(id)), {
        designer_visible: designerVisible,
    });
};

export const updateDesignerServiceStatus = async (id: string | number, serviceEnabled: boolean) => {
    await patch(API_ENDPOINTS.DESIGNERS.SERVICE_STATUS(String(id)), {
        service_enabled: serviceEnabled,
    });
};
