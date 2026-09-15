import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    FiMenu,
    FiLogOut,
    FiMonitor,
} from 'react-icons/fi';
import { logout, getMe } from '../api/auth';
import { useBookmark } from './bookmark';
import './topbar.scss';

interface TopbarProps {
    title?: string;
    collapsed?: boolean;
    onToggleSidebar?: () => void;
}

const Topbar: React.FC<TopbarProps> = ({ title = '管理后台', collapsed = false, onToggleSidebar }) => {
    const navigate = useNavigate();
    const [username, setUsername] = useState('管理员');
    const [showUserMenu, setShowUserMenu] = useState(false);

    // 获取当前激活的书签（Topbar 在 BookmarkProvider 内部，可以直接使用）
    const { getActiveBookmark } = useBookmark();
    const activeBookmark = getActiveBookmark();

    // 根据当前激活的书签或路径确定显示的标题和图标
    const displayTitle = activeBookmark?.title || title;
    const DisplayIcon = activeBookmark?.icon || FiMonitor;

    useEffect(() => {
        // 加载用户信息
        const loadUserInfo = async () => {
            try {
                const user = await getMe();
                setUsername(user.username || '管理员');
            } catch (error) {
                console.error('获取用户信息失败:', error);
            }
        };
        loadUserInfo();
    }, []);

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
                    <button className="topbar-menu-btn" type="button" onClick={onToggleSidebar}>
                        <FiMenu />
                    </button>
                )}
                <h1 className="topbar-title">
                    <DisplayIcon className="topbar-title-icon" />
                    {displayTitle}
                </h1>
            </div>
            <div className="topbar-right">
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

