import { get, put } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

/** 充值档位配置项（金额可配，灵石由兑换比例折算，不入库） */
export interface PricingRechargePlan {
    type?: string;
    title: string;
    amount_fen: number;
    recommended?: boolean;
}

/** 认证道具价格配置项 */
export interface PricingCertPrice {
    product_id: string;
    price_fen: number;
}

export interface PricingConfigData {
    recharge_plans: PricingRechargePlan[];
    cert_prices: PricingCertPrice[];
}

export interface PricingConfigResponse extends PricingConfigData {
    /** db = 后台配置生效；default = 尚未配置，展示默认值 */
    source: 'db' | 'default';
    /** 兑换比例（只读：须与微信 mp 后台一致，暂不支持后台修改） */
    coin_ratio: number;
    cert_title_map: Record<string, string>;
    membership_note: string;
}

export const getPricingConfig = async () => {
    const res = await get<PricingConfigResponse>(API_ENDPOINTS.PRICING.CONFIG);
    return res.data!;
};

export const savePricingConfig = async (data: PricingConfigData) => {
    const res = await put<{ source: string; recharge_plans: PricingRechargePlan[]; cert_prices: PricingCertPrice[] }>(
        API_ENDPOINTS.PRICING.CONFIG,
        data,
    );
    return res.data!;
};
