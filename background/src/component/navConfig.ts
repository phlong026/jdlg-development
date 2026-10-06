// 后台导航的单一数据源。
// 侧栏渲染、可见性判断都读这里；页面网址保持不变（24 个 scss 以旧网址限定样式作用域，
// 网址一改页面会整页失去样式——见 scripts/check-routes.mjs）。
import type { ComponentType } from 'react';
import {
    FiHome,
    FiUsers,
    FiCompass,
    FiFileText,
    FiActivity,
    FiShare2,
    FiSettings,
    FiCloud,
    FiLayers,
    FiDollarSign,
    FiTool,
    FiUserCheck,
    FiImage,
    FiShield,
    FiBarChart2,
} from 'react-icons/fi';

export interface NavItem {
    path: string;
    label: string;
    icon: ComponentType<{ className?: string }>;
}

export interface NavSection {
    title: string;
    items: NavItem[];
}

export const platformSections: NavSection[] = [
    {
        title: '经营驾驶舱',
        items: [
            { path: '/dashboard', label: '后台总控台', icon: FiHome },
            { path: '/users', label: '用户管理', icon: FiUsers },
            { path: '/user-workbench', label: '用户360工作台', icon: FiCompass },
            { path: '/recharge', label: '订单中心', icon: FiDollarSign },
            { path: '/virtualpay', label: '虚拟支付监控', icon: FiDollarSign },
            { path: '/ai-tasks', label: 'AI任务中心', icon: FiActivity },
            { path: '/support-tickets', label: '异常工单中心', icon: FiFileText },
        ],
    },
    {
        title: '用户与内容运营',
        items: [
            { path: '/certification', label: '资质认证与审核', icon: FiUserCheck },
            { path: '/designer-center', label: '设计师中心', icon: FiUsers },
            { path: '/membership-operations', label: '用户会员运营', icon: FiUserCheck },
            { path: '/customer-leads', label: '客服线索工作台', icon: FiFileText },
            { path: '/distribution', label: '分销邀请中心', icon: FiShare2 },
            { path: '/templates', label: '模板广场管理', icon: FiLayers },
            { path: '/inspirations', label: '灵感素材审核', icon: FiImage },
            { path: '/content-analytics', label: '内容运营分析', icon: FiActivity },
        ],
    },
    {
        title: '风险与追踪',
        items: [
            { path: '/risk-control', label: '风控台', icon: FiShield },
            { path: '/report-center', label: '报表导出中心', icon: FiBarChart2 },
            { path: '/logs', label: '日志管理', icon: FiFileText },
        ],
    },
    {
        title: '低频配置',
        items: [
            { path: '/ai-config', label: 'AI配置', icon: FiSettings },
            { path: '/ai-tools', label: 'AI工具管理', icon: FiLayers },
            { path: '/recharge-config', label: '充值配置', icon: FiDollarSign },
            { path: '/membership-plans', label: '会员计划', icon: FiDollarSign },
            { path: '/oss', label: 'OSS管理', icon: FiCloud },
            { path: '/utility-tools', label: '实用工具管理', icon: FiTool },
        ],
    },
];

export const customerSection: NavSection = {
    title: '客户管理',
    items: [{ path: '/customer-management', label: '客户管理工作台', icon: FiUsers }],
};

/**
 * 当前账号可见的导航分组。
 * fail-closed：仅 platform_admin 渲染平台菜单；scope 缺失或未知值时不暴露平台模块（与页内 tab 判定一致）。
 */
export function visibleSections(scope: string | undefined, crmEnabled: boolean | undefined): NavSection[] {
    if (scope !== 'platform_admin') return [customerSection];
    return crmEnabled ? [platformSections[0], customerSection, ...platformSections.slice(1)] : platformSections;
}
