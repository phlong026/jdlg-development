import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiAlertCircle, FiClipboard, FiRefreshCw, FiSearch, FiTool } from 'react-icons/fi';
import Layout from '../component/layout';
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

const defaultOverview: SupportTicketOverview = {
    total_count: 0,
    open_count: 0,
    in_progress_count: 0,
    closed_count: 0,
    high_priority_count: 0,
};

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

const getSourceSummaryItems = (item?: SupportTicketItem | null) => {
    if (!item) {
        return [] as Array<{ label: string; value: string }>;
    }
    const payload = parseSourcePayload(item.source_payload);
    if (item.source_type === 'order') {
        return [
            { label: '订单号', value: String(payload?.order_no || item.source_id || '-') },
            { label: '订单状态', value: String(payload?.status || '-') },
            { label: '订单类型', value: String(payload?.type || '-') },
            { label: '业务分类', value: String(payload?.order_category || '-') },
            { label: '订单数值', value: payload?.amount !== undefined && payload?.amount !== null ? String(payload.amount) : '-' },
        ].filter((entry) => entry.value && entry.value !== '-');
    }
    if (item.source_type === 'task') {
        return [
            { label: '任务号', value: String(payload?.task_no || item.source_id || '-') },
            { label: '任务类型', value: String(payload?.task_type || '-') },
            { label: '任务状态', value: String(payload?.status || payload?.raw_status || '-') },
            { label: '场景/模型', value: String(payload?.scene || payload?.model || '-') },
            { label: '错误信息', value: String(payload?.error_message || '-') },
            { label: '原始错误', value: String(payload?.raw_error_message || '-') },
        ].filter((entry) => entry.value && entry.value !== '-');
    }
    return [] as Array<{ label: string; value: string }>;
};

