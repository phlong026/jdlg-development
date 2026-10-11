"use strict";
// pages/utilitytools/utilitytools.ts
// API基础地址
const UTILITY_TOOLS_API_BASE_URL = 'https://api.jiadilingguang.com';
Page({
    /**
     * 页面的初始数据
     */
    data: {
        currentCategory: 'all', // all, local_norm, faq, video_tutorial
        tools: [],
        loading: false,
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad() {
        this.loadTools();
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShow() {
        // 可以在这里刷新数据
    },
    /**
     * 切换分类
     */
    switchCategory(e) {
        const category = e.currentTarget.dataset.category || 'all';
        this.setData({ currentCategory: category });
        this.loadTools();
    },
    /**
     * 加载工具列表
     */
    async loadTools() {
        this.setData({ loading: true });
        try {
            const category = this.data.currentCategory === 'all' ? '' : this.data.currentCategory;
            const url = `${UTILITY_TOOLS_API_BASE_URL}/api/v1/miniprogram/utility-tools${category ? `?category=${category}` : ''}`;
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url,
                    method: 'GET',
                    header: {
                        'Content-Type': 'application/json',
                    },
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '加载失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
            this.setData({
                tools: res.list || [],
                loading: false,
            });
        }
        catch (error) {
            console.error('加载工具列表失败:', error);
            wx.showToast({
                title: error.message || '加载失败',
                icon: 'none',
            });
            this.setData({ loading: false });
        }
    },
    /**
     * 点击工具项
     */
    onToolTap(e) {
        const id = e.currentTarget.dataset.id;
        if (!id)
            return;
        // 跳转到详情页
        wx.navigateTo({
            url: `/pages/utilitytooldetail/utilitytooldetail?id=${id}`,
        });
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · 工具合集',
            path: '/pages/utilitytools/utilitytools',
        };
    },
});
