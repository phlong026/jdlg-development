import { generateRequestParams, paramsToHeaders } from '../../utils/parameter'
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint'
import { sanitizeAIGenerationErrorMessage } from '../../utils/aiError'
import { blockUnsafeText } from '../../utils/promptReview'
import { AIToolItem, AIToolPresetReference, getCategoryLabel } from '../../utils/aiTools'
import { fetchAIToolDetail } from '../../utils/aiToolApi'
import { shouldUseMinimalAIToolPresentation } from '../../utils/aiToolPresentation'

const API_BASE_URL = 'https://api.jiadilingguang.com'
const DEFAULT_SERVICE_TYPE = 'normal'
const DEFAULT_SERVICE = 'normal_style_change'
const DEFAULT_QUALITY = 'uhd'
const DEFAULT_CANVAS = '1:1'
const DEFAULT_GENERATE_COUNT = 1
const DEFAULT_DRAW_SINGLE_COST = 0
const DEFAULT_DRAW_MULTI_COST = 0

type PageOptions = {
  id?: string
}

type PresetTapEvent = {
  currentTarget: {
    dataset: {
      id?: string
      key?: string
    }
  }
}

type InputEvent = {
  detail: {
    value?: string
  }
}

type VisualStyleSource = 'style' | 'reference'

type VisualStyleOption = {
  key: string
  sourceType: VisualStyleSource
  sourceId: string
  name: string
  imageUrl: string
  modelReferenceImageUrl: string
  referenceMode: 'preview_only' | 'preview_and_model'
  modeLabel: string
  promptSuffix: string
}

function normalizeUploadedImageUrl(value: any): string {
  const text = String(value || '').trim()
  if (!text) {
    return ''
  }
  if (!text.startsWith('http://') && !text.startsWith('https://')) {
    return ''
  }
  return text
}

function normalizeToolId(value: any): number | string {
  const numericValue = Number(value)
  if (Number.isFinite(numericValue) && numericValue > 0) {
    return numericValue
  }
  return String(value || '').trim()
}

function buildVisualStyleOptions(tool: AIToolItem | null | undefined): VisualStyleOption[] {
  if (!tool) {
    return []
  }
  const presetReferences = Array.isArray(tool.presetReferences) ? tool.presetReferences : []
  const stylePresets = Array.isArray(tool.stylePresets) ? tool.stylePresets : []

  if (stylePresets.length > 0) {
    return stylePresets.map((stylePreset, index) => {
      const fallbackReference = presetReferences[index]
      const sourceId = String(stylePreset.id || '').trim()
      const fallbackName = String(fallbackReference?.name || '').trim()
      const name = String(stylePreset.name || fallbackName || `风格${index + 1}`).trim()
      const referenceMode: VisualStyleOption['referenceMode'] = stylePreset.referenceMode === 'preview_only' ? 'preview_only' : 'preview_and_model'
      const previewImageUrl = normalizeUploadedImageUrl(stylePreset.previewImageUrl || stylePreset.imageUrl || fallbackReference?.imageUrl || '')
      const modelReferenceImageUrl = referenceMode === 'preview_and_model'
        ? normalizeUploadedImageUrl(stylePreset.modelReferenceImageUrl || stylePreset.imageUrl || fallbackReference?.imageUrl || '')
        : ''
      return {
        key: `style:${sourceId || index}`,
        sourceType: 'style' as VisualStyleSource,
        sourceId: sourceId || `style-${index}`,
        name,
        imageUrl: previewImageUrl,
        modelReferenceImageUrl,
        referenceMode,
        modeLabel: modelReferenceImageUrl ? '图像参考' : '风格提示',
        promptSuffix: String(stylePreset.promptSuffix || fallbackReference?.promptSuffix || '').trim(),
      }
    }).filter((item) => item.name || item.imageUrl || item.promptSuffix)
  }

  return presetReferences.map((reference, index) => {
    const sourceId = String(reference.id || '').trim()
    return {
      key: `reference:${sourceId || index}`,
      sourceType: 'reference' as VisualStyleSource,
      sourceId: sourceId || `reference-${index}`,
      name: String(reference.name || `风格${index + 1}`).trim(),
      imageUrl: normalizeUploadedImageUrl(reference.imageUrl || ''),
      modelReferenceImageUrl: normalizeUploadedImageUrl(reference.imageUrl || ''),
      referenceMode: 'preview_and_model' as const,
      modeLabel: normalizeUploadedImageUrl(reference.imageUrl || '') ? '图像参考' : '风格提示',
      promptSuffix: String(reference.promptSuffix || '').trim(),
    }
  }).filter((item) => item.name || item.imageUrl || item.promptSuffix)
}

