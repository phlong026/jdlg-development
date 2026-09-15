import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    FiAlertCircle,
    FiCheckCircle,
    FiChevronRight,
    FiClock,
    FiCopy,
    FiExternalLink,
    FiRefreshCw,
    FiSearch,
    FiShield,
    FiUser,
    FiZap,
} from 'react-icons/fi';
import Layout from '../component/layout';
import {
    adjustUserStones,
    getUserDetail,
    getUserEnterpriseWechatVerification,
    getUserList,
    getUserWorkbenchSummary,
    setUserStones,
    updateUser,
    updateUserEnterpriseWechatVerification,
    type User as ApiUser,
    type UserWorkbenchSummary,
} from '../api/users';
import {
    getCertificationDetail,
    getCertificationList,
    reviewCertification,
    type CertificationApplication,
    type CertificationDetailResponse,
} from '../api/certification';
import './userWorkbench.scss';

interface RecentUserItem {
    id: string;
    username: string;
}

interface PendingItem {
    key: string;
    title: string;
    description: string;
    actionLabel: string;
    tone: 'warning' | 'danger' | 'normal';
    action: () => void;
}

type DetailTabKey = 'membership' | 'orders' | 'tasks' | 'stones' | 'risk';

const RECENT_USERS_STORAGE_KEY = 'user-workbench-recent-users';

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

const ORDER_TYPE_MAP: Record<string, string> = {
    recharge: '充值',
    consume: 'AI消费',
    culture: '内容消费',
    certification: '认证费',
    withdraw: '提现',
};

const ORDER_STATUS_MAP: Record<string, string> = {
    pending: '待处理',
    success: '成功',
    failed: '失败',
    cancelled: '已取消',
};

const TASK_STATUS_MAP: Record<string, string> = {
    pending: '待处理',
    running: '处理中',
    processing: '处理中',
    success: '成功',
    failed: '失败',
};

const MEMBERSHIP_STATUS_MAP: Record<string, string> = {
    active: '生效中',
    expired: '已到期',
    inactive: '未开通',
};

const STONE_TYPE_MAP: Record<string, string> = {
    recharge: '充值',
    consume: '消耗',
    checkin: '签到',
    task: '任务奖励',
    invite: '邀请奖励',
    invite_reward: '邀请返利',
    manual_grant: '后台补发',
    manual_deduct: '后台扣减',
    withdraw: '提现',
};

const RISK_TAG_MAP: Record<string, string> = {
    no_password: '未设置密码',
    recent_device_change: '近7天换绑设备',
    same_device_multiple_accounts: '同设备多账号',
};

const RISK_LEVEL_MAP: Record<'low' | 'medium' | 'high', string> = {
    low: '低风险',
    medium: '中风险',
    high: '高风险',
};

const formatDateTime = (value?: string | null) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const formatStoneRecordAmount = (item: { amount: number; type: string }) => {
    const amount = Math.abs(Number(item.amount || 0));
    const negativeTypes = new Set(['consume', 'manual_deduct', 'withdraw']);
    return `${negativeTypes.has(item.type) ? '-' : '+'}${amount}`;
};

const formatOrderAmount = (amount: number) => {
    if (amount > 0) return `+${amount}`;
    return `${amount}`;
};

const formatDeviceText = (value?: string | null) => {
    if (!value) return '暂无';
    if (value.length <= 16) return value;
    return `${value.slice(0, 6)}...${value.slice(-6)}`;
};

const getUserDisplayName = (user: ApiUser | null) => {
    if (!user) return '';
    return user.nickname?.trim() || user.username || `用户${user.id}`;
};

const getUserAvatarText = (user: ApiUser | null) => {
    const name = getUserDisplayName(user);
    return name?.slice(0, 1).toUpperCase() || 'U';
};

const sortCertifications = (items: CertificationApplication[]) => {
    return [...items].sort((a, b) => {
        const priority = (status: string) => {
            if (status === 'approved') return 0;
            if (status === 'pending_review') return 1;
            if (status === 'pending_payment') return 2;
            return 3;
        };
        const priorityDiff = priority(a.status) - priority(b.status);
        if (priorityDiff !== 0) {
            return priorityDiff;
        }
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    });
};

