import { HOME_ASSETS } from '../utils/homeAssets'

export {}

function getStoredTabBarCurrent() {
  const app = getApp<any>()
  const current = Number(app?.globalData?.tabBarCurrent)
  return Number.isNaN(current) ? 0 : current
}

const TABS = [
  {
    index: 0,
    key: 'home',
    text: '首页',
    pagePath: '/pages/index/index'
  },
  {
    index: 1,
    key: 'template',
    text: '模板广场',
    pagePath: '/pages/template/template'
  },
  {
    index: 2,
    key: 'designer',
    text: '设计师中心',
    pagePath: '/pages/distribution/distribution'
  },
  {
    index: 3,
    key: 'my',
    text: '我',
    pagePath: '/pages/my/my'
  }
]

Component({
  data: {
    current: getStoredTabBarCurrent(),
    tabs: TABS,
    defaultTabbarImage: HOME_ASSETS.defaultTabbar,
    homeTabbarImage: HOME_ASSETS.homeTabbar,
    templateTabbarImage: HOME_ASSETS.templateTabbar,
    designerTabbarImage: HOME_ASSETS.designerTabbar,
    myTabbarImage: HOME_ASSETS.myTabbar
  },

  lifetimes: {
    attached() {
      this.updateCurrentByRoute()
    }
  },

  pageLifetimes: {
    show() {
      this.updateCurrentByRoute()
    }
  },

  methods: {
    normalizePath(path: string) {
      return String(path || '').replace(/^\//, '')
    },

    setCurrent(index: number) {
      const nextIndex = Number(index)
      if (Number.isNaN(nextIndex) || nextIndex < 0) {
        return
      }

      const app = getApp<any>()
      if (app?.globalData) {
        app.globalData.tabBarCurrent = nextIndex
      }

      if (nextIndex !== this.data.current) {
        this.setData({
          current: nextIndex
        })
      }
    },

    updateCurrentByRoute() {
      const pages = getCurrentPages()
      const currentPage = pages[pages.length - 1]
      const currentRoute = this.normalizePath(currentPage?.route || '')
      const nextIndex = this.data.tabs.findIndex((tab: any) => this.normalizePath(tab.pagePath) === currentRoute)

      if (nextIndex !== -1) {
        this.setCurrent(nextIndex)
      }
    },

    switchTab(e: any) {
      const index = Number(e.currentTarget.dataset.index)
      const tab = this.data.tabs[index]

      if (!tab || index === this.data.current) {
        return
      }

      const previousCurrent = this.data.current

      this.setCurrent(index)

      wx.switchTab({
        url: tab.pagePath,
        fail: () => {
          this.setCurrent(previousCurrent)
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none'
          })
        }
      })
    }
  }
})