Page({
  data: {
    tool: null as AIToolItem | null,
    categoryLabel: '',
    useMinimalPresentation: false,
    visualStyleOptions: [] as VisualStyleOption[],
    selectedStyleOptionKey: '',
    selectedReferencePresetId: '',
    selectedPresetReferenceImageUrl: '',
    promptText: '',
    optimizingPrompt: false,
    uploadedOriginalImageUrl: '',
    uploadingOriginal: false,
    generating: false,
    qualityOptions: [
      { label: '2K', value: 'hd', desc: '适合展示' },
      { label: '4K', value: 'uhd', desc: '适合打印' },
    ],
    selectedQuality: DEFAULT_QUALITY,
    canvasOptions: [
      { size: '16:9', value: '16:9', desc: '横版' },
      { size: '1:1', value: '1:1', desc: '默认' },
      { size: '9:16', value: '9:16', desc: '竖版' },
    ],
    selectedCanvas: DEFAULT_CANVAS,
    generateCountOptions: [1, 2, 3],
    selectedGenerateCount: DEFAULT_GENERATE_COUNT,
    drawSingleCost: DEFAULT_DRAW_SINGLE_COST,
    drawMultiCost: DEFAULT_DRAW_MULTI_COST,
    currentScene: 'ai_draw_single',
    currentUnitCost: DEFAULT_DRAW_SINGLE_COST,
    currentCost: DEFAULT_DRAW_SINGLE_COST,
    pricingLoaded: false,
    stoneBalance: 0,
    balanceLoaded: false,
    bottomTipText: '正在加载计费信息…',
    loadingTool: false,
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
    this.setData({ loadingTool: true })
    try {
      const tool = await fetchAIToolDetail(id)
      this.setData({
        tool,
        categoryLabel: getCategoryLabel(tool.category),
        useMinimalPresentation: shouldUseMinimalAIToolPresentation(tool),
        visualStyleOptions: buildVisualStyleOptions(tool),
        selectedStyleOptionKey: '',
      }, () => {
        this.loadAIPricing()
        this.loadStoneBalance()
      })
    } catch (error: any) {
      wx.showToast({
        title: error?.message || '工具信息不存在',
        icon: 'none',
      })
    } finally {
      this.setData({ loadingTool: false })
    }
  },

  onShow() {
    this.loadStoneBalance()
  },

  onHide() {
    wx.hideLoading()
  },

  onUnload() {
    wx.hideLoading()
  },

  getToken(): string {
    return String(wx.getStorageSync('token') || '').trim()
  },

  ensureToken(): string {
    const token = this.getToken()
    if (token) {
      return token
    }
    wx.showModal({
      title: '提示',
      content: '请先登录后再使用 AI 生图工具',
      confirmText: '去登录',
      success: (res) => {
        if (res.confirm) {
          wx.navigateTo({ url: '/pages/login/login' })
        }
      },
    })
    return ''
  },

  buildBottomTipText(options?: {
    pricingLoaded?: boolean
    currentUnitCost?: number
    currentCost?: number
    balanceLoaded?: boolean
    stoneBalance?: number
  }) {
    const pricingLoaded = typeof options?.pricingLoaded === 'boolean' ? options.pricingLoaded : this.data.pricingLoaded
    const currentUnitCost = typeof options?.currentUnitCost === 'number' ? options.currentUnitCost : Number(this.data.currentUnitCost || 0)
    const currentCost = typeof options?.currentCost === 'number' ? options.currentCost : Number(this.data.currentCost || 0)
    const balanceLoaded = typeof options?.balanceLoaded === 'boolean' ? options.balanceLoaded : this.data.balanceLoaded
    const stoneBalance = typeof options?.stoneBalance === 'number' ? options?.stoneBalance : Number(this.data.stoneBalance || 0)

    if (!pricingLoaded || currentUnitCost <= 0) {
      return '正在获取生成费用，请稍候。'
    }
    if (balanceLoaded) {
      if (stoneBalance >= currentCost) {
        return `当前余额 ${stoneBalance} 灵石，生成后预计剩余 ${stoneBalance - currentCost} 灵石。`
      }
      return `当前余额 ${stoneBalance} 灵石，本次需 ${currentCost} 灵石，还差 ${currentCost - stoneBalance} 灵石。`
    }
    return `本次需 ${currentCost} 灵石。`
  },

  getSelectedVisualStyle(): VisualStyleOption | undefined {
    return (this.data.visualStyleOptions || []).find((item: VisualStyleOption) => item.key === this.data.selectedStyleOptionKey)
  },

  getSelectedVisualStyleImageUrl(): string {
    return normalizeUploadedImageUrl(this.getSelectedVisualStyle()?.modelReferenceImageUrl || '')
  },

  hasVisualStyleReferenceImage(): boolean {
    return !!this.getSelectedVisualStyleImageUrl()
  },

  getCurrentScene(): 'ai_draw_single' | 'ai_draw_multi' {
    return this.hasVisualStyleReferenceImage() ? 'ai_draw_multi' : 'ai_draw_single'
  },

  getCurrentGenerateCount(): number {
    const currentValue = Number(this.data.selectedGenerateCount || DEFAULT_GENERATE_COUNT)
    if (!Number.isFinite(currentValue) || currentValue <= 0) {
      return DEFAULT_GENERATE_COUNT
    }
    return Math.min(3, Math.max(1, Math.floor(currentValue)))
  },

  syncCurrentCost(callback?: () => void) {
    const nextScene = this.getCurrentScene()
    const nextGenerateCount = this.getCurrentGenerateCount()
    const nextUnitCost = nextScene === 'ai_draw_multi' ? Number(this.data.drawMultiCost || 0) : Number(this.data.drawSingleCost || 0)
    const nextCost = nextUnitCost * nextGenerateCount
    this.setData({
      currentScene: nextScene,
      selectedGenerateCount: nextGenerateCount,
      currentUnitCost: nextUnitCost,
      currentCost: nextCost,
      bottomTipText: this.buildBottomTipText({
        currentUnitCost: nextUnitCost,
        currentCost: nextCost,
      }),
    }, callback)
  },

  async loadAIPricing() {
    try {
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/ai/pricing?scenes=ai_draw_single,ai_draw_multi`,
          method: 'GET',
          success: (requestRes) => {
            if (requestRes.statusCode === 200 && requestRes.data) {
              const data = requestRes.data as any
              if (data.code === 0) {
                resolve(data.data || {})
              } else {
                reject(new Error(data.msg || '获取 AI 计费失败'))
              }
            } else {
              reject(new Error(`请求失败: ${requestRes.statusCode}`))
            }
          },
          fail: reject,
        })
      })

      const prices = res?.prices || {}
      const nextSingleCost = Number(prices.ai_draw_single)
      const nextMultiCost = Number(prices.ai_draw_multi)
      this.setData({
        drawSingleCost: Number.isFinite(nextSingleCost) && nextSingleCost > 0 ? nextSingleCost : DEFAULT_DRAW_SINGLE_COST,
        drawMultiCost: Number.isFinite(nextMultiCost) && nextMultiCost > 0 ? nextMultiCost : DEFAULT_DRAW_MULTI_COST,
        pricingLoaded: Number.isFinite(nextSingleCost) && nextSingleCost > 0 && Number.isFinite(nextMultiCost) && nextMultiCost > 0,
      }, () => this.syncCurrentCost())
    } catch (_error) {
      this.setData({
        pricingLoaded: false,
      }, () => this.syncCurrentCost())
    }
  },

  async requestStoneBalance(token: string): Promise<number> {
    const res = await new Promise<any>((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/user/stones`,
        method: 'GET',
        header: {
          token,
          'Content-Type': 'application/json',
        },
        success: (requestRes) => {
          if (requestRes.statusCode === 200 && requestRes.data) {
            const data = requestRes.data as any
            if (data.code === 0) {
              resolve(data.data || {})
            } else {
              reject(new Error(data.msg || '获取余额失败'))
            }
          } else {
            reject(new Error(`请求失败: ${requestRes.statusCode}`))
          }
        },
        fail: reject,
      })
    })
    return Number(res?.stones || 0)
  },

  async loadStoneBalance() {
    const token = this.getToken()
    if (!token) {
      this.setData({
        stoneBalance: 0,
        balanceLoaded: false,
        bottomTipText: this.buildBottomTipText({
          stoneBalance: 0,
          balanceLoaded: false,
        }),
      })
      return
    }

    try {
      const stones = await this.requestStoneBalance(token)
      this.setData({
        stoneBalance: stones,
        balanceLoaded: true,
        bottomTipText: this.buildBottomTipText({
          stoneBalance: stones,
          balanceLoaded: true,
        }),
      })
    } catch (_error) {
      this.setData({
        balanceLoaded: false,
        bottomTipText: this.buildBottomTipText({
          balanceLoaded: false,
        }),
      })
    }
  },

  async uploadImageFile(tempFilePath: string, token: string): Promise<string> {
    const apiPath = '/api/v1/miniprogram/ai/upload-reference-image'
    const deviceID = getCachedDeviceFingerprint() || ''
    const params = generateRequestParams(token, '{}', apiPath, deviceID)
    const headers = paramsToHeaders(params)

    return new Promise((resolve, reject) => {
      wx.uploadFile({
        url: `${API_BASE_URL}${apiPath}`,
        filePath: tempFilePath,
        name: 'file',
        header: headers,
        success: (res) => {
          if (res.statusCode !== 200) {
            reject(new Error(`上传失败: ${res.statusCode}`))
            return
          }
          try {
            const data = JSON.parse(res.data) as any
            if (data.code === 0 && data.data && data.data.url) {
              resolve(String(data.data.url))
            } else {
              reject(new Error(data.msg || '上传失败'))
            }
          } catch (_error) {
            reject(new Error('解析响应失败'))
          }
        },
        fail: reject,
      })
    })
  },

  onUploadOriginal() {
    const token = this.ensureToken()
    if (!token) {
      return
    }

    wx.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: async (res) => {
        const tempFiles = (res.tempFiles || []) as Array<{ tempFilePath: string }>
        const tempFilePath = String(tempFiles[0]?.tempFilePath || '')
        if (!tempFilePath) {
          return
        }

        this.setData({ uploadingOriginal: true })
        wx.showLoading({ title: '上传原图中...', mask: true })

        try {
          const uploadedUrl = await this.uploadImageFile(tempFilePath, token)
          this.setData({ uploadedOriginalImageUrl: uploadedUrl })
          wx.showToast({ title: '上传成功', icon: 'success' })
        } catch (error: any) {
          wx.showToast({ title: error?.message || '上传失败', icon: 'none' })
        } finally {
          wx.hideLoading()
          this.setData({ uploadingOriginal: false })
        }
      },
      fail: (err) => {
        const message = String(err?.errMsg || '')
        if (message.indexOf('cancel') >= 0) {
          return
        }
        wx.showToast({ title: '选择图片失败', icon: 'none' })
      },
    })
  },

  onDeleteOriginal() {
    this.setData({ uploadedOriginalImageUrl: '' })
  },

  onPreviewOriginal() {
    const url = normalizeUploadedImageUrl(this.data.uploadedOriginalImageUrl)
    if (!url) {
      return
    }
    wx.previewImage({
      current: url,
      urls: [url],
      fail: () => {
        wx.showToast({ title: '预览失败', icon: 'none' })
      },
    })
  },

  onSelectPresetReference(e: PresetTapEvent) {
    const id = String(e.currentTarget.dataset.id || '')
    const tool = this.data.tool
    if (!id || !tool) {
      return
    }

    // 再次点击同一个预设 → 取消选中
    if (this.data.selectedReferencePresetId === id) {
      this.setData({
        selectedReferencePresetId: '',
        selectedPresetReferenceImageUrl: '',
      }, () => this.syncCurrentCost())
      return
    }

    // 选中新的预设
    const targetReference = tool.presetReferences.find((item: AIToolPresetReference) => item.id === id)
    const targetUrl = normalizeUploadedImageUrl(targetReference?.imageUrl || '')

    this.setData({
      selectedReferencePresetId: id,
      selectedPresetReferenceImageUrl: targetUrl,
    }, () => this.syncCurrentCost())

    // 如果预设没有图片，仅用文字提示词
    if (!targetUrl) {
      wx.showToast({
        title: '已选择风格，将使用文字描述指导生成',
        icon: 'none',
      })
    }
  },

  onSelectStyle(e: PresetTapEvent) {
    const key = String(e.currentTarget.dataset.key || '')
    if (!key) {
      return
    }
    const nextKey = this.data.selectedStyleOptionKey === key ? '' : key
    this.setData({
      selectedStyleOptionKey: nextKey,
    }, () => this.syncCurrentCost())
  },

  onSelectQuality(e: any) {
    const value = String(e.currentTarget.dataset.value || '')
    if (!value || value === this.data.selectedQuality) {
      return
    }
    this.setData({
      selectedQuality: value,
    })
  },

  onSelectCanvas(e: any) {
    const value = String(e.currentTarget.dataset.value || '')
    if (!value || value === this.data.selectedCanvas) {
      return
    }
    this.setData({
      selectedCanvas: value,
    })
  },

  onSelectGenerateCount(e: any) {
    const value = Number(e.currentTarget.dataset.count)
    if (!Number.isFinite(value) || value <= 0) {
      return
    }
    const nextGenerateCount = Math.min(3, Math.max(1, Math.floor(value)))
    if (nextGenerateCount === this.data.selectedGenerateCount) {
      return
    }
    this.setData({
      selectedGenerateCount: nextGenerateCount,
    }, () => this.syncCurrentCost())
  },

  onPromptInput(e: InputEvent) {
    this.setData({
      promptText: String(e.detail.value || ''),
    })
  },

  async onOptimizePrompt() {
    if (this.data.optimizingPrompt) return;
    const rawPrompt = (this.data.promptText || '').trim();
    if (!rawPrompt) {
      wx.showToast({ title: '请先输入提示词', icon: 'none' });
      return;
    }
    this.setData({ optimizingPrompt: true });
    const { optimizePrompt } = require('../../utils/promptOptimize');
    const scene = (this.data.tool && this.data.tool.category) || 'architecture';
    const result = await optimizePrompt(rawPrompt, scene);
    this.setData({ optimizingPrompt: false });
    if (result.success && result.optimizedPrompt) {
      this.setData({ promptText: result.optimizedPrompt });
      wx.showToast({ title: '优化完成', icon: 'success' });
    } else {
      if (result.needMembership) {
        wx.showModal({ title: '会员专属', content: result.error || '请开通会员后使用AI优化', confirmText: '去开通', success: (res) => { if (res.confirm) wx.navigateTo({ url: '/pages/topupcenter/topupcenter' }); } });
      } else {
        wx.showToast({ title: result.error || '优化失败', icon: 'none' });
      }
    }
  },

  async onStartCreate() {
    const tool = this.data.tool
    if (!tool || this.data.generating) {
      return
    }

    const promptText = String(this.data.promptText || '').trim()
    if (promptText && blockUnsafeText(promptText, { scene: 'prompt' })) {
      return
    }

    const originalImageUrl = normalizeUploadedImageUrl(this.data.uploadedOriginalImageUrl)
    if (!originalImageUrl) {
      wx.showToast({
        title: '请先上传待处理图片',
        icon: 'none',
      })
      return
    }

    const token = this.ensureToken()
    if (!token) {
      return
    }

    const selectedStyle = this.getSelectedVisualStyle()
    const selectedStyleImageUrl = this.getSelectedVisualStyleImageUrl()
    const hasStyleReferenceImage = this.hasVisualStyleReferenceImage()
    const generateCount = this.getCurrentGenerateCount()

    const imageUrls: string[] = [originalImageUrl]
    if (selectedStyleImageUrl) {
      imageUrls.push(selectedStyleImageUrl)
    }

    if (!this.data.pricingLoaded || this.data.currentUnitCost <= 0 || this.data.currentCost <= 0) {
      wx.showToast({
        title: '计费信息加载中，请稍后再试',
        icon: 'none',
      })
      this.loadAIPricing()
      return
    }

    try {
      const stones = await this.requestStoneBalance(token)
      this.setData({
        stoneBalance: stones,
        balanceLoaded: true,
      }, () => this.syncCurrentCost())
      if (stones < this.data.currentCost) {
        wx.showModal({
          title: '余额不足',
          content: `当前余额：${stones}灵石，需要：${this.data.currentCost}灵石`,
          confirmText: '去充值',
          success: (res) => {
            if (res.confirm) {
              wx.navigateTo({ url: '/pages/topupcenter/topupcenter' })
            }
          },
        })
        return
      }
    } catch (_error) {
      wx.showToast({
        title: '检查余额失败',
        icon: 'none',
      })
      return
    }

    const scene = hasStyleReferenceImage ? 'ai_draw_multi' : 'ai_draw_single'
    const payload: Record<string, any> = {
      service_type: DEFAULT_SERVICE_TYPE,
      service: DEFAULT_SERVICE,
      quality: this.data.selectedQuality,
      canvas: this.data.selectedCanvas,
      generate_count: generateCount,
      tool_id: normalizeToolId(tool.id),
      user_prompt: promptText,
      reference_selection_type: selectedStyle?.sourceType === 'reference' ? 'preset' : (selectedStyle ? 'style' : 'none'),
      reference_preset_id: selectedStyle?.sourceType === 'reference' ? selectedStyle.sourceId : '',
      style_preset_id: selectedStyle?.sourceType === 'style' ? selectedStyle.sourceId : '',
      original_image_url: originalImageUrl,
      image_url: originalImageUrl,
      images: imageUrls,
    }

    if (selectedStyle?.name) {
      payload.style = selectedStyle.name
    }
    if (selectedStyleImageUrl) {
      payload.reference_image_url = selectedStyleImageUrl
    }

    const requestBody = { scene, payload }
    const apiPath = '/api/v1/miniprogram/ai/draw'
    const deviceID = getCachedDeviceFingerprint() || ''
    const params = generateRequestParams(token, requestBody, apiPath, deviceID)
    const headers = paramsToHeaders(params)

    this.setData({ generating: true })
    wx.showLoading({ title: '创作中...', mask: true })

    try {
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: {
            ...headers,
            'Content-Type': 'application/json',
          },
          data: requestBody,
          success: (requestRes) => {
            if (requestRes.statusCode === 200 && requestRes.data) {
              const data = requestRes.data as any
              if (data.code === 0) {
                resolve(data.data)
              } else {
                reject(new Error(data.msg || '生成失败'))
              }
            } else {
              reject(new Error(`请求失败: ${requestRes.statusCode}`))
            }
          },
          fail: reject,
        })
      })

      if (!res?.task_no) {
        throw new Error('生成失败')
      }

      const taskNo = encodeURIComponent(String(res.task_no))
      const safePrompt = encodeURIComponent(promptText || tool.name)
      const safeToolId = encodeURIComponent(tool.id)
      wx.navigateTo({
        url: `/pages/generatedetails/generatedetails?task_no=${taskNo}&prompt=${safePrompt}&tool_id=${safeToolId}`,
        success: (navRes) => {
          navRes.eventChannel.emit('taskData', {
            id: 0,
            task_no: res.task_no,
            scene,
            status: 'pending',
            requested_count: generateCount,
            generated_count: 0,
            stones_used: this.data.currentCost,
            result: {},
            error_message: '',
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
            task_type: 'ai_draw',
            prompt: promptText || tool.name,
            user_prompt: promptText || tool.name,
            reference_image_url: selectedStyleImageUrl || '',
            original_image_url: originalImageUrl,
            quality: this.data.selectedQuality,
            canvas: this.data.selectedCanvas,
            tool_id: tool.id,
            tool_name: tool.name,
          })
        },
        fail: () => {
          wx.showToast({
            title: '页面跳转失败',
            icon: 'none',
          })
        },
      })
    } catch (error: any) {
      wx.showToast({
        title: sanitizeAIGenerationErrorMessage(error?.message || '生成失败'),
        icon: 'none',
        duration: 2000,
      })
    } finally {
      wx.hideLoading()
      this.setData({ generating: false })
      this.loadStoneBalance()
    }
  },

  onShareAppMessage() {
    const tool = this.data.tool
    return {
      title: tool ? `${tool.name} · 开始使用` : 'AI生图工具',
      path: tool ? `/pages/aitoolworkbench/aitoolworkbench?id=${tool.id}` : '/pages/aitools/aitools',
    }
  },
})
