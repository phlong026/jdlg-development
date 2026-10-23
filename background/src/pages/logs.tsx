import React, { useState, useEffect, useRef } from 'react';
import { FiRefreshCw, FiSearch } from 'react-icons/fi';
import { API_BASE_URL, API_ENDPOINTS } from '../config/api';
import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import ManagementSearchPanel from '../component/managementSearchPanel';
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

// 筛选项仍按请求方法过滤（服务端参数），显示成运营能懂的操作类型
const METHOD_LABELS: Record<string, string> = {
    POST: '提交',
    PUT: '修改',
    PATCH: '修改（部分）',
    DELETE: '删除',
};

const TARGET_LABELS: Record<string, string> = {
    users: '用户账号',
    'membership-plans': '会员计划',
    templates: '模板',
    inspirations: '灵感素材',
    'utility-tools': '实用工具',
    'ai-tools': 'AI 工具',
    ai: 'AI 服务',
    orders: '订单',
    audit: '操作审计',
    oss: '文件库',
};

function targetLabel(type: string): string {
    return TARGET_LABELS[type] || '后台数据';
}

// 接口路径末段是动作名时用它描述操作（{x} 为对象，如「用户账号 #1」）；否则按请求方法兜底。
// POST 也用于审核、同步等动作，未识别时只说「提交了…的操作」，不臆测为新增。
const PATH_ACTIONS: Record<string, string> = {
    publish: '发布了{x}',
    unpublish: '下架了{x}',
    ban: '封禁了{x}',
    unban: '解封了{x}',
    approve: '通过了{x}',
    reject: '驳回了{x}',
    review: '审核了{x}',
    retry: '重试了{x}',
    refund: '退款了{x}',
    stones: '调整了{x}的灵石',
    adjust: '调整了{x}',
    featured: '调整了{x}的精选状态',
    category: '调整了{x}的分类',
    status: '修改了{x}的状态',
    assign: '分派了{x}',
    sync: '同步了{x}',
};
const METHOD_ACTIONS: Record<string, string> = {
    POST: '提交了{x}的操作',
    PUT: '修改了{x}',
    PATCH: '修改了{x}',
    DELETE: '删除了{x}',
};

function auditSentence(item: AuditItem): string {
    // 编号后留空格，避免「#1的灵石」粘连；末尾多出的空格在返回前去掉
    const target = item.target_id ? `${targetLabel(item.target_type)} #${item.target_id} ` : targetLabel(item.target_type);
    const lastSegment = item.path.split('?')[0].split('/').filter(Boolean).pop() || '';
    const template = PATH_ACTIONS[lastSegment] || METHOD_ACTIONS[item.method] || '操作了{x}';
    return `${item.username || `后台账号 #${item.user_id}`} ${template.replace('{x}', target)}`.trim();
}

/**
 * 操作审计页（路由沿用 /logs，页面已从"日志管理"改造）：
 * 记录所有管理端写操作——谁在什么时候对什么资源做了什么，结果如何。
 * 数据来自后端审计中间件自动落库，无需任何手动配置。
 */
