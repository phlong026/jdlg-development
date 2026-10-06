import React from 'react';
import { Link, NavLink, useLocation } from 'react-router-dom';
import './sidebar.scss';
import { useManagementSession } from './ManagementSession';
import { locate, visibleGroups, visibleSections } from './navConfig';
import type { NavMode } from './navMode';

interface SidebarProps {
    collapsed?: boolean;
    mobile?: boolean;
    mode?: NavMode;
    onNavigate?: () => void;
    onToggleMode?: () => void;
}

const Sidebar: React.FC<SidebarProps> = ({ collapsed = false, mobile = false, mode = 'new', onNavigate, onToggleMode }) => {
    const session = useManagementSession();
    const { pathname } = useLocation();
    const scope = session.user?.scope;
    const crmEnabled = session.user?.crmEnabled;
    const asideProps = {
        id: 'admin-sidebar',
        'aria-label': '后台导航',
        className: `layout-sidebar ${collapsed ? 'collapsed' : ''} ${mobile ? 'mobile' : ''} ${mobile && !collapsed ? 'open' : ''}`,
        ...(mobile && collapsed ? { inert: '' } : {}),
    };
    const logo = (
        <div className="sidebar-logo">
            <span className="logo-mark">AI</span>
            {!collapsed && <span className="logo-text">管理后台</span>}
        </div>
    );
    const toggle = !collapsed && onToggleMode ? (
        <button type="button" className="sidebar-mode-toggle" onClick={onToggleMode}>
            {mode === 'new' ? '切换到旧导航' : '返回新导航'}
        </button>
    ) : null;

    if (mode === 'old') {
        const sections = visibleSections(scope, crmEnabled);
        return (
            <aside {...asideProps}>
                {logo}
                <nav className="sidebar-menu">
                    {sections.map((section) => (
                        <div key={section.title} className="sidebar-section">
                            {!collapsed && <div className="sidebar-section-title">{section.title}</div>}
                            <div className="sidebar-section-items">
                                {section.items.map((item) => {
                                    const Icon = item.icon;
                                    return (
                                        <NavLink key={item.path} to={item.path} aria-label={item.label} title={collapsed ? item.label : undefined} className="sidebar-item" onClick={onNavigate}>
                                            <Icon className="sidebar-icon" />
                                            {!collapsed && <span className="sidebar-label">{item.label}</span>}
                                        </NavLink>
                                    );
                                })}
                            </div>
                        </div>
                    ))}
                </nav>
                {toggle}
            </aside>
        );
    }

    const groups = visibleGroups(scope, crmEnabled);
    const activeId = locate(pathname)?.group.id;
    const renderItem = (group: (typeof groups)[number]) => {
        const Icon = group.icon;
        const active = group.id === activeId;
        return (
            <Link
                key={group.id}
                to={group.pages[0].path}
                aria-label={group.label}
                aria-current={active ? 'page' : undefined}
                title={collapsed ? group.label : undefined}
                className={`sidebar-item ${active ? 'active' : ''}`}
                onClick={onNavigate}
            >
                <Icon className="sidebar-icon" />
                {!collapsed && <span className="sidebar-label">{group.label}</span>}
            </Link>
        );
    };
    const pinned = groups.filter((g) => g.pinned);
    return (
        <aside {...asideProps}>
            {logo}
            <nav className="sidebar-menu sidebar-groups">
                <div className="sidebar-section-items">{groups.filter((g) => !g.pinned).map(renderItem)}</div>
            </nav>
            {pinned.length > 0 && <nav className="sidebar-pinned" aria-label="系统设置">{pinned.map(renderItem)}</nav>}
            {toggle}
        </aside>
    );
};

export default Sidebar;
