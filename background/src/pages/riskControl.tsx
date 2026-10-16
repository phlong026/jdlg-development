import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiAlertTriangle, FiChevronDown, FiCpu, FiRefreshCw, FiRepeat, FiSearch, FiShield } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { useIsMobile } from '../component/useIsMobile';
import {
    getRiskControlAlerts,
    getRiskControlDeviceChanges,
    getRiskControlDeviceGroups,
    getRiskControlOverview,
    getRiskControlUsers,
    type RiskControlAlertItem,
    type RiskControlOverview,
    type RiskControlListParams,
    type RiskControlListResponse,
} from '../api/riskControl';
import './riskControl.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiShieldIcon = FiShield as unknown as React.ComponentType<any>;
const FiCpuIcon = FiCpu as unknown as React.ComponentType<any>;
const FiRepeatIcon = FiRepeat as unknown as React.ComponentType<any>;
const FiAlertTriangleIcon = FiAlertTriangle as unknown as React.ComponentType<any>;

const PAGE_SIZE = 10;
const isRecord = (value: unknown): value is Record<string, any> => !!value && typeof value === 'object' && !Array.isArray(value);
const isCount = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0;
const isId = (value: unknown) => (typeof value === 'string' && !!value.trim()) || (isCount(value) && value > 0);
const optionalText = (value: unknown) => value == null || typeof value === 'string';
const isUser = (value: unknown) => isRecord(value) && isId(value.user_id) && typeof value.username === 'string' && optionalText(value.nickname);
const isDeviceGroup = (value: unknown) => isRecord(value) && typeof value.device_id === 'string' && isCount(value.user_count)
    && optionalText(value.latest_activity_at) && Array.isArray(value.users) && value.users.every(isUser);
const isDeviceChange = (value: unknown) => isUser(value) && isRecord(value) && optionalText(value.device_id) && optionalText(value.last_device_change_time);
const isAlert = (value: unknown) => {
    if (!isRecord(value) || typeof value.alert_type !== 'string' || !isCount(value.alert_count)
        || !optionalText(value.latest_time) || !optionalText(value.detail)) return false;
    // 访问告警允许匿名访客（user_id=0，以 visitor_key 呈现）；其余类型要求真实用户
    if (value.alert_type === 'access') return isCount(value.user_id) && optionalText(value.visitor_key);
    return isUser(value);
};
const isRiskUser = (value: unknown) => isUser(value) && isRecord(value) && optionalText(value.device_id)
    && isCount(value.shared_device_count) && isCount(value.failed_task_count) && isCount(value.abnormal_payment_count)
    && optionalText(value.last_device_change_time) && (value.risk_tags == null || (Array.isArray(value.risk_tags) && value.risk_tags.every(tag => typeof tag === 'string')));

interface RiskListState<T> {
    list: T[];
    total: number | null;
    page: number;
    loading: boolean;
    error: string;
    generation: number;
}

