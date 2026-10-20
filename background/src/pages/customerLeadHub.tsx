import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { FiChevronDown, FiChevronRight, FiRefreshCw } from 'react-icons/fi';
import type { CustomerLeadOverview } from '../api/customerLeads';
import { getCustomerLeadFunnel, CustomerFunnelMetric } from '../api/customerLeads';
import { useIsMobile } from '../component/useIsMobile';
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

const pad = (n: number) => String(n).padStart(2, '0');

/**
 * 客服服务漏斗（近 30 天，按登录账号去重）：
 * ① 每层可与服务端 SQL 对账（"提交线索"按 customer_leads 去重账号统计，
 *    不再被同人多次提交虚高）；② 底层"进入客服"依赖小程序埋点——旧版
 * 小程序未发版期间该层数字偏低，只统计到留资过的会话，看板不代表真实进线量。
 */
const ServiceFunnel: React.FC<{ refreshKey: number }> = ({ refreshKey }) => {
    const [state, setState] = useState<{ loading: boolean; error: string; metrics: CustomerFunnelMetric[] }>({ loading: true, error: '', metrics: [] });
    useEffect(() => {
        let active = true;
        setState(value => ({ ...value, loading: true, error: '' }));
        getCustomerLeadFunnel(30)
            .then(metrics => { if (active) setState({ loading: false, error: '', metrics }); })
            .catch(() => { if (active) setState({ loading: false, error: '漏斗暂不可用', metrics: [] }); });
        return () => { active = false; };
    }, [refreshKey]);
    return <details className="lead-hub-card lead-hub-funnel-wrap">
        <summary className="lead-hub-stage-summary"><span>客服服务漏斗 · 近30天</span><FiChevronDown className="lead-hub-stage-caret" aria-hidden /></summary>
        <header className="lead-hub-card-head"><h2>客服服务漏斗</h2><span className="lead-hub-period">近30天 · 按账号去重</span></header>
        {state.loading ? <p className="lead-hub-funnel-note">正在读取…</p> : state.error ? <p className="lead-hub-funnel-note">{state.error}</p> : <>
            <ul className="lead-hub-funnel">
                {state.metrics.map(metric => (
                    <li key={metric.key}>
                        <span>{metric.label}</span>
                        <b>{metric.count}</b>
                        <small>{metric.count > 0 ? `${metric.conversion_rate.toFixed(0)}%` : '—'}</small>
                    </li>
                ))}
            </ul>
            <p className="lead-hub-funnel-note">"提交线索/已成交"按登录账号去重；"进入客服"层在小程序新版埋点全量生效前偏低，仅覆盖留资会话。</p>
        </>}
    </details>;
};

interface HubProps {
    overview: CustomerLeadOverview;
    loading: boolean;
    updatedAt: Date | null;
    onRefresh: () => void;
}

/**
 * 销售线索的概览区：线索简报 + 线索阶段（环形图），桌面端左右并排，让下面的队列与列表尽早出现在首屏。
 * 客户 · 线索 · 商机 的入口卡片在布局层（SubNavCards），三个页面共用。
 */
const CustomerLeadHub: React.FC<HubProps> = ({ overview, loading, updatedAt, onRefresh }) => {
    const isMobile = useIsMobile();
    // 环图卡 447px 是线索列表沉到 1565px 的主因：手机端默认收起（摘要带总条数），桌面常开
    const [stageOpen, setStageOpen] = useState<boolean | null>(null);
    const stageExpanded = stageOpen ?? !isMobile;
    const total = overview.total_count || 0;
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
                    <span className="lead-hub-period">全量概览</span>
                    <button type="button" className="lead-hub-refresh" onClick={onRefresh} disabled={loading}>
                        <FiRefreshCw />
                        刷新
                    </button>
                </header>
                <div className="lead-hub-figures">
                    <div><span>今日新增</span><strong>{overview.today_new_count || 0}</strong></div>
                    <div><span>待处理</span><strong>{overview.todo_count || 0}</strong></div>
                    {/* 与「高意向」队列同口径（只算还要跟进的），否则简报和 Tab 角标对不上 */}
                    <div><span>待跟进高意向</span><strong>{overview.high_todo_count ?? '待确认'}</strong></div>
                    <div><span>超时未跟进</span><strong className={overview.overdue_count ? 'is-alert' : ''}>{overview.overdue_count || 0}</strong></div>
                </div>
            </section>

            <details
                className="lead-hub-card lead-hub-stage-wrap"
                open={stageExpanded}
                onToggle={(event) => setStageOpen((event.target as HTMLDetailsElement).open)}
            >
                <summary className="lead-hub-stage-summary">
                    <span>线索阶段 · 共 {total} 条</span>
                    <FiChevronDown className="lead-hub-stage-caret" aria-hidden />
                </summary>
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
                        {updatedAt && <small>{pad(updatedAt.getHours())}:{pad(updatedAt.getMinutes())} 更新</small>}
                    </div>
                    <ul className="lead-hub-legend">
                        {stages.map((stage) => (
                            <li key={stage.key}>
                                <i style={{ background: stage.color }} />
                                <span>{stage.label}</span>
                                <b>{stage.count}({stage.percent}%)</b>
                            </li>
                        ))}
                    </ul>
                </div>
            </details>
            <ServiceFunnel refreshKey={updatedAt ? updatedAt.getTime() : 0} />
        </div>
    );
};

export default CustomerLeadHub;
