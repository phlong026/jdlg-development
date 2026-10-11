import React, { useEffect, useRef, useState } from 'react';
import { Context, IntentRecomputeResult, IntentRuleConfiguration, IntentRuleParameters, IntentRulesResult, tracking } from '../../api/shareTracking';
import { intentExclusionText, intentMetricText, time } from './model';

const fields: { key: keyof IntentRuleParameters; label: string; min: number; max: number }[] = [
    { key: 'window_days', label: '评估窗口（天）', min: 1, max: 90 },
    { key: 'medium_sessions', label: '中意向：至少会话数', min: 2, max: 100 },
    { key: 'medium_favorites', label: '中意向：至少已确认收藏数', min: 1, max: 30 },
    { key: 'high_business_results', label: '高意向：至少已确认业务结果数', min: 1, max: 30 },
    { key: 'daily_session_cap', label: '每个账号每日会话贡献上限', min: 1, max: 10 },
    { key: 'daily_action_cap', label: '每个账号每类行为每日贡献上限', min: 1, max: 5 },
];
export type RuleDraft = Record<keyof IntentRuleParameters, string>;
export function ruleDraft(rule: IntentRuleParameters): RuleDraft {
    return Object.fromEntries(fields.map(({ key }) => [key, String(rule[key])])) as RuleDraft;
}
export function validateRuleDraft(draft: RuleDraft, reason: string): string {
    if (!reason.trim() || reason.trim().length > 500) return '请填写 1–500 字的变更原因';
    for (const field of fields) {
        const value = Number(draft[field.key]);
        if (!/^\d+$/.test(draft[field.key]) || !Number.isSafeInteger(value) || value < field.min || value > field.max) return `${field.label}须为 ${field.min}–${field.max} 的整数`;
    }
    if (Number(draft.medium_sessions) > Number(draft.window_days) * Number(draft.daily_session_cap)) return '会话阈值不能超过评估窗口内每日会话贡献上限之和';
    if (Number(draft.medium_favorites) > Number(draft.window_days) * Number(draft.daily_action_cap)) return '收藏阈值不能超过评估窗口内每日收藏贡献上限之和';
    if (Number(draft.high_business_results) > Number(draft.window_days) * Number(draft.daily_action_cap) * 3) return '业务结果阈值不能超过窗口内咨询、预约、报价三类贡献上限之和';
    return '';
}
function validRules(data: IntentRulesResult, owner: string): boolean {
    return Boolean(data?.current?.rule_version && data.current.owner_id === owner && Array.isArray(data.history)
        && data.history.every(rule => rule.owner_id === owner && rule.rule_version)
        && !validateRuleDraft(ruleDraft(data.current), '校验'));
}
export function validRecompute(result: IntentRecomputeResult | undefined, version: string): result is IntentRecomputeResult {
    return Boolean(result && result.rule_version === version && Number.isSafeInteger(result.evaluated_count) && result.evaluated_count >= 0
        && Number.isSafeInteger(result.changed_count) && result.changed_count >= 0 && result.changed_count <= result.evaluated_count
        && Number.isFinite(Date.parse(result.as_of)));
}
function RuleSummary({ rule }: { rule: IntentRuleConfiguration }) {
    return <><p>规则 {rule.rule_version} · 修订 {rule.revision} · 生效 {rule.effective_at?.startsWith('0001-') ? '系统初始规则' : time(rule.effective_at)} · {rule.timezone}</p>
        <p>近 {rule.window_days} 天；中意向须同时满足 {rule.medium_sessions} 次会话和 {rule.medium_favorites} 次已确认收藏；高意向须有 {rule.high_business_results} 次已确认业务结果。</p>
        <p>每日会话最多贡献 {rule.daily_session_cap} 次，每类业务行为最多贡献 {rule.daily_action_cap} 次。操作者：{rule.actor ?? '未提供'}；原因：{rule.reason || '默认规则'}。</p>
        <p>参与指标：{rule.participating_metrics?.map(intentMetricText).join('、') || '未提供'}</p><p>排除项：{rule.exclusions?.map(intentExclusionText).join('、') || '未提供'}</p></>;
}
type StatusProps = { changed: () => void; onBusyChange: (busy: boolean) => void; onDirtyChange: (dirty: boolean) => void };
export function IntentRulesPanel({ context, changed, onBusyChange, onDirtyChange }: StatusProps & { context: Context }) {
    const [owner, setOwner] = useState('');
    const allowed = context.can_view && context.owners.some(item => item.id === owner);
    const scopeKey = allowed ? `${owner}:${context.can_rules === true}` : '';
    const scope = useRef(scopeKey); scope.current = scopeKey;
    const state = useRef({ busy: false, dirty: false });
    const [busy, setBusy] = useState(false);
    const live = useRef(true);
    useEffect(() => { live.current = true; return () => { live.current = false; scope.current = ''; }; }, []);
    useEffect(() => { state.current = { busy: false, dirty: false }; setBusy(false); onBusyChange(false); onDirtyChange(false); }, [scopeKey]);
    return <section className="st-rules" aria-label="意向规则管理"><p>意向仅用于跟进优先级参考，不代表真实需求或成交概率。规则独立于访问列表的日期筛选，默认窗口为近 30 天。</p>
        <label>规则归属范围<select value={allowed ? owner : ''} disabled={busy} onChange={event => {
            if (!live.current || scope.current !== scopeKey || state.current.busy || !context.can_view) return;
            const next = event.target.value;
            if (next && !context.owners.some(item => item.id === next)) return;
            if (next === owner) return;
            if (state.current.dirty && !window.confirm('切换归属将放弃未保存的规则输入，确认继续？')) return;
            scope.current = ''; state.current = { busy: false, dirty: false }; onBusyChange(false); onDirtyChange(false); setOwner(next);
        }}><option value="">请选择单一授权归属</option>{context.owners.map(item => <option key={item.id} value={item.id}>{item.name || item.id}</option>)}</select></label>
        {!allowed && <p className="st-warning">请明确选择有权查看的归属，随后读取该归属的当前规则及历史版本。</p>}
        {allowed && <IntentRuleEditor key={scopeKey} owner={owner} canWrite={context.can_rules === true} isCurrent={() => live.current && scope.current === scopeKey}
            changed={() => { if (scope.current === scopeKey) changed(); }}
            onBusyChange={value => { if (scope.current !== scopeKey) return; state.current.busy = value; setBusy(value); onBusyChange(value); }}
            onDirtyChange={value => { if (scope.current !== scopeKey) return; state.current.dirty = value; onDirtyChange(value); }} />}
    </section>;
}
export function IntentRuleEditor({ owner, canWrite, isCurrent, changed, onBusyChange, onDirtyChange }: StatusProps & { owner: string; canWrite: boolean; isCurrent: () => boolean }) {
    const [data, setData] = useState<IntentRulesResult>();
    const [base, setBase] = useState<IntentRuleConfiguration>();
    const [draft, setDraft] = useState<RuleDraft>();
    const [reason, setReason] = useState('');
    const [recomputeReason, setRecomputeReason] = useState('');
    const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false);
    const [readError, setReadError] = useState(''); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
    const [verifyRequired, setVerifyRequired] = useState(false); const [lastResult, setLastResult] = useState<IntentRecomputeResult>();
    const alive = useRef(true); const pending = useRef(false); const reading = useRef(false); const readId = useRef(0); const controller = useRef<AbortController>();
    const editRevision = useRef(0); const renderRevision = editRevision.current;
    const recomputeAttempt = useRef<{ request_id: string; reason: string; rule_version: string }>();
    const current = () => alive.current && isCurrent();
    const conflict = Boolean(base && data && base.rule_version !== data.current.rule_version);
    const dirty = Boolean(reason || recomputeReason || (base && draft && JSON.stringify(draft) !== JSON.stringify(ruleDraft(base))));
    const markEdited = () => { editRevision.current += 1; onDirtyChange(true); };
    useEffect(() => { onDirtyChange(dirty); }, [dirty, onDirtyChange]);
    async function readLatest(mode: 'replace' | 'keep'): Promise<boolean> {
        if (!current()) return false;
        editRevision.current += 1;
        const id = ++readId.current; controller.current?.abort(); const abort = new AbortController(); controller.current = abort;
        reading.current = true; setLoading(true); setReadError('');
        try {
            const result = await tracking.intentRules(owner, abort.signal);
            if (!current() || id !== readId.current) return false;
            if (!validRules(result, owner)) throw new Error('返回的规则范围或内容不匹配，未采用该响应');
            setData(result); setVerifyRequired(false);
            if (mode === 'replace') { setBase(result.current); setDraft(ruleDraft(result.current)); }
            return true;
        } catch (problem) {
            if (current() && id === readId.current) setReadError((problem as Error).message || '读取规则失败');
            return false;
        } finally { if (current() && id === readId.current) { reading.current = false; setLoading(false); } }
    }
    useEffect(() => {
        alive.current = true; void readLatest('replace');
        return () => { alive.current = false; readId.current += 1; controller.current?.abort(); };
    }, []);
    useEffect(() => {
        const guard = (event: BeforeUnloadEvent) => { if (dirty || pending.current) { event.preventDefault(); event.returnValue = ''; } };
        window.addEventListener('beforeunload', guard); return () => window.removeEventListener('beforeunload', guard);
    }, [dirty]);
    function canMutate() { return current() && canWrite && !pending.current && !reading.current && !verifyRequired && !conflict && data && base && draft && renderRevision === editRevision.current; }
    async function mutate(kind: 'save' | 'recompute') {
        if (!canMutate() || !draft || !data || !base) return;
        const problem = kind === 'save' ? validateRuleDraft(draft, reason) : !recomputeReason.trim() || recomputeReason.trim().length > 500 ? '请填写 1–500 字的重算原因' : '';
        if (problem) { setError(problem); return; }
        if (kind === 'recompute' && (reason || JSON.stringify(draft) !== JSON.stringify(ruleDraft(base)))) { setError('请先保存规则修改，或恢复当前规则，再执行重算'); return; }
        pending.current = true; editRevision.current += 1; setBusy(true); onBusyChange(true); setError(''); setNotice('');
        try {
            let result: IntentRecomputeResult;
            if (kind === 'save') {
                const parameters = Object.fromEntries(fields.map(({ key }) => [key, Number(draft[key])])) as unknown as IntentRuleParameters;
                const response = await tracking.saveIntentRules({ ...parameters, expected_rule_version: base.rule_version, reason: reason.trim() }, owner);
                if (!current()) return;
                if (!validRules(response, owner) || response.current.rule_version === base.rule_version || !validRecompute(response.recomputed, response.current.rule_version)) throw new Error('服务端未确认新规则与重算结果，请先读取最新规则核对');
                result = response.recomputed;
                setData(response); setBase(response.current); setDraft(ruleDraft(response.current)); setReason(''); recomputeAttempt.current = undefined;
                setNotice(`已保存新版本 ${response.current.rule_version}，立即生效并完成重算。`);
            } else {
                const body = { reason: recomputeReason.trim(), rule_version: data.current.rule_version };
                if (!recomputeAttempt.current || recomputeAttempt.current.reason !== body.reason || recomputeAttempt.current.rule_version !== body.rule_version) recomputeAttempt.current = { ...body, request_id: crypto.randomUUID() };
                const response = await tracking.recomputeIntent(recomputeAttempt.current, owner);
                if (!current()) return;
                if (!validRecompute(response, body.rule_version)) throw new Error('服务端未确认所选版本的重算结果，请先读取最新规则核对');
                result = response; setRecomputeReason(''); recomputeAttempt.current = undefined;
                setNotice('服务端已完成所选归属的重算。');
            }
            setLastResult(result); changed();
            // Refresh from the server; a confirmed write and a failed read are reported separately.
            await readLatest('keep');
        } catch (problem) {
            if (current()) { setError(`${(problem as Error).message}。输入已保留；请先读取最新规则核对，再决定是否重试。`); setVerifyRequired(true); }
        } finally { pending.current = false; if (current()) { setBusy(false); onBusyChange(false); } }
    }
    return <div className="st-rule-editor">
        {loading && <p role="status">正在读取所选归属的规则…</p>}
        {readError && <p className="st-error" role="alert">{readError}。{notice ? '操作已确认，但最新规则读取失败。' : '未能确认最新规则。'}</p>}
        {error && <p className="st-error" role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
        {lastResult && <p role="status">规则 {lastResult.rule_version} · 已评估 {lastResult.evaluated_count} 位访客 · 结果变化 {lastResult.changed_count} 位 · 计算至 {time(lastResult.as_of)}</p>}
        <button disabled={busy || loading} onClick={() => { if (!current() || pending.current || reading.current) return; void readLatest(base ? 'keep' : 'replace'); }}>读取最新规则核对（保留输入）</button>
        {data && <section aria-label="当前生效规则"><h3>当前生效规则</h3><RuleSummary rule={data.current} /></section>}
        {!canWrite && <p className="st-warning">当前账号仅可查看规则与历史，没有规则配置或重算权限。</p>}
        {canWrite && draft && base && <fieldset disabled={busy || loading}><legend>新建规则版本</legend><p>保存会创建不可变版本并立即对所选归属生效、重算；历史版本保留。修改须记录原因；窗口还受服务端已批准的数据保留期限制。高意向业务结果汇总咨询、预约、报价三类各自封顶后的贡献。</p>
            {conflict && <div className="st-warning" role="alert"><p>规则版本已变化。输入仍基于 {base.rule_version}，当前为 {data?.current.rule_version}。请核对上方新规则。</p><button onClick={() => { if (!current() || pending.current || reading.current || !data || renderRevision !== editRevision.current) return; markEdited(); setBase(data.current); setError(''); }}>已核对，保留输入并采用最新版本基线</button></div>}
            {verifyRequired && <p className="st-warning">上次操作结果需要核对；读取成功前不能再次提交。</p>}
            <div className="st-rule-fields">{fields.map(field => <label key={field.key}>{field.label}<input type="number" step="1" min={field.min} max={field.max} value={draft[field.key]} onChange={event => { if (!current() || pending.current || reading.current || renderRevision !== editRevision.current) return; markEdited(); setDraft({ ...draft, [field.key]: event.target.value }); }} /></label>)}</div>
            <label>变更原因<textarea maxLength={500} value={reason} onChange={event => { if (!current() || pending.current || reading.current || renderRevision !== editRevision.current) return; markEdited(); setReason(event.target.value); }} /></label>
            <div className="st-rule-actions"><button className="st-primary" disabled={busy || loading || verifyRequired || conflict} onClick={() => void mutate('save')}>保存新版本并重算</button><button onClick={() => { if (!current() || pending.current || reading.current || !data) return; if (dirty && !window.confirm('确认放弃未保存的规则输入并恢复当前生效规则？')) return; editRevision.current += 1; onDirtyChange(Boolean(recomputeReason)); setBase(data.current); setDraft(ruleDraft(data.current)); setReason(''); setError(''); }}>恢复当前生效规则</button></div>
            <section className="st-rule-recompute"><h3>按当前版本重新计算</h3><p>用于核对业务证据或数据调整后的结果；仅覆盖所选归属，不使用列表日期筛选。同一次失败重试沿用请求编号。</p><label>重算原因<textarea maxLength={500} value={recomputeReason} onChange={event => { if (!current() || pending.current || reading.current || renderRevision !== editRevision.current) return; markEdited(); setRecomputeReason(event.target.value); }} /></label><button disabled={busy || loading || verifyRequired || conflict} onClick={() => void mutate('recompute')}>按当前版本重算</button></section>
        </fieldset>}
        {data && <details className="st-definitions"><summary>历史规则版本（{data.history.length}）</summary>{data.history.length ? data.history.map(rule => <article key={rule.rule_version}><h4>{rule.rule_version}</h4><RuleSummary rule={rule} /></article>) : <p>暂无已保存的历史版本，当前使用默认规则。</p>}</details>}
    </div>;
}
