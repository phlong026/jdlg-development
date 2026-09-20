import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
const ROOT = '/api/v1/miniprogram/reports';
export async function reportRequest<T = any>(suffix: string, method: 'GET' | 'POST' = 'GET', body: any = {}): Promise<T> {
    const token = String(wx.getStorageSync('token') || '');
    if (!token && suffix !== '/config')
        throw new Error('请先登录后查看您的报告。');
    const path = ROOT + suffix.split('?')[0];
    const params = token ? await generateRequestParams(token, body, path, getCachedDeviceFingerprint() || '') : null;
    return new Promise((resolve, reject) => wx.request({
        url: 'https://api.jiadilingguang.com' + ROOT + suffix, method, data: body, timeout: 30000,
        header: { 'Content-Type': 'application/json', ...(params ? paramsToHeaders(params) : {}) },
        success: (response) => {
            const result = response.data as any;
            if (response.statusCode === 401)
                wx.removeStorageSync('token');
            if (response.statusCode === 200 && result?.code === 0)
                resolve(result.data);
            else
                reject(Object.assign(new Error(result?.msg || '暂时无法读取报告，请稍后重试。'), { statusCode: response.statusCode }));
        },
        fail: () => reject(new Error('网络连接中断，您的信息不会被重复提交，请重试。')),
    }));
}
export function reportID(value: unknown): string {
    if (typeof value !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(value))
        throw new Error('报告链接不完整，请从建房报告入口重新进入。');
    return value;
}
