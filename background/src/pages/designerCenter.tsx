import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
    FiCheckCircle,
    FiChevronRight,
    FiEye,
    FiImage,
    FiRefreshCw,
    FiSearch,
    FiShield,
    FiStar,
    FiUser,
    FiUsers,
    FiXCircle,
    FiZap,
} from 'react-icons/fi';
import { useNavigate } from 'react-router-dom';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getDesignerManagementDetail,
    getDesignerManagementList,
    updateDesignerServiceStatus,
    updateDesignerVisibility,
    type DesignerManagementDetail,
    type DesignerManagementItem,
    type DesignerManagementListSummary,
    type DesignerManagementReviewItem,
    type DesignerManagementWorkItem,
} from '../api/designers';
import './designerCenter.scss';

    const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
    const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
    const FiUsersIcon = FiUsers as unknown as React.ComponentType<any>;
    const FiEyeIcon = FiEye as unknown as React.ComponentType<any>;
    const FiCheckCircleIcon = FiCheckCircle as unknown as React.ComponentType<any>;
    const FiImageIcon = FiImage as unknown as React.ComponentType<any>;
    const FiShieldIcon = FiShield as unknown as React.ComponentType<any>;
    const FiStarIcon = FiStar as unknown as React.ComponentType<any>;
    const FiUserIcon = FiUser as unknown as React.ComponentType<any>;
    const FiChevronRightIcon = FiChevronRight as unknown as React.ComponentType<any>;
    const FiXCircleIcon = FiXCircle as unknown as React.ComponentType<any>;
    const FiZapIcon = FiZap as unknown as React.ComponentType<any>;

const CERT_STATUS_MAP: Record<string, string> = {
    pending_payment: '待支付',
    pending_review: '待审核',
    approved: '已通过',
    rejected: '已拒绝',
};

const CERT_TYPE_MAP: Record<string, string> = {
    designer: '个人设计师',
    enterprise: '企业认证',
};

const WORK_STATUS_MAP: Record<string, string> = {
    pending: '审核中',
    published: '已上线',
    rejected: '已拒绝',
    draft: '草稿',
    archived: '已下线',
};

const REVIEW_SENTIMENT_MAP: Record<string, string> = {
    positive: '好评',
    negative: '差评',
};

const PAGE_SIZE = 20;

type DesignerListScope = {
    page: number;
    keyword: string;
    certification_status: string;
    visible: string;
    service_state: 'all' | 'enabled' | 'paused';
};

const formatDateTime = (value?: string | null) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const getDesignerDisplayName = (item?: DesignerManagementItem | DesignerManagementDetail | null) => {
    if (!item) return '未命名设计师';
    const profileName = 'profile' in item ? item.profile?.nickname : item.display_name;
    return profileName || item.username || `用户${item.user_id}`;
};

