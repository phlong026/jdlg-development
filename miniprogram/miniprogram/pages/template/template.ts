// pages/template/template.ts

import { resolveAssetPath, normalizeCosUrl } from '../../utils/asset';
import { getPageCache, prefetchImages, setPageCache } from '../../utils/perf';

export {};

const API_BASE_URL = 'https://api.jiadilingguang.com';
const TEMPLATE_CARD_COLUMN_WIDTH_RPX = 347;
const DEFAULT_CARD_MEDIA_HEIGHT_RPX = 320;
const TEMPLATE_FETCH_BATCH_SIZE = 500;

type MainTabValue = string;

type TemplateListItem = {
  id: number;
  image: string;
  imageWidth?: number;
  imageHeight?: number;
  mediaHeightRpx?: number;
  displayTitle: string;
  tags: string[];
  targetType: 'template' | 'inspiration';
  localDemo?: boolean;
  cardKey: string;
};

type MainTabItem = {
  label: string;
  value: MainTabValue;
};

type SubTabItem = {
  label: string;
  value: string;
  parent?: string;
  keywords?: string[];
};

type TemplateApiListItem = {
  id?: number | string;
  name?: string;
  category?: string;
  sub_tab?: string;
  third_tab?: string;
  main_tab?: string;
  description?: string;
  thumbnail?: string;
  preview_url?: string;
  image_width?: number | string;
  image_height?: number | string;
};

type InspirationApiListItem = {
  id?: number | string;
  title?: string;
  description?: string;
  cover_image?: string;
  images?: string[];
  image_width?: number | string;
  image_height?: number | string;
  tags?: string[];
  scene?: string;
  style?: string;
  topic?: string;
};

type ApiListResponseData<T> = {
  list?: T[];
  total?: number;
};

type IndexTapEvent = {
  currentTarget: {
    dataset: {
      index?: number | string;
    };
  };
};

type InputEvent = {
  detail: {
    value?: string;
  };
};

type CardTapEvent = {
  currentTarget: {
    dataset: {
      id?: number | string;
      targetType?: string;
      localDemo?: boolean | string;
    };
  };
};

const TEMPLATE_DETAIL_CACHE_TTL = 2 * 60 * 1000;
const detailPrefetchingKeys = new Set<string>();

const AI_TOOLS_MAIN_TAB: MainTabItem = { label: '工具', value: 'ai_tools' };
const NEW_MINNAN_TOOL_MAIN_TAB: MainTabItem = { label: '新闽派工具', value: 'new_minnan_tool' };

const TEMPLATE_MAIN_TABS: MainTabItem[] = [
  { label: '场景', value: 'scene' },
  { label: '风格', value: 'style' },
  { label: '灵感', value: 'inspiration' },
  NEW_MINNAN_TOOL_MAIN_TAB,
  AI_TOOLS_MAIN_TAB,
];

const TEMPLATE_SUB_TAB_MAP: Record<MainTabValue, SubTabItem[]> = {
  scene: [
    { label: '乡墅外观', value: 'villa_exterior', parent: 'scene', keywords: ['乡墅', '别墅', '外观', '外立面', '门头', '立面'] },
    { label: '室内空间', value: 'interior_space', parent: 'scene', keywords: ['室内', '空间', '客厅', '卧室', '餐厅', '书房', '茶室', '门厅', '玄关'] },
    { label: '花园庭院', value: 'garden_courtyard', parent: 'scene', keywords: ['花园', '庭院', '景观', '院子', '露台', '庭景'] },
    { label: '改造翻新', value: 'renovation', parent: 'scene', keywords: ['改造', '翻新', '更新', '焕新', '旧房', '旧改'] },
    { label: '商业空间', value: 'commercial_space', parent: 'scene', keywords: ['商业', '民宿', '酒店', '餐饮', '咖啡', '展厅', '办公', '会所', '店铺'] },
    { label: '设计辅助', value: 'design_assist', parent: 'scene', keywords: ['设计辅助', '辅助', '分析', '方案', '提示词', '草图', '布局', '概念'] },
  ],
  style: [
    { label: '新闽派', value: 'new_minnan', parent: 'style', keywords: ['新闽派', '闽南', '闽式', '红砖'] },
    { label: '新中式', value: 'new_chinese', parent: 'style', keywords: ['新中式', '中式', '东方'] },
    { label: '现代风格', value: 'modern', parent: 'style', keywords: ['现代', '极简', '奶油', '侘寂', '原木', '简约'] },
    { label: '经典欧式', value: 'classic_european', parent: 'style', keywords: ['欧式', '法式', '复古', '古典'] },
    { label: '地域特色', value: 'regional', parent: 'style', keywords: ['地域', '在地', '地方', '本土', '民俗', '文化特色'] },
  ],
  inspiration: [
    { label: '乡建趋势', value: 'rural_trend', parent: 'inspiration', keywords: ['乡建', '趋势', '乡村振兴', '乡村建设'] },
    { label: '生活方式', value: 'lifestyle', parent: 'inspiration', keywords: ['生活方式', '适老', '养老', '长者', '老年', '无障碍', '儿童', '亲子', '成长', '陪伴', '家庭互动'] },
    { label: '地域文化', value: 'regional_culture', parent: 'inspiration', keywords: ['地域文化', '文化', '非遗', '在地'] },
    { label: '功能创新', value: 'function_innovation', parent: 'inspiration', keywords: ['功能创新', '创新', '收纳', '模块', '多功能'] },
    { label: '案例精选', value: 'selected_cases', parent: 'inspiration', keywords: ['案例', '精选', '合集', '样板', '实景'] },
  ],
};

