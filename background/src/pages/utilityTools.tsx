import React, { useState, useEffect, useCallback, useRef } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiFileText, FiHelpCircle, FiVideo } from 'react-icons/fi';
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
    const [query, setQuery] = useState({ keyword: '', category: 'all', published: 'all', page: 1 });
    const { keyword: searchKeyword, category: categoryFilter, published: publishedFilter } = query;
    const pageSize = 20;
    const [total, setTotal] = useState<number | null>(null);
    const [loadError, setLoadError] = useState('');
    const [feedback, setFeedback] = useState('');
    const queryRef = useRef(query);
    const readRef = useRef(0);
    const loadedKeyRef = useRef('');
    const mountedRef = useRef(true);
    const [editorMode, setEditorMode] = useState<'add' | 'edit' | 'loading' | null>(null);
    const editorModeRef = useRef(editorMode);
    const editorSessionRef = useRef(0);
    const renderedSession = editorSessionRef.current;
    const [editorError, setEditorError] = useState('');
    const [selectedTool, setSelectedTool] = useState<UtilityTool | null>(null);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const writeRef = useRef<string | null>(null);
    const [formData, setFormData] = useState(defaultForm);
    const showAddModal = editorMode === 'add';
    const showEditModal = editorMode === 'edit' || editorMode === 'loading';
    const editorSaving = actionLoading === 'save';
    queryRef.current = query;
    const renderedQueryKey = JSON.stringify(query);
    const isCurrentList = () => mountedRef.current && renderedQueryKey === JSON.stringify(queryRef.current);

    const changeQuery = (patch: Partial<typeof query>) => {
        if (!mountedRef.current) return;
        const next = { ...queryRef.current, ...patch };
        if (JSON.stringify(next) === JSON.stringify(queryRef.current)) return;
        queryRef.current = next;
        readRef.current += 1;
        setQuery(next);
        setTools([]);
        setTotal(null);
        setLoadError('');
        setLoading(true);
    };

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

    // Every refresh reads the current query, including a refresh after a write.
    const loadTools = useCallback(async () => {
        if (!mountedRef.current) return null;
        const current = queryRef.current;
        const key = JSON.stringify(current);
        const request = ++readRef.current;
        const isCurrent = () => mountedRef.current && request === readRef.current && key === JSON.stringify(queryRef.current);
        setLoading(true);
        setLoadError('');
        try {
            const res = await getUtilityToolList({
                page: current.page, page_size: pageSize,
                category: current.category === 'all' ? undefined : current.category,
                keyword: current.keyword || undefined,
                is_published: current.published === 'all' ? undefined : current.published === 'published',
            });
            if (!isCurrent()) return null;
            if (!Array.isArray(res?.list) || !Number.isSafeInteger(res.total) || res.total < 0 ||
                (res.page !== undefined && res.page !== current.page) ||
                (res.page_size !== undefined && res.page_size !== pageSize)) throw new Error('列表分页响应不完整或页码不匹配，请重试');
            const lastPage = Math.max(1, Math.ceil(res.total / pageSize));
            if (current.page > lastPage) {
                changeQuery({ page: lastPage });
                return null;
            }
            setTools(res.list.map(convertTool));
            setTotal(res.total);
            loadedKeyRef.current = key;
            return true;
        } catch (error: any) {
            if (!isCurrent()) return null;
            setLoadError(error?.message || '加载实用工具内容列表失败');
            if (loadedKeyRef.current !== key) { setTools([]); setTotal(null); }
            return false;
        } finally {
            if (isCurrent()) setLoading(false);
        }
    }, []);

    useEffect(() => {
        mountedRef.current = true;
        return () => { mountedRef.current = false; readRef.current += 1; editorSessionRef.current += 1; editorModeRef.current = null; };
    }, []);

    useEffect(() => { void loadTools(); }, [query, loadTools]);

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

    const closeEditor = () => {
        editorSessionRef.current += 1;
        editorModeRef.current = null;
        setEditorMode(null);
        setSelectedTool(null);
        setEditorError('');
    };
    const isCurrentEditor = () => mountedRef.current && renderedSession === editorSessionRef.current &&
        editorMode !== null && editorModeRef.current === editorMode;
    const dismissEditor = () => { if (isCurrentEditor() && writeRef.current !== 'save') closeEditor(); };
    const updateForm = (update: React.SetStateAction<typeof defaultForm>) => {
        if (isCurrentEditor() && editorMode !== 'loading' && !writeRef.current) setFormData(update);
    };
    const openAdd = () => {
        if (!mountedRef.current || writeRef.current) return;
        closeEditor();
        editorModeRef.current = 'add';
        setEditorMode('add');
        setFormData({ ...defaultForm });
        setFeedback('');
    };
    const handleEdit = async (tool: UtilityTool) => {
        if (!isCurrentList() || writeRef.current) return;
        closeEditor();
        const session = editorSessionRef.current;
        editorModeRef.current = 'loading';
        setEditorMode('loading');
        setSelectedTool(tool);
        setFeedback('');
        try {
            const detail = await getUtilityToolDetail(tool.id);
            if (!mountedRef.current || session !== editorSessionRef.current) return;
            if (!detail || String(detail.id) !== tool.id) throw new Error('详情对象不匹配，请关闭后重新选择');
            if (typeof detail.is_published !== 'boolean') throw new Error('详情发布状态不完整，不能保存，请关闭后重试');
            setFormData({
                category: detail.category, title: detail.title, content: detail.content || '',
                coverImage: detail.cover_image || '', videoUrl: detail.video_url || '', fileUrl: detail.file_url || '',
                sortOrder: detail.sort_order || 0, isPublished: detail.is_published ?? true,
            });
            editorModeRef.current = 'edit';
            setEditorMode('edit');
        } catch (error: any) {
            if (mountedRef.current && session === editorSessionRef.current) setEditorError(error?.message || '获取详情失败');
        }
    };

    const refreshAfterWrite = async (message: string) => {
        if (!mountedRef.current) return;
        setFeedback(message);
        const refreshed = await loadTools();
        if (mountedRef.current && refreshed === false) setFeedback(`${message}，但列表刷新失败，请重试加载，不要重复提交`);
    };
    const handleSave = async () => {
        if (!isCurrentEditor() || editorMode === 'loading' || writeRef.current || (editorMode === 'edit' && !selectedTool)) return;
        if (!formData.title.trim()) { setEditorError('请填写标题'); return; }
        const session = renderedSession;
        const editing = editorMode === 'edit';
        const id = selectedTool?.id;
        writeRef.current = 'save';
        setActionLoading('save');
        setEditorError('');
        const payload = {
            category: formData.category, title: formData.title, content: formData.content,
            cover_image: formData.coverImage, video_url: formData.videoUrl, file_url: formData.fileUrl,
            sort_order: formData.sortOrder, is_published: formData.isPublished,
        };
        try {
            if (editing) await updateUtilityTool(id!, payload);
            else await createUtilityTool(payload);
        } catch (error: any) {
            if (mountedRef.current && session === editorSessionRef.current) setEditorError(error?.message || (editing ? '更新失败' : '创建失败'));
            return;
        } finally {
            writeRef.current = null;
            if (mountedRef.current) setActionLoading(null);
        }
        if (mountedRef.current && session === editorSessionRef.current) closeEditor();
        await refreshAfterWrite(editing ? '内容已更新' : '内容已创建');
    };

    const handleDelete = async (toolId: string) => {
        if (!isCurrentList() || writeRef.current || !window.confirm('确定要删除这个内容吗？此操作不可恢复！')) return;
        writeRef.current = toolId;
        setActionLoading(toolId);
        setFeedback('');
        try {
            await deleteUtilityTool(toolId);
        } catch (error: any) {
            if (mountedRef.current) setFeedback(error?.message || '删除失败');
            return;
        } finally {
            writeRef.current = null;
            if (mountedRef.current) setActionLoading(null);
        }
        if (mountedRef.current) setTools(items => items.filter(item => item.id !== toolId));
        await refreshAfterWrite('内容已删除');
    };

    const handleSearch = () => { changeQuery({ keyword: searchInput.trim(), page: 1 }); };
    const handleReset = () => {
        setSearchInput('');
        changeQuery({ keyword: '', category: 'all', published: 'all', page: 1 });
    };

    return (
        <>
            <div className="utility-tools-container">
                <ManagementSearchPanel
                    title="实用工具检索与内容维护"
                    description="先按分类、发布状态和关键词找到目标内容，再决定新增、编辑还是下线，避免只在前端本地列表里假搜索。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadTools()} disabled={loading}>刷新列表</button>
                            <button className="btn-primary" onClick={openAdd} disabled={!!actionLoading}>
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
                                onChange={(e) => changeQuery({ category: e.target.value, page: 1 })}
                            >
                                <option value="all">全部分类</option>
                                <option value="local_norm">本地规范</option>
                                <option value="faq">FAQ</option>
                                <option value="video_tutorial">视频教程</option>
                            </select>
                            <select
                                className="management-search-select"
                                value={publishedFilter}
                                onChange={(e) => changeQuery({ published: e.target.value, page: 1 })}
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
                                当前筛选共 <strong>{total ?? '—'}</strong> 条工具内容
                            </div>
                            <div className="management-search-tags">
                                {searchKeyword ? <span className="management-search-tag">关键词：{searchKeyword}</span> : null}
                                {categoryFilter !== 'all' ? <span className="management-search-tag">分类：{getCategoryLabel(categoryFilter)}</span> : null}
                                {publishedFilter !== 'all' ? <span className="management-search-tag">状态：{publishedFilter === 'published' ? '已发布' : '未发布'}</span> : null}
                            </div>
                        </>
                    )}
                />

                {feedback && <div className="utility-feedback" role="status">{feedback}</div>}
                {loadError && <div className="utility-feedback" role="alert">{loadError}{total !== null ? '（保留上次成功结果）' : ''}<button className="btn-secondary" onClick={() => void loadTools()} disabled={loading}>重试加载</button></div>}
                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="utility-tools-list">
                        {tools.length === 0 ? (
                            <div className="empty-state">{total === null ? '列表尚未成功加载' : '暂无内容'}</div>
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
                                        <button className="btn-action btn-edit" onClick={() => handleEdit(tool)} disabled={!!actionLoading} title="编辑">
                                            <FiEdit size={14} />
                                        </button>
                                        <button
                                            className="btn-action btn-delete"
                                            onClick={() => handleDelete(tool.id)}
                                            disabled={!!actionLoading}
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

                <nav className="utility-pagination" aria-label="实用工具分页">
                    <span>第 {query.page} 页 · 共 {total === null ? '—' : Math.max(1, Math.ceil(total / pageSize))} 页 · 每页 {pageSize} 条</span>
                    <button className="btn-secondary" disabled={loading || query.page <= 1} onClick={() => { if (isCurrentList() && query.page > 1) changeQuery({ page: query.page - 1 }); }}>上一页</button>
                    <button className="btn-secondary" disabled={loading || total === null || query.page * pageSize >= total} onClick={() => { if (isCurrentList() && total !== null && query.page * pageSize < total) changeQuery({ page: query.page + 1 }); }}>下一页</button>
                </nav>

                {/* 添加/编辑弹窗 */}
                {(showAddModal || showEditModal) && (
                    <div className="modal-overlay" onClick={dismissEditor}>
                        <div className="modal-content modal-form" role="dialog" aria-modal="true" aria-label={showEditModal ? '编辑内容' : '添加内容'} onClick={(e) => e.stopPropagation()} onKeyDown={(e) => { if (e.key === 'Escape') dismissEditor(); }}>
                            <div className="modal-header">
                                <h3>{showEditModal ? '编辑内容' : '添加内容'}</h3>
                                <button className="modal-close" onClick={dismissEditor} disabled={editorSaving} aria-label="关闭内容编辑">✕</button>
                            </div>
                            {editorError && <div className="utility-feedback" role="alert">{editorError}</div>}
                            {editorMode === 'loading' && <div className="loading-state">{editorError ? '详情未加载，不能保存。请关闭后重试。' : '正在加载内容详情...'}</div>}
                            <fieldset className="modal-body" disabled={editorMode === 'loading' || editorSaving} hidden={editorMode === 'loading'}>
                                <div className="form-group">
                                    <label>分类 *</label>
                                    <select className="form-input" value={formData.category} onChange={(e) => updateForm(f => ({ ...f, category: e.target.value as any }))}>
                                        <option value="local_norm">本地规范</option>
                                        <option value="faq">FAQ</option>
                                        <option value="video_tutorial">视频教程</option>
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>标题 *</label>
                                    <input className="form-input" value={formData.title} onChange={(e) => updateForm(f => ({ ...f, title: e.target.value }))} placeholder="内容标题" />
                                </div>
                                <div className="form-group">
                                    <label>内容</label>
                                    <textarea className="form-input" rows={6} value={formData.content} onChange={(e) => updateForm(f => ({ ...f, content: e.target.value }))} placeholder="支持Markdown或HTML格式" />
                                </div>
                                {formData.category === 'video_tutorial' && (
                                    <>
                                        <div className="form-group">
                                            <label>封面图 URL</label>
                                            <input className="form-input" value={formData.coverImage} onChange={(e) => updateForm(f => ({ ...f, coverImage: e.target.value }))} placeholder="https://..." />
                                        </div>
                                        <div className="form-group">
                                            <label>视频 URL</label>
                                            <input className="form-input" value={formData.videoUrl} onChange={(e) => updateForm(f => ({ ...f, videoUrl: e.target.value }))} placeholder="https://..." />
                                        </div>
                                    </>
                                )}
                                {formData.category === 'local_norm' && (
                                    <div className="form-group">
                                        <label>文件 URL（如PDF）</label>
                                        <input className="form-input" value={formData.fileUrl} onChange={(e) => updateForm(f => ({ ...f, fileUrl: e.target.value }))} placeholder="https://..." />
                                    </div>
                                )}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>排序顺序</label>
                                        <input type="number" className="form-input" value={formData.sortOrder} onChange={(e) => updateForm(f => ({ ...f, sortOrder: parseInt(e.target.value, 10) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-inline">
                                        <label>
                                            <input type="checkbox" checked={formData.isPublished} onChange={(e) => updateForm(f => ({ ...f, isPublished: e.target.checked }))} />
                                            发布
                                        </label>
                                    </div>
                                </div>
                            </fieldset>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={dismissEditor} disabled={editorSaving}>取消</button>
                                <button className="btn-primary" onClick={handleSave} disabled={editorMode === 'loading' || !!actionLoading}>
                                    {editorSaving ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default UtilityTools;
