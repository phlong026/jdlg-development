import React, { useState, useEffect, useCallback } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiFileText, FiHelpCircle, FiVideo } from 'react-icons/fi';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getUtilityToolList,
    getUtilityToolDetail,
    createUtilityTool,
    updateUtilityTool,
    deleteUtilityTool,
    type UtilityToolItem,
} from '../api/utilityTools';
import './utilityTools.scss';

interface UtilityTool {
    id: string;
    category: 'local_norm' | 'faq' | 'video_tutorial';
    title: string;
    content: string;
    coverImage?: string;
    videoUrl?: string;
    fileUrl?: string;
    sortOrder: number;
    isPublished: boolean;
    viewCount: number;
    createdAt: string;
    updatedAt: string;
}

const defaultForm = {
    category: 'local_norm' as 'local_norm' | 'faq' | 'video_tutorial',
    title: '',
    content: '',
    coverImage: '',
    videoUrl: '',
    fileUrl: '',
    sortOrder: 0,
    isPublished: true,
};

const UtilityTools: React.FC = () => {
    const [tools, setTools] = useState<UtilityTool[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [searchKeyword, setSearchKeyword] = useState('');
    const [categoryFilter, setCategoryFilter] = useState<string>('all');
    const [publishedFilter, setPublishedFilter] = useState<string>('all');
    const [total, setTotal] = useState(0);
    const [showAddModal, setShowAddModal] = useState(false);
    const [showEditModal, setShowEditModal] = useState(false);
    const [selectedTool, setSelectedTool] = useState<UtilityTool | null>(null);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [formData, setFormData] = useState(defaultForm);

    const convertTool = (t: UtilityToolItem): UtilityTool => ({
        id: String(t.id),
        category: t.category,
        title: t.title,
        content: t.content || '',
        coverImage: t.cover_image,
        videoUrl: t.video_url,
        fileUrl: t.file_url,
        sortOrder: t.sort_order || 0,
        isPublished: t.is_published ?? true,
        viewCount: t.view_count || 0,
        createdAt: t.created_at,
        updatedAt: t.updated_at,
    });

    const loadTools = useCallback(async () => {
        setLoading(true);
        try {
            const params: any = { page: 1, page_size: 100 };
            if (categoryFilter !== 'all') params.category = categoryFilter;
            if (searchKeyword) params.keyword = searchKeyword;
            if (publishedFilter !== 'all') params.is_published = publishedFilter === 'published';
            const res = await getUtilityToolList(params);
            const list = (res?.list || []).map(convertTool);
            setTools(list);
            setTotal(res?.total || list.length);
        } catch (error) {
            console.error('加载实用工具内容列表失败:', error);
            alert('加载实用工具内容列表失败');
        } finally {
            setLoading(false);
        }
    }, [categoryFilter, publishedFilter, searchKeyword]);

    useEffect(() => {
        loadTools();
    }, [loadTools]);

    const getCategoryLabel = (category: string) => {
        const categoryMap: Record<string, string> = {
            local_norm: '本地规范',
            faq: 'FAQ',
            video_tutorial: '视频教程',
        };
        return categoryMap[category] || category;
    };

    const getCategoryIcon = (category: string) => {
        switch (category) {
            case 'local_norm':
                return <FiFileText size={20} />;
            case 'faq':
                return <FiHelpCircle size={20} />;
            case 'video_tutorial':
                return <FiVideo size={20} />;
            default:
                return <FiFileText size={20} />;
        }
    };

    const handleEdit = async (tool: UtilityTool) => {
        try {
            const detail = await getUtilityToolDetail(tool.id);
            setFormData({
                category: detail.category,
                title: detail.title,
                content: detail.content || '',
                coverImage: detail.cover_image || '',
                videoUrl: detail.video_url || '',
                fileUrl: detail.file_url || '',
                sortOrder: detail.sort_order || 0,
                isPublished: detail.is_published ?? true,
            });
            setSelectedTool(tool);
            setShowEditModal(true);
        } catch (e) {
            console.error(e);
            alert('获取详情失败');
        }
    };

    const handleSaveEdit = async () => {
        if (!selectedTool) return;
        if (!formData.title.trim()) {
            alert('请填写标题');
            return;
        }
        setActionLoading(selectedTool.id);
        try {
            await updateUtilityTool(selectedTool.id, {
                category: formData.category,
                title: formData.title,
                content: formData.content,
                cover_image: formData.coverImage || undefined,
                video_url: formData.videoUrl || undefined,
                file_url: formData.fileUrl || undefined,
                sort_order: formData.sortOrder,
                is_published: formData.isPublished,
            });
            setShowEditModal(false);
            setSelectedTool(null);
            loadTools();
        } catch (e: any) {
            alert(e?.message || '更新失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleAdd = async () => {
        if (!formData.title.trim()) {
            alert('请填写标题');
            return;
        }
        setActionLoading('add');
        try {
            await createUtilityTool({
                category: formData.category,
                title: formData.title,
                content: formData.content,
                cover_image: formData.coverImage || undefined,
                video_url: formData.videoUrl || undefined,
                file_url: formData.fileUrl || undefined,
                sort_order: formData.sortOrder,
                is_published: formData.isPublished,
            });
            setShowAddModal(false);
            setFormData(defaultForm);
            loadTools();
        } catch (e: any) {
            alert(e?.message || '创建失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleDelete = async (toolId: string) => {
        if (!window.confirm('确定要删除这个内容吗？此操作不可恢复！')) return;
        setActionLoading(toolId);
        try {
            await deleteUtilityTool(toolId);
            await loadTools();
        } catch (error) {
            console.error('删除失败:', error);
            alert('删除失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleSearch = () => {
        setSearchKeyword(searchInput.trim());
    };

    const handleReset = () => {
        setSearchInput('');
        setSearchKeyword('');
        setCategoryFilter('all');
        setPublishedFilter('all');
    };

    return (
        <Layout title="实用工具管理">
            <div className="utility-tools-container">
                <ManagementSearchPanel
                    title="实用工具检索与内容维护"
                    description="先按分类、发布状态和关键词找到目标内容，再决定新增、编辑还是下线，避免只在前端本地列表里假搜索。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadTools()} disabled={loading}>刷新列表</button>
                            <button className="btn-primary" onClick={() => { setFormData(defaultForm); setShowAddModal(true); }}>
                                <FiPlus />
                                添加内容
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <div className="management-search-searchbox">
                                <FiSearch className="management-search-searchicon" />
                                <input
                                    type="text"
                                    className="management-search-input"
                                    placeholder="搜索标题、内容、文件地址或视频地址..."
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                />
                            </div>
                            <select
                                className="management-search-select"
                                value={categoryFilter}
                                onChange={(e) => setCategoryFilter(e.target.value)}
                            >
                                <option value="all">全部分类</option>
                                <option value="local_norm">本地规范</option>
                                <option value="faq">FAQ</option>
                                <option value="video_tutorial">视频教程</option>
                            </select>
                            <select
                                className="management-search-select"
                                value={publishedFilter}
                                onChange={(e) => setPublishedFilter(e.target.value)}
                            >
                                <option value="all">全部状态</option>
                                <option value="published">已发布</option>
                                <option value="draft">未发布</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索内容</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共 <strong>{total}</strong> 条工具内容
                            </div>
                            <div className="management-search-tags">
                                {searchKeyword ? <span className="management-search-tag">关键词：{searchKeyword}</span> : null}
                                {categoryFilter !== 'all' ? <span className="management-search-tag">分类：{getCategoryLabel(categoryFilter)}</span> : null}
                                {publishedFilter !== 'all' ? <span className="management-search-tag">状态：{publishedFilter === 'published' ? '已发布' : '未发布'}</span> : null}
                            </div>
                        </>
                    )}
                />

                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="utility-tools-list">
                        {tools.length === 0 ? (
                            <div className="empty-state">暂无内容</div>
                        ) : (
                            tools.map((tool) => (
                                <div key={tool.id} className="utility-tool-item">
                                    <div className="tool-icon">{getCategoryIcon(tool.category)}</div>
                                    <div className="tool-content">
                                        <div className="tool-header">
                                            <h4>{tool.title}</h4>
                                            <span className={`tool-status ${tool.isPublished ? 'published' : 'draft'}`}>
                                                {tool.isPublished ? '已发布' : '未发布'}
                                            </span>
                                        </div>
                                        <div className="tool-meta">
                                            <span className="tool-category">{getCategoryLabel(tool.category)}</span>
                                            <span>查看: {tool.viewCount}</span>
                                            <span>排序: {tool.sortOrder}</span>
                                        </div>
                                        <div className="tool-preview">
                                            {tool.content ? (
                                                <p>{tool.content.slice(0, 100)}{tool.content.length > 100 ? '...' : ''}</p>
                                            ) : (
                                                <p className="text-muted">暂无内容</p>
                                            )}
                                        </div>
                                    </div>
                                    <div className="tool-actions">
                                        <button className="btn-action btn-edit" onClick={() => handleEdit(tool)} title="编辑">
                                            <FiEdit size={14} />
                                        </button>
                                        <button
                                            className="btn-action btn-delete"
                                            onClick={() => handleDelete(tool.id)}
                                            disabled={actionLoading === tool.id}
                                            title="删除"
                                        >
                                            <FiTrash2 size={14} />
                                        </button>
                                    </div>
                                </div>
                            ))
                        )}
                    </div>
                )}

                {/* 添加/编辑弹窗 */}
                {(showAddModal || showEditModal) && (
                    <div className="modal-overlay" onClick={() => { setShowAddModal(false); setShowEditModal(false); setSelectedTool(null); }}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{showEditModal ? '编辑内容' : '添加内容'}</h3>
                                <button className="modal-close" onClick={() => { setShowAddModal(false); setShowEditModal(false); setSelectedTool(null); }}>✕</button>
                            </div>
                            <div className="modal-body">
                                <div className="form-group">
                                    <label>分类 *</label>
                                    <select className="form-input" value={formData.category} onChange={(e) => setFormData(f => ({ ...f, category: e.target.value as any }))}>
                                        <option value="local_norm">本地规范</option>
                                        <option value="faq">FAQ</option>
                                        <option value="video_tutorial">视频教程</option>
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>标题 *</label>
                                    <input className="form-input" value={formData.title} onChange={(e) => setFormData(f => ({ ...f, title: e.target.value }))} placeholder="内容标题" />
                                </div>
                                <div className="form-group">
                                    <label>内容</label>
                                    <textarea className="form-input" rows={6} value={formData.content} onChange={(e) => setFormData(f => ({ ...f, content: e.target.value }))} placeholder="支持Markdown或HTML格式" />
                                </div>
                                {formData.category === 'video_tutorial' && (
                                    <>
                                        <div className="form-group">
                                            <label>封面图 URL</label>
                                            <input className="form-input" value={formData.coverImage} onChange={(e) => setFormData(f => ({ ...f, coverImage: e.target.value }))} placeholder="https://..." />
                                        </div>
                                        <div className="form-group">
                                            <label>视频 URL</label>
                                            <input className="form-input" value={formData.videoUrl} onChange={(e) => setFormData(f => ({ ...f, videoUrl: e.target.value }))} placeholder="https://..." />
                                        </div>
                                    </>
                                )}
                                {formData.category === 'local_norm' && (
                                    <div className="form-group">
                                        <label>文件 URL（如PDF）</label>
                                        <input className="form-input" value={formData.fileUrl} onChange={(e) => setFormData(f => ({ ...f, fileUrl: e.target.value }))} placeholder="https://..." />
                                    </div>
                                )}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>排序顺序</label>
                                        <input type="number" className="form-input" value={formData.sortOrder} onChange={(e) => setFormData(f => ({ ...f, sortOrder: parseInt(e.target.value, 10) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-inline">
                                        <label>
                                            <input type="checkbox" checked={formData.isPublished} onChange={(e) => setFormData(f => ({ ...f, isPublished: e.target.checked }))} />
                                            发布
                                        </label>
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => { setShowAddModal(false); setShowEditModal(false); setSelectedTool(null); }}>取消</button>
                                <button className="btn-primary" onClick={showEditModal ? handleSaveEdit : handleAdd} disabled={actionLoading === (selectedTool?.id || 'add')}>
                                    {actionLoading === (selectedTool?.id || 'add') ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default UtilityTools;
