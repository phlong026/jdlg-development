import { useSyncExternalStore } from 'react';

// Counts belong to one verified session. A late request from a previous login
// must not repopulate the navigation after logout or a permission refresh.
type Counts = Record<string, number>;
type LeadOverviewSession = Readonly<{ generation: number; userId: number | null }>;
let session: LeadOverviewSession = { generation: 0, userId: null };
let counts: Counts = {};
const listeners = new Set<() => void>();
const notify = () => listeners.forEach(listener => listener());

export function resetLeadOverviewSession(userId: number | null = null): void {
    session = { generation: session.generation + 1, userId };
    counts = {};
    notify();
}
export const getLeadOverviewSession = (): LeadOverviewSession => session;
export const isLeadOverviewSessionCurrent = (request: LeadOverviewSession): boolean => request === session && session.userId !== null;

export function publishLeadOverview(next: Counts, request: LeadOverviewSession): void {
    if (!isLeadOverviewSessionCurrent(request)) return;
    counts = { ...next };
    notify();
}

export function useLeadOverviewCounts(): Counts {
    return useSyncExternalStore(
        listener => { listeners.add(listener); return () => listeners.delete(listener); },
        () => counts,
    );
}
