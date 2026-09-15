import React, { useState, useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiEye, FiCheck, FiX, FiRefreshCw, FiSearch } from 'react-icons/fi';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getCertificationList,
    getCertificationDetail,
    reviewCertification,
    type CertificationApplication,
    type CertificationDetailResponse,
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
    enterprise: '企业',
};

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;

const Certification: React.FC = () => {
    const navigate = useNavigate();
    const [list, setList] = useState<CertificationApplication[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [keywordInput, setKeywordInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [statusFilter, setStatusFilter] = useState<string>('');
    const [page, setPage] = useState(1);
    const pageSize = 20;

    const [detail, setDetail] = useState<CertificationDetailResponse | null>(null);
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [detailLoading, setDetailLoading] = useState(false);

    const [reviewTarget, setReviewTarget] = useState<CertificationApplication | null>(null);
    const [showReviewModal, setShowReviewModal] = useState(false);
    const [reviewAction, setReviewAction] = useState<'approve' | 'reject'>('approve');
    const [adminRemark, setAdminRemark] = useState('');
    const [reviewSubmitting, setReviewSubmitting] = useState(false);

    const loadList = async () => {
        setLoading(true);
        try {
            const res = await getCertificationList({
                status: statusFilter || undefined,
                keyword: keyword || undefined,
                limit: pageSize,
                offset: (page - 1) * pageSize,
            });
            setList(res.list);
            setTotal(res.total);
        } catch (e) {
            console.error('加载列表失败:', e);
            alert('加载列表失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        loadList();
    }, [page, statusFilter, keyword]);

    const handleSearch = () => {
        setPage(1);
        setKeyword(keywordInput.trim());
    };

    const handleReset = () => {
        setKeywordInput('');
        setKeyword('');
        setStatusFilter('');
        setPage(1);
    };

    const openUserWorkbench = (userId: number) => {
        navigate(`/user-workbench?userId=${userId}`);
    };

    const openDesignerCenter = (app: CertificationApplication) => {
        const keywordValue = app.real_name || String(app.user_id);
        navigate(`/designer-center?keyword=${encodeURIComponent(keywordValue)}`);
    };

    const handleViewDetail = async (app: CertificationApplication) => {
        setDetailLoading(true);
        setShowDetailModal(true);
        setDetail(null);
        try {
            const res = await getCertificationDetail(String(app.id));
            setDetail(res || null);
        } catch (e) {
            console.error('加载详情失败:', e);
            alert('加载详情失败');
        } finally {
            setDetailLoading(false);
        }
    };

    const handleOpenReview = (app: CertificationApplication, action: 'approve' | 'reject') => {
        setReviewTarget(app);
        setReviewAction(action);
        setAdminRemark('');
        setShowReviewModal(true);
    };

    const handleSubmitReview = async () => {
        if (!reviewTarget) return;
        setReviewSubmitting(true);
        try {
            await reviewCertification(String(reviewTarget.id), reviewAction, adminRemark.trim() || undefined);
            alert(reviewAction === 'approve' ? '已通过' : '已拒绝');
            setShowReviewModal(false);
            setReviewTarget(null);
            loadList();
        } catch (e: any) {
            alert(e?.message || '操作失败');
        } finally {
            setReviewSubmitting(false);
        }
    };

    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    const summaryCards = useMemo(() => {
        const pendingReviewCount = list.filter((item) => item.status === 'pending_review').length;
        const pendingPaymentCount = list.filter((item) => item.status === 'pending_payment').length;
        const enterpriseCount = list.filter((item) => item.type === 'enterprise').length;
        const designerCount = list.filter((item) => item.type === 'designer').length;
        const overdueCount = list.filter((item) => {
            if (item.status !== 'pending_review') return false;
            const createdAt = new Date(item.created_at).getTime();
            if (Number.isNaN(createdAt)) return false;
            return Date.now() - createdAt >= 48 * 60 * 60 * 1000;
        }).length;

        return [
            {
                key: 'pendingReview',
                title: '待审核申请',
                value: pendingReviewCount,
                desc: '需要人工尽快处理的申请数量。',
                tone: 'warning',
            },
            {
                key: 'overdue',
                title: '超48小时未处理',
                value: overdueCount,
                desc: '已进入优先催处理区间的待审核申请。',
                tone: overdueCount > 0 ? 'danger' : 'neutral',
            },
            {
                key: 'enterprise',
                title: '企业认证申请',
                value: enterpriseCount,
                desc: '通常资料更复杂，建议优先排队核验。',
                tone: 'info',
            },
            {
                key: 'designer',
                title: '个人设计师申请',
                value: designerCount,
                desc: `其中待支付 ${pendingPaymentCount} 条，可结合付款状态继续跟进。`,
                tone: 'neutral',
            },
        ];
    }, [list]);

    return (
        <Layout title="资质认证与审核">
            <div className="certification-container">
                <ManagementSearchPanel
                    title="认证申请检索与审核处理"
                    description="先按申请人、企业主体或状态快速找到目标申请，再进入详情做审核，减少人工翻页。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={loadList} disabled={loading}>
                                <FiRefreshCwIcon size={14} />
                                {loading ? '刷新中...' : '刷新'}
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
                                    placeholder="搜索用户ID、姓名、企业名、证件号、统一社会信用代码"
                                    value={keywordInput}
                                    onChange={(e) => setKeywordInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                />
                            </div>
                            <select
                                className="management-search-select"
                                value={statusFilter}
                                onChange={(e) => {
                                    setStatusFilter(e.target.value);
                                    setPage(1);
                                }}
                            >
                                <option value="">全部状态</option>
                                <option value="pending_payment">待支付</option>
                                <option value="pending_review">待审核</option>
                                <option value="approved">已通过</option>
                                <option value="rejected">已拒绝</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索申请</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共 <strong>{total}</strong> 条认证申请
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                                {statusFilter ? <span className="management-search-tag">状态：{STATUS_MAP[statusFilter] || statusFilter}</span> : null}
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

                <div className="section-card">
                    <div className="table-wrap">
                        {loading ? (
                            <div className="table-loading">加载中...</div>
                        ) : (
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>申请ID</th>
                                        <th>用户ID</th>
                                        <th>类型</th>
                                        <th>姓名/企业</th>
                                        <th>状态</th>
                                        <th>申请时间</th>
                                        <th>协同入口</th>
                                        <th>操作</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {list.length === 0 ? (
                                        <tr>
                                            <td colSpan={8} className="empty-state">
                                                暂无认证申请
                                            </td>
                                        </tr>
                                    ) : (
                                        list.map((app) => (
                                            <tr key={app.id}>
                                                <td>{app.id}</td>
                                                <td>{app.user_id}</td>
                                                <td>{TYPE_MAP[app.type] || app.type}</td>
                                                <td>
                                                    {app.type === 'enterprise'
                                                        ? app.company_name || '-'
                                                        : app.real_name || '-'}
                                                </td>
                                                <td>
                                                    <span className={`status-badge status-${app.status}`}>
                                                        {STATUS_MAP[app.status] || app.status}
                                                    </span>
                                                </td>
                                                <td>{new Date(app.created_at).toLocaleString('zh-CN')}</td>
                                                <td>
                                                    <div className="action-buttons action-buttons-stack">
                                                        <button
                                                            className="btn-action btn-link-lite"
                                                            onClick={() => openUserWorkbench(app.user_id)}
                                                            title="进入用户360"
                                                        >
                                                            用户360
                                                        </button>
                                                        {app.type === 'designer' ? (
                                                            <button
                                                                className="btn-action btn-link-lite"
                                                                onClick={() => openDesignerCenter(app)}
                                                                title="进入设计师中心"
                                                            >
                                                                设计师中心
                                                            </button>
                                                        ) : null}
                                                    </div>
                                                </td>
                                                <td>
                                                    <div className="action-buttons">
                                                        <button
                                                            className="btn-action btn-view"
                                                            onClick={() => handleViewDetail(app)}
                                                            title="查看详情"
                                                        >
                                                            <FiEye size={14} />
                                                        </button>
                                                        {app.status === 'pending_review' && (
                                                            <>
                                                                <button
                                                                    className="btn-action btn-approve"
                                                                    onClick={() => handleOpenReview(app, 'approve')}
                                                                    title="通过"
                                                                >
                                                                    <FiCheck size={14} />
                                                                </button>
                                                                <button
                                                                    className="btn-action btn-reject"
                                                                    onClick={() => handleOpenReview(app, 'reject')}
                                                                    title="拒绝"
                                                                >
                                                                    <FiX size={14} />
                                                                </button>
                                                            </>
                                                        )}
                                                    </div>
                                                </td>
                                            </tr>
                                        ))
                                    )}
                                </tbody>
                            </table>
                        )}
                    </div>

                    {totalPages > 1 && (
                        <div className="pagination">
                            <button
                                className="btn-page"
                                disabled={page <= 1}
                                onClick={() => setPage((p) => p - 1)}
                            >
                                上一页
                            </button>
                            <span className="page-info">
                                第 {page} 页，共 {totalPages} 页
                            </span>
                            <button
                                className="btn-page"
                                disabled={page >= totalPages}
                                onClick={() => setPage((p) => p + 1)}
                            >
                                下一页
                            </button>
                        </div>
                    )}
                </div>

                {/* 详情弹窗 */}
                {showDetailModal && (
                    <div className="modal-overlay" onClick={() => setShowDetailModal(false)}>
                        <div className="modal-content detail-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>认证申请详情</h3>
                                <button className="modal-close" onClick={() => setShowDetailModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                {detailLoading ? (
                                    <div className="table-loading">加载中...</div>
                                ) : detail ? (
                                    <div className="cert-detail">
                                        <div className="detail-section">
                                            <h4>申请信息</h4>
                                            <div className="info-row">
                                                <span className="info-label">类型</span>
                                                <span className="info-value">{TYPE_MAP[detail.application.type]}</span>
                                            </div>
                                            <div className="info-row">
                                                <span className="info-label">真实姓名</span>
                                                <span className="info-value">{detail.application.real_name || '-'}</span>
                                            </div>
                                            <div className="info-row">
                                                <span className="info-label">身份证号</span>
                                                <span className="info-value">{detail.application.id_card_no || '-'}</span>
                                            </div>
                                            {detail.application.type === 'enterprise' && (
                                                <>
                                                    <div className="info-row">
                                                        <span className="info-label">企业名称</span>
                                                        <span className="info-value">{detail.application.company_name || '-'}</span>
                                                    </div>
                                                    <div className="info-row">
                                                        <span className="info-label">统一社会信用代码</span>
                                                        <span className="info-value">{detail.application.credit_code || '-'}</span>
                                                    </div>
                                                    <div className="info-row">
                                                        <span className="info-label">法人姓名</span>
                                                        <span className="info-value">{detail.application.legal_person || '-'}</span>
                                                    </div>
                                                </>
                                            )}
                                            <div className="info-row">
                                                <span className="info-label">阿里云核验</span>
                                                <span className="info-value">
                                                    {detail.application.aliyun_passed ? '通过' : '未通过'}
                                                    {detail.application.aliyun_msg && `（${detail.application.aliyun_msg}）`}
                                                </span>
                                            </div>
                                            {detail.application.extra_docs_remark && (
                                                <div className="info-row full">
                                                    <span className="info-label">其他证件说明</span>
                                                    <span className="info-value">{detail.application.extra_docs_remark}</span>
                                                </div>
                                            )}
                                            <div className="info-row">
                                                <span className="info-label">状态</span>
                                                <span className={`status-badge status-${detail.application.status}`}>
                                                    {STATUS_MAP[detail.application.status]}
                                                </span>
                                            </div>
                                            {detail.application.admin_remark && (
                                                <div className="info-row full">
                                                    <span className="info-label">审核备注</span>
                                                    <span className="info-value">{detail.application.admin_remark}</span>
                                                </div>
                                            )}
                                            <div className="info-row">
                                                <span className="info-label">申请时间</span>
                                                <span className="info-value">
                                                    {new Date(detail.application.created_at).toLocaleString('zh-CN')}
                                                </span>
                                            </div>
                                        </div>
                                        {detail.user && (
                                            <div className="detail-section">
                                                <h4>用户信息</h4>
                                                <div className="info-row">
                                                    <span className="info-label">用户ID</span>
                                                    <span className="info-value">{detail.user.id}</span>
                                                </div>
                                                <div className="info-row">
                                                    <span className="info-label">用户名</span>
                                                    <span className="info-value">{detail.user.username}</span>
                                                </div>
                                                <div className="info-row">
                                                    <span className="info-label">可提现</span>
                                                    <span className="info-value">{detail.user.can_withdraw ? '是' : '否'}</span>
                                                </div>
                                                <div className="detail-actions compact-actions">
                                                    <button className="btn-secondary" onClick={() => openUserWorkbench(detail.user!.id)}>
                                                        进入用户360
                                                    </button>
                                                    {detail.application.type === 'designer' ? (
                                                        <button className="btn-secondary" onClick={() => openDesignerCenter(detail.application)}>
                                                            进入设计师中心
                                                        </button>
                                                    ) : null}
                                                </div>
                                            </div>
                                        )}
                                        {detail.application.status === 'pending_review' && (
                                            <div className="detail-actions">
                                                <button
                                                    className="btn-primary"
                                                    onClick={() => {
                                                        setShowDetailModal(false);
                                                        handleOpenReview(detail.application, 'approve');
                                                    }}
                                                >
                                                    通过
                                                </button>
                                                <button
                                                    className="btn-danger"
                                                    onClick={() => {
                                                        setShowDetailModal(false);
                                                        handleOpenReview(detail.application, 'reject');
                                                    }}
                                                >
                                                    拒绝
                                                </button>
                                            </div>
                                        )}
                                    </div>
                                ) : (
                                    <div className="empty-state">暂无详情</div>
                                )}
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowDetailModal(false)}>
                                    关闭
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 审核弹窗 */}
                {showReviewModal && reviewTarget && (
                    <div className="modal-overlay" onClick={() => setShowReviewModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{reviewAction === 'approve' ? '通过认证' : '拒绝认证'}</h3>
                                <button className="modal-close" onClick={() => setShowReviewModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <p className="review-hint">
                                    {reviewTarget.type === 'enterprise' ? '企业' : '个人设计师'}申请（用户ID: {reviewTarget.user_id}）
                                </p>
                                <div className="form-group">
                                    <label>审核备注（选填）</label>
                                    <textarea
                                        className="form-input"
                                        rows={3}
                                        value={adminRemark}
                                        onChange={(e) => setAdminRemark(e.target.value)}
                                        placeholder="填写审核意见或拒绝原因"
                                    />
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowReviewModal(false)}>
                                    取消
                                </button>
                                <button
                                    className={reviewAction === 'approve' ? 'btn-primary' : 'btn-danger'}
                                    onClick={handleSubmitReview}
                                    disabled={reviewSubmitting}
                                >
                                    {reviewSubmitting ? '提交中...' : reviewAction === 'approve' ? '通过' : '拒绝'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default Certification;
