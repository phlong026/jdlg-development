import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    FiActivity,
    FiAlertCircle,
    FiChevronRight,
    FiClock,
    FiCreditCard,
    FiDollarSign,
    FiFileText,
    FiRefreshCw,
    FiTrendingUp,
    FiUserCheck,
    FiUsers,
    FiZap,
} from 'react-icons/fi';
import {
    getDashboardOverview,
    getDashboardTodoItems,
    type DashboardOverview,
    type DashboardTodoItem,
    type DashboardTrendItem,
    getDashboardTrends,
} from '../api/stats';
import './dashboard.scss';

interface RecentUserItem {
    id: string;
    username: string;
}

const RECENT_USERS_STORAGE_KEY = 'user-workbench-recent-users';

const emptyOverview: DashboardOverview = {
    total_users: 0,
    today_new_users: 0,
    yesterday_new_users: 0,
    today_income_fen: 0,
    yesterday_income_fen: 0,
    today_orders: 0,
    yesterday_orders: 0,
    today_success_orders: 0,
    yesterday_success_orders: 0,
    today_image_tasks: 0,
    today_video_tasks: 0,
    yesterday_tasks: 0,
    today_failed_tasks: 0,
    yesterday_failed_tasks: 0,
    pending_certifications: 0,
};

const formatMoneyYuan = (fen: number) => {
    const yuan = fen / 100;
    if (yuan >= 10000) {
        return `${(yuan / 10000).toFixed(1)}w`;
    }
    return yuan.toLocaleString('zh-CN', { maximumFractionDigits: yuan % 1 === 0 ? 0 : 2 });
};

const formatCompact = (value: number) => (value >= 10000 ? `${(value / 10000).toFixed(1)}w` : String(value));

/** 环比文案：昨为 0 今为 0 → 持平；昨 0 今>0 → 新增；否则百分比。涨红跌绿（国内电商后台惯例）。 */
const buildDelta = (today: number, yesterday: number, unit = '') => {
    if (today === 0 && yesterday === 0) {
        return { text: '— 持平', tone: 'flat' as const };
    }
    if (yesterday === 0) {
        return { text: `新增 ${today}${unit}`, tone: 'up' as const };
    }
    const pct = Math.round(((today - yesterday) / yesterday) * 100);
    if (pct === 0) {
        return { text: '— 持平', tone: 'flat' as const };
    }
    return pct > 0
        ? { text: `↑ ${pct}%`, tone: 'up' as const }
        : { text: `↓ ${Math.abs(pct)}%`, tone: 'down' as const };
};

const CHART_W = 960;
const CHART_H = 210;
const CHART_PAD = { left: 46, right: 14, top: 14, bottom: 26 };

type TrendMetricKey = 'income' | 'orders' | 'users' | 'tasks';

const TREND_METRICS: { key: TrendMetricKey; label: string; drillPath: string }[] = [
    { key: 'income', label: '充值收入', drillPath: '/recharge' },
    { key: 'orders', label: '订单量', drillPath: '/recharge' },
    { key: 'users', label: '新增用户', drillPath: '/users' },
    { key: 'tasks', label: 'AI 任务', drillPath: '/ai-tasks' },
];

const TODO_ICONS: Record<string, React.ReactNode> = {
    certifications: <FiUserCheck />,
    tickets: <FiFileText />,
    tickets_overdue: <FiAlertCircle />,
    leads_todo: <FiTrendingUp />,
    leads_overdue: <FiClock />,
    failed_tasks_7d: <FiZap />,
};

