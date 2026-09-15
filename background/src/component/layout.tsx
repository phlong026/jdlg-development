import React, { useEffect, useRef, useState } from 'react';
import Sidebar from './sidebar';
import Topbar from './topbar';
import Bookmark, { useBookmark } from './bookmark';
import './layout.scss';

interface LayoutProps {
    title?: string;
    children: React.ReactNode;
}

const Layout: React.FC<LayoutProps> = ({ title = '管理后台', children }) => {
    const [collapsed, setCollapsed] = useState(false);
    const [isMobile, setIsMobile] = useState(false);
    const contentRef = useRef<HTMLDivElement>(null);
    const { refreshKey } = useBookmark();

    useEffect(() => {
        const updateViewportState = () => {
            const nextIsMobile = window.innerWidth <= 1024;
            setIsMobile(nextIsMobile);
            setCollapsed(nextIsMobile);
        };
        updateViewportState();
        window.addEventListener('resize', updateViewportState);
        return () => window.removeEventListener('resize', updateViewportState);
    }, []);

    const handleToggleSidebar = () => {
        setCollapsed((prev) => !prev);
    };

    const handleSidebarNavigate = () => {
        if (isMobile) {
            setCollapsed(true);
        }
    };

    useEffect(() => {
        const root = contentRef.current;
        if (!root) return;

        const syncTableLabels = () => {
            root.querySelectorAll('table').forEach((table) => {
                const headers = Array.from(table.querySelectorAll('thead th')).map((header) =>
                    header.textContent?.replace(/\s+/g, ' ').trim() || ''
                );

                table.querySelectorAll('tbody tr').forEach((row) => {
                    Array.from(row.children).forEach((cell, index) => {
                        if (cell instanceof HTMLElement && headers[index]) {
                            cell.dataset.label = headers[index];
                        }
                    });
                });
            });
        };

        syncTableLabels();
        const observer = new MutationObserver(syncTableLabels);
        observer.observe(root, { childList: true, subtree: true });

        return () => observer.disconnect();
    }, [children, refreshKey]);

    return (
        <div className="layout-container">
            <Sidebar collapsed={collapsed} mobile={isMobile} onNavigate={handleSidebarNavigate} />
            {isMobile && !collapsed ? <button className="layout-overlay" type="button" onClick={handleToggleSidebar} aria-label="关闭侧边栏" /> : null}
            <div className="layout-main">
                <Topbar title={title} collapsed={collapsed} onToggleSidebar={handleToggleSidebar} />
                <Bookmark />
                <div className="layout-content" key={refreshKey} ref={contentRef}>
                    {children}
                </div>
            </div>
        </div>
    );
};

export default Layout;
