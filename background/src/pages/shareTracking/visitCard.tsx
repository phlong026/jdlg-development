import { visitorName } from './model';
import React, { useEffect, useState } from 'react';
import type { Visit, VisitMaterial } from '../../api/shareTracking';
import { ConvertLeadButton } from './convertLead';
import { duration, forwarderText, identityBadge, intentShortLabels, progressText, safeMediaURL, sourceLabel, visitRange } from './model';

const typeLabels: Record<string, string> = { image: '图片', video: '视频', rich_text: '图文', article: '图文', page: '页面' };

function MaterialThumb({ material }: { material: VisitMaterial }) {
    const cover = material.content.content_accessible ? safeMediaURL(material.content.cover?.url) : undefined;
    const [failed, setFailed] = useState(false);
    useEffect(() => { setFailed(false); }, [cover]);
    return <span className="st-visit-material-thumb">{cover && !failed ? <img src={cover} alt="" loading="lazy" onError={() => setFailed(true)} /> : <span>{typeLabels[material.content.type] || '素材'}</span>}</span>;
}

// 客户查看时间线的一张卡：默认是同一访客 30 分钟内、跨素材/分享入口的连续访问；
// 按天视图是同一访客一个自然日的汇总。点头像名字或素材区打开客户访问详情。
export function VisitCard({ visit, onOpen, canCRM = false, now }: { visit: Visit; onOpen: () => void; canCRM?: boolean; now?: Date }) {
    const { visitor, content, source } = visit;
    const anonymous = visitor.identity_state !== 'identified';
    const name = anonymous ? '匿名访客' : visitorName(visitor);
    const avatar = anonymous ? undefined : safeMediaURL(visitor.avatar_url);
    const materials: VisitMaterial[] = visit.materials?.length ? visit.materials : [{
        content,
        started_at: visit.started_at,
        ended_at: visit.ended_at || visit.started_at,
        visible_ms: visit.visible_ms,
        page_views: visit.content_page_views,
        progress: visit.progress,
        engaged: visit.engaged,
    }];
    const multi = materials.length > 1;
    const materialLabel = materials.some(item => item.content.type === 'page') ? '个页面或素材' : '个素材';
    const cover = content.content_accessible ? safeMediaURL(content.cover?.url) : undefined;
    const [avatarFailed, setAvatarFailed] = useState(false);
    const [coverFailed, setCoverFailed] = useState(false);
    const [copy, setCopy] = useState<'' | 'done' | 'failed'>('');
    useEffect(() => { setAvatarFailed(false); }, [avatar]);
    useEffect(() => { setCoverFailed(false); }, [cover]);
    const identity = identityBadge(visitor.identity_state, visitor.identity_type);
    const level = visitor.intent?.level || 'unknown';
    const forwarder = forwarderText(source);
    const progress = progressText(visit.progress);
    const range = visitRange(visit.started_at, visit.ended_at, now);
    useEffect(() => {
        if (!copy) return;
        const timer = setTimeout(() => setCopy(''), 2000);
        return () => clearTimeout(timer);
    }, [copy]);
    const copyName = () => {
        const done = navigator.clipboard?.writeText(name);
        if (!done) { setCopy('failed'); return; }
        done.then(() => setCopy('done'), () => setCopy('failed'));
    };
    return <article className="st-visit">
        <div className="st-visit-head">
            <button className="st-visit-person" onClick={onOpen} aria-label={`查看${name}的访问详情`}>
                <span className="st-avatar">{avatar && !avatarFailed ? <img src={avatar} alt="" onError={() => setAvatarFailed(true)} /> : '访'}</span>
                <strong>{name}</strong>
            </button>
            {!anonymous && <button className="st-visit-copy" aria-label="复制昵称" title={copy === 'done' ? '已复制' : copy === 'failed' ? '复制失败，请手动选择' : '复制昵称'} onClick={copyName}>{copy === 'done' ? '已复制' : '⧉'}</button>}
            {!anonymous && <span className="st-sr-only" role="status" aria-live="polite">{copy === 'done' ? '昵称已复制' : copy === 'failed' ? '复制失败，请手动选择' : ''}</span>}
            {forwarder && <span className="st-visit-forward">{forwarder}</span>}
            {identity && <span className={`st-identity st-identity-${identity.tone}`}>{identity.label}</span>}
            {visitor.risk_tags?.map(tag => <em key={tag} className="st-visit-risk" title="来自风控台的风险标签">{tag}</em>)}
            {visit.group === 'day' && <span className="st-visit-group" title="按北京时间自然日汇总">按天汇总</span>}
            <span className={`st-intent st-${level}`} title="按意向规则计算，仅供跟进参考；点开详情查看依据">{intentShortLabels[level]}</span>
        </div>
        {multi ? (
            <button className="st-visit-body st-visit-body-multi" onClick={onOpen} aria-label={`查看${name}本次访问的 ${materials.length} ${materialLabel}`}>
                <span className="st-visit-batch-head">
                    <span>来源:{sourceLabel(source)}</span>
                    <span>{materials.length} {materialLabel}</span>
                    {visit.visit_count && visit.visit_count > 1 && <span>{visit.visit_count} 次访问</span>}
                    {visit.engaged && <em className="st-visit-engaged" title="本次观看≥60秒，或本素材累计浏览≥3次，或视频看到80%以上；只反映浏览行为，不是意向等级">看得认真</em>}
                </span>
                <span className="st-visit-materials">
                    {materials.map(material => <span className="st-visit-material" key={`${material.content.route_key}:${material.content.content_id}:${material.content.content_version}`}>
                        <MaterialThumb material={material} />
                        <span className="st-visit-material-info">
                            <strong>{material.content.title || '历史素材信息不可用'}</strong>
                            <small>{typeLabels[material.content.type] || '素材'} · 本次{duration(material.visible_ms)} · 累计{material.page_views}次</small>
                        </span>
                        {progressText(material.progress) && <small className="st-visit-material-progress">{progressText(material.progress)}</small>}
                        {material.engaged && <em className="st-visit-engaged">看得认真</em>}
                    </span>)}
                </span>
            </button>
        ) : (
            <button className="st-visit-body" onClick={onOpen} aria-label={`查看${name}看「${content.title || '历史素材'}」的记录`}>
                <span className="st-visit-thumb">{cover && !coverFailed ? <img src={cover} alt="" loading="lazy" onError={() => setCoverFailed(true)} /> : <span>{typeLabels[content.type] || '素材'}</span>}</span>
                <span className="st-visit-title">{content.title || '历史素材信息不可用'}</span>
                <span className="st-visit-source">
                    <span>来源:{sourceLabel(source)}</span>
                    {visit.engaged && <em className="st-visit-engaged" title="本次观看≥60秒，或本素材累计浏览≥3次，或视频看到80%以上；只反映浏览行为，不是意向等级">看得认真</em>}
                </span>
            </button>
        )}
        <p className="st-visit-metrics">
            <time dateTime={visit.started_at}>{range}</time>
            <span>{multi ? '本次共' : content.type === 'page' ? '本次停留' : '本次观看'}{duration(visit.visible_ms)}</span>
            {!multi && visit.visit_count && visit.visit_count > 1 && <span>{visit.visit_count} 次访问</span>}
            {!multi && progress && <span>{progress}</span>}
            {visit.progress_omitted && <span>进度因数据量过大未加载</span>}
            <span>累计浏览{visit.content_page_views}次</span>
        </p>
        <ConvertLeadButton visitor={visitor} canCRM={canCRM} />
    </article>;
}
