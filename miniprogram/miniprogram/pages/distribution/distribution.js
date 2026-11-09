"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const asset_1 = require("../../utils/asset");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_MAIN_TABS = [
    { label: '全部', value: 'all' },
    { label: '乡墅私宅', value: 'template' },
    { label: '空间场景', value: 'scene' },
    { label: '材质软装', value: 'material' },
    { label: '创意玩法', value: 'latest' },
    { label: '庭院乡墅', value: 'villa' },
];
const DESIGNER_TRAIT_FILTERS = ['全部'];
const DESIGNER_SORTS = [
    { label: '综合排序', value: 'comprehensive' },
    { label: '作品点赞最多', value: 'popular' },
    { label: '作品最新发布', value: 'latest' },
];
function formatCompactCount(value) {
    const num = Number(value || 0);
    if (num >= 10000)
        return `${(num / 10000).toFixed(1).replace(/\.0$/, '')}W`;
    if (num >= 1000)
        return `${(num / 1000).toFixed(1).replace(/\.0$/, '')}K`;
    return String(num);
}
function getAvatarText(name) {
    const text = String(name || '').trim();
    return text ? text.slice(0, 1) : '设';
}
function normalizeImageUrl(url) {
    const cleanUrl = String(url || '').trim();
    if (!cleanUrl) {
        return '';
    }
    if (/^(https?:\/\/|wxfile:\/\/|file:\/\/|data:)/i.test(cleanUrl)) {
        return cleanUrl.startsWith('http') ? (0, asset_1.normalizeCosUrl)(cleanUrl) : cleanUrl;
    }
    if (cleanUrl.startsWith('//')) {
        return (0, asset_1.normalizeCosUrl)(`https:${cleanUrl}`);
    }
    if (cleanUrl.startsWith('/')) {
        return `${API_BASE_URL}${cleanUrl}`;
    }
    return `${API_BASE_URL}/${cleanUrl}`;
}
function buildDesignerEmptyTip(currentTabLabel, keyword, selectedTrait) {
    const base = keyword
        ? `未找到与“${keyword}”相关的设计师`
        : `${currentTabLabel} 暂时还没有设计师`;
    return selectedTrait && selectedTrait !== '全部' ? `${base} · 已筛选${selectedTrait}` : base;
}
function buildResultSummary(keyword, selectedTrait) {
    const parts = [];
    if (keyword)
        parts.push(`搜索“${keyword}”`);
    if (selectedTrait && selectedTrait !== '全部')
        parts.push(selectedTrait);
    return parts.length ? `已按${parts.join(' · ')}筛选` : '';
}
function requestDesignerApi(data, pathname = '/designers') {
    return new Promise((resolve) => {
        wx.request({
            url: `${API_BASE_URL}/api/v1/miniprogram${pathname}`,
            method: 'GET',
            timeout: pathname === '/designers' ? 15000 : 5000,
            data,
            success: (res) => {
                const response = (res.data || {});
                if (res.statusCode !== 200 || !res.data || response.code !== 0) {
                    console.error('设计师数据请求失败', pathname, res.statusCode);
                    resolve(null);
                    return;
                }
                resolve(response.data || {});
            },
            fail: (error) => {
                console.error('设计师数据网络异常', pathname, error);
                resolve(null);
            },
        });
    });
}
const CATEGORY_ALIASES = {
    template: ['template', 'xiangshu', 'villa', 'villa_exterior', 'interior_space'],
    scene: ['scene', 'urban', 'family', 'culture', 'villa_exterior', 'interior_space', 'garden_courtyard', 'renovation', 'commercial_space', 'design_assist'],
    material: ['material', 'style', 'new_minnan', 'new_chinese', 'modern', 'classic_european', 'regional'],
    latest: ['latest', 'inspiration', 'gallery', 'rural_trend', 'lifestyle', 'regional_culture', 'function_innovation', 'selected_cases'],
    villa: ['villa', 'xiangshu', 'villa_exterior', 'garden_courtyard', 'villa_case', 'villa_courtyard', 'villa_construction'],
};
async function completeLegacyWorkPreviews(list, mainTab) {
    const result = list.slice();
    let cursor = 0;
    // 仅兼容旧列表协议，按页最多三路补拉；不对新接口增加 N+1 请求。
    const worker = async () => {
        while (cursor < list.length) {
            const index = cursor++;
            const item = list[index];
            if (item.work_previews || !Number(item.designer_user_id))
                continue;
            const homepage = await requestDesignerApi(undefined, `/designers/${Number(item.designer_user_id)}/homepage`);
            if (!Array.isArray(homepage?.works))
                continue;
            const aliases = CATEGORY_ALIASES[mainTab];
            const works = homepage.works.filter(work => Number(work.id) > 0 && (!aliases ||
                [work.main_tab, work.category, work.sub_tab].some(value => aliases.includes(String(value || '').trim().toLowerCase()))));
            const byLatest = (a, b) => (Date.parse(b.created_at || '') || 0) - (Date.parse(a.created_at || '') || 0) || Number(b.id) - Number(a.id);
            result[index] = { ...item, work_previews: {
                    popular: works.slice().sort((a, b) => Number(b.likes || 0) - Number(a.likes || 0) || byLatest(a, b)).slice(0, 2),
                    latest: works.slice().sort(byLatest).slice(0, 2),
                } };
        }
    };
    await Promise.all(Array.from({ length: Math.min(3, list.length) }, worker));
    return result;
}
function buildDesignerCardFromApi(item) {
    const designerUserId = Number(item?.designer_user_id) || 0;
    if (!designerUserId) {
        return null;
    }
    const representativeWork = item?.representative_work || {};
    const designerName = String(item?.designer_name || '匿名设计师');
    const designerTitle = String(item?.designer_title || '设计师');
    const mapWorks = (list) => list.filter(work => Number(work.id) > 0).slice(0, 2).map(work => ({
        id: Number(work.id), title: String(work.title || '未命名作品'), image: normalizeImageUrl(String(work.image || '')),
        likesText: formatCompactCount(Number(work.likes) || 0), tagsText: (work.tags || []).slice(0, 2).join(' · '),
        publishScope: String(work.publish_scope || 'square'),
    }));
    const fallback = Number(representativeWork.id) > 0 ? [representativeWork] : [];
    const workSets = { popular: mapWorks(item.work_previews?.popular || fallback), latest: mapWorks(item.work_previews?.latest || fallback) };
    return {
        certified: item.cert_status === 'approved',
        regionText: String(item.designer_region || '').trim() || '未填写地区',
        specialtyText: String(item.specialties_text || designerTitle).replace(/^专业领域[：:]\s*/, ''),
        workSort: 'popular', works: workSets.popular, workSets,
        designerUserId,
        designerName,
        designerAvatar: normalizeImageUrl(String(item?.designer_avatar || '')),
        designerAvatarText: getAvatarText(designerName),
    };
}
function buildDesignerCardsFromApi(list) {
    const seen = new Set();
    return list.reduce((acc, item) => {
        const card = buildDesignerCardFromApi(item);
        if (!card || seen.has(card.designerUserId)) {
            return acc;
        }
        seen.add(card.designerUserId);
        acc.push(card);
        return acc;
    }, []);
}
function mergeDesignerCards(currentList, nextList) {
    const seen = new Set();
    return [...currentList, ...nextList].filter((item) => {
        if (!item.designerUserId || seen.has(item.designerUserId)) {
            return false;
        }
        seen.add(item.designerUserId);
        return true;
    });
}
function getNavLayout() {
    try {
        const systemInfo = wx.getSystemInfoSync();
        const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
            ? wx.getMenuButtonBoundingClientRect()
            : null;
        const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
        if (menuRect) {
            return {
                navSafeTop: safeTop,
                navBarHeight: Number(menuRect.bottom + menuRect.top - safeTop),
                navContentHeight: Number(menuRect.height),
                navSideWidth: Number(systemInfo.windowWidth - menuRect.left),
            };
        }
        return {
            navSafeTop: safeTop,
            navBarHeight: safeTop + 44,
            navContentHeight: 44,
            navSideWidth: 96,
        };
    }
    catch (error) {
        return {
            navSafeTop: 20,
            navBarHeight: 64,
            navContentHeight: 44,
            navSideWidth: 96,
        };
    }
}
const INITIAL_NAV_LAYOUT = getNavLayout();
Page({
    data: {
        contentReady: false,
        loading: false,
        navSafeTop: INITIAL_NAV_LAYOUT.navSafeTop,
        navBarHeight: INITIAL_NAV_LAYOUT.navBarHeight,
        navContentHeight: INITIAL_NAV_LAYOUT.navContentHeight,
        navSideWidth: INITIAL_NAV_LAYOUT.navSideWidth,
        tabs: DEFAULT_MAIN_TABS,
        currentTabIndex: 0,
        designerCards: [],
        page: 1,
        pageSize: 10,
        hasMore: true,
        emptyTipText: '暂时还没有设计师资料',
        designerCountText: '0',
        currentCategoryLabel: DEFAULT_MAIN_TABS[0].label,
        traitFilters: DESIGNER_TRAIT_FILTERS,
        selectedTrait: '全部',
        traitFilterIndex: 0,
        regionPickerValue: [],
        selectedRegionText: '',
        designerSorts: DESIGNER_SORTS,
        designerSortIndex: 0,
        searchInputValue: '',
        searchKeyword: '',
        resultSummaryText: '',
        loadError: '',
        skeletonCards: [0, 1, 2],
    },
    syncTabBar() {
        const tabBar = typeof this.getTabBar === 'function' ? this.getTabBar() : null;
        if (tabBar && typeof tabBar.setCurrent === 'function') {
            tabBar.setCurrent(2);
        }
    },
    onLoad() {
        this.initializePage();
    },
    onShow() {
        this.syncTabBar();
    },
    onReachBottom() {
        if (this.data.hasMore && !this.data.loading) {
            this.loadDesigners(false);
        }
    },
    onLoadMoreDesigners() {
        if (this.data.hasMore && !this.data.loading)
            return this.loadDesigners(false);
        return Promise.resolve(false);
    },
    onPullDownRefresh() {
        this.loadDesigners(true).finally(() => {
            wx.stopPullDownRefresh();
        });
    },
    onShareAppMessage() {
        return {
            title: '找设计师',
            path: '/pages/distribution/distribution',
        };
    },
    getCurrentMainTab() {
        return this.data.tabs[this.data.currentTabIndex] || this.data.tabs[0];
    },
    async initializePage() {
        await this.loadMainTabs();
        await this.loadDesigners(true);
    },
    async loadMainTabs() {
        const tabs = DEFAULT_MAIN_TABS.slice();
        this.setData({
            tabs,
            currentTabIndex: 0,
            currentCategoryLabel: tabs[0]?.label || DEFAULT_MAIN_TABS[0].label,
        });
    },
    async loadDesigners(reset) {
        if (this.data.loading) {
            return false;
        }
        const currentTab = this.getCurrentMainTab();
        const currentTabValue = currentTab?.value || 'all';
        const currentTabLabel = currentTab?.label || DEFAULT_MAIN_TABS[0].label;
        const requestPage = reset ? 1 : this.data.page;
        const keyword = String(this.data.searchKeyword || '').trim();
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        const hasRetainedCards = reset && this.data.designerCards.length > 0;
        this.setData({
            loading: true,
            loadError: '',
            contentReady: hasRetainedCards ? this.data.contentReady : false,
            ...(hasRetainedCards ? {} : { currentCategoryLabel: currentTabLabel }),
            emptyTipText: buildDesignerEmptyTip(currentTabLabel, keyword, selectedTrait),
        });
        const result = await requestDesignerApi({
            keyword,
            specialty: selectedTrait === '全部' ? '' : selectedTrait,
            region: this.data.selectedRegionText,
            sort: DESIGNER_SORTS[this.data.designerSortIndex].value,
            page: requestPage,
            page_size: this.data.pageSize,
            main_tab: currentTabValue === 'all' ? '' : currentTabValue,
        });
        const needsFilterSupport = selectedTrait !== '全部' || !!this.data.selectedRegionText || this.data.designerSortIndex !== 0;
        if (result && needsFilterSupport && !result.filters_applied) {
            this.setData({ loading: false, loadError: '暂时无法筛选，请稍后重试' });
            wx.showToast({ title: '暂时无法筛选，请稍后重试', icon: 'none' });
            return false;
        }
        let sourceCards = [];
        let nextHasMore = false;
        let nextEmptyTipText = buildDesignerEmptyTip(currentTabLabel, keyword, selectedTrait);
        let nextDesignerCountText = String(reset ? 0 : this.data.designerCountText || 0);
        if (!result) {
            if (this.data.designerCards.length > 0) {
                this.setData({ loading: false, loadError: '设计师列表加载失败，请稍后重试' });
                wx.showToast({ title: '设计师列表加载失败，请稍后重试', icon: 'none' });
                return false;
            }
            this.setData({
                designerCards: [],
                contentReady: false,
                loading: false,
                loadError: '设计师列表加载失败，请稍后重试',
                hasMore: false,
                designerCountText: '0',
                currentCategoryLabel: currentTabLabel,
                emptyTipText: '设计师列表加载失败，请稍后重试',
            });
            return false;
        }
        else {
            const list = await completeLegacyWorkPreviews(Array.isArray(result.list) ? result.list : [], currentTabValue);
            const mappedCards = buildDesignerCardsFromApi(list);
            sourceCards = mappedCards;
            nextHasMore = Number(result.total || 0) > requestPage * this.data.pageSize;
            nextDesignerCountText = String(Number(result.total || 0));
        }
        const nextDesignerCards = reset ? sourceCards : mergeDesignerCards(this.data.designerCards, sourceCards);
        const specialtyOptions = result.filter_options?.specialties;
        const traitFilters = Array.isArray(specialtyOptions)
            ? ['全部', ...specialtyOptions.filter(value => typeof value === 'string' && value && value !== '全部')]
            : this.data.traitFilters;
        if (!traitFilters.includes(selectedTrait))
            traitFilters.push(selectedTrait);
        const summary = [buildResultSummary(keyword, selectedTrait),
            this.data.selectedRegionText,
            this.data.designerSortIndex ? DESIGNER_SORTS[this.data.designerSortIndex].label : ''].filter(Boolean).join(' · ');
        this.setData({
            traitFilters,
            traitFilterIndex: traitFilters.indexOf(selectedTrait),
            designerCards: nextDesignerCards,
            contentReady: !!result && Array.isArray(result.list),
            loading: false,
            page: requestPage + 1,
            hasMore: nextHasMore,
            designerCountText: nextDesignerCountText,
            currentCategoryLabel: currentTabLabel,
            resultSummaryText: summary,
            emptyTipText: nextEmptyTipText,
        });
        return true;
    },
    onSearchInput(e) {
        this.setData({
            searchInputValue: String(e.detail.value || ''),
        });
    },
    async onSearchConfirm() {
        if (this.data.loading)
            return false;
        const keyword = String(this.data.searchInputValue || '').trim();
        const currentTab = this.getCurrentMainTab();
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        const previousKeyword = this.data.searchKeyword;
        const hasRetainedCards = this.data.designerCards.length > 0;
        this.setData({
            searchKeyword: keyword,
            emptyTipText: buildDesignerEmptyTip(currentTab?.label || DEFAULT_MAIN_TABS[0].label, keyword, selectedTrait),
        });
        const loaded = await this.loadDesigners(true);
        if (!loaded && hasRetainedCards)
            this.setData({ searchKeyword: previousKeyword });
        return loaded;
    },
    async onSearchClear() {
        if (this.data.loading)
            return false;
        const currentTab = this.getCurrentMainTab();
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        if (!this.data.searchKeyword && !this.data.searchInputValue) {
            return;
        }
        const previousInputValue = this.data.searchInputValue;
        const previousKeyword = this.data.searchKeyword;
        const hasRetainedCards = this.data.designerCards.length > 0;
        this.setData({
            searchInputValue: '',
            searchKeyword: '',
            emptyTipText: buildDesignerEmptyTip(currentTab?.label || DEFAULT_MAIN_TABS[0].label, '', selectedTrait),
        });
        const loaded = await this.loadDesigners(true);
        if (!loaded && hasRetainedCards)
            this.setData({ searchInputValue: previousInputValue, searchKeyword: previousKeyword });
        return loaded;
    },
    async onTraitFilterTap(e) {
        if (this.data.loading)
            return false;
        const index = Number(e.currentTarget.dataset.index);
        const selectedTrait = this.data.traitFilters[index];
        const currentTab = this.getCurrentMainTab();
        if (Number.isNaN(index) || !selectedTrait || selectedTrait === this.data.selectedTrait) {
            return;
        }
        const previousTrait = this.data.selectedTrait;
        this.setData({
            selectedTrait,
            emptyTipText: buildDesignerEmptyTip(currentTab?.label || DEFAULT_MAIN_TABS[0].label, this.data.searchKeyword, selectedTrait),
        });
        const loaded = await this.loadDesigners(true);
        if (!loaded)
            this.setData({ selectedTrait: previousTrait, traitFilterIndex: this.data.traitFilters.indexOf(previousTrait) });
        return loaded;
    },
    async onMainTabTap(e) {
        if (this.data.loading)
            return;
        const index = Number(e.currentTarget.dataset.index);
        const tab = this.data.tabs[index];
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        if (Number.isNaN(index) || index === this.data.currentTabIndex || !tab) {
            return;
        }
        const previousTabIndex = this.data.currentTabIndex;
        const hasRetainedCards = this.data.designerCards.length > 0;
        this.setData({
            currentTabIndex: index,
            emptyTipText: buildDesignerEmptyTip(tab.label, this.data.searchKeyword, selectedTrait),
        });
        const loaded = await this.loadDesigners(true);
        if (!loaded && hasRetainedCards) {
            this.setData({ currentTabIndex: previousTabIndex });
        }
    },
    onWorkSortTap(e) {
        const { userId, sort } = e.currentTarget.dataset;
        if (sort !== 'popular' && sort !== 'latest')
            return;
        this.setData({ designerCards: this.data.designerCards.map(card => card.designerUserId === Number(userId)
                ? { ...card, workSort: sort, works: card.workSets[sort] } : card) });
    },
    onDesignerAvatarError(e) {
        const userId = Number(e.currentTarget.dataset.userId);
        if (!userId)
            return;
        console.warn('设计师头像加载失败', { designerUserId: userId });
        this.setData({ designerCards: this.data.designerCards.map(card => card.designerUserId === userId
                ? { ...card, designerAvatar: '' } : card) });
    },
    onConsultDesigner(e) {
        const userId = Number(e.currentTarget.dataset.userId);
        if (!userId)
            return;
        wx.navigateTo({ url: `/pages/designerhome/designerhome?userId=${userId}&consult=1`,
            fail: () => wx.showToast({ title: '咨询页面打开失败，请重试', icon: 'none' }) });
    },
    async onDesignerSortChange(e) {
        const index = Number(e.detail.value);
        if (this.data.loading || !DESIGNER_SORTS[index] || index === this.data.designerSortIndex)
            return;
        const previous = this.data.designerSortIndex;
        this.setData({ designerSortIndex: index });
        if (!await this.loadDesigners(true))
            this.setData({ designerSortIndex: previous });
    },
    async onRegionChange(e) {
        const values = e.detail.value;
        if (this.data.loading || !Array.isArray(values) || values.length !== 3 || values.some(value => typeof value !== 'string'))
            return;
        const unlimitedIndex = values.indexOf('不限');
        const selectedRegionText = (unlimitedIndex < 0 ? values : values.slice(0, unlimitedIndex)).join(' ').trim();
        if (selectedRegionText === this.data.selectedRegionText)
            return;
        const previous = { selectedRegionText: this.data.selectedRegionText, regionPickerValue: this.data.regionPickerValue };
        this.setData({ selectedRegionText, regionPickerValue: values });
        if (!await this.loadDesigners(true))
            this.setData(previous);
    },
    onRetryDesigners() {
        return this.loadDesigners(true);
    },
    onTraitPickerChange(e) {
        return this.onTraitFilterTap({ currentTarget: { dataset: { index: Number(e.detail.value) } } });
    },
    onFeatureTap(e) {
        const feature = e.currentTarget.dataset.feature;
        if (feature === 'villa' || feature === 'courtyard') {
            const index = this.data.tabs.findIndex(tab => tab.value === (feature === 'villa' ? 'template' : 'villa'));
            void this.onMainTabTap({ currentTarget: { dataset: { index } } });
            return;
        }
        // 专题入口复用模板广场公开的灵感/案例精选，不进入设计师的管理页面。
        const app = getApp();
        app.globalData = app.globalData || {};
        app.globalData.designerTemplateEntry = feature === 'collections' ? 'inspiration' : 'scene';
        wx.switchTab({ url: '/pages/template/template', fail: () => {
                delete app.globalData.designerTemplateEntry;
                wx.showToast({ title: '页面打开失败，请重试', icon: 'none' });
            } });
    },
    onWorkTap(e) {
        const id = Number(e.currentTarget.dataset.id);
        const userId = Number(e.currentTarget.dataset.userId);
        const publishScope = String(e.currentTarget.dataset.publishScope || 'square');
        if (publishScope !== 'square' && userId) {
            wx.navigateTo({
                url: `/pages/designerhome/designerhome?userId=${userId}`,
                fail: () => {
                    wx.showToast({
                        title: '页面跳转失败',
                        icon: 'none',
                    });
                },
            });
            return;
        }
        if (!id) {
            return;
        }
        wx.navigateTo({
            url: `/pages/templatesquaredetails/templatesquaredetails?id=${id}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onDesignerTap(e) {
        const userId = Number(e.currentTarget.dataset.userId);
        if (!userId) {
            wx.showToast({
                title: '暂未获取到设计师信息',
                icon: 'none',
            });
            return;
        }
        wx.navigateTo({
            url: `/pages/designerhome/designerhome?userId=${userId}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
});
