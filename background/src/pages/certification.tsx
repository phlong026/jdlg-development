import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiClock, FiEye, FiRefreshCw, FiSearch } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getCertificationList,
    getCertificationStats,
    getCertificationDetail,
    revealCertificationFields,
    reviewCertification,
    type CertificationApplication,
    type CertificationDetailResponse,
    type CertificationRevealFields,
    type CertificationStats,
} from '../api/certification';
import './certification.scss';

const STATUS_MAP: Record<string, string> = {
    pending_payment: '待支付',
    pending_review: '待审核',
    approved: '已通过',
    rejected: '已拒绝',
};
const TYPE_MAP: Record<string, string> = {
    designer: '个人设计师',
    enterprise: '企业认证',
};

type QueueKey = 'pending' | 'overdue' | 'paywait' | 'approved' | 'rejected' | 'all';

const OVERDUE_MS = 48 * 60 * 60 * 1000;

const waitDuration = (createdAt: string) => {
    const created = new Date(createdAt).getTime();
    if (Number.isNaN(created)) return { text: '-', level: 'none' as const };
    const hours = Math.max(0, (Date.now() - created) / 3600000);
    if (hours >= 48) {
        const days = Math.floor(hours / 24);
        return { text: `${days} 天 ${Math.floor(hours % 24)} 小时`, level: 'bad' as const };
    }
    if (hours >= 24) return { text: `${Math.floor(hours)} 小时`, level: 'mid' as const };
    return { text: `${Math.floor(hours)} 小时`, level: 'ok' as const };
};

