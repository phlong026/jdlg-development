import { AIToolCategory, AIToolItem, AIToolPresetReference, AIToolStylePreset, getCategoryLabel } from './aiTools'

const API_BASE_URL = 'https://api.jiadilingguang.com'

type RawAIToolPresetReference = {
  id?: string | number
  name?: string
  description?: string
  image_url?: string
  prompt_suffix?: string
}

type RawAIToolStylePreset = {
  id?: string | number
  name?: string
  reference_mode?: 'preview_only' | 'preview_and_model' | string
  preview_image_url?: string
  model_reference_image_url?: string
  image_url?: string
  prompt_suffix?: string
}

type RawAIToolItem = {
  id?: string | number
  code?: string
  name?: string
  category?: string
  short_description?: string
  detail_description?: string
  list_cover_image?: string
  detail_before_image?: string
  detail_after_image?: string
  prompt_placeholder?: string
  default_prompt?: string
  upload_hint?: string
  show_usage_tips?: boolean
  usage_tips_title?: string
  usage_tips_content?: string
  sort_order?: number
  is_common?: boolean
  is_published?: boolean
  usage_count?: number
  preset_references?: RawAIToolPresetReference[]
  style_presets?: RawAIToolStylePreset[]
  tags?: string[]
}

type ToolListResponse = {
  list?: RawAIToolItem[]
  total?: number
  page?: number
  page_size?: number
}

export type AIToolListResult = {
  list: AIToolItem[]
  total: number
  page: number
  pageSize: number
}

function normalizeCategory(category: string): AIToolCategory {
  if (category === 'interior' || category === 'landscape' || category === 'planning') {
    return category
  }
  return 'architecture'
}

function normalizePresetReferences(list: RawAIToolPresetReference[] | undefined): AIToolPresetReference[] {
  return (list || []).map((item, index) => ({
    id: String(item.id || `preset-${index}`),
    name: String(item.name || '预设参考图'),
    description: String(item.description || ''),
    imageUrl: String(item.image_url || ''),
    promptSuffix: String(item.prompt_suffix || ''),
  }))
}

function normalizeStylePresets(list: RawAIToolStylePreset[] | undefined): AIToolStylePreset[] {
  return (list || []).map((item, index) => {
    const legacyImageUrl = String(item.image_url || '')
    const referenceMode = item.reference_mode === 'preview_only' || (!item.reference_mode && !legacyImageUrl && !item.model_reference_image_url)
      ? 'preview_only'
      : 'preview_and_model'
    const previewImageUrl = String(item.preview_image_url || legacyImageUrl)
    const modelReferenceImageUrl = referenceMode === 'preview_and_model'
      ? String(item.model_reference_image_url || legacyImageUrl)
      : ''
    return {
      id: String(item.id || `style-${index}`),
      name: String(item.name || '默认风格'),
      referenceMode,
      previewImageUrl,
      modelReferenceImageUrl,
      imageUrl: previewImageUrl,
      promptSuffix: String(item.prompt_suffix || ''),
    }
  })
}

function pickFirstImage(...values: Array<string | undefined>): string {
  for (const value of values) {
    const normalized = String(value || '').trim()
    if (normalized) {
      return normalized
    }
  }
  return ''
}

export function mapAIToolFromResponse(raw: RawAIToolItem): AIToolItem {
  const category = normalizeCategory(String(raw.category || 'architecture'))
  const common = !!raw.is_common
  const listCoverImage = pickFirstImage(raw.list_cover_image, raw.detail_after_image, raw.detail_before_image)
  const detailBeforeImage = pickFirstImage(raw.detail_before_image, raw.list_cover_image)
  const detailAfterImage = pickFirstImage(raw.detail_after_image, raw.list_cover_image)
  const tags = [getCategoryLabel(category), ...(common ? ['常用'] : [])]

  return {
    id: String(raw.id || ''),
    code: String(raw.code || ''),
    name: String(raw.name || ''),
    category,
    shortDescription: String(raw.short_description || ''),
    detailDescription: String(raw.detail_description || ''),
    common,
    promptPlaceholder: String(raw.prompt_placeholder || ''),
    uploadHint: String(raw.upload_hint || ''),
    tags,
    presetReferences: normalizePresetReferences(raw.preset_references),
    stylePresets: normalizeStylePresets(raw.style_presets),
    listCoverImage,
    detailBeforeImage,
    detailAfterImage,
    showUsageTips: raw.show_usage_tips !== false,
    usageTipsTitle: String(raw.usage_tips_title || '使用提示'),
    usageTipsContent: String(raw.usage_tips_content || ''),
    sortOrder: Number(raw.sort_order || 0),
    usageCount: Number(raw.usage_count || 0),
    published: raw.is_published !== false,
  }
}

function request<T>(url: string): Promise<T> {
  return new Promise((resolve, reject) => {
    wx.request({
      url,
      method: 'GET',
      success: (res) => {
        if (res.statusCode !== 200 || !res.data) {
          reject(new Error(`请求失败: ${res.statusCode}`))
          return
        }
        const response = res.data as any
        if (response.code !== 0) {
          reject(new Error(response.msg || '请求失败'))
          return
        }
        resolve(response.data as T)
      },
      fail: reject,
    })
  })
}

export async function fetchAIToolList(params?: { category?: string; keyword?: string; page?: number; pageSize?: number }): Promise<AIToolListResult> {
  const query = [
    params?.category ? `category=${encodeURIComponent(String(params.category))}` : '',
    params?.keyword ? `keyword=${encodeURIComponent(String(params.keyword))}` : '',
    `page=${encodeURIComponent(String(params?.page || 1))}`,
    `page_size=${encodeURIComponent(String(params?.pageSize || 100))}`,
  ].filter(Boolean).join('&')
  const data = await request<ToolListResponse>(`${API_BASE_URL}/api/v1/miniprogram/ai-tools${query ? `?${query}` : ''}`)
  const list = (data.list || []).map((item) => mapAIToolFromResponse(item))
  return {
    list,
    total: Number(data.total || list.length),
    page: Number(data.page || params?.page || 1),
    pageSize: Number(data.page_size || params?.pageSize || 100),
  }
}

export async function fetchAIToolDetail(id: string): Promise<AIToolItem> {
  const data = await request<RawAIToolItem>(`${API_BASE_URL}/api/v1/miniprogram/ai-tools/${encodeURIComponent(String(id || ''))}`)
  return mapAIToolFromResponse(data || {})
}

export async function fetchAIToolByCode(code: string): Promise<AIToolItem> {
  const data = await request<RawAIToolItem>(`${API_BASE_URL}/api/v1/miniprogram/ai-tools/code/${encodeURIComponent(String(code || ''))}`)
  return mapAIToolFromResponse(data || {})
}
