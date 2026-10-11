import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getAIOverview, type AIOverviewData, type AIOverviewScene, type AIOverviewSupplier } from '../api/aiOverview';
import './aiOverview.scss';

const formatPercent = (value: number, digits = 1) => `${(value * 100).toFixed(digits)}%`;

const formatYuan = (value: number) => `¥${value.toFixed(1)}`;

const formatDelta = (today: number, yesterday: number) => {
    if (yesterday <= 0) return null;
    const delta = ((today - yesterday) / yesterday) * 100;
    if (Math.abs(delta) < 0.05) return null;
    return { text: `${delta > 0 ? '+' : ''}${delta.toFixed(1)}%`, up: delta > 0 };
};

const supplierRoleLabel: Record<string, string> = {
    primary: '主力',
    fallback_1: '备用 1',
    fallback_2: '备用 2',
    other: '历史/其他',
};

const failedRateTone = (rate: number) => {
    if (rate >= 0.1) return 'bad';
    if (rate >= 0.05) return 'mid';
    return 'good';
};

/** 近 14 日调用量（柱）+ 成功率（线）双轴图：纯 SVG，无图表依赖 */
const TrendChart: React.FC<{ points: { date: string; total: number; failed: number }[] }> = ({ points }) => {
    const width = 640;
    const height = 180;
    const padding = { left: 34, right: 8, top: 12, bottom: 18 };
    const innerW = width - padding.left - padding.right;
    const innerH = height - padding.top - padding.bottom;
    const maxTotal = Math.max(10, ...points.map((p) => p.total));
    const barSlot = innerW / Math.max(points.length, 1);
    const barWidth = Math.max(6, barSlot * 0.55);
    // 成功率轴：85%~100% 线性映射到图形高度（低于 85% 视为贴近底部）
    const rateY = (rate: number) => {
        const clamped = Math.min(1, Math.max(0.85, rate));
        return padding.top + innerH - ((clamped - 0.85) / 0.15) * innerH;
    };
    const linePoints = points
        .map((p, i) => {
            const rate = p.total > 0 ? (p.total - p.failed) / p.total : 1;
            return `${padding.left + barSlot * (i + 0.5)},${rateY(rate)}`;
        })
        .join(' ');
    return (
        <svg className="aio-trend-svg" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="近14日调用量与成功率">
            {[0, 0.25, 0.5, 0.75, 1].map((t) => {
                const y = padding.top + innerH - t * innerH;
                return <line key={t} className="aio-grid" x1={padding.left} y1={y} x2={width - padding.right} y2={y} />;
            })}
            <text className="aio-axis" x={padding.left - 6} y={padding.top + innerH} textAnchor="end">0</text>
            <text className="aio-axis" x={padding.left - 6} y={padding.top + 4} textAnchor="end">{maxTotal}</text>
            {points.map((p, i) => {
                const barH = (p.total / maxTotal) * innerH;
                return (
                    <rect
                        key={p.date}
                        className={i === points.length - 1 ? 'aio-bar today' : 'aio-bar'}
                        x={padding.left + barSlot * i + (barSlot - barWidth) / 2}
                        y={padding.top + innerH - barH}
                        width={barWidth}
                        height={Math.max(1, barH)}
                        rx={2}
                    >
                        <title>{`${p.date}：${p.total} 次，失败 ${p.failed}`}</title>
                    </rect>
                );
            })}
            <polyline className="aio-rate-line" points={linePoints} />
            {points.map((p, i) => {
                const rate = p.total > 0 ? (p.total - p.failed) / p.total : 1;
                return <circle key={p.date} className="aio-rate-dot" cx={padding.left + barSlot * (i + 0.5)} cy={rateY(rate)} r={2.4} />;
            })}
        </svg>
    );
};

