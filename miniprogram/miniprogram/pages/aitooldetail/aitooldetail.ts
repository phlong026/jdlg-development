import { AIToolItem, getCategoryLabel } from '../../utils/aiTools'
import { fetchAIToolDetail } from '../../utils/aiToolApi'
import { shouldUseMinimalAIToolPresentation } from '../../utils/aiToolPresentation'
import { fetchFavoriteStatus, toggleFavorite } from '../../utils/favoriteApi'

type PageOptions = {
  id?: string
}

function buildUsageTips(tool: AIToolItem | null | undefined): string[] {
  if (!tool || tool.showUsageTips === false) {
    return []
  }

  const content = String(tool.usageTipsContent || '').trim()
  if (!content) {
    return []
  }

  return content
    .split(/\r?\n+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

Page({
  data: {
    tool: null as AIToolItem | null,
    categoryLabel: '',
    useMinimalPresentation: false,
    highlights: [] as string[],
    usageTipsTitle: '使用提示',
    loading: false,
    isFavorited: false,
    favoriteLoading: false,
  },

  async onLoad(options: PageOptions) {
    const id = String(options.id || '')
    if (!id) {
      wx.showToast({
        title: '工具信息不存在',
        icon: 'none',
      })
      return
    }
    this.setData({ loading: true })
    try {
      const tool = await fetchAIToolDetail(id)
      const useMinimalPresentation = shouldUseMinimalAIToolPresentation(tool)
      this.setData({
        tool,
        categoryLabel: getCategoryLabel(tool.category),
        useMinimalPresentation,
        highlights: buildUsageTips(tool),
        usageTipsTitle: String(tool.usageTipsTitle || '使用提示'),
      })
      this.loadFavoriteState()
    } catch (error: any) {
      wx.showToast({
        title: error?.message || '工具信息不存在',
        icon: 'none',
      })
    } finally {
      this.setData({ loading: false })
    }
  },

  onStartUse() {
    const tool = this.data.tool
    if (!tool) {
      return
    }
    if (tool.code === 'new_minnan_tool') {
      wx.navigateTo({
        url: `/pages/newminnantool/newminnantool?id=${tool.id}`,
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
      return
    }
    wx.navigateTo({
      url: `/pages/aitoolworkbench/aitoolworkbench?id=${tool.id}`,
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  async loadFavoriteState() {
    const tool = this.data.tool
    if (!tool?.id) {
      this.setData({ isFavorited: false })
      return
    }
    try {
      const data = await fetchFavoriteStatus('ai_tool', Number(tool.id))
      this.setData({ isFavorited: data.favorited === true })
    } catch (error) {
      this.setData({ isFavorited: false })
    }
  },

  async toggleFavorite() {
    const tool = this.data.tool
    if (!tool?.id || this.data.favoriteLoading) {
      return
    }
    try {
      this.setData({ favoriteLoading: true })
      const data = await toggleFavorite('ai_tool', Number(tool.id), this.data.isFavorited)
      this.setData({
        isFavorited: data.favorited === true,
        favoriteLoading: false,
      })
      wx.showToast({
        title: data.favorited === true ? '已收藏' : '已取消收藏',
        icon: 'none',
      })
    } catch (error: any) {
      this.setData({ favoriteLoading: false })
      wx.showToast({
        title: error?.message || '收藏操作失败',
        icon: 'none',
      })
    }
  },

  onShareAppMessage() {
    const tool = this.data.tool
    return {
      title: tool ? `${tool.name} - AI生图工具` : 'AI生图工具',
      path: tool ? `/pages/aitooldetail/aitooldetail?id=${tool.id}` : '/pages/aitools/aitools',
    }
  },
})
