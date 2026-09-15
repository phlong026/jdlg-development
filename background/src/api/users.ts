import { get, post, put, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// 用户接口类型（适配后端）
export interface User {
    id: number;
    username: string;
    user_type: string;
    stones: number;
    enterprise_wechat_verified?: boolean;
    enterprise_wechat_verified_at?: string;
    enterprise_wechat_contact?: string;
    nickname?: string;
    avatar?: string;
    designer_bio?: string;
    specialty_styles?: string;
    designer_experience_years?: number;
    service_title?: string;
    phone?: string;
    identity_type?: string;
    created_at: string;
    updated_at: string;
}

// 用户列表响应
export interface UserListResponse {
    list: User[];
    total: number;
    page: number;
    page_size: number;
}

// 用户列表参数
export interface UserListParams {
    keyword?: string;
    status?: string;
    role?: string;
    enterprise_wechat_status?: 'all' | 'verified' | 'pending';
    page?: number;
    page_size?: number;
}

// 更新用户登录信息请求（用户名、密码）
export interface UpdateUserRequest {
    username?: string;
    password?: string;
    nickname?: string;
    avatar?: string;
    designer_bio?: string;
    specialty_styles?: string;
    designer_experience_years?: number;
    service_title?: string;
}

// 设置灵石请求
export interface SetStonesRequest {
    stones: number;
    remark?: string;
}

// 调整灵石请求
export interface AdjustStonesRequest {
    amount: number;
    remark?: string;
}

// 灵石操作响应
export interface StonesResponse {
    id: number;
    username: string;
    old_stones: number;
    new_stones: number;
    adjustment?: number;
}

export interface EnterpriseWechatVerification {
    user_id: number;
    enterprise_wechat_verified: boolean;
    enterprise_wechat_verified_at?: string;
    enterprise_wechat_contact?: string;
}

export interface UserWorkbenchSummary {
    membership?: {
        plan_code: string;
        plan_title: string;
        status: string;
        template_download_enabled: boolean;
        started_at: string;
        granted_at: string;
        expired_at: string;
        source_order_no: string;
        is_lifetime: boolean;
    } | null;
    stone_summary: {
        current_stones: number;
        recent_consume: number;
        recent_gain: number;
        checkin_total: number;
    };
    stone_records: Array<{
        id: number;
        type: string;
        amount: number;
        scene_desc: string;
        remark: string;
        created_at: string;
    }>;
    recent_orders: Array<{
        id: number;
        order_no: string;
        type: string;
        order_category: string;
        amount: number;
        status: string;
        title: string;
        description: string;
        created_at: string;
        completed_at?: string | null;
    }>;
    recent_tasks: Array<{
        task_no: string;
        task_type: string;
        scene: string;
        status: string;
        stones_used: number;
        error_message?: string;
        prompt?: string;
        created_at: string;
        updated_at: string;
    }>;
    device_risk?: {
        device_id?: string;
        device_bind_time?: string;
        last_device_change_time?: string;
        has_password: boolean;
        same_device_account_count: number;
        same_device_other_users: Array<{
            user_id: number;
            username?: string;
            display_name?: string;
        }>;
        risk_tags: string[];
        risk_level: 'low' | 'medium' | 'high';
    };
}

export interface UpdateEnterpriseWechatVerificationRequest {
    verified: boolean;
    contact?: string;
}

// 获取用户列表
export const getUserList = async (params?: UserListParams): Promise<UserListResponse> => {
    const response = await get<{
        list: User[];
        total: number;
        page: number;
        page_size: number;
    }>(API_ENDPOINTS.USERS.LIST, params);
    if (response.data) {
        return {
            list: response.data.list || [],
            total: response.data.total || 0,
            page: response.data.page || 1,
            page_size: response.data.page_size || 20,
        };
    }
    return { list: [], total: 0, page: 1, page_size: 20 };
};

// 获取用户详情
export const getUserDetail = async (id: string): Promise<User | null> => {
    const response = await get<User>(API_ENDPOINTS.USERS.DETAIL(id));
    return response.data || null;
};

export const getUserWorkbenchSummary = async (id: string): Promise<UserWorkbenchSummary | null> => {
    const response = await get<UserWorkbenchSummary>(API_ENDPOINTS.USERS.WORKBENCH(id));
    return response.data || null;
};

// 更新用户登录信息（用户名、密码）
export const updateUser = async (id: string, data: UpdateUserRequest): Promise<User | null> => {
    const response = await put<User>(API_ENDPOINTS.USERS.DETAIL(id), data);
    return response.data || null;
};

// 设置用户灵石余额（绝对值）
export const setUserStones = async (id: string, data: SetStonesRequest): Promise<StonesResponse | null> => {
    const response = await put<StonesResponse>(API_ENDPOINTS.USERS.SET_STONES(id), data);
    return response.data || null;
};

// 调整用户灵石余额（增减）
export const adjustUserStones = async (id: string, data: AdjustStonesRequest): Promise<StonesResponse | null> => {
    const response = await post<StonesResponse>(API_ENDPOINTS.USERS.ADJUST_STONES(id), data);
    return response.data || null;
};

export const getUserEnterpriseWechatVerification = async (id: string): Promise<EnterpriseWechatVerification | null> => {
    const response = await get<EnterpriseWechatVerification>(API_ENDPOINTS.USERS.ENTERPRISE_WECHAT(id));
    return response.data || null;
};

export const updateUserEnterpriseWechatVerification = async (
    id: string,
    data: UpdateEnterpriseWechatVerificationRequest,
): Promise<EnterpriseWechatVerification | null> => {
    const response = await put<EnterpriseWechatVerification>(API_ENDPOINTS.USERS.ENTERPRISE_WECHAT(id), data);
    return response.data || null;
};
