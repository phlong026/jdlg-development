import React, { useState } from 'react';
import { Content, tracking, Visitor } from '../../api/shareTracking';
import { Counts, Feedback, Pager, SnapshotInfo, VisitorCard } from './components';
import { CRMTargetButton } from './crmTarget';
import { PageVisit, useDetailPage } from './details';
import { time } from './model';

function VisitTrail({ visitor, query }: { visitor: string; query: Record<string, unknown> }) {
    const [related, setRelated] = useState(false);
    const params = { ...query, include_related: related };
    const { result, cursors, setCursors, reset } = useDetailPage(JSON.stringify([visitor, params]), (cursor, signal) => tracking.sessions(visitor, { ...params, cursor }, signal));
    const [expanded, setExpanded] = useState<string[]>([]);
    return <section className="st-inline-trail" aria-label="素材访客访问轨迹"><label className="st-checkbox"><input type="checkbox" checked={related} onChange={event => { setRelated(event.target.checked); setExpanded([]); }} />查看这些会话中的其他页面</label><SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={reset} empty={result.data?.items.length === 0} />{result.data?.items.map(session => <article className="st-session" key={session.session_id}><button aria-expanded={expanded.includes(session.session_id)} onClick={() => setExpanded(expanded.includes(session.session_id) ? expanded.filter(id => id !== session.session_id) : [...expanded, session.session_id])}><strong>{time(session.started_at)}</strong><span>{session.page_views.length} 个页面 · {expanded.includes(session.session_id) ? '收起页面' : '展开页面'}</span></button>{expanded.includes(session.session_id) && session.page_views.map(view => <PageVisit key={view.page_view_id} view={view} query={params} />)}</article>)}<Pager cursor={cursors} next={result.data?.next_cursor} onNext={value => setCursors([...cursors, value])} onPrevious={() => setCursors(cursors.slice(0, -1))} /><p className="st-muted">只读访问轨迹；后台和锁屏不计时，可见不代表读完。</p></section>;
}
export function ContentVisitorsList({ content, query, canCRM, onOpen }: { content: Content; query: Record<string, unknown>; canCRM: boolean; onOpen: (visitor: Visitor) => void }) {
    const [expanded, setExpanded] = useState<string[]>([]);
    const { result, cursors, setCursors, reset } = useDetailPage(JSON.stringify([content.content_id, query]), (cursor, signal) => tracking.contentVisitors(content.content_id, { ...query, cursor }, signal));
    const scopeQuery = { ...query, content_id: content.content_id, as_of: result.data?.as_of ?? query.as_of };
    const toggle = (key: string) => setExpanded(expanded.includes(key) ? expanded.filter(id => id !== key) : [...expanded, key]);
    return <section className="st-content-visitors st-inline-visitors" aria-label={`访问素材 ${content.title} 的访客`}><Counts data={result.data} /><SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={() => { setExpanded([]); reset(); }} empty={result.data?.items.length === 0} /><div className="st-inline-visitor-list">{result.data?.items.map(visitor => <VisitorCard key={visitor.visitor_key} visitor={visitor} onOpen={() => toggle(visitor.visitor_key)} onOpenRecords={() => onOpen(visitor)}><div className="st-card-actions"><CRMTargetButton visitor={visitor} canCRM={canCRM} owner={typeof query.owner === 'string' && query.owner ? query.owner : undefined} /><button aria-expanded={expanded.includes(visitor.visitor_key)} onClick={() => toggle(visitor.visitor_key)}>{expanded.includes(visitor.visitor_key) ? '收起访问轨迹' : '展开访问轨迹'}</button><button onClick={() => onOpen(visitor)}>客户访问详情</button></div>{expanded.includes(visitor.visitor_key) && <VisitTrail key={JSON.stringify([visitor.visitor_key, scopeQuery])} visitor={visitor.visitor_key} query={scopeQuery} />}</VisitorCard>)}</div><Pager cursor={cursors} next={result.data?.next_cursor} onNext={value => { setExpanded([]); setCursors([...cursors, value]); }} onPrevious={() => { setExpanded([]); setCursors(cursors.slice(0, -1)); }} /></section>;
}
export function ContentVisitorPanel(props: { content: Content; query: Record<string, unknown>; canCRM: boolean; onOpen: (visitor: Visitor) => void }) {
    const [open, setOpen] = useState(false);
    return <div className="st-content-visitor-toggle"><button aria-expanded={open} onClick={() => setOpen(!open)}>{open ? '收起素材访客' : '展开素材访客'}</button>{open && <ContentVisitorsList key={JSON.stringify([props.content.content_id, props.query, props.canCRM])} {...props} />}</div>;
}
