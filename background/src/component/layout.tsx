import React, { useEffect, useRef, useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import type { PageLoadFailure } from '../utils/pageLoadError';
import { updatePageLoadFailures } from '../utils/pageLoadError';
import Sidebar from './sidebar';
import Topbar from './topbar';
import SubNav from './SubNav';
import SubNavCards from './SubNavCards';
import CommandPalette from './CommandPalette';
import { installAdminDialogKeyboard } from './adminDialogKeyboard';
import { useManagementSession } from './ManagementSession';
import { locate, searchablePages, visibleGroups } from './navConfig';
import { readNavMode, saveNavMode, type NavMode } from './navMode';
import { routes } from '../routes';
import './layout.scss';

/**
 * 路由层的统一布局：侧栏、顶栏只挂载一次，页面通过 <Outlet /> 渲染在内容区。
 * 以前每个页面各自包一层 <Layout>，每次跳转都会重建侧栏与顶栏（并重复请求 /me）。
 *
 * 必须保持的约定（24 个 scss 依赖它们）：
 *   - 内容区类名 .layout-content，且 data-admin-page 等于当前路径；
 *   - 内容区内的 <table> 由下面的 MutationObserver 统一加上横向滚动提示。
 */
const AdminLayout: React.FC = () => {
    const { pathname } = useLocation();
    const [loadFailures, setLoadFailures] = useState<Record<string, string>>({});
    const loadFailure = Object.values(loadFailures).join('；');
    const [collapsed, setCollapsed] = useState(() => window.matchMedia('(max-width: 1024px)').matches);
    const [isMobile, setIsMobile] = useState(() => window.matchMedia('(max-width: 1024px)').matches);
    const contentRef = useRef<HTMLDivElement>(null);
    const current = routes.find((route) => route.path === pathname);
    const session = useManagementSession();
    const [navMode, setNavMode] = useState<NavMode>(readNavMode);
    const [searchOpen, setSearchOpen] = useState(false);
    const groups = visibleGroups(session.user?.scope, session.user?.crmEnabled);
    // 在当前账号可见的分组里定位：启用 CRM 时 /customer-management 属于「销售线索」的二级页
    const where = locate(pathname, groups);
    const subnavGroup = where?.group;

    const toggleNavMode = () => {
        const next: NavMode = navMode === 'new' ? 'old' : 'new';
        saveNavMode(next);
        setNavMode(next);
    };

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
                event.preventDefault();
                setSearchOpen((open) => !open);
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, []);

    useEffect(() => {
        setLoadFailures({});
        // 内容区现在是同一个元素：切换页面时要手动回到顶部（以前每页重建，天然从顶部开始）
        contentRef.current?.scrollTo({ top: 0 });
        const report = (event: Event) => {
            const detail = (event as CustomEvent<PageLoadFailure>).detail;
            if (detail?.path === window.location.pathname) setLoadFailures((current) => updatePageLoadFailures(current, detail));
        };
        window.addEventListener('admin-page-load-error', report);
        return () => window.removeEventListener('admin-page-load-error', report);
    }, [pathname]);

    useEffect(() => {
        const root = contentRef.current;
        if (root) return installAdminDialogKeyboard(root);
    }, [pathname]);

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
        // 内容区是同一个节点，页面切换时靠子树变化触发重新同步
        const observer = new MutationObserver(scheduleSync);
        observer.observe(root, { childList: true, subtree: true });
        return () => { observer.disconnect(); resizeObserver.disconnect(); cancelAnimationFrame(frame); };
    }, []);

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
            <Sidebar collapsed={collapsed} mobile={isMobile} mode={navMode} onNavigate={handleSidebarNavigate} onToggleMode={toggleNavMode} />
            {isMobile && !collapsed ? <button className="layout-overlay" type="button" onClick={handleToggleSidebar} aria-label="关闭侧边栏" /> : null}
            <div className="layout-main" {...(isMobile && !collapsed ? { inert: '' } : {})}>
                <Topbar title={current?.title} icon={current?.icon} collapsed={collapsed} onToggleSidebar={handleToggleSidebar} onSearch={() => setSearchOpen(true)} />
                {navMode === 'new' && where && subnavGroup
                    ? (subnavGroup.display === 'cards' ? <SubNavCards group={subnavGroup} location={where} /> : <SubNav group={subnavGroup} location={where} />)
                    : null}
                <div className="layout-content" data-admin-page={pathname} ref={contentRef}>
                    {loadFailure && <div className="page-load-error" role="alert"><div><strong>页面数据未能加载</strong><p>{loadFailure}</p><small>部分数据未能加载，相关统计或列表可能不完整，请重试后再据此操作。</small></div><button type="button" onClick={() => window.location.reload()}>重新加载</button></div>}
                    <Outlet />
                </div>
            </div>
            <CommandPalette open={searchOpen} items={searchablePages(groups)} onClose={() => setSearchOpen(false)} />
        </div>
    );
};

export default AdminLayout;
