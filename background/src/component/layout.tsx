import React, { useEffect, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import type { PageLoadFailure } from '../utils/pageLoadError';
import Sidebar from './sidebar';
import Topbar from './topbar';
import Bookmark, { useBookmark } from './bookmark';
import './layout.scss';

interface LayoutProps {
    title?: string;
    children: React.ReactNode;
}

const Layout: React.FC<LayoutProps> = ({ title = '管理后台', children }) => {
    const { pathname } = useLocation();
    const [loadFailure, setLoadFailure] = useState('');
    const [collapsed, setCollapsed] = useState(() => window.matchMedia('(max-width: 1024px)').matches);
    const [isMobile, setIsMobile] = useState(() => window.matchMedia('(max-width: 1024px)').matches);
    const contentRef = useRef<HTMLDivElement>(null);
    const { refreshKey } = useBookmark();

    useEffect(() => {
        setLoadFailure('');
        const report = (event: Event) => {
            const detail = (event as CustomEvent<PageLoadFailure>).detail;
            if (detail?.path === window.location.pathname) setLoadFailure(detail.message);
        };
        window.addEventListener('admin-page-load-error', report);
        return () => window.removeEventListener('admin-page-load-error', report);
    }, [pathname, refreshKey]);

    useEffect(() => {
        const query = window.matchMedia('(max-width: 1024px)');
        const updateViewportState = () => {
            setIsMobile(query.matches);
            setCollapsed(query.matches);
        };
        query.addEventListener('change', updateViewportState);
        return () => query.removeEventListener('change', updateViewportState);
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

        let frame = 0;
        const scheduleSync = () => {
            if (!frame) frame = requestAnimationFrame(() => { frame = 0; syncTables(); });
        };
        // Write attributes outside the resize delivery cycle: the scroll hint changes height.
        const resizeObserver = new ResizeObserver(scheduleSync);
        const observed = new Set<Element>();
        const syncTables = () => {
            root.querySelectorAll('table').forEach((table) => {
                const region = table.parentElement;
                if (!region) return;
                const overflowing = region.clientWidth > 0 && region.scrollWidth > region.clientWidth + 1;
                region.dataset.adminTableScroll = overflowing ? 'overflow' : 'fit';
                region.tabIndex = overflowing ? 0 : -1;
                region.setAttribute('role', 'region');
                region.setAttribute('aria-label', '数据表格' + (overflowing ? '，可左右滚动查看完整内容' : ''));
                [region, table].forEach(element => {
                    if (!observed.has(element)) { resizeObserver.observe(element); observed.add(element); }
                });
            });
            observed.forEach(element => {
                if (!root.contains(element)) { resizeObserver.unobserve(element); observed.delete(element); }
            });
        };
        syncTables();
        const observer = new MutationObserver(scheduleSync);
        observer.observe(root, { childList: true, subtree: true });
        return () => { observer.disconnect(); resizeObserver.disconnect(); cancelAnimationFrame(frame); };
    }, [refreshKey]);

    useEffect(() => {
        if (!isMobile || collapsed) return;
        const sidebar = document.getElementById('admin-sidebar');
        const previous = document.activeElement as HTMLElement | null;
        const links = () => Array.from(sidebar?.querySelectorAll<HTMLElement>('a[href],button') || []);
        links()[0]?.focus();
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape') setCollapsed(true);
            if (event.key !== 'Tab') return;
            const items = links();
            const first = items[0], last = items[items.length - 1];
            if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
            else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
        };
        window.addEventListener('keydown', onKey);
        return () => { window.removeEventListener('keydown', onKey); previous?.focus(); };
    }, [isMobile, collapsed]);

    return (
        <div className="layout-container">
            <Sidebar collapsed={collapsed} mobile={isMobile} onNavigate={handleSidebarNavigate} />
            {isMobile && !collapsed ? <button className="layout-overlay" type="button" onClick={handleToggleSidebar} aria-label="关闭侧边栏" /> : null}
            <div className="layout-main" {...(isMobile && !collapsed ? { inert: '' } : {})}>
                <Topbar title={title} collapsed={collapsed} onToggleSidebar={handleToggleSidebar} />
                <Bookmark />
                <div className="layout-content" data-admin-page={pathname} key={refreshKey} ref={contentRef}>
                    {loadFailure && <div className="page-load-error" role="alert"><div><strong>页面数据未能加载</strong><p>{loadFailure}</p><small>下方仅显示页面结构，统计值和空列表不代表实际业务数据。</small></div><button type="button" onClick={() => window.location.reload()}>重新加载</button></div>}
                    {children}
                </div>
            </div>
        </div>
    );
};

export default Layout;
