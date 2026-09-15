import React, { useEffect, useMemo, useState } from 'react';
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
} from 'react-icons/fi';
import { useNavigate } from 'react-router-dom';
import Layout from '../component/layout';
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

const defaultSummary: DesignerManagementListSummary = {
    total_designers: 0,
    public_designers: 0,
    approved_designers: 0,
    designers_with_works: 0,
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
    const [loading, setLoading] = useState(false);
    const [detailLoading, setDetailLoading] = useState(false);
    const [actionLoadingId, setActionLoadingId] = useState<number | null>(null);
    const [items, setItems] = useState<DesignerManagementItem[]>([]);
    const [summary, setSummary] = useState<DesignerManagementListSummary>(defaultSummary);
    const [selectedId, setSelectedId] = useState<number | null>(null);
    const [detail, setDetail] = useState<DesignerManagementDetail | null>(null);
    const [keywordInput, setKeywordInput] = useState(initialKeyword);
    const [keyword, setKeyword] = useState(initialKeyword);
    const [certificationStatus, setCertificationStatus] = useState('all');
    const [visible, setVisible] = useState('all');

    const loadList = async (nextSelectedId?: number | null) => {
        setLoading(true);
        try {
            const response = await getDesignerManagementList({
                page: 1,
                page_size: 20,
                keyword,
                certification_status: certificationStatus,
                visible,
            });
            setItems(response.list || []);
            setSummary(response.summary || defaultSummary);
            if (typeof nextSelectedId === 'number') {
                setSelectedId(nextSelectedId);
            } else if (response.list?.length) {
                setSelectedId((current) => {
                    if (current && response.list.some((item) => item.user_id === current)) {
                        return current;
                    }
                    return response.list[0].user_id;
                });
            } else {
                setSelectedId(null);
                setDetail(null);
            }
        } catch (error) {
            console.error('加载设计师列表失败:', error);
            alert('加载设计师列表失败');
        } finally {
            setLoading(false);
        }
    };

    const loadDetail = async (id: number) => {
        setDetailLoading(true);
        try {
            const response = await getDesignerManagementDetail(id);
            setDetail(response);
        } catch (error) {
            console.error('加载设计师详情失败:', error);
            alert('加载设计师详情失败');
        } finally {
            setDetailLoading(false);
        }
    };

    useEffect(() => {
        void loadList();
    }, [keyword, certificationStatus, visible]);

    useEffect(() => {
        if (!selectedId) {
            setDetail(null);
            return;
        }
        void loadDetail(selectedId);
    }, [selectedId]);

    const selectedListItem = useMemo(() => {
        return items.find((item) => item.user_id === selectedId) || null;
    }, [items, selectedId]);

    const summaryCards = useMemo(() => {
        return [
            {
                key: 'total',
                title: '设计师总数',
                value: summary.total_designers,
                icon: FiUsersIcon,
            },
            {
                key: 'public',
                title: '已公开主页',
                value: summary.public_designers,
                icon: FiEyeIcon,
            },
            {
                key: 'approved',
                title: '认证已通过',
                value: summary.approved_designers,
                icon: FiCheckCircleIcon,
            },
            {
                key: 'works',
                title: '已有作品设计师',
                value: summary.designers_with_works,
                icon: FiImageIcon,
            },
        ];
    }, [summary]);

    const handleSearch = () => {
        setKeyword(keywordInput.trim());
    };

    const handleReset = () => {
        setKeywordInput('');
        setKeyword('');
        setCertificationStatus('all');
        setVisible('all');
    };

    const handleToggleVisibility = async (item: DesignerManagementItem) => {
        setActionLoadingId(item.user_id);
        try {
            await updateDesignerVisibility(item.user_id, !item.designer_visible);
            await loadList(item.user_id);
            await loadDetail(item.user_id);
        } catch (error) {
            console.error('更新设计师展示状态失败:', error);
            alert('更新设计师展示状态失败');
        } finally {
            setActionLoadingId(null);
        }
    };

    const handleToggleService = async (item: DesignerManagementItem) => {
        setActionLoadingId(item.user_id);
        try {
            await updateDesignerServiceStatus(item.user_id, !item.service_enabled);
            await loadList(item.user_id);
            await loadDetail(item.user_id);
        } catch (error) {
            console.error('更新设计师接单状态失败:', error);
            alert('更新设计师接单状态失败');
        } finally {
            setActionLoadingId(null);
        }
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
        <Layout title="设计师中心">
            <div className="designer-center-page">
                <ManagementSearchPanel
                    title="设计师检索与运营处理"
                    description="先按认证状态、公开状态和关键词找到设计师，再查看详情、切换主页展示或处理接单状态。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadList(selectedId)} disabled={loading}>
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
                            <select className="management-search-select" value={certificationStatus} onChange={(e) => setCertificationStatus(e.target.value)}>
                                <option value="all">全部认证状态</option>
                                <option value="approved">已通过</option>
                                <option value="pending_review">待审核</option>
                                <option value="rejected">已拒绝</option>
                                <option value="pending_payment">待支付</option>
                                <option value="none">未申请</option>
                            </select>
                            <select className="management-search-select" value={visible} onChange={(e) => setVisible(e.target.value)}>
                                <option value="all">全部公开状态</option>
                                <option value="public">已公开</option>
                                <option value="hidden">已隐藏</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索设计师</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前设计师 <strong>{items.length}</strong> 位
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                                {certificationStatus !== 'all' ? <span className="management-search-tag">认证状态：{CERT_STATUS_MAP[certificationStatus] || certificationStatus}</span> : null}
                                {visible !== 'all' ? <span className="management-search-tag">主页状态：{visible === 'public' ? '已公开' : '已隐藏'}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="designer-summary-grid">
                    {summaryCards.map((card) => {
                        const Icon = card.icon;
                        return (
                            <div key={card.key} className="designer-summary-card section-card">
                                <div className="designer-summary-icon"><Icon /></div>
                                <div className="designer-summary-title">{card.title}</div>
                                <div className="designer-summary-value">{card.value}</div>
                            </div>
                        );
                    })}
                </div>

                <div className="designer-center-grid">
                    <div className="designer-list-panel section-card">
                        <div className="designer-list">
                            {items.length === 0 ? (
                                <div className="designer-empty">当前没有符合条件的设计师。</div>
                            ) : (
                                items.map((item) => (
                                    <button
                                        key={item.user_id}
                                        className={`designer-list-item ${selectedId === item.user_id ? 'active' : ''}`}
                                        onClick={() => setSelectedId(item.user_id)}
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
                    </div>

                    <div className="designer-detail-panel section-card">
                        {!selectedId ? (
                            <div className="designer-empty">请先从左侧选择一个设计师。</div>
                        ) : detailLoading ? (
                            <div className="designer-empty">设计师详情加载中...</div>
                        ) : !detail ? (
                            <div className="designer-empty">未获取到设计师详情。</div>
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
                                            查看用户360
                                            <FiChevronRightIcon />
                                        </button>
                                        <button
                                            className={detail.profile?.designer_visible ? 'btn-warning' : 'btn-primary'}
                                            onClick={() => void handleToggleVisibility(selectedListItem || {
                                                user_id: detail.user_id,
                                                username: detail.username,
                                                designer_visible: detail.profile?.designer_visible === true,
                                                can_withdraw: detail.can_withdraw,
                                                total_works: detail.stats.total_works,
                                                follow_count: detail.stats.follow_count,
                                                review_count: detail.stats.review_count,
                                                positive_review_count: detail.stats.positive_review_count,
                                                negative_review_count: detail.stats.negative_review_count,
                                            } as DesignerManagementItem)}
                                            disabled={actionLoadingId === detail.user_id}
                                        >
                                            {actionLoadingId === detail.user_id ? '提交中...' : detail.profile?.designer_visible ? '下架主页' : '上架主页'}
                                        </button>
                                        <button
                                            className={detail.profile?.service_enabled ? 'btn-warning' : 'btn-primary'}
                                            onClick={() => void handleToggleService(selectedListItem || {
                                                user_id: detail.user_id,
                                                username: detail.username,
                                                service_enabled: detail.profile?.service_enabled === true,
                                                designer_visible: detail.profile?.designer_visible === true,
                                                can_withdraw: detail.can_withdraw,
                                                total_works: detail.stats.total_works,
                                                follow_count: detail.stats.follow_count,
                                                review_count: detail.stats.review_count,
                                                positive_review_count: detail.stats.positive_review_count,
                                                negative_review_count: detail.stats.negative_review_count,
                                            } as DesignerManagementItem)}
                                            disabled={actionLoadingId === detail.user_id}
                                        >
                                            {actionLoadingId === detail.user_id ? '提交中...' : detail.profile?.service_enabled ? '暂停接单' : '恢复接单'}
                                        </button>
                                    </div>
                                </div>

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
        </Layout>
    );
};

export default DesignerCenter;
