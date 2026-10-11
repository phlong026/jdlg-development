import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { getAIOverview, getAIOpsSettings, getAIPricingOpsList, getAIPricingVersions, saveAIOpsSettings, type AIOpsSettings, type AIPricingOpsItem, type AIPricingVersion, type AIOverviewData } from '../api/aiOverview';
import { getAIAPIConfigList, saveAIAPIConfig, saveAIPricing, testAIAPIConfig, type AIAPIConfig, type AIAPIConfigTestResult } from '../api/ai';
import './aiPricing.scss';

const formatPercent = (value: number, digits = 1) => `${(value * 100).toFixed(digits)}%`;
const formatYuan = (value: number) => `¥${value.toFixed(2)}`;

const protocolLabels: Record<string, string> = {
    openai_image_sync: '同步返回（OpenAI Image）',
    gemini_sync: '同步返回（Gemini）',
    toapis_async: '异步任务返回（ToAPIs）',
    chat_sync: '聊天同步返回',
    sync: '通用同步返回',
};

const taskTypeLabels: Record<string, string> = {
    ai_draw: 'AI 绘画',
    ai_chat: 'AI 对话',
};

const marginTone = (rate: number) => {
    if (rate >= 0.6) return 'good';
    if (rate >= 0.3) return 'mid';
    return 'bad';
};

