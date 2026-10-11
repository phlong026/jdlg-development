import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiLayers, FiHome, FiImage, FiCompass } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import {
    getAIToolList,
    getAIToolDetail,
    createAITool,
    updateAITool,
    deleteAITool,
    publishAITool,
    getAIToolDiff,
    getAIToolVersions,
    rollbackAITool,
    type AIToolItem,
    type AIToolCategory,
    type AIToolDiffItem,
    type AIToolVersion,
    type AIToolPresetReference,
    type AIToolStylePreset,
    type AIToolUpsertRequest,
} from '../api/aiTools';
import './aiTools.scss';

const defaultReference = (): AIToolPresetReference => ({
    id: '',
    name: '',
    description: '',
    image_url: '',
    prompt_suffix: '',
});

const defaultStylePreset = (): AIToolStylePreset => ({
    id: '',
    name: '',
    reference_mode: 'preview_only',
    preview_image_url: '',
    model_reference_image_url: '',
    image_url: '',
    prompt_suffix: '',
});

const defaultForm = () => ({
    code: '',
    name: '',
    category: 'architecture' as AIToolCategory,
    shortDescription: '',
    detailDescription: '',
    listCoverImage: '',
    detailBeforeImage: '',
    detailAfterImage: '',
    promptPlaceholder: '',
    defaultPrompt: '',
    uploadHint: '',
    showUsageTips: true,
    usageTipsTitle: '使用提示',
    usageTipsContent: '',
    sortOrder: 0,
    isPublished: true,
    isCommon: false,
    presetReferences: [] as AIToolPresetReference[],
    stylePresets: [] as AIToolStylePreset[],
    draftNote: '',
});

type ToolFormState = ReturnType<typeof defaultForm>;
type ToolImageField = 'listCoverImage' | 'detailBeforeImage' | 'detailAfterImage';
type StylePresetImageField = 'preview_image_url' | 'model_reference_image_url';
const MINIMAL_PRESENTATION_TOOL_CODE = 'masterplan-coloring';
const STYLE_REFERENCE_MODE_LABEL: Record<NonNullable<AIToolStylePreset['reference_mode']>, string> = {
    preview_only: '仅用户预览',
    preview_and_model: '用户预览 + AI引用',
};

const toolImageFieldLabelMap: Record<ToolImageField, string> = {
    listCoverImage: 'List cover',
    detailBeforeImage: 'Detail before',
    detailAfterImage: 'Detail after',
};

const toolImageFields: Array<{ field: ToolImageField; label: string; reuseFrom: ToolImageField[] }> = [
    {
        field: 'listCoverImage',
        label: toolImageFieldLabelMap.listCoverImage,
        reuseFrom: ['detailAfterImage', 'detailBeforeImage'],
    },
    {
        field: 'detailBeforeImage',
        label: toolImageFieldLabelMap.detailBeforeImage,
        reuseFrom: ['listCoverImage', 'detailAfterImage'],
    },
    {
        field: 'detailAfterImage',
        label: toolImageFieldLabelMap.detailAfterImage,
        reuseFrom: ['listCoverImage', 'detailBeforeImage'],
    },
];

const categoryLabelMap: Record<AIToolCategory, string> = {
    architecture: '建筑',
    interior: '室内',
    landscape: '景观',
    planning: '规划',
};

/** 近7日逐日调用迷你趋势图：纯 SVG 折线，数据来自列表接口的 daily_series_7d */
const UsageSpark: React.FC<{ series?: number[] }> = ({ series }) => {
    const points = (series ?? []).filter((n) => typeof n === 'number');
    if (points.length < 2) {
        return <span className="tool-spark-empty">—</span>;
    }
    const max = Math.max(1, ...points);
    const width = 84;
    const height = 22;
    const step = width / (points.length - 1);
    const path = points
        .map((value, index) => `${(index * step).toFixed(1)},${(height - 2 - (value / max) * (height - 4)).toFixed(1)}`)
        .join(' ');
    const rising = points[points.length - 1] >= points[0];
    return (
        <svg className="tool-spark" viewBox={`0 0 ${width} ${height}`} width={width} height={height} aria-label="近7日调用趋势">
            <polyline fill="none" stroke={rising ? '#2f8f6b' : '#c0392b'} strokeWidth="1.6" points={path} />
        </svg>
    );
};

const formatFailedRate = (rate?: number) => (rate == null ? '—' : `${((rate || 0) * 100).toFixed(1)}% 失败`);

