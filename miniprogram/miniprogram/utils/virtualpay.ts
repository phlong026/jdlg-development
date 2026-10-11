// 虚拟支付（小程序虚拟支付 / wx.requestVirtualPayment）统一调用工具。
//
// 设计取舍：
// - 与现有 wechatpay JSAPI 调用风格保持一致（generateRequestParams + paramsToHeaders）。
// - 暴露一个 requestVirtualPay 函数，覆盖三种业务：充值（recharge）、认证费（certification）。
// - 调用前必须做平台能力检测：wx.canIUse('requestVirtualPayment') 与微信版本（iOS ≥ 8.0.68）。
// - signData/paySig/signature 必须原样透传给 wx.requestVirtualPayment，前端不能 JSON.parse 后重组。

import { generateRequestParams, paramsToHeaders } from './parameter'
import { getCachedDeviceFingerprint } from './deviceFingerprint'

const API_BASE_URL = 'https://api.jiadilingguang.com'

const PREPARE_PATH = '/api/v1/miniprogram/virtualpay/prepare'
const CONFIRM_PATH = '/api/v1/miniprogram/virtualpay/confirm'

const IOS_MIN_WECHAT_VERSION = '8.0.68'

export type VirtualPayPlanType = 'recharge' | 'certification' | 'report'

export interface RequestVirtualPayOptions {
  planType: VirtualPayPlanType
  amountFen?: number     // recharge 必填
  productId?: string     // certification 必填，cert_designer / cert_enterprise
  applicationId?: number // certification 必填，关联工单 ID
  reportId?: string      // report 必填；金额、商品由服务端决定
  attach?: string
}

export interface RequestVirtualPayResult {
  success: boolean
  orderNo?: string
  state?: string
  errMsg?: string
}

/**
 * 发起虚拟支付。串联：能力检测 → wx.login 取 code → /prepare 拿签名 → wx.requestVirtualPayment → /confirm 主动确认。
 *
 * 任何一步失败都返回 { success:false, errMsg } 而不抛异常，便于业务层用 toast 直接展示。
 */
export async function requestVirtualPay(opts: RequestVirtualPayOptions): Promise<RequestVirtualPayResult> {
  const guard = ensureVirtualPaySupported()
  if (!guard.ok) {
    return { success: false, errMsg: guard.msg }
  }

  const token = String(wx.getStorageSync('token') || '').trim()
  if (!token) {
    return { success: false, errMsg: '请先登录' }
  }

  // wx.login 每次支付前都重新取 code，避免 session_key 过期
  let code = ''
  try {
    const loginRes = await new Promise<WechatMiniprogram.LoginSuccessCallbackResult>((resolve, reject) => {
      wx.login({ success: resolve as any, fail: reject })
    })
    code = loginRes.code || ''
  } catch (err: any) {
    return { success: false, errMsg: '获取登录态失败' }
  }
  if (!code) {
    return { success: false, errMsg: '获取登录态失败' }
  }

  const platform = detectPlatform()

  const prepareBody = {
    code,
    plan_type: opts.planType,
    amount_fen: opts.amountFen || 0,
    product_id: opts.productId || '',
    application_id: opts.applicationId || 0,
    ...(opts.planType === 'report' ? { report_id: opts.reportId || '' } : {}),
    platform,
    attach: opts.attach || '',
  }

  const prepareRes = await postSigned<any>(PREPARE_PATH, prepareBody, token)
  if (!prepareRes.ok) {
    return { success: false, errMsg: prepareRes.errMsg }
  }
  const prepData = prepareRes.data
  if (!prepData || !prepData.signData || !prepData.paySig || !prepData.signature) {
    return { success: false, errMsg: '准备支付失败' }
  }

  // 拉起虚拟支付。signData / paySig / signature 必须原样传，不要 JSON.parse 再 stringify。
  try {
    await new Promise<void>((resolve, reject) => {
      // @ts-ignore wx 类型可能未声明 requestVirtualPayment
      wx.requestVirtualPayment({
        mode: prepData.mode,
        signData: prepData.signData,
        paySig: prepData.paySig,
        signature: prepData.signature,
        success: () => resolve(),
        fail: (err: any) => reject(err),
      })
    })
  } catch (err: any) {
    const msg = (err && (err.errMsg || err.msg)) || '支付未完成'
    return { success: false, orderNo: prepData.order_no, errMsg: msg }
  }

  // 调用 confirm 主动核对（兜底：即使推送/补偿任务都掉链，这里也能让用户立即拿到发货结果）
  const confirmRes = await postSigned<any>(CONFIRM_PATH, { order_no: prepData.order_no, platform }, token)
  if (!confirmRes.ok) {
    // confirm 失败不算彻底失败：补偿任务会兜底；只是用户感知会稍慢
    return { success: true, orderNo: prepData.order_no, state: 'PENDING', errMsg: confirmRes.errMsg }
  }
  return { success: true, orderNo: prepData.order_no, state: confirmRes.data?.state || '' }
}

