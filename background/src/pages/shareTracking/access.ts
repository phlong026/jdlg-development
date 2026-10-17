import { useEffect, useRef, useState } from 'react';
import type { Context } from '../../api/shareTracking';
import { defaultFilters, type Filters } from './model';

const accessDeniedEvent = 'tracking-access-denied';
const storagePrefix = 'tracking-view-v2:';
type StoredView = { filters: Filters; view: 'visitors' | 'contents' | 'channels' };
type AccessState = { actorId?: number; data?: Context; loading: boolean; error: string; notice?: string; permissionBlocked?: boolean };

export interface TrackingAccessBoundary { readonly actorId?: number; readonly revision: number; }
let boundary: TrackingAccessBoundary = { revision: 0 };
function nextBoundary(actorId: number | undefined) {
    boundary = { actorId, revision: boundary.revision + 1 };
    return boundary;
}
export function captureTrackingAccessBoundary() { return boundary; }
export function notifyTrackingAccessDenied(error: unknown, started: TrackingAccessBoundary, signal?: AbortSignal) {
    if (!signal?.aborted && started === boundary && (error as { status?: number })?.status === 403) {
        window.dispatchEvent(new Event(accessDeniedEvent));
    }
}


function validContext(value: unknown): value is Context {
    if (!value || typeof value !== 'object') return false;
    const data = value as Partial<Context>;
    if (typeof data.can_view !== 'boolean' || typeof data.can_manage !== 'boolean' || !Array.isArray(data.owners)) return false;
    if (['can_crm', 'can_delete', 'can_rules'].some(key => {
        const permission = data[key as keyof Context];
        return permission !== undefined && typeof permission !== 'boolean';
    })) return false;
    const ids = new Set<string>();
    for (const owner of data.owners) {
        if (!owner || typeof owner.id !== 'string' || !owner.id || owner.id.trim() !== owner.id || [...owner.id].length > 64 || /[\u0000-\u001f\u007f]/.test(owner.id) || typeof owner.name !== 'string' || ids.has(owner.id)) return false;
        ids.add(owner.id);
    }
    if ((data.can_view || data.can_manage) && !ids.size) return false;
    return true;
}

// The actor comes only from the verified management session (/management/me).
// Injecting the loader avoids an API -> access -> API runtime dependency cycle.
export function useTrackingAccess(actorId: number | undefined, loadContext: (signal?: AbortSignal) => Promise<Context>): AccessState & { revision: number; reload: () => void } {
    const [state, setState] = useState<AccessState & { boundary?: TrackingAccessBoundary }>({ loading: true, error: '' });
    const [attempt, setAttempt] = useState(0);
    const generation = useRef(0);
    const controller = useRef<AbortController>();
    // A stale granting context must not drive an endless mount -> 403 -> reload loop.
    const automaticCheck = useRef({ actorId, used: false });
    if (!Object.is(automaticCheck.current.actorId, actorId)) automaticCheck.current = { actorId, used: false };
    const actor = useRef(actorId); actor.current = actorId;
    // Invalidate the old actor before effects or any descendants can start work.
    if (!Object.is(boundary.actorId, actorId)) nextBoundary(actorId);
    function reload(permissionChanged = false) {
        if (!Object.is(actor.current, actorId) || !Object.is(boundary.actorId, actorId)) return;
        const started = nextBoundary(actorId);
        ++generation.current;
        controller.current?.abort();
        if (permissionChanged && automaticCheck.current.used) {
            setState({ actorId, boundary: started, loading: false, permissionBlocked: true, error: '权限再次被拒绝，已清除旧记录。请重新检查权限后重试。' });
            return;
        }
        automaticCheck.current.used = permissionChanged;
        setState({ actorId, loading: true, error: '', notice: permissionChanged ? '访问权限发生变化，已清除旧记录并重新核对当前范围。' : '' });
        setAttempt(value => value + 1);
    }
    useEffect(() => {
        const denied = () => reload(true);
        window.addEventListener(accessDeniedEvent, denied);
        return () => window.removeEventListener(accessDeniedEvent, denied);
    }, [actorId]);
    useEffect(() => {
        const id = ++generation.current;
        const started = nextBoundary(actorId);
        if (!Number.isSafeInteger(actorId) || (actorId ?? 0) <= 0) {
            setState({ actorId, boundary: started, loading: false, error: '当前管理账号未确认，请重新登录。' });
            return;
        }
        const current = new AbortController(); controller.current = current;
        setState(previous => ({ actorId, boundary: started, loading: true, error: '', notice: Object.is(previous.actorId, actorId) ? previous.notice : '' }));
        loadContext(current.signal).then(data => {
            if (!validContext(data)) throw new Error('权限响应无效，请重新检查。');
            if (generation.current === id && Object.is(actor.current, actorId) && started === boundary && !current.signal.aborted) {
                setState(previous => ({ actorId, boundary: started, data, loading: false, error: '', notice: previous.notice }));
            }
        }).catch(error => {
            if (generation.current === id && Object.is(actor.current, actorId) && started === boundary && !current.signal.aborted) {
                setState(previous => ({ actorId, boundary: started, loading: false, error: error instanceof Error ? error.message : '权限读取失败，请重试', notice: previous.notice }));
            }
        });
        return () => {
            ++generation.current; current.abort();
            if (boundary === started) nextBoundary(undefined);
        };
    }, [actorId, attempt, loadContext]);
    const visible = Object.is(state.actorId, actorId) && state.boundary === boundary;
    return { ...(visible ? state : { loading: true, error: '' }), revision: boundary.revision, reload: () => reload(false) };
}

function storageKey(actorId: number | undefined) {
    return Number.isSafeInteger(actorId) && (actorId ?? 0) > 0 ? storagePrefix + actorId : undefined;
}
export function normalizeTrackingView(value: unknown, owners: Context['owners']): StoredView {
    const saved = value && typeof value === 'object' ? value as Partial<StoredView> : {};
    const filters: Filters = { ...defaultFilters };
    const stored: Partial<Filters> = saved.filters && typeof saved.filters === 'object' ? saved.filters : {};
    for (const key of [...Object.keys(defaultFilters), 'content_q', 'visitor_q'] as (keyof Filters)[]) {
        const value = stored[key];
        if (typeof value === 'string') filters[key] = value;
    }
    if (typeof filters.owner !== 'string' || !owners.some(owner => owner.id === filters.owner)) filters.owner = '';
    const view = saved.view === 'contents' ? 'contents' : saved.view === 'channels' ? 'channels' : 'visitors';
    if (view === 'contents') filters.intent = '';
    return { filters, view };
}
export function loadTrackingView(actorId: number | undefined, owners: Context['owners']): StoredView {
    const key = storageKey(actorId);
    try { return normalizeTrackingView(key ? JSON.parse(sessionStorage.getItem(key) || '{}') : {}, owners); }
    catch { return normalizeTrackingView({}, owners); }
}
export function saveTrackingView(actorId: number | undefined, owners: Context['owners'], value: StoredView) {
    const key = storageKey(actorId);
    if (!key) return;
    try { sessionStorage.setItem(key, JSON.stringify(normalizeTrackingView(value, owners))); }
    catch { /* Storage can be unavailable without affecting authorization. */ }
}
