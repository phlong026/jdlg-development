"use strict";
// pages/utilitytooldetail/utilitytooldetail.ts
// API基础地址
const UTILITY_TOOL_DETAIL_API_BASE_URL = 'https://api.jiadilingguang.com';
Page({
    /**
     * 页面的初始数据
     */
    data: {
        toolId: 0,
        tool: null,
        loading: false,
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad(options) {
        const id = options.id ? parseInt(options.id) : 0;
        if (id > 0) {
            this.setData({ toolId: id });
            this.loadToolDetail();
        }
        else {
            wx.showToast({
                title: '参数错误',
                icon: 'none',
            });
            setTimeout(() => {
                this.goBack();
            }, 1500);
        }
    },
    /**
     * 加载工具详情
     */
    async loadToolDetail() {
        this.setData({ loading: true });
        try {
            const url = `${UTILITY_TOOL_DETAIL_API_BASE_URL}/api/v1/miniprogram/utility-tools/${this.data.toolId}`;
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
            // 格式化日期
            if (res.updated_at) {
                const date = new Date(res.updated_at);
                res.updated_at = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
            }
            this.setData({
                tool: res,
                loading: false,
            });
        }
        catch (error) {
            console.error('加载工具详情失败:', error);
            wx.showToast({
                title: error.message || '加载失败',
                icon: 'none',
            });
            this.setData({ loading: false });
        }
    },
    /**
     * 下载文件
     */
    downloadFile() {
        const fileUrl = this.data.tool?.file_url;
        if (!fileUrl) {
            wx.showToast({
                title: '文件链接不存在',
                icon: 'none',
            });
            return;
        }
        wx.showLoading({
            title: '下载中...',
        });
        wx.downloadFile({
            url: fileUrl,
            success: (res) => {
                if (res.statusCode === 200) {
                    wx.openDocument({
                        filePath: res.tempFilePath,
                        success: () => {
                            wx.hideLoading();
                        },
                        fail: () => {
                            wx.hideLoading();
                            wx.showToast({
                                title: '打开文件失败',
                                icon: 'none',
                            });
                        },
                    });
                }
                else {
                    wx.hideLoading();
                    wx.showToast({
                        title: '下载失败',
                        icon: 'none',
                    });
                }
            },
            fail: () => {
                wx.hideLoading();
                wx.showToast({
                    title: '下载失败',
                    icon: 'none',
                });
            },
        });
    },
    /**
     * 返回上一页
     */
    goBack() {
        wx.navigateBack({
            fail: () => {
                wx.switchTab({
                    url: '/pages/index/index',
                });
            },
        });
    },
    onShareAppMessage() {
        const title = this.data.tool?.title || '甲第灵光 · 工具详情';
        return {
            title,
            path: `/pages/utilitytooldetail/utilitytooldetail?id=${this.data.toolId || 0}`,
        };
    },
});
