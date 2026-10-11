import { reportPageLoadError } from '../utils/pageLoadError';
﻿import React, { useState, useEffect, useCallback, useRef } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiEye, FiCheck, FiX, FiFolder } from 'react-icons/fi';
import { API_BASE_URL, API_ENDPOINTS } from '../config/api';
import {
    getTemplateList,
    getTemplateDetail,
    createTemplate,
    updateTemplate,
    updateTemplateStatus,
    updateTemplateCategory,
    deleteTemplate,
    getTemplateCategories,
    createTemplateCategory,
    deleteTemplateCategory,
    getTemplateTabConfig,
    putTemplateTabConfig,
    setTemplateFeatured,
    getFeaturedCaseGroups,
    createFeaturedCaseGroup,
    updateFeaturedCaseGroup,
    deleteFeaturedCaseGroup,
    type TemplateItem as ApiTemplate,
    type TemplateUpdateRequest,
    type TemplateCategoryItem,
    type TabConfigItem,
    type FeaturedCaseGroup,
} from '../api/templates';
import { useLocation } from 'react-router-dom';
import './templates.scss';

const FiSearchIcon = FiSearch as unknown as React.ComponentType<any>;
const FiPlusIcon = FiPlus as unknown as React.ComponentType<any>;
const FiEditIcon = FiEdit as unknown as React.ComponentType<any>;
const FiTrash2Icon = FiTrash2 as unknown as React.ComponentType<any>;
const FiEyeIcon = FiEye as unknown as React.ComponentType<any>;
const FiCheckIcon = FiCheck as unknown as React.ComponentType<any>;
const FiXIcon = FiX as unknown as React.ComponentType<any>;
const FiFolderIcon = FiFolder as unknown as React.ComponentType<any>;

interface Template {
    id: string;
    name: string;
    category: string;
    // 标签归属。
    mainTab?: string;
    subTab?: string;
    thirdTab?: string;
    description: string;
    thumbnail?: string;
    previewUrl?: string;
    images?: string;
    imageWidth?: number;
    imageHeight?: number;
    price: number;
    isFree: boolean;
    isFeatured?: boolean;
    downloadCount: number;
    status: string;
    publishScope?: 'homepage_only' | 'square';
    rejectReason?: string;
    sourceType?: 'admin_upload' | 'ai_generated' | 'album_upload';
    creator: string;
    createdAt: string;
    updatedAt: string;
}

const defaultForm = {
    name: '',
    category: '',
    description: '',
    mainTab: '',
    subTab: '',
    thirdTab: '',
    // 上传图片 URL，首张用作缩略图与主预览图。
    imageUrls: [] as string[],
    price: 0,
    isFree: true,
    status: 'draft',
};

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

const isFeaturedDisplayMode = (value: string): value is 'comparison' | 'side_by_side' | 'normal' => ['comparison', 'side_by_side', 'normal'].includes(value);

type TabConfigSaveState = 'idle' | 'editing' | 'saving' | 'saved' | 'invalid' | 'error';

const getChildTabsByParent = (items: TabConfigItem[], parentValue: string) => {
    const currentParent = (parentValue || '').trim();
    if (!currentParent) {
        return [];
    }
    return items.filter(item => (item.parent || '').trim() === currentParent);
};