const DesignerCenter: React.FC = () => {
    const navigate = useNavigate();
    const initialKeyword = new URLSearchParams(window.location.search).get('keyword') || '';
    const [loading, setLoading] = useState(true);
    const [listError, setListError] = useState('');
    const [page, setPage] = useState(1);
    const [total, setTotal] = useState<number | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [actionLoadingId, setActionLoadingId] = useState<number | null>(null);
    const [items, setItems] = useState<DesignerManagementItem[]>([]);
    const [summary, setSummary] = useState<Partial<DesignerManagementListSummary> | null>(null);
    const [selectedId, setSelectedId] = useState<number | null>(null);
    const [detail, setDetail] = useState<DesignerManagementDetail | null>(null);
    const [detailError, setDetailError] = useState('');
    const [actionError, setActionError] = useState('');
    const mounted = useRef(false);
    const listRequestId = useRef(0);
    const listReady = useRef(false);
    const listScope = useRef<DesignerListScope>({
        page: 1, keyword: initialKeyword, certification_status: 'all', visible: 'all', service_state: 'all',
    });
    const currentDetail = useRef<DesignerManagementDetail | null>(null);
    const selectedIdRef = useRef<number | null>(null);
    const detailRequestId = useRef(0);
    const actionInFlight = useRef(false);
    const [keywordInput, setKeywordInput] = useState(initialKeyword);
    const [keyword, setKeyword] = useState(initialKeyword);
    const [certificationStatus, setCertificationStatus] = useState('all');
    const [visible, setVisible] = useState('all');
    const [serviceState, setServiceState] = useState<'all' | 'enabled' | 'paused'>('all');

    // Handlers from a previous list render must not operate on replacement results.
    const renderedListRequestId = listRequestId.current;

    const selectDesigner = (id: number | null) => {
        if (selectedIdRef.current === id) return;
        selectedIdRef.current = id;
        detailRequestId.current += 1;
        setSelectedId(id);
        currentDetail.current = null;
        setDetail(null);
        setDetailError('');
        setActionError('');
        setDetailLoading(id !== null);
    };

    const loadList = async () => {
        if (!mounted.current) return false;
        const scope = listScope.current;
        const requestId = ++listRequestId.current;
        const requestPage = window.location.pathname;
        const isCurrentRequest = () => mounted.current && requestId === listRequestId.current && scope === listScope.current;
        listReady.current = false;
        setLoading(true);
        setListError('');
        setItems([]);
        setSummary(null);
        setTotal(null);
        try {
            const response = await getDesignerManagementList({ page_size: PAGE_SIZE, ...scope });
            if (!isCurrentRequest()) return false;
            const nextTotal = Number.isFinite(response.total) && response.total >= 0 ? response.total : null;
            const lastPage = nextTotal === null ? scope.page : Math.max(1, Math.ceil(nextTotal / PAGE_SIZE));
            if (scope.page > lastPage) {
                changeListScope({ page: lastPage });
                return false;
            }
            const nextItems = response.list || [];
            setItems(nextItems);
            setTotal(nextTotal);
            setSummary(response.summary || null);
            listReady.current = true;
            reportPageLoadRecovered('加载设计师列表失败', requestPage);
            const current = selectedIdRef.current;
            const selectedItem = nextItems.find((item) => item.user_id === current) || nextItems[0];
            const displayedDetail = currentDetail.current;
            const shouldReloadDetail = selectedItem?.user_id === current && (
                !displayedDetail
                || (typeof selectedItem.designer_visible === 'boolean' && selectedItem.designer_visible !== (displayedDetail.profile?.designer_visible === true))
                || (typeof selectedItem.service_enabled === 'boolean' && selectedItem.service_enabled !== (displayedDetail.profile?.service_enabled === true))
            );
            selectDesigner(selectedItem?.user_id ?? null);
            // A same-ID refresh must not re-enable writes from superseded status values.
            // Also replace any pending detail request that predates the refreshed list.
            if (shouldReloadDetail && selectedItem) void loadDetail(selectedItem.user_id);
            return true;
        } catch (error) {
            if (!isCurrentRequest()) return false;
            setItems([]);
            setSummary(null);
            setTotal(null);
            selectDesigner(null);
            setListError('设计师列表加载失败，请重试。');
            console.error('加载设计师列表失败:', error);
            reportPageLoadError(error, '加载设计师列表失败');
            return false;
        } finally {
            if (isCurrentRequest()) setLoading(false);
        }
    };

    const changeListScope = (patch: Partial<DesignerListScope>) => {
        if (!mounted.current) return false;
        const next = { ...listScope.current, ...patch, page: patch.page ?? 1 };
        if (Object.keys(next).every((key) => next[key as keyof DesignerListScope] === listScope.current[key as keyof DesignerListScope])) return false;
        // Invalidate synchronously, before React renders or the next effect starts.
        listScope.current = next;
        listRequestId.current += 1;
        listReady.current = false;
        setItems([]);
        setSummary(null);
        setTotal(null);
        setListError('');
        setLoading(true);
        selectDesigner(null);
        setPage(next.page);
        setKeyword(next.keyword);
        setCertificationStatus(next.certification_status);
        setVisible(next.visible);
        setServiceState(next.service_state);
        return true;
    };

    const loadDetail = async (id: number) => {
        if (!mounted.current || !listReady.current || selectedIdRef.current !== id) return;
        const requestId = ++detailRequestId.current;
        const isCurrentRequest = () => mounted.current && requestId === detailRequestId.current && selectedIdRef.current === id;
        currentDetail.current = null;
        setDetail(null);
        setDetailError('');
        setDetailLoading(true);
        try {
            const response = await getDesignerManagementDetail(id);
            if (!isCurrentRequest()) return;
            if (response.user_id !== id) throw new Error('设计师详情与所选用户不一致');
            currentDetail.current = response;
            setDetail(response);
        } catch (error) {
            if (!isCurrentRequest()) return;
            console.error('加载设计师详情失败:', error);
            setDetailError('设计师详情加载失败，请重试。');
        } finally {
            if (isCurrentRequest()) setDetailLoading(false);
        }
    };

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            listReady.current = false;
            listRequestId.current += 1;
            detailRequestId.current += 1;
            currentDetail.current = null;
        };
    }, []);

    useEffect(() => {
        void loadList();
    }, [page, keyword, certificationStatus, visible, serviceState]);

    useEffect(() => {
        if (!selectedId) {
            setDetail(null);
            return;
        }
        void loadDetail(selectedId);
        return () => { detailRequestId.current += 1; };
    }, [selectedId]);

    const summaryCards = useMemo(() => {
        return [
            {
                key: 'total',
                title: '设计师总数',
                value: summary?.total_designers ?? '—',
                icon: FiUsersIcon,
            },
            {
                key: 'public',
                title: '已公开主页',
                value: summary?.public_designers ?? '—',
                icon: FiEyeIcon,
            },
            {
                key: 'approved',
                title: '认证已通过',
                value: summary?.approved_designers ?? '—',
                icon: FiCheckCircleIcon,
            },
            {
                key: 'works',
                title: '已有作品设计师',
                value: summary?.designers_with_works ?? '—',
                icon: FiImageIcon,
            },
            {
                key: 'service',
                title: '接单中',
                value: summary?.service_enabled_designers ?? '—',
                icon: FiZapIcon,
            },
        ];
    }, [summary]);

    const handlePageChange = (nextPage: number) => {
        if (!mounted.current || !listReady.current || renderedListRequestId !== listRequestId.current || total === null) return;
        const boundedPage = Math.min(Math.max(1, nextPage), Math.max(1, Math.ceil(total / PAGE_SIZE)));
        changeListScope({ page: boundedPage });
    };

    const handleSearch = () => {
        if (!changeListScope({ keyword: keywordInput.trim() })) void loadList();
    };

    const handleReset = () => {
        if (!mounted.current) return;
        setKeywordInput('');
        if (!changeListScope({ keyword: '', certification_status: 'all', visible: 'all', service_state: 'all' })) void loadList();
    };

    const updateDesigner = async (id: number, update: () => Promise<void>, failureMessage: string) => {
        if (
            !mounted.current || !listReady.current || renderedListRequestId !== listRequestId.current
            || id !== selectedIdRef.current || detail?.user_id !== id || currentDetail.current !== detail
            || actionLoadingId !== null || actionInFlight.current
        ) return;
        const scope = listScope.current;
        const detailVersion = detailRequestId.current;
        const isCurrentTarget = () => mounted.current && scope === listScope.current && selectedIdRef.current === id && detailVersion === detailRequestId.current;
        actionInFlight.current = true;
        setActionError('');
        setActionLoadingId(id);
        try {
            await update();
            if (!mounted.current) return;
            // Read the latest filters, never those captured by the write's old render.
            const refreshScope = listScope.current;
            const refreshSelection = selectedIdRef.current;
            const refreshDetailVersion = detailRequestId.current;
            const refreshed = await loadList();
            if (
                refreshed && mounted.current && refreshScope === listScope.current
                && refreshSelection === id && selectedIdRef.current === id
                && refreshDetailVersion === detailRequestId.current
            ) await loadDetail(id);
        } catch (error) {
            if (!isCurrentTarget()) return;
            console.error(failureMessage, error);
            setActionError(`${failureMessage}，请重试。`);
        } finally {
            if (mounted.current) {
                actionInFlight.current = false;
                setActionLoadingId(null);
            }
        }
    };

    const handleToggleVisibility = (item: Pick<DesignerManagementItem, 'user_id' | 'designer_visible'>) => {
        const { user_id: id, designer_visible: visible } = item;
        return updateDesigner(id, () => updateDesignerVisibility(id, !visible), '更新设计师展示状态失败');
    };

    const handleToggleService = (item: Pick<DesignerManagementItem, 'user_id' | 'service_enabled'>) => {
        const { user_id: id, service_enabled: enabled } = item;
        return updateDesigner(id, () => updateDesignerServiceStatus(id, !enabled), '更新设计师接单状态失败');
    };

    const renderWorkCard = (item: DesignerManagementWorkItem) => {
        return (
            <div key={item.id} className="designer-work-card">
                {item.thumbnail ? <img src={item.thumbnail} alt={item.name} className="designer-work-thumb" /> : <div className="designer-work-placeholder">暂无封面</div>}
                <div className="designer-work-body">
                    <div className="designer-work-title">{item.name}</div>
                    <div className="designer-work-meta">{WORK_STATUS_MAP[item.status] || item.status} · {item.publish_scope === 'homepage_only' ? '仅主页' : '主页+模板广场'}</div>
                    <div className="designer-work-stats">点赞 {item.like_count} · 浏览 {item.download_count}</div>
                </div>
            </div>
        );
    };

    const renderReviewRow = (item: DesignerManagementReviewItem) => {
        return (
            <div key={item.id} className="designer-review-row">
                <div className="designer-review-head">
                    <strong>{item.reviewer_name || '匿名用户'}</strong>
                    <span>{REVIEW_SENTIMENT_MAP[item.sentiment] || item.sentiment} · {item.rating} 星</span>
                </div>
                <div className="designer-review-content">{item.content || '暂无评价内容'}</div>
                <div className="designer-review-time">订单号 {item.order_no || '-'} · {formatDateTime(item.created_at)}</div>
            </div>
        );
    };

    return (
        <>
            <div className="designer-center-page">
                <ManagementSearchPanel
                    title="设计师检索与运营处理"
                    description="先按认证状态、公开状态和关键词找到设计师，再查看详情、切换主页展示或处理接单状态。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadList()} disabled={loading}>
                                <FiRefreshCwIcon />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <div className="management-search-searchbox">
                                <FiSearchIcon className="management-search-searchicon" />
                                <input
                                    className="management-search-input"
                                    aria-label="设计师关键词"
                                    value={keywordInput}
                                    onChange={(e) => setKeywordInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                    placeholder="搜昵称、用户名、服务标题、擅长方向"
                                />
                            </div>
                            <select className="management-search-select" aria-label="认证状态" value={certificationStatus} onChange={(e) => changeListScope({ certification_status: e.target.value })}>
                                <option value="all">全部认证状态</option>
                                <option value="approved">已通过</option>
                                <option value="pending_review">待审核</option>
                                <option value="rejected">已拒绝</option>
                                <option value="pending_payment">待支付</option>
                                <option value="none">未申请</option>
                            </select>
                            <select className="management-search-select" aria-label="主页公开状态" value={visible} onChange={(e) => changeListScope({ visible: e.target.value })}>
                                <option value="all">全部公开状态</option>
                                <option value="public">已公开</option>
                                <option value="hidden">已隐藏</option>
                            </select>
                            <select
                                className="management-search-select"
                                value={serviceState}
                                aria-label="接单状态"
                                onChange={(e) => changeListScope({ service_state: e.target.value as 'all' | 'enabled' | 'paused' })}
                            >
                                <option value="all">全部接单状态</option>
                                <option value="enabled">接单中</option>
                                <option value="paused">暂停接单</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索设计师</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前页设计师 <strong>{loading ? '加载中' : listError ? '暂不可用' : items.length}</strong>{!loading && !listError ? ' 位' : ''}
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                                {certificationStatus !== 'all' ? <span className="management-search-tag">认证状态：{CERT_STATUS_MAP[certificationStatus] || certificationStatus}</span> : null}
                                {visible !== 'all' ? <span className="management-search-tag">主页状态：{visible === 'public' ? '已公开' : '已隐藏'}</span> : null}
                                {serviceState !== 'all' ? <span className="management-search-tag">接单状态：{serviceState === 'enabled' ? '接单中' : '暂停接单'}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="designer-summary-heading">全局设计师统计（不随筛选条件变化）</div>
                <div className="designer-summary-grid">
                    {summaryCards.map((card) => {
                        const Icon = card.icon;
                        return (
                            <div key={card.key} className="designer-summary-card section-card">
                                <div className="designer-summary-icon"><Icon /></div>
                                <div className="designer-summary-title">{card.title}</div>
                                <div className="designer-summary-value" aria-label={`${card.title}：${card.value === '—' ? '暂不可用' : card.value}`}>{card.value}</div>
                            </div>
                        );
                    })}
                </div>

                {summaryCards.some((card) => card.value === '—') && (
                    <div className="designer-summary-status" role="status">{loading ? '统计加载中...' : '“—”表示暂未获取统计数据。'}</div>
                )}

                <div className="designer-center-grid">
                    <div className="designer-list-panel section-card">
                        <div className="designer-list" aria-busy={loading}>
                            {loading ? (
                                <div className="designer-empty" role="status">设计师列表加载中...</div>
                            ) : listError ? (
                                <div className="designer-empty designer-load-error" role="alert">
                                    <p>{listError}</p>
                                    <button className="btn-secondary" onClick={() => void loadList()}>重试列表</button>
                                </div>
                            ) : items.length === 0 ? (
                                <div className="designer-empty">当前没有符合条件的设计师。</div>
                            ) : (
                                items.map((item) => (
                                    <button
                                        key={item.user_id}
                                        className={`designer-list-item ${selectedId === item.user_id ? 'active' : ''}`}
                                        onClick={() => {
                                            if (mounted.current && listReady.current && renderedListRequestId === listRequestId.current) selectDesigner(item.user_id);
                                        }}
                                    >
                                        <div className="designer-list-main">
                                            <div className="designer-avatar-wrap">
                                                {item.avatar ? <img src={item.avatar} alt={item.display_name || item.username} className="designer-avatar" /> : <div className="designer-avatar placeholder"><FiUserIcon /></div>}
                                            </div>
                                            <div className="designer-list-info">
                                                <div className="designer-list-title">{item.display_name || item.username}</div>
                                                <div className="designer-list-subtitle">{item.service_title || '暂未填写服务标题'}</div>
                                                <div className="designer-list-tags">
                                                    <span>{CERT_STATUS_MAP[item.certification_status || ''] || '未申请'}</span>
                                                    <span>{item.designer_visible ? '主页已公开' : '主页已隐藏'}</span>
                                                    <span>{item.service_enabled ? '接单已开启' : '暂停接单'}</span>
                                                    <span>作品 {item.total_works}</span>
                                                </div>
                                            </div>
                                        </div>
                                        <div className="designer-list-side">
                                            <div className="designer-list-side-top">关注 {item.follow_count} · 评价 {item.review_count}</div>
                                            <div className="designer-list-side-bottom">{formatDateTime(item.recent_active_at)}</div>
                                        </div>
                                    </button>
                                ))
                            )}
                        </div>
                        <div className="designer-pagination" aria-label="设计师列表分页">
                            <span>{loading ? '分页加载中...' : listError || total === null ? '总数暂不可用' : `共 ${total} 位 · 第 ${page} / ${Math.max(1, Math.ceil(total / PAGE_SIZE))} 页`}</span>
                            <div className="designer-pagination-actions">
                                <button className="btn-secondary" onClick={() => handlePageChange(page - 1)} disabled={loading || !!listError || total === null || page <= 1}>上一页</button>
                                <button className="btn-secondary" onClick={() => handlePageChange(page + 1)} disabled={loading || !!listError || total === null || page >= Math.max(1, Math.ceil(total / PAGE_SIZE))}>下一页</button>
                            </div>
                        </div>
                    </div>

                    <div className="designer-detail-panel section-card">
                        {!selectedId ? (
                            <div className="designer-empty">请先从左侧选择一个设计师。</div>
                        ) : detailLoading ? (
                            <div className="designer-empty">设计师详情加载中...</div>
                        ) : detailError ? (
                            <div className="designer-empty" role="alert">
                                <p>{detailError}</p>
                                <button className="btn-secondary" onClick={() => void loadDetail(selectedId)}>重新加载</button>
                            </div>
                        ) : !detail || detail.user_id !== selectedId ? (
                            <div className="designer-empty">未获取到当前设计师详情。</div>
                        ) : (
                            <>
                                <div className="designer-detail-head">
                                    <div className="designer-detail-profile">
                                        {detail.profile?.avatar ? <img src={detail.profile.avatar} alt={getDesignerDisplayName(detail)} className="designer-detail-avatar" /> : <div className="designer-detail-avatar placeholder"><FiUserIcon /></div>}
                                        <div>
                                            <h3>{getDesignerDisplayName(detail)}</h3>
                                            <p>{detail.profile?.service_title || '暂未填写服务标题'}</p>
                                            <div className="designer-detail-badges">
                                                <span>{CERT_TYPE_MAP[detail.certification?.type || ''] || '未区分类型'}</span>
                                                <span>{CERT_STATUS_MAP[detail.certification?.status || ''] || '未申请认证'}</span>
                                                <span>{detail.profile?.designer_visible ? '主页已公开' : '主页已隐藏'}</span>
                                            </div>
                                        </div>
                                    </div>
                                    <div className="designer-detail-actions">
                                        <button className="btn-secondary" onClick={() => navigate(`/user-workbench?userId=${detail.user_id}`)}>
                                            查看用户详情
                                            <FiChevronRightIcon />
                                        </button>
                                        <button
                                            className={detail.profile?.designer_visible ? 'btn-warning' : 'btn-primary'}
                                            onClick={() => void handleToggleVisibility({
                                                user_id: detail.user_id,
                                                designer_visible: detail.profile?.designer_visible === true,
                                            })}
                                            disabled={loading || actionLoadingId !== null}
                                        >
                                            {actionLoadingId === detail.user_id ? '提交中...' : detail.profile?.designer_visible ? '下架主页' : '上架主页'}
                                        </button>
                                        <button
                                            className={detail.profile?.service_enabled ? 'btn-warning' : 'btn-primary'}
                                            onClick={() => void handleToggleService({
                                                user_id: detail.user_id,
                                                service_enabled: detail.profile?.service_enabled === true,
                                            })}
                                            disabled={loading || actionLoadingId !== null}
                                        >
                                            {actionLoadingId === detail.user_id ? '提交中...' : detail.profile?.service_enabled ? '暂停接单' : '恢复接单'}
                                        </button>
                                    </div>
                                </div>

                                {actionError && <div className="designer-action-error" role="alert">{actionError}</div>}

                                <div className="designer-stat-grid">
                                    <div className="designer-stat-box"><FiImageIcon /><span>作品 {detail.stats.total_works}</span></div>
                                    <div className="designer-stat-box"><FiEyeIcon /><span>已上线 {detail.stats.published_works}</span></div>
                                    <div className="designer-stat-box"><FiUsersIcon /><span>关注 {detail.stats.follow_count}</span></div>
                                    <div className="designer-stat-box"><FiStarIcon /><span>评价 {detail.stats.review_count}</span></div>
                                    <div className="designer-stat-box"><FiShieldIcon /><span>订单 {detail.stats.total_orders}</span></div>
                                    <div className="designer-stat-box"><FiCheckCircleIcon /><span>收益 {detail.stats.total_earnings}</span></div>
                                </div>

                                <div className="designer-section-grid">
                                    <div className="designer-info-card">
                                        <h4>基础资料</h4>
                                        <div className="designer-info-list">
                                            <div><span>用户名</span><strong>{detail.username}</strong></div>
                                            <div><span>擅长方向</span><strong>{detail.profile?.specialty_styles || '暂未填写'}</strong></div>
                                            <div><span>设计经验</span><strong>{detail.profile?.designer_experience_years || 0} 年</strong></div>
                                            <div><span>服务报价</span><strong>{detail.profile?.service_quote || 0} 灵石</strong></div>
                                            <div><span>接单状态</span><strong>{detail.profile?.service_enabled ? '已开启' : '未开启'}</strong></div>
                                            <div><span>企微核验</span><strong>{detail.profile?.enterprise_wechat_verified ? '已核验' : '未核验'}</strong></div>
                                        </div>
                                        <div className="designer-long-text">{detail.profile?.designer_bio || '暂未填写设计师简介'}</div>
                                        <div className="designer-long-text subtle">{detail.profile?.service_intro || '暂未填写服务介绍'}</div>
                                    </div>

                                    <div className="designer-info-card">
                                        <h4>认证信息</h4>
                                        {detail.certification ? (
                                            <div className="designer-info-list">
                                                <div><span>认证类型</span><strong>{CERT_TYPE_MAP[detail.certification.type || ''] || detail.certification.type || '暂无'}</strong></div>
                                                <div><span>认证状态</span><strong>{CERT_STATUS_MAP[detail.certification.status || ''] || detail.certification.status || '暂无'}</strong></div>
                                                <div><span>认证身份</span><strong>{detail.certification.identity_type || '暂无'}</strong></div>
                                                <div><span>真实姓名/主体</span><strong>{detail.certification.real_name || detail.certification.company_name || '暂无'}</strong></div>
                                                <div><span>提交时间</span><strong>{formatDateTime(detail.certification.created_at)}</strong></div>
                                                <div><span>审核时间</span><strong>{formatDateTime(detail.certification.reviewed_at)}</strong></div>
                                            </div>
                                        ) : (
                                            <div className="designer-empty mini">当前还没有认证记录。</div>
                                        )}
                                    </div>
                                </div>

                                <div className="designer-content-section">
                                    <div className="designer-section-head">
                                        <h4>作品列表</h4>
                                        <span>共 {detail.works.length} 条</span>
                                    </div>
                                    {detail.works.length === 0 ? <div className="designer-empty mini">当前还没有发布作品。</div> : <div className="designer-work-grid">{detail.works.slice(0, 8).map(renderWorkCard)}</div>}
                                </div>

                                <div className="designer-content-section">
                                    <div className="designer-section-head">
                                        <h4>用户评价</h4>
                                        <span>好评 {detail.stats.positive_review_count} · 差评 {detail.stats.negative_review_count}</span>
                                    </div>
                                    {detail.reviews.length === 0 ? <div className="designer-empty mini">当前还没有评价。</div> : <div className="designer-review-list">{detail.reviews.slice(0, 6).map(renderReviewRow)}</div>}
                                </div>
                            </>
                        )}
                    </div>
                </div>
            </div>
        </>
    );
};

export default DesignerCenter;
