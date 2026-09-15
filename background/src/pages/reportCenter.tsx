import React, { useEffect, useMemo, useState } from 'react';
import { FiBarChart2, FiDownload, FiRefreshCw } from 'react-icons/fi';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { getReportCenterExportUrl, getReportCenterOverview, getReportCenterReport, type ReportCenterOverview } from '../api/reportCenter';
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

const formatCellValue = (value: any) => {
    if (typeof value === 'number') {
        if (value > 0 && value < 1) {
            return `${(value * 100).toFixed(1)}%`;
        }
        return value;
    }
    if (value === null || value === undefined || value === '') {
        return '-';
    }
    return String(value);
};

const ReportCenter: React.FC = () => {
    const [overview, setOverview] = useState<ReportCenterOverview>(defaultOverview);
    const [loading, setLoading] = useState(false);
    const [reportType, setReportType] = useState('user_growth');
    const [period, setPeriod] = useState('daily');
    const [startDate, setStartDate] = useState('');
    const [endDate, setEndDate] = useState('');
    const [report, setReport] = useState<{ columns: { key: string; label: string }[]; rows: Record<string, any>[]; summary: Record<string, any> }>({ columns: [], rows: [], summary: {} });

    const loadData = async () => {
        setLoading(true);
        try {
            const params = { report_type: reportType, period, start_date: startDate || undefined, end_date: endDate || undefined };
            const [overviewData, reportData] = await Promise.all([
                getReportCenterOverview(),
                getReportCenterReport(params),
            ]);
            setOverview(overviewData);
            setReport({ columns: reportData.columns || [], rows: reportData.rows || [], summary: reportData.summary || {} });
        } catch (error) {
            console.error('加载报表中心失败:', error);
            alert('加载报表中心失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData();
    }, [reportType, period]);

    const exportUrl = useMemo(() => {
        return getReportCenterExportUrl({ report_type: reportType, period, start_date: startDate || undefined, end_date: endDate || undefined });
    }, [reportType, period, startDate, endDate]);

    const currentReportTypeLabel = reportTypeOptions.find((item) => item.value === reportType)?.label || reportType;
    const currentPeriodLabel = periodOptions.find((item) => item.value === period)?.label || period;

    return (
        <Layout title="报表导出中心">
            <div className="report-center-page">
                <ManagementSearchPanel
                    title="报表筛选与导出"
                    description="先选报表类型、统计周期和时间范围，再刷新预览，确认无误后导出当前报表。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading}>
                                <FiRefreshCwIcon />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                            <button className="btn-primary" onClick={() => window.open(exportUrl, '_blank')}>
                                <FiDownloadIcon />
                                导出当前报表
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <select className="management-search-select" value={reportType} onChange={(e) => setReportType(e.target.value)}>
                                {reportTypeOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                            </select>
                            <select className="management-search-select" value={period} onChange={(e) => setPeriod(e.target.value)}>
                                {periodOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                            </select>
                            <input className="management-search-date" type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                            <input className="management-search-date" type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                            <button className="btn-secondary" onClick={() => void loadData()}>应用筛选</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前报表：<strong>{currentReportTypeLabel}</strong> · {currentPeriodLabel}
                            </div>
                            <div className="management-search-tags">
                                <span className="management-search-tag">周期：{currentPeriodLabel}</span>
                                {(startDate || endDate) ? <span className="management-search-tag">区间：{startDate || '不限'} ~ {endDate || '不限'}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="report-stats-grid">
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">用户总量</div><div className="stat-value">{overview.total_users}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">累计收入</div><div className="stat-value">{overview.total_revenue}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">任务总量</div><div className="stat-value">{overview.total_tasks}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">模板总量</div><div className="stat-value">{overview.total_templates}</div></div>
                    <div className="stat-card section-card"><div className="stat-icon"><FiBarChart2Icon /></div><div className="stat-label">设计师数量</div><div className="stat-value">{overview.designer_count}</div></div>
                </div>

                <div className="report-panel section-card">
                    <div className="summary-strip">
                        {Object.keys(report.summary || {}).length === 0 ? <span className="summary-empty">当前没有摘要数据</span> : Object.entries(report.summary).map(([key, value]) => (
                            <div key={key} className="summary-chip">
                                <strong>{key}</strong>
                                <span>{formatCellValue(value)}</span>
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
                                        <td colSpan={Math.max(report.columns.length, 1)} className="empty-state">暂无报表数据</td>
                                    </tr>
                                ) : report.rows.map((row, index) => (
                                    <tr key={`row-${index}`}>
                                        {report.columns.map((column) => <td key={`${index}-${column.key}`}>{formatCellValue(row[column.key])}</td>)}
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </Layout>
    );
};

export default ReportCenter;
