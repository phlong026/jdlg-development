import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiCheck, FiX, FiImage } from 'react-icons/fi';
import { API_BASE_URL, API_ENDPOINTS } from '../config/api';
import {
    createInspiration,
    deleteInspiration,
    getInspirationDetail,
    getInspirationList,
    updateInspiration,
    updateInspirationStatus,
    type InspirationItem,
} from '../api/inspirations';
import { tokenUtils } from '../utils/token';
import './inspirations.scss';

type TopicValue = 'scene' | 'style' | 'villa' | 'inspiration';
type StatusValue = 'published' | 'pending' | 'draft' | 'archived';

interface Inspiration {
    id: string;
    title: string;
    description: string;
    coverImage: string;
    imageUrls: string[];
    imageWidth?: number;
    imageHeight?: number;
    tags: string[];
    scene: string;
    style: string;
    topic: TopicValue;
    sortOrder: number;
    status: StatusValue;
    creator: string;
    creatorUserId?: number;
    viewCount: number;
    likeCount: number;
    createdAt: string;
    updatedAt: string;
}

const defaultForm = {
    title: '',
    description: '',
    scene: '',
    style: '',
    topic: 'inspiration' as TopicValue,
    tagsInput: '',
    imageUrls: [] as string[],
    sortOrder: 0,
    status: 'draft' as StatusValue,
};

const topicOptions: Array<{ label: string; value: TopicValue }> = [
    { label: '场景', value: 'scene' },
    { label: '风格', value: 'style' },
    { label: '乡墅', value: 'villa' },
    { label: '灵感', value: 'inspiration' },
];

const statusOptions: Array<{ label: string; value: StatusValue | 'all' }> = [
    { label: '全部状态', value: 'all' },
    { label: '待审核', value: 'pending' },
    { label: '已发布', value: 'published' },
    { label: '草稿', value: 'draft' },
    { label: '已归档', value: 'archived' },
];

const normalizePositiveNumber = (value?: number) => {
    const num = Number(value || 0);
    return Number.isFinite(num) && num > 0 ? Math.round(num) : 0;
};

const gcd = (left: number, right: number): number => {
    let a = Math.abs(left);
    let b = Math.abs(right);
    while (b !== 0) {
        const temp = a % b;
        a = b;
        b = temp;
    }
    return a || 1;
};

const formatImageMeta = (width?: number, height?: number) => {
    const normalizedWidth = normalizePositiveNumber(width);
    const normalizedHeight = normalizePositiveNumber(height);
    if (!normalizedWidth || !normalizedHeight) {
        return '未识别';
    }
    const divisor = gcd(normalizedWidth, normalizedHeight);
    return `${normalizedWidth / divisor}:${normalizedHeight / divisor} / ${normalizedWidth}×${normalizedHeight}`;
};

const convertInspiration = (item: InspirationItem): Inspiration => ({
    id: String(item.id),
    title: item.title,
    description: item.description || '',
    coverImage: item.cover_image || item.images?.[0] || '',
    imageUrls: Array.isArray(item.images) ? item.images : [],
    imageWidth: item.image_width,
    imageHeight: item.image_height,
    tags: Array.isArray(item.tags) ? item.tags : [],
    scene: item.scene || '',
    style: item.style || '',
    topic: (item.topic || 'inspiration') as TopicValue,
    sortOrder: item.sort_order || 0,
    status: (item.status || 'draft') as StatusValue,
    creator: item.creator || '',
    creatorUserId: item.creator_user_id,
    viewCount: item.view_count || 0,
    likeCount: item.like_count || 0,
    createdAt: item.created_at,
    updatedAt: item.updated_at,
});

