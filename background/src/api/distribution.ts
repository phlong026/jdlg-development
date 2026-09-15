import { get } from '../utils/request';
import { API_ENDPOINTS } from '../config/api';

export interface DistributionOverview {
    inviter_count: number;
    total_invite_count: number;
    paid_invite_count: number;
    total_reward_amount: number;
    month_reward_amount: number;
    month_new_invites: number;
}

export interface DistributionInviterItem {
    user_id: number;
    username: string;
    nickname?: string;
    display_name?: string;
    invite_code?: string;
    invite_count: number;
    paid_invite_count: number;
    total_reward_amount: number;
    month_reward_amount: number;
    last_invited_at?: string;
    last_reward_at?: string;
}

export interface DistributionRewardItem {
    id: number;
    user_id: number;
    username: string;
    nickname?: string;
    display_name?: string;
    type: string;
    amount: number;
    scene_desc?: string;
    remark?: string;
    created_at?: string;
}

export interface DistributionListResponse<T> {
    list: T[];
    total: number;
    page: number;
    page_size: number;
}

export interface DistributionListParams {
    page?: number;
    page_size?: number;
    keyword?: string;
}

export const getDistributionOverview = async () => {
    const response = await get<DistributionOverview>(API_ENDPOINTS.DISTRIBUTION.OVERVIEW);
    if (!response.data) {
        throw new Error('获取分销邀请概览失败');
    }
    return response.data;
};

export const getDistributionInviters = async (params?: DistributionListParams) => {
    const response = await get<DistributionListResponse<DistributionInviterItem>>(API_ENDPOINTS.DISTRIBUTION.INVITERS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取邀请人排行失败');
    }
    return response.data;
};

export const getDistributionRewards = async (params?: DistributionListParams) => {
    const response = await get<DistributionListResponse<DistributionRewardItem>>(API_ENDPOINTS.DISTRIBUTION.REWARDS, params as Record<string, any>);
    if (!response.data) {
        throw new Error('获取奖励明细失败');
    }
    return response.data;
};
