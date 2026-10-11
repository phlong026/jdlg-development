import { get, post, put, patch, del } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface InspirationItem {
  id: number;
  title: string;
  description: string;
  cover_image?: string;
  images?: string[];
  image_width?: number;
  image_height?: number;
  tags?: string[];
  scene?: string;
  style?: string;
  topic?: 'scene' | 'style' | 'villa' | 'inspiration';
  sort_order: number;
  status: 'published' | 'pending' | 'draft' | 'archived';
  creator?: string;
  creator_user_id?: number;
  view_count: number;
  like_count: number;
  created_at: string;
  updated_at: string;
}

export interface InspirationListResponse {
  list: InspirationItem[];
  total: number;
  page: number;
  page_size: number;
}

export interface InspirationListParams {
  topic?: string;
  status?: string;
  keyword?: string;
  scene?: string;
  style?: string;
  page?: number;
  page_size?: number;
}

export interface InspirationUpsertRequest {
  title: string;
  description?: string;
  images?: string[];
  image_width?: number;
  image_height?: number;
  tags?: string[];
  scene?: string;
  style?: string;
  topic?: 'scene' | 'style' | 'villa' | 'inspiration';
  sort_order?: number;
  status?: 'published' | 'pending' | 'draft' | 'archived';
}

export const getInspirationList = async (params?: InspirationListParams) => {
  const res = await get<InspirationListResponse>(API_ENDPOINTS.INSPIRATIONS.LIST, params as Record<string, any>);
  return res.data!;
};

export const getInspirationDetail = async (id: string) => {
  const res = await get<InspirationItem>(API_ENDPOINTS.INSPIRATIONS.DETAIL(id));
  return res.data!;
};

export const createInspiration = async (data: InspirationUpsertRequest) => {
  const res = await post<InspirationItem>(API_ENDPOINTS.INSPIRATIONS.CREATE, data);
  return res.data!;
};

export const updateInspiration = async (id: string, data: InspirationUpsertRequest) => {
  const res = await put<InspirationItem>(API_ENDPOINTS.INSPIRATIONS.UPDATE(id), data);
  return res.data!;
};

export const updateInspirationStatus = async (id: string, status: 'published' | 'pending' | 'draft' | 'archived') => {
  const res = await patch<InspirationItem>(API_ENDPOINTS.INSPIRATIONS.STATUS(id), { status });
  return res.data!;
};

export const deleteInspiration = async (id: string) => {
  await del(API_ENDPOINTS.INSPIRATIONS.DELETE(id));
};