function normalizeMainTabs(list: any): MainTabItem[] {
  const normalized = (Array.isArray(list) ? list : [])
    .map((item) => ({
      label: String(item?.label || '').trim(),
      value: String(item?.value || '').trim(),
    }))
    .filter((item) => item.label && item.value);
  const result = normalized.length ? normalized : TEMPLATE_MAIN_TABS.slice();
  const hasAIToolsTab = result.some((item) => item.value === AI_TOOLS_MAIN_TAB.value);
  if (!hasAIToolsTab) {
    const inspirationIndex = result.findIndex((item) => item.value === 'inspiration');
    if (inspirationIndex >= 0) {
      result.splice(inspirationIndex + 1, 0, AI_TOOLS_MAIN_TAB);
    } else {
      result.push(AI_TOOLS_MAIN_TAB);
    }
  }
  const hasNewMinnanToolTab = result.some((item) => item.value === NEW_MINNAN_TOOL_MAIN_TAB.value || item.label === NEW_MINNAN_TOOL_MAIN_TAB.label);
  if (!hasNewMinnanToolTab) {
    const aiToolsIndex = result.findIndex((item) => item.value === AI_TOOLS_MAIN_TAB.value);
    if (aiToolsIndex >= 0) {
      result.splice(aiToolsIndex, 0, NEW_MINNAN_TOOL_MAIN_TAB);
    } else {
      result.push(NEW_MINNAN_TOOL_MAIN_TAB);
    }
  }
  return result;
}

function normalizeSubTabs(list: any, mainTabs: MainTabItem[]): SubTabItem[] {
  const mainValues = new Set(mainTabs.map((item) => item.value));
  return (Array.isArray(list) ? list : [])
    .map((item) => ({
      label: String(item?.label || '').trim(),
      value: String(item?.value || '').trim(),
      parent: String(item?.parent || '').trim(),
    }))
    .filter((item) => item.label && item.value && item.parent && mainValues.has(item.parent));
}

function truncate(str: string, maxLen: number): string {
  if (!str || typeof str !== 'string') return '';
  const cleanText = str.replace(/\s+/g, ' ').trim();
  if (cleanText.length <= maxLen) return cleanText;

  return `${cleanText.slice(0, maxLen)}...`;
}

function inferStyleTag(name: string, fallback: string): string {
  if (name.includes('闽')) return '新闽派';
  if (name.includes('欧')) return '欧式';
  if (name.includes('中式')) return '新中式';
  if (name.includes('现代')) return '现代';
  return fallback;
}

