import React from 'react';
import { Link } from 'react-router-dom';
import { FiChevronRight } from 'react-icons/fi';
import type { NavGroup, NavLocation } from './navConfig';
import { useLeadOverviewCounts } from './leadOverviewStore';
import './subnavCards.scss';

interface Props {
    group: NavGroup;
    location: NavLocation;
}

/**
 * 卡片式分组导航（display: 'cards'）：位于顶栏与内容区之间，与文字 Tab 的 SubNav 二选一。
 * 销售线索的 客户 · 线索 · 商机 三个页面都显示它，当前所在的那张高亮，所以任何一页都能切到另外两类。
 */
const SubNavCards: React.FC<Props> = ({ group, location }) => {
    const counts = useLeadOverviewCounts();
    return (
        <nav className="layout-subnav-cards" aria-label={`${group.label}分类`}>
            {group.pages.map((page) => {
                const Icon = page.icon;
                const current = page.path === location.page.path;
                const count = page.badgeKey ? counts[page.badgeKey] ?? 0 : 0;
                // 「未启用」是纯占位（如商机），没有可看的页面，渲染为禁用态而非可点击链接；
                // 「未接入」（如未接 CRM 的客户）页面本身的基础栏目可用，保持可点，仅悬停说明
                if (page.note === '未启用') {
                    return (
                        <span
                            key={page.path}
                            className={`subnav-card tone-${page.tone ?? 'blue'} is-disabled`}
                            title="该模块规划中，尚未启用"
                        >
                            <span className="subnav-card-label">{page.label}</span>
                            <small>{page.note}</small>
                            {Icon && <Icon className="subnav-card-icon" aria-hidden />}
                        </span>
                    );
                }
                return (
                    <Link
                        key={page.path}
                        to={page.path}
                        className={`subnav-card tone-${page.tone ?? 'blue'} ${current ? 'is-current' : ''}`}
                        aria-current={current ? 'page' : undefined}
                        title={page.note === '未接入' ? 'CRM 未接入，仅基础栏目可用' : undefined}
                    >
                        <span className="subnav-card-label">{page.label}</span>
                        {count > 0 && <span className="subnav-badge" aria-label={`${count} 条待处理`}>{count > 99 ? '99+' : count}</span>}
                        {page.note && <small>{page.note}</small>}
                        {!page.note && <FiChevronRight className="subnav-card-arrow" />}
                        {Icon && <Icon className="subnav-card-icon" aria-hidden />}
                    </Link>
                );
            })}
        </nav>
    );
};

export default SubNavCards;