const SceneRankRow: React.FC<{ scene: AIOverviewScene; maxTotal: number }> = ({ scene, maxTotal }) => (
    <div className="aio-rank-row">
        <div className="aio-rank-name">
            <span>{scene.label}</span>
        </div>
        <div className="aio-rank-bar-wrap" aria-hidden="true"><div className="aio-rank-bar" style={{ width: `${Math.max(2, (scene.total / maxTotal) * 100)}%` }} /></div>
        <span className="aio-rank-num"><span className="aio-mobile-label">调用量</span>{scene.total.toLocaleString()}</span>
        <span className={`aio-rank-rate ${failedRateTone(scene.failed_rate)}`}><span className="aio-mobile-label">成功率</span>{formatPercent(1 - scene.failed_rate)}</span>
        <span className="aio-rank-stones"><span className="aio-mobile-label">灵石收入</span>{scene.stones.toLocaleString()}</span>
    </div>
);

const SupplierCard: React.FC<{ supplier: AIOverviewSupplier; onManage: () => void }> = ({ supplier, onManage }) => (
    <div className={`aio-prov-card ${supplier.role === 'primary' ? 'primary' : ''} ${supplier.alerting ? 'alerting' : ''}`}>
        <div className="aio-prov-tags">
            <span className={`aio-prov-tag ${supplier.role === 'primary' ? 'main' : ''}`}>{supplierRoleLabel[supplier.role] || supplier.role}</span>
            {supplier.alerting ? <span className="aio-prov-tag alert">近1小时失败率超标</span> : null}
        </div>
        <h4>
            {supplier.provider_name || supplier.model}
            <small>{supplier.protocol_label}</small>
        </h4>
        <div className="aio-prov-stats">
            <div><span>7日调用</span><b>{supplier.total_7d.toLocaleString()}</b></div>
            <div><span>失败率</span><b className={failedRateTone(supplier.failed_rate_7d)}>{formatPercent(supplier.failed_rate_7d)}</b></div>
            <div><span>平均耗时</span><b>{supplier.avg_seconds_7d > 0 ? `${supplier.avg_seconds_7d.toFixed(1)}s` : '—'}</b></div>
            <div><span>估算成本</span><b>{formatYuan(supplier.cost_yuan_per_unit)}/张</b></div>
        </div>
        <div className="aio-prov-foot">
            {supplier.note ? <span className="aio-prov-note">{supplier.note}</span> : <span />}
            <button type="button" className="aio-link-btn" onClick={onManage}>管理</button>
        </div>
    </div>
);

