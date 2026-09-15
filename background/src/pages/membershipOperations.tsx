import React, { useEffect, useMemo, useState } from 'react';
import { FiAlertCircle, FiClock, FiRefreshCw, FiSearch, FiShield, FiUsers } from 'react-icons/fi';
import { useNavigate } from 'react-router-dom';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getMembershipOperationsOverview,
    getMembershipOperationsUsers,
    type MembershipOperationsOverview,
    type MembershipOperationsUserItem,
} from '../api/membershipOperations';
import './membershipOperations.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiUsersIcon = FiUsers as unknown as React.ComponentType<any>;
const FiShieldIcon = FiShield as unknown as React.ComponentType<any>;
const FiClockIcon = FiClock as unknown as React.ComponentType<any>;
const FiAlertCircleIcon = FiAlertCircle as unknown as React.ComponentType<any>;

const defaultOverview: MembershipOperationsOverview = {
    total_users: 0,
    active_users: 0,
    expiring_soon_users: 0,
    expired_users: 0,
    permission_disabled_users: 0,
    legacy_recharge_users: 0,
};

const MEMBERSHIP_STATUS_MAP: Record<string, string> = {
    active: '生效中',
    expired: '已过期',
    inactive: '未生效',
    legacy_active: '旧版长期会员',
};

const ORDER_STATUS_MAP: Record<string, string> = {
    success: '成功',
    pending: '待支付',
    failed: '失败',
    cancelled: '已取消',
};

