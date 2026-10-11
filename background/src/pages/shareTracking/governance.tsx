import React, { useEffect, useRef, useState } from 'react';
import { Context, Intent, IntentEvidence, tracking, Visitor } from '../../api/shareTracking';
import { intentLabels, time } from './model';

export type GovernanceProps = { context?: Context; onBusyChange?: (value: boolean) => void; onDirtyChange?: (value: boolean) => void; changed?: () => void };
export function validateOverride(reason: string, expiry: string, now = Date.now()): string {
    if (!reason.trim() || reason.trim().length > 500) return '请填写 1–500 字的修正原因';
    const expires = new Date(expiry).getTime();
    if (!Number.isFinite(expires) || expires <= now || expires > now + 30 * 86400000) return '有效期必须晚于现在且不超过 30 天';
    return '';
}
function RuleResult({ evidence }: { evidence: IntentEvidence }) {
    return <><p>{intentLabels[evidence.level] || '待判断'} · 规则 {evidence.rule_version || '未提供'}</p><p>评估窗口 {time(evidence.window_start)} 至 {time(evidence.window_end)}</p><ul>{evidence.reasons?.map((reason, i) => <li key={i}>{reason}</li>)}</ul></>;
}
export function VisitorGovernance({ visitor, context, onBusyChange, onDirtyChange, changed, onDeleted }: GovernanceProps & { visitor: Visitor; onDeleted: () => void }) {
    const owners = context?.owners || [];
    const [owner, setOwner] = useState('');
    const selectedOwner = owner || (owners.length === 1 ? owners[0].id : '');
    const ownerAllowed = Boolean(selectedOwner) && owners.some(item => item.id === selectedOwner);
    const ownerTarget = useRef(selectedOwner); ownerTarget.current = selectedOwner;
    const [customer, setCustomer] = useState(''); const [number, setNumber] = useState(''); const [basis, setBasis] = useState('');
    const [level, setLevel] = useState<Intent>('unknown'); const [reason, setReason] = useState(''); const [expiry, setExpiry] = useState('');
    const [deleteReason, setDeleteReason] = useState(''); const [deleteDialog, setDeleteDialog] = useState(false); const [confirmation, setConfirmation] = useState('');
    const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [notice, setNotice] = useState(''); const [completed, setCompleted] = useState(false);
    const pending = useRef(false); const alive = useRef(true); const target = useRef(visitor.visitor_key); target.current = visitor.visitor_key;
    const dirty = Boolean(customer || number || basis || level !== 'unknown' || reason || expiry || deleteReason || deleteDialog);
    useEffect(() => { onDirtyChange?.(dirty); }, [dirty, onDirtyChange]);
    useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
    useEffect(() => { const guard = (event: BeforeUnloadEvent) => { if (dirty || pending.current) { event.preventDefault(); event.returnValue = ''; } }; window.addEventListener('beforeunload', guard); return () => window.removeEventListener('beforeunload', guard); }, [dirty]);
    const signedIn = visitor.identity_state === 'identified' && visitor.visitor_key.startsWith('u:');
    const permitted = { crm: context?.can_crm === true && signedIn, rules: context?.can_rules === true, deletion: context?.can_delete === true };
    async function perform(kind: keyof typeof permitted, operation: (key: string) => Promise<boolean>, message: string, deleted = false) {
        if (!alive.current || target.current !== visitor.visitor_key || pending.current || completed || !permitted[kind]) return;
        if (kind !== 'deletion' && (!ownerAllowed || ownerTarget.current !== selectedOwner)) { setError('请选择已授权的单一归属范围'); return; }
        const key = visitor.visitor_key; pending.current = true; setBusy(true); onBusyChange?.(true); setError(''); setNotice('');
        try {
            if (!await operation(key)) throw new Error('服务端未确认操作完成，请核对状态后重试');
            if (!alive.current || target.current !== key) return;
            setCompleted(true); setNotice(message); setCustomer(''); setNumber(''); setBasis(''); setLevel('unknown'); setReason(''); setExpiry(''); setDeleteReason(''); setDeleteDialog(false); setConfirmation(''); onDirtyChange?.(false); changed?.();
            if (deleted) onDeleted();
        } catch (error) { if (alive.current && target.current === key) setError(`${(error as Error).message}。草稿已保留；结果不明确时请先刷新核对，不要重复提交。`); }
        finally { pending.current = false; if (alive.current && target.current === key) { setBusy(false); onBusyChange?.(false); } }
    }
    if (visitor.identity_state === 'deleted') return null;
    const original = visitor.intent?.original_rule;
    const override = visitor.intent?.override;
    return <section className="st-governance" aria-label="访客治理"><h3>身份与意向依据</h3><p>目标账号 / 访客标识：{visitor.visitor_key}</p><p>{visitor.crm_label || '当前列表范围未提供 CRM 关联'}</p>
        <h4>当前意向结果</h4>{visitor.intent && <RuleResult evidence={visitor.intent} />}
        {(override || visitor.intent?.manual_note) && <p>人工修正：{override ? intentLabels[override.level] : intentLabels[visitor.intent.level]} · {override?.reason || visitor.intent.manual_note} · 操作者 {override?.actor || '接口未提供'} · 修正时间 {time(override?.created_at)} · 有效至 {time(override?.expires_at)}</p>}
        {original ? <><h4>原规则结果</h4><RuleResult evidence={original} /></> : <p className="st-warning">接口尚未返回独立的原规则结果；当前结果可能包含人工覆盖，不能当作原规则结果。</p>}
        {error && <p className="st-error" role="alert">{error}</p>}{notice && <p role="status">{notice} 请关闭详情并刷新后重新打开，核对最新状态。</p>}
        {!completed && <fieldset disabled={busy}><legend>专门授权操作</legend>
            {(permitted.crm || permitted.rules) && <><label>治理归属范围<select value={selectedOwner} onChange={e => { if (!pending.current) setOwner(e.target.value); }}><option value="">请选择归属范围</option>{owners.map(item => <option key={item.id} value={item.id}>{item.name || item.id}</option>)}</select></label><p>CRM 映射和人工意向修正仅作用于所选归属；上方结果仍是当前列表筛选范围。删除操作不随此选择缩小。</p>{!owners.length && <p className="st-warning">未取得授权归属，暂不能提交映射或意向修正。</p>}</>}
            {permitted.crm && <section><h4>明确 CRM 映射</h4><p>只对已登录账号建立明确映射。客户 ID、业务编号及依据须通过上游业务关系与归属范围验证；昵称、头像、相似姓名或分享对象均不能作为自动匹配依据。验证未接入时服务端会拒绝。</p>
                <label>CRM 客户 ID<input value={customer} maxLength={128} onChange={e => setCustomer(e.target.value)} /></label><label>业务编号<input value={number} maxLength={100} onChange={e => setNumber(e.target.value)} /></label><label>已验证业务关系依据<textarea value={basis} maxLength={500} onChange={e => setBasis(e.target.value)} /></label>
                <button disabled={busy || !ownerAllowed || !customer.trim() || !number.trim() || !basis.trim()} onClick={() => { if (!customer.trim() || !number.trim() || !basis.trim()) return; void perform('crm', async key => (await tracking.linkCRM({ visitor_key: key, customer_id: customer.trim(), business_number: number.trim(), basis: basis.trim() }, selectedOwner)).linked, '服务端已确认 CRM 映射'); }}>确认并提交映射</button>
                <button disabled={busy || !ownerAllowed} onClick={() => { if (pending.current || !ownerAllowed || !window.confirm(`确认解除 ${selectedOwner} 范围内 ${visitor.visitor_key} 的 CRM 映射？不会删除访问记录。`)) return; void perform('crm', async key => (await tracking.unlinkCRM(key, selectedOwner)).unlinked, '服务端已确认解除映射'); }}>解除 CRM 映射</button>
            </section>}
            {context?.can_crm && !signedIn && <p>匿名访客不能建立 CRM 映射；须先由服务端确认登录账号。</p>}
            {permitted.rules && <section><h4>人工意向修正</h4><p>须填写原因和有效期（最多 30 天）；操作人和时间由服务端记录。</p><label>修正等级<select value={level} onChange={e => setLevel(e.target.value as Intent)}>{Object.entries(intentLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><label>修正原因<textarea value={reason} maxLength={500} onChange={e => setReason(e.target.value)} /></label><label>有效至（当前设备时区）<input type="datetime-local" value={expiry} onChange={e => setExpiry(e.target.value)} /></label><button disabled={busy || !ownerAllowed} onClick={() => { const validation = validateOverride(reason, expiry); if (validation) { setError(validation); return; } void perform('rules', async key => (await tracking.overrideIntent({ visitor_key: key, level, reason: reason.trim(), expires_at: new Date(expiry).toISOString() }, selectedOwner)).updated, '服务端已确认人工意向修正'); }}>保存人工修正</button></section>}
            {permitted.deletion && <section><h4>删除访问数据</h4><p>删除当前授权范围内该访客的访问记录、汇总和身份映射；不会撤回已发布素材。缓存、授权导出副本和备份须按批准策略治理，不能据此认定备份已立即清除。</p><button disabled={busy} onClick={() => { setConfirmation(''); setDeleteDialog(true); }}>申请删除此访客数据</button>
                {deleteDialog && <section role="alertdialog" aria-modal="false" aria-label="确认删除指定访客数据"><h4>确认删除：{visitor.visitor_key}</h4><p>此操作不可通过本页面恢复。每次删除都需要单独确认，请核对目标。</p><label>删除原因<textarea value={deleteReason} maxLength={500} onChange={e => setDeleteReason(e.target.value)} /></label><label>输入完整访客标识以确认<input autoComplete="off" value={confirmation} onChange={e => setConfirmation(e.target.value)} /></label><button disabled={busy || confirmation !== visitor.visitor_key || !deleteReason.trim()} onClick={() => { if (confirmation !== visitor.visitor_key || !deleteReason.trim()) return; void perform('deletion', async key => (await tracking.deleteVisitor({ visitor_key: key, reason: deleteReason.trim() })).status === 'completed', '服务端已确认当前授权范围内的数据删除；备份和副本仍按治理策略处理', true); }}>确认删除访问记录与映射</button><button disabled={busy} onClick={() => { setDeleteDialog(false); setConfirmation(''); }}>取消删除</button></section>}
            </section>}
            {!permitted.crm && !permitted.rules && !permitted.deletion && <p>当前账号没有适用于此访客的治理操作权限。</p>}
        </fieldset>}
    </section>;
}
