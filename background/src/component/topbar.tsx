import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    FiMenu,
    FiSearch,
    FiLogOut,
    FiMonitor,
} from 'react-icons/fi';
import type { FeatherIcon } from '../compat/react-icons-fi';
import { logout } from '../api/auth';
import { useManagementSession } from './ManagementSession';
import './topbar.scss';

interface TopbarProps {
    title?: string;
    icon?: FeatherIcon;
    collapsed?: boolean;
    onToggleSidebar?: () => void;
    onSearch?: () => void;
}

const Topbar: React.FC<TopbarProps> = ({ title = '管理后台', icon, collapsed = false, onToggleSidebar, onSearch }) => {
    const navigate = useNavigate();
    const session = useManagementSession();
    const [showUserMenu, setShowUserMenu] = useState(false);

    // 用户名直接取会话上下文：不再每次导航重复请求 /me
    const username = session.user?.username || '管理员';
    const DisplayIcon = icon || FiMonitor;

    const handleLogout = async () => {
        try {
            await logout();
            navigate('/login');
        } catch (error) {
            console.error('登出失败:', error);
            // 即使登出失败也跳转到登录页
            navigate('/login');
        }
    };

    return (
        <header className="layout-topbar">
            <div className="topbar-left">
                {onToggleSidebar && (
                    <button className="topbar-menu-btn" type="button" aria-label={collapsed ? '展开导航' : '收起导航'} aria-expanded={!collapsed} aria-controls="admin-sidebar" onClick={onToggleSidebar}>
                        <FiMenu />
                    </button>
                )}
                <h1 className="topbar-title">
                    <DisplayIcon className="topbar-title-icon" />
                    <span>{title}</span>
                </h1>
            </div>
            <div className="topbar-right">
                {onSearch && (
                    <button className="topbar-search" type="button" onClick={onSearch} aria-label="搜索页面（Ctrl/⌘ K）">
                        <FiSearch />
                        <span className="topbar-search-text">搜索页面</span>
                        <kbd>⌘K</kbd>
                    </button>
                )}
                <div className="topbar-user" onClick={() => setShowUserMenu(!showUserMenu)}>
                    <div className="topbar-avatar">{username[0]?.toUpperCase() || 'A'}</div>
                    <span className="topbar-username">{username}</span>
                    {showUserMenu && (
                        <div className="user-menu">
                            <button className="user-menu-item" onClick={handleLogout}>
                                <FiLogOut />
                                登出
                            </button>
                        </div>
                    )}
                </div>
            </div>
        </header>
    );
};

export default Topbar;
