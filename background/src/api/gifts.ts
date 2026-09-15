import { get, post, put, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// 礼品接口类型
export interface Gift {
    id: number;
    name: string;
    barcode?: string;
    qrcode?: string;
    points_cost: number;
    stock: number;
    tags?: string;
    description: string;
    image?: string;
    status: 'active' | 'inactive' | 'sold_out';
    created_at: string;
    updated_at: string;
}

// 礼品列表响应
export interface GiftListResponse {
    list: Gift[];
    total: number;
    page: number;
    page_size: number;
}

// 礼品列表参数
export interface GiftListParams {
    keyword?: string;
    tag?: string;
    status?: string;
    page?: number;
    page_size?: number;
}

// 创建/更新礼品请求
export interface GiftRequest {
    name: string;
    barcode?: string;
    points_cost: number;
    stock: number;
    tags?: string;
    description: string;
    image?: string;
    status?: string;
}

// 扫码请求
export interface ScanGiftRequest {
    barcode: string;
}

// 获取礼品列表
export const getGiftList = async (params?: GiftListParams): Promise<GiftListResponse> => {
    const response = await get<GiftListResponse>(API_ENDPOINTS.GIFTS.LIST, params);
    return response.data || { list: [], total: 0, page: 1, page_size: 20 };
};

// 获取礼品详情
export const getGiftDetail = async (id: string): Promise<Gift> => {
    const response = await get<Gift>(API_ENDPOINTS.GIFTS.DETAIL(id));
    if (!response.data) {
        throw new Error('获取礼品详情失败');
    }
    return response.data;
};

// 创建礼品
export const createGift = async (data: GiftRequest): Promise<Gift> => {
    const response = await post<Gift>(API_ENDPOINTS.GIFTS.CREATE, data);
    if (!response.data) {
        throw new Error('创建礼品失败');
    }
    return response.data;
};

// 更新礼品
export const updateGift = async (id: string, data: GiftRequest): Promise<Gift> => {
    const response = await put<Gift>(API_ENDPOINTS.GIFTS.UPDATE(id), data);
    if (!response.data) {
        throw new Error('更新礼品失败');
    }
    return response.data;
};

// 删除礼品
export const deleteGift = async (id: string): Promise<void> => {
    await del(API_ENDPOINTS.GIFTS.DELETE(id));
};

// 扫码管理礼品
export const scanGift = async (data: ScanGiftRequest): Promise<Gift> => {
    const response = await post<Gift>(API_ENDPOINTS.GIFTS.SCAN, data);
    if (!response.data) {
        throw new Error('扫码失败');
    }
    return response.data;
};
