import { get, post, put } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export type AIVideoStudioMode = 'text' | 'first_frame' | 'first_last_frame' | 'reference';
export type BillingUnit = 'per_request' | 'per_video' | 'per_second';

export interface AIVideoStudioPrice {
    code: string;
    label: string;
    billing_unit: BillingUnit;
    unit_stones: number | null;
    enabled: boolean;
    version?: number;
}

export interface AIVideoStudioModeReadiness {
    enabled: boolean;
    disabled_reason: string;
}

export interface AIVideoStudioConfig {
    version: number;
    enabled: boolean;
    provider_code: string;
    provider_display_name: string;
    provider_configured: boolean;
    execution_ready?: boolean;
    accounting_ready?: boolean;
    billing_ready?: boolean;
    media_storage_ready?: boolean;
    media_signing_ready?: boolean;
    ffprobe_ready?: boolean;
    new_tasks_ready?: boolean;
    disabled_reason?: string;
    mode_readiness?: Partial<Record<AIVideoStudioMode, AIVideoStudioModeReadiness>>;
    modes: Record<AIVideoStudioMode, boolean>;
    limits: {
        max_concurrency: number;
        timeout_seconds: number;
        max_quantity: number;
        max_duration_768p: number;
        max_duration_2k: number;
        max_reference_images: number;
        max_reference_videos: number;
        max_reference_audios: number;
        max_reference_total: number;
    };
    prices: AIVideoStudioPrice[];
    updated_at?: string;
}

export interface AIVideoStudioTask {
    id: string;
    batch_id: string;
    user_id: number;
    draft_id: number;
    quote_id: string;
    mode: AIVideoStudioMode;
    status: string;
    fee_status: string;
    total_stones: number;
    error_code?: string;
    error_message?: string;
    created_at: string;
    updated_at: string;
}

export interface AIVideoStudioLedgerItem {
    id: string;
    batch_id: string;
    task_id?: string;
    user_id: number;
    action: string;
    stones: number;
    status: string;
    price_code: string;
    price_version: number;
    created_at: string;
}

export interface PageResult<T> {
    items: T[];
    total: number;
    page: number;
    page_size: number;
}

export interface AIVideoStudioResult {
    result_id: string;
    status: string;
    external_id?: string;
    error_message?: string;
}

export interface AIVideoStudioBatchDetail {
    batch_id: string;
    status: string;
    items: AIVideoStudioResult[];
    charge: { quoted_stones?: number; total_stones?: number; settled_stones?: number; released_stones?: number };
}

export type StudioReconciliationAction = 'query' | 'attach_external_id' | 'refund_confirmed_rejected';
export interface StudioReconciliationInput {
    action: StudioReconciliationAction;
    evidence: string;
    external_id?: string;
}

const defaultPriceCatalog: AIVideoStudioPrice[] = [
    { code: 'ai_video_studio_prompt_optimize', label: '提示词优化', billing_unit: 'per_request', unit_stones: null, enabled: false },
    { code: 'ai_video_studio_768p', label: '768P 视频生成', billing_unit: 'per_second', unit_stones: null, enabled: false },
    { code: 'ai_video_studio_2k', label: '2K 视频生成', billing_unit: 'per_second', unit_stones: null, enabled: false },
    { code: 'ai_video_studio_768p_long', label: '768P 长档生成', billing_unit: 'per_second', unit_stones: null, enabled: false },
];

export function normalizeAIVideoStudioConfig(config: AIVideoStudioConfig): AIVideoStudioConfig {
    const byCode = new Map((config.prices || []).map((price) => [price.code, price]));
    const prices = defaultPriceCatalog.map((fallback) => ({ ...fallback, ...byCode.get(fallback.code) }));
    return {
        ...config,
        provider_code: config.provider_code || 'metaso_h3',
        provider_display_name: config.provider_display_name || '秘塔 H3',
        modes: { text: false, first_frame: false, first_last_frame: false, reference: false, ...(config.modes || {}) },
        limits: {
            max_concurrency: 3, timeout_seconds: 900, max_quantity: 1,
            max_duration_768p: 15, max_duration_2k: 15,
            max_reference_images: 8, max_reference_videos: 3,
            max_reference_audios: 3, max_reference_total: 12,
            ...(config.limits || {}),
        },
        prices,
    };
}

export const getAIVideoStudioConfig = async (): Promise<AIVideoStudioConfig> => {
    const response = await get<AIVideoStudioConfig>(API_ENDPOINTS.AI_VIDEO_STUDIO.CONFIG);
    return normalizeAIVideoStudioConfig(response.data!);
};

export const saveAIVideoStudioConfig = async (config: AIVideoStudioConfig): Promise<AIVideoStudioConfig> => {
    const {
        version,
        provider_configured: _providerConfigured,
        provider_display_name: _providerName,
        execution_ready: _executionReady,
        accounting_ready: _accountingReady,
        billing_ready: _billingReady,
        media_storage_ready: _mediaStorageReady,
        media_signing_ready: _mediaSigningReady,
        ffprobe_ready: _ffprobeReady,
        new_tasks_ready: _newTasksReady,
        disabled_reason: _disabledReason,
        mode_readiness: _modeReadiness,
        updated_at: _updatedAt,
        ...editable
    } = config;
    const response = await put<AIVideoStudioConfig>(API_ENDPOINTS.AI_VIDEO_STUDIO.CONFIG, {
        ...editable,
        expected_version: version,
    });
    return normalizeAIVideoStudioConfig(response.data!);
};

export const getAIVideoStudioTasks = async (params: Record<string, string | number | undefined> = {}): Promise<PageResult<AIVideoStudioTask>> => {
    const response = await get<PageResult<AIVideoStudioTask>>(API_ENDPOINTS.AI_VIDEO_STUDIO.TASKS, params);
    const page = response.data || { items: [], total: 0, page: 1, page_size: 20 };
    return { ...page, items: (page.items || []).map((item) => ({ ...item, id: item.id || item.batch_id })) };
};

export const getAIVideoStudioTask = async (id: string): Promise<AIVideoStudioBatchDetail> => {
    const response = await get<AIVideoStudioBatchDetail>(API_ENDPOINTS.AI_VIDEO_STUDIO.TASK_DETAIL(id));
    return response.data!;
};

export const reconcileAIVideoStudioResult = async (id: string, input: StudioReconciliationInput): Promise<void> => {
    await post(API_ENDPOINTS.AI_VIDEO_STUDIO.RECONCILE(id), input);
};

export const getAIVideoStudioLedger = async (params: Record<string, string | number | undefined> = {}): Promise<PageResult<AIVideoStudioLedgerItem>> => {
    const response = await get<PageResult<AIVideoStudioLedgerItem>>(API_ENDPOINTS.AI_VIDEO_STUDIO.LEDGER, params);
    return response.data || { items: [], total: 0, page: 1, page_size: 20 };
};
