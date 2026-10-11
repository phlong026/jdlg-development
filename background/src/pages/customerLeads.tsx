import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { FiActivity, FiClock, FiFileText, FiRefreshCw, FiSearch, FiUserCheck, FiX } from 'react-icons/fi';
import {
    batchUpdateCustomerLeads,
    createCustomerLeadFollowup,
    getCustomerLeadDetail,
    getCustomerLeadList,
    getCustomerLeadOverview,
    updateCustomerLeadStatus,
    type CustomerLeadActionPayload,
    type CustomerLeadDetailResponse,
    type CustomerLeadFollowupItem,
    type CustomerLeadItem,
    type CustomerLeadOverview,
    type CustomerServiceEventItem,
} from '../api/customerLeads';
import { DEFAULT_LEAD_QUEUE, leadQueues } from '../component/navConfig';
import { publishLeadOverview } from '../component/leadOverviewStore';
import CustomerLeadHub from './customerLeadHub';
import { useManagementSession } from '../component/ManagementSession';
import './customerLeads.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiFileTextIcon = FiFileText as unknown as React.ComponentType<any>;
const FiUserCheckIcon = FiUserCheck as unknown as React.ComponentType<any>;
const FiXIcon = FiX as unknown as React.ComponentType<any>;
const FiClockIcon = FiClock as unknown as React.ComponentType<any>;
const FiActivityIcon = FiActivity as unknown as React.ComponentType<any>;

const defaultOverview: CustomerLeadOverview = {
    total_count: 0,
    new_count: 0,
    contacted_count: 0,
    high_intent_count: 0,
    converted_count: 0,
    today_new_count: 0,
    todo_count: 0,
    overdue_count: 0,
};

const statusLabelMap: Record<string, string> = {
    new: '新线索',
    contacted: '已联系',
    converted: '已成交',
    invalid: '无效',
};

const intentLabelMap: Record<string, string> = {
    low: '低意向',
    medium: '中意向',
    high: '高意向',
};

const defaultAction = {
    status: 'contacted',
    assignedTo: '',
    nextFollowupAt: '',
    remark: '',
};

type ActionForm = typeof defaultAction;

const formatDateTime = (value?: string | null) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN', { hour12: false });
};

const formatDateInputValue = (value?: string | null) => {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    const pad = (num: number) => String(num).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
};

const getDisplayName = (item: CustomerLeadItem) => {
    return item.name?.trim() || (item.user_id ? `用户${item.user_id}` : '匿名用户');
};

const getContactText = (item: CustomerLeadItem) => {
    const contacts = [item.wechat, item.enterprise_wechat_contact, item.phone].filter(Boolean);
    return contacts.length > 0 ? contacts.join(' / ') : '未留联系方式';
};

const getQueueCount = (overview: CustomerLeadOverview, key: string) => {
    return Number((overview as any)[key] || 0);
};

const buildActionPayload = (form: ActionForm): CustomerLeadActionPayload => ({
    status: form.status,
    remark: form.remark.trim(),
    assigned_to: form.assignedTo.trim(),
    next_followup_at: form.nextFollowupAt ? new Date(form.nextFollowupAt).toISOString() : undefined,
});

const describeEvent = (item: CustomerServiceEventItem) => {
    const payload = item.payload || {};
    const styleReference = payload.style_reference as Record<string, any> | undefined;
    const summary = item.demand_summary || '';
    switch (item.event_type) {
        case 'welcome_shown':
            return '进入智能客服页';
        case 'structured_requirement_collected':
            return summary ? `提交了结构化建房需求：${summary}` : '提交了结构化建房需求';
        case 'generate_click':
            return '点击了一键生成方案';
        case 'human_handoff_triggered':
            return '触发人工承接';
        case 'style_reference_selected':
            return `选择了风格参考图${styleReference?.title ? `：${styleReference.title}` : ''}`;
        case 'knowledge_base_hit':
            return '命中知识库回答';
        case 'ai_chat_fallback':
            return '进入 AI 兜底回答';
        case 'lead_submit':
            return '系统生成或更新了客服线索';
        default:
            return item.event_type || '未知事件';
    }
};

