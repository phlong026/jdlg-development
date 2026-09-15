import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    FiActivity,
    FiAlertCircle,
    FiBarChart2,
    FiChevronRight,
    FiClock,
    FiCreditCard,
    FiLayers,
    FiRefreshCw,
    FiShield,
    FiUsers,
    FiZap,
} from 'react-icons/fi';
import Layout from '../component/layout';
import {
    getDashboardOverview,
    getDashboardTodos,
    getDashboardTrends,
    type DashboardFailedTaskItem,
    type DashboardOverview,
    type DashboardTodoCertificationItem,
    type DashboardTodoUserItem,
    type DashboardTodos,
    type DashboardTrendItem,
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
    today_orders: 0,
    today_success_orders: 0,
    today_success_amount: 0,
    today_image_tasks: 0,
    today_video_tasks: 0,
    today_failed_tasks: 0,
    pending_certifications: 0,
    pending_exceptions: 0,
};

const emptyTodos: DashboardTodos = {
    counts: {
        pending_certifications: 0,
        zero_stones_users: 0,
        pending_wechat_users: 0,
        failed_tasks: 0,
    },
    pending_certifications: [],
    zero_stones_users: [],
    pending_wechat_users: [],
    failed_tasks: [],
};

const formatDateTime = (value?: string | null) => {
    if (!value) return '暂无';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const formatCompactNumber = (value: number) => {
    if (value >= 10000) {
        return `${(value / 10000).toFixed(1)}w`;
    }
    return String(value);
};

const getUserDisplayName = (item?: DashboardTodoUserItem | DashboardFailedTaskItem | null) => {
    if (!item) return '';
    return item.nickname?.trim() || item.username || `用户${item.user_id}`;
};

const getCertificationTypeLabel = (item: DashboardTodoCertificationItem) => {
    return item.type === 'designer' ? '个人设计师认证' : '企业认证';
};

const getTaskTypeLabel = (item: DashboardFailedTaskItem) => {
    return item.task_type === 'video' ? '视频任务' : '图片任务';
};

const buildTrendPolyline = (values: number[], width = 260, height = 112, padding = 14) => {
    if (!values.length) {
        return '';
    }
    const maxValue = Math.max(...values, 1);
    const innerWidth = Math.max(width - padding * 2, 1);
    const innerHeight = Math.max(height - padding * 2, 1);
    const step = values.length === 1 ? 0 : innerWidth / (values.length - 1);
    return values.map((value, index) => {
        const x = padding + step * index;
        const y = padding + innerHeight - (value / maxValue) * innerHeight;
        return `${x},${y}`;
    }).join(' ');
};

const buildTrendAreaPath = (values: number[], width = 260, height = 112, padding = 14) => {
    if (!values.length) {
        return '';
    }
    const polyline = buildTrendPolyline(values, width, height, padding);
    if (!polyline) {
        return '';
    }
    const points = polyline.split(' ');
    const baseline = height - padding;
    const firstPoint = points[0]?.split(',') || [`${padding}`, `${baseline}`];
    const lastPoint = points[points.length - 1]?.split(',') || [`${width - padding}`, `${baseline}`];
    return `M ${firstPoint[0]} ${baseline} L ${points.map(point => point.replace(',', ' ')).join(' L ')} L ${lastPoint[0]} ${baseline} Z`;
};

const Dashboard: React.FC = () => {
    const navigate = useNavigate();
    const [loading, setLoading] = useState(true);
    const [overview, setOverview] = useState<DashboardOverview>(emptyOverview);
    const [trends, setTrends] = useState<DashboardTrendItem[]>([]);
    const [todos, setTodos] = useState<DashboardTodos>(emptyTodos);
    const [recentUsers, setRecentUsers] = useState<RecentUserItem[]>([]);

    const loadDashboard = async () => {
        setLoading(true);
        try {
            const [overviewRes, trendRes, todoRes] = await Promise.all([
                getDashboardOverview(),
                getDashboardTrends(),
                getDashboardTodos(),
            ]);
            setOverview(overviewRes || emptyOverview);
            setTrends(trendRes || []);
            setTodos(todoRes || emptyTodos);
        } catch (error) {
            console.error('加载总控台数据失败:', error);
            alert('加载总控台数据失败');
        } finally {
            setLoading(false);
        }
    };

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
    }, []);

    const todayTaskTotal = overview.today_image_tasks + overview.today_video_tasks;

    const taskSuccessRateText = useMemo(() => {
        if (todayTaskTotal === 0) {
            return '—';
        }
        return `${(((todayTaskTotal - overview.today_failed_tasks) / todayTaskTotal) * 100).toFixed(1)}%`;
    }, [overview.today_failed_tasks, todayTaskTotal]);

    const heroHighlights = useMemo(() => {
        return [
            {
                key: 'new-users',
                label: '今日新增',
                value: formatCompactNumber(overview.today_new_users),
                detail: `总用户 ${formatCompactNumber(overview.total_users)}`,
            },
            {
                key: 'revenue',
                label: '成功成交',
                value: formatCompactNumber(overview.today_success_amount),
                detail: `成功订单 ${overview.today_success_orders} 单`,
            },
            {
                key: 'task-success-rate',
                label: '任务成功率',
                value: taskSuccessRateText,
                detail: todayTaskTotal === 0 ? '今天还没有任务' : `失败 ${overview.today_failed_tasks} 条`,
            },
        ];
    }, [overview.today_failed_tasks, overview.today_new_users, overview.today_success_amount, overview.today_success_orders, overview.total_users, taskSuccessRateText, todayTaskTotal]);

    const pillarCards = useMemo(() => {
        return [
            {
                key: 'users',
                title: '用户与增长',
                value: formatCompactNumber(overview.total_users),
                icon: FiUsers,
                tone: 'neutral',
                path: '/users',
                actionLabel: '查看用户中心',
                description: '先看今天有没有新增，再判断增长是否放缓。',
                stats: [
                    { label: '今日新增', value: `${overview.today_new_users} 人` },
                    { label: '异常用户', value: `${overview.pending_exceptions} 个` },
                ],
            },
            {
                key: 'orders',
                title: '交易与收入',
                value: formatCompactNumber(overview.today_success_amount),
                icon: FiCreditCard,
                tone: 'warm',
                path: '/recharge',
                actionLabel: '进入订单中心',
                description: '把金额和成功单量放在一起看，更容易判断收入质量。',
                stats: [
                    { label: '今日订单', value: `${overview.today_orders} 单` },
                    { label: '成功支付', value: `${overview.today_success_orders} 单` },
                ],
            },
            {
                key: 'tasks',
                title: '履约与异常',
                value: formatCompactNumber(todayTaskTotal),
                icon: FiActivity,
                tone: 'warning',
                path: '/ai-tasks',
                actionLabel: '进入AI任务中心',
                description: '这里同时看任务量、失败量和异常池，最能反映交付压力。',
                stats: [
                    { label: '图片 / 视频', value: `${overview.today_image_tasks} / ${overview.today_video_tasks}` },
                    { label: '失败 / 异常', value: `${overview.today_failed_tasks} / ${overview.pending_exceptions}` },
                ],
            },
        ];
    }, [overview.pending_exceptions, overview.today_failed_tasks, overview.today_image_tasks, overview.today_new_users, overview.today_orders, overview.today_success_amount, overview.today_success_orders, overview.today_video_tasks, overview.total_users, todayTaskTotal]);

    const healthStatus = useMemo(() => {
        if (overview.today_failed_tasks >= 8 || overview.pending_exceptions >= 20) {
            return {
                label: '高压预警',
                description: '失败任务或异常队列偏高，建议先处理履约与客服压力。',
                tone: 'danger',
            };
        }
        if (overview.today_failed_tasks >= 3 || overview.pending_certifications >= 8) {
            return {
                label: '关注中',
                description: '业务整体可控，但审核或任务失败已经开始堆积。',
                tone: 'warning',
            };
        }
        return {
            label: '运行平稳',
            description: '今天核心经营与履约指标总体正常，可以重点看增长与效率。',
            tone: 'success',
        };
    }, [overview.pending_certifications, overview.pending_exceptions, overview.today_failed_tasks]);

    const primaryActions = useMemo(() => {
        return [
            { label: '处理失败任务', hint: `${todos.counts.failed_tasks} 条待排查`, path: '/ai-tasks', icon: FiZap },
            { label: '审核认证申请', hint: `${overview.pending_certifications} 条待处理`, path: '/certification', icon: FiClock },
            { label: '排查异常用户', hint: `${overview.pending_exceptions} 个异常点`, path: '/users', icon: FiAlertCircle },
        ];
    }, [overview.pending_certifications, overview.pending_exceptions, todos.counts.failed_tasks]);

    const systemEntryGroups = useMemo(() => {
        return [
            {
                title: '用户与设计师',
                items: [
                    { label: '用户中心', path: '/users', icon: FiUsers },
                    { label: '设计师中心', path: '/designer-center', icon: FiUsers },
                    { label: '会员运营', path: '/membership-operations', icon: FiUsers },
                ],
            },
            {
                title: '交易与运营',
                items: [
                    { label: '订单中心', path: '/recharge', icon: FiCreditCard },
                    { label: '分销邀请', path: '/distribution', icon: FiBarChart2 },
                    { label: '内容分析', path: '/content-analytics', icon: FiBarChart2 },
                ],
            },
            {
                title: '内容与AI',
                items: [
                    { label: '模板广场', path: '/templates', icon: FiLayers },
                    { label: 'AI任务中心', path: '/ai-tasks', icon: FiActivity },
                    { label: '异常工单', path: '/support-tickets', icon: FiAlertCircle },
                ],
            },
            {
                title: '系统配置',
                items: [
                    { label: 'AI配置', path: '/ai-config', icon: FiRefreshCw },
                    { label: '报表中心', path: '/report-center', icon: FiBarChart2 },
                ],
            },
        ];
    }, []);

    const trendCards = useMemo(() => {
        return [
            {
                key: 'new-users',
                label: '新增用户',
                tone: 'neutral',
                values: trends.map(item => item.new_users),
                latest: trends[trends.length - 1]?.new_users || 0,
                total: trends.reduce((sum, item) => sum + item.new_users, 0),
                peak: Math.max(...trends.map(item => item.new_users), 0),
            },
            {
                key: 'orders',
                label: '订单量',
                tone: 'warm',
                values: trends.map(item => item.order_count),
                latest: trends[trends.length - 1]?.order_count || 0,
                total: trends.reduce((sum, item) => sum + item.order_count, 0),
                peak: Math.max(...trends.map(item => item.order_count), 0),
            },
            {
                key: 'revenue',
                label: '成交金额',
                tone: 'gold',
                values: trends.map(item => item.success_amount),
                latest: trends[trends.length - 1]?.success_amount || 0,
                total: trends.reduce((sum, item) => sum + item.success_amount, 0),
                peak: Math.max(...trends.map(item => item.success_amount), 0),
            },
            {
                key: 'tasks',
                label: 'AI任务量',
                tone: 'blue',
                values: trends.map(item => item.image_tasks + item.video_tasks),
                latest: trends[trends.length - 1] ? (trends[trends.length - 1].image_tasks + trends[trends.length - 1].video_tasks) : 0,
                total: trends.reduce((sum, item) => sum + item.image_tasks + item.video_tasks, 0),
                peak: Math.max(...trends.map(item => item.image_tasks + item.video_tasks), 0),
            },
            {
                key: 'failed',
                label: '失败任务',
                tone: 'danger',
                values: trends.map(item => item.failed_tasks),
                latest: trends[trends.length - 1]?.failed_tasks || 0,
                total: trends.reduce((sum, item) => sum + item.failed_tasks, 0),
                peak: Math.max(...trends.map(item => item.failed_tasks), 0),
            },
        ];
    }, [trends]);

    const exceptionLanes = useMemo(() => {
        return [
            {
                key: 'zero-stones',
                title: '灵石不足用户',
                count: todos.counts.zero_stones_users,
                description: '适合客服或运营先补偿、提醒充值，避免用户卡在下一步。',
                tone: 'gold',
                items: todos.zero_stones_users.map(item => ({
                    key: `stone-${item.user_id}`,
                    title: getUserDisplayName(item),
                    meta: `当前灵石 ${item.stones || 0}`,
                    extra: `ID ${item.user_id}`,
                    onClick: () => navigate(`/user-workbench?userId=${item.user_id}`),
                })),
            },
            {
                key: 'pending-wechat',
                title: '下载权限待核验',
                count: todos.counts.pending_wechat_users,
                description: '重点看未留手机号或授权未完成的用户，减少下载咨询堆积。',
                tone: 'blue',
                items: todos.pending_wechat_users.map(item => ({
                    key: `wechat-${item.user_id}`,
                    title: getUserDisplayName(item),
                    meta: item.enterprise_wechat_contact || '当前未填写企微手机号',
                    extra: `ID ${item.user_id}`,
                    onClick: () => navigate(`/user-workbench?userId=${item.user_id}`),
                })),
            },
            {
                key: 'failed-tasks',
                title: '失败任务池',
                count: todos.counts.failed_tasks,
                description: '优先看失败原因、模型和任务号，便于快速补偿或转工单。',
                tone: 'danger',
                items: todos.failed_tasks.map(item => ({
                    key: `${item.task_type}-${item.task_no}`,
                    title: `${getTaskTypeLabel(item)} · ${getUserDisplayName(item)}`,
                    meta: `${item.scene || '-'} · ${item.model || '模型待回填'}`,
                    extra: item.task_no,
                    onClick: () => navigate('/ai-tasks?tab=' + (item.task_type === 'video' ? 'video' : 'image') + `&taskId=${encodeURIComponent(item.task_no)}`),
                })),
            },
        ];
    }, [navigate, todos]);

    return (
        <Layout title="后台总控台">
            <div className="dashboard-page">
                <section className={`dashboard-hero section-card ${healthStatus.tone}`}>
                    <div className="dashboard-hero-main">
                        <div className="dashboard-hero-head">
                            <div>
                                <div className="cockpit-badge">经营总览</div>
                                <h2>{healthStatus.label}</h2>
                                <p>{healthStatus.description}</p>
                            </div>
                            <button className="btn-secondary" onClick={() => void loadDashboard()} disabled={loading}>
                                <FiRefreshCw />
                                {loading ? '刷新中...' : '刷新数据'}
                            </button>
                        </div>
                        <div className="hero-highlight-grid">
                            {heroHighlights.map((item) => (
                                <div key={item.key} className="hero-highlight-card">
                                    <span>{item.label}</span>
                                    <strong>{item.value}</strong>
                                    <small>{item.detail}</small>
                                </div>
                            ))}
                        </div>
                    </div>

                    <div className="dashboard-hero-side">
                        <div className="dashboard-side-title">
                            <h3>马上处理</h3>
                            <p>先解决会直接影响交付和客服压力的事情。</p>
                        </div>
                        <div className="cockpit-action-list">
                            {primaryActions.map((item) => {
                                const Icon = item.icon;
                                return (
                                    <button key={item.label} className="cockpit-action-item" onClick={() => navigate(item.path)}>
                                        <span className="cockpit-action-icon"><Icon /></span>
                                        <span className="cockpit-action-text">
                                            <strong>{item.label}</strong>
                                            <small>{item.hint}</small>
                                        </span>
                                        <FiChevronRight />
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                </section>

                <section className="pillar-grid">
                    {pillarCards.map((item) => {
                        const Icon = item.icon;
                        return (
                            <div key={item.key} className={`pillar-card section-card ${item.tone}`}>
                                <div className="pillar-card-head">
                                    <span className="pillar-card-icon"><Icon /></span>
                                    <button className="btn-link-action" onClick={() => navigate(item.path)}>
                                        {item.actionLabel}
                                        <FiChevronRight />
                                    </button>
                                </div>
                                <div className="pillar-card-title">{item.title}</div>
                                <div className="pillar-card-value">{item.value}</div>
                                <div className="pillar-card-desc">{item.description}</div>
                                <div className="pillar-card-stats">
                                    {item.stats.map((stat) => (
                                        <div key={stat.label} className="pillar-stat-pill">
                                            <span>{stat.label}</span>
                                            <strong>{stat.value}</strong>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        );
                    })}
                </section>

                <div className="dashboard-workspace">
                    <div className="workspace-main-column">
                        <div className="section-card workbench-card">
                            <div className="section-head">
                                <div>
                                    <h3>今日优先处理</h3>
                                    <p>先处理认证、失败任务和异常用户，再进入具体模块继续深挖，顺序更符合运营日常工作流。</p>
                                </div>
                            </div>
                            <div className="workbench-summary-row">
                                <div className="workbench-summary-pill">
                                    <span>待认证</span>
                                    <strong>{overview.pending_certifications}</strong>
                                </div>
                                <div className="workbench-summary-pill">
                                    <span>失败任务</span>
                                    <strong>{todos.counts.failed_tasks}</strong>
                                </div>
                                <div className="workbench-summary-pill">
                                    <span>下载核验</span>
                                    <strong>{todos.counts.pending_wechat_users}</strong>
                                </div>
                                <div className="workbench-summary-pill">
                                    <span>灵石不足</span>
                                    <strong>{todos.counts.zero_stones_users}</strong>
                                </div>
                            </div>
                            <div className="workbench-priority-layout">
                                <div className="priority-action-grid">
                                    {primaryActions.map((item) => {
                                        const Icon = item.icon;
                                        return (
                                            <button key={item.label} className="priority-action-card" onClick={() => navigate(item.path)}>
                                                <span className="priority-action-icon"><Icon /></span>
                                                <span className="priority-action-content">
                                                    <strong>{item.label}</strong>
                                                    <small>{item.hint}</small>
                                                </span>
                                                <FiChevronRight />
                                            </button>
                                        );
                                    })}
                                </div>
                                <div className="workbench-panel certification-panel">
                                    <div className="panel-head-row">
                                        <div>
                                            <h4>待审核认证</h4>
                                            <p>这里保留今天最需要优先清空的认证队列，避免设计师和企业流程堵塞。</p>
                                        </div>
                                        <button className="btn-link-action" onClick={() => navigate('/certification')}>
                                            进入认证页
                                            <FiChevronRight />
                                        </button>
                                    </div>
                                    {todos.pending_certifications.length === 0 ? (
                                        <div className="empty-inline success">当前没有待审核认证。</div>
                                    ) : (
                                        <div className="queue-list">
                                            {todos.pending_certifications.map((item) => (
                                                <button
                                                    key={item.id}
                                                    className="queue-item"
                                                    onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}
                                                >
                                                    <div>
                                                        <div className="queue-title">用户 {item.user_id} · {getCertificationTypeLabel(item)}</div>
                                                        <div className="queue-desc">
                                                            {item.identity_type ? `${item.identity_type} · ` : ''}申请时间：{formatDateTime(item.created_at)}
                                                        </div>
                                                    </div>
                                                    <FiChevronRight />
                                                </button>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </div>
                        </div>

                        <div className="section-card alert-board-card">
                            <div className="section-head">
                                <div>
                                    <h3>异常处理台</h3>
                                    <p>把高频异常改成横向卡片浏览，避免一列长文字把版面挤垮，也方便快速点进处理。</p>
                                </div>
                                <button className="btn-link-action" onClick={() => navigate('/support-tickets')}>
                                    进入异常工单
                                    <FiChevronRight />
                                </button>
                            </div>
                            <div className="alert-lane-list">
                                {exceptionLanes.map((lane) => (
                                    <div key={lane.key} className={`alert-lane ${lane.tone}`}>
                                        <div className="alert-lane-head">
                                            <div>
                                                <h4>{lane.title}</h4>
                                                <p>{lane.description}</p>
                                            </div>
                                            <strong>{lane.count}</strong>
                                        </div>
                                        {lane.items.length === 0 ? (
                                            <div className="mini-queue-empty">当前没有需要处理的记录。</div>
                                        ) : (
                                            <div className="alert-lane-track">
                                                {lane.items.map((item) => (
                                                    <button key={item.key} className="alert-ticket-card" onClick={item.onClick}>
                                                        <div className="alert-ticket-title">{item.title}</div>
                                                        <div className="alert-ticket-meta">{item.meta}</div>
                                                        <div className="alert-ticket-extra">{item.extra}</div>
                                                    </button>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                ))}
                            </div>
                        </div>

                        <div className="section-card trend-section-card">
                            <div className="section-head">
                                <div>
                                    <h3>最近7天趋势</h3>
                                    <p>改为图表化展示，不再堆长行文字。先看用户、订单、成交、任务和失败变化，再判断问题落在哪条业务线。</p>
                                </div>
                            </div>
                            {trends.length === 0 ? (
                                <div className="empty-inline">暂时没有趋势数据。</div>
                            ) : (
                                <div className="trend-chart-grid">
                                    {trendCards.map((card) => (
                                        <div key={card.key} className={`trend-chart-card ${card.tone}`}>
                                            <div className="trend-chart-head">
                                                <div>
                                                    <span>{card.label}</span>
                                                    <strong>{formatCompactNumber(card.latest)}</strong>
                                                </div>
                                                <div className="trend-chart-stats">
                                                    <small>7日累计 {formatCompactNumber(card.total)}</small>
                                                    <small>峰值 {formatCompactNumber(card.peak)}</small>
                                                </div>
                                            </div>
                                            <svg viewBox="0 0 260 112" className="trend-chart-svg" preserveAspectRatio="none" aria-hidden="true">
                                                <path d={buildTrendAreaPath(card.values)} className="trend-area" />
                                                <polyline points={buildTrendPolyline(card.values)} className="trend-line" />
                                            </svg>
                                        </div>
                                    ))}
                                    <div className="trend-date-strip">
                                        {trends.map((item) => (
                                            <span key={item.date}>{item.label}</span>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="workspace-side-column">
                        <div className="section-card side-card">
                            <div className="section-head compact">
                                <div>
                                    <h3>系统入口</h3>
                                    <p>按业务逻辑重新分组，先看人，再看交易和内容，低频配置放到最后，减少“到处找入口”的障碍。</p>
                                </div>
                            </div>
                            <div className="entry-group-list">
                                {systemEntryGroups.map((group) => (
                                    <div key={group.title} className="entry-group-block">
                                        <div className="entry-group-title">{group.title}</div>
                                        <div className="quick-grid">
                                            {group.items.map((item) => {
                                                const Icon = item.icon;
                                                return (
                                                    <button key={item.label} className="quick-entry" onClick={() => navigate(item.path)}>
                                                        <Icon />
                                                        <span>{item.label}</span>
                                                    </button>
                                                );
                                            })}
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>

                        <div className="section-card side-card">
                            <div className="section-head compact">
                                <div>
                                    <h3>最近访问用户</h3>
                                    <p>继续处理最近正在跟进的用户。</p>
                                </div>
                            </div>
                            {recentUsers.length === 0 ? (
                                <div className="mini-queue-empty">你最近还没有进入过用户360。</div>
                            ) : (
                                <div className="recent-user-list">
                                    {recentUsers.map((item) => (
                                        <button
                                            key={item.id}
                                            className="recent-user-row"
                                            onClick={() => navigate(`/user-workbench?userId=${item.id}`)}
                                        >
                                            <span>{item.username}</span>
                                            <strong>ID {item.id}</strong>
                                        </button>
                                    ))}
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            </div>
        </Layout>
    );
};

export default Dashboard;
