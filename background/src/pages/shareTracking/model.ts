import type { IntentEvidence } from '../../api/shareTracking';
export type Filters = { range: string; start: string; end: string; identity: string; intent: string; owner: string; q: string; sort: string; direction: string };
export const defaultFilters: Filters = { range: '7', start: '', end: '', identity: '', intent: '', owner: '', q: '', sort: 'last_visit', direction: 'desc' };
export function shanghaiDate(now = new Date()): string { return new Date(now.getTime() + 8 * 3600000).toISOString().slice(0, 10); }
export function dateRange(filters: Filters, now = new Date()): { start?: string; end?: string } {
    if (filters.range === 'all') return {};
    let start: string, end: string;
    if (filters.range === 'custom') {
        start = filters.start; end = filters.end;
        const valid = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value;
        if (!valid(start) || !valid(end) || start > end) throw new Error('请选择有效的起止日期，开始日期不能晚于结束日期');
    } else {
        const days = Number(filters.range);
        if (![3, 7, 30].includes(days)) throw new Error('无效时间范围');
        end = shanghaiDate(now);
        start = new Date(Date.parse(end) - (days - 1) * 86400000).toISOString().slice(0, 10);
    }
    return { start: new Date(`${start}T00:00:00+08:00`).toISOString(), end: new Date(Date.parse(`${end}T00:00:00+08:00`) + 86400000).toISOString() };
}
export function queryFor(filters: Filters, now = new Date()): Record<string, unknown> { return { ...dateRange(filters, now), identity: filters.identity, intent: filters.intent, owner: filters.owner, q: filters.q.trim(), sort: filters.sort, direction: filters.direction, limit: 20 }; }
export function time(value?: string): string { if (!value) return '未记录'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '时间不可用' : new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date); }
export function duration(ms = 0): string { const seconds = Math.floor(Math.max(0, ms) / 1000); return seconds < 60 ? `${seconds}秒` : seconds < 3600 ? `${Math.floor(seconds / 60)}分${seconds % 60}秒` : `${Math.floor(seconds / 3600)}小时${Math.floor(seconds % 3600 / 60)}分`; }
export function safeMediaURL(value?: string): string | undefined { if (!value) return; try { const url = new URL(value, window.location.origin); return (url.protocol === 'https:' || (url.origin === window.location.origin && url.protocol === window.location.protocol)) && !url.username && !url.password ? url.href : undefined; } catch { return; } }
export const intentLabels = { high: '高意向参考', medium: '中意向参考', low: '低关注待观察', unknown: '待判断' };
export const eventLabels: Record<string, string> = { share_intent: '发起分享（非送达）', page_view: '页面访问', page_end: '页面离开', visible_segment: '可见分段', content_click: '内容点击（非已读）', image_open: '打开原图', outbound_click: '点击外部原文（外部停留未知）', scroll_depth: '滚动到达位置', video_state: '播放状态', video_progress: '播放进度（非观看完成）', business_action_confirmed: '已验证业务行为' };
export function qualityText(flags: string[] = []): string { const names: Record<string, string> = { late: '离线或迟到补报', incomplete: '结束事件缺失，记录不完整', inferred_seek: '推断播放跳转', sampling_gap: '采样间隔缺失', preview: '后台预览', self: '分享人自访', test: '测试流量', sequence_conflict: '分段序号冲突', clock_skew: '客户端时间偏差', clamped: '异常增量已封顶' }; return flags.map(flag => names[flag] || flag).join('、'); }
export function intentMetricText(value: string): string { const labels: Record<string, string> = { sessions: '有效会话', independent_sessions: '独立会话', favorites: '已确认收藏', server_favorite: '服务端确认收藏', business_results: '已确认业务结果', server_consultation: '服务端确认咨询', server_appointment: '服务端确认预约', server_quote: '服务端确认报价' }; return labels[value] || value; }
export function intentExclusionText(value: string): string { const labels: Record<string, string> = { anonymous_identity: '匿名身份', preview: '后台预览', self: '分享人自访', test: '测试流量', abnormal: '异常流量', duplicate: '重复事件', client_click: '客户端点击', unverified_business: '未经服务端确认的业务行为', daily_contribution_over_cap: '超过每日贡献上限的部分' }; return labels[value] || value; }

export function intentWindowQuery(query: Record<string, unknown>, evidence?: IntentEvidence): Record<string, unknown> | undefined {
    if (!evidence || evidence.scope_required) return;
    const start = Date.parse(evidence.window_start), end = Date.parse(evidence.window_end);
    if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end) return;
    // Let the server include the exact as_of boundary; preserve its timestamp precision.
    return { ...query, start: evidence.window_start, end: undefined, as_of: evidence.window_end, cursor: undefined, content_id: undefined, identity: undefined, intent: undefined, q: undefined };
}
