import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiCheck, FiX, FiImage } from 'react-icons/fi';
import Layout from '../component/layout';
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
    const [loading, setLoading] = useState(false);
    const [searchKeyword, setSearchKeyword] = useState('');
    const [statusFilter, setStatusFilter] = useState<'all' | StatusValue>('all');
    const [topicFilter, setTopicFilter] = useState<'all' | TopicValue>('all');
    const [showAddModal, setShowAddModal] = useState(false);
    const [showEditModal, setShowEditModal] = useState(false);
    const [selectedItem, setSelectedItem] = useState<Inspiration | null>(null);
    const [formData, setFormData] = useState(defaultForm);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [uploading, setUploading] = useState(false);

    const loadInspirations = useCallback(async () => {
        setLoading(true);
        try {
            const data = await getInspirationList({
                page: 1,
                page_size: 100,
                keyword: searchKeyword || undefined,
                status: statusFilter === 'all' ? undefined : statusFilter,
                topic: topicFilter === 'all' ? undefined : topicFilter,
            });
            setList((data?.list || []).map(convertInspiration));
        } catch (error) {
            console.error('加载灵感素材失败:', error);
            alert('加载灵感素材失败');
        } finally {
            setLoading(false);
        }
    }, [searchKeyword, statusFilter, topicFilter]);

    useEffect(() => {
        loadInspirations();
    }, [loadInspirations]);

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

    const resetModal = () => {
        setFormData(defaultForm);
        setSelectedItem(null);
        setShowAddModal(false);
        setShowEditModal(false);
    };

    const handleAdd = () => {
        setFormData(defaultForm);
        setShowAddModal(true);
    };

    const handleEdit = async (item: Inspiration) => {
        try {
            const detail = await getInspirationDetail(item.id);
            const converted = convertInspiration(detail);
            setSelectedItem(converted);
            setFormData({
                title: converted.title,
                description: converted.description,
                scene: converted.scene,
                style: converted.style,
                topic: converted.topic,
                tagsInput: converted.tags.join('，'),
                imageUrls: converted.imageUrls,
                sortOrder: converted.sortOrder,
                status: converted.status,
            });
            setShowEditModal(true);
        } catch (error) {
            console.error(error);
            alert('获取详情失败');
        }
    };

    const parseTags = () => formData.tagsInput
        .split(/[，,\n]/)
        .map(item => item.trim())
        .filter((item, index, arr) => Boolean(item) && arr.indexOf(item) === index)
        .slice(0, 12);

    const uploadImages = async (files: FileList | null) => {
        if (!files || !files.length) return;
        const token = tokenUtils.getToken();
        if (!token) {
            alert('请先登录后台');
            return;
        }
        setUploading(true);
        try {
            const urls = [...formData.imageUrls];
            for (const file of Array.from(files)) {
                const form = new FormData();
                form.append('file', file);
                const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.UPLOAD}`, {
                    method: 'POST',
                    headers: {
                        Authorization: `Bearer ${token}`,
                    },
                    credentials: 'include',
                    body: form,
                });
                const data = await response.json();
                if (!response.ok || data.code !== 0 || !data.data?.url) {
                    throw new Error(data.msg || '上传图片失败');
                }
                urls.push(data.data.url);
            }
            setFormData(prev => ({ ...prev, imageUrls: urls.slice(0, 9) }));
        } catch (error: any) {
            alert(error?.message || '上传图片失败');
        } finally {
            setUploading(false);
        }
    };

    const handleSave = async () => {
        if (!formData.title.trim()) {
            alert('请填写标题');
            return;
        }
        if (!formData.imageUrls.length) {
            alert('请至少上传 1 张图片');
            return;
        }
        const payload = {
            title: formData.title.trim(),
            description: formData.description.trim(),
            scene: formData.scene.trim(),
            style: formData.style.trim(),
            topic: formData.topic,
            tags: parseTags(),
            images: formData.imageUrls,
            sort_order: formData.sortOrder,
            status: formData.status,
        };
        setActionLoading(selectedItem?.id || 'add');
        try {
            if (showEditModal && selectedItem) {
                await updateInspiration(selectedItem.id, payload);
            } else {
                await createInspiration(payload);
            }
            resetModal();
            loadInspirations();
        } catch (error: any) {
            alert(error?.message || '保存失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleStatus = async (item: Inspiration, status: StatusValue) => {
        setActionLoading(item.id + status);
        try {
            await updateInspirationStatus(item.id, status);
            loadInspirations();
        } catch (error: any) {
            alert(error?.message || '操作失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleDelete = async (item: Inspiration) => {
        if (!window.confirm(`确定删除「${item.title}」吗？`)) return;
        setActionLoading(item.id);
        try {
            await deleteInspiration(item.id);
            loadInspirations();
        } catch (error: any) {
            alert(error?.message || '删除失败');
        } finally {
            setActionLoading(null);
        }
    };

    return (
        <Layout title="灵感素材审核">
            <div className="inspirations-container">
                <div className="inspirations-stats">
                    <div className="stat-item"><span className="stat-label">总数</span><span className="stat-value">{stats.total}</span></div>
                    <div className="stat-item"><span className="stat-label">待审核</span><span className="stat-value">{stats.pending}</span></div>
                    <div className="stat-item"><span className="stat-label">已发布</span><span className="stat-value">{stats.published}</span></div>
                </div>

                <div className="inspirations-toolbar">
                    <div className="toolbar-left">
                        <div className="search-box">
                            <FiSearch className="search-icon" />
                            <input className="search-input" placeholder="搜索标题、描述、场景或风格" value={searchKeyword} onChange={(e) => setSearchKeyword(e.target.value)} />
                        </div>
                        <div className="filters">
                            <select className="filter-select" value={topicFilter} onChange={(e) => setTopicFilter(e.target.value as 'all' | TopicValue)}>
                                <option value="all">全部话题</option>
                                {topicOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                            </select>
                            <select className="filter-select" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value as 'all' | StatusValue)}>
                                {statusOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                            </select>
                        </div>
                    </div>
                    <div className="toolbar-actions">
                        <button className="btn-primary" onClick={handleAdd}><FiPlus />新增灵感</button>
                    </div>
                </div>

                {loading ? <div className="loading-state">加载中...</div> : (
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
                                                {item.status === 'pending' && <button className="btn-action btn-approve" disabled={actionLoading === item.id + 'published'} onClick={() => handleStatus(item, 'published')}><FiCheck size={14} />通过</button>}
                                                {item.status === 'pending' && <button className="btn-action btn-reject" disabled={actionLoading === item.id + 'draft'} onClick={() => handleStatus(item, 'draft')}><FiX size={14} />驳回</button>}
                                                <button className="btn-action btn-edit" onClick={() => handleEdit(item)}><FiEdit size={14} /></button>
                                                <button className="btn-action btn-delete" disabled={actionLoading === item.id} onClick={() => handleDelete(item)}><FiTrash2 size={14} /></button>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}

                {(showAddModal || showEditModal) && (
                    <div className="modal-overlay" onClick={resetModal}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{showEditModal ? '编辑灵感素材' : '新增灵感素材'}</h3>
                                <button className="modal-close" onClick={resetModal}>✕</button>
                            </div>
                            <div className="modal-body">
                                <div className="form-group">
                                    <label>标题 *</label>
                                    <input className="form-input" value={formData.title} onChange={(e) => setFormData(prev => ({ ...prev, title: e.target.value }))} placeholder="灵感标题" />
                                </div>
                                <div className="form-group">
                                    <label>描述</label>
                                    <textarea className="form-input" rows={4} value={formData.description} onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))} placeholder="描述这组灵感的用途和亮点" />
                                </div>
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>话题</label>
                                        <select className="form-input" value={formData.topic} onChange={(e) => setFormData(prev => ({ ...prev, topic: e.target.value as TopicValue }))}>
                                            {topicOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>状态</label>
                                        <select className="form-input" value={formData.status} onChange={(e) => setFormData(prev => ({ ...prev, status: e.target.value as StatusValue }))}>
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
                                        <input className="form-input" value={formData.scene} onChange={(e) => setFormData(prev => ({ ...prev, scene: e.target.value }))} placeholder="如：客厅 / 庭院" />
                                    </div>
                                    <div className="form-group">
                                        <label>风格</label>
                                        <input className="form-input" value={formData.style} onChange={(e) => setFormData(prev => ({ ...prev, style: e.target.value }))} placeholder="如：奶油 / 侘寂" />
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label>标签</label>
                                    <textarea className="form-input" rows={3} value={formData.tagsInput} onChange={(e) => setFormData(prev => ({ ...prev, tagsInput: e.target.value }))} placeholder="多个标签用逗号隔开" />
                                </div>
                                <div className="form-group">
                                    <label>图片</label>
                                    <div className="image-upload-group">
                                        <label className="btn-secondary upload-trigger">
                                            选择图片并上传
                                            <input type="file" multiple accept="image/*" hidden onChange={(e) => uploadImages(e.target.files)} />
                                        </label>
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
                                                        <button className="btn-action btn-delete" type="button" onClick={() => setFormData(prev => ({ ...prev, imageUrls: prev.imageUrls.filter((_, i) => i !== index) }))}>删除</button>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                                <div className="form-group">
                                    <label>排序值</label>
                                    <input type="number" className="form-input" value={formData.sortOrder} onChange={(e) => setFormData(prev => ({ ...prev, sortOrder: parseInt(e.target.value, 10) || 0 }))} />
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={resetModal}>取消</button>
                                <button className="btn-primary" onClick={handleSave} disabled={Boolean(actionLoading)}>{actionLoading ? '保存中...' : '保存'}</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default Inspirations;
