import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { tracking, Visitor } from '../../api/shareTracking';

export function CRMTargetButton({ visitor, owner, canCRM = false }: { visitor: Visitor; owner?: string; canCRM?: boolean }) {
    const navigate = useNavigate();
    const [state, setState] = useState({ pending: false, error: '' });
    const request = useRef<AbortController>();
    const scope = `${canCRM}:${visitor.identity_state}:${visitor.visitor_key}:${owner || ''}:${visitor.crm_label || ''}`;
    const active = useRef(scope); active.current = scope;
    const eligible = canCRM && visitor.identity_state === 'identified' && Boolean(visitor.crm_label);
    useEffect(() => { setState({ pending: false, error: '' }); return () => { request.current?.abort(); request.current = undefined; }; }, [scope]);
    const open = async () => {
        if (!eligible || active.current !== scope || request.current) return;
        const controller = new AbortController(); request.current = controller;
        setState({ pending: true, error: '' });
        try {
            const target = await tracking.crmTarget(visitor.visitor_key, owner, controller.signal);
            if (controller.signal.aborted || active.current !== scope || request.current !== controller) return;
            if (target.target_kind !== 'customer' || !target.customer_id || target.customer_id.length > 128 || /[\/\\%;\r\n]/.test(target.customer_id) || ['.', '..'].includes(target.customer_id) || !target.business_number || (owner && target.owner_id !== owner)) throw new Error('客户关联返回不一致，请重新核实');
            // Only navigate to this app's fixed route. CRM rechecks permission on its detail read.
            navigate(`/customer-management?tab=customers&customer=${encodeURIComponent(target.customer_id)}&from=share-tracking`);
        } catch (error) {
            if (!controller.signal.aborted && active.current === scope) setState({ pending: false, error: error instanceof Error ? error.message : '客户关联暂不可访问，请重试' });
        } finally {
            if (request.current === controller) { request.current = undefined; if (!controller.signal.aborted && active.current === scope) setState(value => ({ ...value, pending: false })); }
        }
    };
    if (!eligible) return null;
    return <div className="st-crm-target"><button className="st-primary" disabled={state.pending} onClick={open}>{state.pending ? '正在核验客户权限…' : '查看客户 / 线索'}</button>{state.error && <p role="alert" className="st-error">{state.error}</p>}</div>;
}
