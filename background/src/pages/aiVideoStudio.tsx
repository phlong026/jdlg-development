import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    getAIVideoStudioConfig,
    getAIVideoStudioLedger,
    getAIVideoStudioTasks,
    getAIVideoStudioTask,
    reconcileAIVideoStudioResult,
    saveAIVideoStudioConfig,
    type AIVideoStudioConfig,
    type AIVideoStudioLedgerItem,
    type AIVideoStudioMode,
    type AIVideoStudioTask,
    type AIVideoStudioBatchDetail,
    type StudioReconciliationAction,
} from '../api/aiVideoStudio';
import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import { buildStudioReconciliation, validateAIVideoStudioConfig } from './aiVideoStudioModel';
import './aiVideoStudio.scss';

type Tab = 'config' | 'tasks' | 'ledger';
type LoadState = 'loading' | 'ready' | 'error';

const modeLabels: Record<AIVideoStudioMode, string> = {
    text: '文生视频',
    first_frame: '首帧图生视频',
    first_last_frame: '首尾帧生成',
    reference: '混合参考生成',
};

const billingLabels: Record<string, string> = {
    per_request: '按次',
    per_video: '按条',
    per_second: '按秒',
};

const statusLabels: Record<string, string> = {
    queued: '排队中', submitting: '提交中', submitted: '已受理', running: '生成中', processing: '生成中', delivering: '交付准备中', partial_success: '部分完成',
    completed: '已完成', failed: '生成失败', cancelled: '已取消', uncertain: '待确认',
    reserved: '已预留', settled: '已结算', released: '已释放', refunded: '已退款',
    reconciliation_required: '费用待核对', retain_failed: '素材留存失败',
    reserve: '预留', settle: '结算', refund: '退回',
};

const label = (value: string) => statusLabels[value] || value || '-';
const dateTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '-';