/**
 * 平台能力 + 微信版本检测。返回 ok=false 时附 msg 给业务层做兜底提示。
 */
export function ensureVirtualPaySupported(): { ok: true } | { ok: false; msg: string } {
  // @ts-ignore canIUse 字符串
  if (!wx.canIUse('requestVirtualPayment')) {
    return { ok: false, msg: '当前微信版本不支持虚拟支付，请升级到最新版本' }
  }
  const platform = detectPlatform()
  if (platform === 'ios') {
    const sysVersion = getWechatVersion()
    if (sysVersion && compareVersion(sysVersion, IOS_MIN_WECHAT_VERSION) < 0) {
      return { ok: false, msg: `iOS 端需将微信升级至 ${IOS_MIN_WECHAT_VERSION} 及以上版本` }
    }
  }
  return { ok: true }
}

function detectPlatform(): string {
  try {
    // @ts-ignore wx.getDeviceInfo 是新版本 API
    if (typeof wx.getDeviceInfo === 'function') {
      // @ts-ignore
      const info = wx.getDeviceInfo()
      const p = String(info?.platform || '').toLowerCase()
      if (p) return p
    }
  } catch {}
  // 兼容老版本：getSystemInfoSync.platform 也提供同名字段
  try {
    const sys = wx.getSystemInfoSync()
    return String(sys.platform || '').toLowerCase()
  } catch {}
  return ''
}

function getWechatVersion(): string {
  try {
    const sys = wx.getSystemInfoSync()
    return String(sys.version || '')
  } catch {
    return ''
  }
}

/** 比较版本号（语义版本三段比较）。a < b 返回 -1, a > b 返回 1, 相等返回 0。 */
function compareVersion(a: string, b: string): number {
  const pa = a.split('.')
  const pb = b.split('.')
  const len = Math.max(pa.length, pb.length)
  for (let i = 0; i < len; i++) {
    const na = parseInt(pa[i] || '0', 10)
    const nb = parseInt(pb[i] || '0', 10)
    if (na > nb) return 1
    if (na < nb) return -1
  }
  return 0
}

interface SignedResp<T> {
  ok: boolean
  data?: T
  errMsg?: string
}

async function postSigned<T = any>(apiPath: string, body: any, token: string): Promise<SignedResp<T>> {
  try {
    const deviceID = getCachedDeviceFingerprint() || ''
    const params = generateRequestParams(token, body, apiPath, deviceID)
    const headers = paramsToHeaders(params)
    headers['Content-Type'] = 'application/json'
    const res = await new Promise<any>((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}${apiPath}`,
        method: 'POST',
        data: body,
        header: headers,
        success: (r) => resolve(r),
        fail: reject,
      })
    })
    if (res.statusCode !== 200 || !res.data) {
      return { ok: false, errMsg: (res.data && res.data.msg) || `HTTP ${res.statusCode}` }
    }
    if (res.data.code !== 0) {
      return { ok: false, errMsg: res.data.msg || '请求失败' }
    }
    return { ok: true, data: res.data.data as T }
  } catch (err: any) {
    return { ok: false, errMsg: (err && err.errMsg) || '网络错误' }
  }
}