const UserWorkbench: React.FC = () => {
    const navigate = useNavigate();
    const initialUserId = new URLSearchParams(window.location.search).get('userId') || '';

    const [searchKeyword, setSearchKeyword] = useState(initialUserId);
    const [searching, setSearching] = useState(false);
    const [loading, setLoading] = useState(false);
    const [searchResults, setSearchResults] = useState<ApiUser[]>([]);
    const [selectedUser, setSelectedUser] = useState<ApiUser | null>(null);
    const [recentUsers, setRecentUsers] = useState<RecentUserItem[]>([]);
    const [certifications, setCertifications] = useState<CertificationApplication[]>([]);
    const [workbenchSummary, setWorkbenchSummary] = useState<UserWorkbenchSummary | null>(null);
    const [activeDetailTab, setActiveDetailTab] = useState<DetailTabKey>('orders');

    const [showStonesModal, setShowStonesModal] = useState(false);
    const [stonesMode, setStonesMode] = useState<'set' | 'adjust'>('adjust');
    const [stonesValue, setStonesValue] = useState('');
    const [stonesRemark, setStonesRemark] = useState('');
    const [stonesLoading, setStonesLoading] = useState(false);

    const [showEnterpriseWechatModal, setShowEnterpriseWechatModal] = useState(false);
    const [enterpriseWechatVerified, setEnterpriseWechatVerified] = useState(false);
    const [enterpriseWechatContact, setEnterpriseWechatContact] = useState('');
    const [enterpriseWechatVerifiedAtText, setEnterpriseWechatVerifiedAtText] = useState('');
    const [enterpriseWechatLoading, setEnterpriseWechatLoading] = useState(false);
    const [enterpriseWechatSaving, setEnterpriseWechatSaving] = useState(false);

    const [showAccountSecurityModal, setShowAccountSecurityModal] = useState(false);
    const [accountUsername, setAccountUsername] = useState('');
    const [accountPassword, setAccountPassword] = useState('');
    const [accountPasswordConfirm, setAccountPasswordConfirm] = useState('');
    const [accountSecuritySaving, setAccountSecuritySaving] = useState(false);

    const [showCertificationDetailModal, setShowCertificationDetailModal] = useState(false);
    const [certificationDetail, setCertificationDetail] = useState<CertificationDetailResponse | null>(null);
    const [certificationDetailLoading, setCertificationDetailLoading] = useState(false);

    const [showReviewModal, setShowReviewModal] = useState(false);
    const [reviewTarget, setReviewTarget] = useState<CertificationApplication | null>(null);
    const [reviewAction, setReviewAction] = useState<'approve' | 'reject'>('approve');
    const [adminRemark, setAdminRemark] = useState('');
    const [reviewSubmitting, setReviewSubmitting] = useState(false);

    useEffect(() => {
        try {
            const raw = localStorage.getItem(RECENT_USERS_STORAGE_KEY);
            if (raw) {
                const parsed = JSON.parse(raw) as RecentUserItem[];
                setRecentUsers(Array.isArray(parsed) ? parsed : []);
            }
        } catch (error) {
            console.error('读取最近访问用户失败:', error);
        }
    }, []);

    useEffect(() => {
        if (initialUserId) {
            void loadWorkbenchUser(initialUserId);
        }
    }, []);

    const currentCertification = useMemo(() => {
        return certifications[0] || null;
    }, [certifications]);

    const activeCertification = useMemo(() => {
        return certifications.find((item) => item.status === 'approved' || item.status === 'pending_review' || item.status === 'pending_payment') || null;
    }, [certifications]);

    const actionableCertification = useMemo(() => {
        if (activeCertification?.status === 'pending_review' || activeCertification?.status === 'pending_payment') {
            return activeCertification;
        }
        return null;
    }, [activeCertification]);

    const persistRecentUsers = (user: ApiUser) => {
        const next = [
            { id: String(user.id), username: user.username || `用户${user.id}` },
            ...recentUsers.filter((item) => item.id !== String(user.id)),
        ].slice(0, 6);
        setRecentUsers(next);
        localStorage.setItem(RECENT_USERS_STORAGE_KEY, JSON.stringify(next));
    };

    const loadWorkbenchUser = async (userId: string) => {
        setLoading(true);
        try {
            const detail = await getUserDetail(String(userId));
            if (!detail) {
                alert('未找到该用户');
                return;
            }
            setSelectedUser(detail);
            setSearchResults([]);
            setSearchKeyword(String(detail.id));
            setActiveDetailTab('orders');
            persistRecentUsers(detail);

            const [wechatDetail, certRes, summaryRes] = await Promise.all([
                getUserEnterpriseWechatVerification(String(detail.id)).catch(() => null),
                getCertificationList({ limit: 100, offset: 0 }).catch(() => ({ list: [], total: 0 })),
                getUserWorkbenchSummary(String(detail.id)).catch(() => null),
            ]);

            setEnterpriseWechatVerified(!!wechatDetail?.enterprise_wechat_verified);
            setEnterpriseWechatContact(wechatDetail?.enterprise_wechat_contact || '');
            setEnterpriseWechatVerifiedAtText(wechatDetail?.enterprise_wechat_verified_at || '');
            setWorkbenchSummary(summaryRes);
            setCertifications(
                sortCertifications((certRes.list || []).filter((item) => String(item.user_id) === String(detail.id)))
            );
        } catch (error: any) {
            console.error('加载用户360工作台失败:', error);
            alert(error?.message || '加载用户失败');
        } finally {
            setLoading(false);
        }
    };

    const handleSearch = async () => {
        const keyword = searchKeyword.trim();
        if (!keyword) {
            alert('请输入用户ID、用户名或手机号');
            return;
        }
        setSearching(true);
        try {
            if (/^\d+$/.test(keyword)) {
                await loadWorkbenchUser(keyword);
                return;
            }
            const result = await getUserList({ keyword, page: 1, page_size: 8 });
            setSearchResults(result.list || []);
            if ((result.list || []).length === 0) {
                alert('没有搜索到匹配用户');
            }
        } catch (error: any) {
            console.error('搜索用户失败:', error);
            alert(error?.message || '搜索用户失败');
        } finally {
            setSearching(false);
        }
    };

    const handleRefresh = async () => {
        if (!selectedUser) return;
        await loadWorkbenchUser(String(selectedUser.id));
    };

    const handleOpenRelatedUser = async (userId: number) => {
        await loadWorkbenchUser(String(userId));
    };

    const handleCopyUserId = async () => {
        if (!selectedUser) return;
        try {
            await navigator.clipboard.writeText(String(selectedUser.id));
            alert('用户ID已复制');
        } catch (error) {
            console.error('复制用户ID失败:', error);
            alert('复制失败，请手动复制');
        }
    };

    const handleOpenStonesModal = () => {
        if (!selectedUser) return;
        setStonesMode('adjust');
        setStonesValue('');
        setStonesRemark('');
        setShowStonesModal(true);
    };

    const handleQuickAdjust = (amount: number) => {
        setStonesMode('adjust');
        setStonesValue(String(amount));
    };

    const handleStonesSubmit = async () => {
        if (!selectedUser) return;
        const value = parseInt(stonesValue, 10);
        if (Number.isNaN(value)) {
            alert('请输入有效数字');
            return;
        }
        if (stonesMode === 'set' && value < 0) {
            alert('新余额不能为负数');
            return;
        }
        if (stonesMode === 'adjust' && value === 0) {
            alert('调整数量不能为0');
            return;
        }
        setStonesLoading(true);
        try {
            if (stonesMode === 'set') {
                await setUserStones(String(selectedUser.id), {
                    stones: value,
                    remark: stonesRemark.trim() || undefined,
                });
            } else {
                await adjustUserStones(String(selectedUser.id), {
                    amount: value,
                    remark: stonesRemark.trim() || undefined,
                });
            }
            alert('灵石已更新');
            setShowStonesModal(false);
            await loadWorkbenchUser(String(selectedUser.id));
        } catch (error: any) {
            console.error('更新灵石失败:', error);
            alert(error?.message || '更新灵石失败');
        } finally {
            setStonesLoading(false);
        }
    };

    const handleOpenEnterpriseWechatModal = async () => {
        if (!selectedUser) return;
        setShowEnterpriseWechatModal(true);
        setEnterpriseWechatLoading(true);
        try {
            const detail = await getUserEnterpriseWechatVerification(String(selectedUser.id));
            setEnterpriseWechatVerified(!!detail?.enterprise_wechat_verified);
            setEnterpriseWechatContact(detail?.enterprise_wechat_contact || '');
            setEnterpriseWechatVerifiedAtText(detail?.enterprise_wechat_verified_at || '');
        } catch (error: any) {
            console.error('获取手机号授权状态失败:', error);
            alert(error?.message || '获取手机号授权状态失败');
            setShowEnterpriseWechatModal(false);
        } finally {
            setEnterpriseWechatLoading(false);
        }
    };

    const handleOpenAccountSecurityModal = () => {
        if (!selectedUser) return;
        setAccountUsername(selectedUser.username || '');
        setAccountPassword('');
        setAccountPasswordConfirm('');
        setShowAccountSecurityModal(true);
    };

    const handleAccountSecuritySubmit = async () => {
        if (!selectedUser) return;
        const username = accountUsername.trim();
        const password = accountPassword;
        const passwordConfirm = accountPasswordConfirm;
        if (!username) {
            alert('请输入登录用户名');
            return;
        }
        if (username.length < 4) {
            alert('登录用户名至少 4 位');
            return;
        }
        if (password || passwordConfirm) {
            if (password.length < 6) {
                alert('新密码至少 6 位');
                return;
            }
            if (password !== passwordConfirm) {
                alert('两次输入的新密码不一致');
                return;
            }
        }
        const usernameChanged = username !== (selectedUser.username || '');
        const passwordChanged = !!password;
        if (!usernameChanged && !passwordChanged) {
            alert('请至少修改用户名或输入新密码');
            return;
        }
        setAccountSecuritySaving(true);
        try {
            await updateUser(String(selectedUser.id), {
                username,
                password: passwordChanged ? password : undefined,
            });
            alert(passwordChanged ? '账号信息和登录密码已更新' : '登录用户名已更新');
            setShowAccountSecurityModal(false);
            await loadWorkbenchUser(String(selectedUser.id));
        } catch (error: any) {
            console.error('更新账号与安全信息失败:', error);
            alert(error?.message || '更新账号与安全信息失败');
        } finally {
            setAccountSecuritySaving(false);
        }
    };

    const handleEnterpriseWechatSubmit = async () => {
        if (!selectedUser) return;
        const contact = enterpriseWechatContact.trim();
        if (enterpriseWechatVerified && !contact) {
            alert('已授权状态必须填写手机号');
            return;
        }
        setEnterpriseWechatSaving(true);
        try {
            await updateUserEnterpriseWechatVerification(String(selectedUser.id), {
                verified: enterpriseWechatVerified,
                contact,
            });
            alert('下载权限状态已更新');
            setShowEnterpriseWechatModal(false);
            await loadWorkbenchUser(String(selectedUser.id));
        } catch (error: any) {
            console.error('保存手机号授权状态失败:', error);
            alert(error?.message || '保存手机号授权状态失败');
        } finally {
            setEnterpriseWechatSaving(false);
        }
    };

    const openCertificationDetail = async (application: CertificationApplication) => {
        setShowCertificationDetailModal(true);
        setCertificationDetailLoading(true);
        setCertificationDetail(null);
        try {
            const detail = await getCertificationDetail(String(application.id));
            setCertificationDetail(detail || null);
        } catch (error: any) {
            console.error('获取认证详情失败:', error);
            alert(error?.message || '获取认证详情失败');
        } finally {
            setCertificationDetailLoading(false);
        }
    };

    const handleOpenCertification = async () => {
        if (activeCertification) {
            await openCertificationDetail(activeCertification);
            return;
        }
        if (currentCertification) {
            await openCertificationDetail(currentCertification);
            return;
        }
        navigate('/certification');
    };

    const handleOpenReview = (application: CertificationApplication, action: 'approve' | 'reject') => {
        setReviewTarget(application);
        setReviewAction(action);
        setAdminRemark('');
        setShowReviewModal(true);
    };

    const handleSubmitReview = async () => {
        if (!reviewTarget || !selectedUser) return;
        setReviewSubmitting(true);
        try {
            await reviewCertification(String(reviewTarget.id), reviewAction, adminRemark.trim() || undefined);
            alert(reviewAction === 'approve' ? '已通过认证' : '已拒绝认证');
            setShowReviewModal(false);
            setShowCertificationDetailModal(false);
            await loadWorkbenchUser(String(selectedUser.id));
        } catch (error: any) {
            console.error('提交审核失败:', error);
            alert(error?.message || '提交审核失败');
        } finally {
            setReviewSubmitting(false);
        }
    };

    const pendingItems = useMemo<PendingItem[]>(() => {
        if (!selectedUser) return [];
        const items: PendingItem[] = [];
        if (!enterpriseWechatVerified) {
            items.push({
                key: 'wechat',
                title: '下载权限待核验',
                description: '当前用户还未完成手机号授权验证，可能影响下载高清图。',
                actionLabel: '修正权限',
                tone: 'warning',
                action: () => {
                    void handleOpenEnterpriseWechatModal();
                },
            });
        }
        if (actionableCertification) {
            items.push({
                key: 'certification',
                title: '认证申请待处理',
                description: `${CERT_TYPE_MAP[actionableCertification.type] || '资质认证'}${actionableCertification.status === 'pending_payment' ? '待支付确认。' : '正在等待审核。'}`,
                actionLabel: '去审核',
                tone: 'danger',
                action: () => {
                    void openCertificationDetail(actionableCertification);
                },
            });
        }
        if ((selectedUser.stones || 0) <= 0) {
            items.push({
                key: 'stones',
                title: '灵石余额不足',
                description: '当前灵石余额为 0，若用户反馈无法继续生成，可优先核查充值或补发。',
                actionLabel: '调整灵石',
                tone: 'warning',
                action: handleOpenStonesModal,
            });
        }
        return items.slice(0, 3);
    }, [actionableCertification, enterpriseWechatVerified, selectedUser]);

    const summaryCards = useMemo(() => {
        const recentTasks = workbenchSummary?.recent_tasks || [];
        const recentOrders = workbenchSummary?.recent_orders || [];
        const stoneSummary = workbenchSummary?.stone_summary;
        const membership = workbenchSummary?.membership;
        const failedTaskCount = recentTasks.filter((item) => item.status === 'failed').length;
        const pendingOrderCount = recentOrders.filter((item) => item.status === 'pending').length;
        const latestTask = recentTasks[0];
        const latestOrder = recentOrders[0];
        return [
            {
                key: 'task',
                title: '任务摘要',
                value: failedTaskCount > 0 ? `失败${failedTaskCount}条` : recentTasks.length > 0 ? `最近${recentTasks.length}条` : '暂无任务',
                description: latestTask
                    ? `最近一条：${latestTask.task_type === 'video' ? '视频任务' : latestTask.scene} · ${TASK_STATUS_MAP[latestTask.status] || latestTask.status} · ${formatDateTime(latestTask.created_at)}`
                    : '当前用户暂无生成记录。',
                actionLabel: '查看AI任务中心',
                disabled: false,
                onClick: () => navigate('/ai-tasks'),
            },
            {
                key: 'order',
                title: '订单摘要',
                value: pendingOrderCount > 0 ? `待处理${pendingOrderCount}笔` : recentOrders.length > 0 ? `最近${recentOrders.length}笔` : '暂无订单',
                description: latestOrder
                    ? `最近一笔：${ORDER_TYPE_MAP[latestOrder.type] || latestOrder.type} · ${ORDER_STATUS_MAP[latestOrder.status] || latestOrder.status} · ${formatDateTime(latestOrder.created_at)}`
                    : '当前用户暂无订单记录。',
                actionLabel: '查看订单中心',
                disabled: false,
                onClick: () => navigate('/recharge'),
            },
            {
                key: 'membership',
                title: '会员摘要',
                value: membership?.plan_title || '未开通',
                description: membership
                    ? `${MEMBERSHIP_STATUS_MAP[membership.status] || membership.status} · ${membership.is_lifetime ? '长期有效' : `到期时间 ${formatDateTime(membership.expired_at)}`}`
                    : `近30天获得 ${stoneSummary?.recent_gain || 0} 灵石，消耗 ${stoneSummary?.recent_consume || 0} 灵石。`,
                actionLabel: '查看订单中心',
                disabled: false,
                onClick: () => navigate('/recharge'),
            },
        ];
    }, [navigate, workbenchSummary]);

    return (
        <Layout title="用户360工作台">
            <div className="user-workbench-page">
                <div className="workbench-breadcrumb-row">
                    <button className="link-button" onClick={() => navigate('/users')}>返回用户列表</button>
                    {selectedUser && (
                        <button className="link-button" onClick={handleCopyUserId}>
                            <FiCopy />
                            复制用户ID
                        </button>
                    )}
                </div>

                {!selectedUser && (
                    <div className="section-card entry-guidance-card">
                        <div className="empty-hero-icon">
                            <FiUser />
                        </div>
                        <div className="entry-guidance-content">
                            <h3>用户360是单用户处理页，不是后台找人首页</h3>
                            <p>推荐你从“后台总控台”的异常队列，或者“用户管理”列表点击某个用户进入。只有临时快速找人时，才使用下面的辅助搜索。</p>
                            <div className="entry-guidance-actions">
                                <button className="btn-secondary" onClick={() => navigate('/dashboard')}>返回总控台</button>
                                <button className="btn-primary" onClick={() => navigate('/users')}>进入用户管理</button>
                            </div>
                        </div>
                    </div>
                )}

                <div className="section-card search-panel">
                    <div className="section-head compact">
                        <div>
                            <h2>{selectedUser ? '快速切换用户（辅助）' : '临时快速找人（辅助）'}</h2>
                            <p>{selectedUser ? '当前已经在单用户处理页中，如需切换别的用户，可在这里快速跳转。' : '支持按用户ID、用户名、手机号或企微联系方式快速定位用户。'}</p>
                        </div>
                    </div>
                    <div className="search-row">
                        <div className="workbench-search-box">
                            <FiSearch className="search-icon" />
                            <input
                                value={searchKeyword}
                                onChange={(e) => setSearchKeyword(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') {
                                        void handleSearch();
                                    }
                                }}
                                placeholder="请输入用户ID、用户名、手机号或企微手机号"
                                className="workbench-search-input"
                            />
                        </div>
                        <button className="btn-primary" onClick={() => void handleSearch()} disabled={searching}>
                            {searching ? '搜索中...' : '搜索'}
                        </button>
                    </div>
                    {recentUsers.length > 0 && (
                        <div className="recent-users-row">
                            <span className="recent-users-label">最近访问</span>
                            <div className="recent-users-list">
                                {recentUsers.map((item) => (
                                    <button
                                        key={item.id}
                                        className="recent-user-chip"
                                        onClick={() => void loadWorkbenchUser(item.id)}
                                    >
                                        {item.username} · {item.id}
                                    </button>
                                ))}
                            </div>
                        </div>
                    )}
                    {searchResults.length > 0 && (
                        <div className="search-results-list">
                            {searchResults.map((user) => (
                                <button
                                    key={user.id}
                                    className="search-result-item"
                                    onClick={() => void loadWorkbenchUser(String(user.id))}
                                >
                                    <div className="search-result-main">
                                        <strong>{getUserDisplayName(user)}</strong>
                                        <span>ID {user.id}</span>
                                    </div>
                                    <div className="search-result-meta">
                                        <span>{user.username}</span>
                                        <FiChevronRight />
                                    </div>
                                </button>
                            ))}
                        </div>
                    )}
                </div>

                {!selectedUser ? (
                    <div className="section-card empty-panel compact-empty-panel">
                        <h3>还没有进入具体用户</h3>
                        <p>你可以返回总控台看待处理队列，或回到用户管理列表点击某个用户进入用户360。</p>
                    </div>
                ) : (
                    <>
                        <div className="section-card user-main-card">
                            <div className="user-main-left">
                                <div className="user-avatar-large">
                                    {selectedUser.avatar ? <img src={selectedUser.avatar} alt={getUserDisplayName(selectedUser)} /> : getUserAvatarText(selectedUser)}
                                </div>
                                <div className="user-main-info">
                                    <div className="user-main-title-row">
                                        <h2>{getUserDisplayName(selectedUser)}</h2>
                                        <span className="user-main-id">ID {selectedUser.id}</span>
                                    </div>
                                    <div className="user-main-meta">
                                        <span>{selectedUser.user_type || '普通用户'}</span>
                                        <span>{formatDateTime(selectedUser.created_at)} 注册</span>
                                        <span>{formatDateTime(selectedUser.updated_at)} 最近更新</span>
                                    </div>
                                    <div className="status-tag-list">
                                        <span className="status-tag warm">
                                            <FiZap />
                                            灵石 {selectedUser.stones || 0}
                                        </span>
                                        <span className={`status-tag ${enterpriseWechatVerified ? 'success' : 'warning'}`}>
                                            {enterpriseWechatVerified ? <FiCheckCircle /> : <FiClock />}
                                            {enterpriseWechatVerified ? '企微已验证' : '下载权限待核验'}
                                        </span>
                                        <span className={`status-tag ${activeCertification?.status === 'pending_review' || activeCertification?.status === 'pending_payment' ? 'danger' : activeCertification ? 'success' : 'neutral'}`}>
                                            {activeCertification?.status === 'pending_review' || activeCertification?.status === 'pending_payment' ? <FiAlertCircle /> : <FiShield />}
                                            {activeCertification
                                                    ? CERT_STATUS_MAP[activeCertification.status] || activeCertification.status
                                                    : '暂无认证'}
                                        </span>
                                        <span className="status-tag neutral">
                                            <FiUser />
                                            {selectedUser.username || `用户${selectedUser.id}`}
                                        </span>
                                    </div>
                                </div>
                            </div>
                            <div className="user-main-right">
                                <button className="btn-secondary" onClick={() => void handleRefresh()} disabled={loading}>
                                    <FiRefreshCw />
                                    刷新数据
                                </button>
                            </div>
                        </div>

                        <div className="content-grid">
                            <div className="content-main">
                                <div className="section-card">
                                    <div className="section-head">
                                        <div>
                                            <h3>当前待处理事项</h3>
                                            <p>主页面只显示最值得优先处理的事项，避免信息过载。</p>
                                        </div>
                                    </div>
                                    {pendingItems.length === 0 ? (
                                        <div className="empty-inline success-state">
                                            <FiCheckCircle />
                                            当前没有紧急待处理事项，用户状态整体正常。
                                        </div>
                                    ) : (
                                        <div className="pending-list">
                                            {pendingItems.map((item, index) => (
                                                <div key={item.key} className={`pending-item ${item.tone}`}>
                                                    <div className="pending-item-index">{index + 1}</div>
                                                    <div className="pending-item-content">
                                                        <div className="pending-item-title">{item.title}</div>
                                                        <div className="pending-item-desc">{item.description}</div>
                                                    </div>
                                                    <button className="btn-inline" onClick={item.action}>
                                                        {item.actionLabel}
                                                    </button>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>

                                <div className="summary-grid">
                                    {summaryCards.map((card) => (
                                        <div key={card.key} className="section-card summary-card">
                                            <div className="summary-card-title">{card.title}</div>
                                            <div className="summary-card-value">{card.value}</div>
                                            <div className="summary-card-desc">{card.description}</div>
                                            <button
                                                className="btn-link-action"
                                                onClick={card.onClick}
                                                disabled={card.disabled}
                                            >
                                                {card.actionLabel}
                                                {!card.disabled && <FiExternalLink />}
                                            </button>
                                        </div>
                                    ))}
                                </div>

                                <div className="detail-tab-row">
                                    <button className={`detail-tab-button ${activeDetailTab === 'membership' ? 'active' : ''}`} onClick={() => setActiveDetailTab('membership')}>会员信息</button>
                                    <button className={`detail-tab-button ${activeDetailTab === 'orders' ? 'active' : ''}`} onClick={() => setActiveDetailTab('orders')}>最近订单</button>
                                    <button className={`detail-tab-button ${activeDetailTab === 'tasks' ? 'active' : ''}`} onClick={() => setActiveDetailTab('tasks')}>最近任务</button>
                                    <button className={`detail-tab-button ${activeDetailTab === 'stones' ? 'active' : ''}`} onClick={() => setActiveDetailTab('stones')}>灵石流水</button>
                                    <button className={`detail-tab-button ${activeDetailTab === 'risk' ? 'active' : ''}`} onClick={() => setActiveDetailTab('risk')}>风险信息</button>
                                </div>

                                {activeDetailTab === 'membership' && (
                                    <div className="section-card detail-section-card">
                                        <div className="section-head compact">
                                            <div>
                                                <h3>会员信息</h3>
                                                <p>看当前会员状态、下载权限和会员来源订单。</p>
                                            </div>
                                        </div>
                                        {workbenchSummary?.membership ? (
                                            <div className="detail-list">
                                                <div className="detail-list-row"><span>会员计划</span><strong>{workbenchSummary.membership.plan_title}</strong></div>
                                                <div className="detail-list-row"><span>当前状态</span><strong>{MEMBERSHIP_STATUS_MAP[workbenchSummary.membership.status] || workbenchSummary.membership.status}</strong></div>
                                                <div className="detail-list-row"><span>下载权限</span><strong>{workbenchSummary.membership.template_download_enabled ? '已开启' : '未开启'}</strong></div>
                                                <div className="detail-list-row"><span>来源订单</span><strong>{workbenchSummary.membership.source_order_no || '暂无'}</strong></div>
                                                <div className="detail-list-row"><span>有效期</span><strong>{workbenchSummary.membership.is_lifetime ? '长期有效' : formatDateTime(workbenchSummary.membership.expired_at)}</strong></div>
                                            </div>
                                        ) : (
                                            <div className="empty-inline">当前用户还没有会员记录。</div>
                                        )}
                                    </div>
                                )}

                                {activeDetailTab === 'orders' && (
                                    <div className="section-card detail-section-card">
                                        <div className="section-head compact">
                                            <div>
                                                <h3>最近订单</h3>
                                                <p>方便快速判断用户最近是充值、消费还是认证费。</p>
                                            </div>
                                        </div>
                                        {(workbenchSummary?.recent_orders || []).length === 0 ? (
                                            <div className="empty-inline">当前用户暂无订单记录。</div>
                                        ) : (
                                            <div className="mini-record-list">
                                                {(workbenchSummary?.recent_orders || []).map((item) => (
                                                    <div key={item.id} className="mini-record-item">
                                                        <div className="mini-record-main">
                                                            <strong>{item.title || item.order_no}</strong>
                                                            <span>{ORDER_TYPE_MAP[item.type] || item.type} · {ORDER_STATUS_MAP[item.status] || item.status}</span>
                                                        </div>
                                                        <div className="mini-record-side">
                                                            <strong>{formatOrderAmount(item.amount)}</strong>
                                                            <span>{formatDateTime(item.created_at)}</span>
                                                        </div>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                )}

                                {activeDetailTab === 'tasks' && (
                                    <div className="section-card detail-section-card">
                                        <div className="section-head compact">
                                            <div>
                                                <h3>最近任务</h3>
                                                <p>图片任务和视频任务会合并展示，便于排查失败。</p>
                                            </div>
                                        </div>
                                        {(workbenchSummary?.recent_tasks || []).length === 0 ? (
                                            <div className="empty-inline">当前用户暂无任务记录。</div>
                                        ) : (
                                            <div className="mini-record-list">
                                                {(workbenchSummary?.recent_tasks || []).map((item) => (
                                                    <div key={item.task_no} className="mini-record-item">
                                                        <div className="mini-record-main">
                                                            <strong>{item.task_type === 'video' ? '视频任务' : item.scene}</strong>
                                                            <span>{TASK_STATUS_MAP[item.status] || item.status} · {item.task_no}</span>
                                                            {item.error_message ? <em>{item.error_message}</em> : null}
                                                        </div>
                                                        <div className="mini-record-side">
                                                            <strong>{item.stones_used} 灵石</strong>
                                                            <span>{formatDateTime(item.created_at)}</span>
                                                        </div>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                )}

                                {activeDetailTab === 'stones' && (
                                    <div className="section-card detail-section-card">
                                        <div className="section-head compact">
                                            <div>
                                                <h3>灵石流水</h3>
                                                <p>看最近变动、近30天消耗和累计签到获得。</p>
                                            </div>
                                        </div>
                                        <div className="stone-overview-grid">
                                            <div className="stone-overview-card"><span>当前灵石</span><strong>{workbenchSummary?.stone_summary.current_stones ?? selectedUser.stones ?? 0}</strong></div>
                                            <div className="stone-overview-card"><span>近30天获得</span><strong>{workbenchSummary?.stone_summary.recent_gain || 0}</strong></div>
                                            <div className="stone-overview-card"><span>近30天消耗</span><strong>{workbenchSummary?.stone_summary.recent_consume || 0}</strong></div>
                                            <div className="stone-overview-card"><span>累计签到获得</span><strong>{workbenchSummary?.stone_summary.checkin_total || 0}</strong></div>
                                        </div>
                                        {(workbenchSummary?.stone_records || []).length === 0 ? (
                                            <div className="empty-inline">当前用户暂无灵石流水。</div>
                                        ) : (
                                            <div className="mini-record-list compact">
                                                {(workbenchSummary?.stone_records || []).map((item) => (
                                                    <div key={item.id} className="mini-record-item">
                                                        <div className="mini-record-main">
                                                            <strong>{STONE_TYPE_MAP[item.type] || item.type}</strong>
                                                            <span>{item.scene_desc || item.remark || '暂无说明'}</span>
                                                        </div>
                                                        <div className="mini-record-side">
                                                            <strong>{formatStoneRecordAmount({ amount: item.amount, type: item.type })}</strong>
                                                            <span>{formatDateTime(item.created_at)}</span>
                                                        </div>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                )}

                                {activeDetailTab === 'risk' && (
                                    <div className="section-card detail-section-card">
                                        <div className="section-head compact">
                                            <div>
                                                <h3>设备与风险信息</h3>
                                                <p>看设备绑定、同设备账号和风险标签，便于快速判断是否要进风控台继续排查。</p>
                                            </div>
                                        </div>
                                        <div className="risk-card-header">
                                            <strong>当前风险等级</strong>
                                            <span className={`risk-level-pill ${workbenchSummary?.device_risk?.risk_level || 'low'}`}>
                                                {RISK_LEVEL_MAP[workbenchSummary?.device_risk?.risk_level || 'low']}
                                            </span>
                                        </div>
                                        {(workbenchSummary?.device_risk?.risk_tags || []).length > 0 ? (
                                            <div className="risk-tag-list">
                                                {(workbenchSummary?.device_risk?.risk_tags || []).map((tag) => (
                                                    <span key={tag} className="risk-tag">{RISK_TAG_MAP[tag] || tag}</span>
                                                ))}
                                            </div>
                                        ) : (
                                            <div className="empty-inline success-state">当前没有明显设备风险标签。</div>
                                        )}
                                        <div className="detail-list">
                                            <div className="detail-list-row"><span>设备标识</span><strong>{formatDeviceText(workbenchSummary?.device_risk?.device_id)}</strong></div>
                                            <div className="detail-list-row"><span>首次绑定时间</span><strong>{formatDateTime(workbenchSummary?.device_risk?.device_bind_time)}</strong></div>
                                            <div className="detail-list-row"><span>最近换绑时间</span><strong>{formatDateTime(workbenchSummary?.device_risk?.last_device_change_time)}</strong></div>
                                            <div className="detail-list-row"><span>同设备账号数</span><strong>{workbenchSummary?.device_risk?.same_device_account_count || 0}</strong></div>
                                            <div className="detail-list-row"><span>密码状态</span><strong>{workbenchSummary?.device_risk?.has_password ? '已设置' : '未设置'}</strong></div>
                                        </div>
                                        {(workbenchSummary?.device_risk?.same_device_other_users || []).length > 0 && (
                                            <div className="risk-peer-list">
                                                {(workbenchSummary?.device_risk?.same_device_other_users || []).map((item) => (
                                                    <div key={item.user_id} className="risk-peer-row">
                                                        <div className="risk-peer-main">
                                                            <strong>{item.display_name || item.username || `用户${item.user_id}`}</strong>
                                                            <span>ID {item.user_id} · {item.username || '未命名用户'}</span>
                                                        </div>
                                                        <button className="btn-inline" onClick={() => void handleOpenRelatedUser(item.user_id)}>切到该用户</button>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                        <div className="risk-action-row">
                                            <button className="btn-secondary" onClick={() => navigate('/risk-control')}>进入风控台</button>
                                        </div>
                                    </div>
                                )}
                            </div>

                            <div className="content-side">
                                <div className="section-card">
                                    <div className="section-head compact">
                                        <div>
                                            <h3>高频操作</h3>
                                            <p>只保留最常用的处理动作。</p>
                                        </div>
                                    </div>
                                    <div className="action-grid">
                                        <button className="action-button primary" onClick={handleOpenStonesModal}>
                                            <FiZap />
                                            <span>调整灵石</span>
                                        </button>
                                        <button className="action-button" onClick={() => void handleOpenEnterpriseWechatModal()}>
                                            <FiShield />
                                            <span>修正下载权限</span>
                                        </button>
                                        <button className="action-button" onClick={() => void handleOpenCertification()}>
                                            <FiUser />
                                            <span>查看认证</span>
                                        </button>
                                        <button className="action-button" onClick={() => navigate('/recharge')}>
                                            <FiExternalLink />
                                            <span>查看订单中心</span>
                                        </button>
                                    </div>
                                </div>

                                <div className="section-card">
                                    <div className="section-head compact">
                                        <div>
                                            <h3>账号与安全</h3>
                                            <p>集中处理登录用户名、登录密码、手机号授权状态和账号风险提醒。</p>
                                        </div>
                                    </div>
                                    <div className="account-security-list">
                                        <div className="account-security-row">
                                            <span>登录用户名</span>
                                            <strong>{selectedUser.username || '未设置'}</strong>
                                        </div>
                                        <div className="account-security-row">
                                            <span>登录密码</span>
                                            <strong>{workbenchSummary?.device_risk?.has_password ? '已设置' : '未设置'}</strong>
                                        </div>
                                        <div className="account-security-row">
                                            <span>手机号授权状态</span>
                                            <strong>{enterpriseWechatVerified && enterpriseWechatContact ? `${enterpriseWechatContact}（已授权）` : '未授权'}</strong>
                                        </div>
                                        <div className="account-security-row align-start">
                                            <span>账号风险提示</span>
                                            <div className="account-security-risk-block">
                                                <span className={`risk-level-pill ${workbenchSummary?.device_risk?.risk_level || 'low'}`}>
                                                    {RISK_LEVEL_MAP[workbenchSummary?.device_risk?.risk_level || 'low']}
                                                </span>
                                                {(workbenchSummary?.device_risk?.risk_tags || []).length > 0 ? (
                                                    <div className="account-security-risk-tags">
                                                        {(workbenchSummary?.device_risk?.risk_tags || []).map((tag) => (
                                                            <span key={tag} className="risk-tag">{RISK_TAG_MAP[tag] || tag}</span>
                                                        ))}
                                                    </div>
                                                ) : (
                                                    <span className="account-security-risk-empty">当前没有明显风险标签</span>
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                    <div className="account-security-actions">
                                        <button className="btn-primary" onClick={handleOpenAccountSecurityModal}>
                                            修改用户名 / 重置密码
                                        </button>
                                        <button className="btn-secondary" onClick={() => void handleOpenEnterpriseWechatModal()}>
                                            修正手机号授权
                                        </button>
                                        <button className="btn-secondary" onClick={() => setActiveDetailTab('risk')}>
                                            查看完整风险详情
                                        </button>
                                    </div>
                                </div>

                                <div className="section-card compact-note-card">
                                    <div className="section-head compact">
                                        <div>
                                            <h3>当前主页面字段摘要</h3>
                                        </div>
                                    </div>
                                    <div className="mini-field-list">
                                        <div>身份：用户ID、显示名、用户名、用户类型</div>
                                        <div>状态：灵石、企微验证、认证状态、会员状态、设备风险、最近更新时间</div>
                                        <div>操作：调整灵石、修正权限、查看认证、刷新数据</div>
                                        <div>摘要：待处理事项、任务/订单/会员摘要、灵石流水、设备与风险信息</div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </>
                )}

                {showAccountSecurityModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowAccountSecurityModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>账号与安全</h3>
                                <button className="modal-close" onClick={() => setShowAccountSecurityModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="account-security-form">
                                    <div className="summary-banner">
                                        <div className="summary-banner-row">
                                            <span>用户：{getUserDisplayName(selectedUser)}</span>
                                            <span className={`risk-level-pill ${workbenchSummary?.device_risk?.risk_level || 'low'}`}>
                                                {RISK_LEVEL_MAP[workbenchSummary?.device_risk?.risk_level || 'low']}
                                            </span>
                                        </div>
                                        <div className="summary-banner-meta">用户ID：{selectedUser.id}</div>
                                        <div className="summary-banner-meta">手机号授权：{enterpriseWechatVerified && enterpriseWechatContact ? enterpriseWechatContact : '未授权'}</div>
                                    </div>
                                    <div className="form-group">
                                        <label>登录用户名</label>
                                        <input
                                            className="form-input"
                                            value={accountUsername}
                                            onChange={(e) => setAccountUsername(e.target.value)}
                                            placeholder="请输入登录用户名"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>新密码</label>
                                        <input
                                            type="password"
                                            className="form-input"
                                            value={accountPassword}
                                            onChange={(e) => setAccountPassword(e.target.value)}
                                            placeholder="留空则不修改密码"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>确认新密码</label>
                                        <input
                                            type="password"
                                            className="form-input"
                                            value={accountPasswordConfirm}
                                            onChange={(e) => setAccountPasswordConfirm(e.target.value)}
                                            placeholder="再次输入新密码"
                                        />
                                    </div>
                                    <div className="account-security-hint">
                                        如只需修改用户名，可将新密码留空；如需重置登录密码，请输入两次一致的新密码。
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowAccountSecurityModal(false)}>
                                    取消
                                </button>
                                <button className="btn-primary" onClick={handleAccountSecuritySubmit} disabled={accountSecuritySaving}>
                                    {accountSecuritySaving ? '保存中...' : '保存账号与安全信息'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {showCertificationDetailModal && (
                    <div className="modal-overlay" onClick={() => setShowCertificationDetailModal(false)}>
                        <div className="modal-content detail-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>认证详情</h3>
                                <button className="modal-close" onClick={() => setShowCertificationDetailModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                {certificationDetailLoading ? (
                                    <div className="empty-inline">正在加载认证详情...</div>
                                ) : certificationDetail ? (
                                    <div className="detail-info-list">
                                        <div className="info-row">
                                            <span className="info-label">认证类型</span>
                                            <span className="info-value">{CERT_TYPE_MAP[certificationDetail.application.type] || certificationDetail.application.type}</span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">状态</span>
                                            <span className={`status-pill ${certificationDetail.application.status}`}>
                                                {CERT_STATUS_MAP[certificationDetail.application.status] || certificationDetail.application.status}
                                            </span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">真实姓名</span>
                                            <span className="info-value">{certificationDetail.application.real_name || '-'}</span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">企业名称</span>
                                            <span className="info-value">{certificationDetail.application.company_name || '-'}</span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">阿里云核验</span>
                                            <span className="info-value">
                                                {certificationDetail.application.aliyun_passed ? '通过' : '未通过'}
                                                {certificationDetail.application.aliyun_msg ? `（${certificationDetail.application.aliyun_msg}）` : ''}
                                            </span>
                                        </div>
                                        <div className="info-row full">
                                            <span className="info-label">补充说明</span>
                                            <span className="info-value">{certificationDetail.application.extra_docs_remark || '暂无说明'}</span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">申请时间</span>
                                            <span className="info-value">{formatDateTime(certificationDetail.application.created_at)}</span>
                                        </div>
                                        {certificationDetail.application.admin_remark && (
                                            <div className="info-row full">
                                                <span className="info-label">审核备注</span>
                                                <span className="info-value">{certificationDetail.application.admin_remark}</span>
                                            </div>
                                        )}
                                        {certificationDetail.application.status === 'pending_review' && (
                                            <div className="detail-actions">
                                                <button
                                                    className="btn-primary"
                                                    onClick={() => handleOpenReview(certificationDetail.application, 'approve')}
                                                >
                                                    通过认证
                                                </button>
                                                <button
                                                    className="btn-danger"
                                                    onClick={() => handleOpenReview(certificationDetail.application, 'reject')}
                                                >
                                                    拒绝认证
                                                </button>
                                            </div>
                                        )}
                                    </div>
                                ) : (
                                    <div className="empty-inline">暂无认证详情</div>
                                )}
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowCertificationDetailModal(false)}>
                                    关闭
                                </button>
                            </div>
                        </div>
                    </div>
                )}

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
                                    当前处理：{CERT_TYPE_MAP[reviewTarget.type] || reviewTarget.type}（用户ID：{reviewTarget.user_id}）
                                </p>
                                <div className="form-group">
                                    <label>审核备注</label>
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

                {showEnterpriseWechatModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowEnterpriseWechatModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>修正下载权限</h3>
                                <button className="modal-close" onClick={() => setShowEnterpriseWechatModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                {enterpriseWechatLoading ? (
                                    <div className="empty-inline">正在加载授权状态...</div>
                                ) : (
                                    <div className="enterprise-wechat-form">
                                        <div className="summary-banner">
                                            <div className="summary-banner-row">
                                                <span>用户：{getUserDisplayName(selectedUser)}</span>
                                                <span className={`status-pill ${enterpriseWechatVerified ? 'approved' : 'pending_review'}`}>
                                                    {enterpriseWechatVerified ? '已授权' : '待授权'}
                                                </span>
                                            </div>
                                            <div className="summary-banner-meta">用户ID：{selectedUser.id}</div>
                                            <div className="summary-banner-meta">授权时间：{enterpriseWechatVerifiedAtText || '暂无'}</div>
                                        </div>
                                        <div className="form-group">
                                            <label>授权状态</label>
                                            <div className="mode-toggle">
                                                <button
                                                    className={`mode-btn ${!enterpriseWechatVerified ? 'active' : ''}`}
                                                    onClick={() => setEnterpriseWechatVerified(false)}
                                                >
                                                    待授权
                                                </button>
                                                <button
                                                    className={`mode-btn ${enterpriseWechatVerified ? 'active' : ''}`}
                                                    onClick={() => setEnterpriseWechatVerified(true)}
                                                >
                                                    已授权
                                                </button>
                                            </div>
                                        </div>
                                        <div className="form-group">
                                            <label>授权手机号</label>
                                            <input
                                                className="form-input"
                                                value={enterpriseWechatContact}
                                                onChange={(e) => setEnterpriseWechatContact(e.target.value)}
                                                placeholder="请输入用户授权后的手机号"
                                            />
                                        </div>
                                    </div>
                                )}
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowEnterpriseWechatModal(false)}>
                                    取消
                                </button>
                                <button
                                    className="btn-primary"
                                    onClick={handleEnterpriseWechatSubmit}
                                    disabled={enterpriseWechatLoading || enterpriseWechatSaving}
                                >
                                    {enterpriseWechatSaving ? '保存中...' : '保存权限状态'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {showStonesModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowStonesModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>调整灵石</h3>
                                <button className="modal-close" onClick={() => setShowStonesModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="stones-form">
                                    <div className="summary-banner">
                                        <div className="summary-banner-row">
                                            <span>用户：{getUserDisplayName(selectedUser)}</span>
                                            <span className="highlight-number">当前灵石 {selectedUser.stones || 0}</span>
                                        </div>
                                    </div>
                                    <div className="form-group">
                                        <label>操作类型</label>
                                        <div className="mode-toggle">
                                            <button
                                                className={`mode-btn ${stonesMode === 'adjust' ? 'active' : ''}`}
                                                onClick={() => setStonesMode('adjust')}
                                            >
                                                增减灵石
                                            </button>
                                            <button
                                                className={`mode-btn ${stonesMode === 'set' ? 'active' : ''}`}
                                                onClick={() => setStonesMode('set')}
                                            >
                                                直接设置余额
                                            </button>
                                        </div>
                                    </div>
                                    {stonesMode === 'adjust' && (
                                        <div className="form-group">
                                            <label>快捷调整</label>
                                            <div className="quick-buttons">
                                                <button className="quick-btn add" onClick={() => handleQuickAdjust(100)}>+100</button>
                                                <button className="quick-btn add" onClick={() => handleQuickAdjust(500)}>+500</button>
                                                <button className="quick-btn add" onClick={() => handleQuickAdjust(1000)}>+1000</button>
                                                <button className="quick-btn minus" onClick={() => handleQuickAdjust(-100)}>-100</button>
                                                <button className="quick-btn minus" onClick={() => handleQuickAdjust(-500)}>-500</button>
                                            </div>
                                        </div>
                                    )}
                                    <div className="form-group">
                                        <label>{stonesMode === 'set' ? '新余额' : '调整数量'}</label>
                                        <input
                                            type="number"
                                            className="form-input"
                                            value={stonesValue}
                                            onChange={(e) => setStonesValue(e.target.value)}
                                            placeholder={stonesMode === 'set' ? '请输入新的灵石余额' : '正数增加，负数扣除'}
                                        />
                                    </div>
                                    {stonesMode === 'adjust' && stonesValue && !Number.isNaN(parseInt(stonesValue, 10)) && (
                                        <div className="preview-result">
                                            预计修改后余额：
                                            <strong>{(selectedUser.stones || 0) + parseInt(stonesValue, 10)}</strong>
                                        </div>
                                    )}
                                    <div className="form-group">
                                        <label>备注</label>
                                        <input
                                            className="form-input"
                                            value={stonesRemark}
                                            onChange={(e) => setStonesRemark(e.target.value)}
                                            placeholder="请输入备注，例如补偿、活动奖励等"
                                        />
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowStonesModal(false)}>
                                    取消
                                </button>
                                <button className="btn-primary" onClick={handleStonesSubmit} disabled={stonesLoading || !stonesValue}>
                                    {stonesLoading ? '处理中...' : '确认修改'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default UserWorkbench;
