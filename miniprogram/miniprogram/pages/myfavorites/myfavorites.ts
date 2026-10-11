import { normalizeCosUrl, resolveAssetPath } from '../../utils/asset';
import { fetchFavorites, removeFavorite, FavoriteTargetType } from '../../utils/favoriteApi';
import {
  buildSelectedMap,
  getCurrentSelectableIds,
  isEveryCurrentSelected,
  toggleCurrentSelection,
} from './currentPageSelection';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const MANAGE_VISIBLE_FALLBACK_COUNT = 9;

const favoriteTabs = [
  { label: '全部', value: '' },
  { label: '模板', value: 'template' },
  { label: 'AI工具', value: 'ai_tool' },
  { label: '设计师', value: 'designer' },
  { label: '灵感', value: 'inspiration' },
];

const typeLabels: Record<string, string> = {
  template: '模板',
  ai_tool: 'AI工具',
  designer: '设计师',
  inspiration: '灵感',
};

function normalizeImageUrl(url: string, fallback = ''): string {
  const cleanUrl = String(url || '').trim();
  if (!cleanUrl) {
    return fallback;
  }
  if (/^(https?:\/\/|wxfile:\/\/|file:\/\/|data:)/i.test(cleanUrl)) {
    return /^https?:\/\//i.test(cleanUrl) ? normalizeCosUrl(cleanUrl) : cleanUrl;
  }
  if (cleanUrl.startsWith('//')) {
    return normalizeCosUrl(`https:${cleanUrl}`);
  }
  if (cleanUrl.startsWith('/')) {
    return `${API_BASE_URL}${cleanUrl}`;
  }
  return `${API_BASE_URL}/${cleanUrl}`;
}

function normalizeTextList(value: any): string[] {
  return Array.isArray(value)
    ? value.map((item) => String(item || '').trim()).filter(Boolean).slice(0, 3)
    : [];
}

function buildFavoriteSearchText(item: any): string {
  return [
    item?.title,
    item?.author,
    item?.typeLabel,
    ...(Array.isArray(item?.tags) ? item.tags : []),
  ]
    .map((value) => String(value || '').trim().toLowerCase())
    .filter(Boolean)
    .join(' ');
}