const MembershipOperations: React.FC = () => {
    const navigate = useNavigate();
    const [overview, setOverview] = useState<MembershipOperationsOverview>(defaultOverview);
    const [users, setUsers] = useState<MembershipOperationsUserItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [keywordInput, setKeywordInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [status, setStatus] = useState('all');
    const [permissionState, setPermissionState] = useState('all');

    const loadData = async () => {
        setLoading(true);
        try {
            const [overviewData, usersData] = await Promise.all([
                getMembershipOperationsOverview(),
                getMembershipOperationsUsers({
                    page: 1,
                    page_size: 50,
                    keyword,
                    status,
                    permission_state: permissionState,
                }),
            ]);
            setOverview(overviewData);
            setUsers(usersData.list || []);
        } catch (error) {
            console.error('加载会员运营页失败:', error);
            alert('加载会员运营页失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData();
    }, [keyword, status, permissionState]);

    const summaryCards = useMemo(() => {
        return [
            {
                key: 'total',
                title: '会员相关用户',
                value: overview.total_users,
                icon: FiUsersIcon,
                desc: '包含真实会员和历史充值长期有效用户。',
            },
            {
                key: 'active',
                title: '当前生效中',
                value: overview.active_users,
                icon: FiShieldIcon,
                desc: '当前可正常使用下载权益的会员用户。',
            },
            {
                key: 'expiring',
                title: '7天内到期',
                value: overview.expiring_soon_users,
                icon: FiClockIcon,
                desc: '适合做续费提醒和重点运营。',
            },
            {
                key: 'risk',
                title: '权限异常/已过期',
                value: overview.permission_disabled_users + overview.expired_users,
                icon: FiAlertCircleIcon,
                desc: '优先排查下载权限关闭或已过期用户。',
            },
        ];
    }, [overview]);

    const handleSearch = () => {
        setKeyword(keywordInput.trim());
    };

    const handleReset = () => {
        setKeywordInput('');
        setKeyword('');
        setStatus('all');
        setPermissionState('all');
    };

    return (
        <Layout title="用户会员运营">
            <div className="membership-operations-page">
                <ManagementSearchPanel
                    title="会员检索与权益排查"
                    description="先按用户、会员状态和下载权限缩小范围，再进入用户360或订单中心核查来源订单与权限异常。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading}>
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
                                    placeholder="搜索用户名、昵称、会员计划、来源订单号"
                                />
                            </div>
                            <select className="management-search-select" value={status} onChange={(e) => setStatus(e.target.value)}>
                                <option value="all">全部会员状态</option>
                                <option value="active">当前生效中</option>
                                <option value="expiring">7天内到期</option>
                                <option value="expired">已过期</option>
                                <option value="legacy">历史长期有效</option>
                                <option value="permission_disabled">权限异常</option>
                            </select>
                            <select className="management-search-select" value={permissionState} onChange={(e) => setPermissionState(e.target.value)}>
                                <option value="all">全部下载权限</option>
                                <option value="enabled">权限已开启</option>
                                <option value="disabled">权限未开启</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索用户</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共 <strong>{users.length}</strong> 位会员相关用户
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                                {status !== 'all' ? <span className="management-search-tag">会员状态：{status}</span> : null}
                                {permissionState !== 'all' ? <span className="management-search-tag">下载权限：{permissionState === 'enabled' ? '已开启' : '未开启'}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="membership-summary-grid">
                    {summaryCards.map((card) => {
                        const Icon = card.icon;
                        return (
                            <div key={card.key} className="membership-summary-card section-card">
                                <div className="membership-summary-icon"><Icon /></div>
                                <div className="membership-summary-title">{card.title}</div>
                                <div className="membership-summary-value">{card.value}</div>
                                <div className="membership-summary-desc">{card.desc}</div>
                            </div>
                        );
                    })}
                </div>

                <div className="membership-main section-card">
                    <div className="membership-table-wrap">
                        <table className="membership-table">
                            <thead>
                                <tr>
                                    <th>用户</th>
                                    <th>会员计划</th>
                                    <th>会员状态</th>
                                    <th>下载权限</th>
                                    <th>有效期</th>
                                    <th>来源订单</th>
                                    <th>最近充值</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {users.length === 0 ? (
                                    <tr>
                                        <td colSpan={8} className="empty-state">暂无会员用户数据</td>
                                    </tr>
                                ) : (
                                    users.map((item) => {
                                        const sourceOrder = item.source_order;
                                        const latestRecharge = item.latest_recharge_order;
                                        return (
                                            <tr key={item.user_id}>
                                                <td>
                                                    <div className="user-cell">
                                                        <strong>{item.display_name || item.username}</strong>
                                                        <span>ID {item.user_id} · {item.username}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    <div className="plan-cell">
                                                        <strong>{item.plan_title || (item.legacy_recharge_member ? '旧版长期有效会员' : '未命名计划')}</strong>
                                                        <span>{item.plan_code || 'legacy_recharge'}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    <span className={`status-badge status-${item.status || 'inactive'}`}>
                                                        {MEMBERSHIP_STATUS_MAP[item.status || 'inactive'] || item.status || '未知'}
                                                    </span>
                                                    {item.remaining_days > 0 && !item.lifetime_membership ? (
                                                        <div className="cell-sub">剩余 {item.remaining_days} 天</div>
                                                    ) : null}
                                                </td>
                                                <td>
                                                    <span className={`perm-badge ${item.template_download_enabled ? 'enabled' : 'disabled'}`}>
                                                        {item.template_download_enabled ? '已开启' : '未开启'}
                                                    </span>
                                                </td>
                                                <td>
                                                    <div className="time-cell">
                                                        <span>生效：{item.granted_at_text || '暂无'}</span>
                                                        <span>到期：{item.expired_at_text || '暂无'}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    <div className="order-cell">
                                                        <strong>{item.source_order_no || '暂无'}</strong>
                                                        <span>{sourceOrder?.status ? `状态：${ORDER_STATUS_MAP[sourceOrder.status] || sourceOrder.status}` : '无来源订单'}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    <div className="order-cell">
                                                        <strong>{latestRecharge?.order_no || '暂无'}</strong>
                                                        <span>{latestRecharge?.status ? `状态：${ORDER_STATUS_MAP[latestRecharge.status] || latestRecharge.status}` : '暂无成功充值'}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    <div className="table-actions">
                                                        <button className="btn-link" onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}>用户360</button>
                                                        <button className="btn-link" onClick={() => navigate('/recharge')}>订单中心</button>
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })
                                )}
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </Layout>
    );
};

export default MembershipOperations;
