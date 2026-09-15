import { reportPageLoadError } from '../utils/pageLoadError';
﻿import React, { useState, useEffect, useCallback, useRef } from 'react';
import { FiSearch, FiPlus, FiEdit, FiTrash2, FiEye, FiCheck, FiX, FiFolder } from 'react-icons/fi';
import Layout from '../component/layout';
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
    type TemplateCategoryItem,
    type TabConfigItem,
    type FeaturedCaseGroup,
} from '../api/templates';
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
    // 鍙岄噸 Tab 褰掔被
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
    // 涓婁紶鍚庣殑鍥剧墖URL鍒楄〃锛堝鍥撅級锛岀1寮犻粯璁や綔涓虹缉鐣ュ浘/涓婚瑙?
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

type TabConfigSaveState = 'idle' | 'editing' | 'saving' | 'saved' | 'invalid' | 'error';

const getChildTabsByParent = (items: TabConfigItem[], parentValue: string) => {
    const currentParent = (parentValue || '').trim();
    if (!currentParent) {
        return [];
    }
    return items.filter(item => (item.parent || '').trim() === currentParent);
};

const Templates: React.FC = () => {
    const initialKeyword = new URLSearchParams(window.location.search).get('keyword') || '';
    const [templates, setTemplates] = useState<Template[]>([]);
    const [categories, setCategories] = useState<TemplateCategoryItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState(initialKeyword);
    const [searchKeyword, setSearchKeyword] = useState(initialKeyword);
    const [categoryFilter, setCategoryFilter] = useState<string>('all');
    const [statusFilter, setStatusFilter] = useState<string>('all');
    const [showAddModal, setShowAddModal] = useState(false);
    const [showEditModal, setShowEditModal] = useState(false);
    const [showCategoryModal, setShowCategoryModal] = useState(false);
    const [showMoveCategoryModal, setShowMoveCategoryModal] = useState(false);
    const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
    const [moveTargetTemplate, setMoveTargetTemplate] = useState<Template | null>(null);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [formData, setFormData] = useState<typeof defaultForm>(defaultForm);
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
    const [featuredGroupSectionOpen, setFeaturedGroupSectionOpen] = useState(false);
    const [featuredGroups, setFeaturedGroups] = useState<FeaturedCaseGroup[]>([]);
    const [loadingFeaturedGroups, setLoadingFeaturedGroups] = useState(false);
    const [showFeaturedGroupModal, setShowFeaturedGroupModal] = useState(false);
    const [selectedFeaturedGroup, setSelectedFeaturedGroup] = useState<FeaturedCaseGroup | null>(null);
    const [featuredGroupForm, setFeaturedGroupForm] = useState({
        name: '',
        display_mode: 'comparison' as 'comparison' | 'side_by_side' | 'normal',
        case1_id: '',
        case2_id: '',
        case1_label: '鐪熷疄',
        case2_label: 'AI',
        sort_order: 0,
    });

    const convertTemplate = (t: ApiTemplate): Template => ({
        id: String(t.id),
        name: t.name,
        category: t.category,
        // 鍚庣瀛楁 main_tab / sub_tab 鏄犲皠鍒板墠绔?Template 缁撴瀯
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
        publishScope: t.publish_scope || 'square',
        rejectReason: t.reject_reason || '',
        sourceType: t.source_type || 'admin_upload',
        creator: t.creator || '',
        createdAt: t.created_at,
        updatedAt: t.updated_at,
    });

    const loadCategories = useCallback(async () => {
        try {
            const list = await getTemplateCategories();
            setCategories(list);
        } catch (e) {
            console.error('鍔犺浇鍒嗙被澶辫触:', e);
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

    const loadTabConfig = useCallback(async () => {
        try {
            const cfg = await getTemplateTabConfig();
            const nextMainTabs = Array.isArray(cfg?.main_tabs) ? cfg.main_tabs.map(t => ({ ...t })) : [];
            const nextSubTabs = Array.isArray(cfg?.sub_tabs) ? cfg.sub_tabs.map(t => ({ ...t, parent: t.parent || '' })) : [];
            const nextThirdTabs = Array.isArray(cfg?.third_tabs) ? cfg.third_tabs.map(t => ({ ...t, parent: t.parent || '' })) : [];
            lastSavedTabConfigRef.current = serializeTabConfig(nextMainTabs, nextSubTabs, nextThirdTabs);
            tabConfigLoadedRef.current = true;
            setMainTabs(nextMainTabs);
            setSubTabs(nextSubTabs);
            setThirdTabs(nextThirdTabs);
            setTabConfigSaveState('idle');
            setTabConfigSaveMessage('');
        } catch (e) {
            console.error('鍔犺浇 Tab 閰嶇疆澶辫触:', e);
            setTabConfigSaveState('error');
            setTabConfigSaveMessage('鍔犺浇 Tab 閰嶇疆澶辫触锛岃绋嶅悗閲嶈瘯');
        }
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
            setTabConfigSaveMessage('鑷冲皯淇濈暀涓€涓竴绾ab');
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
            setTabConfigSaveMessage('璇峰厛淇濈暀鑷冲皯涓€涓竴绾ab锛屽啀鏂板浜岀骇Tab');
            return;
        }
        const defaultParent = mainTabs.find(item => (item.value || '').trim())?.value || mainTabs[0]?.value || '';
        setSubTabs(prev => [...prev, { label: '', value: '', parent: defaultParent }]);
    }, [mainTabs]);

    const addThirdTab = useCallback(() => {
        if (!subTabs.length) {
            setTabConfigSaveState('invalid');
            setTabConfigSaveMessage('鐠囧嘲鍘涙穱婵堟殌閼峰啿鐨稉鈧稉顏冪癌缁绢湚ab閿涘苯鍟€閺傛澘顤冩稉澶岄獓Tab');
            return;
        }
        const defaultParent = subTabs.find(item => (item.value || '').trim())?.value || subTabs[0]?.value || '';
        setThirdTabs(prev => [...prev, { label: '', value: '', parent: defaultParent }]);
    }, [subTabs]);

    const handleSaveTabConfig = useCallback(async (options?: { silent?: boolean }) => {
        const validationError = getTabConfigValidationError(mainTabs, subTabs, thirdTabs);
        if (validationError) {
            setTabConfigSaveState('invalid');
            setTabConfigSaveMessage(validationError);
            if (!options?.silent) {
                alert(validationError);
            }
            return false;
        }

        setTabConfigSaving(true);
        setTabConfigSaveState('saving');
        setTabConfigSaveMessage(options?.silent ? '姝ｅ湪鑷姩淇濆瓨...' : '姝ｅ湪淇濆瓨...');
        try {
            const payload = normalizeTabConfig(mainTabs, subTabs, thirdTabs);
            await putTemplateTabConfig(payload);
            lastSavedTabConfigRef.current = serializeTabConfig(mainTabs, subTabs, thirdTabs);
            setTabConfigSaveState('saved');
            setTabConfigSaveMessage(options?.silent ? '已自动保存' : '保存成功');
            if (!options?.silent) {
                alert('保存成功');
            }
            return true;
        } catch (e: any) {
            const message = e?.message || '保存失败';
            setTabConfigSaveState('error');
            setTabConfigSaveMessage(message);
            if (!options?.silent) {
                alert(message);
            }
            return false;
        } finally {
            setTabConfigSaving(false);
        }
    }, [getTabConfigValidationError, mainTabs, normalizeTabConfig, serializeTabConfig, subTabs, thirdTabs]);

    const loadTemplates = useCallback(async () => {
        setLoading(true);
        try {
            const params: any = { page: 1, page_size: 500 };
            if (categoryFilter !== 'all') params.category = categoryFilter;
            if (statusFilter !== 'all') params.status = statusFilter;
            const res = await getTemplateList(params);
            const list = (res?.list || []).map(convertTemplate);
            setTemplates(list);
        } catch (error) {
            console.error('鍔犺浇妯℃澘鍒楄〃澶辫触:', error);
            reportPageLoadError(error, '加载模板列表失败');
        } finally {
            setLoading(false);
        }
    }, [categoryFilter, statusFilter]);

    useEffect(() => {
        // 鍔犺浇鍒嗙被鍜屽弻閲?Tab 閰嶇疆锛岀‘淇濇坊鍔?缂栬緫寮圭獥鏈夊彲閫夌殑涓€绾?浜岀骇 Tab
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
        setTabConfigSaveMessage('妫€娴嬪埌淇敼锛屾鍦ㄥ噯澶囪嚜鍔ㄤ繚瀛?..');
        const timer = window.setTimeout(() => {
            void handleSaveTabConfig({ silent: true });
        }, 800);
        return () => window.clearTimeout(timer);
    }, [getTabConfigValidationError, handleSaveTabConfig, mainTabs, serializeTabConfig, subTabs, thirdTabs]);

    useEffect(() => {
        loadTemplates();
    }, [loadTemplates]);

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
        return publishScope === 'homepage_only' ? '仅首页展示' : '首页 + 模板广场';
    };

    const getSourceTypeLabel = (sourceType?: string) => {
        if (sourceType === 'album_upload') return '鐩稿唽涓婁紶';
        if (sourceType === 'ai_generated') return 'AI鐢熸垚鍙戝竷';
        return '鍚庡彴鍒涘缓';
    };

    const getCategoryLabel = (categoryId: string) => {
        const cat = categories.find(c => c.id === categoryId);
        return cat ? cat.name : categoryId;
    };

    const handleEdit = async (template: Template) => {
        try {
            if (!mainTabs.length || !subTabs.length) {
                await loadTabConfig();
            }
            const detail = await getTemplateDetail(template.id);
            // 杩樺師宸叉湁鍥剧墖锛?
            // 1锛変紭鍏堜粠 detail.images 瑙ｆ瀽锛堝瓧绗︿覆鏁扮粍鎴栧璞℃暟缁勶級锛?
            // 2锛夊鏋滄病鏈?images锛屼絾鏈?thumbnail/preview_url锛屽垯鐢ㄥ畠浠ˉ涓€寮狅紝閬垮厤缂栬緫鏃舵妸鑰佸浘鐗囨竻绌恒€?
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
            setFormData({
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
            });
            setSelectedTemplate(detailTemplate);
            setShowEditModal(true);
        } catch (e) {
            console.error(e);
            reportPageLoadError(e, '鑾峰彇璇︽儏澶辫触');
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
        if (!selectedTemplate) return;
        if (!formData.name.trim()) {
            alert('请填写名称');
            return;
        }
        setActionLoading(selectedTemplate.id);
        try {
            const { thumbnail, previewUrl, images } = buildImageFields(formData.imageUrls);
            // 鍒嗙被锛氱紪杈戞椂娌跨敤鍘熸湁鍒嗙被
            const category = formData.category || selectedTemplate.category || categories[0]?.id || 'villa';
            const updated = await updateTemplate(selectedTemplate.id, {
                name: formData.name,
                category,
                main_tab: formData.mainTab || undefined,
                sub_tab: formData.subTab || undefined,
                third_tab: formData.thirdTab || undefined,
                description: formData.description,
                thumbnail: thumbnail || undefined,
                preview_url: previewUrl || undefined,
                images: images || undefined,
                price: formData.price,
                is_free: formData.isFree,
                status: formData.status,
            });
            setTemplates(templates.map(t => t.id === selectedTemplate.id ? convertTemplate(updated) : t));
            setShowEditModal(false);
            setSelectedTemplate(null);
        } catch (e: any) {
            alert(e?.message || '更新失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleAddTemplate = async () => {
        if (!formData.name.trim()) {
            alert('请填写名称');
            return;
        }
        setActionLoading('add');
        try {
            const { thumbnail, previewUrl, images } = buildImageFields(formData.imageUrls);
            // 鍒嗙被锛氫笉鍐嶅崟鐙€夋嫨锛岄粯璁や娇鐢ㄧ涓€涓垎绫?
            const category = categories[0]?.id || 'villa';
            await createTemplate({
                name: formData.name,
                category,
                main_tab: formData.mainTab || undefined,
                sub_tab: formData.subTab || undefined,
                third_tab: formData.thirdTab || undefined,
                description: formData.description,
                thumbnail: thumbnail || undefined,
                preview_url: previewUrl || undefined,
                images: images || undefined,
                price: formData.price,
                is_free: formData.isFree,
                status: formData.status,
            });
            setShowAddModal(false);
            setFormData(defaultForm);
            loadTemplates();
        } catch (e: any) {
            alert(e?.message || '创建失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleDelete = async (templateId: string) => {
        if (!window.confirm('确定要删除这个模板吗？此操作不可恢复。')) return;
        setActionLoading(templateId);
        try {
            await deleteTemplate(templateId);
            setTemplates(templates.filter(t => t.id !== templateId));
        } catch (error) {
            console.error('鍒犻櫎澶辫触:', error);
            alert('删除失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleApprove = async (template: Template) => {
        setActionLoading(template.id);
        try {
            await updateTemplateStatus(template.id, 'published');
            setTemplates(templates.map(t => t.id === template.id ? { ...t, status: 'published', rejectReason: '' } : t));
            if (selectedTemplate?.id === template.id) setSelectedTemplate({ ...selectedTemplate, status: 'published', rejectReason: '' });
        } catch (error) {
            console.error('瀹℃牳閫氳繃澶辫触:', error);
            alert('操作失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleReject = async (template: Template) => {
        const rejectReason = window.prompt('请输入拒绝原因（会回传给设计师）', template.rejectReason || '');
        if (rejectReason === null) {
            return;
        }
        setActionLoading(template.id);
        try {
            await updateTemplateStatus(template.id, 'rejected', rejectReason);
            setTemplates(templates.map(t => t.id === template.id ? { ...t, status: 'rejected', rejectReason } : t));
            if (selectedTemplate?.id === template.id) setSelectedTemplate({ ...selectedTemplate, status: 'rejected', rejectReason });
        } catch (error) {
            console.error('瀹℃牳鎷掔粷澶辫触:', error);
            alert('操作失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleMoveCategory = (template: Template) => {
        setMoveTargetTemplate(template);
        setShowMoveCategoryModal(true);
    };

    const confirmMoveCategory = async (categoryId: string) => {
        if (!moveTargetTemplate) return;
        setActionLoading(moveTargetTemplate.id);
        try {
            await updateTemplateCategory(moveTargetTemplate.id, categoryId);
            setTemplates(templates.map(t => t.id === moveTargetTemplate.id ? { ...t, category: categoryId } : t));
            setShowMoveCategoryModal(false);
            setMoveTargetTemplate(null);
        } catch (e: any) {
            alert(e?.message || '移动失败');
        } finally {
            setActionLoading(null);
        }
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
        setActionLoading(template.id);
        try {
            await setTemplateFeatured(template.id, isFeatured);
            await loadTemplates();
        } catch (e: any) {
            alert(e?.message || '操作失败');
        } finally {
            setActionLoading(null);
        }
    };

    const loadFeaturedGroups = useCallback(async () => {
        setLoadingFeaturedGroups(true);
        try {
            const res = await getFeaturedCaseGroups(1, 100);
            setFeaturedGroups(Array.isArray(res?.list) ? res.list : []);
        } catch (e: any) {
            console.error('鍔犺浇绮鹃€夋渚嬬粍澶辫触:', e);
            reportPageLoadError(e, e?.message || '鍔犺浇绮鹃€夋渚嬬粍澶辫触');
            setFeaturedGroups([]); // 鍑洪敊鏃惰缃负绌烘暟缁?
        } finally {
            setLoadingFeaturedGroups(false);
        }
    }, []);

    const handleCreateFeaturedGroup = async () => {
        if (!featuredGroupForm.name.trim() || !featuredGroupForm.case1_id) {
            alert('请填写组名称并选择第一个案例');
            return;
        }
        if (featuredGroupForm.display_mode !== 'normal' && !featuredGroupForm.case2_id) {
            alert('对比模式和并排模式需要选择两个案例');
            return;
        }
        setActionLoading('create-group');
        try {
            await createFeaturedCaseGroup({
                name: featuredGroupForm.name,
                display_mode: featuredGroupForm.display_mode,
                case1_id: parseInt(featuredGroupForm.case1_id, 10),
                case2_id: featuredGroupForm.case2_id ? parseInt(featuredGroupForm.case2_id, 10) : undefined,
                case1_label: featuredGroupForm.case1_label,
                case2_label: featuredGroupForm.case2_label,
                sort_order: featuredGroupForm.sort_order,
            });
            setShowFeaturedGroupModal(false);
            setFeaturedGroupForm({
                name: '',
                display_mode: 'comparison',
                case1_id: '',
                case2_id: '',
                case1_label: '鐪熷疄',
                case2_label: 'AI',
                sort_order: 0,
            });
            await loadFeaturedGroups();
        } catch (e: any) {
            alert(e?.message || '创建失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleUpdateFeaturedGroup = async () => {
        if (!selectedFeaturedGroup) return;
        if (!featuredGroupForm.name.trim() || !featuredGroupForm.case1_id) {
            alert('请填写组名称并选择第一个案例');
            return;
        }
        if (featuredGroupForm.display_mode !== 'normal' && !featuredGroupForm.case2_id) {
            alert('对比模式和并排模式需要选择两个案例');
            return;
        }
        setActionLoading('update-group');
        try {
            await updateFeaturedCaseGroup(String(selectedFeaturedGroup.id), {
                name: featuredGroupForm.name,
                display_mode: featuredGroupForm.display_mode,
                case1_id: parseInt(featuredGroupForm.case1_id, 10),
                case2_id: featuredGroupForm.case2_id ? parseInt(featuredGroupForm.case2_id, 10) : undefined,
                case1_label: featuredGroupForm.case1_label,
                case2_label: featuredGroupForm.case2_label,
                sort_order: featuredGroupForm.sort_order,
            });
            setShowFeaturedGroupModal(false);
            setSelectedFeaturedGroup(null);
            await loadFeaturedGroups();
        } catch (e: any) {
            alert(e?.message || '更新失败');
        } finally {
            setActionLoading(null);
        }
    };

    const handleDeleteFeaturedGroup = async (groupId: number) => {
        if (!window.confirm('确定要删除这个精选案例组吗？')) return;
        setActionLoading(`group-${groupId}`);
        try {
            await deleteFeaturedCaseGroup(String(groupId));
            await loadFeaturedGroups();
        } catch (e: any) {
            alert(e?.message || '删除失败');
        } finally {
            setActionLoading(null);
        }
    };

    const openEditFeaturedGroup = (group: FeaturedCaseGroup) => {
        if (!group) return;
        setSelectedFeaturedGroup(group);
        setFeaturedGroupForm({
            name: group.name || '',
            display_mode: group.display_mode || 'comparison',
            case1_id: String(group.case1_id || ''),
            case2_id: String(group.case2_id || ''),
            case1_label: group.case1_label || '鐪熷疄',
            case2_label: group.case2_label || 'AI',
            sort_order: group.sort_order || 0,
        });
        setShowFeaturedGroupModal(true);
    };

    return (
        <Layout title="模板广场管理">
            <div className="templates-container">
                <div className="templates-toolbar section-card">
                    <div className="templates-toolbar-top">
                        <div className="toolbar-title-block">
                            <h3>模板检索与内容运营</h3>
                            <p>先按关键词、分类和状态缩小范围，再决定是做模板审核、分类整理，还是调整首页精选案例与标签结构。</p>
                        </div>
                        <div className="toolbar-actions management-actions">
                            <button className="btn-secondary" onClick={() => { setFeaturedGroupSectionOpen(!featuredGroupSectionOpen); if (!featuredGroupSectionOpen) loadFeaturedGroups(); }}>
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
                                    setFormData(defaultForm);
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
                                placeholder="搜索模板名称或描述..."
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
                                onChange={(e) => setCategoryFilter(e.target.value)}
                            >
                                <option value="all">全部分类</option>
                                {categories.map((c) => (
                                    <option key={c.id} value={c.id}>{c.name}</option>
                                ))}
                            </select>
                            <select
                                className="filter-select"
                                value={statusFilter}
                                onChange={(e) => setStatusFilter(e.target.value)}
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
                            当前显示 <strong>{filteredTemplates.length}</strong> / {templates.length} 个模板
                            {searchKeyword ? <span className="summary-tag">关键词：{searchKeyword}</span> : null}
                            {categoryFilter !== 'all' ? <span className="summary-tag">分类：{getCategoryLabel(categoryFilter)}</span> : null}
                            {statusFilter !== 'all' ? <span className="summary-tag">状态：{getStatusLabel(statusFilter).label}</span> : null}
                        </div>
                        <div className="toolbar-actions search-actions">
                            <button className="btn-secondary" onClick={handleResetSearch}>重置筛选</button>
                            <button className="btn-primary" onClick={handleSearch}>搜索模板</button>
                        </div>
                    </div>
                </div>

                {tabConfigSectionOpen && (
                    <div className="section-card tab-config-management">
                        <h4>模板广场标签配置（小程序与发布页统一读取）</h4>
                        <div className="tab-config-grid">
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
                        </div>
                        <div className="tab-config-footer">
                            <span style={{ fontSize: 12, color: tabConfigSaveState === 'error' || tabConfigSaveState === 'invalid' ? '#d14343' : tabConfigSaveState === 'saved' ? '#2f7a4d' : '#666' }}>
                                {tabConfigSaveMessage || '修改后将自动保存'}
                            </span>
                            <button className="btn-primary" onClick={() => { void handleSaveTabConfig(); }} disabled={tabConfigSaving}>
                                {tabConfigSaving ? '保存中...' : '保存标签配置'}
                            </button>
                        </div>
                    </div>
                )}

                {featuredGroupSectionOpen && (
                    <div className="section-card featured-group-management">
                        <div className="section-header">
                            <h4>首页精选案例组管理</h4>
                            <button className="btn-primary btn-sm" onClick={() => { setSelectedFeaturedGroup(null); setFeaturedGroupForm({ name: '', display_mode: 'comparison', case1_id: '', case2_id: '', case1_label: '真实', case2_label: 'AI', sort_order: 0 }); setShowFeaturedGroupModal(true); }}>
                                <FiPlusIcon /> 新建案例组
                            </button>
                        </div>
                        <p className="section-desc">案例组会显示在小程序首页的精选案例区域，每组可配置 1-2 个案例，支持对比、并排和普通模式。</p>
                        {loadingFeaturedGroups ? (
                            <div className="loading-state">加载中...</div>
                        ) : (
                            <div className="featured-group-list">
                                {!featuredGroups || featuredGroups.length === 0 ? (
                                    <div className="empty-state">暂无精选案例组</div>
                                ) : (
                                    featuredGroups.map((group) => {
                                        if (!group) return null;
                                        return (
                                            <div key={group.id} className="featured-group-item">
                                                <div className="featured-group-content">
                                                    <div className="featured-group-header">
                                                        <h5>{group.name || '未命名案例组'}</h5>
                                                        <span className="group-mode-badge">{group.display_mode === 'comparison' ? '对比模式' : group.display_mode === 'side_by_side' ? '并排模式' : '普通模式'}</span>
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
                                                    <button className="btn-action btn-edit" onClick={() => openEditFeaturedGroup(group)} title="编辑">
                                                        <FiEditIcon size={14} />
                                                    </button>
                                                    <button
                                                        className="btn-action btn-delete"
                                                        onClick={() => handleDeleteFeaturedGroup(group.id)}
                                                        disabled={actionLoading === `group-${group.id}`}
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
                        <span className="stat-label">模板总数</span>
                        <span className="stat-value">{templates.length}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">待审核</span>
                        <span className="stat-value">{templates.filter(t => t.status === 'pending').length}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">已发布</span>
                        <span className="stat-value">{templates.filter(t => t.status === 'published').length}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">总下载量</span>
                        <span className="stat-value">{templates.reduce((sum, t) => sum + t.downloadCount, 0)}</span>
                    </div>
                </div>

                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="templates-grid">
                        {filteredTemplates.length === 0 ? (
                            <div className="empty-state">暂无模板数据</div>
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
                                        <div className="template-footer">
                                            <div className="template-actions">
                                                {template.status === 'pending' && (
                                                    <>
                                                        <button className="btn-action btn-approve" onClick={() => handleApprove(template)} disabled={actionLoading === template.id} title="通过">
                                                            <FiCheckIcon size={14} /> 通过
                                                        </button>
                                                        <button className="btn-action btn-reject" onClick={() => handleReject(template)} disabled={actionLoading === template.id} title="拒绝">
                                                            <FiXIcon size={14} /> 拒绝
                                                        </button>
                                                    </>
                                                )}
                                                {template.status === 'published' && (
                                                    <button
                                                        className={`btn-action ${template.isFeatured ? 'btn-featured-active' : 'btn-featured'}`}
                                                        onClick={() => handleSetFeatured(template, !template.isFeatured)}
                                                        disabled={actionLoading === template.id}
                                                        title={template.isFeatured ? '取消精选' : '设为精选'}
                                                    >
                                                        {template.isFeatured ? '已精选' : '设为精选'}
                                                    </button>
                                                )}
                                                <button className="btn-action btn-move" onClick={() => handleMoveCategory(template)} title="移动分类">
                                                    <FiFolderIcon size={14} />
                                                </button>
                                                <button className="btn-action btn-edit" onClick={() => handleEdit(template)} title="编辑">
                                                    <FiEditIcon size={14} />
                                                </button>
                                                <button className="btn-action btn-delete" onClick={() => handleDelete(template.id)} disabled={actionLoading === template.id} title="删除">
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

                {/* 娣诲姞妯℃澘寮圭獥 */}
                {showAddModal && (
                    <div className="modal-overlay" onClick={() => setShowAddModal(false)}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>添加模板</h3>
                                <button className="modal-close" onClick={() => setShowAddModal(false)}>×</button>
                            </div>
                            <div className="modal-body">
                                <div className="form-group">
                                    <label>名称 *</label>
                                    <input className="form-input" value={formData.name} onChange={(e) => setFormData(f => ({ ...f, name: e.target.value }))} placeholder="模板名称" />
                                </div>
                                {/* 分类由系统根据配置自动处理，这里不再单独选择 */}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>一级标签（可选）</label>
                                        <select className="form-input" value={formData.mainTab} onChange={(e) => {
                                            const mainTabValue = e.target.value;
                                            setFormData(f => ({ ...f, mainTab: mainTabValue, subTab: '', thirdTab: '' }));
                                        }}>
                                            <option value="">不设置（仅分类）</option>
                                            {mainTabs.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>二级标签（可选）</label>
                                        <select className="form-input" value={formData.subTab} onChange={(e) => setFormData(f => ({ ...f, subTab: e.target.value, thirdTab: '' }))} disabled={!formData.mainTab}>
                                            <option value="">不设置（仅父标签）</option>
                                            {formData.mainTab && getChildTabsByParent(subTabs, formData.mainTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label>三级Tab（可选）</label>
                                    <select className="form-input" value={formData.thirdTab} onChange={(e) => setFormData(f => ({ ...f, thirdTab: e.target.value }))} disabled={!formData.subTab || getChildTabsByParent(thirdTabs, formData.subTab).length === 0}>
                                        <option value="">不设置（仅二级Tab）</option>
                                        {formData.subTab && getChildTabsByParent(thirdTabs, formData.subTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>描述</label>
                                    <textarea className="form-input" rows={3} value={formData.description} onChange={(e) => setFormData(f => ({ ...f, description: e.target.value }))} placeholder="模板描述" />
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
                                </div>
                                <div className="form-group">
                                    <label>模板图片（支持多图）</label>
                                    <div className="image-upload-group">
                                        <input
                                            id="template-images-upload"
                                            type="file"
                                            accept="image/*"
                                            multiple
                                            style={{ display: 'none' }}
                                            onChange={async (e) => {
                                                const files = Array.from(e.target.files || []);
                                                if (!files.length) return;
                                                const uploaded: string[] = [];
                                                for (const file of files) {
                                                    const formDataUpload = new FormData();
                                                    formDataUpload.append('file', file);
                                                    try {
                                                        const resp = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.UPLOAD}`, {
                                                            method: 'POST',
                                                            credentials: 'include',
                                                            body: formDataUpload,
                                                        });
                                                        const result = await resp.json();
                                                        if (resp.ok && result.code === 0 && result.data?.url) {
                                                            uploaded.push(result.data.url);
                                                        } else {
                                                            alert(result.msg || '上传失败');
                                                        }
                                                    } catch (err: any) {
                                                        alert('上传失败: ' + (err?.message || '未知错误'));
                                                    }
                                                }
                                                if (uploaded.length) {
                                                    setFormData(f => ({
                                                        ...f,
                                                        imageUrls: [...(f.imageUrls || []), ...uploaded],
                                                    }));
                                                }
                                                // 清空 input，避免同一文件不触发 change
                                                e.target.value = '';
                                            }}
                                        />
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            onClick={() => {
                                                const input = document.getElementById('template-images-upload') as HTMLInputElement | null;
                                                input?.click();
                                            }}
                                        >
                                            选择图片并上传
                                        </button>
                                    </div>
                                    {selectedTemplate && (
                                        <div className="image-size-tip">
                                            当前封面尺寸：{formatImageMeta(selectedTemplate.imageWidth, selectedTemplate.imageHeight)}
                                        </div>
                                    )}
                                    {formData.imageUrls && formData.imageUrls.length > 0 && (
                                        <div className="image-preview-list">
                                            {formData.imageUrls.map((url, idx) => (
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
                                                                setFormData(f => ({
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
                                        <input type="number" className="form-input" value={formData.price || ''} onChange={(e) => setFormData(f => ({ ...f, price: parseInt(e.target.value, 10) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-inline">
                                        <label>
                                            <input type="checkbox" checked={formData.isFree} onChange={(e) => setFormData(f => ({ ...f, isFree: e.target.checked }))} />
                                            免费
                                        </label>
                                    </div>
                                    <div className="form-group">
                                        <label>状态</label>
                                        <select className="form-input" value={formData.status} onChange={(e) => setFormData(f => ({ ...f, status: e.target.value }))}>
                                            <option value="draft">草稿</option>
                                            <option value="published">已发布</option>
                                            <option value="archived">已归档</option>
                                        </select>
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowAddModal(false)}>取消</button>
                                <button className="btn-primary" onClick={handleAddTemplate} disabled={actionLoading === 'add'}>保存</button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 编辑模板弹窗 */}
                {showEditModal && selectedTemplate && (
                    <div className="modal-overlay" onClick={() => setShowEditModal(false)}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>编辑模板</h3>
                                <button className="modal-close" onClick={() => setShowEditModal(false)}>×</button>
                            </div>
                            <div className="modal-body">
                                <div className="form-group">
                                    <label>名称 *</label>
                                    <input className="form-input" value={formData.name} onChange={(e) => setFormData(f => ({ ...f, name: e.target.value }))} placeholder="模板名称" />
                                </div>
                                {/* 分类由系统根据配置自动处理，这里不再单独选择 */}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>一级标签（可选）</label>
                                        <select className="form-input" value={formData.mainTab} onChange={(e) => {
                                            const mainTabValue = e.target.value;
                                            setFormData(f => ({ ...f, mainTab: mainTabValue, subTab: '', thirdTab: '' }));
                                        }}>
                                            <option value="">不设置（仅分类）</option>
                                            {mainTabs.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                    <div className="form-group">
                                        <label>二级标签（可选）</label>
                                        <select className="form-input" value={formData.subTab} onChange={(e) => setFormData(f => ({ ...f, subTab: e.target.value, thirdTab: '' }))} disabled={!formData.mainTab}>
                                            <option value="">不设置（仅父标签）</option>
                                            {formData.mainTab && getChildTabsByParent(subTabs, formData.mainTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                        </select>
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label>三级Tab（可选）</label>
                                    <select className="form-input" value={formData.thirdTab} onChange={(e) => setFormData(f => ({ ...f, thirdTab: e.target.value }))} disabled={!formData.subTab || getChildTabsByParent(thirdTabs, formData.subTab).length === 0}>
                                        <option value="">不设置（仅二级Tab）</option>
                                        {formData.subTab && getChildTabsByParent(thirdTabs, formData.subTab).map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                                    </select>
                                </div>
                                <div className="form-group">
                                    <label>描述</label>
                                    <textarea className="form-input" rows={3} value={formData.description} onChange={(e) => setFormData(f => ({ ...f, description: e.target.value }))} placeholder="模板描述" />
                                </div>
                                <div className="form-group">
                                    <label>模板图片（支持多图）</label>
                                    <div className="image-upload-group">
                                        {/* 复用同一个 input，编辑弹窗仅触发 click */}
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            onClick={() => {
                                                const input = document.getElementById('template-images-upload') as HTMLInputElement | null;
                                                input?.click();
                                            }}
                                        >
                                            选择图片并上传
                                        </button>
                                    </div>
                                    {formData.imageUrls && formData.imageUrls.length > 0 && (
                                        <div className="image-preview-list">
                                            {formData.imageUrls.map((url, idx) => (
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
                                                                setFormData(f => ({
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
                                        <input type="number" className="form-input" value={formData.price || ''} onChange={(e) => setFormData(f => ({ ...f, price: parseInt(e.target.value, 10) || 0 }))} />
                                    </div>
                                    <div className="form-group form-group-inline">
                                        <label>
                                            <input type="checkbox" checked={formData.isFree} onChange={(e) => setFormData(f => ({ ...f, isFree: e.target.checked }))} />
                                            免费
                                        </label>
                                    </div>
                                    <div className="form-group">
                                        <label>状态</label>
                                        <select className="form-input" value={formData.status} onChange={(e) => setFormData(f => ({ ...f, status: e.target.value }))}>
                                            <option value="draft">草稿</option>
                                            <option value="pending">待审核</option>
                                            <option value="published">已发布</option>
                                            <option value="archived">已归档</option>
                                        </select>
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowEditModal(false)}>取消</button>
                                <button className="btn-primary" onClick={handleSaveEdit} disabled={actionLoading === selectedTemplate.id}>保存</button>
                            </div>
                        </div>
                    </div>
                )}

                {/* 绉诲姩鍒嗙被寮圭獥 */}
                {showMoveCategoryModal && moveTargetTemplate && (
                    <div className="modal-overlay" onClick={() => setShowMoveCategoryModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>移动“{moveTargetTemplate.name}”到分类</h3>
                                <button className="modal-close" onClick={() => setShowMoveCategoryModal(false)}>×</button>
                            </div>
                            <div className="modal-body">
                                <p className="modal-hint">点击目标分类完成移动。</p>
                                <div className="move-category-btns">
                                    {categories.filter(c => c.id !== moveTargetTemplate.category).map(c => (
                                        <button
                                            key={c.id}
                                            className="btn-secondary"
                                            onClick={() => confirmMoveCategory(c.id)}
                                            disabled={actionLoading === moveTargetTemplate.id}
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

                {/* 绮鹃€夋渚嬬粍缂栬緫寮圭獥 */}
                {showFeaturedGroupModal && (
                    <div className="modal-overlay" onClick={() => setShowFeaturedGroupModal(false)}>
                        <div className="modal-content modal-form" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>{selectedFeaturedGroup ? '编辑精选案例组' : '新建精选案例组'}</h3>
                                <button className="modal-close" onClick={() => setShowFeaturedGroupModal(false)}>×</button>
                            </div>
                            <div className="modal-body">
                                <div className="form-group">
                                    <label>组名称 *</label>
                                    <input
                                        className="form-input"
                                        value={featuredGroupForm.name}
                                        onChange={(e) => setFeaturedGroupForm(f => ({ ...f, name: e.target.value }))}
                                        placeholder="例如：真实场景 vs AI 设计"
                                    />
                                </div>
                                <div className="form-group">
                                    <label>显示模式 *</label>
                                    <select
                                        className="form-input"
                                        value={featuredGroupForm.display_mode}
                                        onChange={(e) => setFeaturedGroupForm(f => ({ ...f, display_mode: e.target.value as any }))}
                                    >
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
                                        onChange={(e) => setFeaturedGroupForm(f => ({ ...f, case1_id: e.target.value }))}
                                    >
                                        <option value="">请选择案例</option>
                                        {templates.filter(t => t.status === 'published').map(t => (
                                            <option key={t.id} value={t.id}>{t.name}</option>
                                        ))}
                                    </select>
                                </div>
                                {featuredGroupForm.display_mode !== 'normal' && (
                                    <div className="form-group">
                                        <label>第二个案例 *</label>
                                        <select
                                            className="form-input"
                                            value={featuredGroupForm.case2_id}
                                            onChange={(e) => setFeaturedGroupForm(f => ({ ...f, case2_id: e.target.value }))}
                                        >
                                            <option value="">请选择案例</option>
                                            {templates.filter(t => t.status === 'published' && t.id !== featuredGroupForm.case1_id).map(t => (
                                                <option key={t.id} value={t.id}>{t.name}</option>
                                            ))}
                                        </select>
                                    </div>
                                )}
                                <div className="form-row">
                                    <div className="form-group">
                                        <label>第一个案例标签</label>
                                        <input
                                            className="form-input"
                                            value={featuredGroupForm.case1_label}
                                            onChange={(e) => setFeaturedGroupForm(f => ({ ...f, case1_label: e.target.value }))}
                                            placeholder="例如：真实"
                                        />
                                    </div>
                                    {featuredGroupForm.display_mode !== 'normal' && (
                                        <div className="form-group">
                                            <label>第二个案例标签</label>
                                            <input
                                                className="form-input"
                                                value={featuredGroupForm.case2_label}
                                                onChange={(e) => setFeaturedGroupForm(f => ({ ...f, case2_label: e.target.value }))}
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
                                        onChange={(e) => setFeaturedGroupForm(f => ({ ...f, sort_order: parseInt(e.target.value, 10) || 0 }))}
                                        placeholder="数字越小越靠前"
                                    />
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowFeaturedGroupModal(false)}>取消</button>
                                <button
                                    className="btn-primary"
                                    onClick={selectedFeaturedGroup ? handleUpdateFeaturedGroup : handleCreateFeaturedGroup}
                                    disabled={actionLoading === 'create-group' || actionLoading === 'update-group'}
                                >
                                    {actionLoading === 'create-group' || actionLoading === 'update-group' ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default Templates;
