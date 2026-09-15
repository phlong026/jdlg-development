import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiAlertTriangle, FiCpu, FiRefreshCw, FiRepeat, FiSearch, FiShield } from 'react-icons/fi';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getRiskControlAlerts,
    getRiskControlDeviceChanges,
    getRiskControlDeviceGroups,
    getRiskControlOverview,
    getRiskControlUsers,
    type RiskControlAlertItem,
    type RiskControlDeviceChangeItem,
    type RiskControlDeviceGroupItem,
    type RiskControlOverview,
    type RiskControlUserItem,
} from '../api/riskControl';
import './riskControl.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiShieldIcon = FiShield as unknown as React.ComponentType<any>;
const FiCpuIcon = FiCpu as unknown as React.ComponentType<any>;
const FiRepeatIcon = FiRepeat as unknown as React.ComponentType<any>;
const FiAlertTriangleIcon = FiAlertTriangle as unknown as React.ComponentType<any>;

const defaultOverview: RiskControlOverview = {
    shared_devices: 0,
    device_risk_users: 0,
    recent_device_changes: 0,
    abnormal_payments: 0,
    failed_tasks: 0,
};

const formatDateTime = (value?: string) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const RiskControl: React.FC = () => {
    const navigate = useNavigate();
    const [overview, setOverview] = useState<RiskControlOverview>(defaultOverview);
    const [deviceGroups, setDeviceGroups] = useState<RiskControlDeviceGroupItem[]>([]);
    const [deviceChanges, setDeviceChanges] = useState<RiskControlDeviceChangeItem[]>([]);
    const [alerts, setAlerts] = useState<RiskControlAlertItem[]>([]);
    const [riskUsers, setRiskUsers] = useState<RiskControlUserItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');

    const loadData = async () => {
        setLoading(true);
        try {
            const params = { page: 1, page_size: 10, keyword };
            const [overviewData, groupsData, changesData, alertsData, usersData] = await Promise.all([
                getRiskControlOverview(),
                getRiskControlDeviceGroups(params),
                getRiskControlDeviceChanges(params),
                getRiskControlAlerts(params),
                getRiskControlUsers(params),
            ]);
            setOverview(overviewData);
            setDeviceGroups(groupsData.list || []);
            setDeviceChanges(changesData.list || []);
            setAlerts(alertsData.list || []);
            setRiskUsers(usersData.list || []);
        } catch (error) {
            console.error('加载风控台失败:', error);
            alert('加载风控台失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData();
    }, [keyword]);

    const handleSearch = () => {
        setKeyword(searchInput.trim());
    };

    const handleReset = () => {
        setSearchInput('');
        setKeyword('');
    };

    const openUserWorkbench = (userId: number | string) => {
        navigate(`/user-workbench?userId=${userId}`);
    };

    const openAlertSourcePage = (item: RiskControlAlertItem) => {
        if (item.alert_type === 'payment') {
            navigate('/recharge');
            return;
        }
        navigate('/ai-tasks');
    };

    return (
        <Layout title="风控台">
            <div className="risk-control-page">
                <ManagementSearchPanel
                    title="风险检索与异常排查"
                    description="先按用户名、昵称或设备ID锁定风险对象，再查看多账号同设备、换绑记录、失败任务和风险标签。"
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
                                    type="text"
                                    className="management-search-input"
                                    placeholder="搜索用户名、昵称或设备ID"
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                />
                            </div>
                            <button className="btn-primary" onClick={handleSearch}>搜索风险</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共享设备 <strong>{overview.shared_devices}</strong> 个，风险用户 <strong>{overview.device_risk_users}</strong> 位
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="risk-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiCpuIcon /></div>
                        <div className="stat-label">多账号同设备</div>
                        <div className="stat-value">{overview.shared_devices}</div>
                        <div className="stat-desc">同一个设备被多个账号使用，需要优先排查共享与薅羊毛风险。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiShieldIcon /></div>
                        <div className="stat-label">设备风险用户</div>
                        <div className="stat-value">{overview.device_risk_users}</div>
                        <div className="stat-desc">已经绑定设备的用户规模，可以帮助判断风险面有多大。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiRepeatIcon /></div>
                        <div className="stat-label">最近换绑设备</div>
                        <div className="stat-value">{overview.recent_device_changes}</div>
                        <div className="stat-desc">近 30 天内出现过设备更换的账号。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAlertTriangleIcon /></div>
                        <div className="stat-label">异常支付 / 失败任务</div>
                        <div className="stat-value">{overview.abnormal_payments} / {overview.failed_tasks}</div>
                        <div className="stat-desc">近 7 天的支付异常和任务失败，方便快速发现坏点。</div>
                    </div>
                </div>

                <div className="risk-panel section-card">
                    <div className="risk-section">
                        <div className="section-header">
                            <div>
                                <h3>多账号同设备识别</h3>
                                <p>一个设备绑定多个账号时，优先看是不是工作室批量号、共享号，或者被重复注册滥用。</p>
                            </div>
                        </div>
                        <div className="risk-table-container">
                            <table className="risk-table">
                                <thead>
                                    <tr>
                                        <th>设备ID</th>
                                        <th>账号数量</th>
                                        <th>关联账号</th>
                                        <th>最近活跃</th>
                                        <th>建议动作</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {deviceGroups.length === 0 ? (
                                        <tr><td colSpan={5} className="empty-state">暂无异常设备分组</td></tr>
                                    ) : deviceGroups.map((item) => (
                                        <tr key={item.device_id}>
                                            <td className="mono-cell">{item.device_id}</td>
                                            <td>{item.user_count}</td>
                                            <td>
                                                <div className="tag-list">
                                                    {item.users.map((user) => (
                                                        <span key={`${item.device_id}-${user.user_id}`} className="inline-tag">
                                                            {user.nickname?.trim() || user.username || `用户${user.user_id}`}
                                                        </span>
                                                    ))}
                                                </div>
                                            </td>
                                            <td>{formatDateTime(item.latest_activity_at)}</td>
                                            <td>
                                                <div className="table-action-stack">
                                                    <button className="btn-text-link" onClick={() => navigate('/users')}>查看用户列表</button>
                                                    {item.users[0] ? (
                                                        <button className="btn-text-link" onClick={() => openUserWorkbench(item.users[0].user_id)}>查看首个用户360</button>
                                                    ) : null}
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </div>

                    <div className="risk-grid">
                        <div className="risk-section half">
                            <div className="section-header">
                                <div>
                                    <h3>近期换绑设备提醒</h3>
                                    <p>最近改过设备的账号，适合和投诉、支付异常、任务异常一起联动看。</p>
                                </div>
                            </div>
                            <div className="risk-table-container">
                                <table className="risk-table compact">
                                    <thead>
                                        <tr>
                                            <th>用户</th>
                                            <th>设备ID</th>
                                            <th>换绑时间</th>
                                            <th>建议动作</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {deviceChanges.length === 0 ? (
                                            <tr><td colSpan={4} className="empty-state">暂无近期换绑记录</td></tr>
                                        ) : deviceChanges.map((item) => (
                                            <tr key={`change-${item.user_id}`}>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.nickname?.trim() || item.username || `用户${item.user_id}`}</strong>
                                                        <span>ID {item.user_id}</span>
                                                    </div>
                                                </td>
                                                <td className="mono-cell">{item.device_id || '暂无'}</td>
                                                <td>{formatDateTime(item.last_device_change_time)}</td>
                                                <td>
                                                    <div className="table-action-stack">
                                                        <button className="btn-text-link" onClick={() => openUserWorkbench(item.user_id)}>进入用户360</button>
                                                        <button className="btn-text-link" onClick={() => navigate('/support-tickets')}>转异常工单</button>
                                                    </div>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        </div>

                        <div className="risk-section half">
                            <div className="section-header">
                                <div>
                                    <h3>异常支付 / 异常任务告警</h3>
                                    <p>同一用户短时间内反复失败时，通常意味着通道、账号行为或第三方接口有问题。</p>
                                </div>
                            </div>
                            <div className="risk-table-container">
                                <table className="risk-table compact">
                                    <thead>
                                        <tr>
                                            <th>用户</th>
                                            <th>告警类型</th>
                                            <th>次数</th>
                                            <th>最近时间</th>
                                            <th>建议动作</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {alerts.length === 0 ? (
                                            <tr><td colSpan={5} className="empty-state">暂无风险告警</td></tr>
                                        ) : alerts.map((item) => (
                                            <tr key={`${item.alert_type}-${item.user_id}`}>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.nickname?.trim() || item.username || `用户${item.user_id}`}</strong>
                                                        <span>{item.detail || '暂无详情'}</span>
                                                    </div>
                                                </td>
                                                <td>{item.alert_type === 'payment' ? '支付异常' : '任务异常'}</td>
                                                <td>{item.alert_count}</td>
                                                <td>{formatDateTime(item.latest_time)}</td>
                                                <td>
                                                    <div className="table-action-stack">
                                                        <button className="btn-text-link" onClick={() => openUserWorkbench(item.user_id)}>进入用户360</button>
                                                        <button className="btn-text-link" onClick={() => openAlertSourcePage(item)}>{item.alert_type === 'payment' ? '查看订单中心' : '查看AI任务中心'}</button>
                                                        <button className="btn-text-link" onClick={() => navigate('/support-tickets')}>进入工单中心</button>
                                                    </div>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>

                    <div className="risk-section">
                        <div className="section-header">
                            <div>
                                <h3>用户风险标签</h3>
                                <p>把多个异常合并成用户级标签后，运营就能直接判断谁该优先回访、限制或人工核查。</p>
                            </div>
                        </div>
                        <div className="risk-table-container">
                            <table className="risk-table">
                                <thead>
                                    <tr>
                                        <th>用户</th>
                                        <th>设备ID</th>
                                        <th>同设备账号数</th>
                                        <th>失败任务</th>
                                        <th>异常支付</th>
                                        <th>风险标签</th>
                                        <th>建议动作</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {riskUsers.length === 0 ? (
                                        <tr><td colSpan={7} className="empty-state">暂无风险用户</td></tr>
                                    ) : riskUsers.map((item) => (
                                        <tr key={`user-${item.user_id}`}>
                                            <td>
                                                <div className="identity-cell">
                                                    <strong>{item.nickname?.trim() || item.username || `用户${item.user_id}`}</strong>
                                                    <span>ID {item.user_id} · 最近换绑 {formatDateTime(item.last_device_change_time)}</span>
                                                </div>
                                            </td>
                                            <td className="mono-cell">{item.device_id || '暂无'}</td>
                                            <td>{item.shared_device_count}</td>
                                            <td>{item.failed_task_count}</td>
                                            <td>{item.abnormal_payment_count}</td>
                                            <td>
                                                <div className="tag-list">
                                                    {item.risk_tags?.length ? item.risk_tags.map((tag) => (
                                                        <span key={`${item.user_id}-${tag}`} className="inline-tag danger">{tag}</span>
                                                    )) : <span className="inline-tag">待观察</span>}
                                                </div>
                                            </td>
                                            <td>
                                                <div className="table-action-stack">
                                                    <button className="btn-text-link" onClick={() => openUserWorkbench(item.user_id)}>进入用户360</button>
                                                    <button className="btn-text-link" onClick={() => navigate(item.abnormal_payment_count > 0 ? '/recharge' : '/ai-tasks')}>
                                                        {item.abnormal_payment_count > 0 ? '查看订单中心' : '查看AI任务中心'}
                                                    </button>
                                                    <button className="btn-text-link" onClick={() => navigate('/support-tickets')}>进入工单中心</button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </div>
        </Layout>
    );
};

export default RiskControl;