const getNextActionText = (lead: CustomerLeadItem | null) => {
    if (!lead) return '先查看线索详情';
    if (lead.status === 'converted') return '已成交，补充成交备注即可';
    if (lead.status === 'invalid') return '已标记无效，无需继续跟进';
    if (lead.intent_level === 'high') return '建议优先电话或微信联系';
    if (!lead.assigned_to) return '建议先分配负责人';
    return '按下次跟进时间继续推进';
};

// embedded：嵌在客户管理页的一个栏目里时，队列用页内卡片切换、不读写地址；
// 单独打开（/customer-leads）时队列由地址 ?queue= 决定，切换入口在顶部子导航条，?user_id= 可只看某个用户的线索。
const CustomerLeads: React.FC<{ readOnly?:boolean; embedded?:boolean }> = ({ readOnly=false, embedded=false }) => {
    const session = useManagementSession();
    // /me.crmEnabled is the global takeover switch, not this account's CRM access.
    // Missing/failed session checks must never reopen the legacy write endpoints.
    const sessionReady = !session.loading && !session.error && !!session.user;
    const canWrite = !readOnly && !session.crmLoading && sessionReady && session.user?.scope === 'platform_admin' && session.user.crmEnabled === false;
    const canOpenCustomers = sessionReady && !session.crmLoading && !!session.crmContext;
    const writeAllowed = useRef(canWrite);
    writeAllowed.current = canWrite;
    const writeBlockedText = !sessionReady || session.user?.crmEnabled === undefined
        ? '会话或客户接入状态尚未确认，暂不能修改线索。请重新检查会话。'
        : session.user.crmEnabled
            ? '小程序线索已由统一客户档案接管，旧线索仅供查看；跟进与归属请在客户档案中处理。'
            : '当前页面仅可查看线索，暂不能修改跟进与归属。';
    const [params, setParams] = useSearchParams();
    const [overview, setOverview] = useState<CustomerLeadOverview>(defaultOverview);
    const [updatedAt, setUpdatedAt] = useState<Date | null>(null);
    const [list, setList] = useState<CustomerLeadItem[]>([]);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const [pageSize] = useState(20);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [localQueue, setLocalQueue] = useState(DEFAULT_LEAD_QUEUE);
    const urlQueue = params.get('queue') || '';
    const queue = embedded ? localQueue : (leadQueues.some((item) => item.key === urlQueue) ? urlQueue : DEFAULT_LEAD_QUEUE);
    const userIdFilter = embedded ? 0 : Number(params.get('user_id')) || 0;
    const [statusFilter, setStatusFilter] = useState('all');
    const [intentFilter, setIntentFilter] = useState('all');
    const [assignedFilter, setAssignedFilter] = useState('all');
    const [selectedIds, setSelectedIds] = useState<number[]>([]);
    const [detail, setDetail] = useState<CustomerLeadDetailResponse | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [actionTarget, setActionTarget] = useState<CustomerLeadItem | null>(null);
    const [batchMode, setBatchMode] = useState(false);
    const [actionForm, setActionForm] = useState<ActionForm>(defaultAction);
    const [savingAction, setSavingAction] = useState(false);

    useEffect(() => {
        setActionTarget(null);
        setBatchMode(false);
        setActionForm(defaultAction);
        setSelectedIds([]);
    }, [canWrite, session.user?.id]);

    const selectedItems = useMemo(() => list.filter((item) => selectedIds.includes(item.id)), [list, selectedIds]);
    const activeLead = detail?.lead || null;
    const activeEvents = detail?.events || [];
    const activeFollowups = detail?.followups || [];
    const activeIntentAnalysis = detail?.intent_analysis || null;
    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    const loadData = async () => {
        setLoading(true);
        try {
            const params = {
                page,
                page_size: pageSize,
                keyword,
                queue: queue === 'all' ? undefined : queue,
                user_id: userIdFilter || undefined,
                status: statusFilter === 'all' ? undefined : statusFilter,
                intent_level: intentFilter === 'all' ? undefined : intentFilter,
                assigned_to: assignedFilter === 'all' ? undefined : assignedFilter,
            };
            const [overviewData, listData] = await Promise.all([
                getCustomerLeadOverview(),
                getCustomerLeadList(params),
            ]);
            const nextOverview = { ...defaultOverview, ...overviewData };
            setOverview(nextOverview);
            setUpdatedAt(new Date());
            publishLeadOverview(nextOverview as unknown as Record<string, number>);
            setList(listData.list || []);
            setTotal(listData.total || 0);
            setSelectedIds((ids) => ids.filter((id) => (listData.list || []).some((item) => item.id === id)));
        } catch (error) {
            reportPageLoadError(error, error instanceof Error ? error.message : '加载客服线索失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        loadData();
    }, [page, pageSize, keyword, queue, userIdFilter, statusFilter, intentFilter, assignedFilter]);

    // 切换队列 / 用户过滤时回到第一页并清空勾选
    useEffect(() => {
        setPage(1);
        setSelectedIds([]);
    }, [queue, userIdFilter]);

    const resetToFirstPage = () => {
        if (page === 1) {
            loadData();
        } else {
            setPage(1);
        }
    };

    const handleSearch = () => {
        setKeyword(searchInput.trim());
        setPage(1);
    };

    const handleQueueChange = (nextQueue: string) => {
        setLocalQueue(nextQueue);
        setPage(1);
        setSelectedIds([]);
    };

    const handleViewDetail = async (item: CustomerLeadItem) => {
        setDetailLoading(true);
        try {
            const data = await getCustomerLeadDetail(String(item.id));
            setDetail(data);
        } catch (error) {
            reportPageLoadError(error, error instanceof Error ? error.message : '加载客服线索详情失败');
        } finally {
            setDetailLoading(false);
        }
    };

    const handleCloseDetail = () => {
        setDetail(null);
    };

    const openAction = (item?: CustomerLeadItem) => {
        if (!writeAllowed.current) return;
        const lead = item || activeLead || null;
        setActionTarget(lead);
        setBatchMode(!lead);
        setActionForm({
            status: lead?.status === 'converted' || lead?.status === 'invalid' ? lead.status : 'contacted',
            assignedTo: lead?.assigned_to || '',
            nextFollowupAt: formatDateInputValue(lead?.next_followup_at || null),
            remark: '',
        });
    };

    const closeAction = () => {
        setActionTarget(null);
        setBatchMode(false);
        setActionForm(defaultAction);
    };

    const saveAction = async () => {
        if (!writeAllowed.current || savingAction) return;
        if (!actionForm.remark.trim()) {
            window.alert('请填写本次跟进记录，方便后续同事接手');
            return;
        }
        const payload = buildActionPayload(actionForm);
        setSavingAction(true);
        try {
            if (batchMode) {
                if (selectedIds.length === 0) {
                    window.alert('请先选择要批量处理的线索');
                    return;
                }
                await batchUpdateCustomerLeads(selectedIds, payload);
                setSelectedIds([]);
            } else if (actionTarget) {
                await updateCustomerLeadStatus(String(actionTarget.id), payload);
                if (activeLead?.id === actionTarget.id) {
                    const latest = await getCustomerLeadDetail(String(actionTarget.id));
                    setDetail(latest);
                }
            } else if (activeLead) {
                await createCustomerLeadFollowup(String(activeLead.id), payload);
                const latest = await getCustomerLeadDetail(String(activeLead.id));
                setDetail(latest);
            }
            closeAction();
            await loadData();
        } catch (error) {
            window.alert(error instanceof Error ? error.message : '保存客服线索跟进失败');
        } finally {
            setSavingAction(false);
        }
    };

    const toggleSelected = (id: number) => {
        if (!writeAllowed.current) return;
        setSelectedIds((ids) => ids.includes(id) ? ids.filter((item) => item !== id) : [...ids, id]);
    };

    const toggleSelectPage = () => {
        if (!writeAllowed.current) return;
        if (selectedIds.length === list.length) {
            setSelectedIds([]);
            return;
        }
        setSelectedIds(list.map((item) => item.id));
    };

    return (
        <>
            <div className="customer-lead-page">
                {!canWrite && <section className="customer-lead-panel" role="status">
                    <p>{writeBlockedText}</p>
                    {canOpenCustomers ? <Link className="customer-lead-btn" to="/customer-management?tab=customers">前往客户档案列表</Link> : <>
                        {session.user?.crmEnabled && <p>{session.crmError || '当前账号的客户访问尚未就绪，请重新检查；仍不可用时联系系统管理员。'}</p>}
                        <button type="button" className="customer-lead-btn secondary" disabled={session.loading || session.crmLoading} onClick={session.refresh}>重新检查会话与客户权限</button>
                    </>}
                </section>}
                {embedded ? (
                <>
                <div className="customer-lead-hero">
                    <div>
                        <p>管理后台 / 客服线索工作台</p>
                        <h2>把智能客服里的意向用户推进到可跟进、可分配、可复盘</h2>
                    </div>
                    <button type="button" className="customer-lead-btn secondary" onClick={resetToFirstPage} disabled={loading}>
                        <FiRefreshCwIcon />
                        刷新
                    </button>
                </div>

                <div className="customer-lead-stats">
                    <div className="customer-lead-stat">
                        <FiClockIcon />
                        <span>待处理</span>
                        <strong>{overview.todo_count || 0}</strong>
                    </div>
                    <div className="customer-lead-stat">
                        <FiFileTextIcon />
                        <span>今日新增</span>
                        <strong>{overview.today_new_count || 0}</strong>
                    </div>
                    <div className="customer-lead-stat">
                        <FiUserCheckIcon />
                        <span>高意向</span>
                        <strong>{overview.high_intent_count}</strong>
                    </div>
                    <div className="customer-lead-stat danger">
                        <FiActivityIcon />
                        <span>超时未跟进</span>
                        <strong>{overview.overdue_count || 0}</strong>
                    </div>
                </div>
                </>
                ) : (
                    <CustomerLeadHub overview={overview} loading={loading} updatedAt={updatedAt} onRefresh={resetToFirstPage} />
                )}

                {userIdFilter > 0 && (
                    <div className="customer-lead-userfilter" role="status">
                        <span>仅显示用户 ID <strong>{userIdFilter}</strong> 的线索</span>
                        <Link to={`/user-workbench?userId=${userIdFilter}`}>查看用户账号</Link>
                        <button type="button" onClick={() => { const next = new URLSearchParams(params); next.delete('user_id'); setParams(next); }}>
                            <FiX />清除
                        </button>
                    </div>
                )}

                {embedded && (
                <section className="customer-lead-panel">
                    <div className="customer-lead-queues">
                        {leadQueues.map((item) => (
                            <button
                                key={item.key}
                                type="button"
                                className={`customer-lead-queue ${queue === item.key ? 'active' : ''}`}
                                onClick={() => handleQueueChange(item.key)}
                            >
                                <span>{item.label}</span>
                                <strong>{getQueueCount(overview, item.countKey)}</strong>
                                <small>{item.help}</small>
                            </button>
                        ))}
                    </div>
                </section>
                )}

                {!embedded && (
                    <nav className="customer-lead-queuebar" aria-label="线索队列">
                        {leadQueues.map((item) => {
                            const next = new URLSearchParams(params);
                            next.set('queue', item.key);
                            const count = getQueueCount(overview, item.countKey);
                            return (
                                <Link
                                    key={item.key}
                                    to={`/customer-leads?${next.toString()}`}
                                    className={`customer-lead-queuepill ${queue === item.key ? 'active' : ''}`}
                                    aria-current={queue === item.key ? 'page' : undefined}
                                    title={item.help}
                                >
                                    {item.label}
                                    <b className={item.alert && count > 0 ? 'is-alert' : ''}>{count}</b>
                                </Link>
                            );
                        })}
                    </nav>
                )}

                <section className="customer-lead-panel">
                    <div className="customer-lead-toolbar">
                        <div className="customer-lead-search">
                            <FiSearchIcon />
                            <input
                                value={searchInput}
                                onChange={(event) => setSearchInput(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === 'Enter') handleSearch();
                                }}
                                placeholder="搜索用户、需求、微信、电话"
                            />
                        </div>
                        <select value={statusFilter} onChange={(event) => { setStatusFilter(event.target.value); setPage(1); }}>
                            <option value="all">全部状态</option>
                            <option value="new">新线索</option>
                            <option value="contacted">已联系</option>
                            <option value="converted">已成交</option>
                            <option value="invalid">无效</option>
                        </select>
                        <select value={intentFilter} onChange={(event) => { setIntentFilter(event.target.value); setPage(1); }}>
                            <option value="all">全部意向</option>
                            <option value="high">高意向</option>
                            <option value="medium">中意向</option>
                            <option value="low">低意向</option>
                        </select>
                        <select value={assignedFilter} onChange={(event) => { setAssignedFilter(event.target.value); setPage(1); }}>
                            <option value="all">全部负责人</option>
                            <option value="unassigned">未分配</option>
                        </select>
                        <button type="button" className="customer-lead-btn" onClick={handleSearch}>
                            <FiSearchIcon />
                            查询
                        </button>
                    </div>

                    {selectedIds.length > 0 ? (
                        <div className="customer-lead-batchbar">
                            <span>已选择 {selectedIds.length} 条线索</span>
                            <button type="button" className="customer-lead-btn" disabled={!canWrite} title={!canWrite ? writeBlockedText : undefined} onClick={() => openAction()}>
                                批量处理
                            </button>
                            <button type="button" className="customer-lead-btn secondary" onClick={() => setSelectedIds([])}>
                                取消选择
                            </button>
                        </div>
                    ) : null}

                    <div className="customer-lead-table-wrap">
                        <table className="customer-lead-table">
                            <thead>
                                <tr>
                                    <th className="checkbox-cell">
                                        <input type="checkbox" disabled={!canWrite} checked={list.length > 0 && selectedIds.length === list.length} onChange={toggleSelectPage} />
                                    </th>
                                    <th>用户</th>
                                    <th>需求与联系方式</th>
                                    <th>来源</th>
                                    <th>负责人 / 下次跟进</th>
                                    <th>意向</th>
                                    <th>状态</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {list.map((item) => (
                                    <tr key={item.id}>
                                        <td className="checkbox-cell">
                                            <input type="checkbox" disabled={!canWrite} checked={selectedIds.includes(item.id)} onChange={() => toggleSelected(item.id)} />
                                        </td>
                                        <td>
                                            <strong>{getDisplayName(item)}</strong>
                                            {item.user_id > 0
                                                ? <Link className="customer-lead-userlink" to={`/user-workbench?userId=${item.user_id}`}>查看用户账号 ›</Link>
                                                : <span>未关联用户账号</span>}
                                            <span>{formatDateTime(item.created_at)}</span>
                                        </td>
                                        <td className="customer-lead-demand">
                                            <strong>{item.demand_summary || '暂无需求摘要'}</strong>
                                            <span>{getContactText(item)}</span>
                                        </td>
                                        <td>
                                            <strong>{item.source || '-'}</strong>
                                            <span>{item.source_task_no || item.session_no || '-'}</span>
                                        </td>
                                        <td>
                                            <strong>{item.assigned_to || '未分配'}</strong>
                                            <span>{item.next_followup_at ? formatDateTime(item.next_followup_at) : '未设置下次跟进'}</span>
                                        </td>
                                        <td>
                                            <span className={`intent-tag intent-${item.intent_level}`}>
                                                {intentLabelMap[item.intent_level] || item.intent_level || '-'}
                                            </span>
                                        </td>
                                        <td>
                                            <span className={`status-tag status-${item.status}`}>
                                                {statusLabelMap[item.status] || item.status || '-'}
                                            </span>
                                        </td>
                                        <td>
                                            <div className="customer-lead-actions">
                                                <button type="button" className="customer-lead-link" onClick={() => void handleViewDetail(item)}>
                                                    详情
                                                </button>
                                                <button type="button" className="customer-lead-link" disabled={!canWrite} title={!canWrite ? writeBlockedText : undefined} onClick={() => openAction(item)}>
                                                    跟进
                                                </button>
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                                {!loading && list.length === 0 ? (
                                    <tr>
                                        <td colSpan={8} className="customer-lead-empty">暂无客服线索</td>
                                    </tr>
                                ) : null}
                            </tbody>
                        </table>
                    </div>

                    <div className="customer-lead-pagination">
                        <span>共 {total} 条，第 {page} / {totalPages} 页</span>
                        <div>
                            <button type="button" className="customer-lead-btn secondary" disabled={page <= 1 || loading} onClick={() => setPage((value) => Math.max(1, value - 1))}>上一页</button>
                            <button type="button" className="customer-lead-btn secondary" disabled={page >= totalPages || loading} onClick={() => setPage((value) => Math.min(totalPages, value + 1))}>下一页</button>
                        </div>
                    </div>
                </section>

                {(activeLead || detailLoading) ? (
                    <div className="customer-lead-modal-overlay" onClick={handleCloseDetail}>
                        <div className="customer-lead-modal" onClick={(event) => event.stopPropagation()}>
                            <div className="customer-lead-modal-header">
                                <div>
                                    <h3>线索详情</h3>
                                    <p>{activeLead ? `${getDisplayName(activeLead)} · ${getNextActionText(activeLead)}` : '正在加载线索详情'}</p>
                                </div>
                                <button type="button" className="customer-lead-modal-close" onClick={handleCloseDetail}>
                                    <FiXIcon />
                                </button>
                            </div>
                            {detailLoading && !activeLead ? (
                                <div className="customer-lead-modal-empty">正在加载详情...</div>
                            ) : activeLead ? (
                                <div className="customer-lead-modal-body">
                                    <section className="customer-lead-detail-section priority">
                                        <h4>下一步建议</h4>
                                        <div className="customer-lead-next-action">
                                            <strong>{getNextActionText(activeLead)}</strong>
                                            <span>{detail?.summary || '暂无客户摘要。'}</span>
                                            <button type="button" className="customer-lead-btn" disabled={!canWrite} title={!canWrite ? writeBlockedText : undefined} onClick={() => openAction(activeLead)}>记录跟进</button>
                                            {!canWrite && canOpenCustomers && <Link to="/customer-management?tab=customers">前往客户档案列表处理</Link>}
                                        </div>
                                    </section>

                                    <section className="customer-lead-detail-section">
                                        <h4>客户信息</h4>
                                        <div className="customer-lead-detail-grid">
                                            <div><span>用户</span><strong>{getDisplayName(activeLead)}</strong></div>
                                            <div><span>用户账号</span><strong>{activeLead.user_id > 0 ? <Link className="customer-lead-userlink" to={`/user-workbench?userId=${activeLead.user_id}`}>ID {activeLead.user_id} ›</Link> : '未关联'}</strong></div>
                                            <div><span>联系方式</span><strong>{getContactText(activeLead)}</strong></div>
                                            <div><span>负责人</span><strong>{activeLead.assigned_to || '未分配'}</strong></div>
                                            <div><span>下次跟进</span><strong>{formatDateTime(activeLead.next_followup_at)}</strong></div>
                                            <div><span>意向等级</span><strong>{intentLabelMap[activeLead.intent_level] || activeLead.intent_level || '-'}</strong></div>
                                        </div>
                                    </section>

                                    <section className="customer-lead-detail-section">
                                        <h4>建房需求</h4>
                                        <div className="customer-lead-detail-grid">
                                            <div className="wide"><span>需求摘要</span><strong>{activeLead.demand_summary || '暂无'}</strong></div>
                                            <div><span>层数</span><strong>{activeLead.house_floors || '-'}</strong></div>
                                            <div><span>风格</span><strong>{activeLead.house_style || '-'}</strong></div>
                                            <div><span>面宽</span><strong>{activeLead.land_width || '-'}</strong></div>
                                            <div><span>进深</span><strong>{activeLead.land_depth || '-'}</strong></div>
                                            <div><span>房间需求</span><strong>{activeLead.room_requirement || '-'}</strong></div>
                                        </div>
                                    </section>

                                    <section className="customer-lead-detail-section">
                                        <h4>来源追踪</h4>
                                        <div className="customer-lead-detail-grid">
                                            <div><span>来源</span><strong>{activeLead.source || '-'}</strong></div>
                                            <div><span>任务号</span><strong>{activeLead.source_task_no || '-'}</strong></div>
                                            <div><span>会话号</span><strong>{activeLead.session_no || '-'}</strong></div>
                                            <div><span>更新时间</span><strong>{formatDateTime(activeLead.updated_at)}</strong></div>
                                        </div>
                                    </section>

                                    <section className="customer-lead-detail-section">
                                        <h4>自动判断</h4>
                                        <div className="customer-lead-summary">
                                            {activeIntentAnalysis ? (
                                                <div>
                                                    <span className={`intent-tag intent-${activeIntentAnalysis.level}`}>
                                                        {intentLabelMap[activeIntentAnalysis.level] || activeIntentAnalysis.level}
                                                    </span>
                                                    <small>评分 {activeIntentAnalysis.score}：{activeIntentAnalysis.reasons.join('；')}</small>
                                                </div>
                                            ) : <p>暂无自动判断。</p>}
                                        </div>
                                    </section>

                                    <section className="customer-lead-detail-section">
                                        <h4>跟进记录</h4>
                                        <div className="customer-lead-followup-list">
                                            {activeFollowups.length > 0 ? activeFollowups.map((item: CustomerLeadFollowupItem) => (
                                                <div className="customer-lead-followup-item" key={item.id}>
                                                    <div>
                                                        <strong>{statusLabelMap[item.status] || item.status}</strong>
                                                        <span>{formatDateTime(item.created_at)}</span>
                                                    </div>
                                                    <p>{item.remark}</p>
                                                    <small>{item.operator_name || '系统记录'}{item.assigned_to ? ` · 分配给 ${item.assigned_to}` : ''}{item.next_followup_at ? ` · 下次 ${formatDateTime(item.next_followup_at)}` : ''}</small>
                                                </div>
                                            )) : (
                                                <div className="customer-lead-modal-empty">暂无跟进记录</div>
                                            )}
                                        </div>
                                    </section>

                                    <section className="customer-lead-detail-section">
                                        <h4>客户行为时间线</h4>
                                        <div className="customer-lead-timeline">
                                            {activeEvents.length > 0 ? activeEvents.map((event) => (
                                                <div className="customer-lead-timeline-item" key={event.id}>
                                                    <div className="customer-lead-timeline-dot" />
                                                    <div>
                                                        <strong>{describeEvent(event)}</strong>
                                                        <span>{formatDateTime(event.created_at)}</span>
                                                        {event.demand_summary ? <p>{event.demand_summary}</p> : null}
                                                    </div>
                                                </div>
                                            )) : (
                                                <div className="customer-lead-modal-empty">暂无事件记录</div>
                                            )}
                                        </div>
                                    </section>
                                </div>
                            ) : null}
                        </div>
                    </div>
                ) : null}

                {canWrite && (actionTarget || batchMode) ? (
                    <div className="customer-lead-modal-overlay" onClick={closeAction}>
                        <div className="customer-lead-action-modal" onClick={(event) => event.stopPropagation()}>
                            <div className="customer-lead-modal-header">
                                <div>
                                    <h3>{batchMode ? `批量处理 ${selectedIds.length} 条线索` : `跟进 ${actionTarget ? getDisplayName(actionTarget) : ''}`}</h3>
                                    <p>填写记录后，其他同事能直接接上，不用重新翻聊天上下文。</p>
                                </div>
                                <button type="button" className="customer-lead-modal-close" onClick={closeAction}>
                                    <FiXIcon />
                                </button>
                            </div>
                            <div className="customer-lead-action-form">
                                {batchMode && selectedItems.length > 0 ? (
                                    <div className="customer-lead-selected-list">
                                        {selectedItems.slice(0, 5).map((item) => <span key={item.id}>{getDisplayName(item)}</span>)}
                                        {selectedItems.length > 5 ? <span>+{selectedItems.length - 5}</span> : null}
                                    </div>
                                ) : null}
                                <label>
                                    状态
                                    <select value={actionForm.status} onChange={(event) => setActionForm((form) => ({ ...form, status: event.target.value }))}>
                                        <option value="new">新线索</option>
                                        <option value="contacted">已联系</option>
                                        <option value="converted">已成交</option>
                                        <option value="invalid">无效</option>
                                    </select>
                                </label>
                                <label>
                                    负责人
                                    <input value={actionForm.assignedTo} onChange={(event) => setActionForm((form) => ({ ...form, assignedTo: event.target.value }))} placeholder="例如：张三 / 设计顾问A" />
                                </label>
                                <label>
                                    下次跟进时间
                                    <input type="datetime-local" value={actionForm.nextFollowupAt} onChange={(event) => setActionForm((form) => ({ ...form, nextFollowupAt: event.target.value }))} />
                                </label>
                                <label className="wide">
                                    本次跟进记录
                                    <textarea value={actionForm.remark} onChange={(event) => setActionForm((form) => ({ ...form, remark: event.target.value }))} rows={5} placeholder="例如：已电话沟通，用户关心三层新中式造价，约明天下午发初步预算。" />
                                </label>
                                <div className="customer-lead-action-footer">
                                    <button type="button" className="customer-lead-btn secondary" onClick={closeAction}>取消</button>
                                    <button type="button" className="customer-lead-btn" disabled={savingAction} onClick={() => void saveAction()}>
                                        保存跟进
                                    </button>
                                </div>
                            </div>
                        </div>
                    </div>
                ) : null}
            </div>
        </>
    );
};

export default CustomerLeads;
