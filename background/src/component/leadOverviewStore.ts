import { useSyncExternalStore } from 'react';

// 线索页加载数据后把"各队列数量"发布到这里，子导航条的队列 Tab 订阅它显示角标。
// 这样角标不用再多发一次请求，也会随页面里的操作（分配、跟进）一起刷新。
type Counts = Record<string, number>;

let counts: Counts = {};
const listeners = new Set<() => void>();

export function publishLeadOverview(next: Counts): void {
    counts = { ...next };
    listeners.forEach((listener) => listener());
}

export function useLeadOverviewCounts(): Counts {
    return useSyncExternalStore(
        (listener) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
        },
        () => counts,
    );
}