const Templates: React.FC = () => {
    const location = useLocation();
    const queryParams = new URLSearchParams(location.search);
    const initialKeyword = queryParams.get('keyword') || '';
    const requestedTemplateId = queryParams.get('templateId');
    const linkedTemplateReadRef = useRef(0);
    const linkedTargetRef = useRef(requestedTemplateId);
    if (linkedTargetRef.current !== requestedTemplateId) {
        linkedTargetRef.current = requestedTemplateId;
        linkedTemplateReadRef.current += 1;
    }
    const renderedLinkedRequest = linkedTemplateReadRef.current;
    const isCurrentLinkedTarget = (id: string | null) => id !== null && linkedTargetRef.current === id &&
        new URLSearchParams(window.location.search).get('templateId') === id;
    const [linkedStateTarget, setLinkedStateTarget] = useState<string | null>(null);
    const [linkedTemplate, setLinkedTemplate] = useState<Template | null>(null);
    const [linkedTemplateLoading, setLinkedTemplateLoading] = useState(false);
    const [linkedTemplateError, setLinkedTemplateError] = useState('');
    const [templates, setTemplates] = useState<Template[]>([]);
    const [categories, setCategories] = useState<TemplateCategoryItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState(initialKeyword);
    const [searchKeyword, setSearchKeyword] = useState(initialKeyword);
    const [categoryFilter, setCategoryFilter] = useState<string>('all');
    const [statusFilter, setStatusFilter] = useState<string>('all');
    const [templatePage, setTemplatePage] = useState(1);
    const [templateTotal, setTemplateTotal] = useState(0);
    const [templatesError, setTemplatesError] = useState('');
    const [templatesLoaded, setTemplatesLoaded] = useState(false);
    const templatePageSize = 20;
    const templateReadRef = useRef(0);
    const templateQueryRef = useRef({ page: templatePage, category: categoryFilter, status: statusFilter });
    templateQueryRef.current = { page: templatePage, category: categoryFilter, status: statusFilter };
    const lastTemplateQueryRef = useRef('');
    const [showAddModal, setShowAddModal] = useState(false);
    const [showEditModal, setShowEditModal] = useState(false);
    const [showCategoryModal, setShowCategoryModal] = useState(false);
    const [showMoveCategoryModal, setShowMoveCategoryModal] = useState(false);
    const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
    const [moveTargetTemplate, setMoveTargetTemplate] = useState<Template | null>(null);
    const moveSessionRef = useRef(0);
    const renderedMoveSession = moveSessionRef.current;
    const closeMoveCategory = () => {
        moveSessionRef.current += 1;
        setShowMoveCategoryModal(false);
        setMoveTargetTemplate(null);
    };
    const dismissMoveCategory = () => {
        if (renderedMoveSession === moveSessionRef.current) closeMoveCategory();
    };
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const templateWritesRef = useRef(new Set<string>());
    const beginTemplateWrite = (id: string) => {
        if (templateWritesRef.current.has(id)) return false;
        templateWritesRef.current.add(id);
        setActionLoading(id);
        return true;
    };
    const endTemplateWrite = (id: string) => {
        templateWritesRef.current.delete(id);
        setActionLoading(current => current === id ? null : current);
    };
    const [addFormData, setAddFormData] = useState<typeof defaultForm>(defaultForm);
    const [editFormData, setEditFormData] = useState<typeof defaultForm>(defaultForm);
    const editInitialFormRef = useRef<typeof defaultForm | null>(null);
    const addUploadRef = useRef<HTMLInputElement>(null);
    const editUploadRef = useRef<HTMLInputElement>(null);
    const editorSessionRef = useRef(0);
    const editorLinkedTargetRef = useRef<string | null>(null);
    const renderedEditorSession = editorSessionRef.current;
    const renderedEditorLinkedTarget = editorLinkedTargetRef.current;
    const savePendingRef = useRef(false);
    const [editorSaving, setEditorSaving] = useState(false);
    const uploadPendingRef = useRef(false);
    const [uploading, setUploading] = useState(false);

    const closeTemplateEditor = () => {
        editorSessionRef.current += 1;
        editorLinkedTargetRef.current = null;
        setShowAddModal(false);
        setShowEditModal(false);
        setSelectedTemplate(null);
        editInitialFormRef.current = null;
        uploadPendingRef.current = false;
        setUploading(false);
    };

    const isCurrentEditor = (mode: 'add' | 'edit') => renderedEditorSession === editorSessionRef.current &&
        (mode === 'add' ? showAddModal : showEditModal) &&
        (!renderedEditorLinkedTarget || isCurrentLinkedTarget(renderedEditorLinkedTarget));
    const dismissTemplateEditor = () => {
        if (renderedEditorSession === editorSessionRef.current) closeTemplateEditor();
    };
    const updateAddForm = (update: React.SetStateAction<typeof defaultForm>) => {
        if (isCurrentEditor('add')) setAddFormData(update);
    };
    const updateEditForm = (update: React.SetStateAction<typeof defaultForm>) => {
        if (isCurrentEditor('edit')) setEditFormData(update);
    };

    const handleImageUpload = async (event: React.ChangeEvent<HTMLInputElement>, mode: 'add' | 'edit') => {
        if (!isCurrentEditor(mode)) return;
        const input = event.currentTarget;
        const files = Array.from(input.files || []);
        input.value = '';
        if (!files.length || uploadPendingRef.current || savePendingRef.current) return;
        const session = editorSessionRef.current;
        uploadPendingRef.current = true;
        setUploading(true);
        const uploaded: string[] = [];
        try {
            for (const file of files) {
                if (session !== editorSessionRef.current) return;
                const body = new FormData();
                body.append('file', file);
                try {
                    const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.UPLOAD}`, {
                        method: 'POST', credentials: 'include', body,
                    });
                    const result = await response.json();
                    if (session !== editorSessionRef.current) return;
                    if (response.ok && result.code === 0 && result.data?.url) {
                        uploaded.push(result.data.url);
                    } else {
                        alert(result.msg || '上传失败');
                    }
                } catch (error: any) {
                    if (session !== editorSessionRef.current) return;
                    alert('上传失败: ' + (error?.message || '未知错误'));
                }
            }
            if (session === editorSessionRef.current && uploaded.length) {
                const setForm = mode === 'add' ? setAddFormData : setEditFormData;
                setForm(form => ({ ...form, imageUrls: [...form.imageUrls, ...uploaded] }));
            }
        } finally {
            if (session === editorSessionRef.current) {
                uploadPendingRef.current = false;
                setUploading(false);
            }
        }
    };
    const [categoryForm, setCategoryForm] = useState({ id: '', name: '', sort_order: 0 });
    const [categorySectionOpen, setCategorySectionOpen] = useState(false);
    const [tabConfigSectionOpen, setTabConfigSectionOpen] = useState(false);
    const [mainTabs, setMainTabs] = useState<TabConfigItem[]>([]);
    const [subTabs, setSubTabs] = useState<TabConfigItem[]>([]);
    const [thirdTabs, setThirdTabs] = useState<TabConfigItem[]>([]);
    const [tabConfigSaving, setTabConfigSaving] = useState(false);
    const [tabConfigSaveState, setTabConfigSaveState] = useState<TabConfigSaveState>('idle');
    const [tabConfigSaveMessage, setTabConfigSaveMessage] = useState('');
    const tabConfigLoadedRef = useRef(false);
    const lastSavedTabConfigRef = useRef('');
    const tabConfigLoadPromiseRef = useRef<Promise<boolean> | null>(null);
    const tabConfigSavePromiseRef = useRef<Promise<boolean> | null>(null);
    const tabDraftRef = useRef({ mainTabs, subTabs, thirdTabs });
    tabDraftRef.current = { mainTabs, subTabs, thirdTabs };
    const [tabConfigLoading, setTabConfigLoading] = useState(false);
    const [featuredGroupSectionOpen, setFeaturedGroupSectionOpen] = useState(false);
    const [featuredGroups, setFeaturedGroups] = useState<FeaturedCaseGroup[]>([]);
    const [loadingFeaturedGroups, setLoadingFeaturedGroups] = useState(false);
    const [featuredGroupsError, setFeaturedGroupsError] = useState('');
    const [featuredGroupsLoaded, setFeaturedGroupsLoaded] = useState(false);
    const featuredGroupReadRef = useRef(0);
    const featuredAliveRef = useRef(true);
    const featuredSectionOpenRef = useRef(false);
    const [featuredPage, setFeaturedPage] = useState(1);
    const featuredPageRef = useRef(1);
    const featuredLoadedPageRef = useRef(0);
    const [featuredTotal, setFeaturedTotal] = useState<number | null>(null);
    const featuredPageSize = 20;
    const toggleFeaturedSection = () => {
        if (!featuredAliveRef.current) return;
        const open = !featuredSectionOpenRef.current;
        featuredSectionOpenRef.current = open;
        featuredGroupReadRef.current += 1;
        setLoadingFeaturedGroups(false);
        setFeaturedGroupSectionOpen(open);
    };
    const changeFeaturedPage = (page: number) => {
        if (!featuredAliveRef.current || !featuredSectionOpenRef.current || page < 1 || page === featuredPageRef.current) return;
        featuredPageRef.current = page;
        featuredGroupReadRef.current += 1;
        setFeaturedPage(page);
        setFeaturedGroups([]);
        setFeaturedTotal(null);
        setFeaturedGroupsLoaded(false);
        setFeaturedGroupsError('');
        setLoadingFeaturedGroups(true);
    };
    const featuredGroupWriteRef = useRef(false);
    const [featuredGroupSaving, setFeaturedGroupSaving] = useState(false);
    const featuredEditorSessionRef = useRef(0);
    const renderedFeaturedSession = featuredEditorSessionRef.current;
    const featuredOptionsReadRef = useRef(0);
    const featuredOptionsPendingRef = useRef(false);
    const [featuredOptions, setFeaturedOptions] = useState<Template[]>([]);
    const [featuredOptionsPage, setFeaturedOptionsPage] = useState(0);
    const [featuredOptionsTotal, setFeaturedOptionsTotal] = useState(0);
    const [featuredOptionsLoading, setFeaturedOptionsLoading] = useState(false);
    const [featuredOptionsError, setFeaturedOptionsError] = useState('');
    const [showFeaturedGroupModal, setShowFeaturedGroupModal] = useState(false);
    const [selectedFeaturedGroup, setSelectedFeaturedGroup] = useState<FeaturedCaseGroup | null>(null);
    const [featuredGroupForm, setFeaturedGroupForm] = useState({
        name: '',
        display_mode: 'comparison' as string,
        case1_id: '',
        case2_id: '',
        case1_label: '真实',
        case2_label: 'AI',
        sort_order: 0,
    });

    const convertTemplate = (t: ApiTemplate): Template => ({
        id: String(t.id),
        name: t.name,
        category: t.category,
        // 后端标签字段映射到页面结构。
        mainTab: (t as any).main_tab || '',
        subTab: (t as any).sub_tab || '',
        thirdTab: (t as any).third_tab || '',
        description: t.description || '',
        thumbnail: t.thumbnail,
        previewUrl: t.preview_url,
        images: t.images,
        imageWidth: t.image_width,
        imageHeight: t.image_height,
        price: t.price ?? 0,
        isFree: t.is_free ?? true,
        isFeatured: t.is_featured ?? false,
        downloadCount: t.download_count || 0,
        status: t.status,
        publishScope: t.publish_scope,
        rejectReason: t.reject_reason || '',
        sourceType: t.source_type,
        creator: t.creator || '',
        createdAt: t.created_at,
        updatedAt: t.updated_at,
    });

    const loadLinkedTemplate = useCallback(async () => {
        if (requestedTemplateId !== linkedTargetRef.current ||
            new URLSearchParams(window.location.search).get('templateId') !== requestedTemplateId) return;
        const request = ++linkedTemplateReadRef.current;
        setLinkedStateTarget(requestedTemplateId);
        setLinkedTemplate(null);
        setLinkedTemplateError('');
        if (requestedTemplateId === null) {
            setLinkedTemplateLoading(false);
            return;
        }
        if (!/^[1-9]\d*$/.test(requestedTemplateId) || !Number.isSafeInteger(Number(requestedTemplateId))) {
            setLinkedTemplateLoading(false);
            setLinkedTemplateError('链接中的 templateId 必须是可精确表示的正整数，请检查来源链接');
            return;
        }
        setLinkedTemplateLoading(true);
        try {
            const detail = await getTemplateDetail(requestedTemplateId);
            if (request !== linkedTemplateReadRef.current || !isCurrentLinkedTarget(requestedTemplateId)) return;
            if (String(detail?.id) !== requestedTemplateId) throw new Error('返回的模板 ID 与链接不一致，未打开其他模板');
            setLinkedTemplate(convertTemplate(detail));
        } catch (error: any) {
            if (request !== linkedTemplateReadRef.current || !isCurrentLinkedTarget(requestedTemplateId)) return;
            setLinkedTemplateError(`无法加载指定模板 #${requestedTemplateId}：${error?.message || '读取失败'}`);
        } finally {
            if (request === linkedTemplateReadRef.current && isCurrentLinkedTarget(requestedTemplateId)) setLinkedTemplateLoading(false);
        }
    }, [requestedTemplateId]);

    useEffect(() => {
        if (editorLinkedTargetRef.current && !isCurrentLinkedTarget(editorLinkedTargetRef.current)) closeTemplateEditor();
        void loadLinkedTemplate();
        return () => { linkedTemplateReadRef.current += 1; };
    }, [loadLinkedTemplate]);

    useEffect(() => {
        featuredAliveRef.current = true;
        return () => {
        featuredAliveRef.current = false;
        editorSessionRef.current += 1;
        templateReadRef.current += 1;
        featuredGroupReadRef.current += 1;
        featuredEditorSessionRef.current += 1;
        featuredOptionsReadRef.current += 1;
        };
    }, []);

    const loadCategories = useCallback(async () => {
        try {
            const list = await getTemplateCategories();
            setCategories(list);
        } catch (e) {
            console.error('加载分类失败:', e);
        }
    }, []);

    const normalizeTabConfig = useCallback((mainTabsData: TabConfigItem[], subTabsData: TabConfigItem[], thirdTabsData: TabConfigItem[]) => ({
        main_tabs: mainTabsData.map(t => ({
            label: (t.label || '').trim(),
            value: (t.value || '').trim(),
        })),
        sub_tabs: subTabsData.map(t => ({
            label: (t.label || '').trim(),
            value: (t.value || '').trim(),
            parent: (t.parent || '').trim(),
        })),
        third_tabs: thirdTabsData.map(t => ({
            label: (t.label || '').trim(),
            value: (t.value || '').trim(),
            parent: (t.parent || '').trim(),
        })),
    }), []);

    const serializeTabConfig = useCallback((mainTabsData: TabConfigItem[], subTabsData: TabConfigItem[], thirdTabsData: TabConfigItem[]) => {
        return JSON.stringify(normalizeTabConfig(mainTabsData, subTabsData, thirdTabsData));
    }, [normalizeTabConfig]);

    const getTabConfigValidationError = useCallback((mainTabsData: TabConfigItem[], subTabsData: TabConfigItem[], thirdTabsData: TabConfigItem[]) => {
        if (!mainTabsData.length) {
            return '至少保留一个一级Tab';
        }

        const mainTabValues = new Set<string>();
        for (let i = 0; i < mainTabsData.length; i++) {
            const item = mainTabsData[i];
            const label = (item.label || '').trim();
            const value = (item.value || '').trim();
            if (!label) {
                return `第 ${i + 1} 个一级Tab缺少显示名`;
            }
            if (!value) {
                return `第 ${i + 1} 个一级Tab缺少 value`;
            }
            if (mainTabValues.has(value)) {
                return `一级Tab的 value 不能重复：${value}`;
            }
            mainTabValues.add(value);
        }

        const subTabValues = new Set<string>();
        for (let i = 0; i < subTabsData.length; i++) {
            const item = subTabsData[i];
            const label = (item.label || '').trim();
            const value = (item.value || '').trim();
            const parent = (item.parent || '').trim();
            if (!label) {
                return `第 ${i + 1} 个二级Tab缺少显示名`;
            }
            if (!value) {
                return `第 ${i + 1} 个二级Tab缺少 value`;
            }
            if (!parent) {
                return `第 ${i + 1} 个二级Tab必须设置所属的一级Tab`;
            }
            if (!mainTabValues.has(parent)) {
                return `第 ${i + 1} 个二级Tab的父Tab（${parent}）不存在于一级Tab列表中`;
            }
            if (subTabValues.has(value)) {
                return `二级Tab的 value 不能重复：${value}`;
            }
            subTabValues.add(value);
        }

        const thirdTabValues = new Set<string>();
        for (let i = 0; i < thirdTabsData.length; i++) {
            const item = thirdTabsData[i];
            const label = (item.label || '').trim();
            const value = (item.value || '').trim();
            const parent = (item.parent || '').trim();
            if (!label) {
                return `第 ${i + 1} 个三级Tab缺少显示名`;
            }
            if (!value) {
                return `第 ${i + 1} 个三级Tab缺少 value`;
            }
            if (!parent) {
                return `第 ${i + 1} 个三级Tab必须设置所属的二级Tab`;
            }
            if (!subTabValues.has(parent)) {
                return `第 ${i + 1} 个三级Tab的父Tab（${parent}）不存在于二级Tab列表中`;
            }
            if (thirdTabValues.has(value)) {
                return `三级Tab的 value 不能重复：${value}`;
            }
            thirdTabValues.add(value);
        }

        return '';
    }, []);

    const loadTabConfig = useCallback((): Promise<boolean> => {
        // Opening a modal must not replace a dirty local configuration draft.
        if (tabConfigLoadedRef.current) return Promise.resolve(true);
        if (tabConfigLoadPromiseRef.current) return tabConfigLoadPromiseRef.current;
        setTabConfigLoading(true);
        const request = (async () => {
            try {
                const cfg = await getTemplateTabConfig();
                if (!cfg || !Array.isArray(cfg.main_tabs) || !Array.isArray(cfg.sub_tabs) || !Array.isArray(cfg.third_tabs)) {
                    throw new Error('标签配置响应不完整，未覆盖本地草稿');
                }
                const nextMainTabs = Array.isArray(cfg?.main_tabs) ? cfg.main_tabs.map(t => ({ ...t })) : [];
                const nextSubTabs = Array.isArray(cfg?.sub_tabs) ? cfg.sub_tabs.map(t => ({ ...t, parent: t.parent || '' })) : [];
                const nextThirdTabs = Array.isArray(cfg?.third_tabs) ? cfg.third_tabs.map(t => ({ ...t, parent: t.parent || '' })) : [];
                lastSavedTabConfigRef.current = serializeTabConfig(nextMainTabs, nextSubTabs, nextThirdTabs);
                tabDraftRef.current = { mainTabs: nextMainTabs, subTabs: nextSubTabs, thirdTabs: nextThirdTabs };
                tabConfigLoadedRef.current = true;
                setMainTabs(nextMainTabs);
                setSubTabs(nextSubTabs);
                setThirdTabs(nextThirdTabs);
                setTabConfigSaveState('idle');
                setTabConfigSaveMessage('');
                return true;
            } catch (error: any) {
                setTabConfigSaveState('error');
                setTabConfigSaveMessage(error?.message || '加载标签配置失败，请重试');
                return false;
            } finally {
                setTabConfigLoading(false);

            }
        })();
        tabConfigLoadPromiseRef.current = request;
        void request.finally(() => { if (tabConfigLoadPromiseRef.current === request) tabConfigLoadPromiseRef.current = null; });
        return request;
    }, [serializeTabConfig]);

    const updateMainTabLabel = useCallback((index: number, label: string) => {
        setMainTabs(prev => prev.map((item, itemIndex) => itemIndex === index ? { ...item, label } : item));
    }, []);

    const updateMainTabValue = useCallback((index: number, value: string) => {
        const previousValue = mainTabs[index]?.value || '';
        setMainTabs(prev => prev.map((item, itemIndex) => itemIndex === index ? { ...item, value } : item));
        if (previousValue && previousValue !== value) {
            setSubTabs(prev => prev.map(item => item.parent === previousValue ? { ...item, parent: value } : item));
        }
    }, [mainTabs]);

    const removeMainTab = useCallback((index: number) => {
        if (mainTabs.length <= 1) {
            setTabConfigSaveState('invalid');
            setTabConfigSaveMessage('至少保留一个一级标签');
            return;
        }
        const removedValue = (mainTabs[index]?.value || '').trim();
        const nextMainTabs = mainTabs.filter((_, itemIndex) => itemIndex !== index);
        const nextSubTabs = removedValue ? subTabs.filter(item => item.parent !== removedValue) : subTabs;
        const nextSubTabValues = new Set(nextSubTabs.map(item => (item.value || '').trim()).filter(Boolean));
        const nextThirdTabs = thirdTabs.filter(item => nextSubTabValues.has((item.parent || '').trim()));
        setMainTabs(nextMainTabs);
        setSubTabs(nextSubTabs);
        setThirdTabs(nextThirdTabs);
        const removedSubTabCount = subTabs.length - nextSubTabs.length;
        const removedThirdTabCount = thirdTabs.length - nextThirdTabs.length;
        if (removedSubTabCount > 0 || removedThirdTabCount > 0) {
            setTabConfigSaveState('editing');
            setTabConfigSaveMessage(`已同步删除 ${removedSubTabCount} 个二级Tab 和 ${removedThirdTabCount} 个三级Tab，正在准备自动保存...`);
        }
    }, [mainTabs, subTabs, thirdTabs]);

    const updateSubTab = useCallback((index: number, patch: Partial<TabConfigItem>) => {
        const previousValue = subTabs[index]?.value || '';
        setSubTabs(prev => prev.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item));
        if (patch.value && previousValue && patch.value !== previousValue) {
            setThirdTabs(prev => prev.map(item => item.parent === previousValue ? { ...item, parent: patch.value as string } : item));
        }
    }, [subTabs]);

    const addMainTab = useCallback(() => {
        setMainTabs(prev => [...prev, { label: '', value: '' }]);
    }, []);

    const addSubTab = useCallback(() => {
        if (!mainTabs.length) {
            setTabConfigSaveState('invalid');
            setTabConfigSaveMessage('请先保留至少一个一级标签，再新增二级标签');
            return;
        }
        const defaultParent = mainTabs.find(item => (item.value || '').trim())?.value || mainTabs[0]?.value || '';
        setSubTabs(prev => [...prev, { label: '', value: '', parent: defaultParent }]);
    }, [mainTabs]);

    const addThirdTab = useCallback(() => {
        if (!subTabs.length) {
            setTabConfigSaveState('invalid');
            setTabConfigSaveMessage('请先保留至少一个二级标签，再新增三级标签');
            return;
        }
        const defaultParent = subTabs.find(item => (item.value || '').trim())?.value || subTabs[0]?.value || '';
        setThirdTabs(prev => [...prev, { label: '', value: '', parent: defaultParent }]);
    }, [subTabs]);

    const handleSaveTabConfig = useCallback((options?: { silent?: boolean }): Promise<boolean> => {
        if (!tabConfigLoadedRef.current) {
            setTabConfigSaveState('error');
            setTabConfigSaveMessage('请先成功加载标签配置，再保存修改');
            return Promise.resolve(false);
        }
        if (tabConfigSavePromiseRef.current) return tabConfigSavePromiseRef.current;
        const request = (async () => {
            setTabConfigSaving(true);
            try {
                while (true) {
                    const draft = tabDraftRef.current;
                    const error = getTabConfigValidationError(draft.mainTabs, draft.subTabs, draft.thirdTabs);
                    if (error) {
                        setTabConfigSaveState('invalid');
                        setTabConfigSaveMessage(error);
                        return false;
                    }
                    const payload = normalizeTabConfig(draft.mainTabs, draft.subTabs, draft.thirdTabs);
                    const serialized = JSON.stringify(payload);
                    if (serialized === lastSavedTabConfigRef.current) {
                        setTabConfigSaveState('saved');
                        setTabConfigSaveMessage('最新标签配置已保存');
                        return true;
                    }
                    setTabConfigSaveState('saving');
                    setTabConfigSaveMessage('正在保存标签配置...');
                    await putTemplateTabConfig(payload);
                    lastSavedTabConfigRef.current = serialized;
                    const latest = tabDraftRef.current;
                    if (serializeTabConfig(latest.mainTabs, latest.subTabs, latest.thirdTabs) === serialized) {
                        setTabConfigSaveState('saved');
                        setTabConfigSaveMessage(options?.silent ? '已自动保存' : '保存成功');
                        return true;
                    }
                    // One request at a time. Coalesce intermediate edits and
                    // persist only the newest draft after the current request.
                    setTabConfigSaveState('editing');
                    setTabConfigSaveMessage('已有新修改，正在保存最新草稿...');
                }
            } catch (error: any) {
                setTabConfigSaveState('error');
                setTabConfigSaveMessage(error?.message || '保存失败，草稿已保留，请重试');
                return false;
            } finally {
                setTabConfigSaving(false);

            }
        })();
        tabConfigSavePromiseRef.current = request;
        void request.finally(() => { if (tabConfigSavePromiseRef.current === request) tabConfigSavePromiseRef.current = null; });
        return request;
    }, [getTabConfigValidationError, normalizeTabConfig, serializeTabConfig]);

    const loadTemplates = useCallback(async () => {
        const request = ++templateReadRef.current;
        const query = { ...templateQueryRef.current };
        const queryKey = JSON.stringify(query);
        if (lastTemplateQueryRef.current !== queryKey) {
            setTemplates([]);
            setTemplatesLoaded(false);
        }
        setLoading(true);
        setTemplatesError('');
        try {
            const params: any = { page: query.page, page_size: templatePageSize };
            if (query.category !== 'all') params.category = query.category;
            if (query.status !== 'all') params.status = query.status;
            const res = await getTemplateList(params);
            if (request !== templateReadRef.current || queryKey !== JSON.stringify(templateQueryRef.current)) return null;
            if (!Array.isArray(res?.list) || !Number.isSafeInteger(res?.total) || res.total < 0) {
                throw new Error('模板列表响应缺少有效列表或总数，请重试');
            }
            const total = res.total;
            const lastPage = Math.max(1, Math.ceil(total / templatePageSize));
            if (query.page > lastPage) {
                setTemplatePage(lastPage);
                return null;
            }
            setTemplates((res?.list || []).map(convertTemplate));
            setTemplateTotal(total);
            setTemplatesLoaded(true);
            lastTemplateQueryRef.current = queryKey;
            return true;
        } catch (error: any) {
            if (request !== templateReadRef.current || queryKey !== JSON.stringify(templateQueryRef.current)) return null;
            setTemplatesError(error?.message || '加载模板列表失败');
            return false;
        } finally {
            if (request === templateReadRef.current) setLoading(false);
        }
    }, []);

    useEffect(() => {
        // 加载分类和标签选项。
        loadCategories();
        loadTabConfig();
    }, [loadCategories, loadTabConfig]);

    useEffect(() => {
        if (!tabConfigLoadedRef.current) {
            return;
        }
        const currentSerialized = serializeTabConfig(mainTabs, subTabs, thirdTabs);
        if (currentSerialized === lastSavedTabConfigRef.current) {
            return;
        }
        const validationError = getTabConfigValidationError(mainTabs, subTabs, thirdTabs);
        if (validationError) {
            setTabConfigSaveState('invalid');
            setTabConfigSaveMessage(validationError);
            return;
        }
        setTabConfigSaveState('editing');
        setTabConfigSaveMessage(tabConfigSavePromiseRef.current ? '已有新修改，将在当前保存完成后继续保存' : '检测到修改，准备自动保存...');
        const timer = window.setTimeout(() => {
            void handleSaveTabConfig({ silent: true });
        }, 800);
        return () => window.clearTimeout(timer);
    }, [getTabConfigValidationError, handleSaveTabConfig, mainTabs, serializeTabConfig, subTabs, thirdTabs]);

    useEffect(() => {
        loadTemplates();
    }, [loadTemplates, templatePage, categoryFilter, statusFilter]);

    const filteredTemplates = templates.filter(template => {
        const matchKeyword = !searchKeyword ||
            template.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
            template.description.toLowerCase().includes(searchKeyword.toLowerCase());
        return matchKeyword;
    });

    const handleSearch = () => {
        setSearchKeyword(searchInput.trim());
    };

    const handleResetSearch = () => {
        setSearchInput('');
        setSearchKeyword('');
        setCategoryFilter('all');
        setStatusFilter('all');
        setTemplatePage(1);
    };

    const getStatusLabel = (status: string) => {
        const statusMap: Record<string, { label: string; className: string }> = {
            published: { label: '已发布', className: 'status-published' },
            pending: { label: '待审核', className: 'status-pending' },
            draft: { label: '草稿', className: 'status-draft' },
            rejected: { label: '已拒绝', className: 'status-draft' },
            archived: { label: '已归档', className: 'status-archived' },
        };
        return statusMap[status] || { label: status, className: '' };
    };

    const getPublishScopeLabel = (publishScope?: string) => {
        if (publishScope === 'homepage_only') return '仅首页展示';
        if (publishScope === 'square') return '首页 + 模板广场';
        return publishScope ? `未知去向（${publishScope}）` : '未提供';
    };

    const getSourceTypeLabel = (sourceType?: string) => {
        if (sourceType === 'album_upload') return '相册上传';
        if (sourceType === 'ai_generated') return 'AI 生成发布';
        if (sourceType === 'admin_upload') return '后台创建';
        return sourceType ? `未知来源（${sourceType}）` : '未提供';
    };

    const getCategoryLabel = (categoryId: string) => {
        const cat = categories.find(c => c.id === categoryId);
        return cat ? cat.name : categoryId;
    };

    const handleEdit = async (template: Template, linkedTargetId?: string) => {
        if (linkedTargetId && (template.id !== linkedTargetId || !isCurrentLinkedTarget(linkedTargetId))) return;
        if (savePendingRef.current || templateWritesRef.current.has(template.id)) return;
        closeTemplateEditor();
        editorLinkedTargetRef.current = linkedTargetId || null;
        const session = editorSessionRef.current;
        try {
            if (!mainTabs.length || !subTabs.length) {
                await loadTabConfig();
            }
            if (session !== editorSessionRef.current || (linkedTargetId && !isCurrentLinkedTarget(linkedTargetId))) return;
            const detail = await getTemplateDetail(template.id);
            if (session !== editorSessionRef.current || (linkedTargetId && !isCurrentLinkedTarget(linkedTargetId))) return;
            if (String(detail?.id) !== template.id) throw new Error('返回的模板 ID 与选择对象不一致');
            // 兼容 images 的字符串/对象数组；为空时回退已有封面。
            const existedImageUrls: string[] = (() => {
                const urls: string[] = [];
                if (detail.images) {
                    try {
                        const parsed = JSON.parse(detail.images);
                        if (Array.isArray(parsed)) {
                            for (const item of parsed) {
                                if (typeof item === 'string') {
                                    urls.push(item);
                                } else if (item && typeof item === 'object') {
                                    const obj = item as any;
                                    if (typeof obj.image === 'string' && obj.image) {
                                        urls.push(obj.image);
                                    } else if (typeof obj.url === 'string' && obj.url) {
                                        urls.push(obj.url);
                                    }
                                }
                            }
                        }
                    } catch {
                        // ignore parse error
                    }
                }
                if (!urls.length) {
                    if (detail.thumbnail) {
                        urls.push(detail.thumbnail);
                    } else if (detail.preview_url) {
                        urls.push(detail.preview_url);
                    }
                }
            return urls;
            })();

            const detailTemplate = convertTemplate(detail);
            const initialForm = {
                name: detail.name,
                category: detail.category,
                mainTab: detail.main_tab || '',
                subTab: detail.sub_tab || '',
                thirdTab: (detail as any).third_tab || '',
                description: detail.description || '',
                imageUrls: existedImageUrls,
                price: detail.price ?? 0,
                isFree: detail.is_free ?? true,
                status: detail.status,
            };
            editInitialFormRef.current = initialForm;
            setEditFormData(initialForm);
            setSelectedTemplate(detailTemplate);
            setShowEditModal(true);
        } catch (e) {
            if (session !== editorSessionRef.current || (linkedTargetId && !isCurrentLinkedTarget(linkedTargetId))) return;
            console.error(e);
            reportPageLoadError(e, '获取模板详情失败');
        }
    };

    const buildImageFields = (urls: string[] | undefined) => {
        const list = urls && urls.length ? urls : [];
        const thumbnail = list[0] || '';
        const previewUrl = list[0] || '';
        const images = list.length ? JSON.stringify(list) : '';
        return { thumbnail, previewUrl, images };
    };

    const handleSaveEdit = async () => {
        if (!isCurrentEditor('edit')) return;
        if (!selectedTemplate || !editInitialFormRef.current || savePendingRef.current || uploadPendingRef.current) return;
        if (!editFormData.name.trim()) {
            alert('请填写名称');
            return;
        }
        const initial = editInitialFormRef.current;
        const patch: TemplateUpdateRequest = {};
        if (editFormData.name !== initial.name) patch.name = editFormData.name;
        if (editFormData.description !== initial.description) patch.description = editFormData.description;
        if (editFormData.mainTab !== initial.mainTab) patch.main_tab = editFormData.mainTab;
        if (editFormData.subTab !== initial.subTab) patch.sub_tab = editFormData.subTab;
        if (editFormData.thirdTab !== initial.thirdTab) patch.third_tab = editFormData.thirdTab;
        if (editFormData.price !== initial.price) patch.price = editFormData.price;
        if (editFormData.isFree !== initial.isFree) patch.is_free = editFormData.isFree;
        if (editFormData.status !== initial.status) patch.status = editFormData.status;
        if (JSON.stringify(editFormData.imageUrls) !== JSON.stringify(initial.imageUrls)) {
            const { thumbnail, previewUrl, images } = buildImageFields(editFormData.imageUrls);
            patch.thumbnail = thumbnail;
            patch.preview_url = previewUrl;
            patch.images = images;
        }
        if (!Object.keys(patch).length) {
            closeTemplateEditor();
            return;
        }
        if (!beginTemplateWrite(selectedTemplate.id)) return;
        savePendingRef.current = true;
        setEditorSaving(true);
        const session = editorSessionRef.current;
        try {
            const updated = await updateTemplate(selectedTemplate.id, patch);
            if (updated) {
                setTemplates(items => items.map(item => item.id === selectedTemplate.id ? convertTemplate(updated) : item));
                setLinkedTemplate(item => item?.id === String(updated.id) ? convertTemplate(updated) : item);
            }
            if (session === editorSessionRef.current) closeTemplateEditor();
            if (!updated) {
                alert('修改已保存，但最新数据暂时无法读取，请刷新列表核实');
                void loadTemplates();
            }
        } catch (error: any) {
            alert(error?.message || '更新失败');
        } finally {
            savePendingRef.current = false;
            setEditorSaving(false);
            endTemplateWrite(selectedTemplate.id);
        }
    };

    const handleAddTemplate = async () => {
        if (!isCurrentEditor('add')) return;
        if (savePendingRef.current || uploadPendingRef.current) return;
        if (!addFormData.name.trim()) {
            alert('请填写名称');
            return;
        }
        savePendingRef.current = true;
        setEditorSaving(true);
        const session = editorSessionRef.current;
        setActionLoading('add');
        try {
            const { thumbnail, previewUrl, images } = buildImageFields(addFormData.imageUrls);
            // 新模板沿用第一分类，未配置时默认 villa。
            const category = categories[0]?.id || 'villa';
            await createTemplate({
                name: addFormData.name,
                category,
                main_tab: addFormData.mainTab || undefined,
                sub_tab: addFormData.subTab || undefined,
                third_tab: addFormData.thirdTab || undefined,
                description: addFormData.description,
                thumbnail: thumbnail || undefined,
                preview_url: previewUrl || undefined,
                images: images || undefined,
                price: addFormData.price,
                is_free: addFormData.isFree,
                status: addFormData.status,
            });
            if (session === editorSessionRef.current) closeTemplateEditor();
            loadTemplates();
        } catch (e: any) {
            alert(e?.message || '创建失败');
        } finally {
            savePendingRef.current = false;
            setEditorSaving(false);
            setActionLoading(null);
        }
    };

    const refreshAfterTemplateWrite = async (message: string) => {
        const refreshed = await loadTemplates();
        if (refreshed === false) setTemplatesError(`${message}，但列表刷新失败，请重新加载`);
    };

    const handleDelete = async (templateId: string) => {
        if (templateWritesRef.current.has(templateId) || !window.confirm('确定要删除这个模板吗？此操作不可恢复。')) return;
        if (!beginTemplateWrite(templateId)) return;
        try {
            await deleteTemplate(templateId);
        } catch (error: any) {
            alert(error?.message || '删除失败');
            return;
        } finally {
            endTemplateWrite(templateId);
        }
        setTemplates(items => items.filter(item => item.id !== templateId));
        await refreshAfterTemplateWrite('模板已删除');
    };

    const handleApprove = async (template: Template) => {
        if (!beginTemplateWrite(template.id)) return;
        try {
            await updateTemplateStatus(template.id, 'published');
        } catch (error: any) {
            alert(error?.message || '审核通过失败');
            return;
        } finally {
            endTemplateWrite(template.id);
        }
        setTemplates(items => items.map(item => item.id === template.id ? { ...item, status: 'published', rejectReason: '' } : item));
        setSelectedTemplate(item => item?.id === template.id ? { ...item, status: 'published', rejectReason: '' } : item);
        await refreshAfterTemplateWrite('模板已审核通过');
    };

    const handleReject = async (template: Template) => {
        if (templateWritesRef.current.has(template.id)) return;
        const rejectReason = window.prompt('请输入拒绝原因（会回传给设计师）', template.rejectReason || '');
        if (rejectReason === null || !beginTemplateWrite(template.id)) return;
        try {
            await updateTemplateStatus(template.id, 'rejected', rejectReason);
        } catch (error: any) {
            alert(error?.message || '审核拒绝失败');
            return;
        } finally {
            endTemplateWrite(template.id);
        }
        setTemplates(items => items.map(item => item.id === template.id ? { ...item, status: 'rejected', rejectReason } : item));
        setSelectedTemplate(item => item?.id === template.id ? { ...item, status: 'rejected', rejectReason } : item);
        await refreshAfterTemplateWrite('模板已拒绝');
    };

    const handleMoveCategory = (template: Template) => {
        if (templateWritesRef.current.has(template.id)) return;
        moveSessionRef.current += 1;
        setMoveTargetTemplate(template);
        setShowMoveCategoryModal(true);
    };

    const confirmMoveCategory = async (categoryId: string) => {
        if (!showMoveCategoryModal || renderedMoveSession !== moveSessionRef.current) return;
        if (!moveTargetTemplate || !beginTemplateWrite(moveTargetTemplate.id)) return;
        const id = moveTargetTemplate.id;
        const session = moveSessionRef.current;
        try {
            await updateTemplateCategory(id, categoryId);
        } catch (error: any) {
            alert(error?.message || '移动失败');
            return;
        } finally {
            endTemplateWrite(id);
        }
        setTemplates(items => items.map(item => item.id === id ? { ...item, category: categoryId } : item));
        if (session === moveSessionRef.current) closeMoveCategory();
        await refreshAfterTemplateWrite('模板分类已更新');
    };

    const handleAddCategory = async () => {
        if (!categoryForm.id.trim() || !categoryForm.name.trim()) {
            alert('请填写分类 ID 和名称（ID 建议英文，例如 villa、urban）');
            return;
        }
        try {
            await createTemplateCategory({
                id: categoryForm.id.trim(),
                name: categoryForm.name.trim(),
                sort_order: categoryForm.sort_order,
            });
            setCategoryForm({ id: '', name: '', sort_order: categories.length + 1 });
            loadCategories();
        } catch (e: any) {
            alert(e?.message || '创建分类失败');
        }
    };

    const handleDeleteCategory = async (id: string) => {
        if (!window.confirm('确定删除该分类？如果该分类下还有模板，将无法删除。')) return;
        try {
            await deleteTemplateCategory(id);
            loadCategories();
        } catch (e: any) {
            alert(e?.message || '删除失败');
        }
    };

    const handleSetFeatured = async (template: Template, isFeatured: boolean) => {
        if (!beginTemplateWrite(template.id)) return;
        try {
            await setTemplateFeatured(template.id, isFeatured);
        } catch (error: any) {
            alert(error?.message || '精选设置失败');
            return;
        } finally {
            endTemplateWrite(template.id);
        }
        setTemplates(items => items.map(item => item.id === template.id ? { ...item, isFeatured } : item));
        await refreshAfterTemplateWrite('精选状态已更新');
    };

    const loadFeaturedGroups = useCallback(async () => {
        if (!featuredAliveRef.current || !featuredSectionOpenRef.current) return null;
        const page = featuredPageRef.current;
        const request = ++featuredGroupReadRef.current;
        const isCurrent = () => featuredAliveRef.current && featuredSectionOpenRef.current &&
            request === featuredGroupReadRef.current && page === featuredPageRef.current;
        setLoadingFeaturedGroups(true);
        setFeaturedGroupsError('');
        try {
            const res = await getFeaturedCaseGroups(page, featuredPageSize);
            if (!isCurrent()) return null;
            if (!Array.isArray(res?.list) || !Number.isSafeInteger(res.total) || res.total < 0 ||
                (res.page !== undefined && res.page !== page) ||
                (res.page_size !== undefined && res.page_size !== featuredPageSize)) throw new Error('案例组分页响应不完整或页码不匹配，请重试');
            const lastPage = Math.max(1, Math.ceil(res.total / featuredPageSize));
            if (page > lastPage) { changeFeaturedPage(lastPage); return null; }
            setFeaturedGroups(res.list);
            setFeaturedTotal(res.total);
            setFeaturedGroupsLoaded(true);
            featuredLoadedPageRef.current = page;
            return true;
        } catch (error: any) {
            if (!isCurrent()) return null;
            if (featuredLoadedPageRef.current !== page) { setFeaturedGroups([]); setFeaturedTotal(null); setFeaturedGroupsLoaded(false); }
            setFeaturedGroupsError(error?.message || '加载精选案例组失败');
            return false;
        } finally {
            if (isCurrent()) setLoadingFeaturedGroups(false);
        }
    }, []);

    useEffect(() => { if (featuredGroupSectionOpen) void loadFeaturedGroups(); }, [featuredGroupSectionOpen, featuredPage, loadFeaturedGroups]);

    const loadFeaturedOptions = async (page = 1, session = featuredEditorSessionRef.current) => {
        if (session !== featuredEditorSessionRef.current || featuredOptionsPendingRef.current) return;
        const request = ++featuredOptionsReadRef.current;
        featuredOptionsPendingRef.current = true;
        setFeaturedOptionsLoading(true);
        setFeaturedOptionsError('');
        try {
            // This catalog is independent of the main grid's filters and page.
            const res = await getTemplateList({ status: 'published', page, page_size: 100 });
            if (request !== featuredOptionsReadRef.current || session !== featuredEditorSessionRef.current) return;
            if (!Array.isArray(res?.list) || !Number.isSafeInteger(res?.total) || res.total < 0) {
                throw new Error('案例选项响应缺少有效列表或总数，请重试');
            }
            const list = res.list.map(convertTemplate);
            setFeaturedOptions(items => page === 1 ? list : Array.from(new Map([...items, ...list].map(item => [item.id, item])).values()));
            setFeaturedOptionsPage(page);
            setFeaturedOptionsTotal(res.total);
        } catch (error: any) {
            if (request !== featuredOptionsReadRef.current || session !== featuredEditorSessionRef.current) return;
            setFeaturedOptionsError(error?.message || '加载已发布案例失败');
        } finally {
            if (request === featuredOptionsReadRef.current && session === featuredEditorSessionRef.current) {
                featuredOptionsPendingRef.current = false;
                setFeaturedOptionsLoading(false);
            }
        }
    };

    const closeFeaturedGroupEditor = () => {
        featuredEditorSessionRef.current += 1;
        featuredOptionsReadRef.current += 1;
        featuredOptionsPendingRef.current = false;
        setShowFeaturedGroupModal(false);
        setSelectedFeaturedGroup(null);
        setFeaturedOptionsLoading(false);
    };

    const dismissFeaturedGroupEditor = () => {
        if (renderedFeaturedSession === featuredEditorSessionRef.current) closeFeaturedGroupEditor();
    };
    const updateFeaturedForm = (update: React.SetStateAction<typeof featuredGroupForm>) => {
        if (showFeaturedGroupModal && renderedFeaturedSession === featuredEditorSessionRef.current) setFeaturedGroupForm(update);
    };

    const openFeaturedGroupEditor = (group?: FeaturedCaseGroup) => {
        if (!featuredAliveRef.current || !featuredSectionOpenRef.current || featuredPage !== featuredPageRef.current || featuredGroupWriteRef.current) return;
        closeFeaturedGroupEditor();
        setSelectedFeaturedGroup(group || null);
        setFeaturedGroupForm({
            name: group?.name || '',
            display_mode: group ? group.display_mode ?? '' : 'comparison',
            case1_id: group?.case1_id ? String(group.case1_id) : '',
            case2_id: group?.case2_id ? String(group.case2_id) : '',
            case1_label: group ? group.case1_label ?? '' : '真实',
            case2_label: group ? group.case2_label ?? '' : 'AI',
            sort_order: group?.sort_order ?? 0,
        });
        setFeaturedOptions([]);
        setFeaturedOptionsPage(0);
        setFeaturedOptionsTotal(0);
        setFeaturedOptionsError('');
        setShowFeaturedGroupModal(true);
        void loadFeaturedOptions(1, featuredEditorSessionRef.current);
    };

    const featuredOptionEntries = (() => {
        const options = new Map(featuredOptions.map(item => [item.id, { id: item.id, label: item.name }]));
        for (const id of [featuredGroupForm.case1_id, featuredGroupForm.case2_id]) {
            if (!id || options.has(id)) continue;
            const existing = selectedFeaturedGroup?.case1_id === Number(id) ? selectedFeaturedGroup.case1 :
                selectedFeaturedGroup?.case2_id === Number(id) ? selectedFeaturedGroup.case2 : undefined;
            options.set(id, { id, label: existing ? `${existing.name}（当前已选，${getStatusLabel(existing.status).label}）` : `当前案例 #${id}（未加载或不可用）` });
        }
        return Array.from(options.values());
    })();

    const handleSaveFeaturedGroup = async () => {
        if (!showFeaturedGroupModal || renderedFeaturedSession !== featuredEditorSessionRef.current || featuredGroupWriteRef.current) return;
        if (!featuredGroupForm.name.trim() || !featuredGroupForm.case1_id) {
            alert('请填写组名称并选择第一个案例');
            return;
        }
        if (!isFeaturedDisplayMode(featuredGroupForm.display_mode)) {
            alert('当前显示模式不受支持，请明确选择一个受支持的模式后再保存');
            return;
        }
        if (featuredGroupForm.display_mode !== 'normal' && !featuredGroupForm.case2_id) {
            alert('对比模式和并排模式需要选择两个案例');
            return;
        }
        const case1 = Number(featuredGroupForm.case1_id);
        const case2 = featuredGroupForm.case2_id ? Number(featuredGroupForm.case2_id) : undefined;
        if (!Number.isSafeInteger(case1) || case1 <= 0 || (case2 !== undefined && (!Number.isSafeInteger(case2) || case2 <= 0))) {
            alert('案例 ID 无效，请重新选择');
            return;
        }
        featuredGroupWriteRef.current = true;
        setFeaturedGroupSaving(true);
        const session = featuredEditorSessionRef.current;
        const groupId = selectedFeaturedGroup?.id;
        setActionLoading(groupId ? 'update-group' : 'create-group');
        const payload = {
            name: featuredGroupForm.name,
            display_mode: featuredGroupForm.display_mode,
            case1_id: case1,
            // Keep an existing second case even when normal mode hides it.
            case2_id: case2,
            case1_label: featuredGroupForm.case1_label,
            case2_label: featuredGroupForm.case2_label,
            sort_order: featuredGroupForm.sort_order,
        };
        try {
            if (groupId) await updateFeaturedCaseGroup(String(groupId), payload);
            else await createFeaturedCaseGroup(payload);
        } catch (error: any) {
            if (featuredAliveRef.current) alert(error?.message || '保存案例组失败');
            return;
        } finally {
            featuredGroupWriteRef.current = false;
            if (featuredAliveRef.current) {
                setFeaturedGroupSaving(false);
                setActionLoading(null);
            }
        }
        if (session === featuredEditorSessionRef.current) closeFeaturedGroupEditor();
        // The write is committed. A refresh failure has its own feedback and
        // must never invite the user to repeat a successful create/update.
        const refreshed = await loadFeaturedGroups();
        if (refreshed === false) setFeaturedGroupsError('案例组已保存，但列表刷新失败，请重试加载');
    };

    const handleDeleteFeaturedGroup = async (groupId: number) => {
        if (!featuredAliveRef.current || !featuredSectionOpenRef.current || featuredPage !== featuredPageRef.current || featuredGroupWriteRef.current || !window.confirm('确定要删除这个精选案例组吗？')) return;
        featuredGroupWriteRef.current = true;
        setFeaturedGroupSaving(true);
        setActionLoading(`group-${groupId}`);
        try {
            await deleteFeaturedCaseGroup(String(groupId));
        } catch (error: any) {
            if (featuredAliveRef.current) alert(error?.message || '删除失败');
            return;
        } finally {
            featuredGroupWriteRef.current = false;
            if (featuredAliveRef.current) {
                setFeaturedGroupSaving(false);
                setActionLoading(null);
            }
        }
        if (!featuredAliveRef.current) return;
        setFeaturedGroups(items => items.filter(item => item.id !== groupId));
        const refreshed = await loadFeaturedGroups();
        if (refreshed === false) setFeaturedGroupsError('案例组已删除，但列表刷新失败，请重试加载');
    };

    const linkedStateMatchesTarget = linkedStateTarget === requestedTemplateId && isCurrentLinkedTarget(requestedTemplateId);
    const visibleLinkedTemplate = linkedStateMatchesTarget && linkedTemplate?.id === requestedTemplateId ? linkedTemplate : null;
    const openLinkedTemplateEditor = () => {
        if (!visibleLinkedTemplate || renderedLinkedRequest !== linkedTemplateReadRef.current ||
            !isCurrentLinkedTarget(requestedTemplateId) || visibleLinkedTemplate.id !== requestedTemplateId) return;
        return handleEdit(visibleLinkedTemplate, requestedTemplateId);
    };

    return (
        <>
            <div className="templates-container">
                <div className="templates-toolbar section-card">
                    <div className="templates-toolbar-top">
                        <div className="toolbar-title-block">
                            <h3>模板检索与内容运营</h3>
                            <p>分类与状态筛选覆盖全部模板；关键词仅检索当前页。可审核模板、整理分类，或配置首页精选案例与标签。</p>
                        </div>
                        <div className="toolbar-actions management-actions">
                            <button className="btn-secondary" onClick={toggleFeaturedSection}>
                                精选案例组管理
                            </button>
                            <button className="btn-secondary" onClick={() => { setTabConfigSectionOpen(!tabConfigSectionOpen); if (!tabConfigSectionOpen) loadTabConfig(); }}>
                                标签配置
                            </button>
                            <button className="btn-secondary" onClick={() => setCategorySectionOpen(!categorySectionOpen)}>
                                <FiFolderIcon /> 分类管理
                            </button>
                            <button
                                className="btn-primary"
                                onClick={() => {
                                    if (!mainTabs.length || !subTabs.length) {
                                        loadTabConfig();
                                    }
                                    if (savePendingRef.current) return;
                                    closeTemplateEditor();
                                    setAddFormData({ ...defaultForm, imageUrls: [] });
                                    setShowAddModal(true);
                                }}
                            >
                                <FiPlusIcon /> 添加模板
                            </button>
                        </div>
                    </div>
                    <div className="toolbar-left">
                        <div className="search-box">
                            <FiSearchIcon className="search-icon" />
                            <input
                                type="text"
                                placeholder="仅搜索本页模板名称或描述..."
                                value={searchInput}
                                onChange={(e) => setSearchInput(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') {
                                        handleSearch();
                                    }
                                }}
                                className="search-input"
                            />
                        </div>
                        <div className="filters">
                            <select
                                className="filter-select"
                                value={categoryFilter}
                                onChange={(e) => { setCategoryFilter(e.target.value); setTemplatePage(1); }}
                            >
                                <option value="all">全部分类</option>
                                {categories.map((c) => (
                                    <option key={c.id} value={c.id}>{c.name}</option>
                                ))}
                            </select>
                            <select
                                className="filter-select"
                                value={statusFilter}
                                onChange={(e) => { setStatusFilter(e.target.value); setTemplatePage(1); }}
                            >
                                <option value="all">全部状态</option>
                                <option value="pending">待审核</option>
                                <option value="published">已发布</option>
                                <option value="draft">草稿</option>
                                <option value="rejected">已拒绝</option>
                                <option value="archived">已归档</option>
                            </select>
                        </div>
                    </div>
                    <div className="templates-toolbar-footer">
                        <div className="toolbar-summary">
                            {templatesLoaded ? <>本页显示 <strong>{filteredTemplates.length}</strong> / {templates.length} 个；筛选结果共 {templateTotal} 个模板</> : '模板列表尚未成功加载'}
                            {searchKeyword ? <span className="summary-tag">本页关键词：{searchKeyword}</span> : null}
                            {categoryFilter !== 'all' ? <span className="summary-tag">分类：{getCategoryLabel(categoryFilter)}</span> : null}
                            {statusFilter !== 'all' ? <span className="summary-tag">状态：{getStatusLabel(statusFilter).label}</span> : null}
                        </div>
                        <div className="toolbar-actions search-actions">
                            <button className="btn-secondary" onClick={handleResetSearch}>重置筛选</button>
                            <button className="btn-primary" onClick={handleSearch}>搜索本页</button>
                        </div>
                    </div>
                </div>

                {requestedTemplateId !== null && (
                    <section className="section-card linked-template" aria-label="指定模板定位" aria-live="polite">
                        <h4>指定模板 #{requestedTemplateId}</h4>
                        {(!linkedStateMatchesTarget || linkedTemplateLoading) && <p>正在按 ID 读取模板详情...</p>}
                        {linkedStateMatchesTarget && linkedTemplateError && <p role="alert">{linkedTemplateError}</p>}
                        {visibleLinkedTemplate && <>
                            <p>{visibleLinkedTemplate.name} · {getStatusLabel(visibleLinkedTemplate.status).label} · {getCategoryLabel(visibleLinkedTemplate.category)}</p>
                            <p>{visibleLinkedTemplate.description || '暂无说明'}</p>
                            <button className="btn-primary" onClick={openLinkedTemplateEditor} disabled={templateWritesRef.current.has(visibleLinkedTemplate.id) || editorSaving}>编辑指定模板</button>
                        </>}
                        {!linkedTemplateLoading && <button className="btn-secondary" onClick={loadLinkedTemplate}>{linkedTemplateError ? '重新加载指定模板' : '刷新指定模板'}</button>}
                        <p className="template-feedback">按 ID 定位，不受下方列表筛选或分页影响。此处只读取信息，点击编辑后再保存才会修改。</p>
                    </section>
                )}

                {tabConfigSectionOpen && (
                    <div className="section-card tab-config-management">
                        <h4>模板广场标签配置（小程序与发布页统一读取）</h4>
                        <fieldset className="tab-config-grid" disabled={!tabConfigLoadedRef.current || tabConfigLoading}>
                            <div className="tab-config-block">
                                <h5>一级标签（main_tabs）</h5>
                                {mainTabs.map((t, i) => (
                                    <div key={i} className="tab-config-row">
                                        <input
                                            className="form-input small"
                                            placeholder="显示名"
                                            value={t.label}
                                            onChange={(e) => updateMainTabLabel(i, e.target.value)}
                                        />
                                        <input
                                            className="form-input small"
                                            placeholder="value"
                                            value={t.value}
                                            onChange={(e) => updateMainTabValue(i, e.target.value)}
                                        />
                                        <button type="button" className="btn-action btn-delete" onClick={() => removeMainTab(i)} title="删除">
                                            <FiTrash2Icon size={14} />
                                        </button>
                                    </div>
                                ))}
                                <button type="button" className="btn-secondary btn-sm" onClick={addMainTab}>
                                    新增一级标签
                                </button>
                            </div>
                            <div className="tab-config-block">
                                <h5>二级标签（sub_tabs，必须隶属于一级标签）</h5>
                                {subTabs.map((t, i) => (
                                    <div key={i} className="tab-config-row">
                                        <input
                                            className="form-input small"
                                            placeholder="显示名"
                                            value={t.label}
                                            onChange={(e) => updateSubTab(i, { label: e.target.value })}
                                        />
                                        <input
                                            className="form-input small"
                                            placeholder="value"
                                            value={t.value}
                                            onChange={(e) => updateSubTab(i, { value: e.target.value })}
                                        />
                                        <select
                                            className="form-input small"
                                            value={t.parent || ''}
                                            onChange={(e) => updateSubTab(i, { parent: e.target.value })}
                                        >
                                            <option value="">请选择父标签</option>
                                            {mainTabs.map((mt, mtIndex) => <option key={`${mt.value || 'empty'}-${mtIndex}`} value={mt.value}>{mt.label || '未命名一级标签'}</option>)}
                                        </select>
                                        <button type="button" className="btn-action btn-delete" onClick={() => {
                                            const removedValue = (subTabs[i]?.value || '').trim();
                                            setSubTabs(subTabs.filter((_, j) => j !== i));
                                            if (removedValue) {
                                                setThirdTabs(thirdTabs.filter(item => (item.parent || '').trim() !== removedValue));
                                            }
                                        }} title="删除">
                                            <FiTrash2Icon size={14} />
                                        </button>
                                    </div>
                                ))}
                                <button type="button" className="btn-secondary btn-sm" onClick={addSubTab}>
                                    新增二级标签
                                </button>
                            </div>
                            <div className="tab-config-block">
                                <h5>三级标签（third_tabs，必须隶属于二级标签）</h5>
                                {thirdTabs.map((t, i) => (
                                    <div key={i} className="tab-config-row">
                                        <input
                                            className="form-input small"
                                            placeholder="显示名"
                                            value={t.label}
                                            onChange={(e) => setThirdTabs(thirdTabs.map((item, itemIndex) => itemIndex === i ? { ...item, label: e.target.value } : item))}
                                        />
                                        <input
                                            className="form-input small"
                                            placeholder="value"
                                            value={t.value}
                                            onChange={(e) => setThirdTabs(thirdTabs.map((item, itemIndex) => itemIndex === i ? { ...item, value: e.target.value } : item))}
                                        />
                                        <select
                                            className="form-input small"
                                            value={t.parent || ''}
                                            onChange={(e) => setThirdTabs(thirdTabs.map((item, itemIndex) => itemIndex === i ? { ...item, parent: e.target.value } : item))}
                                        >
                                            <option value="">请选择父标签</option>
                                            {subTabs.map((st, stIndex) => <option key={`${st.value || 'empty'}-${stIndex}`} value={st.value}>{st.label || '未命名二级标签'}</option>)}
                                        </select>
                                        <button type="button" className="btn-action btn-delete" onClick={() => setThirdTabs(thirdTabs.filter((_, j) => j !== i))} title="删除">
                                            <FiTrash2Icon size={14} />
                                        </button>
                                    </div>
                                ))}
                                <button type="button" className="btn-secondary btn-sm" onClick={addThirdTab}>
                                    新增三级标签
                                </button>
                            </div>
                        </fieldset>
                        <div className="tab-config-footer">
                            {!tabConfigLoadedRef.current && <button className="btn-secondary" onClick={() => loadTabConfig()} disabled={tabConfigLoading}>{tabConfigLoading ? '正在加载标签...' : '重新加载标签配置'}</button>}
                            <span style={{ fontSize: 12, color: tabConfigSaveState === 'error' || tabConfigSaveState === 'invalid' ? '#d14343' : tabConfigSaveState === 'saved' ? '#2f7a4d' : '#666' }}>
                                {tabConfigSaveMessage || '修改后将自动保存'}
                            </span>
                            <button className="btn-primary" onClick={() => { void handleSaveTabConfig(); }} disabled={tabConfigSaving || tabConfigLoading || !tabConfigLoadedRef.current}>
                                {tabConfigSaving ? '保存中...' : '保存标签配置'}
                            </button>
                        </div>
                    </div>
                )}

                {featuredGroupSectionOpen && (
                    <div className="section-card featured-group-management">
                        <div className="section-header">
                            <h4>首页精选案例组管理</h4>
                            <button className="btn-primary btn-sm" disabled={featuredGroupSaving} onClick={() => openFeaturedGroupEditor()}>
                                <FiPlusIcon /> 新建案例组
                            </button>
                        </div>
                        <p className="section-desc">案例组会显示在小程序首页的精选案例区域，每组可配置 1-2 个案例，支持对比、并排和普通模式。</p>
                        {featuredGroupsError && <div className="template-feedback" role="alert">{featuredGroupsError}{featuredGroupsLoaded ? '（保留上次成功结果）' : ''}<button className="btn-secondary" onClick={loadFeaturedGroups}>重新加载案例组</button></div>}
                        {loadingFeaturedGroups ? (
                            <div className="loading-state">加载中...</div>
                        ) : (
                            <div className="featured-group-list">
                                {!featuredGroups || featuredGroups.length === 0 ? (
                                    <div className="empty-state">{featuredGroupsLoaded && !featuredGroupsError ? '暂无精选案例组' : '案例组列表尚未成功加载'}</div>
                                ) : (
                                    featuredGroups.map((group) => {
                                        if (!group) return null;
                                        return (
                                            <div key={group.id} className="featured-group-item">
                                                <div className="featured-group-content">
                                                    <div className="featured-group-header">
                                                        <h5>{group.name || '未命名案例组'}</h5>
                                                        <span className="group-mode-badge">{group.display_mode === 'comparison' ? '对比模式' : group.display_mode === 'side_by_side' ? '并排模式' : group.display_mode === 'normal' ? '普通模式' : `未知模式（${group.display_mode || '未提供'}）`}</span>
                                                    </div>
                                                    <div className="featured-group-cases">
                                                        <div className="case-item">
                                                            <div className="case-label">{group.case1_label || '案例1'}</div>
                                                            {group.case1 ? (
                                                                <div className="case-info">
                                                                    <img src={group.case1.thumbnail || group.case1.preview_url || '/placeholder.png'} alt={group.case1.name || '案例'} className="case-thumbnail" />
                                                                    <span>{group.case1.name || '未命名案例'}</span>
                                                                </div>
                                                            ) : (
                                                                <span className="case-missing">案例不存在</span>
                                                            )}
                                                        </div>
                                                        {group.case2_id > 0 && (
                                                            <>
                                                                <div className="case-divider">VS</div>
                                                                <div className="case-item">
                                                                    <div className="case-label">{group.case2_label || '案例2'}</div>
                                                                    {group.case2 ? (
                                                                        <div className="case-info">
                                                                            <img src={group.case2.thumbnail || group.case2.preview_url || '/placeholder.png'} alt={group.case2.name || '案例'} className="case-thumbnail" />
                                                                            <span>{group.case2.name || '未命名案例'}</span>
                                                                        </div>
                                                                    ) : (
                                                                        <span className="case-missing">案例不存在</span>
                                                                    )}
                                                                </div>
                                                            </>
                                                        )}
                                                    </div>
                                                    <div className="featured-group-meta">
                                                        <span>排序: {group.sort_order ?? 0}</span>
                                                    </div>
                                                </div>
                                                <div className="featured-group-actions">
                                                    <button className="btn-action btn-edit" onClick={() => openFeaturedGroupEditor(group)} disabled={featuredGroupSaving} title="编辑">
                                                        <FiEditIcon size={14} />
                                                    </button>
                                                    <button
                                                        className="btn-action btn-delete"
                                                        onClick={() => handleDeleteFeaturedGroup(group.id)}
                                                        disabled={featuredGroupSaving}
                                                        title="删除"
                                                    >
                                                        <FiTrash2Icon size={14} />
                                                    </button>
                                                </div>
                                            </div>
                                        );
                                    })
                                )}
                            </div>
                        )}
                        <nav className="template-pagination" aria-label="精选案例组分页">
                            <span>共 {featuredTotal ?? '—'} 组 · 第 {featuredPage} 页 · 共 {featuredTotal === null ? '—' : Math.max(1, Math.ceil(featuredTotal / featuredPageSize))} 页 · 每页 {featuredPageSize} 组</span>
                            <button className="btn-secondary" disabled={loadingFeaturedGroups || featuredPage <= 1} onClick={() => { if (featuredPage === featuredPageRef.current) changeFeaturedPage(featuredPage - 1); }}>上一页</button>
                            <button className="btn-secondary" disabled={loadingFeaturedGroups || featuredTotal === null || featuredPage * featuredPageSize >= featuredTotal} onClick={() => { if (featuredPage === featuredPageRef.current && featuredTotal !== null && featuredPage * featuredPageSize < featuredTotal) changeFeaturedPage(featuredPage + 1); }}>下一页</button>
                        </nav>
                    </div>
                )}

                {categorySectionOpen && (
                    <div className="category-management section-card">
                        <h4>分类列表</h4>
                        <div className="category-list">
                            {categories.map((c) => (
                                <div key={c.id} className="category-row">
                                    <span><strong>{c.id}</strong> - {c.name}</span>
                                    <button className="btn-action btn-delete" onClick={() => handleDeleteCategory(c.id)} title="删除分类">
                                        <FiTrash2Icon size={14} />
                                    </button>
                                </div>
                            ))}
                        </div>
                        <div className="category-add">
                            <input
                                placeholder="分类ID（英文）"
                                value={categoryForm.id}
                                onChange={(e) => setCategoryForm(f => ({ ...f, id: e.target.value }))}
                                className="form-input small"
                            />
                            <input
                                placeholder="分类名称"
                                value={categoryForm.name}
                                onChange={(e) => setCategoryForm(f => ({ ...f, name: e.target.value }))}
                                className="form-input small"
                            />
                            <input
                                type="number"
                                placeholder="排序"
                                value={categoryForm.sort_order || ''}
                                onChange={(e) => setCategoryForm(f => ({ ...f, sort_order: parseInt(e.target.value, 10) || 0 }))}
                                className="form-input small"
                                style={{ width: 80 }}
                            />
                            <button className="btn-primary" onClick={handleAddCategory}>新增分类</button>
                        </div>
                    </div>
                )}

                <div className="templates-stats">
                    <div className="stat-item">
                        <span className="stat-label">筛选结果总数</span>
                        <span className="stat-value">{templatesLoaded ? templateTotal : '—'}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本页待审核</span>
                        <span className="stat-value">{templatesLoaded ? templates.filter(t => t.status === 'pending').length : '—'}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本页已发布</span>
                        <span className="stat-value">{templatesLoaded ? templates.filter(t => t.status === 'published').length : '—'}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本页下载量</span>
                        <span className="stat-value">{templatesLoaded ? templates.reduce((sum, t) => sum + t.downloadCount, 0) : '—'}</span>
                    </div>
                </div>

                <p className="template-feedback">关键词搜索仅覆盖本页；当前接口未提供全库关键词检索。</p>
                {templatesError && <div className="template-feedback" role="alert">{templatesError}{templatesLoaded ? '，当前显示上次加载的数据。' : '，尚不能确认列表为空。'}<button className="btn-secondary" onClick={loadTemplates}>重新加载模板</button></div>}
                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="templates-grid">
                        {filteredTemplates.length === 0 ? (
                            <div className="empty-state">{templatesError ? '当前模板列表未能加载' : searchKeyword ? '本页没有匹配的模板，可翻页继续查找' : '当前筛选下暂无模板数据'}</div>
                        ) : (
                            filteredTemplates.map((template) => (
                                <div key={template.id} className="template-card">
                                    <div className="template-thumbnail">
                                        {template.thumbnail ? (
                                            <img src={template.thumbnail} alt={template.name} />
                                        ) : (
                                            <div className="thumbnail-placeholder"><FiEyeIcon size={32} /></div>
                                        )}
                                    </div>
                                    <div className="template-content">
                                        <div className="template-header">
                                            <h3 className="template-name">{template.name}</h3>
                                            <span className={`status-badge ${getStatusLabel(template.status).className}`}>
                                                {getStatusLabel(template.status).label}
                                            </span>
                                        </div>
                                        <p className="template-description">
                                            {template.description.slice(0, 80)}{template.description.length > 80 ? '...' : ''}
                                        </p>
                                        {template.rejectReason ? (
                                            <p className="template-description">拒绝原因：{template.rejectReason}</p>
                                        ) : null}
                                        <dl className="template-meta">
                                            <div><dt>分类</dt><dd>{getCategoryLabel(template.category) || '未提供'}</dd></div>
                                            <div><dt>去向</dt><dd>{getPublishScopeLabel(template.publishScope)}</dd></div>
                                            <div><dt>来源</dt><dd>{getSourceTypeLabel(template.sourceType)}</dd></div>
                                            <div><dt>尺寸</dt><dd>{formatImageMeta(template.imageWidth, template.imageHeight)}</dd></div>
                                            <div><dt>下载</dt><dd>{template.downloadCount}</dd></div>
                                        </dl>
                                        <div className="template-footer">
                                            <div className="template-actions">
                                                {template.status === 'pending' && (
                                                    <>
                                                        <button className="btn-action btn-approve" onClick={() => handleApprove(template)} disabled={templateWritesRef.current.has(template.id)} title="通过">
                                                            <FiCheckIcon size={14} /> 通过
                                                        </button>
                                                        <button className="btn-action btn-reject" onClick={() => handleReject(template)} disabled={templateWritesRef.current.has(template.id)} title="拒绝">
                                                            <FiXIcon size={14} /> 拒绝
                                                        </button>
                                                    </>
                                                )}
                                                {template.status === 'published' && (
                                                    <button
                                                        className={`btn-action ${template.isFeatured ? 'btn-featured-active' : 'btn-featured'}`}
                                                        onClick={() => handleSetFeatured(template, !template.isFeatured)}
                                                        disabled={templateWritesRef.current.has(template.id)}
                                                        title={template.isFeatured ? '取消精选' : '设为精选'}
                                                    >
                                                        {template.isFeatured ? '已精选' : '设为精选'}
                                                    </button>
                                                )}
                                                <button className="btn-action btn-move" onClick={() => handleMoveCategory(template)} disabled={templateWritesRef.current.has(template.id)} title="移动分类">
                                                    <FiFolderIcon size={14} />
                                                </button>
                                                <button className="btn-action btn-edit" onClick={() => handleEdit(template)} disabled={templateWritesRef.current.has(template.id)} title="编辑">
                                                    <FiEditIcon size={14} />
                                                </button>
                                                <button className="btn-action btn-delete" onClick={() => handleDelete(template.id)} disabled={templateWritesRef.current.has(template.id)} title="删除">
                                                    <FiTrash2Icon size={14} />
                                                </button>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            ))
                        )}
                    </div>
                )}

                <nav className="template-pagination" aria-label="模板分页">
                    <button className="btn-secondary" disabled={loading || templatePage <= 1} onClick={() => setTemplatePage(page => Math.max(1, page - 1))}>上一页模板</button>
                    <span>第 {templatePage} / {templatesLoaded ? Math.max(1, Math.ceil(templateTotal / templatePageSize)) : '—'} 页，每页 {templatePageSize} 条</span>
                    <button className="btn-secondary" disabled={loading || !templatesLoaded || templatePage * templatePageSize >= templateTotal} onClick={() => setTemplatePage(page => page + 1)}>下一页模板</button>
                </nav>

                {/* 新增模板弹窗 */}
                {showAddModal && (
                    <div className="modal-overlay" onClick={dismissTemplateEditor}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>添加模板</h3>
                                <button className="modal-close" onClick={dismissTemplateEditor}>×</button>
                            </div>
                            <fieldset className="modal-body" disabled={editorSaving}>
                                <div className="form-group">
                                    <label>名称 *</label>
                                    <input className="form-input" value={addFormData.name} onChange={(e) => updateAddForm(f => ({ ...f, name: e.target.value }))} placeholder="模板名称" />
                                </div>
                                {/* 分类由系统根据配置自动处理，这里不再单独选择 */}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>一级标签（可选）</label>
                                        <select className="form-input" value={addFormData.mainTab} onChange={(e) => {
                                            const mainTabValue = e.target.value;
                                            updateAddForm(f => ({ ...f, mainTab: mainTabValue, subTab: '', thirdTab: '' }));
                                        }}>
                                            <option value="">不设置（仅分类）</option>
                                            {mainTabs.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>二级标签（可选）</label>
                                        <select className="form-input" value={addFormData.subTab} onChange={(e) => updateAddForm(f => ({ ...f, subTab: e.target.value, thirdTab: '' }))} disabled={!addFormData.mainTab}>
                                            <option value="">不设置（仅父标签）</option>
                                            {addFormData.mainTab && getChildTabsByParent(subTabs, addFormData.mainTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label>三级Tab（可选）</label>
                                    <select className="form-input" value={addFormData.thirdTab} onChange={(e) => updateAddForm(f => ({ ...f, thirdTab: e.target.value }))} disabled={!addFormData.subTab || getChildTabsByParent(thirdTabs, addFormData.subTab).length === 0}>
                                        <option value="">不设置（仅二级Tab）</option>
                                        {addFormData.subTab && getChildTabsByParent(thirdTabs, addFormData.subTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>描述</label>
                                    <textarea className="form-input" rows={3} value={addFormData.description} onChange={(e) => updateAddForm(f => ({ ...f, description: e.target.value }))} placeholder="模板描述" />
                                </div>
                                <div className="form-group">
                                    <label>模板图片（支持多图）</label>
                                    <div className="image-upload-group">
                                        <input ref={addUploadRef} type="file" accept="image/*" multiple
                                            style={{ display: 'none' }} onChange={event => handleImageUpload(event, 'add')} />
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            disabled={uploading || editorSaving}
                                            onClick={() => { if (isCurrentEditor('add')) addUploadRef.current?.click(); }}
                                        >
                                            选择图片并上传
                                        </button>
                                    </div>
                                    {addFormData.imageUrls && addFormData.imageUrls.length > 0 && (
                                        <div className="image-preview-list">
                                            {addFormData.imageUrls.map((url, idx) => (
                                                <div key={url + idx} className="image-preview-item">
                                                    <img
                                                        src={url}
                                                        alt={`图片${idx + 1}`}
                                                        className="image-preview-thumb"
                                                        onClick={() => window.open(url, '_blank')}
                                                    />
                                                    <div className="image-preview-meta">
                                                        {idx === 0 && <span className="badge-primary">首图（缩略图/主图）</span>}
                                                        <button
                                                            type="button"
                                                            className="btn-action btn-delete"
                                                            onClick={() =>
                                                                updateAddForm(f => ({
                                                                    ...f,
                                                                    imageUrls: (f.imageUrls || []).filter((_, i) => i !== idx),
                                                                }))
                                                            }
                                                        >
                                                            删除
                                                        </button>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>价格（灵石）</label>
                                        <input type="number" className="form-input" value={addFormData.price || ''} onChange={(e) => updateAddForm(f => ({ ...f, price: parseInt(e.target.value, 10) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-inline">
                                        <label>
                                            <input type="checkbox" checked={addFormData.isFree} onChange={(e) => updateAddForm(f => ({ ...f, isFree: e.target.checked }))} />
                                            免费
                                        </label>
                                    </div>
                                    <div className="form-group">
                                        <label>状态</label>
                                        <select className="form-input" value={addFormData.status} onChange={(e) => updateAddForm(f => ({ ...f, status: e.target.value }))}>
                                            <option value="draft">草稿</option>
                                            <option value="published">已发布</option>
                                            <option value="archived">已归档</option>
                                        </select>
                                    </div>
                                </div>
                            </fieldset>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={dismissTemplateEditor}>取消</button>
                                <button className="btn-primary" onClick={handleAddTemplate} disabled={uploading || editorSaving}>保存</button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 编辑模板弹窗 */}
                {showEditModal && selectedTemplate && (!renderedEditorLinkedTarget || isCurrentLinkedTarget(renderedEditorLinkedTarget)) && (
                    <div className="modal-overlay" onClick={dismissTemplateEditor}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>编辑模板</h3>
                                <button className="modal-close" onClick={dismissTemplateEditor}>×</button>
                            </div>
                            <fieldset className="modal-body" disabled={templateWritesRef.current.has(selectedTemplate.id)}>
                                <div className="form-group">
                                    <label>名称 *</label>
                                    <input className="form-input" value={editFormData.name} onChange={(e) => updateEditForm(f => ({ ...f, name: e.target.value }))} placeholder="模板名称" />
                                </div>
                                {/* 分类由系统根据配置自动处理，这里不再单独选择 */}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>一级标签（可选）</label>
                                        <select className="form-input" value={editFormData.mainTab} onChange={(e) => {
                                            const mainTabValue = e.target.value;
                                            updateEditForm(f => ({ ...f, mainTab: mainTabValue, subTab: '', thirdTab: '' }));
                                        }}>
                                            <option value="">不设置（仅分类）</option>
                                            {mainTabs.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>二级标签（可选）</label>
                                        <select className="form-input" value={editFormData.subTab} onChange={(e) => updateEditForm(f => ({ ...f, subTab: e.target.value, thirdTab: '' }))} disabled={!editFormData.mainTab}>
                                            <option value="">不设置（仅父标签）</option>
                                            {editFormData.mainTab && getChildTabsByParent(subTabs, editFormData.mainTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label>三级Tab（可选）</label>
                                    <select className="form-input" value={editFormData.thirdTab} onChange={(e) => updateEditForm(f => ({ ...f, thirdTab: e.target.value }))} disabled={!editFormData.subTab || getChildTabsByParent(thirdTabs, editFormData.subTab).length === 0}>
                                        <option value="">不设置（仅二级Tab）</option>
                                        {editFormData.subTab && getChildTabsByParent(thirdTabs, editFormData.subTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>描述</label>
                                    <textarea className="form-input" rows={3} value={editFormData.description} onChange={(e) => updateEditForm(f => ({ ...f, description: e.target.value }))} placeholder="模板描述" />
                                </div>
                                <div className="readonly-info-panel">
                                    <div className="readonly-info-title">运营信息</div>
                                    <div className="readonly-info-grid">
                                        <div className="readonly-info-item">
                                            <span className="readonly-info-label">分类</span>
                                            <span className="readonly-info-value">{getCategoryLabel(selectedTemplate.category)}</span>
                                        </div>
                                        <div className="readonly-info-item">
                                            <span className="readonly-info-label">去向</span>
                                            <span className="readonly-info-value">{getPublishScopeLabel(selectedTemplate.publishScope)}</span>
                                        </div>
                                        <div className="readonly-info-item">
                                            <span className="readonly-info-label">来源</span>
                                            <span className="readonly-info-value">{getSourceTypeLabel(selectedTemplate.sourceType)}</span>
                                        </div>
                                        <div className="readonly-info-item">
                                            <span className="readonly-info-label">下载</span>
                                            <span className="readonly-info-value">{selectedTemplate.downloadCount}</span>
                                        </div>
                                        <div className="readonly-info-item">
                                            <span className="readonly-info-label">尺寸</span>
                                            <span className="readonly-info-value">{formatImageMeta(selectedTemplate.imageWidth, selectedTemplate.imageHeight)}</span>
                                        </div>
                                        <div className="readonly-info-item">
                                            <span className="readonly-info-label">创建者</span>
                                            <span className="readonly-info-value">{selectedTemplate.creator || '-'}</span>
                                        </div>
                                    </div>
                                    {selectedTemplate.rejectReason && <p className="template-feedback">拒绝原因：{selectedTemplate.rejectReason}</p>}
                                </div>
                                <div className="form-group">
                                    <label>模板图片（支持多图）</label>
                                    <div className="image-upload-group">
                                        <input ref={editUploadRef} type="file" accept="image/*" multiple
                                            style={{ display: 'none' }} onChange={event => handleImageUpload(event, 'edit')} />
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            disabled={uploading || editorSaving || templateWritesRef.current.has(selectedTemplate.id)}
                                            onClick={() => { if (isCurrentEditor('edit')) editUploadRef.current?.click(); }}
                                        >
                                            选择图片并上传
                                        </button>
                                    </div>
                                    {editFormData.imageUrls && editFormData.imageUrls.length > 0 && (
                                        <div className="image-preview-list">
                                            {editFormData.imageUrls.map((url, idx) => (
                                                <div key={url + idx} className="image-preview-item">
                                                    <img
                                                        src={url}
                                                        alt={`图片${idx + 1}`}
                                                        className="image-preview-thumb"
                                                        onClick={() => window.open(url, '_blank')}
                                                    />
                                                    <div className="image-preview-meta">
                                                        {idx === 0 && <span className="badge-primary">首图（缩略图/主图）</span>}
                                                        <button
                                                            type="button"
                                                            className="btn-action btn-delete"
                                                            onClick={() =>
                                                                updateEditForm(f => ({
                                                                    ...f,
                                                                    imageUrls: (f.imageUrls || []).filter((_, i) => i !== idx),
                                                                }))
                                                            }
                                                        >
                                                            删除
                                                        </button>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>价格（灵石）</label>
                                        <input type="number" className="form-input" value={editFormData.price || ''} onChange={(e) => updateEditForm(f => ({ ...f, price: parseInt(e.target.value, 10) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-inline">
                                        <label>
                                            <input type="checkbox" checked={editFormData.isFree} onChange={(e) => updateEditForm(f => ({ ...f, isFree: e.target.checked }))} />
                                            免费
                                        </label>
                                    </div>
                                    <div className="form-group">
                                        <label>状态</label>
                                        <select className="form-input" value={editFormData.status} onChange={(e) => updateEditForm(f => ({ ...f, status: e.target.value }))}>
                                            {!['draft', 'pending', 'published', 'archived'].includes(selectedTemplate.status) && <option value={selectedTemplate.status}>{getStatusLabel(selectedTemplate.status).label || '未知状态'}（当前）</option>}
                                            <option value="draft">草稿</option>
                                            <option value="pending">待审核</option>
                                            <option value="published">已发布</option>
                                            <option value="archived">已归档</option>
                                        </select>
                                    </div>
                                </div>
                            </fieldset>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={dismissTemplateEditor}>取消</button>
                                <button className="btn-primary" onClick={handleSaveEdit} disabled={uploading || editorSaving || templateWritesRef.current.has(selectedTemplate.id)}>保存</button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 移动分类弹窗 */}
                {showMoveCategoryModal && moveTargetTemplate && (
                    <div className="modal-overlay" onClick={dismissMoveCategory}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>移动“{moveTargetTemplate.name}”到分类</h3>
                                <button className="modal-close" onClick={dismissMoveCategory}>×</button>
                            </div>
                            <div className="modal-body">
                                <p className="modal-hint">点击目标分类完成移动。</p>
                                <div className="move-category-btns">
                                    {categories.filter(c => c.id !== moveTargetTemplate.category).map(c => (
                                        <button
                                            key={c.id}
                                            className="btn-secondary"
                                            onClick={() => confirmMoveCategory(c.id)}
                                            disabled={templateWritesRef.current.has(moveTargetTemplate.id)}
                                        >
                                            {c.name}
                                        </button>
                                    ))}
                                    {categories.filter(c => c.id !== moveTargetTemplate.category).length === 0 && (
                                        <span className="text-muted">暂无其他分类</span>
                                    )}
                                </div>
                            </div>
                        </div>
                    </div>
                )}

                {/* 精选案例组编辑弹窗 */}
                {showFeaturedGroupModal && (
                    <div className="modal-overlay" onClick={dismissFeaturedGroupEditor}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{selectedFeaturedGroup ? '编辑精选案例组' : '新建精选案例组'}</h3>
                                <button className="modal-close" onClick={dismissFeaturedGroupEditor}>×</button>
                            </div>
                            <fieldset className="modal-body" disabled={featuredGroupSaving}>
                                <div className="form-group">
                                    <label>组名称 *</label>
                                    <input
                                        className="form-input"
                                        value={featuredGroupForm.name}
                                        onChange={(e) => updateFeaturedForm(f => ({ ...f, name: e.target.value }))}
                                        placeholder="例如：真实场景 vs AI 设计"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>显示模式 *</label>
                                    <select
                                        className="form-input"
                                        value={featuredGroupForm.display_mode}
                                        onChange={(e) => updateFeaturedForm(f => ({ ...f, display_mode: e.target.value }))}
                                    >
                                        {!isFeaturedDisplayMode(featuredGroupForm.display_mode) && <option value={featuredGroupForm.display_mode}>未知模式（{featuredGroupForm.display_mode || '未提供'}），请明确选择</option>}
                                        <option value="comparison">对比模式（真实 vs AI）</option>
                                        <option value="side_by_side">并排模式（真实和 AI）</option>
                                        <option value="normal">普通模式（单个案例）</option>
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>第一个案例 *</label>
                                    <select
                                        className="form-input"
                                        value={featuredGroupForm.case1_id}
                                        onChange={(e) => updateFeaturedForm(f => ({ ...f, case1_id: e.target.value }))}
                                    >
                                        <option value="">请选择案例</option>
                                        {featuredOptionEntries.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
                                    </select>
                                </div>
                                {featuredGroupForm.display_mode !== 'normal' && (
                                    <div className="form-group">
                                        <label>第二个案例 *</label>
                                        <select
                                            className="form-input"
                                            value={featuredGroupForm.case2_id}
                                            onChange={(e) => updateFeaturedForm(f => ({ ...f, case2_id: e.target.value }))}
                                        >
                                            <option value="">请选择案例</option>
                                            {featuredOptionEntries.filter(item => item.id !== featuredGroupForm.case1_id || item.id === featuredGroupForm.case2_id).map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
                                        </select>
                                    </div>
                                )}
                                <div className="template-feedback" aria-live="polite">
                                    <p>案例选项独立读取已发布模板，不受主列表分类、状态或页码影响。当前已选 ID 会保留。</p>
                                    {featuredOptionsError && <p role="alert">{featuredOptionsError}</p>}
                                    <span>{featuredOptionsLoading ? '正在加载案例...' : `已加载 ${featuredOptions.length} / ${featuredOptionsTotal} 个已发布案例`}</span>
                                    {featuredOptionsError ? <button type="button" className="btn-secondary" onClick={() => loadFeaturedOptions(featuredOptionsPage ? featuredOptionsPage + 1 : 1, renderedFeaturedSession)} disabled={featuredOptionsLoading}>重试加载案例</button> : featuredOptions.length < featuredOptionsTotal && <button type="button" className="btn-secondary" onClick={() => loadFeaturedOptions(featuredOptionsPage + 1, renderedFeaturedSession)} disabled={featuredOptionsLoading}>加载更多案例</button>}
                                    {featuredGroupForm.display_mode === 'normal' && featuredGroupForm.case2_id && <p>普通模式保留现有第二案例 #{featuredGroupForm.case2_id}，不会自动清空。</p>}
                                </div>
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>第一个案例标签</label>
                                        <input
                                            className="form-input"
                                            value={featuredGroupForm.case1_label}
                                            onChange={(e) => updateFeaturedForm(f => ({ ...f, case1_label: e.target.value }))}
                                            placeholder="例如：真实"
                                        />
                                    </div>
                                    {featuredGroupForm.display_mode !== 'normal' && (
                                        <div className="form-group">
                                            <label>第二个案例标签</label>
                                            <input
                                                className="form-input"
                                                value={featuredGroupForm.case2_label}
                                                onChange={(e) => updateFeaturedForm(f => ({ ...f, case2_label: e.target.value }))}
                                                placeholder="例如：AI"
                                            />
                                        </div>
                                    )}
                                </div>
                                <div className="form-group">
                                    <label>排序顺序</label>
                                    <input
                                        type="number"
                                        className="form-input"
                                        value={featuredGroupForm.sort_order}
                                        onChange={(e) => updateFeaturedForm(f => ({ ...f, sort_order: parseInt(e.target.value, 10) || 0 }))}
                                        placeholder="数字越小越靠前"
                                    />
                                </div>
                            </fieldset>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={dismissFeaturedGroupEditor}>取消</button>
                                <button
                                    className="btn-primary"
                                    onClick={handleSaveFeaturedGroup}
                                    disabled={featuredGroupSaving}
                                >
                                    {featuredGroupSaving ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default Templates;
