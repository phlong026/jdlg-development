import React, { useEffect, useState } from 'react';
import type { Visit } from '../../api/shareTracking';
import { ConvertLeadButton } from './convertLead';
import { duration, forwarderText, identityBadge, intentShortLabels, progressText, safeMediaURL, sourceLabel, visitTime } from './model';

const typeLabels: Record<string, string> = { image: '图片', video: '视频', rich_text: '图文', article: '图文' };

// 客户查看时间线的一张卡：一次访问 = 同一会话内对同一素材的浏览。
// 点头像名字或素材区打开现有「客户访问详情」，按钮之间不嵌套。
export function VisitCard({ visit, onOpen, canCRM = false, now }: { visit: Visit; onOpen: () => void; canCRM?: boolean; now?: Date }) {
    const { visitor, content, source } = visit;
    const anonymous = visitor.identity_state !== 'identified';
    const name = anonymous ? '匿名访客' : visitor.nickname || '已识别账号';
    const avatar = anonymous ? undefined : safeMediaURL(visitor.avatar_url);
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
            <span className={`st-intent st-${level}`} title="按意向规则计算，仅供跟进参考；点开详情查看依据">{intentShortLabels[level]}</span>
        </div>
        <button className="st-visit-body" onClick={onOpen} aria-label={`查看${name}看「${content.title || '历史素材'}」的记录`}>
            <span className="st-visit-thumb">{cover && !coverFailed ? <img src={cover} alt="" loading="lazy" onError={() => setCoverFailed(true)} /> : <span>{typeLabels[content.type] || '素材'}</span>}</span>
            <span className="st-visit-title">{content.title || '历史素材信息不可用'}</span>
            <span className="st-visit-source">
                <span>来源:{sourceLabel(source)}</span>
                {visit.engaged && <em className="st-visit-engaged" title="本次观看≥60秒，或本素材累计浏览≥3次，或视频看到80%以上；只反映浏览行为，不是意向等级">看得认真</em>}
            </span>
        </button>
        <p className="st-visit-metrics">
            <time dateTime={visit.started_at}>{visitTime(visit.started_at, now)}</time>
            <span>本次观看{duration(visit.visible_ms)}</span>
            {progress && <span>{progress}</span>}
            <span>累计浏览{visit.content_page_views}次</span>
        </p>
        <ConvertLeadButton visitor={visitor} canCRM={canCRM} />
    </article>;
}
