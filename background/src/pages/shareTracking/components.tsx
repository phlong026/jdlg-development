import { visitorName } from './model';
import React, { useEffect, useRef, useState } from 'react';
import { ArticleBlock, ArticleDocument, Asset, Content, LatestVisit, Page, Visitor } from '../../api/shareTracking';
import { duration, identityBadge, intentLabels, safeMediaURL, time } from './model';

export function useResource<T>(loader: (signal: AbortSignal) => Promise<T>, key: string) {
    const [state, setState] = useState<{ data?: T; error: string; loading: boolean; key?: string; status?: number }>({ error: '', loading: true });
    const [retry, setRetry] = useState(0);
    useEffect(() => {
        const controller = new AbortController(); let active = true;
        setState({ error: '', loading: true, key });
        loader(controller.signal).then(data => { if (active) setState({ data, error: '', loading: false, key }); }).catch(error => {
            if (active && !controller.signal.aborted) {
                const status = typeof (error as { status?: unknown })?.status === 'number' ? (error as { status: number }).status : undefined;
                setState({ error: error instanceof Error ? error.message : '加载失败', loading: false, key, status });
            }
        });
        return () => { active = false; controller.abort(); };
    }, [key, retry]);
    return { ...(state.key === key ? state : { error: '', loading: true }), reload: () => setRetry(value => value + 1) };
}
export function Feedback({ loading, error, status, retry, empty, resetSnapshot, rangeRecovery, rangeLimitHint }: { loading?: boolean; error?: string; status?: number; retry?: () => void; empty?: boolean; resetSnapshot?: () => void; rangeRecovery?: { label: string; onClick: () => void }; rangeLimitHint?: string }) {
    if (loading) return <div className="st-feedback" role="status">正在读取访问记录…</div>;
    if (error) {
        if (status === 422) return <div className="st-feedback st-error" role="alert"><p>{error}</p>{rangeRecovery ? <button onClick={rangeRecovery.onClick}>{rangeRecovery.label}</button> : rangeLimitHint ? <p className="st-muted">{rangeLimitHint}</p> : null}</div>;
        return <div className="st-feedback st-error" role="alert"><p>{error}</p>{resetSnapshot && error.includes('刷新') && /快照|游标|资料|搜索/.test(error) ? <button onClick={resetSnapshot}>刷新结果，保留筛选</button> : <button onClick={retry}>重试，保留筛选</button>}</div>;
    }
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
    const name = visitorName(visitor);
    const identity = identityBadge(visitor.identity_state, visitor.identity_type);
    return <article className="st-visitor"><button className="st-card-main" onClick={onOpen}><span className="st-avatar">{avatar && !avatarFailed ? <img src={avatar} alt="" onError={() => setAvatarFailed(true)} /> : '访'}</span><span><strong>{name}</strong><small>{visitor.identity_state === 'identified' ? '已登录账号 · ' : ''}{visitor.crm_label || '未关联客户'}</small>{identity && <span className={`st-identity st-identity-${identity.tone}`}>{identity.label}</span>}</span><span className="st-chevron" aria-hidden="true">›</span></button><span className={`st-intent st-${visitor.intent?.level || 'unknown'}`}>{intentLabels[visitor.intent?.level || 'unknown']}</span>{visitor.risk_tags?.length ? <span className="st-risk-tags" title="来自风控台的风险标签">{visitor.risk_tags.map(tag => <em key={tag}>{tag}</em>)}</span> : null}<dl className="st-metrics st-metrics-three"><div><dt>看了几个页面</dt><dd>{visitor.page_views} 次</dd></div><div><dt>来过几次</dt><dd>{visitor.session_count} 次</dd></div><div title="客户在小程序里主动点分享的次数；多次点击分别计，不含后台生成的码/链接"><dt>发起转发</dt><dd>{visitor.share_count} 次</dd></div></dl>{visitor.recent_page_views !== undefined && <p className="st-recent" title="固定最近7天小计，不随上方时间范围变化">近7天：看了 {visitor.recent_page_views} 个页面 · 转发 {visitor.recent_share_count ?? 0} 次</p>}<VisitDurations latest={visitor.latest_visit} cumulative={visitor.visible_ms} /><p className="st-muted">最近访问 {time(visitor.last_visit)}</p>{children}</article>;
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

export function ArticleBlockEditor({ body, media, onChange }: { body: string; media: Asset[]; onChange: (body: string) => void }) {
    let document: ArticleDocument;
    try { document = body ? JSON.parse(body) : { schema: 1, blocks: [] }; }
    catch { return <p className="st-warning">正文格式无法读取，可在高级源码中修正。</p>; }
    if (document.schema !== 1 || !Array.isArray(document.blocks) || document.layout_html) return <p className="st-muted">原排版正文保持只读。</p>;
    function write(blocks: ArticleBlock[]) {
        const next = { ...document, blocks };
        const mediaIndexes = blocks.map((block, index) => ['image', 'video'].includes(block.kind) ? index : -1).filter(index => index >= 0);
        const mediaAreContiguous = mediaIndexes.every((index, position) => position === 0 || index === mediaIndexes[position - 1] + 1);
        const images = blocks.filter(block => block.kind === 'image').length;
        const videos = blocks.filter(block => block.kind === 'video').length;
        const onlySocialBlocks = blocks.every(block => ['image', 'video', 'paragraph'].includes(block.kind));
        if (next.presentation === 'gallery' && (!onlySocialBlocks || images < 1 || videos > 0 || !mediaAreContiguous)
            || next.presentation === 'video' && (!onlySocialBlocks || videos !== 1 || images > 0)) next.presentation = 'article';
        onChange(JSON.stringify(next));
    }
    function patch(index: number, changes: Partial<ArticleBlock>) { write(document.blocks.map((block, position) => position === index ? { ...block, ...changes } : block)); }
    function move(index: number, step: number) {
        const blocks = document.blocks.slice(), target = index + step;
        if (target < 0 || target >= blocks.length) return;
        [blocks[index], blocks[target]] = [blocks[target], blocks[index]]; write(blocks);
    }
    const labels = { paragraph: '文字', heading: '小标题', quote: '引用', list: '列表', image: '图片', video: '视频' };
    return <div className="st-block-editor">{document.blocks.map((block, index) => <section className="st-edit-block" key={index}>
        <header><strong>{labels[block.kind]} {index + 1}</strong><div>
            <button type="button" disabled={index === 0} onClick={() => move(index, -1)} aria-label={`上移第${index + 1}段`}>上移</button>
            <button type="button" disabled={index === document.blocks.length - 1} onClick={() => move(index, 1)} aria-label={`下移第${index + 1}段`}>下移</button>
            <button type="button" onClick={() => write(document.blocks.filter((_, position) => position !== index))}>移除</button>
        </div></header>
        {block.kind === 'image' || block.kind === 'video' ? <><label>选择{labels[block.kind]}<select value={block.media_id || ''} onChange={event => patch(index, { media_id: event.target.value })}><option value="">请选择已上传素材</option>{media.filter(asset => asset.kind === block.kind).map(asset => <option key={asset.media_id} value={asset.media_id}>{asset.title || labels[block.kind]}</option>)}</select></label><label>说明（选填）<input value={block.caption || ''} maxLength={100} onChange={event => patch(index, { caption: event.target.value })} /></label></>
            : block.kind === 'list' ? <label>每行一项<textarea rows={4} value={(block.items || []).join('\n')} onChange={event => patch(index, { items: event.target.value.split('\n') })} /></label>
            : <textarea rows={block.kind === 'paragraph' ? 4 : 2} value={block.text || ''} placeholder={`填写${labels[block.kind]}`} onChange={event => patch(index, { text: event.target.value })} />}
    </section>)}<div className="st-add-blocks">
        <button type="button" onClick={() => write([...document.blocks, { kind: 'paragraph', text: '' }])}>＋ 文字</button>
        <button type="button" onClick={() => write([...document.blocks, { kind: 'heading', level: 2, text: '' }])}>＋ 小标题</button>
        {(['image', 'video'] as const).map(kind => <button type="button" key={kind} disabled={!media.some(asset => asset.kind === kind)} onClick={() => write([...document.blocks, { kind, media_id: media.find(asset => asset.kind === kind)?.media_id }])}>＋ {labels[kind]}</button>)}
    </div></div>;
}

export function articleReferencesMedia(content: Pick<Content, 'body' | 'body_format'>, mediaID?: string): boolean {
    if (!mediaID || content.body_format !== 'article_v2') return false;
    try {
        const document = JSON.parse(content.body) as ArticleDocument;
        const escapedID = mediaID.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        return document.blocks?.some(block => block.media_id === mediaID) === true
            || new RegExp(`data-media-id=["']${escapedID}["']`).test(document.layout_html || '');
    } catch { return false; }
}

// Only use persisted server responses here. Unsaved editor input must never reach HTML rendering.
export function ContentBody({ content }: { content: Pick<Content, 'body' | 'body_format'> & Partial<Pick<Content, 'media' | 'presentation'>> }) {
    if (content.body_format === 'rich_text') return <div className="st-body st-rich-body" dangerouslySetInnerHTML={{ __html: content.body }} />;
    if (content.body_format !== 'article_v2') return <div className="st-body">{content.body}</div>;
    let document: ArticleDocument;
    try { document = JSON.parse(content.body) as ArticleDocument; } catch { return <div className="st-warning">原排版正文无法预览，请重新保存。</div>; }
    if (document.schema !== 1 || !Array.isArray(document.blocks) || !document.blocks.length) return <div className="st-warning">原排版正文无法预览，请重新保存。</div>;
	if (document.layout_html) {
		const layout = renderAdminArticleLayout(document.layout_html, content.media || []);
		return layout ? <div className="st-body st-original-layout" dangerouslySetInnerHTML={{ __html: layout }} /> : <div className="st-warning">原排版正文或媒体无法安全预览，请重新导入。</div>;
	}
    const media = new Map((content.media || []).map(asset => [asset.media_id, asset]));
    const presentation = document.presentation || content.presentation;
    if (presentation === 'gallery' || presentation === 'video') {
        const blocks = document.blocks.filter(block => block.kind === (presentation === 'gallery' ? 'image' : 'video'));
        const text = document.blocks.filter(block => block.kind === 'paragraph').map(block => block.text || '').join('\n\n');
        return <div className={`st-body st-social-body st-social-${presentation}`}><div className={presentation === 'gallery' ? 'st-gallery-preview' : 'st-video-preview'}>{blocks.map((block, index) => <figure className="st-gallery-slide" key={index}><MediaPreview asset={media.get(block.media_id || '')} title={block.caption || '内容素材'} />{block.caption && <figcaption>{block.caption}</figcaption>}</figure>)}</div>{presentation === 'gallery' && <p className="st-muted">{blocks.length}张图片 · 左右滑动查看</p>}<div className="st-note-copy">{text}</div></div>;
    }
    const align = { left: 'left', center: 'center', right: 'right' } as const;
    const size = { small: 'small', normal: 'normal', large: 'large' } as const;
    const border = { left: 'left', box: 'box' } as const;
    return <div className="st-body st-article-body">{document.blocks.map((block, index) => {
        const classes = ['st-article-block'];
        if (block.kind === 'heading') classes.push('st-article-heading', `st-article-heading-${block.level}`);
        else classes.push(`st-article-${block.kind}`);
        if (block.style?.align && align[block.style.align]) classes.push(`st-article-align-${align[block.style.align]}`);
        if (block.style?.font_size && size[block.style.font_size]) classes.push(`st-article-size-${size[block.style.font_size]}`);
        if (block.style?.border && block.style.border !== 'none' && border[block.style.border]) classes.push(`st-article-border-${border[block.style.border]}`);
        const style = { marginTop: block.style?.spacing_before, marginBottom: block.style?.spacing_after };
        if (block.kind === 'image' || block.kind === 'video') {
            const asset = block.media_id ? media.get(block.media_id) : undefined;
            return <div className={classes.join(' ')} style={style} key={index}><MediaPreview asset={asset} title={block.caption || asset?.title || '文章媒体'} />{block.caption && <p className="st-article-caption">{block.caption}</p>}</div>;
        }
        if (block.kind === 'list') return <div className={classes.join(' ')} style={style} key={index}>{block.ordered ? <ol>{block.items?.map((item, itemIndex) => <li key={itemIndex}>{item}</li>)}</ol> : <ul>{block.items?.map((item, itemIndex) => <li key={itemIndex}>{item}</li>)}</ul>}</div>;
        return <div className={classes.join(' ')} style={style} key={index}>{block.text}</div>;
    })}</div>;
}

const layoutTag = /^<\/?(?:section|div|p|span|strong|b|em|i|u|s|br|h[1-6]|blockquote|ul|ol|li|figure|figcaption|table|thead|tbody|tfoot|tr|td|th|hr|code|pre)(?: (?:style="[^"]*"|colspan="\d+"|rowspan="\d+"))*?>$/;
const layoutImageTag = /^<img data-media-id="[A-Za-z0-9_-]{8,128}"(?: alt="[^"]*")?(?: style="[^"]*")?>$/;
const layoutVideoTag = /^(?:<jd-video data-media-id="[A-Za-z0-9_-]{8,128}"(?: style="[^"]*")?>|<\/jd-video>)$/;
function escapeLayoutAttribute(value: string): string { return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
export function renderAdminArticleLayout(layout: string, media: Asset[]): string | undefined {
	if (/(?:\b(?:src|href|on[a-z]+)=|<\/?(?:script|style|iframe|object|embed|svg|audio|link|meta)\b|url\s*\(|expression\s*\(|@import|\\|(?:position|z-index|transform|filter|visibility|opacity|content|behavior)\s*:)/i.test(layout)) return;
	for (const token of layout.match(/<[^>]*>/g) || []) if (!layoutTag.test(token) && !layoutImageTag.test(token) && !layoutVideoTag.test(token)) return;
	const assets = new Map(media.map(asset => [asset.media_id, asset]));
	let safe = layout.replace(/<img data-media-id="([A-Za-z0-9_-]{8,128})"((?: alt="[^"]*")?(?: style="[^"]*")?)>/g, (_, id: string, attrs: string) => {
		const asset = assets.get(id), url = safeMediaURL(asset?.url);
		return asset?.kind === 'image' && url ? `<img src="${escapeLayoutAttribute(url)}"${attrs}>` : '<invalid-media>';
	});
	safe = safe.replace(/<jd-video data-media-id="([A-Za-z0-9_-]{8,128})"((?: style="[^"]*")?)><\/jd-video>/g, (_, id: string, attrs: string) => {
		const asset = assets.get(id), url = safeMediaURL(asset?.url);
		return asset?.kind === 'video' && url ? `<video src="${escapeLayoutAttribute(url)}" controls playsinline preload="metadata"${attrs}></video>` : '<invalid-media>';
	});
	if (safe.includes('<invalid-media>') || /data-media-id|jd-video/.test(safe)) return;
	return safe;
}
