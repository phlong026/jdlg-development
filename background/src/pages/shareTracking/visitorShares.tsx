import React, { useRef, useState } from 'react';
import { Page, tracking, VisitorShare } from '../../api/shareTracking';
import { Feedback, Pager, SnapshotInfo, useResource } from './components';
import { channelLabel, pageLabel, time } from './model';

// 访客详情里的「转发记录」：该访客发起了哪些分享、通过什么渠道、带回来多少
// 下游流量。外层开关收起时不挂载列表、不发请求；展开后沿用列表同一套筛选
// 范围与快照口径。
export function VisitorSharesPanel({ visitor, query }: { visitor: string; query: Record<string, unknown> }) {
    const [open, setOpen] = useState(false);
    return <div className="st-material-toggle st-shares-toggle"><button aria-expanded={open} onClick={() => setOpen(!open)}>{open ? '收起转发记录' : '转发记录'}</button>{open && <VisitorSharesList key={JSON.stringify([visitor, query])} visitor={visitor} query={query} />}</div>;
}

export function VisitorSharesList({ visitor, query }: { visitor: string; query: Record<string, unknown> }) {
    const [cursors, setCursors] = useState<string[]>([]);
    const snapshot = useRef<{ as_of: string; first_cursor?: string }>();
    const result = useResource<Page<VisitorShare>>(async signal => {
        const data = await tracking.visitorShares(visitor, { ...query, as_of: snapshot.current?.as_of || query.as_of, cursor: cursors[cursors.length - 1] ?? snapshot.current?.first_cursor }, signal);
        if (!signal.aborted) snapshot.current = { as_of: data.as_of, first_cursor: data.first_cursor };
        return data;
    }, JSON.stringify([visitor, query, cursors]));
    return <section aria-label="转发记录"><SnapshotInfo data={result.data} /><Feedback {...result} retry={result.reload} resetSnapshot={() => { snapshot.current = undefined; setCursors([]); result.reload(); }} empty={result.data?.items.length === 0} />{result.data?.items.length ? <p className="st-muted">转发按客户点击分享的次数计（多次点击分别计）；微信不回传对方是否打开，带来访客只统计进入小程序的下游访问。</p> : null}<ul className="st-share-list">{result.data?.items.map(item => <li key={item.share_id}><div><strong>{item.title || pageLabel(item.route_key)}</strong>{item.status === 'revoked' && <em className="st-warning">素材已撤回</em>}{item.status === 'unavailable' && <em className="st-warning">素材暂不可访问</em>}</div><p>{time(item.occurred_at)} · {channelLabel(item.channel)} · 带来访客 {item.brought_visitors} 人 · 浏览 {item.brought_views} 次</p></li>)}</ul><Pager cursor={cursors} next={result.data?.next_cursor} onNext={next => setCursors([...cursors, next])} onPrevious={() => setCursors(cursors.slice(0, -1))} /></section>;
}