function cleanDescription(description: string): string {
  return String(description || '')
    .replace(/提示词[:：][\s\S]*/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

function normalizeImageUrl(url: string): string {
  const cleanUrl = String(url || '').trim();
  if (!cleanUrl) {
    return resolveAssetPath('/assets/images/home.jpg');
  }
  if (/^https?:\/\//i.test(cleanUrl)) {
    return normalizeCosUrl(cleanUrl);
  }
  if (cleanUrl.startsWith('//')) {
    return normalizeCosUrl(`https:${cleanUrl}`);
  }
  if (cleanUrl.startsWith('/')) {
    return `${API_BASE_URL}${cleanUrl}`;
  }
  return `${API_BASE_URL}/${cleanUrl}`;
}

function normalizePositiveNumber(value: unknown): number {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) {
    return 0;
  }
  return Math.round(num);
}

function computeCardMediaHeightRpx(width?: number, height?: number): number {
  const normalizedWidth = normalizePositiveNumber(width);
  const normalizedHeight = normalizePositiveNumber(height);
  if (!normalizedWidth || !normalizedHeight) {
    return DEFAULT_CARD_MEDIA_HEIGHT_RPX;
  }
  return Math.max(1, Math.round(TEMPLATE_CARD_COLUMN_WIDTH_RPX * normalizedHeight / normalizedWidth));
}

function withCardImageSize(card: TemplateListItem, width?: number, height?: number): TemplateListItem {
  const imageWidth = normalizePositiveNumber(width || card.imageWidth);
  const imageHeight = normalizePositiveNumber(height || card.imageHeight);
  return {
    ...card,
    imageWidth: imageWidth || undefined,
    imageHeight: imageHeight || undefined,
    mediaHeightRpx: computeCardMediaHeightRpx(imageWidth, imageHeight),
  };
}

function requestListApi<T>(path: string, data?: Record<string, unknown>): Promise<ApiListResponseData<T> | null> {
  return new Promise((resolve) => {
    wx.request({
      url: `${API_BASE_URL}${path}`,
      method: 'GET',
      data,
      success: (res) => {
        const response = (res.data || {}) as { code?: number; data?: ApiListResponseData<T> };
        if (res.statusCode !== 200 || !res.data || response.code !== 0) {
          resolve(null);
          return;
        }
        resolve(response.data && Array.isArray(response.data.list) ? response.data : null);
      },
      fail: () => {
        resolve(null);
      },
    });
  });
}

function requestTabConfig(parent?: string): Promise<{ main_tabs?: MainTabItem[]; sub_tabs?: SubTabItem[]; third_tabs?: SubTabItem[] } | null> {
  return new Promise((resolve) => {
    wx.request({
      url: `${API_BASE_URL}/api/v1/miniprogram/templates/tab-config`,
      method: 'GET',
      data: parent ? { parent } : undefined,
      success: (res) => {
        const response = (res.data || {}) as { code?: number; data?: { main_tabs?: MainTabItem[]; sub_tabs?: SubTabItem[]; third_tabs?: SubTabItem[] } };
        if (res.statusCode !== 200 || !res.data || response.code !== 0) {
          resolve(null);
          return;
        }
        resolve(response.data || {});
      },
      fail: () => {
        resolve(null);
      },
    });
  });
}

function ensureAllTabPrefix(tabs: SubTabItem[], parent: string): SubTabItem[] {
  const hasAll = tabs.some((item) => !String(item.value || '').trim());
  if (hasAll) {
    return tabs;
  }
  return [{ label: '全部', value: '', parent }, ...tabs];
}

function getSubTabsByMainTab(mainTabValue: MainTabValue, allSubTabs?: SubTabItem[]): SubTabItem[] {
  const currentParent = String(mainTabValue || '').trim();
  if (!currentParent || !Array.isArray(allSubTabs) || allSubTabs.length === 0) {
    return [];
  }
  const matched = allSubTabs.filter((item) => String(item.parent || '').trim() === currentParent);
  if (!matched.length) {
    return [];
  }
  return ensureAllTabPrefix(matched, currentParent);
}

function normalizeThirdTabs(list: any, subTabs: SubTabItem[]): SubTabItem[] {
  const subValues = new Set(subTabs.map((item) => item.value));
  return (Array.isArray(list) ? list : [])
    .map((item) => ({
      label: String(item?.label || '').trim(),
      value: String(item?.value || '').trim(),
      parent: String(item?.parent || '').trim(),
    }))
    .filter((item) => item.label && item.value && item.parent && subValues.has(item.parent));
}

function getThirdTabsBySubTab(subTabValue: string, allThirdTabs?: SubTabItem[]): SubTabItem[] {
  const currentParent = String(subTabValue || '').trim();
  if (!currentParent || !Array.isArray(allThirdTabs) || allThirdTabs.length === 0) {
    return [];
  }
  const matched = allThirdTabs.filter((item) => String(item.parent || '').trim() === currentParent);
  if (!matched.length) {
    return [];
  }
  return [{ label: '全部', value: '', parent: currentParent }, ...matched];
}

function getTemplateFilterText(item: TemplateApiListItem): string {
  return [
    String(item?.name || ''),
    String(item?.category || ''),
    String(item?.sub_tab || ''),
    String(item?.third_tab || ''),
    String(item?.main_tab || ''),
    cleanDescription(String(item?.description || '')),
  ].join(' ').toLowerCase();
}

function getInspirationFilterText(item: InspirationApiListItem): string {
  const tags = Array.isArray(item?.tags) ? item.tags.join(' ') : '';
  return [
    String(item?.title || ''),
    cleanDescription(String(item?.description || '')),
    String(item?.scene || ''),
    String(item?.style || ''),
    String(item?.topic || ''),
    tags,
  ].join(' ').toLowerCase();
}

function matchesMainTabKeywords(text: string, mainTab: MainTabItem): boolean {
  const keywordMap: Record<string, string[]> = {
    scene: ['scene', '场景', '室内', '庭院', '花园', '改造', '商业', '设计辅助'],
    style: ['style', '风格', '闽', '中式', '现代', '欧式', '法式', '地域'],
    inspiration: ['inspiration', '灵感', '趋势', '生活方式', '文化', '创新', '案例'],
  };

  const baseKeywords = keywordMap[String(mainTab.value || '').trim()] || [];
  const dynamicKeywords = [String(mainTab.label || '').trim(), String(mainTab.value || '').trim()].filter(Boolean);
  return [...baseKeywords, ...dynamicKeywords].some((keyword) => text.includes(keyword.toLowerCase()));
}

function matchesSubTabKeywords(text: string, subTab?: SubTabItem): boolean {
  if (!subTab || !String(subTab.value || '').trim()) {
    return true;
  }
  const keywords = (Array.isArray(subTab.keywords) ? subTab.keywords : []).concat([subTab.label, subTab.value]);
  return keywords.some((keyword) => String(keyword || '').trim() && text.includes(String(keyword).toLowerCase()));
}

function matchesThirdTab(item: TemplateApiListItem, thirdTab?: SubTabItem): boolean {
  if (!thirdTab || !String(thirdTab.value || '').trim()) {
    return true;
  }
  return String(item?.third_tab || '').trim() === String(thirdTab.value || '').trim();
}

void TEMPLATE_SUB_TAB_MAP;
void getTemplateFilterText;
void matchesMainTabKeywords;
void matchesThirdTab;

function isInspirationMainTab(mainTab: MainTabItem | undefined): boolean {
  if (!mainTab) {
    return false;
  }
  const value = String(mainTab.value || '').trim().toLowerCase();
  const label = String(mainTab.label || '').trim();
  return value === 'inspiration' || label === '灵感';
}

function buildTags(item: TemplateApiListItem, currentMainLabel: string, currentSubLabel: string): string[] {
  const name = String(item?.name || '').trim();
  const category = String(item?.category || '').trim();
  const subTab = String(item?.sub_tab || '').trim();
  const styleTag = inferStyleTag(name, '');
  return [currentMainLabel, currentSubLabel, styleTag, category, subTab]
    .map((tag) => truncate(String(tag || '').trim(), 6))
    .filter((tag, index, list) => Boolean(tag) && tag !== '全部' && list.indexOf(tag) === index)
    .slice(0, 3);
}

function buildInspirationTags(item: InspirationApiListItem, currentSubLabel: string): string[] {
  const tags = Array.isArray(item?.tags) ? item.tags : [];
  return [currentSubLabel, String(item?.scene || ''), String(item?.style || ''), ...tags]
    .map((tag) => truncate(String(tag || '').trim(), 6))
    .filter((tag, index, list) => Boolean(tag) && tag !== '全部' && list.indexOf(tag) === index)
    .slice(0, 4);
}

function buildEmptyTip(mainLabel: string, subLabel = ''): string {
  const suffix = mainLabel === '灵感' ? '暂无灵感内容' : '暂无模板内容';
  return (!subLabel || subLabel === '全部') ? `${mainLabel} ${suffix}` : `${subLabel} ${suffix}`;
}

function buildSearchEmptyTip(mainLabel: string, keyword: string): string {
  return mainLabel === '灵感' ? `未找到与“${keyword}”相关的灵感` : `未找到与“${keyword}”相关的模板`;
}

function buildSummaryText(mainLabel: string, subLabel: string, keyword: string): string {
  if (keyword) {
    return `搜索“${keyword}” · ${subLabel === '全部' ? mainLabel : subLabel}`;
  }
  return subLabel === '全部' ? mainLabel : `${mainLabel} · ${subLabel}`;
}

function buildTemplateCard(item: TemplateApiListItem, currentMainLabel: string, currentSubLabel: string, index: number): TemplateListItem {
  const title = String(item?.name || `模板方案 ${index + 1}`);
  const id = Number(item?.id) || index + 1;
  return withCardImageSize({
    id,
    image: normalizeImageUrl(String(item?.thumbnail || item?.preview_url || '')),
    displayTitle: truncate(title, 14),
    tags: buildTags(item, currentMainLabel, currentSubLabel),
    targetType: 'template',
    cardKey: `template-${id}-${index}`,
    mediaHeightRpx: DEFAULT_CARD_MEDIA_HEIGHT_RPX,
  }, Number(item?.image_width || 0), Number(item?.image_height || 0));
}

function buildInspirationCard(item: InspirationApiListItem, currentSubLabel: string, index: number): TemplateListItem {
  const id = Number(item?.id) || index + 1;
  return withCardImageSize({
    id,
    image: normalizeImageUrl(String(item?.cover_image || item?.images?.[0] || '')),
    displayTitle: truncate(String(item?.title || `灵感内容 ${index + 1}`), 14),
    tags: buildInspirationTags(item, currentSubLabel),
    targetType: 'inspiration',
    cardKey: `inspiration-${id}-${index}`,
    mediaHeightRpx: DEFAULT_CARD_MEDIA_HEIGHT_RPX,
  }, Number(item?.image_width || 0), Number(item?.image_height || 0));
}

function paginateTemplateItems(items: TemplateApiListItem[], page: number, pageSize: number): TemplateApiListItem[] {
  const start = Math.max(page - 1, 0) * pageSize;
  return items.slice(start, start + pageSize);
}

function matchesTemplateSelection(item: TemplateApiListItem, mainTab: MainTabItem, subTab?: SubTabItem, thirdTab?: SubTabItem): boolean {
  const itemMainTab = String(item?.main_tab || '').trim();
  const itemSubTab = String(item?.sub_tab || '').trim();
  const itemThirdTab = String(item?.third_tab || '').trim();
  if (itemMainTab !== String(mainTab.value || '').trim()) {
    return false;
  }
  if (subTab && String(subTab.value || '').trim() && itemSubTab !== String(subTab.value || '').trim()) {
    return false;
  }
  if (thirdTab && String(thirdTab.value || '').trim() && itemThirdTab !== String(thirdTab.value || '').trim()) {
    return false;
  }
  return true;
}

async function requestTemplateListByMainTab(mainTab: MainTabItem, subTab: SubTabItem | undefined, thirdTab: SubTabItem | undefined, keyword: string, page: number, pageSize: number): Promise<{ list: TemplateApiListItem[]; hasMore: boolean } | null> {
  if (keyword) {
    const result = await requestListApi<TemplateApiListItem>('/api/v1/miniprogram/templates/search', {
      keyword,
      page: 1,
      page_size: TEMPLATE_FETCH_BATCH_SIZE,
    });
    if (!result) {
      return null;
    }
    const filteredList = (Array.isArray(result.list) ? result.list : []).filter((item: TemplateApiListItem) => matchesTemplateSelection(item, mainTab, subTab, thirdTab));
    const list = paginateTemplateItems(filteredList, page, pageSize);
    return {
      list,
      hasMore: filteredList.length > page * pageSize,
    };
  }

  const result = await requestListApi<TemplateApiListItem>('/api/v1/miniprogram/templates', {
    main_tab: mainTab.value,
    page: 1,
    page_size: TEMPLATE_FETCH_BATCH_SIZE,
  });
  if (!result) {
    return null;
  }
  const filteredList = (Array.isArray(result.list) ? result.list : []).filter((item: TemplateApiListItem) => matchesTemplateSelection(item, mainTab, subTab, thirdTab));
  return {
    list: paginateTemplateItems(filteredList, page, pageSize),
    hasMore: filteredList.length > page * pageSize,
  };
}

async function requestInspirationListBySubTab(keyword: string, page: number, pageSize: number, subTab?: SubTabItem): Promise<{ list: InspirationApiListItem[]; hasMore: boolean } | null> {
  const result = await requestListApi<InspirationApiListItem>('/api/v1/miniprogram/inspirations', {
    keyword,
    page,
    page_size: pageSize,
  });
  if (!result) {
    return null;
  }
  const list = (Array.isArray(result.list) ? result.list : []).filter((item: InspirationApiListItem) => matchesSubTabKeywords(getInspirationFilterText(item), subTab));
  return {
    list,
    hasMore: Number(result.total || 0) > page * pageSize || list.length >= pageSize,
  };
}

function splitWaterfallColumns(cards: TemplateListItem[]) {
  const leftColumnCards: TemplateListItem[] = [];
  const rightColumnCards: TemplateListItem[] = [];
  let leftWeight = 0;
  let rightWeight = 0;

  cards.forEach((item, index) => {
    const weight = Number(item.mediaHeightRpx || DEFAULT_CARD_MEDIA_HEIGHT_RPX) + 18 + item.displayTitle.length * 1.6 + item.tags.length * 4 + (index % 3) * 8;
    if (leftWeight <= rightWeight) {
      leftColumnCards.push(item);
      leftWeight += weight;

      return;
    }
    rightColumnCards.push(item);
    rightWeight += weight;
  });

  return {
    leftColumnCards,
    rightColumnCards,
  };
}

function getNavLayout() {
  try {
    const systemInfo = wx.getSystemInfoSync();
    const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function' ? wx.getMenuButtonBoundingClientRect() : null;
    const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);

    if (menuRect) {
      return {
        navSafeTop: safeTop,
        navBarHeight: Number(menuRect.bottom + menuRect.top - safeTop),
        navContentHeight: Number(menuRect.height),
        navSideWidth: Number(systemInfo.windowWidth - menuRect.left),
      };
    }

    return {
      navSafeTop: 20,
      navBarHeight: 64,
      navContentHeight: 44,
      navSideWidth: 96,
    };
  } catch (error) {
    return {
      navSafeTop: 20,
      navBarHeight: 64,
      navContentHeight: 44,
      navSideWidth: 96,
    };
  }
}

