import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface MembershipOperationsOverview {
    total_users: number;
    active_users: number;
    expiring_soon_users: number;
    expired_users: number;
    permission_disabled_users: number;
    legacy_recharge_users: number;
}

export interface MembershipOperationsOrderInfo {
    order_no?: string;
    status?: string;
    amount?: number;
    order_category?: string;
    created_at?: string;
}

export interface MembershipOperationsUserItem {
    user_id: number;
    username: string;
    nickname?: string;
    service_title?: string;
    display_name?: string;
    plan_code?: string;
    plan_title?: string;
    status?: string;
    template_download_enabled: boolean;
    lifetime_membership: boolean;
    legacy_recharge_member: boolean;
    remaining_days: number;
    started_at_text?: string;
    granted_at_text?: string;
    expired_at_text?: string;
    source_order_no?: string;
    source_order?: MembershipOperationsOrderInfo;
    latest_recharge_order?: MembershipOperationsOrderInfo;
}

export interface MembershipOperationsUserListResponse {
    list: MembershipOperationsUserItem[];
    total: number;
    page: number;
    page_size: number;
}

export interface MembershipOperationsUserListParams {
    page?: number;
    page_size?: number;
    keyword?: string;
    status?: string;
    permission_state?: string;
}

export const getMembershipOperationsOverview = async () => {
    const response = await get<MembershipOperationsOverview>(API_ENDPOINTS.MEMBERSHIP_OPERATIONS.OVERVIEW);
    if (!response.data) {
        throw new Error('获取会员运营概览失败');
    }
    return response.data;
};

export const getMembershipOperationsUsers = async (params?: MembershipOperationsUserListParams) => {
    const response = await get<MembershipOperationsUserListResponse>(API_ENDPOINTS.MEMBERSHIP_OPERATIONS.USERS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取会员用户列表失败');
    }
    return response.data;
};
