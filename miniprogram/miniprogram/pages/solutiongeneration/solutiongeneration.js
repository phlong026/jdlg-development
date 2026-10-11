"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/solutiongeneration/solutiongeneration.ts
const asset_1 = require("../../utils/asset");
Page({
    /**
     * 页面的初始数据
     */
    data: {
        progress: 65,
        activeTab: 'floorplan',
        tabs: [
            { label: '平面图', value: 'floorplan' },
            { label: '效果图', value: 'effect' },
            { label: '立面图', value: 'elevation' },
            { label: '邀价调单', value: 'quotation' }
        ],
        progressTimer: null,
        solutionImage: (0, asset_1.resolveAssetPath)('/assets/images/home.jpg')
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad() {
        // 模拟进度条更新
        this.startProgress();
    },
    /**
     * 生命周期函数--监听页面初次渲染完成
     */
    onReady() {
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShow() {
    },
    /**
     * 生命周期函数--监听页面隐藏
     */
    onHide() {
        this.stopProgress();
    },
    /**
     * 生命周期函数--监听页面卸载
     */
    onUnload() {
        this.stopProgress();
    },
    /**
     * 页面相关事件处理函数--监听用户下拉动作
     */
    onPullDownRefresh() {
    },
    /**
     * 页面上拉触底事件的处理函数
     */
    onReachBottom() {
    },
    /**
     * 用户点击右上角分享
     */
    onShareAppMessage() {
        return {
            title: '方案生成中',
            path: '/pages/solutiongeneration/solutiongeneration'
        };
    },
    /**
     * 开始进度条
     */
    startProgress() {
        const timer = setInterval(() => {
            if (this.data.progress < 100) {
                this.setData({
                    progress: Math.min(this.data.progress + 1, 100)
                });
            }
            else {
                this.stopProgress();
            }
        }, 1000);
        this.setData({
            progressTimer: timer
        });
    },
    /**
     * 停止进度条
     */
    stopProgress() {
        if (this.data.progressTimer) {
            clearInterval(this.data.progressTimer);
            this.setData({
                progressTimer: null
            });
        }
    },
    /**
     * Tab切换
     */
    onTabSwitch(e) {
        const value = e.currentTarget.dataset.value;
        this.setData({
            activeTab: value
        });
        console.log('切换Tab', value);
    },
    /**
     * 确认生成
     */
    onConfirm() {
        wx.showToast({
            title: '确认生成成功',
            icon: 'success'
        });
    },
    /**
     * 导出相册
     */
    onExport() {
        wx.showToast({
            title: '导出到相册',
            icon: 'success'
        });
    }
});
