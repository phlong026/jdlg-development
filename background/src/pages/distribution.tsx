import React, { useState, useEffect } from 'react';
import { FiAward, FiGift, FiRefreshCw, FiSearch, FiShare2, FiUsers } from 'react-icons/fi';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { getDistributionInviters, getDistributionOverview, getDistributionRewards, type DistributionInviterItem, type DistributionOverview, type DistributionRewardItem } from '../api/distribution';
import './distribution.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiUsersIcon = FiUsers as unknown as React.ComponentType<any>;
const FiShare2Icon = FiShare2 as unknown as React.ComponentType<any>;
const FiAwardIcon = FiAward as unknown as React.ComponentType<any>;
const FiGiftIcon = FiGift as unknown as React.ComponentType<any>;

const defaultOverview: DistributionOverview = {
    inviter_count: 0,
    total_invite_count: 0,
    paid_invite_count: 0,
    total_reward_amount: 0,
    month_reward_amount: 0,
    month_new_invites: 0,
};

const rewardTypeLabelMap: Record<string, string> = {
    invite: '邀请奖励',
    invite_reward: '邀请返奖',
};

const Distribution: React.FC = () => {
    const [overview, setOverview] = useState<DistributionOverview>(defaultOverview);
    const [inviters, setInviters] = useState<DistributionInviterItem[]>([]);
    const [rewards, setRewards] = useState<DistributionRewardItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');

    const loadData = async () => {
        setLoading(true);
        try {
            const [overviewData, invitersData, rewardsData] = await Promise.all([
                getDistributionOverview(),
                getDistributionInviters({ page: 1, page_size: 50, keyword }),
                getDistributionRewards({ page: 1, page_size: 50, keyword }),
            ]);
            setOverview(overviewData);
            setInviters(invitersData.list || []);
            setRewards(rewardsData.list || []);
        } catch (error) {
            console.error('加载分销邀请中心失败:', error);
            alert('加载分销邀请中心失败');
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

    return (
        <Layout title="分销邀请中心">
            <div className="distribution-page">
                <ManagementSearchPanel
                    title="邀请检索与奖励追踪"
                    description="先用关键词找到邀请人或奖励记录，再结合下方分销概览判断谁值得重点扶持、哪笔奖励需要排查。"
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
                                    placeholder="搜索用户名、昵称、邀请码或奖励描述"
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                />
                            </div>
                            <button className="btn-primary" onClick={handleSearch}>搜索</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前邀请人 <strong>{inviters.length}</strong> 个，奖励记录 <strong>{rewards.length}</strong> 条
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="distribution-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiUsersIcon /></div>
                        <div className="stat-label">邀请人数量</div>
                        <div className="stat-value">{overview.inviter_count}</div>
                        <div className="stat-desc">已经至少邀请过 1 位好友的用户数</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiShare2Icon /></div>
                        <div className="stat-label">邀请注册总数</div>
                        <div className="stat-value">{overview.total_invite_count}</div>
                        <div className="stat-desc">真实已建立邀请关系的好友数量</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAwardIcon /></div>
                        <div className="stat-label">邀请付费数</div>
                        <div className="stat-value">{overview.paid_invite_count}</div>
                        <div className="stat-desc">已发生成功充值的被邀请好友数量</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiGiftIcon /></div>
                        <div className="stat-label">累计奖励灵石</div>
                        <div className="stat-value">{overview.total_reward_amount}</div>
                        <div className="stat-desc">本月新增 {overview.month_new_invites} 人，本月奖励 {overview.month_reward_amount}</div>
                    </div>
                </div>

                <div className="distribution-panel section-card">
                    <div className="distribution-section">
                        <div className="section-header">
                            <div>
                                <h3>邀请人排行</h3>
                                <p>优先看谁带来更多注册和付费，方便做重点扶持。</p>
                            </div>
                        </div>
                        <div className="distribution-table-container">
                            <table className="distribution-table">
                                <thead>
                                    <tr>
                                        <th>邀请人</th>
                                        <th>邀请码</th>
                                        <th>邀请注册数</th>
                                        <th>邀请付费数</th>
                                        <th>累计奖励</th>
                                        <th>本月奖励</th>
                                        <th>最近邀请时间</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {inviters.length === 0 ? (
                                        <tr>
                                            <td colSpan={7} className="empty-state">暂无邀请人数据</td>
                                        </tr>
                                    ) : (
                                        inviters.map((item) => (
                                            <tr key={item.user_id}>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.display_name || item.username}</strong>
                                                        <span>ID {item.user_id} · {item.username}</span>
                                                    </div>
                                                </td>
                                                <td>{item.invite_code || '暂无'}</td>
                                                <td>{item.invite_count}</td>
                                                <td>{item.paid_invite_count}</td>
                                                <td>{item.total_reward_amount}</td>
                                                <td>{item.month_reward_amount}</td>
                                                <td>{item.last_invited_at ? new Date(item.last_invited_at).toLocaleString('zh-CN') : '暂无'}</td>
                                            </tr>
                                        ))
                                    )}
                                </tbody>
                            </table>
                        </div>
                    </div>

                    <div className="distribution-section">
                        <div className="section-header">
                            <div>
                                <h3>奖励发放明细</h3>
                                <p>查看邀请奖励是否发出，便于客服排查“我邀请了为什么没到账”。</p>
                            </div>
                        </div>
                        <div className="distribution-table-container">
                            <table className="distribution-table">
                                <thead>
                                    <tr>
                                        <th>奖励归属人</th>
                                        <th>奖励类型</th>
                                        <th>奖励灵石</th>
                                        <th>场景说明</th>
                                        <th>备注</th>
                                        <th>发放时间</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {rewards.length === 0 ? (
                                        <tr>
                                            <td colSpan={6} className="empty-state">暂无奖励明细</td>
                                        </tr>
                                    ) : (
                                        rewards.map((item) => (
                                            <tr key={item.id}>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.display_name || item.username}</strong>
                                                        <span>ID {item.user_id} · {item.username}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    <span className="status-badge status-active">
                                                        {rewardTypeLabelMap[item.type] || item.type}
                                                    </span>
                                                </td>
                                                <td>{item.amount}</td>
                                                <td>{item.scene_desc || '暂无'}</td>
                                                <td>{item.remark || '暂无'}</td>
                                                <td>{item.created_at ? new Date(item.created_at).toLocaleString('zh-CN') : '暂无'}</td>
                                            </tr>
                                        ))
                                    )}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </div>
        </Layout>
    );
};

export default Distribution;