const AuditLogs: React.FC = () => {
    const [items, setItems] = useState<AuditItem[]>([]);
    const [total, setTotal] = useState<number | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [query, setQuery] = useState({ page: 1, username: '', target_type: '', method: '' });
    const [searchInput, setSearchInput] = useState('');
    const pageSize = 20;
    const queryRef = useRef(query);
    const inputRef = useRef('');
    const version = useRef(0);
    const mounted = useRef(true);
    const ready = useRef(false);
    const renderedVersion = version.current;
    const invalidate = () => {
        version.current += 1;
        ready.current = false;
        setItems([]);
        setTotal(null);
        setError('');
        setLoading(true);
    };
    const fetchList = async () => {
        if (!mounted.current) return;
        invalidate();
        const requestId = version.current;
        const params = queryRef.current;
        const isCurrent = () => mounted.current && requestId === version.current && params === queryRef.current;
        try {
            const search = new URLSearchParams({ page: String(params.page), page_size: String(pageSize) });
            if (params.username) search.set('username', params.username);
            if (params.target_type) search.set('target_type', params.target_type);
            if (params.method) search.set('method', params.method);
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.AUDIT.LIST}?${search}`, { credentials: 'include' });
            if (!response.ok) throw new Error('获取操作审计失败');
            const result = await response.json();
            if (!isCurrent()) return;
            if (result.code !== 0) throw new Error(result.msg || '获取操作审计失败');
            if (!result.data || (result.data.list !== null && !Array.isArray(result.data.list))) throw new Error('审计列表响应无效');
            const count = Number.isFinite(result.data.total) && result.data.total >= 0 ? result.data.total : null;
            const lastPage = count === null ? params.page : Math.max(1, Math.ceil(count / pageSize));
            if (params.page > lastPage) {
                changeQuery({ page: lastPage });
                return;
            }
            setItems(result.data.list || []);
            setTotal(count);
            ready.current = true;
            reportPageLoadRecovered('获取操作审计失败');
        } catch (error) {
            if (!isCurrent()) return;
            setError(error instanceof Error ? error.message : '请重试');
            reportPageLoadError(error, '获取操作审计失败');
        } finally {
            if (isCurrent()) setLoading(false);
        }
    };
    const changeQuery = (patch: Partial<typeof query>) => {
        if (!mounted.current) return;
        const next = { ...queryRef.current, ...patch };
        if (Object.keys(next).every(key => next[key as keyof typeof next] === queryRef.current[key as keyof typeof next])) { void fetchList(); return; }
        queryRef.current = next;
        invalidate();
        setQuery(next);
    };
    const changePage = (page: number) => {
        if (!mounted.current || !ready.current || renderedVersion !== version.current || total === null) return;
        const bounded = Math.max(1, Math.min(page, Math.max(1, Math.ceil(total / pageSize))));
        changeQuery({ page: bounded });
    };
    const handleSearch = () => changeQuery({ page: 1, username: inputRef.current.trim() });
    const handleReset = () => {
        if (!mounted.current) return;
        inputRef.current = '';
        setSearchInput('');
        changeQuery({ page: 1, username: '', target_type: '', method: '' });
    };
    useEffect(() => {
        mounted.current = true;
        return () => { mounted.current = false; ready.current = false; version.current += 1; };
    }, []);
    useEffect(() => { void fetchList(); }, [query]);
    const totalPages = total === null ? null : Math.max(1, Math.ceil(total / pageSize));

    const statusClass = (status: number) => {
        if (status >= 500) return 'level-error';
        if (status >= 400) return 'level-warning';
        return 'level-info';
    };

    return (
        <div className="logs-container">
            <ManagementSearchPanel
                title="管理操作审计"
                actions={<>
                    <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                    <button className="btn-secondary" onClick={() => void fetchList()} disabled={loading}><FiRefreshCw />{loading ? '刷新中...' : '刷新'}</button>
                </>}
                controls={<>
                    <div className="management-search-searchbox">
                        <FiSearch className="management-search-searchicon" />
                        <input type="text" className="management-search-input" aria-label="审计操作人" placeholder="按操作人筛选（如 admin）" value={searchInput}
                            onChange={(event) => { inputRef.current = event.target.value; setSearchInput(event.target.value); }}
                            onKeyDown={(event) => { if (event.key === 'Enter') handleSearch(); }} />
                    </div>
                    <select className="management-search-select" aria-label="审计资源" value={query.target_type} onChange={(event) => changeQuery({ page: 1, target_type: event.target.value })}>
                        <option value="">全部资源</option>
                        {Object.entries(TARGET_LABELS).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                    </select>
                    <select className="management-search-select" aria-label="审计请求方法" value={query.method} onChange={(event) => changeQuery({ page: 1, method: event.target.value })}>
                        <option value="">全部操作类型</option>
                        {Object.entries(METHOD_LABELS).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                    </select>
                    <button className="btn-secondary" onClick={handleSearch}><FiSearch />查询</button>
                </>}
                summary={<span>当前筛选共 {total ?? '—'} 条；后台的新增、修改、删除等操作会自动记录。</span>}
            />

            <div className="section-card logs-table-panel">
                <div className="logs-table-container">
                    <table className="logs-table">
                        <thead>
                            <tr>
                                <th>时间</th>
                                <th>操作</th>
                                <th>结果</th>
                                <th>详情</th>
                            </tr>
                        </thead>
                        <tbody>
                            {items.length === 0 ? (
                                <tr>
                                    <td colSpan={4} className="empty-state">
                                        {loading ? '加载中...' : error ? <span role="alert">审计加载失败：{error}<button className="btn-secondary" onClick={() => void fetchList()}>重新加载审计</button></span> : '暂无操作记录'}
                                    </td>
                                </tr>
                            ) : (
                                items.map((item) => (
                                    <tr key={item.id}>
                                        <td>{new Date(item.created_at).toLocaleString('zh-CN')}</td>
                                        <td className="audit-sentence">{auditSentence(item)}</td>
                                        <td>
                                            <span className={`level-badge ${statusClass(item.status)}`}>{item.status < 400 ? '成功' : '失败'}</span>
                                        </td>
                                        <td className="audit-detail-cell">
                                            <details>
                                                <summary>查看</summary>
                                                <div className="audit-path" title={item.path}>{item.method} {item.path}</div>
                                                <div>状态码 {item.status} · 耗时 {item.duration_ms}ms · 来源 IP {item.ip || '—'}</div>
                                            </details>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
                <div className="pagination">
                    <button className="btn-secondary" disabled={query.page <= 1 || loading || Boolean(error) || totalPages === null} onClick={() => changePage(query.page - 1)}>
                        上一页
                    </button>
                    <span className="page-info">第 {query.page} / {totalPages ?? '—'} 页 · 共 {total ?? '—'} 条</span>
                    <button className="btn-secondary" disabled={totalPages === null || query.page >= totalPages || loading || Boolean(error)} onClick={() => changePage(query.page + 1)}>
                        下一页
                    </button>
                </div>
            </div>
        </div>
    );
};

export default AuditLogs;
