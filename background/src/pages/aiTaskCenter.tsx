import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
    backfillAITaskModels,
    batchCreateAITaskSupportTickets,
    createAITaskSupportTicket,
    createAIVideoTaskSupportTicket,
    getAITaskDetail,
    getAITaskList,
    getAIVideoTaskDetail,
    getAIVideoTaskList,
    retryAITask,
    type AITaskDetail,
    type AITaskItem,
    type AITaskSummary,
    type AIQueueCounts,
    type AITaskQueue,
} from '../api/aiTasks';
import { getSupportTicketDetail, updateSupportTicketResolutionNote, updateSupportTicketStatus } from '../api/supportTickets';
import './aiTaskCenter.scss';

const emptySummary: AITaskSummary = {
    total_count: 0,
    pending_count: 0,
    running_count: 0,
    failed_count: 0,
};

const emptyQueues: AIQueueCounts = { failed: 0, timeout: 0, running: 0, today_new: 0, total_open: 0 };

const pageSizeOptions = [20, 50, 100];

/**
 * 状态统一映射（改版 2026-10）：图片/视频两套状态机（pending·queued / running·processing / success·completed）
 * 归并为 4 个运营状态。列表展示与筛选选项都用这一份。
 */
const formatStatus = (status: string) => {
    switch (status) {
        case 'pending':
        case 'queued':
            return { label: '待处理', className: 'status-pending', key: 'pending' };
        case 'running':
        case 'processing':
            return { label: '处理中', className: 'status-running', key: 'running' };
        case 'success':
        case 'completed':
            return { label: '成功', className: 'status-success', key: 'success' };
        case 'failed':
            return { label: '失败', className: 'status-failed', key: 'failed' };
        default:
            return { label: status || '-', className: '', key: status || '-' };
    }
};

const sceneLabels: Record<string, string> = {
    ai_draw_single: 'AI 绘画-单图',
    ai_draw_multi: 'AI 绘画-多图',
    new_minnan_tool_text_to_image: '新闽派-文生图',
    new_minnan_tool_image_to_image: '新闽派-图生图',
    ai_chat_single: 'AI 对话-单轮',
    ai_chat_multi: 'AI 对话-多轮',
    rural_villa_design: '乡村别墅设计',
    allround_design: '全能设计',
    building_replacement: '建筑换新',
    parent_child_design: '亲子设计',
    ai_cost_doc: 'AI 造价文档',
};

const sceneLabel = (scene?: string) => (scene ? sceneLabels[scene] || scene : '-');

interface TaskQueueTab {
    key: AITaskQueue;
    label: string;
    help: string;
    /** 需要人处理的队列：角标用红色 */
    alert?: boolean;
}

const queueTabs: TaskQueueTab[] = [
    { key: 'failed', label: '失败待处理', help: '近7日失败任务，可排查原因、核对已退灵石或转工单', alert: true },
    { key: 'timeout', label: '超时未完成', help: '处理中但超过30分钟未出结果', alert: true },
    { key: 'running', label: '处理中', help: '排队与执行中的任务' },
    { key: 'today', label: '今日新增', help: '今天进入系统的全部任务' },
    { key: 'all', label: '全部任务', help: '全部任务记录，可按条件筛选' },
];

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

/** 主力生图模型（与后端降级链一致）：任务最终模型 ≠ 主力 即发生了降级 */
const PRIMARY_DRAW_MODEL = 'gpt-image-2-vip';
const FALLBACK_DRAW_MODELS = ['gemini-3-pro', 'seedream'];

