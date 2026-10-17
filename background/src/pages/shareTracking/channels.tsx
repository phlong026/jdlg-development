import React from 'react';
import { ChannelSummary, tracking } from '../../api/shareTracking';
import { Feedback, SnapshotInfo, useResource } from './components';
import { identityBadge } from './model';

// 渠道视角：把分享者（设计师/员工/活跃客户）当作分发节点排名——谁带来的
// 访客最多、其中多少高意向、多少已转成线索。设计师在这里是主角而非
// 干扰项：转发统计与转化统计共享同一套分享归因口径。
export function ChannelLeaderboard({ query }: { query: Record<string, unknown> }) {
    const result = useResource<ChannelSummary[]>(async signal => {
        const page = await tracking.channels({ ...query, intent: undefined, q: undefined, identity: undefined }, signal);
        return page.items || [];
    }, JSON.stringify(query));
    return <section aria-label="渠道视角">
        <SnapshotInfo data={result.data ? { as_of: query.as_of as string, metric_version: '' } : undefined} />
        <Feedback {...result} retry={result.reload} empty={(result.data?.length ?? 1) === 0} />
        <p className="st-muted">按筛选时间范围内的「客户主动转发」聚合（后台生成的码/链接不计）；带来访客按进入小程序的下游访问去重。设计师与员工是渠道节点，不进入意向-线索门控。</p>
        {result.data && result.data.length > 0 ? <div className="st-channel-list" role="table" aria-label="渠道排行">
            <div className="st-channel-head" role="row"><span>分享者</span><span>发起分享</span><span>带来访客</span><span>其中高意向</span><span>已转线索</span></div>
            {result.data.map((item, index) => {
                const identity = identityBadge(item.visitor_key.startsWith('u:') ? 'identified' : 'anonymous', item.identity_type);
                return <div className="st-channel-row" role="row" key={item.visitor_key}>
                    <span className="st-channel-name">
                        <em>{index + 1}</em>
                        <strong>{item.nickname || (item.visitor_key.startsWith('u:') ? '已识别账号' : '匿名访客 · 尾号' + item.visitor_key.slice(-6))}</strong>
                        {identity && <span className={`st-identity st-identity-${identity.tone}`}>{identity.label}</span>}
                    </span>
                    <span>{item.share_count} 次</span>
                    <span>{item.brought_visitors} 人<small>（已识别 {item.brought_identified}）</small></span>
                    <span>{item.brought_high > 0 ? <strong className="st-channel-high">{item.brought_high}</strong> : '0'}</span>
                    <span>{item.converted_leads > 0 ? <strong>{item.converted_leads}</strong> : '0'}</span>
                </div>;
            })}
        </div> : null}
    </section>;
}
