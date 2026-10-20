// 客户页的标签：沿用现有阶段枚举（new/following/won/paused/invalid），「全部」包含新客户；公海是独立队列。
export type CustomerTabKey = 'all' | 'following' | 'won' | 'paused' | 'invalid' | 'pool';

export const customerTabs: { key: CustomerTabKey; label: string }[] = [
    { key: 'all', label: '全部' },
    { key: 'following', label: '跟进中' },
    { key: 'won', label: '已成交' },
    { key: 'paused', label: '暂缓' },
    { key: 'invalid', label: '无效' },
    { key: 'pool', label: '公海' },
];

export const resolveCustomerTab = (value: string | null | undefined): CustomerTabKey =>
    customerTabs.find((tab) => tab.key === value)?.key ?? 'all';

/** 标签对应的列表筛选：公海走 queue=pool，阶段标签走 stage，其余沿用 queue=all。 */
export function customerFilter(tab: CustomerTabKey): { queue: string; stage?: string } {
    if (tab === 'pool') return { queue: 'pool' };
    if (tab === 'all') return { queue: 'all' };
    return { queue: 'all', stage: tab };
}
