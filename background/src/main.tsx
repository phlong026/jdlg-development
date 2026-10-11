import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.scss'

const rootElement = document.getElementById('root');
if (!rootElement) {
    throw new Error('Root element not found');
}

const escapeHtml = (value: string) => value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

const recoverUpdatedAssets = (message: string) => {
    if (!/Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i.test(message)) return false;
    try {
        const key = 'admin-asset-reload-at';
        const previous = Number(sessionStorage.getItem(key) || 0);
        if (Date.now() - previous < 60_000) return false;
        sessionStorage.setItem(key, String(Date.now()));
        window.location.reload();
        return true;
    } catch { return false; }
};

window.addEventListener('vite:preloadError', (event) => {
    const payload = (event as Event & { payload?: Error }).payload;
    if (recoverUpdatedAssets(payload?.message || '')) event.preventDefault();
});

const renderBootstrapError = (detail: string) => {
    rootElement.innerHTML = `
        <div style="min-height:100vh;padding:24px;background:#fff7ed;display:flex;align-items:center;justify-content:center;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;">
            <div style="width:100%;max-width:960px;background:#ffffff;border:1px solid #fdba74;border-radius:16px;box-shadow:0 16px 40px rgba(0,0,0,0.08);padding:24px;">
                <h1 style="margin:0 0 12px;font-size:24px;line-height:1.4;color:#9a3412;">页面暂时无法加载</h1>
                <p style="font-size:14px;line-height:1.7;color:#7c2d12;">请刷新页面后重试。如果仍无法打开，请联系管理员。</p>
                <button id="reload-admin-page" type="button" style="min-height:44px;padding:10px 20px;border:0;border-radius:10px;background:#8b5e3c;color:white;cursor:pointer;">刷新页面</button>
                <details style="margin-top:20px;"><summary>查看错误详情</summary><pre style="white-space:pre-wrap;word-break:break-word;font-size:13px;line-height:1.6;overflow:auto;">${escapeHtml(detail)}</pre></details>
            </div>
        </div>
    `;
    document.getElementById('reload-admin-page')?.addEventListener('click', () => window.location.reload());
};

window.addEventListener('error', (event) => {
    // Browsers defer resize notifications to the next frame; this is not an app crash.
    if (event.message === 'ResizeObserver loop completed with undelivered notifications.' ||
        event.message === 'ResizeObserver loop limit exceeded') return;
    const detail = event.error instanceof Error
        ? (event.error.stack || event.error.message)
        : (event.message || '未知错误');
    if (recoverUpdatedAssets(detail)) return;
    renderBootstrapError(detail);
});

window.addEventListener('unhandledrejection', (event) => {
    const reason = event.reason;
    const detail = reason instanceof Error
        ? (reason.stack || reason.message)
        : String(reason || '未知错误');
    if (recoverUpdatedAssets(detail)) return;
    renderBootstrapError(detail);
});

const root = ReactDOM.createRoot(rootElement);
try {
    root.render(
        <React.StrictMode>
            <App />
        </React.StrictMode>
    );
} catch (error) {
    const detail = error instanceof Error
        ? (error.stack || error.message)
        : String(error || '未知错误');
    renderBootstrapError(detail);
}
