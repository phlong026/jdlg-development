import { get, post } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface CustomerLeadOverview {
    total_count: number;
    new_count: number;
    contacted_count: number;
    high_intent_count: number;
    converted_count: number;
    today_new_count?: number;
    todo_count?: number;
    overdue_count?: number;
    high_todo_count?: number;
    unassigned_count?: number;
    invalid_count?: number;
    /** 合作线索（设计师、施工队等 B 端身份）：待处理条数与全部条数；以上其余计数只含销售线索 */
    partner_todo_count?: number;
    partner_count?: number;
    /** 待处理销售线索中尚未选择身份的条数 */
    identity_pending_count?: number;
}

/** sales=乡墅业主；partner=设计师/施工队/建材商/机构用户；identity_pending=账号尚未选择身份 */
export type CustomerLeadType = 'sales' | 'partner' | 'identity_pending';

export interface CustomerLeadItem {
    id: number;
    user_id: number;
    name?: string;
    phone?: string;
    wechat?: string;
    enterprise_wechat_contact?: string;
    session_no: string;
    source: string;
    source_task_no?: string;
    demand_summary: string;
    house_floors?: string;
    house_style?: string;
    land_width?: string;
    land_depth?: string;
    room_requirement?: string;
    intent_level: 'low' | 'medium' | 'high' | string;
    status: 'new' | 'contacted' | 'converted' | 'invalid' | string;
    assigned_to?: string;
    next_followup_at?: string | null;
    remark?: string;
    /** 线索写入时账号的自选身份（乡墅业主/设计师/…），空表示尚未选择 */
    identity_type?: string;
    lead_type?: CustomerLeadType | string;
    created_at: string;
    updated_at: string;
}

export interface CustomerServiceEventItem {
    id: number;
    user_id: number;
    lead_id: number;
    session_no: string;
    event_type: string;
    source: string;
    source_task_no: string;
    intent_level: string;
    demand_summary: string;
    payload?: Record<string, any> | null;
    created_at: string;
}

export interface CustomerLeadFollowupItem {
    id: number;
    lead_id: number;
    status: string;
    remark: string;
    assigned_to?: string;
    next_followup_at?: string | null;
    operator_id: number;
    operator_name: string;
    created_at: string;
}

export interface CustomerIntentAnalysis {
    level: string;
    score: number;
    reasons: string[];
}

export interface CustomerLeadListParams {
    keyword?: string;
    status?: string;
    intent_level?: string;
    queue?: string;
    assigned_to?: string;
    source?: string;
    lead_type?: string;
    user_id?: number;
    page?: number;
    page_size?: number;
}

export interface CustomerLeadListResponse {
    list: CustomerLeadItem[];
    total: number;
    page: number;
    page_size: number;
}

export interface CustomerLeadDetailResponse {
    lead: CustomerLeadItem;
    events: CustomerServiceEventItem[];
    followups: CustomerLeadFollowupItem[];
    summary: string;
    intent_analysis: CustomerIntentAnalysis;
}

export const getCustomerLeadOverview = async (): Promise<CustomerLeadOverview> => {
    const response = await get<CustomerLeadOverview>(API_ENDPOINTS.CUSTOMER_LEADS.OVERVIEW);
    if (!response.data) {
        throw new Error('获取客服线索概览失败');
    }
    return response.data;
};

export const getCustomerLeadList = async (params?: CustomerLeadListParams): Promise<CustomerLeadListResponse> => {
    const response = await get<CustomerLeadListResponse>(API_ENDPOINTS.CUSTOMER_LEADS.LIST, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取客服线索列表失败');
    }
    return response.data;
};

export const getCustomerLeadDetail = async (id: string): Promise<CustomerLeadDetailResponse> => {
    const response = await get<CustomerLeadDetailResponse>(API_ENDPOINTS.CUSTOMER_LEADS.DETAIL(id));
    if (!response.data) {
        throw new Error('获取客服线索详情失败');
    }
    return response.data;
};

export interface CustomerLeadActionPayload {
    status: string;
    remark: string;
    assigned_to?: string;
    next_followup_at?: string;
}

export const updateCustomerLeadStatus = async (id: string, payload: CustomerLeadActionPayload): Promise<void> => {
    await post(API_ENDPOINTS.CUSTOMER_LEADS.STATUS(id), payload);
};

export const createCustomerLeadFollowup = async (id: string, payload: CustomerLeadActionPayload): Promise<void> => {
    await post(API_ENDPOINTS.CUSTOMER_LEADS.FOLLOWUPS(id), payload);
};

export const batchUpdateCustomerLeads = async (ids: number[], payload: CustomerLeadActionPayload): Promise<void> => {
    await post(API_ENDPOINTS.CUSTOMER_LEADS.BATCH, { ids, ...payload });
};
