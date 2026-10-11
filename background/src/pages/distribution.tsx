import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useState, useEffect, useRef } from 'react';
import { FiAward, FiGift, FiRefreshCw, FiSearch, FiShare2, FiUsers } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { getDistributionInviters, getDistributionOverview, getDistributionRewards, type DistributionInviterItem, type DistributionOverview, type DistributionRewardItem, type DistributionListParams, type DistributionListResponse } from '../api/distribution';
import './distribution.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiUsersIcon = FiUsers as unknown as React.ComponentType<any>;
const FiShare2Icon = FiShare2 as unknown as React.ComponentType<any>;
const FiAwardIcon = FiAward as unknown as React.ComponentType<any>;
const FiGiftIcon = FiGift as unknown as React.ComponentType<any>;

const PAGE_SIZE = 50;

// Each table owns its query, lifecycle and recovery; one failed request never erases another section.
function useDistributionList<T>(fetcher: (params: DistributionListParams) => Promise<DistributionListResponse<T>>, source: string) {
    const [query, setQuery] = useState({ page: 1, keyword: '' });
    const [rows, setRows] = useState<T[]>([]);
    const [total, setTotal] = useState<number | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const queryRef = useRef(query);
    const version = useRef(0);
    const mounted = useRef(true);
    const ready = useRef(false);
    const renderedVersion = version.current;
    const invalidate = () => {
        version.current += 1;
        ready.current = false;
        setRows([]);
        setTotal(null);
        setLoading(true);
        setError('');
    };
    const changeQuery = (next: typeof query) => {
        queryRef.current = next;
        invalidate();
        setQuery(next);
    };
    const load = async () => {
        if (!mounted.current) return;
        invalidate();
        const requestId = version.current;
        const params = queryRef.current;
        const isCurrent = () => mounted.current && requestId === version.current && params === queryRef.current;
        try {
            const data = await fetcher({ ...params, page_size: PAGE_SIZE });
            if (!isCurrent()) return;
            if (!data || (data.list !== null && !Array.isArray(data.list))) throw new Error('列表响应无效');
            if (data.page !== params.page || data.page_size !== PAGE_SIZE) throw new Error('分页响应与当前查询不一致');
            const count = Number.isFinite(data.total) && data.total >= 0 ? data.total : null;
            const lastPage = count === null ? params.page : Math.max(1, Math.ceil(count / PAGE_SIZE));
            if (params.page > lastPage) { changeQuery({ ...params, page: lastPage }); return; }
            setRows(data.list || []);
            setTotal(count);
            ready.current = true;
            reportPageLoadRecovered(source);
        } catch (error) {
            if (!isCurrent()) return;
            setError(error instanceof Error ? error.message : '请重试');
            reportPageLoadError(error, source);
        } finally {
            if (isCurrent()) setLoading(false);
        }
    };
    const setKeyword = (keyword: string) => {
        if (!mounted.current) return;
        if (queryRef.current.keyword === keyword && queryRef.current.page === 1) { void load(); return; }
        changeQuery({ page: 1, keyword });
    };
    const setPage = (page: number) => {
        if (!mounted.current || !ready.current || renderedVersion !== version.current || total === null) return;
        const bounded = Math.max(1, Math.min(page, Math.max(1, Math.ceil(total / PAGE_SIZE))));
        if (bounded !== queryRef.current.page) changeQuery({ ...queryRef.current, page: bounded });
    };
    useEffect(() => {
        mounted.current = true;
        return () => { mounted.current = false; ready.current = false; version.current += 1; };
    }, []);
    useEffect(() => { void load(); }, [query]);
    return { rows, total, loading, error, page: query.page, setPage, setKeyword, load };
}

const rewardTypeLabelMap: Record<string, string> = {
    invite: '邀请奖励',
    invite_reward: '邀请返奖',
};

