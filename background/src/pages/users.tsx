import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import Layout from '../component/layout';
import {
    getUserList,
    getUserDetail,
    setUserStones,
    adjustUserStones,
    updateUser,
    getUserEnterpriseWechatVerification,
    updateUserEnterpriseWechatVerification,
    type User as ApiUser,
    type UserListParams,
} from '../api/users';
import './users.scss';

interface User {
    id: string;
    username: string;
    pointsBalance: number;
    createdAt: string;
    enterpriseWechatVerified?: boolean;
    enterpriseWechatVerifiedAt?: string;
    enterpriseWechatContact?: string;
    nickname?: string;
    avatar?: string;
    designerBio?: string;
    specialtyStyles?: string;
    designerExperienceYears?: number;
    serviceTitle?: string;
    phone?: string;
    identityType?: string;
}

const pageSizeOptions = [20, 50, 100];

const Users: React.FC = () => {
    const navigate = useNavigate();
    const [searchInput, setSearchInput] = useState('');
    const [searchKeyword, setSearchKeyword] = useState('');
    const [enterpriseWechatFilter, setEnterpriseWechatFilter] = useState<'all' | 'verified' | 'pending'>('all');
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [showStonesModal, setShowStonesModal] = useState(false);
    const [selectedUser, setSelectedUser] = useState<User | null>(null);
    const [users, setUsers] = useState<User[]>([]);
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(pageSizeOptions[0]);
    const [total, setTotal] = useState(0);
    const [reloadSeed, setReloadSeed] = useState(0);
    const [loading, setLoading] = useState(true);
    const [stonesLoading, setStonesLoading] = useState(false);

    // 灵石修改相关状态
    const [stonesMode, setStonesMode] = useState<'set' | 'adjust'>('adjust'); // set: 设置绝对值, adjust: 增减
    const [stonesValue, setStonesValue] = useState<string>('');
    const [stonesRemark, setStonesRemark] = useState('');
    // 用户信息编辑
    const [showEditUserModal, setShowEditUserModal] = useState(false);
    const [editUsername, setEditUsername] = useState('');
    const [editPassword, setEditPassword] = useState('');
    const [editPasswordConfirm, setEditPasswordConfirm] = useState('');
    const [editNickname, setEditNickname] = useState('');
    const [editAvatar, setEditAvatar] = useState('');
    const [editDesignerBio, setEditDesignerBio] = useState('');
    const [editSpecialtyStyles, setEditSpecialtyStyles] = useState('');
    const [editDesignerExperienceYears, setEditDesignerExperienceYears] = useState('0');
    const [editServiceTitle, setEditServiceTitle] = useState('');
    const [editLoading, setEditLoading] = useState(false);
    const [showEnterpriseWechatModal, setShowEnterpriseWechatModal] = useState(false);
    const [enterpriseWechatVerified, setEnterpriseWechatVerified] = useState(false);
    const [enterpriseWechatContact, setEnterpriseWechatContact] = useState('');
    const [enterpriseWechatVerifiedAtText, setEnterpriseWechatVerifiedAtText] = useState('');
    const [enterpriseWechatLoading, setEnterpriseWechatLoading] = useState(false);
    const [enterpriseWechatSaving, setEnterpriseWechatSaving] = useState(false);

    // 转换API数据格式
    const convertUser = (apiUser: ApiUser): User => {
        return {
            id: String(apiUser.id),
            username: apiUser.username || '用户' + apiUser.id,
            pointsBalance: apiUser.stones || 0,
            createdAt: apiUser.created_at,
            enterpriseWechatVerified: !!apiUser.enterprise_wechat_verified,
            enterpriseWechatVerifiedAt: apiUser.enterprise_wechat_verified_at || '',
            enterpriseWechatContact: apiUser.enterprise_wechat_contact || '',
            nickname: apiUser.nickname || '',
            avatar: apiUser.avatar || '',
            designerBio: apiUser.designer_bio || '',
            specialtyStyles: apiUser.specialty_styles || '',
            designerExperienceYears: apiUser.designer_experience_years || 0,
            serviceTitle: apiUser.service_title || '',
            phone: apiUser.phone || '',
            identityType: apiUser.identity_type || '',
        };
    };

    const fetchUsers = async ({
        nextPage = page,
        nextPageSize = pageSize,
        nextKeyword = searchKeyword,
        nextEnterpriseWechatFilter = enterpriseWechatFilter,
    }: {
        nextPage?: number;
        nextPageSize?: number;
        nextKeyword?: string;
        nextEnterpriseWechatFilter?: 'all' | 'verified' | 'pending';
    } = {}) => {
        const params: UserListParams = {
            page: nextPage,
            page_size: nextPageSize,
        };
        if (nextKeyword) params.keyword = nextKeyword;
        if (nextEnterpriseWechatFilter !== 'all') {
            params.enterprise_wechat_status = nextEnterpriseWechatFilter;
        }
        const response = await getUserList(params);
        setUsers(response.list.map(convertUser));
        setTotal(response.total || 0);
    };

    const refreshUsers = () => {
        setReloadSeed((value) => value + 1);
    };

    // 加载用户列表
    useEffect(() => {
        const loadUsers = async () => {
            setLoading(true);
            try {
                await fetchUsers();
            } catch (error) {
                console.error('加载用户列表失败:', error);
                alert('加载用户列表失败');
            } finally {
                setLoading(false);
            }
        };
        void loadUsers();
    }, [page, pageSize, searchKeyword, enterpriseWechatFilter, reloadSeed]);

    const handleSearch = () => {
        const nextKeyword = searchInput.trim();
        if (page !== 1) {
            setPage(1);
        }
        if (nextKeyword !== searchKeyword) {
            setSearchKeyword(nextKeyword);
            return;
        }
        if (page === 1) {
            refreshUsers();
        }
    };

    const handleResetFilters = () => {
        setSearchInput('');
        setSearchKeyword('');
        setEnterpriseWechatFilter('all');
        setPage(1);
        setPageSize(pageSizeOptions[0]);
        if (searchKeyword === '' && enterpriseWechatFilter === 'all' && page === 1 && pageSize === pageSizeOptions[0]) {
            refreshUsers();
        }
    };

    const totalPages = Math.max(1, Math.ceil(total / pageSize) || 1);
    const currentStart = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const currentEnd = total === 0 ? 0 : Math.min(page * pageSize, total);
    const currentPageCount = users.length;
    const currentPageVerifiedCount = users.filter((user) => !!user.enterpriseWechatVerified).length;
    const currentPagePendingCount = users.filter((user) => !user.enterpriseWechatVerified).length;
    const currentPageStones = users.reduce((sum, user) => sum + user.pointsBalance, 0);

    const handleEnterpriseWechatFilterChange = (value: 'all' | 'verified' | 'pending') => {
        setEnterpriseWechatFilter(value);
        setPage(1);
    };

    const handlePageSizeChange = (value: number) => {
        setPageSize(value);
        setPage(1);
    };

    const handlePrevPage = () => {
        if (page <= 1) return;
        setPage(page - 1);
    };

    const handleNextPage = () => {
        if (page >= totalPages) return;
        setPage(page + 1);
    };

    // 处理查看详情
    const handleViewDetail = (user: User) => {
        setSelectedUser(user);
        setShowDetailModal(true);
    };

    // 打开编辑用户信息弹窗
    const handleOpenEditUserModal = async (user: User) => {
        try {
            const detail = await getUserDetail(user.id);
            setSelectedUser(detail ? convertUser(detail) : user);
            setEditUsername(detail?.username || user.username);
            setEditPassword('');
            setEditPasswordConfirm('');
            setEditNickname(detail?.nickname || '');
            setEditAvatar(detail?.avatar || '');
            setEditDesignerBio(detail?.designer_bio || '');
            setEditSpecialtyStyles(detail?.specialty_styles || '');
            setEditDesignerExperienceYears(String(detail?.designer_experience_years || 0));
            setEditServiceTitle(detail?.service_title || '');
            setShowEditUserModal(true);
        } catch (e) {
            console.error('获取用户详情失败:', e);
            alert('获取用户详情失败');
        }
    };

    // 处理打开灵石修改弹窗
    const handleOpenStonesModal = (user: User) => {
        setSelectedUser(user);
        setStonesMode('adjust');
        setStonesValue('');
        setStonesRemark('');
        setShowStonesModal(true);
    };

    const handleOpenEnterpriseWechatModal = async (user: User) => {
        setSelectedUser(user);
        setEnterpriseWechatLoading(true);
        setShowEnterpriseWechatModal(true);
        try {
            const detail = await getUserEnterpriseWechatVerification(user.id);
            setEnterpriseWechatVerified(!!detail?.enterprise_wechat_verified);
            setEnterpriseWechatContact(detail?.enterprise_wechat_contact || '');
            setEnterpriseWechatVerifiedAtText(detail?.enterprise_wechat_verified_at || '');
        } catch (error) {
            console.error('获取手机号授权验证状态失败:', error);
            alert('获取手机号授权验证状态失败');
            setShowEnterpriseWechatModal(false);
        } finally {
            setEnterpriseWechatLoading(false);
        }
    };

    const handleEnterpriseWechatSubmit = async () => {
        if (!selectedUser) return;
        const contact = enterpriseWechatContact.trim();
        if (enterpriseWechatVerified && !contact) {
            alert('手机号授权通过时必须填写手机号');
            return;
        }
        setEnterpriseWechatSaving(true);
        try {
            const result = await updateUserEnterpriseWechatVerification(selectedUser.id, {
                verified: enterpriseWechatVerified,
                contact,
            });
            setEnterpriseWechatVerified(!!result?.enterprise_wechat_verified);
            setEnterpriseWechatContact(result?.enterprise_wechat_contact || contact);
            setEnterpriseWechatVerifiedAtText(result?.enterprise_wechat_verified_at || '');
            alert('手机号授权状态已保存');
            refreshUsers();
            setShowEnterpriseWechatModal(false);
        } catch (error: any) {
            console.error('保存手机号授权状态失败:', error);
            alert(error?.message || '保存手机号授权状态失败');
        } finally {
            setEnterpriseWechatSaving(false);
        }
    };

    // 处理灵石修改
    const handleStonesSubmit = async () => {
        if (!selectedUser) return;

        const value = parseInt(stonesValue);
        if (isNaN(value)) {
            alert('请输入有效的数字');
            return;
        }

        if (stonesMode === 'set' && value < 0) {
            alert('灵石余额不能为负数');
            return;
        }

        if (stonesMode === 'adjust' && value === 0) {
            alert('调整数量不能为0');
            return;
        }

        setStonesLoading(true);
        try {
            if (stonesMode === 'set') {
                await setUserStones(selectedUser.id, {
                    stones: value,
                    remark: stonesRemark || undefined,
                });
            } else {
                await adjustUserStones(selectedUser.id, {
                    amount: value,
                    remark: stonesRemark || undefined,
                });
            }

            alert('修改成功');
            setShowStonesModal(false);

            // 刷新用户列表
            refreshUsers();
        } catch (error: any) {
            console.error('修改灵石失败:', error);
            alert(error.message || '修改灵石失败');
        } finally {
            setStonesLoading(false);
        }
    };

    // 快捷调整
    const handleQuickAdjust = (amount: number) => {
        setStonesMode('adjust');
        setStonesValue(String(amount));
    };



    return (
        <Layout title="用户管理">
            <div className="users-container">
                <div className="users-toolbar section-card">
                    <div className="toolbar-top">
                        <div className="toolbar-title-block">
                            <h3>用户检索与工作台入口</h3>
                            <p>这里主要负责找人、筛人和判断优先级。真正的订单、任务、灵石、认证与风控处理，请进入用户360工作台完成。</p>
                        </div>
                        <div className="toolbar-actions">
                            <button className="btn-secondary" onClick={refreshUsers} disabled={loading}>
                                {loading ? '刷新中...' : '刷新数据'}
                            </button>
                            <button className="btn-secondary" onClick={handleResetFilters}>
                                重置筛选
                            </button>
                        </div>
                    </div>
                    <div className="toolbar-left">
                        <div className="search-box">
                            <span className="search-icon">🔍</span>
                            <input
                                type="text"
                                placeholder="搜索用户名、用户ID或手机号..."
                                value={searchInput}
                                onChange={(e) => setSearchInput(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') {
                                        handleSearch();
                                    }
                                }}
                                className="search-input"
                            />
                        </div>
                        <div className="filters">
                            <select
                                className="filter-select"
                                value={enterpriseWechatFilter}
                                onChange={(e) => handleEnterpriseWechatFilterChange(e.target.value as 'all' | 'verified' | 'pending')}
                            >
                                <option value="all">手机号授权：全部</option>
                                <option value="verified">仅看已授权</option>
                                <option value="pending">仅看待授权</option>
                            </select>
                        </div>
                    </div>
                    <div className="toolbar-footer">
                        <div className="toolbar-summary">
                            <span className="summary-tag">第 {page} / {totalPages} 页 · 当前 {currentStart}-{currentEnd} / {total}</span>
                            当前显示 <strong>{currentPageCount}</strong> 条，本页范围 {currentStart}-{currentEnd} / {total}
                            {searchKeyword ? <span className="summary-tag">关键词：{searchKeyword}</span> : null}
                            {enterpriseWechatFilter !== 'all' ? <span className="summary-tag">手机号授权：{enterpriseWechatFilter === 'verified' ? '已授权' : '待授权'}</span> : null}
                            <span className="summary-note">建议：确认目标用户后，优先进入用户360继续处理</span>
                        </div>
                        <button className="btn-primary" onClick={handleSearch}>搜索用户</button>
                    </div>
                </div>

                <div className="users-stats-note">除总用户数外，其余统计按当前页计算。</div>

                <div className="users-stats">
                    <div className="stat-item">
                        <span className="stat-label">用户总数</span>
                        <span className="stat-value">{total}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">手机号已授权</span>
                        <span className="stat-value">{currentPageVerifiedCount}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">手机号待授权验证</span>
                        <span className="stat-value">{currentPagePendingCount}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">总灵石</span>
                        <span className="stat-value">{currentPageStones}</span>
                    </div>
                </div>

                <div className="users-table-container">
                    <table className="users-table">
                        <thead>
                            <tr>
                                <th>用户ID</th>
                                <th>用户名</th>
                                <th>手机号</th>
                                <th>身份类型</th>
                                <th>手机号授权</th>
                                <th>灵石余额</th>
                                <th>创建时间</th>
                                <th>工作台入口</th>
                            </tr>
                        </thead>
                        <tbody>
                            {users.length === 0 ? (
                                <tr>
                                    <td colSpan={8} className="empty-state">
                                        暂无用户数据
                                    </td>
                                </tr>
                            ) : (
                                users.map((user) => (
                                    <tr key={user.id}>
                                        <td>{user.id}</td>
                                        <td>
                                            <button
                                                className="user-link-button"
                                                onClick={() => navigate(`/user-workbench?userId=${user.id}`)}
                                                title="进入用户360工作台"
                                            >
                                                {user.username}
                                            </button>
                                        </td>
                                        <td>
                                            <span className="user-phone-text">{user.phone || '-'}</span>
                                        </td>
                                        <td>
                                            <span className="identity-type-badge">{user.identityType || '-'}</span>
                                        </td>
                                        <td>
                                            <div className="wechat-status-cell">
                                                <span className={`status-badge ${user.enterpriseWechatVerified ? 'status-verified' : 'status-pending'}`}>
                                                    {user.enterpriseWechatVerified ? '已授权' : '待授权'}
                                                </span>
                                                <span className="wechat-status-meta">
                                                    {user.enterpriseWechatContact || (user.enterpriseWechatVerified ? '已留手机号' : '手机号待授权验证')}
                                                </span>
                                            </div>
                                        </td>
                                        <td>
                                            <span className="points-balance">
                                                <span style={{ display: 'inline', marginRight: '4px', verticalAlign: 'middle' }}>✦</span>
                                                {user.pointsBalance}
                                            </span>
                                        </td>
                                        <td>{new Date(user.createdAt).toLocaleString('zh-CN')}</td>
                                        <td>
                                            <div className="action-buttons">
                                                <button
                                                    className="btn-action btn-enter-workbench"
                                                    onClick={() => navigate(`/user-workbench?userId=${user.id}`)}
                                                    title="进入用户360工作台"
                                                >
                                                    <span>进入360</span>
                                                </button>
                                                <button
                                                    className="btn-action btn-view"
                                                    onClick={() => handleViewDetail(user)}
                                                    title="查看当前列表摘要"
                                                >
                                                    <span>列表摘要</span>
                                                </button>
                                            </div>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                    <div className="users-pagination">
                        <div className="users-pagination-left">
                            <span className="users-pagination-label">每页显示</span>
                            <select
                                className="filter-select users-page-size-select"
                                value={pageSize}
                                onChange={(e) => handlePageSizeChange(Number(e.target.value))}
                                disabled={loading}
                            >
                                {pageSizeOptions.map((option) => (
                                    <option key={option} value={option}>{option}</option>
                                ))}
                            </select>
                            <span className="users-pagination-note">共 {total} 个用户</span>
                        </div>
                        <div className="users-pagination-right">
                            <button className="users-page-button" disabled={page <= 1 || loading} onClick={handlePrevPage}>上一页</button>
                            <span className="users-page-info">第 {page} 页，共 {totalPages} 页</span>
                            <button className="users-page-button" disabled={page >= totalPages || loading} onClick={handleNextPage}>下一页</button>
                        </div>
                    </div>
                </div>

                {/* 用户详情弹窗 */}
                {showDetailModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowDetailModal(false)}>
                        <div className="modal-content detail-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>用户详情</h3>
                                <button className="modal-close" onClick={() => setShowDetailModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="user-detail">
                                    <div className="detail-section">
                                        <div className="detail-avatar">
                                            <span>{selectedUser.username[0]?.toUpperCase() || 'U'}</span>
                                        </div>
                                        <div className="detail-name">{selectedUser.username}</div>
                                    </div>
                                    <div className="detail-info">
                                        <div className="info-row">
                                            <span className="info-label">用户ID:</span>
                                            <span className="info-value">{selectedUser.id}</span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">灵石余额:</span>
                                            <span className="info-value points">
                                                <span style={{ display: 'inline', marginRight: '4px', verticalAlign: 'middle' }}>✦</span>
                                                {selectedUser.pointsBalance}
                                            </span>
                                        </div>
                                        <div className="info-row">
                                            <span className="info-label">创建时间:</span>
                                            <span className="info-value">{new Date(selectedUser.createdAt).toLocaleString('zh-CN')}</span>
                                        </div>
                                        <div className="info-row info-row-tip">
                                            <span className="info-label">下一步:</span>
                                            <span className="info-value">如需继续处理订单、任务、灵石、认证或风控，请进入用户360工作台。</span>
                                        </div>
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-primary" onClick={() => navigate(`/user-workbench?userId=${selectedUser.id}`)}>
                                    进入用户360
                                </button>
                                <button className="btn-secondary" onClick={() => setShowDetailModal(false)}>
                                    关闭
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 编辑用户信息弹窗 */}
                {showEditUserModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowEditUserModal(false)}>
                        <div className="modal-content edit-user-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>编辑用户信息</h3>
                                <button className="modal-close" onClick={() => setShowEditUserModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="form-group">
                                    <label>登录用户名 *</label>
                                    <input
                                        className="form-input"
                                        value={editUsername}
                                        onChange={(e) => setEditUsername(e.target.value)}
                                        placeholder="请输入登录用户名"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>新密码（留空则不修改）</label>
                                    <input
                                        type="password"
                                        className="form-input"
                                        value={editPassword}
                                        onChange={(e) => setEditPassword(e.target.value)}
                                        placeholder="设置登录密码"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>确认新密码</label>
                                    <input
                                        type="password"
                                        className="form-input"
                                        value={editPasswordConfirm}
                                        onChange={(e) => setEditPasswordConfirm(e.target.value)}
                                        placeholder="再次输入新密码"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>昵称</label>
                                    <input
                                        className="form-input"
                                        value={editNickname}
                                        onChange={(e) => setEditNickname(e.target.value)}
                                        placeholder="请输入昵称"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>头像 URL</label>
                                    <input
                                        className="form-input"
                                        value={editAvatar}
                                        onChange={(e) => setEditAvatar(e.target.value)}
                                        placeholder="https://..."
                                    />
                                </div>
                                <div className="form-group">
                                    <label>设计师简介</label>
                                    <textarea
                                        className="form-input"
                                        rows={4}
                                        value={editDesignerBio}
                                        onChange={(e) => setEditDesignerBio(e.target.value)}
                                        placeholder="请输入设计师简介"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>擅长方向</label>
                                    <textarea
                                        className="form-input"
                                        rows={3}
                                        value={editSpecialtyStyles}
                                        onChange={(e) => setEditSpecialtyStyles(e.target.value)}
                                        placeholder="例如：别墅设计、庭院营造、室内软装"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>真实从业经验（年）</label>
                                    <input
                                        type="number"
                                        min={0}
                                        className="form-input"
                                        value={editDesignerExperienceYears}
                                        onChange={(e) => setEditDesignerExperienceYears(e.target.value)}
                                        placeholder="例如：8"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>服务标题</label>
                                    <input
                                        className="form-input"
                                        value={editServiceTitle}
                                        onChange={(e) => setEditServiceTitle(e.target.value)}
                                        placeholder="例如：室内设计师"
                                    />
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowEditUserModal(false)}>
                                    取消
                                </button>
                                <button
                                    className="btn-primary"
                                    onClick={async () => {
                                        if (!selectedUser) return;
                                        const username = editUsername.trim();
                                        if (!username) {
                                            alert('用户名不能为空');
                                            return;
                                        }
                                        if (editPassword || editPasswordConfirm) {
                                            if (editPassword !== editPasswordConfirm) {
                                                alert('两次输入的密码不一致');
                                                return;
                                            }
                                            if (editPassword.length < 6) {
                                                alert('密码长度至少 6 位');
                                                return;
                                            }
                                        }
                                        setEditLoading(true);
                                        try {
                                            await updateUser(selectedUser.id, {
                                                username,
                                                password: editPassword || undefined,
                                                nickname: editNickname.trim() || undefined,
                                                avatar: editAvatar.trim() || undefined,
                                                designer_bio: editDesignerBio.trim() || undefined,
                                                specialty_styles: editSpecialtyStyles.trim() || undefined,
                                                designer_experience_years: Math.max(0, Number(editDesignerExperienceYears) || 0),
                                                service_title: editServiceTitle.trim() || undefined,
                                            });
                                            // 更新本地列表展示的用户名
                                            refreshUsers();
                                            alert('保存成功');
                                            setShowEditUserModal(false);
                                        } catch (error: any) {
                                            console.error('更新用户失败:', error);
                                            alert(error?.message || '更新用户失败');
                                        } finally {
                                            setEditLoading(false);
                                        }
                                    }}
                                    disabled={editLoading || !editUsername.trim()}
                                >
                                    {editLoading ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {showEnterpriseWechatModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowEnterpriseWechatModal(false)}>
                        <div className="modal-content enterprise-wechat-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>手机号授权验证</h3>
                                <button className="modal-close" onClick={() => setShowEnterpriseWechatModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                {enterpriseWechatLoading ? (
                                    <div className="empty-state">正在加载手机号授权状态...</div>
                                ) : (
                                    <div className="enterprise-wechat-form">
                                        <div className="verification-summary-card">
                                            <div className="verification-summary-row">
                                                <span className="user-label">用户：{selectedUser.username}</span>
                                                <span className={`verification-badge ${enterpriseWechatVerified ? 'verified' : 'pending'}`}>
                                                    {enterpriseWechatVerified ? '已授权' : '待授权'}
                                                </span>
                                            </div>
                                            <div className="verification-summary-meta">用户ID：{selectedUser.id}</div>
                                            <div className="verification-summary-meta">
                                                当前状态：{enterpriseWechatVerified ? '已完成手机号授权验证' : '手机号待授权验证'}
                                            </div>
                                            <div className="verification-summary-meta">
                                                授权时间：{enterpriseWechatVerifiedAtText || '暂未授权'}
                                            </div>
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
                                                type="text"
                                                value={enterpriseWechatContact}
                                                onChange={(e) => setEnterpriseWechatContact(e.target.value)}
                                                placeholder="请输入用户授权后的手机号"
                                                className="form-input"
                                            />
                                        </div>

                                        <div className="verification-tip-box">
                                            说明：正常情况下用户完成手机号授权后，系统会自动更新状态；如需补录，可在这里同步手机号授权结果。
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
                                    {enterpriseWechatSaving ? '保存中...' : '保存授权状态'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 修改灵石弹窗 */}
                {showStonesModal && selectedUser && (
                    <div className="modal-overlay" onClick={() => setShowStonesModal(false)}>
                        <div className="modal-content stones-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>修改灵石余额</h3>
                                <button className="modal-close" onClick={() => setShowStonesModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="stones-form">
                                    <div className="user-info-bar">
                                        <span className="user-label">用户: {selectedUser.username}</span>
                                        <span className="current-stones">
                                            当前灵石: <strong>{selectedUser.pointsBalance}</strong>
                                        </span>
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
                                                设置余额
                                            </button>
                                        </div>
                                    </div>

                                    {stonesMode === 'adjust' && (
                                        <div className="form-group">
                                            <label>快捷操作</label>
                                            <div className="quick-buttons">
                                                <button className="quick-btn add" onClick={() => handleQuickAdjust(100)}>
                                                    <span>+</span> 100
                                                </button>
                                                <button className="quick-btn add" onClick={() => handleQuickAdjust(500)}>
                                                    <span>+</span> 500
                                                </button>
                                                <button className="quick-btn add" onClick={() => handleQuickAdjust(1000)}>
                                                    <span>+</span> 1000
                                                </button>
                                                <button className="quick-btn minus" onClick={() => handleQuickAdjust(-100)}>
                                                    <span>-</span> 100
                                                </button>
                                                <button className="quick-btn minus" onClick={() => handleQuickAdjust(-500)}>
                                                    <span>-</span> 500
                                                </button>
                                            </div>
                                        </div>
                                    )}

                                    <div className="form-group">
                                        <label>
                                            {stonesMode === 'set' ? '新余额' : '调整数量'}
                                            {stonesMode === 'adjust' && <span className="hint">（正数增加，负数扣除）</span>}
                                        </label>
                                        <input
                                            type="number"
                                            value={stonesValue}
                                            onChange={(e) => setStonesValue(e.target.value)}
                                            placeholder={stonesMode === 'set' ? '请输入新的灵石余额' : '请输入调整数量'}
                                            className="form-input"
                                        />
                                    </div>

                                    {stonesMode === 'adjust' && stonesValue && !isNaN(parseInt(stonesValue)) && (
                                        <div className="preview-result">
                                            预计修改后: <strong>{selectedUser.pointsBalance + parseInt(stonesValue)}</strong> 灵石
                                        </div>
                                    )}

                                    <div className="form-group">
                                        <label>备注（可选）</label>
                                        <input
                                            type="text"
                                            value={stonesRemark}
                                            onChange={(e) => setStonesRemark(e.target.value)}
                                            placeholder="请输入操作备注，如：活动奖励、补偿等"
                                            className="form-input"
                                        />
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowStonesModal(false)}>
                                    取消
                                </button>
                                <button
                                    className="btn-primary"
                                    onClick={handleStonesSubmit}
                                    disabled={stonesLoading || !stonesValue}
                                >
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

export default Users;