const Certification: React.FC = () => {
    const navigate = useNavigate();
    const [stats, setStats] = useState<CertificationStats | null>(null);
    const [list, setList] = useState<CertificationApplication[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [keywordInput, setKeywordInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [queue, setQueue] = useState<QueueKey>('pending');
    const [page, setPage] = useState(1);
    const pageSize = 20;

    // 审核抽屉
    const [drawerApp, setDrawerApp] = useState<CertificationApplication | null>(null);
    const [detail, setDetail] = useState<CertificationDetailResponse | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState('');
    const [revealed, setRevealed] = useState<CertificationRevealFields | null>(null);
    const [revealing, setRevealing] = useState(false);
    const [revealError, setRevealError] = useState('');
    const [adminRemark, setAdminRemark] = useState('');
    const [reviewSubmitting, setReviewSubmitting] = useState<'approve' | 'reject' | null>(null);
    const [reviewError, setReviewError] = useState('');
    const mounted = useRef(true);
    const detailRequestId = useRef(0);
    const revealRequestId = useRef(0);
    const drawerTargetId = useRef<number | null>(null);
    const revealBusy = useRef(false);
    const reviewBusy = useRef(false);
    const renderedDetailRequestId = detailRequestId.current;

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            detailRequestId.current += 1;
            revealRequestId.current += 1;
            drawerTargetId.current = null;
        };
    }, []);

    const isCurrentCertification = () => Boolean(
        mounted.current && drawerApp && detail && !detailLoading && !detailError
        && renderedDetailRequestId === detailRequestId.current
        && drawerTargetId.current === drawerApp.id
        && detail.application?.id === drawerApp.id
        && (!detail.user || detail.user.id === detail.application.user_id)
        && (!revealed || revealed.id === drawerApp.id)
    );

    const queueParams = useCallback((q: QueueKey): { status?: string; overdue?: boolean } => {
        switch (q) {
            case 'pending': return { status: 'pending_review' };
            case 'overdue': return { status: 'pending_review', overdue: true };
            case 'paywait': return { status: 'pending_payment' };
            case 'approved': return { status: 'approved' };
            case 'rejected': return { status: 'rejected' };
            default: return {};
        }
    }, []);

    const loadList = useCallback(async () => {
        const requestPage = window.location.pathname;
        setLoading(true);
        try {
            const res = await getCertificationList({
                ...queueParams(queue),
                keyword: keyword || undefined,
                limit: pageSize,
                offset: (page - 1) * pageSize,
            });
            setList(res.list);
            setTotal(res.total);
            reportPageLoadRecovered('加载认证列表失败', requestPage);
        } catch (e) {
            console.error('加载列表失败:', e);
            reportPageLoadError(e, '加载认证列表失败');
        } finally {
            setLoading(false);
        }
    }, [queue, keyword, page, queueParams]);

    const loadStats = useCallback(async () => {
        try {
            setStats(await getCertificationStats());
        } catch (e) {
            console.error('加载认证统计失败:', e);
        }
    }, []);

    useEffect(() => {
        void loadList();
    }, [loadList]);

    useEffect(() => {
        void loadStats();
    }, [loadStats]);

    const refreshAll = () => {
        void loadList();
        void loadStats();
    };

    const handleSearch = () => {
        setPage(1);
        setKeyword(keywordInput.trim());
    };

    const switchQueue = (next: QueueKey) => {
        setQueue(next);
        setPage(1);
    };

    const openUserWorkbench = (userId: number) => {
        closeDrawer();
        navigate(`/user-workbench?userId=${userId}`);
    };

    const openDrawer = async (app: CertificationApplication) => {
        const requestPage = window.location.pathname;
        const requestId = ++detailRequestId.current;
        drawerTargetId.current = app.id;
        revealRequestId.current += 1;
        revealBusy.current = false;
        setDrawerApp(app);
        setDetail(null);
        setDetailError('');
        setRevealed(null);
        setRevealing(false);
        setRevealError('');
        setReviewError('');
        setAdminRemark('');
        setDetailLoading(true);
        try {
            const data = await getCertificationDetail(String(app.id));
            if (!mounted.current || requestId !== detailRequestId.current || drawerTargetId.current !== app.id) return;
            if (!data || data.application?.id !== app.id || (data.user && data.user.id !== data.application.user_id)) {
                throw new Error('返回的认证材料与当前申请不一致，请重新加载');
            }
            setDetail(data);
            setDrawerApp(data.application);
            reportPageLoadRecovered('加载认证详情失败', requestPage);
        } catch (e: any) {
            if (!mounted.current || requestId !== detailRequestId.current) return;
            console.error('加载详情失败:', e);
            setDetailError(e?.message || '加载认证详情失败，请重新加载');
        } finally {
            if (mounted.current && requestId === detailRequestId.current) setDetailLoading(false);
        }
    };

    const closeDrawer = () => {
        detailRequestId.current += 1;
        revealRequestId.current += 1;
        drawerTargetId.current = null;
        revealBusy.current = false;
        setDrawerApp(null);
        setDetail(null);
        setDetailLoading(false);
        setDetailError('');
        setRevealed(null);
        setRevealing(false);
        setRevealError('');
        setReviewError('');
        setAdminRemark('');
    };

    const handleReveal = async () => {
        const requestPage = window.location.pathname;
        if (!isCurrentCertification() || revealBusy.current) return;
        const targetId = drawerApp!.id;
        const detailGeneration = detailRequestId.current;
        const requestId = ++revealRequestId.current;
        revealBusy.current = true;
        setRevealError('');
        setRevealing(true);
        try {
            const data = await revealCertificationFields(String(targetId));
            if (!mounted.current || detailGeneration !== detailRequestId.current || requestId !== revealRequestId.current || drawerTargetId.current !== targetId) return;
            if (!data || data.id !== targetId) throw new Error('返回的完整材料与当前申请不一致，请重试');
            setRevealed(data);
            reportPageLoadRecovered('查看完整材料失败', requestPage);
        } catch (e: any) {
            if (!mounted.current || detailGeneration !== detailRequestId.current || requestId !== revealRequestId.current) return;
            console.error('查看完整材料失败:', e);
            setRevealError(e?.message || '查看完整材料失败，请重试');
        } finally {
            if (mounted.current && requestId === revealRequestId.current) {
                revealBusy.current = false;
                setRevealing(false);
            }
        }
    };

    const submitReview = async (action: 'approve' | 'reject') => {
        const requestPage = window.location.pathname;
        if (!isCurrentCertification() || drawerApp?.status !== 'pending_review' || reviewBusy.current) return;
        if (action === 'reject' && !adminRemark.trim()) {
            return; // 拒绝必须填原因（按钮已禁用，双保险）
        }
        const targetId = drawerApp.id;
        const requestId = detailRequestId.current;
        reviewBusy.current = true;
        setReviewError('');
        setReviewSubmitting(action);
        try {
            await reviewCertification(String(targetId), action, adminRemark.trim() || undefined);
            if (!mounted.current) return;
            reportPageLoadRecovered('审核操作失败', requestPage);
            if (requestId === detailRequestId.current && drawerTargetId.current === targetId) closeDrawer();
            refreshAll();
        } catch (e: any) {
            if (!mounted.current || requestId !== detailRequestId.current || drawerTargetId.current !== targetId) return;
            console.error('审核失败:', e);
            setReviewError(e?.message || '审核操作失败，请重试');
        } finally {
            reviewBusy.current = false;
            if (mounted.current) setReviewSubmitting(null);
        }
    };

    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    const queueTabs: { key: QueueKey; label: string; count?: number; alert?: boolean; hint?: string }[] = [
        { key: 'pending', label: '待审核', count: stats?.pending_review, alert: (stats?.pending_review ?? 0) > 0, hint: '需要人工处理' },
        { key: 'overdue', label: '超 48 小时', count: stats?.overdue_48h, alert: (stats?.overdue_48h ?? 0) > 0, hint: '优先清零' },
        { key: 'paywait', label: '待支付', count: stats?.pending_payment },
        { key: 'approved', label: '已通过', count: stats?.approved },
        { key: 'rejected', label: '已拒绝', count: stats?.rejected },
        { key: 'all', label: '全部', count: stats?.total },
    ];

    const summaryCards = [
        {
            key: 'pendingReview',
            title: '待审核申请',
            value: stats?.pending_review ?? '—',
            desc: '全库口径；今日进入的申请会实时更新。',
            tone: 'warning',
        },
        {
            key: 'overdue',
            title: '超48小时未处理',
            value: stats?.overdue_48h ?? '—',
            desc: '已进入优先催处理区间的待审核申请。',
            tone: (stats?.overdue_48h ?? 0) > 0 ? 'danger' : 'neutral',
        },
        {
            key: 'total',
            title: '累计申请',
            value: stats?.total ?? '—',
            desc: `已通过 ${stats?.approved ?? 0} · 已拒绝 ${stats?.rejected ?? 0}`,
            tone: 'info',
        },
        {
            key: 'paywait',
            title: '待支付（个人设计师）',
            value: stats?.pending_payment ?? '—',
            desc: '付认证费后自动进入待审核队列。',
            tone: 'neutral',
        },
    ];

    return (
        <div className="certification-container">
            <ManagementSearchPanel
                title="认证审核队列"
                description="按队列处理认证申请：待审核优先、超时红标；点开右侧抽屉核对材料后一键通过/拒绝。以下统计为全库真实口径。"
                actions={(
                    <>
                        <button className="btn-secondary" onClick={refreshAll} disabled={loading}>
                            <FiRefreshCw size={14} />
                            {loading ? '刷新中...' : '刷新'}
                        </button>
                    </>
                )}
                controls={(
                    <>
                        <div className="management-search-searchbox">
                            <FiSearch size={14} className="management-search-searchicon" />
                            <input
                                type="text"
                                className="management-search-input"
                                placeholder="搜索姓名、企业名、证件号或统一社会信用代码"
                                value={keywordInput}
                                onChange={(e) => setKeywordInput(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') handleSearch();
                                }}
                            />
                        </div>
                        <button className="btn-primary" onClick={handleSearch}>搜索申请</button>
                    </>
                )}
                summary={(
                    <>
                        <div>
                            当前队列共 <strong>{total}</strong> 条申请
                            {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                        </div>
                    </>
                )}
            />

            <div className="cert-summary-grid">
                {summaryCards.map((card) => (
                    <div key={card.key} className={`cert-summary-card section-card ${card.tone}`}>
                        <div className="cert-summary-title">{card.title}</div>
                        <div className="cert-summary-value">{card.value}</div>
                        <div className="cert-summary-desc">{card.desc}</div>
                    </div>
                ))}
            </div>

            <div className="cert-queues">
                {queueTabs.map((tab) => (
                    <button
                        key={tab.key}
                        className={`cert-queue-tab ${queue === tab.key ? 'on' : ''} ${tab.alert ? 'alert' : ''}`}
                        onClick={() => switchQueue(tab.key)}
                    >
                        {tab.label}
                        {tab.count != null && <span className="num">{tab.count}</span>}
                        {tab.hint && <span className="hint">{tab.hint}</span>}
                    </button>
                ))}
            </div>

            <div className="section-card">
                <div className="table-wrap">
                    {loading && list.length === 0 ? (
                        <div className="table-loading">加载中...</div>
                    ) : (
                        <div className="admin-table-region"><table className="data-table">
                            <thead>
                                <tr>
                                    <th>申请</th>
                                    <th>申请人</th>
                                    <th>类型</th>
                                    <th>主体信息</th>
                                    <th>阿里云核验</th>
                                    <th>状态</th>
                                    <th><FiClock size={12} style={{ verticalAlign: '-2px' }} /> 等待时长</th>
                                    <th>申请时间</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {list.length === 0 ? (
                                    <tr>
                                        <td colSpan={9} className="empty-state">
                                            当前队列为空 🎉
                                        </td>
                                    </tr>
                                ) : (
                                    list.map((app) => {
                                        const wait = waitDuration(app.created_at);
                                        const overdueRow = app.status === 'pending_review' && wait.level === 'bad';
                                        const subject = app.type === 'enterprise'
                                            ? (app.company_name || '-')
                                            : (revealedSubject(app) || '-');
                                        const subjectSub = app.type === 'enterprise'
                                            ? (app.credit_code || '统一社会信用代码待补')
                                            : (app.id_card_no || '证件号待提交');
                                        return (
                                            <tr
                                                key={app.id}
                                                className={overdueRow ? 'row-overdue' : ''}
                                                onClick={() => openDrawer(app)}
                                                style={{ cursor: 'pointer' }}
                                            >
                                                <td><b>#{app.id}</b><div className="cell-sub">用户 {app.user_id}</div></td>
                                                <td className="cert-subject-cell">{subject}<div className="cell-sub">{subjectSub}</div></td>
                                                <td>
                                                    <span className={`type-badge ${app.type}`}>
                                                        {TYPE_MAP[app.type] || app.type}
                                                    </span>
                                                </td>
                                                <td>{app.identity_type || '-'}</td>
                                                <td>
                                                    {app.aliyun_passed
                                                        ? <span className="aliyun-badge ok">通过</span>
                                                        : app.id_card_no || app.credit_code
                                                            ? <span className="aliyun-badge bad">未通过</span>
                                                            : <span className="aliyun-badge none">待提交材料</span>}
                                                </td>
                                                <td>
                                                    <span className={`status-badge status-${app.status}`}>
                                                        {STATUS_MAP[app.status] || app.status}
                                                    </span>
                                                </td>
                                                <td><span className={`wait-${wait.level}`}>{wait.text}</span></td>
                                                <td className="time-cell">{new Date(app.created_at).toLocaleString('zh-CN')}</td>
                                                <td onClick={(e) => e.stopPropagation()}>
                                                    <div className="action-buttons">
                                                        <button
                                                            className="btn-action btn-view"
                                                            onClick={() => openDrawer(app)}
                                                            title="打开审核抽屉"
                                                        >
                                                            <FiEye size={14} />
                                                        </button>
                                                        {app.status === 'pending_review' && (
                                                            <button
                                                                className="btn-action btn-enter-workbench"
                                                                onClick={() => openUserWorkbench(app.user_id)}
                                                                title="进入用户详情核查"
                                                            >
                                                                用户详情
                                                            </button>
                                                        )}
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })
                                )}
                            </tbody>
                        </table></div>
                    )}
                </div>

                {totalPages > 1 && (
                    <div className="pagination">
                        <button className="btn-page" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                            上一页
                        </button>
                        <span className="page-info">
                            第 {page} 页，共 {totalPages} 页
                        </span>
                        <button className="btn-page" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
                            下一页
                        </button>
                    </div>
                )}
            </div>

            {/* 审核抽屉：材料、核验结果、用户信息、备注、通过/拒绝同屏 */}
            {drawerApp && (
                <>
                    <div className="cert-drawer-mask" onClick={closeDrawer} />
                    <aside className="cert-drawer">
                        <div className="cert-drawer-head">
                            <h3>认证申请 #{drawerApp.id}</h3>
                            <span className={`status-badge status-${drawerApp.status}`}>{STATUS_MAP[drawerApp.status]}</span>
                            {drawerApp.status === 'pending_review' && waitDuration(drawerApp.created_at).level === 'bad' && (
                                <span className="overdue-flag">超时 {waitDuration(drawerApp.created_at).text}</span>
                            )}
                            <button className="cert-drawer-close" onClick={closeDrawer}>✕</button>
                        </div>
                        <div className="cert-drawer-body">
                            {detailLoading ? (
                                <div className="table-loading" role="status">加载中...</div>
                            ) : detailError || !detail ? (
                                <div className="table-loading" role="alert">
                                    <p>加载认证详情失败：{detailError || '未取得当前申请材料'}</p>
                                    <button className="btn-secondary" onClick={() => void openDrawer(drawerApp)}>重新加载</button>
                                </div>
                            ) : (
                                <>
                                    <div className="cert-sect">
                                        <h4>主体信息（{TYPE_MAP[drawerApp.type] || drawerApp.type}）</h4>
                                        <div className="kv"><span className="k">姓名/法人</span><span className="v">{revealed?.real_name || revealed?.legal_person || (drawerApp.type === 'enterprise' ? drawerApp.legal_person : drawerApp.real_name) || '-'}</span></div>
                                        {drawerApp.type === 'enterprise' && (
                                            <div className="kv"><span className="k">企业名称</span><span className="v">{drawerApp.company_name || '-'}</span></div>
                                        )}
                                        <div className="kv">
                                            <span className="k">{drawerApp.type === 'enterprise' ? '信用代码' : '身份证号'}</span>
                                            <span className="v">
                                                {revealed?.id_card_no || revealed?.credit_code || (drawerApp.type === 'enterprise' ? drawerApp.credit_code : drawerApp.id_card_no) || '-'}
                                                <button className="reveal-link" onClick={handleReveal} disabled={revealing}>
                                                    {revealing ? '查询中...' : revealed ? '已展示完整' : '查看完整（记审计）'}
                                                </button>
                                            </span>
                                        </div>
                                        {revealError && <div role="alert">查看完整材料失败：{revealError}</div>}
                                        <div className="kv"><span className="k">申请身份</span><span className="v">{drawerApp.identity_type || '-'}</span></div>
                                        {drawerApp.extra_docs_remark && (
                                            <div className="kv"><span className="k">其他证件说明</span><span className="v">{drawerApp.extra_docs_remark}</span></div>
                                        )}
                                    </div>
                                    <div className="cert-sect">
                                        <h4>自动核验</h4>
                                        {drawerApp.aliyun_passed ? (
                                            <div className="aliyun-result ok">阿里云二要素核验通过{drawerApp.aliyun_msg ? `（${drawerApp.aliyun_msg}）` : ''}</div>
                                        ) : (drawerApp.id_card_no || drawerApp.credit_code) ? (
                                            <div className="aliyun-result bad">阿里云核验未通过{drawerApp.aliyun_msg ? `：${drawerApp.aliyun_msg}` : ''}；请人工比对材料后决定。</div>
                                        ) : (
                                            <div className="aliyun-result none">材料尚未提交（待支付状态），核验在支付后自动进行。</div>
                                        )}
                                    </div>
                                    {detail?.user && (
                                        <div className="cert-sect">
                                            <h4>申请用户</h4>
                                            <div className="kv"><span className="k">用户</span><span className="v">ID {detail.user.id} · {detail.user.username}</span></div>
                                            <div className="kv"><span className="k">可提现</span><span className="v">{detail.user.can_withdraw ? '已开通' : '未开通（通过后自动开通）'}</span></div>
                                            <div className="drawer-links">
                                                <button className="btn-secondary" onClick={() => openUserWorkbench(detail.user!.id)}>用户详情</button>
                                            </div>
                                        </div>
                                    )}
                                    {drawerApp.status === 'pending_review' && (
                                        <div className="cert-sect">
                                            <h4>审核备注</h4>
                                            <textarea
                                                className="cert-remark-input"
                                                rows={3}
                                                value={adminRemark}
                                                onChange={(e) => setAdminRemark(e.target.value)}
                                                placeholder={drawerApp.type === 'enterprise' ? '企业认证建议核对：执照与法人证件是否一致、信用代码格式' : '填写审核意见或拒绝原因（拒绝时将展示给申请人）'}
                                            />
                                            {drawerApp.admin_remark && (
                                                <div className="prev-remark">历史备注：{drawerApp.admin_remark}</div>
                                            )}
                                        </div>
                                    )}
                                </>
                            )}
                            {reviewError && <div role="alert">审核操作失败：{reviewError}</div>}
                        </div>
                        {drawerApp.status === 'pending_review' && (
                            <div className="cert-drawer-foot">
                                <span className="foot-note">通过后自动开通可提现，并同步设计师/企业主页</span>
                                <button
                                    className="btn-reject"
                                    onClick={() => submitReview('reject')}
                                    disabled={!isCurrentCertification() || reviewSubmitting != null || !adminRemark.trim()}
                                    title={adminRemark.trim() ? '' : '拒绝必须填写原因'}
                                >
                                    拒绝
                                </button>
                                <button
                                    className="btn-approve"
                                    onClick={() => submitReview('approve')}
                                    disabled={!isCurrentCertification() || reviewSubmitting != null}
                                >
                                    {reviewSubmitting === 'approve' ? '提交中...' : '通过认证'}
                                </button>
                            </div>
                        )}
                    </aside>
                </>
            )}
        </div>
    );
};

// 列表层不需要揭示值（默认脱敏展示），统一走后端返回的脱敏字段
const revealedSubject = (app: CertificationApplication) =>
    app.type === 'enterprise' ? (app.company_name || '') : (app.real_name || '');

export default Certification;