const SupportTickets: React.FC = () => {
    const navigate = useNavigate();
    const [overview, setOverview] = useState<SupportTicketOverview>(defaultOverview);
    const [list, setList] = useState<SupportTicketItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [statusFilter, setStatusFilter] = useState('all');
    const [typeFilter, setTypeFilter] = useState('all');
    const [sourceTypeFilter, setSourceTypeFilter] = useState('all');
    const [detail, setDetail] = useState<SupportTicketItem | null>(null);
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [showCreateModal, setShowCreateModal] = useState(false);
    const [createSubmitting, setCreateSubmitting] = useState(false);
    const [createForm, setCreateForm] = useState({ user_id: '', type: 'complaint', title: '', content: '', priority: 'medium' });
    const [resolutionNote, setResolutionNote] = useState('');
    const [resolutionSubmitting, setResolutionSubmitting] = useState(false);

    const loadData = async () => {
        setLoading(true);
        try {
            const [overviewData, listData] = await Promise.all([
                getSupportTicketOverview(),
                getSupportTicketList({ page: 1, page_size: 20, keyword, status: statusFilter, type: typeFilter, source_type: sourceTypeFilter }),
            ]);
            setOverview(overviewData);
            setList(listData.list || []);
        } catch (error) {
            console.error('加载工单中心失败:', error);
            alert('加载工单中心失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData();
    }, [keyword, statusFilter, typeFilter, sourceTypeFilter]);

    const handleReset = () => {
        setSearchInput('');
        setKeyword('');
        setStatusFilter('all');
        setTypeFilter('all');
        setSourceTypeFilter('all');
    };

    const handleSync = async () => {
        try {
            const createdCount = await syncSupportTicketSystemExceptions();
            alert(`同步完成，本次新增 ${createdCount} 条系统工单`);
            await loadData();
        } catch (error) {
            console.error('同步系统异常工单失败:', error);
            alert('同步系统异常工单失败');
        }
    };

    const handleAssign = async (item: SupportTicketItem) => {
        try {
            await assignSupportTicket(String(item.id));
            await loadData();
            if (showDetailModal && detail?.id === item.id) {
                const fresh = await getSupportTicketDetail(String(item.id));
                setDetail(fresh);
                setResolutionNote(fresh.resolution_note || '');
            }
        } catch (error) {
            console.error('分配工单失败:', error);
            alert('分配工单失败');
        }
    };

    const handleUpdateStatus = async (item: SupportTicketItem, status: string) => {
        try {
            await updateSupportTicketStatus(String(item.id), status);
            await loadData();
            if (showDetailModal && detail?.id === item.id) {
                const fresh = await getSupportTicketDetail(String(item.id));
                setDetail(fresh);
                setResolutionNote(fresh.resolution_note || '');
            }
        } catch (error) {
            console.error('更新工单状态失败:', error);
            alert('更新工单状态失败');
        }
    };

    const handleViewDetail = async (item: SupportTicketItem) => {
        try {
            const detailData = await getSupportTicketDetail(String(item.id));
            setDetail(detailData);
            setResolutionNote(detailData.resolution_note || '');
            setShowDetailModal(true);
        } catch (error) {
            console.error('获取工单详情失败:', error);
            alert('获取工单详情失败');
        }
    };

    const handleSaveResolutionNote = async () => {
        if (!detail) {
            return;
        }
        setResolutionSubmitting(true);
        try {
            await updateSupportTicketResolutionNote(String(detail.id), resolutionNote.trim());
            const fresh = await getSupportTicketDetail(String(detail.id));
            setDetail(fresh);
            setResolutionNote(fresh.resolution_note || '');
            await loadData();
        } catch (error) {
            console.error('保存工单处理备注失败:', error);
            alert('保存工单处理备注失败');
        } finally {
            setResolutionSubmitting(false);
        }
    };

    const handleCreate = async () => {
        if (!createForm.title.trim()) {
            alert('请先填写工单标题');
            return;
        }
        setCreateSubmitting(true);
        try {
            await createSupportTicket({
                user_id: createForm.user_id.trim() ? Number(createForm.user_id) : undefined,
                type: createForm.type,
                title: createForm.title.trim(),
                content: createForm.content.trim(),
                priority: createForm.priority,
            });
            setShowCreateModal(false);
            setCreateForm({ user_id: '', type: 'complaint', title: '', content: '', priority: 'medium' });
            await loadData();
        } catch (error) {
            console.error('创建工单失败:', error);
            alert('创建工单失败');
        } finally {
            setCreateSubmitting(false);
        }
    };

    const detailSourceTarget = getSourceTarget(detail);
    const detailSourceSummaryItems = getSourceSummaryItems(detail);

    return (
        <Layout title="异常工单中心">
            <div className="support-ticket-page">
                <ManagementSearchPanel
                    title="工单检索与异常处理"
                    description="先按用户、工单类型、来源和状态找到目标工单，再决定是同步异常、手工建单，还是直接推进处理。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading}>
                                <FiRefreshCwIcon />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                            <button className="btn-secondary" onClick={() => setShowCreateModal(true)}>
                                <FiClipboardIcon />
                                新建工单
                            </button>
                            <button className="btn-primary" onClick={handleSync}>
                                <FiToolIcon />
                                同步异常
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
                                    placeholder="搜索用户名、昵称、工单标题或来源ID"
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            setKeyword(searchInput.trim());
                                        }
                                    }}
                                />
                            </div>
                            <select className="management-search-select" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
                                <option value="all">全部状态</option>
                                <option value="open">待处理</option>
                                <option value="in_progress">处理中</option>
                                <option value="closed">已关闭</option>
                            </select>
                            <select className="management-search-select" value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)}>
                                <option value="all">全部类型</option>
                                <option value="complaint">用户投诉</option>
                                <option value="order">异常订单</option>
                                <option value="task">失败任务</option>
                            </select>
                            <select className="management-search-select" value={sourceTypeFilter} onChange={(e) => setSourceTypeFilter(e.target.value)}>
                                <option value="all">全部来源</option>
                                <option value="manual">人工录入</option>
                                <option value="order">订单同步</option>
                                <option value="task">任务同步</option>
                            </select>
                            <button className="btn-primary" onClick={() => setKeyword(searchInput.trim())}>搜索工单</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前工单 <strong>{list.length}</strong> 条
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                                {statusFilter !== 'all' ? <span className="management-search-tag">状态：{statusLabelMap[statusFilter] || statusFilter}</span> : null}
                                {typeFilter !== 'all' ? <span className="management-search-tag">类型：{typeLabelMap[typeFilter] || typeFilter}</span> : null}
                                {sourceTypeFilter !== 'all' ? <span className="management-search-tag">来源：{sourceTypeLabelMap[sourceTypeFilter] || sourceTypeFilter}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="support-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiClipboardIcon /></div>
                        <div className="stat-label">工单总数</div>
                        <div className="stat-value">{overview.total_count}</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAlertCircleIcon /></div>
                        <div className="stat-label">待处理 / 处理中</div>
                        <div className="stat-value">{overview.open_count} / {overview.in_progress_count}</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiToolIcon /></div>
                        <div className="stat-label">高优先级</div>
                        <div className="stat-value">{overview.high_priority_count}</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiRefreshCwIcon /></div>
                        <div className="stat-label">已关闭</div>
                        <div className="stat-value">{overview.closed_count}</div>
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
                                {list.length === 0 ? (
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
                                                {!item.assignee_name && item.status !== 'closed' ? <button className="btn-inline" onClick={() => void handleAssign(item)}>分配给我</button> : null}
                                                {item.status === 'open' ? <button className="btn-inline" onClick={() => void handleUpdateStatus(item, 'in_progress')}>开始处理</button> : null}
                                                {item.status !== 'closed' ? <button className="btn-inline danger" onClick={() => void handleUpdateStatus(item, 'closed')}>关闭</button> : null}
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>

                {showCreateModal ? (
                    <div className="modal-overlay" onClick={() => setShowCreateModal(false)}>
                        <div className="modal-content support-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>新建投诉工单</h3>
                                <button className="modal-close" onClick={() => setShowCreateModal(false)}>✕</button>
                            </div>
                            <div className="modal-body support-form">
                                <label>
                                    <span>用户ID</span>
                                    <input value={createForm.user_id} onChange={(e) => setCreateForm((prev) => ({ ...prev, user_id: e.target.value }))} placeholder="可不填" />
                                </label>
                                <label>
                                    <span>工单类型</span>
                                    <select value={createForm.type} onChange={(e) => setCreateForm((prev) => ({ ...prev, type: e.target.value }))}>
                                        <option value="complaint">用户投诉</option>
                                        <option value="order">异常订单</option>
                                        <option value="task">失败任务</option>
                                    </select>
                                </label>
                                <label>
                                    <span>标题</span>
                                    <input value={createForm.title} onChange={(e) => setCreateForm((prev) => ({ ...prev, title: e.target.value }))} placeholder="例如：用户反馈付款后未到账" />
                                </label>
                                <label>
                                    <span>优先级</span>
                                    <select value={createForm.priority} onChange={(e) => setCreateForm((prev) => ({ ...prev, priority: e.target.value }))}>
                                        <option value="high">高优先级</option>
                                        <option value="medium">中优先级</option>
                                        <option value="low">低优先级</option>
                                    </select>
                                </label>
                                <label className="full">
                                    <span>内容说明</span>
                                    <textarea value={createForm.content} onChange={(e) => setCreateForm((prev) => ({ ...prev, content: e.target.value }))} rows={5} placeholder="把用户反馈、订单信息、异常现象写清楚" />
                                </label>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowCreateModal(false)}>取消</button>
                                <button className="btn-primary" onClick={() => void handleCreate()} disabled={createSubmitting}>{createSubmitting ? '提交中...' : '创建工单'}</button>
                            </div>
                        </div>
                    </div>
                ) : null}

                {showDetailModal && detail ? (
                    <div className="modal-overlay" onClick={() => setShowDetailModal(false)}>
                        <div className="modal-content support-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>工单详情</h3>
                                <button className="modal-close" onClick={() => setShowDetailModal(false)}>✕</button>
                            </div>
                            <div className="modal-body detail-stack">
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
                                <div className="detail-block">
                                    <label>来源载荷</label>
                                    <pre>{detail.source_payload || '暂无'}</pre>
                                </div>
                                <div className="detail-block">
                                    <label>处理备注</label>
                                    <textarea
                                        className="detail-note-textarea"
                                        value={resolutionNote}
                                        onChange={(e) => setResolutionNote(e.target.value)}
                                        rows={4}
                                        placeholder="记录处理过程、补偿结果、结论说明，方便后续追踪"
                                    />
                                </div>
                            </div>
                            <div className="modal-footer">
                                {detailSourceTarget ? <button className="btn-secondary" onClick={() => navigate(detailSourceTarget.path)}>{detailSourceTarget.label}</button> : null}
                                <button className="btn-secondary" onClick={() => void handleSaveResolutionNote()} disabled={resolutionSubmitting}>
                                    {resolutionSubmitting ? '保存中...' : '保存备注'}
                                </button>
                                {!detail.assignee_name && detail.status !== 'closed' ? <button className="btn-secondary" onClick={() => void handleAssign(detail)}>分配给我</button> : null}
                                {detail.status === 'open' ? <button className="btn-secondary" onClick={() => void handleUpdateStatus(detail, 'in_progress')}>开始处理</button> : null}
                                {detail.status !== 'closed' ? <button className="btn-primary" onClick={() => void handleUpdateStatus(detail, 'closed')}>关闭工单</button> : null}
                            </div>
                        </div>
                    </div>
                ) : null}
            </div>
        </Layout>
    );
};

export default SupportTickets;