const AIOverview: React.FC = () => {
    const navigate = useNavigate();
    const [data, setData] = useState<AIOverviewData | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState(false);
    const [refreshVersion, setRefreshVersion] = useState(0);

    useEffect(() => {
        let cancelled = false;
        setLoading(true);
        setLoadError(false);
        setData(null);
        getAIOverview()
            .then((res) => { if (!cancelled) { setData(res); reportPageLoadRecovered('加载AI运营总览失败'); } })
            .catch((error) => {
                console.error('加载AI运营总览失败:', error);
                if (!cancelled) { setLoadError(true); reportPageLoadError(error, '加载AI运营总览失败'); }
            })
            .finally(() => { if (!cancelled) setLoading(false); });
        return () => { cancelled = true; };
    }, [refreshVersion]);

    const imageQueues = data?.queues?.image;
    const pendingAnomalies = imageQueues ? imageQueues.failed + imageQueues.timeout : 0;
    const maxSceneTotal = useMemo(() => Math.max(1, ...(data?.scenes ?? []).map((s) => s.total)), [data]);
    const failReasonTotal = useMemo(() => Math.max(1, ...(data?.fail_reasons ?? []).map((r) => r.count)), [data]);

    const todayKPI = data?.kpi.today;
    const delta = todayKPI && data ? formatDelta(todayKPI.total, data.kpi.yesterday.total) : null;

    /** 导出周报 CSV：KPI / 14日趋势 / 场景分布 / 供应商健康度 / 失败原因，客户端生成直接下载 */
    const handleExportCSV = () => {
        if (!data) return;
        const esc = (value: string | number) => `"${String(value).replace(/"/g, '""')}"`;
        const lines: string[] = [];
        lines.push(['AI 运营周报', new Date().toLocaleString('zh-CN')].map(esc).join(','));
        lines.push('');
        lines.push(['指标', '数值'].map(esc).join(','));
        lines.push(['今日调用', todayKPI?.total ?? ''].map(esc).join(','));
        lines.push(['今日成功率', todayKPI ? formatPercent(todayKPI.success_rate) : ''].map(esc).join(','));
        lines.push(['今日灵石收入', todayKPI?.stones_revenue ?? ''].map(esc).join(','));
        lines.push(['今日服务成本估算(元)', todayKPI?.cost_yuan?.toFixed(2) ?? ''].map(esc).join(','));
        lines.push(['今日毛利率', todayKPI?.margin_rate != null ? formatPercent(todayKPI.margin_rate) : ''].map(esc).join(','));
        lines.push(['成本口径', data.kpi.cost_note].map(esc).join(','));
        lines.push('');
        lines.push(['日期', '调用量', '失败', '视频'].map(esc).join(','));
        data.trend.forEach((point) => lines.push([point.date, point.total, point.failed, point.video_total].map(esc).join(',')));
        lines.push('');
        lines.push(['场景', '调用量', '成功率', '收入灵石', '成本估算(元)', '毛利率'].map(esc).join(','));
        data.scenes.forEach((scene) => lines.push([scene.label, scene.total, formatPercent(1 - scene.failed_rate), scene.stones, scene.cost_yuan.toFixed(2), formatPercent(scene.margin_rate)].map(esc).join(',')));
        lines.push('');
        lines.push(['供应商', '角色', '7日调用', '失败率', '平均耗时(s)', '估算成本(元/张)'].map(esc).join(','));
        data.suppliers.forEach((s) => lines.push([s.provider_name || s.model, supplierRoleLabel[s.role] || s.role, s.total_7d, formatPercent(s.failed_rate_7d), s.avg_seconds_7d.toFixed(1), s.cost_yuan_per_unit.toFixed(2)].map(esc).join(',')));
        lines.push('');
        lines.push(['失败原因', '件数'].map(esc).join(','));
        data.fail_reasons.forEach((reason) => lines.push([reason.category, reason.count].map(esc).join(',')));
        const blob = new Blob(['\uFEFF' + lines.join('\n')], { type: 'text/csv;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = `AI运营周报-${new Date().toISOString().slice(0, 10)}.csv`;
        anchor.click();
        URL.revokeObjectURL(url);
    };

    return (
        <div className="ai-overview-page">
            <div className="aio-pagehead">
                <h1>AI 运营总览</h1>
                <span className="aio-sub">查看 AI 使用情况、收入成本与服务运行情况，及时处理异常任务。</span>
                <button type="button" className="aio-refresh" onClick={() => void handleExportCSV()} disabled={!data}>
                    导出周报 CSV
                </button>
                <button type="button" className="aio-refresh" onClick={() => setRefreshVersion((version) => version + 1)} disabled={loading}>
                    {loading ? '刷新中…' : '刷新'}
                </button>
            </div>

            {!data ? (
                <div className="section-card aio-empty" role={loadError ? 'alert' : 'status'}>
                    {loadError ? 'AI 运营数据未能加载，请重试。当前无法判断调用量、收入或异常数量。' : '正在加载 AI 运营数据…'}
                    {loadError && <button type="button" className="aio-refresh" onClick={() => setRefreshVersion((version) => version + 1)}>重新加载</button>}
                </div>
            ) : <>
            {data?.alerts?.length ? (
                <div className="aio-alert-strip">
                    <span className="aio-alert-icon">⚠</span>
                    <div>
                        {data.alerts.map((alert) => (
                            <div key={alert.id} className="aio-alert-line">
                                <b>{alert.title}</b>
                                <span className="aio-alert-time">{new Date(alert.created_at).toLocaleString('zh-CN')}</span>
                            </div>
                        ))}
                    </div>
                    <button type="button" className="aio-alert-go" onClick={() => navigate('/support-tickets')}>去工单处理</button>
                </div>
            ) : null}

            <div className="aio-kpi-band">
                <button type="button" className="aio-kpi" onClick={() => navigate('/ai-tasks')}>
                    <span>今日调用</span>
                    <b>{(todayKPI?.total ?? 0).toLocaleString()}</b>
                    <small>{delta ? `环比昨日 ${delta.text}` : '昨日无任务或持平'}</small>
                </button>
                <div className="aio-kpi">
                    <span>任务成功率</span>
                    <b>{todayKPI ? formatPercent(todayKPI.success_rate) : '—'}</b>
                    <small>昨日 {data ? formatPercent(data.kpi.yesterday.success_rate) : '—'}</small>
                </div>
                <div className="aio-kpi">
                    <span>今日灵石收入</span>
                    <b>{(todayKPI?.stones_revenue ?? 0).toLocaleString()}</b>
                    <small>≈ {formatYuan((todayKPI?.stones_revenue ?? 0) / 10)}（10 灵石 = ¥1）</small>
                </div>
                <div className="aio-kpi warn">
                    <span>今日服务成本</span>
                    <b>{formatYuan(todayKPI?.cost_yuan ?? 0)}</b>
                    <small>
                        {data?.kpi.cost_landed_ratio != null && data.kpi.cost_landed_ratio > 0
                            ? `${Math.round(data.kpi.cost_landed_ratio * 100)}% 使用任务记录成本，其余按模型单价估算`
                            : '按模型单价估算'}
                    </small>
                </div>
                <div className="aio-kpi">
                    <span>毛利率</span>
                    <b>{todayKPI?.margin_rate != null ? formatPercent(todayKPI.margin_rate) : '—'}</b>
                    <small>（收入 − 成本）÷ 收入</small>
                </div>
                <button type="button" className={`aio-kpi ${pendingAnomalies > 0 ? 'bad' : ''}`} onClick={() => navigate('/ai-tasks?queue=failed')}>
                    <span>待处理图片异常</span>
                    <b>{pendingAnomalies}</b>
                    <small>失败 {imageQueues?.failed ?? 0} · 超时 {imageQueues?.timeout ?? 0} · 查看异常任务</small>
                </button>
            </div>

            <div className="aio-grid-main">
                <section className="section-card aio-card">
                    <div className="aio-card-head">
                        <h3>近 14 日调用量与成功率</h3>
                        <div className="aio-legend">
                            <span><i className="bar" />调用量（图+视频）</span>
                            <span><i className="line" />成功率</span>
                        </div>
                    </div>
                    {data ? <TrendChart points={data.trend} /> : <div className="aio-empty">暂无数据</div>}
                </section>

                <section className="section-card aio-card">
                    <div className="aio-card-head">
                        <h3>场景分布（近 7 日）</h3>
                        <button type="button" className="aio-link-btn" onClick={() => navigate('/ai-pricing')}>定价视角</button>
                    </div>
                    <div className="aio-rank-head">
                        <span>场景</span><span>调用量 / 成功率 / 收入灵石</span>
                    </div>
                    {data?.scenes?.length ? (
                        data.scenes.map((scene) => <SceneRankRow key={scene.scene} scene={scene} maxTotal={maxSceneTotal} />)
                    ) : (
                        <div className="aio-empty">近 7 日暂无任务</div>
                    )}
                    {data?.video_7d && data.video_7d.total > 0 ? (
                        <div className="aio-video-note">
                            视频业务线（近7日）：{data.video_7d.total} 条 · {data.video_7d.segments} 段 · 收入 {data.video_7d.stones.toLocaleString()} 灵石 · 成本约 {formatYuan(data.video_7d.cost_yuan)}
                        </div>
                    ) : null}
                </section>
            </div>

            <section className="section-card aio-card">
                <div className="aio-card-head">
                    <h3>供应商健康度（近 7 日）</h3>
                    <button type="button" className="aio-link-btn" onClick={() => navigate('/ai-pricing?tab=provider')}>管理供应商与备用服务</button>
                </div>
                <div className="aio-prov-grid">
                    {data?.suppliers?.length ? (
                        data.suppliers.map((supplier) => (
                            <SupplierCard key={`${supplier.role}-${supplier.model}`} supplier={supplier} onManage={() => navigate('/ai-pricing?tab=provider')} />
                        ))
                    ) : (
                        <div className="aio-empty">近 7 日暂无执行记录</div>
                    )}
                </div>
                {data?.chain?.length ? (
                    <div className="aio-chain">
                        <b>图片生成服务顺序</b>
                        {data.chain.map((node, index) => (
                            <React.Fragment key={node.model}>
                                {index > 0 ? <span className="aio-chain-arrow">→</span> : null}
                                <span className="aio-chain-node">
                                    {node.provider_name}
                                    {node.role !== 'primary' && data.fallback_out_7d > 0 ? (
                                        <em>（近7日备用服务使用情况见上方）</em>
                                    ) : null}
                                </span>
                            </React.Fragment>
                        ))}
                        <span className="aio-chain-note">主力服务失败时，系统按此顺序尝试备用服务。</span>
                    </div>
                ) : null}
            </section>

            <div className="aio-grid-eq">
                <section className="section-card aio-card">
                    <div className="aio-card-head">
                        <h3>主要失败原因（近 7 日）</h3>
                        <button type="button" className="aio-link-btn" onClick={() => navigate('/ai-tasks?queue=failed')}>查看失败任务</button>
                    </div>
                    {data?.fail_reasons?.length ? (
                        data.fail_reasons.map((reason) => (
                            <div key={reason.category} className="aio-fail-row">
                                <span className="aio-fail-name">{reason.category}</span>
                                <div className="aio-fail-bar-wrap"><div className="aio-fail-bar" style={{ width: `${Math.max(3, (reason.count / failReasonTotal) * 100)}%` }} /></div>
                                <span className="aio-fail-count">{reason.count} 件</span>
                            </div>
                        ))
                    ) : (
                        <div className="aio-empty">近 7 日无失败任务 🎉</div>
                    )}
                    <div className="aio-foot-note">失败任务的退款情况与已退灵石金额，请在任务列表中核对。</div>
                </section>

                <section className="section-card aio-card">
                    <div className="aio-card-head">
                        <h3>热门工具前 5 名（近 7 日）</h3>
                        <button type="button" className="aio-link-btn" onClick={() => navigate('/ai-tools')}>全部工具</button>
                    </div>
                    {data?.tools_top?.length ? (
                        data.tools_top.map((tool) => (
                            <div key={tool.tool_id} className="aio-tool-row">
                                <span className="aio-tool-name">{tool.name}</span>
                                <div className="aio-rank-bar-wrap" aria-hidden="true"><div className="aio-rank-bar alt" style={{ width: `${Math.max(3, (tool.total_7d / (data.tools_top[0]?.total_7d || 1)) * 100)}%` }} /></div>
                                <span className="aio-rank-num">{tool.total_7d.toLocaleString()}</span>
                            </div>
                        ))
                    ) : (
                        <div className="aio-empty">近 7 日暂无工具调用</div>
                    )}
                </section>
            </div>

            <div className="aio-foot-note">{data?.kpi.cost_note}。调价入口见「定价与供应商」，失败任务处理见「AI 任务中心」队列。</div>
            </>}
        </div>
    );
};

export default AIOverview;
