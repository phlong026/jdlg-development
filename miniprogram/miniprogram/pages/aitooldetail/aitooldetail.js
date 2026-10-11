"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const aiTools_1 = require("../../utils/aiTools");
const aiToolApi_1 = require("../../utils/aiToolApi");
const aiToolPresentation_1 = require("../../utils/aiToolPresentation");
const favoriteApi_1 = require("../../utils/favoriteApi");
function buildUsageTips(tool) {
    if (!tool || tool.showUsageTips === false) {
        return [];
    }
    const content = String(tool.usageTipsContent || '').trim();
    if (!content) {
        return [];
    }
    return content
        .split(/\r?\n+/)
        .map((item) => item.trim())
        .filter(Boolean);
}
Page({
    data: {
        tool: null,
        categoryLabel: '',
        useMinimalPresentation: false,
        highlights: [],
        usageTipsTitle: '使用提示',
        loading: false,
        isFavorited: false,
        favoriteLoading: false,
    },
    async onLoad(options) {
        const id = String(options.id || '');
        if (!id) {
            wx.showToast({
                title: '工具信息不存在',
                icon: 'none',
            });
            return;
        }
        this.setData({ loading: true });
        try {
            const tool = await (0, aiToolApi_1.fetchAIToolDetail)(id);
            const useMinimalPresentation = (0, aiToolPresentation_1.shouldUseMinimalAIToolPresentation)(tool);
            this.setData({
                tool,
                categoryLabel: (0, aiTools_1.getCategoryLabel)(tool.category),
                useMinimalPresentation,
                highlights: buildUsageTips(tool),
                usageTipsTitle: String(tool.usageTipsTitle || '使用提示'),
            });
            this.loadFavoriteState();
        }
        catch (error) {
            wx.showToast({
                title: error?.message || '工具信息不存在',
                icon: 'none',
            });
        }
        finally {
            this.setData({ loading: false });
        }
    },
    onStartUse() {
        const tool = this.data.tool;
        if (!tool) {
            return;
        }
        if (tool.code === 'new_minnan_tool') {
            wx.navigateTo({
                url: `/pages/newminnantool/newminnantool?id=${tool.id}`,
                fail: () => {
                    wx.showToast({
                        title: '页面跳转失败',
                        icon: 'none',
                    });
                },
            });
            return;
        }
        wx.navigateTo({
            url: `/pages/aitoolworkbench/aitoolworkbench?id=${tool.id}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    async loadFavoriteState() {
        const tool = this.data.tool;
        if (!tool?.id) {
            this.setData({ isFavorited: false });
            return;
        }
        try {
            const data = await (0, favoriteApi_1.fetchFavoriteStatus)('ai_tool', Number(tool.id));
            this.setData({ isFavorited: data.favorited === true });
        }
        catch (error) {
            this.setData({ isFavorited: false });
        }
    },
    async toggleFavorite() {
        const tool = this.data.tool;
        if (!tool?.id || this.data.favoriteLoading) {
            return;
        }
        try {
            this.setData({ favoriteLoading: true });
            const data = await (0, favoriteApi_1.toggleFavorite)('ai_tool', Number(tool.id), this.data.isFavorited);
            this.setData({
                isFavorited: data.favorited === true,
                favoriteLoading: false,
            });
            wx.showToast({
                title: data.favorited === true ? '已收藏' : '已取消收藏',
                icon: 'none',
            });
        }
        catch (error) {
            this.setData({ favoriteLoading: false });
            wx.showToast({
                title: error?.message || '收藏操作失败',
                icon: 'none',
            });
        }
    },
    onShareAppMessage() {
        const tool = this.data.tool;
        return {
            title: tool ? `${tool.name} - AI生图工具` : 'AI生图工具',
            path: tool ? `/pages/aitooldetail/aitooldetail?id=${tool.id}` : '/pages/aitools/aitools',
        };
    },
});
