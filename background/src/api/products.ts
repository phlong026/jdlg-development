import { get, post, put, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

// 商品接口类型
export interface Product {
    id: number;
    name: string;
    category: string;
    price: number;
    original_price?: number;
    stock: number;
    image?: string;
    description: string;
    status: 'on_sale' | 'off_sale' | 'sold_out';
    created_at: string;
    updated_at: string;
}

// 商品列表响应
export interface ProductListResponse {
    list: Product[];
    total: number;
    page: number;
    page_size: number;
}

// 商品列表参数
export interface ProductListParams {
    keyword?: string;
    category?: string;
    status?: string;
    page?: number;
    page_size?: number;
}

// 创建/更新商品请求
export interface ProductRequest {
    name: string;
    category: string;
    price: number;
    original_price?: number;
    stock: number;
    image?: string;
    description: string;
    status?: string;
}

// 获取商品列表
export const getProductList = async (params?: ProductListParams): Promise<ProductListResponse> => {
    const response = await get<ProductListResponse>(API_ENDPOINTS.PRODUCTS.LIST, params);
    return response.data || { list: [], total: 0, page: 1, page_size: 20 };
};

// 获取商品详情
export const getProductDetail = async (id: string): Promise<Product> => {
    const response = await get<Product>(API_ENDPOINTS.PRODUCTS.DETAIL(id));
    if (!response.data) {
        throw new Error('获取商品详情失败');
    }
    return response.data;
};

// 创建商品
export const createProduct = async (data: ProductRequest): Promise<Product> => {
    const response = await post<Product>(API_ENDPOINTS.PRODUCTS.CREATE, data);
    if (!response.data) {
        throw new Error('创建商品失败');
    }
    return response.data;
};

// 更新商品
export const updateProduct = async (id: string, data: ProductRequest): Promise<Product> => {
    const response = await put<Product>(API_ENDPOINTS.PRODUCTS.UPDATE(id), data);
    if (!response.data) {
        throw new Error('更新商品失败');
    }
    return response.data;
};

// 删除商品
export const deleteProduct = async (id: string): Promise<void> => {
    await del(API_ENDPOINTS.PRODUCTS.DELETE(id));
};

// 切换商品状态
export const toggleProductStatus = async (id: string): Promise<Product> => {
    const response = await put<Product>(API_ENDPOINTS.PRODUCTS.TOGGLE_STATUS(id));
    if (!response.data) {
        throw new Error('切换商品状态失败');
    }
    return response.data;
};