/** 调价弹窗：新定价 + 备注，提交前展示影响摘要并需二次确认（后端自动留痕） */
const AdjustPricingModal: React.FC<{
    item: AIPricingOpsItem;
    initialStones?: number;
    onClose: () => void;
    onSaved: (message: string) => void;
}> = ({ item, initialStones, onClose, onSaved }) => {
    const [stonesInput, setStonesInput] = useState(String(initialStones ?? item.stones));
    const [note, setNote] = useState('');
    const [confirmation, setConfirmation] = useState<{ scene: string; oldStones: number; stones: number; note: string } | null>(null);
    const saveInFlight = useRef(false);
    const mounted = useRef(true);
    useEffect(() => {
        mounted.current = true;
        return () => { mounted.current = false; };
    }, []);
    const currentTarget = useRef({ scene: item.scene, stones: item.stones });
    currentTarget.current = { scene: item.scene, stones: item.stones };
    const [saving, setSaving] = useState(false);
    const stones = Number(stonesInput);
    const validStones = stonesInput.trim() !== '' && Number.isSafeInteger(stones) && stones >= 0;
    const changed = stones !== item.stones;
    const confirming = confirmation?.scene === item.scene && confirmation.oldStones === item.stones && confirmation.stones === stones && confirmation.note === note;
    useEffect(() => {
        setStonesInput(String(initialStones ?? item.stones));
        setNote('');
        setConfirmation(null);
    }, [item.scene, item.stones, initialStones]);
    const close = () => { if (!saving && !saveInFlight.current) onClose(); };

    const handleSave = async () => {
        if (saveInFlight.current || saving || !confirming || !validStones || !changed || currentTarget.current.scene !== item.scene || currentTarget.current.stones !== item.stones) return;
        saveInFlight.current = true;
        setSaving(true);
        try {
            await saveAIPricing({ scene: item.scene, stones, note: note.trim() || undefined });
            if (!mounted.current || currentTarget.current.scene !== item.scene || currentTarget.current.stones !== item.stones) return;
            onSaved(`已调整 ${item.label} 定价：${item.stones} → ${stones} 灵石，并写入版本记录`);
        } catch (error: any) {
            if (!mounted.current || currentTarget.current.scene !== item.scene || currentTarget.current.stones !== item.stones) return;
            alert(`保存失败: ${error?.message || '未知错误'}`);
        } finally {
            saveInFlight.current = false;
            if (mounted.current) setSaving(false);
        }
    };

    return (
        <div className="aip-modal-overlay" onClick={close}>
            <div className="aip-modal" onClick={(event) => event.stopPropagation()}>
                <div className="aip-modal-head">
                    <h3>调整定价 · {item.label}</h3>
                    <button type="button" className="aip-modal-close" onClick={close} disabled={saving} aria-label="关闭调价窗口">✕</button>
                </div>
                <div className="aip-modal-body">
                    <div className="aip-form-row">
                        <label>当前定价</label>
                        <b>{item.stones} 灵石</b>
                    </div>
                    <div className="aip-form-row">
                        <label htmlFor="aip-new-stones">新定价（灵石）*</label>
                        <input
                            id="aip-new-stones"
                            type="number"
                            min={0}
                            step={1}
                            value={stonesInput}
                            aria-invalid={!validStones}
                            aria-describedby={!validStones ? "aip-price-error" : undefined}
                            onChange={(event) => { setStonesInput(event.target.value); setConfirmation(null); }}
                            disabled={saving}
                        />
                    </div>
                    {!validStones ? <div id="aip-price-error" className="aip-context-warn" role="alert">请输入大于或等于 0 的有效整数。</div> : null}
                    <div className="aip-form-row">
                        <label htmlFor="aip-price-note">调价备注</label>
                        <input
                            id="aip-price-note"
                            value={note}
                            onChange={(event) => { setNote(event.target.value); setConfirmation(null); }}
                            placeholder="如：成本上涨 / 促销促活（会写入版本记录）"
                            disabled={saving}
                        />
                    </div>
                    <div className="aip-context">
                        <div>近 7 日调用 <b>{item.usage_7d.toLocaleString()}</b> 次 · 收入 <b>{item.stones_revenue_7d.toLocaleString()}</b> 灵石</div>
                        <div>当前估算毛利率 <b>{formatPercent(item.margin_rate)}</b>（成本口径：{formatYuan(item.cost_yuan_est)} / 7 日估算）</div>
                        <div className="aip-context-warn">
                            {!validStones
                                ? '请先填写有效定价，再确认调价。'
                                : changed
                                ? `确认后立即生效并留痕（操作人与新旧值），回滚 = 用旧值 ${item.stones} 再调一次。`
                                : '定价未变化，无需保存；备注仅随调价写入新的版本记录。'}
                        </div>
                    </div>
                </div>
                <div className="aip-modal-foot">
                    <button type="button" className="aip-btn" onClick={close} disabled={saving}>{validStones && !changed ? '关闭' : '取消'}</button>
                    {(!validStones || changed) && (confirming ? (
                        <button type="button" className="aip-btn primary danger" onClick={() => void handleSave()} disabled={saving || !validStones || !changed}>
                            {saving ? '保存中…' : `二次确认：改为 ${stones} 灵石`}
                        </button>
                    ) : (
                        <button type="button" className="aip-btn primary" onClick={() => setConfirmation({ scene: item.scene, oldStones: item.stones, stones, note })} disabled={saving || !validStones || !changed}>
                            下一步（确认调价）
                        </button>
                    ))}
                </div>
            </div>
        </div>
    );
};

