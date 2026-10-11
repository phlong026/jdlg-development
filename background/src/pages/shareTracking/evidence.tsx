import React from 'react';
import { IntentEvidence } from '../../api/shareTracking';
import { intentExclusionText, intentLabels, intentMetricText, time } from './model';

const actionLabels: Record<string, string> = { favorite: '收藏成功', consultation: '咨询成功', appointment: '预约成功', quote: '报价成功' };
export function IntentEvidenceDetails({ evidence, onOpenRecords, recordsLabel = '查看有权访问的客户会话与页面记录' }: { evidence: IntentEvidence; onOpenRecords?: () => void; recordsLabel?: string }) {
    const threshold = evidence.thresholds;
    return <div className="st-intent-evidence"><p>{intentLabels[evidence.level] || '待判断'} · 规则 {evidence.rule_version || '未提供'}</p>
        <p>评估窗口 {time(evidence.window_start)} 至 {time(evidence.window_end)} · {evidence.timezone || '时区未提供'}</p>
        <p>计算时间 {time(evidence.calculated_at)} · 人工覆盖状态：{{ none: '无人工覆盖', active: '有效人工覆盖', expired: '人工覆盖已过期' }[evidence.override_state || ''] || '接口未提供'}</p>
        {evidence.scope_required && <p className="st-warning">当前包含规则不同的多个归属，暂不合并判断。请选择单一跟进归属后查看。</p>}
        <ul>{evidence.reasons?.map((reason, i) => <li key={i}>{reason}</li>)}</ul>
        {evidence.metrics && <p>计入本次评估：有效会话 {evidence.metrics.sessions} 次、已确认收藏 {evidence.metrics.favorites} 次、已确认业务结果 {evidence.metrics.business_results} 次。</p>}
        {evidence.participating_metrics && <p>参与指标：{evidence.participating_metrics.map(intentMetricText).join('、') || '无'}</p>}
        {threshold && <p>阈值：近 {threshold.window_days} 天；中意向 ≥ {threshold.medium_sessions} 次会话且 ≥ {threshold.medium_favorites} 次已确认收藏；高意向 ≥ {threshold.high_business_results} 次已确认业务结果。每账号每日会话上限 {threshold.daily_session_cap}，每类行为上限 {threshold.daily_action_cap}。</p>}
        {evidence.exclusions && <p>排除项：{evidence.exclusions.map(intentExclusionText).join('、') || '未提供'}</p>}
        {evidence.evidence && <details className="st-definitions"><summary>本次评估的来源记录（{evidence.evidence.length}{evidence.evidence_truncated ? '+' : ''}）</summary>{evidence.evidence.length ? <ol>{evidence.evidence.map((record, index) => <li key={`${record.kind}:${record.record_id}:${index}`}>
            {record.kind === 'business' ? actionLabels[record.action || ''] || `服务端业务结果：${record.action || '未提供类型'}` : '有效页面访问'} · {time(record.occurred_at)}
            <p>记录 {record.record_id}{record.session_id ? ` · 会话 ${record.session_id}` : ''}{record.page_view_id ? ` · 页面访问 ${record.page_view_id}` : ''}{record.content_id ? ` · 素材 ${record.content_id}` : ''}{record.content_version ? ` · v${record.content_version}` : ''}</p>
        </li>)}</ol> : <p>本次未返回可展示的来源记录。</p>}{evidence.evidence_truncated && <p>来源记录较多，此处仅展示服务端返回的部分证据，不代表全部。</p>}{onOpenRecords && <button onClick={onOpenRecords}>{recordsLabel}</button>}</details>}
        <p>仅用于跟进优先级参考，不代表真实需求或成交概率。客户点击与已确认业务成功分别统计。</p>
    </div>;
}
