export type PageLoadFailure = { path: string; message: string };

export function reportPageLoadError(error: unknown, fallback: string) {
    const failure = error as { message?: string; requestPage?: string } | null;
    const detail: PageLoadFailure = {
        path: failure?.requestPage || window.location.pathname,
        message: failure?.message && !fallback.includes(failure.message) ? `${fallback}：${failure.message}` : fallback,
    };
    window.dispatchEvent(new CustomEvent('admin-page-load-error', { detail }));
}
