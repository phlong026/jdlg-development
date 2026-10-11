import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiActivity, FiClock, FiDownload, FiRefreshCw, FiSearch, FiTrendingDown, FiTrendingUp } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getContentAnalyticsDownloadRanking,
    getContentAnalyticsEngagementRanking,
    getContentAnalyticsFeaturedCases,
    getContentAnalyticsLowConversion,
    getContentAnalyticsNewTemplates,
    getContentAnalyticsOverview,
    type ContentAnalyticsFeaturedCaseItem,
    type ContentAnalyticsOverview,
    type ContentAnalyticsTemplateItem,
} from '../api/contentAnalytics';
import './contentAnalytics.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiRefreshCwIcon = FiRefreshCw as unknown as React.ComponentType<any>;
const FiDownloadIcon = FiDownload as unknown as React.ComponentType<any>;
const FiActivityIcon = FiActivity as unknown as React.ComponentType<any>;
const FiClockIcon = FiClock as unknown as React.ComponentType<any>;
const FiTrendingDownIcon = FiTrendingDown as unknown as React.ComponentType<any>;
const FiTrendingUpIcon = FiTrendingUp as unknown as React.ComponentType<any>;

const loadFailureSource = '加载内容运营分析失败';

// Analytics currently returns numeric template IDs and creator names only.
// Do not coerce rounded, malformed or missing IDs into a different record.
const isTemplateId = (value: unknown): value is number =>
    typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
const creatorSearchName = (value: unknown) =>
    typeof value === 'string' && value.trim() !== '系统' ? value.trim() : '';

type AnalyticsReport = {
    overview: ContentAnalyticsOverview;
    downloadRanking: ContentAnalyticsTemplateItem[];
    engagementRanking: ContentAnalyticsTemplateItem[];
    newTemplates: ContentAnalyticsTemplateItem[];
    lowConversionTemplates: ContentAnalyticsTemplateItem[];
    featuredCases: ContentAnalyticsFeaturedCaseItem[];
};

const conversionTypeLabelMap: Record<string, string> = {
    unlock: '付费解锁转化',
    download: '下载使用转化',
};

