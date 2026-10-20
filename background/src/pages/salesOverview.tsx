import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { FiChevronRight, FiAlertCircle } from 'react-icons/fi';
import { useManagementSession } from '../component/ManagementSession';
import SalesFunnel from '../component/SalesFunnel';
import SalesDonut, { type DonutSlice } from '../component/SalesDonut';
import type { FunnelStep } from '../component/salesFunnelMath';
import { getCustomerLeadFunnel, getCustomerLeadList, getCustomerLeadOverview, type CustomerLeadOverview } from '../api/customerLeads';
import { crm, stages, type CustomerPage, type Dashboard, type Stage, type SyncState } from '../api/wecomCustomers';
import { timeAgo } from './salesFormat';
import './salesModule.scss';
import './salesOverview.scss';

type Mode = 'team' | 'personal';

const STAGE_COLORS: Record<Stage, string> = { new: '#d9a441', following: '#8b5e3c', won: '#3f8f6b', paused: '#b8a99a', invalid: '#c9cdd3' };
const PERIODS = [{ days: 1, label: '今天' }, { days: 7, label: '近 7 天' }, { days: 30, label: '近 30 天' }];

interface Snapshot {
    brief: { newLeads: number | null; newCustomers: number; todo: number };
    todos: { key: string; label: string; count: number; to: string }[];
    stages: DonutSlice[];
    problems: string[];
}

/** 读不到的部分返回 undefined 并记入 problems，页面其余部分照常显示。 */
async function loadSnapshot(mode: Mode, withLeads: boolean, account: { employeeId: string | null; displayName: string } | undefined): Promise<Snapshot> {
    const problems: string[] = [];
    const settle = async <T,>(name: string, job: Promise<T>): Promise<T | undefined> => {
        try { return await job; } catch { problems.push(name); return undefined; }
    };
    const personal = mode === 'personal';
    const owner = personal && account?.employeeId ? { ownerId: account.employeeId } : {};
    const customerCount = (query: Record<string, string>) => crm<CustomerPage>('/customers', { query: { page_size: 1, ...owner, ...query } }).then((page) => page.total);
    const leadCount = (queue: string) => getCustomerLeadList({ assigned_to: account?.displayName, queue, page: 1, page_size: 1 }).then((page) => page.total);
    const crmReady = !!account;
    const noOwner = personal && crmReady && !account?.employeeId; // 渠道账号没有绑定员工，个人口径没有客户可统计

    const [leads, dash, today, added, overdue, ...stageTotals] = await Promise.all([
        // 线索接口是平台接口，crm_only 账号不请求
        withLeads ? settle<CustomerLeadOverview | number[]>('线索', personal
            ? Promise.all([leadCount('today'), leadCount('overdue')])
            : getCustomerLeadOverview()) : Promise.resolve(undefined),
        crmReady ? settle<Dashboard>('客户', crm<Dashboard>('/dashboard')) : Promise.resolve(undefined),
        crmReady && !noOwner ? settle<number>('客户', customerCount({ queue: 'today' })) : Promise.resolve(undefined),
        crmReady && !noOwner ? settle<number>('客户', customerCount({ queue: 'added' })) : Promise.resolve(undefined),
        crmReady && !noOwner ? settle<number>('客户', customerCount({ queue: 'overdue' })) : Promise.resolve(undefined),
        ...(Object.keys(stages) as Stage[]).map((stage) => crmReady && !noOwner ? settle<number>('客户', customerCount({ queue: 'all', stage })) : Promise.resolve(undefined)),
    ]);

    const leadToday = Array.isArray(leads) ? leads[0] : leads?.today_new_count ?? 0;
    const leadOverdue = Array.isArray(leads) ? leads[1] : leads?.overdue_count ?? 0;
    const customerToday = personal ? today ?? 0 : dash?.overview.today ?? 0;
    const customerOverdue = personal ? overdue ?? 0 : dash?.overview.overdue ?? 0;
    return {
        brief: { newLeads: withLeads ? leadToday : null, newCustomers: personal ? added ?? 0 : dash?.todayAdded ?? 0, todo: customerToday },
        todos: [
            ...(withLeads ? [{ key: 'lead-overdue', label: '线索超时未跟进', count: leadOverdue, to: '/customer-leads?queue=overdue' }] : []),
            ...(crmReady ? [
                { key: 'customer-overdue', label: '客户超时未跟进', count: customerOverdue, to: '/customer-management?tab=tasks&queue=overdue' },
                { key: 'customer-today', label: '今日待跟进客户', count: customerToday, to: '/customer-management?tab=tasks&queue=today' },
                { key: 'pool', label: '公海待认领', count: dash?.overview.pool ?? 0, to: '/customer-management?view=pool' },
            ] : []),
        ],
        stages: (Object.keys(stages) as Stage[]).map((stage, index) => ({ key: stage, label: stages[stage], count: stageTotals[index] ?? 0, color: STAGE_COLORS[stage] })),
        problems: Array.from(new Set(problems)),
    };
}

