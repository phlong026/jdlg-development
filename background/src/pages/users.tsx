import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { FiEye, FiSearch } from 'react-icons/fi';
import {
    getUserList,
    getUserStats,
    revealUserPhone,
    banUser,
    unbanUser,
    getUserMergePrecheck,
    type User as ApiUser,
    type UserListParams,
    type UserStats,
    type UserMergePrecheck,
} from '../api/users';
import './users.scss';

interface UserRow {
    id: string;
    username: string;
    displayName: string;
    stones: number;
    createdAt: string;
    phone: string; // 后端已脱敏；揭示后为完整号码
    identityType: string;
    status: string; // active | banned
    bannedReason: string;
    bannedAt: string;
    riskTags: string[];
    memberStatus: string;
    memberPlanTitle: string;
    enterpriseWechatVerified: boolean;
}

const pageSizeOptions = [20, 50, 100];

const IDENTITY_TYPES = ['乡墅业主', '设计师', '施工队', '建材商', '机构用户'];
const STONES_RANGES = [
    { key: 'zero', label: '余额 0（耗尽）', min: 0, max: 0 },
    { key: 'low', label: '1 – 500', min: 1, max: 500 },
    { key: 'mid', label: '500 – 5,000', min: 501, max: 5000 },
    { key: 'high', label: '5,000 以上', min: 5001, max: undefined as number | undefined },
];
const CREATED_RANGES = [
    { key: '1', label: '今日' },
    { key: '7', label: '近 7 天' },
    { key: '30', label: '近 30 天' },
];

const RISK_TAG_LABELS: Record<string, string> = {
    recent_device_change: '近7天换绑设备',
    same_device_multiple_accounts: '同设备多账号',
};

const formatDateTime = (value: string) => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value || '-';
    return date.toLocaleString('zh-CN');
};

