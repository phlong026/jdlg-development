"use strict";
/**
 * 统一导航栏组件
 * 所有页面共用，自动适配状态栏高度和胶囊按钮位置
 */
Component({
    options: {
        multipleSlots: true,
    },
    properties: {
        /** 标题文字 */
        title: { type: String, value: '' },
        /** 背景色，默认透明 */
        background: { type: String, value: 'transparent' },
        /** 图标和标题颜色，默认深棕（暖色背景用） */
        color: { type: String, value: '#473720' },
        /** 是否显示返回按钮 */
        back: { type: Boolean, value: true },
        /** 副标题 */
        subtitle: { type: String, value: '' },
        /** 返回页面深度 */
        delta: { type: Number, value: 1 },
    },
    data: {
        navSafeTop: 0,
        navBarHeight: 96,
        navContentHeight: 44,
        navSideWidth: 88,
    },
    lifetimes: {
        attached() {
            this._initNavLayout();
        },
    },
    methods: {
        /** 计算导航栏布局尺寸，与胶囊按钮对齐 */
        _initNavLayout() {
            try {
                const systemInfo = wx.getSystemInfoSync();
                const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
                    ? wx.getMenuButtonBoundingClientRect()
                    : null;
                const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
                if (menuRect && menuRect.bottom > 0) {
                    const menuTop = Number(menuRect.top || safeTop);
                    const menuHeight = Number(menuRect.height || 32);
                    const contentHeight = Math.max(menuHeight, 36);
                    const bottomGap = 8;
                    this.setData({
                        navSafeTop: menuTop,
                        navBarHeight: Math.ceil(menuTop + contentHeight + bottomGap),
                        navContentHeight: contentHeight,
                        navSideWidth: Number(systemInfo.windowWidth - menuRect.left),
                    });
                    return;
                }
                // 无法获取胶囊信息时回退
                this.setData({
                    navSafeTop: safeTop,
                    navBarHeight: safeTop + 52,
                    navContentHeight: 44,
                    navSideWidth: 96,
                });
            }
            catch (_e) {
                this.setData({
                    navSafeTop: 20,
                    navBarHeight: 64,
                    navContentHeight: 44,
                    navSideWidth: 96,
                });
            }
        },
        /** 返回上一页 */
        onBack() {
            const delta = this.data.delta;
            this.triggerEvent('back', { delta });
            wx.navigateBack({
                delta,
                fail: () => {
                    wx.switchTab({ url: '/pages/index/index' });
                },
            });
        },
    },
});
