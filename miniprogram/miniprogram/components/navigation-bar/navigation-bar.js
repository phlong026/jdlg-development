/**
 * 统一导航栏组件
 * 所有页面共用，自动适配状态栏高度和胶囊按钮位置
 */
function readNavigationLayout() {
    try {
        const systemInfo = (wx.getWindowInfo ? Object.assign({}, wx.getDeviceInfo && wx.getDeviceInfo(), wx.getWindowInfo(), wx.getAppBaseInfo && wx.getAppBaseInfo()) : wx.getSystemInfoSync());
        const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
            ? wx.getMenuButtonBoundingClientRect()
            : null;
        const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
        if (menuRect && menuRect.bottom > 0) {
            const menuTop = Number(menuRect.top || safeTop);
            const menuHeight = Number(menuRect.height || 32);
            const contentHeight = Math.max(menuHeight, 36);
            const bottomGap = 8;
            return {
                navSafeTop: menuTop,
                navBarHeight: Math.ceil(menuTop + contentHeight + bottomGap),
                navContentHeight: contentHeight,
                navSideWidth: Number(systemInfo.windowWidth - menuRect.left),
            };
        }
        // 无法获取胶囊信息时回退
        return {
            navSafeTop: safeTop,
            navBarHeight: safeTop + 52,
            navContentHeight: 44,
            navSideWidth: 96,
        };
    }
    catch (_e) {
        return {
            navSafeTop: 20,
            navBarHeight: 64,
            navContentHeight: 44,
            navSideWidth: 96,
        };
    }
}
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
        /** 多层视图由页面决定返回层级，其他页面维持原导航行为 */
        customBack: { type: Boolean, value: false },
    },
    data: readNavigationLayout(),
    lifetimes: {
        attached() {
            this._initNavLayout();
        },
    },
    methods: {
        /** 计算导航栏布局尺寸，与胶囊按钮对齐 */
        _initNavLayout() {
            const layout = readNavigationLayout();
            if (this.data.navSafeTop !== layout.navSafeTop
                || this.data.navBarHeight !== layout.navBarHeight
                || this.data.navContentHeight !== layout.navContentHeight
                || this.data.navSideWidth !== layout.navSideWidth) {
                this.setData(layout);
            }
        },
        /** 返回上一页 */
        onBack() {
            const delta = this.data.delta;
            this.triggerEvent('back', { delta });
            if (this.data.customBack)
                return;
            wx.navigateBack({
                delta,
                fail: () => {
                    wx.switchTab({ url: '/pages/index/index' });
                },
            });
        },
    },
});
