import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiAlertCircle, FiClipboard, FiRefreshCw, FiSearch, FiTool } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    assignSupportTicket,
    createSupportTicket,
    getSupportTicketDetail,
    getSupportTicketList,
    getSupportTicketOverview,
    syncSupportTicketSystemExceptions,
    updateSupportTicketResolutionNote,
    updateSupportTicketStatus,
    type SupportTicketItem,
    type SupportTicketOverview,
} from '../api/supportTickets';
import './supportTickets.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiClipboardIcon = FiClipboard as unknown as React.ComponentType<any>;
const FiAlertCircleIcon = FiAlertCircle as unknown as React.ComponentType<any>;
const FiToolIcon = FiTool as unknown as React.ComponentType<any>;

const typeLabelMap: Record<string, string> = {
    complaint: '用户投诉',
    order: '异常订单',
    task: '失败任务',
    certification: '认证工单',
};

const sourceTypeLabelMap: Record<string, string> = {
    manual: '人工录入',
    order: '订单同步',
    task: '任务同步',
};

const priorityLabelMap: Record<string, string> = {
    high: '高优先级',
    medium: '中优先级',
    low: '低优先级',
};

const statusLabelMap: Record<string, string> = {
    open: '待处理',
    in_progress: '处理中',
    closed: '已关闭',
};

const formatDateTime = (value?: string | null) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const getDisplayName = (item?: SupportTicketItem | null) => {
    if (!item) return '';
    return item.nickname?.trim() || item.username || (item.user_id ? `用户${item.user_id}` : '匿名用户');
};

const getSourceTarget = (item?: SupportTicketItem | null) => {
    if (!item?.source_type || !item.source_id) {
        return null;
    }
    if (item.source_type === 'order') {
        return {
            path: `/recharge?orderId=${item.source_id}`,
            label: '查看原订单',
        };
    }
    if (item.source_type === 'task') {
        const matched = item.source_id.match(/^(image|video)-(\d+)$/);
        if (matched) {
            return {
                path: `/ai-tasks?tab=${matched[1]}&taskId=${matched[2]}`,
                label: '查看原任务',
            };
        }
    }
    return null;
};

const failureMessage = (error: unknown, fallback: string) => {
    const message = (error as { message?: unknown } | null)?.message;
    return typeof message === 'string' && message ? message : fallback;
};

const parseSourcePayload = (value?: string) => {
    if (!value) {
        return null;
    }
    try {
        return JSON.parse(value) as Record<string, any>;
    } catch {
        return null;
    }
};

const sourceStatusLabel = (value: unknown) => {
    const labels: Record<string, string> = { pending: '待处理', queued: '待处理', running: '处理中', processing: '处理中',
        success: '成功', completed: '成功', failed: '失败', cancelled: '已取消', delivering: '发货中', refunding: '退款中', refunded: '已退款' };
    return value ? labels[String(value)] || '其他状态（见排障信息）' : '-';
};
const sourceTypeLabel = (value: unknown, category = false) => {
    const labels: Record<string, string> = category
        ? { recharge: '充值', ai: 'AI生成', template: '模板', certification: '认证', withdraw: '提现', service: '设计服务', report: '建房报告', enterprise_recharge: '企业充值' }
        : { recharge: '充值', consume: 'AI消费', culture: '内容消费', withdraw: '提现', certification: '认证费', report: '建房报告', image: '图片任务', video: '视频任务' };
    return value ? labels[String(value)] || '其他类型（见排障信息）' : '-';
};