const SalesOverview: React.FC = () => {
    const session = useManagementSession();
    const account = session.crmContext?.account;
    const withLeads = session.user?.scope === 'platform_admin';
    const supervisor = account?.role === 'admin';
    // 主管可看团队也可看个人；顾问只有个人。没有客户账号的平台管理员只有全局线索数据（团队口径）。
    const [mode, setMode] = useState<Mode>(supervisor || !account ? 'team' : 'personal');
    const effectiveMode: Mode = supervisor || !account ? mode : 'personal';
    const [days, setDays] = useState(30);
    const [version, setVersion] = useState(0);
    const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
    const [funnel, setFunnel] = useState<{ steps: FunnelStep[]; error: boolean } | null>(null);
    const [sync, setSync] = useState<SyncState | null>(null);

    useEffect(() => {
        let active = true;
        setSnapshot(null);
        loadSnapshot(effectiveMode, withLeads, account ? { employeeId: account.employeeId, displayName: account.displayName } : undefined)
            .then((value) => { if (active) setSnapshot(value); });
        return () => { active = false; };
    }, [effectiveMode, withLeads, account?.username, account?.employeeId, version]);

    useEffect(() => {
        if (!withLeads) return;
        let active = true;
        getCustomerLeadFunnel(days)
            .then((metrics) => { if (active) setFunnel({ steps: metrics.map((m) => ({ key: m.key, label: m.label, count: m.count })), error: false }); })
            .catch(() => { if (active) setFunnel({ steps: [], error: true }); });
        return () => { active = false; };
    }, [days, version, withLeads]);

    // 主管需要的接入状态：只读一行，读不到就不显示
    useEffect(() => {
        if (!supervisor) { setSync(null); return; }
        const controller = new AbortController();
        crm<SyncState>('/sync', { signal: controller.signal }).then(setSync).catch(() => setSync(null));
        return () => controller.abort();
    }, [supervisor, version]);

    const retry = useCallback(() => setVersion((v) => v + 1), []);
    const lastSync = sync?.runs.find((run) => run.finishedAt)?.finishedAt;
    const syncLine = !sync ? '' : !sync.configured ? '企微未接入' : sync.failed > 0 ? `企微同步有 ${sync.failed} 条失败事件` : `企微接入正常${lastSync ? ` · 最近同步 ${timeAgo(lastSync)}` : ''}`;

    return (
        <div className="sx-overview">
            <header className="sx-page-head">
                <h1>销售总览</h1>
                {supervisor && (
                    <div className="sx-segment" role="group" aria-label="简报范围">
                        <button type="button" className={mode === 'team' ? 'active' : ''} aria-pressed={mode === 'team'} onClick={() => setMode('team')}>团队简报</button>
                        <button type="button" className={mode === 'personal' ? 'active' : ''} aria-pressed={mode === 'personal'} onClick={() => setMode('personal')}>个人简报</button>
                    </div>
                )}
            </header>
            {syncLine && <p className={`sx-sync-line${sync && (!sync.configured || sync.failed > 0) ? ' is-warn' : ''}`}>{syncLine}</p>}
            {snapshot?.problems.length ? (
                <div className="sx-feedback" role="alert"><FiAlertCircle /><span>{snapshot.problems.join('、')}数据未能读取</span><button type="button" onClick={retry}>重试</button></div>
            ) : null}

            <section className={`sx-brief${withLeads ? '' : ' is-two'}`} aria-label={effectiveMode === 'team' ? '团队简报' : '个人简报'}>
                {withLeads && <div><strong>{snapshot ? snapshot.brief.newLeads : '—'}</strong><span>新增线索</span></div>}
                <div><strong>{snapshot ? snapshot.brief.newCustomers : '—'}</strong><span>新增客户</span></div>
                <div><strong>{snapshot ? snapshot.brief.todo : '—'}</strong><span>待跟进</span></div>
            </section>

            <div className={`sx-grid${withLeads ? '' : ' is-customers-only'}`}>
                {withLeads && <section className="sx-card sx-card-funnel">
                    <header className="sx-card-head">
                        <h2>线索漏斗</h2>
                        <select aria-label="漏斗周期" value={days} onChange={(event) => setDays(Number(event.target.value))}>
                            {PERIODS.map((p) => <option key={p.days} value={p.days}>{p.label}</option>)}
                        </select>
                    </header>
                    {!funnel ? <p className="sx-muted">正在读取…</p> : funnel.error ? <p className="sx-muted">漏斗暂不可用</p> : <SalesFunnel steps={funnel.steps} label="线索漏斗" />}
                </section>}

                <section className="sx-card sx-card-todo">
                    <header className="sx-card-head"><h2>今日待办</h2></header>
                    <ul className="sx-todo">
                        {(snapshot?.todos ?? []).map((item) => (
                            <li key={item.key}>
                                <Link to={item.to} className={item.count > 0 && item.key.endsWith('overdue') ? 'is-alert' : ''}>
                                    <span>{item.label}</span>
                                    <b>{item.count}</b>
                                    <FiChevronRight aria-hidden />
                                </Link>
                            </li>
                        ))}
                        {!snapshot && <li className="sx-muted">正在读取…</li>}
                    </ul>
                </section>

                <section className="sx-card sx-card-stage">
                    <header className="sx-card-head"><h2>客户阶段</h2></header>
                    {account ? <SalesDonut slices={snapshot?.stages ?? []} centerLabel="客户" /> : <p className="sx-muted">未接入客户账号</p>}
                </section>
            </div>
        </div>
    );
};

export default SalesOverview;
