import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface OrderListItem {
    id: number;
    user_id: number;
    username: string;
    designer_user_id: number;
    template_id?: number | null;
    order_no: string;
    type: string;
    order_category: string;
    amount: number;
    status: string;
    review_status: string;
    pay_channel: string;
    pay_mode: string;
    product_id: string;
    amount_fen: number;
    deliver_reported: boolean;
    deliver_report_attempts: number;
    title: string;
    description: string;
    completed_at?: string | null;
    created_at: string;
    /** 该用户"微信侧代币在账"本地镜像（T3，用户维度；道具/消费类订单前端按列语义展示） */
    wechat_coin_mirror?: number;
    /** 微信实付金额（分）与比对结论（R3；无证据为 null/unknown） */
    wechat_paid_fen?: number | null;
    wechat_pay_match?: string;
    wxpay_order_id?: string;
}

/** 微信侧实付证据（R3，逐单两方对平） */
export interface WechatOrderEvidenceView {
    wechat_state: string;
    order_fee_fen?: number | null;
    paid_fee_fen?: number | null;
    refund_fee_fen?: number | null;
    left_fee_fen?: number | null;
    wxpay_order_id: string;
    channel_order_id: string;
    sett_state?: number | null;
    platform_fee_fen?: number | null;
    amount_match: string;
    queried_at: string;
}

export interface OrderSummary {
    total_count: number;
    success_amount: number;
    success_count: number;
    pending_count: number;
}

/** 全站资金统计带（G1，与列表筛选无关的全局口径） */
export interface OrderFundStats {
    today_recharge_fen: number;
    today_recharge_count: number;
    today_recharge_stones: number;
    wechat_mirror_total: number;
    local_stones_total: number;
    exception_count: number;
}

export interface OrderDetail {
    order: {
        id: number;
        user_id: number;
        designer_user_id: number;
        template_id?: number | null;
        order_no: string;
        type: string;
        order_category: string;
        amount: number;
        status: string;
        review_status: string;
        pay_channel: string;
        pay_mode: string;
        product_id: string;
        amount_fen: number;
        deliver_reported: boolean;
        deliver_report_attempts: number;
        title: string;
        description: string;
        completed_at?: string | null;
        created_at: string;
    };
    user?: {
        id: number;
        username: string;
        user_type: string;
        can_withdraw: boolean;
        created_at?: string;
    } | null;
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
    /** 资金视图（T3）：仅虚拟支付代币用户返回 */
    fund_view?: OrderFundView;
    /** 微信侧实付证据（R3）：已核对订单返回 */
    wechat_evidence?: WechatOrderEvidenceView | null;
}

export interface OrderListParams {
    keyword?: string;
    type?: string;
    order_category?: string;
    status?: string;
    pay_channel?: string;
    start_date?: string;
    end_date?: string;
    page?: number;
    page_size?: number;
}

export interface CreateSupportTicketResult {
    id: number;
    existed: boolean;
}

export interface VirtualPayRefundResult {
    order_no: string;
    out_refund_no: string;
    refund_amount: number;
    old_stones: number;
    new_stones: number;
    status: string;
}

export const getOrderList = async (params?: OrderListParams) => {
    const res = await get<{
        list: OrderListItem[];
        summary: OrderSummary;
        status_counts?: Record<string, number>;
        total: number;
        page: number;
        page_size: number;
    }>(API_ENDPOINTS.ORDERS.LIST, params);
    return res.data!;
};

export const getOrderFundStats = async () => {
    const res = await get<OrderFundStats>(API_ENDPOINTS.ORDERS.FUND_STATS);
    return res.data!;
};

export const getOrderDetail = async (id: string) => {
    const res = await get<OrderDetail>(API_ENDPOINTS.ORDERS.DETAIL(id));
    return res.data!;
};

export const createOrderSupportTicket = async (id: string) => {
    const res = await post<CreateSupportTicketResult>(API_ENDPOINTS.ORDERS.SUPPORT_TICKET(id), {});
    return res.data!;
};

export const refundVirtualPayOrder = async (id: string, reason?: string) => {
    const res = await post<VirtualPayRefundResult>(API_ENDPOINTS.ORDERS.VIRTUAL_PAY_REFUND(id), { reason: reason || '' });
    return res.data!;
};

/** 资金视图（T3）：虚拟支付用户的微信侧/本地两侧余额与差额 */
export interface OrderFundView {
    wechat_mirror?: number;
    local_stones?: number;
    consumed_net?: number;
    calibrated_at?: string;
    measured_balance?: number;
    diff?: number;
}

/** 退款影响预览（T8）：退款确认弹窗数据源 */
export interface RefundPreview {
    order_id: number;
    order_no: string;
    user_id: number;
    stones_to_deduct: number;
    current_stones: number;
    sufficient: boolean;
    amount_fen: number;
    membership_recover_plan: string;
    membership?: {
        plan_title: string;
        expired_at: string;
        is_lifetime: boolean;
        source_order_no: string;
        recover_plan: 'revoke' | 'restored_prev' | 'skipped_superseded';
        recover_detail?: string;
    };
}

export const getRefundPreview = async (id: string) => {
    const res = await get<RefundPreview>(API_ENDPOINTS.ORDERS.VIRTUAL_PAY_REFUND_PREVIEW(id));
    return res.data!;
};

/** 卡退款核对微信侧（T6）：微信侧已 REFUND 时服务端自动落终态 refunded（含会员回收） */
export const verifyRefundAgainstWechat = async (id: string) => {
    const res = await post<{ order_no: string; wechat_state: string; settled: boolean }>(API_ENDPOINTS.ORDERS.VIRTUAL_PAY_VERIFY_REFUND(id), {});
    return res.data!;
};

/** 卡发货立即推进（T6）：手动触发一次 confirm 链路 */
export const advanceVirtualPayOrder = async (id: string) => {
    const res = await post<{ order_no: string; state: string }>(API_ENDPOINTS.ORDERS.VIRTUAL_PAY_ADVANCE(id), {});
    return res.data!;
};
