import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useRef, useState } from 'react';
import { FiBarChart2, FiDownload, FiRefreshCw } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { getReportCenterExportUrl, getReportCenterOverview, getReportCenterReport, type ReportCenterOverview, type ReportCenterParams, type ReportCenterResponse } from '../api/reportCenter';
import './reportCenter.scss';

const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiDownloadIcon = FiDownload as unknown as React.ComponentType<any>;
const FiBarChart2Icon = FiBarChart2 as unknown as React.ComponentType<any>;

const defaultOverview: ReportCenterOverview = {
    total_users: 0,
    total_revenue: 0,
    total_tasks: 0,
    total_templates: 0,
    designer_count: 0,
};

const reportTypeOptions = [
    { value: 'user_growth', label: '用户新增与活跃' },
    { value: 'revenue_conversion', label: '收入与支付转化' },
    { value: 'ai_success_rate', label: 'AI任务成功率' },
    { value: 'template_conversion', label: '模板转化' },
    { value: 'designer_health', label: '设计师供给健康度' },
];

const periodOptions = [
    { value: 'daily', label: '日报' },
    { value: 'weekly', label: '周报' },
    { value: 'monthly', label: '月报' },
];

// Report contract: these three fields are ratios. Never infer units from a value's magnitude.
const ratioFields = new Set(['success_rate', 'pay_conversion_rate', 'template_conversion_rate']);
const summaryLabels: Record<string, string> = {
    new_users: '新增用户', active_users: '活跃用户', order_count: '订单数', success_orders: '成功订单',
    success_amount: '成交金额', pay_conversion_rate: '支付转化率', task_count: '总任务数',
    success_tasks: '成功任务', success_rate: '成功率', downloads: '下载量', unlocks: '解锁量',
    interactions: '互动量', template_conversion_rate: '模板转化率', published_works: '发布作品数',
    active_designers: '活跃设计师数',
};
const formatCellValue = (key: string, value: unknown) => {
    if (value === null || value === undefined || value === '') return '-';
    if (ratioFields.has(key) && typeof value === 'number' && Number.isFinite(value)) return `${(value * 100).toFixed(1)}%`;
    return String(value);
};
const sameParams = (left: ReportCenterParams, right: ReportCenterParams) =>
    left.report_type === right.report_type && left.period === right.period &&
    (left.start_date || '') === (right.start_date || '') && (left.end_date || '') === (right.end_date || '');
const invalidRange = (params: ReportCenterParams) => Boolean(params.start_date && params.end_date && params.start_date > params.end_date);
interface ReportPreview {
    requested: ReportCenterParams;
    applied: ReportCenterParams;
    overview: ReportCenterOverview;
    report: ReportCenterResponse;
}