const AITools: React.FC = () => {
    const [tools, setTools] = useState<AIToolItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [searchKeyword, setSearchKeyword] = useState('');
    const [categoryFilter, setCategoryFilter] = useState<string>('all');
    const [publishedFilter, setPublishedFilter] = useState<string>('all');
    const [total, setTotal] = useState(0);
    const [showAddModal, setShowAddModal] = useState(false);
    const [showEditModal, setShowEditModal] = useState(false);
    const [selectedTool, setSelectedTool] = useState<AIToolItem | null>(null);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [formData, setFormData] = useState<ToolFormState>(defaultForm());
    /** 列表排序：usage_7d 按近7日调用降序（默认，运营视角），sort_order 按后台配置顺序 */
    const [sortBy, setSortBy] = useState<'usage_7d' | 'sort_order'>('usage_7d');
    /** 发布确认弹窗（diff of 草稿 vs 线上） */
    const [diffTarget, setDiffTarget] = useState<{ tool: AIToolItem; items: AIToolDiffItem[]; draftNote: string } | null>(null);
    const [publishNote, setPublishNote] = useState('');
    const [publishing, setPublishing] = useState(false);
    /** 版本历史弹窗 */
    const [versionsTarget, setVersionsTarget] = useState<AIToolItem | null>(null);
    const [versions, setVersions] = useState<AIToolVersion[] | null>(null);
    const [rollingBackId, setRollingBackId] = useState<number | null>(null);

    const loadTools = useCallback(async () => {
        setLoading(true);
        try {
            const params: Record<string, any> = { page: 1, page_size: 100 };
            if (categoryFilter !== 'all') params.category = categoryFilter;
            if (searchKeyword) params.keyword = searchKeyword;
            if (publishedFilter !== 'all') params.is_published = publishedFilter === 'published';
            const res = await getAIToolList(params);
            setTools(res?.list || []);
            setTotal(res?.total || 0);
        } catch (error) {
            console.error('加载 AI 工具列表失败:', error);
            reportPageLoadError(error, '加载 AI 工具列表失败');
        } finally {
            setLoading(false);
        }
    }, [categoryFilter, publishedFilter, searchKeyword]);

    /** 展示排序：近7日调用（默认）或配置排序 */
    const sortedTools = useMemo(() => {
        const list = [...tools];
        if (sortBy === 'usage_7d') {
            list.sort((a, b) => (b.usage_7d ?? 0) - (a.usage_7d ?? 0) || (a.sort_order || 0) - (b.sort_order || 0));
        } else {
            list.sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0));
        }
        return list;
    }, [tools, sortBy]);

    /** 近7日零调用的已发布工具：运营下线/合并候选 */
    const zeroUsageCount = useMemo(() => tools.filter((tool) => tool.is_published && (tool.usage_7d ?? 0) === 0).length, [tools]);

    /** 有未发布修改（待发布）的工具数 */
    const pendingDraftCount = useMemo(() => tools.filter((tool) => tool.draft_differs).length, [tools]);

    /** 打开发布确认弹窗：拉取草稿与线上的差异 */
    const handleOpenDiff = async (tool: AIToolItem) => {
        try {
            const diff = await getAIToolDiff(tool.id);
            setPublishNote('');
            setDiffTarget({ tool, items: diff.items || [], draftNote: diff.draft_note || '' });
        } catch (error: any) {
            alert(`获取草稿差异失败: ${error?.message || '未知错误'}`);
        }
    };

    const handleConfirmPublish = async () => {
        if (!diffTarget) return;
        setPublishing(true);
        try {
            const result = await publishAITool(diffTarget.tool.id, publishNote.trim() || undefined);
            alert(`已发布 v${result.version_no}，线上即时生效。`);
            setDiffTarget(null);
            await loadTools();
        } catch (error: any) {
            alert(`发布失败: ${error?.message || '未知错误'}`);
        } finally {
            setPublishing(false);
        }
    };

    const handleOpenVersions = async (tool: AIToolItem) => {
        setVersionsTarget(tool);
        setVersions(null);
        try {
            const list = await getAIToolVersions(tool.id);
            setVersions(list);
        } catch (error: any) {
            console.error('加载版本历史失败:', error);
            setVersions([]);
        }
    };

    const handleRollback = async (tool: AIToolItem, version: AIToolVersion) => {
        if (!window.confirm(`回滚到 v${version.version_no}？\n\n线上将立即恢复为该版本配置；回滚本身也会生成新版本号并留痕。`)) {
            return;
        }
        setRollingBackId(version.id);
        try {
            const result = await rollbackAITool(tool.id, version.id);
            alert(`已回滚：新版本 v${result.version_no}（内容 = v${result.rolled_back_to}）。`);
            await handleOpenVersions(tool);
            await loadTools();
        } catch (error: any) {
            alert(`回滚失败: ${error?.message || '未知错误'}`);
        } finally {
            setRollingBackId(null);
        }
    };

    useEffect(() => {
        void loadTools();
    }, [loadTools]);

    const getCategoryLabel = (category: string) => categoryLabelMap[category as AIToolCategory] || category;

    const getCategoryIcon = (category: string) => {
        switch (category) {
            case 'architecture':
                return <FiHome size={20} />;
            case 'interior':
                return <FiLayers size={20} />;
            case 'landscape':
                return <FiImage size={20} />;
            case 'planning':
                return <FiCompass size={20} />;
            default:
                return <FiLayers size={20} />;
        }
    };

    const resetModal = () => {
        setShowAddModal(false);
        setShowEditModal(false);
        setSelectedTool(null);
        setFormData(defaultForm());
    };

    const fillFormByDetail = (detail: AIToolItem) => {
        setFormData({
            code: detail.code || '',
            name: detail.name || '',
            category: detail.category,
            shortDescription: detail.short_description || '',
            detailDescription: detail.detail_description || '',
            listCoverImage: detail.list_cover_image || '',
            detailBeforeImage: detail.detail_before_image || '',
            detailAfterImage: detail.detail_after_image || '',
            promptPlaceholder: detail.prompt_placeholder || '',
            defaultPrompt: detail.default_prompt || '',
            uploadHint: detail.upload_hint || '',
            showUsageTips: detail.show_usage_tips !== false,
            usageTipsTitle: detail.usage_tips_title || '使用提示',
            usageTipsContent: detail.usage_tips_content || '',
            sortOrder: detail.sort_order || 0,
            isPublished: detail.is_published ?? true,
            isCommon: detail.is_common ?? false,
            presetReferences: Array.isArray(detail.preset_references) ? detail.preset_references.map((item) => ({ ...item })) : [],
            stylePresets: Array.isArray(detail.style_presets) ? detail.style_presets.map((item) => ({ ...item })) : [],
            draftNote: (detail as any).draft_note || '',
        });
    };

    const isMinimalPresentationTool = String(formData.code || '').trim() === MINIMAL_PRESENTATION_TOOL_CODE;

    const handleEdit = async (tool: AIToolItem) => {
        try {
            const detail = await getAIToolDetail(String(tool.id));
            fillFormByDetail(detail);
            setSelectedTool(detail);
            setShowEditModal(true);
        } catch (error) {
            console.error(error);
            reportPageLoadError(error, '获取 AI 工具详情失败');
        }
    };

    const buildPayload = (): AIToolUpsertRequest => ({
        code: formData.code.trim(),
        name: formData.name.trim(),
        category: formData.category,
        short_description: formData.shortDescription.trim(),
        detail_description: formData.detailDescription.trim(),
        list_cover_image: formData.listCoverImage.trim(),
        detail_before_image: formData.detailBeforeImage.trim(),
        detail_after_image: formData.detailAfterImage.trim(),
        prompt_placeholder: formData.promptPlaceholder.trim(),
        default_prompt: formData.defaultPrompt.trim(),
        upload_hint: isMinimalPresentationTool ? '' : formData.uploadHint.trim(),
        show_usage_tips: formData.showUsageTips,
        usage_tips_title: formData.usageTipsTitle.trim(),
        usage_tips_content: formData.usageTipsContent.trim(),
        sort_order: Number(formData.sortOrder) || 0,
        is_published: formData.isPublished,
        is_common: formData.isCommon,
        preset_references: formData.presetReferences.map((item) => ({
            id: String(item.id || '').trim(),
            name: String(item.name || '').trim(),
            description: String(item.description || '').trim(),
            image_url: String(item.image_url || '').trim(),
            prompt_suffix: String(item.prompt_suffix || '').trim(),
        })).filter((item) => item.id || item.name || item.description || item.image_url || item.prompt_suffix),
        style_presets: isMinimalPresentationTool ? [] : formData.stylePresets.map((item) => {
            const legacyImageUrl = String(item.image_url || '').trim();
            const referenceMode: NonNullable<AIToolStylePreset['reference_mode']> = item.reference_mode
                ? (item.reference_mode === 'preview_only' ? 'preview_only' : 'preview_and_model')
                : (legacyImageUrl ? 'preview_and_model' : 'preview_only');
            const previewImageUrl = String(item.preview_image_url || legacyImageUrl).trim();
            const modelReferenceImageUrl = String(item.model_reference_image_url || (referenceMode === 'preview_and_model' ? legacyImageUrl : '')).trim();
            return {
                id: String(item.id || '').trim(),
                name: String(item.name || '').trim(),
                reference_mode: referenceMode,
                preview_image_url: previewImageUrl,
                model_reference_image_url: modelReferenceImageUrl,
                image_url: previewImageUrl,
                prompt_suffix: String(item.prompt_suffix || '').trim(),
            };
        }).filter((item) => item.id || item.name || item.preview_image_url || item.model_reference_image_url || item.prompt_suffix),
        note: formData.draftNote.trim(),
    });

    const validateForm = () => {
        if (!formData.code.trim()) {
            alert('请填写工具编码');
            return false;
        }
        if (!formData.name.trim()) {
            alert('请填写工具名称');
            return false;
        }
        if (!isMinimalPresentationTool) {
            for (const item of formData.stylePresets) {
                const name = String(item.name || item.id || '未命名风格卡片').trim();
                const legacyImageUrl = String(item.image_url || '').trim();
                const referenceMode = item.reference_mode
                    ? (item.reference_mode === 'preview_only' ? 'preview_only' : 'preview_and_model')
                    : (legacyImageUrl ? 'preview_and_model' : 'preview_only');
                const modelReferenceImageUrl = String(item.model_reference_image_url || (referenceMode === 'preview_and_model' ? legacyImageUrl : '')).trim();
                if (referenceMode === 'preview_and_model' && !modelReferenceImageUrl) {
                    alert(`风格卡片「${name}」选择“用户预览 + AI引用”时，请配置 AI引用图`);
                    return false;
                }
            }
        }
        return true;
    };

    const handleAdd = async () => {
        if (!validateForm()) return;
        setActionLoading('add');
        try {
            await createAITool(buildPayload());
            resetModal();
            alert('工具已创建，内容存为草稿' + (formData.isPublished ? '并已发布 v1（线上可见）' : '（未发布，线上不可见；发布后生效）'));
            await loadTools();
        } catch (error: any) {
            alert(error?.message || '创建失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleSaveEdit = async () => {
        if (!selectedTool) return;
        if (!validateForm()) return;
        setActionLoading(String(selectedTool.id));
        try {
            await updateAITool(String(selectedTool.id), buildPayload());
            resetModal();
            alert('已保存为草稿：线上配置未变化。要生效请在列表点「发布」。');
            await loadTools();
        } catch (error: any) {
            alert(error?.message || '保存失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleDelete = async (toolId: string) => {
        if (!window.confirm('确定要删除这个 AI 工具吗？此操作不可恢复！')) return;
        setActionLoading(toolId);
        try {
            await deleteAITool(toolId);
            await loadTools();
        } catch (error: any) {
            console.error('删除 AI 工具失败:', error);
            const errorMsg = error?.response?.data?.msg || error?.message || '删除失败';
            alert(errorMsg);
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

    const uploadImageFile = async (file?: File | null) => {
        if (!file) return '';
        const formData = new FormData();
        formData.append('file', file);

        const token = localStorage.getItem('token');
        const response = await fetch('/api/v1/management/oss/upload', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`,
            },
            body: formData,
        });

        const result = await response.json();
        if (result.code === 0 && result.data?.url) {
            return String(result.data.url);
        }
        throw new Error(result.msg || '未知错误');
    };

    const setImageFieldValue = (field: ToolImageField, value: string) => {
        setFormData((prev) => ({ ...prev, [field]: value }));
    };

    const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>, field: ToolImageField) => {
        try {
            const url = await uploadImageFile(e.target.files?.[0]);
            if (!url) return;
            setImageFieldValue(field, url);
        } catch (error: any) {
            console.error('上传图片失败:', error);
            alert('上传失败: ' + (error?.message || '未知错误'));
        }
    };

    const reuseImageField = (targetField: ToolImageField, sourceField: ToolImageField) => {
        const sourceValue = String(formData[sourceField] || '').trim();
        if (!sourceValue) {
            alert(`请先配置${toolImageFieldLabelMap[sourceField]}`);
            return;
        }
        setImageFieldValue(targetField, sourceValue);
    };

    const updateReference = (index: number, key: keyof AIToolPresetReference, value: string) => {
        setFormData((prev) => {
            const next = [...prev.presetReferences];
            next[index] = { ...next[index], [key]: value };
            return { ...prev, presetReferences: next };
        });
    };

    const updateStylePreset = (index: number, key: keyof AIToolStylePreset, value: string) => {
        setFormData((prev) => {
            const next = [...prev.stylePresets];
            next[index] = { ...next[index], [key]: value };
            return { ...prev, stylePresets: next };
        });
    };

    const handleReferenceImageUpload = async (e: React.ChangeEvent<HTMLInputElement>, index: number) => {
        try {
            const url = await uploadImageFile(e.target.files?.[0]);
            if (!url) return;
            updateReference(index, 'image_url', url);
        } catch (error: any) {
            console.error('上传兼容项图片失败:', error);
            alert('上传失败: ' + (error?.message || '未知错误'));
        }
    };

    const handleStylePresetImageUpload = async (e: React.ChangeEvent<HTMLInputElement>, index: number, key: StylePresetImageField) => {
        try {
            const url = await uploadImageFile(e.target.files?.[0]);
            if (!url) return;
            updateStylePreset(index, key, url);
        } catch (error: any) {
            console.error('上传风格卡片图片失败:', error);
            alert('上传失败: ' + (error?.message || '未知错误'));
        }
    };

    const renderToolImageField = (field: ToolImageField, label: string, reuseFrom: ToolImageField[]) => {
        const value = String(formData[field] || '');

        return (
            <div className="form-group" key={field}>
                <label>{label}</label>
                <input type="file" accept="image/*" onChange={(e) => handleImageUpload(e, field)} className="form-input" style={{ marginBottom: '8px' }} />
                <input className="form-input" value={value} onChange={(e) => setImageFieldValue(field, e.target.value)} placeholder="https://..." />
                <div className="image-field-toolbar">
                    <span className="image-field-tip">支持与其他图片字段共用同一张图</span>
                    <div className="image-field-actions">
                        {reuseFrom.map((sourceField) => (
                            <button
                                key={`${field}-${sourceField}`}
                                type="button"
                                className="btn-tertiary"
                                disabled={!String(formData[sourceField] || '').trim()}
                                onClick={() => reuseImageField(field, sourceField)}
                            >
                                复用{toolImageFieldLabelMap[sourceField]}
                            </button>
                        ))}
                    </div>
                </div>
                {value && (
                    <div className="image-preview">
                        <img src={value} alt={`${label}预览`} onError={(e) => (e.currentTarget.style.display = 'none')} />
                    </div>
                )}
            </div>
        );
    };

    void renderToolImageField;

    return (
        <>
            <div className="ai-tools-container">
                <ManagementSearchPanel
                    title="AI 工具检索与配置维护"
                    description="先找到目标工具，再维护默认提示词、风格卡片、用户预览图、AI引用图和上下线状态，避免每次都改代码发版。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={() => void loadTools()} disabled={loading}>刷新列表</button>
                            <button className="btn-primary" onClick={() => { setFormData(defaultForm()); setShowAddModal(true); }}>
                                <FiPlus />
                                添加工具
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
                                    placeholder="搜索工具名称、编码、短描述..."
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') handleSearch();
                                    }}
                                />
                            </div>
                            <select className="management-search-select" value={categoryFilter} onChange={(e) => setCategoryFilter(e.target.value)}>
                                <option value="all">全部分类</option>
                                <option value="architecture">建筑</option>
                                <option value="interior">室内</option>
                                <option value="landscape">景观</option>
                                <option value="planning">规划</option>
                            </select>
                            <select className="management-search-select" value={publishedFilter} onChange={(e) => setPublishedFilter(e.target.value)}>
                                <option value="all">全部状态</option>
                                <option value="published">已发布</option>
                                <option value="draft">未发布</option>
                            </select>
                            <select className="management-search-select" value={sortBy} onChange={(e) => setSortBy(e.target.value as 'usage_7d' | 'sort_order')}>
                                <option value="usage_7d">按近7日调用排序</option>
                                <option value="sort_order">按配置顺序排序</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索工具</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共 <strong>{total}</strong> 个 AI 工具
                                {pendingDraftCount > 0 ? <span className="tool-pending-draft"> · {pendingDraftCount} 个待发布</span> : null}
                                {zeroUsageCount > 0 ? <span className="tool-zero-usage"> · {zeroUsageCount} 个已发布但近7日零调用</span> : null}
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
                    <div className="ai-tools-list">
                        {sortedTools.length === 0 ? (
                            <div className="empty-state">暂无 AI 工具</div>
                        ) : (
                            sortedTools.map((tool) => {
                                const zeroUsage = tool.is_published && (tool.usage_7d ?? 0) === 0;
                                return (
                                <div key={tool.id} className={`ai-tool-item ${zeroUsage ? 'tool-item-zero' : ''}`}>
                                    <div className="tool-icon">{getCategoryIcon(tool.category)}</div>
                                    <div className="tool-content">
                                        <div className="tool-header">
                                            <div>
                                                <h4>{tool.name}</h4>
                                                <div className="tool-code">编码：{tool.code}</div>
                                            </div>
                                            <div className="tool-header-badges">
                                                {tool.is_common ? <span className="tool-badge common">常用</span> : null}
                                                <span className={`tool-status ${tool.is_published ? 'published' : 'draft'}`}>
                                                    {tool.is_published ? '已发布' : '未发布'}
                                                </span>
                                                {tool.draft_differs ? <span className="tool-badge pending-draft">有未发布修改</span> : null}
                                                {!tool.draft_differs && tool.has_draft ? <span className="tool-badge draft-clean">草稿</span> : null}
                                            </div>
                                        </div>
                                        <div className="tool-meta">
                                            <span className="tool-category">{getCategoryLabel(tool.category)}</span>
                                            <span>排序: {tool.sort_order || 0}</span>
                                            <span className="tool-usage-hl">近7日调用: {(tool.usage_7d ?? 0).toLocaleString()}</span>
                                            <span>{formatFailedRate(tool.failed_rate_7d)}</span>
                                            <span>贡献灵石: {(tool.stones_7d ?? 0).toLocaleString()}</span>
                                            <span>累计使用: {tool.usage_count || 0}</span>
                                        </div>
                                        <div className="tool-preview">
                                            <p>{tool.short_description || '暂无短描述'}</p>
                                            {tool.default_prompt ? <p className="tool-prompt">默认提示词：{tool.default_prompt}</p> : null}
                                        </div>
                                        <div className="tool-usage-foot">
                                            <UsageSpark series={tool.daily_series_7d} />
                                            {zeroUsage ? <span className="tool-zero-hint">近7日零调用 · 建议下线或合并</span> : null}
                                        </div>
                                    </div>
                                    <div className="tool-actions">
                                        <button className="btn-action btn-publish" onClick={() => void handleOpenDiff(tool)} disabled={!tool.has_draft}
                                            title={tool.has_draft ? '把当前草稿发布到线上' : '没有待发布的草稿'}>
                                            发布
                                        </button>
                                        <button className="btn-action btn-versions" onClick={() => void handleOpenVersions(tool)} title="版本历史与效果对比">
                                            版本
                                        </button>
                                        <button className="btn-action btn-edit" onClick={() => void handleEdit(tool)} title="编辑（保存进草稿）">
                                            <FiEdit size={14} />
                                        </button>
                                        <button className="btn-action btn-delete" onClick={() => void handleDelete(String(tool.id))} disabled={actionLoading === String(tool.id)} title="删除">
                                            <FiTrash2 size={14} />
                                        </button>
                                    </div>
                                </div>
                                );
                            })
                        )}
                    </div>
                )}

                {(showAddModal || showEditModal) && (
                    <div className="modal-overlay" onClick={resetModal}>
                        <div className="modal-content modal-form ai-tool-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{showEditModal ? '编辑 AI 工具' : '新增 AI 工具'}</h3>
                                <button className="modal-close" onClick={resetModal}>✕</button>
                            </div>
                            <div className="modal-body">
                                <div className="image-reuse-panel">
                                    <div className="image-reuse-title">图片复用快捷操作</div>
                                    <div className="image-reuse-grid">
                                        {toolImageFields.map((item) => (
                                            <div className="image-reuse-card" key={`reuse-${item.field}`}>
                                                <strong>给{item.label}快速填图</strong>
                                                <div className="image-field-actions">
                                                    {item.reuseFrom.map((sourceField) => (
                                                        <button
                                                            key={`${item.field}-${sourceField}`}
                                                            type="button"
                                                            className="btn-tertiary"
                                                            disabled={!String(formData[sourceField] || '').trim()}
                                                            onClick={() => reuseImageField(item.field, sourceField)}
                                                        >
                                                            复用{toolImageFieldLabelMap[sourceField]}
                                                        </button>
                                                    ))}
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                                <div className="form-grid two-columns">
                                    <div className="form-group">
                                        <label>工具编码 *</label>
                                        <input className="form-input" value={formData.code} onChange={(e) => setFormData((prev) => ({ ...prev, code: e.target.value }))} placeholder="如 plan-coloring" />
                                    </div>
                                    <div className="form-group">
                                        <label>工具名称 *</label>
                                        <input className="form-input" value={formData.name} onChange={(e) => setFormData((prev) => ({ ...prev, name: e.target.value }))} placeholder="如 户型填色" />
                                    </div>
                                </div>

                                <div className="form-grid three-columns">
                                    <div className="form-group">
                                        <label>分类 *</label>
                                        <select className="form-input" value={formData.category} onChange={(e) => setFormData((prev) => ({ ...prev, category: e.target.value as AIToolCategory }))}>
                                            <option value="architecture">建筑</option>
                                            <option value="interior">室内</option>
                                            <option value="landscape">景观</option>
                                            <option value="planning">规划</option>
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>排序</label>
                                        <input type="number" className="form-input" value={formData.sortOrder} onChange={(e) => setFormData((prev) => ({ ...prev, sortOrder: Number(e.target.value) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-checkboxes">
                                        <label><input type="checkbox" checked={formData.isPublished} onChange={(e) => setFormData((prev) => ({ ...prev, isPublished: e.target.checked }))} />发布</label>
                                        <label><input type="checkbox" checked={formData.isCommon} onChange={(e) => setFormData((prev) => ({ ...prev, isCommon: e.target.checked }))} />常用</label>
                                    </div>
                                </div>

                                <div className="form-group">
                                    <label>短描述</label>
                                    <input className="form-input" value={formData.shortDescription} onChange={(e) => setFormData((prev) => ({ ...prev, shortDescription: e.target.value }))} placeholder="列表卡片上的一句话描述" />
                                </div>
                                <div className="form-group">
                                    <label>详细描述</label>
                                    <textarea className="form-input" rows={4} value={formData.detailDescription} onChange={(e) => setFormData((prev) => ({ ...prev, detailDescription: e.target.value }))} placeholder="工具详情页介绍" />
                                </div>
                                <div className="form-group">
                                    <label>修改备注 <span className="form-help-inline">（写入版本记录，不进入生效内容）</span></label>
                                    <input className="form-input" value={formData.draftNote} onChange={(e) => setFormData((prev) => ({ ...prev, draftNote: e.target.value }))} placeholder="如：强化线稿表现（发布与回滚历史里可见）" />
                                </div>
                                <div className="form-group">
                                    <label>默认提示词</label>
                                    <textarea className="form-input" rows={5} value={formData.defaultPrompt} onChange={(e) => setFormData((prev) => ({ ...prev, defaultPrompt: e.target.value }))} placeholder="管理员配置的默认系统提示词" />
                                </div>
                                <div className="form-grid three-columns">
                                    <div className="form-group">
                                        <label>列表封面图</label>
                                        <input type="file" accept="image/*" onChange={(e) => handleImageUpload(e, 'listCoverImage')} className="form-input" style={{ marginBottom: '8px' }} />
                                        <input className="form-input" value={formData.listCoverImage} onChange={(e) => setFormData((prev) => ({ ...prev, listCoverImage: e.target.value }))} placeholder="https://..." />
                                        {formData.listCoverImage && (
                                            <div className="image-preview">
                                                <img src={formData.listCoverImage} alt="列表封面图预览" onError={(e) => (e.currentTarget.style.display = 'none')} />
                                            </div>
                                        )}
                                    </div>
                                    <div className="form-group">
                                        <label>详情前图</label>
                                        <input type="file" accept="image/*" onChange={(e) => handleImageUpload(e, 'detailBeforeImage')} className="form-input" style={{ marginBottom: '8px' }} />
                                        <input className="form-input" value={formData.detailBeforeImage} onChange={(e) => setFormData((prev) => ({ ...prev, detailBeforeImage: e.target.value }))} placeholder="https://..." />
                                        {formData.detailBeforeImage && (
                                            <div className="image-preview">
                                                <img src={formData.detailBeforeImage} alt="详情前图预览" onError={(e) => (e.currentTarget.style.display = 'none')} />
                                            </div>
                                        )}
                                    </div>
                                    <div className="form-group">
                                        <label>详情后图</label>
                                        <input type="file" accept="image/*" onChange={(e) => handleImageUpload(e, 'detailAfterImage')} className="form-input" style={{ marginBottom: '8px' }} />
                                        <input className="form-input" value={formData.detailAfterImage} onChange={(e) => setFormData((prev) => ({ ...prev, detailAfterImage: e.target.value }))} placeholder="https://..." />
                                        {formData.detailAfterImage && (
                                            <div className="image-preview">
                                                <img src={formData.detailAfterImage} alt="详情后图预览" onError={(e) => (e.currentTarget.style.display = 'none')} />
                                            </div>
                                        )}
                                    </div>
                                </div>
                                <div className="form-grid two-columns">
                                    <div className="form-group">
                                        <label>输入框占位文案</label>
                                        <input className="form-input" value={formData.promptPlaceholder} onChange={(e) => setFormData((prev) => ({ ...prev, promptPlaceholder: e.target.value }))} placeholder="提示用户补充输入什么" />
                                    </div>
                                    {!isMinimalPresentationTool ? (
                                        <div className="form-group">
                                            <label>上传提示文案</label>
                                            <input className="form-input" value={formData.uploadHint} onChange={(e) => setFormData((prev) => ({ ...prev, uploadHint: e.target.value }))} placeholder="上传图片时的说明文案" />
                                        </div>
                                    ) : null}
                                </div>

                                <div className="editor-section">
                                    <div className="editor-section-header">
                                        <div>
                                            <h4>使用提示模块</h4>
                                            <p>控制详情页是否显示“使用提示”，并支持后台直接修改标题和内容。</p>
                                        </div>
                                    </div>
                                    <div className="form-grid three-columns">
                                        <div className="form-group form-group-checkboxes">
                                            <label><input type="checkbox" checked={formData.showUsageTips} onChange={(e) => setFormData((prev) => ({ ...prev, showUsageTips: e.target.checked }))} />显示使用提示</label>
                                        </div>
                                        <div className="form-group" style={{ gridColumn: 'span 2' }}>
                                            <label>提示标题</label>
                                            <input className="form-input" value={formData.usageTipsTitle} onChange={(e) => setFormData((prev) => ({ ...prev, usageTipsTitle: e.target.value }))} placeholder="如 使用提示" />
                                        </div>
                                    </div>
                                    <div className="form-group">
                                        <label>提示内容</label>
                                        <textarea className="form-input" rows={5} value={formData.usageTipsContent} onChange={(e) => setFormData((prev) => ({ ...prev, usageTipsContent: e.target.value }))} placeholder={'每一行会在小程序里显示为一条提示\n例如：\n系统会自动带入默认提示词\n只需要补充你自己的要求'} />
                                    </div>
                                </div>

                                <div className="editor-section">
                                    <div className="editor-section-header">
                                        <div>
                                            <h4>旧版兼容项</h4>
                                            <p>仅用于兼容历史数据和旧链路。新配置请优先使用下方“风格卡片配置”；若风格卡片未配用户预览图，会按顺序回退到这里取图。</p>
                                        </div>
                                        <button className="btn-secondary" onClick={() => setFormData((prev) => ({ ...prev, presetReferences: [...prev.presetReferences, defaultReference()] }))}>
                                            <FiPlus />
                                            添加兼容项
                                        </button>
                                    </div>
                                    {formData.presetReferences.length === 0 ? <div className="section-empty">暂无旧版兼容项</div> : null}
                                    {formData.presetReferences.map((item, index) => (
                                        <div key={`preset-${index}`} className="nested-editor-card">
                                            <div className="nested-editor-toolbar">
                                                <strong>兼容项 {index + 1}</strong>
                                                <button className="btn-action btn-delete" onClick={() => setFormData((prev) => ({ ...prev, presetReferences: prev.presetReferences.filter((_, currentIndex) => currentIndex !== index) }))}>
                                                    <FiTrash2 size={14} />
                                                </button>
                                            </div>
                                            <div className="form-grid two-columns">
                                                <div className="form-group">
                                                    <label>ID *</label>
                                                    <input className="form-input" value={item.id || ''} onChange={(e) => updateReference(index, 'id', e.target.value)} placeholder="如 arch-ref-1" />
                                                </div>
                                                <div className="form-group">
                                                    <label>名称 *</label>
                                                    <input className="form-input" value={item.name || ''} onChange={(e) => updateReference(index, 'name', e.target.value)} placeholder="如 新中式外观参考" />
                                                </div>
                                            </div>
                                            <div className="form-group">
                                                <label>说明</label>
                                                <input className="form-input" value={item.description || ''} onChange={(e) => updateReference(index, 'description', e.target.value)} placeholder="给运营看的简短说明" />
                                            </div>
                                            <div className="form-group">
                                                <label>图片 URL</label>
                                                <input
                                                    type="file"
                                                    accept="image/*"
                                                    onChange={(e) => void handleReferenceImageUpload(e, index)}
                                                    className="form-input"
                                                    style={{ marginBottom: '8px' }}
                                                />
                                                <input className="form-input" value={item.image_url || ''} onChange={(e) => updateReference(index, 'image_url', e.target.value)} placeholder="https://..." />
                                                {item.image_url ? (
                                                    <div className="image-preview">
                                                        <img src={item.image_url} alt="兼容项预览" onError={(e) => (e.currentTarget.style.display = 'none')} />
                                                    </div>
                                                ) : null}
                                            </div>
                                            <div className="form-group">
                                                <label>提示词后缀</label>
                                                <textarea className="form-input" rows={3} value={item.prompt_suffix || ''} onChange={(e) => updateReference(index, 'prompt_suffix', e.target.value)} placeholder="旧链路选中该兼容项后叠加的提示词" />
                                            </div>
                                        </div>
                                    ))}
                                </div>

                                {!isMinimalPresentationTool ? (
                                    <div className="editor-section">
                                        <div className="editor-section-header">
                                            <div>
                                            <h4>风格卡片配置</h4>
                                            <p>这里是前台主配置入口。每个卡片可选择“仅用户预览”或“用户预览 + AI引用”，避免展示图被误传给模型分析。</p>
                                            </div>
                                            <button className="btn-secondary" onClick={() => setFormData((prev) => ({ ...prev, stylePresets: [...prev.stylePresets, defaultStylePreset()] }))}>
                                                <FiPlus />
                                                添加风格卡片
                                            </button>
                                        </div>
                                        {formData.stylePresets.length === 0 ? <div className="section-empty">暂无风格卡片</div> : null}
                                        {formData.stylePresets.map((item, index) => (
                                            <div key={`style-${index}`} className="nested-editor-card">
                                                <div className="nested-editor-toolbar">
                                                    <strong>风格卡片 {index + 1}</strong>
                                                    <button className="btn-action btn-delete" onClick={() => setFormData((prev) => ({ ...prev, stylePresets: prev.stylePresets.filter((_, currentIndex) => currentIndex !== index) }))}>
                                                        <FiTrash2 size={14} />
                                                    </button>
                                                </div>
                                                <div className="form-grid two-columns">
                                                    <div className="form-group">
                                                        <label>ID *</label>
                                                        <input className="form-input" value={item.id || ''} onChange={(e) => updateStylePreset(index, 'id', e.target.value)} placeholder="如 arch-style-1" />
                                                    </div>
                                                    <div className="form-group">
                                                        <label>名称 *</label>
                                                        <input className="form-input" value={item.name || ''} onChange={(e) => updateStylePreset(index, 'name', e.target.value)} placeholder="如 专业方案" />
                                                    </div>
                                                </div>
                                                <div className="form-group">
                                                    <label>模式</label>
                                                    <select
                                                        className="form-input"
                                                        value={item.reference_mode || 'preview_and_model'}
                                                        onChange={(e) => updateStylePreset(index, 'reference_mode', e.target.value)}
                                                    >
                                                        <option value="preview_only">{STYLE_REFERENCE_MODE_LABEL.preview_only}</option>
                                                        <option value="preview_and_model">{STYLE_REFERENCE_MODE_LABEL.preview_and_model}</option>
                                                    </select>
                                                    <div className="form-help">仅用户预览：图片只展示给用户；用户预览 + AI引用：生成时会把 AI 引用图传给模型分析。</div>
                                                </div>
                                                <div className="form-group">
                                                    <label>用户预览图 URL</label>
                                                    <input
                                                        type="file"
                                                        accept="image/*"
                                                        onChange={(e) => void handleStylePresetImageUpload(e, index, 'preview_image_url')}
                                                        className="form-input"
                                                        style={{ marginBottom: '8px' }}
                                                    />
                                                    <input
                                                        className="form-input"
                                                        value={item.preview_image_url || item.image_url || ''}
                                                        onChange={(e) => updateStylePreset(index, 'preview_image_url', e.target.value)}
                                                        placeholder="展示在小程序风格卡片中"
                                                    />
                                                    {(item.preview_image_url || item.image_url) ? (
                                                        <div className="image-preview">
                                                            <img src={item.preview_image_url || item.image_url} alt="用户预览图" onError={(e) => (e.currentTarget.style.display = 'none')} />
                                                        </div>
                                                    ) : null}
                                                </div>
                                                {item.reference_mode !== 'preview_only' ? (
                                                    <div className="form-group">
                                                        <label>AI引用图 URL</label>
                                                        <input
                                                            type="file"
                                                            accept="image/*"
                                                            onChange={(e) => void handleStylePresetImageUpload(e, index, 'model_reference_image_url')}
                                                            className="form-input"
                                                            style={{ marginBottom: '8px' }}
                                                        />
                                                        <input
                                                            className="form-input"
                                                            value={item.model_reference_image_url || item.image_url || ''}
                                                            onChange={(e) => updateStylePreset(index, 'model_reference_image_url', e.target.value)}
                                                            placeholder="生成时传给 AI 模型分析"
                                                        />
                                                        {(item.model_reference_image_url || item.image_url) ? (
                                                            <div className="image-preview">
                                                                <img src={item.model_reference_image_url || item.image_url} alt="AI引用图" onError={(e) => (e.currentTarget.style.display = 'none')} />
                                                            </div>
                                                        ) : null}
                                                    </div>
                                                ) : null}
                                                <div className="form-group">
                                                    <label>旧版图片 URL（只读兼容）</label>
                                                    <input className="form-input" value={item.image_url || ''} readOnly placeholder="历史 image_url，保存后会迁移为用户预览图/AI引用图" />
                                                </div>
                                                <div className="form-group">
                                                    <label>提示词后缀</label>
                                                    <textarea className="form-input" rows={3} value={item.prompt_suffix || ''} onChange={(e) => updateStylePreset(index, 'prompt_suffix', e.target.value)} placeholder="选中该风格后叠加的提示词" />
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                ) : null}
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={resetModal}>取消</button>
                                <button className="btn-primary" onClick={showEditModal ? () => void handleSaveEdit() : () => void handleAdd()} disabled={actionLoading === (selectedTool ? String(selectedTool.id) : 'add')}>
                                    {actionLoading === (selectedTool ? String(selectedTool.id) : 'add') ? '保存中...' : (showEditModal ? '保存草稿' : '创建（存为草稿）')}
                                </button>
                            </div>
                            {showEditModal && selectedTool ? (
                                <div className="modal-draft-hint">
                                    保存只写入草稿，线上配置不变；需要生效请在列表点「发布」，发布前会展示与线上的差异。
                                </div>
                            ) : null}
                        </div>
                    </div>
                )}

                {/* 发布确认弹窗：草稿 vs 线上差异 + 备注 */}
                {diffTarget && (
                    <div className="modal-overlay" onClick={() => setDiffTarget(null)}>
                        <div className="modal-content modal-form tool-diff-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>发布确认 · {diffTarget.tool.name}</h3>
                                <button className="modal-close" onClick={() => setDiffTarget(null)}>✕</button>
                            </div>
                            <div className="modal-body">
                                {diffTarget.items.length === 0 ? (
                                    <div className="diff-empty">草稿与线上配置一致，无需发布。</div>
                                ) : (
                                    <>
                                        <p className="diff-summary">以下 <b>{diffTarget.items.length}</b> 处将发布到线上（小程序即时生效）：</p>
                                        <table className="diff-table">
                                            <thead>
                                                <tr><th>项目</th><th>线上</th><th>草稿</th></tr>
                                            </thead>
                                            <tbody>
                                                {diffTarget.items.map((item) => (
                                                    <tr key={item.field}>
                                                        <td className="diff-label">
                                                            {item.label}
                                                            {item.kind === 'added' ? <em className="diff-kind added">新增</em> : null}
                                                            {item.kind === 'removed' ? <em className="diff-kind removed">移除</em> : null}
                                                        </td>
                                                        <td className="diff-before">{item.before || <span className="diff-none">（无）</span>}</td>
                                                        <td className="diff-after">{item.after || <span className="diff-none">（删除）</span>}</td>
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </table>
                                        <div className="form-group" style={{ marginTop: 12 }}>
                                            <label>发布备注</label>
                                            <input value={publishNote} onChange={(e) => setPublishNote(e.target.value)} placeholder="写入版本记录，如：v3 强化线稿" />
                                        </div>
                                    </>
                                )}
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setDiffTarget(null)}>取消</button>
                                <button className="btn-primary" onClick={() => void handleConfirmPublish()} disabled={publishing || diffTarget.items.length === 0}>
                                    {publishing ? '发布中…' : `确认发布（${diffTarget.items.length} 处变更）`}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 版本历史弹窗：版本链 + 发布前后效果对比 + 回滚 */}
                {versionsTarget && (
                    <div className="modal-overlay" onClick={() => setVersionsTarget(null)}>
                        <div className="modal-content modal-form tool-versions-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>版本历史 · {versionsTarget.name}</h3>
                                <button className="modal-close" onClick={() => setVersionsTarget(null)}>✕</button>
                            </div>
                            <div className="modal-body">
                                {versions === null ? (
                                    <div className="diff-empty">加载中…</div>
                                ) : versions.length === 0 ? (
                                    <div className="diff-empty">该工具还没有发布过版本（旧工具从下次发布起开始留版本）。</div>
                                ) : (
                                    <table className="diff-table versions-table">
                                        <thead>
                                            <tr><th>版本</th><th>发布时间</th><th>操作人 / 备注</th><th>发布前7日</th><th>发布后</th><th>操作</th></tr>
                                        </thead>
                                        <tbody>
                                            {versions.map((version, index) => (
                                                <tr key={version.id}>
                                                    <td><b>v{version.version_no}</b>{index === 0 ? <em className="diff-kind current">线上</em> : null}</td>
                                                    <td>{version.published_at ? new Date(version.published_at).toLocaleString('zh-CN') : '-'}</td>
                                                    <td>{version.created_by || '-'}<span className="diff-note">{version.note || ''}</span></td>
                                                    <td className="diff-stat">
                                                        {version.stats ? `${version.stats.before.total} 次 · ${version.stats.before.success_rate >= 0 ? (version.stats.before.success_rate * 100).toFixed(1) + '%' : '—'}` : '—'}
                                                    </td>
                                                    <td className="diff-stat">
                                                        {version.stats ? `${version.stats.after.total} 次 · ${version.stats.after.success_rate >= 0 ? (version.stats.after.success_rate * 100).toFixed(1) + '%' : '—'}` : '—'}
                                                    </td>
                                                    <td>
                                                        {index === 0 ? <span className="diff-none">当前线上</span> : (
                                                            <button className="btn-link rollback-link" onClick={() => void handleRollback(versionsTarget, version)} disabled={rollingBackId === version.id}>
                                                                {rollingBackId === version.id ? '回滚中…' : `回滚到此版`}
                                                            </button>
                                                        )}
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                )}
                                <div className="diff-empty small">效果对比口径：发布前 7 日 vs 本版发布至下版发布之间；「—」表示无样本。</div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-primary" onClick={() => setVersionsTarget(null)}>关闭</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default AITools;