const Dashboard: React.FC = () => {
    const navigate = useNavigate();
    const [loading, setLoading] = useState(true);
    const [overview, setOverview] = useState<DashboardOverview>(emptyOverview);
    const [trends, setTrends] = useState<DashboardTrendItem[]>([]);
    const [todoItems, setTodoItems] = useState<DashboardTodoItem[]>([]);
    const [recentUsers, setRecentUsers] = useState<RecentUserItem[]>([]);
    const [loadedAt, setLoadedAt] = useState<Date | null>(null);
    const [trendMetric, setTrendMetric] = useState<TrendMetricKey>('income');
    const [trendRange, setTrendRange] = useState<7 | 30>(7);
    const [hoverIndex, setHoverIndex] = useState<number | null>(null);
    const chartWrapRef = useRef<HTMLDivElement>(null);

    const loadDashboard = useCallback(async (range: 7 | 30 = trendRange) => {
        setLoading(true);
        try {
            const recoveryPage = window.location.pathname;
            const [overviewRes, trendRes, todoRes] = await Promise.all([
                getDashboardOverview(),
                getDashboardTrends(range),
                getDashboardTodoItems(),
            ]);
            setOverview(overviewRes || emptyOverview);
            setTrends(trendRes || []);
            setTodoItems(todoRes || []);
            setLoadedAt(new Date());
            reportPageLoadRecovered("加载总控台数据失败", recoveryPage);
        } catch (error) {
            console.error('加载总控台数据失败:', error);
            reportPageLoadError(error, '加载总控台数据失败');
        } finally {
            setLoading(false);
        }
    }, [trendRange]);

    useEffect(() => {
        void loadDashboard();
        try {
            const raw = localStorage.getItem(RECENT_USERS_STORAGE_KEY);
            if (raw) {
                const parsed = JSON.parse(raw) as RecentUserItem[];
                setRecentUsers(Array.isArray(parsed) ? parsed : []);
            }
        } catch (error) {
            console.error('读取最近访问用户失败:', error);
        }
        // 每分钟自动刷新（页面切到后台时跳过，回来后立即补一次）
        const timer = window.setInterval(() => {
            if (!document.hidden) {
                void loadDashboard();
            }
        }, 60000);
        const onVisible = () => {
            if (!document.hidden) {
                void loadDashboard();
            }
        };
        document.addEventListener('visibilitychange', onVisible);
        return () => {
            window.clearInterval(timer);
            document.removeEventListener('visibilitychange', onVisible);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // 超时的排最前、数量多的靠前——运营从上往下处理即可
    const actionItems = useMemo(
        () => todoItems
            .filter(item => item.kind === 'action' && item.count > 0)
            .sort((a, b) => (b.urgent ? 1 : 0) - (a.urgent ? 1 : 0) || b.count - a.count),
        [todoItems],
    );
    const watchItems = useMemo(() => todoItems.filter(item => item.kind === 'watch'), [todoItems]);
    const urgentCount = useMemo(() => todoItems.filter(item => item.kind === 'action' && item.urgent && item.count > 0).length, [todoItems]);
    const todoTotal = useMemo(() => actionItems.reduce((sum, item) => sum + item.count, 0), [actionItems]);

    const todayTasks = overview.today_image_tasks + overview.today_video_tasks;
    const successRate = todayTasks === 0 ? null : ((todayTasks - overview.today_failed_tasks) / todayTasks) * 100;
    const yesterdayRate = overview.yesterday_tasks === 0 ? null : ((overview.yesterday_tasks - overview.yesterday_failed_tasks) / overview.yesterday_tasks) * 100;

    // 健康状态用可解释口径：有超时待办或失败率异常才亮黄，不再依赖魔法数字阈值
    const health = useMemo(() => {
        if (urgentCount > 0) {
            return {
                label: '需关注',
                description: `有 ${urgentCount} 项超时待办（工单或线索），建议今天清掉。`,
                tone: 'warning' as const,
            };
        }
        if (successRate !== null && successRate < 90 && todayTasks >= 10) {
            return {
                label: '履约承压',
                description: `今日任务失败率 ${(100 - successRate).toFixed(1)}%，建议先排查失败原因。`,
                tone: 'danger' as const,
            };
        }
        return {
            label: '运行平稳',
            description: '今日核心经营与履约指标正常。',
            tone: 'success' as const,
        };
    }, [successRate, todayTasks, urgentCount]);

    const kpiCards = useMemo(() => {
        const cards = [
            {
                key: 'income',
                label: '今日充值收入',
                value: `¥${formatMoneyYuan(overview.today_income_fen)}`,
                delta: buildDelta(overview.today_income_fen, overview.yesterday_income_fen),
                sub: '较昨日 · 微信支付成功口径',
                path: '/recharge',
                icon: <FiDollarSign />,
            },
            {
                key: 'orders',
                label: '成功订单',
                value: `${overview.today_success_orders}`,
                delta: buildDelta(overview.today_success_orders, overview.yesterday_success_orders, '单'),
                sub: `今日下单 ${overview.today_orders} 单`,
                path: '/recharge',
                icon: <FiCreditCard />,
            },
            {
                key: 'users',
                label: '新增用户',
                value: `${overview.today_new_users}`,
                delta: buildDelta(overview.today_new_users, overview.yesterday_new_users, '人'),
                sub: `累计 ${formatCompact(overview.total_users)} 人`,
                path: '/users',
                icon: <FiUsers />,
            },
            {
                key: 'tasks',
                label: 'AI 任务量',
                value: `${formatCompact(todayTasks)}`,
                delta: buildDelta(todayTasks, overview.yesterday_tasks, '个'),
                sub: `图片 ${overview.today_image_tasks} · 视频 ${overview.today_video_tasks}`,
                path: '/ai-tasks',
                icon: <FiActivity />,
            },
        ];
        if (successRate !== null) {
            cards.push({
                key: 'rate',
                label: '任务成功率',
                value: `${successRate.toFixed(1)}%`,
                delta: successRate !== null && yesterdayRate !== null
                    ? (() => {
                        const diff = successRate - yesterdayRate;
                        if (Math.abs(diff) < 0.05) return { text: '— 持平', tone: 'flat' as const };
                        return diff > 0
                            ? { text: `↑ ${diff.toFixed(1)}pp`, tone: 'up' as const }
                            : { text: `↓ ${Math.abs(diff).toFixed(1)}pp`, tone: 'down' as const };
                    })()
                    : { text: '— 昨日无任务', tone: 'flat' as const },
                sub: `今日失败 ${overview.today_failed_tasks} 个`,
                path: '/ai-tasks',
                icon: <FiZap />,
            });
        }
        cards.push({
            key: 'todo',
            label: '待我处理',
            value: `${todoTotal}`,
            delta: { text: urgentCount > 0 ? `${urgentCount} 项超时` : '无超时项', tone: urgentCount > 0 ? ('up' as const) : ('flat' as const) },
            sub: '点击查看下方待办清单',
            path: '',
            icon: <FiClock />,
        });
        return cards;
    }, [overview, successRate, yesterdayRate, todayTasks, todoTotal, urgentCount]);

    /* ───────────── 趋势图 ───────────── */

    // 主序列：AI 任务用「图片+视频合计」画主线；视频另以虚线单独呈现，便于区分两条业务线
    const trendSeries = useMemo(() => {
        switch (trendMetric) {
            case 'income':
                return trends.map(item => item.income_fen / 100);
            case 'orders':
                return trends.map(item => item.order_count);
            case 'users':
                return trends.map(item => item.new_users);
            case 'tasks':
            default:
                return trends.map(item => item.image_tasks + item.video_tasks);
        }
    }, [trendMetric, trends]);

    const trendSecondary = useMemo(
        () => (trendMetric === 'tasks' ? trends.map(item => item.video_tasks) : null),
        [trendMetric, trends],
    );

    const trendMax = useMemo(() => Math.max(...trendSeries, 1) * 1.15, [trendSeries]);

    const trendGeom = useMemo(() => {
        const innerW = CHART_W - CHART_PAD.left - CHART_PAD.right;
        const innerH = CHART_H - CHART_PAD.top - CHART_PAD.bottom;
        const n = trendSeries.length;
        const x = (i: number) => CHART_PAD.left + (n <= 1 ? innerW / 2 : (innerW * i) / (n - 1));
        const y = (v: number) => CHART_PAD.top + innerH - (v / trendMax) * innerH;
        const polyline = trendSeries.map((v, i) => `${x(i)},${y(v)}`).join(' ');
        const baseline = CHART_H - CHART_PAD.bottom;
        const areaPath = polyline
            ? `M ${x(0)} ${baseline} L ${trendSeries.map((v, i) => `${x(i)} ${y(v)}`).join(' L ')} L ${x(n - 1)} ${baseline} Z`
            : '';
        const secondaryPolyline = trendSecondary
            ? trendSecondary.map((v, i) => `${x(i)},${y(v)}`).join(' ')
            : '';
        return { x, y, polyline, areaPath, secondaryPolyline, innerW, innerH, baseline };
    }, [trendMax, trendSeries, trendSecondary]);

    const yTicks = useMemo(() => {
        const metric = TREND_METRICS.find(m => m.key === trendMetric);
        return [0, 1, 2, 3, 4].map(k => {
            const raw = (trendMax * (4 - k)) / 4;
            const text = metric?.key === 'income'
                ? (raw >= 1000 ? `${(raw / 1000).toFixed(1)}k` : Math.round(raw).toString())
                : Math.round(raw).toString();
            return { y: CHART_PAD.top + ((CHART_H - CHART_PAD.top - CHART_PAD.bottom) * k) / 4, text };
        });
    }, [trendMax, trendMetric]);

    const handleChartMove = (event: React.MouseEvent<SVGSVGElement>) => {
        if (!trendSeries.length) return;
        const rect = event.currentTarget.getBoundingClientRect();
        const px = ((event.clientX - rect.left) / rect.width) * CHART_W;
        const innerW = CHART_W - CHART_PAD.left - CHART_PAD.right;
        const n = trendSeries.length;
        const index = Math.max(0, Math.min(n - 1, Math.round(((px - CHART_PAD.left) / innerW) * (n - 1))));
        setHoverIndex(index);
    };

    const chartTooltip = useMemo(() => {
        if (hoverIndex === null || !trends[hoverIndex]) return null;
        const item = trends[hoverIndex];
        const prev = hoverIndex > 0 ? trendSeries[hoverIndex - 1] : null;
        const value = trendSeries[hoverIndex];
        const metric = TREND_METRICS.find(m => m.key === trendMetric)!;
        // income 序列存的已经是元；其余指标是计数
        const valueText = metric.key === 'income'
            ? `¥${value.toLocaleString('zh-CN', { maximumFractionDigits: 2 })}`
            : `${value}`;
        const diffText = prev === null || prev === 0 ? '' : ` · 环比 ${value >= prev ? '+' : ''}${Math.round(((value - prev) / prev) * 100)}%`;
        const breakText = trendMetric === 'tasks'
            ? ` · 图片 ${item.image_tasks} · 视频 ${item.video_tasks}`
            : '';
        return { item, valueText: `${valueText}${breakText}`, diffText, metric };
    }, [hoverIndex, trendMetric, trends, trendSeries]);

    const switchTrendRange = async (range: 7 | 30) => {
        if (range === trendRange) return;
        setTrendRange(range);
        setLoading(true);
        try {
            const recoveryPage = window.location.pathname;
            const trendRes = await getDashboardTrends(range);
            setTrends(trendRes || []);
            reportPageLoadRecovered("加载趋势数据失败", recoveryPage);
        } catch (error) {
            console.error('切换趋势范围失败:', error);
            reportPageLoadError(error, '加载趋势数据失败');
        } finally {
            setLoading(false);
        }
    };

    const scrollToTodo = () => {
        document.querySelector('.dash-card-todo')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };

    const loadedAtText = loadedAt
        ? `数据截至 ${loadedAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`
        : '数据加载中';

    return (
        <div className="dashboard-page">
            <div className="dash-pagehead">
                <h1>后台总控台</h1>
                <span className={`dash-health ${health.tone}`}><i />{health.label}</span>
                <div className="dash-pagehead-tip" title={health.description}>{health.description}</div>
                <div className="dash-meta">
                    <span>{loadedAtText} · 每分钟自动刷新</span>
                    <button className="dash-btn-refresh" onClick={() => void loadDashboard()} disabled={loading}>
                        <FiRefreshCw />
                        {loading ? '刷新中…' : '刷新'}
                    </button>
                </div>
            </div>

            <section className="dash-kpi-band">
                {kpiCards.map(card => (
                    <button
                        key={card.key}
                        className="dash-kpi"
                        onClick={card.key === 'todo' ? scrollToTodo : () => navigate(card.path)}
                    >
                        <span className="dash-kpi-label">{card.icon}{card.label}</span>
                        <span className="dash-kpi-value">{card.value}</span>
                        <span className={`dash-kpi-delta ${card.delta.tone}`}>{card.delta.text}</span>
                        <span className="dash-kpi-sub">{card.sub}</span>
                    </button>
                ))}
            </section>

            <div className="dash-workspace">
                <div className="dash-main-col">
                    <section className="dash-card dash-card-todo">
                        <div className="dash-card-head">
                            <h3>今日待办</h3>
                            {urgentCount > 0 && <span className="dash-badge-urgent">{urgentCount} 项超时</span>}
                            <span className="dash-card-note">处理完成后实时减少</span>
                        </div>
                        {actionItems.length === 0 ? (
                            <div className="dash-empty-ok">当前没有需要处理的待办。</div>
                        ) : (
                            <div className="dash-todo-list">
                                {actionItems.map(item => (
                                    <button key={item.key} className="dash-todo-row" onClick={() => navigate(item.path)}>
                                        <span className={`dash-todo-ico ${item.urgent ? 'hot' : 'calm'}`}>
                                            {TODO_ICONS[item.key] ?? <FiChevronRight />}
                                        </span>
                                        <span className="dash-todo-main">
                                            <span className="dash-todo-title">
                                                {item.label}
                                                {item.urgent && <em className="dash-tag-urgent">超时</em>}
                                            </span>
                                            {item.hint && <span className="dash-todo-sub">{item.hint}</span>}
                                        </span>
                                        <span className="dash-todo-num">{item.count}</span>
                                        <span className="dash-todo-go"><FiChevronRight /></span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </section>

                    <section className="dash-card">
                        <div className="dash-card-head">
                            <h3>经营趋势</h3>
                            {trendMetric === 'tasks' && (
                                <span className="dash-trend-legend">
                                    <i className="main" />合计
                                    <i className="secondary" />视频（另计）
                                </span>
                            )}
                            <div className="dash-trend-tools">
                                <div className="dash-seg">
                                    {TREND_METRICS.map(metric => (
                                        <button
                                            key={metric.key}
                                            className={trendMetric === metric.key ? 'on' : ''}
                                            onClick={() => setTrendMetric(metric.key)}
                                        >
                                            {metric.label}
                                        </button>
                                    ))}
                                </div>
                                <div className="dash-seg">
                                    {([7, 30] as const).map(range => (
                                        <button
                                            key={range}
                                            className={trendRange === range ? 'on' : ''}
                                            onClick={() => void switchTrendRange(range)}
                                        >
                                            {range} 天
                                        </button>
                                    ))}
                                </div>
                            </div>
                        </div>
                        {trendSeries.length === 0 ? (
                            <div className="dash-empty">暂无趋势数据。</div>
                        ) : (
                            <div className="dash-chart-wrap" ref={chartWrapRef}>
                                {chartTooltip && (
                                    <div
                                        className="dash-chart-tip"
                                        style={{
                                            left: `${(trendGeom.x(hoverIndex!) / CHART_W) * 100}%`,
                                            top: `${(trendGeom.y(trendSeries[hoverIndex!]) / CHART_H) * 100}%`,
                                        }}
                                    >
                                        <span className="dash-chart-tip-date">{chartTooltip.item.label}{chartTooltip.diffText}</span>
                                        <b>{chartTooltip.valueText}</b>
                                    </div>
                                )}
                                <svg
                                    viewBox={`0 0 ${CHART_W} ${CHART_H}`}
                                    preserveAspectRatio="none"
                                    onMouseMove={handleChartMove}
                                    onMouseLeave={() => setHoverIndex(null)}
                                    onClick={() => navigate(TREND_METRICS.find(m => m.key === trendMetric)!.drillPath)}
                                >
                                    {yTicks.map(tick => (
                                        <g key={tick.y}>
                                            <line
                                                x1={CHART_PAD.left}
                                                x2={CHART_W - CHART_PAD.right}
                                                y1={tick.y}
                                                y2={tick.y}
                                                className="dash-chart-grid"
                                            />
                                            <text x={CHART_PAD.left - 8} y={tick.y + 4} textAnchor="end" className="dash-chart-axis">
                                                {tick.text}
                                            </text>
                                        </g>
                                    ))}
                                    {trends.map((item, i) => {
                                        const step = trendRange === 7 ? 1 : 3;
                                        if (i % step !== 0 && i !== trends.length - 1) return null;
                                        return (
                                            <text
                                                key={item.date}
                                                x={trendGeom.x(i)}
                                                y={CHART_H - 8}
                                                textAnchor="middle"
                                                className="dash-chart-axis"
                                            >
                                                {item.label}
                                            </text>
                                        );
                                    })}
                                    <path d={trendGeom.areaPath} className="dash-chart-area" />
                                    <polyline points={trendGeom.polyline} className="dash-chart-line" />
                                    {trendSecondary && (
                                        <polyline points={trendGeom.secondaryPolyline} className="dash-chart-line dash-chart-line-secondary" />
                                    )}
                                    {hoverIndex !== null && (
                                        <>
                                            <line
                                                x1={trendGeom.x(hoverIndex)}
                                                x2={trendGeom.x(hoverIndex)}
                                                y1={CHART_PAD.top}
                                                y2={CHART_H - CHART_PAD.bottom}
                                                className="dash-chart-hover-line"
                                            />
                                            <circle
                                                cx={trendGeom.x(hoverIndex)}
                                                cy={trendGeom.y(trendSeries[hoverIndex])}
                                                r={4}
                                                className="dash-chart-hover-dot"
                                            />
                                            {trendSecondary && (
                                                <circle
                                                    cx={trendGeom.x(hoverIndex)}
                                                    cy={trendGeom.y(trendSecondary[hoverIndex])}
                                                    r={3}
                                                    className="dash-chart-hover-dot dash-chart-hover-dot-secondary"
                                                />
                                            )}
                                        </>
                                    )}
                                </svg>
                            </div>
                        )}
                    </section>
                </div>

                <div className="dash-side-col">
                    <section className="dash-card">
                        <div className="dash-card-head">
                            <h3>异常监控</h3>
                            <span className="dash-card-note">只看计数，明细进各模块页</span>
                        </div>
                        <div className="dash-watch-list">
                            {watchItems.map(item => (
                                <button key={item.key} className="dash-watch-row" onClick={() => navigate(item.path)}>
                                    <span className="dash-watch-title">
                                        {item.label}
                                        {item.hint && <small>{item.hint}</small>}
                                    </span>
                                    <span className={`dash-watch-num ${item.count > 0 ? 'warn' : 'ok'}`}>{item.count}</span>
                                    <FiChevronRight />
                                </button>
                            ))}
                        </div>
                    </section>

                    <section className="dash-card">
                        <div className="dash-card-head">
                            <h3>最近访问用户</h3>
                        </div>
                        {recentUsers.length === 0 ? (
                            <div className="dash-empty">最近没有打开过用户 360。</div>
                        ) : (
                            <div className="dash-recent-list">
                                {recentUsers.slice(0, 5).map(item => (
                                    <button
                                        key={item.id}
                                        className="dash-recent-row"
                                        onClick={() => navigate(`/user-workbench?userId=${item.id}`)}
                                    >
                                        <span className="dash-recent-name">{item.username}</span>
                                        <span className="dash-recent-id">ID {item.id}</span>
                                        <FiChevronRight />
                                    </button>
                                ))}
                            </div>
                        )}
                    </section>
                </div>
            </div>
        </div>
    );
};

export default Dashboard;
