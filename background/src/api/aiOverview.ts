import { get, put } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// AI 运营总览数据（全部为后端对现有任务表的只读聚合）
export interface AIOverviewKPI {
    total: number;
    failed: number;
    success_rate: number;
    stones_revenue: number;
    cost_yuan: number;
    margin_rate?: number;
}

export interface AIOverviewTrendPoint {
    date: string;
    total: number;
    failed: number;
    video_total: number;
}

export interface AIOverviewScene {
    scene: string;
    label: string;
    total: number;
    success: number;
    failed: number;
    failed_rate: number;
    stones: number;
    cost_yuan: number;
    margin_rate: number;
}

export interface AIOverviewSupplier {
    role: 'primary' | 'fallback_1' | 'fallback_2' | 'other';
    model: string;
    provider_name: string;
    provider_code: string;
    protocol: string;
    protocol_label: string;
    note?: string;
    total_7d: number;
    failed_rate_7d: number;
    avg_seconds_7d: number;
    window_total_1h: number;
    window_failed_rate_1h: number;
    alerting: boolean;
    cost_yuan_per_unit: number;
}

export interface AIOverviewChainNode {
    role: string;
    model: string;
    provider_name: string;
    provider_code: string;
    protocol: string;
    protocol_label: string;
    note?: string;
}

export interface AIOverviewFailReason {
    category: string;
    count: number;
}

export interface AIOverviewToolTop {
    tool_id: number;
    name: string;
    total_7d: number;
}

export interface AIOverviewQueues {
    failed: number;
    timeout: number;
    running: number;
    today_new: number;
    total_open: number;
}

export interface AIOverviewAlert {
    id: number;
    title: string;
    priority: string;
    created_at: string;
}

export interface AIOverviewData {
    kpi: {
        today: AIOverviewKPI;
        yesterday: AIOverviewKPI;
        cost_note: string;
        /** 今日成本中来自任务级落库（api_cost）的占比 0~1；上线初期接近 0，随新任务积累趋近 1 */
        cost_landed_ratio?: number;
    };
    trend: AIOverviewTrendPoint[];
    scenes: AIOverviewScene[];
    video_7d: { total: number; failed: number; segments: number; stones: number; cost_yuan: number };
    suppliers: AIOverviewSupplier[];
    chain: AIOverviewChainNode[];
    fallback_out_7d: number;
    fail_reasons: AIOverviewFailReason[];
    tools_top: AIOverviewToolTop[];
    queues: { image: AIOverviewQueues | null; video: AIOverviewQueues | null };
    alerts: AIOverviewAlert[];
    /** 降级链调序建议（只读规则判断：备用2 两项指标均优于备用1 且样本充足时建议对调） */
    chain_suggestion?: {
        suggested: boolean;
        swap?: string[];
        reason: string;
    } | null;
}

export const getAIOverview = async (): Promise<AIOverviewData> => {
    const res = await get<AIOverviewData>(API_ENDPOINTS.AI_OVERVIEW.OVERVIEW);
    return res.data!;
};

// ── 定价运营视角 ──
export interface AIPricingOpsItem {
    scene: string;
    label: string;
    stones: number;
    usage_7d: number;
    success_7d: number;
    failed_rate_7d: number;
    stones_revenue_7d: number;
    cost_yuan_est: number;
    margin_rate: number;
    updated_at: string;
    latest_version?: {
        old_stones: number;
        new_stones: number;
        operator: string;
        note: string;
        created_at: string;
    };
}

export const getAIPricingOpsList = async (): Promise<AIPricingOpsItem[]> => {
    const res = await get<AIPricingOpsItem[]>(API_ENDPOINTS.AI.PRICING_OPS);
    return res.data || [];
};

export interface AIPricingVersion {
    id: number;
    scene: string;
    old_stones: number;
    new_stones: number;
    operator: string;
    note: string;
    created_at: string;
}

export const getAIPricingVersions = async (scene: string): Promise<AIPricingVersion[]> => {
    const res = await get<AIPricingVersion[]>(API_ENDPOINTS.AI.PRICING_VERSIONS, { scene });
    return res.data || [];
};

// ── AI 运营告警设置（供应商管理页） ──
export interface AIOpsSettings {
    alert_failed_rate: number;
    alert_min_window_calls: number;
    alert_interval_minutes: number;
    /** 内容抽检（4a） */
    audit_enabled: boolean;
    audit_sample_rate: number;
    updated_at?: string;
}

export const getAIOpsSettings = async (): Promise<AIOpsSettings> => {
    const res = await get<AIOpsSettings>(API_ENDPOINTS.AI_OVERVIEW.OPS_SETTINGS);
    return res.data!;
};

export const saveAIOpsSettings = async (settings: AIOpsSettings): Promise<AIOpsSettings> => {
    const res = await put<AIOpsSettings>(API_ENDPOINTS.AI_OVERVIEW.OPS_SETTINGS, settings);
    return res.data!;
};
