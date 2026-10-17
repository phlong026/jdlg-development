import React, { useEffect, useState } from 'react';
import { Page, Session, tracking, Visitor } from '../api/shareTracking';
import { intentLabels, pageLabel, time } from './shareTracking/model';

// 线索详情内嵌的小程序访问轨迹：把「分享访问记录」里该客户的最近行为
// 带到销售眼前。依赖 tracking 查看权限（can_view），无权限或无记录时
// 静默降级为一行说明，绝不阻断线索详情本身。
export function CustomerLeadTrail({ userId }: { userId: number }) {
    const [state, setState] = useState<{ status: 'loading' | 'ready' | 'hidden'; visitor?: Visitor; sessions?: Session[] }>({ status: userId > 0 ? 'loading' : 'hidden' });
    useEffect(() => {
        if (userId <= 0) return;
        const controller = new AbortController();
        let active = true;
        setState({ status: 'loading' });
        (async () => {
            try {
                const context = await tracking.context(controller.signal);
                if (!context.can_view) {
                    if (active) setState({ status: 'hidden' });
                    return;
                }
                const visitorKey = `u:${userId}`;
                const page: Page<Visitor> = await tracking.visitors({ visitor: visitorKey, limit: 1 }, controller.signal);
                const visitor = page.items?.[0];
                if (!visitor) {
                    if (active) setState({ status: 'ready' });
                    return;
                }
                let sessions: Session[] = [];
                try {
                    const sessionPage = await tracking.sessions(visitorKey, { limit: 3 }, controller.signal);
                    sessions = sessionPage.items || [];
                } catch {
                    // 意向与到访摘要仍有价值；轨迹明细缺失只降级不报错。
                }
                if (active) setState({ status: 'ready', visitor, sessions });
            } catch {
                if (active) setState({ status: 'hidden' });
            }
        })();
        return () => { active = false; controller.abort(); };
    }, [userId]);
    if (state.status === 'hidden') return null;
    return <section className="customer-lead-detail-section">
        <h4>小程序访问轨迹</h4>
        {state.status === 'loading' ? <p>正在读取访问记录…</p> : !state.visitor ? <p>该客户暂无小程序访问记录（或已行使删除权）。</p> : <>
            <div className="customer-lead-detail-grid">
                <div><span>当前意向参考</span><strong>{intentLabels[state.visitor.intent?.level || 'unknown']}</strong></div>
                <div><span>身份</span><strong>{state.visitor.identity_type || (state.visitor.identity_state === 'identified' ? '未确认' : '匿名')}</strong></div>
                <div><span>累计浏览</span><strong>{state.visitor.page_views} 页 / {state.visitor.session_count} 次到访</strong></div>
                <div><span>最近访问</span><strong>{time(state.visitor.last_visit)}</strong></div>
            </div>
            {state.visitor.intent?.reasons?.length ? <p>{state.visitor.intent.reasons[0]}</p> : null}
            {state.sessions.length ? <ul className="customer-lead-trail">
                {state.sessions.map(session => <li key={session.session_id}>
                    <strong>{time(session.started_at)}</strong>
                    <span>{session.entry_share_id ? '通过分享进入' : '直接访问'} · {session.page_views.slice(0, 4).map(view => pageLabel(view.route_key, view.title)).join(' → ') || '无页面明细'}{session.page_views.length > 4 ? ' …' : ''}</span>
                </li>)}
            </ul> : <p>最近无会话明细，累计口径见上方统计。</p>}
        </>}
    </section>;
}
