import { getPageCache, prefetchImages, setPageCache } from '../../utils/perf';
import { normalizeCosUrl } from '../../utils/asset';
import { prepareShareCardImage } from '../../utils/shareImage';
import { fetchFavoriteStatus, toggleFavorite } from '../../utils/favoriteApi';

const API_BASE_URL = 'https://api.jiadilingguang.com';

export {};

const INSPIRATION_DETAIL_CACHE_TTL = 2 * 60 * 1000;

function buildInspirationDetailCacheKey(inspirationId: number) {
  return `inspiration-detail:${Number(inspirationId || 0)}`;
}

type InspirationDetail = {
  id: number;
  title: string;
  description: string;
  cover_image?: string;
  images?: string[];
  tags?: string[];
  scene?: string;
  style?: string;
  topic?: string;
  creator?: string;
  created_at?: string;
  view_count?: number;
};

Page({
  data: {
    inspirationId: 0,
    loading: true,
    detail: null as InspirationDetail | null,
    currentImage: '',
    shareImageUrl: '',
    shareImageSourceUrl: '',
    navTop: 0,
    navBarHeight: 72,
    isFavorited: false,
    favoriteLoading: false,
  },

  onLoad(options: Record<string, string | undefined>) {
    this.initLayoutMetrics();
    const inspirationId = Number(options?.id || 0);
    this.setData({ inspirationId });
    if (!inspirationId) {
      wx.showToast({ title: '参数错误', icon: 'none' });
      setTimeout(() => this.onBack(), 300);
      return;
    }
    const cachedDetail = getPageCache<InspirationDetail>(buildInspirationDetailCacheKey(inspirationId));
    this.loadFavoriteState();
    if (cachedDetail) {
      this.applyDetail(cachedDetail);
      void this.loadDetail(true);
      return;
    }
    void this.loadDetail();
  },

  onBack() {
    const pages = getCurrentPages();
    if (pages.length > 1) {
      wx.navigateBack();
      return;
    }
    wx.switchTab({ url: '/pages/template/template' });
  },

  initLayoutMetrics() {
    try {
      const menuRect = wx.getMenuButtonBoundingClientRect();
      const systemInfo = wx.getSystemInfoSync();
      const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 20);
      this.setData({
        navTop: Math.max(safeTop + 4, Number(menuRect?.top || safeTop + 8) - 2),
        navBarHeight: Math.max(64, Number(menuRect?.height || 32) + 18),
      });
    } catch (error) {
      this.setData({
        navTop: 26,
        navBarHeight: 68,
      });
    }
  },

  onPreviewImage(e: { currentTarget: { dataset: { url?: string } } }) {
    const url = String(e.currentTarget.dataset.url || '');
    const urls = this.data.detail?.images || [];
    if (!url || !urls.length) return;
    wx.previewImage({ current: url, urls });
  },

  buildInspirationPrompt() {
    const detail = this.data.detail;
    if (!detail) {
      return '';
    }
    const parts = [
      detail.title ? `参考案例：${detail.title}` : '',
      detail.scene ? `场景：${detail.scene}` : '',
      detail.style ? `风格：${detail.style}` : '',
      detail.description || '',
      '请基于当前案例图的风格、构图与空间气质，生成可直接参考的高质量同款效果图',
    ].filter(Boolean);
    return parts.join('，');
  },

  onMakeSame() {
    const detail = this.data.detail;
    const referenceImages = [this.data.currentImage, detail?.cover_image, ...(Array.isArray(detail?.images) ? detail.images : [])]
      .map((item) => String(item || '').trim())
      .filter((item, index, array) => item && array.indexOf(item) === index)
      .slice(0, 6);
    const referenceImage = referenceImages[0] || '';
    const prompt = this.buildInspirationPrompt();
    const sceneText = String(detail?.scene || '');
    const tab = /室内|内装/.test(sceneText) ? 'interior' : 'exterior';
    const query = [
      'source=scene_template',
      'showSceneTabs=1',
      `tab=${tab}`,
    ];
    if (referenceImage) {
      query.push(`reference_image_url=${encodeURIComponent(referenceImage)}`);
    }
    wx.navigateTo({
      url: `/pages/aigenerate/aigenerate?${query.join('&')}`,
      success: (navRes) => {
        navRes.eventChannel.emit('prefillGenerateData', {
          prompt,
          reference_image_url: referenceImage,
          reference_image_urls: referenceImages,
        });
      },
      fail: () => {
        wx.showToast({ title: '页面跳转失败', icon: 'none' });
      },
    });
  },

  async loadFavoriteState() {
    const inspirationId = Number(this.data.inspirationId || 0);
    if (!inspirationId) {
      this.setData({ isFavorited: false });
      return;
    }
    try {
      const data = await fetchFavoriteStatus('inspiration', inspirationId);
      this.setData({ isFavorited: data.favorited === true });
    } catch (error) {
      this.setData({ isFavorited: false });
    }
  },

  async toggleFavorite() {
    const inspirationId = Number(this.data.inspirationId || 0);
    if (!inspirationId || this.data.favoriteLoading) {
      return;
    }
    try {
      this.setData({ favoriteLoading: true });
      const data = await toggleFavorite('inspiration', inspirationId, this.data.isFavorited);
      this.setData({
        isFavorited: data.favorited === true,
        favoriteLoading: false,
      });
      wx.showToast({
        title: data.favorited === true ? '已收藏' : '已取消收藏',
        icon: 'none',
      });
    } catch (error: any) {
      this.setData({ favoriteLoading: false });
      wx.showToast({
        title: error?.message || '收藏操作失败',
        icon: 'none',
      });
    }
  },

  formatDate(raw: string): string {
    if (!raw) return '';
    return String(raw).replace('T', ' ').slice(0, 16);
  },

  applyDetail(detail: InspirationDetail) {
    const normalizedImages = (Array.isArray(detail.images) ? detail.images : []).map((img: string) => normalizeCosUrl(img));
    const normalizedCover = normalizeCosUrl(detail.cover_image || '');
    const shareSourceUrl = normalizedImages[0] || normalizedCover || '';
    this.setData({
      detail,
      currentImage: shareSourceUrl,
      loading: false,
    });
    void prefetchImages([normalizedCover, ...normalizedImages], 2);
    void this.prepareCurrentShareImage(shareSourceUrl);
  },

  async loadDetail(silent = false) {
    if (!silent) {
      this.setData({ loading: true });
    }
    wx.request({
      url: `${API_BASE_URL}/api/v1/miniprogram/inspirations/${this.data.inspirationId}`,
      method: 'GET',
      success: (res) => {
        const response = (res.data || {}) as { code?: number; msg?: string; data?: InspirationDetail };
        if (res.statusCode === 200 && response.code === 0 && response.data) {
          const detail = {
            ...response.data,
            created_at: this.formatDate(String(response.data.created_at || '')),
          };
          setPageCache(buildInspirationDetailCacheKey(this.data.inspirationId), detail, INSPIRATION_DETAIL_CACHE_TTL);
          this.applyDetail(detail);
          return;
        }
        this.setData({ loading: false });
        wx.showToast({ title: response.msg || '加载失败', icon: 'none' });
      },
      fail: () => {
        this.setData({ loading: false });
        wx.showToast({ title: '加载失败，请稍后重试', icon: 'none' });
      },
    });
  },

  onShareAppMessage() {
    const title = this.data.detail?.title || '模板详情';
    return {
      title,
      path: `/pages/inspirationdetail/inspirationdetail?id=${this.data.inspirationId}`,
      imageUrl: this.data.shareImageUrl || String(this.data.currentImage || this.data.detail?.cover_image || '').trim(),
    };
  },

  onShareTimeline() {
    const title = this.data.detail?.title || '模板详情';
    return {
      title,
      query: `id=${this.data.inspirationId}`,
      imageUrl: this.data.shareImageUrl || String(this.data.currentImage || this.data.detail?.cover_image || '').trim(),
    };
  },

  async prepareCurrentShareImage(sourceUrl?: string) {
    const shareSourceUrl = String(sourceUrl || this.data.currentImage || this.data.detail?.cover_image || '').trim();
    if (!shareSourceUrl) {
      this.setData({
        shareImageUrl: '',
        shareImageSourceUrl: '',
      });
      return;
    }
    this.setData({
      shareImageUrl: '',
      shareImageSourceUrl: shareSourceUrl,
    });
    const shareImageUrl = await prepareShareCardImage(shareSourceUrl);
    if (this.data.shareImageSourceUrl !== shareSourceUrl) {
      return;
    }
    if (shareImageUrl) {
      this.setData({ shareImageUrl });
    }
  },
});
