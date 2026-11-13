import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { FiChevronRight, FiRefreshCw } from 'react-icons/fi';
import type { CustomerLeadOverview } from '../api/customerLeads';
import { getCustomerLeadFunnel, CustomerFunnelMetric } from '../api/customerLeads';
import { leadQueues } from '../component/navConfig';
import { funnelStepLabel } from './salesFormat';
import './customerLeadHub.scss';

// 线索阶段：与 customer_leads.status 的四个取值一一对应，颜色沿用常见销售看板的蓝 / 黄 / 绿 / 灰
const STAGES: { key: keyof CustomerLeadOverview; label: string; color: string }[] = [
    { key: 'new_count', label: '新线索', color: '#2f6bff' },
    { key: 'contacted_count', label: '跟进中', color: '#fbc02d' },
    { key: 'converted_count', label: '已成交', color: '#16b8a6' },
    { key: 'invalid_count', label: '无效', color: '#aab8e0' },
];

const RADIUS = 52;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/**
 * 客服服务漏斗（近 30 天，每层都是人数）：常开，只放标题和逐层数字。
 * "进入客服" = 登录人数 + 匿名次数（匿名无账号，只能按次计），层名里标出匿名次数；
 * "提交线索"按留资动作（lead_submit 事件）去重账号——老客户再次留资只更新旧线索，也要算进来。
 */
export const ServiceFunnel: React.FC<{ refreshKey: number }> = ({ refreshKey }) => {
    const [state, setState] = useState<{ loading: boolean; error: string; metrics: CustomerFunnelMetric[] }>({ loading: true, error: '', metrics: [] });
    useEffect(() => {
        let active = true;
        setState(value => ({ ...value, loading: true, error: '' }));
        getCustomerLeadFunnel(30)
            .then(metrics => { if (active) setState({ loading: false, error: '', metrics }); })
            .catch(() => { if (active) setState({ loading: false, error: '漏斗暂不可用', metrics: [] }); });
        return () => { active = false; };
    }, [refreshKey]);
    return <section className="lead-hub-card lead-hub-funnel-wrap">
        <header className="lead-hub-card-head"><h2>客服服务漏斗</h2></header>
        {state.loading ? <p className="lead-hub-funnel-note">正在读取…</p> : state.error ? <p className="lead-hub-funnel-note">{state.error}</p> : (
            <ul className="lead-hub-funnel">
                {state.metrics.map(metric => (
                    <li key={metric.key}>
                        <span>{funnelStepLabel(metric)}</span>
                        <b>{metric.count}</b>
                        <small>{metric.count > 0 ? `${metric.conversion_rate.toFixed(0)}%` : '—'}</small>
                    </li>
                ))}
            </ul>
        )}
    </section>;
};

interface HubProps {
    overview: CustomerLeadOverview;
    loading: boolean;
    updatedAt: Date | null;
    onRefresh: () => void;
    /** 当前队列（?queue=），对应的格子高亮 */
    activeQueue: string;
    /** 生成切换到某个队列的地址：保留地址里的其他参数（如 user_id） */
    queueHref: (key: string) => string;
}

/**
 * 销售线索的概览区：线索简报 + 线索阶段（环形图），桌面端左右并排。
 * 简报里的数字格子就是队列入口（点击切换下方列表），不再另放一条队列胶囊——原来两处显示同样的四个数字。
 * 客户 · 线索 · 商机 的入口卡片在布局层（SubNavCards），三个页面共用。
 */
const CustomerLeadHub: React.FC<HubProps> = ({ overview, loading, updatedAt, onRefresh, activeQueue, queueHref }) => {
    const total = STAGES.reduce((sum, stage) => sum + Number(overview[stage.key] || 0), 0);
    const partnerCount = overview.partner_count || 0;
    const pendingCount = overview.identity_pending_count || 0;
    const stages = STAGES.map((stage) => {
        const count = Number(overview[stage.key] || 0);
        return { ...stage, count, percent: total > 0 ? Math.round((count / total) * 100) : 0 };
    });

    // 环形图：按各阶段占比依次叠加 dash；总数为 0 时只画灰色底环
    let offset = 0;
    const arcs = stages
        .filter((stage) => stage.count > 0 && total > 0)
        .map((stage) => {
            const length = (stage.count / total) * CIRCUMFERENCE;
            const arc = { key: stage.key, color: stage.color, dash: `${length} ${CIRCUMFERENCE - length}`, offset: -offset };
            offset += length;
            return arc;
        });

    return (
        <div className="lead-hub">
            <section className="lead-hub-card">
                <header className="lead-hub-card-head">
                    <h2>线索简报</h2>
                    <button type="button" className="lead-hub-refresh" onClick={onRefresh} disabled={loading}>
                        <FiRefreshCw />
                        刷新
                    </button>
                </header>
                <nav className="lead-hub-queues" aria-label="线索队列">
                    {leadQueues.map((item) => {
                        const count = Number(overview[item.countKey as keyof CustomerLeadOverview] || 0);
                        const active = activeQueue === item.key;
                        return (
                            <Link
                                key={item.key}
                                to={queueHref(item.key)}
                                className={`lead-hub-queue${active ? ' is-active' : ''}${item.alert && count > 0 ? ' is-alert' : ''}`}
                                // 队列是页内筛选，页面级的 page 留给顶部「线索」卡片
                                aria-current={active ? 'true' : undefined}
                                title={item.help}
                            >
                                <span>{item.label}</span>
                                <strong>{count}</strong>
                            </Link>
                        );
                    })}
                </nav>
            </section>

            <section className="lead-hub-card lead-hub-stage-wrap">
                <header className="lead-hub-card-head">
                    <h2>线索阶段</h2>
                    <Link className="lead-hub-more" to="/customer-leads?queue=all">全部<FiChevronRight /></Link>
                </header>
                <div className="lead-hub-stage">
                    <div className="lead-hub-donut">
                        <svg viewBox="0 0 140 140" role="img" aria-label={`线索共 ${total} 条`}>
                            <circle cx="70" cy="70" r={RADIUS} fill="none" stroke="#e8e8ee" strokeWidth="14" />
                            {arcs.map((arc) => (
                                <circle
                                    key={arc.key}
                                    cx="70" cy="70" r={RADIUS} fill="none"
                                    stroke={arc.color} strokeWidth="14"
                                    strokeDasharray={arc.dash} strokeDashoffset={arc.offset}
                                    transform="rotate(-90 70 70)"
                                />
                            ))}
                        </svg>
                        <div className="lead-hub-donut-center">
                            <strong>{total}</strong>
                            <span>线索量</span>
                        </div>
                    </div>
                    <ul className="lead-hub-legend">
                        {stages.map((stage) => (
                            <li key={stage.key}>
                                <i style={{ background: stage.color }} />
                                <span>{stage.label}</span>
                                <b>{stage.count}({stage.percent}%)</b>
                            </li>
                        ))}
                        {partnerCount > 0 && <li><span>合作线索另计</span><b><Link to="/customer-leads?queue=partner">{partnerCount}</Link></b></li>}
                        {pendingCount > 0 && <li><span>身份待确认另计</span><b>{pendingCount}</b></li>}
                    </ul>
                </div>
            </section>
            <ServiceFunnel refreshKey={updatedAt ? updatedAt.getTime() : 0} />
        </div>
    );
};

export default CustomerLeadHub;
