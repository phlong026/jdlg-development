import { visitorName } from './model';
import React from 'react';
import { ChannelSummary, Page, tracking } from '../../api/shareTracking';
import { Feedback, SnapshotInfo, useResource } from './components';
import { identityBadge } from './model';

// 渠道视角：把分享者（设计师/员工/活跃客户）当作分发节点排名——谁带来的
// 访客最多、其中多少高意向、多少已转成线索。设计师在这里是主角而非
// 干扰项：转发统计与转化统计共享同一套分享归因口径。
export function ChannelLeaderboard({ query, refreshKey }: { query: Record<string, unknown>; refreshKey: number }) {
    const result = useResource<Page<ChannelSummary>>(signal => tracking.channels({ ...query, intent: undefined, q: undefined, identity: undefined }, signal), JSON.stringify([query, refreshKey]));
    return <section aria-label="渠道视角">
        <SnapshotInfo data={result.data} />
        <Feedback {...result} retry={result.reload} empty={result.data?.items.length === 0} />
        <p className="st-muted">按筛选时间范围内的「客户主动转发」聚合（后台生成的码/链接不计）；带来访客按进入小程序的下游访问去重。“历史曾有线索”只表示该账号过去存在过线索记录，不代表由本次分享新增或已经成交。</p>
        {result.data && result.data.items.length > 0 ? <div className="st-channel-list" role="table" aria-label="渠道排行">
            <div className="st-channel-head" role="row"><span>分享者</span><span>发起分享</span><span>带来访客</span><span>其中高意向</span><span>历史曾有线索</span></div>
            {result.data.items.map((item, index) => {
                const identity = identityBadge(item.visitor_key.startsWith('u:') ? 'identified' : 'anonymous', item.identity_type);
                return <div className="st-channel-row" role="row" key={item.visitor_key}>
                    <span className="st-channel-name">
                        <em>{index + 1}</em>
                        <strong>{visitorName(item)}</strong>
                        {identity && <span className={`st-identity st-identity-${identity.tone}`}>{identity.label}</span>}
                    </span>
                    <span>{item.share_count} 次</span>
                    <span>{item.brought_visitors} 人<small>（已识别 {item.brought_identified}）</small></span>
                    <span>{item.brought_high > 0 ? <strong className="st-channel-high">{item.brought_high}</strong> : '0'}</span>
                    <span>{item.lead_history_available === false || item.converted_leads == null
                        ? '暂不可用'
                        : item.converted_leads > 0 ? <strong>{item.converted_leads}</strong> : '0'}</span>
                </div>;
            })}
        </div> : null}
    </section>;
}
