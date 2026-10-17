import React, { useEffect, useRef, useState } from 'react';
import { Asset, Content, LatestVisit, Page, Visitor } from '../../api/shareTracking';
import { duration, intentLabels, safeMediaURL, time } from './model';

export function useResource<T>(loader: (signal: AbortSignal) => Promise<T>, key: string) {
    const [state, setState] = useState<{ data?: T; error: string; loading: boolean; key?: string }>({ error: '', loading: true });
    const [retry, setRetry] = useState(0);
    useEffect(() => {
        const controller = new AbortController(); let active = true;
        setState({ error: '', loading: true, key });
        loader(controller.signal).then(data => { if (active) setState({ data, error: '', loading: false, key }); }).catch(error => { if (active && !controller.signal.aborted) setState({ error: error instanceof Error ? error.message : '加载失败', loading: false, key }); });
        return () => { active = false; controller.abort(); };
    }, [key, retry]);
    return { ...(state.key === key ? state : { error: '', loading: true }), reload: () => setRetry(value => value + 1) };
}
export function Feedback({ loading, error, retry, empty, resetSnapshot }: { loading?: boolean; error?: string; retry?: () => void; empty?: boolean; resetSnapshot?: () => void }) {
    if (loading) return <div className="st-feedback" role="status">正在读取访问记录…</div>;
    if (error) return <div className="st-feedback st-error" role="alert"><p>{error}</p>{resetSnapshot && error.includes('刷新') && /快照|游标|资料|搜索/.test(error) ? <button onClick={resetSnapshot}>刷新结果，保留筛选</button> : <button onClick={retry}>重试，保留筛选</button>}</div>;
    if (empty) return <div className="st-feedback">尚无符合条件的访问记录</div>;
    return null;
}
export function Pager({ next, cursor, onNext, onPrevious }: { next?: string; cursor: string[]; onNext: (value: string) => void; onPrevious: () => void }) {
    return <nav className="st-pager" aria-label="记录分页"><button disabled={!cursor.length} onClick={onPrevious}>上一页</button><span>第 {cursor.length + 1} 页</span><button disabled={!next} onClick={() => next && onNext(next)}>下一页</button></nav>;
}
export function SnapshotInfo({ data }: { data?: { as_of: string; metric_version: string } }) {
    return data ? <p className="st-muted">数据统计到 {time(data.as_of)}，晚到的记录可能让数字略微增加</p> : null;
}
export function Counts({ data }: { data?: Page<unknown> }) { return data ? <div className="st-counts"><strong>已识别账号 {data.identified_count} 位</strong><strong>匿名访客 {data.anonymous_count} 个</strong></div> : null; }
export function MediaPreview({ asset, title, original = false }: { asset?: Asset; title: string; original?: boolean }) {
    const [failed, setFailed] = useState(false); const [attempt, setAttempt] = useState(0);
    useEffect(() => { setFailed(false); setAttempt(0); }, [asset?.url]);
    const url = safeMediaURL(asset?.url);
    if (!url || failed) return <div className="st-media st-media-failed"><span>{failed ? '图片或媒体加载失败' : '暂无可用预览'}</span>{failed && <button onClick={() => { setFailed(false); setAttempt(attempt + 1); }}>重试预览</button>}</div>;
    return <div className="st-media">{asset?.kind === 'video' ? <video key={attempt} src={url} controls preload="metadata" onError={() => setFailed(true)} aria-label={title} /> : <img key={attempt} src={url} alt={title} loading="lazy" onError={() => setFailed(true)} />}{original && <a href={url} target="_blank" rel="noopener noreferrer">查看原图</a>}</div>;
}
export function VisitDurations({ latest, cumulative }: { latest?: LatestVisit; cumulative: number }) {
    return <dl className="st-duration-pair"><div><dt>最近一次停留</dt><dd>{latest ? duration(latest.visible_ms) : '单次记录未提供'}</dd>{latest && <small>进入 {time(latest.started_at)}</small>}</div><div><dt>这段时间累计停留</dt><dd>{duration(cumulative)}</dd></div></dl>;
}
export function VisitorCard({ visitor, onOpen, onOpenRecords = onOpen, children }: { visitor: Visitor; onOpen: () => void; onOpenRecords?: () => void; children?: React.ReactNode }) {
    const [evidence, setEvidence] = useState(false); const [avatarFailed, setAvatarFailed] = useState(false); const avatar = visitor.identity_state === 'identified' ? safeMediaURL(visitor.avatar_url) : undefined;
    useEffect(() => { setAvatarFailed(false); }, [avatar]);
    const name = visitor.identity_state === 'anonymous' ? `匿名访客 · 尾号${visitor.visitor_key.slice(-6)}` : visitor.identity_state === 'deleted' ? '已删除账号' : visitor.nickname || '未设置昵称';
    return <article className="st-visitor"><button className="st-card-main" onClick={onOpen}><span className="st-avatar">{avatar && !avatarFailed ? <img src={avatar} alt="" onError={() => setAvatarFailed(true)} /> : '访'}</span><span><strong>{name}</strong><small>{visitor.identity_state === 'identified' ? '已登录账号 · ' : ''}{visitor.crm_label || '未关联客户'}</small></span><span className="st-chevron" aria-hidden="true">›</span></button><span className={`st-intent st-${visitor.intent?.level || 'unknown'}`}>{intentLabels[visitor.intent?.level || 'unknown']}</span>{visitor.risk_tags?.length ? <span className="st-risk-tags" title="来自风控台的风险标签">{visitor.risk_tags.map(tag => <em key={tag}>{tag}</em>)}</span> : null}<dl className="st-metrics"><div><dt>看了几个页面</dt><dd>{visitor.page_views} 次</dd></div><div><dt>来过几次</dt><dd>{visitor.session_count} 次</dd></div></dl><VisitDurations latest={visitor.latest_visit} cumulative={visitor.visible_ms} /><p className="st-muted">最近访问 {time(visitor.last_visit)}</p>{children}</article>;
}
export function Dialog({ title, children, close, busy = false }: { title: string; children: React.ReactNode; close: () => void; busy?: boolean }) {
    const ref = useRef<HTMLDivElement>(null); const closeRef = useRef(close); closeRef.current = close;
    useEffect(() => {
        const previous = document.activeElement as HTMLElement; const root = ref.current; root?.focus();
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape') { event.stopPropagation(); closeRef.current(); }
            if (event.key === 'Tab' && root) {
                const items = [...root.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [tabindex="0"]')].filter(item => item.getClientRects().length);
                const first = items[0], last = items[items.length - 1];
                if (!first) { event.preventDefault(); root.focus(); }
                else if (event.shiftKey && (document.activeElement === first || document.activeElement === root)) { event.preventDefault(); last.focus(); }
                else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
            }
        };
        root?.addEventListener('keydown', onKey); const before = document.body.style.overflow; document.body.style.overflow = 'hidden';
        return () => { root?.removeEventListener('keydown', onKey); document.body.style.overflow = before; previous?.focus(); };
    }, []);
    return <div className="st-overlay"><div className="st-dialog" role="dialog" aria-modal="true" aria-label={title} tabIndex={-1} ref={ref}><header><h2>{title}</h2><button onClick={close} disabled={busy} aria-label="关闭详情">{busy ? '处理中…' : '关闭'}</button></header>{children}</div></div>;
}

// Only use persisted server responses here. Unsaved editor input must never reach HTML rendering.
export function ContentBody({ content }: { content: Pick<Content, 'body' | 'body_format'> }) {
    return content.body_format === 'rich_text'
        ? <div className="st-body st-rich-body" dangerouslySetInnerHTML={{ __html: content.body }} />
        : <div className="st-body">{content.body}</div>;
}
