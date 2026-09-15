import { get, post, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface RechargeConfigData {
    wechat_qrcode?: string;
    alipay_qrcode?: string;
    wechat_account?: string;
    wechat_name?: string;
    alipay_account?: string;
    alipay_name?: string;
    note?: string;
}

export interface RechargeConfigItem {
    id: number;
    payment_mode: 'static_qrcode' | 'wechat_only' | 'alipay_only' | 'wechat_alipay';
    config: string;
    config_data?: RechargeConfigData;
    is_enabled: boolean;
    created_at: string;
    updated_at: string;
}

// 获取充值配置列表
export const getRechargeConfigList = async () => {
    const res = await get<{ list: RechargeConfigItem[] }>(API_ENDPOINTS.RECHARGE_CONFIG.LIST);
    return res.data!.list;
};

// 获取单个充值配置
export const getRechargeConfig = async (paymentMode: string) => {
    const res = await get<RechargeConfigItem>(API_ENDPOINTS.RECHARGE_CONFIG.GET(paymentMode));
    return res.data!;
};

// 创建或更新充值配置
export const createOrUpdateRechargeConfig = async (data: {
    payment_mode: 'static_qrcode' | 'wechat_only' | 'alipay_only' | 'wechat_alipay';
    config_data: RechargeConfigData;
    is_enabled: boolean;
}) => {
    const res = await post<RechargeConfigItem>(API_ENDPOINTS.RECHARGE_CONFIG.CREATE_OR_UPDATE, data);
    return res.data!;
};

// 删除充值配置
export const deleteRechargeConfig = async (id: string) => {
    await del(API_ENDPOINTS.RECHARGE_CONFIG.DELETE(id));
};
