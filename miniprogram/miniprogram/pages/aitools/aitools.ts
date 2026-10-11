import {
  AI_TOOL_CATEGORIES,
  AIToolCategory,
  AIToolItem,
  getCategoryLabel,
  splitToolsIntoColumns,
} from '../../utils/aiTools'
import { fetchAIToolList } from '../../utils/aiToolApi'

type CategoryTapEvent = {
  currentTarget: {
    dataset: {
      category?: AIToolCategory
    }
  }
}

type InputEvent = {
  detail: {
    value?: string
  }
}

type ToolTapEvent = {
  currentTarget: {
    dataset: {
      id?: string
    }
  }
}

function buildSummary(category: AIToolCategory, keyword: string, count: number) {
  const categoryLabel = getCategoryLabel(category)
  if (keyword) {
    return `${categoryLabel} · 搜索“${keyword}”共找到 ${count} 个工具`
  }
  return `${categoryLabel} · 当前共收录 ${count} 个工具`
}

function buildEmptyText(category: AIToolCategory, keyword: string) {
  const categoryLabel = getCategoryLabel(category)
  if (keyword) {
    return `在${categoryLabel}分类下暂时没有找到和“${keyword}”相关的工具`
  }
  return `当前${categoryLabel}分类下暂时没有可展示的工具`
}

Page({
  data: {
    categories: AI_TOOL_CATEGORIES,
    currentCategory: 'architecture' as AIToolCategory,
    searchInputValue: '',
    searchKeyword: '',
    loading: false,
    displayTools: [] as AIToolItem[],
    leftColumnTools: [] as AIToolItem[],
    rightColumnTools: [] as AIToolItem[],
    summaryText: '',
    emptyText: '',
  },

  onLoad() {
    this.refreshTools()
  },

  onCategoryTap(e: CategoryTapEvent) {
    const category = e.currentTarget.dataset.category || 'architecture'
    if (category === this.data.currentCategory) {
      return
    }
    this.setData({
      currentCategory: category,
    })
    this.refreshTools()
  },

  onSearchInput(e: InputEvent) {
    this.setData({
      searchInputValue: String(e.detail.value || ''),
    })
  },

  onSearchConfirm() {
    this.setData({
      searchKeyword: String(this.data.searchInputValue || '').trim(),
    })
    this.refreshTools()
  },

  onSearchClear() {
    this.setData({
      searchInputValue: '',
      searchKeyword: '',
    })
    this.refreshTools()
  },

  async refreshTools() {
    this.setData({ loading: true })
    try {
      const result = await fetchAIToolList({
        category: this.data.currentCategory,
        keyword: this.data.searchKeyword,
        page: 1,
        pageSize: 100,
      })
      const tools = result.list.slice().sort((a, b) => {
        if (!!a.common === !!b.common) {
          return Number(a.sortOrder || 0) - Number(b.sortOrder || 0)
        }
        return a.common ? -1 : 1
      })
      const columns = splitToolsIntoColumns(tools)
      this.setData({
        displayTools: tools,
        leftColumnTools: columns.left,
        rightColumnTools: columns.right,
        summaryText: buildSummary(this.data.currentCategory, this.data.searchKeyword, result.total || tools.length),
        emptyText: buildEmptyText(this.data.currentCategory, this.data.searchKeyword),
      })
    } catch (error: any) {
      this.setData({
        displayTools: [],
        leftColumnTools: [],
        rightColumnTools: [],
        summaryText: buildSummary(this.data.currentCategory, this.data.searchKeyword, 0),
        emptyText: buildEmptyText(this.data.currentCategory, this.data.searchKeyword),
      })
      wx.showToast({
        title: error?.message || '加载工具失败',
        icon: 'none',
      })
    } finally {
      this.setData({ loading: false })
    }
  },

  onToolTap(e: ToolTapEvent) {
    const id = String(e.currentTarget.dataset.id || '')
    if (!id) {
      return
    }
    wx.navigateTo({
      url: `/pages/aitooldetail/aitooldetail?id=${id}`,
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  onShareAppMessage() {
    return {
      title: 'AI生图工具',
      path: '/pages/aitools/aitools',
    }
  },
})