const isFallbackModel = (model?: string) => {
    const value = String(model || '').toLowerCase();
    if (!value || value === '模型待回填') return false;
    if (value.startsWith(PRIMARY_DRAW_MODEL)) return false;
    return FALLBACK_DRAW_MODELS.some((prefix) => value.startsWith(prefix));
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

const formatQuickActionTimestamp = () => new Date().toLocaleString('zh-CN');

const AITaskCenter: React.FC = () => {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const [activeTab, setActiveTab] = useState<'image' | 'video'>('image');
    const [queue, setQueue] = useState<AITaskQueue>(() => {
        const fromUrl = searchParams.get('queue');
        return queueTabs.some((tab) => tab.key === fromUrl) ? (fromUrl as AITaskQueue) : 'failed';
    });
    const [list, setList] = useState<AITaskItem[]>([]);
    const [summary, setSummary] = useState<AITaskSummary>(emptySummary);
    const [queues, setQueues] = useState<AIQueueCounts>(emptyQueues);
    /** 视频队列计数（与图片分开存，Tab 切换时各自显示） */
    const [videoQueues, setVideoQueues] = useState<AIQueueCounts>(emptyQueues);
    /** 失败队列勾选中的任务 ID（批量转工单用） */
    const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
    const [batchLoading, setBatchLoading] = useState(false);
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
    const activeQueue = queueTabs.find((tab) => tab.key === queue) || queueTabs[0];

    const loadData = async (
        tab: 'image' | 'video',
        nextQueue: AITaskQueue = queue,
        nextKeyword = keyword,
        nextStatus = status,
        nextScene = scene,
        nextPage = page,
        nextPageSize = pageSize,
    ) => {
        setLoading(true);
        try {
            const recoveryPage = window.location.pathname;
            const params = {
                keyword: nextKeyword,
                status: nextQueue === 'all' ? nextStatus : 'all',
                scene: nextScene,
                page: nextPage,
                page_size: nextPageSize,
                queue: nextQueue,
            };
            const response = tab === 'image'
                ? await getAITaskList(params)
                : await getAIVideoTaskList(params);
            setList(response.list || []);
            setSummary(response.summary || emptySummary);
            if (response.queues) {
                if (tab === 'image') {
                    setQueues(response.queues);
                } else {
                    setVideoQueues(response.queues);
                }
            }
            setTotal(response.total || 0);
            setPage(response.page || nextPage);
            setPageSize(response.page_size || nextPageSize);
            reportPageLoadRecovered("加载AI任务中心失败", recoveryPage);
        } catch (error) {
            console.error('加载AI任务中心失败:', error);
            reportPageLoadError(error, '加载AI任务中心失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData('image', queue, '', 'all', 'all', 1, 20);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [queue]);

    useEffect(() => {
        // 支持 ?tab=image|video 与 ?status=failed（总控台/总览页带参直达，落地即应用）
        const tab = searchParams.get('tab');
        const statusParam = searchParams.get('status');
        const queueParam = searchParams.get('queue');
        let changed = false;
        if ((tab === 'image' || tab === 'video') && tab !== activeTab) {
            setActiveTab(tab);
            changed = true;
        }
        if (queueParam && queueTabs.some((t) => t.key === queueParam) && queueParam !== queue) {
            setQueue(queueParam as AITaskQueue);
            return; // queue 变化由上面的 effect 处理
        }
        if (statusParam && statusParam !== 'all' && statusParam !== status) {
            setStatus(statusParam);
            setQueue('all');
            void loadData(tab === 'video' ? 'video' : 'image', 'all', keyword, statusParam, scene, 1, pageSize);
            changed = true;
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [searchParams]);

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

    const pickQueue = (nextQueue: AITaskQueue) => {
        if (nextQueue === queue) return;
        setQueue(nextQueue);
        setBackfillFeedback('');
        const next = new URLSearchParams(searchParams);
        next.set('queue', nextQueue);
        setSearchParams(next, { replace: true });
    };

    const handleSearch = () => {
        void loadData(activeTab, queue, keyword, status, scene, 1, pageSize);
    };

    const handleReset = () => {
        setKeyword('');
        setStatus('all');
        setScene('all');
        setBackfillFeedback('');
        void loadData(activeTab, queue, '', 'all', 'all', 1, pageSize);
    };

    const handlePageSizeChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        const nextPageSize = Number(event.target.value) || 20;
        void loadData(activeTab, queue, keyword, status, scene, 1, nextPageSize);
    };

    const handlePrevPage = () => {
        if (page <= 1 || loading) {
            return;
        }
        void loadData(activeTab, queue, keyword, status, scene, page - 1, pageSize);
    };

    const handleNextPage = () => {
        if (page >= totalPages || loading) {
            return;
        }
        void loadData(activeTab, queue, keyword, status, scene, page + 1, pageSize);
    };

    const handleTabChange = (tab: 'image' | 'video') => {
        if (tab === activeTab) {
            return;
        }
        setBackfillFeedback('');
        setSelectedIds(new Set());
        setActiveTab(tab);
        void loadData(tab, queue, keyword, 'all', 'all', 1, pageSize);
    };

    const handleBackfillModels = async () => {
        setBackfillLoading(true);
        setBackfillFeedback('');
        try {
            const result = await backfillAITaskModels(500);
            const summaryText = `本次检查 ${result.inspected_count} 条成功图片任务，回填 ${result.updated_count} 条模型/接口信息。`;
            setBackfillFeedback(summaryText);
            alert(summaryText);
            await loadData('image', queue, keyword, status, scene, page, pageSize);
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
            const recoveryPage = window.location.pathname;
            const detail = activeTab === 'image'
                ? await getAITaskDetail(String(item.id))
                : await getAIVideoTaskDetail(String(item.id));
            setSelectedDetail(detail);
            const nextParams = new URLSearchParams(searchParams);
            nextParams.set('tab', activeTab);
            nextParams.set('taskId', String(item.id));
            setSearchParams(nextParams);
            reportPageLoadRecovered("加载任务详情失败", recoveryPage);
        } catch (error) {
            console.error('加载任务详情失败:', error);
            reportPageLoadError(error, '加载任务详情失败');
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

    const activeQueueCounts = activeTab === 'image' ? queues : videoQueues;
    const queueCount = (key: AITaskQueue): number | undefined => {
        switch (key) {
            case 'failed': return activeQueueCounts.failed;
            case 'timeout': return activeQueueCounts.timeout;
            case 'running': return activeQueueCounts.running;
            case 'today': return activeQueueCounts.today_new;
            default: return undefined;
        }
    };

    const toggleSelected = (id: number) => {
        setSelectedIds((prev) => {
            const next = new Set(prev);
            if (next.has(id)) {
                next.delete(id);
            } else {
                next.add(id);
            }
            return next;
        });
    };

    const handleBatchTickets = async () => {
        const ids = Array.from(selectedIds);
        if (!ids.length) return;
        if (!window.confirm(`将为勾选的 ${ids.length} 个失败任务批量创建异常工单（已有未关闭工单的自动跳过），是否继续？`)) {
            return;
        }
        setBatchLoading(true);
        try {
            const result = await batchCreateAITaskSupportTickets(ids);
            alert(`批量转单完成：新建 ${result.created_count} 个工单，${result.existed_count} 个任务已有工单跳过。`);
            setSelectedIds(new Set());
            navigate('/support-tickets');
        } catch (error: any) {
            console.error('批量转工单失败:', error);
            alert(`批量转工单失败: ${error?.message || '未知错误'}`);
        } finally {
            setBatchLoading(false);
        }
    };

    const handleRetryTask = async () => {
        if (!selectedTaskItem || selectedDetail?.type === 'video') return;
        if (!window.confirm(`重新执行任务 ${selectedTaskItem.task_no}？\n\n将以当前启用配置重新入队，不重复扣费（管理员补偿动作）。`)) {
            return;
        }
        try {
            const result = await retryAITask(selectedTaskItem.id);
            alert(`重试任务已入队：${result.new_task_no}（不重复扣费），稍后可在队列中查看进度。`);
            handleCloseDetail();
            void loadData(activeTab, queue, keyword, status, scene, 1, pageSize);
        } catch (error: any) {
            console.error('重新执行失败:', error);
            alert(`重新执行失败: ${error?.message || '未知错误'}`);
        }
    };

    return (
        <>
            <div className="ai-task-center-container">
                <div className="task-tabs">
                    <button className={`tab-button ${activeTab === 'image' ? 'active' : ''}`} onClick={() => handleTabChange('image')}>图片任务</button>
                    <button className={`tab-button ${activeTab === 'video' ? 'active' : ''}`} onClick={() => handleTabChange('video')}>视频任务</button>
                </div>

                <div className="task-queue-tabs">
                    {queueTabs.map((tab) => {
                        const count = queueCount(tab.key);
                        return (
                            <button
                                key={tab.key}
                                className={`task-queue-tab ${queue === tab.key ? 'on' : ''} ${tab.alert && count ? 'alert-t' : ''}`}
                                onClick={() => pickQueue(tab.key)}
                                title={tab.help}
                            >
                                {tab.label}
                                {count !== undefined ? <span className="task-queue-count">{count.toLocaleString()}</span> : null}
                            </button>
                        );
                    })}
                    <span className="task-queue-help">{activeQueue.help}</span>
                    {queue === 'failed' ? (
                        <button
                            className="task-batch-btn"
                            onClick={() => void handleBatchTickets()}
                            disabled={batchLoading || selectedIds.size === 0}
                        >
                            {batchLoading ? '转单中…' : `批量转工单${selectedIds.size ? `（已选 ${selectedIds.size}）` : '（先勾选任务）'}`}
                        </button>
                    ) : null}
                </div>

                <div className="task-search-card section-card">
                    <div className="task-search-top">
                        <div className="task-search-title-block">
                            <h3>{activeTab === 'image' ? `${activeQueue.label} · 任务检索` : '视频任务检索'}</h3>
                            <p>
                                {activeTab === 'image'
                                    ? '按任务状态、任务号、用户或场景查找。失败任务可查看原因、转工单，或进入用户详情跟进。'
                                    : '按任务号、用户和状态缩小范围，再查看失败原因或转异常工单。'}
                            </p>
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
                            {queue === 'all' ? (
                                <select className="task-select" value={status} onChange={(event) => setStatus(event.target.value)}>
                                    <option value="all">全部状态</option>
                                    <option value="pending">待处理</option>
                                    <option value="running">处理中</option>
                                    <option value="processing">处理中（历史任务）</option>
                                    <option value="success">成功</option>
                                    <option value="failed">失败</option>
                                </select>
                            ) : null}
                            {activeTab === 'image' && (
                                <select className="task-select" value={scene} onChange={(event) => setScene(event.target.value)}>
                                    <option value="all">全部场景</option>
                                    {Object.entries(sceneLabels).map(([value, label]) => (
                                        <option key={value} value={value}>{label}</option>
                                    ))}
                                </select>
                            )}
                        </div>
                        <div className="task-toolbar-right compact-actions">
                            <button className="btn-primary" onClick={handleSearch}>查询任务</button>
                        </div>
                    </div>
                </div>

                {backfillFeedback ? (
                    <div className="task-action-feedback-banner">{backfillFeedback}</div>
                ) : null}

                <div className="task-summary-banner">
                    <div className="task-summary-banner-row">
                        <span>{activeTab === 'image' ? `${activeQueue.label} · 共` : '当前筛选 · 共'}</span>
                        <strong>{total} 条</strong>
                    </div>
                    <div className="task-summary-banner-meta">
                        {`全部任务统计：失败 ${activeQueueCounts.failed} · 超时 ${activeQueueCounts.timeout} · 处理中 ${activeQueueCounts.running} · 今日新增 ${activeQueueCounts.today_new}（不受当前筛选影响）`}
                    </div>
                </div>

                <div className="task-table-container">
                    <table className="task-table">
                        <thead>
                            <tr>
                                {activeTab === 'image' && queue === 'failed' ? (
                                    <th className="task-check-cell">
                                        <input
                                            type="checkbox"
                                            checked={list.length > 0 && selectedIds.size === list.length}
                                            onChange={(event) => {
                                                setSelectedIds(event.target.checked ? new Set(list.map((item) => item.id)) : new Set());
                                            }}
                                            title="全选当前页"
                                        />
                                    </th>
                                ) : null}
                                <th>任务号</th>
                                <th>用户</th>
                                <th>{activeTab === 'image' ? '场景 / 模型' : '模型 / 时长'}</th>
                                <th>状态</th>
                                <th>灵石</th>
                                <th>{activeTab === 'image' ? '异常说明' : '生成要求 / 异常说明'}</th>
                                <th>创建时间</th>
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
                                    const degraded = activeTab === 'image' && isFallbackModel(resolvedModel);
                                    const showCheck = activeTab === 'image' && queue === 'failed';
                                    return (
                                        <tr key={item.task_no} className={item.status === 'failed' ? 'task-row-failed' : longRunning ? 'task-row-timeout' : ''}>
                                            {showCheck ? (
                                                <td className="task-check-cell">
                                                    <input
                                                        type="checkbox"
                                                        checked={selectedIds.has(item.id)}
                                                        onChange={() => toggleSelected(item.id)}
                                                    />
                                                </td>
                                            ) : null}
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
                                                        <strong>{sceneLabel(item.scene)}</strong>
                                                        <span>
                                                            {resolvedModel}
                                                            {degraded ? <em className="task-degrade-badge">备用服务</em> : null}
                                                        </span>
                                                    </div>
                                                ) : (
                                                    <div className="text-cell">
                                                        <strong>{item.model || '-'}</strong>
                                                        <span>{item.duration ? `${item.duration}秒` : '暂未记录时长'} / {item.resolution || '暂未记录分辨率'}</span>
                                                    </div>
                                                )}
                                            </td>
                                            <td>
                                                <div className="status-stack">
                                                    <span className={`status-badge ${statusMeta.className}`}>{statusMeta.label}</span>
                                                    {longRunning ? <span className="risk-badge">超30分钟未完成</span> : null}
                                                </div>
                                            </td>
                                            <td>
                                                {item.stones_used}
                                                {item.refunded_stones ? <span className="task-refund-tag">已退 {item.refunded_stones}</span> : null}
                                            </td>
                                            <td>
                                                <div className="text-cell">
                                                    {activeTab === 'video' && <strong>{item.prompt || '-'}</strong>}
                                                    <span>{item.error_message || '暂无异常说明'}</span>
                                                </div>
                                            </td>
                                            <td>{formatDateTime(item.created_at)}</td>
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
                                                    <button className="btn-link" onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}>查看用户详情</button>
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
                                                    <div><span>状态</span><strong>{formatStatus(selectedDetail.status).label}</strong></div>
                                                    <div><span>灵石消耗</span><strong>{selectedDetail.stones_used}</strong></div>
                                                    <div><span>创建时间</span><strong>{formatDateTime(selectedDetail.created_at)}</strong></div>
                                                    <div><span>更新时间</span><strong>{formatDateTime(selectedDetail.updated_at)}</strong></div>
                                                </div>
                                            </div>

                                            <div className="task-detail-card">
                                                <h4>生成信息</h4>
                                                <div className="task-detail-list">
                                                    <div><span>场景</span><strong>{sceneLabel(selectedDetail.scene)}</strong></div>
                                                    <div><span>模型</span><strong>{selectedResolvedModel}{isFallbackModel(selectedResolvedModel) ? '（备用服务）' : ''}</strong></div>
                                                    <div><span>分段数</span><strong>{selectedDetail.segment_count || 0}</strong></div>
                                                    <div><span>时长</span><strong>{selectedDetail.duration ? `${selectedDetail.duration} 秒` : '-'}</strong></div>
                                                    <div><span>分辨率</span><strong>{selectedDetail.resolution || '-'}</strong></div>
                                                    <div><span>结果地址</span><strong>{selectedDetail.oss_url || '-'}</strong></div>
                                                </div>
                                            </div>
                                        </div>

                                        {selectedDetail.prompt ? (
                                            <div className="task-detail-card full-width">
                                                <h4>生成要求</h4>
                                                <pre className="payload-block">{selectedDetail.prompt}</pre>
                                            </div>
                                        ) : null}

                                        {selectedDetail.error_message ? (
                                            <div className="task-detail-card full-width danger-card">
                                                <h4>异常说明</h4>
                                                <pre className="payload-block">{selectedDetail.error_message}</pre>
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
                                        <details key={`${selectedDetail.type}-${selectedDetail.id}`} className="task-detail-card full-width execution-meta-card">
                                            <summary>技术诊断详情（排查问题时查看）</summary>
                                            <div className="task-detail-list">
                                                <div><span>原始状态</span><strong>{selectedDetail.raw_status || selectedDetail.status}</strong></div>
                                                <div><span>调用接口</span><strong>{selectedDetail.api_endpoint || selectedExecutionMeta?.api_endpoint || '-'}</strong></div>
                                                <div><span>外部任务ID</span><strong>{selectedDetail.external_id || '-'}</strong></div>
                                            </div>
                                            {selectedExecutionMeta ? (
                                                <div className="task-detail-card">
                                                    <h4>服务执行记录</h4>
                                                    <div className="task-detail-list">
                                                        <div><span>最终模型</span><strong>{selectedExecutionMeta.used_model || selectedResolvedModel}</strong></div>
                                                        <div><span>最终接口</span><strong>{selectedExecutionMeta.api_endpoint || selectedDetail.api_endpoint || '-'}</strong></div>
                                                        <div><span>尝试模型</span><strong>{selectedExecutionMeta.attempted_models?.filter(Boolean).join(' / ') || '-'}</strong></div>
                                                        <div><span>尝试接口</span><strong>{selectedExecutionMeta.attempted_endpoints?.filter(Boolean).join(' / ') || '-'}</strong></div>
                                                    </div>
                                                </div>
                                            ) : null}
                                            {selectedDetail.raw_error_message && selectedDetail.raw_error_message !== selectedDetail.error_message ? (
                                                <div className="task-detail-card danger-card">
                                                    <h4>原始错误信息</h4>
                                                    <pre className="payload-block">{selectedDetail.raw_error_message}</pre>
                                                </div>
                                            ) : null}
                                            {selectedDetail.request_payload ? (
                                                <div className="task-detail-card">
                                                    <h4>请求记录</h4>
                                                    <pre className="payload-block">{formatPayload(selectedDetail.request_payload)}</pre>
                                                </div>
                                            ) : null}
                                            {selectedDetail.result_payload ? (
                                                <div className="task-detail-card">
                                                    <h4>返回记录</h4>
                                                    <pre className="payload-block">{formatPayload(selectedDetail.result_payload)}</pre>
                                                </div>
                                            ) : null}
                                        </details>
                                    </div>
                                )}
                            </div>
                            <div className="modal-footer">
                                {selectedTaskItem && selectedDetail?.type !== 'video' && selectedDetail?.status === 'failed' ? (
                                    <button className="btn-secondary" onClick={() => void handleRetryTask()}>
                                        重新执行（不扣费）
                                    </button>
                                ) : null}
                                {selectedTaskItem && (
                                    <button
                                        className="btn-secondary"
                                        onClick={() => void handleCreateSupportTicket(selectedTaskItem, selectedDetail?.type === 'video' ? 'video' : 'image')}
                                        disabled={creatingTicketKey === `${selectedDetail?.type === 'video' ? 'video' : 'image'}-${selectedTaskItem.id}`}
                                    >
                                        {creatingTicketKey === `${selectedDetail?.type === 'video' ? 'video' : 'image'}-${selectedTaskItem.id}` ? '转单中...' : '转异常工单'}
                                    </button>
                                )}
                                {selectedTaskItem ? <button className="btn-secondary" onClick={() => navigate(`/user-workbench?userId=${selectedTaskItem.user_id}`)}>查看用户详情</button> : null}
                                <button className="btn-secondary" onClick={() => navigate('/support-tickets')}>去工单中心</button>
                                <button className="btn-primary" onClick={handleCloseDetail}>关闭</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default AITaskCenter;