const Users: React.FC = () => {
    const navigate = useNavigate();
    // ?wechat=pending|verified：总控台「下载权限待核验」带参直达，落地即应用手机号授权筛选
    const [searchParams] = useSearchParams();
    const wechatParam = searchParams.get('wechat');

    const [searchInput, setSearchInput] = useState('');
    const [searchKeyword, setSearchKeyword] = useState('');
    const [identityFilter, setIdentityFilter] = useState('');
    const [statusFilter, setStatusFilter] = useState('');
    const [wechatFilter, setWechatFilter] = useState<'all' | 'verified' | 'pending'>(
        wechatParam === 'pending' || wechatParam === 'verified' ? wechatParam : 'all',
    );
    const [memberFilter, setMemberFilter] = useState('');
    const [createdFilter, setCreatedFilter] = useState('');
    const [stonesFilter, setStonesFilter] = useState('');
    const [riskFilter, setRiskFilter] = useState(false);
    const [sortBy, setSortBy] = useState<'stones' | 'created_at' | ''>('');
    const [sortDesc, setSortDesc] = useState(true);

    const [stats, setStats] = useState<UserStats | null>(null);
    const [rows, setRows] = useState<UserRow[]>([]);
    const [revealedPhones, setRevealedPhones] = useState<Record<string, string>>({});
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(pageSizeOptions[0]);
    const [total, setTotal] = useState(0);
    const [reloadSeed, setReloadSeed] = useState(0);
    const [loading, setLoading] = useState(true);

    // 封禁/解封弹窗
    const [banTarget, setBanTarget] = useState<UserRow | null>(null);
    const [banMode, setBanMode] = useState<'ban' | 'unban'>('ban');
    const [banReason, setBanReason] = useState('');
    const [banSubmitting, setBanSubmitting] = useState(false);
    const [banError, setBanError] = useState('');
    // A submitted mutation keeps its lock until it settles, independent of dialog events.
    const banInFlight = useRef(false);
    const banRequestId = useRef(0);
    const renderedBanRequestId = banRequestId.current;

    // 账号合并向导（只读预检）
    const [showMergeModal, setShowMergeModal] = useState(false);
    const [mergeFromId, setMergeFromId] = useState('');
    const [mergeToId, setMergeToId] = useState('');
    const [mergePrecheck, setMergePrecheck] = useState<UserMergePrecheck | null>(null);
    const [mergeLoading, setMergeLoading] = useState(false);
    const [mergeError, setMergeError] = useState('');
    const mergeRequestId = useRef(0);
    const renderedMergeRequestId = mergeRequestId.current;

    useEffect(() => () => {
        banRequestId.current += 1;
        mergeRequestId.current += 1;
    }, []);

    const convertRow = (apiUser: ApiUser): UserRow => ({
        id: String(apiUser.id),
        username: apiUser.username || `用户${apiUser.id}`,
        // 昵称为空时不再回退展示 openid 式用户名（如 wx_o_gjx…），避免主副标题重复且暴露内部 ID；
        // username 仍保留在副标题与搜索中
        displayName: apiUser.nickname?.trim() || `用户${apiUser.id}`,
        stones: apiUser.stones || 0,
        createdAt: apiUser.created_at,
        phone: apiUser.phone || '',
        identityType: apiUser.identity_type || '',
        status: apiUser.status || 'active',
        bannedReason: apiUser.banned_reason || '',
        bannedAt: apiUser.banned_at || '',
        riskTags: apiUser.risk_tags || [],
        memberStatus: apiUser.member_status || '',
        memberPlanTitle: apiUser.member_plan_title || '',
        enterpriseWechatVerified: !!apiUser.enterprise_wechat_verified,
    });

    const buildListParams = useCallback((overrides: Partial<UserListParams> = {}): UserListParams => {
        const params: UserListParams = {
            page,
            page_size: pageSize,
        };
        if (searchKeyword) params.keyword = searchKeyword;
        if (identityFilter) params.identity_type = identityFilter;
        if (statusFilter) params.status = statusFilter;
        if (wechatFilter !== 'all') params.enterprise_wechat_status = wechatFilter;
        if (memberFilter) params.member = memberFilter as 'active' | 'none';
        if (createdFilter) params.created_within_days = Number(createdFilter);
        if (stonesFilter) {
            const range = STONES_RANGES.find((item) => item.key === stonesFilter);
            if (range) {
                params.stones_min = range.min;
                params.stones_max = range.max;
            }
        }
        if (riskFilter) params.risk = 1;
        if (sortBy) {
            params.sort_by = sortBy;
            params.sort_order = sortDesc ? 'desc' : 'asc';
        }
        return { ...params, ...overrides };
    }, [page, pageSize, searchKeyword, identityFilter, statusFilter, wechatFilter, memberFilter, createdFilter, stonesFilter, riskFilter, sortBy, sortDesc]);

    const refresh = () => setReloadSeed((value) => value + 1);

    useEffect(() => {
        const load = async () => {
            const requestPage = window.location.pathname;
            setLoading(true);
            try {
                const response = await getUserList(buildListParams());
                setRows(response.list.map(convertRow));
                setTotal(response.total || 0);
                setRevealedPhones({});
                reportPageLoadRecovered('加载用户列表失败', requestPage);
            } catch (error) {
                console.error('加载用户列表失败:', error);
                reportPageLoadError(error, '加载用户列表失败');
            } finally {
                setLoading(false);
            }
        };
        void load();
        // 列表重载只依赖筛选与分页状态；buildListParams 是它们的纯函数
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [page, pageSize, searchKeyword, identityFilter, statusFilter, wechatFilter, memberFilter, createdFilter, stonesFilter, riskFilter, sortBy, sortDesc, reloadSeed]);

    // 全库统计与列表解耦：进入页面与每次刷新/封禁操作后重取
    useEffect(() => {
        const loadStats = async () => {
            try {
                setStats(await getUserStats());
            } catch (error) {
                console.error('加载用户统计失败:', error);
            }
        };
        void loadStats();
    }, [reloadSeed]);

    const hasActiveFilters = () =>
        !!searchKeyword || !!identityFilter || !!statusFilter || wechatFilter !== 'all' ||
        !!memberFilter || !!createdFilter || !!stonesFilter || riskFilter;

    const handleSearch = () => {
        const next = searchInput.trim();
        if (page !== 1) setPage(1);
        if (next !== searchKeyword) {
            setSearchKeyword(next);
            return;
        }
        refresh();
    };

    const resetFilters = () => {
        setSearchInput('');
        setSearchKeyword('');
        setIdentityFilter('');
        setStatusFilter('');
        setWechatFilter('all');
        setMemberFilter('');
        setCreatedFilter('');
        setStonesFilter('');
        setRiskFilter(false);
        setSortBy('');
        setPage(1);
        if (hasActiveFilters()) refresh();
    };

    const applyKpiFilter = (patch: () => void) => {
        resetFilters();
        // resetFilters 在无既有筛选时不会触发 reload，这里统一补一次刷新
        window.setTimeout(() => {
            patch();
            setPage(1);
            refresh();
        }, 0);
    };

    const totalPages = Math.max(1, Math.ceil(total / pageSize) || 1);

    const handleRevealPhone = async (row: UserRow) => {
        const requestPage = window.location.pathname;
        if (revealedPhones[row.id]) {
            setRevealedPhones((prev) => {
                const next = { ...prev };
                delete next[row.id];
                return next;
            });
            return;
        }
        try {
            const data = await revealUserPhone(row.id);
            reportPageLoadRecovered('查看完整手机号失败', requestPage);
            if (data) {
                const full = data.phone || data.enterprise_wechat_contact || '';
                if (full) {
                    setRevealedPhones((prev) => ({ ...prev, [row.id]: full }));
                }
            }
        } catch (error) {
            console.error('查看完整手机号失败:', error);
            reportPageLoadError(error, '查看完整手机号失败');
        }
    };

    const openBanModal = (row: UserRow, mode: 'ban' | 'unban') => {
        if (banInFlight.current) return;
        banRequestId.current += 1;
        setBanError('');
        setBanSubmitting(false);
        setBanTarget(row);
        setBanMode(mode);
        setBanReason('');
    };

    const closeBanModal = () => {
        if (banInFlight.current) return;
        banRequestId.current += 1;
        setBanTarget(null);
        setBanError('');
        setBanSubmitting(false);
    };

    const submitBan = async () => {
        if (!banTarget || banInFlight.current || banSubmitting || renderedBanRequestId !== banRequestId.current || (banMode === 'ban' && !banReason.trim())) return;
        banInFlight.current = true;
        const requestId = ++banRequestId.current;
        setBanError('');
        setBanSubmitting(true);
        try {
            if (banMode === 'ban') {
                await banUser(banTarget.id, banReason.trim());
            } else {
                await unbanUser(banTarget.id, banReason.trim());
            }
            if (requestId === banRequestId.current) setBanTarget(null);
            refresh();
        } catch (error: any) {
            console.error('封禁/解封失败:', error);
            if (requestId === banRequestId.current) {
                setBanError(`${banMode === 'ban' ? '封禁失败' : '解封失败'}：${error?.message || '请稍后重试'}`);
            }
        } finally {
            banInFlight.current = false;
            if (requestId === banRequestId.current) setBanSubmitting(false);
        }
    };

    const toggleSort = (key: 'stones' | 'created_at') => {
        if (sortBy === key) {
            setSortDesc((prev) => !prev);
        } else {
            setSortBy(key);
            setSortDesc(true);
        }
        setPage(1);
    };

    const openMergeModal = () => {
        mergeRequestId.current += 1;
        setMergeError('');
        setMergeLoading(false);
        setMergePrecheck(null);
        setShowMergeModal(true);
    };

    const closeMergeModal = () => {
        mergeRequestId.current += 1;
        setMergeError('');
        setMergeLoading(false);
        setShowMergeModal(false);
    };

    const runMergePrecheck = async () => {
        const from = mergeFromId.trim();
        const to = mergeToId.trim();
        if (!from || !to || mergeLoading || renderedMergeRequestId !== mergeRequestId.current) {
            return;
        }
        const requestId = ++mergeRequestId.current;
        setMergeError('');
        setMergeLoading(true);
        setMergePrecheck(null);
        try {
            const result = await getUserMergePrecheck(from, to);
            if (requestId === mergeRequestId.current) setMergePrecheck(result);
        } catch (error: any) {
            console.error('合并预检失败:', error);
            if (requestId === mergeRequestId.current) setMergeError(`合并预检失败：${error?.message || '请稍后重试'}`);
        } finally {
            if (requestId === mergeRequestId.current) setMergeLoading(false);
        }
    };

    const phoneVerifiedPercent = stats && stats.total > 0 ? ((stats.phone_verified / stats.total) * 100).toFixed(1) : '0.0';

    const kpiCards = [
        {
            key: 'total',
            label: '用户总数',
            value: stats ? stats.total.toLocaleString() : '—',
            sub: `今日新增 ${stats?.today_new ?? '—'}`,
            onClick: resetFilters,
        },
        {
            key: 'verified',
            label: '手机号已授权',
            value: stats ? stats.phone_verified.toLocaleString() : '—',
            sub: `${phoneVerifiedPercent}% · 待授权 ${stats?.phone_pending ?? '—'}`,
            onClick: () => applyKpiFilter(() => setWechatFilter('verified')),
        },
        {
            key: 'member',
            label: '会员生效中',
            value: stats ? stats.member_active.toLocaleString() : '—',
            sub: '全库口径',
            onClick: () => applyKpiFilter(() => setMemberFilter('active')),
        },
        {
            key: 'stones',
            label: '灵石总余额',
            value: stats ? stats.stones_total.toLocaleString() : '—',
            sub: '资金口径 · 以流水对账为准',
            onClick: null, // 只读资金指标，不参与筛选
        },
        {
            key: 'zero',
            label: '灵石耗尽·曾充值',
            value: stats ? stats.zero_stones_recharged.toLocaleString() : '—',
            sub: '转化召回候选',
            onClick: () => applyKpiFilter(() => setStonesFilter('zero')),
        },
        {
            key: 'risk',
            label: '风险账号',
            value: stats ? (stats.banned + stats.risk_users).toLocaleString() : '—',
            sub: `已封禁 ${stats?.banned ?? '—'} · 信号待处置 ${stats?.risk_users ?? '—'}`,
            onClick: () => applyKpiFilter(() => setRiskFilter(true)),
        },
    ];

    const filterChips: { label: string; clear: () => void }[] = [];
    if (searchKeyword) filterChips.push({ label: `搜索：${searchKeyword}`, clear: () => { setSearchInput(''); setSearchKeyword(''); } });
    if (identityFilter) filterChips.push({ label: `身份：${identityFilter}`, clear: () => setIdentityFilter('') });
    if (statusFilter) filterChips.push({ label: `状态：${statusFilter === 'banned' ? '已封禁' : '正常'}`, clear: () => setStatusFilter('') });
    if (wechatFilter !== 'all') filterChips.push({ label: `手机授权：${wechatFilter === 'verified' ? '已授权' : '待授权'}`, clear: () => setWechatFilter('all') });
    if (memberFilter) filterChips.push({ label: `会员：${memberFilter === 'active' ? '生效中' : '无会员'}`, clear: () => setMemberFilter('') });
    if (createdFilter) filterChips.push({ label: `注册：${CREATED_RANGES.find((r) => r.key === createdFilter)?.label ?? ''}`, clear: () => setCreatedFilter('') });
    if (stonesFilter) filterChips.push({ label: `余额：${STONES_RANGES.find((r) => r.key === stonesFilter)?.label ?? ''}`, clear: () => setStonesFilter('') });
    if (riskFilter) filterChips.push({ label: '风险信号账号', clear: () => setRiskFilter(false) });

    return (
        <div className="users-container">
            <div className="users-toolbar section-card">
                <div className="toolbar-top">
                    <div className="toolbar-title-block">
                        <h3>用户检索与账号管理</h3>
                        <p>找人、筛人、看账号状态与风险信号；订单、灵石、认证等单用户处理请进入用户详情。以下统计为全库真实口径。</p>
                    </div>
                    <div className="toolbar-actions">
                        <button
                            className="btn-secondary"
                            onClick={openMergeModal}
                            title="两个账号的资产对照与冲突预检（执行走 DBA 受控流程）"
                        >
                            账号合并预检
                        </button>
                        <button className="btn-secondary" onClick={refresh} disabled={loading}>
                            {loading ? '刷新中...' : '刷新数据'}
                        </button>
                        <button className="btn-secondary" onClick={resetFilters} disabled={!hasActiveFilters()}>
                            重置筛选
                        </button>
                    </div>
                </div>
                <div className="toolbar-left">
                    <div className="search-box">
                        <span className="search-icon"><FiSearch size={14} /></span>
                        <input
                            type="text"
                            placeholder="搜索 ID / 用户名 / 昵称 / 手机号（模糊匹配）"
                            value={searchInput}
                            onChange={(e) => setSearchInput(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') handleSearch();
                            }}
                            className="search-input"
                        />
                    </div>
                    <div className="filters">
                        <select className="filter-select" value={identityFilter} onChange={(e) => { setIdentityFilter(e.target.value); setPage(1); }}>
                            <option value="">身份类型：全部</option>
                            {IDENTITY_TYPES.map((type) => <option key={type} value={type}>{type}</option>)}
                        </select>
                        <select className="filter-select" value={statusFilter} onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}>
                            <option value="">账号状态：全部</option>
                            <option value="active">仅正常</option>
                            <option value="banned">仅已封禁</option>
                        </select>
                        <select className="filter-select" value={wechatFilter} onChange={(e) => { setWechatFilter(e.target.value as 'all' | 'verified' | 'pending'); setPage(1); }}>
                            <option value="all">手机授权：全部</option>
                            <option value="verified">仅已授权</option>
                            <option value="pending">仅待授权</option>
                        </select>
                        <select className="filter-select" value={memberFilter} onChange={(e) => { setMemberFilter(e.target.value); setPage(1); }}>
                            <option value="">会员：全部</option>
                            <option value="active">生效中</option>
                            <option value="none">无会员</option>
                        </select>
                        <select className="filter-select" value={createdFilter} onChange={(e) => { setCreatedFilter(e.target.value); setPage(1); }}>
                            <option value="">注册时间：全部</option>
                            {CREATED_RANGES.map((range) => <option key={range.key} value={range.key}>{range.label}</option>)}
                        </select>
                        <select className="filter-select" value={stonesFilter} onChange={(e) => { setStonesFilter(e.target.value); setPage(1); }}>
                            <option value="">灵石余额：全部</option>
                            {STONES_RANGES.map((range) => <option key={range.key} value={range.key}>{range.label}</option>)}
                        </select>
                    </div>
                    {filterChips.length > 0 && (
                        <div className="filter-chips">
                            {filterChips.map((chip) => (
                                <span className="filter-chip" key={chip.label}>
                                    {chip.label}
                                    <button onClick={chip.clear} title="移除该筛选">✕</button>
                                </span>
                            ))}
                            <button className="filter-chip-clear" onClick={resetFilters}>清空全部</button>
                        </div>
                    )}
                </div>
            </div>

            <div className="users-stats">
                {kpiCards.map((card) => (
                    <div
                        key={card.key}
                        className={`stat-item ${card.onClick ? 'clickable' : ''}`}
                        onClick={card.onClick ?? undefined}
                        title={card.onClick ? '点击应用筛选' : '只读指标'}
                    >
                        <span className="stat-label">{card.label}</span>
                        <span className="stat-value">{card.value}</span>
                        <span className="stat-sub">{card.sub}</span>
                    </div>
                ))}
            </div>

            <div className="users-table-container">
                <div className="users-table-wrap">
                    <table className="users-table">
                    <thead>
                        <tr>
                            <th>用户</th>
                            <th>手机号</th>
                            <th>身份</th>
                            <th>状态</th>
                            <th>风险</th>
                            <th>手机授权</th>
                            <th>会员</th>
                            <th className="sortable" onClick={() => toggleSort('stones')}>
                                灵石余额 {sortBy === 'stones' ? (sortDesc ? '↓' : '↑') : '↕'}
                            </th>
                            <th className="sortable" onClick={() => toggleSort('created_at')}>
                                注册时间 {sortBy === 'created_at' ? (sortDesc ? '↓' : '↑') : '↕'}
                            </th>
                            <th>操作</th>
                        </tr>
                    </thead>
                    <tbody>
                        {rows.length === 0 ? (
                            <tr>
                                <td colSpan={10} className="empty-state">
                                    {loading ? '加载中...' : '没有符合当前筛选条件的用户'}
                                </td>
                            </tr>
                        ) : (
                            rows.map((row) => {
                                const banned = row.status !== 'active';
                                const fullPhone = revealedPhones[row.id];
                                return (
                                    <tr key={row.id} className={banned ? 'row-banned' : ''} onClick={() => navigate(`/user-workbench?userId=${row.id}`)}>
                                        <td>
                                            <div className="user-id-cell">
                                                <span className="user-avatar-letter">{row.displayName.slice(0, 1).toUpperCase()}</span>
                                                <span>
                                                    <span className="user-name">{row.displayName}</span>
                                                    <span className="user-sub">ID {row.id} · {row.username}</span>
                                                </span>
                                            </div>
                                        </td>
                                        <td onClick={(e) => e.stopPropagation()}>
                                            {fullPhone ? (
                                                <span className="user-phone-text revealed" title="已展示完整号码，本次查看已记入审计">{fullPhone}</span>
                                            ) : row.phone ? (
                                                <span className="user-phone-text">
                                                    {row.phone}
                                                    <button className="phone-reveal" onClick={() => handleRevealPhone(row)} title="查看完整号码（记审计）">
                                                        <FiEye size={13} />
                                                    </button>
                                                </span>
                                            ) : (
                                                <span className="user-phone-none">未授权</span>
                                            )}
                                        </td>
                                        <td>
                                            {row.identityType
                                                ? <span className="identity-type-badge">{row.identityType}</span>
                                                : <span className="user-phone-none">未填写</span>}
                                        </td>
                                        <td>
                                            {banned ? (
                                                <span className="status-badge status-banned" title={`${row.bannedReason}${row.bannedAt ? ` · ${row.bannedAt}` : ''}`}>已封禁</span>
                                            ) : (
                                                <span className="status-badge status-normal">正常</span>
                                            )}
                                        </td>
                                        <td>
                                            {row.riskTags.length > 0
                                                ? <span className="risk-tag">{row.riskTags.map((tag) => RISK_TAG_LABELS[tag] || tag).join('、')}</span>
                                                : <span className="user-phone-none">—</span>}
                                        </td>
                                        <td>
                                            <span className={`status-badge ${row.enterpriseWechatVerified ? 'status-verified' : 'status-pending'}`}>
                                                {row.enterpriseWechatVerified ? '已授权' : '待授权'}
                                            </span>
                                        </td>
                                        <td>
                                            {row.memberStatus === 'active'
                                                ? <span className="member-badge on" title={row.memberPlanTitle}>生效中</span>
                                                : row.memberStatus === 'expired'
                                                    ? <span className="member-badge expired">已到期</span>
                                                    : <span className="user-phone-none">—</span>}
                                        </td>
                                        <td>
                                            <span className="points-balance">
                                                <span style={{ display: 'inline', marginRight: '4px', verticalAlign: 'middle' }}>✦</span>
                                                {row.stones.toLocaleString()}
                                            </span>
                                        </td>
                                        <td>{formatDateTime(row.createdAt)}</td>
                                        <td onClick={(e) => e.stopPropagation()}>
                                            <div className="action-buttons">
                                                <button
                                                    className="btn-action btn-enter-workbench"
                                                    onClick={() => navigate(`/user-workbench?userId=${row.id}`)}
                                                    title="进入用户详情"
                                                >
                                                    用户详情
                                                </button>
                                                {banned ? (
                                                    <button className="btn-action btn-unban" onClick={() => openBanModal(row, 'unban')} title="解封该账号">
                                                        解封
                                                    </button>
                                                ) : (
                                                    <button className="btn-action btn-ban" onClick={() => openBanModal(row, 'ban')} title="封禁该账号（拦截登录与AI任务/支付）">
                                                        封禁
                                                    </button>
                                                )}
                                            </div>
                                        </td>
                                    </tr>
                                );
                            })
                        )}
                    </tbody>
                </table>
                </div>
                <div className="users-pagination">
                    <div className="users-pagination-left">
                        <span className="users-pagination-label">每页显示</span>
                        <select
                            className="filter-select users-page-size-select"
                            value={pageSize}
                            onChange={(e) => { setPageSize(Number(e.target.value)); setPage(1); }}
                            disabled={loading}
                        >
                            {pageSizeOptions.map((option) => (
                                <option key={option} value={option}>{option}</option>
                            ))}
                        </select>
                        <span className="users-pagination-note">共 {total} 个用户</span>
                    </div>
                    <div className="users-pagination-right">
                        <button className="users-page-button" disabled={page <= 1 || loading} onClick={() => setPage((p) => p - 1)}>上一页</button>
                        <span className="users-page-info">第 {page} 页，共 {totalPages} 页</span>
                        <button className="users-page-button" disabled={page >= totalPages || loading} onClick={() => setPage((p) => p + 1)}>下一页</button>
                    </div>
                </div>
            </div>

            {/* 封禁/解封确认弹窗 */}
            {banTarget && (
                <div className="modal-overlay" onClick={closeBanModal}>
                    <div className="modal-content ban-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="modal-header">
                            <h3>{banMode === 'ban' ? '封禁账号' : '解封账号'}</h3>
                            <button className="modal-close" onClick={closeBanModal} disabled={banSubmitting} aria-label="关闭账号操作">✕</button>
                        </div>
                        <div className="modal-body">
                            <div className="ban-user-bar">
                                <span>{banTarget.displayName}（ID {banTarget.id}）</span>
                                {banMode === 'ban' && banTarget.stones > 0 && (
                                    <span className="ban-warn">余额 ✦{banTarget.stones.toLocaleString()} 将被冻结（数据保留，解封后恢复）</span>
                                )}
                            </div>
                            {banError ? <div className="ban-tip" role="alert">{banError}</div> : null}
                            {banMode === 'ban' ? (
                                <>
                                    <div className="form-group">
                                        <label>封禁原因（必填，将展示给用户并记入审计）</label>
                                        <textarea
                                            className="form-input"
                                            rows={3}
                                            value={banReason}
                                            disabled={banSubmitting}
                                            onChange={(e) => setBanReason(e.target.value)}
                                            placeholder="例如：同设备批量注册刷签到 / 违规内容 / 资金风控"
                                        />
                                    </div>
                                    <div className="ban-tip">
                                        封禁后立即生效：拒绝登录（微信/手机号/密码三条通路）、拦截 AI 任务与充值发起；解封后自动恢复。
                                    </div>
                                </>
                            ) : (
                                <>
                                    <div className="form-group">
                                        <label>解封说明（选填，记入审计）</label>
                                        <textarea
                                            className="form-input"
                                            rows={2}
                                            value={banReason}
                                            disabled={banSubmitting}
                                            onChange={(e) => setBanReason(e.target.value)}
                                            placeholder="例如：已与用户核实，误判解除"
                                        />
                                    </div>
                                    {banTarget.bannedReason && (
                                        <div className="ban-tip">原封禁原因：{banTarget.bannedReason}{banTarget.bannedAt ? ` · ${banTarget.bannedAt}` : ''}</div>
                                    )}
                                </>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeBanModal} disabled={banSubmitting}>取消</button>
                            <button
                                className={banMode === 'ban' ? 'btn-danger-solid' : 'btn-primary'}
                                onClick={submitBan}
                                disabled={banSubmitting || (banMode === 'ban' && !banReason.trim())}
                            >
                                {banSubmitting ? '提交中...' : banMode === 'ban' ? '确认封禁' : '确认解封'}
                            </button>
                        </div>
                    </div>
                </div>
            )}
            {/* 账号合并预检弹窗 */}
            {showMergeModal && (
                <div className="modal-overlay" onClick={closeMergeModal}>
                    <div className="modal-content merge-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="modal-header">
                            <h3>账号合并预检</h3>
                            <button className="modal-close" onClick={closeMergeModal}>✕</button>
                        </div>
                        <div className="modal-body">
                            <div className="merge-inputs">
                                <div className="form-group">
                                    <label>源账号 ID（被合并、将被冻结）</label>
                                    <input className="form-input" value={mergeFromId} disabled={mergeLoading} onChange={(e) => { setMergeFromId(e.target.value); setMergePrecheck(null); setMergeError(''); }} placeholder="例如 25" />
                                </div>
                                <div className="form-group">
                                    <label>主账号 ID（资产并入、保留登录）</label>
                                    <input className="form-input" value={mergeToId} disabled={mergeLoading} onChange={(e) => { setMergeToId(e.target.value); setMergePrecheck(null); setMergeError(''); }} placeholder="例如 13" />
                                </div>
                                <button className="btn-primary" onClick={runMergePrecheck} disabled={mergeLoading || !mergeFromId.trim() || !mergeToId.trim()}>
                                    {mergeLoading ? '预检中...' : '开始预检'}
                                </button>
                            </div>
                            {mergeError ? <div className="ban-tip" role="alert">{mergeError}</div> : null}
                            {mergePrecheck && (
                                <div className="merge-result">
                                    <div className="admin-table-region"><table className="merge-compare">
                                        <thead>
                                            <tr>
                                                <th>资产项</th>
                                                <th>源账号 #{mergePrecheck.from.user_id}（{mergePrecheck.from.username}）</th>
                                                <th>主账号 #{mergePrecheck.to.user_id}（{mergePrecheck.to.username}）</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {([
                                                ['灵石余额', (a: typeof mergePrecheck.from) => `✦ ${a.stones.toLocaleString()}`],
                                                ['订单数', (a: typeof mergePrecheck.from) => a.orders.toLocaleString()],
                                                ['灵石流水', (a: typeof mergePrecheck.from) => a.stone_records.toLocaleString()],
                                                ['AI 任务（图/视频）', (a: typeof mergePrecheck.from) => `${a.ai_tasks} / ${a.ai_video_tasks}`],
                                                ['发布作品', (a: typeof mergePrecheck.from) => a.templates.toLocaleString()],
                                                ['会员记录', (a: typeof mergePrecheck.from) => a.memberships.toLocaleString()],
                                                ['认证申请', (a: typeof mergePrecheck.from) => a.certifications.toLocaleString()],
                                                ['账号状态', (a: typeof mergePrecheck.from) => a.status === 'banned' ? '已封禁' : '正常'],
                                            ] as [string, (a: typeof mergePrecheck.from) => string][]).map(([label, pick]) => (
                                                <tr key={label}>
                                                    <td>{label}</td>
                                                    <td className="col-from">{pick(mergePrecheck.from)}</td>
                                                    <td className="col-to">{pick(mergePrecheck.to)}</td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table></div>
                                    {mergePrecheck.conflicts.length > 0 && (
                                        <div className="merge-conflicts">
                                            <b>冲突点（执行前需人工决策）：</b>
                                            <ul>
                                                {mergePrecheck.conflicts.map((item) => <li key={item}>{item}</li>)}
                                            </ul>
                                        </div>
                                    )}
                                    <div className="ban-tip">{mergePrecheck.execution_note}</div>
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeMergeModal}>关闭</button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};

export default Users;