const AIVideoStudio: React.FC = () => {
    const [tab, setTab] = useState<Tab>('config');
    const [config, setConfig] = useState<AIVideoStudioConfig | null>(null);
    const [savedConfig, setSavedConfig] = useState<AIVideoStudioConfig | null>(null);
    const [tasks, setTasks] = useState<AIVideoStudioTask[]>([]);
    const [ledger, setLedger] = useState<AIVideoStudioLedgerItem[]>([]);
    const [state, setState] = useState<Record<Tab, LoadState>>({ config: 'loading', tasks: 'loading', ledger: 'loading' });
    const [saving, setSaving] = useState(false);
    const [confirming, setConfirming] = useState(false);
    const [status, setStatus] = useState('');
    const [keyword, setKeyword] = useState('');
    const [taskPage, setTaskPage] = useState(1);
    const [taskTotal, setTaskTotal] = useState(0);
    const [ledgerPage, setLedgerPage] = useState(1);
    const [ledgerTotal, setLedgerTotal] = useState(0);
    const configRequest = useRef(0);
    const taskRequest = useRef(0);
    const ledgerRequest = useRef(0);
    const saveInFlight = useRef(false);
    const mounted = useRef(true);
    const detailRequest = useRef(0);
    const [detail, setDetail] = useState<AIVideoStudioBatchDetail | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState('');
    const [targetID, setTargetID] = useState('');
    const [reconcileAction, setReconcileAction] = useState<StudioReconciliationAction>('query');
    const [externalID, setExternalID] = useState('');
    const [evidence, setEvidence] = useState('');
    const [reconciling, setReconciling] = useState(false);
    const reconciliationInFlight = useRef(false);

    const loadConfig = useCallback(async () => {
        const request = ++configRequest.current;
        setState((current) => ({ ...current, config: 'loading' }));
        try {
            const result = await getAIVideoStudioConfig();
            if (request !== configRequest.current) return;
            setConfig(result); setSavedConfig(result);
            setState((current) => ({ ...current, config: 'ready' }));
            reportPageLoadRecovered('AI 视频工作区配置加载失败', window.location.pathname);
        } catch (error) {
            if (request !== configRequest.current) return;
            setState((current) => ({ ...current, config: 'error' }));
            reportPageLoadError(error, 'AI 视频工作区配置加载失败');
        }
    }, []);

    const loadTasks = useCallback(async () => {
        const request = ++taskRequest.current;
        setState((current) => ({ ...current, tasks: 'loading' }));
        try {
            const result = await getAIVideoStudioTasks({ status: status || undefined, keyword: keyword.trim() || undefined, page: taskPage, page_size: 20 });
            if (request !== taskRequest.current) return;
            setTasks(result.items || []); setTaskTotal(result.total || 0); setState((current) => ({ ...current, tasks: 'ready' }));
        } catch (error) {
            if (request !== taskRequest.current) return;
            setState((current) => ({ ...current, tasks: 'error' }));
            reportPageLoadError(error, 'AI 视频任务加载失败');
        }
    }, [keyword, status, taskPage]);

    const loadLedger = useCallback(async () => {
        const request = ++ledgerRequest.current;
        setState((current) => ({ ...current, ledger: 'loading' }));
        try {
            const result = await getAIVideoStudioLedger({ keyword: keyword.trim() || undefined, page: ledgerPage, page_size: 20 });
            if (request !== ledgerRequest.current) return;
            setLedger(result.items || []); setLedgerTotal(result.total || 0); setState((current) => ({ ...current, ledger: 'ready' }));
        } catch (error) {
            if (request !== ledgerRequest.current) return;
            setState((current) => ({ ...current, ledger: 'error' }));
            reportPageLoadError(error, 'AI 视频费用流水加载失败');
        }
    }, [keyword, ledgerPage]);

    useEffect(() => { void loadConfig(); }, [loadConfig]);
    useEffect(() => { if (tab === 'tasks') void loadTasks(); }, [loadTasks, tab]);
    useEffect(() => { if (tab === 'ledger') void loadLedger(); }, [loadLedger, tab]);
    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            configRequest.current += 1;
            taskRequest.current += 1;
            ledgerRequest.current += 1;
            detailRequest.current += 1;
        };
    }, []);

    const errors = useMemo(() => config ? validateAIVideoStudioConfig(config) : [], [config]);
    const dirty = !!config && !!savedConfig && JSON.stringify(config) !== JSON.stringify(savedConfig);
    const target = detail?.items.find((item) => item.result_id === targetID);

    const loadTaskDetail = async (id: string) => {
        const request = ++detailRequest.current;
        setDetailLoading(true); setDetailError(''); setDetail(null);
        try {
            const value = await getAIVideoStudioTask(id);
            if (!mounted.current || request !== detailRequest.current) return;
            const first = value.items.find((item) => item.status === 'uncertain') || value.items[0];
            setDetail(value); setTargetID(first?.result_id || '');
            setReconcileAction(first?.external_id ? 'query' : 'attach_external_id');
            setExternalID(''); setEvidence('');
        } catch (error: any) {
            if (mounted.current && request === detailRequest.current) setDetailError(error?.message || '任务详情读取失败');
        } finally {
            if (mounted.current && request === detailRequest.current) setDetailLoading(false);
        }
    };

    const reconcile = async () => {
        if (!target || !detail || reconciliationInFlight.current) return;
        try {
            const input = buildStudioReconciliation(reconcileAction, externalID, evidence);
            if (reconcileAction === 'refund_confirmed_rejected' && !window.confirm('确认供应商未受理该任务？此操作将按原报价退回预留灵石并记录操作依据。')) return;
            reconciliationInFlight.current = true; setReconciling(true); setDetailError('');
            await reconcileAIVideoStudioResult(target.result_id, input);
            if (!mounted.current) return;
            await Promise.all([loadTaskDetail(detail.batch_id), loadTasks(), loadLedger()]);
        } catch (error: any) {
            if (mounted.current) setDetailError(error?.message || '核对失败，请刷新后重试');
        } finally {
            reconciliationInFlight.current = false;
            if (mounted.current) setReconciling(false);
        }
    };

    const changeLimit = (key: keyof AIVideoStudioConfig['limits'], value: string) => {
        const number = Math.max(0, Math.trunc(Number(value) || 0));
        setConfig((current) => current ? { ...current, limits: { ...current.limits, [key]: number } } : current);
        setConfirming(false);
    };

    const save = async () => {
        if (!config || !dirty || errors.length || !confirming || saving || saveInFlight.current) return;
        saveInFlight.current = true;
        setSaving(true);
        try {
            const result = await saveAIVideoStudioConfig(config);
            if (!mounted.current) return;
            setConfig(result); setSavedConfig(result); setConfirming(false);
            alert('AI 视频工作区配置已保存，新提交将按新版本执行；历史任务保持原快照。');
        } catch (error: any) {
            if (!mounted.current) return;
            alert(`保存失败：${error?.message || '请刷新后重试'}`);
        } finally {
            saveInFlight.current = false;
            if (mounted.current) setSaving(false);
        }
    };

    return <div className="aivs-page">
        <p className="aivs-intro">独立能力、价格、任务和费用核对；不会改动原 AI 视频功能。</p>
        <div className="aivs-tabs" role="tablist" aria-label="AI 视频工作区管理">
            {([['config', '能力与价格'], ['tasks', '任务与异常'], ['ledger', '费用流水']] as const).map(([value, text]) =>
                <button key={value} type="button" className={tab === value ? 'on' : ''} onClick={() => { setTab(value); setKeyword(''); setTaskPage(1); setLedgerPage(1); }} role="tab" aria-selected={tab === value}>{text}</button>)}
        </div>

        {tab === 'config' ? <section className="aivs-panel">
            {state.config === 'loading' ? <div className="aivs-state" role="status">正在加载真实配置…</div> : null}
            {state.config === 'error' ? <div className="aivs-state error" role="alert">配置加载失败，当前状态未知，不能执行修改。<button type="button" onClick={() => void loadConfig()}>重新加载</button></div> : null}
            {state.config === 'ready' && config ? <>
                <div className="aivs-summary">
                    <div><span>模块状态</span><b className={config.enabled ? 'ok' : ''}>{config.enabled ? (config.execution_ready && config.accounting_ready && config.provider_configured ? '允许新任务' : '等待服务就绪') : '已停用'}</b></div>
                    <div><span>服务适配</span><b>{config.provider_display_name || config.provider_code || '未选择'}</b><small>{config.provider_configured ? '服务端已配置' : '未配置，不能开放'}</small></div>
                    <div><span>配置版本</span><b>v{config.version}</b><small>{dateTime(config.updated_at)}</small></div>
                </div>
                <div className="aivs-card">
                    <h3>运行开关与模式</h3>
                    <label className="aivs-switch"><input type="checkbox" checked={config.enabled} onChange={(event) => { setConfig({ ...config, enabled: event.target.checked }); setConfirming(false); }} />允许新任务提交</label>
                    <p>关闭后仍可查询历史任务和费用；供应商未配置或价格未配置时，服务端继续拒绝新提交。</p>
                    <div className="aivs-mode-grid">{(Object.keys(modeLabels) as AIVideoStudioMode[]).map((mode) =>
                        <label key={mode}><input type="checkbox" checked={!!config.modes[mode]} onChange={(event) => { setConfig({ ...config, modes: { ...config.modes, [mode]: event.target.checked } }); setConfirming(false); }} />{modeLabels[mode]}</label>)}</div>
                </div>
                <div className="aivs-card">
                    <h3>运行限制</h3>
                    <div className="aivs-form-grid">
                        {([
                            ['max_concurrency', '并发任务上限'], ['timeout_seconds', '单任务超时（秒）'], ['max_quantity', '单批数量上限'],
                            ['max_duration_768p', '768P 时长上限（秒）'], ['max_duration_2k', '2K 时长上限（秒）'],
                            ['max_reference_images', '参考图片上限'], ['max_reference_videos', '参考视频上限'],
                            ['max_reference_audios', '参考音频上限'], ['max_reference_total', '参考素材总上限'],
                        ] as const).map(([key, text]) => <label key={key}>{text}<input type="number" min={0} value={config.limits[key]} onChange={(event) => changeLimit(key, event.target.value)} /></label>)}
                    </div>
                </div>
                <div className="aivs-card">
                    <h3>独立价格目录</h3>
                    <p>已启用价格项必须有正整数价格。改价只影响新报价，已创建批次按费用快照结算。</p>
                    <div className="admin-table-region"><table><thead><tr><th>收费项</th><th>计费单位</th><th>灵石单价</th><th>状态</th><th>版本</th></tr></thead><tbody>
                        {config.prices.map((price, index) => <tr key={price.code}><td><b>{price.label}</b><small>{price.code}</small></td><td>{billingLabels[price.billing_unit] || price.billing_unit}</td><td><input aria-label={`${price.label}灵石单价`} type="number" min={1} step={1} value={price.unit_stones && price.unit_stones > 0 ? price.unit_stones : ''} placeholder="未配置" onChange={(event) => { const prices = [...config.prices]; prices[index] = { ...price, unit_stones: event.target.value === '' ? null : Number(event.target.value) }; setConfig({ ...config, prices }); setConfirming(false); }} /></td><td><label className="aivs-switch compact"><input type="checkbox" checked={price.enabled} onChange={(event) => { const prices = [...config.prices]; prices[index] = { ...price, enabled: event.target.checked }; setConfig({ ...config, prices }); setConfirming(false); }} />{price.enabled ? '启用' : '停用'}</label></td><td>v{price.version || 1}</td></tr>)}
                    </tbody></table></div>
                </div>
                {errors.length ? <div className="aivs-errors" role="alert">{errors.map((error) => <div key={error}>{error}</div>)}</div> : null}
                <div className="aivs-actions">
                    <button type="button" onClick={() => { setConfig(savedConfig); setConfirming(false); }} disabled={!dirty || saving}>撤销未保存修改</button>
                    {confirming ? <button className="primary danger" type="button" onClick={() => void save()} disabled={!!errors.length || saving}>{saving ? '保存中…' : `二次确认保存 v${config.version + 1}`}</button> : <button className="primary" type="button" onClick={() => setConfirming(true)} disabled={!dirty || !!errors.length || saving}>核对并准备保存</button>}
                </div>
            </> : null}
        </section> : null}

        {tab === 'tasks' ? <section className="aivs-panel">
            <div className="aivs-filters"><input value={keyword} onChange={(event) => { setKeyword(event.target.value); setTaskPage(1); }} placeholder="批次、报价或用户 ID" /><select value={status} onChange={(event) => { setStatus(event.target.value); setTaskPage(1); }}><option value="">全部状态</option><option value="queued">排队中</option><option value="running">生成中</option><option value="uncertain">待确认</option><option value="completed">已完成</option><option value="partial_success">部分完成</option><option value="failed">生成失败</option><option value="cancelled">已取消</option></select><button type="button" onClick={() => void loadTasks()}>查询</button></div>
            {state.tasks === 'loading' ? <div className="aivs-state">任务加载中…</div> : state.tasks === 'error' ? <div className="aivs-state error">任务加载失败。<button onClick={() => void loadTasks()}>重试</button></div> : tasks.length === 0 ? <div className="aivs-state">暂无符合条件的新模块任务。</div> : <div className="admin-table-region"><table><thead><tr><th>批次</th><th>用户 / 草稿</th><th>模式</th><th>任务状态</th><th>费用状态</th><th>费用快照</th><th>错误摘要</th><th>更新时间</th><th>核对</th></tr></thead><tbody>{tasks.map((task) => <tr key={task.id}><td><b>{task.id}</b><small>报价 {task.quote_id || '-'}</small></td><td>{task.user_id}<small>草稿 {task.draft_id}</small></td><td>{modeLabels[task.mode] || task.mode}</td><td><b>{label(task.status)}</b></td><td><b>{label(task.fee_status)}</b></td><td>{task.total_stones} 灵石</td><td>{task.error_message || task.error_code || '-'}</td><td>{dateTime(task.updated_at)}</td><td><button type="button" onClick={() => void loadTaskDetail(task.id)} disabled={reconciling}>查看与核对</button></td></tr>)}</tbody></table></div>}
            <p className="aivs-note">待确认任务保留原预留费用；请先核对供应商任务标识。有标识时查询实际结果；确认未受理才能退回费用。</p>
            {detailLoading ? <div className="aivs-state">任务详情加载中…</div> : null}
            {detailError ? <div className="aivs-errors" role="alert">{detailError}</div> : null}
            {detail ? <div className="aivs-card">
                <h3>批次核对 · {detail.batch_id}</h3>
                <p>{label(detail.status)} · 预计 {detail.charge?.quoted_stones ?? detail.charge?.total_stones ?? 0} 灵石 · 已结算 {detail.charge?.settled_stones || 0} · 已退回 {detail.charge?.released_stones || 0}</p>
                <div className="admin-table-region"><table><thead><tr><th>子任务</th><th>状态</th><th>供应商标识</th><th>说明</th></tr></thead><tbody>{detail.items.map((item) => <tr key={item.result_id}><td>{item.result_id}</td><td>{label(item.status)}</td><td>{item.external_id || '尚未取得'}</td><td>{item.error_message || '-'}</td></tr>)}</tbody></table></div>
                <div className="aivs-form-grid">
                    <label>子任务<select value={targetID} disabled={reconciling} onChange={(event) => { setTargetID(event.target.value); setReconcileAction(detail.items.find((item) => item.result_id === event.target.value)?.external_id ? 'query' : 'attach_external_id'); setExternalID(''); setEvidence(''); }}>{detail.items.map((item) => <option key={item.result_id} value={item.result_id}>{item.result_id} · {label(item.status)}</option>)}</select></label>
                    <label>核对动作<select value={reconcileAction} disabled={reconciling} onChange={(event) => setReconcileAction(event.target.value as StudioReconciliationAction)}><option value="query" disabled={!target?.external_id}>查询供应商实际结果</option><option value="attach_external_id" disabled={target?.status !== 'uncertain' || !!target?.external_id}>补回供应商任务标识</option><option value="refund_confirmed_rejected" disabled={target?.status !== 'uncertain' || !!target?.external_id}>确认未受理并退款</option></select></label>
                    {reconcileAction === 'attach_external_id' ? <label>供应商任务标识<input value={externalID} maxLength={191} disabled={reconciling} onChange={(event) => setExternalID(event.target.value)} /></label> : null}
                </div>
                {reconcileAction !== 'query' ? <label className="aivs-evidence">核对依据<textarea value={evidence} minLength={10} maxLength={500} disabled={reconciling} placeholder="填写供应商核对记录与依据，10至500字" onChange={(event) => setEvidence(event.target.value)} /></label> : null}
                <div className="aivs-actions"><button type="button" disabled={reconciling || !target || (reconcileAction === 'query' ? !target.external_id : target.status !== 'uncertain' || !!target.external_id)} className={reconcileAction === 'refund_confirmed_rejected' ? 'primary danger' : 'primary'} onClick={() => void reconcile()}>{reconciling ? '正在核对…' : reconcileAction === 'query' ? '查询任务' : reconcileAction === 'attach_external_id' ? '核验并补回标识' : '确认未受理并退回灵石'}</button></div>
            </div> : null}
            {taskTotal > 20 ? <div className="aivs-pagination"><button type="button" onClick={() => setTaskPage((page) => Math.max(1, page - 1))} disabled={taskPage <= 1}>上一页</button><span>第 {taskPage} 页 · 共 {taskTotal} 条</span><button type="button" onClick={() => setTaskPage((page) => page + 1)} disabled={taskPage * 20 >= taskTotal}>下一页</button></div> : null}
        </section> : null}

        {tab === 'ledger' ? <section className="aivs-panel">
            <div className="aivs-filters"><input value={keyword} onChange={(event) => { setKeyword(event.target.value); setLedgerPage(1); }} placeholder="批次、任务或用户 ID" /><button type="button" onClick={() => void loadLedger()}>查询</button></div>
            {state.ledger === 'loading' ? <div className="aivs-state">费用流水加载中…</div> : state.ledger === 'error' ? <div className="aivs-state error">费用流水加载失败。<button onClick={() => void loadLedger()}>重试</button></div> : ledger.length === 0 ? <div className="aivs-state">暂无符合条件的费用流水。</div> : <div className="admin-table-region"><table><thead><tr><th>流水</th><th>批次 / 任务</th><th>用户</th><th>动作</th><th>灵石</th><th>状态</th><th>价格快照</th><th>时间</th></tr></thead><tbody>{ledger.map((item) => <tr key={item.id}><td>{item.id}</td><td><b>{item.batch_id}</b><small>{item.task_id || '-'}</small></td><td>{item.user_id}</td><td>{label(item.action)}</td><td>{item.stones}</td><td>{label(item.status)}</td><td>{item.price_code} · v{item.price_version}</td><td>{dateTime(item.created_at)}</td></tr>)}</tbody></table></div>}
            {ledgerTotal > 20 ? <div className="aivs-pagination"><button type="button" onClick={() => setLedgerPage((page) => Math.max(1, page - 1))} disabled={ledgerPage <= 1}>上一页</button><span>第 {ledgerPage} 页 · 共 {ledgerTotal} 条</span><button type="button" onClick={() => setLedgerPage((page) => page + 1)} disabled={ledgerPage * 20 >= ledgerTotal}>下一页</button></div> : null}
        </section> : null}
    </div>;
};

export default AIVideoStudio;
