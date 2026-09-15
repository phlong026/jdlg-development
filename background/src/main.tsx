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

const renderBootstrapError = (detail: string) => {
    rootElement.innerHTML = `
        <div style="min-height:100vh;padding:24px;background:#fff7ed;display:flex;align-items:center;justify-content:center;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;">
            <div style="width:100%;max-width:960px;background:#ffffff;border:1px solid #fdba74;border-radius:16px;box-shadow:0 16px 40px rgba(0,0,0,0.08);padding:24px;">
                <h1 style="margin:0 0 12px;font-size:24px;line-height:1.4;color:#9a3412;">后台页面启动失败</h1>
                <div style="margin-bottom:12px;font-size:14px;line-height:1.7;color:#7c2d12;">已拦截到浏览器运行时错误，下面是详细信息：</div>
                <pre style="margin:0;white-space:pre-wrap;word-break:break-word;font-size:13px;line-height:1.6;color:#431407;background:#fffaf5;border-radius:12px;padding:16px;overflow:auto;">${escapeHtml(detail)}</pre>
            </div>
        </div>
    `;
};

window.addEventListener('error', (event) => {
    const detail = event.error instanceof Error
        ? (event.error.stack || event.error.message)
        : (event.message || '未知错误');
    renderBootstrapError(detail);
});

window.addEventListener('unhandledrejection', (event) => {
    const reason = event.reason;
    const detail = reason instanceof Error
        ? (reason.stack || reason.message)
        : String(reason || '未知错误');
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