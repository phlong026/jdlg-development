import { getCachedDeviceFingerprint } from './deviceFingerprint';
import { generateRequestParams, paramsToHeaders } from './parameter';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const CREATE_PATH = '/api/v1/miniprogram/ai/cost/shares';
const REPORT_ID_PATTERN = /^[A-Za-z0-9_-]{32}$/;

export type CostEstimateBasicInfo = {
  label: string;
  value: string;
  fullWidth?: boolean;
};

export type CostEstimateBreakdown = {
  name: string;
  amount: number;
  amountText: string;
  percent: number;
  note: string;
  estimable: boolean;
  color?: string;
};

export type CostEstimateShareResult = {
  version: number;
  estimatedTotalCost: string;
  estimatedUnitCost: string;
  totalLabel: string;
  unitLabel: string;
  basicInfo: CostEstimateBasicInfo[];
  costBreakdown: CostEstimateBreakdown[];
  dynamicNotes: string[];
};

export type CostEstimateShareSnapshot = {
  report_id: string;
  expires_at: string;
  result: CostEstimateShareResult;
};

function validText(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0;
}

function isBasicInfo(item: unknown): item is CostEstimateBasicInfo {
  if (!item || typeof item !== 'object') return false;
  const value = item as Partial<CostEstimateBasicInfo>;
  return validText(value.label) && validText(value.value)
    && (value.fullWidth === undefined || typeof value.fullWidth === 'boolean');
}

function isBreakdown(item: unknown): item is CostEstimateBreakdown {
  if (!item || typeof item !== 'object') return false;
  const value = item as Partial<CostEstimateBreakdown>;
  return validText(value.name)
    && typeof value.amount === 'number' && Number.isFinite(value.amount) && value.amount >= 0
    && validText(value.amountText)
    && typeof value.percent === 'number' && Number.isFinite(value.percent) && value.percent >= 0 && value.percent <= 100
    && validText(value.note)
    && typeof value.estimable === 'boolean';
}

export function isCostEstimateShareResult(value: unknown): value is CostEstimateShareResult {
  if (!value || typeof value !== 'object') return false;
  const result = value as Partial<CostEstimateShareResult>;
  return result.version === 4
    && validText(result.estimatedTotalCost)
    && validText(result.estimatedUnitCost)
    && validText(result.totalLabel)
    && validText(result.unitLabel)
    && Array.isArray(result.basicInfo) && result.basicInfo.length === 14 && result.basicInfo.every(isBasicInfo)
    && Array.isArray(result.costBreakdown) && (result.costBreakdown.length === 6 || result.costBreakdown.length === 7) && result.costBreakdown.every(isBreakdown)
    && Array.isArray(result.dynamicNotes) && result.dynamicNotes.every(note => typeof note === 'string');
}

export function sanitizeCostEstimateShareResult(value: unknown): CostEstimateShareResult {
  if (!isCostEstimateShareResult(value)) {
    throw new Error('测算报告数据无效，请重新测算');
  }
  return {
    version: value.version,
    estimatedTotalCost: value.estimatedTotalCost,
    estimatedUnitCost: value.estimatedUnitCost,
    totalLabel: value.totalLabel,
    unitLabel: value.unitLabel,
    basicInfo: value.basicInfo.map(item => ({
      label: item.label,
      value: item.value,
      ...(item.fullWidth === true ? { fullWidth: true } : {}),
    })),
    costBreakdown: value.costBreakdown.map(item => ({
      name: item.name,
      amount: item.amount,
      amountText: item.amountText,
      percent: item.percent,
      note: item.note,
      estimable: item.estimable,
    })),
    dynamicNotes: value.dynamicNotes.slice(),
  };
}

function request<T>(options: WechatMiniprogram.RequestOption): Promise<T> {
  return new Promise((resolve, reject) => {
    wx.request({
      ...options,
      timeout: 8000,
      success: (response) => {
        const body = response.data as any;
        if (response.statusCode === 200 && body && body.code === 0) {
          resolve(body.data as T);
          return;
        }
        const error: any = new Error(String(body?.msg || `请求失败: ${response.statusCode}`));
        error.statusCode = Number(response.statusCode || 0);
        error.code = Number(body?.code || 0);
        reject(error);
      },
      fail: () => reject(new Error('网络连接失败，请稍后重试')),
    });
  });
}

function validShareIdentity(value: any): value is { report_id: string; expires_at: string } {
  return !!value && REPORT_ID_PATTERN.test(String(value.report_id || '')) && validText(value.expires_at);
}

export async function createCostEstimateShare(result: CostEstimateShareResult): Promise<{ report_id: string; expires_at: string }> {
  const token = String(wx.getStorageSync('token') || '').trim();
  if (!token) throw new Error('请先登录后分享报告');
  const body = sanitizeCostEstimateShareResult(result);
  const params = generateRequestParams(token, body, CREATE_PATH, getCachedDeviceFingerprint() || '');
  const response = await request<{ report_id: string; expires_at: string }>({
    url: `${API_BASE_URL}${CREATE_PATH}`,
    method: 'POST',
    data: body,
    header: { ...paramsToHeaders(params), 'Content-Type': 'application/json' },
    __profileAccessResponseMode: 'soft-full-profile',
  } as any);
  if (!validShareIdentity(response)) throw new Error('分享报告创建失败，请重试');
  return response;
}

export async function getCostEstimateShare(reportId: string): Promise<CostEstimateShareSnapshot> {
  const id = String(reportId || '').trim();
  if (!REPORT_ID_PATTERN.test(id)) throw new Error('无效的分享报告链接');
  const path = `${CREATE_PATH}/${id}`;
  const response = await request<CostEstimateShareSnapshot>({
    url: `${API_BASE_URL}${path}`,
    method: 'GET',
  });
  if (!validShareIdentity(response) || response.report_id !== id || !isCostEstimateShareResult(response.result)) {
    throw new Error('分享报告数据无效，请重新测算');
  }
  return { ...response, result: sanitizeCostEstimateShareResult(response.result) };
}
