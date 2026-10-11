"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const aiTools_1 = require("../../utils/aiTools");
const aiToolApi_1 = require("../../utils/aiToolApi");
function buildSummary(category, keyword, count) {
    const categoryLabel = (0, aiTools_1.getCategoryLabel)(category);
    if (keyword) {
        return `${categoryLabel} · 搜索“${keyword}”共找到 ${count} 个工具`;
    }
    return `${categoryLabel} · 当前共收录 ${count} 个工具`;
}
function buildEmptyText(category, keyword) {
    const categoryLabel = (0, aiTools_1.getCategoryLabel)(category);
    if (keyword) {
        return `在${categoryLabel}分类下暂时没有找到和“${keyword}”相关的工具`;
    }
    return `当前${categoryLabel}分类下暂时没有可展示的工具`;
}
Page({
    data: {
        categories: aiTools_1.AI_TOOL_CATEGORIES,
        currentCategory: 'architecture',
        searchInputValue: '',
        searchKeyword: '',
        loading: false,
        displayTools: [],
        leftColumnTools: [],
        rightColumnTools: [],
        summaryText: '',
        emptyText: '',
    },
    onLoad() {
        this.refreshTools();
    },
    onCategoryTap(e) {
        const category = e.currentTarget.dataset.category || 'architecture';
        if (category === this.data.currentCategory) {
            return;
        }
        this.setData({
            currentCategory: category,
        });
        this.refreshTools();
    },
    onSearchInput(e) {
        this.setData({
            searchInputValue: String(e.detail.value || ''),
        });
    },
    onSearchConfirm() {
        this.setData({
            searchKeyword: String(this.data.searchInputValue || '').trim(),
        });
        this.refreshTools();
    },
    onSearchClear() {
        this.setData({
            searchInputValue: '',
            searchKeyword: '',
        });
        this.refreshTools();
    },
    async refreshTools() {
        this.setData({ loading: true });
        try {
            const result = await (0, aiToolApi_1.fetchAIToolList)({
                category: this.data.currentCategory,
                keyword: this.data.searchKeyword,
                page: 1,
                pageSize: 100,
            });
            const tools = result.list.slice().sort((a, b) => {
                if (!!a.common === !!b.common) {
                    return Number(a.sortOrder || 0) - Number(b.sortOrder || 0);
                }
                return a.common ? -1 : 1;
            });
            const columns = (0, aiTools_1.splitToolsIntoColumns)(tools);
            this.setData({
                displayTools: tools,
                leftColumnTools: columns.left,
                rightColumnTools: columns.right,
                summaryText: buildSummary(this.data.currentCategory, this.data.searchKeyword, result.total || tools.length),
                emptyText: buildEmptyText(this.data.currentCategory, this.data.searchKeyword),
            });
        }
        catch (error) {
            this.setData({
                displayTools: [],
                leftColumnTools: [],
                rightColumnTools: [],
                summaryText: buildSummary(this.data.currentCategory, this.data.searchKeyword, 0),
                emptyText: buildEmptyText(this.data.currentCategory, this.data.searchKeyword),
            });
            wx.showToast({
                title: error?.message || '加载工具失败',
                icon: 'none',
            });
        }
        finally {
            this.setData({ loading: false });
        }
    },
    onToolTap(e) {
        const id = String(e.currentTarget.dataset.id || '');
        if (!id) {
            return;
        }
        wx.navigateTo({
            url: `/pages/aitooldetail/aitooldetail?id=${id}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onShareAppMessage() {
        return {
            title: 'AI生图工具',
            path: '/pages/aitools/aitools',
        };
    },
});
