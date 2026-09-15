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
}

export interface OrderSummary {
    total_count: number;
    success_amount: number;
    success_count: number;
    pending_count: number;
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
        total: number;
        page: number;
        page_size: number;
    }>(API_ENDPOINTS.ORDERS.LIST, params);
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
