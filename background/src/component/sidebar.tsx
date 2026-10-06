import React from 'react';
import { NavLink } from 'react-router-dom';
import './sidebar.scss';
import { useManagementSession } from './ManagementSession';
import { visibleSections } from './navConfig';

interface SidebarProps {
    collapsed?: boolean;
    mobile?: boolean;
    onNavigate?: () => void;
}

const Sidebar: React.FC<SidebarProps> = ({ collapsed = false, mobile = false, onNavigate }) => {
    const session = useManagementSession();
    const sections = visibleSections(session.user?.scope, session.user?.crmEnabled);
    return (
        <aside id="admin-sidebar" aria-label="后台导航" {...(mobile && collapsed ? { inert: '' } : {})} className={`layout-sidebar ${collapsed ? 'collapsed' : ''} ${mobile ? 'mobile' : ''} ${mobile && !collapsed ? 'open' : ''}`}>
            <div className="sidebar-logo">
                <span className="logo-mark">AI</span>
                {!collapsed && <span className="logo-text">管理后台</span>}
            </div>

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
        </aside>
    );
};

export default Sidebar;
