import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getAIContentAudits, reviewAIContentAudit, type AIContentAuditItem } from '../api/aiContentAudit';
import './aiContentAudits.scss';

const statusMeta: Record<string, { label: string; className: string }> = {
    pending: { label: '待审核', className: 'pending' },
    approved: { label: '通过', className: 'approved' },
    rejected: { label: '驳回', className: 'rejected' },
};

const sceneLabels: Record<string, string> = {
    ai_draw_single: 'AI 绘画-单图',
    ai_draw_multi: 'AI 绘画-多图',
    new_minnan_tool_text_to_image: '新闽派-文生图',
    new_minnan_tool_image_to_image: '新闽派-图生图',
    rural_villa_design: '乡村别墅设计',
    allround_design: '全能设计',
    building_replacement: '建筑换新',
    parent_child_design: '亲子设计',
};

const sceneLabel = (scene?: string) => (scene ? sceneLabels[scene] || scene : '-');

const AIContentAudits: React.FC = () => {
    const navigate = useNavigate();
    const [list, setList] = useState<AIContentAuditItem[]>([]);
    const [pendingCount, setPendingCount] = useState(0);
    const [total, setTotal] = useState(0);
    const [status, setStatus] = useState('pending');
    const [keyword, setKeyword] = useState('');
    const [page, setPage] = useState(1);
    const [pageSize] = useState(20);
    const [loading, setLoading] = useState(false);
    /** 图片预览（点击缩略图放大） */
    const [previewUrl, setPreviewUrl] = useState('');
    /** 审核中：驳回需要备注 */
    const [rejectTarget, setRejectTarget] = useState<AIContentAuditItem | null>(null);
    const [rejectNote, setRejectNote] = useState('');
    const [actingId, setActingId] = useState<number | null>(null);

    const loadData = useCallback(async (nextStatus = status, nextKeyword = keyword, nextPage = page) => {
        setLoading(true);
        try {
            const recoveryPage = window.location.pathname;
            const res = await getAIContentAudits({ status: nextStatus, keyword: nextKeyword.trim(), page: nextPage, page_size: pageSize });
            setList(res.list || []);
            setTotal(res.total || 0);
            setPendingCount(res.pending_count || 0);
            reportPageLoadRecovered("加载内容抽检列表失败", recoveryPage);
        } catch (error) {
            console.error('加载内容抽检列表失败:', error);
            reportPageLoadError(error, '加载内容抽检列表失败');
        } finally {
            setLoading(false);
        }
    }, [status, keyword, page, pageSize]);

    useEffect(() => {
        void loadData('pending', '', 1);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const handleApprove = async (item: AIContentAuditItem) => {
        if (!window.confirm(`确认通过任务 ${item.task_no} 的生成内容？`)) return;
        setActingId(item.id);
        try {
            await reviewAIContentAudit(item.id, 'approved');
            await loadData();
        } catch (error: any) {
            alert(`审核失败: ${error?.message || '未知错误'}`);
        } finally {
            setActingId(null);
        }
    };

    const handleRejectConfirm = async () => {
        if (!rejectTarget) return;
        setActingId(rejectTarget.id);
        try {
            await reviewAIContentAudit(rejectTarget.id, 'rejected', rejectNote.trim());
            setRejectTarget(null);
            setRejectNote('');
            await loadData();
        } catch (error: any) {
            alert(`审核失败: ${error?.message || '未知错误'}`);
        } finally {
            setActingId(null);
        }
    };

    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    return (
        <div className="ai-content-audits-page">
            <div className="aca-pagehead">
                <h1>内容抽检</h1>
                <span className="aca-sub">成功生成结果按比例随机抽入（默认 2%，比例在「定价与供应商 → 告警设置」调整）；审核不拦截用户侧结果，驳回仅记录并支持转工单跟进</span>
            </div>

            <div className="aca-toolbar">
                <div className="aca-status-tabs">
                    {[
                        { key: 'pending', label: `待审核${pendingCount ? ` (${pendingCount})` : ''}` },
                        { key: 'approved', label: '已通过' },
                        { key: 'rejected', label: '已驳回' },
                        { key: 'all', label: '全部' },
                    ].map((tab) => (
                        <button
                            key={tab.key}
                            className={`aca-tab ${status === tab.key ? 'on' : ''} ${tab.key === 'pending' && pendingCount > 0 ? 'alert' : ''}`}
                            onClick={() => { setStatus(tab.key); setPage(1); void loadData(tab.key, keyword, 1); }}
                        >
                            {tab.label}
                        </button>
                    ))}
                </div>
                <input
                    className="aca-search"
                    placeholder="搜索任务号、用户名、用户ID"
                    value={keyword}
                    onChange={(event) => setKeyword(event.target.value)}
                    onKeyDown={(event) => { if (event.key === 'Enter') { setPage(1); void loadData(status, keyword, 1); } }}
                />
                <button className="aca-btn" onClick={() => { setPage(1); void loadData(status, keyword, 1); }}>查询</button>
            </div>

            <div className="aca-table-wrap">
                <table className="aca-table">
                    <thead>
                        <tr>
                            <th>缩略图</th>
                            <th>任务号</th>
                            <th>用户</th>
                            <th>场景 / 模型</th>
                            <th>状态</th>
                            <th>抽检时间</th>
                            <th>审核结论</th>
                            <th>操作</th>
                        </tr>
                    </thead>
                    <tbody>
                        {list.length === 0 ? (
                            <tr><td colSpan={8} className="aca-empty">{loading ? '加载中…' : '暂无抽检记录'}</td></tr>
                        ) : list.map((item) => {
                            const meta = statusMeta[item.status] || { label: item.status, className: '' };
                            return (
                                <tr key={item.id}>
                                    <td>
                                        {item.result_url ? (
                                            <img
                                                className="aca-thumb"
                                                src={item.result_url}
                                                alt="生成结果"
                                                onClick={() => setPreviewUrl(item.result_url)}
                                                onError={(event) => { (event.currentTarget as HTMLImageElement).style.visibility = 'hidden'; }}
                                            />
                                        ) : <span className="aca-dim">无图</span>}
                                    </td>
                                    <td className="aca-mono">{item.task_no}</td>
                                    <td>
                                        <button className="aca-link" onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}>
                                            {item.username || `用户${item.user_id}`}
                                        </button>
                                        <span className="aca-dim"> {item.user_id}</span>
                                    </td>
                                    <td>
                                        <div className="aca-scene-cell">
                                            <span>{sceneLabel(item.scene)}</span>
                                            <small>{item.model || '-'}</small>
                                        </div>
                                    </td>
                                    <td><span className={`aca-status ${meta.className}`}>{meta.label}</span></td>
                                    <td>{new Date(item.created_at).toLocaleString('zh-CN')}</td>
                                    <td>
                                        {item.status === 'pending' ? <span className="aca-dim">—</span> : (
                                            <div className="aca-review-cell">
                                                <span>{item.reviewer || '-'}</span>
                                                {item.review_note ? <small>{item.review_note}</small> : null}
                                            </div>
                                        )}
                                    </td>
                                    <td>
                                        {item.status === 'pending' ? (
                                            <div className="aca-actions">
                                                <button className="aca-btn sm primary" disabled={actingId === item.id} onClick={() => void handleApprove(item)}>
                                                    通过
                                                </button>
                                                <button className="aca-btn sm danger" disabled={actingId === item.id} onClick={() => { setRejectTarget(item); setRejectNote(''); }}>
                                                    驳回
                                                </button>
                                            </div>
                                        ) : <span className="aca-dim">已处理</span>}
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            </div>

            {total > pageSize ? (
                <div className="aca-pagination">
                    <button className="aca-btn" disabled={page <= 1 || loading} onClick={() => { const p = page - 1; setPage(p); void loadData(status, keyword, p); }}>上一页</button>
                    <span>第 {page} 页 / 共 {totalPages} 页（{total} 条）</span>
                    <button className="aca-btn" disabled={page >= totalPages || loading} onClick={() => { const p = page + 1; setPage(p); void loadData(status, keyword, p); }}>下一页</button>
                </div>
            ) : null}

            {/* 驳回备注弹窗 */}
            {rejectTarget ? (
                <div className="aca-modal-overlay" onClick={() => setRejectTarget(null)}>
                    <div className="aca-modal" onClick={(event) => event.stopPropagation()}>
                        <div className="aca-modal-head">
                            <h3>驳回 · {rejectTarget.task_no}</h3>
                            <button className="aca-modal-close" onClick={() => setRejectTarget(null)}>✕</button>
                        </div>
                        <div className="aca-modal-body">
                            <textarea
                                rows={4}
                                placeholder="驳回原因（写入审核记录，如：画面异常 / 内容不适宜）"
                                value={rejectNote}
                                onChange={(event) => setRejectNote(event.target.value)}
                                autoFocus
                            />
                            <p className="aca-modal-hint">抽检不拦截用户侧结果；驳回后如需补偿或回访，请到 AI 任务中心对该任务转异常工单跟进。</p>
                        </div>
                        <div className="aca-modal-foot">
                            <button className="aca-btn" onClick={() => setRejectTarget(null)}>取消</button>
                            <button className="aca-btn primary danger" disabled={actingId === rejectTarget.id} onClick={() => void handleRejectConfirm()}>
                                {actingId === rejectTarget.id ? '保存中…' : '确认驳回'}
                            </button>
                        </div>
                    </div>
                </div>
            ) : null}

            {/* 大图预览 */}
            {previewUrl ? (
                <div className="aca-modal-overlay" onClick={() => setPreviewUrl('')}>
                    <div className="aca-preview" onClick={(event) => event.stopPropagation()}>
                        <img src={previewUrl} alt="生成结果预览" />
                        <button className="aca-btn" onClick={() => setPreviewUrl('')}>关闭</button>
                    </div>
                </div>
            ) : null}
        </div>
    );
};

export default AIContentAudits;
