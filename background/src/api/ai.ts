import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// AI计费配置
export interface AIPricing {
    id?: number;
    scene: string;
    stones: number;
    extra_config?: any;
    created_at?: string;
    updated_at?: string;
}

// API Key 发送方式
export type APIKeyLocation = 'none' | 'header_bearer' | 'header_custom' | 'query' | 'body';

// AI API配置
export interface AIAPIConfig {
    id?: number;
    task_type: string; // 'ai_draw' | 'ai_chat'
    provider_code?: string;
    provider_name?: string;
    protocol_type?: string;
    is_active?: boolean;
    api_endpoint: string;
    method: string; // 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
    headers?: any;
    body_template?: any;
    prompt_path?: string; // 提示词在JSON中的路径，如 "prompt" 或 "data.prompt"
    enable_prompt_optimization?: boolean; // 是否开启提示词优化
    image_path?: string; // 用户图片在JSON中的路径，如 "image" 或 "data.images[0]"
    // API Key 配置
    api_key?: string; // API Key 值
    api_key_location?: APIKeyLocation; // API Key 发送位置: header, query, body
    api_key_name?: string; // API Key 参数名，如 Authorization, api_key, X-API-Key
    created_at?: string;
    updated_at?: string;
}

// 获取所有计费配置
export const getAIPricingList = async (): Promise<AIPricing[]> => {
    const response = await get<AIPricing[]>(API_ENDPOINTS.AI.PRICING);
    return response.data || [];
};

// 创建或更新计费配置
export const saveAIPricing = async (pricing: AIPricing): Promise<void> => {
    await post(API_ENDPOINTS.AI.PRICING, pricing);
};

// 获取所有API配置
export const getAIAPIConfigList = async (): Promise<AIAPIConfig[]> => {
    const response = await get<AIAPIConfig[]>(API_ENDPOINTS.AI.API_CONFIG);
    return response.data || [];
};

// 创建或更新API配置
export const saveAIAPIConfig = async (config: AIAPIConfig): Promise<void> => {
    await post(API_ENDPOINTS.AI.API_CONFIG, config);
};