function buildTemplateCacheKey(mainTabValue: string, subTabValue: string, thirdTabValue: string, keyword: string) {
  return [String(mainTabValue || '').trim(), String(subTabValue || '').trim(), String(thirdTabValue || '').trim(), String(keyword || '').trim().toLowerCase()].join('::');
}

function buildDetailCacheKey(targetType: 'template' | 'inspiration', id: number) {
  return `${targetType}-detail:${Number(id || 0)}`;
}

function parseTemplateImages(value: unknown): string[] {
  const rawValue = typeof value === 'string' ? value.trim() : '';
  if (!rawValue) {
    return [];
  }

  try {
    const parsed = JSON.parse(rawValue);
    if (!Array.isArray(parsed)) {
      return [];
    }

    const urls: string[] = [];
    parsed.forEach((item) => {
      if (typeof item === 'string' && item.trim()) {
        urls.push(item.trim());
        return;
      }
      if (item && typeof item === 'object') {
        const imageUrl = String((item as { image?: string; url?: string; preview_url?: string }).image || (item as { url?: string }).url || (item as { preview_url?: string }).preview_url || '').trim();
        if (imageUrl) {
          urls.push(imageUrl);
        }
      }
    });
    return urls;
  } catch (error) {
    return [];
  }
}

function getTemplatePrefetchImageCandidates(payload: any, fallbackImage = ''): string[] {
  return [
    String(payload?.preview_url || '').trim(),
    String(payload?.thumbnail || '').trim(),
    ...parseTemplateImages(payload?.images),
    String(fallbackImage || '').trim(),
  ].filter((item, index, list) => Boolean(item) && list.indexOf(item) === index);
}