const ReportCenter: React.FC = () => {
    const [loading, setLoading] = useState(false);
    const [loadError, setLoadError] = useState('');
    const [reportType, setReportType] = useState('user_growth');
    const [period, setPeriod] = useState('daily');
    const [startDate, setStartDate] = useState('');
    const [endDate, setEndDate] = useState('');
    const [preview, setPreview] = useState<ReportPreview | null>(null);
    const requestVersion = useRef(0);
    const mounted = useRef(true);
    const loadingRef = useRef(false);
    const draft: ReportCenterParams = { report_type: reportType, period, start_date: startDate || undefined, end_date: endDate || undefined };
    const draftRef = useRef(draft);
    const previewRef = useRef(preview);
    draftRef.current = draft;
    previewRef.current = preview;
    const dateError = invalidRange(draft) ? '开始日期不能晚于结束日期。' : '';
    const hasUnappliedChanges = Boolean(preview && !sameParams(draft, preview.requested));
    const canExport = Boolean(preview && !loading && !loadError && !dateError && !hasUnappliedChanges);

    // Invalidate synchronously so an old response or saved export handler cannot win the render gap.
    const changeDraft = (changes: Partial<ReportCenterParams>) => {
        draftRef.current = { ...draftRef.current, ...changes };
        requestVersion.current += 1;
        loadingRef.current = false;
        setLoading(false);
        setLoadError('');
    };

    const loadData = async () => {
        const params = { ...draftRef.current };
        if (!mounted.current || invalidRange(params)) return;
        const version = ++requestVersion.current;
        const isCurrent = () => mounted.current && requestVersion.current === version && sameParams(draftRef.current, params);
        loadingRef.current = true;
        previewRef.current = null;
        setLoading(true);
        setLoadError('');
        setPreview(null);
        try {
            const [overviewData, reportData] = await Promise.all([getReportCenterOverview(), getReportCenterReport(params)]);
            if (!isCurrent()) return;
            // Resolve server-defaulted dates once and export exactly this successful preview range.
            if (reportData.report_type !== params.report_type || reportData.period !== params.period ||
                !/^\d{4}-\d{2}-\d{2}$/.test(reportData.start_date || '') || !/^\d{4}-\d{2}-\d{2}$/.test(reportData.end_date || '') ||
                reportData.start_date > reportData.end_date) throw new Error('报表响应的类型、周期或日期范围无效，请重试');
            const nextPreview = {
                requested: params,
                applied: { report_type: reportData.report_type, period: reportData.period, start_date: reportData.start_date, end_date: reportData.end_date },
                overview: overviewData,
                report: { ...reportData, columns: reportData.columns || [], rows: reportData.rows || [], summary: reportData.summary || {} },
            };
            previewRef.current = nextPreview;
            setPreview(nextPreview);
            reportPageLoadRecovered('加载报表中心失败');
        } catch (error) {
            if (!isCurrent()) return;
            const message = error instanceof Error ? error.message : '请重试';
            setLoadError(`报表加载失败：${message}`);
            reportPageLoadError(error, '加载报表中心失败');
        } finally {
            if (isCurrent()) {
                loadingRef.current = false;
                setLoading(false);
            }
        }
    };

    useEffect(() => {
        mounted.current = true;
        return () => { mounted.current = false; requestVersion.current += 1; };
    }, []);
    useEffect(() => { void loadData(); }, [reportType, period]);

    const handleExport = () => {
        const current = previewRef.current;
        if (!mounted.current || !current || loadingRef.current || invalidRange(draftRef.current) || !sameParams(draftRef.current, current.requested)) return;
        window.open(getReportCenterExportUrl(current.applied), '_blank');
    };
    const overview = preview?.overview || defaultOverview;
    const report = preview?.report || { columns: [], rows: [], summary: {} };
    const currentReportTypeLabel = reportTypeOptions.find((item) => item.value === reportType)?.label || reportType;
    const currentPeriodLabel = periodOptions.find((item) => item.value === period)?.label || period;
    const previewTypeLabel = reportTypeOptions.find((item) => item.value === preview?.applied.report_type)?.label;
    const previewPeriodLabel = periodOptions.find((item) => item.value === preview?.applied.period)?.label;

    return (
        <>
            <div className="report-center-page">
                <ManagementSearchPanel
                    title="报表筛选与导出"
                    description="先选报表类型、统计周期和时间范围，再刷新预览，确认无误后导出当前报表。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading || Boolean(dateError)}>
                                <FiRefreshCwIcon />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                            <button className="btn-primary" onClick={handleExport} disabled={!canExport} aria-describedby="report-preview-status">
                                <FiDownloadIcon />
                                导出当前报表
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <select className="management-search-select" aria-label="报表类型" value={reportType} onChange={(e) => { changeDraft({ report_type: e.target.value }); setReportType(e.target.value); }}>
                                {reportTypeOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                            </select>
                            <select className="management-search-select" aria-label="统计周期" value={period} onChange={(e) => { changeDraft({ period: e.target.value }); setPeriod(e.target.value); }}>
                                {periodOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                            </select>
                            <input className="management-search-date" type="date" aria-label="开始日期" aria-invalid={Boolean(dateError)} aria-describedby={dateError ? "report-date-error" : undefined} value={startDate} onChange={(e) => { changeDraft({ start_date: e.target.value || undefined }); setStartDate(e.target.value); }} />
                            <input className="management-search-date" type="date" aria-label="结束日期" aria-invalid={Boolean(dateError)} aria-describedby={dateError ? "report-date-error" : undefined} value={endDate} onChange={(e) => { changeDraft({ end_date: e.target.value || undefined }); setEndDate(e.target.value); }} />
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading || Boolean(dateError)}>应用筛选</button>
                            {dateError && <span id="report-date-error" className="report-error" role="alert">{dateError}</span>}
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前条件：<strong>{currentReportTypeLabel}</strong> · {currentPeriodLabel}
                            </div>
                            <div className="management-search-tags">
                                <span className="management-search-tag">周期：{currentPeriodLabel}</span>
                                {(startDate || endDate) ? <span className="management-search-tag">区间：{startDate || '不限'} ~ {endDate || '不限'}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div id="report-preview-status" className={`report-preview-status${loadError || dateError ? ' report-error' : ''}`} role={loadError ? 'alert' : 'status'}>
                    {dateError ? '日期范围无效，请修正后应用筛选。当前不可导出。' : loading ? '正在加载预览，完成前不可导出。' : loadError ? `${loadError}。请刷新重试，当前不可导出。` : hasUnappliedChanges ? '条件已更改，请应用筛选后再导出。下方仍为上次成功预览。' : !preview ? '当前条件尚未成功预览，请应用筛选。当前不可导出。' : '预览已更新，可导出下方报表。'}
                    {preview && <div>已预览：{previewTypeLabel} · {previewPeriodLabel} · {preview.applied.start_date} ~ {preview.applied.end_date}</div>}
                </div>

                <div className="report-stats-grid">
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">用户总量</div><div className="stat-value">{preview ? overview.total_users : '—'}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">累计收入</div><div className="stat-value">{preview ? overview.total_revenue : '—'}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">任务总量</div><div className="stat-value">{preview ? overview.total_tasks : '—'}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">模板总量</div><div className="stat-value">{preview ? overview.total_templates : '—'}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">设计师数量</div><div className="stat-value">{preview ? overview.designer_count : '—'}</div></div>
                </div>

                <div className="report-panel section-card">
                    <div className="summary-strip">
                        {Object.keys(report.summary || {}).length === 0 ? <span className="summary-empty">{preview ? '当前没有摘要数据' : '摘要尚未加载'}</span> : Object.entries(report.summary).map(([key, value]) => (
                            <div key={key} className="summary-chip">
                                <strong>{summaryLabels[key] || key}</strong>
                                <span>{formatCellValue(key, value)}</span>
                            </div>
                        ))}
                    </div>

                    <div className="report-table-container">
                        <table className="report-table">
                            <thead>
                                <tr>
                                    {report.columns.map((column) => <th key={column.key}>{column.label}</th>)}
                                </tr>
                            </thead>
                            <tbody>
                                {report.rows.length === 0 ? (
                                    <tr>
                                        <td colSpan={Math.max(report.columns.length, 1)} className="empty-state">{preview ? '暂无报表数据' : loading ? '正在加载报表...' : '报表尚未加载，请应用筛选或重试'}</td>
                                    </tr>
                                ) : report.rows.map((row, index) => (
                                    <tr key={`row-${index}`}>
                                        {report.columns.map((column) => <td key={`${index}-${column.key}`}>{formatCellValue(column.key, row[column.key])}</td>)}
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </>
    );
};

export default ReportCenter;