const ContentAnalytics: React.FC = () => {
    const navigate = useNavigate();
    const [report, setReport] = useState<AnalyticsReport | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const keywordRef = useRef('');
    const mountedRef = useRef(true);
    const requestVersion = useRef(0);
    const activeRequest = useRef<AbortController | null>(null);

    const cancelLoad = () => {
        requestVersion.current += 1;
        activeRequest.current?.abort();
        activeRequest.current = null;
    };

    const loadData = async () => {
        // A second refresh/Enter in the same render must not start another batch.
        if (!mountedRef.current || activeRequest.current) return;
        const controller = new AbortController();
        activeRequest.current = controller;
        const version = ++requestVersion.current;
        const isCurrent = () => version === requestVersion.current && !controller.signal.aborted;
        setLoading(true);
        setLoadError('');
        setReport(null);
        try {
            const params = { page: 1, page_size: 10, keyword: keywordRef.current };
            const [
                overview,
                downloadData,
                engagementData,
                newTemplatesData,
                lowConversionData,
                featuredCasesData,
            ] = await Promise.all([
                getContentAnalyticsOverview(controller.signal),
                getContentAnalyticsDownloadRanking(params, controller.signal),
                getContentAnalyticsEngagementRanking(params, controller.signal),
                getContentAnalyticsNewTemplates(params, controller.signal),
                getContentAnalyticsLowConversion(params, controller.signal),
                getContentAnalyticsFeaturedCases(params, controller.signal),
            ]);
            if (!isCurrent()) return;
            setReport({
                overview,
                downloadRanking: downloadData.list,
                engagementRanking: engagementData.list,
                newTemplates: newTemplatesData.list,
                lowConversionTemplates: lowConversionData.list,
                featuredCases: featuredCasesData.list,
            });
            reportPageLoadRecovered(loadFailureSource);
        } catch (error) {
            if (!isCurrent()) return;
            setLoadError('内容运营分析读取失败，暂时无法判断库存、排行或案例效果。请重新加载。');
            reportPageLoadError(error, loadFailureSource);
        } finally {
            if (isCurrent()) {
                activeRequest.current = null;
                // Stop any sibling reads still running after one source failed.
                controller.abort();
                setLoading(false);
            }
        }
    };

    useEffect(() => {
        mountedRef.current = true;
        void loadData();
        return () => { mountedRef.current = false; cancelLoad(); };
    }, []);

    const applyKeyword = (nextKeyword: string) => {
        if (!mountedRef.current) return;
        if (keywordRef.current === nextKeyword) {
            void loadData();
            return;
        }
        // Start the new query in this event, including search/reset calls batched
        // before a render. Old responses cannot restore another query's rows.
        cancelLoad();
        keywordRef.current = nextKeyword;
        setReport(null);
        setLoadError('');
        setLoading(true);
        setKeyword(nextKeyword);
        void loadData();
    };

    const handleSearch = () => applyKeyword(searchInput.trim());

    const handleReset = () => {
        if (!mountedRef.current) return;
        setSearchInput('');
        applyKeyword('');
    };

    const openTemplateDetail = (templateId: unknown) => {
        if (mountedRef.current && isTemplateId(templateId)) navigate(`/templates?templateId=${templateId}`);
    };

    const searchDesignerByName = (creator: unknown) => {
        const name = creatorSearchName(creator);
        if (mountedRef.current && name) navigate(`/designer-center?keyword=${encodeURIComponent(name)}`);
    };

    const renderTemplateLink = (templateId: unknown, label: string) => (
        <button
            className="btn-text-link"
            disabled={!isTemplateId(templateId)}
            title={isTemplateId(templateId) ? `只读查看模板 #${templateId}` : '模板 ID 缺失或无效，无法查看详情'}
            onClick={() => openTemplateDetail(templateId)}
        >{label}</button>
    );

    const renderPerformanceTable = (
        title: string,
        desc: string,
        list: ContentAnalyticsTemplateItem[],
        mode: 'download' | 'engagement' | 'new' | 'low',
    ) => (
        <div className="analytics-section">
            <div className="section-header">
                <div>
                    <h3>{title}</h3>
                    <p>{desc}</p>
                </div>
            </div>
            <div className="analytics-table-container">
                <table className="analytics-table">
                    <thead>
                        <tr>
                            <th>模板</th>
                            <th>分类</th>
                            <th>下载</th>
                            <th>互动分</th>
                            <th>点赞 / 评论 / 分享</th>
                            <th>解锁数</th>
                            {mode === 'low' ? <th>转化判断</th> : null}
                            <th>创建时间</th>
                            <th>处理动作</th>
                        </tr>
                    </thead>
                    <tbody>
                        {list.length === 0 ? (
                            <tr>
                                <td colSpan={mode === 'low' ? 9 : 8} className="empty-state">暂无数据</td>
                            </tr>
                        ) : (
                            list.map((item) => (
                                <tr key={`${mode}-${item.id}`}>
                                    <td>
                                        <div className="identity-cell">
                                            <strong>{item.name}</strong>
                                            <span>{item.creator || '系统'} · {item.is_free ? '免费模板' : `${item.price} 灵石`}</span>
                                        </div>
                                    </td>
                                    <td>{item.category || '未分类'}</td>
                                    <td>{item.download_count}</td>
                                    <td>{item.engagement_score}</td>
                                    <td>{item.like_count} / {item.comment_count} / {item.share_count}</td>
                                    <td>{item.unlock_count}</td>
                                    {mode === 'low' ? (
                                        <td>
                                            <div className="identity-cell">
                                                <strong>{conversionTypeLabelMap[item.conversion_type || ''] || '待观察'}</strong>
                                                <span>
                                                    当前转化 {item.conversion_count ?? '未提供'}
                                                    {typeof item.conversion_rate === 'number' ? ` · 比率 ${(item.conversion_rate * 100).toFixed(1)}%` : ''}
                                                </span>
                                            </div>
                                        </td>
                                    ) : null}
                                    <td>{item.created_at ? new Date(item.created_at).toLocaleString('zh-CN') : '暂无'}</td>
                                    <td>
                                        <div className="table-action-stack">
                                            {renderTemplateLink(item.id, '查看模板详情')}
                                            <button
                                                className="btn-text-link"
                                                disabled={!creatorSearchName(item.creator)}
                                                title={creatorSearchName(item.creator) ? '仅按创建者名称搜索，不能确认是同一位设计师' : '暂无可检索的创建者名称'}
                                                onClick={() => searchDesignerByName(item.creator)}
                                            >按创建者同名搜索</button>
                                        </div>
                                    </td>
                                </tr>
                            ))
                        )}
                    </tbody>
                </table>
            </div>
        </div>
    );

    return (
        <>
            <div className="content-analytics-page">
                <ManagementSearchPanel
                    title="模板检索与内容分析"
                    description="按模板名、创建者或分类检索已发布内容，观察下载、互动和新创建模板表现。创建者仅提供名称，同名搜索需自行核对设计师身份。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadData()} disabled={loading}>
                                <FiRefreshCwIcon />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <div className="management-search-searchbox">
                                <FiSearchIcon className="management-search-searchicon" />
                                <input
                                    type="text"
                                    className="management-search-input"
                                    placeholder="搜索模板名、创建者、分类或来源类型"
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                />
                            </div>
                            <button className="btn-primary" onClick={handleSearch}>搜索内容</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                {report ? <>全部模板库存 <strong>{report.overview.total_templates}</strong> 个，已发布 <strong>{report.overview.published_templates}</strong> 个（概览不随关键词筛选）</> : '库存数据尚未读取完成'}
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                            </div>
                        </>
                    )}
                />

                {loading ? <div className="analytics-load-state section-card" role="status">正在加载内容运营分析，请稍候…</div> : null}
                {loadError ? (
                    <div className="analytics-load-state section-card" role="alert">
                        <p>{loadError}</p>
                        <button className="btn-secondary" onClick={() => void loadData()}>重新加载</button>
                    </div>
                ) : null}

                {report ? <>
                <div className="analytics-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiDownloadIcon /></div>
                        <div className="stat-label">模板总量 / 已发布</div>
                        <div className="stat-value">{report.overview.total_templates} / {report.overview.published_templates}</div>
                        <div className="stat-desc">先看库存够不够，再看真正上架了多少。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiActivityIcon /></div>
                        <div className="stat-label">累计下载 / 解锁</div>
                        <div className="stat-value">{report.overview.total_downloads} / {report.overview.total_unlocks}</div>
                        <div className="stat-desc">下载看使用结果，解锁看付费意愿。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiTrendingUpIcon /></div>
                        <div className="stat-label">累计互动量</div>
                        <div className="stat-value">{report.overview.total_interactions}</div>
                        <div className="stat-desc">包含点赞、评论和分享。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiClockIcon /></div>
                        <div className="stat-label">近 7 天新创建模板</div>
                        <div className="stat-value">{report.overview.week_new_templates}</div>
                        <div className="stat-desc">按创建时间统计全部状态的模板，含未发布。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiTrendingDownIcon /></div>
                        <div className="stat-label">精选案例组</div>
                        <div className="stat-value">{report.overview.featured_case_group_count}</div>
                        <div className="stat-desc">方便观察首页重点内容是否真的有效。</div>
                    </div>
                </div>

                <div className="analytics-panel section-card">
                    <p className="analytics-scope-note">以下每个排行及模板观察区最多显示 10 条，精选案例最多显示 10 组，均非完整清单；概览统计不受此展示数量限制。</p>
                    {renderPerformanceTable('模板下载排行', '优先找出最常被真正使用的模板。', report.downloadRanking, 'download')}
                    {renderPerformanceTable('点赞 / 评论 / 分享排行', '互动高的内容代表更容易引发讨论和传播。', report.engagementRanking, 'engagement')}
                    {renderPerformanceTable('近 14 天新创建模板表现', '仅展示已发布且创建于近 14 天的模板，按创建时间统计，不代表首次上架时间。', report.newTemplates, 'new')}
                    {renderPerformanceTable('低转化模板识别', '有互动但没形成下载或解锁，通常说明内容吸引力和成交力之间断层明显。', report.lowConversionTemplates, 'low')}

                    <div className="analytics-section">
                        <div className="section-header">
                            <div>
                                <h3>精选案例效果观察</h3>
                                <p>判断首页精选位和案例对比位，是不是在真正带动使用和互动。</p>
                            </div>
                        </div>
                        <div className="analytics-table-container">
                            <table className="analytics-table">
                                <thead>
                                    <tr>
                                        <th>案例组</th>
                                        <th>模式</th>
                                        <th>案例 1</th>
                                        <th>案例 2</th>
                                        <th>组合下载</th>
                                        <th>组合互动分</th>
                                        <th>最近更新时间</th>
                                        <th>处理动作</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {report.featuredCases.length === 0 ? (
                                        <tr>
                                            <td colSpan={8} className="empty-state">暂无精选案例数据</td>
                                        </tr>
                                    ) : (
                                        report.featuredCases.map((item) => (
                                            <tr key={item.id}>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.name}</strong>
                                                        <span>案例组 ID {item.id}</span>
                                                    </div>
                                                </td>
                                                <td>{item.display_mode}</td>
                                                <td>
                                                    <div className="identity-cell">
                                                        <strong>{item.case1?.name || '暂无'}</strong>
                                                        <span>下载 {item.case1?.download_count ?? '未提供'} · 互动 {item.case1?.engagement_score ?? '未提供'}</span>
                                                    </div>
                                                </td>
                                                <td>
                                                    {item.case2 ? (
                                                        <div className="identity-cell">
                                                            <strong>{item.case2.name}</strong>
                                                            <span>下载 {item.case2.download_count} · 互动 {item.case2.engagement_score}</span>
                                                        </div>
                                                    ) : '单案例'}
                                                </td>
                                                <td>{item.combined_download_count}</td>
                                                <td>{item.combined_engagement_score}</td>
                                                <td>{item.updated_at ? new Date(item.updated_at).toLocaleString('zh-CN') : '暂无'}</td>
                                                <td>
                                                    <div className="table-action-stack">
                                                        {renderTemplateLink(item.case1?.id, '查看主案例')}
                                                        {item.case2 ? renderTemplateLink(item.case2.id, '查看对比案例') : null}
                                                    </div>
                                                </td>
                                            </tr>
                                        ))
                                    )}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
                </> : null}
            </div>
        </>
    );
};

export default ContentAnalytics;
