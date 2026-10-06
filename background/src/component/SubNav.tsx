import React from 'react';
import { Link } from 'react-router-dom';
import { FiArrowLeft, FiSettings } from 'react-icons/fi';
import type { NavGroup, NavLocation } from './navConfig';
import { useLeadOverviewCounts } from './leadOverviewStore';
import './subnav.scss';

interface SubNavProps {
    /** 当前账号可见的该分组（已按权限 / CRM 开关裁剪过 Tab） */
    group: NavGroup;
    location: NavLocation;
}

/**
 * 分组内的子导航条，位于顶栏与内容区之间（内容区本身不变，页面样式不受影响）。
 * - 分组页：显示同组页面的 Tab（可带数量角标）；有二级页时右侧给出入口（如 订单与支付 → 支付与会员配置）；
 * - 二级页：显示二级页之间的 Tab 和"返回分组"；
 * - 只有一个页面且没有二级页入口时不渲染；二级页即使只有一个页面也要保留「返回」链接。
 */
const SubNav: React.FC<SubNavProps> = ({ group, location }) => {
    const counts = useLeadOverviewCounts();
    const inSecondary = location.inSecondary && !!group.secondary;
    const tabs = inSecondary ? group.secondary!.pages : group.pages;
    // 详情页（如用户 360）不在 Tab 里：高亮其所属分组的落地 Tab
    const activePath = group.pages.some((p) => p.path === location.page.path) || inSecondary ? location.page.path : group.pages[0].path;
    const showEntry = !inSecondary && !!group.secondary;

    if (!inSecondary && tabs.length <= 1 && !showEntry) return null;

    return (
        <nav className="layout-subnav" aria-label={`${group.label}子导航`}>
            {inSecondary && (
                <Link className="subnav-back" to={group.pages[0].path}>
                    <FiArrowLeft />
                    返回{group.label}
                </Link>
            )}
            {tabs.length > 1 && tabs.map((page) => {
                const count = page.badgeKey ? counts[page.badgeKey] ?? 0 : 0;
                return (
                    <Link
                        key={page.path}
                        to={page.path}
                        className={`subnav-tab ${page.path === activePath ? 'active' : ''}`}
                        aria-current={page.path === activePath ? 'page' : undefined}
                    >
                        {page.label}
                        {count > 0 && <span className="subnav-badge" aria-label={`${count} 条待处理`}>{count > 99 ? '99+' : count}</span>}
                    </Link>
                );
            })}
            <span className="subnav-spacer" />
            {showEntry && (
                <Link className="subnav-entry" to={group.secondary!.pages[0].path}>
                    <FiSettings />
                    {group.secondary!.label}
                </Link>
            )}
        </nav>
    );
};

export default SubNav;
