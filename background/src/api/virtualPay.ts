import { get, post } from '../utils/request';
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
        delivering_orders?: number;
        refunding_stuck_orders?: number;
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
    /** 反查自 user_orders/users；无对应订单时为 0/空 */
    user_id: number;
    username: string;
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

/** 重放推送（T6）：用留存的推送原文重新执行业务分发（幂等） */
export const replayXPayLog = async (id: number) => {
    const res = await post<{ id: number; event: string; order_no: string; replayed: boolean }>(API_ENDPOINTS.VIRTUAL_PAY.XPAY_LOG_REPLAY(id), {});
    return res.data!;
};

/** 双账差额名单条目（G3） */
export interface CalibrationDiffItem {
    user_id: number;
    username: string;
    measured_balance: number;
    local_mirror: number;
    diff: number;
    order_no: string;
    calibrated_at: string;
}

export interface CalibrationSummary {
    calibrated_users: number;
    diff_users: number;
    diffs: CalibrationDiffItem[];
}

export const getCalibrationSummary = async () => {
    const res = await get<CalibrationSummary>(API_ENDPOINTS.VIRTUAL_PAY.CALIBRATIONS_SUMMARY);
    return res.data!;
};

/** 立即校准（G3）：对近期有过代币充值的用户批量实测，离线用户自动跳过 */
export const runCalibration = async () => {
    const res = await post<{ checked: number; calibrated: number; offline: number; diff_found: number }>(API_ENDPOINTS.VIRTUAL_PAY.CALIBRATE, {});
    return res.data!;
};
