/** "5 分钟前" 之类的相对时间；空值与非法时间返回 empty。 */
export function timeAgo(value: string | null | undefined, now: number = Date.now(), empty = '尚未跟进'): string {
    if (!value) return empty;
    const then = new Date(value).getTime();
    if (Number.isNaN(then)) return empty;
    const minutes = Math.floor(Math.max(0, now - then) / 60000);
    if (minutes < 1) return '刚刚';
    if (minutes < 60) return `${minutes} 分钟前`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} 小时前`;
    const days = Math.floor(hours / 24);
    return days < 30 ? `${days} 天前` : `${Math.floor(days / 30)} 个月前`;
}

/** 未认领的公海客户只显示号码首尾：13812345678 → 138****5678。 */
export function maskPhone(phone: string | null | undefined): string {
    const digits = (phone || '').trim();
    if (digits.length < 7) return digits ? '****' : '';
    return `${digits.slice(0, 3)}****${digits.slice(-4)}`;
}

/** 漏斗层名：只有"进入客服"带匿名次数（匿名用户没有账号，按次计入人数）。 */
export function funnelStepLabel(step: { label: string; anonymous_sessions?: number }): string {
    return step.anonymous_sessions ? `${step.label}（含匿名 ${step.anonymous_sessions} 次）` : step.label;
}

// 同一次会话里填表、点生成会连续更新同一条线索，相隔不到 1 小时的不算再次留资
const RESUBMIT_GAP_MS = 60 * 60 * 1000;

/** 再次留资提示：老客户再次留资只会更新旧线索，静默保存也会写留资事件，所以叫"再次留资"而非"再次咨询"。 */
export function resubmitHint(lead: { created_at?: string; last_submitted_at?: string | null }, now: number = Date.now()): string {
    const created = Date.parse(lead.created_at || '');
    const last = Date.parse(lead.last_submitted_at || '');
    if (Number.isNaN(created) || Number.isNaN(last) || last - created < RESUBMIT_GAP_MS) return '';
    return `再次留资 · ${timeAgo(lead.last_submitted_at, now)}`;
}
