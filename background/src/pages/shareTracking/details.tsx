import React, { useState } from 'react';
import { Content, Page, PageView, tracking, Visitor } from '../../api/shareTracking';
import { ContentBody, Counts, Feedback, MediaPreview, Pager, SnapshotInfo, useResource, VisitorCard } from './components';
import { CRMTargetButton } from './crmTarget';
import { GovernanceProps, VisitorGovernance } from './governance';
import { duration, intentWindowQuery, pageLabel, qualityText, time } from './model';

// Detail endpoints bind cursors to their object, permissions and filter. Keep the
// returned first cursor when navigating back so profile revisions cannot drift.
export function useDetailPage<T>(scope: string, fetchPage: (cursor: string | undefined, signal: AbortSignal) => Promise<Page<T>>) {
    const [pagination, setPagination] = useState<{ scope: string; cursors: string[] }>({ scope, cursors: [] });
    const cursors = pagination.scope === scope ? pagination.cursors : [];
    if (pagination.scope !== scope) setPagination({ scope, cursors: [] });
    const snapshot = React.useRef<{ scope: string; first?: string }>({ scope });
    if (snapshot.current.scope !== scope) snapshot.current = { scope };
    const active = snapshot.current;
    const result = useResource(async signal => {
        const data = await fetchPage(cursors[cursors.length - 1] ?? active.first, signal);
        if (!signal.aborted && snapshot.current === active) active.first = data.first_cursor;
        return data;
    }, JSON.stringify([scope, cursors]));
    const setCursors = (values: string[]) => { if (snapshot.current === active) setPagination({ scope, cursors: values }); };
    const reset = () => { snapshot.current = { scope }; setPagination({ scope, cursors: [] }); result.reload(); };
    return { result, cursors, setCursors, reset };
}
export function PageVisit({ view, query }: { view: PageView; query: Record<string, unknown> }) {
    return <article className="st-page-visit"><div className="st-page-visit-head"><strong>{pageLabel(view.route_key, view.title)}</strong><span> · 停留 {duration(view.visible_ms)}</span></div><p>进入 {time(view.started_at)}{view.ended_at ? ` · 离开 ${time(view.ended_at)}` : ''}</p>{view.quality_flags?.length > 0 && <p className="st-warning">{qualityText(view.quality_flags)}</p>}</article>;
}
export function VisitorDetails({ visitor, query, contentId, ...governance }: { visitor: Visitor; query: Record<string, unknown>; contentId?: string } & GovernanceProps) {
    const [deleted, setDeleted] = useState(false);
    const [evidenceWindow, setEvidenceWindow] = useState(false);
    const evidenceQuery = intentWindowQuery(query, visitor.intent);
    const showingEvidence = evidenceWindow && Boolean(evidenceQuery);
    const selectedQuery = showingEvidence ? evidenceQuery! : query;
    const [expanded, setExpanded] = useState<string[]>([]); const [otherPages, setOtherPages] = useState(false);
    const params = { ...selectedQuery, content_id: showingEvidence ? undefined : contentId, include_related: otherPages };
    const { result, cursors, setCursors, reset } = useDetailPage(JSON.stringify([visitor.visitor_key, params]), (cursor, signal) => tracking.sessions(visitor.visitor_key, { ...params, cursor }, signal));
    if (deleted) return <p role="status">服务端已确认删除当前授权范围内该访客的数据。备份和副本按治理策略处理。请关闭详情并刷新记录。</p>;
    return <section><h3>{visitor.identity_state === 'anonymous' ? '匿名访客' : visitor.nickname || '访问账号'} · 访问详情</h3><p className="st-muted">{showingEvidence ? `意向评估时间：${time(visitor.intent.window_start)} 至 ${time(visitor.intent.window_end)}（这段时间内该访客看过的全部素材）` : `统计范围：${query.start ? time(String(query.start)) : '全部保留数据'} 至 ${query.end ? time(String(query.end)) : '现在'}`}</p>{showingEvidence && <button onClick={() => { setEvidenceWindow(false); setCursors([]); setExpanded([]); }}>返回列表筛选范围</button>}{contentId && !showingEvidence && <label className="st-checkbox"><input type="checkbox" checked={otherPages} onChange={event => { setOtherPages(event.target.checked); setCursors([]); }} />查看这些到访中的其他页面</label>}<Feedback {...result} retry={result.reload} resetSnapshot={reset} empty={result.data?.items.length === 0} />{result.data?.items.map(session => <article className="st-session" key={session.session_id}><button aria-expanded={expanded.includes(session.session_id)} onClick={() => setExpanded(expanded.includes(session.session_id) ? expanded.filter(id => id !== session.session_id) : [...expanded, session.session_id])}><strong>{time(session.started_at)}</strong><span>{session.entry_share_id ? '通过分享进入' : '直接访问'} · 看了 {session.page_views.length} 个页面 · {expanded.includes(session.session_id) ? '收起轨迹' : '展开轨迹'}</span></button><p className="st-muted">最近活动 {time(session.last_active_at)}</p>{expanded.includes(session.session_id) && session.page_views.map(view => <PageVisit key={view.page_view_id} view={view} query={{ ...selectedQuery, include_related: otherPages && !showingEvidence }} />)}</article>)}<Pager cursor={cursors} next={result.data?.next_cursor} onNext={value => setCursors([...cursors, value])} onPrevious={() => setCursors(cursors.slice(0, -1))} /><p className="st-footnote">同一个人用不同设备访问会分开统计；停留时长只统计页面实际展示的时间。</p><details className="st-definitions"><summary>管理操作（CRM 关联、意向修正、数据删除）</summary><VisitorGovernance key={visitor.visitor_key} visitor={visitor} {...governance} onOpenEvidence={evidenceQuery ? () => { setEvidenceWindow(true); setCursors([]); setExpanded(visitor.intent.evidence?.map(item => item.session_id).filter((id): id is string => Boolean(id)) || []); } : undefined} onDeleted={() => setDeleted(true)} /></details></section>;
}
export function ContentDetails({ content, query, manage, edit, ...governance }: { content: Content; query: Record<string, unknown>; manage: boolean; edit: (content: Content) => void } & GovernanceProps) {
    const scope = JSON.stringify([content.content_id, query]);
    const [pagination, setPagination] = useState<{ scope: string; cursors: string[] }>({ scope, cursors: [] });
    const cursors = pagination.scope === scope ? pagination.cursors : [];
    if (pagination.scope !== scope) setPagination({ scope, cursors: [] });
    const snapshot = React.useRef<{ scope: string; as_of?: string; first_cursor?: string }>({ scope });
    if (snapshot.current.scope !== scope) snapshot.current = { scope };
    const activeSnapshot = snapshot.current;
    const setCursors = (values: string[]) => { if (snapshot.current === activeSnapshot) setPagination({ scope, cursors: values }); };
    const [visitor, setVisitor] = useState<Visitor>();
    const busy = React.useRef(false); const dirty = React.useRef(false);
    const setBusy = (value: boolean) => { busy.current = value; governance.onBusyChange?.(value); };
    const setDirty = (value: boolean) => { dirty.current = value; governance.onDirtyChange?.(value); };
    const detail = useResource(signal => tracking.content(content.content_id, query, signal), JSON.stringify([content.content_id, query]));
    const result = useResource(async signal => {
        const saved = snapshot.current?.scope === scope ? snapshot.current : undefined;
        const data = await tracking.contentVisitors(content.content_id, { ...query, as_of: saved?.as_of ?? query.as_of, cursor: cursors[cursors.length - 1] ?? saved?.first_cursor }, signal);
        if (!signal.aborted && snapshot.current === activeSnapshot) Object.assign(activeSnapshot, { as_of: data.as_of, first_cursor: data.first_cursor });
        return data;
    }, JSON.stringify([scope, cursors]));
    if (visitor) return <><button onClick={() => { if (busy.current || (dirty.current && !window.confirm('有未提交的治理修改，确认放弃并返回？'))) return; setDirty(false); setVisitor(undefined); }}>← 返回素材访客</button><VisitorDetails key={visitor.visitor_key} visitor={visitor} contentId={content.content_id} query={{ ...query, as_of: result.data?.as_of ?? query.as_of, metric_version: result.data?.metric_version ?? query.metric_version }} {...governance} onBusyChange={setBusy} onDirtyChange={setDirty} /></>;
    const item = detail.data;
    const typeLabel: Record<string, string> = { image: '图片', video: '视频', rich_text: '图文' };
    return <section><Feedback {...detail} retry={detail.reload} />{item && <><h3>{item.title}</h3><p>{typeLabel[item.type] || item.type} · {item.status === 'revoked' ? '已撤回，内容不可阅读' : item.status === 'draft' ? '草稿' : '已发布'}</p>{item.status !== 'revoked' && <><MediaPreview asset={item.cover} title={item.title} original /><ContentBody content={item} />{item.media?.filter(asset => asset.media_id !== item.cover?.media_id).map(asset => <MediaPreview key={asset.media_id} asset={asset} title={asset.title || item.title} original />)}</>}<p>这段时间：被看了 {item.page_views} 次 · 累计停留 {duration(item.visible_ms)}</p>{item.read_only && <p>原页面内容快照，只读；不提供历史正文回放。</p>}{manage && !item.read_only && <button onClick={() => edit(item)}>编辑草稿 / 发布与撤回</button>}</>}<h3>访问该素材的访客</h3><Counts data={result.data} /><SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={() => { snapshot.current = { scope }; setPagination({ scope, cursors: [] }); result.reload(); }} empty={result.data?.items.length === 0} /><div className="st-cards">{result.data?.items.map(item => <VisitorCard key={item.visitor_key} visitor={item} onOpen={() => setVisitor(item)}><CRMTargetButton visitor={item} canCRM={governance.context.can_crm} owner={typeof query.owner === 'string' && query.owner ? query.owner : undefined} /></VisitorCard>)}</div><Pager cursor={cursors} next={result.data?.next_cursor} onNext={value => setCursors([...cursors, value])} onPrevious={() => setCursors(cursors.slice(0, -1))} /></section>;
}