const getSourceSummaryItems = (item?: SupportTicketItem | null) => {
    if (!item) {
        return [] as Array<{ label: string; value: string }>;
    }
    const payload = parseSourcePayload(item.source_payload);
    if (item.source_type === 'order') {
        return [
            { label: '订单号', value: String(payload?.order_no || item.source_id || '-') },
            { label: '订单状态', value: sourceStatusLabel(payload?.status) },
            { label: '订单类型', value: sourceTypeLabel(payload?.type) },
            { label: '业务分类', value: sourceTypeLabel(payload?.order_category, true) },
            { label: '订单数值（单位待核实）', value: payload?.amount !== undefined && payload?.amount !== null ? String(payload.amount) : '-' },
        ].filter((entry) => entry.value && entry.value !== '-');
    }
    if (item.source_type === 'task') {
        return [
            { label: '任务号', value: String(payload?.task_no || item.source_id || '-') },
            { label: '任务类型', value: sourceTypeLabel(payload?.task_type) },
            { label: '任务状态', value: sourceStatusLabel(payload?.status || payload?.raw_status) },
            { label: '业务场景', value: String(payload?.scene_label || '-') },
            { label: '错误信息', value: String(payload?.error_message || '-') },
        ].filter((entry) => entry.value && entry.value !== '-');
    }
    return [] as Array<{ label: string; value: string }>;
};

