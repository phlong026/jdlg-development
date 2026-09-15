import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import Layout from '../component/layout';
import {
    backfillAITaskModels,
    createAITaskSupportTicket,
    createAIVideoTaskSupportTicket,
    getAITaskDetail,
    getAITaskList,
    getAIVideoTaskDetail,
    getAIVideoTaskList,
    type AITaskDetail,
    type AITaskItem,
    type AITaskSummary,
} from '../api/aiTasks';
import { getSupportTicketDetail, updateSupportTicketResolutionNote, updateSupportTicketStatus } from '../api/supportTickets';
import './aiTaskCenter.scss';

const emptySummary: AITaskSummary = {
    total_count: 0,
    pending_count: 0,
    running_count: 0,
    failed_count: 0,
};

const pageSizeOptions = [20, 50, 100];

const formatStatus = (status: string) => {
    const map: Record<string, { label: string; className: string }> = {
        pending: { label: '待处理', className: 'status-pending' },
        running: { label: '处理中', className: 'status-running' },
        processing: { label: '处理中', className: 'status-running' },
        success: { label: '成功', className: 'status-success' },
        failed: { label: '失败', className: 'status-failed' },
    };
    return map[status] || { label: status || '-', className: '' };
};

const formatDateTime = (value?: string) => {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const formatPayload = (value?: string) => {
    if (!value) return '暂无';
    try {
        return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
        return value;
    }
};

interface TaskExecutionMeta {
    used_model?: string;
    api_endpoint?: string;
    attempted_models?: string[];
    attempted_endpoints?: string[];
}

const parseExecutionMeta = (value?: string): TaskExecutionMeta | null => {
    if (!value) return null;
    try {
        const parsed = JSON.parse(value) as { execution_meta?: TaskExecutionMeta } & TaskExecutionMeta;
        if (parsed.execution_meta) {
            return parsed.execution_meta;
        }
        if (parsed.used_model || parsed.api_endpoint || parsed.attempted_models || parsed.attempted_endpoints) {
            return parsed;
        }
        return null;
    } catch {
        return null;
    }
};

const inferModelFromEndpoint = (endpoint?: string) => {
    const raw = String(endpoint || '').trim();
    if (!raw) return '';
    const lower = raw.toLowerCase();
    const markerIndex = lower.indexOf('/models/');
    if (markerIndex >= 0) {
        const segment = raw.slice(markerIndex + '/models/'.length);
        return segment.split(/[:/?]/)[0]?.trim() || '';
    }
    const seedreamIndex = lower.indexOf('seedream-');
    if (seedreamIndex >= 0) {
        const segment = raw.slice(seedreamIndex);
        return (segment.match(/[A-Za-z0-9._-]+/) || [''])[0].trim();
    }
    return '';
};

const resolveTaskModelText = (task?: { model?: string; api_endpoint?: string }, resultPayload?: string) => {
    const directModel = String(task?.model || '').trim();
    if (directModel) return directModel;
    const executionMeta = parseExecutionMeta(resultPayload);
    const metaModel = String(executionMeta?.used_model || '').trim();
    if (metaModel) return metaModel;
    const endpointModel = inferModelFromEndpoint(task?.api_endpoint);
    if (endpointModel) return endpointModel;
    const metaEndpointModel = inferModelFromEndpoint(executionMeta?.api_endpoint);
    if (metaEndpointModel) return metaEndpointModel;
    return '模型待回填';
};

const isLongRunningTask = (item: AITaskItem) => {
    if (!['pending', 'running', 'processing'].includes(item.status)) {
        return false;
    }
    const createdAt = new Date(item.created_at).getTime();
    if (Number.isNaN(createdAt)) {
        return false;
    }
    return Date.now() - createdAt >= 30 * 60 * 1000;
};

const getTaskFilterSummary = (params: { tab: 'image' | 'video'; keyword: string; status: string; scene: string }) => {
    const parts: string[] = [];
    parts.push(params.tab === 'image' ? '图片任务' : '视频任务');
    if (params.keyword) parts.push(`关键词：${params.keyword}`);
    if (params.status !== 'all') parts.push(`状态：${formatStatus(params.status).label}`);
    if (params.tab === 'image' && params.scene !== 'all') parts.push(`场景：${params.scene}`);
    return parts.join(' ｜ ');
};

const formatQuickActionTimestamp = () => new Date().toLocaleString('zh-CN');

const AITaskCenter: React.FC = () => {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const [activeTab, setActiveTab] = useState<'image' | 'video'>('image');
    const [list, setList] = useState<AITaskItem[]>([]);
    const [summary, setSummary] = useState<AITaskSummary>(emptySummary);
    const [loading, setLoading] = useState(false);
    const [detailLoading, setDetailLoading] = useState(false);
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [selectedDetail, setSelectedDetail] = useState<AITaskDetail | null>(null);
    const [selectedTaskItem, setSelectedTaskItem] = useState<AITaskItem | null>(null);
    const [keyword, setKeyword] = useState('');
    const [status, setStatus] = useState('all');
    const [scene, setScene] = useState('all');
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [total, setTotal] = useState(0);
    const [creatingTicketKey, setCreatingTicketKey] = useState<string | null>(null);
    const [quickActionKey, setQuickActionKey] = useState<string | null>(null);
    const [quickActionFeedback, setQuickActionFeedback] = useState('');
    const [backfillLoading, setBackfillLoading] = useState(false);
    const [backfillFeedback, setBackfillFeedback] = useState('');

    const selectedExecutionMeta = parseExecutionMeta(selectedDetail?.result_payload);
    const selectedResolvedModel = resolveTaskModelText(selectedDetail, selectedDetail?.result_payload);
    const totalPages = Math.max(1, Math.ceil(total / pageSize) || 1);
    const currentStart = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const currentEnd = total === 0 ? 0 : Math.min(page * pageSize, total);

    const loadData = async (
        tab: 'image' | 'video',
        nextKeyword = keyword,
        nextStatus = status,
        nextScene = scene,
        nextPage = page,
        nextPageSize = pageSize,
    ) => {
        setLoading(true);
        try {
            const response = tab === 'image'
                ? await getAITaskList({ keyword: nextKeyword, status: nextStatus, scene: nextScene, page: nextPage, page_size: nextPageSize })
                : await getAIVideoTaskList({ keyword: nextKeyword, status: nextStatus, page: nextPage, page_size: nextPageSize });
            setList(response.list || []);
            setSummary(response.summary || emptySummary);
            setTotal(response.total || 0);
            setPage(response.page || nextPage);
            setPageSize(response.page_size || nextPageSize);
        } catch (error) {
            console.error('加载AI任务中心失败:', error);
            alert('加载AI任务中心失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData(activeTab, keyword, status, scene, 1, pageSize);
    }, [activeTab]);

    useEffect(() => {
        const tab = searchParams.get('tab');
        if ((tab === 'image' || tab === 'video') && tab !== activeTab) {
            setActiveTab(tab);
        }
    }, [searchParams, activeTab]);

    useEffect(() => {
        const taskId = searchParams.get('taskId');
        const tab = searchParams.get('tab') === 'video' ? 'video' : 'image';
        if (!taskId) {
            return;
        }
        if (selectedDetail?.id && String(selectedDetail.id) === taskId && selectedDetail.type === tab) {
            return;
        }
        void (async () => {
            try {
                const detail = tab === 'image'
                    ? await getAITaskDetail(taskId)
                    : await getAIVideoTaskDetail(taskId);
                setSelectedDetail(detail);
                setSelectedTaskItem((prev) => prev && prev.id === detail.id ? prev : {
                    id: detail.id,
                    task_no: detail.task_no,
                    user_id: detail.user_id,
                    username: '',
                    scene: detail.scene,
                    model: detail.model,
                    api_endpoint: detail.api_endpoint,
                    prompt: detail.prompt,
                    status: detail.status,
                    raw_status: detail.raw_status,
                    stones_used: detail.stones_used,
                    error_message: detail.error_message || '',
                    segment_count: detail.segment_count,
                    duration: detail.duration,
                    resolution: detail.resolution,
                    created_at: detail.created_at,
                    updated_at: detail.updated_at,
                });
                setQuickActionFeedback('');
                setShowDetailModal(true);
            } catch (error) {
                console.error('按来源打开任务详情失败:', error);
            }
        })();
    }, [searchParams, selectedDetail?.id, selectedDetail?.type]);

    const handleSearch = () => {
        void loadData(activeTab, keyword, status, scene, 1, pageSize);
    };

    const handleReset = () => {
        setKeyword('');
        setStatus('all');
        setScene('all');
        setBackfillFeedback('');
        void loadData(activeTab, '', 'all', 'all', 1, pageSize);
    };

    const handlePageSizeChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        const nextPageSize = Number(event.target.value) || 20;
        void loadData(activeTab, keyword, status, scene, 1, nextPageSize);
    };

    const handlePrevPage = () => {
        if (page <= 1 || loading) {
            return;
        }
        void loadData(activeTab, keyword, status, scene, page - 1, pageSize);
    };

    const handleNextPage = () => {
        if (page >= totalPages || loading) {
            return;
        }
        void loadData(activeTab, keyword, status, scene, page + 1, pageSize);
    };

    const handleTabChange = (tab: 'image' | 'video') => {
        if (tab === activeTab) {
            return;
        }
        setBackfillFeedback('');
        setActiveTab(tab);
    };

    const handleBackfillModels = async () => {
        setBackfillLoading(true);
        setBackfillFeedback('');
        try {
            const result = await backfillAITaskModels(500);
            const summaryText = `本次检查 ${result.inspected_count} 条成功图片任务，回填 ${result.updated_count} 条模型/接口信息。`;
            setBackfillFeedback(summaryText);
            alert(summaryText);
            await loadData('image', activeTab === 'image' ? keyword : '', activeTab === 'image' ? status : 'all', activeTab === 'image' ? scene : 'all', activeTab === 'image' ? page : 1, activeTab === 'image' ? pageSize : 20);
        } catch (error) {
            console.error('回填历史任务模型失败:', error);
            alert('回填历史任务模型失败');
        } finally {
            setBackfillLoading(false);
        }
    };

    const handleViewDetail = async (item: AITaskItem) => {
        setSelectedTaskItem(item);
        setSelectedDetail(null);
        setQuickActionFeedback('');
        setShowDetailModal(true);
        setDetailLoading(true);
        try {
            const detail = activeTab === 'image'
                ? await getAITaskDetail(String(item.id))
                : await getAIVideoTaskDetail(String(item.id));
            setSelectedDetail(detail);
            const nextParams = new URLSearchParams(searchParams);
            nextParams.set('tab', activeTab);
            nextParams.set('taskId', String(item.id));
            setSearchParams(nextParams);
        } catch (error) {
            console.error('加载任务详情失败:', error);
            alert('加载任务详情失败');
            setShowDetailModal(false);
        } finally {
            setDetailLoading(false);
        }
    };

    const handleCloseDetail = () => {
        setShowDetailModal(false);
        setSelectedDetail(null);
        setSelectedTaskItem(null);
        setQuickActionFeedback('');
        const nextParams = new URLSearchParams(searchParams);
        nextParams.delete('taskId');
        setSearchParams(nextParams);
    };

    const handleCreateSupportTicket = async (item: Pick<AITaskItem, 'id' | 'task_no'>, taskType: 'image' | 'video') => {
        const actionKey = `${taskType}-${item.id}`;
        setCreatingTicketKey(actionKey);
        try {
            const result = taskType === 'image'
                ? await createAITaskSupportTicket(String(item.id))
                : await createAIVideoTaskSupportTicket(String(item.id));
            alert(result.existed ? `该任务已有未关闭工单 #${result.id}，现在带你去工单中心继续跟进。` : `已为任务 ${item.task_no} 创建异常工单 #${result.id}。`);
            navigate('/support-tickets');
        } catch (error) {
            console.error('创建任务异常工单失败:', error);
            alert('创建任务异常工单失败');
        } finally {
            setCreatingTicketKey(null);
        }
    };

    const handleQuickDisposition = async (label: '已排查' | '已补偿' | '已回访') => {
        if (!selectedTaskItem) {
            return;
        }
        const taskType: 'image' | 'video' = selectedDetail?.type === 'video' ? 'video' : 'image';
        const actionKey = `${label}-${taskType}-${selectedTaskItem.id}`;
        setQuickActionKey(actionKey);
        try {
            const createResult = taskType === 'image'
                ? await createAITaskSupportTicket(String(selectedTaskItem.id))
                : await createAIVideoTaskSupportTicket(String(selectedTaskItem.id));
            const ticket = await getSupportTicketDetail(String(createResult.id));
            const appendedLine = `[${formatQuickActionTimestamp()}] ${label}：来自AI任务中心快捷处理`;
            const mergedNote = ticket.resolution_note?.trim()
                ? `${ticket.resolution_note.trim()}\n${appendedLine}`
                : appendedLine;
            await updateSupportTicketResolutionNote(String(ticket.id), mergedNote);
            if (ticket.status === 'open') {
                await updateSupportTicketStatus(String(ticket.id), 'in_progress');
            }
            setQuickActionFeedback(`${label}已同步到工单 #${ticket.id}，并写入当前处理备注。`);
        } catch (error) {
            console.error(`执行${label}失败:`, error);
            alert(`${label}失败`);
        } finally {
            setQuickActionKey(null);
        }
    };

    return (
        <Layout title="AI任务中心">
            <div className="ai-task-center-container">
                <div className="task-tabs">
                    <button className={`tab-button ${activeTab === 'image' ? 'active' : ''}`} onClick={() => handleTabChange('image')}>图片任务</button>
                    <button className={`tab-button ${activeTab === 'video' ? 'active' : ''}`} onClick={() => handleTabChange('video')}>视频任务</button>
                </div>

                <div className="task-search-card section-card">
                    <div className="task-search-top">
                        <div className="task-search-title-block">
                            <h3>{activeTab === 'image' ? '图片任务检索' : '视频任务检索'}</h3>
                            <p>{activeTab === 'image' ? '先按任务号、用户、状态和场景筛选，再决定是看详情、转异常工单，还是进入用户360继续跟进。' : '先按任务号、用户和状态缩小范围，再查看失败原因或转异常工单。'}</p>
                        </div>
                        <div className="task-toolbar-right">
                            {activeTab === 'image' && (
                                <button className="btn-secondary" onClick={() => void handleBackfillModels()} disabled={backfillLoading || loading}>
                                    {backfillLoading ? '回填中...' : '回填历史模型'}
                                </button>
                            )}
                            <button className="btn-secondary" onClick={handleReset} disabled={loading}>{loading ? '刷新中...' : '重置筛选'}</button>
                        </div>
                    </div>
                    <div className="task-toolbar">
                        <div className="task-toolbar-left">
                            <input
                                className="task-input"
                                value={keyword}
                                onChange={(event) => setKeyword(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === 'Enter') {
                                        handleSearch();
                                    }
                                }}
                                placeholder={activeTab === 'image' ? '搜索任务号、用户ID、用户名' : '搜索视频任务号、用户ID、用户名、提示词'}
                            />
                            <select className="task-select" value={status} onChange={(event) => setStatus(event.target.value)}>
                                <option value="all">全部状态</option>
                                <option value="pending">待处理</option>
                                <option value="running">处理中</option>
                                <option value="processing">处理中</option>
                                <option value="success">成功</option>
                                <option value="failed">失败</option>
                                <option value="queued">队列中</option>
                                <option value="completed">已完成</option>
                            </select>
                            {activeTab === 'image' && (
                                <select className="task-select" value={scene} onChange={(event) => setScene(event.target.value)}>
                                    <option value="all">全部场景</option>
                                    <option value="ai_draw_single">单图生成</option>
                                    <option value="ai_draw_multi">多图生成</option>
                                    <option value="ai_chat_single">单轮聊天</option>
                                    <option value="ai_chat_multi">多轮聊天</option>
                                    <option value="ai_cost_doc">造价文档</option>
                                </select>
                            )}
                        </div>
                        <div className="task-toolbar-right compact-actions">
                            <button className="btn-primary" onClick={handleSearch}>查询任务</button>
                        </div>
                    </div>
                    <div className="task-search-footer">
                        <div className="task-filter-tags">
                            {keyword.trim() ? <span className="task-filter-tag">关键词：{keyword.trim()}</span> : null}
                            {status !== 'all' ? <span className="task-filter-tag">状态：{formatStatus(status).label}</span> : null}
                            {activeTab === 'image' && scene !== 'all' ? <span className="task-filter-tag">场景：{scene}</span> : null}
                        </div>
                    </div>
                </div>

                {backfillFeedback ? (
                    <div className="task-action-feedback-banner">{backfillFeedback}</div>
                ) : null}

                <div className="task-summary-banner">
                    <div className="task-summary-banner-row">
                        <span>当前筛选摘要</span>
                        <strong>{summary.total_count} 条</strong>
                    </div>
                    <div className="task-summary-banner-meta">
                        {getTaskFilterSummary({ tab: activeTab, keyword: keyword.trim(), status, scene })}
                    </div>
                </div>

                <div className="task-summary-grid">
                    <div className="ai-task-summary-card"><span>任务总数</span><strong>{summary.total_count}</strong></div>
                    <div className="ai-task-summary-card"><span>待处理</span><strong>{summary.pending_count}</strong></div>
                    <div className="ai-task-summary-card"><span>处理中</span><strong>{summary.running_count}</strong></div>
                    <div className="ai-task-summary-card danger"><span>失败任务</span><strong>{summary.failed_count}</strong></div>
                </div>

                <div className="task-table-container">
                    <table className="task-table">
                        <thead>
                            <tr>
                                <th>任务号</th>
                                <th>用户</th>
                                <th>{activeTab === 'image' ? '场景 / 模型' : '模型 / 时长'}</th>
                                <th>状态</th>
                                <th>灵石</th>
                                <th>{activeTab === 'image' ? '错误信息' : '提示词 / 错误信息'}</th>
                                <th>创建时间</th>
                                <th>更新时间</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {list.length === 0 ? (
                                <tr>
                                    <td colSpan={9} className="empty-state">暂无任务记录</td>
                                </tr>
                            ) : (
                                list.map((item) => {
                                    const statusMeta = formatStatus(item.status);
                                    const longRunning = isLongRunningTask(item);
                                    const resolvedModel = resolveTaskModelText(item);
                                    return (
                                        <tr key={item.task_no}>
                                            <td><span className="task-no">{item.task_no}</span></td>
                                            <td>
                                                <div className="user-cell">
                                                    <button className="user-link-button" onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}>
                                                        {item.username || `用户${item.user_id}`}
                                                    </button>
                                                    <span>ID {item.user_id}</span>
                                                </div>
                                            </td>
                                            <td>
                                                {activeTab === 'image' ? (
                                                    <div className="text-cell">
                                                        <strong>{item.scene || '-'}</strong>
                                                        <span>{resolvedModel}</span>
                                                    </div>
                                                ) : (
                                                    <div className="text-cell">
                                                        <strong>{item.model || '-'}</strong>
                                                        <span>{item.duration ? `${item.duration}秒` : '时长待回填'} / {item.resolution || '分辨率待回填'}</span>
                                                    </div>
                                                )}
                                            </td>
                                            <td>
                                                <div className="status-stack">
                                                    <span className={`status-badge ${statusMeta.className}`}>{statusMeta.label}</span>
                                                    {longRunning ? <span className="risk-badge">超30分钟未完成</span> : null}
                                                </div>
                                            </td>
                                            <td>{item.stones_used}</td>
                                            <td>
                                                <div className="text-cell">
                                                    {activeTab === 'video' && <strong>{item.prompt || '-'}</strong>}
                                                    <span>{item.error_message || '无异常'}</span>
                                                </div>
                                            </td>
                                            <td>{formatDateTime(item.created_at)}</td>
                                            <td>{formatDateTime(item.updated_at)}</td>
                                            <td>
                                                <div className="task-action-group">
                                                    <button className="btn-link" onClick={() => void handleViewDetail(item)}>查看详情</button>
                                                    <button
                                                        className="btn-link danger"
                                                        onClick={() => void handleCreateSupportTicket(item, activeTab)}
                                                        disabled={creatingTicketKey === `${activeTab}-${item.id}`}
                                                    >
                                                        {creatingTicketKey === `${activeTab}-${item.id}` ? '转单中...' : '转异常工单'}
                                                    </button>
                                                    <button className="btn-link" onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}>进入用户360</button>
                                                </div>
                                            </td>
                                        </tr>
                                    );
                                })
                            )}
                        </tbody>
                    </table>
                </div>

                <div className="task-pagination">
                    <div className="task-pagination-left">
                        <span className="task-pagination-label">每页显示</span>
                        <select className="task-select task-page-size-select" value={pageSize} onChange={handlePageSizeChange} disabled={loading}>
                            {pageSizeOptions.map((option) => (
                                <option key={option} value={option}>{option} 条</option>
                            ))}
                        </select>
                        <span className="task-page-range">当前显示 {currentStart}-{currentEnd} / 共 {total} 条</span>
                    </div>
                    <div className="task-pagination-right">
                        <button className="btn-page" disabled={page <= 1 || loading} onClick={handlePrevPage}>上一页</button>
                        <span className="page-info">第 {page} 页，共 {totalPages} 页</span>
                        <button className="btn-page" disabled={page >= totalPages || loading} onClick={handleNextPage}>下一页</button>
                    </div>
                </div>

                {showDetailModal && (
                    <div className="modal-overlay" onClick={handleCloseDetail}>
                        <div className="modal-content task-detail-modal" onClick={(event) => event.stopPropagation()}>
                            <div className="modal-header">
                                <h3>任务详情</h3>
                                <button className="modal-close" onClick={handleCloseDetail}>✕</button>
                            </div>
                            <div className="modal-body">
                                {detailLoading ? (
                                    <div className="empty-state slim">正在加载任务详情...</div>
                                ) : !selectedDetail ? (
                                    <div className="empty-state slim">暂无任务详情</div>
                                ) : (
                                    <div className="task-detail-body">
                                        <div className="task-detail-grid">
                                            <div className="task-detail-card">
                                                <h4>基础信息</h4>
                                                <div className="task-detail-list">
                                                    <div><span>任务号</span><strong>{selectedDetail.task_no}</strong></div>
                                                    <div><span>任务类型</span><strong>{selectedDetail.type === 'video' ? '视频任务' : '图片任务'}</strong></div>
                                                    <div><span>用户ID</span><strong>{selectedDetail.user_id}</strong></div>
                                                    <div><span>用户名</span><strong>{selectedTaskItem?.username || '-'}</strong></div>
                                                    <div><span>状态</span><strong>{formatStatus(selectedDetail.status).label}</strong></div>
                                                    <div><span>灵石消耗</span><strong>{selectedDetail.stones_used}</strong></div>
                                                    <div><span>创建时间</span><strong>{formatDateTime(selectedDetail.created_at)}</strong></div>
                                                    <div><span>更新时间</span><strong>{formatDateTime(selectedDetail.updated_at)}</strong></div>
                                                </div>
                                            </div>

                                            <div className="task-detail-card">
                                                <h4>{selectedDetail.type === 'video' ? '任务配置' : '任务上下文'}</h4>
                                                <div className="task-detail-list">
                                                    <div><span>场景</span><strong>{selectedDetail.scene || '-'}</strong></div>
                                                    <div><span>模型</span><strong>{selectedResolvedModel}</strong></div>
                                                    <div><span>调用接口</span><strong>{selectedDetail.api_endpoint || selectedExecutionMeta?.api_endpoint || '-'}</strong></div>
                                                    <div><span>原始状态</span><strong>{selectedDetail.raw_status || selectedDetail.status}</strong></div>
                                                    <div><span>分段数</span><strong>{selectedDetail.segment_count || 0}</strong></div>
                                                    <div><span>时长</span><strong>{selectedDetail.duration ? `${selectedDetail.duration} 秒` : '-'}</strong></div>
                                                    <div><span>分辨率</span><strong>{selectedDetail.resolution || '-'}</strong></div>
                                                    <div><span>外部任务ID</span><strong>{selectedDetail.external_id || '-'}</strong></div>
                                                    <div><span>结果地址</span><strong>{selectedDetail.oss_url || '-'}</strong></div>
                                                </div>
                                            </div>
                                        </div>

                                        {selectedExecutionMeta ? (
                                            <div className="task-detail-card full-width execution-meta-card">
                                                <h4>执行链路</h4>
                                                <div className="task-detail-list">
                                                    <div><span>最终模型</span><strong>{selectedExecutionMeta.used_model || selectedResolvedModel}</strong></div>
                                                    <div><span>最终接口</span><strong>{selectedExecutionMeta.api_endpoint || selectedDetail.api_endpoint || '-'}</strong></div>
                                                    <div><span>尝试模型</span><strong>{selectedExecutionMeta.attempted_models?.filter(Boolean).join(' / ') || '-'}</strong></div>
                                                    <div><span>尝试接口</span><strong>{selectedExecutionMeta.attempted_endpoints?.filter(Boolean).join(' / ') || '-'}</strong></div>
                                                </div>
                                            </div>
                                        ) : null}

                                        {selectedDetail.prompt ? (
                                            <div className="task-detail-card full-width">
                                                <h4>提示词</h4>
                                                <pre className="payload-block">{selectedDetail.prompt}</pre>
                                            </div>
                                        ) : null}

                                        {selectedDetail.error_message ? (
                                            <div className="task-detail-card full-width danger-card">
                                                <h4>错误信息</h4>
                                                <pre className="payload-block">{selectedDetail.error_message}</pre>
                                            </div>
                                        ) : null}

                                        {selectedDetail.raw_error_message && selectedDetail.raw_error_message !== selectedDetail.error_message ? (
                                            <div className="task-detail-card full-width danger-card">
                                                <h4>原始错误信息</h4>
                                                <pre className="payload-block">{selectedDetail.raw_error_message}</pre>
                                            </div>
                                        ) : null}

                                        {selectedDetail.request_payload ? (
                                            <div className="task-detail-card full-width">
                                                <h4>请求载荷</h4>
                                                <pre className="payload-block">{formatPayload(selectedDetail.request_payload)}</pre>
                                            </div>
                                        ) : null}

                                        {selectedDetail.result_payload ? (
                                            <div className="task-detail-card full-width">
                                                <h4>结果载荷</h4>
                                                <pre className="payload-block">{formatPayload(selectedDetail.result_payload)}</pre>
                                            </div>
                                        ) : null}

                                        <div className="task-detail-card full-width quick-action-card">
                                            <h4>快捷处理动作</h4>
                                            <div className="quick-action-list">
                                                <button className="btn-secondary" onClick={() => void handleQuickDisposition('已排查')} disabled={quickActionKey !== null}>
                                                    {quickActionKey === `已排查-${selectedDetail.type}-${selectedDetail.id}` ? '处理中...' : '标记已排查'}
                                                </button>
                                                <button className="btn-secondary" onClick={() => void handleQuickDisposition('已补偿')} disabled={quickActionKey !== null}>
                                                    {quickActionKey === `已补偿-${selectedDetail.type}-${selectedDetail.id}` ? '处理中...' : '标记已补偿'}
                                                </button>
                                                <button className="btn-secondary" onClick={() => void handleQuickDisposition('已回访')} disabled={quickActionKey !== null}>
                                                    {quickActionKey === `已回访-${selectedDetail.type}-${selectedDetail.id}` ? '处理中...' : '标记已回访'}
                                                </button>
                                            </div>
                                        </div>

                                        {quickActionFeedback ? (
                                            <div className="task-detail-card full-width action-feedback-card">
                                                <h4>处理结果</h4>
                                                <div className="action-feedback-text">{quickActionFeedback}</div>
                                            </div>
                                        ) : null}
                                    </div>
                                )}
                            </div>
                            <div className="modal-footer">
                                {selectedTaskItem && (
                                    <button
                                        className="btn-secondary"
                                        onClick={() => void handleCreateSupportTicket(selectedTaskItem, selectedDetail?.type === 'video' ? 'video' : 'image')}
                                        disabled={creatingTicketKey === `${selectedDetail?.type === 'video' ? 'video' : 'image'}-${selectedTaskItem.id}`}
                                    >
                                        {creatingTicketKey === `${selectedDetail?.type === 'video' ? 'video' : 'image'}-${selectedTaskItem.id}` ? '转单中...' : '转异常工单'}
                                    </button>
                                )}
                                {selectedTaskItem ? <button className="btn-secondary" onClick={() => navigate(`/user-workbench?userId=${selectedTaskItem.user_id}`)}>进入用户360</button> : null}
                                <button className="btn-secondary" onClick={() => navigate('/support-tickets')}>去工单中心</button>
                                <button className="btn-primary" onClick={handleCloseDetail}>关闭</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default AITaskCenter;
