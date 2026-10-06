import React from 'react';
import { Link } from 'react-router-dom';
import { FiChevronRight, FiRefreshCw, FiTrendingUp, FiUserCheck, FiUsers } from 'react-icons/fi';
import type { CustomerLeadOverview } from '../api/customerLeads';
import { useManagementSession } from '../component/ManagementSession';
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

interface HubProps {
    overview: CustomerLeadOverview;
    loading: boolean;
    updatedAt: Date | null;
    onRefresh: () => void;
}

/**
 * 销售线索的概览区：入口卡片 → 线索简报 → 线索阶段（环形图）。
 * 版式参考企业微信「销售」页：先给全局数字，再进入下面的队列与列表。
 */
const CustomerLeadHub: React.FC<HubProps> = ({ overview, loading, updatedAt, onRefresh }) => {
    const session = useManagementSession();
    const crmEnabled = !!session.user?.crmEnabled && session.user?.scope === 'platform_admin';
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
            <div className="lead-hub-tiles">
                {crmEnabled && (
                    <Link className="lead-hub-tile lead-hub-tile-customer" to="/customer-management">
                        <span>客户</span>
                        <FiChevronRight className="lead-hub-tile-arrow" />
                        <FiUsers className="lead-hub-tile-icon" aria-hidden />
                    </Link>
                )}
                <Link className="lead-hub-tile lead-hub-tile-lead is-current" to="/customer-leads?queue=todo" aria-current="page">
                    <span>线索</span>
                    <FiChevronRight className="lead-hub-tile-arrow" />
                    <FiUserCheck className="lead-hub-tile-icon" aria-hidden />
                </Link>
                <Link className="lead-hub-tile lead-hub-tile-opportunity" to="/opportunities">
                    <span>商机</span>
                    <small>未启用</small>
                    <FiTrendingUp className="lead-hub-tile-icon" aria-hidden />
                </Link>
            </div>

            <section className="lead-hub-card">
                <header className="lead-hub-card-head">
                    <h2>线索简报</h2>
                    <span className="lead-hub-period">今天</span>
                    <button type="button" className="lead-hub-refresh" onClick={onRefresh} disabled={loading}>
                        <FiRefreshCw />
                        刷新
                    </button>
                </header>
                <div className="lead-hub-figures">
                    <div><span>新增线索</span><strong>{overview.today_new_count || 0}</strong></div>
                    <div><span>待处理</span><strong>{overview.todo_count || 0}</strong></div>
                    {/* 与「高意向」队列同口径（只算还要跟进的），否则简报和 Tab 角标对不上 */}
                    <div><span>高意向</span><strong>{overview.high_todo_count || 0}</strong></div>
                    <div><span>超时未跟进</span><strong className={overview.overdue_count ? 'is-alert' : ''}>{overview.overdue_count || 0}</strong></div>
                </div>
            </section>

            <section className="lead-hub-card">
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
            </section>
        </div>
    );
};

export default CustomerLeadHub;