const SupportTickets: React.FC = () => {
    const navigate = useNavigate();
    const [overview, setOverview] = useState<SupportTicketOverview | null>(null);
    const [list, setList] = useState<SupportTicketItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [notice, setNotice] = useState('');
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [statusFilter, setStatusFilter] = useState('all');
    const [typeFilter, setTypeFilter] = useState('all');
    const [sourceTypeFilter, setSourceTypeFilter] = useState('all');
    const [page, setPage] = useState(1);
    const [total, setTotal] = useState(0);
    const [refreshKey, setRefreshKey] = useState(0);
    const pageSize = 20;
    const [detail, setDetail] = useState<SupportTicketItem | null>(null);
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState('');
    const [showCreateModal, setShowCreateModal] = useState(false);
    const [createSubmitting, setCreateSubmitting] = useState(false);
    const emptyCreateForm = { user_id: '', type: 'complaint', title: '', content: '', priority: 'medium' };
    const [createForm, setCreateForm] = useState(emptyCreateForm);
    const [resolutionNote, setResolutionNote] = useState('');
    const [resolutionSubmitting, setResolutionSubmitting] = useState(false);
    const [pendingIds, setPendingIds] = useState<number[]>([]);
    const [syncing, setSyncing] = useState(false);
    const mounted = useRef(true);
    const listRequest = useRef(0);
    const renderedListRequest = listRequest.current;
    const readyIds = useRef<number[]>([]);
    const scope = JSON.stringify([page, keyword, statusFilter, typeFilter, sourceTypeFilter, refreshKey]);
    const scopeRef = useRef(scope);
    scopeRef.current = scope;
    const ticketLocks = useRef(new Set<number>());
    const detailSession = useRef(0);
    const detailRequest = useRef(0);
    const detailTarget = useRef<number | null>(null);
    const detailReady = useRef(false);
    const renderedDetailSession = detailSession.current;
    const noteDraft = useRef({ value: '', baseline: '', revision: 0 });
    const createLock = useRef(false);
    const createSession = useRef(0);
    const renderedCreateSession = createSession.current;
    const createVisible = useRef(false);
    const syncLock = useRef(false);
    const loadLatest = useRef<() => Promise<void>>(async () => {});

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            listRequest.current += 1;
            detailSession.current += 1;
            detailRequest.current += 1;
            readyIds.current = [];
            createSession.current += 1; createVisible.current = false;
        };
    }, []);

    const loadData = async () => {
        if (!mounted.current) return;
        const request = ++listRequest.current;
        const requestScope = scope;
        readyIds.current = [];
        setLoading(true);
        setLoadError('');
        const current = () => mounted.current && request === listRequest.current && requestScope === scopeRef.current;
        try {
            const [overviewData, listData] = await Promise.all([
                getSupportTicketOverview(),
                getSupportTicketList({ page, page_size: pageSize, keyword, status: statusFilter, type: typeFilter, source_type: sourceTypeFilter }),
            ]);
            if (!current()) return;
            if (!Number.isFinite(listData.total) || listData.total < 0 || (listData.list !== null && !Array.isArray(listData.list))) {
                throw new Error('工单列表返回格式异常');
            }
            const lastPage = Math.max(1, Math.ceil(listData.total / pageSize));
            if (page > lastPage) { listRequest.current += 1; setPage(lastPage); return; }
            setOverview(overviewData);
            const items = listData.list || [];
            setList(items);
            setTotal(listData.total);
            readyIds.current = items.map(item => item.id);
            reportPageLoadRecovered('加载工单中心失败');
        } catch (error) {
            if (!current()) return;
            setList([]);
            setOverview(null);
            setLoadError('工单未能加载，请重试。');
            reportPageLoadError(error, '加载工单中心失败');
        } finally {
            if (current()) setLoading(false);
        }
    };
    loadLatest.current = loadData;
    useEffect(() => { void loadData(); }, [page, keyword, statusFilter, typeFilter, sourceTypeFilter, refreshKey]);

    const changeScope = (change: () => void, resetPage = true) => {
        if (!mounted.current) return;
        listRequest.current += 1;
        readyIds.current = [];
        setList([]);
        setLoading(true);
        setLoadError('');
        if (resetPage) setPage(1);
        setRefreshKey(value => value + 1);
        change();
    };
    const handleReset = () => changeScope(() => {
        setSearchInput(''); setKeyword(''); setStatusFilter('all'); setTypeFilter('all'); setSourceTypeFilter('all');
    });
    const editNote = (value: string) => {
        noteDraft.current = { ...noteDraft.current, value, revision: noteDraft.current.revision + 1 };
        setResolutionNote(value);
    };
    const dirtyNote = () => noteDraft.current.value !== noteDraft.current.baseline;
    const canLeaveDetail = () => detailTarget.current === null || (!ticketLocks.current.has(detailTarget.current)
        && (!dirtyNote() || window.confirm('处理备注尚未保存，确定放弃这些修改吗？')));
    const closeDetail = () => {
        if (renderedDetailSession !== detailSession.current || !canLeaveDetail()) return;
        detailSession.current += 1; detailRequest.current += 1; detailTarget.current = null; detailReady.current = false;
        setShowDetailModal(false); setDetailError('');
    };
    const readDetail = async (id: number, session: number) => {
        if (!mounted.current || session !== detailSession.current || id !== detailTarget.current) return;
        const request = ++detailRequest.current;
        const current = () => mounted.current && detailSession.current === session && detailTarget.current === id && detailRequest.current === request;
        detailReady.current = false;
        setDetailLoading(true); setDetailError('');
        try {
            const fresh = await getSupportTicketDetail(String(id));
            if (!current()) return;
            if (fresh.id !== id) throw new Error('返回的工单与请求对象不一致，请重新读取');
            setDetail(fresh);
            detailReady.current = true;
            // Assignment/status/read-back must never erase an unsaved note.
            if (!dirtyNote()) {
                const value = fresh.resolution_note || '';
                noteDraft.current = { value, baseline: value, revision: noteDraft.current.revision + 1 };
                setResolutionNote(value);
            }
        } catch (error) {
            if (current()) setDetailError(`${failureMessage(error, '最新工单详情暂时无法读取')}。已保存的操作不会撤回，请重新读取。`);
        } finally {
            if (current()) setDetailLoading(false);
        }
    };
    const handleViewDetail = async (item: SupportTicketItem) => {
        if (!mounted.current || !readyIds.current.includes(item.id) || !canLeaveDetail()) return;
        const session = ++detailSession.current;
        detailTarget.current = item.id;
        const value = item.resolution_note || '';
        noteDraft.current = { value, baseline: value, revision: noteDraft.current.revision + 1 };
        setResolutionNote(value); setDetail(item); setShowDetailModal(true);
        await readDetail(item.id, session);
    };
    const targetAllowed = (id: number) => detailTarget.current === id
        ? detailReady.current : readyIds.current.includes(id);
    const mutateTicket = async (item: SupportTicketItem, operation: () => Promise<void>, message: string, savedNote?: { value: string; revision: number }) => {
        const fromDetail = showDetailModal && detail?.id === item.id;
        const originCurrent = fromDetail
            ? renderedDetailSession === detailSession.current && detailTarget.current === item.id
            : renderedListRequest === listRequest.current && scope === scopeRef.current;
        if (!mounted.current || !originCurrent || !targetAllowed(item.id) || ticketLocks.current.has(item.id)) return;
        ticketLocks.current.add(item.id);
        setPendingIds([...ticketLocks.current]);
        const session = detailTarget.current === item.id ? detailSession.current : null;
        if (savedNote) setResolutionSubmitting(true);
        try {
            await operation();
            if (!mounted.current) return;
            setNotice(`工单 ${item.id}：${message}`);
            if (session !== null && session === detailSession.current && detailTarget.current === item.id) {
                if (savedNote) {
                    // Only acknowledge the submitted snapshot; later typing stays dirty.
                    const unchanged = noteDraft.current.revision === savedNote.revision;
                    noteDraft.current = { ...noteDraft.current, baseline: savedNote.value,
                        value: unchanged ? savedNote.value : noteDraft.current.value };
                    if (unchanged) setResolutionNote(savedNote.value);
                }
                await readDetail(item.id, session);
            }
            await loadLatest.current();
        } catch (error) {
            if (mounted.current) setNotice(`工单 ${item.id}：${failureMessage(error, '操作未完成')}。原输入已保留，请核实后重试。`);
        } finally {
            ticketLocks.current.delete(item.id);
            if (mounted.current) { setPendingIds([...ticketLocks.current]); if (savedNote) setResolutionSubmitting(false); }
        }
    };
    const handleAssign = (item: SupportTicketItem) => mutateTicket(item, () => assignSupportTicket(String(item.id)), '已分配给当前处理人。');
    const handleUpdateStatus = (item: SupportTicketItem, status: string) => mutateTicket(item, () => updateSupportTicketStatus(String(item.id), status), '状态已更新。');
    const handleSaveResolutionNote = () => {
        if (!detail || detailTarget.current !== detail.id || renderedDetailSession !== detailSession.current) return;
        const saved = { value: noteDraft.current.value.trim(), revision: noteDraft.current.revision };
        return mutateTicket(detail, () => updateSupportTicketResolutionNote(String(detail.id), saved.value), '处理备注已保存。', saved);
    };
    const handleSync = async () => {
        if (!mounted.current || syncLock.current) return;
        syncLock.current = true; setSyncing(true);
        try {
            const createdCount = await syncSupportTicketSystemExceptions();
            if (!mounted.current) return;
            setNotice(`同步完成，本次新增 ${createdCount} 条系统工单。`);
            await loadLatest.current();
        } catch (error) {
            if (mounted.current) setNotice(`${failureMessage(error, '同步系统异常工单未完成')}。请核实后重试。`);
        } finally { syncLock.current = false; if (mounted.current) setSyncing(false); }
    };
    const openCreate = () => {
        if (createLock.current) return;
        createSession.current += 1; createVisible.current = true; setShowCreateModal(true);
    };
    const closeCreate = () => {
        if (createLock.current || renderedCreateSession !== createSession.current) return;
        createSession.current += 1; createVisible.current = false; setShowCreateModal(false);
    };
    const handleCreate = async () => {
        if (!mounted.current || createLock.current || !createVisible.current || renderedCreateSession !== createSession.current) return;
        if (!createForm.title.trim()) { setNotice('请先填写工单标题。'); return; }
        if (createForm.user_id.trim() && (!Number.isSafeInteger(Number(createForm.user_id)) || Number(createForm.user_id) <= 0)) {
            setNotice('用户编号应为有效的正整数，未关联用户时可留空。'); return;
        }
        const session = createSession.current;
        const payload = { user_id: createForm.user_id.trim() ? Number(createForm.user_id) : undefined,
            type: createForm.type, title: createForm.title.trim(), content: createForm.content.trim(), priority: createForm.priority };
        createLock.current = true; setCreateSubmitting(true);
        try {
            const id = await createSupportTicket(payload);
            if (!mounted.current) return;
            setNotice(`工单 ${id} 已创建。`);
            if (session === createSession.current) { setShowCreateModal(false); setCreateForm(emptyCreateForm); createSession.current += 1; createVisible.current = false; }
            await loadLatest.current();
        } catch (error) {
            if (mounted.current && session === createSession.current) setNotice(`${failureMessage(error, '暂未确认创建结果')}。输入已保留，请先刷新列表核实是否已创建，再决定是否重试。`);
        } finally { createLock.current = false; if (mounted.current) setCreateSubmitting(false); }
    };

    const detailSourceTarget = getSourceTarget(detail);
    const detailSourceSummaryItems = getSourceSummaryItems(detail);

    return (
        <>
            <div className="support-ticket-page">
                <ManagementSearchPanel
                    title="工单搜索"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading}>
                                <FiRefreshCwIcon />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                            <button className="btn-secondary" onClick={openCreate}>
                                <FiClipboardIcon />
                                新建工单
                            </button>
                            <button className="btn-primary" onClick={handleSync} disabled={syncing}>
                                <FiToolIcon />
                                {syncing ? '同步中...' : '同步异常'}
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <div className="management-search-searchbox">
                                <FiSearchIcon className="management-search-searchicon" />
                                <input
                                    type="text"
                                    className="management-search-input"
                                    placeholder="搜索用户名、昵称或工单标题"
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            changeScope(() => setKeyword(searchInput.trim()));
                                        }
                                    }}
                                />
                            </div>
                            <select className="management-search-select" value={statusFilter} onChange={(e) => changeScope(() => setStatusFilter(e.target.value))}>
                                <option value="all">全部状态 / 队列</option>
                                <option value="today">今日新增队列</option>
                                <option value="overdue">超48小时待处理队列</option>
                                <option value="open">待处理</option>
                                <option value="in_progress">处理中</option>
                                <option value="closed">已关闭</option>
                            </select>
                            <select className="management-search-select" value={typeFilter} onChange={(e) => changeScope(() => setTypeFilter(e.target.value))}>
                                <option value="all">全部类型</option>
                                <option value="complaint">用户投诉</option>
                                <option value="order">异常订单</option>
                                <option value="task">失败任务</option>
                            </select>
                            <select className="management-search-select" value={sourceTypeFilter} onChange={(e) => changeScope(() => setSourceTypeFilter(e.target.value))}>
                                <option value="all">全部来源</option>
                                <option value="manual">人工录入</option>
                                <option value="order">订单同步</option>
                                <option value="task">任务同步</option>
                            </select>
                            <button className="btn-primary" onClick={() => changeScope(() => setKeyword(searchInput.trim()))}>搜索工单</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                匹配工单 <strong>{loading || loadError ? '—' : total}</strong> 条，本页 <strong>{loading || loadError ? '—' : list.length}</strong> 条
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                                {statusFilter !== 'all' ? <span className="management-search-tag">状态：{({ today: '今日新增', overdue: '超48小时待处理', ...statusLabelMap } as Record<string, string>)[statusFilter] || statusFilter}</span> : null}
                                {typeFilter !== 'all' ? <span className="management-search-tag">类型：{typeLabelMap[typeFilter] || typeFilter}</span> : null}
                                {sourceTypeFilter !== 'all' ? <span className="management-search-tag">来源：{sourceTypeLabelMap[sourceTypeFilter] || sourceTypeFilter}</span> : null}
                            </div>
                        </>
                    )}
                />

                {notice ? <div className="support-feedback" role="status">{notice}</div> : null}
                <p className="support-scope-note">以下概览为全部工单，不随列表筛选变化；今日与超时队列以服务端时间为准。</p>
                {/* 队列筛选条（B2）：对齐销售线索页的模式，客服上班先看队列而不是先想筛选条件 */}
                <div className="ticket-queues" role="group" aria-label="工单队列">
                    {([
                        { key: 'open', label: '待处理', count: overview?.open_count, todo: true },
                        { key: 'in_progress', label: '处理中', count: overview?.in_progress_count, todo: false },
                        { key: 'today', label: '今日新增', count: overview?.today_count, todo: false },
                        { key: 'overdue', label: '超48小时', count: overview?.overdue_count, todo: true },
                        { key: 'all', label: '全部工单', count: overview?.total_count, todo: false },
                    ] as const).map((queue) => (
                        <button
                            key={queue.key}
                            className={`ticket-queue-tab ${statusFilter === queue.key ? 'active' : ''} ${queue.todo && (queue.count ?? 0) > 0 ? 'is-todo' : ''}`}
                            onClick={() => changeScope(() => setStatusFilter(queue.key))} aria-pressed={statusFilter === queue.key}
                        >
                            {queue.label}
                            <span className="ticket-queue-count">{queue.count ?? '—'}</span>
                        </button>
                    ))}
                </div>

                <div className="support-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiClipboardIcon /></div>
                        <div className="stat-label">工单总数</div>
                        <div className="stat-value">{overview?.total_count ?? '—'}</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAlertCircleIcon /></div>
                        <div className="stat-label">待处理 / 处理中</div>
                        <div className="stat-value">{overview?.open_count ?? '—'} / {overview?.in_progress_count ?? '—'}</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiToolIcon /></div>
                        <div className="stat-label">高优先级</div>
                        <div className="stat-value">{overview?.high_priority_count ?? '—'}</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiRefreshCwIcon /></div>
                        <div className="stat-label">已关闭</div>
                        <div className="stat-value">{overview?.closed_count ?? '—'}</div>
                    </div>
                </div>

                <div className="support-panel section-card">
                    <div className="support-table-container">
                        <table className="support-table">
                            <thead>
                                <tr>
                                    <th>工单</th>
                                    <th>用户</th>
                                    <th>类型 / 来源</th>
                                    <th>优先级</th>
                                    <th>状态</th>
                                    <th>负责人</th>
                                    <th>时间</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {loading ? (
                                    <tr><td colSpan={8} className="empty-state" role="status">正在加载工单...</td></tr>
                                ) : loadError ? (
                                    <tr><td colSpan={8} className="empty-state" role="alert">{loadError}<button className="btn-inline" onClick={() => void loadLatest.current()}>重试加载</button></td></tr>
                                ) : list.length === 0 ? (
                                    <tr>
                                        <td colSpan={8} className="empty-state">暂无工单数据</td>
                                    </tr>
                                ) : list.map((item) => (
                                    <tr key={item.id}>
                                        <td>
                                            <div className="identity-cell">
                                                <strong>{item.title}</strong>
                                                <span>{item.content || '暂无说明'}</span>
                                            </div>
                                        </td>
                                        <td>
                                            <div className="identity-cell">
                                                <strong>{getDisplayName(item)}</strong>
                                                <span>{item.user_id ? `ID ${item.user_id}` : '未关联用户'}</span>
                                            </div>
                                        </td>
                                        <td>
                                            <div className="identity-cell">
                                                <strong>{typeLabelMap[item.type] || item.type}</strong>
                                                <span>{sourceTypeLabelMap[item.source_type] || item.source_type}{item.source_id ? ` · 来源 ${item.source_id}` : ''}</span>
                                            </div>
                                        </td>
                                        <td><span className={`inline-tag ${item.priority === 'high' ? 'danger' : ''}`}>{priorityLabelMap[item.priority] || item.priority}</span></td>
                                        <td><span className={`inline-tag ${item.status === 'closed' ? '' : item.status === 'in_progress' ? 'warning' : 'danger'}`}>{statusLabelMap[item.status] || item.status}</span></td>
                                        <td>{item.assignee_name || '未分配'}</td>
                                        <td>
                                            <div className="identity-cell">
                                                <strong>创建：{formatDateTime(item.created_at)}</strong>
                                                <span>更新：{formatDateTime(item.updated_at)}</span>
                                            </div>
                                        </td>
                                        <td>
                                            <div className="action-group">
                                                <button className="btn-inline" onClick={() => void handleViewDetail(item)}>详情</button>
                                                {getSourceTarget(item) ? <button className="btn-inline" onClick={() => navigate(getSourceTarget(item)!.path)}>{getSourceTarget(item)!.label}</button> : null}
                                                {!item.assignee_name && item.status !== 'closed' ? <button className="btn-inline" onClick={() => void handleAssign(item)} disabled={pendingIds.includes(item.id)}>分配给我</button> : null}
                                                {item.status === 'open' ? <button className="btn-inline" onClick={() => void handleUpdateStatus(item, 'in_progress')} disabled={pendingIds.includes(item.id)}>开始处理</button> : null}
                                                {item.status !== 'closed' ? <button className="btn-inline danger" onClick={() => void handleUpdateStatus(item, 'closed')} disabled={pendingIds.includes(item.id)}>关闭</button> : null}
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>

                <div className="support-pagination" aria-label="工单分页">
                    <span>第 {page} / {Math.max(1, Math.ceil(total / pageSize))} 页，每页 {pageSize} 条</span>
                    <button className="btn-secondary" disabled={loading || !!loadError || page <= 1} onClick={() => changeScope(() => setPage(page - 1), false)}>上一页</button>
                    <button className="btn-secondary" disabled={loading || !!loadError || page * pageSize >= total} onClick={() => changeScope(() => setPage(page + 1), false)}>下一页</button>
                </div>

                {showCreateModal ? (
                    <div className="modal-overlay" onClick={closeCreate}>
                        <div className="modal-content support-modal" role="dialog" aria-modal="true" aria-labelledby="support-create-title" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3 id="support-create-title">新建工单</h3>
                                <button className="modal-close" onClick={closeCreate} disabled={createSubmitting} aria-label="关闭新建工单">✕</button>
                            </div>
                            <div className="modal-body support-form">
                                <label>
                                    <span>用户编号</span>
                                    <input value={createForm.user_id} disabled={createSubmitting} onChange={(e) => setCreateForm((prev) => ({ ...prev, user_id: e.target.value }))} placeholder="可不填" />
                                </label>
                                <label>
                                    <span>工单类型</span>
                                    <select value={createForm.type} disabled={createSubmitting} onChange={(e) => setCreateForm((prev) => ({ ...prev, type: e.target.value }))}>
                                        <option value="complaint">用户投诉</option>
                                        <option value="order">异常订单</option>
                                        <option value="task">失败任务</option>
                                    </select>
                                </label>
                                <label>
                                    <span>标题</span>
                                    <input value={createForm.title} disabled={createSubmitting} onChange={(e) => setCreateForm((prev) => ({ ...prev, title: e.target.value }))} placeholder="例如：用户反馈付款后未到账" />
                                </label>
                                <label>
                                    <span>优先级</span>
                                    <select value={createForm.priority} disabled={createSubmitting} onChange={(e) => setCreateForm((prev) => ({ ...prev, priority: e.target.value }))}>
                                        <option value="high">高优先级</option>
                                        <option value="medium">中优先级</option>
                                        <option value="low">低优先级</option>
                                    </select>
                                </label>
                                <label className="full">
                                    <span>内容说明</span>
                                    <textarea value={createForm.content} disabled={createSubmitting} onChange={(e) => setCreateForm((prev) => ({ ...prev, content: e.target.value }))} rows={5} placeholder="把用户反馈、订单信息、异常现象写清楚" />
                                </label>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={closeCreate} disabled={createSubmitting}>取消</button>
                                <button className="btn-primary" onClick={() => void handleCreate()} disabled={createSubmitting}>{createSubmitting ? '提交中...' : '创建工单'}</button>
                            </div>
                        </div>
                    </div>
                ) : null}

                {showDetailModal && detail ? (
                    <div className="modal-overlay" onClick={closeDetail}>
                        <div className="modal-content support-modal" role="dialog" aria-modal="true" aria-labelledby="support-detail-title" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3 id="support-detail-title">工单详情</h3>
                                <button className="modal-close" onClick={closeDetail} disabled={pendingIds.includes(detail.id)} aria-label="关闭工单详情">✕</button>
                            </div>
                            <div className="modal-body detail-stack">
                                {detailLoading ? <p role="status">正在读取最新工单...</p> : null}
                                {detailError ? <div role="alert">{detailError}<button className="btn-inline" onClick={() => void readDetail(detail.id, detailSession.current)}>重新读取详情</button></div> : null}
                                <div className="detail-block">
                                    <strong>{detail.title}</strong>
                                    <span>{detail.content || '暂无内容说明'}</span>
                                </div>
                                <div className="detail-grid">
                                    <div><label>用户</label><span>{getDisplayName(detail)}{detail.user_id ? `（ID ${detail.user_id}）` : ''}</span></div>
                                    <div><label>类型</label><span>{typeLabelMap[detail.type] || detail.type}</span></div>
                                    <div><label>来源</label><span>{sourceTypeLabelMap[detail.source_type] || detail.source_type}{detail.source_id ? ` / ${detail.source_id}` : ''}</span></div>
                                    <div><label>优先级</label><span>{priorityLabelMap[detail.priority] || detail.priority}</span></div>
                                    <div><label>状态</label><span>{statusLabelMap[detail.status] || detail.status}</span></div>
                                    <div><label>负责人</label><span>{detail.assignee_name || '未分配'}</span></div>
                                    <div><label>创建人</label><span>{detail.created_by || '未知'}</span></div>
                                    <div><label>关闭时间</label><span>{formatDateTime(detail.closed_at)}</span></div>
                                </div>
                                {detailSourceSummaryItems.length > 0 ? (
                                    <div className="detail-block source-summary-card">
                                        <label>来源摘要</label>
                                        <div className="source-summary-grid">
                                            {detailSourceSummaryItems.map((entry) => (
                                                <div key={entry.label}>
                                                    <label>{entry.label}</label>
                                                    <span>{entry.value}</span>
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                ) : null}
                                <details className="detail-block">
                                    <summary>排障信息（原始记录）</summary>
                                    <pre>{detail.source_payload || '暂无'}</pre>
                                </details>
                                <div className="detail-block">
                                    <label>处理备注</label>
                                    <textarea
                                        className="detail-note-textarea"
                                        value={resolutionNote}
                                        onChange={(e) => editNote(e.target.value)}
                                        disabled={detailLoading || !!detailError}
                                        rows={4}
                                        placeholder="记录处理过程、补偿结果、结论说明，方便后续追踪"
                                    />
                                </div>
                            </div>
                            <div className="modal-footer">
                                {detailSourceTarget ? <button className="btn-secondary" onClick={() => { if (canLeaveDetail()) navigate(detailSourceTarget.path); }}>{detailSourceTarget.label}</button> : null}
                                <button className="btn-secondary" onClick={() => void handleSaveResolutionNote()} disabled={pendingIds.includes(detail.id) || detailLoading || !!detailError}>
                                    {resolutionSubmitting ? '保存中...' : '保存备注'}
                                </button>
                                {!detail.assignee_name && detail.status !== 'closed' ? <button className="btn-secondary" onClick={() => void handleAssign(detail)} disabled={pendingIds.includes(detail.id) || detailLoading || !!detailError}>分配给我</button> : null}
                                {detail.status === 'open' ? <button className="btn-secondary" onClick={() => void handleUpdateStatus(detail, 'in_progress')} disabled={pendingIds.includes(detail.id) || detailLoading || !!detailError}>开始处理</button> : null}
                                {detail.status !== 'closed' ? <button className="btn-primary" onClick={() => void handleUpdateStatus(detail, 'closed')} disabled={pendingIds.includes(detail.id) || detailLoading || !!detailError}>关闭工单</button> : null}
                            </div>
                        </div>
                    </div>
                ) : null}
            </div>
        </>
    );
};

export default SupportTickets;
