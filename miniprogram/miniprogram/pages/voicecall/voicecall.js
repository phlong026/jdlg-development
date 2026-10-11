"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/voicecall/voicecall.ts
const asset_1 = require("../../utils/asset");
Page({
    /**
     * 页面的初始数据
     */
    data: {
        isMuted: false,
        isRecording: false,
        avatarImage: (0, asset_1.resolveAssetPath)('/assets/images/home.jpg')
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad() {
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
    },
    /**
     * 生命周期函数--监听页面卸载
     */
    onUnload() {
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
            title: '语音通话',
            path: '/pages/voicecall/voicecall'
        };
    },
    /**
     * 菜单
     */
    onMenu() {
        wx.showActionSheet({
            itemList: ['设置', '帮助', '关于'],
            success: (res) => {
                console.log('选择', res.tapIndex);
            }
        });
    },
    /**
     * 选择情景
     */
    onSelectScene() {
        wx.showActionSheet({
            itemList: ['工作', '休闲', '学习', '其他'],
            success: (res) => {
                console.log('选择情景', res.tapIndex);
            }
        });
    },
    /**
     * 静音/取消静音
     */
    onMute() {
        this.setData({
            isMuted: !this.data.isMuted
        });
        wx.showToast({
            title: this.data.isMuted ? '已静音' : '取消静音',
            icon: 'none'
        });
    },
    /**
     * 上传
     */
    onUpload() {
        wx.showToast({
            title: '上传功能',
            icon: 'none'
        });
    },
    /**
     * 摄像头
     */
    onCamera() {
        wx.showToast({
            title: '切换摄像头',
            icon: 'none'
        });
    },
    /**
     * 结束通话
     */
    onEndCall() {
        wx.showModal({
            title: '提示',
            content: '确定要结束通话吗？',
            success: (res) => {
                if (res.confirm) {
                    wx.navigateBack({
                        fail: () => {
                            wx.switchTab({
                                url: '/pages/index/index'
                            });
                        }
                    });
                }
            }
        });
    }
});