/** 调价历史弹窗：版本链 + 确认回滚（回滚本身也是一次调价，同样留痕） */
const PricingHistoryModal: React.FC<{
    scene: string;
    label: string;
    onClose: () => void;
    onRollback: (stones: number) => void;
}> = ({ scene, label, onClose, onRollback }) => {
    const [versions, setVersions] = useState<AIPricingVersion[] | null>(null);
    const [loadFailed, setLoadFailed] = useState(false);
    const [loadAttempt, setLoadAttempt] = useState(0);

    useEffect(() => {
        let cancelled = false;
        setVersions(null);
        setLoadFailed(false);
        getAIPricingVersions(scene)
            .then((list) => { if (!cancelled) setVersions(list); })
            .catch((error) => {
                console.error('加载调价历史失败:', error);
                if (!cancelled) setLoadFailed(true);
            });
        return () => { cancelled = true; };
    }, [scene, loadAttempt]);

    return (
        <div className="aip-modal-overlay" onClick={onClose}>
            <div className="aip-modal" onClick={(event) => event.stopPropagation()}>
                <div className="aip-modal-head">
                    <h3>调价历史 · {label}</h3>
                    <button type="button" className="aip-modal-close" onClick={onClose}>✕</button>
                </div>
                <div className="aip-modal-body">
                    {loadFailed ? (
                        <div className="aip-empty" role="alert">
                            <p>调价历史加载失败，暂时无法确认历史记录。请重试。</p>
                            <button type="button" className="aip-btn" onClick={() => setLoadAttempt((attempt) => attempt + 1)}>重新加载</button>
                        </div>
                    ) : versions === null ? (
                        <div className="aip-empty" role="status">加载中…</div>
                    ) : versions.length === 0 ? (
                        <div className="aip-empty">该场景还没有调价记录（初始定价不产生版本）</div>
                    ) : (
                        <div className="admin-table-region"><table className="aip-table slim">
                            <thead>
                                <tr><th>时间</th><th>调整</th><th>操作人</th><th>备注</th><th>操作</th></tr>
                            </thead>
                            <tbody>
                                {versions.map((version) => (
                                    <tr key={version.id}>
                                        <td>{new Date(version.created_at).toLocaleString('zh-CN')}</td>
                                        <td><b>{version.old_stones}</b> → <b>{version.new_stones}</b> 灵石</td>
                                        <td>{version.operator || '-'}</td>
                                        <td className="aip-dim">{version.note || '-'}</td>
                                        <td>
                                            <button type="button" className="aip-link" onClick={() => onRollback(version.old_stones)}>
                                                回滚到 {version.old_stones}
                                            </button>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table></div>
                    )}
                </div>
                <div className="aip-modal-foot">
                    <span className="aip-foot-hint">回滚 = 用旧值再调一次价，同样会写入新的版本记录</span>
                    <button type="button" className="aip-btn primary" onClick={onClose}>关闭</button>
                </div>
            </div>
        </div>
    );
};

/** 供应商卡片里的轻量连通性测试（复用旧 AI 配置页的测试接口，不产生生成费用） */
const ProviderTestButton: React.FC<{ configId: number }> = ({ configId }) => {
    const [testing, setTesting] = useState(false);
    const [result, setResult] = useState<AIAPIConfigTestResult | null>(null);
    return (
        <span className="aip-test-wrap">
            <button
                type="button"
                className="aip-btn sm"
                disabled={testing}
                onClick={() => {
                    setTesting(true);
                    setResult(null);
                    testAIAPIConfig({ id: configId })
                        .then(setResult)
                        .catch((error: any) => setResult({ ok: false, category: 'network_error', message: `测试请求失败: ${error?.message || error}` }))
                        .finally(() => setTesting(false));
                }}
            >
                {testing ? '测试中…' : '测试'}
            </button>
            {result ? <span className={`aip-test-result ${result.ok ? 'ok' : 'fail'}`}>{result.ok ? '✓ ' : '✗ '}{result.message}</span> : null}
        </span>
    );
};

const AIPricing: React.FC = () => {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const activeTab = searchParams.get('tab') === 'provider' ? 'provider' : 'pricing';

    const [pricingList, setPricingList] = useState<AIPricingOpsItem[]>([]);
    const [overview, setOverview] = useState<AIOverviewData | null>(null);
    const [apiConfigs, setApiConfigs] = useState<AIAPIConfig[]>([]);
    const [loading, setLoading] = useState(false);
    const [adjusting, setAdjusting] = useState<{ item: AIPricingOpsItem; initialStones?: number } | null>(null);
    const [historyScene, setHistoryScene] = useState<{ scene: string; label: string } | null>(null);
    const [feedback, setFeedback] = useState('');
    /** 告警设置（供应商管理页）：processor 每分钟读取，保存后下个周期生效 */
    const [alertSettings, setAlertSettings] = useState<AIOpsSettings | null>(null);
    const [savingSettings, setSavingSettings] = useState(false);

    const loadData = async () => {
        setLoading(true);
        try {
            const [pricing, ov] = await Promise.all([getAIPricingOpsList(), getAIOverview().catch(() => null)]);
            setPricingList(pricing);
            setOverview(ov);
            if (activeTab === 'provider') {
                const [configs, settings] = await Promise.all([
                    getAIAPIConfigList(),
                    getAIOpsSettings().catch(() => null),
                ]);
                setApiConfigs(configs);
                if (settings) setAlertSettings(settings);
            }
        } catch (error) {
            console.error('加载定价与供应商数据失败:', error);
            reportPageLoadError(error, '加载定价与供应商数据失败');
        } finally {
            setLoading(false);
        }
    };

    const handleSaveAlertSettings = async () => {
        if (!alertSettings) return;
        if (!window.confirm(`保存设置？\n\n告警：失败率阈值 ${(alertSettings.alert_failed_rate * 100).toFixed(0)}%、最小样本 ${alertSettings.alert_min_window_calls} 次、检查间隔 ${alertSettings.alert_interval_minutes} 分钟。\n抽检：${alertSettings.audit_enabled ? `开启，比例 ${(alertSettings.audit_sample_rate * 100).toFixed(1)}%` : '关闭'}。\n保存后下个检查周期生效。`)) {
            return;
        }
        setSavingSettings(true);
        try {
            const saved = await saveAIOpsSettings(alertSettings);
            setAlertSettings(saved);
            setFeedback('告警设置已保存，下个检查周期生效。');
        } catch (error: any) {
            alert(`保存失败: ${error?.message || '未知错误'}`);
        } finally {
            setSavingSettings(false);
        }
    };

    useEffect(() => {
        void loadData();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeTab]);

    const switchTab = (tab: 'pricing' | 'provider') => {
        const next = new URLSearchParams(searchParams);
        if (tab === 'provider') next.set('tab', 'provider');
        else next.delete('tab');
        setSearchParams(next, { replace: true });
    };

    const suppliers = overview?.suppliers ?? [];
    const chain = overview?.chain ?? [];
    const lowMarginRows = useMemo(() => pricingList.filter((item) => item.usage_7d > 0 && item.margin_rate < 0.3), [pricingList]);

    /** 启用/停用切换：复用保存接口，密钥留空由后端回填原值；切换只影响新任务，停用后执行链路回落到内置配置 */
    const [togglingConfigId, setTogglingConfigId] = useState<number | null>(null);
    const handleToggleConfigActive = async (config: AIAPIConfig) => {
        const action = config.is_active ? '停用' : '启用';
        if (!window.confirm(`确认${action}「${config.provider_name || config.provider_code}」配置？\n\n切换只影响新任务，在途任务不受影响；停用后该场景将按内置默认配置执行。`)) {
            return;
        }
        setTogglingConfigId(config.id ?? null);
        try {
            await saveAIAPIConfig({ ...config, is_active: !config.is_active, api_key: '' });
            setFeedback(`已${action}「${config.provider_name || config.provider_code}」，只影响新任务。`);
            await loadData();
        } catch (error: any) {
            alert(`${action}失败: ${error?.message || '未知错误'}`);
        } finally {
            setTogglingConfigId(null);
        }
    };

    return (
        <div className="ai-pricing-page">
            <div className="aip-pagehead">
                <h1>定价与供应商</h1>
                <div className="aip-subtabs">
                    <button type="button" className={activeTab === 'pricing' ? 'on' : ''} onClick={() => switchTab('pricing')}>
                        定价管理 <small>运营</small>
                    </button>
                    <button type="button" className={activeTab === 'provider' ? 'on' : ''} onClick={() => switchTab('provider')}>
                        供应商管理 <small>技术</small>
                    </button>
                </div>
                <button type="button" className="aip-refresh" onClick={() => void loadData()} disabled={loading}>
                    {loading ? '刷新中…' : '刷新'}
                </button>
            </div>

            {feedback ? <div className="aip-feedback">{feedback}</div> : null}

            {activeTab === 'pricing' ? (
                <>
                    <div className="aip-notice">
                        调价需<b>二次确认并自动留痕</b>（操作人 / 旧值 / 新值），可从历史记录发起回滚，确认后生效。
                        毛利率 =（灵石收入折元 − 服务成本估算）÷ 收入，按 10 灵石 = ¥1 折算；成本为模型单价估算口径，非财务数据。
                    </div>
                    {lowMarginRows.length > 0 ? (
                        <div className="aip-notice warn">
                            <b>毛利预警：</b>
                            {lowMarginRows.map((item) => `${item.label}（${formatPercent(item.margin_rate)}）`).join('、')}
                            低于 30%。请先核对实际服务成本、失败原因及退款记录，再决定是否调价。
                        </div>
                    ) : null}
                    <section className="section-card aip-card">
                        <div className="admin-table-region"><table className="aip-table">
                            <thead>
                                <tr>
                                    <th>场景</th>
                                    <th>定价</th>
                                    <th>服务成本<span className="th-hint">¥/次 · 估算</span></th>
                                    <th>毛利率</th>
                                    <th>近7日调用</th>
                                    <th>最近调价</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {pricingList.length === 0 ? (
                                    <tr><td colSpan={7} className="aip-empty">暂无定价配置</td></tr>
                                ) : pricingList.map((item) => (
                                    <tr key={item.scene} className={item.margin_rate < 0.3 && item.usage_7d > 0 ? 'row-warn' : ''}>
                                        <td>
                                            <div className="aip-scene-cell">
                                                <b>{item.label}</b>
                                                <details className="aip-scene-diagnostic"><summary>场景标识</summary><code>{item.scene}</code></details>
                                            </div>
                                        </td>
                                        <td><b>{item.stones}</b> 灵石</td>
                                        <td>{formatYuan(item.cost_yuan_est)}</td>
                                        <td><span className={`aip-margin ${marginTone(item.margin_rate)}`}>{item.usage_7d > 0 ? formatPercent(item.margin_rate) : '—'}</span></td>
                                        <td>
                                            {item.usage_7d.toLocaleString()}
                                            {item.usage_7d > 0 ? <span className="aip-dim"> · 成功率 {formatPercent(1 - item.failed_rate_7d)}</span> : <span className="aip-dim">（近7日无调用）</span>}
                                        </td>
                                        <td>
                                            {item.latest_version ? (
                                                <span className="aip-version-cell">
                                                    {item.latest_version.old_stones} → {item.latest_version.new_stones}（{new Date(item.latest_version.created_at).toLocaleDateString('zh-CN')} · {item.latest_version.operator}）
                                                </span>
                                            ) : <span className="aip-dim">初始定价</span>}
                                        </td>
                                        <td>
                                            <div className="aip-op-links">
                                                <button type="button" className="aip-link" onClick={() => { setFeedback(''); setAdjusting({ item }); }}>调价</button>
                                                <button type="button" className="aip-link" onClick={() => setHistoryScene({ scene: item.scene, label: item.label })}>历史</button>
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table></div>
                        <div className="aip-foot-note">
                            高级设置保留服务协议、请求参数等技术配置，调整前请核对对新任务的影响。
                            <button type="button" className="aip-link" onClick={() => navigate('/ai-config')}>打开高级设置</button>
                        </div>
                    </section>
                </>
            ) : (
                <>
                    <div className="aip-notice">
                        本页面向<b>技术人员</b>（协议、请求模板、密钥）。降级顺序由后端执行链路决定，本页只读展示健康度与承接量；
                        调整降级顺序需修改服务端配置并发版。协议与模板的完整表单在「AI 配置（旧版）」（⌘K 可达）。
                    </div>

                    <section className="section-card aip-card">
                        <div className="aip-card-head"><h3>告警与抽检设置</h3><span className="aip-card-sub">失败率告警自动创建工单；成功结果按比例抽入「内容抽检」队列。改动保存后下个检查周期生效</span></div>
                        {alertSettings ? (
                            <div className="aip-settings-row">
                                <label>
                                    失败率阈值（%）
                                    <input
                                        type="number" min={1} max={100} step={1}
                                        value={Math.round(alertSettings.alert_failed_rate * 100)}
                                        onChange={(event) => setAlertSettings({ ...alertSettings, alert_failed_rate: (Number(event.target.value) || 1) / 100 })}
                                        disabled={savingSettings}
                                    />
                                </label>
                                <label>
                                    最小样本（次/小时）
                                    <input
                                        type="number" min={1} max={1000}
                                        value={alertSettings.alert_min_window_calls}
                                        onChange={(event) => setAlertSettings({ ...alertSettings, alert_min_window_calls: Number(event.target.value) || 1 })}
                                        disabled={savingSettings}
                                    />
                                </label>
                                <label>
                                    检查间隔（分钟）
                                    <input
                                        type="number" min={1} max={120}
                                        value={alertSettings.alert_interval_minutes}
                                        onChange={(event) => setAlertSettings({ ...alertSettings, alert_interval_minutes: Number(event.target.value) || 10 })}
                                        disabled={savingSettings}
                                    />
                                </label>
                                <label>
                                    内容抽检
                                    <span className="aip-inline-check">
                                        <input
                                            type="checkbox"
                                            checked={alertSettings.audit_enabled}
                                            onChange={(event) => setAlertSettings({ ...alertSettings, audit_enabled: event.target.checked })}
                                            disabled={savingSettings}
                                        />
                                        开启
                                    </span>
                                </label>
                                <label>
                                    抽检比例（%）
                                    <input
                                        type="number" min={0} max={50} step={0.5}
                                        value={Math.round(alertSettings.audit_sample_rate * 1000) / 10}
                                        onChange={(event) => setAlertSettings({ ...alertSettings, audit_sample_rate: (Number(event.target.value) || 0) / 100 })}
                                        disabled={savingSettings}
                                    />
                                </label>
                                <button type="button" className="aip-btn primary" onClick={() => void handleSaveAlertSettings()} disabled={savingSettings}>
                                    {savingSettings ? '保存中…' : '保存设置'}
                                </button>
                            </div>
                        ) : (
                            <div className="aip-empty">加载中…</div>
                        )}
                    </section>

                    <section className="section-card aip-card">
                        <div className="aip-card-head"><h3>生图降级链（只读展示）</h3></div>
                        <div className="aip-chain">
                            {chain.map((node, index) => (
                                <React.Fragment key={node.model}>
                                    {index > 0 ? <span className="aip-chain-arrow">→</span> : null}
                                    <span className="aip-chain-node">
                                        <b>{node.provider_name}</b>
                                        <small>{node.protocol_label}</small>
                                    </span>
                                </React.Fragment>
                            ))}
                            <span className="aip-chain-note">主力失败自动切换备用；近 7 日各节点承接量见下方卡片</span>
                        </div>
                        {overview?.chain_suggestion ? (
                            <div className={`aip-suggestion ${overview.chain_suggestion.suggested ? 'on' : ''}`}>
                                <b>{overview.chain_suggestion.suggested ? '⚙ 调序建议' : '调序检查'}</b>
                                <span>{overview.chain_suggestion.reason}</span>
                            </div>
                        ) : null}
                    </section>

                    <div className="aip-prov-grid">
                        {suppliers.length ? suppliers.map((supplier) => (
                            <div key={`${supplier.role}-${supplier.model}`} className={`aip-prov-card ${supplier.role === 'primary' ? 'primary' : ''} ${supplier.alerting ? 'alerting' : ''}`}>
                                <div className="aip-prov-tags">
                                    <span className={`aip-prov-tag ${supplier.role === 'primary' ? 'main' : ''}`}>
                                        {supplier.role === 'primary' ? '主力' : supplier.role === 'fallback_1' ? '备用 1' : supplier.role === 'fallback_2' ? '备用 2' : '历史/其他'}
                                    </span>
                                    {supplier.alerting ? <span className="aip-prov-tag alert">近1小时失败率超标</span> : null}
                                </div>
                                <h4>{supplier.provider_name || supplier.model}<small>{supplier.protocol_label}</small></h4>
                                <div className="aip-prov-stats">
                                    <div><span>7日调用</span><b>{supplier.total_7d.toLocaleString()}</b></div>
                                    <div><span>失败率</span><b className={supplier.failed_rate_7d >= 0.1 ? 'bad' : supplier.failed_rate_7d >= 0.05 ? 'mid' : 'good'}>{formatPercent(supplier.failed_rate_7d)}</b></div>
                                    <div><span>平均耗时</span><b>{supplier.avg_seconds_7d > 0 ? `${supplier.avg_seconds_7d.toFixed(1)}s` : '—'}</b></div>
                                    <div><span>估算成本</span><b>{formatYuan(supplier.cost_yuan_per_unit)}/张</b></div>
                                </div>
                                {supplier.note ? <div className="aip-prov-note">{supplier.note}</div> : null}
                            </div>
                        )) : <div className="aip-empty">近 7 日暂无执行记录</div>}
                    </div>

                    <section className="section-card aip-card">
                        <div className="aip-card-head"><h3>接口配置（中文视图）</h3></div>
                        <div className="admin-table-region"><table className="aip-table">
                            <thead>
                                <tr>
                                    <th>配置</th><th>任务类型</th><th>协议</th><th>状态</th><th>接口地址</th><th>更新时间</th><th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {apiConfigs.length === 0 ? (
                                    <tr><td colSpan={7} className="aip-empty">暂无接口配置</td></tr>
                                ) : apiConfigs.map((config) => (
                                    <tr key={config.id}>
                                        <td><b>{config.provider_name || config.provider_code}</b></td>
                                        <td>{taskTypeLabels[config.task_type] || config.task_type}</td>
                                        <td>{protocolLabels[config.protocol_type || ''] || config.protocol_type}</td>
                                        <td>
                                            <span className={`aip-margin ${config.is_active ? 'good' : 'mid'}`}>{config.is_active ? '启用' : '停用'}</span>
                                        </td>
                                        <td className="aip-endpoint" title={config.api_endpoint}>{config.api_endpoint}</td>
                                        <td>{config.updated_at ? new Date(config.updated_at).toLocaleDateString('zh-CN') : '-'}</td>
                                        <td>
                                            <div className="aip-op-links">
                                                {config.id ? <ProviderTestButton configId={config.id} /> : null}
                                                <button
                                                    type="button"
                                                    className={`aip-link ${config.is_active ? 'danger-text' : ''}`}
                                                    disabled={togglingConfigId === config.id}
                                                    onClick={() => void handleToggleConfigActive(config)}
                                                >
                                                    {togglingConfigId === config.id ? '切换中…' : config.is_active ? '停用' : '启用'}
                                                </button>
                                                <button type="button" className="aip-link" onClick={() => navigate('/ai-config')}>高级配置</button>
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table></div>
                        <div className="aip-foot-note">同一场景仅一条启用配置，切换只影响新任务；密钥已配置的显示掩码，明文不下发。</div>
                    </section>
                </>
            )}

            {adjusting ? (
                <AdjustPricingModal
                    key={`${adjusting.item.scene}:${adjusting.item.stones}:${adjusting.initialStones ?? adjusting.item.stones}`}
                    item={adjusting.item}
                    initialStones={adjusting.initialStones}
                    onClose={() => setAdjusting(null)}
                    onSaved={(message) => {
                        setAdjusting(null);
                        setFeedback(message);
                        void loadData();
                    }}
                />
            ) : null}

            {historyScene ? (
                <PricingHistoryModal
                    scene={historyScene.scene}
                    label={historyScene.label}
                    onClose={() => setHistoryScene(null)}
                    onRollback={(stones) => {
                        setHistoryScene(null);
                        setFeedback(`回滚已发起：请在新弹窗中确认改为 ${stones} 灵石（回滚同样留痕）`);
                        const target = pricingList.find((item) => item.scene === historyScene.scene);
                        if (target) {
                            setAdjusting({ item: target, initialStones: stones });
                        }
                    }}
                />
            ) : null}
        </div>
    );
};

export default AIPricing;
