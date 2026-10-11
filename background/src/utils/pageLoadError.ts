export type PageLoadFailure = { path: string; message: string; source?: string; cleared?: boolean };

export function updatePageLoadFailures(current: Record<string, string>, event: PageLoadFailure) {
    const next = { ...current };
    const source = event.source || event.message;
    if (event.cleared) delete next[source];
    else next[source] = event.message;
    return next;
}

export function reportPageLoadError(error: unknown, fallback: string) {
    const failure = error as { message?: string; requestPage?: string } | null;
    const detail: PageLoadFailure = {
        path: failure?.requestPage || window.location.pathname,
        source: fallback,
        message: failure?.message && !fallback.includes(failure.message) ? `${fallback}：${failure.message}` : fallback,
    };
    window.dispatchEvent(new CustomEvent('admin-page-load-error', { detail }));
}

// Clear only the data source that recovered, never an unrelated failed request.
export function reportPageLoadRecovered(source: string, path = window.location.pathname) {
    const detail: PageLoadFailure = { path, source, message: '', cleared: true };
    window.dispatchEvent(new CustomEvent('admin-page-load-error', { detail }));
}
