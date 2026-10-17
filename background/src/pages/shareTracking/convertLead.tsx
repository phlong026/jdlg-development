import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { tracking, Visitor } from '../../api/shareTracking';

// 访客意向 → 销售线索的半自动转化入口。前端只做与后端 trackingLeadGate
// 同口径的可见性预判（乡墅业主才显示按钮），真正的门控、查重与证据快照
// 都在后端 POST /visitors/:key/lead 里裁决。
export function ConvertLeadButton({ visitor, canCRM = false }: { visitor: Visitor; canCRM?: boolean }) {
    const navigate = useNavigate();
    const eligible = canCRM && visitor.identity_state === 'identified' && (visitor.identity_type || '').trim() === '乡墅业主';
    const [state, setState] = useState<{ stage: 'idle' | 'confirm' | 'done'; pending: boolean; error: string; leadID?: number; existed?: boolean }>({ stage: 'idle', pending: false, error: '' });
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
            setState({ stage: 'done', pending: false, error: '', leadID: result.lead_id, existed: result.existed });
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
        {state.stage === 'done'
            ? <p role="status" className="st-muted">{state.existed ? `已有未关闭线索（#${state.leadID}），已追加本次转化证据` : `已创建线索 #${state.leadID}`} <button onClick={() => navigate('/customer-leads')}>前往线索工作台</button></p>
            : state.stage === 'confirm'
                ? <p role="alert">将把该访客的访问动机转为销售线索（含证据快照）<button className="st-primary" disabled={state.pending} onClick={convert}>{state.pending ? '正在转化…' : '确认转化'}</button><button disabled={state.pending} onClick={() => setState({ stage: 'idle', pending: false, error: '' })}>取消</button></p>
                : <button disabled={state.pending} onClick={() => setState({ stage: 'confirm', pending: false, error: '' })}>转为销售线索</button>}
        {state.error && <p role="alert" className="st-error">{state.error}</p>}
    </div>;
}
