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
