"use strict";
// components/tabbar/tabbar.ts
const DEFAULT_TABBAR_IMAGE = '/assets/tabbar/未选中态@2x.png';
const HOME_TABBAR_IMAGE = '/assets/tabbar/首页导航栏@2x.png';
const TEMPLATE_TABBAR_IMAGE = '/assets/tabbar/模板广场导航栏@2x.png';
const DESIGNER_TABBAR_IMAGE = '/assets/tabbar/设计师导航栏@2x.png';
const MY_TABBAR_IMAGE = '/assets/tabbar/我的导航栏@2x.png';
Component({
    /**
     * 组件的属性列表
     */
    properties: {
        // 当前激活的页面索引
        current: {
            type: Number,
            value: 0
        }
    },
    /**
     * 组件的初始数据
     */
    data: {
        // tabbar 配置
        tabs: [
            {
                index: 0,
                key: 'home',
                text: '首页',
                path: '/pages/index/index'
            },
            {
                index: 1,
                key: 'template',
                text: '模板广场',
                path: '/pages/template/template'
            },
            {
                index: 2,
                key: 'designer',
                text: '设计师中心',
                path: '/pages/distribution/distribution'
            },
            {
                index: 3,
                key: 'my',
                text: '我',
                path: '/pages/my/my'
            }
        ],
        defaultTabbarImage: DEFAULT_TABBAR_IMAGE,
        homeTabbarImage: HOME_TABBAR_IMAGE,
        templateTabbarImage: TEMPLATE_TABBAR_IMAGE,
        designerTabbarImage: DESIGNER_TABBAR_IMAGE,
        myTabbarImage: MY_TABBAR_IMAGE
    },
    /**
     * 组件的方法列表
     */
    methods: {
        // 切换 tab
        switchTab(e) {
            const index = Number(e.currentTarget.dataset.index);
            const tab = this.data.tabs[index];
            // 如果点击的是当前页面，不处理
            if (index === Number(this.properties.current)) {
                return;
            }
            // 触发切换事件，让父组件处理
            this.triggerEvent('switch', {
                index,
                path: tab.path
            });
            wx.switchTab({
                url: tab.path,
                fail: () => {
                    wx.reLaunch({
                        url: tab.path,
                        fail: () => {
                            wx.showToast({
                                title: '页面跳转失败',
                                icon: 'none'
                            });
                        }
                    });
                }
            });
        }
    }
});
