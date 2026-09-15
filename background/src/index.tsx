import React, { useState } from 'react';
import Sidebar from './component/sidebar';
import Topbar from './component/topbar';
import './index.scss';

interface IndexLayoutProps {
    children?: React.ReactNode;
}

/**
 * 初始化页面 - 负责初始化侧边栏和顶栏
 * 不包含工作台内容，工作台由独立页面处理
 */
const IndexLayout: React.FC<IndexLayoutProps> = ({ children }) => {
    const [collapsed, setCollapsed] = useState(false);

    const handleToggleSidebar = () => {
        setCollapsed((prev) => !prev);
    };

    return (
        <div className="index-layout">
            <Sidebar collapsed={collapsed} />
            <div className="index-main">
                <Topbar title="Delta 后台管理" collapsed={collapsed} onToggleSidebar={handleToggleSidebar} />
                <div className="index-content">
                    {/* 内容区域 - 通过路由渲染不同页面 */}
                    {children}
                </div>
            </div>
        </div>
    );
};

export default IndexLayout;