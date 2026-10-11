"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_MAIN_TABS = [
    { label: '全部设计师', value: 'all' },
    { label: '乡墅私宅', value: 'template' },
    { label: '空间场景', value: 'scene' },
    { label: '材质软装', value: 'material' },
    { label: '创意玩法', value: 'latest' },
    { label: '庭院乡墅', value: 'villa' },
];
const DESIGNER_TRAIT_FILTERS = ['全部', '主案统筹', '落地交付', '庭院营造', '软装陈设', '焕新改造'];
function formatCompactCount(value) {
    const num = Number(value || 0);
    if (num >= 10000)
        return `${(num / 10000).toFixed(1).replace(/\.0$/, '')}W`;
    if (num >= 1000)
        return `${(num / 1000).toFixed(1).replace(/\.0$/, '')}K`;
    return String(num);
}
function truncate(str, maxLen) {
    const cleanText = str.replace(/\s+/g, ' ').trim();
    if (cleanText.length <= maxLen)
        return cleanText;
    return `${cleanText.slice(0, maxLen)}...`;
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
        return cleanUrl;
    }
    if (cleanUrl.startsWith('//')) {
        return `https:${cleanUrl}`;
    }
    if (cleanUrl.startsWith('/')) {
        return `${API_BASE_URL}${cleanUrl}`;
    }
    return `${API_BASE_URL}/${cleanUrl}`;
}
function buildTags(item, currentTabLabel) {
    const category = String(item?.category || '').trim();
    return [currentTabLabel, category]
        .map((tag) => truncate(String(tag || '').trim(), 6))
        .filter((tag, index, list) => Boolean(tag) && list.indexOf(tag) === index)
        .slice(0, 3);
}
function buildDesignerTraits(sourceText, currentTabLabel) {
    const text = `${sourceText || ''} ${currentTabLabel || ''}`;
    const traits = [];
    if (/庭院|乡墅|别墅/.test(text))
        traits.push('庭院营造');
    if (/软装|材质|陈设|配色|氛围/.test(text))
        traits.push('软装陈设');
    if (/改造|更新|立面|翻新/.test(text))
        traits.push('焕新改造');
    if (/空间|落地|方案|设计/.test(text))
        traits.push('落地交付');
    if (!traits.length)
        traits.push('主案统筹');
    if (!traits.includes('主案统筹'))
        traits.push('主案统筹');
    return traits.slice(0, 3);
}
function getPlaceholderToneClass(index, currentTabValue) {
    const seed = index % 4;
    if (currentTabValue === 'villa')
        return 'placeholder-tone-villa';
    if (currentTabValue === 'material')
        return 'placeholder-tone-material';
    if (currentTabValue === 'scene')
        return 'placeholder-tone-scene';
    if (seed === 0)
        return 'placeholder-tone-villa';
    if (seed === 1)
        return 'placeholder-tone-scene';
    if (seed === 2)
        return 'placeholder-tone-material';
    return 'placeholder-tone-creative';
}
function getWorkPlaceholderText(title, currentTabLabel) {
    const text = `${title || ''} ${currentTabLabel || ''}`;
    if (/庭院|乡墅|别墅/.test(text))
        return '庭院场景预览';
    if (/软装|材质|陈设|配色/.test(text))
        return '软装陈设预览';
    if (/改造|更新|立面/.test(text))
        return '焕新方案预览';
    if (/亲子|活动|工坊|场景/.test(text))
        return '空间氛围预览';
    return `${truncate(currentTabLabel || '设计方案', 4)}方案预览`;
}
function matchesTraitFilter(item, selectedTrait) {
    const trait = String(selectedTrait || '全部').trim();
    if (!trait || trait === '全部')
        return true;
    return item.designerTraits.includes(trait);
}
function filterDesignerList(items, keyword, selectedTrait) {
    const safeKeyword = String(keyword || '').trim();
    const filteredByTrait = items.filter((item) => matchesTraitFilter(item, selectedTrait));
    if (!safeKeyword) {
        return filteredByTrait;
    }
    return filteredByTrait.filter((item) => {
        const text = [item.title, item.designerName, item.designerTitle, ...item.tags, ...item.designerTraits].join(' ');
        return text.includes(safeKeyword);
    });
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
function requestDesignerApi(data) {
    return new Promise((resolve) => {
        wx.request({
            url: `${API_BASE_URL}/api/v1/miniprogram/designers`,
            method: 'GET',
            data,
            success: (res) => {
                const response = (res.data || {});
                if (res.statusCode !== 200 || !res.data || response.code !== 0) {
                    resolve(null);
                    return;
                }
                resolve(response.data || {});
            },
            fail: () => {
                resolve(null);
            },
        });
    });
}
function buildDesignerCardFromApi(item, index, currentTabValue, currentTabLabel) {
    const designerUserId = Number(item?.designer_user_id) || 0;
    if (!designerUserId) {
        return null;
    }
    const representativeWork = item?.representative_work || {};
    const title = String(representativeWork?.title || '暂未上传代表作品');
    const designerName = String(item?.designer_name || '匿名设计师');
    const designerTitle = String(item?.designer_title || '设计师');
    const tags = Array.isArray(representativeWork?.tags)
        ? representativeWork.tags.map((tag) => String(tag || '').trim()).filter(Boolean).slice(0, 3)
        : buildTags({ category: String(representativeWork?.category || '') }, currentTabLabel);
    const mergedText = [
        title,
        designerName,
        designerTitle,
        String(item?.experience_text || ''),
        String(item?.specialties_text || ''),
        ...tags,
    ].join(' ');
    return {
        id: Number(representativeWork?.id) || 0,
        title: truncate(title, 22),
        tags,
        designerTraits: buildDesignerTraits(mergedText, currentTabLabel),
        viewsText: formatCompactCount(Number(representativeWork?.views) || 0),
        likesText: formatCompactCount(Number(representativeWork?.likes) || 0),
        workImage: normalizeImageUrl(String(representativeWork?.image || '')),
        publishScope: String(representativeWork?.publish_scope || 'square'),
        placeholderToneClass: getPlaceholderToneClass(index, currentTabValue),
        workPlaceholderText: getWorkPlaceholderText(title, currentTabLabel),
        designerUserId,
        designerName,
        designerAvatar: normalizeImageUrl(String(item?.designer_avatar || '')),
        designerAvatarText: getAvatarText(designerName),
        designerTitle: truncate(designerTitle, 12),
    };
}
function buildDesignerCardsFromApi(list, currentTabValue, currentTabLabel) {
    const seen = new Set();
    return list.reduce((acc, item, index) => {
        const card = buildDesignerCardFromApi(item, index, currentTabValue, currentTabLabel);
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
        searchInputValue: '',
        searchKeyword: '',
        resultSummaryText: '',
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
    onPullDownRefresh() {
        this.loadDesigners(true).finally(() => {
            wx.stopPullDownRefresh();
        });
    },
    onShareAppMessage() {
        return {
            title: '设计师中心',
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
            return;
        }
        const currentTab = this.getCurrentMainTab();
        const currentTabValue = currentTab?.value || 'all';
        const currentTabLabel = currentTab?.label || DEFAULT_MAIN_TABS[0].label;
        const requestPage = reset ? 1 : this.data.page;
        const keyword = String(this.data.searchKeyword || '').trim();
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        this.setData({
            loading: true,
            currentCategoryLabel: currentTabLabel,
            emptyTipText: buildDesignerEmptyTip(currentTabLabel, keyword, selectedTrait),
        });
        const result = await requestDesignerApi({
            keyword,
            page: requestPage,
            page_size: this.data.pageSize,
            main_tab: currentTabValue === 'all' ? '' : currentTabValue,
        });
        let sourceCards = [];
        let nextHasMore = false;
        let nextEmptyTipText = buildDesignerEmptyTip(currentTabLabel, keyword, selectedTrait);
        let nextDesignerCountText = String(reset ? 0 : this.data.designerCountText || 0);
        if (!result) {
            sourceCards = [];
            nextHasMore = false;
            nextEmptyTipText = '设计师列表加载失败，请稍后重试';
            nextDesignerCountText = '0';
        }
        else {
            const list = Array.isArray(result.list) ? result.list : [];
            const mappedCards = buildDesignerCardsFromApi(list, currentTabValue, currentTabLabel);
            sourceCards = filterDesignerList(mappedCards, keyword, selectedTrait);
            nextHasMore = Number(result.total || 0) > requestPage * this.data.pageSize;
            nextDesignerCountText = String(Number(result.total || 0));
        }
        const nextDesignerCards = reset ? sourceCards : mergeDesignerCards(this.data.designerCards, sourceCards);
        if (selectedTrait && selectedTrait !== '全部') {
            nextDesignerCountText = String(nextDesignerCards.length);
        }
        this.setData({
            designerCards: nextDesignerCards,
            loading: false,
            page: requestPage + 1,
            hasMore: nextHasMore,
            designerCountText: nextDesignerCountText,
            currentCategoryLabel: currentTabLabel,
            resultSummaryText: buildResultSummary(keyword, selectedTrait),
            emptyTipText: nextEmptyTipText,
        });
    },
    onSearchInput(e) {
        this.setData({
            searchInputValue: String(e.detail.value || ''),
        });
    },
    onSearchConfirm() {
        const keyword = String(this.data.searchInputValue || '').trim();
        const currentTab = this.getCurrentMainTab();
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        this.setData({
            searchKeyword: keyword,
            page: 1,
            hasMore: true,
            designerCards: [],
            currentCategoryLabel: currentTab?.label || DEFAULT_MAIN_TABS[0].label,
            resultSummaryText: buildResultSummary(keyword, selectedTrait),
            emptyTipText: buildDesignerEmptyTip(currentTab?.label || DEFAULT_MAIN_TABS[0].label, keyword, selectedTrait),
        });
        this.loadDesigners(true);
    },
    onSearchClear() {
        const currentTab = this.getCurrentMainTab();
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        if (!this.data.searchKeyword && !this.data.searchInputValue) {
            return;
        }
        this.setData({
            searchInputValue: '',
            searchKeyword: '',
            page: 1,
            hasMore: true,
            designerCards: [],
            currentCategoryLabel: currentTab?.label || DEFAULT_MAIN_TABS[0].label,
            resultSummaryText: buildResultSummary('', selectedTrait),
            emptyTipText: buildDesignerEmptyTip(currentTab?.label || DEFAULT_MAIN_TABS[0].label, '', selectedTrait),
        });
        this.loadDesigners(true);
    },
    onTraitFilterTap(e) {
        const index = Number(e.currentTarget.dataset.index);
        const selectedTrait = this.data.traitFilters[index];
        const currentTab = this.getCurrentMainTab();
        if (Number.isNaN(index) || !selectedTrait || selectedTrait === this.data.selectedTrait) {
            return;
        }
        this.setData({
            selectedTrait,
            page: 1,
            hasMore: true,
            designerCards: [],
            currentCategoryLabel: currentTab?.label || DEFAULT_MAIN_TABS[0].label,
            resultSummaryText: buildResultSummary(this.data.searchKeyword, selectedTrait),
            emptyTipText: buildDesignerEmptyTip(currentTab?.label || DEFAULT_MAIN_TABS[0].label, this.data.searchKeyword, selectedTrait),
        });
        this.loadDesigners(true);
    },
    async onMainTabTap(e) {
        const index = Number(e.currentTarget.dataset.index);
        const tab = this.data.tabs[index];
        const selectedTrait = String(this.data.selectedTrait || '全部').trim();
        if (Number.isNaN(index) || index === this.data.currentTabIndex || !tab) {
            return;
        }
        this.setData({
            currentTabIndex: index,
            page: 1,
            hasMore: true,
            designerCards: [],
            currentCategoryLabel: tab.label,
            resultSummaryText: buildResultSummary(this.data.searchKeyword, selectedTrait),
            emptyTipText: buildDesignerEmptyTip(tab.label, this.data.searchKeyword, selectedTrait),
        });
        await this.loadDesigners(true);
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
    onTabSwitch(e) {
        console.log('切换 tab', e.detail);
    },
});
