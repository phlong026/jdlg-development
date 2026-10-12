import React, { useState } from 'react';
import { Content, Page, PageView, tracking, Visitor } from '../../api/shareTracking';
import { ContentBody, Counts, Feedback, MediaPreview, Pager, SnapshotInfo, useResource, VisitorCard } from './components';
import { CRMTargetButton } from './crmTarget';
import { GovernanceProps, VisitorGovernance } from './governance';
import { duration, eventLabels, intentWindowQuery, qualityText, time } from './model';

// Detail endpoints bind cursors to their object, permissions and filter. Keep the
// returned first cursor when navigating back so profile revisions cannot drift.
function useDetailPage<T>(scope: string, fetchPage: (cursor: string | undefined, signal: AbortSignal) => Promise<Page<T>>) {
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
function Events({ view, query }: { view: PageView; query: Record<string, unknown> }) {
    const { result: data, cursors, setCursors, reset } = useDetailPage(JSON.stringify([view.page_view_id, query]), (cursor, signal) => tracking.events(view.page_view_id, { ...query, cursor }, signal));
    return <div className="st-events"><Feedback {...data} retry={data.reload} resetSnapshot={reset} empty={data.data?.items.length === 0} /><SnapshotInfo data={data.data} /><ol>{data.data?.items.map(event => <li key={event.event_id}><strong>{eventLabels[event.event_type] || event.event_type}</strong><p>{time(event.occurred_at)} · 接收于 {time(event.received_at)}</p>{event.target_content_id && <p>目标素材 {event.target_content_id}</p>}{event.visible_delta_ms != null && <p>可见增量 {duration(event.visible_delta_ms)}</p>}{event.scroll_depth != null && <p>滚动到达 {event.scroll_depth}%（不表示读完）</p>}{event.current_time_ms != null && <p>播放位置 {duration(event.current_time_ms)} / {duration(event.video_duration_ms)}</p>}{event.video_watch_ms != null && <p>有效观看墙钟 {duration(event.video_watch_ms)} · 不重复覆盖 {duration(event.unique_played_ms)}</p>}{event.content_version != null && <p>访问版本 v{event.content_version}</p>}{event.quality_flags?.length > 0 && <p className="st-warning">质量标记：{qualityText(event.quality_flags)}</p>}</li>)}</ol><Pager cursor={cursors} next={data.data?.next_cursor} onNext={value => setCursors([...cursors, value])} onPrevious={() => setCursors(cursors.slice(0, -1))} /></div>;
}
function PageVisit({ view, query }: { view: PageView; query: Record<string, unknown> }) {
    const [open, setOpen] = useState(false);
    const eventQuery = { ...query, content_id: view.content_id || undefined, content_version: view.content_id && view.content_version ? view.content_version : undefined, ...(query.include_related ? { content_q: undefined } : {}) };
    return <article className="st-page-visit"><button aria-expanded={open} onClick={() => setOpen(!open)}><strong>{view.title || view.route_key}</strong><span>{view.content_version != null ? `v${view.content_version}` : '页面'} · {duration(view.visible_ms)} · {open ? '收起事件' : '查看事件'}</span></button><p>进入 {time(view.started_at)} · {view.ended_at ? `离开 ${time(view.ended_at)}` : '会话未完整结束，时长仅含已收分段'}</p><p>{view.share_id ? `分享来源 ${view.share_id}` : view.previous_page_view_id ? `由上一页 ${view.previous_page_view_id} 进入（站内访问）` : '直接访问'}</p>{view.quality_flags?.length > 0 && <p className="st-warning">{qualityText(view.quality_flags)}</p>}{open && <Events view={view} query={eventQuery} />}</article>;
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
    return <section><VisitorGovernance key={visitor.visitor_key} visitor={visitor} {...governance} onOpenEvidence={evidenceQuery ? () => { setEvidenceWindow(true); setCursors([]); setExpanded(visitor.intent.evidence?.map(item => item.session_id).filter((id): id is string => Boolean(id)) || []); } : undefined} onDeleted={() => setDeleted(true)} /><h3>{visitor.identity_state === 'anonymous' ? '匿名访客' : visitor.nickname || '访问账号'} · 访问详情</h3><p className="st-muted">{showingEvidence ? `意向来源范围 ${time(visitor.intent.window_start)} 至 ${time(visitor.intent.window_end)}（含计算时刻）；保留当前授权归属，包含该访客在此范围内的全部有权查看的素材。` : `当前列表范围 ${query.start ? time(String(query.start)) : '保留期内全部'} 至 ${query.end ? time(String(query.end)) : '当前'}（结束时刻不含）`}</p>{showingEvidence && <button onClick={() => { setEvidenceWindow(false); setCursors([]); setExpanded([]); }}>返回列表筛选范围</button>}{contentId && !showingEvidence && <label className="st-checkbox"><input type="checkbox" checked={otherPages} onChange={event => { setOtherPages(event.target.checked); setCursors([]); }} />查看这些会话中的其他页面</label>}<SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={reset} empty={result.data?.items.length === 0} />{result.data?.items.map(session => <article className="st-session" key={session.session_id}><button aria-expanded={expanded.includes(session.session_id)} onClick={() => setExpanded(expanded.includes(session.session_id) ? expanded.filter(id => id !== session.session_id) : [...expanded, session.session_id])}><strong>{time(session.started_at)}</strong><span>{session.entry_share_id ? '分享入口' : '直接入口'} · {session.page_views.length} 个页面 · {expanded.includes(session.session_id) ? '收起轨迹' : '展开轨迹'}</span></button><p className="st-muted">会话 {session.session_id} · 最近活动 {time(session.last_active_at)}</p>{expanded.includes(session.session_id) && session.page_views.map(view => <PageVisit key={view.page_view_id} view={view} query={{ ...selectedQuery, include_related: otherPages && !showingEvidence }} />)}</article>)}<Pager cursor={cursors} next={result.data?.next_cursor} onNext={value => setCursors([...cursors, value])} onPrevious={() => setCursors(cursors.slice(0, -1))} /><p className="st-muted">同账号多端按各自设备会话累计，不能解释为真人连续阅读。缺失末尾事件不补造时长。</p></section>;
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
    return <section><Feedback {...detail} retry={detail.reload} />{item && <><h3>{item.title}</h3><p>{item.type} · v{item.version} · {item.status === 'revoked' ? '已撤回，内容不可阅读' : item.status === 'draft' ? '草稿' : '已发布'}</p>{item.status !== 'revoked' && <><MediaPreview asset={item.cover} title={item.title} original /><ContentBody content={item} />{item.media?.filter(asset => asset.media_id !== item.cover?.media_id).map(asset => <MediaPreview key={asset.media_id} asset={asset} title={asset.title || item.title} original />)}</>}<p>当前范围：页面访问 {item.page_views} 次 · 可见时长 {duration(item.visible_ms)}</p>{item.read_only && <p>原页面内容快照，只读；不提供历史正文回放。</p>}{manage && !item.read_only && <button onClick={() => edit(item)}>编辑草稿 / 发布与撤回</button>}</>}<h3>访问该素材的访客</h3><Counts data={result.data} /><SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={() => { snapshot.current = { scope }; setPagination({ scope, cursors: [] }); result.reload(); }} empty={result.data?.items.length === 0} /><div className="st-cards">{result.data?.items.map(item => <VisitorCard key={item.visitor_key} visitor={item} onOpen={() => setVisitor(item)}><CRMTargetButton visitor={item} canCRM={governance.context.can_crm} owner={typeof query.owner === 'string' && query.owner ? query.owner : undefined} /></VisitorCard>)}</div><Pager cursor={cursors} next={result.data?.next_cursor} onNext={value => setCursors([...cursors, value])} onPrevious={() => setCursors(cursors.slice(0, -1))} /></section>;
}
