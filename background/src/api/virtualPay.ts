import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface VirtualPayHealthItem {
    key: string;
    label: string;
    ok: boolean;
    message: string;
}

export interface VirtualPayHealth {
    ok: boolean;
    items: VirtualPayHealthItem[];
    sandbox_mode: boolean;
    ios_enabled: boolean;
    order_summary: {
        total_virtualpay_orders: number;
        pending_orders: number;
        refunded_orders: number;
        unreported_goods_orders: number;
    };
}

export interface XPayMessageLogItem {
    id: number;
    msg_id: string;
    event: string;
    out_trade_no: string;
    openid: string;
    processed: boolean;
    error_msg: string;
    created_at: string;
}

export interface XPayLogSummary {
    total_count: number;
    processed_count: number;
    unprocessed_count: number;
}

export interface XPayLogListParams {
    keyword?: string;
    event?: string;
    processed?: string;
    page?: number;
    page_size?: number;
}

export const getVirtualPayHealth = async () => {
    const res = await get<VirtualPayHealth>(API_ENDPOINTS.VIRTUAL_PAY.HEALTH);
    return res.data!;
};

export const getXPayLogs = async (params?: XPayLogListParams) => {
    const res = await get<{
        list: XPayMessageLogItem[];
        summary: XPayLogSummary;
        total: number;
        page: number;
        page_size: number;
    }>(API_ENDPOINTS.VIRTUAL_PAY.LOGS, params);
    return res.data!;
};
