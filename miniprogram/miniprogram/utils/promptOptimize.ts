/**
 * AI提示词优化工具 - 会员专属功能
 * 调用后端接口将用户随意输入优化为结构化提示词
 */
import { generateRequestParams, paramsToHeaders } from './parameter'
import { getCachedDeviceFingerprint } from './deviceFingerprint'

const API_BASE_URL = 'https://api.jiadilingguang.com'

export type PromptOptimizeScene = 'architecture' | 'interior' | 'landscape' | 'video' | 'general'

export interface PromptOptimizeResult {
  success: boolean
  optimizedPrompt?: string
  error?: string
  needMembership?: boolean
}

/**
 * 调用AI优化提示词接口
 * @param rawPrompt 用户原始输入
 * @param scene 场景类型
 * @returns 优化结果
 */
export function optimizePrompt(rawPrompt: string, scene: PromptOptimizeScene): Promise<PromptOptimizeResult> {
  return new Promise((resolve) => {
    const token = wx.getStorageSync('token')
    if (!token) {
      resolve({ success: false, error: '请先登录' })
      return
    }

    const trimmed = (rawPrompt || '').trim()
    if (!trimmed) {
      resolve({ success: false, error: '请先输入提示词内容' })
      return
    }

    const apiPath = '/api/v1/miniprogram/ai/prompt/optimize'
    const body = { raw_prompt: trimmed, scene }
    const deviceID = getCachedDeviceFingerprint() || ''
    const params = generateRequestParams(token, body, apiPath, deviceID)
    const headers = paramsToHeaders(params)

    wx.request({
      url: `${API_BASE_URL}${apiPath}`,
      method: 'POST',
      header: {
        ...headers,
        'Content-Type': 'application/json',
      },
      data: body,
      success: (res) => {
        const data = res.data as any
        if (res.statusCode === 403) {
          resolve({ success: false, error: data?.msg || '请开通会员后使用', needMembership: true })
          return
        }
        if (res.statusCode === 401) {
          resolve({ success: false, error: '请先登录' })
          return
        }
        if (res.statusCode === 200 && data?.code === 0 && data?.data?.optimized_prompt) {
          resolve({ success: true, optimizedPrompt: data.data.optimized_prompt })
        } else {
          resolve({ success: false, error: data?.msg || '优化失败，请稍后再试' })
        }
      },
      fail: () => {
        resolve({ success: false, error: '网络请求失败，请检查网络' })
      },
    })
  })
}