Page({
  data: {
    loading: true,
    pageReady: false,
    favoriteTabs,
    currentType: '',
    allItems: [] as any[],
    items: [] as any[],
    emptyText: '暂时还没有收藏内容',
    defaultImage: resolveAssetPath('/assets/images/home.jpg'),
    searchInputValue: '',
    searchKeyword: '',
    page: 1,
    pageSize: 20,
    total: 0,
    hasMore: true,
    loadingMore: false,
    removingKey: '',
    isEditMode: false,
    selectedKeys: [] as string[],
    selectedMap: {} as Record<string, boolean>,
    selectAll: false,
    visibleKeys: [] as string[],
  },

  manageVisibilityObserver: null as any,
  visibleKeySet: {} as Record<string, boolean>,

  onLoad() {
    this.setData({ pageReady: true });
    this.loadFavorites({ reset: true });
  },

  onShow() {
    if (!this.data.pageReady) {
      return;
    }
    this.loadFavorites({ reset: true });
  },

  onUnload() {
    this.disconnectManageVisibilityObserver();
  },

  onPullDownRefresh() {
    this.loadFavorites({ reset: true }).finally(() => {
      wx.stopPullDownRefresh();
    });
  },

  onReachBottom() {
    this.loadNextPage();
  },

  mapFavoriteItem(data: any) {
    const targetType = String(data.target_type || 'template') as FavoriteTargetType;
    const targetId = Number(data.target_id || data.id || 0);
    const metaTexts = normalizeTextList(data.meta);
    const typeLabel = String(data.type_label || typeLabels[targetType] || '收藏');
    return {
      id: targetId,
      targetId,
      targetType,
      key: `${targetType}:${targetId}`,
      title: String(data.title || data.name || '未命名收藏'),
      subtitle: String(data.subtitle || data.description || ''),
      author: String(data.author || data.creator || ''),
      image: normalizeImageUrl(String(data.image || data.thumbnail || data.preview_url || ''), this.data.defaultImage),
      tags: normalizeTextList(data.tags),
      metaTexts,
      typeLabel,
      badgeText: String(data.badge_text || typeLabel),
    };
  },

  applySearchFilter(keyword?: string) {
    const rawKeyword = String(keyword ?? this.data.searchKeyword ?? '').trim();
    const normalizedKeyword = rawKeyword.toLowerCase();
    const allItems = Array.isArray(this.data.allItems) ? this.data.allItems : [];
    const items = normalizedKeyword
      ? allItems.filter((item) => buildFavoriteSearchText(item).includes(normalizedKeyword))
      : allItems;
    this.setData({
      searchKeyword: normalizedKeyword,
      items,
      emptyText: normalizedKeyword
        ? `没有找到与“${rawKeyword}”相关的收藏`
        : '暂时还没有收藏内容',
    }, () => this.syncManageSelectionAfterItemsChange());
  },

  onSearchInput(e: any) {
    this.setData({
      searchInputValue: String(e.detail?.value || ''),
    });
  },

  onSearchConfirm() {
    this.applySearchFilter(this.data.searchInputValue);
  },

  onSearchClear() {
    if (!this.data.searchInputValue && !this.data.searchKeyword) {
      return;
    }
    this.setData({
      searchInputValue: '',
    }, () => this.applySearchFilter(''));
  },

  onFavoriteTabTap(e: any) {
    const type = String(e.currentTarget.dataset.type || '');
    if (type === this.data.currentType) {
      return;
    }
    this.setData({
      currentType: type,
      searchInputValue: '',
      searchKeyword: '',
    }, () => this.loadFavorites({ reset: true }));
  },

  async loadFavorites(options: { reset?: boolean; page?: number } = {}) {
    const reset = options.reset !== false;
    const page = options.page || (reset ? 1 : Number(this.data.page || 1));
    const pageSize = Number(this.data.pageSize || 20);
    this.setData(reset ? { loading: true, hasMore: true } : { loadingMore: true });

    try {
      // fetchFavorites uses /favorites?type=... so the page can filter by category.
      const response = await fetchFavorites({
        type: this.data.currentType as FavoriteTargetType | '',
        page,
        pageSize,
      });
      const list = Array.isArray(response.list) ? response.list : [];
      const items = list.map((data: any) => this.mapFavoriteItem(data)).filter((item: any) => item.id > 0);
      const total = Number(response.total || 0);
      const mergedItems = reset ? items : [...(Array.isArray(this.data.allItems) ? this.data.allItems : []), ...items];
      this.setData({
        loading: false,
        loadingMore: false,
        allItems: mergedItems,
        page,
        total,
        hasMore: page * pageSize < total,
      }, () => this.applySearchFilter(this.data.searchInputValue || this.data.searchKeyword));
    } catch (error: any) {
      this.setData({
        loading: false,
        loadingMore: false,
        allItems: [],
        items: [],
        hasMore: false,
        emptyText: error?.message || '收藏加载失败',
      });
      wx.showToast({
        title: error?.message || '收藏加载失败',
        icon: 'none',
      });
    }
  },

  loadNextPage() {
    if (this.data.loading || this.data.loadingMore || !this.data.hasMore) {
      return;
    }
    this.loadFavorites({
      reset: false,
      page: Number(this.data.page || 1) + 1,
    });
  },

  async removeFavorite(e: any) {
    const targetType = String(e.currentTarget.dataset.targetType || '') as FavoriteTargetType;
    const targetId = Number(e.currentTarget.dataset.targetId || 0);
    const removingKey = `${targetType}:${targetId}`;
    if (!targetType || !targetId || this.data.removingKey) {
      return;
    }

    try {
      this.setData({ removingKey });
      await removeFavorite(targetType, targetId);
      const allItems = (Array.isArray(this.data.allItems) ? this.data.allItems : []).filter((item: any) => item.key !== removingKey);
      this.setData({
        allItems,
        total: Math.max(0, Number(this.data.total || 0) - 1),
        removingKey: '',
      }, () => this.applySearchFilter(this.data.searchInputValue || this.data.searchKeyword));
      wx.showToast({
        title: '已取消收藏',
        icon: 'none',
      });
    } catch (error: any) {
      this.setData({ removingKey: '' });
      wx.showToast({
        title: error?.message || '取消收藏失败',
        icon: 'none',
      });
    }
  },

  onToggleEditMode() {
    const isEditMode = !this.data.isEditMode;
    this.setData({
      isEditMode,
      selectedKeys: [],
      selectedMap: {},
      selectAll: false,
      visibleKeys: [],
    }, () => {
      if (isEditMode) {
        this.startManageVisibilityObserver();
      } else {
        this.disconnectManageVisibilityObserver();
      }
    });
  },

  onToggleSelect(e: any) {
    const key = String(e.currentTarget.dataset.key || '').trim();
    if (!key) {
      return;
    }
    const selectedKeys = this.data.selectedKeys.slice();
    const index = selectedKeys.indexOf(key);
    if (index >= 0) {
      selectedKeys.splice(index, 1);
    } else {
      selectedKeys.push(key);
    }
    this.applySelectionState(selectedKeys);
  },

  onToggleSelectAll() {
    const selectedKeys = toggleCurrentSelection(this.data.selectedKeys, this.getCurrentManageKeys());
    this.applySelectionState(selectedKeys);
  },

  applySelectionState(selectedKeys: string[]) {
    this.setData({
      selectedKeys,
      selectedMap: buildSelectedMap(selectedKeys),
      selectAll: isEveryCurrentSelected(selectedKeys, this.getCurrentManageKeys()),
    });
  },

  getCurrentManageKeys(): string[] {
    return getCurrentSelectableIds(
      this.data.items,
      (item: any) => item.key,
      this.data.visibleKeys,
      MANAGE_VISIBLE_FALLBACK_COUNT,
    );
  },

  syncManageSelectionAfterItemsChange() {
    const availableMap = buildSelectedMap(this.data.items.map((item: any) => item.key));
    const selectedKeys = this.data.selectedKeys.filter((key) => availableMap[key]);
    this.visibleKeySet = Object.keys(this.visibleKeySet || {}).reduce((acc, key) => {
      if (availableMap[key]) {
        acc[key] = true;
      }
      return acc;
    }, {} as Record<string, boolean>);
    this.applySelectionState(selectedKeys);
    if (this.data.isEditMode) {
      this.startManageVisibilityObserver();
    }
  },

  refreshManageVisibleKeys() {
    const nextVisibleKeys = this.data.items
      .map((item: any) => item.key)
      .filter((key: string) => this.visibleKeySet[key]);
    const currentVisibleKeys = this.data.visibleKeys || [];
    if (nextVisibleKeys.join('|') === currentVisibleKeys.join('|')) {
      this.refreshManageSelectAllState();
      return;
    }
    this.setData({ visibleKeys: nextVisibleKeys }, () => this.refreshManageSelectAllState());
  },

  refreshManageSelectAllState() {
    const nextSelectAll = isEveryCurrentSelected(this.data.selectedKeys, this.getCurrentManageKeys());
    if (nextSelectAll !== this.data.selectAll) {
      this.setData({ selectAll: nextSelectAll });
    }
  },

  startManageVisibilityObserver() {
    this.disconnectManageVisibilityObserver();
    this.visibleKeySet = {};
    this.setData({ visibleKeys: [] });
    if (!this.data.isEditMode) {
      return;
    }
    (wx as any).nextTick(() => {
      if (!this.data.isEditMode) {
        return;
      }
      const createObserver = (this as any).createIntersectionObserver;
      if (typeof createObserver !== 'function') {
        return;
      }
      const observer = createObserver.call(this, { observeAll: true });
      this.manageVisibilityObserver = observer;
      observer.relativeToViewport().observe('.manage-card', (res: any) => {
        const key = String(res?.dataset?.key || '').trim();
        if (!key) {
          return;
        }
        if (Number(res?.intersectionRatio || 0) > 0) {
          this.visibleKeySet[key] = true;
        } else {
          delete this.visibleKeySet[key];
        }
        this.refreshManageVisibleKeys();
      });
    });
  },

  disconnectManageVisibilityObserver() {
    if (this.manageVisibilityObserver && typeof this.manageVisibilityObserver.disconnect === 'function') {
      this.manageVisibilityObserver.disconnect();
    }
    this.manageVisibilityObserver = null;
    this.visibleKeySet = {};
  },

  onBatchRemove() {
    const selectedKeys = this.data.selectedKeys.slice();
    if (!selectedKeys.length || this.data.removingKey) {
      return;
    }
    const selectedKeyMap = buildSelectedMap(selectedKeys);
    const selectedItems = this.data.allItems.filter((item: any) => selectedKeyMap[item.key]);
    if (!selectedItems.length) {
      return;
    }

    wx.showModal({
      title: '确认删除',
      content: `确定要删除选中的 ${selectedItems.length} 条收藏吗？`,
      confirmText: '删除',
      confirmColor: '#c4543a',
      cancelText: '取消',
      success: async (res) => {
        if (!res.confirm) return;
        wx.showLoading({ title: '删除中...' });
        const removedKeys: string[] = [];
        let failCount = 0;
        for (const item of selectedItems) {
          try {
            await removeFavorite(item.targetType, item.targetId);
            removedKeys.push(item.key);
          } catch {
            failCount++;
          }
        }
        wx.hideLoading();
        const removedMap = buildSelectedMap(removedKeys);
        const allItems = this.data.allItems.filter((item: any) => !removedMap[item.key]);
        this.setData({
          allItems,
          total: Math.max(0, Number(this.data.total || 0) - removedKeys.length),
          selectedKeys: [],
          selectedMap: {},
          selectAll: false,
          visibleKeys: [],
          isEditMode: false,
        }, () => {
          this.disconnectManageVisibilityObserver();
          this.applySearchFilter(this.data.searchInputValue || this.data.searchKeyword);
        });
        wx.showToast({
          title: failCount > 0 ? `${failCount} 条删除失败` : '已删除',
          icon: failCount > 0 ? 'none' : 'success',
        });
      },
    });
  },

  onFavoriteCardTap(e: any) {
    const targetType = String(e.currentTarget.dataset.targetType || '') as FavoriteTargetType;
    const targetId = Number(e.currentTarget.dataset.targetId || 0);
    if (!targetType || !targetId) {
      return;
    }
    const routeMap: Record<FavoriteTargetType, string> = {
      template: `/pages/templatesquaredetails/templatesquaredetails?id=${targetId}`,
      ai_tool: `/pages/aitooldetail/aitooldetail?id=${targetId}`,
      designer: `/pages/designerhome/designerhome?userId=${targetId}`,
      inspiration: `/pages/inspirationdetail/inspirationdetail?id=${targetId}`,
    };
    wx.navigateTo({
      url: routeMap[targetType],
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  onShareAppMessage() {
    return {
      title: '我的收藏',
      path: '/pages/myfavorites/myfavorites',
    };
  },
});