const Distribution: React.FC = () => {
    const [overview, setOverview] = useState<DistributionOverview | null>(null);
    const [overviewLoading, setOverviewLoading] = useState(true);
    const [overviewError, setOverviewError] = useState('');
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const inputRef = useRef('');
    const overviewVersion = useRef(0);
    const mounted = useRef(true);
    const inviterList = useDistributionList<DistributionInviterItem>(getDistributionInviters, '获取邀请人排行失败');
    const rewardList = useDistributionList<DistributionRewardItem>(getDistributionRewards, '获取奖励明细失败');
    const inviters = inviterList.rows;
    const rewards = rewardList.rows;
    const loading = overviewLoading || inviterList.loading || rewardList.loading;
    const loadOverview = async () => {
        if (!mounted.current) return;
        const version = ++overviewVersion.current;
        const isCurrent = () => mounted.current && version === overviewVersion.current;
        setOverview(null);
        setOverviewLoading(true);
        setOverviewError('');
        try {
            const data = await getDistributionOverview();
            if (!isCurrent()) return;
            if (!data) throw new Error('概览响应无效');
            setOverview(data);
            reportPageLoadRecovered('获取分销邀请概览失败');
        } catch (error) {
            if (!isCurrent()) return;
            setOverviewError(error instanceof Error ? error.message : '请重试');
            reportPageLoadError(error, '获取分销邀请概览失败');
        } finally {
            if (isCurrent()) setOverviewLoading(false);
        }
    };
    useEffect(() => {
        mounted.current = true;
        void loadOverview();
        return () => { mounted.current = false; overviewVersion.current += 1; };
    }, []);
    const loadData = () => { void loadOverview(); void inviterList.load(); void rewardList.load(); };
    const applyKeyword = (value: string) => {
        if (!mounted.current) return;
        setKeyword(value);
        inviterList.setKeyword(value);
        rewardList.setKeyword(value);
    };
    const handleSearch = () => applyKeyword(inputRef.current.trim());
    const handleReset = () => {
        if (!mounted.current) return;
        inputRef.current = '';
        setSearchInput('');
        applyKeyword('');
    };
    const renderPagination = (list: typeof inviterList | typeof rewardList, label: string) => (
        <div className="distribution-pagination" aria-label={`${label}分页`}>
            <span>{list.loading ? '分页加载中...' : list.error || list.total === null ? '总数暂不可用' : `当前筛选共 ${list.total} 条 · 第 ${list.page} / ${Math.max(1, Math.ceil(list.total / PAGE_SIZE))} 页`}</span>
            <button className="btn-secondary" aria-label={`${label}上一页`} disabled={list.loading || Boolean(list.error) || list.total === null || list.page <= 1} onClick={() => list.setPage(list.page - 1)}>上一页</button>
            <button className="btn-secondary" aria-label={`${label}下一页`} disabled={list.loading || Boolean(list.error) || list.total === null || list.page >= Math.max(1, Math.ceil(list.total / PAGE_SIZE))} onClick={() => list.setPage(list.page + 1)}>下一页</button>
        </div>
    );

    return (
        <>
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
                                    aria-label="分销搜索"
                                    placeholder="搜索用户名、昵称、邀请码或奖励描述"
                                    value={searchInput}
                                    onChange={(e) => { inputRef.current = e.target.value; setSearchInput(e.target.value); }}
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
                                当前筛选邀请人共 <strong>{inviterList.total ?? '—'}</strong> 位，奖励记录共 <strong>{rewardList.total ?? '—'}</strong> 条
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="distribution-scope-note">全局邀请统计，不随下方关键词筛选变化。本月按服务器月初统计，奖励单位均为灵石。</div>
                {overviewLoading ? <div role="status">概览加载中...</div> : overviewError ? <div className="distribution-error" role="alert">概览加载失败：{overviewError}<button className="btn-secondary" onClick={() => void loadOverview()}>重新加载概览</button></div> : null}
                <div className="distribution-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiUsersIcon /></div>
                        <div className="stat-label">邀请人数量</div>
                        <div className="stat-value">{overview?.inviter_count ?? '—'}</div>
                        <div className="stat-desc">已经至少邀请过 1 位好友的用户数</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiShare2Icon /></div>
                        <div className="stat-label">邀请注册总数</div>
                        <div className="stat-value">{overview?.total_invite_count ?? '—'}</div>
                        <div className="stat-desc">真实已建立邀请关系的好友数量</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAwardIcon /></div>
                        <div className="stat-label">邀请付费数</div>
                        <div className="stat-value">{overview?.paid_invite_count ?? '—'}</div>
                        <div className="stat-desc">已发生成功充值的被邀请好友数量</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiGiftIcon /></div>
                        <div className="stat-label">累计奖励灵石</div>
                        <div className="stat-value">{overview?.total_reward_amount ?? '—'}</div>
                        <div className="stat-desc">本月新增 {overview?.month_new_invites ?? '—'} 人，本月奖励 {overview?.month_reward_amount ?? '—'} 灵石</div>
                    </div>
                </div>

                <div className="distribution-panel section-card">
                    <div className="distribution-section">
                        <div className="section-header">
                            <div>
                                <h3>邀请人排行</h3>
                                <p>按服务端全量结果分页；关键词匹配用户名、昵称和邀请码。</p>
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
                                        <th>累计奖励（灵石）</th>
                                        <th>本月奖励（灵石）</th>
                                        <th>最近邀请时间</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {inviterList.loading || inviterList.error || inviters.length === 0 ? (
                                        <tr>
                                            <td colSpan={7} className="empty-state">{inviterList.loading ? <span role="status">邀请人排行加载中...</span> : inviterList.error ? <span role="alert">邀请人排行加载失败：{inviterList.error}<button className="btn-secondary" onClick={() => void inviterList.load()}>重新加载邀请人</button></span> : '暂无邀请人数据'}</td>
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
                        {renderPagination(inviterList, '邀请人')}
                    </div>

                    <div className="distribution-section">
                        <div className="section-header">
                            <div>
                                <h3>奖励发放明细</h3>
                                <p>按服务端全量结果分页；关键词匹配用户名、昵称、奖励描述和备注。</p>
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
                                    {rewardList.loading || rewardList.error || rewards.length === 0 ? (
                                        <tr>
                                            <td colSpan={6} className="empty-state">{rewardList.loading ? <span role="status">奖励明细加载中...</span> : rewardList.error ? <span role="alert">奖励明细加载失败：{rewardList.error}<button className="btn-secondary" onClick={() => void rewardList.load()}>重新加载奖励</button></span> : '暂无奖励明细'}</td>
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
                        {renderPagination(rewardList, '奖励')}
                    </div>
                </div>
            </div>
        </>
    );
};

export default Distribution;
