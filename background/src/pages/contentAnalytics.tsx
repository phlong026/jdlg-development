import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiActivity, FiClock, FiDownload, FiRefreshCw, FiSearch, FiTrendingDown, FiTrendingUp } from 'react-icons/fi';
import Layout from '../component/layout';
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

const defaultOverview: ContentAnalyticsOverview = {
    total_templates: 0,
    published_templates: 0,
    total_downloads: 0,
    total_unlocks: 0,
    total_interactions: 0,
    week_new_templates: 0,
    featured_case_group_count: 0,
};

const conversionTypeLabelMap: Record<string, string> = {
    unlock: '付费解锁转化',
    download: '下载使用转化',
};

const ContentAnalytics: React.FC = () => {
    const navigate = useNavigate();
    const [overview, setOverview] = useState<ContentAnalyticsOverview>(defaultOverview);
    const [downloadRanking, setDownloadRanking] = useState<ContentAnalyticsTemplateItem[]>([]);
    const [engagementRanking, setEngagementRanking] = useState<ContentAnalyticsTemplateItem[]>([]);
    const [newTemplates, setNewTemplates] = useState<ContentAnalyticsTemplateItem[]>([]);
    const [lowConversionTemplates, setLowConversionTemplates] = useState<ContentAnalyticsTemplateItem[]>([]);
    const [featuredCases, setFeaturedCases] = useState<ContentAnalyticsFeaturedCaseItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [keyword, setKeyword] = useState('');

    const loadData = async () => {
        setLoading(true);
        try {
            const params = { page: 1, page_size: 10, keyword };
            const [
                overviewData,
                downloadData,
                engagementData,
                newTemplatesData,
                lowConversionData,
                featuredCasesData,
            ] = await Promise.all([
                getContentAnalyticsOverview(),
                getContentAnalyticsDownloadRanking(params),
                getContentAnalyticsEngagementRanking(params),
                getContentAnalyticsNewTemplates(params),
                getContentAnalyticsLowConversion(params),
                getContentAnalyticsFeaturedCases(params),
            ]);
            setOverview(overviewData);
            setDownloadRanking(downloadData.list || []);
            setEngagementRanking(engagementData.list || []);
            setNewTemplates(newTemplatesData.list || []);
            setLowConversionTemplates(lowConversionData.list || []);
            setFeaturedCases(featuredCasesData.list || []);
        } catch (error) {
            console.error('加载内容运营分析失败:', error);
            alert('加载内容运营分析失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData();
    }, [keyword]);

    const handleSearch = () => {
        setKeyword(searchInput.trim());
    };

    const handleReset = () => {
        setSearchInput('');
        setKeyword('');
    };

    const openTemplateManagement = (templateName?: string) => {
        if (!templateName) {
            navigate('/templates');
            return;
        }
        navigate(`/templates?keyword=${encodeURIComponent(templateName)}`);
    };

    const openDesignerCenter = (creator?: string) => {
        if (!creator || creator === '系统') {
            navigate('/designer-center');
            return;
        }
        navigate(`/designer-center?keyword=${encodeURIComponent(creator)}`);
    };

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
                            <th>{mode === 'new' ? '上架时间' : '创建时间'}</th>
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
                                                    当前转化 {item.conversion_count || 0}
                                                    {typeof item.conversion_rate === 'number' ? ` · 比率 ${(item.conversion_rate * 100).toFixed(1)}%` : ''}
                                                </span>
                                            </div>
                                        </td>
                                    ) : null}
                                    <td>{item.created_at ? new Date(item.created_at).toLocaleString('zh-CN') : '暂无'}</td>
                                    <td>
                                        <div className="table-action-stack">
                                            <button className="btn-text-link" onClick={() => openTemplateManagement(item.name)}>去模板广场管理</button>
                                            <button className="btn-text-link" onClick={() => openDesignerCenter(item.creator)}>去设计师中心</button>
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
        <Layout title="内容运营分析">
            <div className="content-analytics-page">
                <ManagementSearchPanel
                    title="模板检索与内容分析"
                    description="先按模板名、创建者或分类检索内容，再观察下载、互动、新上架表现和低转化问题。"
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
                                当前模板库存 <strong>{overview.total_templates}</strong> 个，已发布 <strong>{overview.published_templates}</strong> 个
                            </div>
                            <div className="management-search-tags">
                                {keyword ? <span className="management-search-tag">关键词：{keyword}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="analytics-stats-grid">
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiDownloadIcon /></div>
                        <div className="stat-label">模板总量 / 已发布</div>
                        <div className="stat-value">{overview.total_templates} / {overview.published_templates}</div>
                        <div className="stat-desc">先看库存够不够，再看真正上架了多少。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiActivityIcon /></div>
                        <div className="stat-label">累计下载 / 解锁</div>
                        <div className="stat-value">{overview.total_downloads} / {overview.total_unlocks}</div>
                        <div className="stat-desc">下载看使用结果，解锁看付费意愿。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiTrendingUpIcon /></div>
                        <div className="stat-label">累计互动量</div>
                        <div className="stat-value">{overview.total_interactions}</div>
                        <div className="stat-desc">包含点赞、评论和分享。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiClockIcon /></div>
                        <div className="stat-label">近 7 天新上架模板</div>
                        <div className="stat-value">{overview.week_new_templates}</div>
                        <div className="stat-desc">用来判断最近上新节奏是否健康。</div>
                    </div>
                    <div className="stat-card section-card">
                        <div className="stat-icon"><FiTrendingDownIcon /></div>
                        <div className="stat-label">精选案例组</div>
                        <div className="stat-value">{overview.featured_case_group_count}</div>
                        <div className="stat-desc">方便观察首页重点内容是否真的有效。</div>
                    </div>
                </div>

                <div className="analytics-panel section-card">
                    {renderPerformanceTable('模板下载排行', '优先找出最常被真正使用的模板。', downloadRanking, 'download')}
                    {renderPerformanceTable('点赞 / 评论 / 分享排行', '互动高的内容代表更容易引发讨论和传播。', engagementRanking, 'engagement')}
                    {renderPerformanceTable('新上架模板表现', '新模板上架后的前两周表现，是判断内容方向是否正确的关键窗口。', newTemplates, 'new')}
                    {renderPerformanceTable('低转化模板识别', '有互动但没形成下载或解锁，通常说明内容吸引力和成交力之间断层明显。', lowConversionTemplates, 'low')}

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
                                    {featuredCases.length === 0 ? (
                                        <tr>
                                            <td colSpan={8} className="empty-state">暂无精选案例数据</td>
                                        </tr>
                                    ) : (
                                        featuredCases.map((item) => (
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
                                                        <span>下载 {item.case1?.download_count || 0} · 互动 {item.case1?.engagement_score || 0}</span>
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
                                                        <button className="btn-text-link" onClick={() => openTemplateManagement(item.case1?.name || item.name)}>查看主案例</button>
                                                        {item.case2?.name ? <button className="btn-text-link" onClick={() => openTemplateManagement(item.case2?.name)}>查看对比案例</button> : null}
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
            </div>
        </Layout>
    );
};

export default ContentAnalytics;