const Inspirations: React.FC = () => {
    const [list, setList] = useState<Inspiration[]>([]);
    const [loading, setLoading] = useState(true);
    const [searchKeyword, setSearchKeyword] = useState('');
    const [statusFilter, setStatusFilter] = useState<'all' | StatusValue>('all');
    const [topicFilter, setTopicFilter] = useState<'all' | TopicValue>('all');
    const [showAddModal, setShowAddModal] = useState(false);
    const [showEditModal, setShowEditModal] = useState(false);
    const [selectedItem, setSelectedItem] = useState<Inspiration | null>(null);
    const [formData, setFormData] = useState(defaultForm);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [uploading, setUploading] = useState(false);

    const [page, setPage] = useState(1);
    const pageSize = 20;
    const [total, setTotal] = useState<number | null>(null);
    const [listError, setListError] = useState('');
    const [listActionError, setListActionError] = useState('');
    const [loadedScope, setLoadedScope] = useState<string | null>(null);
    const [editorLoading, setEditorLoading] = useState(false);
    const [editorError, setEditorError] = useState('');
    const [formError, setFormError] = useState('');
    const [uploadNotice, setUploadNotice] = useState('');
    const mounted = useRef(true);
    const listVersion = useRef(0);
    const detailVersion = useRef(0);
    const editorSession = useRef(0);
    const editorTarget = useRef<string | null>(null);
    const editorReady = useRef(false);
    const uploadSession = useRef<number | null>(null);
    const uploadInputRef = useRef<HTMLInputElement>(null);
    const writePending = useRef(false);
    const formRef = useRef(formData);
    const scope = JSON.stringify([page, searchKeyword, statusFilter, topicFilter]);
    const scopeRef = useRef(scope);
    const ready = loadedScope === scope && !loading && !listError;
    const listContext = useRef({ ready, list });
    const renderSession = editorSession.current;
    scopeRef.current = scope;
    formRef.current = formData;
    listContext.current = { ready, list };

    const invalidateList = () => {
        listVersion.current += 1;
        listContext.current = { ready: false, list: [] };
        setLoadedScope(null);
        setListError('');
        setListActionError('');
        setLoading(true);
    };
    const loadInspirations = async () => {
        if (!mounted.current || scopeRef.current !== scope) return;
        invalidateList();
        const version = listVersion.current;
        const isCurrent = () => mounted.current && version === listVersion.current && scopeRef.current === scope;
        try {
            const data = await getInspirationList({
                page, page_size: pageSize, keyword: searchKeyword || undefined,
                status: statusFilter === 'all' ? undefined : statusFilter,
                topic: topicFilter === 'all' ? undefined : topicFilter,
            });
            if (!isCurrent()) return;
            if (!data || (data.list !== null && !Array.isArray(data.list))) throw new Error('灵感列表响应无效');
            const resultTotal = Number.isFinite(data.total) && data.total >= 0 ? data.total : null;
            if (resultTotal !== null && page > Math.max(1, Math.ceil(resultTotal / pageSize))) {
                invalidateList();
                setPage(Math.max(1, Math.ceil(resultTotal / pageSize)));
                return;
            }
            setList((data.list || []).map(convertInspiration));
            setTotal(resultTotal);
            setLoadedScope(scope);
            reportPageLoadRecovered('加载灵感素材失败');
        } catch (error) {
            if (!isCurrent()) return;
            setListError(error instanceof Error ? error.message : '请重试');
            reportPageLoadError(error, '加载灵感素材失败');
        } finally {
            if (isCurrent()) setLoading(false);
        }
    };
    const reloadList = useRef(loadInspirations);
    reloadList.current = loadInspirations;
    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            listVersion.current += 1;
            detailVersion.current += 1;
            editorSession.current += 1;
            editorTarget.current = null;
        };
    }, []);
    useEffect(() => { void loadInspirations(); }, [page, searchKeyword, statusFilter, topicFilter]);

    const stats = useMemo(() => {
        const pending = list.filter(item => item.status === 'pending').length;
        const published = list.filter(item => item.status === 'published').length;
        return {
            total: list.length,
            pending,
            published,
        };
    }, [list]);

    const getTopicLabel = (topic: string) => topicOptions.find(option => option.value === topic)?.label || '灵感';

    const getStatusLabel = (status: StatusValue) => {
        const map: Record<StatusValue, { label: string; className: string }> = {
            published: { label: '已发布', className: 'status-published' },
            pending: { label: '待审核', className: 'status-pending' },
            draft: { label: '草稿', className: 'status-draft' },
            archived: { label: '已归档', className: 'status-archived' },
        };
        return map[status];
    };

    const replaceForm = (next: typeof defaultForm) => {
        formRef.current = next;
        setFormData(next);
    };
    const updateForm = (change: (previous: typeof defaultForm) => typeof defaultForm) => {
        if (!mounted.current || renderSession !== editorSession.current || !editorTarget.current || !editorReady.current || writePending.current) return;
        replaceForm(change(formRef.current));
    };
    const startEditor = (target: string | null) => {
        const session = ++editorSession.current;
        detailVersion.current += 1;
        editorTarget.current = target;
        editorReady.current = target === 'add';
        uploadSession.current = null;
        setUploading(false);
        setUploadNotice('');
        setEditorError('');
        setFormError('');
        setEditorLoading(false);
        setSelectedItem(null);
        replaceForm({ ...defaultForm, imageUrls: [] });
        setShowAddModal(target === 'add');
        setShowEditModal(target !== null && target !== 'add');
        return session;
    };
    const closeEditor = (session: number) => {
        if (!mounted.current || session !== editorSession.current) return;
        startEditor(null);
    };
    const resetModal = () => closeEditor(renderSession);
    const handleAdd = () => { if (mounted.current) startEditor('add'); };
    const loadEditorDetail = async (id: string, session: number) => {
        if (!mounted.current || editorSession.current !== session || editorTarget.current !== id) return;
        const version = ++detailVersion.current;
        const isCurrent = () => mounted.current && editorSession.current === session && editorTarget.current === id && detailVersion.current === version;
        editorReady.current = false;
        setEditorLoading(true);
        setEditorError('');
        try {
            const detail = await getInspirationDetail(id);
            if (!isCurrent()) return;
            if (!detail || String(detail.id) !== id) throw new Error('详情与所选素材不一致');
            const converted = convertInspiration(detail);
            setSelectedItem(converted);
            replaceForm({
                title: converted.title, description: converted.description, scene: converted.scene,
                style: converted.style, topic: converted.topic, tagsInput: converted.tags.join('，'),
                imageUrls: [...converted.imageUrls], sortOrder: converted.sortOrder, status: converted.status,
            });
            editorReady.current = true;
        } catch (error) {
            if (isCurrent()) setEditorError(`获取素材 ${id} 详情失败：${error instanceof Error ? error.message : '请重试'}`);
        } finally {
            if (isCurrent()) setEditorLoading(false);
        }
    };
    const handleEdit = (item: Inspiration) => {
        if (!mounted.current || scopeRef.current !== scope || !listContext.current.ready || !listContext.current.list.some(row => row.id === item.id)) return;
        const session = startEditor(item.id);
        void loadEditorDetail(item.id, session);
    };

    const openImagePicker = () => {
        if (!mounted.current || renderSession !== editorSession.current || !editorTarget.current || !editorReady.current || uploadSession.current !== null || writePending.current || formRef.current.imageUrls.length >= 9) return;
        uploadInputRef.current?.click();
    };

    const uploadImages = async (files: File[]) => {
        const session = renderSession;
        if (!files.length || !mounted.current || session !== editorSession.current || !editorTarget.current || !editorReady.current || uploadSession.current !== null || writePending.current) return;
        const remaining = Math.max(0, 9 - formRef.current.imageUrls.length);
        if (!remaining) { setFormError('最多 9 张图片，请先删除不需要的图片。'); return; }
        const accepted = files.slice(0, remaining);
        const token = tokenUtils.getToken();
        if (!token) { setFormError('请先登录后台'); return; }
        uploadSession.current = session;
        setUploading(true);
        setFormError('');
        setUploadNotice(files.length > remaining ? `最多 9 张，本次仅上传前 ${accepted.length} 张，其余未上传。` : '');
        const isCurrent = () => mounted.current && session === editorSession.current && uploadSession.current === session;
        let completed = 0;
        try {
            for (const file of accepted) {
                if (!isCurrent()) return;
                const form = new FormData();
                form.append('file', file);
                const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.UPLOAD}`, {
                    method: 'POST', headers: { Authorization: `Bearer ${token}` }, credentials: 'include', body: form,
                });
                const data = await response.json();
                if (!isCurrent()) return;
                if (!response.ok || data.code !== 0 || !data.data?.url) throw new Error(data.msg || '上传图片失败');
                // Merge into this editor's latest draft, preserving text edits and image removals.
                replaceForm({ ...formRef.current, imageUrls: [...formRef.current.imageUrls, data.data.url].slice(0, 9) });
                completed += 1;
            }
        } catch (error) {
            if (isCurrent()) setFormError(`上传失败：${error instanceof Error ? error.message : '请重试'}。本次已成功 ${completed} 张，已保留草稿及成功图片，可重新选择剩余图片。`);
        } finally {
            if (isCurrent()) { uploadSession.current = null; setUploading(false); }
        }
    };

    const handleSave = async () => {
        const session = renderSession;
        const target = editorTarget.current;
        if (!mounted.current || session !== editorSession.current || !target || !editorReady.current || writePending.current || uploadSession.current !== null) return;
        const draft = formRef.current;
        if (!draft.title.trim()) { setFormError('请填写标题'); return; }
        if (!draft.imageUrls.length) { setFormError('请至少上传 1 张图片'); return; }
        if (draft.imageUrls.length > 9) { setFormError('最多 9 张图片，请先删除多余图片。'); return; }
        const payload = {
            title: draft.title.trim(), description: draft.description.trim(), scene: draft.scene.trim(), style: draft.style.trim(),
            topic: draft.topic, tags: draft.tagsInput.split(/[，,\n]/).map(item => item.trim()).filter((item, index, all) => Boolean(item) && all.indexOf(item) === index).slice(0, 12),
            images: [...draft.imageUrls], sort_order: draft.sortOrder, status: draft.status,
        };
        writePending.current = true;
        setActionLoading(target);
        setFormError('');
        try {
            if (target === 'add') await createInspiration(payload);
            else await updateInspiration(target, payload);
            if (!mounted.current) return;
            closeEditor(session);
            void reloadList.current();
        } catch (error) {
            if (mounted.current && editorSession.current === session) setFormError(`保存失败：${error instanceof Error ? error.message : '请重试'}。草稿已保留。`);
        } finally {
            writePending.current = false;
            if (mounted.current) setActionLoading(null);
        }
    };

    const mutateItem = async (item: Inspiration, status?: StatusValue) => {
        const current = listContext.current.list.find(row => row.id === item.id);
        if (!mounted.current || writePending.current || editorTarget.current || !listContext.current.ready || scopeRef.current !== scope || !current || (status && current.status !== 'pending')) return;
        const target = { id: current.id, title: current.title };
        writePending.current = true;
        try {
            if (!status && !window.confirm(`确定删除「${target.title}」（ID: ${target.id}）吗？`)) return;
            if (!mounted.current || editorTarget.current || scopeRef.current !== scope || !listContext.current.ready) return;
            setActionLoading(target.id + (status || ''));
            setListActionError('');
            if (status) await updateInspirationStatus(target.id, status);
            else await deleteInspiration(target.id);
            if (mounted.current) void reloadList.current();
        } catch (error) {
            if (mounted.current && scopeRef.current === scope && listContext.current.ready && listContext.current.list.some(row => row.id === target.id)) setListActionError(`「${target.title}」（ID: ${target.id}）操作失败：${error instanceof Error ? error.message : '请重试'}`);
        } finally {
            writePending.current = false;
            if (mounted.current) setActionLoading(null);
        }
    };
    const handleStatus = (item: Inspiration, status: StatusValue) => mutateItem(item, status);
    const handleDelete = (item: Inspiration) => mutateItem(item);

    return (
        <>
            <div className="inspirations-container">
                <div className="inspirations-stats">
                    <div className="stat-item"><span className="stat-label">本页素材</span><span className="stat-value">{ready ? stats.total : '—'}</span></div>
                    <div className="stat-item"><span className="stat-label">本页待审核</span><span className="stat-value">{ready ? stats.pending : '—'}</span></div>
                    <div className="stat-item"><span className="stat-label">本页已发布</span><span className="stat-value">{ready ? stats.published : '—'}</span></div>
                </div>

                <div className="inspirations-toolbar">
                    <div className="toolbar-left">
                        <div className="search-box">
                            <FiSearch className="search-icon" />
                            <input className="search-input" placeholder="搜索标题、描述、场景或风格" aria-label="搜索灵感" value={searchKeyword} onChange={(e) => { if (e.target.value === searchKeyword && page === 1) return; invalidateList(); setPage(1); setSearchKeyword(e.target.value); }} />
                        </div>
                        <div className="filters">
                            <select className="filter-select" aria-label="灵感话题" value={topicFilter} onChange={(e) => { if (e.target.value === topicFilter && page === 1) return; invalidateList(); setPage(1); setTopicFilter(e.target.value as 'all' | TopicValue); }}>
                                <option value="all">全部话题</option>
                                {topicOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                            </select>
                            <select className="filter-select" aria-label="灵感状态" value={statusFilter} onChange={(e) => { if (e.target.value === statusFilter && page === 1) return; invalidateList(); setPage(1); setStatusFilter(e.target.value as 'all' | StatusValue); }}>
                                {statusOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                            </select>
                        </div>
                    </div>
                    <div className="toolbar-actions">
                        <button className="btn-secondary" onClick={() => void loadInspirations()} disabled={loading}>刷新列表</button>
                        <button className="btn-primary" onClick={handleAdd}><FiPlus />新增灵感</button>
                    </div>
                </div>

                {listActionError && <div className="inspiration-error" role="alert">{listActionError}</div>}
                {listError ? <div className="inspiration-error" role="alert">灵感列表加载失败：{listError}<button className="btn-secondary" onClick={() => void loadInspirations()}>重新加载列表</button></div> : !ready ? <div className="loading-state" role="status">加载中...</div> : (
                    <div className="inspiration-list">
                        {list.length === 0 ? <div className="empty-state">暂无灵感素材</div> : list.map(item => {
                            const status = getStatusLabel(item.status);
                            return (
                                <div key={item.id} className="inspiration-item">
                                    <div className="inspiration-cover">
                                        {item.coverImage ? <img src={item.coverImage} alt={item.title} /> : <div className="cover-empty"><FiImage /></div>}
                                    </div>
                                    <div className="inspiration-content">
                                        <div className="item-header">
                                            <h4>{item.title}</h4>
                                            <span className={`status-badge ${status.className}`}>{status.label}</span>
                                            <span>尺寸：{formatImageMeta(item.imageWidth, item.imageHeight)}</span>
                                        </div>
                                        <div className="item-meta">
                                            <span>{getTopicLabel(item.topic)}</span>
                                            {item.scene && <span>场景：{item.scene}</span>}
                                            {item.style && <span>风格：{item.style}</span>}
                                            <span>浏览：{item.viewCount}</span>
                                        </div>
                                        <div className="tag-list">
                                            {item.tags.map(tag => <span key={tag} className="tag-item">{tag}</span>)}
                                        </div>
                                        <p className="item-description">{item.description || '暂无描述'}</p>
                                        <div className="item-footer">
                                            <span className="creator-text">投稿人：{item.creator || '未知'}</span>
                                            <div className="item-actions">
                                                {item.status === 'pending' && <button className="btn-action btn-approve" disabled={Boolean(actionLoading)} onClick={() => handleStatus(item, 'published')}><FiCheck size={14} />通过</button>}
                                                {item.status === 'pending' && <button className="btn-action btn-reject" disabled={Boolean(actionLoading)} onClick={() => handleStatus(item, 'draft')}><FiX size={14} />驳回</button>}
                                                <button className="btn-action btn-edit" aria-label={`编辑灵感 ${item.id}`} onClick={() => handleEdit(item)}><FiEdit size={14} /></button>
                                                <button className="btn-action btn-delete" aria-label={`删除灵感 ${item.id}`} disabled={Boolean(actionLoading)} onClick={() => handleDelete(item)}><FiTrash2 size={14} /></button>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}

                {ready && <div className="inspiration-pagination" aria-label="灵感分页">
                    <span>{total === null ? `本页 ${list.length} 条，总数未返回` : `当前筛选共 ${total} 条`} · 第 {page} 页{total !== null ? ` / ${Math.max(1, Math.ceil(total / pageSize))}` : ''} · 搜索与筛选由服务端执行</span>
                    <button className="btn-secondary" disabled={page <= 1} onClick={() => { invalidateList(); setPage(page - 1); }}>上一页</button>
                    <button className="btn-secondary" disabled={total === null || page >= Math.ceil(total / pageSize)} onClick={() => { invalidateList(); setPage(page + 1); }}>下一页</button>
                </div>}

                {(showAddModal || showEditModal) && (
                    <div className="modal-overlay" onClick={resetModal}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{showEditModal ? '编辑灵感素材' : '新增灵感素材'}</h3>
                                <button className="modal-close" onClick={resetModal}>✕</button>
                            </div>
                            <div className="modal-body">
                                {editorLoading ? <div role="status">素材详情加载中...</div> : editorError ? <div className="inspiration-error" role="alert">{editorError}<button className="btn-secondary" onClick={() => { if (editorTarget.current && editorTarget.current !== 'add') void loadEditorDetail(editorTarget.current, renderSession); }}>重新加载详情</button></div> : null}
                                {formError && <div className="inspiration-error" role="alert">{formError}</div>}
                                {uploadNotice && <div className="upload-hint" role="status">{uploadNotice}</div>}
                                <fieldset className="inspiration-editor-fields" disabled={editorLoading || Boolean(editorError) || Boolean(actionLoading)}>
                                <div className="form-group">
                                    <label>标题 *</label>
                                    <input className="form-input" value={formData.title} onChange={(e) => updateForm(prev => ({ ...prev, title: e.target.value }))} placeholder="灵感标题" />
                                </div>
                                <div className="form-group">
                                    <label>描述</label>
                                    <textarea className="form-input" rows={4} value={formData.description} onChange={(e) => updateForm(prev => ({ ...prev, description: e.target.value }))} placeholder="描述这组灵感的用途和亮点" />
                                </div>
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>话题</label>
                                        <select className="form-input" value={formData.topic} onChange={(e) => updateForm(prev => ({ ...prev, topic: e.target.value as TopicValue }))}>
                                            {topicOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>状态</label>
                                        <select className="form-input" value={formData.status} onChange={(e) => updateForm(prev => ({ ...prev, status: e.target.value as StatusValue }))}>
                                            <option value="draft">草稿</option>
                                            <option value="pending">待审核</option>
                                            <option value="published">已发布</option>
                                            <option value="archived">已归档</option>
                                        </select>
                                    </div>
                                </div>
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>场景</label>
                                        <input className="form-input" value={formData.scene} onChange={(e) => updateForm(prev => ({ ...prev, scene: e.target.value }))} placeholder="如：客厅 / 庭院" />
                                    </div>
                                    <div className="form-group">
                                        <label>风格</label>
                                        <input className="form-input" value={formData.style} onChange={(e) => updateForm(prev => ({ ...prev, style: e.target.value }))} placeholder="如：奶油 / 侘寂" />
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label>标签</label>
                                    <textarea className="form-input" rows={3} value={formData.tagsInput} onChange={(e) => updateForm(prev => ({ ...prev, tagsInput: e.target.value }))} placeholder="多个标签用逗号隔开" />
                                </div>
                                <div className="form-group">
                                    <label>图片</label>
                                    <div className="image-upload-group">
                                        <button type="button" className="btn-secondary upload-trigger" onClick={openImagePicker} disabled={uploading || Boolean(actionLoading) || editorLoading || Boolean(editorError) || formData.imageUrls.length >= 9}>
                                            选择图片并上传
                                        </button>
                                        <input ref={uploadInputRef} aria-label="上传灵感图片" type="file" multiple accept="image/*" hidden disabled={uploading || Boolean(actionLoading) || editorLoading || Boolean(editorError) || formData.imageUrls.length >= 9} onChange={(e) => { const files = Array.from(e.target.files || []); e.target.value = ''; void uploadImages(files); }} />
                                        <span className="upload-hint">最多 9 张，第一张作为封面</span>
                                    </div>
                                    {uploading && <div className="upload-hint">图片上传中...</div>}
                                    {selectedItem && <div className="image-size-tip">当前封面尺寸：{formatImageMeta(selectedItem.imageWidth, selectedItem.imageHeight)}</div>}
                                    {formData.imageUrls.length > 0 && (
                                        <div className="image-preview-list">
                                            {formData.imageUrls.map((url, index) => (
                                                <div key={url + index} className="image-preview-item">
                                                    <img src={url} alt={`图${index + 1}`} className="image-preview-thumb" />
                                                    <div className="image-preview-meta">
                                                        {index === 0 && <span className="badge-primary">封面</span>}
                                                        <button className="btn-action btn-delete" aria-label={`移除图片 ${index + 1}`} type="button" onClick={() => updateForm(prev => ({ ...prev, imageUrls: prev.imageUrls[index] === url ? prev.imageUrls.filter((_, i) => i !== index) : prev.imageUrls }))}>删除</button>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                                <div className="form-group">
                                    <label>排序值</label>
                                    <input type="number" className="form-input" value={formData.sortOrder} onChange={(e) => updateForm(prev => ({ ...prev, sortOrder: parseInt(e.target.value, 10) || 0 }))} />
                                </div>
                                </fieldset>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={resetModal}>取消</button>
                                <button className="btn-primary" onClick={handleSave} disabled={Boolean(actionLoading) || uploading || editorLoading || Boolean(editorError)}>{actionLoading ? '保存中...' : '保存'}</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default Inspirations;