function getInspirationPrefetchImageCandidates(payload: any, fallbackImage = ''): string[] {
  const images = Array.isArray(payload?.images) ? payload.images : [];
  return [
    String(payload?.cover_image || '').trim(),
    ...images.map((item: unknown) => String(item || '').trim()),
    String(fallbackImage || '').trim(),
  ].filter((item, index, list) => Boolean(item) && list.indexOf(item) === index);
}

Page({
  templateFirstPageCache: {} as Record<string, { cards: TemplateListItem[]; hasMore: boolean; nextPage: number }>,
  requestSerial: 0,
  imageSizeCache: {} as Record<string, { width: number; height: number }>,
  imageSizeLoadingKeys: new Set<string>(),

  data: {
    contentReady: false,
    navSafeTop: 0,
    navBarHeight: 96,
    navContentHeight: 44,
    navSideWidth: 88,
    mainTabs: [] as MainTabItem[],
    allSubTabs: [] as SubTabItem[],
    subTabs: [] as SubTabItem[],
    allThirdTabs: [] as SubTabItem[],
    thirdTabs: [] as SubTabItem[],
    currentMainTabIndex: -1,
    currentSubTabIndex: -1,
    currentThirdTabIndex: -1,
    searchInputValue: '',
    searchKeyword: '',
    resultSummaryText: buildSummaryText('场景', '乡墅外观', ''),
    displayCards: [] as TemplateListItem[],
    leftColumnCards: [] as TemplateListItem[],
    rightColumnCards: [] as TemplateListItem[],
    skeletonCards: [0, 1, 2, 3],
    loading: false,
    page: 1,
    pageSize: 12,
    hasMore: true,
    emptyTipText: buildEmptyTip('场景', '乡墅外观'),
  },

  syncTabBar() {
    const tabBar = typeof (this as any).getTabBar === 'function' ? (this as any).getTabBar() : null
    if (tabBar && typeof tabBar.setCurrent === 'function') {
      tabBar.setCurrent(1)
    }
  },

  onLoad() {
    this.setData(getNavLayout());
    this.initializePage();
  },

  onShow() {
    this.syncTabBar()
  },

  onReachBottom() {
    if (this.data.hasMore && !this.data.loading) {
      this.loadTemplates(false);
    }
  },

  onShareAppMessage() {
    return {
      title: '模板广场',
      path: '/pages/template/template',
    };
  },

  getCurrentMainTab(): MainTabItem {
    return this.data.mainTabs[this.data.currentMainTabIndex] || this.data.mainTabs[0] || TEMPLATE_MAIN_TABS[0];
  },

  getCurrentSubTab(): SubTabItem | undefined {
    return this.data.subTabs[this.data.currentSubTabIndex] || this.data.subTabs[0];
  },

  getCurrentThirdTab(): SubTabItem | undefined {
    return this.data.thirdTabs[this.data.currentThirdTabIndex];
  },

  syncWaterfallCards(cards: TemplateListItem[]) {
    const nextCards = this.applyKnownImageSizes(cards);
    const { leftColumnCards, rightColumnCards } = splitWaterfallColumns(nextCards);
    this.setData({
      displayCards: nextCards,
      leftColumnCards,
      rightColumnCards,
    });
    this.scheduleImageSizeProbe(nextCards);
  },

  applyKnownImageSizes(cards: TemplateListItem[]): TemplateListItem[] {
    return (Array.isArray(cards) ? cards : []).map((card) => {
      const cacheEntry = this.imageSizeCache[String(card?.image || '').trim()];
      if (cacheEntry) {
        return withCardImageSize(card, cacheEntry.width, cacheEntry.height);
      }
      return withCardImageSize(card);
    });
  },

  scheduleImageSizeProbe(cards: TemplateListItem[]) {
    (Array.isArray(cards) ? cards : []).forEach((card) => {
      this.probeCardImageSize(card);
    });
  },

  probeCardImageSize(card: TemplateListItem) {
    const imageUrl = String(card?.image || '').trim();
    if (!imageUrl || this.imageSizeCache[imageUrl] || this.imageSizeLoadingKeys.has(imageUrl)) {
      return;
    }
    this.imageSizeLoadingKeys.add(imageUrl);
    wx.getImageInfo({
      src: imageUrl,
      success: (res) => {
        const width = normalizePositiveNumber(res?.width);
        const height = normalizePositiveNumber(res?.height);
        if (!width || !height) {
          return;
        }
        this.imageSizeCache[imageUrl] = { width, height };
        const nextCards = this.applyKnownImageSizes(this.data.displayCards || []);
        const changed = nextCards.some((item, index) => Number(item.mediaHeightRpx || 0) !== Number(this.data.displayCards?.[index]?.mediaHeightRpx || 0));
        if (changed) {
          const { leftColumnCards, rightColumnCards } = splitWaterfallColumns(nextCards);
          this.setData({
            displayCards: nextCards,
            leftColumnCards,
            rightColumnCards,
          });
        }
      },
      complete: () => {
        this.imageSizeLoadingKeys.delete(imageUrl);
      },
    });
  },

  prefetchCardDetail(card: TemplateListItem) {
    if (!card || !Number(card.id) || (card.targetType !== 'template' && card.targetType !== 'inspiration')) {
      return;
    }

    const targetType = card.targetType === 'inspiration' ? 'inspiration' : 'template';
    const cacheKey = buildDetailCacheKey(targetType, Number(card.id));
    if (getPageCache(cacheKey) || detailPrefetchingKeys.has(cacheKey)) {
      return;
    }

    detailPrefetchingKeys.add(cacheKey);
    wx.request({
      url: targetType === 'template'
        ? `${API_BASE_URL}/api/v1/miniprogram/templates/${Number(card.id)}`
        : `${API_BASE_URL}/api/v1/miniprogram/inspirations/${Number(card.id)}`,
      method: 'GET',
      success: (res) => {
        const response = (res.data || {}) as { code?: number; data?: any };
        if (res.statusCode !== 200 || response.code !== 0 || !response.data) {
          return;
        }

        setPageCache(cacheKey, response.data, TEMPLATE_DETAIL_CACHE_TTL);
        const imageCandidates = targetType === 'template'
          ? getTemplatePrefetchImageCandidates(response.data, card.image)
          : getInspirationPrefetchImageCandidates(response.data, card.image);
        void prefetchImages(imageCandidates, 2);
      },
      complete: () => {
        detailPrefetchingKeys.delete(cacheKey);
      },
    });
  },

  warmLeadingCards(cards: TemplateListItem[], count = 2) {
    const nextCards = Array.isArray(cards) ? cards.filter((item) => item) : [];
    if (!nextCards.length) {
      return;
    }

    void prefetchImages(nextCards.map((item) => item.image), count);
    nextCards.slice(0, count).forEach((item) => this.prefetchCardDetail(item));
  },

  async initializePage() {
    const tabConfig = await requestTabConfig();
    const mainTabs = normalizeMainTabs(tabConfig?.main_tabs);
    if (!mainTabs.length) {
      this.syncWaterfallCards([]);
      this.setData({
        mainTabs: [],
        allSubTabs: [],
        allThirdTabs: [],
        subTabs: [],
        thirdTabs: [],
        currentMainTabIndex: -1,
        currentSubTabIndex: -1,
        currentThirdTabIndex: -1,
        hasMore: false,
        resultSummaryText: '模板广场分类配置不可用',
        emptyTipText: '请先在管理端配置一级、二级、三级标签',
      });
      return;
    }
    const allSubTabs = normalizeSubTabs(tabConfig?.sub_tabs, mainTabs);
    const allThirdTabs = normalizeThirdTabs(tabConfig?.third_tabs, allSubTabs);
    const firstMain = mainTabs[0];
    const subTabs = getSubTabsByMainTab(firstMain?.value || '', allSubTabs);
    const firstSub = subTabs[0];
    const thirdTabs = getThirdTabsBySubTab(firstSub?.value || '', allThirdTabs);
    this.setData({
      mainTabs,
      allSubTabs,
      allThirdTabs,
      subTabs,
      thirdTabs,
      currentMainTabIndex: 0,
      currentSubTabIndex: firstSub ? 0 : -1,
      currentThirdTabIndex: thirdTabs.length ? 0 : -1,
      resultSummaryText: buildSummaryText(firstMain?.label || '模板广场', firstSub?.label || firstMain?.label || '模板广场', ''),
      emptyTipText: buildEmptyTip(firstMain?.label || '模板广场', firstSub?.label || ''),
    });
    await this.loadTemplates(true);
  },

  async onMainTabTap(e: IndexTapEvent) {
    const index = Number(e.currentTarget.dataset.index);
    const tab = this.data.mainTabs[index];
    if (Number.isNaN(index) || index === this.data.currentMainTabIndex || !tab) {
      return;
    }

    if (tab.value === NEW_MINNAN_TOOL_MAIN_TAB.value || tab.label === NEW_MINNAN_TOOL_MAIN_TAB.label) {
      this.onNewMinnanToolTap();
      return;
    }

    if (tab.value === AI_TOOLS_MAIN_TAB.value) {
      wx.navigateTo({
        url: '/pages/aitools/aitools',
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          });
        },
      });
      return;
    }

    const nextSubTabs = getSubTabsByMainTab(tab.value, this.data.allSubTabs || []);
    const nextSubTab = nextSubTabs[0];
    const nextThirdTabs = getThirdTabsBySubTab(nextSubTab?.value || '', this.data.allThirdTabs || []);

    this.setData({
      currentMainTabIndex: index,
      subTabs: nextSubTabs,
      thirdTabs: nextThirdTabs,
      currentSubTabIndex: 0,
      currentThirdTabIndex: nextThirdTabs.length ? 0 : -1,
      page: 1,
      hasMore: true,
      resultSummaryText: buildSummaryText(tab.label, nextSubTab?.label || tab.label, this.data.searchKeyword),
      emptyTipText: buildEmptyTip(tab.label, nextSubTab?.label || ''),
    });

    this.loadTemplates(true);
  },

  async onSubTabTap(e: IndexTapEvent) {
    const index = Number(e.currentTarget.dataset.index);
    const subTab = this.data.subTabs[index];
    const currentMainTab = this.getCurrentMainTab();
    if (Number.isNaN(index) || index === this.data.currentSubTabIndex || !subTab) {
      return;
    }

    const nextThirdTabs = getThirdTabsBySubTab(subTab.value, this.data.allThirdTabs || []);

    this.setData({
      currentSubTabIndex: index,
      thirdTabs: nextThirdTabs,
      currentThirdTabIndex: nextThirdTabs.length ? 0 : -1,
      page: 1,
      hasMore: true,
      resultSummaryText: buildSummaryText(currentMainTab.label, subTab.label, this.data.searchKeyword),
      emptyTipText: buildEmptyTip(currentMainTab.label, subTab.label),
    });

    this.loadTemplates(true);
  },

  async onThirdTabTap(e: IndexTapEvent) {
    const index = Number(e.currentTarget.dataset.index);
    const thirdTab = this.data.thirdTabs[index];
    if (Number.isNaN(index) || index === this.data.currentThirdTabIndex || !thirdTab) {
      return;
    }

    this.setData({
      currentThirdTabIndex: index,
      page: 1,
      hasMore: true,
    });

    this.loadTemplates(true);
  },

  onNewMinnanToolTap() {
    wx.navigateTo({
      url: '/pages/newminnantool/newminnantool?code=new_minnan_tool',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  onSearchInput(e: InputEvent) {
    this.setData({
      searchInputValue: String(e.detail.value || ''),
    });
  },

  onSearchConfirm() {
    const keyword = String(this.data.searchInputValue || '').trim();
    const currentMainTab = this.getCurrentMainTab();
    if (!currentMainTab) {
      this.syncWaterfallCards([]);
      this.setData({
        loading: false,
        contentReady: false,
        hasMore: false,
        resultSummaryText: '模板广场分类配置不可用',
        emptyTipText: '请先在管理端配置一级、二级、三级标签',
      });
      return;
    }
    const currentSubTab = this.getCurrentSubTab();
    const currentThirdTab = this.getCurrentThirdTab();

    this.setData({
      searchKeyword: keyword,
      page: 1,
      hasMore: true,
      resultSummaryText: buildSummaryText(currentMainTab.label, (currentThirdTab && currentThirdTab.value ? currentThirdTab.label : '') || currentSubTab?.label || currentMainTab.label, keyword),
      emptyTipText: keyword ? buildSearchEmptyTip(currentMainTab.label, keyword) : buildEmptyTip(currentMainTab.label, currentSubTab?.label || ''),
    });

    this.loadTemplates(true);
  },

  onSearchClear() {
    const currentMainTab = this.getCurrentMainTab();
    if (!currentMainTab) {
      return;
    }
    const currentSubTab = this.getCurrentSubTab();
    const currentThirdTab = this.getCurrentThirdTab();
    if (!this.data.searchKeyword && !this.data.searchInputValue) {
      return;
    }

    this.setData({
      searchInputValue: '',
      searchKeyword: '',
      page: 1,
      hasMore: true,
      resultSummaryText: buildSummaryText(currentMainTab.label, (currentThirdTab && currentThirdTab.value ? currentThirdTab.label : '') || currentSubTab?.label || currentMainTab.label, ''),
      emptyTipText: buildEmptyTip(currentMainTab.label, currentSubTab?.label || ''),
    });

    this.loadTemplates(true);
  },

  async loadTemplates(reset: boolean) {
    if (this.data.loading) {
      return;
    }

    const currentMainTab = this.getCurrentMainTab();
    const currentSubTab = this.getCurrentSubTab();
    const currentThirdTab = this.getCurrentThirdTab();
    const currentMainLabel = currentMainTab.label;
    const currentSubLabel = currentSubTab?.label || currentMainLabel;
    const currentThirdLabel = currentThirdTab && String(currentThirdTab.value || '').trim() ? currentThirdTab.label : '';
    const keyword = String(this.data.searchKeyword || '').trim();
    const requestPage = reset ? 1 : this.data.page;
    const currentCacheKey = buildTemplateCacheKey(currentMainTab?.value || '', currentSubTab?.value || '', currentThirdTab?.value || '', keyword);
    const requestSerial = ++this.requestSerial;

    if (reset) {
      const cachedEntry = this.templateFirstPageCache[currentCacheKey];
      if (cachedEntry && Array.isArray(cachedEntry.cards) && cachedEntry.cards.length > 0) {
        this.syncWaterfallCards(cachedEntry.cards);
        this.warmLeadingCards(cachedEntry.cards, 2);
        this.setData({
          loading: false,
          contentReady: true,
          page: cachedEntry.nextPage,
          hasMore: cachedEntry.hasMore,
          resultSummaryText: buildSummaryText(currentMainLabel, currentThirdLabel || currentSubLabel, keyword),
          emptyTipText: keyword ? buildSearchEmptyTip(currentMainLabel, keyword) : buildEmptyTip(currentMainLabel, currentSubLabel),
        });
        return;
      }
    }

    this.setData({
      loading: true,
      contentReady: false,
      resultSummaryText: buildSummaryText(currentMainLabel, currentThirdLabel || currentSubLabel, keyword),
      emptyTipText: keyword ? buildSearchEmptyTip(currentMainLabel, keyword) : buildEmptyTip(currentMainLabel, currentSubLabel),
    });

    let mappedList: TemplateListItem[] = [];
    let nextHasMore = false;

    if (isInspirationMainTab(currentMainTab)) {
      const templateResult = await requestTemplateListByMainTab(currentMainTab, currentSubTab, currentThirdTab, keyword, requestPage, this.data.pageSize);

      if (templateResult && templateResult.list.length > 0) {
        mappedList = templateResult.list.map((item: TemplateApiListItem, index: number) => buildTemplateCard(item, currentMainLabel, currentSubLabel, index));
        nextHasMore = templateResult.hasMore;
      } else {
        const inspirationResult = await requestInspirationListBySubTab(keyword, requestPage, this.data.pageSize, currentSubTab);
        if (!templateResult && !inspirationResult) {
          if (reset) {
            this.syncWaterfallCards([]);
          }
          this.setData({
            loading: false,
            contentReady: false,
            hasMore: false,
            resultSummaryText: '模板或灵感接口暂时不可用',
            emptyTipText: '加载失败，请稍后重试',
          });
          return;
        }

        if (inspirationResult && inspirationResult.list.length > 0) {
          mappedList = inspirationResult.list.map((item: InspirationApiListItem, index: number) => buildInspirationCard(item, currentSubLabel, index));
          nextHasMore = inspirationResult.hasMore;
        } else {
          mappedList = [];
          nextHasMore = false;
        }
      }
    } else {
      const result = await requestTemplateListByMainTab(currentMainTab, currentSubTab, currentThirdTab, keyword, requestPage, this.data.pageSize);
      if (!result) {
        if (reset) {
          this.syncWaterfallCards([]);
        }
        this.setData({
          loading: false,
          contentReady: false,
          hasMore: false,
          resultSummaryText: '模板接口暂时不可用',
          emptyTipText: '加载失败，请稍后重试',
        });
        return;
      }

      mappedList = result.list.map((item: TemplateApiListItem, index: number) => buildTemplateCard(item, currentMainLabel, currentSubLabel, index));
      nextHasMore = result.hasMore;
    }

    if (requestSerial !== this.requestSerial) {
      return;
    }

    const nextCards = reset ? mappedList : [...this.data.displayCards, ...mappedList];
    this.syncWaterfallCards(nextCards);
    this.warmLeadingCards(reset ? nextCards : mappedList, reset ? 2 : 1);

    if (reset) {
      this.templateFirstPageCache[currentCacheKey] = {
        cards: nextCards,
        hasMore: nextHasMore,
        nextPage: requestPage + 1,
      };
    }

    this.setData({
      loading: false,
      contentReady: true,
      page: requestPage + 1,
      hasMore: nextHasMore,
      resultSummaryText: buildSummaryText(currentMainLabel, currentThirdLabel || currentSubLabel, keyword),
      emptyTipText: keyword ? buildSearchEmptyTip(currentMainLabel, keyword) : buildEmptyTip(currentMainLabel, currentSubLabel),
    });
  },

  onCardTap(e: CardTapEvent) {
    const id = Number(e.currentTarget.dataset.id);
    const targetType = String(e.currentTarget.dataset.targetType || 'template');
    const localDemo = false;
    const targetCard = (this.data.displayCards || []).find((item) => Number(item.id) === id && item.targetType === targetType);

    if (localDemo) {
      wx.showToast({
        title: '当前为本地演示卡片，请等待模板接口恢复',
        icon: 'none',
      });
      return;
    }

    const detailUrl = targetType === 'inspiration'
      ? `/pages/inspirationdetail/inspirationdetail?id=${id}`
      : `/pages/templatesquaredetails/templatesquaredetails?id=${id}`;

    if (targetCard) {
      this.prefetchCardDetail(targetCard);
    }

    wx.navigateTo({
      url: detailUrl,
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        });
      },
    });
  },

  onTabSwitch() {},
})
