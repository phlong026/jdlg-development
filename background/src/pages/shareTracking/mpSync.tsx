import React, { useEffect, useRef, useState } from 'react';
import { MPSyncCounts, MPSyncInfo, MPSyncRun, tracking } from '../../api/shareTracking';
import { time } from './model';

export const mpProblemText: Record<string, string> = {
    deleted_upstream: '公众号已删除该文章；不会自动撤回本地素材，请人工决定',
    title_empty: '标题为空',
    empty_body: '正文清理后为空（可能只有图片或视频）',
    invalid: '内容未通过校验',
    revoked: '本地素材已撤回，同步不会恢复',
    edited_locally: '本地草稿已被人工修改，同步不会覆盖；公众号原文有更新，请人工合并',
    failed: '处理失败，下次同步会重试',
};
export function mpCountsText(counts: MPSyncCounts): string {
    const parts = [`读取 ${counts.fetched} 篇`, `新建草稿 ${counts.created}`, `更新草稿 ${counts.updated}`, `无变化 ${counts.unchanged}`];
    if (counts.skipped) parts.push(`跳过 ${counts.skipped}`);
    if (counts.conflicts) parts.push(`需人工处理 ${counts.conflicts}`);
    if (counts.failed) parts.push(`失败 ${counts.failed}`);
    if (counts.images_imported || counts.image_failures) parts.push(`配图导入 ${counts.images_imported}，失败 ${counts.image_failures}`);
    return parts.join(' · ');
}
function RunSummary({ run, ok = true, error }: { run: MPSyncRun; ok?: boolean; error?: string }) {
    return <>
        <p>{time(run.finished_at)} · {ok ? mpCountsText(run.counts) : error || '同步失败'}</p>
        {run.problems.length > 0 && <ul>{run.problems.map((problem, index) => <li key={`${problem.ref}-${index}`}>文章 {problem.ref}：{mpProblemText[problem.reason] || problem.reason}</li>)}</ul>}
    </>;
}
export function MPSyncPanel({ onBusyChange, changed }: { onBusyChange?: (value: boolean) => void; changed?: () => void }) {
    const [info, setInfo] = useState<MPSyncInfo>(); const [error, setError] = useState(''); const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false); const [fresh, setFresh] = useState<MPSyncRun>();
    const alive = useRef(true); const pending = useRef(false);
    useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
    function load() {
        const controller = new AbortController(); setLoading(true); setError('');
        tracking.mpSyncInfo(controller.signal).then(data => { if (!controller.signal.aborted) { setInfo(data); setLoading(false); } })
            .catch(reason => { if (!controller.signal.aborted) { setError(reason instanceof Error ? reason.message : '读取失败'); setLoading(false); } });
        return () => controller.abort();
    }
    useEffect(load, []);
    async function run() {
        if (pending.current) return;
        pending.current = true; setBusy(true); onBusyChange?.(true); setError('');
        try {
            const result = await tracking.mpSyncRun();
            if (!alive.current) return;
            setFresh(result); changed?.(); load();
        } catch (reason) {
            if (alive.current) setError(`${reason instanceof Error ? reason.message : '同步失败'}。草稿是否已生成请以素材列表为准。`);
        } finally { pending.current = false; if (alive.current) { setBusy(false); onBusyChange?.(false); } }
    }
    if (loading) return <div className="st-feedback" role="status">正在读取同步状态…</div>;
    return <section className="st-mp-sync">
        <p>把公众号已发布的文章复制为本系统的素材<strong>草稿</strong>。同步<strong>不会自动发布</strong>，不会撤回已有素材，也不会覆盖被人工修改过的草稿；每篇都需要人工核对后，走原有的预览、发布流程。</p>
        {error && <p className="st-error" role="alert">{error}</p>}
        {info && !info.enabled && <><p className="st-warning">{info.problems?.length ? '公众号同步配置不完整，尚未启用：' : '公众号同步尚未启用。'}</p>{info.problems && <ul>{info.problems.map(problem => <li key={problem}>{problem}</li>)}</ul>}</>}
        {info?.enabled && <>
            <p className="st-muted">归属 {info.owner_id} · 权属声明 {info.rights === 'authorized' ? '已获授权' : '自有内容'} · 每次最多读取 {info.max_articles} 篇 · {info.import_images ? '导入配图' : '不导入配图（图片需在编辑页上传）'} · {info.interval_minutes ? `每 ${info.interval_minutes} 分钟自动同步` : '仅手动同步'}</p>
            <button className="st-primary" disabled={busy} onClick={run}>{busy ? '正在同步…' : '立即同步'}</button>
            {fresh && <><h4>本次结果</h4><RunSummary run={fresh} /></>}
            {info.last_run && !fresh && <><h4>最近一次同步</h4><RunSummary run={info.last_run} ok={info.last_run.ok} error={info.last_run.error} />{info.last_run.wechat_errcode ? <p className="st-muted">微信错误码 {info.last_run.wechat_errcode}</p> : null}</>}
            {!info.last_run && !fresh && <p className="st-muted">尚未同步过。</p>}
        </>}
    </section>;
}
