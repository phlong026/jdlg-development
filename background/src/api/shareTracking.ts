import { request } from '../utils/request';
import { API_BASE_URL } from '../config/api';
import { expireSession } from '../utils/session';

export const TRACKING_API = '/api/v1/management/tracking';
export type Intent = 'high' | 'medium' | 'low' | 'unknown';
export interface Snapshot { as_of: string; metric_version: string; }
export interface Page<T> extends Snapshot { items: T[]; next_cursor?: string; identified_count: number; anonymous_count: number; }
export interface IntentRuleParameters { window_days: number; medium_sessions: number; medium_favorites: number; high_business_results: number; daily_session_cap: number; daily_action_cap: number; }
export interface IntentRuleConfiguration extends IntentRuleParameters { rule_version: string; revision: number; owner_id: string; effective_at: string; actor: number | string; reason: string; timezone: string; participating_metrics: string[]; exclusions: string[]; }
export interface IntentRecomputeResult { rule_version: string; evaluated_count: number; changed_count: number; as_of: string; }
export interface IntentRulesResult { current: IntentRuleConfiguration; history: IntentRuleConfiguration[]; recomputed?: IntentRecomputeResult; }
export interface IntentRuleInput extends IntentRuleParameters { expected_rule_version: string; reason: string; }
export interface IntentRecordEvidence { kind: 'page_view' | 'business'; record_id: string; session_id?: string; page_view_id?: string; action?: string; occurred_at: string; content_id?: string; content_version?: number; }
export interface IntentEvidence { metrics?: { sessions: number; favorites: number; business_results: number }; thresholds?: IntentRuleParameters; exclusions?: string[]; participating_metrics?: string[]; timezone?: string; override_state?: 'none' | 'active' | 'expired'; scope_required?: boolean; evidence?: IntentRecordEvidence[]; evidence_truncated?: boolean; level: Intent; reasons: string[]; rule_version: string; window_start: string; window_end: string; calculated_at?: string; manual_note?: string; original_rule?: IntentEvidence; override?: { level: Intent; reason: string; actor?: number | string; created_at?: string; expires_at: string }; }
export interface Visitor { visitor_key: string; nickname?: string; avatar_url?: string; identity_state: 'identified' | 'anonymous' | 'deleted'; crm_label?: string; last_visit: string; page_views: number; visible_ms: number; session_count: number; intent: IntentEvidence; }
export interface Asset { media_id: string; kind: 'image' | 'video'; url: string; width: number; height: number; mime: string; title?: string; }
export interface Content { read_only?: boolean; route_key?: string; version_mode?: string; content_id: string; version: number; revision: number; title: string; type: string; status: 'draft' | 'published' | 'revoked'; cover?: Asset; media: Asset[]; body: string; body_format?: 'plain' | 'rich_text'; source_url?: string; source_note?: string; rights: 'own' | 'authorized'; owner_id?: string; page_views: number; visible_ms: number; last_visit?: string; published_at?: string; }
export interface PageView { page_view_id: string; route_key: string; title?: string; content_id?: string; content_version?: number; previous_page_view_id?: string; share_id?: string; started_at: string; ended_at?: string; visible_ms: number; quality_flags: string[]; }
export interface Session { session_id: string; started_at: string; last_active_at: string; entry_share_id?: string; source: string; page_views: PageView[]; }
export interface TrackingEvent { event_id: string; event_type: string; occurred_at: string; received_at: string; content_version?: number; target_content_id?: string; visible_delta_ms?: number; scroll_depth?: number; current_time_ms?: number; video_watch_ms?: number; unique_played_ms?: number; video_duration_ms?: number; quality_flags: string[]; }
export interface Context { can_view: boolean; can_manage: boolean; can_crm?: boolean; can_rules?: boolean; can_delete?: boolean; owners: { id: string; name: string }[]; share_methods?: string[]; }
export interface DraftInput { title: string; body: string; body_format?: 'plain' | 'rich_text'; source_url: string; source_note: string; rights: 'own' | 'authorized'; owner_id: string; cover_media_id: string; media_ids: string[]; expected_revision?: number; }
export interface ShareMethods { message?: string; miniprogram_code_url?: string; url_link?: string; expires_at?: string; }
export interface PublishResult { content: Content; share_methods: ShareMethods; }
export async function trackingRequest<T>(resource: string, options: { signal?: AbortSignal; params?: Record<string, unknown>; method?: string; body?: unknown } = {}): Promise<T> {
    const controller = new AbortController(); let timedOut = false;
    const abort = () => controller.abort();
    if (options.signal?.aborted) controller.abort();
    options.signal?.addEventListener('abort', abort, { once: true });
    const timeout = setTimeout(() => { timedOut = true; controller.abort(); }, 20000);
    try {
        const result = await request<T>(TRACKING_API + resource, { method: options.method || 'GET', signal: controller.signal, params: options.params, headers: { 'X-Tracking-Request': '1' }, body: options.body === undefined ? undefined : JSON.stringify(options.body) });
        if (result.data === undefined || result.data === null) throw new Error('服务未返回有效数据，请重试');
        return result.data;
    } catch (error) {
        if (timedOut) throw new Error(options.method && options.method !== 'GET' ? '请求超时，操作结果尚未确认。请保留草稿并核对状态后重试' : '读取超时，请重试');
        throw error;
    } finally { clearTimeout(timeout); options.signal?.removeEventListener('abort', abort); }
}
export const tracking = {
    intentRules: (owner: string, signal?: AbortSignal) => trackingRequest<IntentRulesResult>('/intent-rules', { params: { owner }, signal }),
    saveIntentRules: (body: IntentRuleInput, owner: string) => trackingRequest<IntentRulesResult>('/intent-rules', { method: 'POST', body, params: { owner } }),
    recomputeIntent: (body: { request_id: string; reason: string; rule_version: string }, owner: string) => trackingRequest<IntentRecomputeResult>('/intent-rules/recompute', { method: 'POST', body, params: { owner } }),
    linkCRM: (body: { visitor_key: string; customer_id: string; business_number: string; basis: string }, owner: string) => trackingRequest<{ linked: boolean }>('/identity-links', { method: 'POST', body, params: { owner } }),
    unlinkCRM: (visitor: string, owner: string) => trackingRequest<{ unlinked: boolean }>(`/identity-links/${encodeURIComponent(visitor)}`, { method: 'DELETE', params: { owner } }),
    overrideIntent: (body: { visitor_key: string; level: Intent; reason: string; expires_at: string }, owner: string) => trackingRequest<{ updated: boolean }>('/intent-overrides', { method: 'POST', body, params: { owner } }),
    deleteVisitor: (body: { visitor_key: string; reason: string }) => trackingRequest<{ status: string }>('/deletions', { method: 'POST', body }),
    context: (signal?: AbortSignal) => trackingRequest<Context>('/context', { signal }),
    visitors: (params: Record<string, unknown>, signal?: AbortSignal) => trackingRequest<Page<Visitor>>('/visitors', { params, signal }),
    contents: (params: Record<string, unknown>, signal?: AbortSignal) => trackingRequest<Page<Content>>('/contents', { params, signal }),
    content: (id: string, params: Record<string, unknown>, signal?: AbortSignal) => trackingRequest<Content & Snapshot>(`/contents/${encodeURIComponent(id)}`, { params, signal }),
    contentVisitors: (id: string, params: Record<string, unknown>, signal?: AbortSignal) => trackingRequest<Page<Visitor>>(`/contents/${encodeURIComponent(id)}/visitors`, { params, signal }),
    sessions: (id: string, params: Record<string, unknown>, signal?: AbortSignal) => trackingRequest<Page<Session>>(`/visitors/${encodeURIComponent(id)}/sessions`, { params, signal }),
    events: (id: string, params: Record<string, unknown>, signal?: AbortSignal) => trackingRequest<Page<TrackingEvent>>(`/page-views/${encodeURIComponent(id)}/events`, { params, signal }),
    media: (signal?: AbortSignal, cursor?: string) => trackingRequest<Page<Asset>>('/media', { signal, params: { cursor, limit: 20 } }),
    upload: async (file: File, rights: 'own' | 'authorized', owner: string): Promise<Asset> => {
        const body = new FormData(); body.append('file', file); body.append('rights', rights); body.append('owner_id', owner);
        const response = await fetch(API_BASE_URL + TRACKING_API + '/media', { method: 'POST', credentials: 'include', headers: { 'X-Tracking-Request': '1' }, body, signal: AbortSignal.timeout(120000) });
        if (response.status === 401) expireSession();
        const result = await response.json().catch(() => null);
        if (!response.ok || !result || ![0, 200].includes(result.code) || !result.data) throw new Error(result?.msg || result?.message || `上传失败（HTTP ${response.status}）`);
        return result.data;
    },
    save: (body: DraftInput, id?: string) => trackingRequest<Content>(id ? `/contents/${encodeURIComponent(id)}` : '/contents', { method: id ? 'PATCH' : 'POST', body }),
    preview: (id: string, revision: number) => trackingRequest<{ content: Content; preview_token: string }>(`/contents/${encodeURIComponent(id)}/preview`, { method: 'POST', body: { expected_revision: revision } }),
    publish: (id: string, revision: number, previewToken: string, key: string) => trackingRequest<PublishResult>(`/contents/${encodeURIComponent(id)}/publish`, { method: 'POST', body: { expected_revision: revision, preview_token: previewToken, idempotency_key: key } }),
    revoke: (id: string, revision: number, reason: string, key: string) => trackingRequest<Content>(`/contents/${encodeURIComponent(id)}/revoke`, { method: 'POST', body: { expected_revision: revision, reason, idempotency_key: key } }),
};