// Each module owns its request generation. Handler-time invalidation closes the
// gap before React's next render/effect, including filter and retry races.
function useRiskList<T>(fetchList: (params: RiskControlListParams) => Promise<RiskControlListResponse<T>>, validItem: (value: unknown) => boolean, label: string) {
    const [state, setState] = useState<RiskListState<T>>({ list: [], total: null, page: 1, loading: true, error: '', generation: 0 });
    const scope = useRef({ mounted: false, generation: 0, keyword: '', page: 1 });
    const source = `加载${label}失败`;

    const load = async (page: number, keyword: string) => {
        if (!scope.current.mounted) return;
        const generation = ++scope.current.generation;
        scope.current.keyword = keyword;
        scope.current.page = page;
        setState({ list: [], total: null, page, loading: true, error: '', generation });
        const isCurrent = () => scope.current.mounted && generation === scope.current.generation;
        try {
            const response = await fetchList({ page, page_size: PAGE_SIZE, keyword });
            if (!isCurrent()) return;
            if (!isRecord(response) || !Array.isArray(response.list) || !response.list.every(validItem)
                || !isCount(response.total) || response.page !== page || response.page_size !== PAGE_SIZE
                || response.list.length > PAGE_SIZE || response.list.length > response.total) {
                throw new Error('返回数据格式异常，请重试');
            }
            const lastPage = Math.max(1, Math.ceil(response.total / PAGE_SIZE));
            if (page > lastPage) {
                // Totals can shrink between reads. Only this table moves back.
                void load(lastPage, keyword);
                return;
            }
            if (response.total > 0 && response.list.length === 0) throw new Error('当前页数据缺失，请重试');
            setState({ list: response.list, total: response.total, page, loading: false, error: '', generation });
            reportPageLoadRecovered(source);
        } catch (error) {
            if (!isCurrent()) return;
            const detail = error instanceof Error ? error.message : '请求失败，请重试';
            setState({ list: [], total: null, page, loading: false, error: detail, generation });
            reportPageLoadError(error, source);
        }
    };

    useEffect(() => {
        scope.current.mounted = true;
        void load(1, '');
        return () => { scope.current.mounted = false; scope.current.generation++; };
    }, []);

    const isCurrentView = () => scope.current.mounted && state.generation === scope.current.generation;
    return {
        ...state,
        label,
        applyKeyword: (keyword: string) => { void load(1, keyword); },
        refresh: () => { if (isCurrentView()) void load(scope.current.page, scope.current.keyword); },
        changePage: (page: number) => {
            if (!isCurrentView() || state.loading || state.error || state.total == null || !Number.isSafeInteger(page)
                || page < 1 || page > Math.max(1, Math.ceil(state.total / PAGE_SIZE)) || page === scope.current.page) return;
            void load(page, scope.current.keyword);
        },
    };
}

function useRiskOverview() {
    const [state, setState] = useState<{ data: RiskControlOverview | null; loading: boolean; error: string; generation: number }>({ data: null, loading: true, error: '', generation: 0 });
    const request = useRef({ mounted: false, generation: 0 });
    const source = '加载风控全局概览失败';
    const load = async () => {
        if (!request.current.mounted) return;
        const generation = ++request.current.generation;
        setState({ data: null, loading: true, error: '', generation });
        const isCurrent = () => request.current.mounted && generation === request.current.generation;
        try {
            const data = await getRiskControlOverview();
            if (!isCurrent()) return;
            if (!isRecord(data) || !['shared_devices', 'device_risk_users', 'recent_device_changes', 'abnormal_payments', 'failed_tasks', 'access_alerts'].every(key => isCount(data[key]))) {
                throw new Error('返回数据格式异常，请重试');
            }
            setState({ data, loading: false, error: '', generation });
            reportPageLoadRecovered(source);
        } catch (error) {
            if (!isCurrent()) return;
            setState({ data: null, loading: false, error: error instanceof Error ? error.message : '请求失败，请重试', generation });
            reportPageLoadError(error, source);
        }
    };
    useEffect(() => {
        request.current.mounted = true;
        void load();
        return () => { request.current.mounted = false; request.current.generation++; };
    }, []);
    return { ...state, refresh: () => { if (request.current.mounted && state.generation === request.current.generation) void load(); } };
}

function tableFeedback<T>(table: RiskListState<T> & { label: string; refresh: () => void }, columns: number, emptyText: string) {
    if (!table.loading && !table.error && table.list.length) return null;
    return <tr><td colSpan={columns} className="empty-state">
        {table.loading ? <span role="status">{table.label}加载中…</span> : table.error ? (
            <div className="risk-load-error" role="alert">
                <span>{table.label}加载失败：{table.error}</span>
                <button className="btn-secondary" onClick={table.refresh}>重试{table.label}</button>
            </div>
        ) : emptyText}
    </td></tr>;
}

function pagination<T>(table: RiskListState<T> & { label: string; changePage: (page: number) => void }) {
    const pageCount = table.total == null ? null : Math.max(1, Math.ceil(table.total / PAGE_SIZE));
    const disabled = table.loading || !!table.error || table.total == null;
    return <nav className="risk-pagination" aria-label={`${table.label}分页`}>
        <span aria-live="polite">共 {table.total ?? '—'} 条 · 第 {table.page} / {pageCount ?? '—'} 页 · 每页 {PAGE_SIZE} 条</span>
        <div className="risk-pagination-actions">
            <button className="btn-secondary" aria-label={`${table.label}上一页`} onClick={() => table.changePage(table.page - 1)} disabled={disabled || table.page <= 1}>上一页</button>
            <button className="btn-secondary" aria-label={`${table.label}下一页`} onClick={() => table.changePage(table.page + 1)} disabled={disabled || table.page >= (pageCount ?? 1)}>下一页</button>
        </div>
    </nav>;
}

