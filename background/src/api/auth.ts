import { get, post } from '../utils/request';
import { API_ENDPOINTS, API_BASE_URL } from '../config/api';
import { tokenUtils } from '../utils/token';

// 登录请求
export interface LoginRequest {
    username: string;
    password: string;
}

// 登录响应
export interface LoginResponse {
    id: number;
    username: string;
    scope?: 'platform_admin' | 'crm_only';
    crmEnabled?: boolean;
}

// 登录
export const login = async (data: LoginRequest): Promise<LoginResponse> => {
    const response = await post<LoginResponse>(API_ENDPOINTS.AUTH.LOGIN, data);
    if (response.data) {
        // 后端使用 session cookie，不需要存储 token
        // 但为了兼容性，可以设置一个标识
        tokenUtils.setToken('session_active');
        window.dispatchEvent(new Event('management-session-changed'));
        return response.data;
    }
    throw new Error(response.msg || response.message || '登录失败');
};

// 登出
export const logout = async (): Promise<void> => {
    try {
        await post(API_ENDPOINTS.AUTH.LOGOUT);
    } finally {
        tokenUtils.removeToken();
        window.dispatchEvent(new Event('management-session-expired'));
    }
};

// 获取当前用户信息
export const getMe = async (): Promise<LoginResponse> => {
    // no-store + 超时：会话核验是全后台首屏前置，避免悬挂请求阻塞页面
    const result = await fetch(API_BASE_URL + API_ENDPOINTS.AUTH.ME, {credentials:'include',cache:'no-store',signal:AbortSignal.timeout(10000)});
    const response = await result.json();
    if (!result.ok) throw Object.assign(new Error(response.msg || '会话检查失败'), {status:result.status});
    if (response.data) {
        return response.data;
    }
    throw new Error(response.msg || response.message || '获取用户信息失败');
};
