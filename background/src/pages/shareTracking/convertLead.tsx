import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { tracking, Visitor } from '../../api/shareTracking';
import { B_SIDE_IDENTITIES } from './model';

type LeadKind = 'sales' | 'partner' | 'identity_pending';
const leadKindOf = (identity: string): LeadKind => B_SIDE_IDENTITIES.has(identity) ? 'partner' : identity ? 'sales' : 'identity_pending';
const kindNames: Record<LeadKind, string> = { sales: '销售线索', partner: '合作线索', identity_pending: '身份待确认线索' };
const leadQueuePath = (kind: LeadKind) => kind === 'partner' ? '/customer-leads?queue=partner' : kind === 'identity_pending' ? '/customer-leads?queue=identity_pending' : '/customer-leads';

// 访客意向 → 销售线索的半自动转化入口。前端只做与后端 trackingLeadGate
// 同口径的可见性预判（乡墅业主才显示按钮），真正的门控、查重与证据快照
// 都在后端 POST /visitors/:key/lead 里裁决。
export function ConvertLeadButton({ visitor, canCRM = false }: { visitor: Visitor; canCRM?: boolean }) {
    const navigate = useNavigate();
    const identity = (visitor.identity_type || '').trim();
    const eligible = canCRM && visitor.identity_state === 'identified' && (identity === '' || identity === '乡墅业主' || B_SIDE_IDENTITIES.has(identity));
    const kind = leadKindOf(identity);
    const [state, setState] = useState<{ stage: 'idle' | 'confirm' | 'done'; pending: boolean; error: string; leadID?: number; existed?: boolean; crm?: boolean; kind?: LeadKind }>({ stage: 'idle', pending: false, error: '' });
    const request = useRef<AbortController>();
    const scope = `${eligible}:${visitor.visitor_key}`;
    const active = useRef(scope); active.current = scope;
    useEffect(() => {
        setState({ stage: 'idle', pending: false, error: '' });
        return () => { request.current?.abort(); request.current = undefined; };
    }, [scope]);
    if (!eligible) return null;
    const convert = async () => {
        if (active.current !== scope || request.current) return;
        const controller = new AbortController(); request.current = controller;
        setState(stage => ({ ...stage, pending: true, error: '' }));
        try {
            const result = await tracking.convertLead(visitor.visitor_key);
            if (controller.signal.aborted || active.current !== scope || request.current !== controller) return;
            setState({ stage: 'done', pending: false, error: '', leadID: result.lead_id, existed: result.existed, crm: result.crm, kind: result.lead_type || kind });
        } catch (error) {
            if (!controller.signal.aborted && active.current === scope) {
                // 转化失败可能是门控（B端/未确认身份）、查重或服务波动：原文透出给销售判断。
                setState(stage => ({ ...stage, pending: false, error: error instanceof Error ? error.message : '转化为线索失败，请重试' }));
            }
        } finally {
            if (request.current === controller) { request.current = undefined; if (!controller.signal.aborted && active.current === scope) setState(stage => ({ ...stage, pending: false })); }
        }
    };
    return <div className="st-crm-target st-lead-convert">
        {state.stage === 'done' && state.crm
            ? <p role="status" className="st-muted">{state.existed ? `该客户已有未关闭线索（#${state.leadID}），已追加本次转化证据` : `已提交 CRM（${kindNames[state.kind || kind]} #${state.leadID}）`}，由客户主管分配后在客户管理中跟进 <button onClick={() => navigate('/customer-management')}>前往客户管理</button></p>
            : state.stage === 'done'
            ? <p role="status" className="st-muted">{state.existed ? `已有未关闭线索（#${state.leadID}），已追加本次转化证据` : `已创建${kindNames[state.kind || kind]} #${state.leadID}`} <button onClick={() => navigate(leadQueuePath(state.kind || kind))}>前往线索工作台</button></p>
            : state.stage === 'confirm'
                ? <p role="alert">{kind === 'partner' ? `该访客为${identity}，将转为合作线索（含证据快照）` : kind === 'identity_pending' ? '该访客尚未选择身份，将转为身份待确认线索（含证据快照）' : '将把该访客的访问动机转为销售线索（含证据快照）'}<button className="st-primary" disabled={state.pending} onClick={convert}>{state.pending ? '正在转化…' : '确认转化'}</button><button disabled={state.pending} onClick={() => setState({ stage: 'idle', pending: false, error: '' })}>取消</button></p>
                : <button disabled={state.pending} onClick={() => setState({ stage: 'confirm', pending: false, error: '' })}>{kind === 'partner' ? '转为合作线索' : kind === 'identity_pending' ? '转为待确认线索' : '转为销售线索'}</button>}
        {state.error && <p role="alert" className="st-error">{state.error}</p>}
    </div>;
}