const formatDateTime = (value?: string) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const RiskControl: React.FC = () => {
    const navigate = useNavigate();
    const isMobile = useIsMobile();
    // 概览卡 773px：手机端收起为一行摘要（共用设备/换绑数常显），桌面常开
    const [overviewOpen, setOverviewOpen] = useState<boolean | null>(null);
    const overviewExpanded = overviewOpen ?? !isMobile;
    const overview = useRiskOverview();
    const groups = useRiskList(getRiskControlDeviceGroups, isDeviceGroup, '同设备账号');
    const changes = useRiskList(getRiskControlDeviceChanges, isDeviceChange, '设备换绑');
    const alertList = useRiskList(getRiskControlAlerts, isAlert, '风险告警');
    const users = useRiskList(getRiskControlUsers, isRiskUser, '风险用户');
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const tables = [groups, changes, alertList, users];
    const loading = overview.loading || tables.some(table => table.loading);
    const applySearch = (value: string) => {
        setKeyword(value);
        tables.forEach(table => table.applyKeyword(value));
    };
    const handleSearch = () => applySearch(searchInput.trim());
    const handleReset = () => {
        setSearchInput('');
        applySearch('');
    };
    const refreshAll = () => {
        overview.refresh();
        tables.forEach(table => table.refresh());
    };
    const metric = (key: keyof RiskControlOverview) => overview.data?.[key] ?? '—';

    const openUserWorkbench = (userId: number | string) => {
        navigate(`/user-workbench?userId=${userId}`);
    };

    // 联动A：按已识别访客键跳分享访问记录（visitor 精确过滤），风控与访问行为同屏互证
    const openVisitorTracking = (userId: number) => {
        if (!Number.isSafeInteger(userId) || userId <= 0) return;
        navigate(`/share-tracking?visitor=${encodeURIComponent(`u:${userId}`)}`);
    };

    const openAlertSourcePage = (item: RiskControlAlertItem) => {
        if (item.alert_type === 'payment') {
            navigate('/recharge');
            return;
        }
        if (item.alert_type === 'task') navigate('/ai-tasks');
        // 访问告警：直接带 visitor 精确过滤跳到分享访问记录（联动A/B 的打通点）
        if (item.alert_type === 'access' && item.visitor_key) {
            navigate(`/share-tracking?visitor=${encodeURIComponent(item.visitor_key)}`);
        }
    };

    return (
        <>
            <div className="risk-control-page">
                <ManagementSearchPanel
                    title="风险检索与异常排查"
                    description="关键词用于下方四张表：同设备账号、换绑与风险用户支持用户名、昵称或设备ID；风险告警仅支持用户名或昵称。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={refreshAll} disabled={loading}>
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
                                    placeholder="搜索用户名或昵称（也支持设备ID）"
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
                                下方列表各自分页；全局概览不受关键词筛选影响。
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                            </div>
                        </>
                    )}
                />

                <section className="risk-overview" aria-label="风控全局概览" aria-busy={overview.loading}>
                    <details
                        className="risk-overview-fold"
                        open={overviewExpanded}
                        onToggle={(event) => setOverviewOpen((event.target as HTMLDetailsElement).open)}
                    >
                        <summary className="risk-overview-summary">
                            <span>全局概览 · 共用设备 {metric('shared_devices')} · 近30天换绑 {metric('recent_device_changes')}</span>
                            <FiChevronDown className="risk-overview-caret" aria-hidden />
                        </summary>
                    <div className="section-header">
                        <div><h3>全局概览</h3><p>不受关键词筛选影响，各项统计时间范围见下方说明。</p></div>
                        <button className="btn-secondary" onClick={overview.refresh} disabled={overview.loading}>刷新全局概览</button>
                    </div>
                    {overview.loading ? <p role="status">全局概览加载中…</p> : overview.error ? (
                        <div className="risk-load-error" role="alert">
                            <span>全局概览加载失败：{overview.error}</span>
                            <button className="btn-secondary" onClick={overview.refresh}>重试全局概览</button>
                        </div>
                    ) : null}
                <div className="risk-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiCpuIcon /></div>
                        <div className="stat-label">多账号同设备</div>
                        <div className="stat-value">{metric('shared_devices')}</div>
                        <div className="stat-desc">全量共享设备数：同一设备绑定多个账号，需人工核查原因。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiShieldIcon /></div>
                        <div className="stat-label">已绑定设备用户</div>
                        <div className="stat-value">{metric('device_risk_users')}</div>
                        <div className="stat-desc">全量已绑定设备账号，非命中风险人数；按接口统计展示。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiRepeatIcon /></div>
                        <div className="stat-label">最近换绑设备</div>
                        <div className="stat-value">{metric('recent_device_changes')}</div>
                        <div className="stat-desc">近 30 天内出现过设备更换的账号。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAlertTriangleIcon /></div>
                        <div className="stat-label">异常支付 / 失败任务</div>
                        <div className="stat-value">{metric('abnormal_payments')} / {metric('failed_tasks')}</div>
                        <div className="stat-desc">近 7 天：异常支付含失败与待支付；任务仅统计失败。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiAlertTriangleIcon /></div>
                        <div className="stat-label">异常访问（分享追踪）</div>
                        <div className="stat-value">{metric('access_alerts')}</div>
                        <div className="stat-desc">近 7 天单访客页面访问量超阈值的访客数，明细见下方风险告警。</div>
                    </div>
                </div>
                    </details>

                </section>

                <div className="risk-panel section-card">
                    <div className="risk-section">
                        <div className="section-header">
                            <div>
                                <h3>多账号同设备识别</h3>
                                <p>一个设备绑定多个账号时，优先看是不是工作室批量号、共享号，或者被重复注册滥用。</p>
                            </div>
                        </div>
                        <div className="risk-table-container">
                            <table className="risk-table" aria-label="同设备账号" aria-busy={groups.loading}>
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
                                    {tableFeedback(groups, 5, '当前筛选下暂无同设备账号分组') || groups.list.map((item) => (
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
                                                        <button className="btn-text-link" onClick={() => navigate('/users')}>打开用户列表（未筛选）</button>
                                                        {item.users[0] ? (
                                                            <button className="btn-text-link" onClick={() => openUserWorkbench(item.users[0].user_id)}>查看首个用户详情</button>
                                                        ) : null}
                                                        {item.users[0] && Number(item.users[0].user_id) > 0 ? (
                                                            <button className="btn-text-link" onClick={() => openVisitorTracking(Number(item.users[0].user_id))}>查看首个用户访问记录</button>
                                                        ) : null}
                                                    </div>
                                                </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                        {pagination(groups)}
                    </div>

                    <div className="risk-grid">
                        <div className="risk-section half">
                            <div className="section-header">
                                <div>
                                    <h3>设备换绑记录</h3>
                                    <p>列出所有有设备换绑时间的账号，不限近 30 天；与上方近 30 天概览口径不同。</p>
                                </div>
                            </div>
                            <div className="risk-table-container">
                                <table className="risk-table compact" aria-label="设备换绑" aria-busy={changes.loading}>
                                    <thead>
                                        <tr>
                                            <th>用户</th>
                                            <th>设备ID</th>
                                            <th>换绑时间</th>
                                            <th>建议动作</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {tableFeedback(changes, 4, '当前筛选下暂无设备换绑记录') || changes.list.map((item) => (
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
                                                        <button className="btn-text-link" onClick={() => openUserWorkbench(item.user_id)}>查看用户详情</button>
                                                        <button className="btn-text-link" onClick={() => openVisitorTracking(item.user_id)}>查看访问记录</button>
                                                        <button className="btn-text-link" onClick={() => navigate('/support-tickets')}>打开工单中心（未筛选）</button>
                                                    </div>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                            {pagination(changes)}
                        </div>

                        <div className="risk-section half">
                            <div className="section-header">
                                <div>
                                    <h3>异常支付 / 异常任务告警</h3>
                                    <p>近 7 天按用户和类型汇总，次数 ≥ 2 才告警；支付含失败与待支付，任务仅失败。此表仅搜索用户名或昵称。</p>
                                </div>
                            </div>
                            <div className="risk-table-container">
                                <table className="risk-table compact" aria-label="风险告警" aria-busy={alertList.loading}>
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
                                        {tableFeedback(alertList, 5, '当前筛选下暂无风险告警') || alertList.list.map((item) => (
                                            <tr key={`${item.alert_type}-${item.user_id}`}>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.nickname?.trim() || item.username || `用户${item.user_id}`}</strong>
                                                        <span>{item.detail || '暂无详情'}</span>
                                                    </div>
                                                </td>
                                                <td>{item.alert_type === 'payment' ? '支付异常' : item.alert_type === 'task' ? '任务异常' : item.alert_type === 'access' ? '异常访问' : `未知类型（${item.alert_type || '空值'}）`}</td>
                                                <td>{item.alert_count}</td>
                                                <td>{formatDateTime(item.latest_time)}</td>
                                                <td>
                                                    <div className="table-action-stack">
                                                        {item.user_id > 0 ? (
                                                            <button className="btn-text-link" onClick={() => openUserWorkbench(item.user_id)}>查看用户详情</button>
                                                        ) : null}
                                                        {item.user_id > 0 && item.alert_type !== 'access' ? (
                                                            <button className="btn-text-link" onClick={() => openVisitorTracking(item.user_id)}>查看访问记录</button>
                                                        ) : null}
                                                        {item.alert_type === 'payment' || item.alert_type === 'task' ? (
                                                            <button className="btn-text-link" onClick={() => openAlertSourcePage(item)}>{item.alert_type === 'payment' ? '打开订单列表（未筛选）' : '打开AI任务列表（未筛选）'}</button>
                                                        ) : item.alert_type === 'access' && item.visitor_key ? (
                                                            <button className="btn-text-link" onClick={() => openAlertSourcePage(item)}>查看访问记录</button>
                                                        ) : item.alert_type === 'access' ? (
                                                            <span className="risk-unknown-source">缺少访客标识，无法跳转</span>
                                                        ) : <span className="risk-unknown-source">未知类型，无对应来源入口</span>}
                                                        <button className="btn-text-link" onClick={() => navigate('/support-tickets')}>打开工单中心（未筛选）</button>
                                                    </div>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                            {pagination(alertList)}
                        </div>
                    </div>

                    <div className="risk-section">
                        <div className="section-header">
                            <div>
                                <h3>用户风险标签</h3>
                                <p>共享设备与换绑记录不限时间；失败任务及异常支付为近 7 天，支付含失败与待支付。标签供人工核查，列表入口不创建工单或执行账号处置。</p>
                            </div>
                        </div>
                        <div className="risk-table-container">
                            <table className="risk-table" aria-label="风险用户" aria-busy={users.loading}>
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
                                    {tableFeedback(users, 7, '当前筛选下暂无风险用户') || users.list.map((item) => (
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
                                                        <span key={`${item.user_id}-${tag}`} className="inline-tag danger">{tag === '近期换绑设备' ? '有设备换绑记录' : tag}</span>
                                                    )) : <span className="inline-tag">待观察</span>}
                                                </div>
                                            </td>
                                            <td>
                                                <div className="table-action-stack">
                                                    <button className="btn-text-link" onClick={() => openUserWorkbench(item.user_id)}>查看用户详情</button>
                                                    <button className="btn-text-link" onClick={() => openVisitorTracking(item.user_id)}>查看访问记录</button>
                                                    <button className="btn-text-link" onClick={() => navigate(item.abnormal_payment_count > 0 ? '/recharge' : '/ai-tasks')}>
                                                        {item.abnormal_payment_count > 0 ? '打开订单列表（未筛选）' : '打开AI任务列表（未筛选）'}
                                                    </button>
                                                    <button className="btn-text-link" onClick={() => navigate('/support-tickets')}>打开工单中心（未筛选）</button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                        {pagination(users)}
                    </div>
                </div>
            </div>
        </>
    );
};

export default RiskControl;
