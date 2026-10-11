import React, { useState, useEffect } from 'react';
import { FiRefreshCw, FiSearch } from 'react-icons/fi';
import { API_BASE_URL, API_ENDPOINTS } from '../config/api';
import { reportPageLoadError } from '../utils/pageLoadError';
import './logs.scss';

interface AuditItem {
    id: number;
    user_id: number;
    username: string;
    method: string;
    path: string;
    target_type: string;
    target_id: string;
    status: number;
    duration_ms: number;
    ip: string;
    created_at: string;
}

const METHOD_LABELS: Record<string, string> = {
    POST: '创建',
    PUT: '更新',
    PATCH: '更新',
    DELETE: '删除',
};

const TARGET_LABELS: Record<string, string> = {
    users: '用户账号',
    'membership-plans': '会员计划',
    templates: '模板',
    inspirations: '灵感素材',
    'utility-tools': '实用工具',
    'ai-tools': 'AI 工具',
    'ai/api': 'AI 配置',
    orders: '订单',
    audit: '操作审计',
    oss: '文件库',
};

function targetLabel(type: string): string {
    return TARGET_LABELS[type] || type || '—';
}

/**
 * 操作审计页（路由沿用 /logs，页面已从"日志管理"改造）：
 * 记录所有管理端写操作——谁在什么时候对什么资源做了什么，结果如何。
 * 数据来自后端审计中间件自动落库，无需任何手动配置。
 */
const AuditLogs: React.FC = () => {
    const [items, setItems] = useState<AuditItem[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [page, setPage] = useState(1);
    const pageSize = 20;
    const [usernameFilter, setUsernameFilter] = useState('');
    const [targetFilter, setTargetFilter] = useState('');
    const [methodFilter, setMethodFilter] = useState('');
    const [searchInput, setSearchInput] = useState('');

    const fetchList = async () => {
        setLoading(true);
        try {
            const params = new URLSearchParams({
                page: page.toString(),
                page_size: pageSize.toString(),
            });
            if (usernameFilter) params.set('username', usernameFilter);
            if (targetFilter) params.set('target_type', targetFilter);
            if (methodFilter) params.set('method', methodFilter);

            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.AUDIT.LIST}?${params}`, {
                credentials: 'include',
            });
            if (!response.ok) {
                throw new Error('获取操作审计失败');
            }
            const result = await response.json();
            if (result.code === 0) {
                setItems(result.data.list || []);
                setTotal(result.data.total || 0);
            } else {
                reportPageLoadError(null, result.msg || '获取操作审计失败');
            }
        } catch (error: any) {
            reportPageLoadError(error, '获取操作审计失败: ' + error.message);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchList();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [page, usernameFilter, targetFilter, methodFilter]);

    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    const statusClass = (status: number) => {
        if (status >= 500) return 'level-error';
        if (status >= 400) return 'level-warning';
        return 'level-info';
    };

    return (
        <div className="logs-container">
            <div className="management-search-panel">
                <div className="management-search-searchbox">
                    <FiSearch className="management-search-searchicon" />
                    <input
                        type="text"
                        className="management-search-input"
                        placeholder="按操作人筛选（如 admin）"
                        value={searchInput}
                        onChange={(e) => setSearchInput(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                setPage(1);
                                setUsernameFilter(searchInput.trim());
                            }
                        }}
                    />
                </div>
                <select
                    className="management-search-select"
                    value={targetFilter}
                    onChange={(e) => {
                        setPage(1);
                        setTargetFilter(e.target.value);
                    }}
                >
                    <option value="">全部资源</option>
                    {Object.entries(TARGET_LABELS).map(([value, label]) => (
                        <option key={value} value={value}>{label}</option>
                    ))}
                </select>
                <select
                    className="management-search-select"
                    value={methodFilter}
                    onChange={(e) => {
                        setPage(1);
                        setMethodFilter(e.target.value);
                    }}
                >
                    <option value="">全部操作</option>
                    {Object.entries(METHOD_LABELS).map(([value, label]) => (
                        <option key={value} value={value}>{label}（{value}）</option>
                    ))}
                </select>
                <button
                    className="btn-secondary"
                    onClick={() => {
                        setPage(1);
                        setUsernameFilter(searchInput.trim());
                    }}
                    disabled={loading}
                >
                    <FiSearch />
                    查询
                </button>
                <button
                    className="btn-secondary"
                    onClick={() => {
                        setSearchInput('');
                        setUsernameFilter('');
                        setTargetFilter('');
                        setMethodFilter('');
                        setPage(1);
                    }}
                >
                    重置筛选
                </button>
                <button className="btn-secondary" onClick={fetchList} disabled={loading}>
                    <FiRefreshCw />
                    {loading ? '刷新中...' : '刷新'}
                </button>
            </div>

            <div className="logs-table-container section-card">
                <table className="logs-table">
                    <thead>
                        <tr>
                            <th>时间</th>
                            <th>操作人</th>
                            <th>操作</th>
                            <th>资源</th>
                            <th>接口</th>
                            <th>结果</th>
                            <th>耗时</th>
                            <th>来源 IP</th>
                        </tr>
                    </thead>
                    <tbody>
                        {items.length === 0 ? (
                            <tr>
                                <td colSpan={8} className="empty-state">
                                    {loading ? '加载中...' : '暂无操作记录——管理端的创建、更新、删除操作会自动出现在这里'}
                                </td>
                            </tr>
                        ) : (
                            items.map((item) => (
                                <tr key={item.id}>
                                    <td>{new Date(item.created_at).toLocaleString('zh-CN')}</td>
                                    <td>{item.username || `用户#${item.user_id}`}</td>
                                    <td>{METHOD_LABELS[item.method] || item.method}</td>
                                    <td>
                                        {targetLabel(item.target_type)}
                                        {item.target_id ? ` #${item.target_id}` : ''}
                                    </td>
                                    <td className="audit-path" title={item.path}>{item.path}</td>
                                    <td>
                                        <span className={statusClass(item.status)}>{item.status}</span>
                                    </td>
                                    <td>{item.duration_ms}ms</td>
                                    <td>{item.ip || '—'}</td>
                                </tr>
                            ))
                        )}
                    </tbody>
                </table>
                <div className="pagination">
                    <button className="btn-secondary" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}>
                        上一页
                    </button>
                    <span className="page-info">第 {page} / {totalPages} 页 · 共 {total} 条</span>
                    <button className="btn-secondary" disabled={page >= totalPages || loading} onClick={() => setPage(page + 1)}>
                        下一页
                    </button>
                </div>
            </div>
        </div>
    );
};

export default AuditLogs;
