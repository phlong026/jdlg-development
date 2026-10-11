import { generateRequestParams, paramsToHeaders } from './parameter';
import {
  cacheDeviceFingerprint,
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
} from './deviceFingerprint';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const FAVORITES_API_PATH = '/api/v1/miniprogram/favorites';

export type FavoriteTargetType = 'template' | 'ai_tool' | 'designer' | 'inspiration';

export interface FavoriteListParams {
  type?: FavoriteTargetType | '';
  page?: number;
  pageSize?: number;
}

async function resolveDeviceId(): Promise<string> {
  let deviceId = getCachedDeviceFingerprint();
  if (deviceId) {
    return deviceId;
  }
  try {
    deviceId = await generateDeviceFingerprint();
    if (deviceId) {
      cacheDeviceFingerprint(deviceId);
    }
  } catch (error) {
    console.warn('获取设备ID失败:', error);
  }
  return deviceId || '';
}

async function buildSignedHeaders(apiPath: string, body: any = {}) {
  const token = String(wx.getStorageSync('token') || '').trim();
  if (!token) {
    throw new Error('请先登录');
  }
  const deviceId = await resolveDeviceId();
  const params = generateRequestParams(token, body, apiPath, deviceId);
  return {
    ...paramsToHeaders(params),
    'Content-Type': 'application/json',
  };
}

function request<T>(options: {
  apiPath: string;
  method: 'GET' | 'POST' | 'DELETE';
  body?: any;
  query?: string;
}): Promise<T> {
  const body = options.body || {};
  const signedPath = options.query ? `${options.apiPath}?${options.query}` : options.apiPath;
  return buildSignedHeaders(signedPath, body).then((headers) => new Promise<T>((resolve, reject) => {
    wx.request({
      url: `${API_BASE_URL}${signedPath}`,
      method: options.method,
      header: headers,
      data: body,
      success: (res) => {
        const response = (res.data || {}) as any;
        if (res.statusCode === 200 && response.code === 0) {
          resolve((response.data || {}) as T);
          return;
        }
        reject(new Error(response.msg || `请求失败: ${res.statusCode}`));
      },
      fail: reject,
    });
  }));
}

export function fetchFavorites(params: FavoriteListParams = {}) {
  const page = Math.max(1, Number(params.page || 1));
  const pageSize = Math.max(1, Math.min(50, Number(params.pageSize || 20)));
  const type = params.type || '';
  return request<any>({
    apiPath: FAVORITES_API_PATH,
    method: 'GET',
    query: `type=${encodeURIComponent(type)}&page=${page}&page_size=${pageSize}`,
  });
}

export function fetchFavoriteStatus(targetType: FavoriteTargetType, targetId: number) {
  return request<{ favorited: boolean }>({
    apiPath: `${FAVORITES_API_PATH}/status`,
    method: 'GET',
    query: `target_type=${encodeURIComponent(targetType)}&target_id=${Number(targetId || 0)}`,
  });
}

export function addFavorite(targetType: FavoriteTargetType, targetId: number) {
  return request<{ favorited: boolean }>({
    apiPath: FAVORITES_API_PATH,
    method: 'POST',
    body: {
      target_type: targetType,
      target_id: Number(targetId || 0),
    },
  });
}

export function removeFavorite(targetType: FavoriteTargetType, targetId: number) {
  return request<{ favorited: boolean }>({
    apiPath: FAVORITES_API_PATH,
    method: 'DELETE',
    body: {
      target_type: targetType,
      target_id: Number(targetId || 0),
    },
  });
}

export function toggleFavorite(targetType: FavoriteTargetType, targetId: number, favorited: boolean) {
  return favorited ? removeFavorite(targetType, targetId) : addFavorite(targetType, targetId);
}
