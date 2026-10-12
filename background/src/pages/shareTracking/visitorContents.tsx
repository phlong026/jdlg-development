import React, { useRef, useState } from 'react';
import { tracking, VisitorContent } from '../../api/shareTracking';
import { Feedback, MediaPreview, Pager, SnapshotInfo, useResource } from './components';
import { duration, time } from './model';

export function VisitorContentsList({ visitor, query, onOpen }: { visitor: string; query: Record<string, unknown>; onOpen: (content: VisitorContent) => void }) {
    const [cursors, setCursors] = useState<string[]>([]);
    const snapshot = useRef<{ as_of: string; first_cursor?: string }>();
    const result = useResource(async signal => {
        const data = await tracking.visitorContents(visitor, { ...query, as_of: snapshot.current?.as_of || query.as_of, cursor: cursors[cursors.length - 1] ?? snapshot.current?.first_cursor }, signal);
        if (!signal.aborted) snapshot.current = { as_of: data.as_of, first_cursor: data.first_cursor };
        return data;
    }, JSON.stringify([visitor, query, cursors]));
    return <section className="st-visitor-contents" aria-label="该访客访问素材"><SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={() => { snapshot.current = undefined; setCursors([]); result.reload(); }} empty={result.data?.items.length === 0} />{result.data?.items.map(item => <article key={`${item.content_id}:${item.content_version}`} className="st-visited-content"><MediaPreview asset={item.content_accessible ? item.cover : undefined} title={item.title || '历史素材'} original={item.content_accessible} /><div><strong>{item.title || '历史素材信息不可用'}</strong><p>访问版本 v{item.content_version} · {item.page_views} 次 · 当前范围累计可见 {duration(item.visible_ms)}</p><p className="st-muted">最近访问 {time(item.last_visit)}</p>{!item.content_accessible && <p className="st-warning">内容已撤回、过期或当前不可阅读，仅展示有权访问的历史统计。</p>}<button onClick={() => onOpen(item)}>查看该版本访问轨迹</button></div></article>)}<Pager cursor={cursors} next={result.data?.next_cursor} onNext={next => setCursors([...cursors, next])} onPrevious={() => setCursors(cursors.slice(0, -1))} /><p className="st-muted">{result.data?.omitted_page_count ? `另有 ${result.data.omitted_page_count} 次一般页面访问未生成素材卡。` : ''}仅展示具有素材标识的实际访问；一般页面轨迹请看访问详情，不把列表曝光算作素材阅读。</p></section>;
}
export function VisitorContentPanel(props: { visitor: string; query: Record<string, unknown>; onOpen: (content: VisitorContent) => void }) {
    const [open, setOpen] = useState(false);
    return <div className="st-material-toggle"><button aria-expanded={open} onClick={() => setOpen(!open)}>{open ? '收起访问素材' : '展开访问素材'}</button>{open && <VisitorContentsList key={JSON.stringify([props.visitor, props.query])} {...props} />}</div>;
}
