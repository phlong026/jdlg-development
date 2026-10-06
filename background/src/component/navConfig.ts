// 后台导航的单一数据源。
// 新导航（分组 + 子导航条）、旧导航（平铺分组，灰度回退用）与 ⌘K 页面搜索都读这里；
// 页面网址保持不变（24 个 scss 以旧网址限定样式作用域，网址一改页面会整页失去样式——见 scripts/check-routes.mjs）。
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

/* ───────────────────────── 新导航：分组 + 子导航条 ───────────────────────── */

export interface NavPage {
    path: string;
    label: string;
}

export interface NavGroup {
    id: string;
    label: string;
    icon: ComponentType<{ className?: string }>;
    /** 分组内的页面；第一个是点击侧栏时进入的落地页，多于一个时在子导航条里显示为 Tab */
    pages: NavPage[];
    /** 属于该分组、但不出现在 Tab 里的页面（例如用户 360 是从用户列表点进去的详情页） */
    related?: NavPage[];
    /** 低频的"二级页"：不占侧栏，从分组页右侧的入口进入 */
    secondary?: { label: string; pages: NavPage[] };
    /** 固定在侧栏底部 */
    pinned?: boolean;
}

export const navGroups: NavGroup[] = [
    { id: 'dashboard', label: '工作台', icon: FiHome, pages: [{ path: '/dashboard', label: '后台总控台' }] },
    {
        id: 'users', label: '用户与设计师', icon: FiUsers,
        pages: [
            { path: '/users', label: '用户列表' },
            { path: '/designer-center', label: '设计师' },
            { path: '/certification', label: '认证审核' },
            { path: '/membership-operations', label: '会员运营' },
        ],
        related: [{ path: '/user-workbench', label: '用户360工作台' }],
    },
    {
        id: 'orders', label: '订单与支付', icon: FiDollarSign,
        pages: [
            { path: '/recharge', label: '订单中心' },
            { path: '/virtualpay', label: '虚拟支付监控' },
        ],
        secondary: {
            label: '支付与会员配置',
            pages: [
                { path: '/recharge-config', label: '充值配置' },
                { path: '/membership-plans', label: '会员计划' },
            ],
        },
    },
    {
        id: 'ai', label: 'AI 运营', icon: FiActivity,
        pages: [
            { path: '/ai-tasks', label: 'AI 任务中心' },
            { path: '/ai-tools', label: 'AI 工具管理' },
        ],
    },
    {
        id: 'content', label: '内容运营', icon: FiLayers,
        pages: [
            { path: '/templates', label: '模板广场' },
            { path: '/inspirations', label: '灵感素材' },
            { path: '/utility-tools', label: '实用工具' },
            { path: '/content-analytics', label: '内容分析' },
        ],
    },
    { id: 'support', label: '异常工单', icon: FiFileText, pages: [{ path: '/support-tickets', label: '异常工单中心' }] },
    // 客服线索工作台是高频的一级入口：不放进 Tab，也不因启用 CRM 而隐藏（原侧栏里它始终独立可见）
    { id: 'leads', label: '客服线索工作台', icon: FiUserCheck, pages: [{ path: '/customer-leads', label: '客服线索工作台' }] },
    { id: 'crm', label: '客户管理', icon: FiUsers, pages: [{ path: '/customer-management', label: '客户管理工作台' }] },
    {
        id: 'growth', label: '风控与增长', icon: FiShield,
        pages: [
            { path: '/risk-control', label: '风控台' },
            { path: '/distribution', label: '分销邀请' },
            { path: '/report-center', label: '报表导出' },
        ],
    },
    {
        id: 'settings', label: '系统设置', icon: FiSettings, pinned: true,
        pages: [
            { path: '/ai-config', label: 'AI 配置' },
            { path: '/oss', label: 'OSS 管理' },
            { path: '/logs', label: '日志管理' },
        ],
    },
];

/**
 * 当前账号可见的新导航分组。
 * - 非 platform_admin（含 scope 缺失）：只有客户管理（fail-closed，与旧导航一致）；
 * - platform_admin：未启用 CRM 时不显示"客户管理"，其余分组全部可见。
 */
export function visibleGroups(scope: string | undefined, crmEnabled: boolean | undefined): NavGroup[] {
    const crm = navGroups.find((g) => g.id === 'crm')!;
    if (scope !== 'platform_admin') return [crm];
    return navGroups.filter((g) => g.id !== 'crm' || crmEnabled);
}

export interface NavLocation {
    group: NavGroup;
    page: NavPage;
    /** 是否位于该分组的二级页（如充值配置、会员计划） */
    inSecondary: boolean;
}

/** 根据当前路径找到所属分组；不在任何分组里返回 undefined。 */
export function locate(pathname: string, groups: NavGroup[] = navGroups): NavLocation | undefined {
    for (const group of groups) {
        const page = group.pages.find((p) => p.path === pathname) ?? group.related?.find((p) => p.path === pathname);
        if (page) return { group, page, inSecondary: false };
        const secondary = group.secondary?.pages.find((p) => p.path === pathname);
        if (secondary) return { group, page: secondary, inSecondary: true };
    }
    return undefined;
}

/** ⌘K 页面搜索的条目：当前账号可见的所有页面，含二级页与关联页。 */
export function searchablePages(groups: NavGroup[]): { path: string; label: string; group: string }[] {
    return groups.flatMap((g) => [
        ...g.pages.map((p) => ({ path: p.path, label: p.label, group: g.label })),
        ...(g.related ?? []).map((p) => ({ path: p.path, label: p.label, group: g.label })),
        ...(g.secondary?.pages ?? []).map((p) => ({ path: p.path, label: p.label, group: `${g.label} › ${g.secondary!.label}` })),
    ]);
}
