import { API_BASE_URL } from '../config/api';
import { tokenUtils } from './token';

// API响应类型
export interface ApiResponse<T = any> {
    code: number;
    message?: string;
    msg?: string;
    data?: T;
}

// 请求配置
interface RequestConfig extends RequestInit {
    params?: Record<string, any>;
}

// 创建完整的URL
const createUrl = (endpoint: string, params?: Record<string, any>): string => {
    let url = `${API_BASE_URL}${endpoint}`;
    
    if (params) {
        const searchParams = new URLSearchParams();
        Object.keys(params).forEach(key => {
            if (params[key] !== undefined && params[key] !== null && params[key] !== '') {
                searchParams.append(key, String(params[key]));
            }
        });
        const queryString = searchParams.toString();
        if (queryString) {
            url += `?${queryString}`;
        }
    }
    
    return url;
};

// 请求函数
export const request = async <T = any>(
    endpoint: string,
    config: RequestConfig = {}
): Promise<ApiResponse<T>> => {
    const { params, ...fetchConfig } = config;
    const requestPage = window.location.pathname;
    
    const url = createUrl(endpoint, params);
    
    // 设置默认headers
    const headers: HeadersInit = {
        'Content-Type': 'application/json',
        ...fetchConfig.headers,
    };
    
    // 添加token
    const token = tokenUtils.getToken();
    if (token) {
        headers['Authorization'] = `Bearer ${token}`;
    }
    
    try {
        const response = await fetch(url, {
            ...fetchConfig,
            headers,
            credentials: 'include', // 支持 cookie（session）
        });
        
        // 处理HTTP错误
        if (!response.ok) {
            if (response.status === 401) {
                // Token过期，清除并跳转到登录页
                tokenUtils.removeToken();
                window.dispatchEvent(new Event('management-session-expired'));
                window.location.href = '/admin/login';
                throw new Error('未授权，请重新登录');
            }
            const problem = await response.json().catch(() => null);
            throw new Error(problem?.message || problem?.msg || `服务暂时不可用（HTTP ${response.status}）`);
        }
        
        const data: ApiResponse<T> = await response.json();
        
        // 处理业务错误（后端使用 code: 0 表示成功）
        if (data.code !== 0 && data.code !== 200) {
            throw new Error(data.message || data.msg || '请求失败');
        }
        
        return data;
    } catch (error) {
        if (error instanceof Error) Object.assign(error, { requestPage });
        console.error('API请求失败:', error);
        throw error;
    }
};

// GET请求
export const get = <T = any>(endpoint: string, params?: Record<string, any>): Promise<ApiResponse<T>> => {
    return request<T>(endpoint, {
        method: 'GET',
        params,
    });
};

// POST请求
export const post = <T = any>(endpoint: string, data?: any): Promise<ApiResponse<T>> => {
    return request<T>(endpoint, {
        method: 'POST',
        body: data ? JSON.stringify(data) : undefined,
    });
};

// PUT请求
export const put = <T = any>(endpoint: string, data?: any): Promise<ApiResponse<T>> => {
    return request<T>(endpoint, {
        method: 'PUT',
        body: data ? JSON.stringify(data) : undefined,
    });
};

// PATCH请求
export const patch = <T = any>(endpoint: string, data?: any): Promise<ApiResponse<T>> => {
    return request<T>(endpoint, {
        method: 'PATCH',
        body: data ? JSON.stringify(data) : undefined,
    });
};

// DELETE请求
export const del = <T = any>(endpoint: string): Promise<ApiResponse<T>> => {
    return request<T>(endpoint, {
        method: 'DELETE',
    });
};
